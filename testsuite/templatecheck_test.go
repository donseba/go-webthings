package testsuite

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/modules/newsletter"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

// problemLog keeps the "problem" of every record logged at WARN, such as
// those of the template check and of template mistakes, and the
// "generate" of a missing template.
type problemLog struct {
	mu       sync.Mutex
	problems []string
}

func (l *problemLog) Enabled(context.Context, slog.Level) bool {
	return true
}

func (l *problemLog) Handle(_ context.Context, record slog.Record) error {
	if record.Level != slog.LevelWarn {
		return nil
	}

	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "problem" || attr.Key == "generate" {
			l.mu.Lock()
			l.problems = append(l.problems, attr.Value.String())
			l.mu.Unlock()
		}

		return true
	})

	return nil
}

func (l *problemLog) WithAttrs([]slog.Attr) slog.Handler {
	return l
}

func (l *problemLog) WithGroup(string) slog.Handler {
	return l
}

// count is how often problem was logged.
func (l *problemLog) count(problem string) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	count := 0

	for _, logged := range l.problems {
		if logged == problem {
			count++
		}
	}

	return count
}

// mistakes is how many template mistakes were logged.
func (l *problemLog) mistakes() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	count := 0

	for _, logged := range l.problems {
		if !strings.HasPrefix(logged, "task templates -- ") {
			count++
		}
	}

	return count
}

func (l *problemLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return strings.Join(l.problems, "\n")
}

// logProblems makes the default logger keep the problems logged at WARN
// until the test ends.
func logProblems(t *testing.T) *problemLog {
	t.Helper()

	logged := &problemLog{}
	previous := slog.Default()
	slog.SetDefault(slog.New(logged))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return logged
}

// startTemplates starts an app with templates files and modules, in
// development or not, and returns the error of Start.
func startTemplates(t *testing.T, files fstest.MapFS, development bool, modules ...webthings.Module) error {
	t.Helper()

	app := webthings.New(webthings.Config{
		StateDir:    t.TempDir(),
		Templates:   files,
		Insecure:    true,
		Development: development,
	})

	if err := app.Use(modules...); err != nil {
		t.Fatal(err)
	}

	err := app.Start(context.Background())
	if err == nil {
		t.Cleanup(func() { _ = app.Close() })
	}

	return err
}

// checkedThemes is a default theme, with page.gohtml and the layout given,
// and a theme dark of its own page.gohtml.
func checkedThemes(layout, page, darkPage string) fstest.MapFS {
	return fstest.MapFS{
		"default/layout.gohtml": {Data: []byte(layout)},
		"default/page.gohtml":   {Data: []byte(page)},
		"dark/theme.json":       {Data: []byte(`{"title": "Dark"}`)},
		"dark/page.gohtml":      {Data: []byte(darkPage)},
	}
}

