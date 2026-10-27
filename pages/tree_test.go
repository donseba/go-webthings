package pages_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/pages"
)

// order lists the titles of the tree, indented by depth, in one language.
func order(t *testing.T, service *pages.Service, locale string) string {
	t.Helper()

	nodes, err := service.Tree(context.Background(), locale)
	if err != nil {
		t.Fatal(err)
	}

	var titles []string

	for _, node := range nodes {
		text, _ := node.Page.Text(locale)
		titles = append(titles, strings.Repeat("-", node.Depth)+text.Title)
	}

	return strings.Join(titles, " ")
}

// menu lists the published paths of a language in route order.
func menu(t *testing.T, service *pages.Service, locale string) string {
	t.Helper()

	routes, err := service.Routes(context.Background(), locale)
	if err != nil {
		t.Fatal(err)
	}

	var paths []string

	for _, route := range routes {
		paths = append(paths, route.Path)
	}

	return strings.Join(paths, " ")
}

// TestTreeOrderAndMoves checks that new pages go last among their siblings,
// that moves reorder siblings at once in both the tree and the published
// routes, and that the routes are in tree order.
func TestTreeOrderAndMoves(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := setup(t, database)

		home, err := service.Home(ctx)
		if err != nil {
			t.Fatal(err)
		}

		// Titles in reverse alphabetical order: creation order wins.
		zoo := publish(t, service, pages.Draft{
			Fields:       pages.Fields{ParentID: home},
			Translations: []content.Translation[pages.Text]{text("en", "zoo", "Zoo")},
		})
		mid := publish(t, service, pages.Draft{
			Fields:       pages.Fields{ParentID: home},
			Translations: []content.Translation[pages.Text]{text("en", "mid", "Mid")},
		})
		publish(t, service, pages.Draft{
			Fields:       pages.Fields{ParentID: zoo.ID},
			Translations: []content.Translation[pages.Text]{text("en", "cage", "Cage")},
		})
		apple := publish(t, service, pages.Draft{
			Fields:       pages.Fields{ParentID: home},
			Translations: []content.Translation[pages.Text]{text("en", "apple", "Apple")},
		})

		if got, want := order(t, service, "en"), "Home -Zoo --Cage -Mid -Apple"; got != want {
			t.Fatalf("tree = %q, want %q", got, want)
		}

		if got, want := menu(t, service, "en"), "/ /zoo /zoo/cage /mid /apple"; got != want {
			t.Fatalf("routes = %q, want %q", got, want)
		}

		if err := service.Move(ctx, apple.ID, -1); err != nil {
			t.Fatal(err)
		}

		if err := service.Move(ctx, zoo.ID, 1); err != nil {
			t.Fatal(err)
		}

		if got, want := order(t, service, "en"), "Home -Apple -Zoo --Cage -Mid"; got != want {
			t.Fatalf("moved tree = %q, want %q", got, want)
		}

		// The published routes follow without publishing again.
		if got, want := menu(t, service, "en"), "/ /apple /zoo /zoo/cage /mid"; got != want {
			t.Fatalf("moved routes = %q, want %q", got, want)
		}

		// Moves stop at the ends; Home does not move.
		for _, move := range []struct {
			id     string
			offset int
		}{{apple.ID, -5}, {mid.ID, 3}, {home, 1}} {
			if err := service.Move(ctx, move.id, move.offset); err != nil {
				t.Fatal(err)
			}
		}

		if got, want := order(t, service, "en"), "Home -Apple -Zoo --Cage -Mid"; got != want {
			t.Fatalf("tree after edge moves = %q, want %q", got, want)
		}

		if err := service.Move(ctx, "missing", 1); !errors.Is(err, pages.ErrNotFound) {
			t.Fatalf("move unknown page: %v", err)
		}

		// A new draft keeps the page's place; a new parent puts it last there.
		latest, err := service.Latest(ctx, apple.ID)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := service.Save(
			ctx,
			pages.Draft{ID: apple.ID, Fields: latest.Fields, Translations: latest.Translations},
		); err != nil {
			t.Fatal(err)
		}

		if got, want := order(t, service, "en"), "Home -Apple -Zoo --Cage -Mid"; got != want {
			t.Fatalf("tree after saving = %q, want %q", got, want)
		}

		publish(t, service, pages.Draft{
			ID:           mid.ID,
			Fields:       pages.Fields{ParentID: zoo.ID},
			Translations: mid.Translations,
		})

		if got, want := order(t, service, "en"), "Home -Apple -Zoo --Cage --Mid"; got != want {
			t.Fatalf("tree after new parent = %q, want %q", got, want)
		}

		// An explicit position places the page and the published routes follow.
		if _, err := service.Save(ctx, pages.Draft{
			ID:           apple.ID,
			Fields:       pages.Fields{ParentID: home, Position: 9},
			Translations: apple.Translations,
		}); err != nil {
			t.Fatal(err)
		}

		if got, want := menu(t, service, "en"), "/ /zoo /zoo/cage /zoo/mid /apple"; got != want {
			t.Fatalf("routes after explicit position = %q, want %q", got, want)
		}

		resolved, err := service.Resolve(ctx, "en", "/apple")
		if err != nil || resolved.Page.Fields.Position != 9 {
			t.Fatalf("resolved position: %+v %v", resolved.Page.Fields, err)
		}
	})
}

