package pageadmin_test

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/modules/banners"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/modules/redirects"
	"github.com/donseba/go-webthings/sitetest"
)

// publishMoved finds the action of the form that publishes a revision
// that changes the page's address.
var publishMoved = regexp.MustCompile(`action="([^"]*/publish-moved[^"]*)"`)

// linkedSite is a site with About, which has Team below it, and Contact,
// whose text links to About and to Team.
type linkedSite struct {
	*sitetest.Site
	home, about, team, contact string
}

func startLinked(t *testing.T, modules ...webthings.Module) linkedSite {
	t.Helper()

	s := sitetest.Start(t, modules...)

	home, err := s.App.Pages.Home(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	site := linkedSite{Site: s, home: home}
	site.about = site.page(t, url.Values{"Title": {"About"}, "Slug": {"about"}, "Parent": {home}})
	site.team = site.page(t, url.Values{"Title": {"Team"}, "Slug": {"team"}, "Parent": {id(site.about)}})
	site.contact = site.page(t, url.Values{
		"Title":  {"Contact"},
		"Slug":   {"contact"},
		"Parent": {home},
		"Body":   {"Read [about us](/en/about) and meet [the team](about/team)."},
	})

	return site
}

// page creates and publishes a page and returns its editor.
func (s linkedSite) page(t *testing.T, values url.Values) string {
	t.Helper()

	editor := s.Submit("/admin/pages/new?locale=en", values)
	s.Publish(editor + "?locale=en")

	return editor
}

// redirected returns the path a visitor ends on after asking for path.
func redirected(t *testing.T, s *sitetest.Site, path string) string {
	t.Helper()

	response, body := s.Visitor().Response(path)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d\n%s", path, response.StatusCode, body)
	}

	return response.Request.URL.Path
}

// TestLinksToPage checks the notice in the editor of a page that published
// content links to: the pages and module content, from Markdown and link
// fields, linked to their editors, and the questions before the page is
// unpublished or deleted. Without the redirects module no redirect is
// offered.
func TestLinksToPage(t *testing.T) {
	s := startLinked(t, publishing.Module("news", "News"), banners.Module("hero", "Hero"))

	banner := s.Submit("/admin/hero/new?locale=en", url.Values{
		"Title":       {"Welcome"},
		"ButtonLabel": {"Who we are"},
		"ButtonURL":   {"/en/about#history"},
		"Tone":        {"info"},
	})
	s.Publish(banner + "?locale=en")

	s.Mount("news", "News", "news")
	post := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title": {"Hello"},
		"Slug":  {"hello"},
		"Date":  {"2026-10-01"},
		"Body":  {"See [who we are](../about)."},
	})
	s.Publish(post + "?locale=en")

	_, body := s.Get(s.about + "?locale=en")
	inOrder(t, body, "3 published items link to this page.",
		`href="`+s.contact+`?locale=en">Contact</a>`, "Page · en",
		`href="`+post+`?locale=en">Hello</a>`, "News · en",
		`href="`+banner+`?locale=en">Welcome</a>`, "Hero · en")

	for _, question := range []string{
		"Published items link to this page. Unpublish it anyway?",
		"Published items link to this page. Move it to the trash anyway?",
	} {
		if !strings.Contains(body, html.EscapeString(question)) {
			t.Fatalf("no question %q:\n%s", question, body)
		}
	}

	if strings.Contains(body, "unpublish-redirect") {
		t.Fatalf("a redirect is offered without the redirects module:\n%s", body)
	}

	// Team is linked too, below About; Contact is linked by nothing.
	s.Expect(s.team+"?locale=en", http.StatusOK, "1 published item links to this page.")
	s.ExpectMissing(s.contact+"?locale=en", "link to this page")

	// Unpublishing anyway breaks the links, which the report lists.
	s.Post(s.about+"?locale=en", s.about+"/unpublish?locale=en", url.Values{})

	_, report := s.Get("/admin/pages/links")
	inOrder(
		t,
		report,
		"/en/about",
		"The page with this address is not published.",
		">About<",
		">Contact<",
		">Hello<",
		">Welcome<",
		"/en/about/team",
		"The page with this address is not published.",
		">Team<",
		">Contact<",
	)

	if strings.Contains(report, "links/redirect") || !strings.Contains(report, "With the Redirects module") {
		t.Fatalf("the report offers redirects without the module:\n%s", report)
	}
}

