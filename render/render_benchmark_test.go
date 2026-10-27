package render_test

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/render"
)

// The benchmarks render a page the size of a typical WebThings page: a
// layout with five regions, one of them in <head>, and a fragment, a
// content template that calls a partial for every card, about 150 template
// functions and a model.

type benchmarkPage struct {
	Title string
	Menu  []string
	Cards []string
}

const benchmarkModel = `{{/* @model Page github.com/donseba/go-webthings/render_test.benchmarkPage */}}`

var benchmarkTemplates = map[string]string{
	"layout.gohtml": benchmarkModel + `<!doctype html><html><head><meta charset="utf-8">{{slot "head"}}</head><body>
{{slot "flash"}}<header>{{slot "menu"}}{{slot "languages"}}</header>{{slot "editor"}}
{{content}}
<footer>{{requiredSlot "newsletter"}}</footer></body></html>`,
	"regions/head.gohtml": benchmarkModel + `<title>{{Page.Title}} · {{upper "site"}}</title>
<meta name="description" content="{{Page.Title}}">
<link rel="canonical" href="https://example.com/en/{{lower Page.Title}}">
<link rel="alternate" hreflang="en" href="https://example.com/en/{{lower Page.Title}}">
<link rel="alternate" hreflang="nl" href="https://example.com/nl/{{lower Page.Title}}">`,
	"regions/menu.gohtml":      `<nav id="menu" {{oobAttr}}>{{range .Menu}}<a href="/{{lower .}}">{{.}}</a>{{end}}</nav>`,
	"regions/languages.gohtml": `<nav id="languages" {{oobAttr}}><a href="/nl/">nl</a><a href="/en/">en</a></nav>`,
	"regions/editor.gohtml":    `<div id="editor" {{oobAttr}}>{{if visitor}}<a href="/admin/">edit</a>{{end}}</div>`,
	"regions/flash.gohtml":     `<div id="flash" {{oobAttr}}>{{with flash}}<p>{{.}}</p>{{end}}</div>`,
	"page.gohtml": benchmarkModel + `<main id="content"><h1>{{Page.Title}}</h1>
{{range .Cards}}{{partial runtime "partials/card.gohtml" .}}{{end}}
{{template "partials/footer-note.gohtml" .}}</main>`,
	"partials/card.gohtml":        benchmarkModel + `<article><h2>{{upper .}}</h2><p>{{date}} · {{Page.Title}}</p></article>`,
	"partials/footer-note.gohtml": `<p>{{len .Cards}} cards</p>`,
	"fragments/newsletter.gohtml": benchmarkModel + `<div id="newsletter"><h3>{{Page.Title}}</h3><form>{{formFields "newsletter"}}</form></div>`,
}

func BenchmarkRenderPage(b *testing.B) {
	benchmarkRender(b, func(*http.Request) {})
}

func BenchmarkRenderNavigation(b *testing.B) {
	benchmarkRender(b, func(r *http.Request) {
		r.Header.Set("HX-Request", "true")
		r.Header.Set("HX-Target", "main#content")
	})
}

func BenchmarkRenderFragment(b *testing.B) {
	benchmarkRender(b, func(r *http.Request) {
		r.Header.Set("HX-Request", "true")
		r.Header.Set("HX-Target", "div#newsletter")
	})
}

func benchmarkRender(b *testing.B, htmx func(*http.Request)) {
	renderer, view := benchmarkRenderer(b)

	render := func() {
		request := httptest.NewRequest(http.MethodGet, "/en/", nil)
		htmx(request)

		response := httptest.NewRecorder()
		if err := renderer.Render(response, request, view); err != nil {
			b.Fatal(err)
		}

		if response.Body.Len() == 0 {
			b.Fatal("empty render")
		}
	}

	render()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		render()
	}
}

func benchmarkRenderer(b testing.TB) (*render.Renderer, render.View) {
	b.Helper()

	dir := b.TempDir()

	for name, source := range benchmarkTemplates {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			b.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			b.Fatal(err)
		}
	}

	renderer := render.New(os.DirFS(dir), "layout.gohtml", benchmarkFuncs())

	page := &benchmarkPage{
		Title: "Workshops",
		Menu:  []string{"Home", "Workshops", "Courses", "Work", "Journal", "About", "Practical", "Contact"},
		Cards: []string{"wheel", "glaze", "teapot", "raku", "kiln", "clay", "slip", "bisque"},
	}

	regions := make([]render.Region, 0, 5)
	for _, id := range []string{"head", "menu", "languages", "editor", "flash"} {
		regions = append(regions, render.Region{ID: id, Template: "regions/" + id + ".gohtml", Data: page})
	}

	view := render.View{
		Template:   "page.gohtml",
		Includes:   []string{"partials/footer-note.gohtml"},
		Data:       page,
		Shell:      page,
		Models:     []any{page},
		Regions:    regions,
		HeadRegion: "head",
		Fragments:  []render.Fragment{{ID: "newsletter", Template: "fragments/newsletter.gohtml", Data: page}},
	}

	return renderer, view
}

// benchmarkFuncs are as many functions as a site with its modules has, some
// of them with the request's context.
func benchmarkFuncs() template.FuncMap {
	funcs := template.FuncMap{
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"date": func(context.Context) string {
			return "5 October 2026"
		},
		"visitor": func(context.Context) bool {
			return false
		},
		"flash": func(context.Context) string {
			return ""
		},
		"formFields": func(_ context.Context, anchor string) template.HTML {
			return template.HTML(`<input type="hidden" name="back" value="#` + template.HTMLEscapeString(anchor) + `">`)
		},
	}

	for i := range 150 {
		funcs[fmt.Sprintf("module%d", i)] = func(context.Context, string) string {
			return ""
		}
	}

	return funcs
}
