package lists

import (
	"context"
	"errors"
	"testing"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"
)

// TestStore checks the schema and ordering on every configured database.
func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "module:lists", schema()); err != nil {
			t.Fatal(err)
		}

		entries, err := content.NewCollection(database, spec())
		if err != nil {
			t.Fatal(err)
		}

		var ids []string

		for _, title := range []string{"One", "Two", "Three"} {
			item, err := entries.Save(ctx, "faq", content.Draft[Fields, Text]{
				Fields:       Fields{Link: "/contact"},
				Translations: []content.Translation[Text]{{Locale: "en", Text: Text{Title: title}}},
			})
			if err != nil {
				t.Fatal(err)
			}

			ids = append(ids, item.ID)
		}

		for _, id := range ids {
			if err := entries.SetPublished(ctx, "faq", id, true, "en"); err != nil {
				t.Fatal(err)
			}
		}

		if err := entries.Move(ctx, "faq", ids[2], 0); err != nil {
			t.Fatal(err)
		}

		// Another list keeps its own entries.
		_, err = entries.Save(ctx, "services", content.Draft[Fields, Text]{
			Translations: []content.Translation[Text]{{Locale: "en", Text: Text{Title: "Other"}}},
		})
		if err != nil {
			t.Fatal(err)
		}

		published, err := entries.ListPublished(ctx, "faq", "en")
		if err != nil {
			t.Fatal(err)
		}

		var titles []string
		for _, item := range published {
			titles = append(titles, item.Translations[0].Text.Title)
		}

		if len(titles) != 3 || titles[0] != "Three" || titles[1] != "One" || titles[2] != "Two" {
			t.Fatalf("order: %v", titles)
		}

		if published[0].Fields.Image != "" || published[0].Fields.Link != "/contact" {
			t.Fatalf("fields: %+v", published[0].Fields)
		}

		_, err = entries.Save(ctx, "faq", content.Draft[Fields, Text]{
			Fields:       Fields{Link: "javascript:alert(1)"},
			Translations: []content.Translation[Text]{{Locale: "en", Text: Text{Title: "Bad"}}},
		})
		if !errors.Is(err, errInvalidLink) {
			t.Fatalf("want invalid link, got %v", err)
		}
	})
}

func TestValidLink(t *testing.T) {
	valid := []string{
		"",
		"/contact",
		"#pricing",
		"https://example.com/a?b=c",
		"http://example.com",
		"mailto:info@example.com",
		"tel:+31201234567",
	}

	for _, link := range valid {
		if !validLink(link) {
			t.Errorf("%q should be valid", link)
		}
	}

	invalid := []string{
		"javascript:alert(1)",
		"//evil.example",
		"https://",
		"example.com",
		"data:text/html,hi",
	}

	for _, link := range invalid {
		if validLink(link) {
			t.Errorf("%q should be invalid", link)
		}
	}
}