// TestTemplateCheckAtStart checks that Start parses every template of every
// theme with the site's functions. A problem a page would fail with is
// logged with the theme's folder in its position and a hint: the module of
// a bundled module's function, or the closest name. In development every
// problem fails Start; otherwise only one that fails every page of the
// active theme, in its layout, regions or partials. It is not parallel: it
// reads the default logger.
func TestTemplateCheckAtStart(t *testing.T) {
	layout := "<html>{{content}}</html>"
	page := `<main id="content">{{absUrl "/"}}</main>`
	pageProblem := `default/page.gohtml:1:22: function "absUrl" not defined; did you mean "absURL"?`
	darkProblem := `dark/page.gohtml:1:6: function "pollBox" not defined: it comes from the polls module; add it to app.Use or remove the call`

	t.Run("a page warns", func(t *testing.T) {
		logged := logProblems(t)

		if err := startTemplates(t, checkedThemes(layout, page, `<p>{{pollBox "poll"}}</p>`), false); err != nil {
			t.Fatal(err)
		}

		for _, problem := range []string{pageProblem, darkProblem} {
			if logged.count(problem) != 1 {
				t.Errorf("not logged once: %s\nlogged:\n%s", problem, logged)
			}
		}
	})

	// A module screen the theme has no template of names the command
	// that writes its starting point.
	t.Run("a missing screen names the command", func(t *testing.T) {
		logged := logProblems(t)

		if err := startTemplates(t, checkedThemes(layout, `<main id="content"></main>`, `<main id="content"></main>`), false,
			publishing.Module("news", "News")); err != nil {
			t.Fatal(err)
		}

		if logged.count("task templates -- news") == 0 {
			t.Fatalf("no command for news:\n%s", logged)
		}
	})

	t.Run("development fails with every problem", func(t *testing.T) {
		logProblems(t)

		err := startTemplates(t, checkedThemes(layout, page, `<p>{{pollBox "poll"}}</p>`), true)
		if !errors.Is(err, webthings.ErrTemplates) || !strings.Contains(err.Error(), pageProblem) ||
			!strings.Contains(err.Error(), darkProblem) {
			t.Fatalf("Start in development: %v", err)
		}
	})

	t.Run("the active theme's layout fails", func(t *testing.T) {
		logged := logProblems(t)

		broken := "<html>{{content}}\n  {{newsletterForm \"letter\"}}</html>"
		problem := `default/layout.gohtml:2:5: function "newsletterForm" not defined: it comes from the newsletter module; add it to app.Use or remove the call`

		// Its regions and partials are parsed with every page too.
		files := checkedThemes(broken, `<main id="content"></main>`, `<main id="content"></main>`)
		files["default/regions/menu.gohtml"] = &fstest.MapFile{Data: []byte(`<nav>{{menuItems}}</nav>`)}
		files["default/partials/card.gohtml"] = &fstest.MapFile{Data: []byte(`{{range .}}`)}

		err := startTemplates(t, files, false)
		if !errors.Is(err, webthings.ErrTemplates) || !strings.Contains(err.Error(), problem) ||
			!strings.Contains(err.Error(), "every page of the active theme default would fail") ||
			!strings.Contains(err.Error(), `default/regions/menu.gohtml:1:8: function "menuItems" not defined`) ||
			!strings.Contains(err.Error(), "default/partials/card.gohtml:1: unexpected EOF") {
			t.Fatalf("Start: %v", err)
		}

		// Dark has the default theme's layout: the problem is one.
		if logged.count(problem) != 1 {
			t.Fatalf("not logged once: %s\nlogged:\n%s", problem, logged)
		}
	})

	t.Run("another theme's partial warns", func(t *testing.T) {
		logged := logProblems(t)

		files := checkedThemes(layout, `<main id="content"></main>`, `<main id="content"></main>`)
		files["dark/partials/card.gohtml"] = &fstest.MapFile{Data: []byte(`{{if .}}`)}
		problem := `dark/partials/card.gohtml:1: unexpected EOF: an {{if}}, {{range}}, {{with}}, {{block}} or {{define}} lacks its {{end}}`

		if err := startTemplates(t, files, false); err != nil {
			t.Fatal(err)
		}

		if logged.count(problem) != 1 {
			t.Fatalf("not logged once: %s\nlogged:\n%s", problem, logged)
		}
	})

	// A {{template}} no file defines fails only where it runs, such as in
	// a branch: it warns, also in the active theme's layout.
	t.Run("an unknown template warns", func(t *testing.T) {
		logged := logProblems(t)

		broken := `<html>{{content}}{{if .Preview}}{{template "partials/nav.gohtml" .}}{{end}}</html>`
		problem := `default/layout.gohtml:1:44: no such template "partials/nav.gohtml"`

		if err := startTemplates(
			t,
			checkedThemes(broken, `<main id="content"></main>`, `<main id="content"></main>`),
			false,
		); err != nil {
			t.Fatal(err)
		}

		if logged.count(problem) != 1 {
			t.Fatalf("not logged once: %s\nlogged:\n%s", problem, logged)
		}
	})

	t.Run("a folder of templates is named", func(t *testing.T) {
		logged := logProblems(t)

		dir := filepath.Join(t.TempDir(), "templates")
		for name, file := range checkedThemes(layout, page, `<main id="content"></main>`) {
			if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(filepath.Join(dir, name), file.Data, 0o600); err != nil {
				t.Fatal(err)
			}
		}

		app := webthings.New(webthings.Config{StateDir: t.TempDir(), Templates: os.DirFS(dir), Insecure: true})
		if err := app.Start(context.Background()); err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = app.Close() })

		if logged.count(filepath.ToSlash(dir)+"/"+pageProblem) != 1 {
			t.Fatalf("no position in %s:\n%s", dir, logged)
		}
	})
}

