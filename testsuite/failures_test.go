package testsuite

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/sitetest"

	"github.com/donseba/go-importmap/assets"
	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/exp/flash"
	partialerrors "github.com/donseba/go-partial/ext/errors"
)

// breaker is a module whose visitor action fails, as a save that the
// database refuses does.
func breaker() webthings.Module {
	return webthings.Module{
		Type: "breaker",
		Key:  "breaker",
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			env.Actions().Post("/post", func(w http.ResponseWriter, r *http.Request) {
				env.Frontend.Fail(w, r, errors.New("disk on fire"))
			})

			return webthings.Parts{}, nil
		},
	}
}

// TestActionFailure checks that a visitor action that fails answers with
// the theme's 500.gohtml in the language of the form's page, with the View
// for @model and the theme's partials, without the error: for a whole
// page, for an HTMX post, which the interactions client's reloadOnError
// shows as the whole page, and also when the site's renders have a flash
// stage, which the frontend's error page does not use.
func TestActionFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"templates/layout.gohtml":        `<html>{{content}}</html>`,
		"templates/page.gohtml":          `<main id="content">{{.Title}}</main>`,
		"templates/partials/home.gohtml": `<a href="{{.}}">home</a>`,
		"templates/regions/flash.gohtml": `<div id="flash" {{oobAttr}}></div>`,
		"templates/flash.gohtml":         `<p class="flash">{{.Text}}</p>`,
		"templates/500.gohtml": `{{/* @model View github.com/donseba/go-webthings/frontend.View */}}` +
			`<!doctype html><html lang="{{View.Locale}}"><h1>{{tl localizer "Something went wrong"}} · {{View.Site.Name}}</h1>` +
			`<p class="message">{{.Message}}</p>{{template "partials/home.gohtml" View.Site.HomeURL}}</html>`,
		"translations/nl.po": `msgid ""
msgstr ""
"Language: nl\n"
"Content-Type: text/plain; charset=UTF-8\n"

msgid "Something went wrong"
msgstr "Er ging iets mis"

msgid "` + partialerrors.SafeMessage + `"
msgstr "Probeer het later opnieuw."
`,
	})

	translations, err := frontend.Translations(filepath.Join(dir, "translations"))
	if err != nil {
		t.Fatal(err)
	}

	for _, stages := range [][]partial.RenderStage{nil, {flash.Stage(flash.WithTemplate("flash.gohtml"))}} {
		s := sitetest.StartWith(t, sitetest.Options{
			Files: os.DirFS(filepath.Join(dir, "templates")),
			Configure: func(config *webthings.Config) {
				config.Translations = translations
				config.Telemetry.RenderStages = stages
			},
		}, breaker())

		visitor := s.Visitor()
		values := url.Values{"back": {"/nl/"}}

		want := []string{
			`<html lang="nl"><h1>Er ging iets mis · Test site</h1>`,
			`<p class="message">Probeer het later opnieuw.</p>`,
			`<a href="/nl/">home</a>`,
		}

		status, body, _ := visitor.Send("/_/breaker/post", values)
		if status != http.StatusInternalServerError || strings.Contains(body, "disk on fire") {
			t.Fatalf("page (flash stage %v): %d\n%s", stages != nil, status, body)
		}

		for _, part := range want {
			if !strings.Contains(body, part) {
				t.Fatalf("page (flash stage %v): no %s\n%s", stages != nil, part, body)
			}
		}

		status, body, _ = visitor.SendHTMX("/_/breaker/post", values, "content")
		if status != http.StatusInternalServerError || strings.Contains(body, "disk on fire") ||
			strings.Contains(body, `class="flash"`) || !strings.HasPrefix(body, "<!doctype html>") {
			t.Fatalf("HTMX post (flash stage %v): %d\n%s", stages != nil, status, body)
		}

		for _, part := range want {
			if !strings.Contains(body, part) {
				t.Fatalf("HTMX post (flash stage %v): no %s\n%s", stages != nil, part, body)
			}
		}
	}
}

