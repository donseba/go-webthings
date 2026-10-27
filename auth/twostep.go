package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/donseba/go-webthings/db"
)

// PendingLifetime is how long a sign-in whose password was right waits for
// the code of its second step (see SignIn).
const PendingLifetime = 5 * time.Minute

const (
	// recoveryCodeCount is how many recovery codes an account gets.
	recoveryCodeCount = 10
	// recoveryLength is the length of a recovery code without its dash:
	// 50 random bits.
	recoveryLength = 10
	// recoveryAlphabet is Crockford's base32 in lowercase: digits and
	// letters without i, l, o and u, which are easily mistaken.
	recoveryAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"
	// secretKeyName is the name of the site's key that encrypts the
	// secrets of authenticator apps (see Secrets).
	secretKeyName = "two_step_sign_in"
	// sealPrefix starts an encrypted secret, so a later way of keeping
	// secrets can tell its own apart.
	sealPrefix = "v1:"
)

var (
	// ErrSecondStep is the correct password of an account with two-step
	// sign-in on, given to Login. Such accounts sign in with SignIn and
	// FinishSignIn.
	ErrSecondStep = errors.New("second_step_required")
	// ErrCode is a code that is wrong or was used already: a code of the
	// authenticator app or a recovery code.
	ErrCode = errors.New("invalid_code")
	// ErrPending is a pending sign-in that is unknown, finished or older
	// than PendingLifetime.
	ErrPending = errors.New("invalid_pending_sign_in")
	// ErrTwoStepOn refuses to set up two-step sign-in while it is on.
	ErrTwoStepOn = errors.New("two_step_on")
	// ErrTwoStepOff is an account without two-step sign-in, or without a
	// setup that waits for its first code.
	ErrTwoStepOff = errors.New("two_step_off")
	// ErrNoSecrets refuses two-step sign-in on a service without the site's
	// keys (see UseSecrets): it could not keep the secrets of
	// authenticator apps.
	ErrNoSecrets = errors.New("two_step_without_keys")

	// errSealed is a stored secret that is not encrypted the way this
	// package encrypts them.
	errSealed = errors.New("two-step secret in an unknown format")
)

// Secrets hands out the site's keys: random keys of 32 bytes by name, the
// same in every process of the site, made on first use. site.Service is
// one (site.Service.Secret).
type Secrets interface {
	Secret(ctx context.Context, name string) ([]byte, error)
}

// UseSecrets gives the service the site's keys. Two-step sign-in encrypts
// the secrets of authenticator apps with one of them (AES-GCM), named
// "two_step_sign_in"; without keys it cannot be turned on (ErrNoSecrets).
// Call it before the service is used.
func (s *Service) UseSecrets(secrets Secrets) {
	s.secrets = secrets
}

// TwoStepAvailable reports whether two-step sign-in can be turned on: the
// service has the site's keys (see UseSecrets).
func (s *Service) TwoStepAvailable() bool {
	return s.secrets != nil
}

// TwoStep is the state of an account's two-step sign-in.
type TwoStep struct {
	// On means that signing in asks for a code after the password.
	On bool
	// Setup means that turning it on waits for the first code of the app
	// (see BeginTwoStep).
	Setup bool
	// RecoveryCodes is the number of recovery codes not used yet.
	RecoveryCodes int
}

// Attempt is what SignIn gives for a correct password: a session, or for
// an account with two-step sign-in on, a pending sign-in that waits for a
// code. One of the two is set.
type Attempt struct {
	// Session is the token of the new session.
	Session string
	// Pending is the token of the pending sign-in, for the browser to keep
	// (see SetPendingCookie) until FinishSignIn turns it into a session,
	// within PendingLifetime.
	Pending string
}

// twoStepRow is an account's row of wt_two_step.
type twoStepRow struct {
	Secret    string     `db:"secret"`
	LastStep  int64      `db:"last_step"`
	EnabledAt *time.Time `db:"enabled_at"`
}

