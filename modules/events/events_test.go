package events_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/sitetest"
)

func TestEvents(t *testing.T) {
	s := sitetest.StartWith(
		t,
		sitetest.Options{Templates: "testdata/templates"},
		events.Module("agenda", "Agenda"),
		events.Module("unlisted", "Unlisted"),
	)

	// West of UTC, times in UTC move to the previous day; events in "UTC"
	// and all-day events must not.
	settings, err := s.App.Site.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	settings.Timezone = "America/Chicago"
	if err := s.App.Site.Save(context.Background(), settings); err != nil {
		t.Fatal(err)
	}

	day := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)
	lastYear := time.Now().AddDate(-1, 0, 0).Format(time.DateOnly)

	fair := s.Submit("/admin/agenda/new?locale=en", url.Values{
		"Title":    {"Summer fair"},
		"Slug":     {"summer-fair"},
		"Start":    {day + "T14:30"},
		"End":      {day + "T16:00"},
		"Timezone": {"Europe/Amsterdam"},
		"Summary":  {"Games and food"},
		"Body":     {"Bring *friends*"},
		"Location": {"Town hall"},
		"Address":  {"Main street 1"},
	})

	// The editor shows the times in the event's timezone, not the site's.
	s.Expect(fair+"?locale=en", http.StatusOK, `value="`+day+`T14:30"`, `value="`+day+`T16:00"`)

	market := s.Submit("/admin/agenda/new?locale=en", url.Values{
		"Title":    {"Old market"},
		"Slug":     {"old-market"},
		"Start":    {lastYear + "T09:00"},
		"AllDay":   {"on"},
		"Timezone": {"UTC"},
	})

	s.Mount("agenda", "Agenda", "agenda")
	s.Expect("/en/agenda", http.StatusOK, "<h1>Agenda</h1>")
	s.ExpectMissing("/en/agenda", "summer-fair")

	s.Publish(fair + "?locale=en")
	s.Publish(market + "?locale=en")

	s.Expect("/en/agenda", http.StatusOK, `href="/en/agenda/summer-fair">Summer fair`, day+" 14:30")
	s.ExpectMissing("/en/agenda", "old-market")
	s.Expect("/en/agenda/past", http.StatusOK, `href="/en/agenda/old-market">Old market`)
	s.ExpectMissing("/en/agenda/past", "summer-fair")

	// Times are shown in the event's own timezone.
	s.Expect("/en/agenda/summer-fair", http.StatusOK,
		"<em>friends</em>",
		day+" 14:30 – "+day+" 16:00 Europe/Amsterdam",
		"Town hall, Main street 1",
		`hreflang="en"`,
	)
	s.Expect("/en/agenda/old-market", http.StatusOK, lastYear+" 00:00 UTC all-day")

	marketDay, err := time.Parse(time.DateOnly, lastYear)
	if err != nil {
		t.Fatal(err)
	}

	s.Expect("/en/agenda/old-market", http.StatusOK, `<p class="local">`+frontend.FormatDate("en", marketDay, "full")+`</p>`)
	s.Expect("/en/agenda/nope", http.StatusNotFound)

	upcoming, err := events.Of(s.App, "agenda").Upcoming(context.Background(), "en", 5)
	if err != nil || len(upcoming) != 1 || upcoming[0].URL != "/en/agenda/summer-fair" {
		t.Fatalf("upcoming: %+v %v", upcoming, err)
	}

	// A limit of 0 or less is no limit.
	for _, limit := range []int{0, -1} {
		all, err := events.Of(s.App, "agenda").Upcoming(context.Background(), "en", limit)
		if err != nil || len(all) != 1 {
			t.Fatalf("limit %d: %+v %v", limit, all, err)
		}
	}

	// The sitemap lists every published event and the past events.
	s.Expect("/sitemap.xml", http.StatusOK,
		"/en/agenda/summer-fair</loc>",
		"/en/agenda/old-market</loc>",
		"/en/agenda/past</loc>",
	)

	// The list title is the published page's, not a newer draft's.
	s.DraftPage("Agenda", "Agenda (draft)", "agenda")
	s.Expect("/en/agenda", http.StatusOK, "<h1>Agenda</h1>")
	s.ExpectMissing("/en/agenda", "Agenda (draft)")

	// Dates are written in the page's language.
	start := upcoming[0].Start
	s.Expect(
		"/en/agenda/summer-fair",
		http.StatusOK,
		`<p class="local">`+frontend.FormatDate(
			"en",
			start,
			"full",
		)+` · `+start.Format(
			"15:04",
		)+`–`+upcoming[0].End.Format(
			"15:04",
		)+`</p>`,
	)

	// Other modules read every event, published or not.
	draft := s.Submit("/admin/agenda/new?locale=en", url.Values{
		"Title":    {"Planning"},
		"Slug":     {"planning"},
		"Start":    {day + "T10:00"},
		"Timezone": {"Europe/Amsterdam"},
	})
	draftID := strings.TrimPrefix(draft, "/admin/agenda/")
	service := events.Of(s.App, "agenda")

	list, err := service.Events(context.Background(), "nl")
	if err != nil || len(list) != 3 {
		t.Fatalf("events: %+v %v", list, err)
	}

	byID := map[string]events.Info{}
	for _, info := range list {
		byID[info.ID] = info
	}

	if fairInfo := byID[strings.TrimPrefix(fair, "/admin/agenda/")]; fairInfo.Title != "Summer fair" || fairInfo.Published ||
		fairInfo.Past {
		t.Fatalf("fair in Dutch (not translated, so not visible): %+v", fairInfo)
	}

	if planning := byID[draftID]; planning.Published || planning.URL != "" || planning.Start.Format("15:04") != "10:00" {
		t.Fatalf("draft: %+v", planning)
	}

	fairInfo, err := service.Event(context.Background(), strings.TrimPrefix(fair, "/admin/agenda/"), "en")
	if err != nil || !fairInfo.Published || fairInfo.URL != "/en/agenda/summer-fair" || fairInfo.End.IsZero() {
		t.Fatalf("event: %+v %v", fairInfo, err)
	}

	marketInfo, err := service.Event(context.Background(), strings.TrimPrefix(market, "/admin/agenda/"), "en")
	if err != nil || !marketInfo.Past || !marketInfo.AllDay {
		t.Fatalf("past event: %+v %v", marketInfo, err)
	}

	if _, err := service.Event(context.Background(), "missing", "en"); err == nil {
		t.Fatal("missing event found")
	}

	// An agenda on no page has events without addresses.
	concert := s.Submit("/admin/unlisted/new?locale=en", url.Values{
		"Title":    {"Concert"},
		"Slug":     {"concert"},
		"Start":    {day + "T20:00"},
		"Timezone": {"UTC"},
	})
	s.Publish(concert + "?locale=en")

	unlisted, err := events.Of(s.App, "unlisted").Upcoming(context.Background(), "en", -1)
	if err != nil || len(unlisted) != 1 || unlisted[0].URL != "" {
		t.Fatalf("unmounted: %+v %v", unlisted, err)
	}

	// Other modules see it without an address too.
	infos, err := events.Of(s.App, "unlisted").Events(context.Background(), "en")
	if err != nil || len(infos) != 1 || infos[0].Title != "Concert" || infos[0].URL != "" {
		t.Fatalf("unmounted infos: %+v %v", infos, err)
	}

	one, err := events.Of(s.App, "unlisted").Event(context.Background(), strings.TrimPrefix(concert, "/admin/unlisted/"), "en")
	if err != nil || one.Title != "Concert" || one.URL != "" {
		t.Fatalf("unmounted info: %+v %v", one, err)
	}

	// An event in "UTC" is shown at its own time, not in the site's timezone.
	format := frontend.Format{Locale: "en", Zone: time.FixedZone("west", -6*60*60)}
	if got := format.Time(unlisted[0].Start); got != "20:00" {
		t.Fatalf("UTC event at %q", got)
	}
}

