package admin_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin"
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
