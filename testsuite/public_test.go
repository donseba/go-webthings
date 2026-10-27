package testsuite

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/modules/comments"
	"github.com/donseba/go-webthings/modules/directory"
	"github.com/donseba/go-webthings/modules/documents"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/forms"
	"github.com/donseba/go-webthings/modules/gallery"
	"github.com/donseba/go-webthings/modules/lists"
	"github.com/donseba/go-webthings/modules/newsletter"
	"github.com/donseba/go-webthings/modules/people"
	"github.com/donseba/go-webthings/modules/polls"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/modules/registrations"
	"github.com/donseba/go-webthings/modules/submissions"
	"github.com/donseba/go-webthings/modules/testimonials"
	"github.com/donseba/go-webthings/render"
	"github.com/donseba/go-webthings/sitetest"

	partialerrors "github.com/donseba/go-partial/ext/errors"
)

const dutchSite = `msgid ""
msgstr ""
"Language: nl\n"
"Content-Type: text/plain; charset=UTF-8\n"

msgid "Not found"
msgstr "Niet gevonden"

msgid "Facebook"
msgstr "Deel op Facebook"

msgid "Share"
msgstr "Delen"
`

// TestPublicSite checks what visitors see of templates, module screens,
// errors, absolute links and images.
func TestPublicSite(t *testing.T) {
	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"templates/layout.gohtml": `<html><head><title>{{.Title}}</title>
<link rel="canonical" href="{{absURL .Path}}">
{{with or .Image .Site.SocialImageURL}}<meta property="og:image" content="{{absURL .}}">{{end}}
</head><body>{{with .Page}}<p id="page">{{.Title}}</p>{{end}}<p id="url">{{with url}}{{.Path}}{{end}}</p>{{content}}</body></html>`,
		"templates/page.gohtml":         `<main id="content"><h1>{{.Title}}</h1><p class="date">{{date .Title}}</p>{{.Body}}</main>`,
		"templates/404.gohtml":          `<main id="content"><h1>{{tl localizer "Not found"}}</h1></main>`,
		"templates/probe/screen.gohtml": `<main id="content"><img class="screen" alt="{{alt .}}"></main>`,
		"templates/probe/broken.gohtml": `<main id="content">{{template "partials/missing.gohtml" .}}</main>`,
		"translations/nl.po":            dutchSite,
	})

	translations, err := frontend.Translations(filepath.Join(dir, "translations"))
	if err != nil {
		t.Fatal(err)
	}

	var asset string

	s := sitetest.StartWith(t, sitetest.Options{
		Files: os.DirFS(filepath.Join(dir, "templates")),
		Configure: func(config *webthings.Config) {
			config.Translations = translations
			config.Funcs = []template.FuncMap{{
				// A function without a context replaces core's date.
				"date": func(value any, _ ...string) string {
					return fmt.Sprintf("site date %v", value)
				},
			}}
		},
	}, screens(&asset))

	ctx := context.Background()

	photo, err := s.App.Media.Upload(ctx, "photo.png", bytes.NewReader(sitetest.PNG(t, 2000, 1000)))
	if err != nil {
		t.Fatal(err)
	}

	asset = photo.ID

	err = s.App.Media.SaveTranslation(ctx, photo.ID, media.Translation{Locale: "en", AltText: "A photo"})
	if err != nil {
		t.Fatal(err)
	}

	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	settings.SocialImageMediaID = photo.ID

	if err := s.App.Site.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}

	s.Mount("probe", "Probe page", "probe")

	// Module screens know their page, url sees the whole path, and links
	// can be absolute.
	response, body := s.Response("/en/probe")
	origin := "http://" + response.Request.URL.Host

	for _, want := range []string{
		`<p id="page">Probe page</p>`,
		`<p id="url">/en/probe</p>`,
		`<link rel="canonical" href="` + origin + `/en/probe">`,
		// The social image is the 1200 × 630 JPEG crop, not the upload.
		`<meta property="og:image" content="` + origin + `/media/v/`,
		photo.ID + `_1200x630c.jpg">`,
		`<img class="screen" alt="A photo">`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in\n%s", want, body)
		}
	}

	// A site function replaces core's date, also in page templates.
	s.Expect("/en/", http.StatusOK, `<p class="date">site date Home</p>`)

	// Unknown paths below a module page get the site's 404 page, with its
	// title in the page's language.
	s.Expect("/en/probe/unknown/deeper", http.StatusNotFound, "<title>Not found</title>", "<h1>Not found</h1>")
	s.Expect("/nl/missing", http.StatusNotFound, "<title>Niet gevonden</title>", "<h1>Niet gevonden</h1>")

	// Modules answer failures with Fail: missing content gets the site's 404
	// page, other errors are logged and shown to visitors without details.
	s.Expect("/en/probe/gone", http.StatusNotFound, "<h1>Not found</h1>")

	var logs syncBuffer

	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))

	status, body := s.Get("/en/probe/failing")

	slog.SetDefault(previous)

	if status != http.StatusInternalServerError || strings.Contains(body, "storage") {
		t.Fatalf("failing module: %d %s", status, body)
	}

	if !strings.Contains(logs.String(), "module=probe") || !strings.Contains(logs.String(), "probe storage is down") {
		t.Fatalf("failure not logged:\n%s", logs.String())
	}

	// A broken template shows visitors no internals: go-partial's plain
	// error page, as the theme has no 500.gohtml.
	status, body = s.Get("/en/probe/broken")
	if status != http.StatusInternalServerError || strings.Contains(body, "missing.gohtml") ||
		!strings.Contains(body, partialerrors.SafeMessage) {
		t.Fatalf("broken template: %d %s", status, body)
	}

	// Canonical redirects keep the query.
	response, _ = s.Response("/en/probe/?x=1&y=2")
	if got := response.Request.URL.RequestURI(); got != "/en/probe?x=1&y=2" {
		t.Fatalf("redirected to %s", got)
	}

	// HTMX requests for another element get the content, retargeted.
	request := httptest.NewRequest(http.MethodGet, "/en/probe", nil)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Target", "div#results")

	recorder := httptest.NewRecorder()
	s.App.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || recorder.Header().Get("HX-Retarget") != "#content" ||
		!strings.Contains(recorder.Body.String(), `<img class="screen"`) || strings.Contains(recorder.Body.String(), "<html") {
		t.Fatalf("HTMX: %d %v %s", recorder.Code, recorder.Header(), recorder.Body.String())
	}

	// Images in content are resized, with the library's alt text.
	home, _ := s.App.Pages.Home(ctx)
	editor := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"Gallery"},
		"Slug":   {"gallery"},
		"Parent": {home},
		"Body":   {"![](media:" + photo.ID + ")"},
	})
	s.Publish(editor + "?locale=en")

	s.Expect(
		"/en/gallery",
		http.StatusOK,
		`<img src="/media/v/`,
		photo.ID+`_1600x0.webp" alt="A photo" srcset="/media/v/`,
		`1600w" sizes="100vw" width="1600" height="800">`,
	)

	// The sitemap lists alternates in the order of the site's languages.
	for range 5 {
		_, sitemap := s.Get("/sitemap.xml")
		en := strings.Index(sitemap, `hreflang="en"`)
		nl := strings.Index(sitemap, `hreflang="nl"`)

		if en < 0 || nl < 0 || en > nl {
			t.Fatalf("sitemap alternates:\n%s", sitemap)
		}
	}

	// Visitors' posts to actions are limited in size.
	if status, _, _ := s.Send("/_/probe/", url.Values{"text": {"short"}}); status != http.StatusNoContent {
		t.Fatalf("small post: %d", status)
	}

	large := strings.Repeat("x", webthings.DefaultActionBodyLimit+1)
	if status, _, _ := s.Send("/_/probe/", url.Values{"text": {large}}); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("large post: %d", status)
	}
}

