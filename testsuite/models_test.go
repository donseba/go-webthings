package testsuite

import (
	"net/http"
	"testing"

	"github.com/donseba/go-webthings/modules/comments"
	"github.com/donseba/go-webthings/sitetest"
)

// TestTemplateModels lets the layout, partials and components of a theme
// name the page and its content with @model, whatever their dot.
func TestTemplateModels(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	view := `{{/* @model View github.com/donseba/go-webthings/frontend.View */}}`
	page := `{{/* @model Page github.com/donseba/go-webthings/frontend.Page */}}`

	writeFiles(t, dir, map[string]string{
		"layout.gohtml": view + `<body><p class="layout">{{View.Locale}}</p>{{content}}</body>`,
		"page.gohtml": `<main id="content"><h1>{{.Title}}</h1>` +
			`{{template "partials/site.gohtml" "partial"}}{{commentThread "remarks" "home"}}</main>`,
		"partials/site.gohtml":  view + `<p class="partial">{{.}}: {{View.Site.Name}}</p>`,
		"remarks/thread.gohtml": view + page + `<p class="component">{{View.Site.Name}} / {{Page.Title}}</p>`,
	})

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir}, comments.Module("remarks", "Comments"))

	s.Visitor().Expect("/en/", http.StatusOK,
		`<p class="layout">en</p>`,
		`<p class="partial">partial: Test site</p>`,
		`<p class="component">Test site / Home</p>`,
	)
}
