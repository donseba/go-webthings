package render_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/render"
)

// TestRenderDeterministic checks that a page, an HTMX navigation and a
// fragment give the same bytes on every render, with the regions in the
// order the view lists them, for Regions and for RegionFiles, where the
// order is that of the file names; ETags and the output cache rely on it.
func TestRenderDeterministic(t *testing.T) {
	renderer, regions := benchmarkRenderer(t)

	files := regions
	files.Regions = nil
	files.RegionFiles = "regions/*.gohtml"

	requests := map[string]func(*http.Request){
		"page": func(*http.Request) {},
		"navigation": func(r *http.Request) {
			r.Header.Set("HX-Request", "true")
			r.Header.Set("HX-Request-Type", "partial")
			r.Header.Set("HX-Target", "main#content")
		},
		"fragment": func(r *http.Request) {
			r.Header.Set("HX-Request", "true")
			r.Header.Set("HX-Request-Type", "partial")
			r.Header.Set("HX-Target", "div#newsletter")
		},
	}

	navigations := map[string]string{}

	for name, view := range map[string]render.View{"Regions": regions, "RegionFiles": files} {
		for kind, htmx := range requests {
			var first string

			for i := range 100 {
				request := httptest.NewRequest(http.MethodGet, "/en/", nil)
				htmx(request)

				response := httptest.NewRecorder()
				if err := renderer.Render(response, request, view); err != nil {
					t.Fatal(err)
				}

				body := response.Body.String() + "\nVary: " + strings.Join(response.Header().Values("Vary"), ",")
				if i == 0 {
					first = body
				}

				if body != first {
					t.Fatalf("%s %s changed between renders:\n%s\n%s", name, kind, first, body)
				}
			}

			if kind == "navigation" {
				navigations[name] = first
			}
		}
	}

	order := map[string][]string{
		"Regions":     {`<title>`, `id="menu"`, `id="languages"`, `id="editor"`, `id="flash"`},
		"RegionFiles": {`id="editor"`, `id="flash"`, `<title>`, `id="languages"`, `id="menu"`},
	}

	for name, ids := range order {
		body := navigations[name]
		last := -1

		for _, id := range ids {
			at := strings.Index(body, id)
			if at <= last {
				t.Fatalf("%s: %s out of order: %s", name, id, body)
			}

			last = at
		}
	}
}
