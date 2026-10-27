package auth_test

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
)

const twoStepPassword = "correct horse battery"

// testKeys is the site's key store of the tests: the same key for every
// name.
type testKeys struct{}

func (testKeys) Secret(context.Context, string) ([]byte, error) {
	return bytes.Repeat([]byte{0x5a}, 32), nil
}

// twoStep is an account with two-step sign-in on, in a service whose clock
// the test sets.
type twoStep struct {
	accounts *auth.Service
	user     auth.User
	secret   []byte
	codes    []string
	now      *time.Time
}

// code is the authenticator app's code at the test's time plus offset.
func (s twoStep) code(offset time.Duration) string {
	return auth.TOTP(s.secret, s.now.Add(offset))
}

// pending signs in with the password and returns the pending sign-in.
func (s twoStep) pending(t *testing.T) string {
	t.Helper()

	attempt, err := s.accounts.SignIn(context.Background(), s.user.Email, twoStepPassword, "")
	if err != nil || attempt.Pending == "" || attempt.Session != "" {
		t.Fatalf("sign in: %+v %v", attempt, err)
	}

	return attempt.Pending
}

// keyedService is service with the site's keys and a clock at now.
func keyedService(t *testing.T, database *db.DB, now *time.Time) *auth.Service {
	t.Helper()

	accounts := service(t, database)
	accounts.UseSecrets(testKeys{})
	auth.SetClock(accounts, func() time.Time { return *now })

	return accounts
}

