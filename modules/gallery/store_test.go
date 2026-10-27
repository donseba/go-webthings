package gallery

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"slices"
	"testing"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"
)

// TestStore runs the schema and media tracking on every configured dialect.
func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "gallery", schema()); err != nil {
			t.Fatal(err)
		}

		storage, err := media.NewFilesystem(t.TempDir(), "/media")
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = storage.Close() })

		library := media.New(database, storage)

		var ids []string

		for width := 3; width <= 4; width++ {
			var buffer bytes.Buffer

			if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, width, 2))); err != nil {
				t.Fatal(err)
			}

			asset, err := library.Upload(ctx, "photo.png", &buffer)
			if err != nil {
				t.Fatal(err)
			}

			ids = append(ids, asset.ID)
		}

		albums, err := content.NewCollection(database, albumSpec())
		if err != nil {
			t.Fatal(err)
		}

		saved, err := albums.Save(ctx, "photos", content.Draft[AlbumFields, AlbumText]{
			Fields: AlbumFields{Images: content.IDs{ids[1], ids[0]}},
			Translations: []content.Translation[AlbumText]{
				{Locale: "en", Text: AlbumText{Slug: "trip", Title: "Trip"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		if saved.Fields.Cover != "" || !slices.Equal(saved.Fields.Images, content.IDs{ids[1], ids[0]}) {
			t.Fatalf("saved fields: %+v", saved.Fields)
		}

		if err := library.Delete(ctx, ids[0]); !errors.Is(err, media.ErrInUse) {
			t.Fatalf("delete used image: %v", err)
		}

		if err := albums.Delete(ctx, "photos", saved.ID); err != nil {
			t.Fatal(err)
		}

		// Deleted content keeps its media until it is purged from the trash.
		if err := library.Delete(ctx, ids[0]); !errors.Is(err, media.ErrInUse) {
			t.Fatalf("delete image of a deleted album: %v", err)
		}

		if err := albums.Purge(ctx, "photos", saved.ID); err != nil {
			t.Fatal(err)
		}

		if err := library.Delete(ctx, ids[0]); err != nil {
			t.Fatalf("delete unused image: %v", err)
		}
	})
}

func TestOrdered(t *testing.T) {
	got := ordered([]string{"c", "a", "b"}, []string{"a", "b", "d", "c"})
	if !slices.Equal(got, content.IDs{"c", "a", "b", "d"}) {
		t.Fatalf("keep stored order: %v", got)
	}

	got = ordered([]string{"c", "a", "b"}, []string{"b", "d"})
	if !slices.Equal(got, content.IDs{"b", "d"}) {
		t.Fatalf("drop unchecked: %v", got)
	}
}

func TestMoved(t *testing.T) {
	ids := content.IDs{"a", "b", "c"}

	cases := []struct {
		id   string
		step int
		want content.IDs
	}{
		{"b", -1, content.IDs{"b", "a", "c"}},
		{"b", 1, content.IDs{"a", "c", "b"}},
		{"a", -1, ids},
		{"c", 1, ids},
		{"x", 1, ids},
	}

	for _, c := range cases {
		if got := moved(ids, c.id, c.step); !slices.Equal(got, c.want) {
			t.Errorf("%s %d: %v, want %v", c.id, c.step, got, c.want)
		}
	}

	if !slices.Equal(ids, content.IDs{"a", "b", "c"}) {
		t.Fatalf("changed the input: %v", ids)
	}
}
