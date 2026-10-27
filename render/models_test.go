package render_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/donseba/go-webthings/render"
)

type Site struct {
	Name string
}

type Visitor struct {
	Email string
}

const siteModel = `{{/* @model Site github.com/donseba/go-webthings/render_test.Site */}}`

// A model is available by name in the layout, the content, its includes and
// partials, whatever their dot.
func TestRenderModels(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(siteModel + `<title>{{Site.Name}}</title>{{content}}`)},
		"page.gohtml": {
			Data: []byte(
				siteModel + `<main id="content">{{template "header.gohtml" .}}{{partial runtime "card.gohtml" "card"}}</main>`,
			),
		},
		"header.gohtml": {Data: []byte(siteModel + `<h1>{{.}} on {{Site.Name}}</h1>`)},
		"card.gohtml":   {Data: []byte(siteModel + `<p>{{.}} by {{Site.Name}}</p>`)},
		"visitor.gohtml": {
			Data: []byte(
				`{{/* @model Visitor github.com/donseba/go-webthings/render_test.Visitor */}}<main id="content">{{Visitor.Email}}</main>`,
			),
		},
	}

	renderer := render.New(files, "layout.gohtml")

	response := httptest.NewRecorder()
	view := render.View{
		Template: "page.gohtml",
		Includes: []string{"header.gohtml"},
		Data:     "welcome",
		Models:   []any{&Site{Name: "Webthings"}},
	}

	if err := renderer.Render(response, httptest.NewRequest(http.MethodGet, "/", nil), view); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"<title>Webthings</title>", "<h1>welcome on Webthings</h1>", "<p>card by Webthings</p>"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("want %q in %s", want, response.Body.String())
		}
	}

	missing := render.View{Template: "visitor.gohtml", Models: []any{Site{Name: "Webthings"}}}
	if err := renderer.Render(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), missing); err == nil {
		t.Fatal("render without a value for @model Visitor: want an error")
	}
}
