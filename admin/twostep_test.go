package admin_test

import (
	"bytes"
	"context"
	"encoding/base32"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/mail"
)

// testKeys is the site's key store of the tests: the same key for every
// name.
type testKeys struct{}

func (testKeys) Secret(context.Context, string) ([]byte, error) {
	return bytes.Repeat([]byte{0x5a}, 32), nil
}

var (
	// keyPattern is the secret of a setup as the account page shows it,
	// in groups of four.
	keyPattern = regexp.MustCompile(`class="two-step-key">([A-Z2-7 ]+)<`)
	// recoveryPattern is a recovery code as the account page shows it,
	// once.
	recoveryPattern = regexp.MustCompile(`class="two-step-code">([0-9a-z]{5}-[0-9a-z]{5})<`)
)

// accountsIn is an auth service on database, with the site's keys and
// one administrator, the owner.
func accountsIn(t *testing.T, database *db.DB) *auth.Service {
	t.Helper()

	ctx := context.Background()

	if err := database.ApplySchema(ctx, "auth", auth.Schema()); err != nil {
		t.Fatal(err)
	}

	accounts, err := auth.New(database)
	if err != nil {
		t.Fatal(err)
	}

	accounts.UseSecrets(testKeys{})

	if err := accounts.Bootstrap(ctx, "owner@example.test", ownerPassword); err != nil {
		t.Fatal(err)
	}

	return accounts
}

// nextCode is the authenticator app's code of the next 30-second step, which
// is accepted and has not been used yet.
func nextCode(secret []byte) string {
	return auth.TOTP(secret, time.Now().Add(30*time.Second))
}

// otherCode is a code of six digits other than code.
func otherCode(code string) string {
	if code == "000000" {
		return "111111"
	}

	return "000000"
}

// turnOnTwoStep turns two-step sign-in on in the signed-in browser, as its
// owner does on the account page, and returns the secret and the recovery
// codes.
func (b browser) turnOnTwoStep() ([]byte, []string) {
	b.t.Helper()

	status, body := b.post("/admin/account/two-step", url.Values{})
	if status != http.StatusOK {
		b.t.Fatalf("start the setup: %d", status)
	}

	match := keyPattern.FindStringSubmatch(body)
	if match == nil {
		b.t.Fatalf("no key on the page:\n%s", body)
	}

	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ReplaceAll(match[1], " ", ""))
	if err != nil {
		b.t.Fatal(err)
	}

	status, body = b.post("/admin/account/two-step/confirm", url.Values{"Code": {auth.TOTP(secret, time.Now())}})
	if status != http.StatusOK {
		b.t.Fatalf("confirm the setup: %d\n%s", status, body)
	}

	var codes []string

	for _, match := range recoveryPattern.FindAllStringSubmatch(body, -1) {
		codes = append(codes, match[1])
	}

	if len(codes) != 10 {
		b.t.Fatalf("%d recovery codes on the page:\n%s", len(codes), body)
	}

	return secret, codes
}

// response is the response to a GET of path, with its headers, and its
// body.
func (b browser) response(path string) (*http.Response, []byte) {
	b.t.Helper()

	response, err := b.client.Get(b.server.URL + path)
	if err != nil {
		b.t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)

	return response, body
}

