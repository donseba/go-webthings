package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/mail"
)

const ownerPassword = "test-only long passphrase"

// emptyAccounts is an auth service without any account.
func emptyAccounts(t *testing.T) *auth.Service {
	t.Helper()

	database := dbtest.SQLite(t)

	if err := database.ApplySchema(context.Background(), "auth", auth.Schema()); err != nil {
		t.Fatal(err)
	}

	accounts, err := auth.New(database)
	if err != nil {
		t.Fatal(err)
	}

	return accounts
}

// restartable serves an admin that restart replaces with a new one on the
// same accounts, as an app restart or another server of the site would.
type restartable struct {
	browser

	current atomic.Pointer[admin.Admin]
	restart func()
}

func serveRestartable(t *testing.T, accounts *auth.Service, options admin.Options) *restartable {
	t.Helper()

	options.Title = "Test site"
	options.CatalogueDir = t.TempDir()

	s := &restartable{}
	s.restart = func() {
		backoffice, err := admin.New(accounts, options)
		if err != nil {
			t.Fatal(err)
		}

		s.current.Store(backoffice)
	}
	s.restart()

	mux := http.NewServeMux()
	mux.Handle("/admin/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.current.Load().ServeHTTP(w, r)
	}))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	s.browser = browser{t: t, client: newClient(server), server: server}

	return s
}

func TestCrossOriginPosts(t *testing.T) {
	t.Parallel()

	accounts := newAccounts(t)
	s := serveRestartable(t, accounts, admin.Options{Language: "en"})
	s.signIn()

	values := url.Values{"Email": {"editor@example.test"}, "Password": {"another long passphrase"}}

	// A page on another website posts with the signed-in browser's cookie.
	if status, _ := s.postFrom("https://evil.example", "cross-site", "/admin/accounts", values); status != http.StatusForbidden {
		t.Fatalf("cross-site post: %d", status)
	}

	// Browsers without Sec-Fetch-Site still send the Origin.
	request, _ := http.NewRequest(http.MethodPost, s.server.URL+"/admin/accounts", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://evil.example")

	response, err := s.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	_ = response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin post without Sec-Fetch-Site: %d", response.StatusCode)
	}

	if status, _ := s.postFrom("https://evil.example", "cross-site", "/admin/login", url.Values{
		"Email": {"owner@example.test"}, "Password": {ownerPassword},
	}); status != http.StatusForbidden {
		t.Fatalf("cross-site sign-in: %d", status)
	}

	users, err := accounts.Users(context.Background())
	if err != nil || len(users) != 1 {
		t.Fatalf("a refused post created an account: %v %v", users, err)
	}

	// An open form still works after a restart: there is no token to expire.
	if _, body := s.get("/admin/accounts"); strings.Contains(body, "_csrf") {
		t.Fatal("forms still carry a CSRF token")
	}

	s.restart()

	if status, body := s.post(
		"/admin/accounts",
		values,
	); status != http.StatusOK ||
		!strings.Contains(body, "editor@example.test") {
		t.Fatalf("same-origin post after a restart: %d\n%s", status, body)
	}
}

