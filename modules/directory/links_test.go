package directory_test

import (
	"html"
	"net/http"
	"net/url"
	"path"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/modules/directory"
	"github.com/donseba/go-webthings/modules/redirects"
	"github.com/donseba/go-webthings/sitetest"
)

// TestLinksToCategory checks the editor of a category, content without
// revisions, that pages link to, one to the category and one to an entry
// in it: the notices, the links to the category apart from those to its
// entries, and their questions, the old address redirecting to the new one
// when the category is saved with a new slug, the entry's old address
// leading to its new one, and unpublishing with a redirect, of the
// category and of its entries.
func TestLinksToCategory(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"},
		directory.Module("guide", "Guide"), redirects.Module())
	guide := s.Mount("guide", "Guide", "guide")

	food := s.Submit("/admin/guide-categories/new?locale=en", url.Values{"Name": {"Food"}, "Slug": {"food"}})
	s.Publish(food + "?locale=en")

	bakery := s.Submit("/admin/guide/new?locale=en", url.Values{
		"Name": {"Bakery"}, "Slug": {"bakery"}, "Category": {path.Base(food)}, "Image": {""},
	})
	s.Publish(bakery + "?locale=en")

	home, err := s.App.Pages.Home(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	lunch := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"Lunch"},
		"Slug":   {"lunch"},
		"Parent": {home},
		"Body":   {"Bread from the [bakery](/en/guide/food/bakery)."},
	})
	s.Publish(lunch + "?locale=en")

	// Only a link to an entry: unpublishing breaks it, a new slug not.
	_, body := s.Get(food + "?locale=en")
	for _, part := range []string{
		"1 published item links to an address below this item. Unpublishing it or moving it to the trash breaks these links.",
		`href="` + lunch + `?locale=en">Lunch</a>`,
		html.EscapeString("Published items link to addresses below this item. Unpublish it anyway?"),
		food + "/unpublish-redirect?locale=en",
	} {
		if !strings.Contains(body, part) {
			t.Fatalf("editor without %q:\n%s", part, body)
		}
	}

	if strings.Contains(body, "link to this item") || strings.Contains(body, "links to this item") {
		t.Fatalf("a link to an entry counts as one to the category:\n%s", body)
	}

	contact := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"Contact"},
		"Slug":   {"contact"},
		"Parent": {home},
		"Body":   {"Where to [eat](/en/guide/food)."},
	})
	s.Publish(contact + "?locale=en")

	_, body = s.Get(food + "?locale=en")
	for _, part := range []string{
		"1 published item links to this item.",
		"1 published item links to an address below this item.",
		`href="` + lunch + `?locale=en">Lunch</a>`,
		"Saving it with another address redirects the old address to the new one.",
		`href="` + contact + `?locale=en">Contact</a>`,
		html.EscapeString("Published items link to this item. Unpublish it anyway?"),
		food + "/unpublish-redirect?locale=en",
		`<option value="` + path.Base(guide) + `" selected`,
	} {
		if !strings.Contains(body, part) {
			t.Fatalf("editor without %q:\n%s", part, body)
		}
	}

	_, body = s.PostStatus(food+"?locale=en", food+"?locale=en", url.Values{"Name": {"Food"}, "Slug": {"eat"}})
	if !sitetest.HasToast(body, "success", "Changes saved. 1 old address redirects now.") {
		t.Fatalf("no toast of the redirect:\n%s", body)
	}

	if got := landsOn(t, s, "/en/guide/food"); got != "/en/guide/eat" {
		t.Fatalf("/en/guide/food leads to %s", got)
	}

	// The entry keeps its slug and moves with its category.
	if got := landsOn(t, s, "/en/guide/food/bakery"); got != "/en/guide/eat/bakery" {
		t.Fatalf("/en/guide/food/bakery leads to %s", got)
	}

	_, body = s.PostStatus(food+"?locale=en", food+"/unpublish-redirect?locale=en", url.Values{"Target": {path.Base(contact)}})
	if !sitetest.HasToast(body, "success", "Unpublished. 2 old addresses redirect now.") {
		t.Fatalf("no toast of the redirects:\n%s", body)
	}

	// The category's address and its entry's, which left with it.
	for _, address := range []string{"/en/guide/eat", "/en/guide/eat/bakery"} {
		if got := landsOn(t, s, address); got != "/en/contact" {
			t.Fatalf("%s leads to %s", address, got)
		}
	}

	s.ExpectMissing(food+"?locale=en", "link to this item")
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
