package testsuite

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/sitetest"
)

// navigationTheme is a theme whose menu region marks the page shown, with
// regions for the head, the languages and the edit link. ids gives the
// menu items their ElementID.
func navigationTheme(t *testing.T, ids bool) string {
	t.Helper()

	id := ""
	if ids {
		id = ` id="{{.ElementID}}"`
	}

	item := `<a` + id + ` href="{{.URL}}"{{if .Active}} aria-current="page"{{end}}>{{.Title}}</a>`
	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"layout.gohtml": `<html><head><meta charset="utf-8">{{slot "head"}}</head><body>` +
			`<header>{{slot "menu"}}{{slot "languages"}}</header>{{slot "editor"}}{{content}}</body></html>`,
		"regions/head.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.View */}}<title>{{.Title}}</title>` +
			`<link rel="canonical" href="{{absURL .Path}}">` +
			`{{range .Languages}}<link rel="alternate" hreflang="{{.Locale}}" href="{{absURL .URL}}">{{end}}`,
		"regions/menu.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.View */}}` +
			`<nav id="menu" {{oobAttr}}>{{range .Menu}}<div>` + item +
			`{{with .Children}}<div>{{range .}}` + item + `{{end}}</div>{{end}}</div>{{end}}</nav>`,
		"regions/languages.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.View */}}` +
			`<nav id="languages" {{oobAttr}}>{{range .Languages}}<a href="{{.URL}}">{{.Locale}}</a>{{end}}</nav>`,
		"regions/editor.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.View */}}` +
			`<div id="editor" {{oobAttr}}>{{with .Editor}}<a href="{{.EditURL}}">Edit</a>{{end}}</div>`,
		"page.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.Page */}}<main id="content"><h1>{{.Title}}</h1></main>`,
		"404.gohtml":  `{{/* @dot github.com/donseba/go-webthings/frontend.NotFound */}}<main id="content">Not found</main>`,
	})

	return dir
}

// navigationSite starts a site in the theme of dir with two sections of
// pages, About (Team, Contact) and Work (Tableware), and returns it signed
// in as admin with the ElementID of each page by path.
func navigationSite(t *testing.T, dir string) (*sitetest.Site, map[string]string) {
	t.Helper()

	return navigationSiteWith(t, sitetest.Options{Templates: dir})
}

// navigationSiteWith is navigationSite with options, whose Templates is the
// theme's folder.
func navigationSiteWith(t *testing.T, options sitetest.Options) (*sitetest.Site, map[string]string) {
	t.Helper()

	s := sitetest.StartWith(t, options)
	ctx := context.Background()

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	add := func(title, slug, parent string) string {
		edit := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {title}, "Slug": {slug}, "Parent": {parent}})
		s.Publish(edit + "?locale=en")

		return edit[strings.LastIndexByte(edit, '/')+1:]
	}

	about := add("About", "about", home)
	add("Team", "team", about)
	add("Contact", "contact", about)

	work := add("Work", "work", home)
	add("Tableware", "tableware", work)

	routes, err := s.App.Pages.Routes(ctx, "en")
	if err != nil {
		t.Fatal(err)
	}

	ids := map[string]string{}
	for _, route := range routes {
		ids[route.Path] = "menu-" + route.PageID
	}

	return s, ids
}

// navigate is an HTMX navigation of s from the page at from, an address
// as HX-Current-URL has it, to the page at path; without from, the
// request has no HX-Current-URL. It returns the response and its body.
func navigate(t *testing.T, s *sitetest.Site, from, path string) (*http.Response, string) {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Request-Type", "partial")
	request.Header.Set("HX-Target", "main#content")

	if from != "" {
		request.Header.Set("HX-Current-URL", from)
	}

	response := s.Request(request)
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	if response.StatusCode != http.StatusOK {
		t.Fatalf("navigation to %s: %d %s", path, response.StatusCode, body)
	}

	return response, string(body)
}

// menuItems are the ElementIDs of ids, by path, that body sends out of
// band on their own, and whether the whole menu goes.
func menuItems(body string, ids map[string]string) (items map[string]bool, whole bool) {
	items = map[string]bool{}

	for path, id := range ids {
		if strings.Contains(body, `hx-swap-oob="true" id="`+id+`"`) {
			items[path] = true
		}
	}

	return items, strings.Contains(body, `<nav id="menu"`)
}

