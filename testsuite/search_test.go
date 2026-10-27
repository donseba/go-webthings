package testsuite

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/gallery"
	"github.com/donseba/go-webthings/modules/lists"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

// TestSearch searches pages and every bundled module through the search
// screen and the {{search}} template function.
func TestSearch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// A design where the "results" page lists hits of the template function.
	page := `<main id="content"><h1>{{.Title}}</h1>{{.Body}}` +
		`{{if eq .Title "Results"}}{{range search "CAFÉ" 10}}<i class="hit">{{.Title}}</i>{{end}}{{end}}</main>`

	if err := os.WriteFile(filepath.Join(dir, "page.gohtml"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir},
		publishing.Module("news", "News"),
		events.Module("agenda", "Agenda"),
		gallery.Module("photos", "Photos"),
		lists.Module("faq", "FAQ"),
	)

	home, err := s.App.Pages.Home(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, module := range []string{"news", "agenda", "photos", "faq"} {
		s.Mount(module, strings.ToUpper(module[:1])+module[1:], module)
	}

	hours := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"Opening hours"},
		"Slug":   {"hours"},
		"Parent": {home},
		"Body":   {"Our **café** opens at nine."},
	})
	s.Publish(hours + "?locale=en")

	post := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title": {"New café"},
		"Slug":  {"new-cafe"},
		"Date":  {"2026-10-01"},
		"Body":  {"Coffee and cake."},
	})
	s.Publish(post + "?locale=en")

	event := s.Submit("/admin/agenda/new?locale=en", url.Values{
		"Title":    {"Tasting"},
		"Slug":     {"tasting"},
		"Start":    {time.Now().AddDate(0, 1, 0).Format(time.DateOnly) + "T14:30"},
		"Timezone": {"UTC"},
		"Location": {"The Café"},
	})
	s.Publish(event + "?locale=en")

	album := s.Submit("/admin/photos/new?locale=en", url.Values{
		"Title":       {"Interior"},
		"Slug":        {"interior"},
		"Description": {"Photos of the cafe"},
	})
	s.Publish(album + "?locale=en")

	entry := s.Submit("/admin/faq/new?locale=en", url.Values{
		"Title": {"Is there wifi?"},
		"Body":  {"Yes, in the whole café."},
	})
	s.Publish(entry + "?locale=en")

	// A draft that is never published.
	s.Submit("/admin/news/new?locale=en", url.Values{
		"Title": {"Secret café plans"},
		"Slug":  {"secret"},
		"Date":  {"2026-10-02"},
	})

	visitor := s.Visitor()
	response, body := visitor.Response("/en/search?q=Cafe")

	if response.StatusCode != http.StatusOK || response.Header.Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("search: %d %v\n%s", response.StatusCode, response.Header, body)
	}

	// Every kind of content is found, accents and case ignored; drafts are not.
	for _, want := range []string{
		`class="result page"><a href="/en/hours">Opening hours</a><p>Our café opens at nine.</p>`,
		`class="result publishing"><a href="/en/news/new-cafe">New café</a>`,
		`class="result events"><a href="/en/agenda/tasting">Tasting</a>`,
		`class="result gallery"><a href="/en/photos/interior">Interior</a>`,
		`class="result lists"><a href="/en/faq#`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q\n%s", want, body)
		}
	}

	if strings.Contains(body, "Secret") {
		t.Fatalf("draft found\n%s", body)
	}

	// A title match ranks above text matches.
	_, results, _ := strings.Cut(body, `<main id="content">`)
	if strings.Index(results, "New café") > strings.Index(results, "Opening hours") {
		t.Fatalf("title match ranked lower\n%s", body)
	}

	// Other languages search their own content.
	visitor.Expect("/nl/search?q=cafe", http.StatusOK, "nothing found")
	visitor.Expect("/en/search", http.StatusOK, "<h1>search: </h1>", "nothing found")

	// The template function searches in the page's language.
	listing := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"Results"},
		"Slug":   {"results"},
		"Parent": {home},
	})
	s.Publish(listing + "?locale=en")
	visitor.Expect("/en/results", http.StatusOK, `<i class="hit">New café</i>`, `<i class="hit">Opening hours</i>`)

	// A published page at /search replaces the search screen.
	own := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"Find things"},
		"Slug":   {"search"},
		"Parent": {home},
	})
	s.Publish(own + "?locale=en")
	visitor.Expect("/en/search?q=cafe", http.StatusOK, "<h1>Find things</h1>")
	visitor.ExpectMissing("/en/search?q=cafe", "result page")
}
