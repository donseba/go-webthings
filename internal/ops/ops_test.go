package ops

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-partial/exp/metrics"
	"github.com/donseba/go-router/middleware"
)

var page = strings.Repeat("<p>Hello, visitor.</p>\n", 200)

func serve(handler http.Handler, r *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, r)

	return response
}

// class is the route class of a path, as App gives it for the admin at
// /admin and its theme files.
func class(r *http.Request) string {
	switch {
	case strings.HasPrefix(r.URL.Path, "/admin/"):
		return "admin"
	case strings.HasPrefix(r.URL.Path, "/theme/"):
		return "asset"
	default:
		return "public"
	}
}

// TestPageCache checks the ETag, 304 and Cache-Control of public pages.
func TestPageCache(t *testing.T) {
	html := func(headers ...string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")

			for i := 0; i+1 < len(headers); i += 2 {
				w.Header().Set(headers[i], headers[i+1])
			}

			_, _ = io.WriteString(w, page)
		})
	}

	revalidate := func(*http.Request) CachePolicy { return CachePolicy{} }
	keep := func(*http.Request) CachePolicy { return CachePolicy{MaxAge: 60, Stale: 600} }

	first := serve(PageCache(revalidate)(html()), httptest.NewRequest(http.MethodGet, "/en/", nil))

	etag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || !strings.HasPrefix(etag, `W/"`) || first.Header().Get("Cache-Control") != "no-cache" ||
		first.Body.String() != page {
		t.Fatalf("%d %v", first.Code, first.Header())
	}

	again := httptest.NewRequest(http.MethodGet, "/en/", nil)
	again.Header.Set("If-None-Match", `"other", `+strings.TrimPrefix(etag, "W/"))

	second := serve(PageCache(revalidate)(html()), again)
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 || second.Header().Get("ETag") != etag {
		t.Fatalf("%d %v %q", second.Code, second.Header(), second.Body.String())
	}

	kept := serve(PageCache(keep)(html()), httptest.NewRequest(http.MethodGet, "/en/", nil))
	if got := kept.Header().Get("Cache-Control"); got != "public, max-age=60, stale-while-revalidate=600" {
		t.Fatalf("cache control %q", got)
	}

	query := serve(PageCache(keep)(html()), httptest.NewRequest(http.MethodGet, "/en/?comment=sent", nil))
	if got := query.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("cache control with a query %q", got)
	}

	own := serve(PageCache(keep)(html("Cache-Control", "public, max-age=5")), httptest.NewRequest(http.MethodGet, "/en/", nil))
	if got := own.Header().Get("Cache-Control"); got != "public, max-age=5" || own.Header().Get("ETag") == "" {
		t.Fatalf("the handler's cache control was replaced: %v", own.Header())
	}

	for name, h := range map[string]http.Handler{
		"no-store": html("Cache-Control", "no-store"),
		"private":  html("Cache-Control", "private, no-store"),
		"cookie":   html("Set-Cookie", "a=b"),
		"json": http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{}`)
		}),
		"not found": http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, page)
		}),
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/en/", nil)
			r.Header.Set("If-None-Match", "*")

			response := serve(PageCache(keep)(h), r)
			if response.Header().Get("ETag") != "" || response.Code == http.StatusNotModified ||
				strings.Contains(response.Header().Get("Cache-Control"), "max-age=60") {
				t.Fatalf("%d %v", response.Code, response.Header())
			}
		})
	}

	post := serve(PageCache(keep)(html()), httptest.NewRequest(http.MethodPost, "/en/", nil))
	if post.Header().Get("ETag") != "" {
		t.Fatalf("a post got an ETag: %v", post.Header())
	}
}

// TestPageCacheFeeds checks that Atom and RSS feeds get an ETag and the
// pages' Cache-Control, and that a feed that is not cacheable as a page
// would not be, such as a private one, does not.
func TestPageCacheFeeds(t *testing.T) {
	keep := func(*http.Request) CachePolicy { return CachePolicy{MaxAge: 60} }

	feed := func(contentType, cacheControl string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", contentType)

			if cacheControl != "" {
				w.Header().Set("Cache-Control", cacheControl)
			}

			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><feed></feed>`)
		})
	}

	for _, contentType := range []string{"application/atom+xml; charset=utf-8", "application/rss+xml; charset=utf-8"} {
		response := serve(PageCache(keep)(feed(contentType, "")), httptest.NewRequest(http.MethodGet, "/en/news/feed.atom", nil))
		if response.Header().Get("ETag") == "" || response.Header().Get("Cache-Control") != "public, max-age=60" ||
			response.Header().Get("Content-Type") != contentType {
			t.Fatalf("%s: %v", contentType, response.Header())
		}
	}

	private := serve(PageCache(keep)(feed("application/atom+xml", "private")), httptest.NewRequest(http.MethodGet, "/", nil))
	if private.Header().Get("ETag") != "" {
		t.Fatalf("a private feed got an ETag: %v", private.Header())
	}
}

