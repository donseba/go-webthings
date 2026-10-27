package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/donseba/go-webthings/db"

	form "github.com/donseba/go-form/v2"
)

// HasUsers reports whether any account exists. Without one, the admin offers
// its first-run setup.
func (s *Service) HasUsers(ctx context.Context) (bool, error) {
	var count int

	if err := s.db.Get(ctx, &count, `SELECT COUNT(*) FROM wt_users`); err != nil {
		return false, err
	}

	return count > 0, nil
}

// SetupAdmin creates the first account, an administrator, as Bootstrap does,
// with a name. It returns ErrBootstrap once any account exists, also when
// another request created one first.
func (s *Service) SetupAdmin(ctx context.Context, email, name, password string) (User, error) {
	var user User

	err := s.db.Tx(ctx, func(ctx context.Context) error {
		exists, err := s.HasUsers(ctx)
		if err != nil {
			return err
		}

		if exists {
			return ErrBootstrap
		}

		user, err = s.CreateUser(ctx, email, name, password, Admin)

		return err
	})
	if err != nil {
		return User{}, err
	}

	return user, nil
}

// User returns one account, without its password hash.
func (s *Service) User(ctx context.Context, userID string) (User, error) {
	var user User

	err := s.db.Get(ctx, &user, `SELECT `+userColumns+` FROM wt_users WHERE id=?`, userID)
	if errors.Is(err, db.ErrNotFound) {
		return User{}, ErrNotFound
	}

	if err != nil {
		return User{}, err
	}

	user.PasswordHash = ""

	return user, nil
}

// UpdateUser changes an account's e-mail address and name. The address must
// be valid and not in use by another account.
func (s *Service) UpdateUser(ctx context.Context, userID, email, name string) error {
	email, ok := normalEmail(email)
	if !ok {
		return ErrEmail
	}

	name = strings.TrimSpace(name)
	if len(name) > 200 {
		name = name[:200]
	}

	return s.db.Tx(ctx, func(ctx context.Context) error {
		if _, err := s.User(ctx, userID); err != nil {
			return err
		}

		_, err := s.db.Exec(ctx,
			`UPDATE wt_users SET email=?, name=?, updated_at=?, updated_by=? WHERE id=?`,
			email, name, s.now().UTC(), db.Actor(ctx), userID,
		)
		if db.IsUnique(err) {
			return ErrDuplicate
		}

		return err
	})
}

// DeleteUser removes an account with its sessions, group memberships and
// reset links. The last active administrator cannot be removed.
func (s *Service) DeleteUser(ctx context.Context, userID string) error {
	return s.db.Tx(ctx, func(ctx context.Context) error {
		user, err := s.User(ctx, userID)
		if err != nil {
			return err
		}

		if user.Role == Admin && user.Status == Active {
			if err := s.keepAdmin(ctx, userID); err != nil {
				return err
			}
		}

		// The foreign keys cascade as well; deleting explicitly does not
		// depend on SQLite having them switched on.
		for _, query := range []string{
			`DELETE FROM wt_sessions WHERE user_id=?`,
			`DELETE FROM wt_group_members WHERE user_id=?`,
			`DELETE FROM wt_password_resets WHERE user_id=?`,
			`DELETE FROM wt_users WHERE id=?`,
		} {
			if _, err := s.db.Exec(ctx, query, userID); err != nil {
				return err
			}
		}

		return nil
	})
}

// keepAdmin returns ErrLastAdmin when no active administrator other than
// userID exists.
func (s *Service) keepAdmin(ctx context.Context, userID string) error {
	var others int

	err := s.db.Get(ctx, &others,
		`SELECT COUNT(*) FROM wt_users WHERE id<>? AND role=? AND status=?`,
		userID, Admin, Active,
	)
	if err != nil {
		return err
	}

	if others == 0 {
		return ErrLastAdmin
	}

	return nil
}

// ChangePassword sets a new password after checking the current one. Wrong
// guesses count towards the same limit as failed sign-ins. Every other
// session of the account is revoked; the session with token keep, the one
// the change was made in, stays signed in.
func (s *Service) ChangePassword(ctx context.Context, userID, current, password, keep string) error {
	var user User

	err := s.db.Get(ctx, &user, `SELECT `+userColumns+` FROM wt_users WHERE id=?`, userID)
	if errors.Is(err, db.ErrNotFound) {
		return ErrNotFound
	}

	if err != nil {
		return err
	}

	key := digest(user.Email)
	now := s.now().UTC()

	blocked, err := s.blocked(ctx, key, now)
	if err != nil {
		return err
	}

	if blocked {
		return ErrRateLimit
	}

	if !verifyPassword(current, user.PasswordHash) {
		if err := s.recordFailure(ctx, key, now); err != nil {
			return err
		}

		return ErrCredentials
	}

	if !validPassword(password) {
		return ErrPassword
	}

	hash, err := hashPassword(password)
	if err != nil {
		return err
	}

	return s.db.Tx(ctx, func(ctx context.Context) error {
		_, err := s.db.Exec(ctx, `UPDATE wt_users SET password_hash=?, updated_at=?, updated_by=? WHERE id=?`,
			hash, now, db.Actor(ctx), userID,
		)
		if err != nil {
			return err
		}

		_, err = s.db.Exec(ctx,
			`UPDATE wt_sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL AND token_hash<>?`,
			now, userID, digest(keep),
		)

		return err
	})
}

// normalEmail lowercases and trims an address and reports whether it is a
// plain, valid address of at most 254 characters, as go-form checks email
// inputs (form.ValidEmail).
func normalEmail(email string) (string, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !form.ValidEmail(email) {
		return "", false
	}

	return email, true
}

// validPassword checks the length rules: 12 to 1024 bytes.
func validPassword(password string) bool {
	return len(password) >= 12 && len(password) <= 1024
}