// TestHTMXNavigationMenu checks that an HTMX navigation sends only the menu
// items whose state changes, the content and the other regions, and the
// whole menu when it cannot tell what changed.
func TestHTMXNavigationMenu(t *testing.T) {
	t.Parallel()

	s, ids := navigationSite(t, navigationTheme(t, true))
	visitor := s.Visitor()

	response, _ := visitor.Response("/en/")
	origin := "http://" + response.Request.URL.Host

	cases := []struct {
		name, from, to string
		// sent are the paths of the items sent on their own; nil sends
		// the whole menu.
		sent []string
	}{
		{
			"to another section",
			"/en/about/team",
			"/en/work/tableware",
			[]string{"/about", "/about/team", "/work", "/work/tableware"},
		},
		{"to a sibling", "/en/about/team", "/en/about/contact", []string{"/about/team", "/about/contact"}},
		{"from home", "/en/", "/en/about/team", []string{"/about", "/about/team"}},
		{"to home", "/en/work", "/en/", []string{"/work"}},
		{"to the same page", "/en/about", "/en/about", []string{"/about"}},
		{"from an address with a query", "/en/about/contact?q=1", "/en/work", []string{"/about", "/about/contact", "/work"}},
		{"from a page that is not found", "/en/about/missing", "/en/work", nil},
		{"from another language", "/nl/", "/en/work", nil},
	}

	for _, tc := range cases {
		response, body := navigate(t, visitor, origin+tc.from, tc.to)

		if !strings.Contains(body, `<main id="content">`) ||
			!strings.Contains(body, `<nav id="languages"  hx-swap-oob="true">`) ||
			!strings.Contains(body, `<div id="editor"  hx-swap-oob="true">`) ||
			strings.Contains(body, "<header>") {
			t.Fatalf("%s: content and regions: %s", tc.name, body)
		}

		if !strings.Contains(strings.Join(response.Header.Values("Vary"), ","), "HX-Current-URL") {
			t.Fatalf("%s: Vary %v", tc.name, response.Header.Values("Vary"))
		}

		items, whole := menuItems(body, ids)

		if tc.sent == nil {
			if !whole || len(items) > 0 {
				t.Fatalf("%s: want the whole menu, got items %v: %s", tc.name, items, body)
			}

			continue
		}

		if whole || len(items) != len(tc.sent) {
			t.Fatalf("%s: items %v, whole menu %v, want %v: %s", tc.name, items, whole, tc.sent, body)
		}

		for _, path := range tc.sent {
			if !items[path] {
				t.Fatalf("%s: item %s not sent: %s", tc.name, path, body)
			}
		}

		// The page arrived at, and the pages above it, are marked.
		target := strings.TrimPrefix(tc.to, "/en")
		for _, path := range tc.sent {
			current := path == target || strings.HasPrefix(target, path+"/")
			if marked := strings.Contains(body, `id="`+ids[path]+`" href="/en`+path+`" aria-current="page"`); marked != current {
				t.Fatalf("%s: item %s marked %v: %s", tc.name, path, marked, body)
			}
		}
	}

	// Without HX-Current-URL, or with another site's address, the whole
	// menu goes, as on any request.
	for _, from := range []string{"", "http://elsewhere.example/en/about"} {
		_, body := navigate(t, visitor, from, "/en/work")
		if items, whole := menuItems(body, ids); !whole || len(items) > 0 {
			t.Fatalf("from %q: items %v, whole %v", from, items, whole)
		}
	}

	// A whole page has every item, with its ElementID.
	_, page := visitor.Get("/en/about/team")
	for path, id := range ids {
		if path != "/" && !strings.Contains(page, `<a id="`+id+`" href="/en`+path+`"`) {
			t.Fatalf("page: no item %s: %s", path, page)
		}
	}
}

