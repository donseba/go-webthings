package db_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
)

const portableSchema = `
CREATE TABLE t_parent (
    id VARCHAR(36) PRIMARY KEY
);

-- Comment lines are skipped.
CREATE TABLE t_child (
    id        VARCHAR(36) PRIMARY KEY,
    parent_id VARCHAR(36) NOT NULL,
    FOREIGN KEY (parent_id) REFERENCES t_parent (id)
);
`

var schema = fstest.MapFS{
	"20261004000000_init.sqlite.sql":   {Data: []byte(portableSchema)},
	"20261004000000_init.postgres.sql": {Data: []byte(portableSchema)},
	"20261004000000_init.mysql.sql":    {Data: []byte(portableSchema)},
}

const update = `ALTER TABLE t_parent ADD COLUMN name VARCHAR(100) NOT NULL DEFAULT '';`

func TestSchemaUpdates(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "test", schema); err != nil {
			t.Fatal(err)
		}

		// A later release adds 002 for every dialect; only that file runs.
		updated := fstest.MapFS{}
		for name, file := range schema {
			updated[name] = file
		}

		for _, dialect := range []string{"sqlite", "postgres", "mysql"} {
			updated["20261120093000_add_name."+dialect+".sql"] = &fstest.MapFile{Data: []byte(update)}
		}

		for range 2 {
			if err := database.ApplySchema(ctx, "test", updated); err != nil {
				t.Fatal(err)
			}
		}

		if _, err := database.Exec(ctx, `INSERT INTO t_parent(id,name) VALUES(?,?)`, "a", "Ada"); err != nil {
			t.Fatal(err)
		}

		var version int64

		err := database.Get(ctx, &version, `SELECT version FROM wt_schema WHERE owner=?`, "test")
		if err != nil || version != 20261120093000 {
			t.Fatalf("version %d: %v", version, err)
		}
	})
}

func TestSchemaTransactionsAndErrors(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		for range 2 { // applying twice is a no-op
			if err := database.ApplySchema(ctx, "test", schema); err != nil {
				t.Fatal(err)
			}
		}

		if _, err := database.Exec(ctx, `INSERT INTO t_parent(id) VALUES(?)`, "a"); err != nil {
			t.Fatal(err)
		}

		if _, err := database.Exec(ctx, `INSERT INTO t_parent(id) VALUES(?)`, "a"); !db.IsUnique(err) {
			t.Fatalf("want unique violation, got %v", err)
		}

		if _, err := database.Exec(ctx, `INSERT INTO t_child(id,parent_id) VALUES(?,?)`, "c", "missing"); !db.IsForeignKey(err) {
			t.Fatalf("want foreign key violation, got %v", err)
		}

		rollback := errors.New("rollback")

		err := database.Tx(ctx, func(ctx context.Context) error {
			if _, err := database.Exec(ctx, `INSERT INTO t_parent(id) VALUES(?)`, "b"); err != nil {
				return err
			}

			// Nested Tx joins the outer transaction.
			return database.Tx(ctx, func(context.Context) error { return rollback })
		})
		if !errors.Is(err, rollback) {
			t.Fatal(err)
		}

		var id string
		if err := database.Get(ctx, &id, `SELECT id FROM t_parent WHERE id=?`, "b"); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("rolled back row visible: %v", err)
		}
	})
}

// TestSchemaChangedAfterApplying checks that a schema file that changed
// after it was applied stops ApplySchema, whichever version it is, while a
// change of only whitespace or comments does not; a database from before the
// checksums gets them recorded.
func TestSchemaChangedAfterApplying(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		applied := fstest.MapFS{}
		for name, file := range schema {
			applied[name] = file
		}

		for _, dialect := range []string{"sqlite", "postgres", "mysql"} {
			applied["20261120093000_add_name."+dialect+".sql"] = &fstest.MapFile{Data: []byte(update)}
		}

		if err := database.ApplySchema(ctx, "test", applied); err != nil {
			t.Fatal(err)
		}

		// Only layout and comments differ: the same statements.
		reformatted := fstest.MapFS{}
		for name, file := range applied {
			reformatted[name] = &fstest.MapFile{
				Data: []byte("-- A comment of its own.\n" + strings.ReplaceAll(string(file.Data), "    ", "\t")),
			}
		}

		if err := database.ApplySchema(ctx, "test", reformatted); err != nil {
			t.Fatalf("reformatted files: %v", err)
		}

		// The first version changed after a later one was applied.
		changed := fstest.MapFS{}
		for name, file := range applied {
			changed[name] = file
		}

		for _, dialect := range []string{"sqlite", "postgres", "mysql"} {
			changed["20261004000000_init."+dialect+".sql"] = &fstest.MapFile{
				Data: []byte(portableSchema + "CREATE TABLE t_extra (id VARCHAR(36) PRIMARY KEY);\n"),
			}
		}

		err := database.ApplySchema(ctx, "test", changed)
		if !errors.Is(err, db.ErrSchemaChanged) || !strings.Contains(err.Error(), "20261004000000_init.") {
			t.Fatalf("changed file: %v", err)
		}

		// A database from before the checksums records them and then
		// notices a change as well.
		if _, err := database.Exec(ctx, `DELETE FROM wt_schema_step WHERE owner=?`, "test"); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "test", applied); err != nil {
			t.Fatalf("without checksums: %v", err)
		}

		if err := database.ApplySchema(ctx, "test", changed); !errors.Is(err, db.ErrSchemaChanged) {
			t.Fatalf("changed file after recording: %v", err)
		}
	})
}