// TestSlugFromTitle checks that pages without a slug get one from their
// title, unique among their siblings and away from reserved slugs.
func TestSlugFromTitle(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := setup(t, database)

		home, err := service.Home(ctx)
		if err != nil {
			t.Fatal(err)
		}

		slug := func(parent, locale, title string) string {
			t.Helper()

			page, err := service.Save(ctx, pages.Draft{
				Fields:       pages.Fields{ParentID: parent},
				Translations: []content.Translation[pages.Text]{text(locale, "", title)},
			})
			if err != nil {
				t.Fatal(err)
			}

			saved, _ := page.Text(locale)

			return saved.Slug
		}

		contact := slug(home, "en", "Contact us!")
		if contact != "contact-us" {
			t.Fatalf("slug = %q", contact)
		}

		if got := slug(home, "en", "Contact us"); got != "contact-us-2" {
			t.Fatalf("second slug = %q", got)
		}

		if got := slug(home, "en", "Admin"); got != "admin-2" {
			t.Fatalf("reserved slug = %q", got)
		}

		if got := slug(home, "nl", "Café"); got != "cafe" {
			t.Fatalf("accented slug = %q", got)
		}

		latest, err := service.ListLatest(ctx)
		if err != nil {
			t.Fatal(err)
		}

		var parent string

		for _, page := range latest {
			if text, _ := page.Text("en"); text.Slug == "contact-us" {
				parent = page.ID
			}
		}

		// Below another parent the same slug is free.
		if got := slug(parent, "en", "Contact us"); got != "contact-us" {
			t.Fatalf("child slug = %q", got)
		}

		// A title without letters or digits cannot make a slug.
		_, err = service.Save(ctx, pages.Draft{
			Fields:       pages.Fields{ParentID: home},
			Translations: []content.Translation[pages.Text]{text("en", "", "!!!")},
		})
		if !errors.Is(err, pages.ErrSlug) || !errors.Is(err, pages.ErrInvalid) {
			t.Fatalf("no slug: %v", err)
		}

		_, err = service.Save(ctx, pages.Draft{
			Translations: []content.Translation[pages.Text]{text("en", "", "Orphan")},
		})
		if !errors.Is(err, pages.ErrParent) {
			t.Fatalf("no parent: %v", err)
		}
	})
}

// TestHomeStays checks that Home cannot be taken off the site.
func TestHomeStays(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := setup(t, database)

		home, err := service.Home(ctx)
		if err != nil {
			t.Fatal(err)
		}

		later := content.Schedule{UnpublishAt: time.Now().Add(time.Hour)}

		for name, err := range map[string]error{
			"unpublish": service.Unpublish(ctx, home),
			"delete":    service.Delete(ctx, home),
			"schedule":  service.Schedule(ctx, home, later),
		} {
			if !errors.Is(err, pages.ErrHome) || !errors.Is(err, pages.ErrInvalid) {
				t.Errorf("%s home: %v", name, err)
			}
		}
	})
}

