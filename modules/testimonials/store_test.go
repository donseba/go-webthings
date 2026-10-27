package testimonials

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"
)

// TestStore runs the schema, ordering and media tracking on every configured dialect.
func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "testimonials", schema()); err != nil {
			t.Fatal(err)
		}

		storage, err := media.NewFilesystem(t.TempDir(), "/media")
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = storage.Close() })

		library := media.New(database, storage)

		var buffer bytes.Buffer

		if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
			t.Fatal(err)
		}

		photo, err := library.Upload(ctx, "alice.png", &buffer)
		if err != nil {
			t.Fatal(err)
		}

		items, err := content.NewCollection(database, spec())
		if err != nil {
			t.Fatal(err)
		}

		alice, err := items.Save(ctx, "quotes", content.Draft[Fields, Text]{
			Fields: Fields{Photo: photo.ID, Author: "Alice"},
			Translations: []content.Translation[Text]{
				{Locale: "en", Text: Text{Quote: "Great work.", Role: "Director"}},
				{Locale: "nl", Text: Text{Quote: "Goed werk.", Role: "Directeur"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		bob, err := items.Save(ctx, "quotes", content.Draft[Fields, Text]{
			Fields: Fields{Author: "Bob"},
			Translations: []content.Translation[Text]{
				{Locale: "en", Text: Text{Quote: "Fast."}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		if bob.Fields.Photo != "" {
			t.Fatalf("bob has a photo: %+v", bob.Fields)
		}

		for _, id := range []string{alice.ID, bob.ID} {
			if err := items.SetPublished(ctx, "quotes", id, true, "en"); err != nil {
				t.Fatal(err)
			}
		}

		if err := items.Move(ctx, "quotes", bob.ID, 0); err != nil {
			t.Fatal(err)
		}

		expectAuthors(t, items, "en", "Bob", "Alice")
		expectAuthors(t, items, "nl", "Alice")

		if err := library.Delete(ctx, photo.ID); !errors.Is(err, media.ErrInUse) {
			t.Fatalf("delete used photo: %v", err)
		}

		_, err = items.Save(ctx, "quotes", content.Draft[Fields, Text]{
			Fields: Fields{Author: strings.Repeat("a", 201)},
		})
		if !errors.Is(err, content.ErrTooLong) {
			t.Fatalf("long author: %v", err)
		}

		// The limit counts characters, as the form does: 200 Cyrillic
		// characters are 400 bytes and still fit.
		_, err = items.Save(ctx, "quotes", content.Draft[Fields, Text]{
			Fields:       Fields{Author: strings.Repeat("ж", 200)},
			Translations: []content.Translation[Text]{{Locale: "en", Text: Text{Quote: "Хорошо"}}},
		})
		if err != nil {
			t.Fatalf("author in Cyrillic: %v", err)
		}

		if err := items.Delete(ctx, "quotes", alice.ID); err != nil {
			t.Fatal(err)
		}

		// Deleted content keeps its media until it is purged from the trash.
		if err := library.Delete(ctx, photo.ID); !errors.Is(err, media.ErrInUse) {
			t.Fatalf("delete photo of a deleted testimonial: %v", err)
		}

		if err := items.Purge(ctx, "quotes", alice.ID); err != nil {
			t.Fatal(err)
		}

		if err := library.Delete(ctx, photo.ID); err != nil {
			t.Fatalf("delete unused photo: %v", err)
		}
	})
}

func expectAuthors(t *testing.T, items *content.Collection[Fields, Text], locale string, authors ...string) {
	t.Helper()

	published, err := items.ListPublished(context.Background(), "quotes", locale)
	if err != nil {
		t.Fatal(err)
	}

	var got []string

	for _, item := range published {
		got = append(got, item.Fields.Author)
	}

	if strings.Join(got, "|") != strings.Join(authors, "|") {
		t.Fatalf("%s: want %q, got %q", locale, authors, got)
	}
}