// TestUnpublishAndRedirect checks that the editor of a linked page offers
// to unpublish it and redirect its addresses, those below it included, to
// another page.
func TestUnpublishAndRedirect(t *testing.T) {
	s := startLinked(t, redirects.Module())

	_, body := s.Get(s.about + "?locale=en")
	if !strings.Contains(body, s.about+"/unpublish-redirect?locale=en") || !strings.Contains(body, "Unpublish and redirect") {
		t.Fatalf("no redirect offered:\n%s", body)
	}

	// About and the pages below it are no targets.
	if strings.Contains(body, `<option value="`+id(s.team)+`"`) || !strings.Contains(body, `<option value="`+id(s.contact)+`"`) {
		t.Fatalf("redirect targets:\n%s", body)
	}

	s.Post(s.about+"?locale=en", s.about+"/unpublish-redirect?locale=en", url.Values{"Target": {id(s.contact)}})

	for _, path := range []string{"/en/about", "/en/about/team"} {
		if got := redirected(t, s.Site, path); got != "/en/contact" {
			t.Fatalf("%s leads to %s", path, got)
		}
	}

	_, report := s.Get("/admin/pages/links")
	if !strings.Contains(report, "No broken links") {
		t.Fatalf("redirected links are broken:\n%s", report)
	}
}

// TestSlugChangeRedirect checks that the editor of a revision that changes
// the address tells so and publishes it with redirects from the old
// addresses to the new ones, also of the pages below, and that the
// redirects work.
func TestSlugChangeRedirect(t *testing.T) {
	s := startLinked(t, redirects.Module())

	s.Submit(s.about+"?locale=en", url.Values{"Title": {"About"}, "Slug": {"about-us"}, "Parent": {s.home}})

	_, body := s.Get(s.about + "?locale=en")
	inOrder(t, body, "Publishing this revision changes the address", "1 published item links to the old address.",
		"/en/about → /en/about-us", "/publish-moved?locale=en", `name="Redirect"`, "checked")

	action := strings.ReplaceAll(publishMoved.FindStringSubmatch(body)[1], "&amp;", "&")
	s.Post(s.about+"?locale=en", action, url.Values{"Redirect": {"true"}})

	tests := map[string]string{"/en/about": "/en/about-us", "/en/about/team": "/en/about-us/team"}
	for from, to := range tests {
		if got := redirected(t, s.Site, from); got != to {
			t.Fatalf("%s leads to %s, want %s", from, got, to)
		}
	}

	_, report := s.Get("/admin/pages/links")
	if !strings.Contains(report, "No broken links") {
		t.Fatalf("redirected links are broken:\n%s", report)
	}

	// Back to the old slug: the redirect of /en/about, which would hide the
	// page there, makes way for one from the slug it leaves.
	s.Submit(s.about+"?locale=en", url.Values{"Title": {"About"}, "Slug": {"about"}, "Parent": {s.home}})

	_, body = s.Get(s.about + "?locale=en")
	action = strings.ReplaceAll(publishMoved.FindStringSubmatch(body)[1], "&amp;", "&")
	s.Post(s.about+"?locale=en", action, url.Values{"Redirect": {"true"}})

	if got := redirected(t, s.Site, "/en/about"); got != "/en/about" {
		t.Fatalf("/en/about leads to %s", got)
	}

	if got := redirected(t, s.Site, "/en/about-us"); got != "/en/about" {
		t.Fatalf("/en/about-us leads to %s", got)
	}
}