// TestPendingParent checks that saving a published page under another parent
// leaves the published menu alone until that revision is published, while
// reordering published siblings still shows at once.
func TestPendingParent(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := setup(t, database)

		home, err := service.Home(ctx)
		if err != nil {
			t.Fatal(err)
		}

		child := func(parent, slug, title string) pages.Page {
			return publish(t, service, pages.Draft{
				Fields:       pages.Fields{ParentID: parent},
				Translations: []content.Translation[pages.Text]{text("en", slug, title)},
			})
		}

		a := child(home, "a", "A")
		b := child(home, "b", "B")
		x := child(a.ID, "x", "X")
		child(a.ID, "z", "Z")
		child(b.ID, "y1", "Y1")
		y2 := child(b.ID, "y2", "Y2")

		published := "/ /a /a/x /a/z /b /b/y1 /b/y2"
		if got := menu(t, service, "en"); got != published {
			t.Fatalf("routes = %q, want %q", got, published)
		}

		// A draft under B: the admin's tree shows it there, the menu does
		// not change.
		draft, err := service.Save(ctx, pages.Draft{
			ID:           x.ID,
			Fields:       pages.Fields{ParentID: b.ID},
			Translations: x.Translations,
		})
		if err != nil {
			t.Fatal(err)
		}

		if got, want := order(t, service, "en"), "Home -A --Z -B --Y1 --Y2 --X"; got != want {
			t.Fatalf("tree = %q, want %q", got, want)
		}

		if got := menu(t, service, "en"); got != published {
			t.Fatalf("routes after a draft with a new parent = %q, want %q", got, published)
		}

		if _, err := service.Resolve(ctx, "en", "/a/x"); err != nil {
			t.Fatalf("published path: %v", err)
		}

		// Saving again keeps both places.
		if _, err := service.Save(ctx, pages.Draft{
			ID:           x.ID,
			Fields:       pages.Fields{ParentID: b.ID},
			Translations: x.Translations,
		}); err != nil {
			t.Fatal(err)
		}

		if got := menu(t, service, "en"); got != published {
			t.Fatalf("routes after saving again = %q, want %q", got, published)
		}

		// Reordering published siblings shows at once.
		if err := service.Move(ctx, y2.ID, -1); err != nil {
			t.Fatal(err)
		}

		if got, want := menu(t, service, "en"), "/ /a /a/x /a/z /b /b/y2 /b/y1"; got != want {
			t.Fatalf("routes after a move = %q, want %q", got, want)
		}

		// Publishing the parent change moves the page in the menu, last
		// below its new parent.
		if err := service.Publish(ctx, x.ID, draft.Revision.ID); err != nil {
			t.Fatal(err)
		}

		if got, want := menu(t, service, "en"), "/ /a /a/z /b /b/y2 /b/y1 /b/x"; got != want {
			t.Fatalf("routes after publishing = %q, want %q", got, want)
		}

		// A draft back under A and then under B again: the menu keeps the
		// published place throughout, and the tree shows the draft's.
		for _, parent := range []string{a.ID, b.ID} {
			if _, err := service.Save(ctx, pages.Draft{
				ID:           x.ID,
				Fields:       pages.Fields{ParentID: parent},
				Translations: x.Translations,
			}); err != nil {
				t.Fatal(err)
			}

			if got, want := menu(t, service, "en"), "/ /a /a/z /b /b/y2 /b/y1 /b/x"; got != want {
				t.Fatalf("routes with a draft under %s = %q, want %q", parent, got, want)
			}
		}

		if got, want := order(t, service, "en"), "Home -A --Z -B --Y2 --Y1 --X"; got != want {
			t.Fatalf("tree back under the published parent = %q, want %q", got, want)
		}

		// Restoring the revision under A is a draft too.
		history, err := service.History(ctx, x.ID)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := service.Restore(ctx, x.ID, history[len(history)-1].ID, ""); err != nil {
			t.Fatal(err)
		}

		if got, want := order(t, service, "en"), "Home -A --Z --X -B --Y2 --Y1"; got != want {
			t.Fatalf("tree after restoring = %q, want %q", got, want)
		}

		if got, want := menu(t, service, "en"), "/ /a /a/z /b /b/y2 /b/y1 /b/x"; got != want {
			t.Fatalf("routes after restoring = %q, want %q", got, want)
		}
	})
}

// TestEqualPositions checks that siblings with equal positions are in the
// same order in the admin's tree, in the menu of every language and for
// Move: the order they were created in, whatever their titles.
func TestEqualPositions(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := setup(t, database)

		home, err := service.Home(ctx)
		if err != nil {
			t.Fatal(err)
		}

		first := publish(t, service, pages.Draft{
			Fields: pages.Fields{ParentID: home, Position: 5},
			Translations: []content.Translation[pages.Text]{
				text("en", "zebra", "Zebra"),
				text("nl", "aap", "Aap"),
			},
		})
		second := publish(t, service, pages.Draft{
			Fields: pages.Fields{ParentID: home, Position: 5},
			Translations: []content.Translation[pages.Text]{
				text("en", "apple", "Apple"),
				text("nl", "zon", "Zon"),
			},
		})

		for _, latest := range []pages.Page{first, second} {
			page, err := service.Latest(ctx, latest.ID)
			if err != nil || page.Fields.Position != 5 {
				t.Fatalf("position: %+v %v", page.Fields, err)
			}
		}

		cases := map[string][2]string{
			"en": {"Home -Zebra -Apple", "/ /zebra /apple"},
			"nl": {"Thuis -Aap -Zon", "/ /aap /zon"},
		}

		for locale, want := range cases {
			if got := order(t, service, locale); got != want[0] {
				t.Errorf("%s tree = %q, want %q", locale, got, want[0])
			}

			if got := menu(t, service, locale); got != want[1] {
				t.Errorf("%s routes = %q, want %q", locale, got, want[1])
			}
		}

		if err := service.Move(ctx, second.ID, -1); err != nil {
			t.Fatal(err)
		}

		if got, want := order(t, service, "nl"), "Thuis -Zon -Aap"; got != want {
			t.Fatalf("tree after a move = %q, want %q", got, want)
		}

		if got, want := menu(t, service, "en"), "/ /apple /zebra"; got != want {
			t.Fatalf("routes after a move = %q, want %q", got, want)
		}
	})
}