// TestReservedFunctions checks that a template function named like one of
// go-partial's helpers, or of its locale and slot stages, stops the start
// instead of being ignored.
func TestReservedFunctions(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"url", "content", "locale", "localizer", "slot", "hasSlot"} {
		app := webthings.New(webthings.Config{
			StateDir: t.TempDir(),
			Funcs: []template.FuncMap{{
				name: func() string {
					return ""
				},
			}},
		})
		t.Cleanup(func() { _ = app.Close() })

		if err := app.Start(context.Background()); !errors.Is(err, render.ErrReserved) {
			t.Fatalf("%s: want ErrReserved, got %v", name, err)
		}
	}
}

// engine is a Renderer of another kind: plain html/template, with the
// functions from frontend.TemplateFuncs.
type engine struct{}

func (engine) Has(name string) bool {
	return name == "page"
}

func (engine) Render(w http.ResponseWriter, r *http.Request, _ string, view frontend.View) error {
	page, err := template.New("page").Funcs(frontend.TemplateFuncs(r.Context())).Parse(
		`<html><h1>{{.Title}}</h1>{{boxWidget}}{{shareBar "" .Title}}</html>`,
	)
	if err != nil {
		return err
	}

	var b bytes.Buffer

	if err := page.Execute(&b, view); err != nil {
		return err
	}

	_, err = w.Write(b.Bytes())

	return err
}

