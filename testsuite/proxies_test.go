package testsuite

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/sitetest"
)

// TestTrustedProxies checks that forwarding headers name the visitor only
// behind a configured proxy.
func TestTrustedProxies(t *testing.T) {
	t.Parallel()

	seen := make(chan string, 1)

	module := webthings.Module{
		Type: "whoami",
		Key:  "whoami",
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			env.Actions().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				seen <- frontend.ClientIP(r)

				w.WriteHeader(http.StatusNoContent)
			})

			return webthings.Parts{}, nil
		},
	}

	visit := func(app http.Handler, remote string) string {
		request := httptest.NewRequest(http.MethodPost, "/_/whoami/", nil)
		request.RemoteAddr = remote
		request.Header.Set("X-Forwarded-For", "198.51.100.7")

		app.ServeHTTP(httptest.NewRecorder(), request)

		return <-seen
	}

	plain := sitetest.Start(t, module)
	if got := visit(plain.App, "192.0.2.1:1234"); got != "192.0.2.1" {
		t.Fatalf("without trusted proxies: %s", got)
	}

	proxied := sitetest.StartWith(t, sitetest.Options{
		Configure: func(config *webthings.Config) {
			config.TrustedProxies = []string{"192.0.2.0/24"}
		},
	}, module)

	if got := visit(proxied.App, "192.0.2.1:1234"); got != "198.51.100.7" {
		t.Fatalf("behind a trusted proxy: %s", got)
	}

	if got := visit(proxied.App, "203.0.113.5:1234"); got != "203.0.113.5" {
		t.Fatalf("from elsewhere: %s", got)
	}

	app := webthings.New(webthings.Config{StateDir: t.TempDir(), TrustedProxies: []string{"proxy.example"}})
	t.Cleanup(func() { _ = app.Close() })

	if err := app.Start(context.Background()); err == nil {
		t.Fatal("invalid trusted proxy accepted")
	}
}
