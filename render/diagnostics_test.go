package render_test

import (
	"bytes"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/donseba/go-webthings/render"

	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/exp/flash"
)

// TestDevelopmentErrors checks that in development a failing template is
// shown with its template and line and status 500, and the error says so;
// without development nothing is written (see TestContentErrors).
func TestDevelopmentErrors(t *testing.T) {
	funcs := template.FuncMap{
		"broken": func() (string, error) {
			return "", errors.New("component exploded")
		},
	}

	cases := map[string]fstest.MapFS{
		"content": {
			"layout.gohtml": {Data: []byte(`<body><h1>Layout</h1>{{content}}</body>`)},
			"page.gohtml":   {Data: []byte("<main id=\"content\">\n{{broken}}</main>")},
		},
		"layout": {
			"layout.gohtml": {Data: []byte("<body>\n{{broken}}{{content}}</body>")},
			"page.gohtml":   {Data: []byte(`<main id="content">fine</main>`)},
		},
	}

	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			renderer := render.New(files, "layout.gohtml", funcs)
			renderer.SetDiagnostics(render.Diagnostics{Development: true})

			response := httptest.NewRecorder()

			err := renderer.Render(response, httptest.NewRequest(http.MethodGet, "/", nil), render.View{Template: "page.gohtml"})
			if !errors.Is(err, render.ErrShown) {
				t.Fatalf("want ErrShown, got %v", err)
			}

			body := response.Body.String()
			if response.Code != http.StatusInternalServerError || !strings.Contains(body, "component exploded") ||
				!strings.Contains(body, ".gohtml:2") {
				t.Fatalf("status %d, body:\n%s", response.Code, body)
			}
		})
	}
}

// TestDevelopmentErrorsHTMX checks that in development a failing HTMX
// request keeps status 500, is never cached and comes without the layout's
// regions, so HTMX does not swap the error page in as content.
func TestDevelopmentErrorsHTMX(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<body><nav id="menu">{{slot "menu"}}</nav>{{content}}</body>`)},
		"menu.gohtml":   {Data: []byte(`<nav id="menu" {{oobAttr}}>MENU</nav>`)},
		"page.gohtml":   {Data: []byte("<main id=\"content\">\n{{broken}}</main>")},
	}

	funcs := template.FuncMap{
		"broken": func() (string, error) {
			return "", errors.New("component exploded")
		},
	}

	renderer := render.New(files, "layout.gohtml", funcs)
	renderer.SetDiagnostics(render.Diagnostics{Development: true})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Target", "main#content")

	response := httptest.NewRecorder()

	err := renderer.Render(response, request, render.View{
		Template: "page.gohtml",
		Regions:  []render.Region{{ID: "menu", Template: "menu.gohtml"}},
	})
	if !errors.Is(err, render.ErrShown) {
		t.Fatalf("want ErrShown, got %v", err)
	}

	body := response.Body.String()
	if response.Code != http.StatusInternalServerError || !strings.Contains(body, "component exploded") {
		t.Fatalf("status %d, body:\n%s", response.Code, body)
	}

	if cache := response.Header().Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("Cache-Control %q, want no-store", cache)
	}

	if strings.Contains(body, "MENU") {
		t.Fatalf("the error came with the regions:\n%s", body)
	}
}

// TestDebug checks that {{debug .}} shows the value in development only,
// and that a site's own debug function wins.
func TestDebug(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<body>{{content}}</body>`)},
		"page.gohtml":   {Data: []byte(`<main id="content">{{debug .}}</main>`)},
	}

	view := render.View{Template: "page.gohtml", Data: map[string]string{"Name": "Ada"}}

	show := func(renderer *render.Renderer) string {
		t.Helper()

		response := httptest.NewRecorder()
		if err := renderer.Render(response, httptest.NewRequest(http.MethodGet, "/", nil), view); err != nil {
			t.Fatal(err)
		}

		return response.Body.String()
	}

	production := render.New(files, "layout.gohtml")
	if body := show(production); body != `<body><main id="content"></main></body>` {
		t.Fatalf("production shows debug output:\n%s", body)
	}

	development := render.New(files, "layout.gohtml")
	development.SetDiagnostics(render.Diagnostics{Development: true})

	if body := show(development); !strings.Contains(body, "go-partial-debug") || !strings.Contains(body, "Ada") {
		t.Fatalf("development shows no debug output:\n%s", body)
	}

	own := render.New(files, "layout.gohtml", template.FuncMap{
		"debug": func(any) string {
			return "mine"
		},
	})
	own.SetDiagnostics(render.Diagnostics{Development: true})

	if body := show(own); !strings.Contains(body, "mine") {
		t.Fatalf("the site's debug function lost:\n%s", body)
	}
}

