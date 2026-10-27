package testsuite

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/modules/comments"
	"github.com/donseba/go-webthings/sitetest"

	"github.com/donseba/go-importmap/assets"
)

// themeFiles is two themes, default and dark, and a folder that is no theme.
func themeFiles() fstest.MapFS {
	file := func(source string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(source)}
	}

	return fstest.MapFS{
		"default/theme.json": file(`{
			"title": "Plain",
			"settings": [
				{"name": "accent", "label": {"en": "Accent", "nl": "Accentkleur"}, "type": "color", "default": "#336699"},
				{"name": "headings", "label": "Headings", "type": "select", "options": ["serif", "sans"], "default": "sans"}
			]
		}`),
		"default/layout.gohtml": file(`<html><link href="{{asset "site.css"}}"><body data-theme="{{.Theme.Name}}" ` +
			`style="--accent:{{.Theme.Values.accent}}">{{content}}</body></html>`),
		"default/page.gohtml":     file(`<main id="content"><h1>{{.Title}}</h1><p>{{theme "headings"}}</p></main>`),
		"default/assets/site.css": file(`body{color:black}`),
		"default/assets/logo.svg": file(`<svg/>`),
		"dark/theme.json":         file(`{"title": "Dark"}`),
		"dark/layout.gohtml":      file(`<html><link href="{{asset "site.css"}}"><body class="dark">{{content}}</body></html>`),
		"dark/assets/site.css":    file(`body{color:white}`),
		// Not a theme: no layout and no theme.json.
		"notes/readme.txt": file(`notes`),
	}
}

// TestThemeAssetTags checks that assetStyle and assetScript write the tags
// of the theme's files, and of core's webthings.css, with their versioned
// URLs and Subresource Integrity, and a file the theme lacks without.
func TestThemeAssetTags(t *testing.T) {
	t.Parallel()

	files := themeFiles()
	files["default/layout.gohtml"] = &fstest.MapFile{Data: []byte(`<html><head>{{assetStyle "webthings.css"}}` +
		`{{assetStyle "site.css" "media" "screen"}}{{assetScript "site.js" "type" "module"}}{{assetStyle "missing.css"}}` +
		`</head><body>{{content}}</body></html>`)}
	files["default/assets/site.js"] = &fstest.MapFile{Data: []byte(`console.log(1);`)}

	s := sitetest.StartWith(t, sitetest.Options{Files: files})

	_, home := s.Get("/en/")

	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`<link rel="stylesheet" href="/_/webthings\.css\?v=[0-9a-f]+" integrity="sha384-[^"]+" crossorigin="anonymous">`),
		regexp.MustCompile(`<link rel="stylesheet" href="/theme/default/site\.css\?v=[0-9a-f]+" integrity="` +
			regexp.QuoteMeta(assets.Integrity([]byte(`body{color:black}`))) + `" crossorigin="anonymous" media="screen">`),
		regexp.MustCompile(`<script src="/theme/default/site\.js\?v=[0-9a-f]+" integrity="` +
			regexp.QuoteMeta(assets.Integrity([]byte(`console.log(1);`))) + `" crossorigin="anonymous" type="module"></script>`),
		regexp.MustCompile(`<link rel="stylesheet" href="/theme/default/missing\.css">`),
	} {
		if !want.MatchString(home) {
			t.Fatalf("want %s in\n%s", want, home)
		}
	}
}

