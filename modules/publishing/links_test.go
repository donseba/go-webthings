package publishing_test

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/modules/redirects"
	"github.com/donseba/go-webthings/sitetest"
)

// publishMoved finds the action of the form that publishes a revision that
// changes the address of an item.
var publishMoved = regexp.MustCompile(`action="([^"]*/publish-moved[^"]*)"`)

// linkedPost starts a site with modules, a news page with the post Hello,
// published at /en/news/hello, and the page Contact, which links to it. It
// returns the site and the editors of the post, the news page and Contact.
func linkedPost(t *testing.T, modules ...webthings.Module) (s *sitetest.Site, post, news, contact string) {
	t.Helper()

	s = sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"},
		append([]webthings.Module{publishing.Module("news", "News")}, modules...)...)
	news = s.Mount("news", "News", "news")

	post = s.Submit("/admin/news/new?locale=en", url.Values{"Title": {"Hello"}, "Slug": {"hello"}, "Date": {"2026-10-01"}})
	s.Publish(post + "?locale=en")

	home, err := s.App.Pages.Home(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	contact = s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"Contact"},
		"Slug":   {"contact"},
		"Parent": {home},
		"Body":   {"Read [the news](/en/news/hello)."},
	})
	s.Publish(contact + "?locale=en")

	return s, post, news, contact
}

// lastPart is the last part of an address, such as the ID in an editor's.
func lastPart(address string) string {
	return address[strings.LastIndex(address, "/")+1:]
}

// landsOn returns the path a visitor ends on after asking for path.
func landsOn(t *testing.T, s *sitetest.Site, path string) string {
	t.Helper()

	response, body := s.Visitor().Response(path)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d\n%s", path, response.StatusCode, body)
	}

	return response.Request.URL.Path
}

// TestLinksToPost checks the editor of a post that a page links to: the
// notice names the page, a new slug is published with a redirect from the
// old address, and unpublishing redirects the post's address to a page.
func TestLinksToPost(t *testing.T) {
	s, post, news, contact := linkedPost(t, redirects.Module())

	_, body := s.Get(post + "?locale=en")
	for _, part := range []string{
		"1 published item links to this item.",
		`href="` + contact + `?locale=en">Contact</a>`,
		"Page · en",
		html.EscapeString("Published items link to this item. Unpublish it anyway?"),
		post + "/unpublish-redirect?locale=en",
		`<option value="` + lastPart(news) + `" selected`,
	} {
		if !strings.Contains(body, part) {
			t.Fatalf("editor without %q:\n%s", part, body)
		}
	}

	// A new slug: publishing it redirects the old address.
	s.Submit(post+"?locale=en", url.Values{"Title": {"Hello"}, "Slug": {"hello-world"}, "Date": {"2026-10-01"}})

	_, body = s.Get(post + "?locale=en")
	for _, part := range []string{
		"Publishing this revision changes the address of this item.",
		"1 published item links to the old address.",
		"/en/news/hello → /en/news/hello-world",
		`name="Redirect"`,
	} {
		if !strings.Contains(body, part) {
			t.Fatalf("revision editor without %q:\n%s", part, body)
		}
	}

	action := strings.ReplaceAll(publishMoved.FindStringSubmatch(body)[1], "&amp;", "&")
	s.Post(post+"?locale=en", action, url.Values{"Redirect": {"true"}})

	if got := landsOn(t, s, "/en/news/hello"); got != "/en/news/hello-world" {
		t.Fatalf("/en/news/hello leads to %s", got)
	}

	_, report := s.Get("/admin/pages/links")
	if !strings.Contains(report, "No broken links") {
		t.Fatalf("redirected link is broken:\n%s", report)
	}

	// Unpublishing with a redirect sends the post's address to Contact.
	s.Post(post+"?locale=en", post+"/unpublish-redirect?locale=en", url.Values{"Target": {lastPart(contact)}})

	if got := landsOn(t, s, "/en/news/hello-world"); got != "/en/contact" {
		t.Fatalf("/en/news/hello-world leads to %s", got)
	}

	s.ExpectMissing(post+"?locale=en", "link to this item")
}