// TestFailureContext checks that a visitor action that fails, outside a
// render, renders the theme's 500.gohtml as every page renders, with the
// theme's asset versions and integrity, and that the log names the
// module of the action. It is not parallel: it reads the default logger.
func TestFailureContext(t *testing.T) {
	files := fstest.MapFS{
		"default/layout.gohtml":   {Data: []byte(`<html>{{content}}</html>`)},
		"default/page.gohtml":     {Data: []byte(`<main id="content">{{.Title}}</main>`)},
		"default/assets/site.css": {Data: []byte(`body{color:black}`)},
		"default/500.gohtml": {
			Data: []byte(`<!doctype html><html><head>{{assetStyle "site.css"}}</head><p>{{.Message}}</p></html>`),
		},
	}

	s := sitetest.StartWith(t, sitetest.Options{Files: files}, breaker())

	var logs syncBuffer

	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))

	status, body, _ := s.Visitor().Send("/_/breaker/post", url.Values{"back": {"/en/"}})

	slog.SetDefault(previous)

	want := regexp.MustCompile(`<link rel="stylesheet" href="/theme/default/site\.css\?v=[0-9a-f]+" integrity="` +
		regexp.QuoteMeta(assets.Integrity([]byte(`body{color:black}`))) + `" crossorigin="anonymous">`)
	if status != http.StatusInternalServerError || !want.MatchString(body) {
		t.Fatalf("error page: %d, want %s in\n%s", status, want, body)
	}

	if !strings.Contains(logs.String(), "module=breaker") || !strings.Contains(logs.String(), "disk on fire") {
		t.Fatalf("failure not logged with its module:\n%s", logs.String())
	}
}

// TestPlainFailurePage checks that a theme without a 500.gohtml answers a
// failure with go-partial's built-in error page in the page's language:
// its message and status heading translated with the site's translations
// and its lang, for a visitor action and for a template that fails, without
// the error.
func TestPlainFailurePage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"templates/layout.gohtml": `<html>{{content}}</html>`,
		"templates/page.gohtml":   `<main id="content">{{if .Body}}{{.Missing.Field}}{{end}}{{.Title}}</main>`,
		"translations/nl.po": `msgid ""
msgstr ""
"Language: nl\n"
"Content-Type: text/plain; charset=UTF-8\n"

msgid "` + partialerrors.SafeMessage + `"
msgstr "Probeer het later opnieuw."

msgid "Internal Server Error"
msgstr "Interne serverfout"
`,
	})

	translations, err := frontend.Translations(filepath.Join(dir, "translations"))
	if err != nil {
		t.Fatal(err)
	}

	s := sitetest.StartWith(t, sitetest.Options{
		Files:     os.DirFS(filepath.Join(dir, "templates")),
		Configure: func(config *webthings.Config) { config.Translations = translations },
	}, breaker())

	visitor := s.Visitor()

	status, body, _ := visitor.Send("/_/breaker/post", url.Values{"back": {"/nl/"}})
	if status != http.StatusInternalServerError || strings.Contains(body, "disk on fire") ||
		!strings.Contains(body, `<html lang="nl">`) || !strings.Contains(body, "<p>Probeer het later opnieuw.</p>") ||
		!strings.Contains(body, "<title>Interne serverfout</title>") || !strings.Contains(body, "<h1>Interne serverfout</h1>") {
		t.Fatalf("action: %d\n%s", status, body)
	}

	status, body, _ = visitor.Send("/_/breaker/post", url.Values{"back": {"/en/"}})
	if status != http.StatusInternalServerError || !strings.Contains(body, `<html lang="en">`) ||
		!strings.Contains(body, partialerrors.SafeMessage) || !strings.Contains(body, "<h1>Internal Server Error</h1>") {
		t.Fatalf("action in English: %d\n%s", status, body)
	}

	// A page whose template fails.
	home, err := s.App.Pages.Home(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	broken := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title": {"Broken"}, "Slug": {"broken"}, "Parent": {home}, "Body": {"Text"},
	})
	s.Submit(broken+"?locale=nl", url.Values{
		"Title": {"Kapot"}, "Slug": {"kapot"}, "Parent": {home}, "Body": {"Tekst"},
	})
	s.Publish(broken + "?locale=en")

	status, body = visitor.Get("/nl/kapot")
	if status != http.StatusInternalServerError || !strings.Contains(body, `<html lang="nl">`) ||
		!strings.Contains(body, "<p>Probeer het later opnieuw.</p>") || !strings.Contains(body, "<h1>Interne serverfout</h1>") ||
		strings.Contains(body, "page.gohtml") {
		t.Fatalf("failing template: %d\n%s", status, body)
	}
}