// TestFirstRunSetup checks that the setup screen opens only with the setup
// token and only while no account exists.
func TestFirstRunSetup(t *testing.T) {
	t.Parallel()

	const token = "test-only-setup-token"

	accounts := emptyAccounts(t)
	s := serveRestartable(t, accounts, admin.Options{Language: "en", SetupToken: token})

	if _, body := s.get("/admin/login"); !strings.Contains(body, "open the setup link from the server") ||
		strings.Contains(body, `href="/admin/setup"`) {
		t.Fatalf("the sign-in page does not point to the setup link:\n%s", body)
	}

	for _, path := range []string{"/admin/setup", "/admin/setup?token=wrong", "/admin/setup?token=" + token + "x"} {
		if status, _ := s.get(path); status != http.StatusNotFound {
			t.Fatalf("%s: %d", path, status)
		}
	}

	status, body := s.get("/admin/setup?token=" + token)
	if status != http.StatusOK || !strings.Contains(body, `value="`+token+`"`) {
		t.Fatalf("setup: %d\n%s", status, body)
	}

	status, _ = s.post("/admin/setup", url.Values{
		"Email": {"owner@example.test"}, "Password": {ownerPassword}, "Repeat": {ownerPassword},
	})
	if status != http.StatusNotFound {
		t.Fatalf("setup without the token: %d", status)
	}

	status, body = s.post("/admin/setup", url.Values{
		"Token": {token},
		"Email": {"owner@example.test"}, "Password": {ownerPassword}, "Repeat": {"something else entirely"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "The passwords do not match.") ||
		!strings.Contains(body, `value="`+token+`"`) {
		t.Fatalf("different passwords: %d", status)
	}

	status, body = s.post("/admin/setup", url.Values{
		"Token": {token},
		"Email": {"owner@example.test"}, "Name": {"Owner"}, "Password": {ownerPassword}, "Repeat": {ownerPassword},
	})
	if status != http.StatusOK || !strings.Contains(body, "Your administrator account is ready.") {
		t.Fatalf("setup did not sign in: %d\n%s", status, body)
	}

	if status, _ := s.get("/admin/accounts"); status != http.StatusOK {
		t.Fatalf("the new administrator cannot manage accounts: %d", status)
	}

	// Once an account exists, the setup is gone, also with the token.
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		var status int

		if method == http.MethodGet {
			status, _ = s.get("/admin/setup?token=" + token)
		} else {
			status, _ = s.post("/admin/setup", url.Values{
				"Token": {token},
				"Email": {"intruder@example.test"}, "Password": {ownerPassword}, "Repeat": {ownerPassword},
			})
		}

		if status != http.StatusNotFound {
			t.Fatalf("%s /admin/setup with an account: %d", method, status)
		}
	}

	if _, body := s.get("/admin/login"); strings.Contains(body, "setup link") {
		t.Fatal("the sign-in page still offers the setup")
	}
}

// TestSetupWithoutToken checks that without a setup token there is no setup
// screen, even on a site without accounts.
func TestSetupWithoutToken(t *testing.T) {
	t.Parallel()

	s := serveRestartable(t, emptyAccounts(t), admin.Options{Language: "en"})

	for _, path := range []string{"/admin/setup", "/admin/setup?token="} {
		if status, _ := s.get(path); status != http.StatusNotFound {
			t.Fatalf("%s: %d", path, status)
		}
	}

	status, _ := s.post("/admin/setup", url.Values{
		"Token": {""},
		"Email": {"owner@example.test"}, "Password": {ownerPassword}, "Repeat": {ownerPassword},
	})
	if status != http.StatusNotFound {
		t.Fatalf("setup post: %d", status)
	}
}

var resetLink = regexp.MustCompile(`https?://[^\s]+/admin/reset-password\?token=[^\s]+`)

// waitMail waits for the outbox to hold count messages; reset links are sent
// in the background.
func waitMail(t *testing.T, outbox *mail.Memory, count int) []mail.Message {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for len(outbox.Sent()) < count {
		if time.Now().After(deadline) {
			t.Fatalf("want %d e-mails, got %d", count, len(outbox.Sent()))
		}

		time.Sleep(10 * time.Millisecond)
	}

	return outbox.Sent()
}

func TestPasswordReset(t *testing.T) {
	t.Parallel()

	outbox := &mail.Memory{}
	s := serveRestartable(t, newAccounts(t), admin.Options{Language: "nl", Mail: outbox})
	s.signIn()

	visitor := browser{t: t, client: newClient(s.server), server: s.server}

	if _, body := visitor.get("/admin/login"); !strings.Contains(body, `href="/admin/forgot-password"`) {
		t.Fatal("the sign-in page does not offer a password reset")
	}

	// The answer is the same with and without an account.
	status, unknown := visitor.post("/admin/forgot-password", url.Values{"Email": {"nobody@example.test"}})
	if status != http.StatusOK {
		t.Fatalf("unknown address: %d", status)
	}

	status, known := visitor.post("/admin/forgot-password", url.Values{"Email": {"owner@example.test"}})
	if status != http.StatusOK || known != unknown || !strings.Contains(known, "De link werkt een uur.") {
		t.Fatalf("known address: %d, same answer: %v\n%s", status, known == unknown, known)
	}

	sent := waitMail(t, outbox, 1)
	if len(sent) != 1 || sent[0].To[0] != "owner@example.test" || !strings.Contains(sent[0].Subject, "Test site") {
		t.Fatalf("reset mail: %+v", sent)
	}

	link := resetLink.FindString(sent[0].Text)
	if link == "" || !strings.HasPrefix(link, s.server.URL) {
		t.Fatalf("no reset link in:\n%s", sent[0].Text)
	}

	parsed, _ := url.Parse(link)
	token := parsed.Query().Get("token")

	if status, body := visitor.get(parsed.RequestURI()); status != http.StatusOK || !strings.Contains(body, `name="Token"`) {
		t.Fatalf("reset form: %d\n%s", status, body)
	}

	status, body := visitor.post("/admin/reset-password", url.Values{
		"Token": {token}, "Password": {"a brand new passphrase"}, "Repeat": {"a brand new passphrase"},
	})
	if status != http.StatusOK || !strings.Contains(body, "Je wachtwoord is gewijzigd.") {
		t.Fatalf("reset: %d\n%s", status, body)
	}

	// Every session of the account ended, also the one of the owner's browser.
	if _, body := s.get("/admin/accounts"); !strings.Contains(body, `action="/admin/login"`) {
		t.Fatal("a session survived the password reset")
	}

	// The link works once.
	if _, body := visitor.get(
		parsed.RequestURI(),
	); strings.Contains(body, `name="Token"`) ||
		!strings.Contains(body, "ongeldig") {
		t.Fatal("a used link still offers the form")
	}

	status, _ = visitor.post("/admin/login", url.Values{"Email": {"owner@example.test"}, "Password": {"a brand new passphrase"}})
	if status != http.StatusOK {
		t.Fatalf("sign in with the new password: %d", status)
	}
}

func TestNoPasswordResetWithoutMail(t *testing.T) {
	t.Parallel()

	s := serveRestartable(t, newAccounts(t), admin.Options{Language: "en"})

	if _, body := s.get("/admin/login"); strings.Contains(body, "forgot-password") {
		t.Fatal("password reset offered without mail")
	}

	if status, _ := s.get("/admin/forgot-password"); status != http.StatusNotFound {
		t.Fatalf("forgot password without mail: %d", status)
	}
}

func TestChangeOwnPassword(t *testing.T) {
	t.Parallel()

	s := serveRestartable(t, newAccounts(t), admin.Options{Language: "en"})
	s.signIn()

	other := browser{t: t, client: newClient(s.server), server: s.server}
	other.signIn()

	if _, body := s.get("/admin/"); !strings.Contains(body, `href="/admin/account"`) {
		t.Fatal("no link to the own account")
	}

	status, body := s.post("/admin/account", url.Values{
		"Current": {"wrong password!"}, "Password": {"a brand new passphrase"}, "Repeat": {"a brand new passphrase"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Your current password is not correct.") {
		t.Fatalf("wrong current password: %d", status)
	}

	status, _ = s.post("/admin/account", url.Values{
		"Current": {ownerPassword}, "Password": {"a brand new passphrase"}, "Repeat": {"another new passphrase"},
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("different passwords: %d", status)
	}

	status, body = s.post("/admin/account", url.Values{
		"Current": {ownerPassword}, "Password": {"a brand new passphrase"}, "Repeat": {"a brand new passphrase"},
	})
	if status != http.StatusOK || !strings.Contains(body, "Your other sessions were signed out.") {
		t.Fatalf("change password: %d\n%s", status, body)
	}

	if status, _ := s.get("/admin/accounts"); status != http.StatusOK {
		t.Fatal("the session of the change ended")
	}

	if _, body := other.get("/admin/accounts"); !strings.Contains(body, `action="/admin/login"`) {
		t.Fatal("another session survived the change")
	}
}

var accountURL = regexp.MustCompile(
	`href="(/admin/accounts/[0-9a-f-]+)"[^>]*>\s*<span class="select-row-title">editor@example.test<`,
)

func TestEditAndDeleteAccounts(t *testing.T) {
	t.Parallel()

	accounts := newAccounts(t)
	s := serveRestartable(t, accounts, admin.Options{Language: "en"})
	s.signIn()

	if _, err := accounts.CreateUser(
		context.Background(),
		"editor@example.test",
		"",
		"another long passphrase",
		auth.Editor,
	); err != nil {
		t.Fatal(err)
	}

	_, body := s.get("/admin/accounts")

	match := accountURL.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("editor not listed:\n%s", body)
	}

	editor := match[1]

	status, body := s.post(editor+"/profile", url.Values{"Email": {"owner@example.test"}, "Name": {"Ed"}})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "This email address already has an account.") {
		t.Fatalf("address of another account: %d", status)
	}

	if status, _ := s.post(
		editor+"/profile",
		url.Values{"Email": {"edith@example.test"}, "Name": {"Edith"}},
	); status != http.StatusOK {
		t.Fatalf("profile: %d", status)
	}

	if _, body := s.get(editor); !strings.Contains(body, "edith@example.test") || !strings.Contains(body, "Edith") {
		t.Fatal("profile not saved")
	}

	// Nobody deletes their own account.
	owner, err := accounts.Login(context.Background(), "owner@example.test", ownerPassword, "")
	if err != nil {
		t.Fatal(err)
	}

	user, err := accounts.Authenticate(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}

	if _, body := s.get("/admin/accounts/" + user.ID); strings.Contains(body, "/delete") {
		t.Fatal("the own account offers a delete button")
	}

	if status, _ := s.post("/admin/accounts/"+user.ID+"/delete", url.Values{}); status != http.StatusConflict {
		t.Fatalf("own account deleted: %d", status)
	}

	if status, _ := s.post(editor+"/delete", url.Values{}); status != http.StatusOK {
		t.Fatalf("delete: %d", status)
	}

	if _, body := s.get("/admin/accounts"); strings.Contains(body, "edith@example.test") {
		t.Fatal("deleted account still listed")
	}
}
