package testsuite

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/donseba/go-webthings/sitetest"

	"github.com/donseba/go-importmap/assets"
)

// themeStyleFiles are two themes whose colour and select settings are
// custom properties of their values stylesheet, and a third without any.
func themeStyleFiles() fstest.MapFS {
	file := func(source string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(source)}
	}

	layout := `<html><head>{{assetStyle "site.css"}}{{themeStyle "media" "screen"}}</head>` +
		`<body data-theme="{{.Theme.Name}}">{{content}}</body></html>`

	return fstest.MapFS{
		"default/theme.json": file(`{
			"settings": [
				{"name": "accent", "label": "Accent", "type": "color", "default": "#336699", "css": "--accent"},
				{"name": "headings", "label": "Headings", "type": "select", "options": ["serif", "sans"], "default": "sans", "css": "--headings"},
				{"name": "outline", "label": "Outline", "type": "color"},
				{"name": "notice", "label": "Notice", "type": "text", "default": "Hello"}
			]
		}`),
		"default/layout.gohtml":   file(layout),
		"default/page.gohtml":     file(`<main id="content"><h1>{{.Title}}</h1></main>`),
		"default/assets/site.css": file(`:root{--accent:black}`),
		"dark/theme.json": file(`{"settings": [
			{"name": "accent", "label": "Accent", "type": "color", "default": "#e0a458", "css": "--accent"}
		]}`),
		"plain/theme.json": file(`{"settings": [{"name": "notice", "label": "Notice", "type": "text"}]}`),
	}
}

// themeStyleLink finds the link {{themeStyle}} wrote for theme in page:
// its address, version and integrity.
func themeStyleLink(t *testing.T, page, theme string) (address, version, integrity string) {
	t.Helper()

	link := regexp.MustCompile(`<link rel="stylesheet" href="(/theme/` + theme + `/values\.css\?v=([0-9a-f]{16}))" ` +
		`integrity="(sha384-[^"]+)" crossorigin="anonymous" media="screen">`).FindStringSubmatch(page)
	if link == nil {
		t.Fatalf("no values stylesheet of %s in\n%s", theme, page)
	}

	return link[1], link[2], link[3]
}