func TestTurnOnTwoStep(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		b := startWith(t, accountsIn(t, database), admin.Options{Language: "en"})
		b.signIn()

		_, body := b.get("/admin/account")
		if !strings.Contains(body, "Two-step sign-in") || !strings.Contains(body, `action="/admin/account/two-step"`) {
			t.Fatalf("no two-step sign-in on the account page:\n%s", body)
		}

		status, body := b.post("/admin/account/two-step", url.Values{})
		if status != http.StatusOK || !strings.Contains(body, `src="/admin/account/two-step/qr"`) {
			t.Fatalf("setup: %d\n%s", status, body)
		}

		match := keyPattern.FindStringSubmatch(body)
		if match == nil || len(strings.Fields(match[1])) != 8 {
			t.Fatalf("key in groups of four: %q", match)
		}

		secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ReplaceAll(match[1], " ", ""))
		if err != nil || len(secret) != 20 {
			t.Fatalf("secret: %d bytes, %v", len(secret), err)
		}

		// The QR code is a PNG for the signed-in browser only, never stored.
		response, image := b.response("/admin/account/two-step/qr")
		if response.StatusCode != http.StatusOK ||
			response.Header.Get("Content-Type") != "image/png" ||
			!strings.Contains(response.Header.Get("Cache-Control"), "no-store") ||
			!bytes.HasPrefix(image, []byte("\x89PNG")) {
			t.Fatalf("QR code: %d %v", response.StatusCode, response.Header)
		}

		visitor := browser{t: t, client: newClient(b.server), server: b.server}

		if response, image := visitor.response("/admin/account/two-step/qr"); bytes.HasPrefix(image, []byte("\x89PNG")) {
			t.Fatalf("QR code without a session: %d", response.StatusCode)
		}

		status, body = b.post("/admin/account/two-step/confirm", url.Values{"Code": {otherCode(auth.TOTP(secret, time.Now()))}})
		if status != http.StatusUnprocessableEntity || !strings.Contains(body, "This code is not correct.") {
			t.Fatalf("wrong code: %d", status)
		}

		if _, body := b.get("/admin/account"); recoveryPattern.MatchString(body) || !keyPattern.MatchString(body) {
			t.Fatal("a wrong code turned two-step sign-in on")
		}

		status, body = b.post("/admin/account/two-step/confirm", url.Values{"Code": {auth.TOTP(secret, time.Now())}})
		if status != http.StatusOK || len(recoveryPattern.FindAllString(body, -1)) != 10 ||
			!strings.Contains(body, "Keep these codes somewhere safe") {
			t.Fatalf("confirm: %d\n%s", status, body)
		}

		// The codes show once; the secret is not shown again.
		_, body = b.get("/admin/account")
		if recoveryPattern.MatchString(body) || keyPattern.MatchString(body) ||
			!strings.Contains(body, "Two-step sign-in is on.") {
			t.Fatalf("account page after turning on:\n%s", body)
		}

		if response, _ := b.response("/admin/account/two-step/qr"); response.StatusCode != http.StatusNotFound {
			t.Fatalf("QR code after turning on: %d", response.StatusCode)
		}
	})
}

func TestTwoStepSignIn(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		owner := startWith(t, accountsIn(t, database), admin.Options{Language: "nl"})
		owner.signIn()

		if _, body := owner.get("/admin/account"); !strings.Contains(body, "Inloggen in twee stappen") {
			t.Fatal("the account page does not offer two-step sign-in in Dutch")
		}

		secret, codes := owner.turnOnTwoStep()
		b := browser{t: t, client: newClient(owner.server), server: owner.server}
		login := url.Values{"Email": {"owner@example.test"}, "Password": {ownerPassword}}

		status, body := b.post("/admin/login", login)
		if status != http.StatusOK || !strings.Contains(body, "Voer de code uit je authenticator-app in") ||
			!strings.Contains(body, `action="/admin/login/code"`) {
			t.Fatalf("password: %d\n%s", status, body)
		}

		// The password alone does not sign in.
		if _, body := b.get("/admin/accounts"); !strings.Contains(body, `action="/admin/login"`) {
			t.Fatal("signed in without the code")
		}

		wrong := url.Values{"Code": {otherCode(nextCode(secret))}}

		if status, _ := b.post("/admin/login/code", wrong); status != http.StatusUnprocessableEntity {
			t.Fatalf("wrong code: %d", status)
		}

		code := nextCode(secret)

		status, body = b.post("/admin/login/code", url.Values{"Code": {code}})
		if status != http.StatusOK || !strings.Contains(body, "Recent bewerkt") {
			t.Fatalf("right code: %d\n%s", status, body)
		}

		if status, body := b.get("/admin/accounts"); status != http.StatusOK || !strings.Contains(body, "owner@example.test") {
			t.Fatalf("accounts after signing in: %d", status)
		}

		// The same code again is refused.
		b.post("/admin/logout", url.Values{})
		b.post("/admin/login", login)

		if status, _ := b.post("/admin/login/code", url.Values{"Code": {code}}); status != http.StatusUnprocessableEntity {
			t.Fatalf("the same code again: %d", status)
		}

		// A recovery code works once.
		if status, body := b.post("/admin/login/code", url.Values{"Code": {codes[0]}}); status != http.StatusOK ||
			!strings.Contains(body, "Recent bewerkt") {
			t.Fatalf("recovery code: %d", status)
		}

		b.post("/admin/logout", url.Values{})
		b.post("/admin/login", login)

		if status, _ := b.post("/admin/login/code", url.Values{"Code": {codes[0]}}); status != http.StatusUnprocessableEntity {
			t.Fatalf("recovery code again: %d", status)
		}

		// Without a pending sign-in, the code page leads to the sign-in.
		visitor := browser{t: t, client: newClient(owner.server), server: owner.server}

		if _, body := visitor.get("/admin/login/code"); !strings.Contains(body, `action="/admin/login"`) ||
			strings.Contains(body, `action="/admin/login/code"`) {
			t.Fatalf("code page without a pending sign-in:\n%s", body)
		}

		_, body = visitor.post("/admin/login/code", url.Values{"Code": {nextCode(secret)}})
		if strings.Contains(body, "Recent bewerkt") {
			t.Fatal("signed in with a code alone")
		}
	})
}

