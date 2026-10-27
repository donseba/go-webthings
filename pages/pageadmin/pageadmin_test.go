package pageadmin_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

// inOrder fails unless every part occurs in body, in the given order.
func inOrder(t *testing.T, body string, parts ...string) {
	t.Helper()

	at := 0

	for _, part := range parts {
		index := strings.Index(body[at:], part)
		if index < 0 {
			t.Fatalf("missing %q after position %d (order %q)\n%s", part, at, parts, body)
		}

		at += index + len(part)
	}
}

// id is the page ID in an editor path such as /admin/pages/<id>.
func id(editor string) string {
	return strings.TrimPrefix(editor, "/admin/pages/")
}

// TestPageTree checks the overview tree, slugs made from titles, moves among
// siblings and the parent choices.
func TestPageTree(t *testing.T) {
	s := sitetest.Start(t, publishing.Module("news", "News"))

	home, err := s.App.Pages.Home(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	about := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About us"}, "Parent": {home}})
	team := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"Team"}, "Parent": {id(about)}})
	contact := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"Contact"}, "Parent": {home}})
	s.Mount("news", "News", "news")

	// The slug is made from the title.
	s.Expect(about+"?locale=en", http.StatusOK, `value="about-us"`)

	_, overview := s.Get("/admin/pages?locale=en")
	inOrder(t, overview, ">Home<", "About us", "/about-us", "Team", "/about-us/team", "Contact", "News", "Module: News")
	inOrder(t, overview, "Contact", "Draft", "News", "Published")

	// Home does not move; the first child only moves down, the last only up.
	if strings.Contains(overview, "/admin/pages/"+home+"/up") || strings.Contains(overview, "/admin/pages/"+home+"/down") {
		t.Fatalf("home can move:\n%s", overview)
	}

	if strings.Contains(overview, about+"/up") || !strings.Contains(overview, about+"/down") {
		t.Fatalf("first page moves:\n%s", overview)
	}

	s.Post("/admin/pages?locale=en", contact+"/up?locale=en", url.Values{})

	_, overview = s.Get("/admin/pages?locale=en")
	inOrder(t, overview, ">Home<", "Contact", "About us", "Team", "News")

	// The menu follows the tree, also after a move without publishing.
	s.Publish(about + "?locale=en")
	s.Publish(contact + "?locale=en")

	visitor := s.Visitor()
	_, page := visitor.Get("/en/")
	inOrder(t, page, `href="/en/contact"`, `href="/en/about-us"`, `href="/en/news"`)

	s.Post("/admin/pages?locale=en", contact+"/down?locale=en", url.Values{})

	_, page = visitor.Get("/en/")
	inOrder(t, page, `href="/en/about-us"`, `href="/en/contact"`, `href="/en/news"`)

	// About's parent can be Home or Contact, in tree order, not itself, the
	// pages below it or a module page.
	_, editor := s.Get(about + "?locale=en")
	inOrder(t, editor, `value="`+home+`"`, `value="`+id(contact)+`"`)

	for _, refused := range []string{team, about} {
		if strings.Contains(editor, `<option value="`+id(refused)+`"`) {
			t.Fatalf("parent choices offer %s:\n%s", refused, editor)
		}
	}

	if strings.Contains(editor, ">— News<") {
		t.Fatalf("parent choices offer the module page:\n%s", editor)
	}

	// A page can move to another parent from its editor.
	s.Post(team+"?locale=en", team+"?locale=en", url.Values{"Title": {"Team"}, "Parent": {id(contact)}})

	_, overview = s.Get("/admin/pages?locale=en")
	inOrder(t, overview, "About us", "Contact", "Team", "/contact/team", "News")
}

// TestHomeStaysPublished checks that the home editor offers no way to take
// Home off the site and that attempts are refused with the reason.
func TestHomeStaysPublished(t *testing.T) {
	s := sitetest.Start(t)

	home, err := s.App.Pages.Home(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	editor := "/admin/pages/" + home + "?locale=en"

	_, body := s.Get(editor)
	for _, missing := range []string{"/unpublish", "/delete", `name="UnpublishAt"`} {
		if strings.Contains(body, missing) {
			t.Fatalf("home editor offers %s:\n%s", missing, body)
		}
	}

	if !strings.Contains(body, `name="PublishAt"`) {
		t.Fatalf("home editor cannot plan a publication:\n%s", body)
	}

	_, latest := s.Get(editor)

	revision := strings.SplitN(strings.SplitN(latest, "/revisions/", 2)[1], "/", 2)[0]
	reason := "The home page always stays on the website"

	attempts := map[string]url.Values{
		"/admin/pages/" + home + "/delete?locale=en":    {},
		"/admin/pages/" + home + "/unpublish?locale=en": {},
		"/admin/pages/" + home + "/revisions/" + revision + "/schedule?locale=en": {
			"UnpublishAt": {time.Now().Add(time.Hour).Format("2006-01-02T15:04")},
		},
	}

	for action, values := range attempts {
		status, refused := s.PostStatus(editor, action, values)
		if status != http.StatusUnprocessableEntity || !strings.Contains(refused, reason) {
			t.Fatalf("POST %s: %d\n%s", action, status, refused)
		}
	}

	s.Visitor().Expect("/en/", http.StatusOK)
}

// TestRevisionTimesInSiteTimezone checks that the revision history shows
// times in the site's timezone, like the schedule.
func TestRevisionTimesInSiteTimezone(t *testing.T) {
	s := sitetest.Start(t)
	ctx := context.Background()

	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Kathmandu is 5:45 ahead of UTC, so neither hour nor minute match UTC.
	settings.Timezone = "Asia/Kathmandu"
	if err := s.App.Site.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}

	zone, err := time.LoadLocation("Asia/Kathmandu")
	if err != nil {
		t.Skip("no timezone data:", err)
	}

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// The admin is English: "6 Oct 2026 03:43".
	shown := func() string {
		now := time.Now().In(zone)

		return frontend.FormatDate("en", now, "medium") + " " + now.Format("15:04")
	}

	before := shown()
	about := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About"}, "Parent": {home}})
	after := shown()

	_, body := s.Get(about + "?locale=en")
	if !strings.Contains(body, before) && !strings.Contains(body, after) {
		t.Fatalf("history misses %s in Asia/Kathmandu:\n%s", after, body)
	}

	if !strings.Contains(body, "(Asia/Kathmandu)") {
		t.Fatalf("history does not name the timezone:\n%s", body)
	}
}
