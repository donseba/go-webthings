// Package db is WebThings' small layer over SQL. It keeps one connection
// pool for SQLite, Postgres or MySQL, so queries are written once with `?`
// placeholders and work on all three. A transaction travels in the context
// (see DB.Tx), so code called inside one joins it without passing it
// along. Core and every module bring their own schema files, applied at
// start (see DB.ApplySchema).
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"uuid"

	"github.com/jmoiron/sqlx"
)

// Dialect is the kind of database behind a DB. It decides which schema
// files apply, and lets the few queries that differ per database choose
// their SQL.
type Dialect string

const (
	// SQLite is the default: one file in the state directory, no server.
	SQLite Dialect = "sqlite"
	// Postgres is PostgreSQL, through the pgx driver.
	Postgres Dialect = "postgres"
	// MySQL is MySQL or MariaDB.
	MySQL Dialect = "mysql"
)

// ErrNotFound is returned by Get when no row matches.
var ErrNotFound = errors.New("not_found")

// dialectFor maps a database/sql driver name to its schema dialect.
func dialectFor(driver string) (Dialect, error) {
	switch driver {
	case "sqlite3", "sqlite":
		return SQLite, nil
	case "pgx", "postgres":
		return Postgres, nil
	case "mysql":
		return MySQL, nil
	}

	return "", fmt.Errorf("unsupported database driver %q (use sqlite, pgx or mysql)", driver)
}

// DB is the site's database. It is safe for concurrent use; every method
// takes part in the transaction its context carries, if any (see Tx).
type DB struct {
	conn    *sqlx.DB
	dialect Dialect
	// statements counts the statements sent, see Statements.
	statements atomic.Uint64
}

// Open connects and pings. Driver is one of sqlite, pgx or mysql; "sqlite3"
// and "postgres" are accepted as aliases. SQLite uses the pure-Go
// modernc.org/sqlite driver, so no cgo is needed for any dialect.
func Open(ctx context.Context, driver, dsn string) (*DB, error) {
	dialect, err := dialectFor(driver)
	if err != nil {
		return nil, err
	}

	switch dialect {
	case SQLite:
		driver = "sqlite"
		dsn = sqliteDSN(dsn)
	case Postgres:
		driver = "pgx"
	case MySQL:
		// Timestamps scan into time.Time only with parseTime, and RowsAffected
		// counts matched rows (like SQLite and Postgres) only with clientFoundRows.
		dsn = withParam(dsn, "parseTime", "true")
		dsn = withParam(dsn, "clientFoundRows", "true")
	}

	conn, err := sqlx.ConnectContext(ctx, driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}

	if dialect == SQLite {
		// SQLite allows one writer; a single connection avoids SQLITE_BUSY
		// between pooled connections inside one process.
		conn.SetMaxOpenConns(1)
	}

	return &DB{conn: conn, dialect: dialect}, nil
}

// Dialect returns the kind of database, for the queries that differ per
// database.
func (db *DB) Dialect() Dialect {
	return db.dialect
}

// Close closes the connection pool. The App does this at Close.
func (db *DB) Close() error {
	return db.conn.Close()
}

// Ping checks that the database answers, for readiness checks.
func (db *DB) Ping(ctx context.Context) error {
	return db.conn.PingContext(ctx)
}

// Stats are the connection pool's statistics, for metrics.
func (db *DB) Stats() sql.DBStats {
	return db.conn.Stats()
}

// Statements is the number of statements this DB has sent so far. Tests use
// it to check that cached work needs no queries; it is cheap enough to keep
// for diagnostics.
func (db *DB) Statements() uint64 {
	return db.statements.Load()
}

type txKey struct{}

// InTx reports whether ctx carries a transaction. Caches use it to avoid
// keeping data that is not committed yet.
func InTx(ctx context.Context) bool {
	_, ok := ctx.Value(txKey{}).(*sqlx.Tx)

	return ok
}

type querier interface {
	GetContext(context.Context, any, string, ...any) error
	SelectContext(context.Context, any, string, ...any) error
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryxContext(context.Context, string, ...any) (*sqlx.Rows, error)
}

