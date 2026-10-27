package stats

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"

	"github.com/donseba/go-router/middleware"
)

// TestStore runs counting, flushing, reports and retention on every
// configured dialect.
func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "stats", schema()); err != nil {
			t.Fatal(err)
		}

		clock := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
		service := newService("stats", database, nil)
		service.now = func() time.Time { return clock }

		alice := hit{
			at:       clock,
			path:     "/en/news/hello",
			title:    "Hello",
			referrer: "example.org",
			device:   "desktop",
			locale:   "en",
			address:  "192.0.2.1",
			agent:    "Firefox",
		}
		bob := alice
		bob.address = "192.0.2.2"
		bob.device = "mobile"
		bob.referrer = ""
		home := alice
		home.path = "/en/"
		home.title = "Home"
		dutch := alice
		dutch.path = "/nl/nieuws/hallo"
		dutch.locale = "nl"

		for _, h := range []hit{alice, alice, bob, home, dutch} {
			service.record(h)
		}

		// Views that are not flushed yet are counted already.
		expectViews(t, service, "/en/news/hello", 3)

		if err := service.Flush(ctx); err != nil {
			t.Fatal(err)
		}

		expectViews(t, service, "/en/news/hello", 3)

		// A second flush adds to the stored rows; Alice is no new visitor.
		service.record(alice)

		if err := service.Flush(ctx); err != nil {
			t.Fatal(err)
		}

		expectViews(t, service, "/en/news/hello", 4)

		// The next day has a new salt: Alice counts as a visitor again.
		clock = clock.AddDate(0, 0, 1)
		alice.at = clock
		service.record(alice)

		if err := service.Flush(ctx); err != nil {
			t.Fatal(err)
		}

		report, err := service.Report(ctx, 7)
		if err != nil {
			t.Fatal(err)
		}

		if report.Views != 7 || report.Visitors != 3 || len(report.Daily) != 7 {
			t.Fatalf("totals: %d views, %d visitors, %d days", report.Views, report.Visitors, len(report.Daily))
		}

		last := report.Daily[6]
		if last.Date != "2026-10-06" || last.Views != 1 || last.Visitors != 1 || report.Daily[5].Views != 6 {
			t.Fatalf("daily: %+v", report.Daily)
		}

		top := report.Pages[0]
		if top.Path != "/en/news/hello" || top.Title != "Hello" || top.Views != 5 || top.Visitors != 3 {
			t.Fatalf("pages: %+v", report.Pages)
		}

		expectCounts(t, "referrers", report.Referrers, Count{Name: "example.org", Views: 6, Visitors: 2})
		expectCounts(t, "devices", report.Devices,
			Count{Name: "desktop", Views: 6, Visitors: 2},
			Count{Name: "mobile", Views: 1, Visitors: 1},
		)
		expectCounts(t, "locales", report.Locales,
			Count{Name: "en", Views: 6, Visitors: 3},
			Count{Name: "nl", Views: 1, Visitors: 1},
		)

		// Popular lists one language without its home page.
		popular, err := service.Popular(ctx, "en", 5, 7)
		if err != nil || len(popular) != 1 || popular[0].Path != "/en/news/hello" {
			t.Fatalf("popular: %+v %v", popular, err)
		}

		// A limit of 0 lists every page.
		all, err := service.Popular(ctx, "en", 0, 7)
		if err != nil || len(all) != 1 {
			t.Fatalf("popular without a limit: %+v %v", all, err)
		}

		testPopularIn(t, database, clock)
		testLatestTitle(t, database, clock)

		// Instances do not share statistics.
		other := newService("other", database, nil)
		other.now = service.now

		empty, err := other.Report(ctx, 7)
		if err != nil || empty.Views != 0 || len(empty.Pages) != 0 {
			t.Fatalf("other instance: %+v %v", empty, err)
		}

		// Keeping one day deletes yesterday.
		if err := service.SetRetention(ctx, 1); err != nil {
			t.Fatal(err)
		}

		if days, err := service.Retention(ctx); err != nil || days != 1 {
			t.Fatalf("retention: %d %v", days, err)
		}

		report, err = service.Report(ctx, 7)
		if err != nil || report.Views != 1 || len(report.Pages) != 1 {
			t.Fatalf("after retention: %+v %v", report, err)
		}

		if err := service.SetRetention(ctx, 0); err != nil {
			t.Fatal(err)
		}
	})
}