// TestRedirectsCancelSchedule checks that publishing and unpublishing a
// post with redirects tell, as the plain actions do, that they cancelled
// the post's schedule, and that the editor says so before.
func TestRedirectsCancelSchedule(t *testing.T) {
	s, post, _, contact := linkedPost(t, redirects.Module())
	scheduling, _ := publishing.Of(s.App, "news").Store().(content.Scheduling)

	plan := func() {
		t.Helper()

		later := content.Schedule{UnpublishAt: time.Now().Add(24 * time.Hour)}
		if err := scheduling.Schedule(t.Context(), lastPart(post), later, "en"); err != nil {
			t.Fatal(err)
		}
	}

	cancelled := func(body, notice string) {
		t.Helper()

		if !sitetest.HasToast(body, "success", notice) {
			t.Fatalf("no notice %q:\n%s", notice, body)
		}
	}

	plan()
	s.Submit(post+"?locale=en", url.Values{"Title": {"Hello"}, "Slug": {"hello-world"}, "Date": {"2026-10-01"}})

	_, body := s.Get(post + "?locale=en")
	action := strings.ReplaceAll(publishMoved.FindStringSubmatch(body)[1], "&amp;", "&")
	_, body = s.PostStatus(post+"?locale=en", action, url.Values{"Redirect": {"true"}})
	cancelled(body, "Published. The schedule was cancelled. 1 old address redirects now.")

	// Contact links to the post's old address now, which the editor does
	// not count as a link to the post, so it offers no redirect; the form
	// is posted all the same.
	plan()
	s.Expect(post+"?locale=en", http.StatusOK, "Publishing or unpublishing by hand cancels the schedule.")

	_, body = s.PostStatus(post+"?locale=en", post+"/unpublish-redirect?locale=en", url.Values{"Target": {lastPart(contact)}})
	cancelled(body, "Unpublished. The schedule was cancelled. 1 old address redirects now.")
}

// TestLinksToPostWithoutRedirects checks that without the redirects module
// the editor of a linked post asks before it leaves its address, and
// offers no redirect.
func TestLinksToPostWithoutRedirects(t *testing.T) {
	s, post, _, _ := linkedPost(t)

	_, body := s.Get(post + "?locale=en")
	for _, question := range []string{
		"Published items link to this item. Unpublish it anyway?",
		"Published items link to this item. Move it to the trash anyway?",
	} {
		if !strings.Contains(body, html.EscapeString(question)) {
			t.Fatalf("no question %q:\n%s", question, body)
		}
	}

	if strings.Contains(body, "unpublish-redirect") {
		t.Fatalf("a redirect is offered without the redirects module:\n%s", body)
	}

	s.Submit(post+"?locale=en", url.Values{"Title": {"Hello"}, "Slug": {"hello-world"}, "Date": {"2026-10-01"}})

	_, body = s.Get(post + "?locale=en")
	if !strings.Contains(body, html.EscapeString(
		"Published items link to the old address, and their links will lead nowhere. Publish anyway?",
	)) || strings.Contains(body, "publish-moved") {
		t.Fatalf("revision editor:\n%s", body)
	}
}

// TestLinksToCategory checks the editor of a category, content without
// revisions, whose list a page links to: the notice and its questions, the
// old address redirecting to the new one when the category is saved with a
// new slug, and unpublishing with a redirect.
func TestLinksToCategory(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"},
		publishing.Module("news", "News"), redirects.Module())
	news := s.Mount("news", "News", "news")

	tips := s.Submit("/admin/news-categories/new?locale=en", url.Values{"Title": {"Tips"}, "Slug": {"tips"}})
	s.Publish(tips + "?locale=en")

	home, err := s.App.Pages.Home(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	contact := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"Contact"},
		"Slug":   {"contact"},
		"Parent": {home},
		"Body":   {"Read [our tips](/en/news/category/tips)."},
	})
	s.Publish(contact + "?locale=en")

	_, body := s.Get(tips + "?locale=en")
	for _, part := range []string{
		"1 published item links to this item.",
		"Saving it with another address redirects the old address to the new one.",
		`href="` + contact + `?locale=en">Contact</a>`,
		html.EscapeString("Published items link to this item. Unpublish it anyway?"),
		tips + "/unpublish-redirect?locale=en",
		`<option value="` + lastPart(news) + `" selected`,
	} {
		if !strings.Contains(body, part) {
			t.Fatalf("editor without %q:\n%s", part, body)
		}
	}

	_, body = s.PostStatus(tips+"?locale=en", tips+"?locale=en", url.Values{"Title": {"Tips"}, "Slug": {"advice"}})
	if !sitetest.HasToast(body, "success", "Changes saved. 1 old address redirects now.") {
		t.Fatalf("no toast of the redirect:\n%s", body)
	}

	if got := landsOn(t, s, "/en/news/category/tips"); got != "/en/news/category/advice" {
		t.Fatalf("/en/news/category/tips leads to %s", got)
	}

	s.Post(tips+"?locale=en", tips+"/unpublish-redirect?locale=en", url.Values{"Target": {lastPart(contact)}})

	if got := landsOn(t, s, "/en/news/category/advice"); got != "/en/contact" {
		t.Fatalf("/en/news/category/advice leads to %s", got)
	}

	s.ExpectMissing(tips+"?locale=en", "link to this item")
}