// TestWrongCodesCountInAdmin checks that wrong codes count towards the
// sign-in limit with wrong passwords.
func TestWrongCodesCountInAdmin(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		owner := startWith(t, accountsIn(t, database), admin.Options{Language: "en"})
		owner.signIn()

		secret, _ := owner.turnOnTwoStep()
		b := browser{t: t, client: newClient(owner.server), server: owner.server}

		for range 5 {
			if status, _ := b.post("/admin/login", url.Values{
				"Email":    {"owner@example.test"},
				"Password": {"wrong password!"},
			}); status != http.StatusUnprocessableEntity {
				t.Fatalf("wrong password: %d", status)
			}
		}

		b.post("/admin/login", url.Values{"Email": {"owner@example.test"}, "Password": {ownerPassword}})

		for range 5 {
			wrong := url.Values{"Code": {otherCode(nextCode(secret))}}

			if status, _ := b.post("/admin/login/code", wrong); status != http.StatusUnprocessableEntity {
				t.Fatalf("wrong code: %d", status)
			}
		}

		status, body := b.post("/admin/login/code", url.Values{"Code": {nextCode(secret)}})
		if status != http.StatusTooManyRequests || !strings.Contains(body, "Too many attempts.") {
			t.Fatalf("right code after 10 failures: %d", status)
		}
	})
}

func TestTurnOffTwoStep(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		b := startWith(t, accountsIn(t, database), admin.Options{Language: "en"})
		b.signIn()

		secret, codes := b.turnOnTwoStep()

		_, body := b.get("/admin/account")
		if !strings.Contains(body, `action="/admin/account/two-step/off"`) ||
			!strings.Contains(body, `action="/admin/account/two-step/recovery-codes"`) {
			t.Fatalf("no forms to turn off or for new codes:\n%s", body)
		}

		// New recovery codes need a code and replace the old ones.
		status, body := b.post("/admin/account/two-step/recovery-codes", url.Values{"Code": {otherCode(nextCode(secret))}})
		if status != http.StatusUnprocessableEntity || !strings.Contains(body, "This code is not correct.") {
			t.Fatalf("new codes with a wrong code: %d", status)
		}

		status, body = b.post("/admin/account/two-step/recovery-codes", url.Values{"Code": {nextCode(secret)}})
		fresh := recoveryPattern.FindAllStringSubmatch(body, -1)

		if status != http.StatusOK || len(fresh) != 10 || fresh[0][1] == codes[0] {
			t.Fatalf("new codes: %d\n%s", status, body)
		}

		// Turning off needs the password and a code.
		status, body = b.post("/admin/account/two-step/off", url.Values{"Password": {"wrong password!"}, "Code": {fresh[0][1]}})
		if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Your current password is not correct.") {
			t.Fatalf("wrong password: %d", status)
		}

		status, body = b.post("/admin/account/two-step/off", url.Values{"Password": {ownerPassword}, "Code": {codes[0]}})
		if status != http.StatusUnprocessableEntity || !strings.Contains(body, "This code is not correct.") {
			t.Fatalf("replaced recovery code: %d", status)
		}

		status, body = b.post("/admin/account/two-step/off", url.Values{"Password": {ownerPassword}, "Code": {fresh[0][1]}})
		if status != http.StatusOK || !strings.Contains(body, "Two-step sign-in is off.") {
			t.Fatalf("turn off: %d\n%s", status, body)
		}

		// The password alone signs in again.
		visitor := browser{t: t, client: newClient(b.server), server: b.server}
		visitor.signIn()

		if status, _ := visitor.get("/admin/accounts"); status != http.StatusOK {
			t.Fatalf("sign in after turning off: %d", status)
		}
	})
}

