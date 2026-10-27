// Package dbtest opens a fresh database per test for every configured dialect.
// SQLite always runs. Set WT_TEST_POSTGRES and/or WT_TEST_MYSQL to a server DSN
// (see `task db:up`) to run the same test against Postgres and MySQL.
package dbtest

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/db"
)

// Each runs fn once per available dialect with an empty database.
func Each(t *testing.T, fn func(t *testing.T, database *db.DB)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) { fn(t, SQLite(t)) })

	if dsn := os.Getenv("WT_TEST_POSTGRES"); dsn != "" {
		t.Run("postgres", func(t *testing.T) { fn(t, server(t, "pgx", dsn)) })
	}

	if dsn := os.Getenv("WT_TEST_MYSQL"); dsn != "" {
		t.Run("mysql", func(t *testing.T) { fn(t, server(t, "mysql", dsn)) })
	}
}

// SQLite opens an empty SQLite database in a temporary directory with the
// settings db.Open adds by default, as a site's default DSN would.
func SQLite(t *testing.T) *db.DB {
	t.Helper()

	return open(t, "sqlite", "file:"+filepath.Join(t.TempDir(), "test.sqlite"))
}

// ForeignKeyError returns a real foreign key violation from SQLite, for
// tests of code that turns db.IsForeignKey errors into messages.
func ForeignKeyError(t *testing.T) error {
	t.Helper()

	ctx := context.Background()
	database := SQLite(t)

	statements := []string{
		`CREATE TABLE fk_parent (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE fk_child (parent_id INTEGER NOT NULL REFERENCES fk_parent (id))`,
	}

	for _, statement := range statements {
		if _, err := database.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	_, err := database.Exec(ctx, `INSERT INTO fk_child (parent_id) VALUES (1)`)
	if !db.IsForeignKey(err) {
		t.Fatalf("want a foreign key violation, got %v", err)
	}

	return err
}

// server creates a uniquely named database on the server and drops it afterwards.
// The DSN must point at an existing database the user may create databases from.
func server(t *testing.T, driver, dsn string) *db.DB {
	t.Helper()
	ctx := context.Background()
	admin := open(t, driver, dsn)
	name := "wt_test_" + strings.ToLower(rand.Text()[:12])

	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP DATABASE "+name) })

	return open(t, driver, withDatabase(driver, dsn, name))
}

// withDatabase swaps the database name in a URL (postgres) or go-sql-driver DSN.
func withDatabase(driver, dsn, name string) string {
	if driver == "mysql" {
		before, after, _ := strings.Cut(dsn, "/")
		_, query, _ := strings.Cut(after, "?")
		return fmt.Sprintf("%s/%s?%s", before, name, query)
	}

	base, query, _ := strings.Cut(dsn, "?")
	base = base[:strings.LastIndex(base, "/")+1] + name

	if query != "" {
		return base + "?" + query
	}

	return base
}

func open(t *testing.T, driver, dsn string) *db.DB {
	t.Helper()

	database, err := db.Open(context.Background(), driver, dsn)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = database.Close() })
	return database
}