func expectViews(t *testing.T, service *Service, path string, want int) {
	t.Helper()

	views, err := service.Views(context.Background(), path)
	if err != nil || views != want {
		t.Fatalf("views of %s: want %d, got %d (%v)", path, want, views, err)
	}
}

func expectCounts(t *testing.T, name string, got []Count, want ...Count) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("%s: want %+v, got %+v", name, want, got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: want %+v, got %+v", name, want, got)
		}
	}
}

// TestClassify checks which requests count and how they are described.
func TestClassify(t *testing.T) {
	const firefox = "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0"

	tests := []struct {
		name   string
		method string
		path   string
		header http.Header
		want   bool
	}{
		{"page", http.MethodGet, "/en/about", nil, true},
		{"boosted link", http.MethodGet, "/en/about", http.Header{"Hx-Request": {"true"}, "Hx-Boosted": {"true"}}, true},
		{"head", http.MethodHead, "/en/about", nil, false},
		{"post", http.MethodPost, "/en/about", nil, false},
		{"file", http.MethodGet, "/sitemap.xml", nil, false},
		{"prefetch", http.MethodGet, "/en/about", http.Header{"Sec-Purpose": {"prefetch;prerender"}}, false},
		{"fragment", http.MethodGet, "/en/about", http.Header{"Hx-Request": {"true"}}, false},
		{"bot", http.MethodGet, "/en/about", http.Header{"User-Agent": {"Mozilla/5.0 (compatible; Googlebot/2.1)"}}, false},
		{"no agent", http.MethodGet, "/en/about", http.Header{"User-Agent": {""}}, false},
	}

	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, nil)
		request.Header.Set("User-Agent", firefox)

		for name, values := range test.header {
			request.Header[name] = values
		}

		if got := countable(request); got != test.want {
			t.Errorf("%s: want %v, got %v", test.name, test.want, got)
		}
	}

	devices := map[string]string{
		firefox: "desktop",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) Mobile/15E148":    "mobile",
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) Chrome/129.0 Mobile Safari/537": "mobile",
		"Mozilla/5.0 (iPad; CPU OS 18_0 like Mac OS X)":                           "tablet",
		"Mozilla/5.0 (Linux; Android 14; SM-X710) Chrome/129.0 Safari/537":        "tablet",
	}

	for agent, want := range devices {
		if got := device(agent); got != want {
			t.Errorf("device of %q: want %s, got %s", agent, want, got)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "http://example.com/en/", nil)

	for from, want := range map[string]string{
		"https://www.Example.org/search?q=x": "example.org",
		"http://example.com/en/news":         "",
		"android-app://com.example":          "",
		"":                                   "",
	} {
		request.Header.Set("Referer", from)

		if got := referrer(request); got != want {
			t.Errorf("referrer %q: want %q, got %q", from, want, got)
		}
	}

	// Behind a trusted proxy the site's own host is the forwarded one.
	trust, err := middleware.TryRealIP(middleware.RealIPOptions{TrustedProxies: []string{"192.0.2.0/24"}})
	if err != nil {
		t.Fatal(err)
	}

	proxied := httptest.NewRequest(http.MethodGet, "http://internal:8080/en/", nil)
	proxied.RemoteAddr = "192.0.2.1:1234"
	proxied.Header.Set("X-Forwarded-Host", "example.com")
	proxied.Header.Set("Referer", "https://example.com/en/news")

	trust(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if got := referrer(r); got != "" {
			t.Errorf("a link within the site behind a proxy counted as %q", got)
		}
	})).ServeHTTP(httptest.NewRecorder(), proxied)

	answers := []struct {
		name   string
		header http.Header
		want   bool
	}{
		{"fresh page", http.Header{"Cache-Control": {"no-store"}}, false},
		{"cached page", http.Header{"Cache-Control": {"public, max-age=60"}}, false},
		{"nofollow", http.Header{"X-Robots-Tag": {"nofollow"}}, false},
		{"noindex", http.Header{"X-Robots-Tag": {"noindex"}}, true},
		{"crawler noindex", http.Header{"X-Robots-Tag": {"googlebot: noindex, nofollow"}}, true},
		{"none", http.Header{"X-Robots-Tag": {"none"}}, true},
		{"private", http.Header{"Cache-Control": {"private, no-store"}}, true},
	}

	for _, answer := range answers {
		if got := private(answer.header); got != answer.want {
			t.Errorf("private %s: want %v, got %v", answer.name, answer.want, got)
		}
	}

	for head, want := range map[string]bool{
		`<meta name="robots" content="noindex">`:             true,
		`<META content='NOINDEX, follow' name=robots>`:       true,
		`<meta name="robots" content="index, follow">`:       false,
		`<meta name="description" content="noindex">`:        false,
		`<meta property="og:title" content="About"><title>x`: false,
	} {
		if got := noindexMeta([]byte("<head>" + head + "</head>")); got != want {
			t.Errorf("noindex meta %s: want %v, got %v", head, want, got)
		}
	}

	head := []byte("<html><head><TITLE>\n  Fish &amp; chips\n</TITLE></head>")
	if got := title(head); got != "Fish & chips" {
		t.Errorf("title: %q", got)
	}

	for raw, want := range map[string]string{
		"About · My site":  "About",
		"About | My site":  "About",
		"My site - About":  "About",
		"About — My site":  "About",
		"My site":          "My site",
		" · My site":       " · My site",
		"About my site":    "About my site",
		"About · Our site": "About · Our site",
	} {
		if got := withoutSiteName(raw, "My site"); got != want {
			t.Errorf("without site name %q: want %q, got %q", raw, want, got)
		}
	}

	if got := withoutSiteName("About · My site", ""); got != "About · My site" {
		t.Errorf("without a site name: %q", got)
	}
}