// TestThemeStylesheet checks the values stylesheet of a theme: its custom
// properties, versioned address, Subresource Integrity and caching, that it
// follows saved settings and previews, and that values that do not belong
// in CSS never reach it.
func TestThemeStylesheet(t *testing.T) {
	t.Parallel()

	s := sitetest.StartWith(t, sitetest.Options{Files: themeStyleFiles()})
	visitor := s.Visitor()

	_, home := visitor.Response("/en/")
	address, version, integrity := themeStyleLink(t, home, "default")

	// The colour and select settings with "css", by their defaults; a
	// colour without a value and a text setting are left out.
	want := ":root {\n    --accent: #336699;\n    --headings: sans;\n}\n"

	response, css := visitor.Response(address)
	if response.StatusCode != http.StatusOK || css != want ||
		response.Header.Get("Content-Type") != "text/css; charset=utf-8" ||
		response.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		response.Header.Get("ETag") != `"`+version+`"` {
		t.Fatalf("values.css: %d %v\n%q", response.StatusCode, response.Header, css)
	}

	if integrity != assets.Integrity([]byte(css)) {
		t.Fatalf("integrity %s is not that of\n%s", integrity, css)
	}

	// Without its version, or with another, it revalidates; its ETag
	// answers a revalidation.
	if plain, _ := visitor.Response("/theme/default/values.css"); plain.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("without version: %v", plain.Header)
	}

	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("If-None-Match", `"`+version+`"`)

	revalidated := visitor.Request(request)
	_ = revalidated.Body.Close()

	if revalidated.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidation: %d", revalidated.StatusCode)
	}

	// A colour that is no colour, as one that would end the rule, is
	// refused by the admin and the stylesheet stays as it was.
	for _, refused := range []string{"red", "#336699;}body{display:none", "#33669g"} {
		status, _ := s.PostStatus("/admin/theme", "/admin/theme/values", url.Values{
			"theme":          {"default"},
			"value.accent":   {refused},
			"value.headings": {"serif"},
		})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("colour %q: %d", refused, status)
		}
	}

	if status, _ := s.PostStatus("/admin/theme", "/admin/theme/values", url.Values{
		"theme":          {"default"},
		"value.accent":   {"#336699"},
		"value.headings": {"serif;}"},
	}); status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown option: %d", status)
	}

	if _, again := visitor.Response(address); again != want {
		t.Fatalf("refused values changed the stylesheet:\n%s", again)
	}

	// Stored values that are not valid, saved around the admin's checks,
	// give the default.
	if err := s.App.Site.SaveThemeValues(context.Background(), "default", map[string]string{
		"accent":   "#000;}body{x:y",
		"headings": "cursive",
	}); err != nil {
		t.Fatal(err)
	}

	_, home = visitor.Response("/en/")

	if same, _, _ := themeStyleLink(t, home, "default"); same != address {
		t.Fatalf("invalid values changed the version: %s", same)
	}

	// Saving the settings gives a new version with the new values.
	s.Post("/admin/theme", "/admin/theme/values", url.Values{
		"theme":          {"default"},
		"value.accent":   {"#AA0000"},
		"value.headings": {"serif"},
		"value.outline":  {"#00ff00"},
	})

	_, home = visitor.Response("/en/")

	saved, savedVersion, _ := themeStyleLink(t, home, "default")
	if savedVersion == version {
		t.Fatal("saving the values kept the version")
	}

	s.Expect(saved, http.StatusOK, "--accent: #aa0000;", "--headings: serif;")
	s.ExpectMissing(saved, "--outline")

	if old, _ := visitor.Response(address); old.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("an old version is cached: %v", old.Header)
	}

	// A preview links the values of the previewed theme.
	_, preview := s.Response("/admin/theme/preview/dark")
	previewed, _, _ := themeStyleLink(t, preview, "dark")
	s.Expect(previewed, http.StatusOK, "--accent: #e0a458;")

	// The active theme's values stay with it.
	_, home = visitor.Response("/en/")
	if strings.Contains(home, "/theme/dark/values.css") {
		t.Fatalf("the preview changed the site:\n%s", home)
	}

	s.Post("/admin/theme", "/admin/theme/activate", url.Values{"theme": {"dark"}})

	_, home = visitor.Response("/en/")
	themeStyleLink(t, home, "dark")

	// A theme without such settings has no stylesheet and no link; an
	// unknown theme neither.
	s.Post("/admin/theme", "/admin/theme/activate", url.Values{"theme": {"plain"}})

	_, home = visitor.Response("/en/")
	if strings.Contains(home, "values.css") {
		t.Fatalf("a theme without css settings links values:\n%s", home)
	}

	s.Expect("/theme/plain/values.css", http.StatusNotFound)
	s.Expect("/theme/nope/values.css", http.StatusNotFound)

	// The theme's other files are where they were.
	s.Expect("/theme/default/site.css", http.StatusOK, "--accent:black")
}

// TestThemeStylesheetOnErrorPage checks that a theme's 500.gohtml links the
// values stylesheet too, for a page whose template failed.
func TestThemeStylesheetOnErrorPage(t *testing.T) {
	t.Parallel()

	files := themeStyleFiles()
	files["default/page.gohtml"] = &fstest.MapFile{Data: []byte(`<main id="content">{{.Missing}}</main>`)}
	files["default/500.gohtml"] = &fstest.MapFile{
		Data: []byte(`<!doctype html><html><head>{{themeStyle "media" "screen"}}</head>` +
			`<h1>Something broke</h1></html>`),
	}

	s := sitetest.StartWith(t, sitetest.Options{Files: files})

	status, body := s.Visitor().Get("/en/")
	if status != http.StatusInternalServerError || !strings.Contains(body, "Something broke") {
		t.Fatalf("error page: %d\n%s", status, body)
	}

	themeStyleLink(t, body, "default")
}

