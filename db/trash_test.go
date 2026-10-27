package db_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
)

// note is a row of the soft-delete test table.
type note struct {
	ID        string     `db:"id"`
	Instance  string     `db:"instance"`
	Title     string     `db:"title"`
	UpdatedAt *time.Time `db:"updated_at"`
	UpdatedBy db.Ref     `db:"updated_by"`
	DeletedAt *time.Time `db:"deleted_at"`
	DeletedBy db.Ref     `db:"deleted_by"`
}

const noteColumns = `id, instance, title, updated_at, updated_by, deleted_at, deleted_by`

// notes creates the test table and returns it scoped to instance "a", with
// rows "a1" to "a3" in it and "b1" in instance "b".
func notes(t *testing.T, database *db.DB) db.SoftTable {
	t.Helper()

	ctx := context.Background()
	timestamp := map[db.Dialect]string{
		db.SQLite:   "DATETIME",
		db.Postgres: "TIMESTAMPTZ",
		db.MySQL:    "DATETIME(6)",
	}[database.Dialect()]

	_, err := database.Exec(ctx, `
		CREATE TABLE t_note (
			id         VARCHAR(36) PRIMARY KEY,
			instance   VARCHAR(64) NOT NULL,
			title      VARCHAR(200) NOT NULL,
			planned    VARCHAR(20) NULL,
			updated_at `+timestamp+` NULL,
			updated_by VARCHAR(36) NULL,
			deleted_at `+timestamp+` NULL,
			deleted_by VARCHAR(36) NULL
		)`)
	if err != nil {
		t.Fatal(err)
	}

	for _, row := range [][2]string{{"a1", "a"}, {"a2", "a"}, {"a3", "a"}, {"b1", "b"}} {
		_, err := database.Exec(ctx,
			`INSERT INTO t_note (id, instance, title, planned) VALUES (?, ?, ?, ?)`,
			row[0], row[1], "Note "+row[0], "soon",
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	return db.SoftTable{DB: database, Table: "t_note", Scope: "instance=?", Args: []any{"a"}}
}

func TestSoftTableDeleteUndeletePurge(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		table := notes(t, database)
		alice := db.WithActor(context.Background(), "alice")
		bob := db.WithActor(context.Background(), "bob")

		if err := table.Delete(alice, "a1"); err != nil {
			t.Fatal(err)
		}

		// A row of another scope, a row in the trash and an unknown one are
		// not found.
		for _, id := range []string{"b1", "a1", "missing", ""} {
			if err := table.Delete(alice, id); !errors.Is(err, db.ErrNotFound) {
				t.Fatalf("delete %q: want ErrNotFound, got %v", id, err)
			}
		}

		var trashed note
		if err := table.GetTrashed(alice, &trashed, noteColumns, "a1"); err != nil {
			t.Fatal(err)
		}

		if trashed.DeletedAt == nil || trashed.DeletedBy != "alice" || trashed.UpdatedAt != nil {
			t.Fatalf("trashed row = %+v", trashed)
		}

		if err := table.GetTrashed(alice, &trashed, noteColumns, "a2"); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("live row read from the trash: %v", err)
		}

		if err := table.Undelete(bob, "a1"); err != nil {
			t.Fatal(err)
		}

		if err := table.Undelete(bob, "a1"); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("undelete of a live row: %v", err)
		}

		var restored note

		err := database.Get(alice, &restored, `SELECT `+noteColumns+` FROM t_note WHERE id=?`, "a1")
		if err != nil {
			t.Fatal(err)
		}

		if restored.DeletedAt != nil || restored.DeletedBy != "" || restored.UpdatedBy != "bob" || restored.UpdatedAt == nil {
			t.Fatalf("restored row = %+v", restored)
		}

		if err := table.Purge(alice, "a1"); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("purge of a live row: %v", err)
		}

		if err := table.Delete(alice, "a1"); err != nil {
			t.Fatal(err)
		}

		if err := table.Purge(alice, "a1"); err != nil {
			t.Fatal(err)
		}

		var count int
		if err := database.Get(alice, &count, `SELECT COUNT(*) FROM t_note WHERE id=?`, "a1"); err != nil || count != 0 {
			t.Fatalf("purged row still there: %d, %v", count, err)
		}
	})
}

