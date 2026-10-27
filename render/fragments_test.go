package render_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/donseba/go-webthings/render"

	partial "github.com/donseba/go-partial"
)

// A fragment renders inline on the page and alone for an HTMX request that
// targets its ID, with the same dot and the page's models.
func TestFragments(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<body><nav id="menu">{{slot "menu"}}</nav>{{content}}</body>`)},
		"page.gohtml":   {Data: []byte(`<main id="content"><h1>page</h1>{{requiredSlot "comments"}}</main>`)},
		"menu.gohtml":   {Data: []byte(`<nav id="menu" {{oobAttr}}>menu</nav>`)},
		"comments.gohtml": {
			Data: []byte(siteModel + `<section id="comments">{{.}} on {{Site.Name}} {{template "badge.gohtml"}}</section>`),
		},
		"badge.gohtml":   {Data: []byte(`<b>new</b>`)},
		"missing.gohtml": {Data: []byte(`<main id="content">{{requiredSlot "nope"}}</main>`)},
	}

	renderer := render.New(files, "layout.gohtml")
	view := render.View{
		Template:  "page.gohtml",
		Includes:  []string{"badge.gohtml"},
		Models:    []any{Site{Name: "Webthings"}},
		Regions:   []render.Region{{ID: "menu", Template: "menu.gohtml"}},
		Fragments: []render.Fragment{{ID: "comments", Template: "comments.gohtml", Data: "3 comments"}},
	}
	fragment := `<section id="comments">3 comments on Webthings <b>new</b></section>`

	full := httptest.NewRecorder()
	if err := renderer.Render(full, httptest.NewRequest(http.MethodGet, "/", nil), view); err != nil {
		t.Fatal(err)
	}

	if body := full.Body.String(); !strings.Contains(body, "<body>") || !strings.Contains(body, fragment) {
		t.Fatalf("full page: want the layout with the fragment inline, got %s", body)
	}

	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Target", "section#comments")

	alone := httptest.NewRecorder()
	if err := renderer.Render(alone, request, view); err != nil {
		t.Fatal(err)
	}

	body := alone.Body.String()
	if !strings.Contains(body, fragment) || strings.Contains(body, "<h1>") || strings.Contains(body, "menu") {
		t.Fatalf("fragment request: want only the fragment, got %s", body)
	}

	if alone.Header().Get("HX-Retarget") != "" {
		t.Fatalf("fragment request: HX-Retarget %q, want none", alone.Header().Get("HX-Retarget"))
	}

	missing := view
	missing.Template = "missing.gohtml"

	if err := renderer.Render(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), missing); err == nil {
		t.Fatal(`{{requiredSlot "nope"}}: want an error`)
	}
}

// With FragmentRegions, a fragment asked for on its own comes with the
// regions out of band, as the content does, from Fragments and from
// FragmentFiles.
func TestFragmentRegions(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml":         {Data: []byte(`<body>{{slot "menu"}}{{content}}</body>`)},
		"page.gohtml":           {Data: []byte(`<main id="content">{{slot "results"}}{{slot "more"}}</main>`)},
		"menu.gohtml":           {Data: []byte(`<nav id="menu" {{oobAttr}}>menu</nav>`)},
		"results.gohtml":        {Data: []byte(`<div id="results">results</div>`)},
		"fragments/more.gohtml": {Data: []byte(`<div id="more">more</div>`)},
	}

	renderer := render.New(files, "layout.gohtml")
	view := render.View{
		Template:        "page.gohtml",
		Regions:         []render.Region{{ID: "menu", Template: "menu.gohtml"}},
		Fragments:       []render.Fragment{{ID: "results", Template: "results.gohtml"}},
		FragmentFiles:   "fragments/*.gohtml",
		FragmentRegions: true,
	}

	for _, target := range []string{"div#results", "div#more"} {
		request := httptest.NewRequest(http.MethodGet, "/?q=new", nil)
		request.Header.Set("HX-Request", "true")
		request.Header.Set("HX-Target", target)

		response := httptest.NewRecorder()
		if err := renderer.Render(response, request, view); err != nil {
			t.Fatal(err)
		}

		body := response.Body.String()
		if !strings.Contains(body, `<nav id="menu"`) || strings.Contains(body, "<main") {
			t.Fatalf("%s: want the fragment with the menu out of band, got %s", target, body)
		}
	}
}

// A request for a fragment this page does not fit, such as the page a form's
// action returned to, is a navigation: the content, retargeted, with the
// address pushed.
func TestFragmentsOfAnotherPage(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<body>{{content}}</body>`)},
		"page.gohtml":   {Data: []byte(`<main id="content"><h1>{{.}}</h1></main>`)},
		"booking.gohtml": {
			Data: []byte(eventModel + `<section id="booking">{{Event.Title}}</section>`),
		},
		"comments.gohtml": {
			Data: []byte(eventDot + `<section id="comments">{{.Title}}</section>`),
		},
	}

	renderer := render.New(files, "layout.gohtml")
	fragments := []render.Fragment{
		{ID: "booking", Template: "booking.gohtml", Data: "Your booking"},
		{ID: "comments", Template: "comments.gohtml", Data: "Your booking"},
	}

	event := render.View{
		Template: "page.gohtml",
		Data:     Event{Title: "Wheel"},
		Models:   []any{Event{Title: "Wheel"}},
		Fragments: []render.Fragment{
			{ID: "booking", Template: "booking.gohtml"},
			{ID: "comments", Template: "comments.gohtml", Data: &Event{Title: "Wheel"}},
		},
	}

	for _, target := range []string{"section#booking", "section#comments"} {
		response := fragmentRequest(t, renderer, event, target)
		if body := response.Body.String(); body != `<section id="`+strings.TrimPrefix(target, "section#")+`">Wheel</section>` {
			t.Fatalf("%s on its page: got %s", target, body)
		}
	}

	booking := render.View{Template: "page.gohtml", Data: "Your booking", Fragments: fragments}

	for _, target := range []string{"section#booking", "section#comments"} {
		response := fragmentRequest(t, renderer, booking, target)

		if body := response.Body.String(); body != `<main id="content"><h1>Your booking</h1></main>` {
			t.Fatalf("%s on another page: want the content, got %s", target, body)
		}

		for header, want := range map[string]string{"HX-Retarget": "#content", "HX-Reswap": "outerHTML", "HX-Push-Url": "/booking?signup=registered"} {
			if got := response.Header().Get(header); got != want {
				t.Fatalf("%s on another page: %s %q, want %q", target, header, got, want)
			}
		}
	}
}