// withTwoStep creates an editor and turns two-step sign-in on for it.
func withTwoStep(t *testing.T, database *db.DB) twoStep {
	t.Helper()

	ctx := context.Background()
	now := time.Date(2026, 10, 27, 12, 0, 10, 0, time.UTC)
	accounts := keyedService(t, database, &now)

	user, err := accounts.CreateUser(ctx, "anna@example.com", "Anna", twoStepPassword, auth.Editor)
	if err != nil {
		t.Fatal(err)
	}

	if err := accounts.BeginTwoStep(ctx, user.ID); err != nil {
		t.Fatal(err)
	}

	secret, err := accounts.SetupSecret(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	codes, err := accounts.ConfirmTwoStep(ctx, user.ID, auth.TOTP(secret, now))
	if err != nil {
		t.Fatal(err)
	}

	return twoStep{accounts: accounts, user: user, secret: secret, codes: codes, now: &now}
}

// wrongCode is a code of six digits other than code.
func wrongCode(code string) string {
	if code == "000000" {
		return "111111"
	}

	return "000000"
}

var recoveryCodePattern = regexp.MustCompile(`^[0-9a-hjkmnp-tv-z]{5}-[0-9a-hjkmnp-tv-z]{5}$`)

func TestTwoStepTurnOn(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		now := time.Date(2026, 10, 27, 12, 0, 10, 0, time.UTC)
		accounts := keyedService(t, database, &now)

		if !accounts.TwoStepAvailable() {
			t.Fatal("two-step sign-in unavailable with keys")
		}

		user, err := accounts.CreateUser(ctx, "anna@example.com", "Anna", twoStepPassword, auth.Editor)
		if err != nil {
			t.Fatal(err)
		}

		state, err := accounts.TwoStep(ctx, user.ID)
		if err != nil || state.On || state.Setup {
			t.Fatalf("new account: %+v %v", state, err)
		}

		if _, err := accounts.ConfirmTwoStep(ctx, user.ID, "123456"); !errors.Is(err, auth.ErrTwoStepOff) {
			t.Fatalf("confirm without a setup: %v", err)
		}

		if err := accounts.BeginTwoStep(ctx, user.ID); err != nil {
			t.Fatal(err)
		}

		first, err := accounts.SetupSecret(ctx, user.ID)
		if err != nil || len(first) != 20 {
			t.Fatalf("secret: %d bytes, %v", len(first), err)
		}

		// Starting again makes a new secret.
		if err := accounts.BeginTwoStep(ctx, user.ID); err != nil {
			t.Fatal(err)
		}

		secret, err := accounts.SetupSecret(ctx, user.ID)
		if err != nil || bytes.Equal(secret, first) {
			t.Fatalf("second setup: same secret %v, %v", bytes.Equal(secret, first), err)
		}

		code := auth.TOTP(secret, now)

		if _, err := accounts.ConfirmTwoStep(ctx, user.ID, wrongCode(code)); !errors.Is(err, auth.ErrCode) {
			t.Fatalf("wrong code: %v", err)
		}

		if _, err := accounts.ConfirmTwoStep(ctx, user.ID, auth.TOTP(first, now)); !errors.Is(err, auth.ErrCode) {
			t.Fatalf("code of the replaced secret: %v", err)
		}

		state, err = accounts.TwoStep(ctx, user.ID)
		if err != nil || state.On || !state.Setup {
			t.Fatalf("after a wrong code: %+v %v", state, err)
		}

		// Until the setup is confirmed, the password alone signs in.
		if _, err := accounts.Login(ctx, user.Email, twoStepPassword, ""); err != nil {
			t.Fatalf("sign in during the setup: %v", err)
		}

		codes, err := accounts.ConfirmTwoStep(ctx, user.ID, code)
		if err != nil {
			t.Fatal(err)
		}

		seen := map[string]bool{}

		for _, recovery := range codes {
			if !recoveryCodePattern.MatchString(recovery) || seen[recovery] {
				t.Fatalf("recovery codes: %q", codes)
			}

			seen[recovery] = true
		}

		if len(codes) != 10 {
			t.Fatalf("%d recovery codes", len(codes))
		}

		state, err = accounts.TwoStep(ctx, user.ID)
		if err != nil || !state.On || state.Setup || state.RecoveryCodes != 10 {
			t.Fatalf("after confirming: %+v %v", state, err)
		}

		// The secret is shown only while the setup waits for its code.
		if _, err := accounts.SetupSecret(ctx, user.ID); !errors.Is(err, auth.ErrTwoStepOff) {
			t.Fatalf("secret after confirming: %v", err)
		}

		if err := accounts.BeginTwoStep(ctx, user.ID); !errors.Is(err, auth.ErrTwoStepOn) {
			t.Fatalf("setup while on: %v", err)
		}

		// The database holds neither the secret nor the recovery codes.
		var stored string

		if err := database.Get(ctx, &stored, `SELECT secret FROM wt_two_step WHERE user_id=?`, user.ID); err != nil {
			t.Fatal(err)
		}

		if stored == "" || strings.Contains(stored, auth.SecretText(secret)) || bytes.Contains([]byte(stored), secret) {
			t.Fatalf("secret stored as %q", stored)
		}

		var hashes []string

		if err := database.Select(ctx, &hashes, `SELECT code_hash FROM wt_recovery_codes WHERE user_id=?`, user.ID); err != nil {
			t.Fatal(err)
		}

		for _, hash := range hashes {
			for _, recovery := range codes {
				if strings.Contains(hash, recovery) || strings.Contains(hash, strings.ReplaceAll(recovery, "-", "")) {
					t.Fatalf("recovery code %s stored as %s", recovery, hash)
				}
			}
		}
	})
}

