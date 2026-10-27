package admin_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
)

// trashedNote is a row of the test table as the trash reads it.
type trashedNote struct {
	ID        string     `db:"id"`
	Title     string     `db:"title"`
	Body      string     `db:"body"`
	DeletedAt *time.Time `db:"deleted_at"`
	DeletedBy db.Ref     `db:"deleted_by"`
}

var errLocked = errors.New("locked")

// noteTrash makes a table of notes, deletes all of them one after the other
// (the last one first in the trash) and returns its RecordTrash. Undelete
// refuses "locked" notes; Purge fails for good on "broken".
func noteTrash(t *testing.T, database *db.DB, ids ...string) admin.Trash {
	t.Helper()

	ctx := db.WithActor(context.Background(), "editor")
	timestamp := map[db.Dialect]string{
		db.SQLite:   "DATETIME",
		db.Postgres: "TIMESTAMPTZ",
		db.MySQL:    "DATETIME(6)",
	}[database.Dialect()]

	_, err := database.Exec(ctx, `
		CREATE TABLE t_note (
			id         VARCHAR(36) PRIMARY KEY,
			title      VARCHAR(200) NOT NULL,
			body       VARCHAR(200) NOT NULL,
			updated_at `+timestamp+` NULL,
			updated_by VARCHAR(36) NULL,
			deleted_at `+timestamp+` NULL,
			deleted_by VARCHAR(36) NULL
		)`)
	if err != nil {
		t.Fatal(err)
	}

	table := db.SoftTable{DB: database, Table: "t_note"}

	for _, id := range ids {
		_, err := database.Exec(ctx, `INSERT INTO t_note (id, title, body) VALUES (?, ?, ?)`, id, "Note "+id, "About "+id)
		if err != nil {
			t.Fatal(err)
		}

		if err := table.Delete(ctx, id); err != nil {
			t.Fatal(err)
		}

		time.Sleep(2 * time.Millisecond)
	}

	return admin.RecordTrash(admin.TrashRecords[trashedNote]{
		Load: func(ctx context.Context, limit, offset int) ([]trashedNote, int, error) {
			var list []trashedNote

			total, err := table.Trashed(ctx, &list, `id, title, body, deleted_at, deleted_by`, limit, offset)

			return list, total, err
		},
		Fields: func(note trashedNote) []string {
			return []string{note.Title, note.Body}
		},
		Item: func(note trashedNote) admin.TrashItem {
			return admin.TrashItem{ID: note.ID, Title: note.Title, DeletedAt: *note.DeletedAt, DeletedBy: note.DeletedBy.String()}
		},
		Undelete: func(ctx context.Context, id string) error {
			return table.Undelete(ctx, id, func(context.Context) error {
				if id == "locked" {
					return errLocked
				}

				return nil
			})
		},
		Purge: func(ctx context.Context, id string) error {
			if id == "broken" {
				return errors.New("disk full")
			}

			return table.Purge(ctx, id)
		},
		Reason: func(err error) string {
			if errors.Is(err, errLocked) {
				return "This note is locked."
			}

			return ""
		},
	})
}

func ids(items []admin.TrashItem) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.ID
	}

	return out
}

func TestRecordTrashList(t *testing.T) {
	t.Parallel()

	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		trash := noteTrash(t, database, "apple", "banana", "cherry", "date")
		ctx := context.Background()

		page, total, err := trash.List(ctx, "", 2, 1)
		if err != nil || total != 4 || !slices.Equal(ids(page), []string{"cherry", "banana"}) {
			t.Fatalf("second page of two: %v %d %v", ids(page), total, err)
		}

		if page[0].DeletedBy != "editor" || page[0].DeletedAt.IsZero() || page[0].Title != "Note cherry" {
			t.Fatalf("item = %+v", page[0])
		}

		// A search looks in Fields, here also the body, and pages the
		// matches.
		page, total, err = trash.List(ctx, "about", 1, 1)
		if err != nil || total != 4 || !slices.Equal(ids(page), []string{"cherry"}) {
			t.Fatalf("search in the body: %v %d %v", ids(page), total, err)
		}

		page, total, err = trash.List(ctx, "banana", 10, 0)
		if err != nil || total != 1 || !slices.Equal(ids(page), []string{"banana"}) {
			t.Fatalf("search: %v %d %v", ids(page), total, err)
		}
	})
}

func TestRecordTrashRestoreAndPurge(t *testing.T) {
	t.Parallel()

	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		trash := noteTrash(t, database, "apple", "locked", "broken")
		ctx := context.Background()

		// The module's reason comes first; a record that is no longer in
		// the trash is explained by default. The others are restored.
		err := trash.Restore(ctx, []string{"apple", "locked", "gone"})

		reasons := map[string]string{}

		for _, part := range err.(interface{ Unwrap() []error }).Unwrap() {
			var failure *admin.TrashError
			if errors.As(part, &failure) {
				reasons[failure.ID] = failure.Reason
			}
		}

		if len(reasons) != 2 || reasons["locked"] != "This note is locked." || reasons["gone"] != admin.NotInTrash {
			t.Fatalf("reasons = %v (%v)", reasons, err)
		}

		_, total, err := trash.List(ctx, "", 10, 0)
		if err != nil || total != 2 {
			t.Fatalf("after restore: %d %v", total, err)
		}

		// An error without a reason stops at once.
		err = trash.Purge(ctx, []string{"locked", "broken"})

		var failure *admin.TrashError
		if err == nil || errors.As(err, &failure) {
			t.Fatalf("want a plain failure, got %v", err)
		}

		_, total, err = trash.List(ctx, "", 10, 0)
		if err != nil || total != 1 {
			t.Fatalf("after purge: %d %v", total, err)
		}
	})
}