type Event struct {
	Title string
}

const (
	eventModel = `{{/* @model Event github.com/donseba/go-webthings/render_test.Event */}}`
	eventDot   = `{{/* @dot github.com/donseba/go-webthings/render_test.Event */}}`
)

func fragmentRequest(t *testing.T, renderer *render.Renderer, view render.View, target string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/booking?signup=registered", nil)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Target", target)

	response := httptest.NewRecorder()
	if err := renderer.Render(response, request, view); err != nil {
		t.Fatal(err)
	}

	return response
}

// A fragment that fails on the page fails the whole page, as any other part.
func TestFragmentFailing(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml":   {Data: []byte(`<body>{{content}}</body>`)},
		"page.gohtml":     {Data: []byte(`<main id="content">{{requiredSlot "comments"}}</main>`)},
		"slot.gohtml":     {Data: []byte(`<main id="content">{{slot "comments"}}</main>`)},
		"comments.gohtml": {Data: []byte(`<section id="comments">{{.Missing}}</section>`)},
	}

	renderer := render.New(files, "layout.gohtml")

	for _, page := range []string{"page.gohtml", "slot.gohtml"} {
		view := render.View{
			Template:  page,
			Fragments: []render.Fragment{{ID: "comments", Template: "comments.gohtml", Data: "text"}},
		}

		response := httptest.NewRecorder()
		if err := renderer.Render(
			response,
			httptest.NewRequest(http.MethodGet, "/", nil),
			view,
		); err == nil ||
			response.Body.Len() > 0 {
			t.Fatalf("%s: a failing fragment: %v, wrote %q", page, err, response.Body.String())
		}
	}
}