func TestSoftTableHooks(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		table := notes(t, database)
		ctx := db.WithActor(context.Background(), "alice")

		if err := table.DeleteWith(ctx, "a1", "planned=NULL, title=?", "Cancelled"); err != nil {
			t.Fatal(err)
		}

		var row struct {
			Title   string  `db:"title"`
			Planned *string `db:"planned"`
		}

		if err := table.GetTrashed(ctx, &row, `title, planned`, "a1"); err != nil {
			t.Fatal(err)
		}

		if row.Title != "Cancelled" || row.Planned != nil {
			t.Fatalf("DeleteWith did not apply its assignments: %+v", row)
		}

		// A check that refuses keeps the row in the trash and returns its
		// error unchanged.
		refused := errors.New("refused")

		err := table.Undelete(
			ctx,
			"a1",
			func(context.Context) error { return nil },
			func(context.Context) error { return refused },
		)
		if !errors.Is(err, refused) {
			t.Fatalf("want the check's error, got %v", err)
		}

		if err := table.GetTrashed(ctx, &row, `title, planned`, "a1"); err != nil {
			t.Fatalf("refused row left the trash: %v", err)
		}

		// Steps run in the purge's transaction: a failing step keeps the
		// row and undoes what the steps did.
		step := func(ctx context.Context) error {
			_, err := database.Exec(ctx, `DELETE FROM t_note WHERE id=?`, "a2")

			return err
		}

		err = table.Purge(ctx, "a1", step, func(context.Context) error { return refused })
		if !errors.Is(err, refused) {
			t.Fatalf("want the step's error, got %v", err)
		}

		var count int
		if err := database.Get(ctx, &count, `SELECT COUNT(*) FROM t_note WHERE id IN ('a1', 'a2')`); err != nil || count != 2 {
			t.Fatalf("failed purge changed rows: %d, %v", count, err)
		}

		// Steps do not run for a row that is not in the trash.
		ran := false
		mark := func(context.Context) error {
			ran = true

			return nil
		}

		if err := table.Purge(ctx, "a3", mark); !errors.Is(err, db.ErrNotFound) || ran {
			t.Fatalf("purge of a live row: %v, step ran: %v", err, ran)
		}

		if err := table.Purge(ctx, "a1", step); err != nil {
			t.Fatal(err)
		}

		if err := database.Get(ctx, &count, `SELECT COUNT(*) FROM t_note WHERE id IN ('a1', 'a2')`); err != nil || count != 0 {
			t.Fatalf("purge with a step left rows: %d, %v", count, err)
		}
	})
}

func TestSoftTableTrashed(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		table := notes(t, database)
		ctx := context.Background()

		var list []note

		total, err := table.Trashed(ctx, &list, noteColumns, 0, 0)
		if err != nil || total != 0 || len(list) != 0 {
			t.Fatalf("empty trash: %d, %v, %v", total, list, err)
		}

		// Deleted one after the other, so the times differ.
		for _, id := range []string{"a1", "a2", "a3"} {
			if err := table.Delete(ctx, id); err != nil {
				t.Fatal(err)
			}

			time.Sleep(2 * time.Millisecond)
		}

		other := db.SoftTable{DB: database, Table: "t_note", Scope: "instance=?", Args: []any{"b"}}
		if err := other.Delete(ctx, "b1"); err != nil {
			t.Fatal(err)
		}

		total, err = table.Trashed(ctx, &list, noteColumns, 0, 0)
		if err != nil || total != 3 || len(list) != 3 || list[0].ID != "a3" || list[2].ID != "a1" {
			t.Fatalf("whole trash: %d, %+v, %v", total, list, err)
		}

		list = nil

		total, err = table.Trashed(ctx, &list, noteColumns, 1, 1)
		if err != nil || total != 3 || len(list) != 1 || list[0].ID != "a2" {
			t.Fatalf("second page of one: %d, %+v, %v", total, list, err)
		}

		// Without a scope the table is one trash.
		all := db.SoftTable{DB: database, Table: "t_note"}
		list = nil

		total, err = all.Trashed(ctx, &list, noteColumns, 2, 0)
		if err != nil || total != 4 || len(list) != 2 || list[0].ID != "b1" {
			t.Fatalf("unscoped trash: %d, %+v, %v", total, list, err)
		}
	})
}