// TestOwnRenderer checks that a site's own Renderer gets the modules' and
// site's functions: for its templates, for components and for translated
// labels.
func TestOwnRenderer(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{"nl.po": dutchSite})

	translations, err := frontend.Translations(dir)
	if err != nil {
		t.Fatal(err)
	}

	box := webthings.Module{
		Type: "box",
		Key:  "box",
		Funcs: template.FuncMap{
			"boxWidget": func(ctx context.Context) (template.HTML, error) {
				return frontend.Component(ctx, "box", "widget", "hi")
			},
			"boxLabel": func(text string) string {
				return strings.ToUpper(text)
			},
		},
		Components: []frontend.Template{{Name: "widget", Example: `<b class="box">{{boxLabel .}}</b>`}},
		Setup: func(webthings.Env) (webthings.Parts, error) {
			return webthings.Parts{}, nil
		},
	}

	s := sitetest.StartWith(t, sitetest.Options{
		Configure: func(config *webthings.Config) {
			config.Templates = nil
			config.Renderer = engine{}
			config.Translations = translations
		},
	}, box)

	s.Expect("/nl/", http.StatusOK, `<b class="box">HI</b>`, "Deel op Facebook", `aria-label="Delen"`)
}

// TestModuleNotFound checks that every module answers unknown paths below
// its page with the site's 404 page.
func TestModuleNotFound(t *testing.T) {
	t.Parallel()

	modules := []webthings.Module{
		comments.Module("guestbook", "Guestbook"),
		directory.Module("shops", "Shops"),
		documents.Module("files", "Files"),
		events.Module("agenda", "Agenda"),
		forms.Module("contact", "Contact"),
		gallery.Module("photos", "Photos"),
		lists.Module("faq", "FAQ"),
		newsletter.Module("letter", "Newsletter"),
		people.Module("team", "Team"),
		polls.Module("survey", "Survey"),
		publishing.Module("news", "News"),
		registrations.Module("signup", "Signup"),
		submissions.Module("ideas", "Ideas", "news"),
		testimonials.Module("quotes", "Quotes"),
	}

	s := sitetest.Start(t, modules...)

	for _, module := range modules {
		s.Mount(module.Key, module.Title, module.Key)
		s.Expect("/en/"+module.Key+"/no/such/page", http.StatusNotFound, "<h1>not found</h1>")
	}
}

// TestReloadTemplates checks that templates are parsed once, and read again
// for every request with Config.ReloadTemplates.
func TestReloadTemplates(t *testing.T) {
	t.Parallel()

	for _, reload := range []bool{false, true} {
		dir := t.TempDir()

		writeFiles(t, dir, map[string]string{
			"layout.gohtml": `<html>{{content}}</html>`,
			"page.gohtml":   `<main id="content">first</main>`,
		})

		s := sitetest.StartWith(t, sitetest.Options{
			Files: os.DirFS(dir),
			Configure: func(config *webthings.Config) {
				config.ReloadTemplates = reload
			},
		})

		s.Expect("/en/", http.StatusOK, "first")

		writeFiles(t, dir, map[string]string{"page.gohtml": `<main id="content">second</main>`})

		want := "first"
		if reload {
			want = "second"
		}

		s.Expect("/en/", http.StatusOK, want)
	}
}