// Data computed when needed: a request for a fragment computes only the
// fragment's data; the page computes the others, each once.
func TestLazyData(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<body>{{.}}{{slot "menu"}}{{content}}</body>`)},
		"menu.gohtml":   {Data: []byte(`<nav id="menu" {{oobAttr}}>{{.}}</nav>`)},
		"page.gohtml":   {Data: []byte(`<main id="content">{{.}}{{requiredSlot "panel"}}</main>`)},
		"panel.gohtml":  {Data: []byte(`<section id="panel">{{.}}</section>`)},
	}

	calls := map[string]int{}
	data := func(name string) partial.Dot {
		return partial.DotFuncOf(func(context.Context) (string, error) {
			calls[name]++

			return name, nil
		})
	}

	renderer := render.New(files, "layout.gohtml")
	view := render.View{
		Template:  "page.gohtml",
		Dot:       data("content"),
		ShellDot:  data("shell"),
		Regions:   []render.Region{{ID: "menu", Template: "menu.gohtml", Dot: data("menu")}},
		Fragments: []render.Fragment{{ID: "panel", Template: "panel.gohtml", Dot: data("panel")}},
	}

	response := fragmentRequest(t, renderer, view, "section#panel")
	if body := response.Body.String(); body != `<section id="panel">panel</section>` ||
		len(calls) != 1 || calls["panel"] != 1 {
		t.Fatalf("fragment request: %v, %s", calls, body)
	}

	clear(calls)

	full := httptest.NewRecorder()
	if err := renderer.Render(full, httptest.NewRequest(http.MethodGet, "/", nil), view); err != nil {
		t.Fatal(err)
	}

	want := `<body>shell<nav id="menu" >menu</nav><main id="content">content<section id="panel">panel</section></main></body>`
	if body := full.Body.String(); body != want {
		t.Fatalf("page: %s", body)
	}

	for _, name := range []string{"shell", "menu", "content", "panel"} {
		if calls[name] != 1 {
			t.Fatalf("page computed %s %d times: %v", name, calls[name], calls)
		}
	}
}

// A Dot given to several parts of a page, such as the layout and its
// regions, is computed once per response.
func TestSharedDot(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<body>{{.}}{{slot "menu"}}{{content}}</body>`)},
		"menu.gohtml":   {Data: []byte(`<nav id="menu" {{oobAttr}}>{{.}}</nav>`)},
		"page.gohtml":   {Data: []byte(`<main id="content">page</main>`)},
	}

	calls := 0
	shell := partial.DotFuncOf(func(context.Context) (string, error) {
		calls++

		return "shell", nil
	})

	renderer := render.New(files, "layout.gohtml")
	view := render.View{
		Template: "page.gohtml",
		ShellDot: shell,
		Regions:  []render.Region{{ID: "menu", Template: "menu.gohtml", Dot: shell}},
	}

	full := httptest.NewRecorder()
	if err := renderer.Render(full, httptest.NewRequest(http.MethodGet, "/", nil), view); err != nil {
		t.Fatal(err)
	}

	want := `<body>shell<nav id="menu" >shell</nav><main id="content">page</main></body>`
	if body := full.Body.String(); body != want || calls != 1 {
		t.Fatalf("page: computed %d times, %s", calls, body)
	}
}

// A deferred fragment is a placeholder on the page, which asks for the
// fragment from the page's address; the request for it renders the
// fragment, whose dot the page never computed. The placeholder gets the
// fragment's PlaceholderData.
func TestDeferredFragment(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<body>{{content}}</body>`)},
		"page.gohtml":   {Data: []byte(`<main id="content">{{slot "panel"}}{{slot "plain"}}</main>`)},
		"panel.gohtml":  {Data: []byte(`<section id="panel">{{.}} {{template "badge.gohtml"}}</section>`)},
		"placeholder.gohtml": {
			Data: []byte(
				`<section id="{{.ID}}" class="loading"{{.Attrs}}>{{.Name}} {{.Data}} {{template "badge.gohtml"}}</section>`,
			),
		},
		"badge.gohtml": {Data: []byte(`<b>new</b>`)},
	}

	calls := 0
	panel := partial.DotFuncOf(func(context.Context) (string, error) {
		calls++

		return "loaded", nil
	})

	renderer := render.New(files, "layout.gohtml")
	view := render.View{
		Template: "page.gohtml",
		Includes: []string{"badge.gohtml"},
		Fragments: []render.Fragment{
			{ID: "panel", Template: "panel.gohtml", Dot: panel, Placeholder: "placeholder.gohtml", PlaceholderData: "Recent"},
			{ID: "plain", Template: "panel.gohtml", Data: "plain", Deferred: true},
		},
	}

	full := httptest.NewRecorder()
	if err := renderer.Render(full, httptest.NewRequest(http.MethodGet, "/dashboard", nil), view); err != nil {
		t.Fatal(err)
	}

	body := full.Body.String()
	for _, want := range []string{
		`<section id="panel" class="loading" hx-get="/dashboard"`,
		`>panel Recent <b>new</b></section>`,
		`<div id="plain" hx-get="/dashboard"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("page: want %s, got %s", want, body)
		}
	}

	if calls != 0 {
		t.Fatalf("page computed the deferred fragment's dot %d times", calls)
	}

	response := fragmentRequest(t, renderer, view, "section#panel")
	if body := response.Body.String(); body != `<section id="panel">loaded <b>new</b></section>` || calls != 1 {
		t.Fatalf("fragment request: computed %d times, %s", calls, body)
	}
}