func TestThemes(t *testing.T) {
	t.Parallel()

	s := sitetest.StartWith(t, sitetest.Options{Files: themeFiles()})

	// The files have no modification time: the version is a hash of the
	// content.
	s.Expect("/en/", http.StatusOK, `data-theme="default"`, `--accent:#336699`, `<p>sans</p>`,
		`href="/theme/default/site.css?v=`)
	s.Expect("/theme/site.css", http.StatusOK, `color:black`)
	s.Expect("/theme/dark/site.css", http.StatusOK, `color:white`)
	s.Expect("/theme/dark/logo.svg", http.StatusOK, `<svg/>`)

	s.Expect("/admin/theme", http.StatusOK, "Plain", "Dark", `type="color"`, `value="#336699"`)

	// Settings are validated and stored per theme.
	status, body := s.PostStatus("/admin/theme", "/admin/theme/values", url.Values{
		"theme":          {"default"},
		"value.accent":   {"red"},
		"value.headings": {"serif"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "use a colour like #1a2b3c") ||
		!strings.Contains(body, `aria-invalid="true"`) || !strings.Contains(body, `value="red"`) {
		t.Fatalf("invalid colour: %d\n%s", status, body)
	}

	status, _ = s.PostStatus("/admin/theme", "/admin/theme/values", url.Values{
		"theme":          {"default"},
		"value.accent":   {"#AA0000"},
		"value.headings": {"cursive"},
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown option: %d", status)
	}

	s.Post("/admin/theme", "/admin/theme/values", url.Values{
		"theme":          {"default"},
		"value.accent":   {"#AA0000"},
		"value.headings": {"serif"},
	})
	s.Expect("/en/", http.StatusOK, `--accent:#aa0000`, `<p>serif</p>`)

	// Saving the general site settings keeps the theme's values.
	s.Post("/admin/site", "/admin/site?locale=en", url.Values{
		"DefaultLocale": {"en"},
		"Locales":       {"en", "nl"},
		"Timezone":      {"Europe/Amsterdam"},
	})
	s.Expect("/en/", http.StatusOK, `--accent:#aa0000`)

	// A preview shows another theme before it is used.
	s.Expect("/admin/theme/preview/dark", http.StatusOK, `class="dark"`, `href="/theme/dark/site.css?v=`)
	s.Expect("/en/", http.StatusOK, `data-theme="default"`)

	s.Post("/admin/theme", "/admin/theme/activate", url.Values{"theme": {"dark"}})
	s.Expect("/en/", http.StatusOK, `class="dark"`, `<p></p>`)
	s.Expect("/theme/site.css", http.StatusOK, `color:white`)
	s.Expect("/theme/logo.svg", http.StatusOK, `<svg/>`)

	if status, _ := s.PostStatus(
		"/admin/theme",
		"/admin/theme/activate",
		url.Values{"theme": {"nope"}},
	); status != http.StatusBadRequest {
		t.Fatalf("unknown theme: %d", status)
	}
}

// TestThemeAssetCaching checks that theme assets are linked by the version of
// their content and served with a strong ETag: cached for long at their
// current version, revalidated otherwise.
func TestThemeAssetCaching(t *testing.T) {
	t.Parallel()

	s := sitetest.StartWith(t, sitetest.Options{Files: themeFiles()})

	_, home := s.Response("/en/")

	link := regexp.MustCompile(`href="(/theme/default/site\.css\?v=[0-9a-f]{16})"`).FindStringSubmatch(home)
	if link == nil {
		t.Fatalf("site.css is not linked by its version:\n%s", home)
	}

	current := request(t, s.App, http.MethodGet, link[1])
	if current.Code != http.StatusOK || current.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		current.Body.String() != `body{color:black}` {
		t.Fatalf("current version: %d %v %s", current.Code, current.Header(), current.Body)
	}

	etag := current.Header().Get("ETag")

	revalidated := request(t, s.App, http.MethodGet, "/theme/site.css", "If-None-Match", etag)
	if revalidated.Code != http.StatusNotModified {
		t.Fatalf("revalidation: %d", revalidated.Code)
	}

	old := request(t, s.App, http.MethodGet, "/theme/default/site.css?v=old")
	if old.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("an old version is cached: %v", old.Header())
	}

	for _, missing := range []string{"/theme/default/missing.css", "/theme/default/", "/theme/dark/"} {
		if response := request(t, s.App, http.MethodGet, missing); response.Code != http.StatusNotFound {
			t.Errorf("%s: %d", missing, response.Code)
		}
	}
}

// TestThemePrecedence checks that components and e-mail follow the order of
// module screens: every candidate is looked up in the active theme before
// the default theme, so the theme's template by module type replaces the
// default theme's by instance key, and the default theme's replaces the
// module's own.
func TestThemePrecedence(t *testing.T) {
	t.Parallel()

	file := func(source string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(source)}
	}

	files := fstest.MapFS{
		"default/layout.gohtml": file(`<html><body>{{content}}</body></html>`),
		"default/page.gohtml":   file(`<main id="content">{{commentThread "remarks" "home"}}</main>`),
		// By instance key, in the default theme.
		"default/remarks/form.gohtml":           file(`<p class="default-key">{{.Subject}}</p>`),
		"default/mail/box/confirmation.gohtml":  file(`{{define "subject"}}Default key{{end}}{{define "text"}}{{.Name}}{{end}}`),
		"default/mail/desk/confirmation.gohtml": file(`{{define "subject"}}Default desk{{end}}{{define "text"}}{{.Name}}{{end}}`),
		"dark/layout.gohtml":                    file(`<html><body class="dark">{{content}}</body></html>`),
		"dark/comments/form.gohtml":             file(`<p class="dark-type">{{.Subject}}</p>`),
		"dark/mail/tickets/confirmation.gohtml": file(`{{define "subject"}}Dark type{{end}}{{define "text"}}{{.Name}}{{end}}`),
		"dark/mail/desk/confirmation.gohtml":    file(`{{define "subject"}}Dark desk{{end}}{{define "text"}}{{.Name}}{{end}}`),
		"default/assets/site.css":               file(`body{}`),
		"dark/assets/site.css":                  file(`body{}`),
	}

	var env webthings.Env

	tickets := func(key string) webthings.Module {
		return webthings.Module{
			Type: "tickets",
			Key:  key,
			Mail: []frontend.Template{{Name: "confirmation", Data: ticket{}, Example: ticketMail}},
			Setup: func(e webthings.Env) (webthings.Parts, error) {
				env = e

				return webthings.Parts{}, nil
			},
		}
	}

	s := sitetest.StartWith(t, sitetest.Options{Files: files},
		comments.Module("remarks", "Comments"),
		tickets("box"),
		tickets("desk"),
	)

	ctx := context.Background()
	data := ticket{Name: "Ada"}

	// The default theme: its own templates by key replace the module's.
	s.Expect("/en/", http.StatusOK, `<p class="default-key">home</p>`)

	if message, err := env.Frontend.Mail(ctx, "en", "box", "confirmation", data); err != nil || message.Subject != "Default key" {
		t.Fatalf("default theme's mail: %+v %v", message, err)
	}

	// The dark theme: its templates by type replace the default theme's by
	// key; by key, they replace its own by type.
	s.Post("/admin/theme", "/admin/theme/activate", url.Values{"theme": {"dark"}})
	s.Expect("/en/", http.StatusOK, `class="dark"`, `<p class="dark-type">home</p>`)

	if message, err := env.Frontend.Mail(ctx, "en", "box", "confirmation", data); err != nil || message.Subject != "Dark type" {
		t.Fatalf("dark theme's mail by type: %+v %v", message, err)
	}

	if message, err := env.Frontend.Mail(ctx, "en", "desk", "confirmation", data); err != nil || message.Subject != "Dark desk" {
		t.Fatalf("dark theme's mail by key: %+v %v", message, err)
	}
}

// TestThemePreviewPath checks that a theme's preview shows the page that
// ?path= names, with its query, also when the path is escaped, and the home
// page for a path that is not on the site.
func TestThemePreviewPath(t *testing.T) {
	t.Parallel()

	files := themeFiles()
	files["default/page.gohtml"] = &fstest.MapFile{
		Data: []byte(`<main id="content"><h1>{{.Title}}</h1><p class="query">{{(request).URL.RawQuery}}</p></main>`),
	}

	s := sitetest.StartWith(t, sitetest.Options{Files: files})

	home, err := s.App.Pages.Home(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	editor := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About"}, "Slug": {"about"}, "Parent": {home}})
	s.Publish(editor + "?locale=en")

	preview := func(path string) string {
		return "/admin/theme/preview/dark?" + url.Values{"path": {path}}.Encode()
	}

	s.Expect(preview("/en/about?x=1"), http.StatusOK, `class="dark"`, `<h1>About</h1>`, `<p class="query">x=1</p>`)
	s.Expect(preview("/en/%61bout"), http.StatusOK, `class="dark"`, `<h1>About</h1>`, `<p class="query"></p>`)
	s.Expect(preview("//elsewhere.example/en/about"), http.StatusOK, `class="dark"`, `<p class="query"></p>`)
}