// testLatestTitle checks that a page has the title it was last viewed with,
// not the alphabetically last one, and that a view without a title keeps it.
func testLatestTitle(t *testing.T, database *db.DB, clock time.Time) {
	t.Helper()

	ctx := context.Background()
	service := newService("titles", database, nil)
	now := clock.AddDate(0, 0, -1)
	service.now = func() time.Time { return now }

	view := func(title string) {
		t.Helper()

		service.record(hit{at: now, path: "/en/renamed", title: title, locale: "en", address: "192.0.2.1", agent: "Firefox"})

		if err := service.Flush(ctx); err != nil {
			t.Fatal(err)
		}
	}

	view("Zebra")
	view("")

	now = clock
	view("Apple")
	view("")

	found, err := service.Popular(ctx, "en", 5, 7)
	if err != nil || len(found) != 1 || found[0].Title != "Apple" || found[0].Views != 4 {
		t.Fatalf("latest title: %+v %v", found, err)
	}
}

// testPopularIn lists the pages below a path, in another instance.
func testPopularIn(t *testing.T, database *db.DB, clock time.Time) {
	t.Helper()

	ctx := context.Background()
	service := newService("blog", database, nil)
	service.now = func() time.Time { return clock }

	paths := []string{
		"/en/blog",
		"/en/blog/",
		"/en/blog/one",
		"/en/blog/two",
		"/en/blog/two",
		"/en/blogs/x",
		"/en/axb/y",
		"/nl/blog/een",
	}

	for _, path := range paths {
		service.record(hit{at: clock, path: path, title: path, locale: "en", address: "192.0.2.1", agent: "Firefox"})
	}

	if err := service.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	for within, want := range map[string]string{
		"/blog":  "/en/blog/two /en/blog/one",
		"/blog/": "/en/blog/two /en/blog/one",
		"/a_b":   "",
		"news":   "",
	} {
		found, err := service.PopularIn(ctx, "en", within, 5, 7)
		if err != nil {
			t.Fatal(err)
		}

		var got []string

		for _, page := range found {
			got = append(got, page.Path)
		}

		if strings.Join(got, " ") != want {
			t.Errorf("popular in %s: want %q, got %q", within, want, got)
		}
	}
}
