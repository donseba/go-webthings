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

const (
	adminPassword  = "correct horse battery"
	editorPassword = "another long password"
)

func TestSetupAdmin(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		accounts := service(t, database)

		exists, err := accounts.HasUsers(ctx)
		if err != nil || exists {
			t.Fatalf("empty database has users: %v %v", exists, err)
		}

		if _, err := accounts.SetupAdmin(ctx, "not an address", "", adminPassword); !errors.Is(err, auth.ErrEmail) {
			t.Fatalf("invalid address: %v", err)
		}

		owner, err := accounts.SetupAdmin(ctx, "Owner@Example.com", " Owner ", adminPassword)
		if err != nil || owner.Role != auth.Admin || owner.Name != "Owner" || owner.Email != "owner@example.com" {
			t.Fatalf("setup: %+v %v", owner, err)
		}

		if _, err := accounts.SetupAdmin(ctx, "other@example.com", "", adminPassword); !errors.Is(err, auth.ErrBootstrap) {
			t.Fatalf("second setup: %v", err)
		}

		if exists, _ := accounts.HasUsers(ctx); !exists {
			t.Fatal("the first account is not counted")
		}
	})
}

func TestUpdateAndDeleteUsers(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		accounts := service(t, database)

		owner, err := accounts.SetupAdmin(ctx, "owner@example.com", "", adminPassword)
		if err != nil {
			t.Fatal(err)
		}

		editor, err := accounts.CreateUser(ctx, "editor@example.com", "Ed", editorPassword, auth.Editor)
		if err != nil {
			t.Fatal(err)
		}

		if err := accounts.UpdateUser(ctx, editor.ID, "Edith@Example.com", " Edith "); err != nil {
			t.Fatal(err)
		}

		changed, err := accounts.User(ctx, editor.ID)
		if err != nil || changed.Email != "edith@example.com" || changed.Name != "Edith" || changed.PasswordHash != "" {
			t.Fatalf("updated account: %+v %v", changed, err)
		}

		if err := accounts.UpdateUser(ctx, editor.ID, "owner@example.com", ""); !errors.Is(err, auth.ErrDuplicate) {
			t.Fatalf("address of another account: %v", err)
		}

		if err := accounts.UpdateUser(ctx, editor.ID, "nope", ""); !errors.Is(err, auth.ErrEmail) {
			t.Fatalf("invalid address: %v", err)
		}

		if err := accounts.UpdateUser(ctx, db.NewID(), "x@example.com", ""); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("unknown account: %v", err)
		}

		// The new address signs in, the old one no longer.
		if _, err := accounts.Login(ctx, "editor@example.com", editorPassword, ""); !errors.Is(err, auth.ErrCredentials) {
			t.Fatalf("old address: %v", err)
		}

		token, err := accounts.Login(ctx, "edith@example.com", editorPassword, "")
		if err != nil {
			t.Fatal(err)
		}

		group, err := accounts.SaveGroup(ctx, auth.Group{Name: "Writers"})
		if err != nil {
			t.Fatal(err)
		}

		if err := accounts.SetUserGroups(ctx, editor.ID, []string{group.ID}); err != nil {
			t.Fatal(err)
		}

		if _, _, err := accounts.RequestPasswordReset(ctx, "edith@example.com"); err != nil {
			t.Fatal(err)
		}

		if err := accounts.DeleteUser(ctx, owner.ID); !errors.Is(err, auth.ErrLastAdmin) {
			t.Fatalf("last admin deleted: %v", err)
		}

		if err := accounts.DeleteUser(ctx, editor.ID); err != nil {
			t.Fatal(err)
		}

		if _, err := accounts.Authenticate(ctx, token); !errors.Is(err, auth.ErrSession) {
			t.Fatalf("session of a deleted account: %v", err)
		}

		if _, err := accounts.User(ctx, editor.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("deleted account found: %v", err)
		}

		if err := accounts.DeleteUser(ctx, editor.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("second delete: %v", err)
		}

		// With a second administrator, the first can go.
		second, err := accounts.CreateUser(ctx, "second@example.com", "", adminPassword, auth.Admin)
		if err != nil {
			t.Fatal(err)
		}

		if err := accounts.DeleteUser(ctx, owner.ID); err != nil {
			t.Fatal(err)
		}

		if err := accounts.DeleteUser(ctx, second.ID); !errors.Is(err, auth.ErrLastAdmin) {
			t.Fatalf("last admin deleted: %v", err)
		}
	})
}