// TestDiagnosticsStagesAndWarnings checks that extra stages and event sinks
// see the renders, and that go-partial's warnings reach the logger.
func TestDiagnosticsStagesAndWarnings(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<body>{{content}}</body>`)},
		"page.gohtml":   {Data: []byte(`<main id="content">hi</main>`)},
	}

	var (
		renders int
		events  int
		log     bytes.Buffer
	)

	stage := partial.RenderStageHooks{
		PrepareFunc: func(rc *partial.RenderContext) (*partial.RenderContext, error) {
			rc.Emit(partial.Event{Kind: "test.warning", Level: partial.EventWarn, Message: "careful"})

			return rc, nil
		},
		FinalizeFunc: func(_ *partial.RenderContext, out template.HTML, err error) (template.HTML, error) {
			renders++

			return out, err
		},
	}

	sink := partial.EventSinkFunc(func(*partial.RenderContext, partial.Event) {
		events++
	})

	renderer := render.New(files, "layout.gohtml")
	renderer.SetDiagnostics(render.Diagnostics{
		Logger: slog.New(slog.NewTextHandler(&log, nil)),
		Stages: []partial.RenderStage{stage},
		Events: []partial.EventSink{sink},
	})

	response := httptest.NewRecorder()
	if err := renderer.Render(
		response,
		httptest.NewRequest(http.MethodGet, "/", nil),
		render.View{Template: "page.gohtml"},
	); err != nil {
		t.Fatal(err)
	}

	if renders < 2 {
		t.Fatalf("the stage saw %d renders, want the layout and the content", renders)
	}

	if events == 0 {
		t.Fatal("the event sink saw nothing")
	}

	if !strings.Contains(log.String(), "level=WARN msg=careful") || !strings.Contains(log.String(), "partial.kind=test.warning") {
		t.Fatalf("warning not logged:\n%s", log.String())
	}
}

// TestErrorTemplates checks that outside development a failing render is
// answered with the error templates: the page for a full request, the
// fragment for an HTMX one, with status 500 and without the error's
// details.
func TestErrorTemplates(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml":   {Data: []byte(`<body><nav id="menu">{{slot "menu"}}</nav>{{content}}</body>`)},
		"menu.gohtml":     {Data: []byte(`<nav id="menu" {{oobAttr}}>MENU</nav>`)},
		"page.gohtml":     {Data: []byte(`<main id="content">{{broken}}</main>`)},
		"500.gohtml":      {Data: []byte(siteModel + `<h1>Sorry, {{Site.Name}} broke</h1>{{if .Detailed}}{{.Message}}{{end}}`)},
		"fragment.gohtml": {Data: []byte(`<p role="alert">That did not work</p>`)},
	}

	funcs := template.FuncMap{
		"broken": func() (string, error) {
			return "", errors.New("secret details")
		},
	}

	view := render.View{
		Template: "page.gohtml",
		Models:   []any{Site{Name: "Webthings"}},
		Regions:  []render.Region{{ID: "menu", Template: "menu.gohtml"}},
	}

	cases := []struct {
		name     string
		fragment string
		target   string
		want     string
	}{
		{name: "page", want: "<h1>Sorry, Webthings broke</h1>"},
		{
			name:     "partial request",
			fragment: "fragment.gohtml",
			target:   "main#content",
			want:     `<p role="alert">That did not work</p>`,
		},
		{name: "partial request, page only", target: "main#content", want: "<h1>Sorry, Webthings broke</h1>"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			renderer := render.New(files, "layout.gohtml", funcs)
			renderer.SetDiagnostics(render.Diagnostics{ErrorPage: "500.gohtml", ErrorFragment: test.fragment})

			request := httptest.NewRequest(http.MethodGet, "/", nil)
			if test.target != "" {
				request.Header.Set("HX-Request", "true")
				request.Header.Set("HX-Target", test.target)
			}

			response := httptest.NewRecorder()

			err := renderer.Render(response, request, view)
			if !errors.Is(err, render.ErrShown) {
				t.Fatalf("want ErrShown, got %v", err)
			}

			body := response.Body.String()
			if response.Code != http.StatusInternalServerError || !strings.Contains(body, test.want) {
				t.Fatalf("status %d, body:\n%s", response.Code, body)
			}

			if strings.Contains(body, "secret details") || strings.Contains(body, "MENU") {
				t.Fatalf("the error page shows details or regions:\n%s", body)
			}
		})
	}
}

// TestErrorFlash checks that outside development a partial request that
// fails to render is answered with ErrorFlash's message, out of band in the
// flash target, leaving the request's target as it is; a full request still
// gets the error page.
func TestErrorFlash(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<body>{{content}}</body>`)},
		"page.gohtml":   {Data: []byte(`<main id="content">{{broken}}</main>`)},
		"500.gohtml":    {Data: []byte(`<h1>Sorry</h1>`)},
		"flash.gohtml":  {Data: []byte(`{{range .Messages}}<p class="toast" data-flash-level="{{.Level}}">{{.Text}}</p>{{end}}`)},
	}

	funcs := template.FuncMap{
		"broken": func() (string, error) {
			return "", errors.New("secret details")
		},
	}

	renderer := render.New(files, "layout.gohtml", funcs)
	renderer.Use(flash.Stage(flash.WithTemplate("flash.gohtml")))
	renderer.SetDiagnostics(render.Diagnostics{
		ErrorPage: "500.gohtml",
		ErrorFlash: func(r *http.Request) string {
			return "Failed at " + r.URL.Path
		},
	})

	view := render.View{Template: "page.gohtml"}

	request := httptest.NewRequest(http.MethodGet, "/pages", nil)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Target", "main#content")

	response := httptest.NewRecorder()
	if err := renderer.Render(response, request, view); !errors.Is(err, render.ErrShown) {
		t.Fatalf("want ErrShown, got %v", err)
	}

	body := response.Body.String()

	toast := `<div id="flash-messages" hx-swap-oob="beforeend"><p class="toast" data-flash-level="error">Failed at /pages</p></div>`
	if !strings.Contains(body, toast) ||
		strings.Contains(body, "secret details") ||
		response.Header().Get("HX-Reswap") != "none" {
		t.Fatalf("status %d, HX-Reswap %q, body:\n%s", response.Code, response.Header().Get("HX-Reswap"), body)
	}

	full := httptest.NewRecorder()
	if err := renderer.Render(full, httptest.NewRequest(http.MethodGet, "/pages", nil), view); !errors.Is(err, render.ErrShown) {
		t.Fatalf("want ErrShown, got %v", err)
	}

	if body := full.Body.String(); full.Code != http.StatusInternalServerError || body != "<h1>Sorry</h1>" {
		t.Fatalf("page: status %d, body:\n%s", full.Code, body)
	}
}
