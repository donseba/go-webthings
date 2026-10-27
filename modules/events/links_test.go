package events_test

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/redirects"
	"github.com/donseba/go-webthings/sitetest"
)

// publishMoved finds the action of the form that publishes a revision that
// changes the address of an item.
var publishMoved = regexp.MustCompile(`action="([^"]*/publish-moved[^"]*)"`)

// linkedEvent starts a site with modules, an agenda page with the event
// Fair, published at /en/agenda/fair, and the page Visit, which links to
// it. It returns the site, the event's values and the editors of the
// event, the agenda page and Visit.
func linkedEvent(t *testing.T, modules ...webthings.Module) (s *sitetest.Site, fair url.Values, event, agenda, visit string) {
	t.Helper()

	s = sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"},
		append([]webthings.Module{events.Module("agenda", "Agenda")}, modules...)...)
	agenda = s.Mount("agenda", "Agenda", "agenda")

	day := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)
	fair = url.Values{"Title": {"Fair"}, "Slug": {"fair"}, "Start": {day + "T14:00"}, "Timezone": {"UTC"}}

	event = s.Submit("/admin/agenda/new?locale=en", fair)
	s.Publish(event + "?locale=en")

	home, err := s.App.Pages.Home(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	visit = s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"Visit"},
		"Slug":   {"visit"},
		"Parent": {home},
		"Body":   {"Come to [the fair](/en/agenda/fair)."},
	})
	s.Publish(visit + "?locale=en")

	return s, fair, event, agenda, visit
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

// TestLinksToEvent checks the editor of an event that a page links to: the
// notice names the page, a new slug is published with a redirect from the
// old address, and unpublishing redirects the event's address to a page.
func TestLinksToEvent(t *testing.T) {
	s, fair, event, agenda, visit := linkedEvent(t, redirects.Module())

	_, body := s.Get(event + "?locale=en")
	for _, part := range []string{
		"1 published item links to this item.",
		`href="` + visit + `?locale=en">Visit</a>`,
		"Page · en",
		html.EscapeString("Published items link to this item. Move it to the trash anyway?"),
		event + "/unpublish-redirect?locale=en",
		`<option value="` + lastPart(agenda) + `" selected`,
	} {
		if !strings.Contains(body, part) {
			t.Fatalf("editor without %q:\n%s", part, body)
		}
	}

	fair.Set("Slug", "summer-fair")
	s.Submit(event+"?locale=en", fair)

	_, body = s.Get(event + "?locale=en")
	for _, part := range []string{
		"Publishing this revision changes the address of this item.",
		"1 published item links to the old address.",
		"/en/agenda/fair → /en/agenda/summer-fair",
	} {
		if !strings.Contains(body, part) {
			t.Fatalf("revision editor without %q:\n%s", part, body)
		}
	}

	// Publishing without the redirect leaves the old address broken.
	action := strings.ReplaceAll(publishMoved.FindStringSubmatch(body)[1], "&amp;", "&")
	s.Post(event+"?locale=en", action, url.Values{})

	if status, _ := s.Visitor().Get("/en/agenda/fair"); status != http.StatusNotFound {
		t.Fatalf("/en/agenda/fair: %d", status)
	}

	// Back to the old slug, with the redirect this time.
	fair.Set("Slug", "fair")
	s.Submit(event+"?locale=en", fair)

	_, body = s.Get(event + "?locale=en")
	action = strings.ReplaceAll(publishMoved.FindStringSubmatch(body)[1], "&amp;", "&")
	s.Post(event+"?locale=en", action, url.Values{"Redirect": {"true"}})

	if got := landsOn(t, s, "/en/agenda/summer-fair"); got != "/en/agenda/fair" {
		t.Fatalf("/en/agenda/summer-fair leads to %s", got)
	}

	s.Post(event+"?locale=en", event+"/unpublish-redirect?locale=en", url.Values{"Target": {lastPart(visit)}})

	if got := landsOn(t, s, "/en/agenda/fair"); got != "/en/visit" {
		t.Fatalf("/en/agenda/fair leads to %s", got)
	}
}
