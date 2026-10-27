package testsuite

import (
	"compress/gzip"
	"context"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/sitetest"

	partialerrors "github.com/donseba/go-partial/ext/errors"
)

// request sends a request straight to the app, with headers as name/value
// pairs, so the test sees the response as a browser gets it.
func request(t *testing.T, app *webthings.App, method, path string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(method, path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}

	response := httptest.NewRecorder()
	app.ServeHTTP(response, r)

	return response
}

// TestHealthAndReadiness checks /healthz and /readyz: alive before start,
// ready once started, not ready when a schema is not applied (any more).
func TestHealthAndReadiness(t *testing.T) {
	t.Parallel()

	idle := webthings.New(webthings.Config{StateDir: t.TempDir()})

	if response := request(t, idle, http.MethodGet, "/healthz"); response.Code != http.StatusOK ||
		response.Body.String() != "ok\n" {
		t.Fatalf("healthz before start: %d %q", response.Code, response.Body.String())
	}

	if response := request(t, idle, http.MethodGet, "/readyz"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz before start: %d", response.Code)
	}

	s := sitetest.Start(t)

	if response := request(t, s.App, http.MethodGet, "/readyz"); response.Code != http.StatusOK ||
		response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("readyz: %d %v", response.Code, response.Header())
	}

	// The database was moved back to before the site's schema.
	if _, err := s.App.DB.Exec(context.Background(), `UPDATE wt_schema SET version = 1 WHERE owner = 'site'`); err != nil {
		t.Fatal(err)
	}

	if err := s.App.Ready(context.Background()); err == nil || !strings.Contains(err.Error(), "schema site") {
		t.Fatalf("ready with an old schema: %v", err)
	}

	if response := request(t, s.App, http.MethodGet, "/readyz"); response.Code != http.StatusServiceUnavailable ||
		strings.Contains(response.Body.String(), "schema") {
		t.Fatalf("readyz with an old schema: %d %q", response.Code, response.Body.String())
	}
}

// TestMetricsEndpoint checks that /metrics is served only with the token
// and shows requests, renders and the database pool.
func TestMetricsEndpoint(t *testing.T) {
	t.Parallel()

	plain := sitetest.Start(t)

	if response := request(t, plain.App, http.MethodGet, "/metrics"); strings.Contains(response.Body.String(), "webthings_") {
		t.Fatalf("metrics without a token:\n%s", response.Body.String())
	}

	s := sitetest.StartWith(t, sitetest.Options{
		Configure: func(config *webthings.Config) {
			config.MetricsToken = "test-metrics-token"
		},
	})

	s.Visitor().Expect("/en/", http.StatusOK)

	if response := request(t, s.App, http.MethodGet, "/metrics"); response.Code != http.StatusUnauthorized {
		t.Fatalf("metrics without authorization: %d", response.Code)
	}

	if response := request(
		t,
		s.App,
		http.MethodGet,
		"/metrics",
		"Authorization",
		"Bearer wrong",
	); response.Code != http.StatusUnauthorized {
		t.Fatalf("metrics with the wrong token: %d", response.Code)
	}

	response := request(t, s.App, http.MethodGet, "/metrics", "Authorization", "Bearer test-metrics-token")
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatalf("metrics: %d %v", response.Code, response.Header())
	}

	for _, want := range []string{
		`webthings_http_requests_total{class="public",method="GET",code="200"}`,
		`webthings_http_requests_total{class="admin",method="POST",code="303"}`,
		`webthings_render_total{kind="partial",result="ok"}`,
		"webthings_db_open_connections ",
		"webthings_db_statements_total ",
		"go_goroutines ",
	} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("metrics lack %s:\n%s", want, response.Body.String())
		}
	}
}

// TestRequestIDHeader checks that every response names its request ID and
// keeps the one a proxy sent.
func TestRequestIDHeader(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t)

	if id := request(t, s.App, http.MethodGet, "/en/").Header().Get("X-Request-ID"); len(id) != 32 {
		t.Fatalf("request ID %q", id)
	}

	if id := request(t, s.App, http.MethodGet, "/en/", "X-Request-ID", "edge-42").Header().Get("X-Request-ID"); id != "edge-42" {
		t.Fatalf("request ID %q", id)
	}
}