func TestTwoStepSignIn(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		s := withTwoStep(t, database)

		// Signing in with the password alone is refused.
		if _, err := s.accounts.Login(ctx, s.user.Email, twoStepPassword, ""); !errors.Is(err, auth.ErrSecondStep) {
			t.Fatalf("login without a code: %v", err)
		}

		pending := s.pending(t)

		if err := s.accounts.CheckPending(ctx, pending); err != nil {
			t.Fatalf("check pending: %v", err)
		}

		// A pending sign-in is no session.
		if _, err := s.accounts.Authenticate(ctx, pending); !errors.Is(err, auth.ErrSession) {
			t.Fatalf("pending sign-in as a session: %v", err)
		}

		// The code of the setup's step was used.
		if _, err := s.accounts.FinishSignIn(ctx, pending, s.code(0), ""); !errors.Is(err, auth.ErrCode) {
			t.Fatalf("code of the setup again: %v", err)
		}

		if _, err := s.accounts.FinishSignIn(ctx, pending, wrongCode(s.code(30*time.Second)), ""); !errors.Is(err, auth.ErrCode) {
			t.Fatalf("wrong code: %v", err)
		}

		// 90 seconds later: one step either side is accepted, two are not.
		*s.now = s.now.Add(90 * time.Second)

		if _, err := s.accounts.FinishSignIn(ctx, pending, s.code(-60*time.Second), ""); !errors.Is(err, auth.ErrCode) {
			t.Fatalf("code of two steps ago: %v", err)
		}

		token, err := s.accounts.FinishSignIn(ctx, pending, s.code(-30*time.Second), "")
		if err != nil {
			t.Fatalf("code of the step before: %v", err)
		}

		signedIn, err := s.accounts.Authenticate(ctx, token)
		if err != nil || signedIn.ID != s.user.ID {
			t.Fatalf("session: %+v %v", signedIn, err)
		}

		// The pending sign-in is finished.
		if err := s.accounts.CheckPending(ctx, pending); !errors.Is(err, auth.ErrPending) {
			t.Fatalf("finished pending sign-in: %v", err)
		}

		pending = s.pending(t)

		if _, err := s.accounts.FinishSignIn(ctx, pending, s.code(-30*time.Second), ""); !errors.Is(err, auth.ErrCode) {
			t.Fatalf("the same code again: %v", err)
		}

		if _, err := s.accounts.FinishSignIn(ctx, pending, s.code(60*time.Second), ""); !errors.Is(err, auth.ErrCode) {
			t.Fatalf("code of two steps ahead: %v", err)
		}

		// Signing in again in the same browser ends its previous session.
		second, err := s.accounts.FinishSignIn(ctx, pending, s.code(30*time.Second), token)
		if err != nil {
			t.Fatalf("code of the step ahead: %v", err)
		}

		if _, err := s.accounts.Authenticate(ctx, token); !errors.Is(err, auth.ErrSession) {
			t.Fatalf("previous session: %v", err)
		}

		if _, err := s.accounts.Authenticate(ctx, second); err != nil {
			t.Fatal(err)
		}

		// The current step comes before the one just used.
		if _, err := s.accounts.FinishSignIn(ctx, s.pending(t), s.code(0), ""); !errors.Is(err, auth.ErrCode) {
			t.Fatalf("code of an earlier step: %v", err)
		}
	})
}

func TestRecoveryCodes(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		s := withTwoStep(t, database)

		// Typed in capitals and without the dash, a recovery code works once.
		typed := strings.ToUpper(strings.ReplaceAll(s.codes[0], "-", ""))

		if _, err := s.accounts.FinishSignIn(ctx, s.pending(t), typed, ""); err != nil {
			t.Fatalf("recovery code: %v", err)
		}

		if _, err := s.accounts.FinishSignIn(ctx, s.pending(t), s.codes[0], ""); !errors.Is(err, auth.ErrCode) {
			t.Fatalf("recovery code again: %v", err)
		}

		state, err := s.accounts.TwoStep(ctx, s.user.ID)
		if err != nil || state.RecoveryCodes != 9 {
			t.Fatalf("after a recovery code: %+v %v", state, err)
		}

		// New codes need a code and replace the old ones.
		_, err = s.accounts.NewRecoveryCodes(ctx, s.user.ID, wrongCode(s.code(30*time.Second)))
		if !errors.Is(err, auth.ErrCode) {
			t.Fatalf("new codes with a wrong code: %v", err)
		}

		fresh, err := s.accounts.NewRecoveryCodes(ctx, s.user.ID, s.code(30*time.Second))
		if err != nil || len(fresh) != 10 {
			t.Fatalf("new codes: %q %v", fresh, err)
		}

		if _, err := s.accounts.FinishSignIn(ctx, s.pending(t), s.codes[1], ""); !errors.Is(err, auth.ErrCode) {
			t.Fatalf("replaced recovery code: %v", err)
		}

		if _, err := s.accounts.FinishSignIn(ctx, s.pending(t), fresh[0], ""); err != nil {
			t.Fatalf("new recovery code: %v", err)
		}
	})
}

