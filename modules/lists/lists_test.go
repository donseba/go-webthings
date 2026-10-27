package lists_test

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/modules/lists"
	"github.com/donseba/go-webthings/sitetest"
)

func TestLists(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, lists.Module("faq", "FAQ"))

	first := s.Submit("/admin/faq/new?locale=en", url.Values{
		"Title": {"First question"},
		"Body":  {"The *first* answer"},
		"Link":  {"https://example.com/first"},
	})
	second := s.Submit("/admin/faq/new?locale=en", url.Values{
		"Title": {"Second question"},
		"Body":  {"Not published"},
	})
	third := s.Submit("/admin/faq/new?locale=en", url.Values{
		"Title": {"Third question"},
		"Body":  {"The third answer"},
	})

	s.Publish(first + "?locale=en")
	s.Publish(third + "?locale=en")

	// Move the third entry to the top: third, first, second.
	thirdID := strings.TrimPrefix(third, "/admin/faq/")

	s.Post("/admin/faq?locale=en", "/admin/faq/"+thirdID+"/up?locale=en", url.Values{})
	s.Post("/admin/faq?locale=en", "/admin/faq/"+thirdID+"/up?locale=en", url.Values{})

	s.Mount("faq", "FAQ", "faq")
	s.Expect("/en/faq", http.StatusOK, "<h1>FAQ</h1>", "<em>first</em>", `href="https://example.com/first"`)
	s.ExpectMissing("/en/faq", "Second question")
	s.Expect("/en/faq/nope", http.StatusNotFound)

	_, page := s.Get("/en/faq")
	if strings.Index(page, "Third question") > strings.Index(page, "First question") {
		t.Fatalf("third entry should come first:\n%s", page)
	}

	if !strings.Contains(page, `id="`+thirdID+`"`) {
		t.Fatalf("missing anchor %s:\n%s", thirdID, page)
	}

	_, order := s.Get("/admin/faq?locale=en")
	if strings.Index(order, "Second question") < strings.Index(order, "First question") {
		t.Fatalf("unpublished entry should stay last:\n%s", order)
	}

	entries, err := lists.Of(s.App, "faq").Entries(context.Background(), "en")
	if err != nil {
		t.Fatal(err)
	}

	var titles []string
	for _, entry := range entries {
		titles = append(titles, entry.Title)
	}

	if strings.Join(titles, ",") != "Third question,First question" {
		t.Fatalf("entries: %v", titles)
	}

	if entries[1].Link != template.URL("https://example.com/first") || entries[0].ID != thirdID {
		t.Fatalf("entries: %+v", entries)
	}

	// Unpublished entries are reachable in the admin, not on the site.
	s.Expect(second+"?locale=en", http.StatusOK, "Second question")
}

// TestGeneratedList renders the generated starting point: entries carry the
// anchor the site search links to, and phone links keep working.
func TestGeneratedList(t *testing.T) {
	dir := t.TempDir()

	s := sitetest.StartWith(t, sitetest.Options{
		Templates: dir,
		Prepare: func(app *webthings.App) error {
			_, err := app.WriteTemplates(dir, "faq")

			return err
		},
	}, lists.Module("faq", "FAQ"))

	entry := s.Submit("/admin/faq/new?locale=en", url.Values{
		"Title": {"How do I call you?"},
		"Body":  {"Any time."},
		"Link":  {"tel:+31201234567"},
	})
	s.Publish(entry + "?locale=en")

	home, err := s.App.Pages.Home(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	pageValues := url.Values{"Title": {"FAQ"}, "Slug": {"faq"}, "Parent": {home}, "Module": {"faq"}}
	page := s.Submit("/admin/pages/new?locale=en", pageValues)
	s.Publish(page + "?locale=en")

	id := strings.TrimPrefix(entry, "/admin/faq/")

	s.Expect("/en/search?q=call", http.StatusOK, `href="/en/faq#`+id+`"`)
	s.Expect("/en/faq", http.StatusOK, `<section id="`+id+`">`, `href="tel:&#43;31201234567"`)
	s.ExpectMissing("/en/faq", "ZgotmplZ")

	// A draft of the page does not change the title on the website.
	pageValues.Set("Title", "Secret new name")
	s.Submit(page+"?locale=en", pageValues)
	s.Expect("/en/faq", http.StatusOK, "<h1>FAQ</h1>")
	s.ExpectMissing("/en/faq", "Secret new name")
}
