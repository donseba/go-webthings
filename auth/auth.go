// Package auth keeps the accounts of the people who edit the site, their
// sessions and their groups of rights. Passwords are stored as Argon2id
// hashes, and sessions only as a SHA-256 digest of their token, so a copy
// of the database lets nobody sign in. Visitors of the public site have no
// accounts.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/donseba/go-webthings/db"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// Schema holds this package's schema files per dialect (see db.ApplySchema).
func Schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}

// Role is what an account may do in general. Administrators may do
// everything; editors may do what their groups allow.
type Role string

const (
	// Admin may do everything, including managing accounts and groups.
	Admin Role = "admin"
	// Editor may do what the account's groups allow, and nothing else.
	Editor Role = "editor"
)

// Status tells whether an account may sign in.
type Status string

const (
	// Active accounts may sign in.
	Active Status = "active"
	// Disabled accounts are kept, with their name on what they changed,
	// but may not sign in; disabling one ends its sessions.
	Disabled Status = "disabled"
)

var (
	// ErrCredentials is a sign-in with an unknown e-mail address or a wrong
	// password. The two are not told apart, so the answer reveals no
	// accounts.
	ErrCredentials = errors.New("invalid_credentials")
	// ErrSession is a session token that is unknown, expired, revoked or
	// belongs to a disabled account.
	ErrSession = errors.New("invalid_session")
	// ErrBootstrap refuses the first-run setup once any account exists.
	ErrBootstrap = errors.New("bootstrap_requires_empty_accounts")
	// ErrPassword is a password shorter than 12 characters (or absurdly
	// long).
	ErrPassword = errors.New("password_too_short")
	// ErrEmail is an e-mail address that is not one.
	ErrEmail = errors.New("invalid_email")
	// ErrDuplicate is an e-mail address another account has, or a group
	// name another group has.
	ErrDuplicate = errors.New("duplicate_email_or_group")
	// ErrRateLimit refuses a sign-in after too many failed attempts for the
	// same address in a short time, to slow down guessing.
	ErrRateLimit = errors.New("login_rate_limited")
	// ErrNotFound is an account, session or group that does not exist.
	ErrNotFound = errors.New("account_not_found")
	// ErrLastAdmin refuses a change that would leave no active
	// administrator, as nobody could manage the accounts any more.
	ErrLastAdmin = errors.New("last_active_admin")
)

const (
	sessionLifetime = 12 * time.Hour
	failureWindow   = 15 * time.Minute
	maxFailures     = 10
)

// User is an account. Lists and the signed-in user (CurrentUser) carry no
// password hash; only sign-in reads it.
type User struct {
	ID           string    `db:"id"`
	Email        string    `db:"email"`
	Name         string    `db:"name"`
	Role         Role      `db:"role"`
	Status       Status    `db:"status"`
	PasswordHash string    `db:"password_hash" json:"-"`
	CreatedAt    time.Time `db:"created_at"`
	CreatedBy    db.Ref    `db:"created_by"`
	UpdatedAt    time.Time `db:"updated_at"`
	UpdatedBy    db.Ref    `db:"updated_by"`
}

// userColumns are the columns of User; publicUserColumns leave out the
// password hash, for lists.
const (
	publicUserColumns = `id, email, name, role, status, created_at, created_by, updated_at, updated_by`
	userColumns       = publicUserColumns + `, password_hash`
)

// DisplayName is the name, or the email when no name is set.
func (u User) DisplayName() string {
	if u.Name != "" {
		return u.Name
	}

	return u.Email
}

// Session is one sign-in of an account, on one browser, as the account's
// session list shows it. The token itself is never stored.
type Session struct {
	ID         string     `db:"id"`
	UserID     string     `db:"user_id"`
	ExpiresAt  time.Time  `db:"expires_at"`
	RevokedAt  *time.Time `db:"revoked_at"`
	CreatedAt  time.Time  `db:"created_at"`
	LastSeenAt *time.Time `db:"last_seen_at"`
}

// Service manages accounts, sessions and groups in the database. It is safe
// for concurrent use.
type Service struct {
	db    *db.DB
	dummy string
	now   func() time.Time
}

// New returns the Service on database, whose schema (Schema) must be
// applied. It fails only when the system has no randomness.
func New(database *db.DB) (*Service, error) {
	// Unknown accounts still pay for one hash check, so timing does not reveal them.
	dummy, err := hashPassword(rand.Text())
	if err != nil {
		return nil, err
	}

	return &Service{db: database, dummy: dummy, now: time.Now}, nil
}

// Bootstrap creates the first admin. It does nothing once any account exists
// and returns ErrBootstrap in that case.
func (s *Service) Bootstrap(ctx context.Context, email, password string) error {
	_, err := s.SetupAdmin(ctx, email, "", password)

	return err
}