// TestThemeNumberSetting checks a number setting end to end: its number
// input in the admin, with range, step and unit, the values it refuses on
// saving, and the saved value in the values stylesheet with its unit and
// in templates without it. Steps count exactly and from min, in the form
// as in the theme's own rule: 0.3 is on a step of 0.1, and 0.75 on a step
// of 0.5 from 0.25.
func TestThemeNumberSetting(t *testing.T) {
	t.Parallel()

	file := func(source string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(source)}
	}

	s := sitetest.StartWith(t, sitetest.Options{Files: fstest.MapFS{
		"default/theme.json": file(`{"settings": [
			{"name": "measure", "label": "Text width", "type": "number", "min": 30, "max": 48, "step": 0.5, "unit": "rem", "default": "38", "css": "--measure"},
			{"name": "gap", "type": "number", "min": -0.3, "max": 0.3, "step": 0.1, "unit": "em", "css": "--gap"},
			{"name": "offset", "type": "number", "min": 0.25, "max": 2, "step": 0.5, "css": "--offset"}
		]}`),
		"default/layout.gohtml": file(
			`<html><head>{{themeStyle "media" "screen"}}</head><body>measure:{{theme "measure"}}{{content}}</body></html>`,
		),
		"default/page.gohtml": file(`<main id="content"><h1>{{.Title}}</h1></main>`),
	}})
	visitor := s.Visitor()

	_, form := s.Response("/admin/theme")
	input := regexp.MustCompile(`<input[^>]*name="value\.measure"[^>]*>`).FindString(form)

	for _, want := range []string{`type="number"`, `min="30"`, `max="48"`, `step="0.5"`, `value="38"`} {
		if !strings.Contains(input, want) {
			t.Fatalf("number input without %s: %s\n%s", want, input, form)
		}
	}

	if !strings.Contains(form, "Text width (rem)") {
		t.Fatalf("label without the unit:\n%s", form)
	}

	_, home := visitor.Response("/en/")
	address, _, _ := themeStyleLink(t, home, "default")
	s.Expect(address, http.StatusOK, "--measure: 38rem;")

	for _, refused := range []string{"29.5", "48.5", "42.25", "big", "42rem", "42;}body{display:none"} {
		status, _ := s.PostStatus("/admin/theme", "/admin/theme/values", url.Values{
			"theme":         {"default"},
			"value.measure": {refused},
		})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("number %q: %d", refused, status)
		}
	}

	s.Post("/admin/theme", "/admin/theme/values", url.Values{
		"theme":         {"default"},
		"value.measure": {"42.5"},
	})

	_, home = visitor.Response("/en/")
	if !strings.Contains(home, "measure:42.5") {
		t.Fatalf("template value:\n%s", home)
	}

	saved, _, _ := themeStyleLink(t, home, "default")
	s.Expect(saved, http.StatusOK, "--measure: 42.5rem;")

	status, body := s.PostStatus("/admin/theme", "/admin/theme/values", url.Values{
		"theme":        {"default"},
		"value.offset": {"1"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Enter a value in steps of 0.5 from 0.25.") {
		t.Fatalf("offset between steps: %d\n%s", status, body)
	}

	s.Post("/admin/theme", "/admin/theme/values", url.Values{
		"theme":        {"default"},
		"value.gap":    {"0.3"},
		"value.offset": {"0.75"},
	})

	_, home = visitor.Response("/en/")
	steps, _, _ := themeStyleLink(t, home, "default")
	s.Expect(steps, http.StatusOK, "--gap: 0.3em;")
	s.Expect(steps, http.StatusOK, "--offset: 0.75;")
}
