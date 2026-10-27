package testsuite

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/sitetest"

	"github.com/donseba/go-partial/exp/metrics"
)

// captureLog makes slog's default logger, with request IDs, write to the
// returned buffer until the test ends.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var log bytes.Buffer

	previous := slog.Default()
	slog.SetDefault(slog.New(webthings.LogHandler(slog.NewTextHandler(&log, nil))))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return &log
}

// TestRecoverPanics checks that a panicking handler is answered with 500
// and logged with its request ID, and that the site keeps serving.
func TestRecoverPanics(t *testing.T) {
	module := webthings.Module{
		Type: "boom",
		Key:  "boom",
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			env.Actions().HandleFunc("/", func(http.ResponseWriter, *http.Request) {
				panic("kaboom")
			})

			return webthings.Parts{}, nil
		},
	}

	s := sitetest.Start(t, module)
	log := captureLog(t)

	response := request(t, s.App, http.MethodPost, "/_/boom/", "X-Request-ID", "panic-1")
	if response.Code != http.StatusInternalServerError || response.Header().Get("X-Request-ID") != "panic-1" {
		t.Fatalf("panic: %d %v", response.Code, response.Header())
	}

	lines := log.String()
	if !strings.Contains(lines, "panic recovered") || !strings.Contains(lines, "kaboom") ||
		strings.Count(lines, "request_id=panic-1") != 1 {
		t.Fatalf("log:\n%s", lines)
	}

	s.Visitor().Expect("/en/", http.StatusOK)
}

// TestRequestIDsReachHandlers checks that a handler sees the request's ID,
// also go-partial's render metrics, and that an unusable incoming ID is
// replaced.
func TestRequestIDsReachHandlers(t *testing.T) {
	t.Parallel()

	type seen struct {
		id, metrics string
	}

	ids := make(chan seen, 1)

	module := webthings.Module{
		Type: "whoami",
		Key:  "whoami",
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			env.Actions().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				ids <- seen{id: webthings.RequestID(r.Context()), metrics: metrics.RequestIDFromContext(r.Context())}

				w.WriteHeader(http.StatusNoContent)
			})

			return webthings.Parts{}, nil
		},
	}

	s := sitetest.Start(t, module)

	response := request(t, s.App, http.MethodPost, "/_/whoami/", "X-Request-ID", "edge-7")
	if got := <-ids; got.id != "edge-7" || got.metrics != "edge-7" || response.Header().Get("X-Request-ID") != "edge-7" {
		t.Fatalf("kept ID: %+v %v", got, response.Header())
	}

	response = request(t, s.App, http.MethodPost, "/_/whoami/", "X-Request-ID", strings.Repeat("x", 65))

	got := <-ids
	if len(got.id) != 32 || got.metrics != got.id || response.Header().Get("X-Request-ID") != got.id {
		t.Fatalf("replaced ID: %+v %v", got, response.Header())
	}
}

// TestForwardedOrigin checks that the address a trusted proxy reports names
// the site in absolute links, through the app's middleware.
func TestForwardedOrigin(t *testing.T) {
	t.Parallel()

	links := make(chan string, 1)

	module := webthings.Module{
		Type: "whoami",
		Key:  "whoami",
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			env.Actions().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				links <- frontend.ClientIP(r) + " " + frontend.AbsoluteURL("", r, "/en/")

				w.WriteHeader(http.StatusNoContent)
			})

			return webthings.Parts{}, nil
		},
	}

	s := sitetest.StartWith(t, sitetest.Options{
		Configure: func(config *webthings.Config) {
			config.TrustedProxies = []string{"192.0.2.0/24"}
		},
	}, module)

	visit := func(remote string) string {
		r := httptest.NewRequest(http.MethodPost, "http://site.test/_/whoami/", nil)
		r.RemoteAddr = remote
		r.Header.Set("X-Forwarded-For", "198.51.100.7")
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "example.com")

		s.App.ServeHTTP(httptest.NewRecorder(), r)

		return <-links
	}

	if got := visit("192.0.2.1:1234"); got != "198.51.100.7 https://example.com/en/" {
		t.Fatalf("behind the proxy: %s", got)
	}

	if got := visit("203.0.113.5:1234"); got != "203.0.113.5 http://site.test/en/" {
		t.Fatalf("from elsewhere: %s", got)
	}
}

// TestAdminRoutes checks addresses below /admin that no route answers: they
// stay in the admin rather than reach the site.
func TestAdminRoutes(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t)

	if response := request(t, s.App, http.MethodGet, "/admin"); response.Code != http.StatusSeeOther ||
		response.Header().Get("Location") != "/admin/" {
		t.Fatalf("/admin: %d %v", response.Code, response.Header())
	}

	if response := request(t, s.App, http.MethodGet, "/admin/no-such-page"); response.Code != http.StatusNotFound ||
		strings.Contains(response.Body.String(), "<html") {
		t.Fatalf("unknown admin page: %d\n%s", response.Code, response.Body.String())
	}

	if response := request(t, s.App, http.MethodGet, "/admin/assets/admin.css"); response.Code != http.StatusOK ||
		response.Header().Get("Cache-Control") == "no-store" {
		t.Fatalf("admin assets: %d %v", response.Code, response.Header())
	}

	// Admin pages are never stored, and failed posts keep their URL.
	login := request(t, s.App, http.MethodPost, "/admin/login")
	if login.Header().Get("Cache-Control") != "no-store" || login.Header().Get("X-Frame-Options") != "DENY" ||
		login.Header().Get("HX-Push-Url") != "false" {
		t.Fatalf("admin page headers: %v", login.Header())
	}
}

// TestEnvAdminURL checks that modules build addresses of named admin routes
// once the app runs, for links into the admin from the site.
func TestEnvAdminURL(t *testing.T) {
	t.Parallel()

	var (
		env    webthings.Env
		before string
	)

	module := webthings.Module{
		Type: "linker",
		Key:  "linker",
		Setup: func(e webthings.Env) (webthings.Parts, error) {
			env = e
			before = e.AdminURL("admin.pages.edit", "id", "p1")

			return webthings.Parts{}, nil
		},
	}

	sitetest.Start(t, module)

	if before != "" {
		t.Fatalf("before start: %q", before)
	}

	if got := env.AdminURL("admin.pages.edit", "id", "p1", url.Values{"locale": {"nl"}}); got != "/admin/pages/p1?locale=nl" {
		t.Fatalf("edit link: %q", got)
	}

	if got := env.AdminURL("admin.accounts.show", "user", "u 1"); got != "/admin/accounts/u%201" {
		t.Fatalf("account link: %q", got)
	}
}
