package testsuite

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/modules/comments"
	"github.com/donseba/go-webthings/sitetest"
)

// TestHTMXTheme drives a theme the way HTMX does: navigation swaps the
// content and the regions follow out of band, a fragment comes alone, and a
// form posted with HTMX lands on its fragment with the result.
func TestHTMXTheme(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"layout.gohtml": `<body>{{slot "menu"}}{{content}}</body>`,
		"regions/menu.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.View */}}` +
			`<nav id="menu" {{oobAttr}}>{{.Title}}</nav>`,
		"page.gohtml": `<main id="content"><h1>{{.Title}}</h1>{{requiredSlot "comments"}}</main>`,
		"fragments/comments.gohtml": `{{/* @model View github.com/donseba/go-webthings/frontend.View */}}` +
			`<section id="comments"><p>{{commentCount "remarks" "home"}} on {{View.Site.Name}}</p>` +
			`{{commentThread "remarks" "home"}}</section>`,
	})

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir}, comments.Module("remarks", "Comments"))

	remarks := s.App.Service("remarks").(*comments.Service)
	if err := remarks.SaveSettings(context.Background(), comments.Settings{Moderated: false}); err != nil {
		t.Fatal(err)
	}

	s.Visitor().Expect("/en/", http.StatusOK,
		`<nav id="menu"`,
		`<h1>Home</h1>`,
		`<section id="comments"><p>0 on Test site</p>`,
	)

	// Navigation: the content, and the menu out of band.
	content := htmxRequest(t, s.App, http.MethodGet, "/en/", "main#content", nil)
	expectBody(t, "content", content, []string{`<main id="content">`, `hx-swap-oob="true"`, `<nav id="menu"`}, []string{"<body>"})

	// A fragment on its own.
	fragment := htmxRequest(t, s.App, http.MethodGet, "/en/", "section#comments", nil)
	expectBody(
		t,
		"fragment",
		fragment,
		[]string{`<section id="comments"><p>0 on Test site</p>`},
		[]string{"<main", "<nav", "<body>"},
	)

	// A comment posted with HTMX: the action returns to the page, HTMX follows
	// with the same target and swaps the fragment, now with the comment.
	posted := htmxRequest(t, s.App, http.MethodPost, "/_/remarks/post", "section#comments", url.Values{
		"subject": {"home"},
		"name":    {"Ada"},
		"body":    {"Hello"},
		"back":    {"/en/#comments"},
	})
	if posted.Code != http.StatusSeeOther {
		t.Fatalf("post: %d %s", posted.Code, posted.Body.String())
	}

	landed, _, _ := strings.Cut(posted.Header().Get("Location"), "#")
	updated := htmxRequest(t, s.App, http.MethodGet, landed, "section#comments", nil)
	expectBody(
		t,
		"after the post",
		updated,
		[]string{`<section id="comments"><p>1 on Test site</p>`, "Ada"},
		[]string{"<main", "<body>"},
	)
}

// TestHTMXSearchThrottled checks that a throttled live search answers with
// the results fragment and its message, status 429, so HTMX swaps it in
// place of the results instead of a plain error that removes them.
func TestHTMXSearchThrottled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"layout.gohtml": `<body>{{content}}</body>`,
		"page.gohtml":   `<main id="content"><h1>{{.Title}}</h1></main>`,
		"search.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.SearchView */}}` +
			`<main id="content"><input name="q" value="{{.Query}}">{{requiredSlot "results"}}</main>`,
		"fragments/results.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.SearchView */}}` +
			`<div id="results">{{if .Throttled}}<p>{{tl localizer "Too many searches"}}</p>` +
			`{{else}}{{len .Results}} results{{end}}</div>`,
	})

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir})
	s.App.Frontend.SetSearchThrottle(1, time.Minute)

	first := htmxRequest(t, s.App, http.MethodGet, "/en/search?q=home", "div#results", nil)
	expectBody(t, "first search", first, []string{`<div id="results">`, "results</div>"}, []string{"<main", "Too many"})

	throttled := htmxRequest(t, s.App, http.MethodGet, "/en/search?q=again", "div#results", nil)
	body := throttled.Body.String()

	if throttled.Code != http.StatusTooManyRequests ||
		!strings.HasPrefix(throttled.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(body, `<div id="results"><p>Too many searches</p></div>`) ||
		strings.Contains(body, "<main") {
		t.Fatalf("throttled: %d %s %s", throttled.Code, throttled.Header().Get("Content-Type"), body)
	}
}