// TestPageCacheLastModified checks that If-Modified-Since alone gets 304
// Not Modified for a response with a Last-Modified that is not newer, such
// as a feed, and that pages, which have none, and requests with an
// If-None-Match that does not match get the response.
func TestPageCacheLastModified(t *testing.T) {
	revalidate := func(*http.Request) CachePolicy { return CachePolicy{} }
	modified := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	feed := PageCache(revalidate)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
		w.Header().Set("Last-Modified", modified.Format(http.TimeFormat))
		_, _ = io.WriteString(w, `<feed></feed>`)
	}))

	since := func(date time.Time, etag string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/en/news/feed.atom", nil)
		r.Header.Set("If-Modified-Since", date.Format(http.TimeFormat))

		if etag != "" {
			r.Header.Set("If-None-Match", etag)
		}

		return r
	}

	if response := serve(feed, since(modified, "")); response.Code != http.StatusNotModified || response.Body.Len() != 0 {
		t.Fatalf("same date: %d %v", response.Code, response.Header())
	}

	if response := serve(feed, since(modified.Add(-time.Second), "")); response.Code != http.StatusOK {
		t.Fatalf("older date: %d", response.Code)
	}

	other := serve(feed, since(modified, `"other"`))
	if other.Code != http.StatusOK || other.Header().Get("ETag") == "" {
		t.Fatalf("If-None-Match decides: %d %v", other.Code, other.Header())
	}

	html := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, page)
	})

	shown := serve(PageCache(revalidate)(html), since(modified.Add(time.Hour), ""))
	if shown.Code != http.StatusOK || shown.Body.String() != page {
		t.Fatalf("page without Last-Modified: %d %v", shown.Code, shown.Header())
	}
}

// TestPageCacheCompressed checks the two together, as App uses them: the
// ETag of the page matches whether it went out compressed or not.
func TestPageCacheCompressed(t *testing.T) {
	compress := middleware.Compress()
	cache := PageCache(func(*http.Request) CachePolicy { return CachePolicy{} })
	handler := compress(cache(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, page)
	})))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")

	first := serve(handler, r)
	if first.Header().Get("Content-Encoding") != "gzip" || first.Header().Get("ETag") == "" {
		t.Fatalf("%v", first.Header())
	}

	plain := httptest.NewRequest(http.MethodGet, "/", nil)
	plain.Header.Set("If-None-Match", first.Header().Get("ETag"))

	second := serve(handler, plain)
	if second.Code != http.StatusNotModified {
		t.Fatalf("%d %v", second.Code, second.Header())
	}
}

// requestIDs is the request ID middleware App installs.
func requestIDs(next http.Handler) http.Handler {
	return middleware.RequestIDWithOptions(middleware.RequestIDOptions{WithContext: metrics.WithRequestID})(next)
}