// TestTemplateMistakes checks that a template call that would show nothing
// is a mistake: an instance key the site has no such module of, an asset
// file neither the theme nor core has, and a setting the theme does not
// have. Each is logged once, with what there is; in development the page
// fails with it. It is not parallel: it reads the default logger.
func TestTemplateMistakes(t *testing.T) {
	files := fstest.MapFS{
		"default/theme.json": {Data: []byte(`{"settings": [
			{"name": "accent", "label": "Accent", "type": "color", "default": "#c2410c"}
		]}`)},
		"default/assets/site.css": {Data: []byte(`body{color:black}`)},
		"default/layout.gohtml":   {Data: []byte(`<html>{{content}}</html>`)},
		"default/page.gohtml": {Data: []byte(`<main id="content">{{.Title}}:` +
			`{{if eq .Title "Key"}}{{range posts "nieuws" 3}}{{.Title}}{{end}}{{end}}` +
			`{{if eq .Title "Asset"}}{{assetStyle "sitee.css"}}{{end}}` +
			`{{if eq .Title "Setting"}}[{{theme "acent"}}]{{end}}` +
			`{{if eq .Title "Letter"}}{{with newsletter "brief"}}{{.Title}}{{end}}{{end}}` +
			`{{if eq .Title "Fine"}}{{range posts "news" 3}}{{end}}{{asset "site.css"}}{{theme "accent"}}{{end}}</main>`)},
	}

	mistakes := map[string]string{
		"key":     `posts: no publishing instance "nieuws" (known: blog, news)`,
		"asset":   `assetStyle: no file "sitee.css" in the assets of theme default (closest: site.css, webthings.css, focal-points.css)`,
		"setting": `theme: no setting "acent" in theme default (known: accent)`,
		// The template's name of the function, not its Go name (signup).
		"letter": `newsletter: no newsletter instance "brief" (known: letter)`,
	}

	modules := []webthings.Module{
		publishing.Module("news", "News"),
		publishing.Module("blog", "Blog"),
		newsletter.Module("letter", "Letter"),
	}

	for _, development := range []bool{false, true} {
		logged := logProblems(t)

		s := sitetest.StartWith(t, sitetest.Options{
			Files: files,
			Configure: func(config *webthings.Config) {
				config.Development = development
			},
		}, modules...)

		for _, title := range []string{"Key", "Asset", "Setting", "Letter", "Fine"} {
			s.Mount("", title, strings.ToLower(title))
		}

		for _, slug := range []string{"key", "asset", "setting", "letter", "key", "asset", "setting", "letter"} {
			status, body := s.Visitor().Get("/en/" + slug)

			switch {
			case development && status != http.StatusInternalServerError:
				t.Fatalf("development: /en/%s answers %d\n%s", slug, status, body)
			case !development && status != http.StatusOK:
				t.Fatalf("/en/%s answers %d\n%s", slug, status, body)
			}
		}

		if status, body := s.Visitor().Get("/en/fine"); status != http.StatusOK {
			t.Fatalf("development %v: /en/fine answers %d\n%s", development, status, body)
		}

		for slug, mistake := range mistakes {
			if logged.count(mistake) != 1 {
				t.Errorf("development %v, %s: not logged once: %s\nlogged:\n%s", development, slug, mistake, logged)
			}
		}

		// The calls that find what they ask for are no mistakes.
		if logged.mistakes() != len(mistakes) {
			t.Errorf("development %v: more is logged:\n%s", development, logged)
		}

		if !development {
			continue
		}

		// The error page shows the mistake.
		_, body := s.Visitor().Get("/en/key")
		if !strings.Contains(body, "no publishing instance") || !strings.Contains(body, "known: blog, news") {
			t.Fatalf("the error page does not show the mistake:\n%s", body)
		}
	}
}
