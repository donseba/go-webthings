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

// TestPagePreview checks which page revisions show at which address: a
// revision of the page published there, or of a page that would be there,
// and never one of another page.
func TestPagePreview(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := setup(t, database)

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

		if err := service.Publish(ctx, about.ID, about.Revision.ID); err != nil {
			t.Fatal(err)
		}

		// A draft that renames the published page, and a page never
		// published, below About.
		renamed, err := service.Save(ctx, pages.Draft{
			ID:           about.ID,
			Fields:       pages.Fields{ParentID: home},
			Translations: []content.Translation[pages.Text]{text("en", "about-us", "About us")},
		})
		if err != nil {
			t.Fatal(err)
		}

		team, err := service.Save(ctx, pages.Draft{
			Fields:       pages.Fields{ParentID: about.ID},
			Translations: []content.Translation[pages.Text]{text("en", "team", "Team")},
		})
		if err != nil {
			t.Fatal(err)
		}

		// A page below a page that is not published has no address yet.
		lost, err := service.Save(ctx, pages.Draft{
			Fields:       pages.Fields{ParentID: team.ID},
			Translations: []content.Translation[pages.Text]{text("en", "lost", "Lost")},
		})
		if err != nil {
			t.Fatal(err)
		}

		for _, check := range []struct {
			name, locale, path, revision string
			found                        bool
		}{
			{"draft at the published address", "en", "/about", renamed.Revision.ID, true},
			{"draft at its new address", "en", "/about-us", renamed.Revision.ID, true},
			{"never published, below its parent", "en", "/about/team", team.Revision.ID, true},
			{"never published, elsewhere", "en", "/team", team.Revision.ID, false},
			{"another page's revision", "en", "/about", team.Revision.ID, false},
			{"unknown revision", "en", "/about", db.NewID(), false},
			{"no text in the language", "nl", "/about/team", team.Revision.ID, false},
			{"parent not published", "en", "/about/team/lost", lost.Revision.ID, false},
		} {
			page, err := service.Preview(ctx, check.locale, check.path, check.revision)

			switch {
			case check.found && (err != nil || page.Revision.ID != check.revision):
				t.Fatalf("%s: %v %v", check.name, page.Revision.ID, err)
			case !check.found && !errors.Is(err, pages.ErrNotFound):
				t.Fatalf("%s: want not found, got %v %v", check.name, page.Revision.ID, err)
			}
		}

		if path, err := service.PreviewPath(ctx, renamed, "en"); err != nil || path != "/about-us" {
			t.Fatalf("preview path of a renamed page: %q %v", path, err)
		}

		if path, err := service.PreviewPath(ctx, team, "en"); err != nil || path != "/about/team" {
			t.Fatalf("preview path of a new page: %q %v", path, err)
		}

		if _, err := service.PreviewPath(ctx, lost, "en"); !errors.Is(err, pages.ErrNotFound) {
			t.Fatalf("preview path below an unpublished page: %v", err)
		}
	})
}