// TestHTMXLiveSearchRegions checks that a live search, which asks for the
// results fragment at the address of the new query, gets the regions out
// of band, so the tab's title and the language links follow the query,
// while a fragment of another page still comes alone.
func TestHTMXLiveSearchRegions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"layout.gohtml": `<html><head>{{slot "head"}}</head><body>{{slot "languages"}}{{content}}</body></html>`,
		"regions/head.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.View */}}` +
			`<title>{{with .Title}}{{.}} · {{end}}{{.Site.Name}}</title>`,
		"regions/languages.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.View */}}` +
			`<nav id="languages" {{oobAttr}}>{{range .Languages}}<a href="{{.URL}}">{{.Locale}}</a>{{end}}</nav>`,
		"page.gohtml":          `<main id="content"><h1>{{.Title}}</h1>{{slot "box"}}</main>`,
		"fragments/box.gohtml": `<div id="box">box</div>`,
		"search.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.SearchView */}}` +
			`<main id="content"><input name="q" value="{{.Query}}">{{requiredSlot "results"}}</main>`,
		"fragments/results.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.SearchView */}}` +
			`<div id="results">{{len .Results}} results for {{.Query}}</div>`,
	})

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir})

	search := htmxRequest(t, s.App, http.MethodGet, "/en/search?q=cake", "div#results", nil)
	expectBody(
		t,
		"live search",
		search,
		[]string{
			`<div id="results">0 results for cake</div>`,
			"<title>cake · ",
			`<nav id="languages"`,
			`href="/en/search?q=cake"`,
		},
		[]string{"<main", "<html"},
	)

	box := htmxRequest(t, s.App, http.MethodGet, "/en/", "div#box", nil)
	expectBody(t, "page fragment", box, []string{`<div id="box">box</div>`}, []string{"<title>", `id="languages"`, "<main"})
}

// htmxRequest sends a request the way HTMX 4 does, aimed at target.
func htmxRequest(t *testing.T, app http.Handler, method, path, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}

	request := httptest.NewRequest(method, path, body)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Request-Type", "partial")
	request.Header.Set("HX-Target", target)
	request.Header.Set("Sec-Fetch-Site", "same-origin")

	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	return response
}

func expectBody(t *testing.T, name string, response *httptest.ResponseRecorder, want, unwanted []string) {
	t.Helper()

	body := response.Body.String()
	if response.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", name, response.Code, body)
	}

	for _, part := range want {
		if !strings.Contains(body, part) {
			t.Fatalf("%s: want %q in %s", name, part, body)
		}
	}

	for _, part := range unwanted {
		if strings.Contains(body, part) {
			t.Fatalf("%s: want no %q in %s", name, part, body)
		}
	}
}

// TestPageStatus checks the status of the not-found page: Templates give it
// the response itself (render.View.Status), also for an HTMX navigation,
// and a site's own Renderer gets it on the response writer.
func TestPageStatus(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"404.gohtml": `<main id="content"><h1>{{.HomeURL}} is home</h1></main>`,
	})

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir})

	s.Visitor().Expect("/en/missing", http.StatusNotFound, `<h1>/en/ is home</h1>`)

	navigation := htmxRequest(t, s.App, http.MethodGet, "/en/missing", "main#content", nil)
	if navigation.Code != http.StatusNotFound || !strings.Contains(navigation.Body.String(), `<h1>/en/ is home</h1>`) {
		t.Fatalf("HTMX navigation: %d %s", navigation.Code, navigation.Body.String())
	}

	own := sitetest.StartWith(t, sitetest.Options{
		Configure: func(config *webthings.Config) {
			config.Renderer = textRenderer{}
		},
	})

	own.Visitor().Expect("/en/missing", http.StatusNotFound, "404: Not found")
}

// textRenderer is a site's own Renderer: the template name and the title.
type textRenderer struct{}

func (textRenderer) Has(string) bool {
	return true
}

func (textRenderer) Render(w http.ResponseWriter, _ *http.Request, name string, view frontend.View) error {
	_, err := w.Write([]byte(name + ": " + view.Title))

	return err
}

// TestAdminSignInOutOverHTMX checks that signing in and out over HTMX swaps
// the whole body, as the shell changes with them, by the name of the route
// that serves the post, while other posts swap the content area.
func TestAdminSignInOutOverHTMX(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t)

	home, err := s.App.Pages.Home(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	visitor := s.Visitor()

	for _, post := range []struct {
		name   string
		route  string
		values url.Values
		target string
	}{
		{"sign in", "admin.login", url.Values{"Email": {"admin@example.test"}, "Password": {"test-only long passphrase"}}, "body"},
		{"new page", "admin.pages.new", url.Values{"Title": {"About"}, "Slug": {"about"}, "Parent": {home}}, "#content"},
		{"sign out", "admin.logout", url.Values{}, "body"},
	} {
		request, err := http.NewRequest(http.MethodPost, s.App.AdminURL(post.route), strings.NewReader(post.values.Encode()))
		if err != nil {
			t.Fatal(err)
		}

		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("HX-Request", "true")
		request.Header.Set("Sec-Fetch-Site", "same-origin")

		response := visitor.Request(request)
		_ = response.Body.Close()

		location := response.Header.Get("HX-Location")
		if !strings.Contains(location, `"target":"`+post.target+`"`) {
			t.Fatalf("%s: %d HX-Location %s", post.name, response.StatusCode, location)
		}
	}
}
