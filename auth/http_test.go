package auth_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/auth"
)

// TestSetCookie checks that the session cookie reaches the whole site,
// wherever the admin is, and stays HttpOnly, Secure and SameSite=Lax.
func TestSetCookie(t *testing.T) {
	w := httptest.NewRecorder()
	auth.SetCookie(w, "token", true)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies: %v", cookies)
	}

	session := cookies[0]
	if session.Path != "/" || session.Value != "token" || !session.HttpOnly || !session.Secure {
		t.Errorf("session cookie: %v", session)
	}

	if !strings.Contains(w.Header().Get("Set-Cookie"), "SameSite=Lax") {
		t.Errorf("session cookie is not SameSite=Lax: %v", w.Header().Values("Set-Cookie"))
	}
}

// TestClearCookie checks that signing out removes the site-wide cookie.
func TestClearCookie(t *testing.T) {
	w := httptest.NewRecorder()
	auth.ClearCookie(w, false)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Path != "/" || cookies[0].MaxAge >= 0 {
		t.Fatalf("cookies: %v", cookies)
	}
}