// TwoStep returns the state of the account's two-step sign-in.
func (s *Service) TwoStep(ctx context.Context, userID string) (TwoStep, error) {
	row, err := s.twoStepRow(ctx, userID)
	if errors.Is(err, ErrTwoStepOff) {
		return TwoStep{}, nil
	}

	if err != nil {
		return TwoStep{}, err
	}

	state := TwoStep{On: row.EnabledAt != nil, Setup: row.EnabledAt == nil}

	if state.On {
		err = s.db.Get(ctx, &state.RecoveryCodes, `SELECT COUNT(*) FROM wt_recovery_codes WHERE user_id=?`, userID)
	}

	return state, err
}

// BeginTwoStep starts turning on two-step sign-in for the account: it makes
// a random secret of 20 bytes, which SetupSecret returns for the
// authenticator app, in place of the secret of an earlier setup.
// Two-step sign-in is on only once ConfirmTwoStep receives a code of the
// app. It returns ErrTwoStepOn while it is on already.
func (s *Service) BeginTwoStep(ctx context.Context, userID string) error {
	// The key is read before the transaction: reading it may create it.
	aead, err := s.aead(ctx)
	if err != nil {
		return err
	}

	secret := make([]byte, secretSize)
	_, _ = rand.Read(secret)

	return s.db.Tx(ctx, func(ctx context.Context) error {
		user, err := s.User(ctx, userID)
		if err != nil {
			return err
		}

		on, err := s.twoStepOn(ctx, user.ID)
		if err != nil {
			return err
		}

		if on {
			return ErrTwoStepOn
		}

		if _, err := s.db.Exec(ctx, `DELETE FROM wt_two_step WHERE user_id=?`, user.ID); err != nil {
			return err
		}

		_, err = s.db.Exec(ctx, `
			INSERT INTO wt_two_step(user_id,secret,last_step,created_at)
			VALUES(?,?,?,?)`,
			user.ID, sealSecret(aead, user.ID, secret), 0, s.now().UTC(),
		)

		return err
	})
}

// SetupSecret returns the secret of the account's setup that waits for its
// first code (see BeginTwoStep), to show as a QR code (KeyURI) and as text
// (SecretText). Once two-step sign-in is on, the secret is never shown
// again: SetupSecret then returns ErrTwoStepOff, as it does without a
// setup.
func (s *Service) SetupSecret(ctx context.Context, userID string) ([]byte, error) {
	row, err := s.twoStepRow(ctx, userID)
	if err != nil {
		return nil, err
	}

	if row.EnabledAt != nil {
		return nil, ErrTwoStepOff
	}

	aead, err := s.aead(ctx)
	if err != nil {
		return nil, err
	}

	return openSecret(aead, userID, row.Secret)
}

// ConfirmTwoStep turns two-step sign-in on with a code of the app the setup
// was scanned into, and returns the account's 10 recovery codes, such as
// 4f7k2-9xq3m. Only their digests are stored, so they cannot be shown
// again. A wrong code returns ErrCode and leaves the setup waiting; without
// a setup it returns ErrTwoStepOff.
func (s *Service) ConfirmTwoStep(ctx context.Context, userID, code string) ([]string, error) {
	aead, err := s.aead(ctx)
	if err != nil {
		return nil, err
	}

	now := s.now().UTC()
	codes := newRecoveryCodes()

	err = s.db.Tx(ctx, func(ctx context.Context) error {
		row, err := s.twoStepRow(ctx, userID)
		if err != nil {
			return err
		}

		if row.EnabledAt != nil {
			return ErrTwoStepOn
		}

		secret, err := openSecret(aead, userID, row.Secret)
		if err != nil {
			return err
		}

		step, ok := matchTOTP(secret, normalCode(code), row.LastStep, now)
		if !ok {
			return ErrCode
		}

		_, err = s.db.Exec(ctx,
			`UPDATE wt_two_step SET last_step=?, enabled_at=? WHERE user_id=?`,
			step, now, userID,
		)
		if err != nil {
			return err
		}

		return s.storeRecoveryCodes(ctx, userID, codes, now)
	})
	if err != nil {
		return nil, err
	}

	return codes, nil
}

