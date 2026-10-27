package render_test

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/donseba/go-webthings/render"

	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/exp/metrics"
	"github.com/donseba/go-partial/exp/sse"
)

type ctxKey struct{}

// TestRoot checks that a part patched over server-sent events under the
// renderer's root renders with the renderer's functions, bound to the
// request, and its file system, also for an HTMX request that targets
// another element, and that failures are returned.
func TestRoot(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<body>LAYOUT {{content}}</body>`)},
		"chip.gohtml":   {Data: []byte(`<span id="chip">{{shout .}} {{who}}</span>`)},
		"broken.gohtml": {Data: []byte(`<span>{{nope}}</span>`)},
	}

	funcs := template.FuncMap{
		"shout": func(value string) string {
			return value + "!"
		},
		"who": func(ctx context.Context) string {
			name, _ := ctx.Value(ctxKey{}).(string)

			return name
		},
	}

	renderer := render.New(files, "layout.gohtml", funcs)

	request := httptest.NewRequest(http.MethodGet, "/events", nil)
	request = request.WithContext(context.WithValue(request.Context(), ctxKey{}, "ann"))
	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Target", "div#elsewhere")

	response := httptest.NewRecorder()
	events := sse.NewWriter(response).SetParent(renderer.Root())

	chip := partial.NewID("chip", "chip.gohtml").SetDot("hi")
	if err := events.PatchPartial(request.Context(), request, "#chip", chip); err != nil {
		t.Fatal(err)
	}

	if body := response.Body.String(); !strings.Contains(body, `<span id=\"chip\">hi! ann</span>`) ||
		strings.Contains(body, "LAYOUT") {
		t.Fatalf("patch = %q", body)
	}

	broken := partial.NewID("broken", "broken.gohtml")
	if err := events.PatchPartial(request.Context(), request, "#chip", broken); err == nil {
		t.Fatal("a failing template rendered")
	}
}

// TestRenderPartial checks that a part renders on its own with the
// renderer's functions, the models it is given and the context, from a file
// system of its own; a failing part returns its error and no HTML.
func TestRenderPartial(t *testing.T) {
	renderer := render.New(nil, "", template.FuncMap{
		"who": func(ctx context.Context) string {
			name, _ := ctx.Value(ctxKey{}).(string)

			return name
		},
	})

	files := partial.Overlay(fstest.MapFS{
		"card.gohtml": {Data: []byte(siteModel + `<p>{{.}} for {{who}} on {{Site.Name}}</p>`)},
	}, partial.Sources{"broken.gohtml": `{{nope}}`})

	ctx := context.WithValue(context.Background(), ctxKey{}, "ann")
	card := partial.NewID("card", "card.gohtml").SetFileSystem(files).SetDot("hi")

	html, err := renderer.RenderPartial(ctx, nil, card, Site{Name: "Webthings"})
	if err != nil || html != `<p>hi for ann on Webthings</p>` {
		t.Fatalf("RenderPartial = %q, %v", html, err)
	}

	broken := partial.NewID("broken", "broken.gohtml").SetFileSystem(files)
	if html, err := renderer.RenderPartial(ctx, nil, broken); err == nil || html != "" {
		t.Fatalf("a failing part: %q, %v", html, err)
	}
}

// TestRenderPartialInPage checks that a part a template function renders
// during a page, such as a component, belongs to the page's render: the
// page's outermost metrics record covers it, and its failure fails the page.
func TestRenderPartialInPage(t *testing.T) {
	var renderer *render.Renderer

	parts := partial.Sources{
		"chip.gohtml":   `<b>{{.}}</b>`,
		"broken.gohtml": `{{nope}}`,
	}

	files := fstest.MapFS{
		"layout.gohtml":  {Data: []byte(`<body>{{content}}</body>`)},
		"page.gohtml":    {Data: []byte(`<main id="content">{{chip "chip"}}</main>`)},
		"failing.gohtml": {Data: []byte(`<main id="content">{{chip "broken"}}</main>`)},
	}

	renderer = render.New(files, "layout.gohtml", template.FuncMap{
		"chip": func(ctx context.Context, name string) (template.HTML, error) {
			part := partial.NewID(name, name+".gohtml").SetFileSystem(parts).SetDot("hi")

			return renderer.RenderPartial(ctx, nil, part)
		},
	})

	var records int

	renderer.SetDiagnostics(render.Diagnostics{
		Stages: []partial.RenderStage{metrics.Stage(metrics.SinkFunc(func(metrics.Record) {
			records++
		}), metrics.Outermost())},
	})

	response := httptest.NewRecorder()
	if err := renderer.Render(
		response,
		httptest.NewRequest(http.MethodGet, "/", nil),
		render.View{Template: "page.gohtml"},
	); err != nil {
		t.Fatal(err)
	}

	if response.Body.String() != `<body><main id="content"><b>hi</b></main></body>` || records != 1 {
		t.Fatalf("%d records for %s", records, response.Body.String())
	}

	response = httptest.NewRecorder()
	if err := renderer.Render(
		response,
		httptest.NewRequest(http.MethodGet, "/", nil),
		render.View{Template: "failing.gohtml"},
	); err == nil ||
		response.Body.Len() > 0 {
		t.Fatalf("a failing part: %v, wrote %q", err, response.Body.String())
	}
}
