package banners_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/modules/banners"
	"github.com/donseba/go-webthings/sitetest"
)

func TestBanners(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"},
		banners.Module("hero", "Hero"),
		banners.Module("notice", "Notices"),
	)

	yesterday := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02T15:04")
	tomorrow := time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02T15:04")

	// A notice in the layout, shown through {{with banner "notice"}}.
	notice := s.Submit("/admin/notice/new?locale=en", url.Values{
		"Title": {"Closed on Monday"},
		"Body":  {"See the **opening hours**."},
		"Tone":  {"warning"},
	})

	s.ExpectMissing("/en/", "Closed on Monday")
	s.Publish(notice + "?locale=en")
	s.Expect("/en/", http.StatusOK, `<div id="notice" class="notice-warning">Closed on Monday`, "<strong>opening hours</strong>")

	// Without a Dutch title the notice is not shown in Dutch.
	s.ExpectMissing("/nl/", "Closed on Monday")

	// Hero slides: one now, one that ended and one that has not started yet.
	first := s.Submit("/admin/hero/new?locale=en", url.Values{
		"Title":       {"Welcome"},
		"Subtitle":    {"To our neighbourhood"},
		"ButtonLabel": {"Contact us"},
		"ButtonURL":   {"/en/contact"},
		"Tone":        {"info"},
		"Starts":      {yesterday},
	})
	ended := s.Submit("/admin/hero/new?locale=en", url.Values{
		"Title": {"Summer fair"},
		"Tone":  {"info"},
		"Ends":  {yesterday},
	})
	upcoming := s.Submit("/admin/hero/new?locale=en", url.Values{
		"Title":  {"Winter market"},
		"Tone":   {"info"},
		"Starts": {tomorrow},
	})
	second := s.Submit("/admin/hero/new?locale=en", url.Values{
		"Title":       {"  Volunteers wanted "},
		"ButtonLabel": {"Call us"},
		"ButtonURL":   {"tel:+31201234567"},
		"Tone":        {"success"},
	})

	for _, editor := range []string{first, ended, upcoming, second} {
		s.Publish(editor + "?locale=en")
	}

	s.Expect("/en/", http.StatusOK,
		`Welcome · To our neighbourhood <a class="button" href="/en/contact">Contact us</a>`,
		// Titles are trimmed and phone links are no #ZgotmplZ.
		`<section class="slide">Volunteers wanted ·  <a class="button" href="tel:&#43;31201234567">Call us</a>`,
	)
	s.ExpectMissing("/en/", "Summer fair")
	s.ExpectMissing("/en/", "Winter market")

	// The admin tells which banners are outside their window.
	s.Expect(
		"/admin/hero?locale=en",
		http.StatusOK,
		"Summer fair</a>",
		"· Expired</span>",
		"Winter market</a>",
		"· Upcoming</span>",
	)

	// Moving the last slide up changes the order on the website.
	s.Post("/admin/hero?locale=en", second+"/up?locale=en", url.Values{})
	s.Post("/admin/hero?locale=en", second+"/up?locale=en", url.Values{})
	s.Post("/admin/hero?locale=en", second+"/up?locale=en", url.Values{})

	_, body := s.Get("/en/")
	if strings.Index(body, "Volunteers wanted") > strings.Index(body, "Welcome") {
		t.Fatalf("order on the website:\n%s", body)
	}

	active, err := banners.Of(s.App, "hero").Active(context.Background(), "en")
	if err != nil || len(active) != 2 || active[0].Title != "Volunteers wanted" || active[1].Starts.IsZero() {
		t.Fatalf("active: %+v %v", active, err)
	}

	// Unsafe button addresses and inverted windows are refused.
	status, body := s.PostStatus("/admin/hero/new?locale=en", "/admin/hero/new?locale=en", url.Values{
		"Title":     {"Click"},
		"Tone":      {"info"},
		"ButtonURL": {"javascript:alert(1)"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "The button address must be") {
		t.Fatalf("unsafe button address: %d\n%s", status, body)
	}

	status, body = s.PostStatus("/admin/hero/new?locale=en", "/admin/hero/new?locale=en", url.Values{
		"Title":  {"Backwards"},
		"Tone":   {"info"},
		"Starts": {tomorrow},
		"Ends":   {yesterday},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Show until must be after show from.") {
		t.Fatalf("inverted window: %d\n%s", status, body)
	}

	// Banners are not offered as page modules.
	_, body = s.Get("/admin/pages/new?locale=en")
	if strings.Contains(body, `value="hero"`) {
		t.Fatalf("banners can be mounted:\n%s", body)
	}
}