// CancelTwoStep drops the account's setup that waits for its first code,
// if any. It does not turn off two-step sign-in that is on.
func (s *Service) CancelTwoStep(ctx context.Context, userID string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM wt_two_step WHERE user_id=? AND enabled_at IS NULL`, userID)

	return err
}

// DisableTwoStep turns two-step sign-in off with the account's password and
// a code: of the app or a recovery code. Wrong passwords and codes count
// towards the sign-in limit (ErrRateLimit). It returns ErrTwoStepOff when it
// is off already.
func (s *Service) DisableTwoStep(ctx context.Context, userID, password, code string) error {
	var user User

	err := s.db.Get(ctx, &user, `SELECT `+userColumns+` FROM wt_users WHERE id=?`, userID)
	if errors.Is(err, db.ErrNotFound) {
		return ErrNotFound
	}

	if err != nil {
		return err
	}

	on, err := s.twoStepOn(ctx, user.ID)
	if err != nil {
		return err
	}

	if !on {
		return ErrTwoStepOff
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

	if !verifyPassword(password, user.PasswordHash) {
		if err := s.recordFailure(ctx, key, now); err != nil {
			return err
		}

		return ErrCredentials
	}

	return s.withCode(ctx, user, code, now, func(ctx context.Context) error {
		return s.removeTwoStep(ctx, user.ID)
	})
}

// NewRecoveryCodes replaces the account's recovery codes with 10 new ones,
// with a code: of the app or one of the old recovery codes. Wrong codes
// count towards the sign-in limit (ErrRateLimit).
func (s *Service) NewRecoveryCodes(ctx context.Context, userID, code string) ([]string, error) {
	user, err := s.User(ctx, userID)
	if err != nil {
		return nil, err
	}

	on, err := s.twoStepOn(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	if !on {
		return nil, ErrTwoStepOff
	}

	now := s.now().UTC()

	blocked, err := s.blocked(ctx, digest(user.Email), now)
	if err != nil {
		return nil, err
	}

	if blocked {
		return nil, ErrRateLimit
	}

	codes := newRecoveryCodes()

	err = s.withCode(ctx, user, code, now, func(ctx context.Context) error {
		return s.storeRecoveryCodes(ctx, user.ID, codes, now)
	})
	if err != nil {
		return nil, err
	}

	return codes, nil
}

// RemoveTwoStep turns two-step sign-in off for an account without its
// password or a code, as an administrator does for someone who lost both
// their phone and their recovery codes. The account's pending sign-ins
// end. It returns ErrTwoStepOff when it is off already.
func (s *Service) RemoveTwoStep(ctx context.Context, userID string) error {
	return s.db.Tx(ctx, func(ctx context.Context) error {
		user, err := s.User(ctx, userID)
		if err != nil {
			return err
		}

		on, err := s.twoStepOn(ctx, user.ID)
		if err != nil {
			return err
		}

		if !on {
			return ErrTwoStepOff
		}

		return s.removeTwoStep(ctx, user.ID)
	})
}

// CheckPending reports whether a pending sign-in still waits for its code:
// ErrPending when it is unknown, finished or expired.
func (s *Service) CheckPending(ctx context.Context, pending string) error {
	_, err := s.pendingUser(ctx, pending, s.now().UTC())

	return err
}

// FinishSignIn completes a pending sign-in (see SignIn) with a code of the
// authenticator app or a recovery code, and returns a new session token.
// A previous token from the same browser is revoked. A code of the app
// works for its 30-second step and one step either side, and only once:
// a step that was used, or one before it, is refused. Each recovery code
// works once. Wrong codes return ErrCode and count towards the sign-in
// limit (ErrRateLimit), together with wrong passwords; the pending sign-in
// stays until it expires.
func (s *Service) FinishSignIn(ctx context.Context, pending, code, previous string) (string, error) {
	now := s.now().UTC()

	user, err := s.pendingUser(ctx, pending, now)
	if err != nil {
		return "", err
	}

	key := digest(user.Email)

	blocked, err := s.blocked(ctx, key, now)
	if err != nil {
		return "", err
	}

	if blocked {
		return "", ErrRateLimit
	}

	token := rand.Text() + rand.Text()

	err = s.withCode(ctx, user, code, now, func(ctx context.Context) error {
		result, err := s.db.Exec(ctx, `DELETE FROM wt_pending_sign_ins WHERE token_hash=?`, digest(pending))

		err = db.Affected(result, err)
		if errors.Is(err, db.ErrNotFound) {
			// Another request finished it first.
			return ErrPending
		}

		if err != nil {
			return err
		}

		return s.startSession(ctx, user.ID, key, token, previous, now)
	})
	if err != nil {
		return "", err
	}

	return token, nil
}

// startPending starts a pending sign-in of the account and returns its
// token.
func (s *Service) startPending(ctx context.Context, userID string, now time.Time) (string, error) {
	token := rand.Text() + rand.Text()

	_, err := s.db.Exec(ctx, `
		INSERT INTO wt_pending_sign_ins(token_hash,user_id,expires_at,created_at)
		VALUES(?,?,?,?)`,
		digest(token), userID, now.Add(PendingLifetime), now,
	)
	if err != nil {
		return "", err
	}

	return token, nil
}

// pendingUser is the active account of a pending sign-in that still waits
// for its code.
func (s *Service) pendingUser(ctx context.Context, pending string, now time.Time) (User, error) {
	if len(pending) < 32 || len(pending) > 128 {
		return User{}, ErrPending
	}

	var user User

	err := s.db.Get(ctx, &user, `
		SELECT u.* FROM wt_users u
		JOIN wt_pending_sign_ins p ON p.user_id=u.id
		WHERE p.token_hash=? AND p.expires_at>? AND u.status=?`,
		digest(pending), now, Active,
	)
	if errors.Is(err, db.ErrNotFound) {
		return User{}, ErrPending
	}

	if err != nil {
		return User{}, err
	}

	user.PasswordHash = ""

	return user, nil
}

// withCode uses up code for the account, a code of the app or a recovery
// code, and runs fn in the same transaction. A wrong code returns ErrCode
// and counts towards the sign-in limit of the account's e-mail address.
func (s *Service) withCode(ctx context.Context, user User, code string, now time.Time, fn func(context.Context) error) error {
	code = normalCode(code)

	// The key is read before the transaction: reading it may create it.
	var aead cipher.AEAD

	if appCode(code) {
		var err error

		aead, err = s.aead(ctx)
		if err != nil {
			return err
		}
	}

	err := s.db.Tx(ctx, func(ctx context.Context) error {
		if err := s.useCode(ctx, aead, user.ID, code, now); err != nil {
			return err
		}

		return fn(ctx)
	})

	// The failure is recorded outside the transaction, which rolled back.
	if errors.Is(err, ErrCode) {
		if err := s.recordFailure(ctx, digest(user.Email), now); err != nil {
			return err
		}
	}

	return err
}

// useCode uses up a code of the app or a recovery code of the account, as
// normalCode wrote it, or returns ErrCode. aead opens the account's secret
// and is needed for codes of the app only.
func (s *Service) useCode(ctx context.Context, aead cipher.AEAD, userID, code string, now time.Time) error {
	switch {
	case appCode(code):
		row, err := s.twoStepRow(ctx, userID)
		if errors.Is(err, ErrTwoStepOff) || (err == nil && row.EnabledAt == nil) {
			return ErrCode
		}

		if err != nil {
			return err
		}

		secret, err := openSecret(aead, userID, row.Secret)
		if err != nil {
			return err
		}

		step, ok := matchTOTP(secret, code, row.LastStep, now)
		if !ok {
			return ErrCode
		}

		// The step is recorded only if no other request recorded it or a
		// later one first.
		result, err := s.db.Exec(ctx,
			`UPDATE wt_two_step SET last_step=? WHERE user_id=? AND last_step<?`,
			step, userID, step,
		)

		return codeUsed(result, err)
	case recoveryCode(code):
		result, err := s.db.Exec(ctx,
			`DELETE FROM wt_recovery_codes WHERE user_id=? AND code_hash=?`,
			userID, digest(code),
		)

		return codeUsed(result, err)
	default:
		return ErrCode
	}
}

// codeUsed is ErrCode for a statement that changed nothing: the code was
// used by then.
func codeUsed(result sql.Result, err error) error {
	err = db.Affected(result, err)
	if errors.Is(err, db.ErrNotFound) {
		return ErrCode
	}

	return err
}

// twoStepRow is the account's row of wt_two_step, or ErrTwoStepOff without
// one.
func (s *Service) twoStepRow(ctx context.Context, userID string) (twoStepRow, error) {
	var row twoStepRow

	err := s.db.Get(ctx, &row, `SELECT secret, last_step, enabled_at FROM wt_two_step WHERE user_id=?`, userID)
	if errors.Is(err, db.ErrNotFound) {
		return twoStepRow{}, ErrTwoStepOff
	}

	return row, err
}

// twoStepOn reports whether the account has two-step sign-in on.
func (s *Service) twoStepOn(ctx context.Context, userID string) (bool, error) {
	var count int

	err := s.db.Get(ctx, &count,
		`SELECT COUNT(*) FROM wt_two_step WHERE user_id=? AND enabled_at IS NOT NULL`,
		userID,
	)

	return count > 0, err
}

// removeTwoStep deletes the account's secret, recovery codes and pending
// sign-ins.
func (s *Service) removeTwoStep(ctx context.Context, userID string) error {
	for _, query := range []string{
		`DELETE FROM wt_two_step WHERE user_id=?`,
		`DELETE FROM wt_recovery_codes WHERE user_id=?`,
		`DELETE FROM wt_pending_sign_ins WHERE user_id=?`,
	} {
		if _, err := s.db.Exec(ctx, query, userID); err != nil {
			return err
		}
	}

	return nil
}

// storeRecoveryCodes keeps the digests of codes as the account's recovery
// codes, in place of the ones it had.
func (s *Service) storeRecoveryCodes(ctx context.Context, userID string, codes []string, now time.Time) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM wt_recovery_codes WHERE user_id=?`, userID); err != nil {
		return err
	}

	for _, code := range codes {
		_, err := s.db.Exec(ctx,
			`INSERT INTO wt_recovery_codes(user_id,code_hash,created_at) VALUES(?,?,?)`,
			userID, digest(normalCode(code)), now,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// newRecoveryCodes makes 10 random recovery codes, such as 4f7k2-9xq3m.
func newRecoveryCodes() []string {
	codes := make([]string, 0, recoveryCodeCount)

	for len(codes) < recoveryCodeCount {
		random := make([]byte, recoveryLength)
		_, _ = rand.Read(random)

		var code strings.Builder

		for i, b := range random {
			if i == recoveryLength/2 {
				code.WriteByte('-')
			}

			code.WriteByte(recoveryAlphabet[b&31])
		}

		codes = append(codes, code.String())
	}

	return codes
}

// normalCode is a code as typed, in the form it is checked in: lowercase,
// without spaces and dashes, and with o, i and l read as 0, 1 and 1, the
// characters recovery codes have instead. A code longer than any code is
// returned empty.
func normalCode(code string) string {
	if len(code) > 64 {
		return ""
	}

	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '\t':
			return -1
		case 'o':
			return '0'
		case 'i', 'l':
			return '1'
		default:
			return r
		}
	}, strings.ToLower(code))
}

