package testsuite

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/forms"
	"github.com/donseba/go-webthings/modules/gallery"
	"github.com/donseba/go-webthings/modules/lists"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/modules/testimonials"
	"github.com/donseba/go-webthings/sitetest"
)

// TestGeneratedTemplates writes the starting points for the site and every
// module into an empty folder and checks a site runs on them.
func TestGeneratedTemplates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	keys := []string{"site", "news", "agenda", "photos", "faq", "reviews", "contact"}

	s := sitetest.StartWith(t, sitetest.Options{
		Templates: dir,
		Prepare: func(app *webthings.App) error {
			for _, key := range keys {
				if _, err := app.WriteTemplates(dir, key); err != nil {
					return err
				}
			}

			// A second Publishing instance shares the "publishing" folder,
			// except for its own post template.
			if err := os.MkdirAll(filepath.Join(dir, "publishing"), 0o755); err != nil {
				return err
			}

			list, err := os.ReadFile(filepath.Join(dir, "news", "list.gohtml"))
			if err != nil {
				return err
			}

			if err := os.WriteFile(filepath.Join(dir, "publishing", "list.gohtml"), list, 0o644); err != nil {
				return err
			}

			blog := filepath.Join(dir, "blog", "post.gohtml")
			if err := os.MkdirAll(filepath.Dir(blog), 0o755); err != nil {
				return err
			}

			return os.WriteFile(blog, []byte(`<main id="content">blog design: {{.Title}}</main>`), 0o644)
		},
	},
		publishing.Module("news", "News"),
		publishing.Module("blog", "Blog"),
		events.Module("agenda", "Agenda"),
		gallery.Module("photos", "Photos"),
		lists.Module("faq", "FAQ"),
		testimonials.Module("reviews", "Reviews"),
		forms.Module("contact", "Contact"),
	)

	news, err := os.ReadFile(filepath.Join(dir, "news", "post.gohtml"))
	if err != nil || !strings.HasPrefix(string(news), "{{/* @dot github.com/donseba/go-webthings/modules/publishing.Post */}}") {
		t.Fatalf("news/post.gohtml: %v\n%s", err, news)
	}

	for _, module := range [][2]string{
		{"news", "news"},
		{"blog", "blog"},
		{"agenda", "agenda"},
		{"photos", "photos"},
		{"faq", "faq"},
		{"reviews", "reviews"},
		{"contact", "contact"},
	} {
		s.Mount(module[0], module[1], module[1])
	}

	for _, key := range []string{"news", "blog"} {
		post := s.Submit("/admin/"+key+"/new?locale=en", url.Values{
			"Title": {"Hello " + key},
			"Slug":  {"hello"},
			"Date":  {"2026-10-01"},
			"Body":  {"Some *text*"},
		})
		s.Publish(post + "?locale=en")
	}

	// The layout links the stylesheet written with it.
	s.Expect("/en/", http.StatusOK, "<h1>Home</h1>", "/site.css?v=")
	s.Expect("/en/news", http.StatusOK, `href="/en/news/hello"`)
	s.Expect("/en/news/hello", http.StatusOK, "Hello news", "<em>text</em>", ">1 October 2026</time>")
	s.Expect("/en/blog", http.StatusOK, `href="/en/blog/hello"`)
	s.Expect("/en/blog/hello", http.StatusOK, "blog design: Hello blog")
	s.Expect("/en/search?q=hello", http.StatusOK, `<a href="/en/news/hello">Hello news</a>`, `action="/en/search"`)
	// The event is upcoming whenever the test runs.
	day := time.Now().AddDate(0, 2, 0)
	event := s.Submit("/admin/agenda/new?locale=en", url.Values{
		"Title":    {"Fair"},
		"Slug":     {"fair"},
		"Start":    {day.Format(time.DateOnly) + "T14:30"},
		"End":      {day.Format(time.DateOnly) + "T16:00"},
		"Timezone": {"Europe/Amsterdam"},
	})
	s.Publish(event + "?locale=en")

	// An event on one day: its day and its times, in one line.
	s.Expect("/en/agenda", http.StatusOK, ">"+day.Format("Monday 2 January 2006")+" · 14:30–16:00</time>")
	s.Expect("/en/agenda/fair", http.StatusOK, "· 14:30–16:00</time> (Europe/Amsterdam)")
	s.Expect("/en/photos", http.StatusOK)
	s.Expect("/en/faq", http.StatusOK)
	s.Expect("/en/reviews", http.StatusOK)
	s.Expect("/en/nope", http.StatusNotFound)

	// Existing templates are never overwritten.
	written, err := s.App.WriteTemplates(dir, "news")
	if err != nil || len(written) != 0 {
		t.Fatalf("rewrote %v: %v", written, err)
	}

	// A site's command line can list the keys with starting points, and an
	// unknown key names them.
	listed := strings.Join(s.App.TemplateKeys(), " ")
	if listed != "site news blog agenda photos faq reviews contact" {
		t.Fatalf("template keys: %s", listed)
	}

	if _, err := s.App.WriteTemplates(dir, "nope"); err == nil || !strings.Contains(err.Error(), "site, news, blog") {
		t.Fatalf("unknown key: %v", err)
	}
}