// TestAdministratorTurnsOffTwoStep checks that administrators turn two-step
// sign-in off for another account, as for a lost phone, and editors cannot.
func TestAdministratorTurnsOffTwoStep(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		accounts := accountsIn(t, database)

		anna, err := accounts.CreateUser(ctx, "anna@example.test", "Anna", "anna long passphrase", auth.Editor)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := accounts.CreateUser(ctx, "ed@example.test", "Ed", "ed long passphrase", auth.Editor); err != nil {
			t.Fatal(err)
		}

		if err := accounts.BeginTwoStep(ctx, anna.ID); err != nil {
			t.Fatal(err)
		}

		secret, err := accounts.SetupSecret(ctx, anna.ID)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := accounts.ConfirmTwoStep(ctx, anna.ID, auth.TOTP(secret, time.Now())); err != nil {
			t.Fatal(err)
		}

		b := startWith(t, accounts, admin.Options{Language: "en"})
		b.signIn()

		editor := browser{t: t, client: newClient(b.server), server: b.server}
		editor.signInAs("ed@example.test", "ed long passphrase")

		off := "/admin/accounts/" + anna.ID + "/two-step/off"

		_, body := b.get("/admin/accounts/" + anna.ID)
		if !strings.Contains(body, `action="`+off+`"`) ||
			!strings.Contains(body, "Turn off two-step sign-in for this account?") {
			t.Fatalf("no form to turn two-step sign-in off:\n%s", body)
		}

		if status, _ := editor.post(off, url.Values{}); status != http.StatusForbidden {
			t.Fatalf("an editor turned two-step sign-in off: %d", status)
		}

		if state, err := accounts.TwoStep(ctx, anna.ID); err != nil || !state.On {
			t.Fatalf("after the editor's post: %+v %v", state, err)
		}

		status, body := b.post(off, url.Values{})
		if status != http.StatusOK || !strings.Contains(body, "Two-step sign-in is off for this account.") {
			t.Fatalf("turn off: %d\n%s", status, body)
		}

		if state, err := accounts.TwoStep(ctx, anna.ID); err != nil || state.On {
			t.Fatalf("after the administrator's post: %+v %v", state, err)
		}

		if _, err := accounts.Login(ctx, "anna@example.test", "anna long passphrase", ""); err != nil {
			t.Fatalf("sign in with the password alone: %v", err)
		}

		if _, body := b.get("/admin/accounts/" + anna.ID); strings.Contains(body, off) {
			t.Fatal("the form shows while two-step sign-in is off")
		}
	})
}

// TestPasswordResetAsksForCode checks that a password reset by e-mail does
// not skip the code.
func TestPasswordResetAsksForCode(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		outbox := &mail.Memory{}
		b := startWith(t, accountsIn(t, database), admin.Options{Language: "en", Mail: outbox})
		b.signIn()

		secret, _ := b.turnOnTwoStep()
		visitor := browser{t: t, client: newClient(b.server), server: b.server}

		visitor.post("/admin/forgot-password", url.Values{"Email": {"owner@example.test"}})

		link := resetLink.FindString(waitMail(t, outbox, 1)[0].Text)
		parsed, _ := url.Parse(link)

		status, _ := visitor.post("/admin/reset-password", url.Values{
			"Token":    {parsed.Query().Get("token")},
			"Password": {"a brand new passphrase"},
			"Repeat":   {"a brand new passphrase"},
		})
		if status != http.StatusOK {
			t.Fatalf("reset: %d", status)
		}

		login := url.Values{"Email": {"owner@example.test"}, "Password": {"a brand new passphrase"}}

		status, body := visitor.post("/admin/login", login)
		if status != http.StatusOK || !strings.Contains(body, `action="/admin/login/code"`) {
			t.Fatalf("sign in with the new password: %d\n%s", status, body)
		}

		if _, body := visitor.get("/admin/accounts"); !strings.Contains(body, `action="/admin/login"`) {
			t.Fatal("the new password alone signed in")
		}

		if status, body := visitor.post("/admin/login/code", url.Values{"Code": {nextCode(secret)}}); status != http.StatusOK ||
			!strings.Contains(body, "Recently edited") {
			t.Fatalf("code after the reset: %d", status)
		}
	})
}
