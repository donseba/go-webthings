package testsuite

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/donseba/go-webthings/sitetest"
)

func TestPagesEndToEnd(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t)

	s.Expect("/", http.StatusOK, "<h1>Home</h1>")

	home, _ := s.App.Pages.Home(context.Background())
	editor := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"About"},
		"Slug":   {"about"},
		"Parent": {home},
		"Body":   {"Hello **world**"},
	})

	s.Expect("/en/about", http.StatusNotFound)
	s.Publish(editor + "?locale=en")
	s.Expect("/en/about", http.StatusOK, "<strong>world</strong>", `href="/en/about">About`)
	s.Expect("/en/missing", http.StatusNotFound, "not found")
}
