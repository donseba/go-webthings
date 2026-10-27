package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"
)

// Rows that people change record who did it and when, in audit columns:
//
//	created_at, created_by   when and by whom the row was made
//	updated_at, updated_by   its last change
//	deleted_at, deleted_by   when it went to the trash; NULL while it is live
//
// The *_by columns hold a user ID, or NULL for visitors and the system (the
// scheduler, imports without a signed-in user). They have no foreign key:
// the record stays when an account is deleted.
//
// The acting user travels in the context: the admin sets it for every
// signed-in request (auth.WithUser calls WithActor), and every store reads
// it with Actor when it writes.

type actorKey struct{}

// WithActor returns ctx with the ID of the user on whose behalf statements
// run, for the *_by audit columns.
func WithActor(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, actorKey{}, userID)
}

// Actor is the user ID WithActor put in ctx, as a Ref: empty, and so NULL in
// the database, for visitors and the system.
func Actor(ctx context.Context) Ref {
	id, _ := ctx.Value(actorKey{}).(string)

	return Ref(id)
}

// Stamp returns the time and the author of a change, for the audit
// columns: the current time in UTC, cut to the microsecond that every
// supported database keeps, and Actor(ctx). A store takes one stamp per
// change and writes it to every row the change touches, so rows changed
// together carry equal times:
//
//	at, by := db.Stamp(ctx)
//
//	_, err := database.Exec(ctx, `
//		UPDATE wt_note
//		SET body=?, updated_at=?, updated_by=?
//		WHERE id=? AND deleted_at IS NULL`,
//		body, at, by, id,
//	)
func Stamp(ctx context.Context) (time.Time, Ref) {
	return time.Now().UTC().Truncate(time.Microsecond), Actor(ctx)
}

// Ref is an optional reference to a row, such as a created_by user ID. It is
// stored as NULL when empty and reads NULL as empty, so structs can use it
// for nullable ID columns.
type Ref string

// Value stores the empty Ref as NULL.
func (r Ref) Value() (driver.Value, error) {
	if r == "" {
		return nil, nil
	}

	return string(r), nil
}

// Scan reads NULL as the empty Ref.
func (r *Ref) Scan(value any) error {
	var text sql.NullString

	if err := text.Scan(value); err != nil {
		return fmt.Errorf("scan db.Ref: %w", err)
	}

	*r = Ref(text.String)

	return nil
}

// String returns the ID.
func (r Ref) String() string {
	return string(r)
}
