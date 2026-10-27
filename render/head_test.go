package render_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/donseba/go-webthings/render"
)

// TestHeadRegion checks that the head region of a view sits between the
// markers on a full page and replaces the elements between them out of
// band, as a region of RegionFiles and of Regions, and that the other
// regions are not affected.
func TestHeadRegion(t *testing.T) {
	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(
			`<html><head>{{slot "head"}}<link rel="stylesheet" href="/site.css"></head><body>{{slot "nav"}}{{content}}</body></html>`,
		)},
		"regions/head.gohtml": {Data: []byte(`<title>{{.}}</title><link rel="canonical" href="/{{.}}">`)},
		"regions/nav.gohtml":  {Data: []byte(`<nav id="nav" {{oobAttr}}>{{.}}</nav>`)},
		"page.gohtml":         {Data: []byte(`<main id="content">{{.}}</main>`)},
	}

	views := map[string]render.View{
		"files": {Template: "page.gohtml", Data: "a", Shell: "a", RegionFiles: "regions/*.gohtml", HeadRegion: "head"},
		"regions": {
			Template: "page.gohtml",
			Data:     "a",
			Regions: []render.Region{
				{ID: "head", Template: "regions/head.gohtml", Data: "a"},
				{ID: "nav", Template: "regions/nav.gohtml", Data: "a"},
			},
			HeadRegion: "head",
		},
	}

	for name, view := range views {
		renderer := render.New(files, "layout.gohtml")

		page := httptest.NewRecorder()
		if err := renderer.Render(page, httptest.NewRequest(http.MethodGet, "/", nil), view); err != nil {
			t.Fatal(err)
		}

		want := `<html><head><title>a</title><template data-partial-head="head"></template>` +
			`<link rel="canonical" href="/a"><template data-partial-head-end="head"></template>` +
			`<link rel="stylesheet" href="/site.css"></head><body><nav id="nav" >a</nav><main id="content">a</main></body></html>`
		if page.Body.String() != want {
			t.Fatalf("%s: page %s", name, page.Body.String())
		}

		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("HX-Request", "true")
		request.Header.Set("HX-Request-Type", "partial")
		request.Header.Set("HX-Target", "main#content")

		navigation := httptest.NewRecorder()
		if err := renderer.Render(navigation, request, view); err != nil {
			t.Fatal(err)
		}

		body := navigation.Body.String()
		remove := `<title>a</title><div hx-swap-oob="delete:[data-partial-head=&#34;head&#34;]`
		add := `<div hx-swap-oob="beforebegin:[data-partial-head-end=&#34;head&#34;]">` +
			`<link rel="canonical" href="/a"></div>`

		if !strings.Contains(body, `<main id="content">a</main>`) ||
			!strings.Contains(body, `<nav id="nav"  hx-swap-oob="true">a</nav>`) ||
			!strings.Contains(body, remove) || !strings.Contains(body, add) || strings.Contains(body, "<template") {
			t.Fatalf("%s: navigation %s", name, body)
		}
	}
}