func TestSchemaNeedsEveryFlavour(t *testing.T) {
	database := dbtest.SQLite(t)
	incomplete := fstest.MapFS{
		"20261004000000_init.sqlite.sql":   {Data: []byte(portableSchema)},
		"20261004000000_init.postgres.sql": {Data: []byte(portableSchema)},
	}

	if err := database.ApplySchema(context.Background(), "test", incomplete); err == nil {
		t.Fatal("missing mysql file accepted")
	}
}

func TestSQLiteDriverNames(t *testing.T) {
	ctx := context.Background()

	for _, driver := range []string{"sqlite", "sqlite3"} {
		t.Run(driver, func(t *testing.T) {
			// A bare file DSN still gets foreign keys and the other defaults.
			database, err := db.Open(ctx, driver, "file:"+filepath.Join(t.TempDir(), "site.sqlite"))
			if err != nil {
				t.Fatal(err)
			}

			defer func() { _ = database.Close() }()

			if database.Dialect() != db.SQLite {
				t.Fatalf("dialect %q", database.Dialect())
			}

			if err := database.ApplySchema(ctx, "test", schema); err != nil {
				t.Fatal(err)
			}

			if _, err := database.Exec(
				ctx,
				`INSERT INTO t_child(id,parent_id) VALUES(?,?)`,
				"c",
				"missing",
			); !db.IsForeignKey(
				err,
			) {
				t.Fatalf("want foreign key violation, got %v", err)
			}
		})
	}
}

// TestSQLiteTimesAndBooleans checks the conversions sites rely on: times come
// back in UTC whatever zone they were written in, compare in time order as
// text in SQL, and BOOLEAN columns scan into bool.
func TestSQLiteTimesAndBooleans(t *testing.T) {
	ctx := context.Background()
	database := dbtest.SQLite(t)

	_, err := database.Exec(ctx, `CREATE TABLE t_times (
		id      INTEGER PRIMARY KEY,
		at      TIMESTAMP NOT NULL,
		created TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		on_air  BOOLEAN NOT NULL DEFAULT FALSE
	)`)
	if err != nil {
		t.Fatal(err)
	}

	amsterdam := time.FixedZone("CEST", 2*60*60)
	early := time.Date(2026, 10, 5, 9, 30, 0, 123456789, amsterdam)
	late := time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)

	if _, err := database.Exec(
		ctx,
		`INSERT INTO t_times(id,at,on_air) VALUES(1,?,?),(2,?,?)`,
		early,
		true,
		late,
		false,
	); err != nil {
		t.Fatal(err)
	}

	var stored string
	if err := database.Get(ctx, &stored, `SELECT CAST(at AS TEXT) FROM t_times WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	if stored != "2026-10-05 07:30:00.123456789+00:00" {
		t.Fatalf("stored as %q", stored)
	}

	var row struct {
		At      time.Time `db:"at"`
		Created time.Time `db:"created"`
		OnAir   bool      `db:"on_air"`
	}

	if err := database.Get(ctx, &row, `SELECT at, created, on_air FROM t_times WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	if !row.At.Equal(early) || row.At.Location() != time.UTC {
		t.Fatalf("at %v", row.At)
	}

	if row.Created.Location() != time.UTC || time.Since(row.Created) > time.Minute {
		t.Fatalf("created %v", row.Created)
	}

	if !row.OnAir {
		t.Fatal("on_air read as false")
	}

	var ids []int
	if err := database.Select(
		ctx,
		&ids,
		`SELECT id FROM t_times WHERE at <= ? ORDER BY at`,
		time.Date(2026, 10, 5, 7, 45, 0, 0, time.UTC),
	); err != nil {
		t.Fatal(err)
	}

	if len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("ids %v", ids)
	}
}

// TestAfterCommit checks that AfterCommit runs at once outside a
// transaction, after the commit of the outer transaction inside one, also
// from a nested Tx, and never for a transaction that rolls back.
func TestAfterCommit(t *testing.T) {
	database := dbtest.SQLite(t)
	ctx := context.Background()

	var calls []string

	db.AfterCommit(ctx, func() { calls = append(calls, "outside") })

	if len(calls) != 1 {
		t.Fatalf("outside a transaction: %v", calls)
	}

	err := database.Tx(ctx, func(ctx context.Context) error {
		db.AfterCommit(ctx, func() { calls = append(calls, "outer") })

		err := database.Tx(ctx, func(ctx context.Context) error {
			db.AfterCommit(ctx, func() { calls = append(calls, "nested") })

			return nil
		})
		if err != nil {
			return err
		}

		if len(calls) != 1 {
			t.Errorf("called before the commit: %v", calls)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(calls, ",") != "outside,outer,nested" {
		t.Fatalf("after the commit: %v", calls)
	}

	rollback := errors.New("rollback")

	err = database.Tx(ctx, func(ctx context.Context) error {
		db.AfterCommit(ctx, func() { calls = append(calls, "rolled back") })

		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}

	if len(calls) != 3 {
		t.Fatalf("called for a rollback: %v", calls)
	}
}