// TestWrongCodesCount checks that wrong codes count towards the sign-in
// limit together with wrong passwords.
func TestWrongCodesCount(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		s := withTwoStep(t, database)
		pending := s.pending(t)
		right := s.code(30 * time.Second)

		for range 5 {
			if _, err := s.accounts.SignIn(ctx, s.user.Email, "wrong password!", ""); !errors.Is(err, auth.ErrCredentials) {
				t.Fatalf("wrong password: %v", err)
			}
		}

		for range 5 {
			if _, err := s.accounts.FinishSignIn(ctx, pending, wrongCode(right), ""); !errors.Is(err, auth.ErrCode) {
				t.Fatalf("wrong code: %v", err)
			}
		}

		if _, err := s.accounts.FinishSignIn(ctx, pending, right, ""); !errors.Is(err, auth.ErrRateLimit) {
			t.Fatalf("right code after 10 failures: %v", err)
		}

		if _, err := s.accounts.SignIn(ctx, s.user.Email, twoStepPassword, ""); !errors.Is(err, auth.ErrRateLimit) {
			t.Fatalf("right password after 10 failures: %v", err)
		}

		// After the window the right code signs in, and the failures are
		// forgotten.
		*s.now = s.now.Add(16 * time.Minute)

		if _, err := s.accounts.FinishSignIn(ctx, s.pending(t), s.code(0), ""); err != nil {
			t.Fatalf("after the window: %v", err)
		}

		var failures int

		if err := database.Get(ctx, &failures, `SELECT COUNT(*) FROM wt_login_attempts`); err != nil || failures != 0 {
			t.Fatalf("failures after signing in: %d %v", failures, err)
		}
	})
}

func TestPendingSignInExpires(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		s := withTwoStep(t, database)
		pending := s.pending(t)

		*s.now = s.now.Add(auth.PendingLifetime)

		if err := s.accounts.CheckPending(ctx, pending); !errors.Is(err, auth.ErrPending) {
			t.Fatalf("pending after 5 minutes: %v", err)
		}

		if _, err := s.accounts.FinishSignIn(ctx, pending, s.code(0), ""); !errors.Is(err, auth.ErrPending) {
			t.Fatalf("finish after 5 minutes: %v", err)
		}

		unknown := strings.Repeat("x", 52)

		if _, err := s.accounts.FinishSignIn(ctx, unknown, s.code(0), ""); !errors.Is(err, auth.ErrPending) {
			t.Fatalf("unknown pending sign-in: %v", err)
		}

		// Cleanup removes it.
		if err := s.accounts.Cleanup(ctx); err != nil {
			t.Fatal(err)
		}

		var left int

		if err := database.Get(ctx, &left, `SELECT COUNT(*) FROM wt_pending_sign_ins`); err != nil || left != 0 {
			t.Fatalf("pending sign-ins after cleanup: %d %v", left, err)
		}
	})
}

func TestTwoStepTurnOff(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		s := withTwoStep(t, database)
		code := s.code(30 * time.Second)

		if err := s.accounts.DisableTwoStep(ctx, s.user.ID, "wrong password!", code); !errors.Is(err, auth.ErrCredentials) {
			t.Fatalf("wrong password: %v", err)
		}

		if err := s.accounts.DisableTwoStep(ctx, s.user.ID, twoStepPassword, wrongCode(code)); !errors.Is(err, auth.ErrCode) {
			t.Fatalf("wrong code: %v", err)
		}

		if err := s.accounts.DisableTwoStep(ctx, s.user.ID, twoStepPassword, ""); !errors.Is(err, auth.ErrCode) {
			t.Fatalf("without code: %v", err)
		}

		state, err := s.accounts.TwoStep(ctx, s.user.ID)
		if err != nil || !state.On {
			t.Fatalf("after refusals: %+v %v", state, err)
		}

		if err := s.accounts.DisableTwoStep(ctx, s.user.ID, twoStepPassword, code); err != nil {
			t.Fatal(err)
		}

		state, err = s.accounts.TwoStep(ctx, s.user.ID)
		if err != nil || state.On || state.RecoveryCodes != 0 {
			t.Fatalf("after turning off: %+v %v", state, err)
		}

		if _, err := s.accounts.Login(ctx, s.user.Email, twoStepPassword, ""); err != nil {
			t.Fatalf("sign in after turning off: %v", err)
		}

		if err := s.accounts.DisableTwoStep(ctx, s.user.ID, twoStepPassword, code); !errors.Is(err, auth.ErrTwoStepOff) {
			t.Fatalf("turning off again: %v", err)
		}
	})
}

