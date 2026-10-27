package admin_test

import (
	"context"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
)

// TestPermanenceStore checks the stored permanent marks on every database:
// unmarked items are not permanent, a mark can be set and removed again,
// and the row records who changed it last.
func TestPermanenceStore(t *testing.T) {
	t.Parallel()

	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "admin", admin.Schema()); err != nil {
			t.Fatal(err)
		}

		marks := admin.NewPermanence(database)
		owner := "0199a3c4-0000-7000-8000-000000000001"
		editor := "0199a3c4-0000-7000-8000-000000000002"

		if permanent, err := marks.Permanent(ctx, "pages", "about"); err != nil || permanent {
			t.Fatalf("unmarked item: %v %v", permanent, err)
		}

		if err := marks.SetPermanent(db.WithActor(ctx, owner), "pages", "about", true); err != nil {
			t.Fatal(err)
		}

		if err := marks.SetPermanent(ctx, "news", "about", false); err != nil {
			t.Fatal(err)
		}

		if permanent, err := marks.Permanent(ctx, "pages", "about"); err != nil || !permanent {
			t.Fatalf("marked item: %v %v", permanent, err)
		}

		if permanent, _ := marks.Permanent(ctx, "news", "about"); permanent {
			t.Fatal("a mark applies to another section")
		}

		if err := marks.SetPermanent(db.WithActor(ctx, editor), "pages", "about", false); err != nil {
			t.Fatal(err)
		}

		if permanent, err := marks.Permanent(ctx, "pages", "about"); err != nil || permanent {
			t.Fatalf("removed mark: %v %v", permanent, err)
		}

		var row struct {
			CreatedBy db.Ref    `db:"created_by"`
			UpdatedBy db.Ref    `db:"updated_by"`
			CreatedAt time.Time `db:"created_at"`
		}

		err := database.Get(ctx, &row,
			`SELECT created_by, updated_by, created_at FROM wt_permanent_items WHERE section = ? AND item_id = ?`,
			"pages", "about",
		)
		if err != nil || row.CreatedBy.String() != owner || row.UpdatedBy.String() != editor || row.CreatedAt.IsZero() {
			t.Fatalf("audit columns: %+v %v", row, err)
		}

		if err := marks.SetPermanent(ctx, "", "about", true); err == nil {
			t.Fatal("a mark without section was stored")
		}
	})
}
