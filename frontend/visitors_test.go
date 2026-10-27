package frontend_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/frontend"
)

// visitorPost is a visitor's form post with values.
func visitorPost(values url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/_/signup/send", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "192.0.2.1:1234"

	return r
}

func TestActions(t *testing.T) {
	var bodies []string

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)

			return
		}

		bodies = append(bodies, string(body))
	})

	handler := frontend.Actions("signup", next, frontend.ActionLimits{Body: 64, Posts: 2, Window: time.Hour})

	small := url.Values{"back": {"/en/"}}

	for range 2 {
		handler.ServeHTTP(httptest.NewRecorder(), visitorPost(small))
	}

	if len(bodies) != 2 {
		t.Fatalf("posts within the limits: %v", bodies)
	}

	// Over the limit the visitor returns to the form's page.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, visitorPost(small))

	location := w.Header().Get("Location")
	if w.Code != http.StatusSeeOther || location != "/en/?signup="+frontend.ResultThrottled || len(bodies) != 2 {
		t.Fatalf("throttled: %d %q", w.Code, location)
	}

	// The body is limited before anything reads it.
	unthrottled := frontend.Actions("signup", next, frontend.ActionLimits{Body: 64})
	w = httptest.NewRecorder()
	unthrottled.ServeHTTP(w, visitorPost(url.Values{"text": {strings.Repeat("x", 100)}}))

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large body: %d", w.Code)
	}

	// A custom answer for throttled visitors reads at most the body limit.
	var throttledBody int

	custom := frontend.Actions("signup", next, frontend.ActionLimits{
		Body:   64,
		Posts:  1,
		Window: time.Hour,
		Throttled: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			throttledBody = len(body)
		}),
	})

	custom.ServeHTTP(httptest.NewRecorder(), visitorPost(small))
	custom.ServeHTTP(httptest.NewRecorder(), visitorPost(url.Values{"text": {strings.Repeat("x", 100)}}))

	if throttledBody > 64 {
		t.Fatalf("the throttled answer read %d bytes", throttledBody)
	}
}

func TestBackPath(t *testing.T) {
	cases := map[string]string{
		"/nl/contact?x=1#form":  "/nl/contact",
		"/":                     "/",
		"":                      "",
		"//evil.example/en/":    "",
		"https://evil.example/": "",
		"/en/\\evil":            "",
	}

	for back, want := range cases {
		if got := frontend.BackPath(visitorPost(url.Values{"back": {back}})); got != want {
			t.Errorf("%q: %q, want %q", back, got, want)
		}
	}
}

func TestResult(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/en/?signup=sent&other=x", nil)

	if got := frontend.Result(r, "signup", "sent", "invalid"); got != "sent" {
		t.Fatalf("known result: %q", got)
	}

	if got := frontend.Result(r, "other", "sent"); got != "" {
		t.Fatalf("unknown result: %q", got)
	}

	if got := frontend.Result(r, "missing", "sent", ""); got != "" {
		t.Fatalf("no result: %q", got)
	}
}