// CreateUser adds an active account. Passwords need at least 12 characters.
func (s *Service) CreateUser(ctx context.Context, email, name, password string, role Role) (User, error) {
	email, ok := normalEmail(email)
	if !ok {
		return User{}, ErrEmail
	}

	if !validPassword(password) {
		return User{}, ErrPassword
	}

	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}

	now := s.now().UTC()
	user := User{
		ID:        db.NewID(),
		Email:     email,
		Name:      strings.TrimSpace(name),
		Role:      role,
		Status:    Active,
		CreatedAt: now,
		CreatedBy: db.Actor(ctx),
		UpdatedAt: now,
		UpdatedBy: db.Actor(ctx),
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO wt_users(id,email,name,role,status,password_hash,created_at,created_by,updated_at,updated_by)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		user.ID, user.Email, user.Name, user.Role, user.Status, hash,
		user.CreatedAt, user.CreatedBy, user.UpdatedAt, user.UpdatedBy,
	)
	if db.IsUnique(err) {
		return User{}, ErrDuplicate
	}

	return user, err
}

// Login checks credentials and returns a new session token. A previous token
// from the same browser is revoked. Errors never reveal whether an account exists.
func (s *Service) Login(ctx context.Context, email, password, previous string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	if len(email) > 254 || len(password) > 1024 {
		return "", ErrCredentials
	}

	key := digest(email)
	now := s.now().UTC()

	blocked, err := s.blocked(ctx, key, now)
	if err != nil {
		return "", err
	}

	if blocked {
		return "", ErrRateLimit
	}

	var user User

	err = s.db.Get(ctx, &user, `SELECT `+userColumns+` FROM wt_users WHERE email=?`, email)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return "", err
	}

	hash := user.PasswordHash
	if hash == "" {
		hash = s.dummy
	}

	if !verifyPassword(password, hash) || user.ID == "" || user.Status != Active {
		if err := s.recordFailure(ctx, key, now); err != nil {
			return "", err
		}

		return "", ErrCredentials
	}

	token := rand.Text() + rand.Text()

	err = s.db.Tx(ctx, func(ctx context.Context) error {
		if err := s.Logout(ctx, previous); err != nil {
			return err
		}

		if _, err := s.db.Exec(ctx, `DELETE FROM wt_login_attempts WHERE key_hash=?`, key); err != nil {
			return err
		}

		_, err := s.db.Exec(ctx, `
			INSERT INTO wt_sessions(id,user_id,token_hash,expires_at,created_at)
			VALUES(?,?,?,?,?)`,
			db.NewID(), user.ID, digest(token), now.Add(sessionLifetime), now,
		)

		return err
	})
	if err != nil {
		return "", err
	}

	return token, nil
}

// lastSeenStep is how old a session's recorded activity may get before
// Authenticate records it again. Recording every admin request would write
// on every click, and SQLite has a single connection for writes.
const lastSeenStep = time.Minute

// Authenticate returns the active user of a valid session token and records
// the session's activity, to the minute.
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	user, err := s.Peek(ctx, token)
	if err != nil {
		return User{}, err
	}

	now := s.now().UTC()

	_, err = s.db.Exec(ctx, `
		UPDATE wt_sessions SET last_seen_at=?
		WHERE token_hash=? AND (last_seen_at IS NULL OR last_seen_at<?)`,
		now, digest(token), now.Add(-lastSeenStep),
	)

	return user, err
}

// Peek is Authenticate without recording activity: one read, for checks on
// the public site where editors only browse.
func (s *Service) Peek(ctx context.Context, token string) (User, error) {
	if len(token) < 32 || len(token) > 128 {
		return User{}, ErrSession
	}

	var user User

	err := s.db.Get(ctx, &user, `
		SELECT u.* FROM wt_users u
		JOIN wt_sessions s ON s.user_id=u.id
		WHERE s.token_hash=? AND s.revoked_at IS NULL AND s.expires_at>? AND u.status=?`,
		digest(token), s.now().UTC(), Active,
	)
	if errors.Is(err, db.ErrNotFound) {
		return User{}, ErrSession
	}

	if err != nil {
		return User{}, err
	}

	user.PasswordHash = ""

	return user, nil
}

// Logout ends the session of token, so it no longer signs anyone in. An
// empty or unknown token is no error: there is nothing to end.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}

	_, err := s.db.Exec(ctx,
		`UPDATE wt_sessions SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`,
		s.now().UTC(), digest(token),
	)

	return err
}

func (s *Service) blocked(ctx context.Context, key string, now time.Time) (bool, error) {
	var count int

	err := s.db.Get(ctx, &count,
		`SELECT COUNT(*) FROM wt_login_attempts WHERE key_hash=? AND blocked_until>?`,
		key, now,
	)

	return count > 0, err
}