// TestCompressionAndPageCache checks the defaults: pages go out gzipped to
// browsers that accept it, visitors' pages carry an ETag and are checked
// every time, with 304 for an unchanged page, and editors' pages are never
// cached.
func TestCompressionAndPageCache(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t)

	stylesheet := request(t, s.App, http.MethodGet, "/_/webthings.css", "Accept-Encoding", "gzip")
	if stylesheet.Header().Get("Content-Encoding") != "gzip" ||
		!strings.Contains(strings.Join(stylesheet.Header().Values("Vary"), ","), "Accept-Encoding") {
		t.Fatalf("not compressed: %v", stylesheet.Header())
	}

	reader, err := gzip.NewReader(stylesheet.Body)
	if err != nil {
		t.Fatal(err)
	}

	if body, _ := io.ReadAll(reader); !strings.Contains(string(body), "{") {
		t.Fatalf("body:\n%s", body)
	}

	// The test site's home page is too small to be worth compressing.
	response := request(t, s.App, http.MethodGet, "/en/", "Accept-Encoding", "gzip")
	if !strings.Contains(strings.Join(response.Header().Values("Vary"), ","), "Accept-Encoding") {
		t.Fatalf("no Vary: %v", response.Header())
	}

	etag := response.Header().Get("ETag")
	if !strings.HasPrefix(etag, `W/"`) || response.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("cache headers: %v", response.Header())
	}

	if again := request(t, s.App, http.MethodGet, "/en/", "If-None-Match", etag); again.Code != http.StatusNotModified ||
		again.Body.Len() != 0 {
		t.Fatalf("revalidation: %d %v", again.Code, again.Header())
	}

	// The admin is compressed too, and never cached.
	admin, _ := s.Response("/admin/site")
	if admin.Header.Get("Cache-Control") != "no-store" || !admin.Uncompressed {
		t.Fatalf("admin: compressed %v, %v", admin.Uncompressed, admin.Header)
	}

	// Editors see their edit links: no cache may keep their pages.
	editor, _ := s.Response("/en/")
	if editor.Header.Get("Cache-Control") != "private, no-store" || editor.Header.Get("ETag") != "" {
		t.Fatalf("editor's page: %v", editor.Header)
	}

	visitor, _ := s.Visitor().Response("/en/")
	if visitor.Header.Get("ETag") == "" {
		t.Fatalf("visitor's page: %v", visitor.Header)
	}
}

// TestPerformanceSettings checks the admin's Performance card: compression
// can be turned off and pages kept for a while, in English and Dutch.
func TestPerformanceSettings(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t)

	s.Expect("/admin/site", http.StatusOK, "Performance", "Compress responses", `name="PageMaxAge"`, "5 minutes")

	status, _ := s.PostStatus("/admin/site", "/admin/site/performance?locale=en", url.Values{
		"PageMaxAge": {"7"},
		"PageStale":  {"0"},
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("an unknown time was saved: %d", status)
	}

	s.Post("/admin/site", "/admin/site/performance?locale=en", url.Values{
		"PageMaxAge": {"300"},
		"PageStale":  {"600"},
	})

	settings, err := s.App.Site.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if settings.Compress() || settings.PageMaxAge != 300 || settings.PageStale != 600 {
		t.Fatalf("settings: %+v", settings)
	}

	// The form shows the saved times.
	s.Expect("/admin/site", http.StatusOK, `<option value="300" selected`, `<option value="600" selected`)

	response := request(t, s.App, http.MethodGet, "/en/", "Accept-Encoding", "gzip")
	if response.Header().Get("Content-Encoding") != "" ||
		response.Header().Get("Cache-Control") != "public, max-age=300, stale-while-revalidate=600" {
		t.Fatalf("headers: %v", response.Header())
	}

	// A query may show a visitor's result: checked every time.
	if got := request(t, s.App, http.MethodGet, "/en/?comment=sent").Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("with a query: %q", got)
	}

	// Saving other settings keeps these.
	s.Post("/admin/site", "/admin/site/profiles?locale=en", url.Values{"GitHub": {"https://github.com/example"}})

	if settings, _ = s.App.Site.Settings(context.Background()); settings.PageMaxAge != 300 || settings.Compress() {
		t.Fatalf("settings after another save: %+v", settings)
	}

	dutchAdmin(t).Expect("/admin/site", http.StatusOK, "Prestaties", "Antwoorden comprimeren", "5 minuten")
}

// TestDevelopmentErrorPage checks that a failing template is a plain 500
// page without details in production and a detailed page in development.
func TestDevelopmentErrorPage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	broken := "{{/* @dot github.com/donseba/go-webthings/frontend.Page */}}\n<main id=\"content\">\n{{explode}}</main>"
	if err := os.WriteFile(filepath.Join(dir, "page.gohtml"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	start := func(development bool) *sitetest.Site {
		return sitetest.StartWith(t, sitetest.Options{
			Templates: dir,
			Configure: func(config *webthings.Config) {
				config.Development = development
				config.Funcs = []template.FuncMap{{
					"explode": func() (string, error) {
						return "", os.ErrPermission
					},
				}}
			},
		})
	}

	production := start(false).Visitor()

	status, body := production.Get("/en/")
	if status != http.StatusInternalServerError || strings.Contains(body, "permission denied") ||
		!strings.Contains(body, partialerrors.SafeMessage) {
		t.Fatalf("production: %d\n%s", status, body)
	}

	development := start(true).Visitor()

	status, body = development.Get("/en/")
	if status != http.StatusInternalServerError || !strings.Contains(body, "permission denied") ||
		!strings.Contains(body, "page.gohtml:3") {
		t.Fatalf("development: %d\n%s", status, body)
	}
}

// TestLogFormat checks that an unknown log format stops the start.
func TestLogFormat(t *testing.T) {
	t.Parallel()

	app := webthings.New(webthings.Config{StateDir: t.TempDir(), LogFormat: "xml"})

	err := app.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), `log format "xml"`) {
		t.Fatalf("start with log format xml: %v", err)
	}

	_ = app.Close()
}
