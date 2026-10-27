package content

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/media"
)

func insertTranslations[T any](
	ctx context.Context,
	database *db.DB,
	table, owner, ownerID string,
	translations []Translation[T],
	texts []column,
) error {
	columns := append([]string{owner, "locale"}, columnNames(texts, "")...)
	statement := `INSERT INTO ` + table + `(` + strings.Join(columns, ",") + `) VALUES(` + placeholders(len(columns)) + `)`

	for _, translation := range translations {
		args := append([]any{ownerID, translation.Locale}, values(translation.Text, texts)...)

		if _, err := database.Exec(ctx, statement, args...); err != nil {
			return err
		}
	}

	return nil
}

// loadTranslations returns translations per owner ID, sorted by locale.
func loadTranslations[T any](
	ctx context.Context,
	database *db.DB,
	table, owner string,
	ids []string,
	texts []column,
) (map[string][]Translation[T], error) {
	out := map[string][]Translation[T]{}

	if len(ids) == 0 {
		return out, nil
	}

	query, args, err := database.In(`
		SELECT `+owner+`, locale`+prefixed(columnNames(texts, ""))+`
		FROM `+table+`
		WHERE `+owner+` IN (?)
		ORDER BY locale`,
		ids,
	)
	if err != nil {
		return nil, err
	}

	rows, err := database.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			id          string
			translation Translation[T]
		)

		destinations := append([]any{&id, &translation.Locale}, pointers(&translation.Text, texts)...)

		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}

		out[id] = append(out[id], translation)
	}

	return out, rows.Err()
}

func zero[T any]() T {
	var value T

	return value
}

func typeOf[T any]() reflect.Type {
	return reflect.TypeOf(zero[T]())
}

// track records media referenced by a revision or item; see media.Track.
func track[F, T any](ctx context.Context, database *db.DB, spec Spec[F, T], ownerID string, draft Draft[F, T]) error {
	err := media.Track(ctx, database, spec.Table, ownerID, spec.media(draft.Fields, draft.Translations))
	if errors.Is(err, media.ErrNotFound) {
		return ErrMediaMissing
	}

	return err
}

// auditColumns scans the audit columns of an item row that the item's
// CreatedAt and UpdatedAt do not cover, all nullable.
type auditColumns struct {
	createdBy db.Ref
	updatedBy db.Ref
	deletedAt *time.Time
	deletedBy db.Ref
}

// pointers are the scan destinations for created_by, updated_by,
// deleted_at and deleted_by.
func (a *auditColumns) pointers() []any {
	return []any{&a.createdBy, &a.updatedBy, &a.deletedAt, &a.deletedBy}
}

func fillAudit[F, T any](item *Item[F, T], audit auditColumns) {
	item.CreatedBy = audit.createdBy.String()
	item.UpdatedBy = audit.updatedBy.String()
	item.DeletedAt = audit.deletedAt
	item.DeletedBy = audit.deletedBy.String()
}
