package testsuite

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

var (
	schedulePattern   = regexp.MustCompile(`action="([^"]*/schedule[^"]*)"`)
	unschedulePattern = regexp.MustCompile(`action="([^"]*/unschedule[^"]*)"`)
)

// action finds a form action on an admin page.
func action(t *testing.T, s *sitetest.Site, page string, pattern *regexp.Regexp) string {
	t.Helper()

	_, body := s.Get(page)

	match := pattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("no %s form on %s", pattern, page)
	}

	return strings.ReplaceAll(match[1], "&amp;", "&")
}

// TestScheduledPublication plans publications in the admin, in the site's
// timezone, and lets the scheduler carry them out.
func TestScheduledPublication(t *testing.T) {
	t.Parallel()

	s := sitetest.StartWith(t, sitetest.Options{StartingPoints: []string{"news"}}, publishing.Module("news", "News"))
	ctx := context.Background()

	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	settings.Timezone = "America/New_York"
	if err := s.App.Site.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}

	zone, _ := time.LoadLocation("America/New_York")
	wall := func(offset time.Duration) string {
		return time.Now().Add(offset).In(zone).Format("2006-01-02T15:04")
	}

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	s.Mount("news", "News", "news")

	launch := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"Launch"}, "Slug": {"launch"}, "Parent": {home}})
	later := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"Later"}, "Slug": {"later"}, "Parent": {home}})
	post := s.Submit("/admin/news/new?locale=en", url.Values{"Title": {"Embargo"}, "Slug": {"embargo"}, "Date": {"2026-10-05"}})

	// A minute ago in New York is due; read as UTC it would be hours away.
	for _, editor := range []string{launch, post} {
		page := editor + "?locale=en"
		s.Post(page, action(t, s, page, schedulePattern), url.Values{"PublishAt": {wall(-time.Minute)}})
	}

	s.Post(later+"?locale=en", action(t, s, later+"?locale=en", schedulePattern), url.Values{
		"PublishAt":   {wall(time.Hour)},
		"UnpublishAt": {wall(2 * time.Hour)},
	})

	// The admin shows the plan in the site's timezone.
	s.Expect(later+"?locale=en", http.StatusOK, "Scheduled publication", wall(time.Hour)[11:], "America/New_York",
		`value="`+wall(time.Hour)+`"`, `value="`+wall(2*time.Hour)+`"`)
	s.Expect("/admin/pages?locale=en", http.StatusOK, "Scheduled")

	// Unpublishing before publishing is refused.
	status, _ := s.PostStatus(later+"?locale=en", action(t, s, later+"?locale=en", schedulePattern), url.Values{
		"PublishAt":   {wall(time.Hour)},
		"UnpublishAt": {wall(time.Minute)},
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("unpublish before publish: %d", status)
	}

	visitor := s.Visitor()
	visitor.Expect("/en/launch", http.StatusNotFound)

	changes, err := s.App.RunSchedules(ctx)
	if err != nil || changes != 2 {
		t.Fatalf("run: %d %v", changes, err)
	}

	visitor.Expect("/en/launch", http.StatusOK, "<h1>Launch</h1>", `href="/en/launch">Launch`)
	visitor.Expect("/en/news/embargo", http.StatusOK, "<h1>Embargo</h1>")
	visitor.Expect("/en/later", http.StatusNotFound)
	s.ExpectMissing(launch+"?locale=en", "Scheduled publication")

	// Running again changes nothing.
	if changes, err := s.App.RunSchedules(ctx); err != nil || changes != 0 {
		t.Fatalf("second run: %d %v", changes, err)
	}

	// A plan can be cancelled.
	s.Post(later+"?locale=en", action(t, s, later+"?locale=en", unschedulePattern), url.Values{})
	s.ExpectMissing(later+"?locale=en", "Scheduled publication")

	// Unpublishing can be planned on its own.
	s.Post(
		launch+"?locale=en",
		action(t, s, launch+"?locale=en", schedulePattern),
		url.Values{"UnpublishAt": {wall(-time.Minute)}},
	)
	s.Expect(launch+"?locale=en", http.StatusOK, "Scheduled unpublication")

	if changes, err := s.App.RunSchedules(ctx); err != nil || changes != 1 {
		t.Fatalf("unpublish run: %d %v", changes, err)
	}

	visitor.Expect("/en/launch", http.StatusNotFound)
}

// TestScheduleClockChanges plans publications at times the site's clocks
// skip or show twice: a skipped time moves forward by the change, a time
// shown twice is the first of the two.
func TestScheduleClockChanges(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t)
	ctx := context.Background()

	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	settings.Timezone = "America/New_York"
	if err := s.App.Site.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		slug    string
		publish string
		shown   string
		instant string
	}{
		// New York skips 02:00–03:00 on 14 March 2027.
		{"gap", "2027-03-14T02:30", "2027-03-14T03:30", "2027-03-14T03:30:00-04:00"},
		// and shows 01:00–02:00 twice on 7 November 2027.
		{"overlap", "2027-11-07T01:30", "2027-11-07T01:30", "2027-11-07T01:30:00-04:00"},
		{"before", "2027-03-14T01:59", "2027-03-14T01:59", "2027-03-14T01:59:00-05:00"},
		{"after", "2027-11-07T02:00", "2027-11-07T02:00", "2027-11-07T02:00:00-05:00"},
	}

	for _, c := range cases {
		values := url.Values{"Title": {c.slug}, "Slug": {c.slug}, "Parent": {home}}
		page := s.Submit("/admin/pages/new?locale=en", values) + "?locale=en"
		s.Post(page, action(t, s, page, schedulePattern), url.Values{"PublishAt": {c.publish}})
		s.Expect(page, http.StatusOK, `value="`+c.shown+`"`, `datetime="`+c.instant+`"`)
	}
}
