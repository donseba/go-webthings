package testsuite

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/pages"
	"github.com/donseba/go-webthings/sitetest"
)

// TestRenderedContentFollowsMedia checks that page bodies, which are kept
// rendered, show a change of the media library's alt text at once, in the
// language of the page.
func TestRenderedContentFollowsMedia(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t)
	ctx := context.Background()

	photo, err := s.App.Media.Upload(ctx, "photo.png", bytes.NewReader(sitetest.PNG(t, 2000, 1000)))
	if err != nil {
		t.Fatal(err)
	}

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	body := "Text and ![](media:" + photo.ID + ")"

	// publish publishes a page below Home with body in English and Dutch;
	// the Dutch slug is the English one with "-nl".
	publish := func(slug, body string) {
		page, err := s.App.Pages.Save(ctx, pages.Draft{
			Fields: pages.Fields{ParentID: home},
			Translations: []content.Translation[pages.Text]{
				{Locale: "en", Text: pages.Text{Title: slug, Slug: slug, Body: body}},
				{Locale: "nl", Text: pages.Text{Title: slug, Slug: slug + "-nl", Body: body}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := s.App.Pages.Publish(ctx, page.ID, page.Revision.ID); err != nil {
			t.Fatal(err)
		}
	}

	publish("gallery", body)
	// A body without media renders the same in every language.
	publish("plain", "Just **text**.")

	// The image has the size of its src, which keeps its place on the page
	// before it loads.
	s.Expect("/en/gallery", http.StatusOK, `alt=""`, `sizes="100vw" width="1600" height="800">`)
	s.Expect("/en/plain", http.StatusOK, `<strong>text</strong>`)
	s.Expect("/nl/plain-nl", http.StatusOK, `<strong>text</strong>`)

	for locale, alt := range map[string]string{"en": "A photo", "nl": "Een foto"} {
		err := s.App.Media.SaveTranslation(ctx, photo.ID, media.Translation{Locale: locale, AltText: alt})
		if err != nil {
			t.Fatal(err)
		}
	}

	// The same body in another language has its own alt text.
	s.Expect("/en/gallery", http.StatusOK, `alt="A photo"`)
	s.Expect("/nl/gallery-nl", http.StatusOK, `alt="Een foto"`)

	err = s.App.Media.SaveTranslation(ctx, photo.ID, media.Translation{Locale: "en", AltText: "Another photo"})
	if err != nil {
		t.Fatal(err)
	}

	s.Expect("/en/gallery", http.StatusOK, `alt="Another photo"`)
	s.Expect("/nl/gallery-nl", http.StatusOK, `alt="Een foto"`)
}

// TestSearchThrottle checks that a visitor cannot run more searches than
// the limit, while visiting the search screen without a query stays free.
func TestSearchThrottle(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t)
	s.App.Frontend.SetSearchThrottle(2, time.Minute)

	visitor := s.Visitor()

	for range 3 {
		visitor.Expect("/en/search", http.StatusOK)
	}

	visitor.Expect("/en/search?q=one", http.StatusOK)
	visitor.Expect("/en/search?q=two", http.StatusOK)

	if status, _ := visitor.Get("/en/search?q=three"); status != http.StatusTooManyRequests {
		t.Fatalf("third search: %d", status)
	}

	s.App.Frontend.SetSearchThrottle(0, 0)
	visitor.Expect("/en/search?q=four", http.StatusOK)
}

// TestSitemapLimits checks that a failing module is left out of the
// sitemap instead of breaking it, that the sitemap holds at most 50,000
// URLs, and that it is not built again for every request.
func TestSitemapLimits(t *testing.T) {
	t.Parallel()

	var builds atomic.Int64

	many := sitemapModule("many", func(context.Context, string) ([]string, error) {
		builds.Add(1)

		paths := make([]string, 60000)
		for i := range paths {
			paths[i] = fmt.Sprintf("/item-%d", i)
		}

		return paths, nil
	})

	failing := sitemapModule("failing", func(context.Context, string) ([]string, error) {
		return nil, errors.New("storage is down")
	})

	s := sitetest.Start(t, many, failing)
	// The failing module comes first, within the 50,000 URLs.
	s.Mount("failing", "Failing", "failing")
	s.Mount("many", "Many", "many")

	status, body := s.Visitor().Get("/sitemap.xml")
	if status != http.StatusOK {
		t.Fatalf("sitemap: %d", status)
	}

	if got := strings.Count(body, "<loc>"); got != 50000 {
		t.Fatalf("sitemap URLs: %d", got)
	}

	if !strings.Contains(body, "/en/failing</loc>") || strings.Contains(body, "/en/failing/") {
		t.Fatal("the failing module's page should stay, without paths below it")
	}

	before := builds.Load()
	s.Visitor().Expect("/sitemap.xml", http.StatusOK, "/en/many</loc>")

	if builds.Load() != before {
		t.Fatal("the sitemap was built again at once")
	}
}

// sitemapModule is a module whose page lists paths in the sitemap.
func sitemapModule(key string, paths func(context.Context, string) ([]string, error)) webthings.Module {
	return webthings.Module{
		Type:  key,
		Key:   key,
		Title: key,
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			routes := env.Frontend.Router()

			routes.Get("/{$}", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(key))
			})

			return webthings.Parts{Public: routes, Sitemap: paths}, nil
		},
	}
}
