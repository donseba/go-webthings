package banners

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"
)

// TestStore runs the schema, the window, ordering and validation on every
// configured dialect.
func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "banners", schema()); err != nil {
			t.Fatal(err)
		}

		items, err := content.NewCollection(database, spec())
		if err != nil {
			t.Fatal(err)
		}

		now := time.Now().UTC().Truncate(time.Second)
		yesterday := now.Add(-24 * time.Hour)
		tomorrow := now.Add(24 * time.Hour)

		open := save(t, items, "notice", "Open", Fields{Tone: ToneInfo})
		save(t, items, "notice", "Past", Fields{Tone: ToneInfo, Ends: &yesterday})
		future := save(t, items, "notice", "Future", Fields{Tone: ToneWarning, Starts: &tomorrow})
		save(t, items, "notice", "Current", Fields{Tone: ToneSuccess, Starts: &yesterday, Ends: &tomorrow})
		save(t, items, "hero", "Slide", Fields{Tone: ToneInfo})

		if future.Fields.Starts == nil || !future.Fields.Starts.Equal(tomorrow) || future.Fields.Ends != nil {
			t.Fatalf("window not stored: %+v", future.Fields)
		}

		expectLive(t, items, now, "Open", "Current")
		expectLive(t, items, tomorrow.Add(time.Hour), "Open", "Future")
		expectLive(t, items, yesterday.Add(-time.Hour), "Open", "Past")

		if err := items.Move(ctx, "notice", open.ID, 3); err != nil {
			t.Fatal(err)
		}

		expectLive(t, items, now, "Current", "Open")

		invalid := []struct {
			fields Fields
			text   Text
			want   error
		}{
			{Fields{Tone: "loud"}, Text{Title: "Tone"}, errTone},
			{Fields{Tone: ToneInfo, Starts: &tomorrow, Ends: &yesterday}, Text{Title: "Window"}, errWindow},
			{Fields{Tone: ToneInfo}, Text{Title: "Script", ButtonURL: "javascript:alert(1)"}, errButton},
			{Fields{Tone: ToneInfo}, Text{Title: "Host", ButtonURL: "//evil.example"}, errButton},
			{Fields{Tone: ToneInfo}, Text{Title: strings.Repeat("a", 201)}, content.ErrTooLong},
		}

		for _, tc := range invalid {
			_, err := items.Save(ctx, "notice", content.Draft[Fields, Text]{
				Fields:       tc.fields,
				Translations: []content.Translation[Text]{{Locale: "en", Text: tc.text}},
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("%s: want %v, got %v", tc.text.Title, tc.want, err)
			}
		}

		for _, address := range []string{"", "/en/contact", "https://example.com/x", "mailto:info@example.com"} {
			if !validButtonURL(address) {
				t.Fatalf("button address %q refused", address)
			}
		}
	})
}

// save adds a published banner with an English title.
func save(t *testing.T, items *content.Collection[Fields, Text], instance, title string, fields Fields) Item {
	t.Helper()

	ctx := context.Background()

	item, err := items.Save(ctx, instance, content.Draft[Fields, Text]{
		Fields:       fields,
		Translations: []content.Translation[Text]{{Locale: "en", Text: Text{Title: title}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := items.SetPublished(ctx, instance, item.ID, true, "en"); err != nil {
		t.Fatal(err)
	}

	return item
}

func expectLive(t *testing.T, items *content.Collection[Fields, Text], now time.Time, titles ...string) {
	t.Helper()

	published, err := items.ListPublished(context.Background(), "notice", "en")
	if err != nil {
		t.Fatal(err)
	}

	var got []string

	for _, item := range published {
		if live(item.Fields, now) {
			text, _ := item.Text("en")
			got = append(got, text.Title)
		}
	}

	if strings.Join(got, "|") != strings.Join(titles, "|") {
		t.Fatalf("at %s: want %q, got %q", now, titles, got)
	}
}
