package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/donseba/go-webthings/db"
)

// ErrResetToken is returned for a password reset link that is unknown,
// used or expired.
var ErrResetToken = errors.New("invalid_reset_token")

const (
	// ResetLifetime is how long a password reset link works.
	ResetLifetime = time.Hour
	// maxResets is how many reset links one account gets per ResetLifetime,
	// so nobody can flood an inbox with them.
	maxResets = 3
)

// RequestPasswordReset creates a one-time token for the active account with
// email, to send in a reset link; only its digest is stored. It returns
// ErrNotFound when there is no such active account and ErrRateLimit after
// three links within an hour. Callers must answer the same in every case, so
// the answer does not reveal which addresses have an account.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) (string, User, error) {
	email, ok := normalEmail(email)
	if !ok {
		return "", User{}, ErrNotFound
	}

	var (
		token string
		user  User
	)

	err := s.db.Tx(ctx, func(ctx context.Context) error {
		err := s.db.Get(ctx, &user, `SELECT `+userColumns+` FROM wt_users WHERE email=? AND status=?`, email, Active)
		if errors.Is(err, db.ErrNotFound) {
			return ErrNotFound
		}

		if err != nil {
			return err
		}

		now := s.now().UTC()

		var recent int

		err = s.db.Get(ctx, &recent,
			`SELECT COUNT(*) FROM wt_password_resets WHERE user_id=? AND created_at>?`,
			user.ID, now.Add(-ResetLifetime),
		)
		if err != nil {
			return err
		}

		if recent >= maxResets {
			return ErrRateLimit
		}

		token = rand.Text() + rand.Text()

		_, err = s.db.Exec(ctx,
			`INSERT INTO wt_password_resets(token_hash,user_id,expires_at,created_at) VALUES(?,?,?,?)`,
			digest(token), user.ID, now.Add(ResetLifetime), now,
		)

		return err
	})
	if err != nil {
		return "", User{}, err
	}

	user.PasswordHash = ""

	return token, user, nil
}

// CheckResetToken reports whether a reset token can still be used, so the
// form to choose a new password is only shown for working links.
func (s *Service) CheckResetToken(ctx context.Context, token string) error {
	_, err := s.resetUser(ctx, token)

	return err
}

// ResetPassword sets a new password with a reset token and uses the token
// up, with every other reset link of the account. All sessions of the
// account are revoked, its pending sign-ins end and its failed sign-ins are
// forgotten, so it signs in anew with the new password: with two-step
// sign-in on, also with a code (see SignIn).
func (s *Service) ResetPassword(ctx context.Context, token, password string) error {
	if !validPassword(password) {
		return ErrPassword
	}

	hash, err := hashPassword(password)
	if err != nil {
		return err
	}

	return s.db.Tx(ctx, func(ctx context.Context) error {
		user, err := s.resetUser(ctx, token)
		if err != nil {
			return err
		}

		now := s.now().UTC()

		_, err = s.db.Exec(ctx, `UPDATE wt_users SET password_hash=?, updated_at=?, updated_by=? WHERE id=?`,
			hash, now, db.Actor(ctx), user.ID,
		)
		if err != nil {
			return err
		}

		if _, err := s.db.Exec(ctx, `DELETE FROM wt_password_resets WHERE user_id=?`, user.ID); err != nil {
			return err
		}

		if _, err := s.db.Exec(ctx, `DELETE FROM wt_pending_sign_ins WHERE user_id=?`, user.ID); err != nil {
			return err
		}

		if _, err := s.db.Exec(ctx, `DELETE FROM wt_login_attempts WHERE key_hash=?`, digest(user.Email)); err != nil {
			return err
		}

		_, err = s.db.Exec(ctx,
			`UPDATE wt_sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`,
			now, user.ID,
		)

		return err
	})
}

// resetUser is the active account a working reset token belongs to.
func (s *Service) resetUser(ctx context.Context, token string) (User, error) {
	if len(token) < 32 || len(token) > 128 {
		return User{}, ErrResetToken
	}

	var user User

	err := s.db.Get(ctx, &user, `
		SELECT u.* FROM wt_users u
		JOIN wt_password_resets r ON r.user_id=u.id
		WHERE r.token_hash=? AND r.expires_at>? AND u.status=?`,
		digest(token), s.now().UTC(), Active,
	)
	if errors.Is(err, db.ErrNotFound) {
		return User{}, ErrResetToken
	}

	return user, err
}