func TestAffected(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		notes(t, database)
		ctx := context.Background()

		err := db.Affected(database.Exec(ctx, `UPDATE t_note SET title=? WHERE id=?`, "Changed", "a1"))
		if err != nil {
			t.Fatal(err)
		}

		// A row that keeps its values still counts as found.
		err = db.Affected(database.Exec(ctx, `UPDATE t_note SET title=? WHERE id=?`, "Changed", "a1"))
		if err != nil {
			t.Fatalf("unchanged row: %v", err)
		}

		err = db.Affected(database.Exec(ctx, `UPDATE t_note SET title=? WHERE id=?`, "Changed", "missing"))
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("no row: want ErrNotFound, got %v", err)
		}

		err = db.Affected(database.Exec(ctx, `UPDATE t_missing SET title=?`, "Changed"))
		if err == nil || errors.Is(err, db.ErrNotFound) {
			t.Fatalf("failed statement: want its error, got %v", err)
		}
	})
}

// failingResult is a Result whose row count is unknown.
type failingResult struct{}

func (failingResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (failingResult) RowsAffected() (int64, error) {
	return 0, errors.New("no count")
}

func TestAffectedCountError(t *testing.T) {
	err := db.Affected(failingResult{}, nil)
	if err == nil || errors.Is(err, db.ErrNotFound) {
		t.Fatalf("want the count's error, got %v", err)
	}
}

func TestStamp(t *testing.T) {
	at, by := db.Stamp(db.WithActor(context.Background(), "alice"))

	if by != "alice" || at.Location() != time.UTC || at.Nanosecond()%1000 != 0 {
		t.Fatalf("Stamp = %v, %q", at, by)
	}

	if _, by := db.Stamp(context.Background()); by != "" {
		t.Fatalf("Stamp without actor = %q", by)
	}
}

// labels is a JSON column type as modules write them.
type labels map[string]string

func (l labels) Value() (driver.Value, error) {
	return db.JSONValue(l)
}

func (l *labels) Scan(src any) error {
	return db.ScanJSON(src, l)
}

func TestJSONColumn(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		_, err := database.Exec(ctx, `CREATE TABLE t_json (id VARCHAR(36) PRIMARY KEY, data TEXT NULL)`)
		if err != nil {
			t.Fatal(err)
		}

		_, err = database.Exec(ctx, `INSERT INTO t_json (id, data) VALUES (?, ?), (?, NULL), (?, '')`,
			"full", labels{"en": "Name", "nl": "Naam"}, "null", "empty",
		)
		if err != nil {
			t.Fatal(err)
		}

		for id, want := range map[string]int{"full": 2, "null": 0, "empty": 0} {
			var read labels
			if err := database.Get(ctx, &read, `SELECT data FROM t_json WHERE id=?`, id); err != nil {
				t.Fatalf("%s: %v", id, err)
			}

			if len(read) != want {
				t.Fatalf("%s: read %v", id, read)
			}
		}

		var broken labels
		if err := broken.Scan(42); err == nil {
			t.Fatal("scanning a number as JSON must fail")
		}
	})
}