// TestEventsDST checks that the editor and the website show an event's times
// in its own timezone also when they fall in a gap of the site's timezone,
// and how times in a gap or overlap of the event's own timezone are read.
func TestEventsDST(t *testing.T) {
	s := sitetest.StartWith(
		t,
		sitetest.Options{Templates: "testdata/templates"},
		events.Module("agenda", "Agenda"),
	)

	settings, err := s.App.Site.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	settings.Timezone = "Europe/Amsterdam"
	if err := s.App.Site.Save(context.Background(), settings); err != nil {
		t.Fatal(err)
	}

	s.Mount("agenda", "Agenda", "agenda")
	service := events.Of(s.App, "agenda")

	cases := []struct {
		name     string
		start    string
		end      string
		timezone string
		// shown is the start the editor and the website show, wantStart
		// the instant that is stored.
		shown     string
		wantStart time.Time
	}{
		{
			// 02:30 does not exist in Amsterdam on 29 March 2026, but does
			// in New York.
			name:      "site-gap",
			start:     "2026-03-29T02:30",
			end:       "2026-03-29T04:00",
			timezone:  "America/New_York",
			shown:     "2026-03-29T02:30",
			wantStart: time.Date(2026, 3, 29, 6, 30, 0, 0, time.UTC),
		},
		{
			// 02:30 occurs twice in Amsterdam on 25 October 2026.
			name:      "site-overlap",
			start:     "2026-10-25T02:30",
			end:       "2026-10-25T04:00",
			timezone:  "America/New_York",
			shown:     "2026-10-25T02:30",
			wantStart: time.Date(2026, 10, 25, 6, 30, 0, 0, time.UTC),
		},
		{
			// 02:30 does not exist in New York on 8 March 2026: it is
			// moved forward by the gap, to 03:30 summer time.
			name:      "event-gap",
			start:     "2026-03-08T02:30",
			end:       "2026-03-08T05:00",
			timezone:  "America/New_York",
			shown:     "2026-03-08T03:30",
			wantStart: time.Date(2026, 3, 8, 7, 30, 0, 0, time.UTC),
		},
		{
			// 01:30 occurs twice in New York on 1 November 2026: the first
			// one, in summer time, is meant.
			name:      "event-overlap-ny",
			start:     "2026-11-01T01:30",
			end:       "2026-11-01T05:00",
			timezone:  "America/New_York",
			shown:     "2026-11-01T01:30",
			wantStart: time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC),
		},
		{
			// 02:30 occurs twice in Amsterdam on 25 October 2026: the first
			// one, in summer time, is meant.
			name:      "event-overlap-amsterdam",
			start:     "2026-10-25T02:30",
			end:       "2026-10-25T05:00",
			timezone:  "Europe/Amsterdam",
			shown:     "2026-10-25T02:30",
			wantStart: time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC),
		},
		{
			// The site's own gap for an event in the site's timezone.
			name:      "amsterdam-gap",
			start:     "2026-03-29T02:30",
			end:       "2026-03-29T05:00",
			timezone:  "Europe/Amsterdam",
			shown:     "2026-03-29T03:30",
			wantStart: time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC),
		},
	}

	for _, c := range cases {
		edit := s.Submit("/admin/agenda/new?locale=en", url.Values{
			"Title":    {c.name},
			"Slug":     {c.name},
			"Start":    {c.start},
			"End":      {c.end},
			"Timezone": {c.timezone},
		})

		info, err := service.Event(context.Background(), strings.TrimPrefix(edit, "/admin/agenda/"), "en")
		if err != nil {
			t.Fatal(err)
		}

		if !info.Start.Equal(c.wantStart) {
			t.Errorf("%s: start %s, want %s", c.name, info.Start.UTC(), c.wantStart)
		}

		// The editor shows the start as it was read and says how such times
		// are read; saving the form unchanged keeps it.
		s.Expect(edit+"?locale=en", http.StatusOK, `value="`+c.shown+`"`, `value="`+c.end+`"`,
			"a time they skip moves forward by the change")

		s.Submit(edit+"?locale=en", url.Values{
			"Title":    {c.name},
			"Slug":     {c.name},
			"Start":    {c.shown},
			"End":      {c.end},
			"Timezone": {c.timezone},
		})

		again, err := service.Event(context.Background(), info.ID, "en")
		if err != nil {
			t.Fatal(err)
		}

		if !again.Start.Equal(c.wantStart) {
			t.Errorf("%s: start after saving again %s, want %s", c.name, again.Start.UTC(), c.wantStart)
		}

		s.Publish(edit + "?locale=en")
		s.Expect("/en/agenda/"+c.name, http.StatusOK, strings.Replace(c.shown, "T", " ", 1)+" – ")
	}

	// A start or end that is no date-time is refused at its field.
	for field, value := range map[string]string{"Start": "tomorrow", "End": "2026-13-01T10:00"} {
		values := url.Values{
			"Title":    {"Malformed"},
			"Slug":     {"malformed"},
			"Start":    {"2026-06-01T10:00"},
			"Timezone": {"Europe/Amsterdam"},
		}
		values.Set(field, value)

		status, body := s.PostStatus("/admin/agenda/new?locale=en", "/admin/agenda/new?locale=en", values)
		if status != http.StatusUnprocessableEntity || !strings.Contains(body, `aria-invalid="true"`) ||
			strings.Contains(body, "The date or time is not valid.") {
			t.Fatalf("%s %q: %d\n%s", field, value, status, body)
		}
	}
}

