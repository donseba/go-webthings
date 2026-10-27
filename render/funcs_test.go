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
)

type key struct{}

func TestRequestFuncs(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<body>{{content}}</body>`)},
		"page.gohtml":   {Data: []byte(`<main id="content">{{greet "Ada"}} {{join "a" "b" "c"}} {{plain}}</main>`)},
	}

	funcs := template.FuncMap{
		"greet": func(ctx context.Context, name string) string {
			return ctx.Value(key{}).(string) + " " + name
		},
		"join": func(ctx context.Context, parts ...string) string {
			return strings.Join(parts, "-")
		},
		"plain": func() string {
			return "static"
		},
	}

	renderer := render.New(files, "layout.gohtml", funcs)

	for _, greeting := range []string{"Hello", "Hallo"} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request = request.WithContext(context.WithValue(request.Context(), key{}, greeting))
		response := httptest.NewRecorder()

		if err := renderer.Render(response, request, render.View{Template: "page.gohtml"}); err != nil {
			t.Fatal(err)
		}

		want := greeting + " Ada a-b-c static"
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("want %q in %s", want, response.Body.String())
		}
	}
}

// Functions are bound once per request and reused by partials, but never
// across requests: every request's partials see its own context.
func TestRequestFuncsInPartials(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<body>{{greet "layout"}} {{content}}</body>`)},
		"page.gohtml":   {Data: []byte(`<main id="content">{{range .}}{{partial runtime "card.gohtml" .}}{{end}}</main>`)},
		"card.gohtml":   {Data: []byte(`<p>{{greet .}}</p>`)},
	}

	funcs := template.FuncMap{
		"greet": func(ctx context.Context, name string) string {
			return ctx.Value(key{}).(string) + " " + name
		},
	}

	renderer := render.New(files, "layout.gohtml", funcs)

	for _, greeting := range []string{"Hello", "Hallo"} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request = request.WithContext(context.WithValue(request.Context(), key{}, greeting))
		response := httptest.NewRecorder()

		view := render.View{Template: "page.gohtml", Data: []string{"Ada", "Grace"}}
		if err := renderer.Render(response, request, view); err != nil {
			t.Fatal(err)
		}

		for _, want := range []string{greeting + " layout", "<p>" + greeting + " Ada</p>", "<p>" + greeting + " Grace</p>"} {
			if !strings.Contains(response.Body.String(), want) {
				t.Fatalf("want %q in %s", want, response.Body.String())
			}
		}
	}
}