func TestChangePassword(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		accounts := service(t, database)

		owner, err := accounts.SetupAdmin(ctx, "owner@example.com", "", adminPassword)
		if err != nil {
			t.Fatal(err)
		}

		here, err := accounts.Login(ctx, "owner@example.com", adminPassword, "")
		if err != nil {
			t.Fatal(err)
		}

		elsewhere, err := accounts.Login(ctx, "owner@example.com", adminPassword, "")
		if err != nil {
			t.Fatal(err)
		}

		err = accounts.ChangePassword(ctx, owner.ID, "wrong password!", "a new long password", here)
		if !errors.Is(err, auth.ErrCredentials) {
			t.Fatalf("wrong current password: %v", err)
		}

		if err := accounts.ChangePassword(ctx, owner.ID, adminPassword, "short", here); !errors.Is(err, auth.ErrPassword) {
			t.Fatalf("short password: %v", err)
		}

		if err := accounts.ChangePassword(ctx, owner.ID, adminPassword, "a new long password", here); err != nil {
			t.Fatal(err)
		}

		if _, err := accounts.Authenticate(ctx, here); err != nil {
			t.Fatalf("the session of the change was revoked: %v", err)
		}

		if _, err := accounts.Authenticate(ctx, elsewhere); !errors.Is(err, auth.ErrSession) {
			t.Fatalf("another session survived the change: %v", err)
		}

		if _, err := accounts.Login(ctx, "owner@example.com", adminPassword, ""); !errors.Is(err, auth.ErrCredentials) {
			t.Fatalf("old password: %v", err)
		}

		if _, err := accounts.Login(ctx, "owner@example.com", "a new long password", ""); err != nil {
			t.Fatal(err)
		}

		// Guessing the current password is throttled like signing in.
		for range 10 {
			_ = accounts.ChangePassword(ctx, owner.ID, "wrong password!", "yet another password", here)
		}

		err = accounts.ChangePassword(ctx, owner.ID, "a new long password", "yet another password", here)
		if !errors.Is(err, auth.ErrRateLimit) {
			t.Fatalf("want rate limit, got %v", err)
		}
	})
}

