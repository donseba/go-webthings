package frontend

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/feed"

	"github.com/donseba/go-router"
)

func buildNothing(*http.Request, int) (feed.Feed, error) {
	return feed.Feed{}, nil
}

// TestServeFeeds checks the routes of a module's feeds: named by the feed's
// name and the format, below the page at its path with the format's
// extension, and refused when they cannot be served.
func TestServeFeeds(t *testing.T) {
	f := &Frontend{}
	routes := router.NewSite()
	routes.Get("/{$}", http.NotFound).As("list")
	routes.Get("/category/{slug}", http.NotFound).As("category")

	err := f.ServeFeeds("news", routes, []Feed{
		{Build: buildNothing},
		{Name: "category-feed", Path: "/category/{slug}/feed", Page: "category", Build: buildNothing},
	})
	if err != nil {
		t.Fatal(err)
	}

	page := routes.At("/en/news")

	for name, want := range map[string]string{
		"feed.atom":          "/en/news/feed.atom",
		"feed.rss":           "/en/news/feed.rss",
		"category-feed.atom": "/en/news/category/clay/feed.atom",
		"category-feed.rss":  "/en/news/category/clay/feed.rss",
	} {
		got, err := page.RoutePathArgs(name, "slug", "clay")
		if strings.HasPrefix(name, "feed.") {
			got, err = page.RoutePathArgs(name)
		}

		if err != nil || got != want {
			t.Fatalf("%s: %q %v, want %q", name, got, err, want)
		}
	}

	cases := map[string][]Feed{
		"no build":           {{}},
		"relative path":      {{Path: "feed", Build: buildNothing}},
		"wildcard last":      {{Path: "/{slug}", Page: "category", Build: buildNothing}},
		"wildcards no page":  {{Path: "/category/{slug}/feed", Build: buildNothing}},
		"unknown page":       {{Path: "/x/{slug}/feed", Page: "missing", Build: buildNothing}},
		"name in use":        {{Path: "/other", Build: buildNothing}},
		"path in use":        {{Name: "other", Build: buildNothing}},
		"trailing slash":     {{Path: "/feed/", Build: buildNothing}},
		"one fine, one used": {{Name: "fine", Path: "/fine", Build: buildNothing}, {Build: buildNothing}},
	}

	for name, feeds := range cases {
		if err := f.ServeFeeds("news", routes, feeds); err == nil {
			t.Fatalf("%s: no error", name)
		}
	}
}

func TestWildcards(t *testing.T) {
	got := wildcards("/{$}/category/{slug}/x/{rest...}/feed")
	if !slices.Equal(got, []string{"slug", "rest"}) {
		t.Fatal(got)
	}
}

// TestTagURI checks that the IDs of entries are on the site's host, without
// its port, from BaseURL or else the request.
func TestTagURI(t *testing.T) {
	date := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8090/en/news/feed.atom", nil)

	if got := (&Frontend{}).TagURI(r, date, "news/1"); got != "tag:127.0.0.1,2026-10-07:news/1" {
		t.Fatal(got)
	}

	f := &Frontend{baseURL: "https://Example.com:8443/"}
	if got := f.TagURI(r, date, "news/1"); got != "tag:example.com,2026-10-07:news/1" {
		t.Fatal(got)
	}
}

// TestFeedDates checks that the date of a feed never goes back: the same
// body keeps its date, a new body gets the clock's second, or a second
// after the one before within the same second, also when its Updated time
// is older, and a later Updated time counts.
func TestFeedDates(t *testing.T) {
	clock := time.Date(2026, 10, 7, 12, 0, 0, 500_000_000, time.UTC)
	second := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	dates := newFeedDates()
	dates.now = func() time.Time {
		return clock
	}

	newest := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	older := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	if got := dates.modified("/en/news/feed.atom", []byte("two posts"), newest); !got.Equal(second) {
		t.Fatalf("first: %v", got)
	}

	clock = clock.Add(time.Minute)

	if got := dates.modified("/en/news/feed.atom", []byte("two posts"), newest); !got.Equal(second) {
		t.Fatalf("same body: %v", got)
	}

	if got := dates.modified("/en/news/feed.atom", []byte("one post"), older); !got.Equal(second.Add(time.Minute)) {
		t.Fatalf("newest taken off: %v", got)
	}

	again := dates.modified("/en/news/feed.atom", []byte("two posts"), newest)
	if !again.Equal(second.Add(time.Minute + time.Second)) {
		t.Fatalf("a new body in the same second: %v", again)
	}

	if got := dates.modified("/en/news/feed.rss", []byte("two posts"), newest); !got.Equal(second.Add(time.Minute)) {
		t.Fatalf("another address: %v", got)
	}

	ahead := clock.Add(time.Hour)
	if got := dates.modified("/en/news/feed.rss", []byte("ahead"), ahead); !got.Equal(ahead.Truncate(time.Second)) {
		t.Fatalf("a later Updated time: %v", got)
	}

	for n := range feedDateLimit {
		dates.modified(fmt.Sprintf("/en/news/category/%d/feed.atom", n), []byte("posts"), older)
	}

	if len(dates.dates) > feedDateLimit {
		t.Fatalf("%d dates kept", len(dates.dates))
	}
}