// TestThemeErrorPage checks that a theme's 500.gohtml answers a page whose
// template fails, with status 500, the View for @model and without the
// error's details; a theme without one answers go-partial's error markup.
func TestThemeErrorPage(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"layout.gohtml": `<html>{{content}}</html>`,
		"page.gohtml":   `<main id="content">{{.Missing}}</main>`,
		"500.gohtml": `{{/* @model View github.com/donseba/go-webthings/frontend.View */}}` +
			`<!doctype html><h1>{{View.Site.Name}} is having a moment</h1>{{if .Detailed}}{{.Message}}{{end}}`,
	}

	dir := t.TempDir()
	writeFiles(t, dir, files)

	s := sitetest.StartWith(t, sitetest.Options{Files: os.DirFS(dir)})

	status, body := s.Get("/en/")
	if status != http.StatusInternalServerError || !strings.Contains(body, "is having a moment</h1>") ||
		strings.Contains(body, "Missing") {
		t.Fatalf("theme error page: %d\n%s", status, body)
	}

	// A failing not-found page answers the error page with 500, not 404.
	broken := t.TempDir()
	files["404.gohtml"] = `<main id="content">{{.Missing}}</main>`
	writeFiles(t, broken, files)

	s = sitetest.StartWith(t, sitetest.Options{Files: os.DirFS(broken)})

	status, body = s.Get("/en/no-such-page")
	if status != http.StatusInternalServerError || !strings.Contains(body, "is having a moment</h1>") {
		t.Fatalf("failing not-found page: %d\n%s", status, body)
	}

	delete(files, "404.gohtml")

	plain := t.TempDir()
	delete(files, "500.gohtml")
	writeFiles(t, plain, files)

	s = sitetest.StartWith(t, sitetest.Options{Files: os.DirFS(plain)})

	// go-partial's own error markup, without the error's details.
	status, body = s.Get("/en/")
	if status != http.StatusInternalServerError || !strings.Contains(body, partialerrors.SafeMessage) ||
		strings.Contains(body, "Missing") {
		t.Fatalf("plain error: %d\n%s", status, body)
	}
}

// TestThemeErrorPageStructuredData checks that a theme's 500.gohtml that
// writes {{structuredData}} writes none for a page whose template fails:
// not the WebSite, Organization and WebPage of the page that failed.
func TestThemeErrorPageStructuredData(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"layout.gohtml": `<html><head>{{structuredData}}</head>{{content}}</html>`,
		"page.gohtml":   `<main id="content">{{.Missing}}</main>`,
		"500.gohtml":    `<!doctype html><html><head>{{structuredData}}</head><h1>Something broke</h1></html>`,
	}

	dir := t.TempDir()
	writeFiles(t, dir, files)

	s := sitetest.StartWith(t, sitetest.Options{Files: os.DirFS(dir)})

	status, body := s.Get("/en/")
	if status != http.StatusInternalServerError || !strings.Contains(body, "Something broke") {
		t.Fatalf("theme error page: %d\n%s", status, body)
	}

	if strings.Contains(body, "application/ld+json") {
		t.Fatalf("theme error page writes structured data:\n%s", body)
	}

	// The same theme writes it for a page that renders.
	working := t.TempDir()
	files["page.gohtml"] = `<main id="content">fine</main>`
	writeFiles(t, working, files)

	s = sitetest.StartWith(t, sitetest.Options{Files: os.DirFS(working)})

	status, body = s.Get("/en/")
	if status != http.StatusOK || !strings.Contains(body, "application/ld+json") {
		t.Fatalf("working page: %d\n%s", status, body)
	}
}

// TestThemeErrorPageMessage checks that the .Message of a theme's
// 500.gohtml arrives in the page's language, with the site's translations.
func TestThemeErrorPageMessage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"templates/layout.gohtml": `<html>{{content}}</html>`,
		"templates/page.gohtml":   `<main id="content">{{.Missing}}</main>`,
		"templates/500.gohtml":    `<!doctype html><p class="message">{{.Message}}</p>`,
		"translations/nl.po": `msgid ""
msgstr ""
"Language: nl\n"
"Content-Type: text/plain; charset=UTF-8\n"

msgid "` + partialerrors.SafeMessage + `"
msgstr "Er ging iets mis. Probeer het opnieuw."
`,
	})

	translations, err := frontend.Translations(filepath.Join(dir, "translations"))
	if err != nil {
		t.Fatal(err)
	}

	s := sitetest.StartWith(t, sitetest.Options{
		Files: os.DirFS(filepath.Join(dir, "templates")),
		Configure: func(config *webthings.Config) {
			config.Translations = translations
		},
	})

	status, body := s.Get("/nl/")
	if status != http.StatusInternalServerError ||
		!strings.Contains(body, `<p class="message">Er ging iets mis. Probeer het opnieuw.</p>`) {
		t.Fatalf("Dutch error page: %d\n%s", status, body)
	}

	status, body = s.Get("/en/")
	if status != http.StatusInternalServerError ||
		!strings.Contains(body, `<p class="message">`+partialerrors.SafeMessage+`</p>`) {
		t.Fatalf("English error page: %d\n%s", status, body)
	}
}