// appCode reports whether a normalCode is a code of an authenticator app:
// 6 digits.
func appCode(code string) bool {
	return len(code) == 6 && strings.Trim(code, "0123456789") == ""
}

// recoveryCode reports whether a normalCode can be a recovery code.
func recoveryCode(code string) bool {
	return len(code) == recoveryLength && strings.Trim(code, recoveryAlphabet) == ""
}

// aead is the cipher of the secrets of authenticator apps: AES-256-GCM
// with the site's key (see UseSecrets), and a random nonce before every
// sealed secret.
func (s *Service) aead(ctx context.Context) (cipher.AEAD, error) {
	if s.secrets == nil {
		return nil, ErrNoSecrets
	}

	key, err := s.secrets.Secret(ctx, secretKeyName)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	return cipher.NewGCMWithRandomNonce(block)
}

// sealSecret encrypts the secret of the account. The account's ID is
// authenticated with it, so the result does not open for another account.
func sealSecret(aead cipher.AEAD, userID string, secret []byte) string {
	return sealPrefix + base64.RawStdEncoding.EncodeToString(aead.Seal(nil, nil, secret, []byte(userID)))
}

// openSecret decrypts what sealSecret made for the account.
func openSecret(aead cipher.AEAD, userID, sealed string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(sealed, sealPrefix)
	if !ok {
		return nil, errSealed
	}

	data, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errSealed
	}

	return aead.Open(nil, nil, data, []byte(userID))
}
