package pages_test

import (
	"context"
	"testing"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/pages"
)

// TestOutline checks that the tree, which is read without the pages'
// descriptions and bodies, still has their titles and slugs, and that the
// changes made from it keep the bodies: a slug made unique among siblings,
// and a move below another parent, which saves a new revision.
func TestOutline(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := setup(t, database)

		home, err := service.Home(ctx)
		if err != nil {
			t.Fatal(err)
		}

		withBody := func(parent, title, body string) pages.Page {
			return publish(t, service, pages.Draft{
				Fields: pages.Fields{ParentID: parent},
				Translations: []content.Translation[pages.Text]{{
					Locale: "en",
					Text:   pages.Text{Title: title, Description: title + " in short", Body: body},
				}},
			})
		}

		about := withBody(home, "About", "About **us**.")
		contact := withBody(home, "Contact", "Call us.")
		second := withBody(home, "Contact", "Write to us.")

		nodes, err := service.Tree(ctx, "en")
		if err != nil || len(nodes) != 4 {
			t.Fatalf("tree: %d nodes, %v", len(nodes), err)
		}

		for _, node := range nodes[1:] {
			text, _ := node.Page.Text("en")
			if text.Title == "" || text.Slug == "" || text.Body != "" || text.Description != "" {
				t.Fatalf("outline of %s: %+v", node.Page.ID, text)
			}
		}

		if got := nodes[3].Path; got != "/contact-2" {
			t.Fatalf("unique slug from the outline: %q", got)
		}

		// A move below another parent keeps the body and the description.
		drafted, err := service.MoveTo(ctx, contact.ID, about.ID, 0, "")
		if err != nil || !drafted {
			t.Fatalf("move: drafted %v, %v", drafted, err)
		}

		moved, err := service.Latest(ctx, contact.ID)
		if err != nil {
			t.Fatal(err)
		}

		text, _ := moved.Text("en")
		if text.Body != "Call us." || text.Description != "Contact in short" || text.Slug != "contact" {
			t.Fatalf("moved page: %+v", text)
		}

		// Saving keeps the full texts it is given.
		latest, err := service.Latest(ctx, second.ID)
		if err != nil {
			t.Fatal(err)
		}

		saved, err := service.Save(ctx, pages.Draft{ID: latest.ID, Fields: latest.Fields, Translations: latest.Translations})
		if err != nil {
			t.Fatal(err)
		}

		if text, _ := saved.Text("en"); text.Body != "Write to us." {
			t.Fatalf("saved page: %+v", text)
		}

		if _, err := service.Content().Outline(ctx, "pages", "slug", "nonsense"); err == nil {
			t.Fatal("an unknown text was accepted")
		}
	})
}