// TestTwoStepTurnOffWithRecoveryCode turns two-step sign-in off with a
// recovery code, as after losing the phone.
func TestTwoStepTurnOffWithRecoveryCode(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		s := withTwoStep(t, database)

		if err := s.accounts.DisableTwoStep(ctx, s.user.ID, twoStepPassword, s.codes[3]); err != nil {
			t.Fatal(err)
		}

		if _, err := s.accounts.Login(ctx, s.user.Email, twoStepPassword, ""); err != nil {
			t.Fatalf("sign in after turning off: %v", err)
		}
	})
}

// TestRemoveTwoStep turns two-step sign-in off as an administrator does for
// an account that lost its phone and its recovery codes.
func TestRemoveTwoStep(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		s := withTwoStep(t, database)
		pending := s.pending(t)

		if err := s.accounts.RemoveTwoStep(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("unknown account: %v", err)
		}

		if err := s.accounts.RemoveTwoStep(ctx, s.user.ID); err != nil {
			t.Fatal(err)
		}

		if err := s.accounts.CheckPending(ctx, pending); !errors.Is(err, auth.ErrPending) {
			t.Fatalf("pending sign-in after removing: %v", err)
		}

		if _, err := s.accounts.Login(ctx, s.user.Email, twoStepPassword, ""); err != nil {
			t.Fatalf("sign in after removing: %v", err)
		}

		if err := s.accounts.RemoveTwoStep(ctx, s.user.ID); !errors.Is(err, auth.ErrTwoStepOff) {
			t.Fatalf("removing again: %v", err)
		}
	})
}

// TestPasswordResetKeepsTwoStep checks that a new password from a reset
// link does not skip the code, and ends pending sign-ins.
func TestPasswordResetKeepsTwoStep(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		s := withTwoStep(t, database)
		before := s.pending(t)

		token, _, err := s.accounts.RequestPasswordReset(ctx, s.user.Email)
		if err != nil {
			t.Fatal(err)
		}

		if err := s.accounts.ResetPassword(ctx, token, "a brand new passphrase"); err != nil {
			t.Fatal(err)
		}

		if err := s.accounts.CheckPending(ctx, before); !errors.Is(err, auth.ErrPending) {
			t.Fatalf("pending sign-in after the reset: %v", err)
		}

		if _, err := s.accounts.Login(ctx, s.user.Email, "a brand new passphrase", ""); !errors.Is(err, auth.ErrSecondStep) {
			t.Fatalf("login with the new password: %v", err)
		}

		attempt, err := s.accounts.SignIn(ctx, s.user.Email, "a brand new passphrase", "")
		if err != nil || attempt.Pending == "" || attempt.Session != "" {
			t.Fatalf("sign in with the new password: %+v %v", attempt, err)
		}

		if _, err := s.accounts.FinishSignIn(ctx, attempt.Pending, s.code(30*time.Second), ""); err != nil {
			t.Fatal(err)
		}
	})
}

// TestDeleteAccountWithTwoStep deletes an account with two-step sign-in
// on, recovery codes and a pending sign-in.
func TestDeleteAccountWithTwoStep(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		s := withTwoStep(t, database)
		pending := s.pending(t)

		if err := s.accounts.DeleteUser(ctx, s.user.ID); err != nil {
			t.Fatal(err)
		}

		if err := s.accounts.CheckPending(ctx, pending); !errors.Is(err, auth.ErrPending) {
			t.Fatalf("pending sign-in of a deleted account: %v", err)
		}

		for _, table := range []string{"wt_two_step", "wt_recovery_codes", "wt_pending_sign_ins"} {
			var left int

			if err := database.Get(ctx, &left, `SELECT COUNT(*) FROM `+table); err != nil || left != 0 {
				t.Fatalf("%s after deleting the account: %d %v", table, left, err)
			}
		}
	})
}

// TestTwoStepWithoutKeys checks that two-step sign-in cannot be turned on
// without the site's keys.
func TestTwoStepWithoutKeys(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		accounts := service(t, database)

		user, err := accounts.CreateUser(ctx, "anna@example.com", "Anna", twoStepPassword, auth.Editor)
		if err != nil {
			t.Fatal(err)
		}

		if accounts.TwoStepAvailable() {
			t.Fatal("two-step sign-in available without keys")
		}

		if err := accounts.BeginTwoStep(ctx, user.ID); !errors.Is(err, auth.ErrNoSecrets) {
			t.Fatalf("setup without keys: %v", err)
		}
	})
}