// TestHTMXNavigationWithoutIDs checks that a menu whose items have no
// ElementID goes whole with every navigation.
func TestHTMXNavigationWithoutIDs(t *testing.T) {
	t.Parallel()

	s, ids := navigationSite(t, navigationTheme(t, false))
	visitor := s.Visitor()

	response, _ := visitor.Response("/en/")
	origin := "http://" + response.Request.URL.Host

	for _, from := range []string{"/en/about/team", "/en/work"} {
		_, body := navigate(t, visitor, origin+from, "/en/work")

		items, whole := menuItems(body, ids)
		if !whole || len(items) > 0 || !strings.Contains(body, `<a href="/en/work" aria-current="page">Work</a>`) {
			t.Fatalf("from %s: items %v, whole %v: %s", from, items, whole, body)
		}
	}
}

// TestHTMXNavigationEditor checks that an editor's navigation brings the
// edit link of the page arrived at, along with the menu items that change,
// and is not cached.
func TestHTMXNavigationEditor(t *testing.T) {
	t.Parallel()

	admin, ids := navigationSite(t, navigationTheme(t, true))

	response, _ := admin.Response("/en/")
	origin := "http://" + response.Request.URL.Host

	response, body := navigate(t, admin, origin+"/en/about/team", "/en/work")

	work := strings.TrimPrefix(ids["/work"], "menu-")
	if !strings.Contains(body, `<div id="editor"  hx-swap-oob="true"><a href="/admin/pages/`+work+`?locale=en">Edit</a></div>`) {
		t.Fatalf("edit link: %s", body)
	}

	if items, whole := menuItems(body, ids); whole || len(items) != 3 {
		t.Fatalf("items %v, whole %v: %s", items, whole, body)
	}

	if response.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("headers %v", response.Header)
	}
}

// TestHTMXNavigationVary checks that only a request that gets the menu
// region out of band looks at HX-Current-URL and varies with it: not a
// request for a fragment, which goes without the regions, not a request for
// the whole page, and not one to a theme without a menu region.
func TestHTMXNavigationVary(t *testing.T) {
	t.Parallel()

	dir := navigationTheme(t, true)
	writeFiles(t, dir, map[string]string{
		"fragments/note.gohtml": `<p id="note">Note</p>`,
	})

	s, _ := navigationSite(t, dir)

	plain := t.TempDir()
	writeFiles(t, plain, map[string]string{
		"layout.gohtml": `<body>{{content}}</body>`,
		"page.gohtml":   `<main id="content"><h1>{{.Title}}</h1></main>`,
	})

	withoutMenu := sitetest.StartWith(t, sitetest.Options{Templates: plain})

	cases := []struct {
		name        string
		site        *sitetest.Site
		target      string
		requestType string
		varies      bool
	}{
		{"navigation", s, "main#content", "partial", true},
		{"fragment", s, "p#note", "partial", false},
		{"whole page", s, "main#content", "full", false},
		{"theme without a menu", withoutMenu, "main#content", "partial", false},
	}

	for _, tc := range cases {
		request, err := http.NewRequest(http.MethodGet, "/en/", nil)
		if err != nil {
			t.Fatal(err)
		}

		request.Header.Set("HX-Request", "true")
		request.Header.Set("HX-Request-Type", tc.requestType)
		request.Header.Set("HX-Target", tc.target)
		request.Header.Set("HX-Current-URL", "http://example.com/en/about")

		response := tc.site.Visitor().Request(request)
		_ = response.Body.Close()

		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d", tc.name, response.StatusCode)
		}

		varies := strings.Contains(strings.Join(response.Header.Values("Vary"), ","), "HX-Current-URL")
		if varies != tc.varies {
			t.Fatalf("%s: Vary %v", tc.name, response.Header.Values("Vary"))
		}
	}
}

