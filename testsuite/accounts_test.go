package testsuite

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/sitetest"
)

// TestFirstRunSetup checks that a site without accounts offers the setup
// screen only with the setup link's token, and only until the first account
// exists.
func TestFirstRunSetup(t *testing.T) {
	t.Parallel()

	s := sitetest.StartWith(t, sitetest.Options{NoAccount: true})

	token := s.App.SetupToken()
	if len(token) < 20 {
		t.Fatalf("setup token %q", token)
	}

	s.Expect("/admin/", http.StatusOK, "open the setup link from the server")
	s.Visitor().ExpectMissing("/admin/login", `href="/admin/setup"`)

	// Without the token, or with another, the setup does not exist.
	s.Expect("/admin/setup", http.StatusNotFound)
	s.Expect("/admin/setup?token=wrong", http.StatusNotFound)

	link := "/admin/setup?" + url.Values{"token": {token}}.Encode()
	s.Expect(link, http.StatusOK, `value="`+token+`"`)

	account := url.Values{
		"Email":    {"owner@example.test"},
		"Name":     {"Owner"},
		"Password": {"owner long passphrase"},
		"Repeat":   {"owner long passphrase"},
	}

	for _, wrong := range []string{"", "wrong"} {
		values := url.Values{"Token": {wrong}}
		for key, value := range account {
			values[key] = value
		}

		if status, _ := s.PostStatus(link, "/admin/setup", values); status != http.StatusNotFound {
			t.Fatalf("setup with token %q: %d", wrong, status)
		}
	}

	account.Set("Token", token)

	// The new administrator goes on to the setup guide, which welcomes them.
	page := s.Post(link, "/admin/setup", account)
	if page != "/admin/site/guide/welcome" {
		t.Fatalf("setup ended on %s", page)
	}

	s.Expect("/admin/accounts", http.StatusOK, "owner@example.test")
	s.Expect(link, http.StatusNotFound)
	s.Visitor().ExpectMissing("/admin/login", "/admin/setup")
	s.Visitor().ExpectMissing("/admin/login", "setup link")
}

// syncBuffer is a log written by the app while the test reads it.
type syncBuffer struct {
	mu   sync.Mutex
	text strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.text.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.text.String()
}

func (b *syncBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.text.Reset()
}

// TestSetupLink checks that a start without accounts logs the setup link:
// on BaseURL, or on the address ListenAndServe listens on; and that every
// start makes a new token.
func TestSetupLink(t *testing.T) {
	var logs syncBuffer

	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	dir := t.TempDir()
	app := webthings.New(webthings.Config{StateDir: dir, BaseURL: "https://example.test/", Insecure: true})

	if err := app.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	first := app.SetupToken()
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(logs.String(), "https://example.test/admin/setup?token="+first) {
		t.Fatalf("no setup link on BaseURL in:\n%s", logs.String())
	}

	logs.Reset()

	app = webthings.New(webthings.Config{StateDir: dir, Insecure: true})
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)

	go func() {
		served <- app.ListenAndServe(ctx, "127.0.0.1:0")
	}()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "serving") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	cancel()

	if err := <-served; err != nil {
		t.Fatal(err)
	}

	second := app.SetupToken()
	if second == "" || second == first {
		t.Fatalf("tokens %q and %q", first, second)
	}

	// The link has the port the listener got, not the ":0" that was asked.
	link := regexp.MustCompile(`http://127\.0\.0\.1:[1-9][0-9]*/admin/setup\?token=` + regexp.QuoteMeta(second))
	if !link.MatchString(logs.String()) {
		t.Fatalf("no setup link on the listening address in:\n%s", logs.String())
	}
}

var resetLinkPattern = regexp.MustCompile(`https?://\S+/admin/reset-password\?token=\S+`)

// TestPasswordResetByMail resets a forgotten password with the link the site
// mails, which signs the account out everywhere.
func TestPasswordResetByMail(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t)
	visitor := s.Visitor()

	visitor.Expect("/admin/login", http.StatusOK, `href="/admin/forgot-password"`)
	visitor.Submit("/admin/forgot-password", url.Values{"Email": {"nobody@example.test"}})
	visitor.Submit("/admin/forgot-password", url.Values{"Email": {"admin@example.test"}})

	sent := s.Mails(1)
	if len(sent) != 1 || sent[0].To[0] != "admin@example.test" {
		t.Fatalf("reset mails: %+v", sent)
	}

	link, err := url.Parse(resetLinkPattern.FindString(sent[0].Text))
	if err != nil || link.Path != "/admin/reset-password" {
		t.Fatalf("no reset link in:\n%s", sent[0].Text)
	}

	page := visitor.Post(link.RequestURI(), "/admin/reset-password", url.Values{
		"Token":    {link.Query().Get("token")},
		"Password": {"a brand new passphrase"},
		"Repeat":   {"a brand new passphrase"},
	})
	if page != "/admin/login" {
		t.Fatalf("reset ended on %s", page)
	}

	// The admin's browser was signed out.
	s.Expect("/admin/accounts", http.StatusOK, `action="/admin/login"`)

	s.As("admin@example.test", "a brand new passphrase").Expect("/admin/accounts", http.StatusOK, "admin@example.test")
}

