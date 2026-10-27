package site_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/site"
)

// TestTrashDays checks that the trash keeps items until someone deletes
// them unless TrashDays is set, that the setting is stored, and that it
// must lie between 0 and site.MaxTrashDays.
func TestTrashDays(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "site", site.Schema()); err != nil {
			t.Fatal(err)
		}

		settings := site.New(database)

		if err := settings.Init(ctx, "My site", locales.Config{Default: "en", Enabled: []string{"en"}}); err != nil {
			t.Fatal(err)
		}

		current, err := settings.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}

		now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

		if cutoff, ok := current.TrashCutoff(now); ok || current.TrashDays != 0 {
			t.Fatalf("a new site empties its trash: %d days, before %v", current.TrashDays, cutoff)
		}

		for _, days := range []int{-1, site.MaxTrashDays + 1} {
			current.TrashDays = days
			if err := settings.Save(ctx, current); !errors.Is(err, site.ErrInvalid) {
				t.Fatalf("%d days: %v", days, err)
			}
		}

		current.TrashDays = 30
		if err := settings.Save(ctx, current); err != nil {
			t.Fatal(err)
		}

		saved, err := settings.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}

		cutoff, ok := saved.TrashCutoff(now)
		if !ok || saved.TrashDays != 30 || !cutoff.Equal(now.Add(-30*24*time.Hour)) {
			t.Fatalf("30 days: %d days, before %v (%v)", saved.TrashDays, cutoff, ok)
		}

		saved.TrashDays = site.MaxTrashDays
		if err := settings.Save(ctx, saved); err != nil {
			t.Fatalf("the longest time: %v", err)
		}
	})
}
