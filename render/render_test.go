package render_test

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/donseba/go-webthings/render"
)

// TestContentErrors checks that a failing content template fails the whole
// render: nothing reaches the visitor, the caller gets the error.
func TestContentErrors(t *testing.T) {
	funcs := template.FuncMap{
		"broken": func() (string, error) {
			return "", errors.New("component exploded")
		},
	}

	cases := map[string]string{
		"execution":        `<main id="content">{{broken}}</main>`,
		"unknown function": `<main id="content">{{nope}}</main>`,
		"missing partial":  `<main id="content">{{template "partials/missing.gohtml" .}}</main>`,
		"parse":            `<main id="content">{{if}}</main>`,
	}

	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			files := fstest.MapFS{
				"layout.gohtml": {Data: []byte(`<body>{{content}}</body>`)},
				"page.gohtml":   {Data: []byte(source)},
			}

			renderer := render.New(files, "layout.gohtml", funcs)
			response := httptest.NewRecorder()

			err := renderer.Render(response, httptest.NewRequest(http.MethodGet, "/", nil), render.View{Template: "page.gohtml"})
			if err == nil {
				t.Fatalf("no error; wrote %s", response.Body.String())
			}

			if response.Body.Len() > 0 || response.Header().Get("Content-Type") != "" {
				t.Fatalf("wrote %q with %v", response.Body.String(), response.Header())
			}
		})
	}
}

// TestFunctionOverride checks that a later function without a context
// replaces one with.
func TestFunctionOverride(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<body>{{content}}</body>`)},
		"page.gohtml":   {Data: []byte(`<main id="content">{{date "x"}}</main>`)},
	}

	core := template.FuncMap{
		"date": func(_ context.Context, value string) string {
			return "core " + value
		},
	}
	site := template.FuncMap{
		"date": func(value string) string {
			return "site " + value
		},
	}

	renderer := render.New(files, "layout.gohtml", core, site)

	for range 2 {
		response := httptest.NewRecorder()

		err := renderer.Render(response, httptest.NewRequest(http.MethodGet, "/", nil), render.View{Template: "page.gohtml"})
		if err != nil {
			t.Fatal(err)
		}

		if !strings.Contains(response.Body.String(), "site x") {
			t.Fatalf("got %s", response.Body.String())
		}
	}
}

// TestHTMXTargets checks the answers to HTMX requests: the content, a
// region, and the content retargeted for any other element.
func TestHTMXTargets(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<body><header id="top">{{slot "top"}}</header>{{content}}</body>`)},
		"page.gohtml":   {Data: []byte(`<main id="content">page</main>`)},
		"top.gohtml":    {Data: []byte(`<nav id="top">menu</nav>`)},
	}

	renderer := render.New(files, "layout.gohtml")
	view := render.View{
		Template: "page.gohtml",
		Regions:  []render.Region{{ID: "top", Template: "top.gohtml"}},
	}

	cases := []struct {
		target   string
		want     string
		retarget string
	}{
		{target: "main#content", want: `<main id="content">page</main>`},
		{target: "nav#top", want: `<nav id="top"`},
		{target: "div#results", want: `<main id="content">page</main>`, retarget: "#content"},
	}

	for _, c := range cases {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("HX-Request", "true")
		request.Header.Set("HX-Target", c.target)

		response := httptest.NewRecorder()

		if err := renderer.Render(response, request, view); err != nil {
			t.Fatalf("%s: %v", c.target, err)
		}

		body := response.Body.String()
		if response.Code != http.StatusOK || !strings.Contains(body, c.want) || strings.Contains(body, "<body>") {
			t.Fatalf("%s: %d %s", c.target, response.Code, body)
		}

		if got := response.Header().Get("HX-Retarget"); got != c.retarget {
			t.Fatalf("%s: HX-Retarget %q", c.target, got)
		}

		if c.retarget != "" && response.Header().Get("HX-Reswap") != "outerHTML" {
			t.Fatalf("%s: HX-Reswap %q", c.target, response.Header().Get("HX-Reswap"))
		}

		// Caches keep the page and its parts apart.
		vary := strings.Join(response.Header().Values("Vary"), ", ")
		if !strings.Contains(vary, "HX-Request") || !strings.Contains(vary, "HX-Target") ||
			!strings.Contains(vary, "HX-Request-Type") || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("%s: Vary %q, Content-Type %q", c.target, vary, response.Header().Get("Content-Type"))
		}
	}
}

