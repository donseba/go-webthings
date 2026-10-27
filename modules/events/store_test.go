package events

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"
)

func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "events", schema()); err != nil {
			t.Fatal(err)
		}

		store, err := content.NewRevisions(database, eventSpec())
		if err != nil {
			t.Fatal(err)
		}

		fields, err := schedule(eventInput{
			Start:    wall(t, "2026-03-29T01:30"),
			End:      wall(t, "2026-03-29T04:00"),
			Timezone: admin.TimezoneChoices("Europe/Amsterdam"),
		})
		if err != nil {
			t.Fatal(err)
		}

		saved, err := store.Save(ctx, "agenda", content.Draft[EventFields, EventText]{
			Fields: fields,
			Translations: []content.Translation[EventText]{
				{Locale: "en", Text: EventText{Slug: "night", Title: "Night"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		// 01:30 is still winter time, 04:00 already summer time.
		start := time.Date(2026, 3, 29, 0, 30, 0, 0, time.UTC)
		end := time.Date(2026, 3, 29, 2, 0, 0, 0, time.UTC)

		got := saved.Fields
		if !got.Start.Equal(start) || got.End == nil || !got.End.Equal(end) || got.AllDay || got.Image != "" {
			t.Fatalf("fields: %+v", got)
		}

		// An all-day event without end is stored without one.
		saved, err = store.Save(ctx, "agenda", content.Draft[EventFields, EventText]{
			ID:           saved.ID,
			Fields:       EventFields{Start: start, AllDay: true, Timezone: "UTC"},
			Translations: saved.Translations,
		})
		if err != nil {
			t.Fatal(err)
		}

		if saved.Fields.End != nil || !saved.Fields.AllDay {
			t.Fatalf("all-day fields: %+v", saved.Fields)
		}

		if want := time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC); !saved.Fields.until().Equal(want) {
			t.Fatalf("until: %v", saved.Fields.until())
		}

		invalid := map[error]EventFields{
			errTimezone: {Start: start, Timezone: "Mars/Olympus"},
			errEnd:      {Start: start, End: &start, Timezone: "UTC"},
		}

		for want, fields := range invalid {
			_, err := store.Save(ctx, "agenda", content.Draft[EventFields, EventText]{Fields: fields})
			if !errors.Is(err, want) {
				t.Fatalf("want %v, got %v", want, err)
			}
		}

		_, err = store.Save(ctx, "agenda", content.Draft[EventFields, EventText]{
			Fields: EventFields{Start: start, Timezone: "UTC"},
			Translations: []content.Translation[EventText]{
				{Locale: "en", Text: EventText{Slug: "past", Title: "Past"}},
			},
		})
		if !errors.Is(err, errPastSlug) {
			t.Fatalf("want reserved slug, got %v", err)
		}
	})
}
