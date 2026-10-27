package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
)

func service(t *testing.T, database *db.DB) *auth.Service {
	t.Helper()

	if err := database.ApplySchema(context.Background(), "auth", auth.Schema()); err != nil {
		t.Fatal(err)
	}

	service, err := auth.New(database)
	if err != nil {
		t.Fatal(err)
	}

	return service
}

func TestLoginSessionsAndStatus(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		accounts := service(t, database)

		if err := accounts.Bootstrap(ctx, "Admin@Example.com", "correct horse battery"); err != nil {
			t.Fatal(err)
		}

		if err := accounts.Bootstrap(ctx, "other@example.com", "correct horse battery"); !errors.Is(err, auth.ErrBootstrap) {
			t.Fatalf("second bootstrap: %v", err)
		}

		if _, err := accounts.Login(ctx, "admin@example.com", "wrong password!", ""); !errors.Is(err, auth.ErrCredentials) {
			t.Fatalf("wrong password: %v", err)
		}

		_, err := accounts.Login(ctx, "nobody@example.com", "correct horse battery", "")
		if !errors.Is(err, auth.ErrCredentials) {
			t.Fatalf("unknown account: %v", err)
		}

		token, err := accounts.Login(ctx, "admin@example.com", "correct horse battery", "")
		if err != nil {
			t.Fatal(err)
		}

		admin, err := accounts.Authenticate(ctx, token)
		if err != nil || admin.Role != auth.Admin || admin.PasswordHash != "" {
			t.Fatalf("authenticate: %+v %v", admin, err)
		}

		// Logging in again from the same browser revokes the old token.
		second, err := accounts.Login(ctx, "admin@example.com", "correct horse battery", token)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := accounts.Authenticate(ctx, token); !errors.Is(err, auth.ErrSession) {
			t.Fatalf("old token still valid: %v", err)
		}

		if err := accounts.SetStatus(ctx, admin.ID, auth.Disabled); !errors.Is(err, auth.ErrLastAdmin) {
			t.Fatalf("last admin disabled: %v", err)
		}

		editor, err := accounts.CreateUser(ctx, "editor@example.com", "Ed", "another long password", auth.Editor)
		if err != nil {
			t.Fatal(err)
		}

		_, err = accounts.CreateUser(ctx, "editor@example.com", "", "another long password", auth.Editor)
		if !errors.Is(err, auth.ErrDuplicate) {
			t.Fatalf("duplicate email: %v", err)
		}

		editorToken, err := accounts.Login(ctx, "editor@example.com", "another long password", "")
		if err != nil {
			t.Fatal(err)
		}

		if err := accounts.SetStatus(ctx, editor.ID, auth.Disabled); err != nil {
			t.Fatal(err)
		}

		if _, err := accounts.Authenticate(ctx, editorToken); !errors.Is(err, auth.ErrSession) {
			t.Fatalf("disabled account still signed in: %v", err)
		}

		if err := accounts.Logout(ctx, second); err != nil {
			t.Fatal(err)
		}

		if _, err := accounts.Authenticate(ctx, second); !errors.Is(err, auth.ErrSession) {
			t.Fatalf("logged out token valid: %v", err)
		}
	})
}

func TestLoginThrottling(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		accounts := service(t, database)

		if err := accounts.Bootstrap(ctx, "admin@example.com", "correct horse battery"); err != nil {
			t.Fatal(err)
		}

		for range 10 {
			_, _ = accounts.Login(ctx, "admin@example.com", "wrong password!", "")
		}

		if _, err := accounts.Login(ctx, "admin@example.com", "correct horse battery", ""); !errors.Is(err, auth.ErrRateLimit) {
			t.Fatalf("want rate limit, got %v", err)
		}
	})
}

// TestLastSeen checks that Authenticate records a session's activity at
// most once a minute, so admin requests do not all write.
func TestLastSeen(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		accounts := service(t, database)
		now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

		auth.SetClock(accounts, func() time.Time { return now })

		if err := accounts.Bootstrap(ctx, "admin@example.com", "correct horse battery"); err != nil {
			t.Fatal(err)
		}

		token, err := accounts.Login(ctx, "admin@example.com", "correct horse battery", "")
		if err != nil {
			t.Fatal(err)
		}

		seen := func() time.Time {
			t.Helper()

			admin, err := accounts.Authenticate(ctx, token)
			if err != nil {
				t.Fatal(err)
			}

			sessions, err := accounts.Sessions(ctx, admin.ID)
			if err != nil || len(sessions) != 1 || sessions[0].LastSeenAt == nil {
				t.Fatalf("sessions: %+v %v", sessions, err)
			}

			return sessions[0].LastSeenAt.UTC()
		}

		first := now

		if got := seen(); !got.Equal(first) {
			t.Fatalf("first request: %v", got)
		}

		now = now.Add(30 * time.Second)

		if got := seen(); !got.Equal(first) {
			t.Fatalf("recorded again within a minute: %v", got)
		}

		now = now.Add(31 * time.Second)

		if got := seen(); !got.Equal(now) {
			t.Fatalf("not recorded after a minute: %v, want %v", got, now)
		}
	})
}

// TestLoginTakesPasswordAsTyped signs in with a password that starts and
// ends with a space: only the password exactly as set is accepted, also
// after a change of password.
func TestLoginTakesPasswordAsTyped(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		accounts := service(t, database)

		if _, err := accounts.CreateUser(
			ctx,
			"spaced@example.test",
			"Spaced",
			" correct horse battery ",
			auth.Editor,
		); err != nil {
			t.Fatal(err)
		}

		if _, err := accounts.Login(ctx, "spaced@example.test", "correct horse battery", ""); err == nil {
			t.Error("signed in without the spaces the password was set with")
		}

		if _, err := accounts.Login(ctx, "spaced@example.test", " correct horse battery ", ""); err != nil {
			t.Errorf("signing in with the password as set: %v", err)
		}

		if _, err := accounts.CreateUser(ctx, "plain@example.test", "Plain", "correct horse battery", auth.Editor); err != nil {
			t.Fatal(err)
		}

		if _, err := accounts.Login(ctx, "plain@example.test", " correct horse battery ", ""); err == nil {
			t.Error("signed in with spaces the password was not set with")
		}
	})
}