// TestObserve checks the metrics and the access log of requests and renders.
func TestObserve(t *testing.T) {
	m := NewMetrics()

	var log bytes.Buffer

	observe := Observe(m, slog.New(slog.NewTextHandler(&log, nil)), class)
	handler := requestIDs(observe(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)

			return
		}

		_, _ = io.WriteString(w, "hello")
	})))

	serve(handler, httptest.NewRequest(http.MethodGet, "/en/?token=secret", nil))
	serve(handler, httptest.NewRequest(http.MethodGet, "/en/", nil))
	serve(handler, httptest.NewRequest(http.MethodGet, "/missing", nil))
	serve(handler, httptest.NewRequest(http.MethodPost, "/admin/pages", nil))
	serve(handler, httptest.NewRequest("BREW", "/theme/site.css", nil))

	m.Record(metrics.Record{Kind: "partial", Duration: 2 * time.Millisecond})
	m.Record(metrics.Record{Kind: "partial", Duration: time.Millisecond, Error: errors.New("broken")})

	var out bytes.Buffer
	if err := m.WriteTo(&out, func(e *Exposition) { e.Gauge("extra_value", "An extra.", 7) }); err != nil {
		t.Fatal(err)
	}

	text := out.String()

	for _, want := range []string{
		`webthings_http_requests_total{class="public",method="GET",code="200"} 2`,
		`webthings_http_requests_total{class="public",method="GET",code="404"} 1`,
		`webthings_http_requests_total{class="admin",method="POST",code="200"} 1`,
		`webthings_http_requests_total{class="asset",method="other",code="200"} 1`,
		`webthings_http_request_duration_seconds_count{class="public"} 3`,
		`webthings_http_request_duration_seconds_bucket{class="public",le="+Inf"} 3`,
		`webthings_render_total{kind="partial",result="error"} 1`,
		`webthings_render_total{kind="partial",result="ok"} 1`,
		`webthings_render_duration_seconds_bucket{kind="partial",le="0.001"} 1`,
		`webthings_render_duration_seconds_bucket{kind="partial",le="0.0025"} 2`,
		"# TYPE webthings_http_requests_in_flight gauge",
		"extra_value 7",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("metrics lack %s:\n%s", want, text)
		}
	}

	lines := log.String()
	if strings.Count(lines, "msg=request") != 5 || strings.Contains(lines, "secret") ||
		!strings.Contains(lines, "status=404") || !strings.Contains(lines, "request_id=") {
		t.Fatalf("access log:\n%s", lines)
	}
}

// A site whose logger already adds request IDs gets one request_id per
// access log line, not two.
func TestObserveWrappedLogger(t *testing.T) {
	var log bytes.Buffer

	logger := slog.New(middleware.LogHandler(slog.NewJSONHandler(&log, nil)))
	observe := Observe(NewMetrics(), logger, class)
	handler := requestIDs(observe(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hello")
	})))

	serve(handler, httptest.NewRequest(http.MethodGet, "/en/", nil))

	if count := strings.Count(log.String(), `"request_id"`); count != 1 {
		t.Fatalf("want one request_id, got %d:\n%s", count, log.String())
	}
}

// TestObservePassingPanics checks that a request whose panic passes through
// Observe is still counted and logged: as a 500 when nothing was written,
// with its status otherwise, and that the panic goes on.
func TestObservePassingPanics(t *testing.T) {
	var log bytes.Buffer

	m := NewMetrics()
	observe := Observe(m, slog.New(slog.NewTextHandler(&log, nil)), class)
	handler := observe(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/started" {
			w.WriteHeader(http.StatusAccepted)
		}

		panic(http.ErrAbortHandler)
	}))

	for _, path := range []string{"/en/", "/started"} {
		func() {
			defer func() {
				if recovered := recover(); recovered != http.ErrAbortHandler {
					t.Errorf("%s: the panic does not go on: %v", path, recovered)
				}
			}()

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		}()
	}

	lines := log.String()
	if !strings.Contains(lines, "path=/en/ status=500") || !strings.Contains(lines, "path=/started status=202") {
		t.Fatalf("access log:\n%s", lines)
	}

	var out bytes.Buffer
	if err := m.WriteTo(&out); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), `webthings_http_requests_total{class="public",method="GET",code="500"} 1`) {
		t.Fatalf("metrics:\n%s", out.String())
	}
}
