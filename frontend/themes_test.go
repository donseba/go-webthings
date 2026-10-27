package frontend_test

import (
	"testing"
	"testing/fstest"

	"github.com/donseba/go-webthings/frontend"

	partial "github.com/donseba/go-partial"
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

// TestLoadThemes checks how themes are found, fall back to the default theme
// and validate their settings.
func TestLoadThemes(t *testing.T) {
	t.Parallel()

	themes, err := frontend.LoadThemes(themeFiles())
	if err != nil {
		t.Fatal(err)
	}

	list := themes.List()
	if len(list) != 2 || list[0].Name != "default" || list[1].Name != "dark" || list[1].Title != "Dark" {
		t.Fatalf("themes: %+v", list)
	}

	// The dark theme finds the page template in the default theme.
	dark, _ := themes.Get("dark")
	if _, err := dark.Files.Open("page.gohtml"); err != nil {
		t.Fatal("no fallback to default:", err)
	}

	if fallback, _ := themes.Get("missing"); fallback.Name != "default" {
		t.Fatalf("unknown theme falls back to %q", fallback.Name)
	}

	values := list[0].Values(map[string]string{"accent": "#ABCDEF", "headings": "comic"})
	if values["accent"] != "#abcdef" || values["headings"] != "sans" {
		t.Fatalf("values: %v", values)
	}

	if label := list[0].Settings[0].Label.In("nl"); label != "Accentkleur" {
		t.Fatalf("label: %q", label)
	}

	// A root with layout.gohtml is the only theme.
	single, err := frontend.LoadThemes(fstest.MapFS{"layout.gohtml": &fstest.MapFile{Data: []byte(`{{content}}`)}})
	if err != nil || len(single.List()) != 1 || single.List()[0].Name != "default" {
		t.Fatalf("single theme: %v %+v", err, single.List())
	}

	_, err = frontend.LoadThemes(fstest.MapFS{
		"bad/theme.json": &fstest.MapFile{Data: []byte(`{"settings": [{"name": "x", "type": "colour"}]}`)},
	})
	if err == nil {
		t.Fatal("unknown setting type accepted")
	}
}

// TestTemplateOfTheme checks which template a module screen uses: the
// theme's own, by the module's key and then its type, before the default
// theme's.
func TestTemplateOfTheme(t *testing.T) {
	t.Parallel()

	file := &fstest.MapFile{Data: []byte(`<main id="content"></main>`)}

	themes, err := frontend.LoadThemes(fstest.MapFS{
		"default/layout.gohtml":          file,
		"default/news/post.gohtml":       file,
		"default/publishing/list.gohtml": file,
		"dark/layout.gohtml":             file,
		"dark/publishing/post.gohtml":    file,
	})
	if err != nil {
		t.Fatal(err)
	}

	site := frontend.Themed(nil, nil, nil, themes)
	site.SetModules(map[string]string{"news": "publishing"}, nil)

	for _, test := range []struct{ theme, name, want string }{
		{theme: "default", name: "post", want: "news/post"},
		{theme: "default", name: "list", want: "publishing/list"},
		{theme: "dark", name: "post", want: "publishing/post"},
		{theme: "dark", name: "list", want: "publishing/list"},
		{theme: "dark", name: "missing", want: "publishing/missing"},
	} {
		if got := site.Template(test.theme, "news", test.name); got != test.want {
			t.Fatalf("%s %s: got %s, want %s", test.theme, test.name, got, test.want)
		}
	}
}

// TestTemplateOfOverlaidTemplates checks that templates given as an
// overlay, such as a site's own over a package's, keep the order of their
// layers below core's templates: every candidate in the upper layer before
// the lower one, so the upper layer's <type>/<name> comes before the lower
// layer's <key>/<name>.
func TestTemplateOfOverlaidTemplates(t *testing.T) {
	t.Parallel()

	file := &fstest.MapFile{Data: []byte(`<main id="content"></main>`)}

	upper := fstest.MapFS{"layout.gohtml": file, "publishing/post.gohtml": file}
	lower := fstest.MapFS{"news/post.gohtml": file}

	themes, err := frontend.LoadThemes(partial.Overlay(upper, lower))
	if err != nil {
		t.Fatal(err)
	}

	site := frontend.Themed(nil, nil, nil, themes)
	site.SetModules(map[string]string{"news": "publishing"}, nil)

	if got := site.Template(frontend.DefaultTheme, "news", "post"); got != "publishing/post" {
		t.Fatalf("got %s, want the upper layer's publishing/post", got)
	}
}

// TestThemeSettingCSS checks which settings theme.json may make custom
// properties of the values stylesheet: colours, and selects whose options
// are plain CSS values, each by a property of its own.
func TestThemeSettingCSS(t *testing.T) {
	t.Parallel()

	load := func(settings string) error {
		_, err := frontend.LoadThemes(fstest.MapFS{
			"default/theme.json": &fstest.MapFile{Data: []byte(`{"settings": [` + settings + `]}`)},
		})

		return err
	}

	if err := load(`{"name": "accent", "type": "color", "default": "#336699", "css": "--accent"},
		{"name": "size", "type": "select", "options": ["1rem", "1.25rem", "small caps", "#fff", "50%", "Georgia, serif"], "css": "--size-2"}`); err != nil {
		t.Fatal(err)
	}

	for name, refused := range map[string]string{
		"a text":               `{"name": "notice", "type": "text", "css": "--notice"}`,
		"no custom property":   `{"name": "accent", "type": "color", "css": "color"}`,
		"a property with ;":    `{"name": "accent", "type": "color", "css": "--a;b"}`,
		"capitals":             `{"name": "accent", "type": "color", "css": "--Accent"}`,
		"an option with ;":     `{"name": "x", "type": "select", "options": ["red;}body{display:none"], "css": "--x"}`,
		"an option with }":     `{"name": "x", "type": "select", "options": ["red}"], "css": "--x"}`,
		"an option with url()": `{"name": "x", "type": "select", "options": ["url(x)"], "css": "--x"}`,
		"an option with quote": `{"name": "x", "type": "select", "options": ["\"Georgia\""], "css": "--x"}`,
		"a comment":            `{"name": "x", "type": "select", "options": ["a /* b"], "css": "--x"}`,
		"a property twice": `{"name": "a", "type": "color", "css": "--accent"},
			{"name": "b", "type": "color", "css": "--accent"}`,
	} {
		if err := load(refused); err == nil {
			t.Errorf("%s accepted", name)
		}
	}

	// Without css, a select's options may be anything.
	if err := load(`{"name": "x", "type": "select", "options": ["a;b"]}`); err != nil {
		t.Fatal(err)
	}
}

// TestThemeSettingNumber checks number settings: theme.json's min, max,
// step and unit checked at load, values cleaned to a number in range and
// on a step, counted from min, and a value out of range or between steps
// giving the default in Values.
func TestThemeSettingNumber(t *testing.T) {
	t.Parallel()

	load := func(settings string) (*frontend.Themes, error) {
		return frontend.LoadThemes(fstest.MapFS{
			"default/theme.json": &fstest.MapFile{Data: []byte(`{"settings": [` + settings + `]}`)},
		})
	}

	themes, err := load(
		`{"name": "measure", "type": "number", "min": 30, "max": 48, "step": 0.5, "unit": "rem", "default": "38", "css": "--measure"},
		{"name": "lines", "type": "number", "min": 1, "max": 2, "default": "1.5", "css": "--lines"},
		{"name": "gap", "type": "number", "min": -0.3, "max": 0.3, "step": 0.1, "unit": "em"},
		{"name": "offset", "type": "number", "min": 0.25, "max": 2, "step": 0.5}`,
	)
	if err != nil {
		t.Fatal(err)
	}

	theme, _ := themes.Get("default")
	measure, lines, gap, offset := theme.Settings[0], theme.Settings[1], theme.Settings[2], theme.Settings[3]

	for _, accepted := range []struct {
		setting     frontend.ThemeSetting
		value, want string
	}{
		{measure, "42", "42"},
		{measure, " 42.0 ", "42"},
		{measure, "30", "30"},
		{measure, "47.5", "47.5"},
		{measure, "48", "48"},
		{measure, "", ""},
		{lines, "1.37", "1.37"},
		{gap, "0.1", "0.1"},
		{gap, "-0.3", "-0.3"},
		{gap, "0.2", "0.2"},
		{gap, "0.3", "0.3"},
		{offset, "0.75", "0.75"},
		{offset, "1.75", "1.75"},
	} {
		got, err := accepted.setting.Clean(accepted.value)
		if err != nil || got != accepted.want {
			t.Errorf("%s %q: %q %v", accepted.setting.Name, accepted.value, got, err)
		}
	}

	for _, refused := range []string{"29.5", "48.5", "42.25", "big", "4e1x", "NaN", "Inf", "42rem", "42;}body{display:none"} {
		if got, err := measure.Clean(refused); err == nil {
			t.Errorf("measure %q accepted as %q", refused, got)
		}
	}

	// Steps count from min, which need not be a whole number of steps.
	for _, refused := range []string{"0.5", "1"} {
		if got, err := offset.Clean(refused); err == nil {
			t.Errorf("offset %q accepted as %q", refused, got)
		}
	}

	values := theme.Values(map[string]string{"measure": "99", "lines": "1.25"})
	if values["measure"] != "38" || values["lines"] != "1.25" || values["gap"] != "" {
		t.Fatalf("values: %v", values)
	}

	for name, refused := range map[string]string{
		"no min":                  `{"name": "x", "type": "number", "max": 2}`,
		"no max":                  `{"name": "x", "type": "number", "min": 2}`,
		"min above max":           `{"name": "x", "type": "number", "min": 3, "max": 2}`,
		"a negative step":         `{"name": "x", "type": "number", "min": 0, "max": 2, "step": -1}`,
		"an unknown unit":         `{"name": "x", "type": "number", "min": 0, "max": 2, "unit": "furlong"}`,
		"a unit with ;":           `{"name": "x", "type": "number", "min": 0, "max": 2, "unit": "px;}"}`,
		"a default out of range":  `{"name": "x", "type": "number", "min": 0, "max": 2, "default": "3"}`,
		"a default between steps": `{"name": "x", "type": "number", "min": 0, "max": 2, "step": 1, "default": "0.5"}`,
	} {
		if _, err := load(refused); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// TestThemeCSP checks theme.json's "csp": the sources a theme's pages need
// are kept, and what site.CheckCSPSources refuses stops LoadThemes.
func TestThemeCSP(t *testing.T) {
	t.Parallel()

	load := func(csp string) (*frontend.Themes, error) {
		return frontend.LoadThemes(fstest.MapFS{
			"default/theme.json": &fstest.MapFile{Data: []byte(`{"csp": ` + csp + `}`)},
		})
	}

	themes, err := load(`{"font-src": ["https://fonts.example.com"]}`)
	if err != nil {
		t.Fatal(err)
	}

	if theme, _ := themes.Get(
		"default",
	); len(theme.CSP["font-src"]) != 1 ||
		theme.CSP["font-src"][0] != "https://fonts.example.com" {
		t.Fatalf("csp: %v", theme.CSP)
	}

	for _, refused := range []string{`{"script-src": ["'unsafe-inline'"]}`, `{"default-src": ["https://a.example.com"]}`, `{"img-src": ["*"]}`} {
		if _, err := load(refused); err == nil {
			t.Errorf("%s accepted", refused)
		}
	}
}