func (db *DB) q(ctx context.Context) querier {
	db.statements.Add(1)

	if tx, ok := ctx.Value(txKey{}).(*sqlx.Tx); ok {
		return tx
	}

	return db.conn
}

// Get scans one row into dest and returns ErrNotFound for no rows, and for
// values a column type rejects, such as text that is not a UUID in Postgres.
func (db *DB) Get(ctx context.Context, dest any, query string, args ...any) error {
	err := db.q(ctx).GetContext(ctx, dest, db.conn.Rebind(query), args...)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}

	return notFound(err)
}

// Select scans all rows into dest. Errors for values a column type rejects,
// such as an invalid UUID, become ErrNotFound (see Get).
func (db *DB) Select(ctx context.Context, dest any, query string, args ...any) error {
	return notFound(db.q(ctx).SelectContext(ctx, dest, db.conn.Rebind(query), args...))
}

// Query runs a query and returns its rows to be read one at a time, for
// results too large to scan at once with Select. The caller closes the
// rows. Errors for values a column type rejects become ErrNotFound (see
// Get).
func (db *DB) Query(ctx context.Context, query string, args ...any) (*sqlx.Rows, error) {
	rows, err := db.q(ctx).QueryxContext(ctx, db.conn.Rebind(query), args...)

	return rows, notFound(err)
}

// Exec runs a statement that returns no rows, such as an INSERT, UPDATE
// or DELETE. Errors for values a column type rejects become ErrNotFound
// (see Get); see Affected to check that a row was there to change.
func (db *DB) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := db.q(ctx).ExecContext(ctx, db.conn.Rebind(query), args...)

	return result, notFound(err)
}

// Affected checks an UPDATE or DELETE meant for rows that must exist, such
// as one record by its ID. It returns err when the statement failed, the
// error of RowsAffected when the count is unknown, and ErrNotFound when no
// row matched. The results of Exec go in directly:
//
//	return db.Affected(database.Exec(ctx, `DELETE FROM wt_note WHERE id=?`, id))
//
// Rows count when they match, also when the statement leaves their values
// as they were: Open sets MySQL up to count like SQLite and Postgres.
func Affected(result sql.Result, err error) error {
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}

	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

// In expands `IN (?)` for a slice argument; Get, Select and Exec rebind it.
func (db *DB) In(query string, args ...any) (string, []any, error) {
	return sqlx.In(query, args...)
}

// Tx runs fn in a transaction. Calls made with the supplied context join it;
// a nested Tx simply joins the outer transaction.
func (db *DB) Tx(ctx context.Context, fn func(context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(*sqlx.Tx); ok {
		return fn(ctx)
	}

	tx, err := db.conn.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() { _ = tx.Rollback() }()

	commit := &commitHooks{}

	ctx = context.WithValue(ctx, txKey{}, tx)
	ctx = context.WithValue(ctx, commitKey{}, commit)

	if err := fn(ctx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	commit.run()

	return nil
}

type commitKey struct{}

// commitHooks are the functions AfterCommit keeps for the commit of a
// transaction.
type commitHooks struct {
	mu    sync.Mutex
	hooks []func()
}

// run calls the hooks in the order they were added.
func (c *commitHooks) run() {
	c.mu.Lock()
	hooks := c.hooks
	c.hooks = nil
	c.mu.Unlock()

	for _, hook := range hooks {
		hook()
	}
}

// AfterCommit calls fn once what was written with ctx is committed: at
// once outside a transaction, and after the commit of the transaction ctx
// carries (see Tx), so nothing that reads committed data, such as a cache
// that is told to forget, sees the state before the write again. When the
// transaction rolls back, fn is not called.
func AfterCommit(ctx context.Context, fn func()) {
	commit, ok := ctx.Value(commitKey{}).(*commitHooks)
	if !ok {
		fn()

		return
	}

	commit.mu.Lock()
	commit.hooks = append(commit.hooks, fn)
	commit.mu.Unlock()
}

// AfterWrite calls fn after the commit of the write that err reports on
// (see AfterCommit), when the write succeeded and fn is set, and returns
// err. Stores use it to tell a cache that what it keeps changed, such as
// the output cache through webthings.Env.Changed.
func AfterWrite(ctx context.Context, err error, fn func()) error {
	if err == nil && fn != nil {
		AfterCommit(ctx, fn)
	}

	return err
}

// split separates a schema file into statements. Statements end with a
// semicolon at the end of a line; `--` comment lines are dropped.
func split(script string) []string {
	var (
		statements []string
		current    strings.Builder
	)

	for line := range strings.Lines(strings.ReplaceAll(script, "\r\n", "\n")) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}

		current.WriteString(line)

		if strings.HasSuffix(trimmed, ";") {
			statement := strings.TrimSuffix(strings.TrimSpace(current.String()), ";")
			statements = append(statements, statement)
			current.Reset()
		}
	}

	if rest := strings.TrimSpace(current.String()); rest != "" {
		statements = append(statements, rest)
	}

	return statements
}

