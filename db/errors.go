package db

import (
	"errors"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// sqliteCode returns the extended result code of a SQLite error. The driver
// enables extended result codes on every connection, so constraint errors
// say which constraint failed.
func sqliteCode(err error) (int, bool) {
	var lite *sqlite.Error
	if !errors.As(err, &lite) {
		return 0, false
	}

	return lite.Code(), true
}

// IsUnique reports a unique or primary key violation on any supported driver.
func IsUnique(err error) bool {
	if code, ok := sqliteCode(err); ok {
		return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}

	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code == "23505"
	}

	var my *mysql.MySQLError
	if errors.As(err, &my) {
		return my.Number == 1062
	}

	return false
}

// IsForeignKey reports a foreign key violation on any supported driver.
func IsForeignKey(err error) bool {
	if code, ok := sqliteCode(err); ok {
		return code == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY
	}

	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code == "23503"
	}

	var my *mysql.MySQLError
	if errors.As(err, &my) {
		return my.Number == 1451 || my.Number == 1452
	}

	return false
}

// invalidValue reports a value the column type rejects, such as text that is
// not a UUID in a Postgres uuid column. Such a value cannot match any row.
func invalidValue(err error) bool {
	var pg *pgconn.PgError

	return errors.As(err, &pg) && pg.Code == "22P02"
}

// notFound turns rejected values into ErrNotFound: an ID that is not a valid
// UUID simply does not exist.
func notFound(err error) error {
	if invalidValue(err) {
		return ErrNotFound
	}

	return err
}
