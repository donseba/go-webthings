package testsuite

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/sitetest"

	form "github.com/donseba/go-form/v2"
)

// TestLocalizedTemplates checks dates and money in the page's language and
// the site's timezone, and the page's place in the tree.
func TestLocalizedTemplates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"layout.gohtml": `<nav>{{range .Menu}}<a data-path="{{.Path}}" data-id="{{.ID}}">{{.Title}}</a>` +
			`{{range .Children}}<a data-path="{{.Path}}">{{.Title}}</a>{{end}}{{end}}</nav>{{content}}`,
		"page.gohtml": `<main id="content"><p class="place">{{.Path}}|{{.Slug}}|{{.ParentID}}</p>` +
			`<p class="when">{{date stamp "full"}} · {{time stamp}} · {{dateRange stamp (stamp.AddDate 0 0 2)}}</p>` +
			`<p class="price">{{money 4500 "EUR"}}</p>{{form_open stampForm}}{{form_close}}</main>`,
	})

	stamp := webthings.Module{
		Type: "stamp",
		Key:  "stamp",
		Funcs: template.FuncMap{
			"stamp": func() time.Time {
				return time.Date(2026, time.October, 25, 13, 0, 0, 0, time.UTC)
			},
			// The way back of a visitor form is the page being rendered.
			"stampForm": func(ctx context.Context) form.RenderModel {
				return frontend.VisitorForm(ctx, struct{}{}, "/_/stamp/send", "sign")
			},
		},
		Setup: func(webthings.Env) (webthings.Parts, error) {
			return webthings.Parts{}, nil
		},
	}

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir}, stamp)
	ctx := context.Background()

	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	settings.Timezone = "Europe/Amsterdam"
	if err := s.App.Site.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	about := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About"}, "Slug": {"about"}, "Parent": {home}})
	s.Publish(about + "?locale=en")

	aboutID := strings.TrimPrefix(about, "/admin/pages/")

	team := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"Team"}, "Slug": {"team"}, "Parent": {aboutID}})
	s.Publish(team + "?locale=en")

	s.Expect("/en/about/team", http.StatusOK,
		`<p class="place">/about/team|team|`+aboutID+`</p>`,
		`<a data-path="/about" data-id="`+aboutID+`">About</a><a data-path="/about/team">Team</a>`,
		`Sunday 25 October 2026 · 14:00 · 25–27 October 2026`,
		`€45.00`,
		`name="back" value="/en/about/team#sign"`,
	)

	s.Expect("/nl/", http.StatusOK,
		`<p class="place">/||</p>`,
		`zondag 25 oktober 2026 · 14:00 · 25–27 oktober 2026`,
		"€ 45,00",
	)
}
