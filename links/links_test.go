package links_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/links"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/pages"
)

// site is a page tree in English and Dutch with a module "blog".
type site struct {
	pages *pages.Service
	home  string
}

func setup(t *testing.T, database *db.DB) site {
	t.Helper()

	ctx := context.Background()

	if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
		t.Fatal(err)
	}

	if err := database.ApplySchema(ctx, "pages", pages.Schema()); err != nil {
		t.Fatal(err)
	}

	tree, err := pages.New(database, mustLocales(t), func() []string { return []string{"blog"} })
	if err != nil {
		t.Fatal(err)
	}

	if err := tree.Init(ctx, map[string]string{"nl": "Thuis"}); err != nil {
		t.Fatal(err)
	}

	home, err := tree.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	return site{pages: tree, home: home}
}

// publish saves and publishes a page below parent with translations of
// slug, title and body.
func (s site) publish(t *testing.T, parent, module string, translations ...pages.Text) pages.Page {
	t.Helper()

	ctx := context.Background()
	draft := pages.Draft{Fields: pages.Fields{ParentID: parent, Module: module}}

	for i, text := range translations {
		locale := "en"
		if i > 0 {
			locale = "nl"
		}

		draft.Translations = append(draft.Translations, content.Translation[pages.Text]{Locale: locale, Text: text})
	}

	page, err := s.pages.Save(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.pages.Publish(ctx, page.ID, page.Revision.ID); err != nil {
		t.Fatal(err)
	}

	return page
}

// redirects is a links.Redirector of fixed redirects.
type redirects map[string]string

func (r redirects) Redirected(_ context.Context, address string) (string, bool) {
	target, ok := r[address]

	return target, ok
}

func (r redirects) Redirect(_ context.Context, address, target string) error {
	r[address] = target

	return nil
}

// addresses lists the links as "address found" in their order.
func addresses(list []links.Link) []string {
	var out []string

	for _, link := range list {
		state := "missing"
		if link.Found {
			state = "found"
		}

		out = append(out, link.Address+" "+state)
	}

	return out
}

// TestIndex checks the links found in the Markdown of published pages and
// in module sources: relative links resolved from the content's address,
// locale prefixes, the site's own full URLs, and what is not a page link.
func TestIndex(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		s := setup(t, database)

		about := s.publish(
			t,
			s.home,
			"",
			pages.Text{Slug: "about", Title: "About"},
			pages.Text{Slug: "over-ons", Title: "Over ons"},
		)
		s.publish(t, about.ID, "", pages.Text{Slug: "team", Title: "Team", Body: "Back to [about](/en/about)."})
		s.publish(t, s.home, "blog", pages.Text{Slug: "blog", Title: "Blog"})
		s.publish(t, s.home, "", pages.Text{Slug: "contact", Title: "Contact", Body: strings.Join([]string{
			"[a](/en/about) [b](team) [c](about/team) [d](/nl/over-ons/)",
			"[e](https://example.com/en/about#form) [f](https://elsewhere.example/en/about)",
			"[g](mailto:office@example.com) [i](/media/file.pdf)",
			"[j](/en/missing) [k](/about) [l](?page=2) [m](#top) [n](/en/search) [o](/en/blog/first-post)",
		}, " ")})

		check := links.New(s.pages, mustLocales(t), links.Options{
			BaseURL: "https://example.com",
			IsCorePath: func(address string) bool {
				return strings.HasPrefix(address, "/media/")
			},
		})

		check.Use("news", "News", func(_ context.Context, locale string) ([]links.Source, error) {
			if locale != "en" {
				return nil, nil
			}

			return []links.Source{{
				ID:        "post-1",
				Title:     "Hello",
				URL:       "/en/blog/hello",
				Markdown:  []string{"See [us](../about) and [the brochure](media:0190c5b2-6b1c-7f2e-9d7a-1c2b3d4e5f60)."},
				Addresses: []string{"/nl/over-ons", "https://elsewhere.example/"},
			}}, nil
		})

		index, err := check.Index(ctx)
		if err != nil {
			t.Fatal(err)
		}

		got := addresses(index.Links())
		want := []string{
			// Team, below About.
			"/en/about found",
			// Contact.
			"/en/about found", "/en/team missing", "/en/about/team found", "/nl/over-ons found",
			"/en/about found", "/en/missing missing", "/about missing", "/en/search found", "/en/blog/first-post found",
			// The module's post.
			"/en/about found", "/nl/over-ons found",
		}

		if !slices.Equal(got, want) {
			t.Fatalf("links:\n got %q\nwant %q", got, want)
		}

		// The links to About and below it, but not those of Team, which is
		// below About itself.
		to := index.To(links.Address("en", "/about"), links.Address("nl", "/over-ons"))
		if got := addresses(to); len(got) != 6 {
			t.Fatalf("links to About: %q", got)
		}

		for _, link := range to {
			if link.Source.Title == "Team" {
				t.Fatalf("a link of a page below About counts: %+v", link)
			}
		}

		if to[len(to)-1].Module != "news" || to[len(to)-1].Source.EditURL != "" || check.Title("news") != "News" {
			t.Fatalf("module link: %+v", to[len(to)-1])
		}

		// The same index until something changes.
		again, err := check.Index(ctx)
		if err != nil || again != index {
			t.Fatalf("index made again: %v", err)
		}
	})
}

// TestBroken checks the links that lead nowhere, and that a redirect to a
// page or another website mends them.
func TestBroken(t *testing.T) {
	ctx := context.Background()
	s := setup(t, dbtest.SQLite(t))

	about := s.publish(t, s.home, "", pages.Text{Slug: "about", Title: "About"})
	s.publish(
		t,
		s.home,
		"",
		pages.Text{Slug: "contact", Title: "Contact", Body: "[a](/en/about) [b](/en/old) [c](/en/gone) [d](/en/loop)"},
	)

	var version uint64

	check := links.New(s.pages, mustLocales(t), links.Options{Version: func() uint64 { return version }})

	broken, err := check.Broken(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if got := addresses(broken); !slices.Equal(got, []string{"/en/old missing", "/en/gone missing", "/en/loop missing"}) {
		t.Fatalf("broken: %q", got)
	}

	// Redirects to a page or to another website mend a link; one to an
	// address without a page does not.
	check.SetRedirects(
		"redirects",
		redirects{"/en/old": "/en/about", "/en/gone": "https://elsewhere.example/", "/en/loop": "/en/nothing"},
	)

	broken, err = check.Broken(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if got := addresses(broken); !slices.Equal(got, []string{"/en/loop missing"}) {
		t.Fatalf("broken with redirects: %q", got)
	}

	// Unpublishing About breaks its link, and the redirect to it; the index
	// follows the page tree.
	if err := s.pages.Unpublish(ctx, about.ID); err != nil {
		t.Fatal(err)
	}

	broken, err = check.Broken(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if got := addresses(broken); !slices.Equal(got, []string{"/en/about missing", "/en/old missing", "/en/loop missing"}) {
		t.Fatalf("broken after unpublishing: %q", got)
	}

	// A new version makes the index again.
	index, err := check.Index(ctx)
	if err != nil {
		t.Fatal(err)
	}

	version++

	again, err := check.Index(ctx)
	if err != nil || again == index {
		t.Fatalf("index kept after a new version: %v", err)
	}
}

func mustLocales(t *testing.T) locales.Provider {
	t.Helper()

	provider, err := locales.New(locales.Config{Default: "en", Enabled: []string{"en", "nl"}})
	if err != nil {
		t.Fatal(err)
	}

	return provider
}