func TestPasswordReset(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		accounts := service(t, database)
		now := time.Now()
		auth.SetClock(accounts, func() time.Time { return now })

		if err := accounts.Bootstrap(ctx, "owner@example.com", adminPassword); err != nil {
			t.Fatal(err)
		}

		if _, _, err := accounts.RequestPasswordReset(ctx, "nobody@example.com"); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("unknown address: %v", err)
		}

		session, err := accounts.Login(ctx, "owner@example.com", adminPassword, "")
		if err != nil {
			t.Fatal(err)
		}

		// A locked-out account can still reset its password.
		for range 10 {
			_, _ = accounts.Login(ctx, "owner@example.com", "wrong password!", "")
		}

		token, user, err := accounts.RequestPasswordReset(ctx, " Owner@Example.com ")
		if err != nil || user.Email != "owner@example.com" || len(token) < 32 {
			t.Fatalf("reset request: %q %+v %v", token, user, err)
		}

		if err := accounts.CheckResetToken(ctx, token); err != nil {
			t.Fatal(err)
		}

		if err := accounts.CheckResetToken(ctx, token+"x"); !errors.Is(err, auth.ErrResetToken) {
			t.Fatalf("unknown token: %v", err)
		}

		if err := accounts.ResetPassword(ctx, token, "short"); !errors.Is(err, auth.ErrPassword) {
			t.Fatalf("short password: %v", err)
		}

		if err := accounts.ResetPassword(ctx, token, "a fresh long password"); err != nil {
			t.Fatal(err)
		}

		if _, err := accounts.Authenticate(ctx, session); !errors.Is(err, auth.ErrSession) {
			t.Fatalf("session survived the reset: %v", err)
		}

		if err := accounts.ResetPassword(ctx, token, "another fresh password"); !errors.Is(err, auth.ErrResetToken) {
			t.Fatalf("token used twice: %v", err)
		}

		if _, err := accounts.Login(ctx, "owner@example.com", "a fresh long password", ""); err != nil {
			t.Fatalf("new password after a lock-out: %v", err)
		}

		// Links expire after an hour.
		token, _, err = accounts.RequestPasswordReset(ctx, "owner@example.com")
		if err != nil {
			t.Fatal(err)
		}

		now = now.Add(auth.ResetLifetime + time.Minute)

		if err := accounts.ResetPassword(ctx, token, "an expired password"); !errors.Is(err, auth.ErrResetToken) {
			t.Fatalf("expired token: %v", err)
		}

		// Three links an hour at most.
		for range 3 {
			if _, _, err := accounts.RequestPasswordReset(ctx, "owner@example.com"); err != nil {
				t.Fatal(err)
			}
		}

		if _, _, err := accounts.RequestPasswordReset(ctx, "owner@example.com"); !errors.Is(err, auth.ErrRateLimit) {
			t.Fatalf("want rate limit, got %v", err)
		}

		// Disabled accounts get no link.
		editor, err := accounts.CreateUser(ctx, "editor@example.com", "", editorPassword, auth.Editor)
		if err != nil {
			t.Fatal(err)
		}

		token, _, err = accounts.RequestPasswordReset(ctx, "editor@example.com")
		if err != nil {
			t.Fatal(err)
		}

		if err := accounts.SetStatus(ctx, editor.ID, auth.Disabled); err != nil {
			t.Fatal(err)
		}

		if err := accounts.CheckResetToken(ctx, token); !errors.Is(err, auth.ErrResetToken) {
			t.Fatalf("token of a disabled account: %v", err)
		}

		if _, _, err := accounts.RequestPasswordReset(ctx, "editor@example.com"); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("disabled account: %v", err)
		}
	})
}

func TestCleanup(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		accounts := service(t, database)
		now := time.Now()
		auth.SetClock(accounts, func() time.Time { return now })

		owner, err := accounts.SetupAdmin(ctx, "owner@example.com", "", adminPassword)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := accounts.Login(ctx, "owner@example.com", adminPassword, ""); err != nil {
			t.Fatal(err)
		}

		_, _ = accounts.Login(ctx, "nobody@example.com", "wrong password!", "")

		if _, _, err := accounts.RequestPasswordReset(ctx, "owner@example.com"); err != nil {
			t.Fatal(err)
		}

		sessions, err := accounts.Sessions(ctx, owner.ID)
		if err != nil || len(sessions) != 1 {
			t.Fatalf("sessions: %v %v", sessions, err)
		}

		now = now.Add(13 * time.Hour)

		// Expired sessions are not listed, even before they are cleaned up.
		if sessions, _ := accounts.Sessions(ctx, owner.ID); len(sessions) != 0 {
			t.Fatalf("expired sessions listed: %v", sessions)
		}

		if err := accounts.Cleanup(ctx); err != nil {
			t.Fatal(err)
		}

		for _, table := range []string{"wt_sessions", "wt_login_attempts", "wt_password_resets"} {
			var count int

			if err := database.Get(ctx, &count, `SELECT COUNT(*) FROM `+table); err != nil {
				t.Fatal(err)
			}

			if count != 0 {
				t.Errorf("%s keeps %d expired rows", table, count)
			}
		}
	})
}
