package people_test

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/modules/people"
	"github.com/donseba/go-webthings/modules/redirects"
	"github.com/donseba/go-webthings/sitetest"
)

// TestLinksToPerson checks the editor of a person, content without
// revisions, that a page links to: the notice and its questions, the old
// address redirecting to the new one when the person is saved with a new
// slug, and unpublishing with a redirect.
func TestLinksToPerson(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"},
		people.Module("team", "Team"), redirects.Module())
	team := s.Mount("team", "Team", "team")

	jane := s.Submit("/admin/team/new?locale=en", url.Values{"Name": {"Jane Doe"}, "Slug": {"jane"}})
	s.Publish(jane + "?locale=en")

	home, err := s.App.Pages.Home(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	contact := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"Contact"},
		"Slug":   {"contact"},
		"Parent": {home},
		"Body":   {"Ask [Jane](/en/team/jane)."},
	})
	s.Publish(contact + "?locale=en")

	_, body := s.Get(jane + "?locale=en")
	for _, part := range []string{
		"1 published item links to this item.",
		"Saving it with another address redirects the old address to the new one.",
		`href="` + contact + `?locale=en">Contact</a>`,
		html.EscapeString("Published items link to this item. Unpublish it anyway?"),
		jane + "/unpublish-redirect?locale=en",
		`<option value="` + team[strings.LastIndex(team, "/")+1:] + `" selected`,
	} {
		if !strings.Contains(body, part) {
			t.Fatalf("editor without %q:\n%s", part, body)
		}
	}

	_, body = s.PostStatus(jane+"?locale=en", jane+"?locale=en", url.Values{"Name": {"Jane Doe"}, "Slug": {"jane-doe"}})
	if !sitetest.HasToast(body, "success", "Changes saved. 1 old address redirects now.") {
		t.Fatalf("no toast of the redirect:\n%s", body)
	}

	response, _ := s.Visitor().Response("/en/team/jane")
	if response.StatusCode != http.StatusOK || response.Request.URL.Path != "/en/team/jane-doe" {
		t.Fatalf("/en/team/jane: %d %s", response.StatusCode, response.Request.URL.Path)
	}

	// Saving without a new address redirects nothing.
	_, body = s.PostStatus(jane+"?locale=en", jane+"?locale=en", url.Values{"Name": {"Jane D."}, "Slug": {"jane-doe"}})
	if !sitetest.HasToast(body, "success", "Changes saved.") {
		t.Fatalf("toast of a save without a new address:\n%s", body)
	}

	s.Post(
		jane+"?locale=en",
		jane+"/unpublish-redirect?locale=en",
		url.Values{"Target": {contact[strings.LastIndex(contact, "/")+1:]}},
	)

	response, _ = s.Visitor().Response("/en/team/jane-doe")
	if response.StatusCode != http.StatusOK || response.Request.URL.Path != "/en/contact" {
		t.Fatalf("/en/team/jane-doe: %d %s", response.StatusCode, response.Request.URL.Path)
	}
}