// TestHTMXNavigationBehindProxy checks that the menu items alone go with an
// HTMX navigation behind a trusted proxy that sends the request on with a
// Host of its own: the visitor's address, HX-Current-URL, has the host the
// proxy names in X-Forwarded-Host, which counts as this site's (go-router's
// middleware.SameOrigin), and an address of another host does not.
func TestHTMXNavigationBehindProxy(t *testing.T) {
	t.Parallel()

	s, ids := navigationSiteWith(t, sitetest.Options{
		Templates: navigationTheme(t, true),
		Configure: func(config *webthings.Config) {
			config.TrustedProxies = []string{"127.0.0.1", "::1"}
		},
	})

	navigate := func(from string) string {
		request, err := http.NewRequest(http.MethodGet, "/en/work/tableware", nil)
		if err != nil {
			t.Fatal(err)
		}

		request.Header.Set("HX-Request", "true")
		request.Header.Set("HX-Request-Type", "partial")
		request.Header.Set("HX-Target", "main#content")
		request.Header.Set("HX-Current-URL", from)
		request.Header.Set("X-Forwarded-Host", "www.example.com")
		request.Header.Set("X-Forwarded-Proto", "https")

		response := s.Visitor().Request(request)
		defer func() { _ = response.Body.Close() }()

		body, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("navigation from %s: %d %v", from, response.StatusCode, err)
		}

		return string(body)
	}

	body := navigate("https://www.example.com/en/about/team")
	if items, whole := menuItems(body, ids); whole || len(items) != 4 {
		t.Fatalf("behind the proxy: items %v, whole %v: %s", items, whole, body)
	}

	body = navigate("https://elsewhere.example/en/about/team")
	if items, whole := menuItems(body, ids); !whole || len(items) > 0 {
		t.Fatalf("from another host: items %v, whole %v: %s", items, whole, body)
	}
}

// TestHTMXNavigationDeterministic checks that a whole page and an HTMX
// navigation give the same bytes on every request, with the regions out of
// band in the order of their file names, so ETags and the output cache stay
// stable. The requests are an editor's, which the output cache leaves out,
// so every one is rendered.
func TestHTMXNavigationDeterministic(t *testing.T) {
	t.Parallel()

	admin, _ := navigationSite(t, navigationTheme(t, true))

	response, _ := admin.Response("/en/")
	origin := "http://" + response.Request.URL.Host

	var firstPage, firstNavigation string

	for i := range 50 {
		_, page := admin.Get("/en/about/team")
		_, navigation := navigate(t, admin, origin+"/en/about/team", "/en/work/tableware")

		if i == 0 {
			firstPage, firstNavigation = page, navigation
		}

		if page != firstPage {
			t.Fatalf("page changed between requests:\n%s\n%s", firstPage, page)
		}

		if navigation != firstNavigation {
			t.Fatalf("navigation changed between requests:\n%s\n%s", firstNavigation, navigation)
		}
	}

	last := -1

	for _, part := range []string{`<main id="content">`, `<div id="editor"`, `<title>Tableware</title>`, `<nav id="languages"`, `id="menu-`} {
		at := strings.Index(firstNavigation, part)
		if at <= last {
			t.Fatalf("%s out of order: %s", part, firstNavigation)
		}

		last = at
	}
}

// TestHTMXNavigationTitleRegion checks that a theme with a region for the
// title alone, and no head region, still works: a whole page has the title
// in the layout's <head> without markers, and a navigation sends it at the
// top level, where htmx takes the document's title from.
func TestHTMXNavigationTitleRegion(t *testing.T) {
	t.Parallel()

	dir := navigationTheme(t, true)
	if err := os.Remove(filepath.Join(dir, "regions", "head.gohtml")); err != nil {
		t.Fatal(err)
	}

	writeFiles(t, dir, map[string]string{
		"layout.gohtml": `<html><head>{{slot "title"}}</head><body>` +
			`<header>{{slot "menu"}}{{slot "languages"}}</header>{{slot "editor"}}{{content}}</body></html>`,
		"regions/title.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.View */}}<title>{{.Title}}</title>`,
	})

	s, ids := navigationSite(t, dir)
	visitor := s.Visitor()

	response, page := visitor.Response("/en/about")
	origin := "http://" + response.Request.URL.Host

	if !strings.Contains(page, `<head><title>About</title></head>`) || strings.Contains(page, "data-partial-head") {
		t.Fatalf("whole page: %s", page)
	}

	_, body := navigate(t, visitor, origin+"/en/about", "/en/work")
	if !strings.Contains(body, `<title>Work</title>`) || strings.Contains(body, "data-partial-head") {
		t.Fatalf("navigation: %s", body)
	}

	if items, whole := menuItems(body, ids); whole || len(items) != 2 {
		t.Fatalf("items %v, whole %v: %s", items, whole, body)
	}
}