// TestEventsStartingPoints checks that the generated templates show an
// event in one unambiguous line: one that runs over several days with the
// day and time of its start and its end, one on one day with its times,
// and an all-day event with its days.
func TestEventsStartingPoints(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{StartingPoints: []string{"agenda"}}, events.Module("agenda", "Agenda"))

	start := time.Date(time.Now().Year()+1, time.March, 6, 0, 0, 0, 0, time.UTC)
	format := frontend.Format{Locale: "en"}
	first := start.Format(time.DateOnly)
	second := start.AddDate(0, 0, 1).Format(time.DateOnly)

	cases := []struct {
		slug   string
		values url.Values
		want   string
	}{
		{
			"weekend",
			url.Values{"Start": {first + "T20:00"}, "End": {second + "T02:00"}},
			format.Date(start, "full") + ", 20:00 – " + format.Date(start.AddDate(0, 0, 1), "full") + ", 02:00",
		},
		{
			"evening",
			url.Values{"Start": {first + "T19:00"}, "End": {first + "T21:00"}},
			format.Date(start, "full") + " · 19:00–21:00",
		},
		{
			"fair",
			url.Values{"Start": {first + "T00:00"}, "End": {second + "T00:00"}, "AllDay": {"on"}},
			format.DateRange(start, start.AddDate(0, 0, 1), "full"),
		},
	}

	s.Mount("agenda", "Agenda", "agenda")

	for _, c := range cases {
		c.values.Set("Title", c.slug)
		c.values.Set("Slug", c.slug)
		c.values.Set("Timezone", "UTC")

		s.Publish(s.Submit("/admin/agenda/new?locale=en", c.values) + "?locale=en")

		s.Expect("/en/agenda/"+c.slug, http.StatusOK, c.want)
		s.Expect("/en/agenda", http.StatusOK, c.want)
	}
}

// TestWebAddressFromTitle checks that an event saved with an empty web
// address gets one made from its title, as pages do.
func TestWebAddressFromTitle(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, events.Module("agenda", "Agenda"))

	s.Expect("/admin/agenda/new?locale=en", http.StatusOK, "Web address", `aria-describedby="Slug_description"`)

	day := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)
	open := s.Submit("/admin/agenda/new?locale=en", url.Values{
		"Title":    {"Open studio"},
		"Start":    {day + "T14:30"},
		"Timezone": {"UTC"},
	})
	s.Publish(open + "?locale=en")

	s.Mount("agenda", "Agenda", "agenda")
	s.Expect("/en/agenda/open-studio", http.StatusOK, "Open studio")
}
