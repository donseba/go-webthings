package db

import (
	"context"
	"slices"
)

// SoftTable is a table whose rows go to the trash before they are removed
// for good. Every row has an id column and the deleted_at and deleted_by
// audit columns (see audit.go): NULL while the row is live, set while it is
// in the trash. Queries for live rows add "deleted_at IS NULL" themselves.
//
// SoftTable writes the statements that every such table needs, so a store
// keeps only its own rules:
//
//	notes := db.SoftTable{DB: database, Table: "wt_note", Scope: "instance=?", Args: []any{key}}
//
//	err := notes.Delete(ctx, id)
//	err = notes.Undelete(ctx, id, func(ctx context.Context) error {
//		// Refuse a note whose title another note took meanwhile.
//		return checkTitle(ctx, id)
//	})
//	err = notes.Purge(ctx, id)
//
// Every method returns ErrNotFound for an ID that is not where it should
// be: not live for Delete, not in the trash for the others. The methods
// join a transaction in ctx (see DB.Tx).
type SoftTable struct {
	// DB is the database the table is in.
	DB *DB
	// Table is the name of the table, such as "wt_redirect". It goes into
	// the SQL as it is, so it never comes from input.
	Table string
	// Scope limits every statement to a part of the table, such as the rows
	// of one module instance: an SQL condition with ? placeholders, such as
	// "instance=?", that Args fill. Empty covers the whole table.
	Scope string
	// Args are the values of Scope's placeholders, in order.
	Args []any
}

// Delete moves the live row with id to the trash, recording when and by
// whom in deleted_at and deleted_by (see Stamp).
func (t SoftTable) Delete(ctx context.Context, id string) error {
	return t.DeleteWith(ctx, id, "")
}

// DeleteWith is Delete with more assignments in the same statement, such as
// "publish_at=NULL" to cancel what was planned for the row. The ?
// placeholders of set take args.
func (t SoftTable) DeleteWith(ctx context.Context, id, set string, args ...any) error {
	at, by := Stamp(ctx)
	assignments := "deleted_at=?, deleted_by=?"

	if set != "" {
		assignments += ", " + set
	}

	where, whereArgs := t.row("deleted_at IS NULL", id)
	values := append([]any{at, by}, args...)

	return Affected(t.DB.Exec(ctx,
		`UPDATE `+t.Table+` SET `+assignments+` WHERE `+where,
		append(values, whereArgs...)...,
	))
}

// Undelete brings the row with id back from the trash, recording when and
// by whom in updated_at and updated_by. Checks run first, in one
// transaction with the change, so a store can keep a row in the trash that
// may not come back, such as a redirect whose path another redirect uses
// now; the first error of a check is returned as it is.
//
// A unique index may still refuse the row; db.IsUnique reports that error.
func (t SoftTable) Undelete(ctx context.Context, id string, checks ...func(ctx context.Context) error) error {
	return t.DB.Tx(ctx, func(ctx context.Context) error {
		for _, check := range checks {
			if err := check(ctx); err != nil {
				return err
			}
		}

		at, by := Stamp(ctx)
		where, args := t.row("deleted_at IS NOT NULL", id)

		return Affected(t.DB.Exec(ctx,
			`UPDATE `+t.Table+` SET deleted_at=NULL, deleted_by=NULL, updated_at=?, updated_by=? WHERE `+where,
			append([]any{at, by}, args...)...,
		))
	})
}

// Purge removes the row with id from the trash for good. Steps run first,
// in one transaction with the delete and only for a row in the trash, for
// what goes with the row, such as releasing the media it uses or removing
// rows that refer to it; the first error of a step is returned as it is.
func (t SoftTable) Purge(ctx context.Context, id string, steps ...func(ctx context.Context) error) error {
	return t.DB.Tx(ctx, func(ctx context.Context) error {
		if len(steps) > 0 {
			if err := t.inTrash(ctx, id); err != nil {
				return err
			}
		}

		for _, step := range steps {
			if err := step(ctx); err != nil {
				return err
			}
		}

		where, args := t.row("deleted_at IS NOT NULL", id)

		return Affected(t.DB.Exec(ctx, `DELETE FROM `+t.Table+` WHERE `+where, args...))
	})
}

// GetTrashed reads the row with id from the trash into dest, a pointer to a
// struct, with the given columns, such as "id, name, deleted_at".
func (t SoftTable) GetTrashed(ctx context.Context, dest any, columns, id string) error {
	where, args := t.row("deleted_at IS NOT NULL", id)

	return t.DB.Get(ctx, dest, `SELECT `+columns+` FROM `+t.Table+` WHERE `+where, args...)
}

// Trashed reads rows in the trash into dest, a pointer to a slice, with the
// given columns: the most recently deleted first, at most limit of them
// from offset. A limit of 0 or less reads them all. It returns how many
// rows the trash holds in all, for paging.
func (t SoftTable) Trashed(ctx context.Context, dest any, columns string, limit, offset int) (int, error) {
	where, args := t.where("deleted_at IS NOT NULL")

	var total int

	err := t.DB.Get(ctx, &total, `SELECT COUNT(*) FROM `+t.Table+` WHERE `+where, args...)
	if err != nil {
		return 0, err
	}

	query := `SELECT ` + columns + ` FROM ` + t.Table + ` WHERE ` + where + ` ORDER BY deleted_at DESC, id DESC`

	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, max(offset, 0))
	}

	return total, t.DB.Select(ctx, dest, query, args...)
}

// inTrash returns ErrNotFound unless the row with id is in the trash.
func (t SoftTable) inTrash(ctx context.Context, id string) error {
	where, args := t.row("deleted_at IS NOT NULL", id)

	var count int

	err := t.DB.Get(ctx, &count, `SELECT COUNT(*) FROM `+t.Table+` WHERE `+where, args...)
	if err != nil {
		return err
	}

	if count == 0 {
		return ErrNotFound
	}

	return nil
}

// where combines the scope and the state condition into one WHERE
// condition with its arguments.
func (t SoftTable) where(state string) (string, []any) {
	if t.Scope == "" {
		return state, nil
	}

	return "(" + t.Scope + ") AND " + state, slices.Clone(t.Args)
}

// row is where for the one row with id.
func (t SoftTable) row(state, id string) (string, []any) {
	where, args := t.where(state)

	return where + " AND id=?", append(args, id)
}
