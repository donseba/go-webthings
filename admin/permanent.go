package admin

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"time"

	"github.com/donseba/go-webthings/db"
)

// Permanence keeps which items an administrator marked permanent. A
// permanent item stays on the website: its editor offers no unpublish, no
// planned unpublication and no delete, and refuses them when they are
// posted anyway. Administrators set and remove the mark in the item's
// editor (see editor.Config.Permanent for items that are permanent by their
// nature, such as the home page, whose mark cannot be removed).
//
// Items are named by the ID of their admin section, such as "pages" or
// "news", and their own ID. Implementations must be safe for concurrent use.
type Permanence interface {
	// Permanent reports whether the item is marked permanent.
	Permanent(ctx context.Context, section, id string) (bool, error)
	// SetPermanent sets or removes the mark; the acting user is the one in
	// ctx (see db.WithActor).
	SetPermanent(ctx context.Context, section, id string, permanent bool) error
}

// Permanence returns the store of permanent items; nil when the admin has
// none (see Options.Permanence), so no item can be marked.
func (a *Admin) Permanence() Permanence {
	return a.options.Permanence
}

//go:embed schema
var schemaFiles embed.FS

// Schema returns the admin's tables for db.ApplySchema: the permanent
// marks of NewPermanence.
func Schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}

// NewPermanence keeps the permanent marks in database, in the table of
// Schema. A removed mark stays as a row with permanent false, which records
// who removed it and when.
func NewPermanence(database *db.DB) Permanence {
	return permanentItems{db: database}
}

type permanentItems struct {
	db *db.DB
}

func (p permanentItems) Permanent(ctx context.Context, section, id string) (bool, error) {
	var permanent bool

	err := p.db.Get(ctx, &permanent,
		`SELECT permanent FROM wt_permanent_items WHERE section = ? AND item_id = ?`,
		section, id,
	)
	if errors.Is(err, db.ErrNotFound) {
		return false, nil
	}

	return permanent, err
}

func (p permanentItems) SetPermanent(ctx context.Context, section, id string, permanent bool) error {
	if section == "" || id == "" {
		return errors.New("permanent mark needs a section and an item")
	}

	now := time.Now().UTC()
	actor := db.Actor(ctx)

	return p.db.Tx(ctx, func(ctx context.Context) error {
		var rows int

		err := p.db.Get(ctx, &rows,
			`SELECT COUNT(*) FROM wt_permanent_items WHERE section = ? AND item_id = ?`,
			section, id,
		)
		if err != nil {
			return err
		}

		if rows > 0 {
			_, err = p.db.Exec(ctx,
				`UPDATE wt_permanent_items SET permanent = ?, updated_at = ?, updated_by = ? WHERE section = ? AND item_id = ?`,
				permanent, now, actor, section, id,
			)

			return err
		}

		_, err = p.db.Exec(ctx,
			`INSERT INTO wt_permanent_items(section,item_id,permanent,created_at,created_by,updated_at,updated_by)
			 VALUES(?,?,?,?,?,?,?)`,
			section, id, permanent, now, actor, now, actor,
		)

		return err
	})
}
