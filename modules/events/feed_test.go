package events_test

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/feed"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/sitetest"
)

// TestFeed checks the feed of upcoming events: the coming events soonest
// first, when and where in the summary, and not the past ones.
func TestFeed(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, events.Module("agenda", "Agenda"))

	later := time.Now().AddDate(0, 2, 0)
	sooner := time.Now().AddDate(0, 1, 0)
	lastYear := time.Now().AddDate(-1, 0, 0)

	for _, event := range []struct {
		title, slug string
		start       time.Time
	}{
		{"Winter fair", "winter-fair", later},
		{"Summer fair", "summer-fair", sooner},
		{"Old market", "old-market", lastYear},
	} {
		editor := s.Submit("/admin/agenda/new?locale=en", url.Values{
			"Title":    {event.title},
			"Slug":     {event.slug},
			"Start":    {event.start.Format(time.DateOnly) + "T14:30"},
			"End":      {event.start.Format(time.DateOnly) + "T16:00"},
			"Timezone": {"Europe/Amsterdam"},
			"Summary":  {"Games and food"},
			"Location": {"Town hall"},
		})
		s.Publish(editor + "?locale=en")
	}

	s.Mount("agenda", "Agenda", "agenda")

	response, body := s.Visitor().Response("/en/agenda/feed.atom")
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != feed.Atom.ContentType() {
		t.Fatalf("%d %v\n%s", response.StatusCode, response.Header, body)
	}

	var document struct {
		Title   string `xml:"title"`
		Entries []struct {
			Title   string `xml:"title"`
			Summary string `xml:"summary"`
		} `xml:"entry"`
	}

	if err := xml.Unmarshal([]byte(body), &document); err != nil {
		t.Fatal(err)
	}

	if document.Title != "Agenda from Test site" || len(document.Entries) != 2 ||
		document.Entries[0].Title != "Summer fair" || document.Entries[1].Title != "Winter fair" {
		t.Fatalf("feed: %+v", document)
	}

	summary := document.Entries[0].Summary
	if !strings.Contains(summary, "14:30") || !strings.Contains(summary, " · Town hall") ||
		!strings.HasSuffix(summary, "Games and food") {
		t.Fatalf("summary %q", summary)
	}

	// The agenda's pages link to it.
	s.Visitor().Expect("/en/agenda", http.StatusOK, "/en/agenda/feed.rss")
}

// TestFeedSize checks that WithFeedSize sets how many events the feed
// lists, rather than the site setting.
func TestFeedSize(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"},
		events.Module("agenda", "Agenda", events.WithFeedSize(1)))

	for i, slug := range []string{"first-fair", "second-fair"} {
		start := time.Now().AddDate(0, i+1, 0)
		editor := s.Submit("/admin/agenda/new?locale=en", url.Values{
			"Title":    {slug},
			"Slug":     {slug},
			"Start":    {start.Format(time.DateOnly) + "T14:30"},
			"Timezone": {"Europe/Amsterdam"},
		})
		s.Publish(editor + "?locale=en")
	}

	s.Mount("agenda", "Agenda", "agenda")

	_, body := s.Visitor().Response("/en/agenda/feed.atom")

	var document struct {
		Entries []struct {
			Title string `xml:"title"`
		} `xml:"entry"`
	}

	if err := xml.Unmarshal([]byte(body), &document); err != nil {
		t.Fatalf("%v\n%s", err, body)
	}

	if len(document.Entries) != 1 || document.Entries[0].Title != "first-fair" {
		t.Fatalf("feed: %+v", document)
	}
}
