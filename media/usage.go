package media

import (
	"context"
	"errors"

	"github.com/donseba/go-webthings/db"
)

// Track records that owner/ownerID references exactly the given assets,
// replacing earlier records. Referenced assets cannot be deleted, and assets
// in the trash cannot be referenced (ErrNotFound). Owner is usually a table
// name, ownerID the referencing row. Empty IDs are ignored.
//
// Content with revisions (pages and the modules with publication, see
// content.Revisions) tracks every revision, not only the published one, and
// keeps old revisions so editors can restore them. A file that any revision
// uses therefore stays in use, and cannot be deleted, until the page or item
// itself is deleted for good (purged from the trash): restoring an old
// revision or a deleted item never finds its images gone.
func Track(ctx context.Context, database *db.DB, owner, ownerID string, assets []string) error {
	return database.Tx(ctx, func(ctx context.Context) error {
		_, err := database.Exec(ctx, `DELETE FROM wt_media_usage WHERE owner=? AND owner_id=?`, owner, ownerID)
		if err != nil {
			return err
		}

		seen := map[string]bool{}

		for _, asset := range assets {
			if asset == "" || seen[asset] {
				continue
			}

			seen[asset] = true

			// A file in the trash cannot get new uses.
			var live int

			err := database.Get(ctx, &live, `SELECT COUNT(*) FROM wt_media_asset WHERE id=? AND deleted_at IS NULL`, asset)
			if errors.Is(err, db.ErrNotFound) || err == nil && live == 0 {
				return ErrNotFound
			}

			if err != nil {
				return err
			}

			_, err = database.Exec(ctx,
				`INSERT INTO wt_media_usage(asset_id,owner,owner_id) VALUES(?,?,?)`,
				asset, owner, ownerID,
			)
			if db.IsForeignKey(err) || errors.Is(err, db.ErrNotFound) {
				return ErrNotFound
			}

			if err != nil {
				return err
			}
		}

		return nil
	})
}

// Untrack removes all usage records of an owner row, for example on delete.
func Untrack(ctx context.Context, database *db.DB, owner, ownerID string) error {
	_, err := database.Exec(ctx, `DELETE FROM wt_media_usage WHERE owner=? AND owner_id=?`, owner, ownerID)

	return err
}