// recordFailure counts failures per email in a 15 minute window and blocks
// further attempts for the rest of the window after 10 failures.
func (s *Service) recordFailure(ctx context.Context, key string, now time.Time) error {
	return s.db.Tx(ctx, func(ctx context.Context) error {
		var attempt struct {
			Failures      int       `db:"failure_count"`
			WindowExpires time.Time `db:"window_expires_at"`
		}

		err := s.db.Get(ctx, &attempt,
			`SELECT failure_count, window_expires_at FROM wt_login_attempts WHERE key_hash=?`,
			key,
		)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			return err
		}

		if errors.Is(err, db.ErrNotFound) || !attempt.WindowExpires.After(now) {
			attempt.Failures = 0
			attempt.WindowExpires = now.Add(failureWindow)
		}

		attempt.Failures++

		var blockedUntil *time.Time
		if attempt.Failures >= maxFailures {
			blockedUntil = &attempt.WindowExpires
		}

		if _, err := s.db.Exec(ctx, `DELETE FROM wt_login_attempts WHERE key_hash=?`, key); err != nil {
			return err
		}

		_, err = s.db.Exec(ctx, `
			INSERT INTO wt_login_attempts(key_hash,failure_count,blocked_until,window_expires_at)
			VALUES(?,?,?,?)`,
			key, attempt.Failures, blockedUntil, attempt.WindowExpires,
		)

		return err
	})
}

// Users lists all accounts by e-mail address, without password hashes.
func (s *Service) Users(ctx context.Context) ([]User, error) {
	var users []User

	err := s.db.Select(ctx, &users, `SELECT `+publicUserColumns+` FROM wt_users ORDER BY email`)

	return users, err
}

// Names maps user IDs to display names, for revision history.
func (s *Service) Names(ctx context.Context) (map[string]string, error) {
	users, err := s.Users(ctx)
	if err != nil {
		return nil, err
	}

	names := make(map[string]string, len(users))

	for _, user := range users {
		names[user.ID] = user.DisplayName()
	}

	return names, nil
}

// Sessions lists the account's sessions that have not expired yet, newest
// first; revoked ones are included until they would have expired.
func (s *Service) Sessions(ctx context.Context, userID string) ([]Session, error) {
	var sessions []Session

	err := s.db.Select(ctx, &sessions, `
		SELECT id, user_id, expires_at, revoked_at, created_at, last_seen_at
		FROM wt_sessions
		WHERE user_id=? AND expires_at>?
		ORDER BY created_at DESC`,
		userID, s.now().UTC(),
	)

	return sessions, err
}

// SetStatus enables or disables an account. Disabling revokes its sessions;
// the last active admin cannot be disabled.
func (s *Service) SetStatus(ctx context.Context, userID string, status Status) error {
	if status != Active && status != Disabled {
		return ErrNotFound
	}

	return s.db.Tx(ctx, func(ctx context.Context) error {
		var user User

		if err := s.db.Get(ctx, &user, `SELECT `+userColumns+` FROM wt_users WHERE id=?`, userID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return ErrNotFound
			}

			return err
		}

		if status == Disabled && user.Role == Admin && user.Status == Active {
			if err := s.keepAdmin(ctx, userID); err != nil {
				return err
			}
		}

		now := s.now().UTC()

		if _, err := s.db.Exec(ctx, `UPDATE wt_users SET status=?, updated_at=?, updated_by=? WHERE id=?`,
			status, now, db.Actor(ctx), userID,
		); err != nil {
			return err
		}

		if status == Disabled {
			_, err := s.db.Exec(ctx,
				`UPDATE wt_sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`,
				now, userID,
			)

			return err
		}

		return nil
	})
}

// RevokeSession ends one session of an account, such as one on a lost
// phone. It returns ErrNotFound when the account has no such active
// session.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	result, err := s.db.Exec(ctx,
		`UPDATE wt_sessions SET revoked_at=? WHERE id=? AND user_id=? AND revoked_at IS NULL`,
		s.now().UTC(), sessionID, userID,
	)

	return changed(result, err)
}

// Cleanup removes expired sessions, ended login windows and expired password
// reset links. The app runs it in the background.
func (s *Service) Cleanup(ctx context.Context) error {
	now := s.now().UTC()

	if _, err := s.db.Exec(ctx, `DELETE FROM wt_sessions WHERE expires_at<=?`, now); err != nil {
		return err
	}

	// A block lasts until the end of its window, so ended windows hold none.
	if _, err := s.db.Exec(ctx, `DELETE FROM wt_login_attempts WHERE window_expires_at<=?`, now); err != nil {
		return err
	}

	_, err := s.db.Exec(ctx, `DELETE FROM wt_password_resets WHERE expires_at<=?`, now)

	return err
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:])
}

type userKey struct{}

// WithUser returns ctx signed in as user. Stores record the user as the
// actor of their changes (see db.WithActor).
func WithUser(ctx context.Context, user User) context.Context {
	user.PasswordHash = ""
	ctx = db.WithActor(ctx, user.ID)

	return context.WithValue(ctx, userKey{}, user)
}

// CurrentUser returns the user that ctx is signed in as (see WithUser), and
// false outside a signed-in request, such as on the public site.
func CurrentUser(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userKey{}).(User)

	return user, ok
}