// NewID returns a new time-ordered identifier (UUIDv7) in its 36-character
// text form. IDs are stored as VARCHAR(36) on SQLite and MySQL and as the
// native UUID type on Postgres; they travel as plain strings in every dialect.
func NewID() string {
	return uuid.NewV7().String()
}

// sqliteDefaults are the connection settings WebThings relies on, as DSN
// parameters of modernc.org/sqlite. Each entry lists the keys that already
// configure the setting; a DSN that uses one of them keeps its own value.
var sqliteDefaults = []struct {
	keys   []string
	pragma string
	param  string
}{
	// Foreign keys are off by default in SQLite; IsForeignKey and the
	// "still in use" checks depend on them.
	{keys: []string{"_foreign_keys", "_fk"}, pragma: "foreign_keys", param: "_foreign_keys=1"},
	// Another process (a backup, the sqlite3 shell) may hold the lock briefly.
	{keys: []string{"_busy_timeout", "_timeout"}, pragma: "busy_timeout", param: "_busy_timeout=5000"},
	// Transactions take the write lock when they begin, so two writers wait
	// for each other instead of failing with SQLITE_BUSY halfway.
	{keys: []string{"_txlock"}, param: "_txlock=immediate"},
	// Times are written as "2006-01-02 15:04:05.999999999-07:00" (the format
	// go-sqlite3 used), so existing databases keep working and SQL compares
	// them in time order as text. The driver's default is time.Time.String.
	{keys: []string{"_time_format"}, param: "_time_format=sqlite"},
	// Times are written in UTC and read back in UTC, like Postgres and MySQL
	// columns; frontend.Format moves UTC times to the site's timezone.
	{keys: []string{"_timezone"}, param: "_timezone=UTC"},
}

// sqliteDSN adds every setting in sqliteDefaults that dsn does not set
// itself. go-sqlite3 style DSNs keep working: modernc.org/sqlite reads the
// same _foreign_keys, _busy_timeout and _txlock parameters.
func sqliteDSN(dsn string) string {
	_, query, _ := strings.Cut(dsn, "?")

	params, err := url.ParseQuery(query)
	if err != nil {
		// The driver reports the malformed DSN when it opens it.
		return dsn
	}

	for _, setting := range sqliteDefaults {
		if hasSQLiteSetting(params, setting.keys, setting.pragma) {
			continue
		}

		if strings.Contains(dsn, "?") {
			dsn += "&" + setting.param
		} else {
			dsn += "?" + setting.param
		}
	}

	return dsn
}

// hasSQLiteSetting reports whether params set one of keys, or run the pragma
// through _pragma=name(value).
func hasSQLiteSetting(params url.Values, keys []string, pragma string) bool {
	for _, key := range keys {
		if _, ok := params[key]; ok {
			return true
		}
	}

	if pragma == "" {
		return false
	}

	for _, value := range params["_pragma"] {
		name := strings.ToLower(strings.TrimSpace(value))
		if strings.HasPrefix(name, pragma) {
			return true
		}
	}

	return false
}

func withParam(dsn, key, value string) string {
	if strings.Contains(dsn, key+"=") {
		return dsn
	}

	if strings.Contains(dsn, "?") {
		return dsn + "&" + key + "=" + value
	}

	return dsn + "?" + key + "=" + value
}
