package admin

import (
	"context"
	"errors"

	"github.com/donseba/go-webthings/db"
)

// TrashRecords describes the records of a table of their own, such as a
// module's messages or registrations, for the admin's trash. RecordTrash
// turns it into a Trash, so a module writes only what is particular to its
// records:
//
//	section.Trash = admin.RecordTrash(admin.TrashRecords[Note]{
//		Load: service.Trashed,
//		Fields: func(note Note) []string {
//			return []string{note.Title, note.Body}
//		},
//		Item: func(note Note) admin.TrashItem {
//			return admin.TrashItem{
//				ID:        note.ID,
//				Title:     note.Title,
//				DeletedAt: *note.DeletedAt,
//				DeletedBy: note.DeletedBy.String(),
//			}
//		},
//		Undelete: service.Undelete,
//		Purge:    service.Purge,
//	})
//
// db.SoftTable has the store's side: Trashed, Undelete and Purge.
type TrashRecords[T any] struct {
	// Load returns records in the trash, most recently deleted first: at
	// most limit of them from offset (all of them for a limit of 0), and how
	// many the trash holds in all. db.SoftTable.Trashed reads both.
	Load func(ctx context.Context, limit, offset int) ([]T, int, error)
	// Fields are the texts of a record that a search of the trash looks in,
	// such as a name and an e-mail address. Nil searches the title of Item.
	Fields func(record T) []string
	// Item describes a record in the trash's list.
	Item func(record T) TrashItem
	// Undelete brings the record with id back from the trash.
	Undelete func(ctx context.Context, id string) error
	// Purge removes the record with id for good.
	Purge func(ctx context.Context, id string) error
	// Reason explains to the editor why a record stays where it is, for the
	// module's own errors, such as "This redirect cannot be restored:
	// another redirect uses its old path now."; it is translated. A record
	// that is no longer in the trash (db.ErrNotFound) is explained already.
	// An error without a reason stops the whole action and is shown as a
	// failure.
	Reason func(err error) string
}

// RecordTrash is the Trash of records. Without a search it lists one page,
// which Load reads with LIMIT and OFFSET. A search reads at most
// SearchLimit records, matches them in memory the way list screens do, and
// pages the matches.
func RecordTrash[T any](records TrashRecords[T]) Trash {
	return recordTrash[T]{records: records}
}

type recordTrash[T any] struct {
	records TrashRecords[T]
}

func (t recordTrash[T]) List(ctx context.Context, query string, limit, offset int) ([]TrashItem, int, error) {
	search := NewSearch(query)

	if !search.Active() {
		records, total, err := t.records.Load(ctx, limit, offset)
		if err != nil {
			return nil, 0, err
		}

		items := make([]TrashItem, len(records))
		for i, record := range records {
			items[i] = t.records.Item(record)
		}

		return items, total, nil
	}

	records, _, err := t.records.Load(ctx, SearchLimit(), 0)
	if err != nil {
		return nil, 0, err
	}

	var matches []TrashItem

	for _, record := range records {
		item := t.records.Item(record)

		fields := []string{item.Title}
		if t.records.Fields != nil {
			fields = t.records.Fields(record)
		}

		if search.Matches(fields...) {
			matches = append(matches, item)
		}
	}

	page := matches[min(max(offset, 0), len(matches)):]
	if limit > 0 {
		page, _ = Paginate(page, 1, limit)
	}

	return page, len(matches), nil
}

func (t recordTrash[T]) Restore(ctx context.Context, ids []string) error {
	return EachTrashItem(ids, func(id string) error {
		return t.records.Undelete(ctx, id)
	}, t.reason)
}

func (t recordTrash[T]) Purge(ctx context.Context, ids []string) error {
	return EachTrashItem(ids, func(id string) error {
		return t.records.Purge(ctx, id)
	}, t.reason)
}

// reason asks the module first, so it can word a not-found case itself.
func (t recordTrash[T]) reason(err error) string {
	if t.records.Reason != nil {
		if message := t.records.Reason(err); message != "" {
			return message
		}
	}

	if errors.Is(err, db.ErrNotFound) {
		return NotInTrash
	}

	return ""
}

// NotInTrash explains an item that another editor restored or deleted for
// good meanwhile, for the Reason of a Trash; it is translated.
const NotInTrash = "This item is no longer in the trash."
