package testsuite

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/sitetest"
)

// TestAdminStrictForms checks that admin forms refuse values that cannot be
// read as their field's type at the field, rather than storing the zero
// value, and the rules the tags carry: a focal point of "abc" or NaN, a
// schedule that is no date, a slug and a missing timezone.
func TestAdminStrictForms(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t)
	ctx := context.Background()

	photo, err := s.App.Media.Upload(ctx, "photo.png", bytes.NewReader(sitetest.PNG(t, 40, 20)))
	if err != nil {
		t.Fatal(err)
	}

	page := "/admin/media/" + photo.ID
	s.Post(page, page+"/focus", url.Values{"FocalX": {"90"}, "FocalY": {"20"}})

	for _, x := range []string{"abc", "NaN", "Inf"} {
		status, body := s.PostStatus(page, page+"/focus", url.Values{"FocalX": {x}, "FocalY": {"20"}})
		if status != http.StatusUnprocessableEntity || !strings.Contains(body, `id="FocalX_error_0"`) {
			t.Fatalf("focal point %q: %d\n%s", x, status, body)
		}
	}

	stored, err := s.App.Media.Get(ctx, photo.ID)
	if err != nil {
		t.Fatal(err)
	}

	if stored.FocalPoint().X != 0.9 {
		t.Fatalf("an invalid focal point changed the stored one: %+v", stored.FocalPoint())
	}

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	status, body := s.PostStatus("/admin/pages/new?locale=en", "/admin/pages/new?locale=en", url.Values{
		"Title": {"About"}, "Slug": {"About Us"}, "Parent": {home},
	})
	if status != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "Use lowercase letters, digits and dashes for the web address.") ||
		!strings.Contains(body, `aria-invalid="true"`) {
		t.Fatalf("slug: %d\n%s", status, body)
	}

	// A date-time that cannot be read is refused, not taken as no schedule.
	about := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About"}, "Slug": {"about"}, "Parent": {home}})
	editor := about + "?locale=en"

	status, _ = s.PostStatus(editor, action(t, s, editor, schedulePattern), url.Values{"PublishAt": {"garbage"}})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("schedule garbage: %d", status)
	}

	// Required selects say what is missing; an unknown choice too.
	for _, zone := range []string{"", "Mars/Olympus"} {
		status, body = s.PostStatus("/admin/site", "/admin/site?locale=en", url.Values{
			"DefaultLocale": {"en"},
			"Locales":       {"en", "nl"},
			"Timezone":      {zone},
		})
		if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Choose a timezone.") {
			t.Fatalf("timezone %q: %d\n%s", zone, status, body)
		}
	}

	// The default language must still be ticked.
	status, body = s.PostStatus("/admin/site", "/admin/site?locale=en", url.Values{
		"DefaultLocale": {"nl"},
		"Locales":       {"en"},
		"Timezone":      {"Europe/Amsterdam"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Tick the default language under Languages as well.") {
		t.Fatalf("default language not ticked: %d\n%s", status, body)
	}
}

// TestSignInAutocomplete checks that the sign-in form tells password
// managers what its fields are.
func TestSignInAutocomplete(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t)

	s.Visitor().Expect("/admin/login", http.StatusOK, `autocomplete="username"`, `autocomplete="current-password"`)
}