// TestStatus checks that a view's status is written for the page and for
// an HTMX request for its content, such as a form with errors.
func TestStatus(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<body>{{content}}</body>`)},
		"page.gohtml":   {Data: []byte(`<main id="content">page</main>`)},
	}

	renderer := render.New(files, "layout.gohtml")
	view := render.View{Template: "page.gohtml", Status: http.StatusUnprocessableEntity}

	for _, target := range []string{"", "main#content"} {
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		if target != "" {
			request.Header.Set("HX-Request", "true")
			request.Header.Set("HX-Target", target)
		}

		response := httptest.NewRecorder()
		if err := renderer.Render(response, request, view); err != nil {
			t.Fatal(err)
		}

		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("target %q: status %d", target, response.Code)
		}
	}
}

// TestTemplateCache checks that templates are parsed once, and read again
// for every render when the renderer reloads.
func TestTemplateCache(t *testing.T) {
	for _, reload := range []bool{false, true} {
		dir := t.TempDir()
		write(t, dir, "layout.gohtml", `<body class="v1">{{content}}</body>`)
		write(t, dir, "page.gohtml", `<main id="content">v1</main>`)

		renderer := render.New(os.DirFS(dir), "layout.gohtml")
		renderer.SetReload(reload)

		first := renderBody(t, renderer)

		write(t, dir, "layout.gohtml", `<body class="v2">{{content}}</body>`)
		write(t, dir, "page.gohtml", `<main id="content">v2</main>`)

		second := renderBody(t, renderer)

		if !strings.Contains(first, `class="v1"`) || !strings.Contains(first, ">v1<") {
			t.Fatalf("first render: %s", first)
		}

		changed := strings.Contains(second, `class="v2"`) && strings.Contains(second, ">v2<")
		kept := strings.Contains(second, `class="v1"`) && strings.Contains(second, ">v1<")

		if reload && !changed || !reload && !kept {
			t.Fatalf("reload %v: %s", reload, second)
		}
	}
}

func renderBody(t *testing.T, renderer *render.Renderer) string {
	t.Helper()

	response := httptest.NewRecorder()

	if err := renderer.Render(
		response,
		httptest.NewRequest(http.MethodGet, "/", nil),
		render.View{Template: "page.gohtml"},
	); err != nil {
		t.Fatal(err)
	}

	return response.Body.String()
}

func write(t *testing.T, dir, name, source string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReserved checks the names go-partial keeps for itself: a template
// function with one of them, with or without a context, is never called.
func TestReserved(t *testing.T) {
	names := []string{
		"runtime", "partial", "content", "ctx", "request", "url", "basePath",
		"urlIs", "urlStarts", "urlContains", "joinPath", "urlPath", "oob", "oobAttr",
		"locale", "localizer", "slot", "hasSlot", "requiredSlot", "deferredSlot", "_private",
	}

	for _, name := range names {
		if !render.Reserved(name) {
			t.Fatalf("%s is not reserved", name)
		}
	}

	if render.Reserved("date") {
		t.Fatal("date is reserved")
	}

	for _, name := range names {
		if strings.HasPrefix(name, "_") {
			continue
		}

		funcs := []template.FuncMap{
			{name: func(...any) string { return "site function" }},
			{name: func(context.Context, ...any) string { return "site function" }},
		}

		for _, set := range funcs {
			files := fstest.MapFS{
				"layout.gohtml": {Data: []byte(`<body>{{content}}</body>`)},
				"page.gohtml":   {Data: []byte(`<main id="content">{{` + name + `}}</main>`)},
			}

			renderer := render.New(files, "layout.gohtml", set)
			response := httptest.NewRecorder()

			// go-partial's own helper renders or fails (for example on
			// missing arguments); the site's function is never called.
			_ = renderer.Render(response, httptest.NewRequest(http.MethodGet, "/", nil), render.View{Template: "page.gohtml"})
			if strings.Contains(response.Body.String(), "site function") {
				t.Errorf("%s: the site's function was called", name)
			}
		}
	}
}
