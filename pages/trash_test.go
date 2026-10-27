package pages_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/pages"
)

// TestTrash deletes pages and brings them back where they were.
func TestTrash(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		editor := db.NewID()
		ctx := db.WithActor(context.Background(), editor)
		service := setup(t, database)

		home, err := service.Home(ctx)
		if err != nil {
			t.Fatal(err)
		}

		about := publish(t, service, pages.Draft{
			Fields:       pages.Fields{ParentID: home},
			Translations: []content.Translation[pages.Text]{text("en", "about", "About")},
		})
		team := publish(t, service, pages.Draft{
			Fields:       pages.Fields{ParentID: about.ID},
			Translations: []content.Translation[pages.Text]{text("en", "team", "Team")},
		})

		if err := service.Delete(ctx, about.ID); !errors.Is(err, pages.ErrHasChildren) {
			t.Fatalf("delete with children: %v", err)
		}

		for _, id := range []string{team.ID, about.ID} {
			if err := service.Delete(ctx, id); err != nil {
				t.Fatal(err)
			}
		}

		if _, err := service.Resolve(ctx, "en", "/about"); !errors.Is(err, pages.ErrNotFound) {
			t.Fatalf("deleted page resolves: %v", err)
		}

		trashed, err := service.Trashed(ctx)
		if err != nil || len(trashed) != 2 || trashed[0].DeletedBy != editor {
			t.Fatalf("trash: %+v %v", trashed, err)
		}

		// A page comes back only below a parent that is there.
		if err := service.Undelete(ctx, team.ID); !errors.Is(err, pages.ErrParentDeleted) {
			t.Fatalf("undelete below a deleted parent: %v", err)
		}

		// A sibling took the slug: the page stays in the trash.
		other := publish(t, service, pages.Draft{
			Fields:       pages.Fields{ParentID: home},
			Translations: []content.Translation[pages.Text]{text("en", "about", "Other about")},
		})

		if err := service.Undelete(ctx, about.ID); !errors.Is(err, pages.ErrDuplicate) {
			t.Fatalf("undelete with a taken slug: %v", err)
		}

		if err := service.Delete(ctx, other.ID); err != nil {
			t.Fatal(err)
		}

		for _, id := range []string{about.ID, team.ID} {
			if err := service.Undelete(ctx, id); err != nil {
				t.Fatal(err)
			}
		}

		resolved, err := service.Resolve(ctx, "en", "/about/team")
		if err != nil || resolved.Page.ID != team.ID {
			t.Fatalf("restored page: %+v %v", resolved, err)
		}

		if err := service.Purge(ctx, other.ID); err != nil {
			t.Fatal(err)
		}

		if trashed, _ := service.Trashed(ctx); len(trashed) != 0 {
			t.Fatalf("trash after purge: %+v", trashed)
		}

		recent, err := service.Recent(ctx, 1)
		if err != nil || len(recent) != 1 || recent[0].UpdatedBy != editor {
			t.Fatalf("recent: %+v %v", recent, err)
		}
	})
}
