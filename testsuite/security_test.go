package testsuite

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/site"
	"github.com/donseba/go-webthings/sitetest"
)

// expectHeaders fails the test when a header of response is not the value
// of want; "" wants the header absent.
func expectHeaders(t *testing.T, what string, header http.Header, want map[string]string) {
	t.Helper()

	for name, value := range want {
		if got := header.Get(name); got != value {
			t.Errorf("%s: %s is %q, want %q", what, name, got, value)
		}
	}
}

// echoModule has a visitor action that answers with plain text.
func echoModule() webthings.Module {
	return webthings.Module{
		Type: "echo",
		Key:  "echo",
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			env.Actions().Post("/say", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("said"))
			})

			return webthings.Parts{}, nil
		},
	}
}

// TestSecurityHeaders checks the public site's security headers: every
// public response is nosniff with a referrer policy, and pages, module
// pages, action responses and the 404 page may only be framed by the site
// itself by default. The Security card changes that; the admin keeps its own
// framing headers, and previews still show.
func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	s := sitetest.StartWith(
		t,
		sitetest.Options{StartingPoints: []string{"news"}},
		publishing.Module("news", "News"),
		echoModule(),
	)
	s.Mount("news", "News", "news")

	nosniff := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}
	sameOrigin := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"X-Frame-Options":         "SAMEORIGIN",
		"Content-Security-Policy": "frame-ancestors 'self'",
	}

	pages := map[string]int{"/en/": http.StatusOK, "/en/news": http.StatusOK, "/en/missing": http.StatusNotFound}

	for path, status := range pages {
		response := request(t, s.App, http.MethodGet, path)
		if response.Code != status {
			t.Fatalf("%s: %d", path, response.Code)
		}

		expectHeaders(t, path, response.Header(), sameOrigin)
	}

	action := request(t, s.App, http.MethodPost, "/_/echo/say")
	if action.Code != http.StatusOK || action.Body.String() != "said" {
		t.Fatalf("action: %d %q", action.Code, action.Body.String())
	}

	expectHeaders(t, "action", action.Header(), sameOrigin)

	// Files are not framed as pages: they only get nosniff. So do the
	// endpoints for operations and go-router's own redirect to a clean
	// path, which no route answers.
	for _, path := range []string{"/robots.txt", "/sitemap.xml", "/media/missing.png", "/_/webthings.css", "/healthz", "//en/"} {
		response := request(t, s.App, http.MethodGet, path)
		expectHeaders(t, path, response.Header(), nosniff)
		expectHeaders(t, path, response.Header(), map[string]string{"X-Frame-Options": "", "Content-Security-Policy": ""})
	}

	if redirect := request(t, s.App, http.MethodGet, "//en/"); redirect.Header().Get("Location") != "/en/" {
		t.Fatalf("//en/: %d %v", redirect.Code, redirect.Header())
	}

	// The admin is never framed, whatever the site allows. Without a
	// Content-Security-Policy of the site (off, the default), its policy is
	// only frame-ancestors (see TestContentSecurityPolicyAdmin).
	admin := map[string]string{"X-Frame-Options": "DENY", "Content-Security-Policy": "frame-ancestors 'none'"}
	expectHeaders(t, "admin", request(t, s.App, http.MethodGet, "/admin/login").Header(), admin)

	// An editor's preview still shows.
	ctx := context.Background()

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	about := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About draft"}, "Slug": {"about"}, "Parent": {home}})

	draft, err := s.App.Pages.Latest(ctx, lastID(about))
	if err != nil {
		t.Fatal(err)
	}

	if status, _, previewed := preview(t, s, "/en/about?revision="+draft.Revision.ID); status != http.StatusOK || !previewed {
		t.Fatalf("preview: %d %v", status, previewed)
	}

	s.Expect("/admin/site", http.StatusOK, "Security", "Other sites may embed pages", "No, only this site")

	status, _ := s.PostStatus("/admin/site", "/admin/site/security?locale=en", url.Values{"Framing": {"everyone"}})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("an unknown framing was saved: %d", status)
	}

	framing := map[string]map[string]string{
		site.FramingAny:        {"X-Frame-Options": "", "Content-Security-Policy": ""},
		site.FramingNone:       {"X-Frame-Options": "DENY", "Content-Security-Policy": "frame-ancestors 'none'"},
		site.FramingSameOrigin: sameOrigin,
	}

	for _, policy := range []string{site.FramingAny, site.FramingNone, site.FramingSameOrigin} {
		s.Post("/admin/site", "/admin/site/security?locale=en", url.Values{"Framing": {policy}})

		settings, err := s.App.Site.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if settings.FramingPolicy() != policy {
			t.Fatalf("framing: %q, want %q", settings.Framing, policy)
		}

		for _, path := range []string{"/en/", "/en/news", "/en/missing"} {
			response := request(t, s.App, http.MethodGet, path)
			expectHeaders(t, policy+" "+path, response.Header(), framing[policy])
			expectHeaders(t, policy+" "+path, response.Header(), nosniff)
		}

		action := request(t, s.App, http.MethodPost, "/_/echo/say")
		expectHeaders(t, policy+" action", action.Header(), framing[policy])
		expectHeaders(t, policy+" admin", request(t, s.App, http.MethodGet, "/admin/login").Header(), admin)
	}

	dutchAdmin(t).Expect("/admin/site", http.StatusOK, "Beveiliging", "Andere websites mogen pagina&#39;s insluiten")
}
