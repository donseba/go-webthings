package pages_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/pages"
)

// TestMoveTo checks dragging pages in the tree: a reorder among siblings
// changes the menu at once, a new parent is a draft that leaves the menu
// alone until it is published, and places the parent dropdown refuses are
// refused without changing anything.
func TestMoveTo(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := setup(t, database)

		home, err := service.Home(ctx)
		if err != nil {
			t.Fatal(err)
		}

		child := func(parent, slug, title, module string) pages.Page {
			return publish(t, service, pages.Draft{
				Fields:       pages.Fields{ParentID: parent, Module: module},
				Translations: []content.Translation[pages.Text]{text("en", slug, title)},
			})
		}

		a := child(home, "a", "A", "")
		b := child(home, "b", "B", "")
		c := child(home, "c", "C", "")
		x := child(a.ID, "x", "X", "")
		blog := child(home, "blog", "Blog", "blog")

		// A reorder among siblings shows in the menu at once, without a
		// new revision.
		drafted, err := service.MoveTo(ctx, c.ID, home, 0, "")
		if err != nil || drafted {
			t.Fatalf("reorder: drafted %v, %v", drafted, err)
		}

		if got, want := order(t, service, "en"), "Home -C -A --X -B -Blog"; got != want {
			t.Fatalf("tree = %q, want %q", got, want)
		}

		if got, want := menu(t, service, "en"), "/ /c /a /a/x /b /blog"; got != want {
			t.Fatalf("routes = %q, want %q", got, want)
		}

		latest, err := service.Latest(ctx, c.ID)
		if err != nil {
			t.Fatal(err)
		}

		if latest.Revision.Number != c.Revision.Number {
			t.Fatalf("reorder made revision %d", latest.Revision.Number)
		}

		// An index past the end puts the page last.
		if _, err := service.MoveTo(ctx, a.ID, home, 99, ""); err != nil {
			t.Fatal(err)
		}

		if got, want := order(t, service, "en"), "Home -C -B -Blog -A --X"; got != want {
			t.Fatalf("tree after moving last = %q, want %q", got, want)
		}

		// A new parent is a draft by the author: the tree shows the page at
		// the dropped place, the menu keeps it where it was.
		const author = "0190a000-0000-7000-8000-000000000001"

		published := "/ /c /b /blog /a /a/x"

		drafted, err = service.MoveTo(ctx, b.ID, a.ID, 0, author)
		if err != nil || !drafted {
			t.Fatalf("new parent: drafted %v, %v", drafted, err)
		}

		if got, want := order(t, service, "en"), "Home -C -Blog -A --B --X"; got != want {
			t.Fatalf("tree with a new parent = %q, want %q", got, want)
		}

		if got := menu(t, service, "en"); got != published {
			t.Fatalf("routes with a new parent = %q, want %q", got, published)
		}

		draft, err := service.Latest(ctx, b.ID)
		if err != nil {
			t.Fatal(err)
		}

		if draft.Fields.ParentID != a.ID || draft.Revision.Author != author || draft.PublishedNumber == draft.Revision.Number {
			t.Fatalf("draft = parent %s, author %q, revision %d (published %d)",
				draft.Fields.ParentID, draft.Revision.Author, draft.Revision.Number, draft.PublishedNumber)
		}

		// Publishing it moves the page in the menu, at the dropped place.
		if err := service.Publish(ctx, b.ID, draft.Revision.ID); err != nil {
			t.Fatal(err)
		}

		if got, want := menu(t, service, "en"), "/ /c /blog /a /a/b /a/x"; got != want {
			t.Fatalf("routes after publishing = %q, want %q", got, want)
		}

		// Refused: below itself or a page below it, below a module page,
		// Home anywhere but the root, no parent, unknown pages and a
		// negative index.
		refused := []struct {
			id, parent string
			index      int
			want       error
		}{
			{a.ID, a.ID, 0, pages.ErrCycle},
			{a.ID, x.ID, 0, pages.ErrCycle},
			{c.ID, blog.ID, 0, pages.ErrModuleParent},
			{home, a.ID, 0, pages.ErrCycle},
			{c.ID, "", 0, pages.ErrParent},
			{"missing", home, 0, pages.ErrNotFound},
			{c.ID, "0190a000-0000-7000-8000-00000000ffff", 0, pages.ErrNotFound},
			{c.ID, home, -1, pages.ErrInvalid},
		}

		for _, move := range refused {
			drafted, err := service.MoveTo(ctx, move.id, move.parent, move.index, "")
			if !errors.Is(err, move.want) || drafted {
				t.Errorf("MoveTo(%s, %s, %d) = %v, %v; want %v", move.id, move.parent, move.index, drafted, err, move.want)
			}
		}

		if got, want := order(t, service, "en"), "Home -C -Blog -A --B --X"; got != want {
			t.Fatalf("tree after refused moves = %q, want %q", got, want)
		}

		latest, err = service.Latest(ctx, c.ID)
		if err != nil {
			t.Fatal(err)
		}

		if latest.Revision.Number != c.Revision.Number {
			t.Fatalf("refused moves made revision %d", latest.Revision.Number)
		}

		// Home staying where it is changes nothing.
		if drafted, err := service.MoveTo(ctx, home, "", 0, ""); err != nil || drafted {
			t.Fatalf("home in place: %v, %v", drafted, err)
		}
	})
}
