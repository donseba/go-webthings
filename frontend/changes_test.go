package frontend_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/frontend"
)

// TestChanges checks that a tracked post changed something unless it says
// otherwise, and that the spam trap and the throttle say so themselves.
func TestChanges(t *testing.T) {
	if !frontend.Changed(post("/en/")) {
		t.Fatal("an untracked post changed nothing")
	}

	tracked := frontend.TrackChanges(post("/en/"))
	if !frontend.Changed(tracked) {
		t.Fatal("a tracked post changed nothing before it said so")
	}

	// The handlers of the post get requests made from it.
	frontend.NothingChanged(tracked.WithContext(tracked.Context()))

	if frontend.Changed(tracked) {
		t.Fatal("NothingChanged was not recorded")
	}

	form := url.Values{"back": {"/en/"}, frontend.Honeypot: {"https://spam.example"}}
	spam := httptest.NewRequest(http.MethodPost, "/_/comments/post", strings.NewReader(form.Encode()))
	spam.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	spam = frontend.TrackChanges(spam)

	if !frontend.IsSpam(spam) || frontend.Changed(spam) {
		t.Fatal("spam changed something")
	}

	person := frontend.TrackChanges(post("/en/"))
	if frontend.IsSpam(person) || !frontend.Changed(person) {
		t.Fatal("a person's post changed nothing")
	}

	handler := frontend.Throttle(1, time.Minute, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	first := frontend.TrackChanges(post("/en/"))
	handler.ServeHTTP(httptest.NewRecorder(), first)

	second := frontend.TrackChanges(post("/en/"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, second)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("throttle: %d", recorder.Code)
	}

	if !frontend.Changed(first) || frontend.Changed(second) {
		t.Fatal("throttle: the post over the limit changed something, or the first did not")
	}
}
