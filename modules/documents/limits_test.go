package documents_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/donseba/go-webthings/modules/documents"
	"github.com/donseba/go-webthings/sitetest"
)

// TestLatestLimitAndPageTitle checks that a limit of 0 or less means all
// documents and that a draft of the module's page does not change its title
// on the website.
func TestLatestLimitAndPageTitle(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, documents.Module("documents", "Documents"))
	ctx := context.Background()

	for _, title := range []string{"First", "Second", "Third"} {
		document := s.Submit("/admin/documents/new?locale=en", url.Values{
			"Title": {title},
			"Date":  {"2026-03-10"},
			"Link":  {"https://example.test/" + title + ".pdf"},
		})
		s.Publish(document + "?locale=en")
	}

	service := documents.Of(s.App, "documents")

	for limit, want := range map[int]int{-1: 3, 0: 3, 2: 2, 10: 3} {
		latest, err := service.Latest(ctx, "en", limit)
		if err != nil || len(latest) != want {
			t.Fatalf("limit %d: want %d, got %d %v", limit, want, len(latest), err)
		}
	}

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	values := url.Values{"Title": {"Documents"}, "Slug": {"documents"}, "Parent": {home}, "Module": {"documents"}}
	page := s.Submit("/admin/pages/new?locale=en", values)
	s.Publish(page + "?locale=en")

	values.Set("Title", "Secret new name")
	s.Submit(page+"?locale=en", values)
	s.Expect("/en/documents", http.StatusOK, "<h1>Documents</h1>")
	s.ExpectMissing("/en/documents", "Secret new name")
}
