package people

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

// TestStore runs the schema, slugs, ordering and media tracking on every
// configured dialect.
func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "people", schema()); err != nil {
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

		photo, err := library.Upload(ctx, "jane.png", &buffer)
		if err != nil {
			t.Fatal(err)
		}

		items, err := content.NewCollection(database, spec())
		if err != nil {
			t.Fatal(err)
		}

		store := items.In("team")

		jane, err := store.Save(ctx, content.Draft[Fields, Text]{
			Fields: Fields{Slug: "jane-doe", Name: "Jane Doe", Photo: photo.ID, Email: "jane@example.test"},
			Translations: []content.Translation[Text]{
				{Locale: "en", Text: Text{Group: "Board", Role: "Chair", Bio: "Leads the *board*."}},
				{Locale: "nl", Text: Text{Group: "Bestuur", Role: "Voorzitter"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		john, err := store.Save(ctx, content.Draft[Fields, Text]{
			Fields:       Fields{Slug: "john-smith", Name: "John Smith", Phone: "+31 20 123 4567"},
			Translations: []content.Translation[Text]{{Locale: "en", Text: Text{Group: "Staff"}}},
		})
		if err != nil {
			t.Fatal(err)
		}

		// The same slug twice in one instance is refused by the database, and
		// uniqueSlug picks the next free one instead.
		_, err = store.Save(ctx, content.Draft[Fields, Text]{Fields: Fields{Slug: "jane-doe", Name: "Jane Doe"}})
		if !db.IsUnique(err) {
			t.Fatalf("duplicate slug: %v", err)
		}

		slug, err := uniqueSlug(ctx, store, "", "", "Jane Doe")
		if err != nil || slug != "jane-doe-2" {
			t.Fatalf("unique slug: %q %v", slug, err)
		}

		slug, err = uniqueSlug(ctx, store, jane.ID, "", "Jane Doe")
		if err != nil || slug != "jane-doe" {
			t.Fatalf("own slug: %q %v", slug, err)
		}

		if _, err := uniqueSlug(ctx, store, "", "john-smith", "Someone"); !errors.Is(err, content.ErrDuplicateSlug) {
			t.Fatalf("taken explicit slug: %v", err)
		}

		// Another instance has its own slugs.
		_, err = items.Save(ctx, "board", content.Draft[Fields, Text]{Fields: Fields{Slug: "jane-doe", Name: "Jane Doe"}})
		if err != nil {
			t.Fatalf("slug in another instance: %v", err)
		}

		for _, id := range []string{jane.ID, john.ID} {
			if err := store.Publish(ctx, id, "", "en"); err != nil {
				t.Fatal(err)
			}
		}

		expectNames(t, items, "en", "Jane Doe", "John Smith")
		expectNames(t, items, "nl", "Jane Doe")

		if err := items.Move(ctx, "team", john.ID, 0); err != nil {
			t.Fatal(err)
		}

		expectNames(t, items, "en", "John Smith", "Jane Doe")

		if err := library.Delete(ctx, photo.ID); !errors.Is(err, media.ErrInUse) {
			t.Fatalf("delete used photo: %v", err)
		}

		invalid := map[string]Fields{
			"email":   {Slug: "x", Name: "X", Email: "not an email"},
			"phone":   {Slug: "x", Name: "X", Phone: "call me"},
			"website": {Slug: "x", Name: "X", Website: "javascript:alert(1)"},
			"slug":    {Slug: "Not A Slug", Name: "X"},
			"name":    {Slug: "x", Name: strings.Repeat("a", 201)},
		}

		for name, fields := range invalid {
			if _, err := store.Save(ctx, content.Draft[Fields, Text]{Fields: fields}); err == nil {
				t.Fatalf("%s: saved %+v", name, fields)
			}
		}

		if err := store.Delete(ctx, jane.ID); err != nil {
			t.Fatal(err)
		}

		// Deleted content keeps its media until it is purged from the trash.
		if err := library.Delete(ctx, photo.ID); !errors.Is(err, media.ErrInUse) {
			t.Fatalf("delete photo of a deleted person: %v", err)
		}

		if err := store.Purge(ctx, jane.ID); err != nil {
			t.Fatal(err)
		}

		if err := library.Delete(ctx, photo.ID); err != nil {
			t.Fatalf("delete unused photo: %v", err)
		}
	})
}

func TestHelpers(t *testing.T) {
	slugs := map[string]string{
		"Jane Doe":             "jane-doe",
		"  Zoë  Ĳsselmeer! ":   "zoe-ijsselmeer",
		"Jean-Luc O'Brien 2nd": "jean-luc-o-brien-2nd",
		"李":                    "",
	}

	for name, want := range slugs {
		if got := content.Slug(name); got != want {
			t.Errorf("content.Slug(%q) = %q, want %q", name, got, want)
		}
	}

	if got := dialable(" +31 (0)20-123 4567 "); got != "+310201234567" {
		t.Errorf("dialable: %q", got)
	}

	list := groups([]Person{
		{Name: "A", Group: "Board"},
		{Name: "B", Group: "Staff"},
		{Name: "C", Group: "board"},
		{Name: "D"},
	})

	var got []string

	for _, group := range list {
		var names []string

		for _, person := range group.People {
			names = append(names, person.Name)
		}

		got = append(got, group.Name+":"+strings.Join(names, ","))
	}

	if strings.Join(got, "|") != "Board:A,C|Staff:B|:D" {
		t.Errorf("groups: %q", got)
	}
}

func expectNames(t *testing.T, items *content.Collection[Fields, Text], locale string, names ...string) {
	t.Helper()

	published, err := items.ListPublished(context.Background(), "team", locale)
	if err != nil {
		t.Fatal(err)
	}

	var got []string

	for _, item := range published {
		got = append(got, item.Fields.Name)
	}

	if strings.Join(got, "|") != strings.Join(names, "|") {
		t.Fatalf("%s: want %q, got %q", locale, names, got)
	}
}
