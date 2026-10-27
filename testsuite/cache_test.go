package testsuite

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/sitetest"
)

// TestCachedPages checks that a public page needs no database queries once
// settings and routes are cached, and that publishing shows at once.
func TestCachedPages(t *testing.T) {
	t.Parallel()

	queries := func(s *sitetest.Site, path string) uint64 {
		t.Helper()

		visitor := s.Visitor()
		visitor.Expect(path, http.StatusOK)

		before := s.App.DB.Statements()
		visitor.Expect(path, http.StatusOK)

		return s.App.DB.Statements() - before
	}

	about := func(s *sitetest.Site) string {
		t.Helper()

		home, err := s.App.Pages.Home(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		editor := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About"}, "Slug": {"about"}, "Parent": {home}})
		s.Publish(editor + "?locale=en")

		return editor
	}

	// The output cache would answer the second view without the
	// services: sites in tests have it off.
	uncached := sitetest.StartWith(t, sitetest.Options{
		Configure: func(config *webthings.Config) { config.CacheTTL = -1 },
	})
	about(uncached)

	without := queries(uncached, "/en/about")

	cached := sitetest.Start(t)
	editor := about(cached)

	with := queries(cached, "/en/about")

	t.Logf("queries per page view: %d without cache, %d with", without, with)

	if without < 5 || with != 0 {
		t.Fatalf("queries per page view: %d without cache, %d with", without, with)
	}

	// Changes in this process show at once.
	cached.Post(editor+"?locale=en", editor+"?locale=en", url.Values{"Title": {"About us"}, "Slug": {"about"}})
	cached.Publish(editor + "?locale=en")
	cached.Visitor().Expect("/en/about", http.StatusOK, "<h1>About us</h1>")
}
