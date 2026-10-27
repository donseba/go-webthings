package pageadmin_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/sitetest"
)

// TestPermanentPage checks the permanent mark end to end: an administrator
// marks a page permanent, after which it cannot be deleted, and removes the
// mark again. The home page is permanent by its nature and offers no mark.
func TestPermanentPage(t *testing.T) {
	s := sitetest.Start(t)

	home, err := s.App.Pages.Home(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if _, body := s.Get("/admin/pages/" + home + "?locale=en"); strings.Contains(body, "/permanent") {
		t.Fatal("the home page offers the permanent mark")
	}

	about := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About"}, "Parent": {home}})
	s.Publish(about + "?locale=en")

	editor := about + "?locale=en"
	s.Post(editor, about+"/permanent?locale=en", url.Values{})

	status, body := s.PostStatus(editor, about+"/delete?locale=en", url.Values{})
	if status != http.StatusUnprocessableEntity || !sitetest.HasToast(
		body,
		"error",
		"This item is permanent: it cannot be unpublished, scheduled to be unpublished or deleted until an administrator removes the mark.",
	) {
		t.Fatalf("delete a permanent page: %d\n%s", status, body)
	}

	s.Visitor().Expect("/en/about", http.StatusOK)

	s.Post(editor, about+"/permanent/off?locale=en", url.Values{})

	if status, body := s.PostStatus(editor, about+"/delete?locale=en", url.Values{}); status != http.StatusOK {
		t.Fatalf("delete after removing the mark: %d\n%s", status, body)
	}
}

var submitPattern = regexp.MustCompile(`action="([^"]*/submit[^"]*)"`)

// TestTreeShowsInReview checks that the page tree names a page waiting for
// review as the other overviews do.
func TestTreeShowsInReview(t *testing.T) {
	s := sitetest.Start(t)
	ctx := context.Background()

	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	settings.Review = []string{"pages"}
	if err := s.App.Site.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	about := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About"}, "Parent": {home}})

	_, page := s.Get(about + "?locale=en")

	match := submitPattern.FindStringSubmatch(page)
	if match == nil {
		t.Fatalf("no submit button:\n%s", page)
	}

	s.Post(about+"?locale=en", strings.ReplaceAll(match[1], "&amp;", "&"), url.Values{})

	_, tree := s.Get("/admin/pages?locale=en")
	inOrder(t, tree, "About", `<span class="badge">In review</span>`)
}
