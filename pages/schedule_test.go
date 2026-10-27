package pages_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/pages"
)

// TestScheduledPages checks that scheduled changes rebuild the routes like
// publishing by hand.
func TestScheduledPages(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := setup(t, database)
		now := time.Now()

		home, err := service.Home(ctx)
		if err != nil {
			t.Fatal(err)
		}

		about, err := service.Save(ctx, pages.Draft{
			Fields:       pages.Fields{ParentID: home},
			Translations: []content.Translation[pages.Text]{text("en", "about", "About")},
		})
		if err != nil {
			t.Fatal(err)
		}

		// Home cannot be taken offline, also not later.
		if err := service.Schedule(ctx, home, content.Schedule{UnpublishAt: now}); !errors.Is(err, pages.ErrInvalid) {
			t.Fatalf("home unpublication: %v", err)
		}

		plan := content.Schedule{RevisionID: about.Revision.ID, PublishAt: now.Add(time.Minute), UnpublishAt: now.Add(time.Hour)}
		if err := service.Schedule(ctx, about.ID, plan); err != nil {
			t.Fatal(err)
		}

		// Read the routes, so the cache holds the table without the page.
		if _, err := service.Resolve(ctx, "en", "/about"); !errors.Is(err, pages.ErrNotFound) {
			t.Fatalf("published early: %v", err)
		}

		if changes, err := service.RunDue(ctx, now, "en"); err != nil || changes != 0 {
			t.Fatalf("nothing due yet: %d %v", changes, err)
		}

		if changes, err := service.RunDue(ctx, now.Add(2*time.Minute), "en"); err != nil || changes != 1 {
			t.Fatalf("publication: %d %v", changes, err)
		}

		resolved, err := service.Resolve(ctx, "en", "/about")
		if err != nil || resolved.Page.ID != about.ID {
			t.Fatalf("scheduled page not routed: %+v %v", resolved.Route, err)
		}

		// The scheduled publication keeps the planned unpublication.
		kept, err := service.Latest(ctx, about.ID)
		if err != nil || !kept.Schedule.UnpublishAt.Equal(plan.UnpublishAt.Truncate(time.Second)) ||
			kept.Schedule.RevisionID != "" {
			t.Fatalf("plan after publication: %+v %v", kept.Schedule, err)
		}

		if changes, err := service.RunDue(ctx, now.Add(2*time.Hour), "en"); err != nil || changes != 1 {
			t.Fatalf("unpublication: %d %v", changes, err)
		}

		if _, err := service.Resolve(ctx, "en", "/about"); !errors.Is(err, pages.ErrNotFound) {
			t.Fatalf("unpublished page still routed: %v", err)
		}
	})
}

// TestManualPublishCancelsPageSchedule checks that the page tree cancels a
// page's plan when an editor publishes or unpublishes it by hand.
func TestManualPublishCancelsPageSchedule(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := setup(t, database)
		now := time.Now()

		home, err := service.Home(ctx)
		if err != nil {
			t.Fatal(err)
		}

		about, err := service.Save(ctx, pages.Draft{
			Fields:       pages.Fields{ParentID: home},
			Translations: []content.Translation[pages.Text]{text("en", "about", "About")},
		})
		if err != nil {
			t.Fatal(err)
		}

		plan := content.Schedule{
			RevisionID:  about.Revision.ID,
			PublishAt:   now.Add(time.Hour),
			UnpublishAt: now.Add(2 * time.Hour),
		}
		if err := service.Schedule(ctx, about.ID, plan); err != nil {
			t.Fatal(err)
		}

		if err := service.Publish(ctx, about.ID, about.Revision.ID); err != nil {
			t.Fatal(err)
		}

		loaded, err := service.Latest(ctx, about.ID)
		if err != nil || !loaded.Schedule.IsZero() {
			t.Fatalf("publish kept the plan: %+v %v", loaded.Schedule, err)
		}

		if changes, err := service.RunDue(ctx, now.Add(3*time.Hour), "en"); err != nil || changes != 0 {
			t.Fatalf("cancelled plan ran: %d %v", changes, err)
		}

		if err := service.Schedule(ctx, about.ID, content.Schedule{UnpublishAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}

		if err := service.Unpublish(ctx, about.ID); err != nil {
			t.Fatal(err)
		}

		loaded, err = service.Latest(ctx, about.ID)
		if err != nil || loaded.Published || !loaded.Schedule.IsZero() {
			t.Fatalf("unpublish kept the plan: %+v %v", loaded.Schedule, err)
		}
	})
}
