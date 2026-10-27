package testsuite

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
)

// TestShutdownWaitsForRequests checks that Serve lets a running request
// finish, with the database still open, before it closes the app.
func TestShutdownWaitsForRequests(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)

	slow := webthings.Module{
		Type: "slow",
		Key:  "slow",
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			return webthings.Parts{
				Middleware: func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path != "/slow" {
							next.ServeHTTP(w, r)

							return
						}

						close(started)
						<-release

						// The app must not be closed while this request runs.
						err := env.DB.Ping(context.Background())
						finished <- err

						_, _ = io.WriteString(w, "done")
					})
				},
			}, nil
		},
	}

	// The app serves on this listener, so no other process can take the
	// port between choosing and serving it.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	addr := listener.Addr().String()

	app := webthings.New(webthings.Config{StateDir: t.TempDir(), Insecure: true})
	if err := app.Use(slow); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)

	go func() {
		served <- app.Serve(ctx, listener)
	}()

	answered := make(chan error, 1)

	go func() {
		var (
			response *http.Response
			err      error
		)

		deadline := time.Now().Add(5 * time.Second)

		for {
			response, err = http.Get("http://" + addr + "/slow")
			if err == nil || time.Now().After(deadline) {
				break
			}

			time.Sleep(20 * time.Millisecond)
		}

		if err == nil {
			_ = response.Body.Close()
		}

		answered <- err
	}()

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the slow request never started")
	}

	cancel()

	select {
	case err := <-served:
		t.Fatalf("Serve returned during a running request: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)

	if err := <-finished; err != nil {
		t.Fatalf("database closed during the request: %v", err)
	}

	if err := <-answered; err != nil {
		t.Fatalf("request: %v", err)
	}

	if err := <-served; err != nil {
		t.Fatal(err)
	}
}
