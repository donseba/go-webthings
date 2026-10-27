package admin_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
)

// memoryTrash is a trash of items deleted at the given minutes past base.
type memoryTrash struct {
	items    []admin.TrashItem
	restored []string
	refuse   string
}

func (m *memoryTrash) List(_ context.Context, query string, limit, offset int) ([]admin.TrashItem, int, error) {
	matches := slices.DeleteFunc(slices.Clone(m.items), func(item admin.TrashItem) bool {
		return !admin.Matches(query, item.Title)
	})

	page, _ := admin.Paginate(matches[min(offset, len(matches)):], 1, limit)

	return page, len(matches), nil
}

func (m *memoryTrash) Restore(_ context.Context, ids []string) error {
	return admin.EachTrashItem(ids, func(id string) error {
		if id == m.refuse {
			return errors.New("conflict")
		}

		m.restored = append(m.restored, id)

		return nil
	}, func(error) string {
		return "Taken."
	})
}

func (m *memoryTrash) Purge(context.Context, []string) error {
	return errors.New("disk full")
}

func TestJoinTrash(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	base := time.Now()

	item := func(id string, minutes int) admin.TrashItem {
		return admin.TrashItem{ID: id, Title: "Item " + id, DeletedAt: base.Add(-time.Duration(minutes) * time.Minute)}
	}

	posts := &memoryTrash{items: []admin.TrashItem{item("a", 1), item("c", 3), item("e", 5)}, refuse: "c"}
	comments := &memoryTrash{items: []admin.TrashItem{item("b", 2), item("d", 4)}}
	joined := admin.JoinTrash(posts, comments)

	page, total, err := joined.List(ctx, "", 2, 1)
	if err != nil || total != 5 {
		t.Fatalf("list: %v %d", err, total)
	}

	ids := []string{page[0].ID, page[1].ID}
	if !slices.Equal(ids, []string{"1:b", "0:c"}) {
		t.Fatalf("second page = %v", ids)
	}

	err = joined.Restore(ctx, []string{"0:a", "0:c", "1:d", "9:x", "garbage"})

	var failure *admin.TrashError
	if !errors.As(err, &failure) || failure.ID != "0:c" || failure.Reason != "Taken." {
		t.Fatalf("restore failure: %v", err)
	}

	if !slices.Equal(posts.restored, []string{"a"}) || !slices.Equal(comments.restored, []string{"d"}) {
		t.Fatalf("restored %v and %v", posts.restored, comments.restored)
	}

	// Unexpected errors are no TrashErrors.
	if err := joined.Purge(ctx, []string{"0:a"}); err == nil || errors.As(err, &failure) {
		t.Fatalf("purge: %v", err)
	}
}

// usedFile is a trash of one file that the note "old" uses: as the media
// library does, it refuses to delete the file for good while the note is
// in the trash.
type usedFile struct {
	database  *db.DB
	deletedAt time.Time
	purged    bool
}

func (u *usedFile) List(context.Context, string, int, int) ([]admin.TrashItem, int, error) {
	if u.purged {
		return nil, 0, nil
	}

	return []admin.TrashItem{{ID: "photo", Title: "photo.png", DeletedAt: u.deletedAt}}, 1, nil
}

func (u *usedFile) Restore(context.Context, []string) error {
	return errors.New("not used")
}

func (u *usedFile) Purge(ctx context.Context, ids []string) error {
	return admin.EachTrashItem(ids, func(string) error {
		var users int

		if err := u.database.Get(ctx, &users, `SELECT COUNT(*) FROM t_note WHERE id='old'`); err != nil {
			return err
		}

		if users > 0 {
			return errInUse
		}

		u.purged = true

		return nil
	}, func(err error) string {
		if errors.Is(err, errInUse) {
			return "This file is still used by content in the trash."
		}

		return ""
	})
}

var errInUse = errors.New("in use")

// trashSection is a section with a trash and no screens.
func trashSection(id string, trash admin.Trash) admin.Section {
	return admin.Section{
		ID:     id,
		Title:  id,
		Trash:  trash,
		Routes: func(*admin.Routes) {},
	}
}

// TestPurgeTrash checks that PurgeTrash deletes for good, through each
// section's Trash, the items deleted before the cutoff and keeps the
// others, which can still be restored. A file that a deleted note uses
// goes in the same run, after the note; a section that fails does not
// stop the others.
func TestPurgeTrash(t *testing.T) {
	t.Parallel()

	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		now := time.Now().UTC()
		ago := func(days int) time.Time {
			return now.Add(-time.Duration(days) * 24 * time.Hour)
		}

		notes := noteTrash(t, database, "old", "recent")

		for id, days := range map[string]int{"old": 31, "recent": 29} {
			if _, err := database.Exec(ctx, `UPDATE t_note SET deleted_at=? WHERE id=?`, ago(days), id); err != nil {
				t.Fatal(err)
			}
		}

		file := &usedFile{database: database, deletedAt: ago(40)}
		broken := &memoryTrash{items: []admin.TrashItem{{ID: "lost", Title: "Lost", DeletedAt: ago(50)}}}

		// The file comes before the note that uses it, as Media comes
		// before the modules.
		backoffice, err := admin.New(newAccounts(t), admin.Options{Title: "Test site", CatalogueDir: t.TempDir()},
			trashSection("broken", broken),
			trashSection("media", file),
			trashSection("notes", notes),
		)
		if err != nil {
			t.Fatal(err)
		}

		purged, err := backoffice.PurgeTrash(ctx, ago(30))
		if purged != 2 || err == nil || !strings.Contains(err.Error(), "broken") {
			t.Fatalf("purged %d: %v", purged, err)
		}

		if !file.purged {
			t.Fatal("the file the old note used stays")
		}

		items, total, err := notes.List(ctx, "", 10, 0)
		if err != nil || total != 1 || items[0].ID != "recent" {
			t.Fatalf("left in the trash: %v %d %v", ids(items), total, err)
		}

		if err := notes.Restore(ctx, []string{"recent"}); err != nil {
			t.Fatalf("restore the recent note: %v", err)
		}

		// Nothing is deleted before its time.
		purged, err = backoffice.PurgeTrash(ctx, ago(60))
		if purged != 0 || err != nil {
			t.Fatalf("purged %d before their time: %v", purged, err)
		}
	})
}
