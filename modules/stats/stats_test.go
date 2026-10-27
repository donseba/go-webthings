package stats_test

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/modules/stats"
	"github.com/donseba/go-webthings/sitetest"
)

const (
	firefox = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14.6; rv:131.0) Gecko/20100101 Firefox/131.0"
	iphone  = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148"
)

func TestStats(t *testing.T) {
	// The templates show the counts as they are: the output cache, which
	// would show them as they were for its time, is off in tests.
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, stats.Module())
	ctx := context.Background()
	service := stats.Of(s.App, "stats")

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	about := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"About"},
		"Slug":   {"about"},
		"Parent": {home},
	})
	s.Publish(about + "?locale=en")

	// The page is rendered before it is counted.
	body := visit(t, s, http.MethodGet, "/en/about", http.StatusOK, http.Header{"Referer": {"https://www.example.org/search"}})
	if !strings.Contains(body, "read 0 times, 0 by URL") {
		t.Fatalf("first view:\n%s", body)
	}

	// The same visitor again, a mobile visitor and the home page.
	visit(t, s, http.MethodGet, "/en/about", http.StatusOK, nil)
	visit(t, s, http.MethodGet, "/en/about", http.StatusOK, http.Header{
		"User-Agent":      {iphone},
		"X-Forwarded-For": {"198.51.100.7, 10.0.0.1"},
	})
	visit(t, s, http.MethodGet, "/en/", http.StatusOK, http.Header{"Referer": {"http://example.com/en/about"}})

	// None of these are a person viewing a page.
	visit(
		t,
		s,
		http.MethodGet,
		"/en/about",
		http.StatusOK,
		http.Header{"User-Agent": {"Mozilla/5.0 (compatible; Googlebot/2.1)"}},
	)
	visit(t, s, http.MethodGet, "/en/about", http.StatusOK, http.Header{"Sec-Purpose": {"prefetch"}})
	visit(t, s, http.MethodGet, "/en/about", http.StatusOK, http.Header{"Hx-Request": {"true"}})
	visit(t, s, http.MethodHead, "/en/about", http.StatusOK, nil)
	visit(t, s, http.MethodGet, "/en/missing", http.StatusNotFound, nil)
	visit(t, s, http.MethodGet, "/sitemap.xml", http.StatusOK, nil)
	visit(t, s, http.MethodGet, "/media/missing.png", http.StatusNotFound, nil)
	visit(t, s, http.MethodGet, "/admin/login", http.StatusOK, nil)
	s.Get("/en/about")

	// Unflushed views already count in templates.
	body = visit(t, s, http.MethodGet, "/en/about", http.StatusOK, nil)
	if !strings.Contains(body, "read 3 times, 3 by URL") {
		t.Fatalf("views in template:\n%s", body)
	}

	if err := service.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	report, err := service.Report(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}

	if report.Views != 5 || report.Visitors != 2 {
		t.Fatalf("totals: %d views, %d visitors", report.Views, report.Visitors)
	}

	if len(report.Referrers) != 1 || report.Referrers[0].Name != "example.org" || report.Referrers[0].Views != 1 {
		t.Fatalf("referrers: %+v", report.Referrers)
	}

	// Titles are stored without the site name of the layout.
	body = visit(t, s, http.MethodGet, "/en/about", http.StatusOK, nil)
	if !strings.Contains(body, "<li>/en/about|About|4</li>") || strings.Contains(body, "<li>/en/|") {
		t.Fatalf("popular in template:\n%s", body)
	}

	// Popular pages below a page leave out that page.
	team := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"Team"},
		"Slug":   {"team"},
		"Parent": {strings.TrimPrefix(about, "/admin/pages/")},
	})
	s.Publish(team + "?locale=en")
	visit(t, s, http.MethodGet, "/en/about/team", http.StatusOK, nil)

	if err := service.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	body = visit(t, s, http.MethodGet, "/en/about", http.StatusOK, nil)
	if !strings.Contains(body, `<ol id="below"><li>/en/about/team|Team</li></ol>`) {
		t.Fatalf("popular below a page:\n%s", body)
	}

	// Editors signed in to the admin are not counted.
	editor := signedIn(t, s)

	for range 3 {
		if response, err := editor.Get(editor.base + "/en/about"); err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("editor view: %v %v", response, err)
		} else {
			_ = response.Body.Close()
		}
	}

	// The dashboard flushes first, so it shows the last views too.
	s.Expect("/admin/stats?days=7", http.StatusOK,
		`id="stats-views">8<`,
		`id="stats-visitors">2<`,
		`<a class="break-all" href="/en/about"`,
		`<span class="block muted text-xs">About</span>`,
		"example.org",
		"Desktop",
		"Mobile",
		`aria-current="page">7 days`,
		`class="bar bar-100"`,
		`<progress class="meter mt-1" max="100" value="100">`,
	)

	s.Post("/admin/stats", "/admin/stats/settings", url.Values{"Retention": {"30"}})

	if days, err := service.Retention(ctx); err != nil || days != 30 {
		t.Fatalf("retention: %d %v", days, err)
	}

	s.Expect("/admin/stats", http.StatusOK, `value="30"`)

	status, _ := s.PostStatus("/admin/stats", "/admin/stats/settings", url.Values{"Retention": {"-1"}})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("negative retention: %d", status)
	}
}

