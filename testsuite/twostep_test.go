package testsuite

import (
	"encoding/base32"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/sitetest"
)

var (
	twoStepKey   = regexp.MustCompile(`class="two-step-key">([A-Z2-7 ]+)<`)
	recoveryCode = regexp.MustCompile(`class="two-step-code">([0-9a-z]{5}-[0-9a-z]{5})<`)
)

// TestTwoStepSignInSite checks two-step sign-in in a whole site, on every
// database: the app keeps the secret of the authenticator app encrypted with
// a key of the site, a new browser needs a code after the password, and an
// administrator who turns it off for another account is logged, without
// secrets or codes.
func TestTwoStepSignInSite(t *testing.T) {
	var logs syncBuffer

	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	dbtest.EachDSN(t, func(t *testing.T, driver, dsn string) {
		s := sitetest.StartWith(t, sitetest.Options{Configure: func(config *webthings.Config) {
			config.Driver = driver
			config.DSN = dsn
		}})
		ctx := t.Context()

		editor, err := s.App.Auth.CreateUser(ctx, "editor@example.test", "Ed", "editor long passphrase", auth.Editor)
		if err != nil {
			t.Fatal(err)
		}

		// The editor turns two-step sign-in on, on their own account page.
		own := s.As("editor@example.test", "editor long passphrase")
		own.Post("/admin/account", "/admin/account/two-step", url.Values{})

		_, page := own.Get("/admin/account")

		match := twoStepKey.FindStringSubmatch(page)
		if match == nil {
			t.Fatalf("no key on the account page:\n%s", page)
		}

		secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ReplaceAll(match[1], " ", ""))
		if err != nil {
			t.Fatal(err)
		}

		status, page := own.PostStatus("/admin/account", "/admin/account/two-step/confirm", url.Values{
			"Code": {auth.TOTP(secret, time.Now())},
		})

		codes := recoveryCode.FindAllStringSubmatch(page, -1)
		if status != http.StatusOK || len(codes) != 10 {
			t.Fatalf("confirm: %d, %d recovery codes", status, len(codes))
		}

		// The database holds the secret encrypted with a key of the site.
		var stored string

		if err := s.App.DB.Get(ctx, &stored, `SELECT secret FROM wt_two_step WHERE user_id=?`, editor.ID); err != nil {
			t.Fatal(err)
		}

		if !strings.HasPrefix(stored, "v1:") || strings.Contains(stored, auth.SecretText(secret)) {
			t.Fatalf("secret stored as %q", stored)
		}

		key, err := s.App.Site.Secret(ctx, "two_step_sign_in")
		if err != nil || len(key) != 32 {
			t.Fatalf("site key: %d bytes, %v", len(key), err)
		}

		// A new browser needs a code after the password.
		visitor := s.Visitor()

		status, page = visitor.PostStatus("/admin/login", "/admin/login", url.Values{
			"Email":    {"editor@example.test"},
			"Password": {"editor long passphrase"},
		})
		if status != http.StatusOK || !strings.Contains(page, `action="/admin/login/code"`) {
			t.Fatalf("password: %d\n%s", status, page)
		}

		status, page = visitor.PostStatus("/admin/login/code", "/admin/login/code", url.Values{
			"Code": {auth.TOTP(secret, time.Now().Add(30*time.Second))},
		})
		if status != http.StatusOK || !strings.Contains(page, `href="/admin/account"`) {
			t.Fatalf("code: %d\n%s", status, page)
		}

		// An administrator turns it off; the log says who did it for whom.
		before := len(logs.String())

		s.Post("/admin/accounts/"+editor.ID, "/admin/accounts/"+editor.ID+"/two-step/off", url.Values{})

		state, err := s.App.Auth.TwoStep(ctx, editor.ID)
		if err != nil || state.On {
			t.Fatalf("after the administrator turned it off: %+v %v", state, err)
		}

		log := logs.String()[before:]
		if !strings.Contains(log, "level=INFO msg=\"two-step sign-in turned off by an administrator\"") ||
			!strings.Contains(log, "email=editor@example.test") ||
			!strings.Contains(log, "administrator_email=admin@example.test") {
			t.Fatalf("log of turning it off:\n%s", log)
		}

		// Nothing logged the secret or a recovery code.
		log = logs.String()

		if strings.Contains(log, auth.SecretText(secret)) || strings.Contains(log, match[1]) {
			t.Fatal("the secret was logged")
		}

		for _, code := range codes {
			if strings.Contains(log, code[1]) {
				t.Fatalf("recovery code %s was logged", code[1])
			}
		}
	})
}