// TestAdminRefusesCrossSitePosts checks the admin's cross-origin protection:
// a page on another site cannot post with an editor's session cookie.
func TestAdminRefusesCrossSitePosts(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t)

	values := url.Values{"Email": {"intruder@example.test"}, "Password": {"intruder long passphrase"}}

	for _, path := range []string{"/admin/accounts", "/admin/login", "/admin/logout"} {
		if status := s.CrossSite(path, values); status != http.StatusForbidden {
			t.Errorf("cross-site POST %s: %d", path, status)
		}
	}

	s.ExpectMissing("/admin/accounts", "intruder@example.test")
}

// TestPasswordResetNeedsBaseURL checks that a site served over HTTPS offers
// the password reset only with a BaseURL and Mail, so the link in the e-mail
// does not depend on the Host header of whoever asked for it.
func TestPasswordResetNeedsBaseURL(t *testing.T) {
	t.Parallel()

	start := func(baseURL string) (*webthings.App, *mail.Memory) {
		outbox := &mail.Memory{}
		app := webthings.New(webthings.Config{
			StateDir:      t.TempDir(),
			AdminEmail:    "admin@example.test",
			AdminPassword: "test-only long passphrase",
			Mail:          outbox,
			BaseURL:       baseURL,
		})

		if err := app.Start(context.Background()); err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = app.Close() })

		return app, outbox
	}

	app, _ := start("")

	response := httptest.NewRecorder()
	app.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://example.test/admin/forgot-password", nil))

	if response.Code != http.StatusNotFound {
		t.Fatalf("password reset without BaseURL: %d", response.Code)
	}

	app, outbox := start("https://www.example.test")

	request := httptest.NewRequest(
		http.MethodPost,
		"https://evil.example/admin/forgot-password",
		strings.NewReader(url.Values{"Email": {"admin@example.test"}}.Encode()),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response = httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("password reset with BaseURL: %d", response.Code)
	}

	deadline := time.Now().Add(5 * time.Second)
	for len(outbox.Sent()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	sent := outbox.Sent()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "https://www.example.test/admin/reset-password?token=") {
		t.Fatalf("reset link does not use the BaseURL: %+v", sent)
	}
}

// TestExpiredSessionsCleanedUp checks that the app removes expired sessions
// in the background, from its start.
func TestExpiredSessionsCleanedUp(t *testing.T) {
	t.Parallel()

	config := webthings.Config{
		StateDir:      t.TempDir(),
		AdminEmail:    "admin@example.test",
		AdminPassword: "test-only long passphrase",
		Insecure:      true,
	}

	app := webthings.New(config)
	if err := app.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	users, err := app.Auth.Users(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("users: %v %v", users, err)
	}

	past := time.Now().Add(-24 * time.Hour).UTC()

	_, err = app.DB.Exec(ctx,
		`INSERT INTO wt_sessions(id,user_id,token_hash,expires_at,created_at) VALUES(?,?,?,?,?)`,
		db.NewID(), users[0].ID, strings.Repeat("a", 64), past, past,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := app.Close(); err != nil {
		t.Fatal(err)
	}

	app = webthings.New(config)
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = app.Close() })

	deadline := time.Now().Add(5 * time.Second)

	for {
		var count int

		if err := app.DB.Get(ctx, &count, `SELECT COUNT(*) FROM wt_sessions`); err != nil {
			t.Fatal(err)
		}

		if count == 0 {
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("%d expired sessions remain", count)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// TestReservedModuleKeys checks that modules cannot take the addresses of the
// admin's own sections and pages.
func TestReservedModuleKeys(t *testing.T) {
	t.Parallel()

	keys := []string{
		"accounts", "groups", "login", "logout", "setup", "forgot-password", "reset-password", "account",
		"assets", "pages", "media", "site", "theme", "mail", "partials", "webthings",
	}

	for _, key := range keys {
		app := webthings.New(webthings.Config{StateDir: t.TempDir()})

		err := app.Use(webthings.Module{Type: "test", Key: key, Setup: func(webthings.Env) (webthings.Parts, error) {
			return webthings.Parts{}, nil
		}})
		if err == nil {
			t.Errorf("module key %q accepted", key)
		}
	}
}

var revokePattern = regexp.MustCompile(`action="(/admin/accounts/[^"]+/sessions/[^"]+/revoke)"`)

// TestRevokeSession ends another account's session from its account page:
// that browser is signed out at once, and the session shows as ended.
func TestRevokeSession(t *testing.T) {
	t.Parallel()

	admin := sitetest.Start(t)
	account := admin.Submit("/admin/accounts", url.Values{
		"Email":    {"editor@example.test"},
		"Password": {"editor long passphrase"},
		"Role":     {"editor"},
	})

	editor := admin.As("editor@example.test", "editor long passphrase")
	editor.Expect("/admin/", http.StatusOK)

	_, page := admin.Get(account)

	revokes := revokePattern.FindAllStringSubmatch(page, -1)
	if len(revokes) != 1 {
		t.Fatalf("want one session to revoke, got %d\n%s", len(revokes), page)
	}

	if path := admin.Post(account, revokes[0][1], url.Values{}); path != account {
		t.Fatalf("revoke ended on %s", path)
	}

	// The editor is signed out; the session can no longer be revoked.
	if response, _ := editor.Response("/admin/"); response.Request.URL.Path != "/admin/login" {
		t.Fatalf("revoked session still signed in: %s", response.Request.URL)
	}

	admin.ExpectMissing(account, "/revoke")

	// Revoking again changes nothing.
	admin.Post(account, revokes[0][1], url.Values{})
}
