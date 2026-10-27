package pages_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-router"
)

func setup(t *testing.T, database *db.DB) *pages.Service {
	t.Helper()
	ctx := context.Background()

	if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
		t.Fatal(err)
	}

	if err := database.ApplySchema(ctx, "pages", pages.Schema()); err != nil {
		t.Fatal(err)
	}

	provider, err := locales.New(locales.Config{Default: "en", Enabled: []string{"en", "nl"}})
	if err != nil {
		t.Fatal(err)
	}

	modules := func() []string { return []string{"blog"} }

	service, err := pages.New(database, provider, modules, "admin", "sitemap.xml")
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Init(ctx, map[string]string{"nl": "Thuis"}); err != nil {
		t.Fatal(err)
	}

	return service
}

func text(locale, slug, title string) content.Translation[pages.Text] {
	return content.Translation[pages.Text]{Locale: locale, Text: pages.Text{Slug: slug, Title: title}}
}

func publish(t *testing.T, service *pages.Service, draft pages.Draft) pages.Page {
	t.Helper()
	ctx := context.Background()

	page, err := service.Save(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Publish(ctx, page.ID, page.Revision.ID); err != nil {
		t.Fatal(err)
	}

	return page
}

func TestTreeRoutesAndModules(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := setup(t, database)

		homeID, err := service.Home(ctx)
		if err != nil {
			t.Fatal(err)
		}

		home, err := service.Resolve(ctx, "nl", "/")
		if err != nil || home.Page.ID != homeID {
			t.Fatalf("home: %+v %v", home, err)
		}

		about := publish(t, service, pages.Draft{
			Fields:       pages.Fields{ParentID: homeID},
			Translations: []content.Translation[pages.Text]{text("en", "about", "About"), text("nl", "over-ons", "Over ons")},
		})

		team := publish(t, service, pages.Draft{
			Fields:       pages.Fields{ParentID: about.ID},
			Translations: []content.Translation[pages.Text]{text("en", "team", "Team")},
		})

		resolved, err := service.Resolve(ctx, "en", "/about/team")
		if err != nil || resolved.Page.ID != team.ID {
			t.Fatalf("resolve team: %+v %v", resolved, err)
		}

		if _, err := service.Resolve(ctx, "nl", "/over-ons/team"); !errors.Is(err, pages.ErrNotFound) {
			t.Fatalf("untranslated page resolved: %v", err)
		}

		// A draft rename does not change public paths until it is published.
		renamed, err := service.Save(ctx, pages.Draft{
			ID:           about.ID,
			Fields:       about.Fields,
			Translations: []content.Translation[pages.Text]{text("en", "company", "Company"), text("nl", "over-ons", "Over ons")},
		})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := service.Resolve(ctx, "en", "/about/team"); err != nil {
			t.Fatalf("draft changed routes: %v", err)
		}

		if err := service.Publish(ctx, about.ID, renamed.Revision.ID); err != nil {
			t.Fatal(err)
		}

		if _, err := service.Resolve(ctx, "en", "/company/team"); err != nil {
			t.Fatalf("children did not follow parent: %v", err)
		}

		alternates, err := service.Alternates(ctx, about.ID)
		if err != nil || alternates["en"] != "/company" || alternates["nl"] != "/over-ons" {
			t.Fatalf("alternates: %v %v", alternates, err)
		}

		blog := publish(t, service, pages.Draft{
			Fields:       pages.Fields{ParentID: homeID, Module: "blog"},
			Translations: []content.Translation[pages.Text]{text("en", "blog", "Blog")},
		})

		below, err := service.Resolve(ctx, "en", "/blog/category/news")
		if err != nil || below.Page.ID != blog.ID || below.Rest != "/category/news" {
			t.Fatalf("module subtree: %+v %v", below, err)
		}

		if path, err := service.MountPath(ctx, "blog", "en"); err != nil || path != "/blog" {
			t.Fatalf("mount path: %q %v", path, err)
		}

		if address, err := service.MountURL(ctx, "blog", "en"); err != nil || address != "/en/blog" {
			t.Fatalf("mount URL: %q %v", address, err)
		}

		// The blog has no Dutch page: no address, and no error either.
		if address, err := service.MountURL(ctx, "blog", "nl"); err != nil || address != "" {
			t.Fatalf("mount URL without a page: %q %v", address, err)
		}

		_, err = service.Save(ctx, pages.Draft{
			Fields:       pages.Fields{ParentID: blog.ID},
			Translations: []content.Translation[pages.Text]{text("en", "child", "Child")},
		})
		if !errors.Is(err, pages.ErrModuleParent) {
			t.Fatalf("child below module page: %v", err)
		}

		_, err = service.Save(ctx, pages.Draft{
			Fields:       pages.Fields{ParentID: homeID, Module: "unknown"},
			Translations: []content.Translation[pages.Text]{text("en", "x", "X")},
		})
		if !errors.Is(err, pages.ErrUnknownMount) {
			t.Fatalf("unknown module mounted: %v", err)
		}

		_, err = service.Save(ctx, pages.Draft{
			ID:           about.ID,
			Fields:       pages.Fields{ParentID: team.ID},
			Translations: renamed.Translations,
		})
		if !errors.Is(err, pages.ErrCycle) {
			t.Fatalf("cycle accepted: %v", err)
		}

		if err := service.Delete(ctx, about.ID); !errors.Is(err, pages.ErrHasChildren) {
			t.Fatalf("deleted page with children: %v", err)
		}

		routes, err := service.Routes(ctx, "en")
		if err != nil || len(routes) != 4 {
			t.Fatalf("routes: %+v %v", routes, err)
		}
	})
}

func TestSlugConflicts(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := setup(t, database)

		homeID, err := service.Home(ctx)
		if err != nil {
			t.Fatal(err)
		}

		publish(t, service, pages.Draft{
			Fields:       pages.Fields{ParentID: homeID},
			Translations: []content.Translation[pages.Text]{text("en", "about", "About")},
		})

		for _, slug := range []string{"about", "admin", "sitemap.xml"} {
			page, err := service.Save(ctx, pages.Draft{
				Fields:       pages.Fields{ParentID: homeID},
				Translations: []content.Translation[pages.Text]{text("en", slug, "Other")},
			})
			if err != nil {
				if slug == "sitemap.xml" {
					continue // not a valid slug at all
				}

				t.Fatal(err)
			}

			if err := service.Publish(ctx, page.ID, page.Revision.ID); !errors.Is(err, pages.ErrDuplicate) {
				t.Fatalf("slug %q: want duplicate, got %v", slug, err)
			}
		}
	})
}

// TestLinks checks that Links gives the routes' paths below a page, and no
// addresses while the instance is on no page.
func TestLinks(t *testing.T) {
	routes := router.NewSite()
	routes.Get("/{$}", func(w http.ResponseWriter, r *http.Request) {}).As("list")
	routes.Get("/{slug}", func(w http.ResponseWriter, r *http.Request) {}).As("show")

	cases := []struct {
		base, name string
		args       []any
		want       string
	}{
		{"/en/news", "list", nil, "/en/news"},
		{"/en/news", "show", []any{"slug", "hello"}, "/en/news/hello"},
		{"", "list", nil, ""},
		{"", "show", []any{"slug", "hello"}, ""},
	}

	for _, tc := range cases {
		paths := pages.Links(routes, tc.base)

		got, err := paths(tc.name, tc.args...)
		if err != nil || got != tc.want {
			t.Errorf("Links(%q)(%q): got %q, %v, want %q", tc.base, tc.name, got, err, tc.want)
		}
	}
}
