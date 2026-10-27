package testsuite

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/sitetest"
)

// TestObserveEveryRequest checks that the metrics count the answers no route
// gives, such as the redirect of a path to its clean form, and leave out the
// endpoints for operations.
func TestObserveEveryRequest(t *testing.T) {
	t.Parallel()

	s := sitetest.StartWith(t, sitetest.Options{
		Configure: func(config *webthings.Config) {
			config.MetricsToken = "test-metrics-token"
		},
	})

	// Go's ServeMux answers both, with a redirect.
	unclean := request(t, s.App, http.MethodGet, "/en//")
	if unclean.Code/100 != 3 || unclean.Header().Get("Location") != "/en/" {
		t.Fatalf("unclean path: %d %v", unclean.Code, unclean.Header())
	}

	admin := request(t, s.App, http.MethodPost, "/admin")
	if admin.Code/100 != 3 || admin.Header().Get("Location") != "/admin/" {
		t.Fatalf("post to the admin's path: %d %v", admin.Code, admin.Header())
	}

	// Requests are classed by the core space their path is in, also those
	// no route there answers.
	classed := map[string]string{
		"/admin/missing":     "admin",
		"/_/missing/post":    "action",
		"/media/missing.jpg": "media",
		"/theme/missing.css": "asset",
		"/_/webthings.css":   "asset",
		"/robots.txt":        "seo",
	}

	for path := range classed {
		request(t, s.App, http.MethodGet, path)
	}

	for _, path := range []string{"/healthz", "/readyz"} {
		if response := request(t, s.App, http.MethodGet, path); response.Code != http.StatusOK {
			t.Fatalf("%s: %d", path, response.Code)
		}
	}

	response := request(t, s.App, http.MethodGet, "/metrics", "Authorization", "Bearer test-metrics-token")
	if response.Code != http.StatusOK {
		t.Fatalf("metrics: %d", response.Code)
	}

	metrics := response.Body.String()

	for _, want := range []string{
		fmt.Sprintf(`webthings_http_requests_total{class="public",method="GET",code="%d"}`, unclean.Code),
		fmt.Sprintf(`webthings_http_requests_total{class="admin",method="POST",code="%d"}`, admin.Code),
	} {
		if !strings.Contains(metrics, want) {
			t.Errorf("metrics lack %s:\n%s", want, metrics)
		}
	}

	for path, class := range classed {
		if !strings.Contains(metrics, `webthings_http_requests_total{class="`+class+`",method="GET"`) {
			t.Errorf("metrics lack class %s of %s:\n%s", class, path, metrics)
		}
	}

	if strings.Contains(metrics, `class="operations"`) {
		t.Errorf("metrics count the endpoints for operations:\n%s", metrics)
	}
}

// TestObservePanics checks that a request whose handler panicked is in the
// access log and the metrics with its 500, as recovery happens inside the
// observation. It changes slog's default logger, so it runs on its own.
func TestObservePanics(t *testing.T) {
	log := captureLog(t)

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

	s := sitetest.StartWith(t, sitetest.Options{
		Configure: func(config *webthings.Config) {
			config.MetricsToken = "test-metrics-token"
			config.AccessLog = true
		},
	}, module)

	response := request(t, s.App, http.MethodPost, "/_/boom/", "X-Request-ID", "panic-2")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("panic: %d", response.Code)
	}

	lines := log.String()
	if !strings.Contains(lines, "msg=request method=POST path=/_/boom/ status=500") ||
		strings.Count(lines, "request_id=panic-2") != 2 {
		t.Fatalf("access log:\n%s", lines)
	}

	metrics := request(t, s.App, http.MethodGet, "/metrics", "Authorization", "Bearer test-metrics-token").Body.String()
	if !strings.Contains(metrics, `webthings_http_requests_total{class="action",method="POST",code="500"} 1`) {
		t.Fatalf("metrics lack the panic:\n%s", metrics)
	}
}
