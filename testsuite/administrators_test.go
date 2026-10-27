package testsuite

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/sitetest"
)

// TestSetupReminder checks that the sign-in page of a site without
// accounts logs the setup link again, at warning level and at most once a
// minute, and says what to look for in the log.
func TestSetupReminder(t *testing.T) {
	var logs syncBuffer

	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	s := sitetest.StartWith(t, sitetest.Options{NoAccount: true, Configure: func(config *webthings.Config) {
		config.BaseURL = "https://example.test"
	}})

	logs.Reset()

	s.Expect("/admin/login", http.StatusOK, "search the log for “no account exists yet”", "AdminEmail and AdminPassword")
	s.Expect("/admin/login", http.StatusOK)

	link := "https://example.test/admin/setup?token=" + s.App.SetupToken()
	if count := strings.Count(logs.String(), "level=WARN"); count != 1 || !strings.Contains(logs.String(), link) ||
		!strings.Contains(logs.String(), "no account exists yet") {
		t.Fatalf("want the link logged once at warning level, got %d times:\n%s", count, logs.String())
	}

	// No setup link on the page itself.
	s.ExpectMissing("/admin/login", "/admin/setup")

	// With an account, the sign-in page reminds nobody.
	logs.Reset()

	signedUp := sitetest.Start(t)
	signedUp.Visitor().Expect("/admin/login", http.StatusOK)

	if strings.Contains(logs.String(), "no account exists yet") {
		t.Fatalf("a site with accounts logs the setup link:\n%s", logs.String())
	}
}

// TestInviteAdministrator checks App.InviteAdministrator: an administrator
// without a password whose link chooses one, refused for an address that
// has an account and before the app started.
func TestInviteAdministrator(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	if _, err := webthings.New(webthings.Config{StateDir: t.TempDir()}).
		InviteAdministrator(ctx, "a@example.test"); !errors.Is(
		err,
		webthings.ErrNotStarted,
	) {
		t.Fatalf("before Start: %v", err)
	}

	s := sitetest.Start(t)

	link, err := s.App.InviteAdministrator(ctx, "second@example.test")
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := url.Parse(link)
	if err != nil || parsed.Path != "/admin/reset-password" || parsed.Query().Get("token") == "" {
		t.Fatalf("link %q", link)
	}

	if _, err := s.App.InviteAdministrator(ctx, "second@example.test"); !errors.Is(err, auth.ErrDuplicate) {
		t.Fatalf("a second invitation: %v", err)
	}

	if _, err := s.App.InviteAdministrator(ctx, "no address"); !errors.Is(err, auth.ErrEmail) {
		t.Fatalf("no address: %v", err)
	}

	visitor := s.Visitor()
	visitor.Expect(link, http.StatusOK, "Choose a new password")

	page := visitor.Post(link, "/admin/reset-password", url.Values{
		"Token":    {parsed.Query().Get("token")},
		"Password": {"second long passphrase"},
		"Repeat":   {"second long passphrase"},
	})
	if page != "/admin/login" {
		t.Fatalf("choosing the password ended on %s", page)
	}

	// The account is an administrator's: it opens the accounts.
	s.As("second@example.test", "second long passphrase").Expect("/admin/accounts", http.StatusOK, "second@example.test")

	// With a BaseURL the link is absolute.
	public := sitetest.StartWith(t, sitetest.Options{Configure: func(config *webthings.Config) {
		config.BaseURL = "https://example.test/"
	}})

	link, err = public.App.InviteAdministrator(ctx, "third@example.test")
	if err != nil || !strings.HasPrefix(link, "https://example.test/admin/reset-password?token=") {
		t.Fatalf("link %q: %v", link, err)
	}
}