// TestRedirectsCancelSchedule checks that publishing and unpublishing a
// page with redirects tell, as the plain actions do, that they cancelled
// the page's schedule, and that the editor says so before.
func TestRedirectsCancelSchedule(t *testing.T) {
	s := startLinked(t, redirects.Module())
	ctx := context.Background()

	plan := func() {
		t.Helper()

		later := content.Schedule{UnpublishAt: time.Now().Add(24 * time.Hour)}
		if err := s.App.Pages.Schedule(ctx, id(s.about), later); err != nil {
			t.Fatal(err)
		}
	}

	cancelled := func(body, notice string) {
		t.Helper()

		if !strings.Contains(body, notice) {
			t.Fatalf("no notice %q:\n%s", notice, body)
		}

		if page, err := s.App.Pages.Latest(ctx, id(s.about)); err != nil || !page.Schedule.IsZero() {
			t.Fatalf("schedule kept: %+v %v", page.Schedule, err)
		}
	}

	publish := func(values url.Values) string {
		t.Helper()

		_, body := s.Get(s.about + "?locale=en")
		action := strings.ReplaceAll(publishMoved.FindStringSubmatch(body)[1], "&amp;", "&")
		_, body = s.PostStatus(s.about+"?locale=en", action, values)

		return body
	}

	// With and without the redirects.
	plan()
	s.Submit(s.about+"?locale=en", url.Values{"Title": {"About"}, "Slug": {"about-us"}, "Parent": {s.home}})
	cancelled(publish(url.Values{"Redirect": {"true"}}), "Published. The schedule was cancelled. 2 old addresses redirect now.")

	plan()
	s.Submit(s.about+"?locale=en", url.Values{"Title": {"About"}, "Slug": {"about"}, "Parent": {s.home}})
	cancelled(publish(url.Values{}), "Published. The schedule was cancelled.")

	plan()
	s.Expect(s.about+"?locale=en", http.StatusOK, "Publishing or unpublishing by hand cancels the schedule.",
		s.about+"/unpublish-redirect?locale=en")

	_, body := s.PostStatus(s.about+"?locale=en", s.about+"/unpublish-redirect?locale=en", url.Values{"Target": {id(s.contact)}})
	cancelled(body, "Unpublished. The schedule was cancelled.")
}

// TestBrokenLinksReport checks the report of the links to a page in the
// trash, and the redirect it offers, which mends them.
func TestBrokenLinksReport(t *testing.T) {
	s := startLinked(t, redirects.Module())

	s.Post(s.team+"?locale=en", s.team+"/delete?locale=en", url.Values{})

	_, report := s.Get("/admin/pages/links")
	inOrder(t, report, "/en/about/team", "The page with this address is in the trash.", ">Team<", ">Contact<",
		"links/redirect", `name="address" value="/en/about/team"`)

	s.Post(
		"/admin/pages/links",
		"/admin/pages/links/redirect",
		url.Values{"address": {"/en/about/team"}, "Target": {id(s.about)}},
	)

	_, report = s.Get("/admin/pages/links")
	if !strings.Contains(report, "No broken links") {
		t.Fatalf("the redirect did not mend the link:\n%s", report)
	}

	if got := redirected(t, s.Site, "/en/about/team"); got != "/en/about" {
		t.Fatalf("/en/about/team leads to %s", got)
	}
}

// TestBrokenLinksReportRights checks that the report tells an editor who
// may not create redirects that this right is missing, rather than that
// the Redirects module is, and offers no redirect.
func TestBrokenLinksReportRights(t *testing.T) {
	s := startLinked(t, redirects.Module())

	s.Post(s.team+"?locale=en", s.team+"/delete?locale=en", url.Values{})

	group := s.Submit("/admin/groups", url.Values{"Name": {"Readers"}})
	s.Post(group, group, url.Values{"name": {"Readers"}, "allow": {"pages:read", "redirects:read"}})

	account := s.Submit("/admin/accounts", url.Values{
		"Email":    {"reader@example.test"},
		"Password": {"reader long passphrase"},
		"Role":     {"editor"},
	})
	s.Post(account, account+"/groups", url.Values{"group": {group[strings.LastIndex(group, "/")+1:]}})

	reader := s.As("reader@example.test", "reader long passphrase")

	_, report := reader.Get("/admin/pages/links")
	if !strings.Contains(report, "You need the right to create redirects.") ||
		strings.Contains(report, "With the Redirects module") || strings.Contains(report, "links/redirect") {
		t.Fatalf("report without the right to create redirects:\n%s", report)
	}

	_, report = s.Get("/admin/pages/links")
	if strings.Contains(report, "You need the right") || strings.Contains(report, "With the Redirects module") {
		t.Fatalf("report with the right to create redirects:\n%s", report)
	}
}
