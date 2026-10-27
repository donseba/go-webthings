package editor

import (
	"context"
	"errors"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
)

// trashed is an item in the trash with its title in the site's default
// language, which the trash shows and searches.
type trashed[F, T any] struct {
	item  content.Item[F, T]
	title string
}

// trash is the admin trash of an editor's store: its deleted items, named
// in the site's default language.
func trash[F, T, I any](editor Editor[F, T, I]) admin.Trash {
	return admin.RecordTrash(admin.TrashRecords[trashed[F, T]]{
		Load: editor.trashed,
		Item: func(record trashed[F, T]) admin.TrashItem {
			return admin.TrashItem{
				ID:        record.item.ID,
				Title:     record.title,
				DeletedAt: *record.item.DeletedAt,
				DeletedBy: record.item.DeletedBy,
			}
		},
		Undelete: editor.config.Store.Undelete,
		Purge:    editor.config.Store.Purge,
		Reason:   editor.trashReason,
	})
}

// trashed returns items in the trash with their titles: at most limit of
// them from offset (all for a limit of 0), and how many there are in all.
// Stores list their whole trash, which stays small next to the content.
func (e Editor[F, T, I]) trashed(ctx context.Context, limit, offset int) ([]trashed[F, T], int, error) {
	config, err := e.config.Locales.Configuration(ctx)
	if err != nil {
		return nil, 0, err
	}

	items, err := e.config.Store.Trashed(ctx)
	if err != nil {
		return nil, 0, err
	}

	page := items[min(max(offset, 0), len(items)):]
	if limit > 0 {
		page, _ = admin.Paginate(page, 1, limit)
	}

	records := make([]trashed[F, T], len(page))
	for i, item := range page {
		records[i] = trashed[F, T]{item: item, title: e.dashboardLabel(item, config.Default)}
	}

	return records, len(items), nil
}

// trashReason explains why an item could not be restored or purged: the
// module's Issue first, then the content kit's errors.
func (e Editor[F, T, I]) trashReason(err error) string {
	if e.config.Issue != nil {
		if message := e.config.Issue(err); message != "" {
			return message
		}
	}

	switch {
	case errors.Is(err, content.ErrDuplicateSlug):
		return "This item cannot be restored: another item uses its web address now."
	case db.IsForeignKey(err):
		// Purging an item that other items, also deleted ones, refer to.
		return "This item is still in use."
	}

	return ""
}