// browser is an HTTP client with cookies and a browser's user agent.
type browser struct {
	*http.Client

	base string
}

// signedIn returns a browser that signed in to the admin of s.
func signedIn(t *testing.T, s *sitetest.Site) browser {
	t.Helper()

	server := httptest.NewServer(s.App)
	t.Cleanup(server.Close)

	jar, _ := cookiejar.New(nil)
	client := browser{Client: &http.Client{Jar: jar, Transport: agent{}}, base: server.URL}

	response, err := client.Get(server.URL + "/admin/login")
	if err != nil {
		t.Fatal(err)
	}

	page, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()

	if !strings.Contains(string(page), `action="/admin/login"`) {
		t.Fatalf("no login form:\n%s", page)
	}

	response, err = client.PostForm(server.URL+"/admin/login", url.Values{
		"Email":    {"admin@example.test"},
		"Password": {"test-only long passphrase"},
	})
	if err != nil {
		t.Fatal(err)
	}

	_ = response.Body.Close()

	return client
}

// agent sends a browser's user agent, so requests are not taken for a bot.
type agent struct{}

func (agent) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("User-Agent", firefox)

	return http.DefaultTransport.RoundTrip(request)
}

// visit requests path as a visitor's browser and returns the body.
func visit(t *testing.T, s *sitetest.Site, method, path string, status int, header http.Header) string {
	t.Helper()

	request := httptest.NewRequest(method, path, nil)
	request.RemoteAddr = "192.0.2.10:52000"
	request.Header.Set("User-Agent", firefox)

	for name, values := range header {
		request.Header[http.CanonicalHeaderKey(name)] = values
	}

	response := httptest.NewRecorder()
	s.App.ServeHTTP(response, request)

	if response.Code != status {
		t.Fatalf("%s %s: want %d, got %d\n%s", method, path, status, response.Code, response.Body)
	}

	return response.Body.String()
}

// TestNamed checks the default key and title and how Named changes them.
func TestNamed(t *testing.T) {
	module := stats.Module()
	if module.Key != "stats" || module.Title != "Statistics" {
		t.Fatalf("default: %q %q", module.Key, module.Title)
	}

	module = stats.Module(stats.Named("visits", "Visits"))
	if module.Key != "visits" || module.Title != "Visits" {
		t.Fatalf("named: %q %q", module.Key, module.Title)
	}
}

// TestStatsCachedPages checks that pages the output cache answers count as
// well, compressed or not, with their title.
func TestStatsCachedPages(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{
		Templates: "testdata/templates",
		Configure: func(config *webthings.Config) { config.OutputCacheTTL = time.Minute },
	}, stats.Module())
	ctx := context.Background()
	service := stats.Of(s.App, "stats")

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	about := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"About"},
		"Slug":   {"about"},
		"Parent": {home},
	})
	s.Publish(about + "?locale=en")

	for _, encoding := range []string{"gzip", "", "gzip", ""} {
		visit(t, s, http.MethodGet, "/en/about", http.StatusOK, http.Header{"Accept-Encoding": {encoding}})
	}

	if err := service.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	report, err := service.Report(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}

	if report.Views != 4 || len(report.Pages) != 1 || report.Pages[0].Title != "About" {
		t.Fatalf("views %d, pages %+v", report.Views, report.Pages)
	}
}
