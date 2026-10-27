package editor_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/content"
)

// TestTrash deletes items in an editor section and restores and purges them
// in the admin's trash.
func TestTrash(t *testing.T) {
	t.Parallel()

	grace := ada()
	grace.ID = "grace"
	grace.Translations = []content.Translation[text]{{Locale: "en", Text: text{Role: "Admiral"}}}

	people := newStore(ada(), grace)
	b, accounts := serve(t, editor.Config[person, text, personInput]{
		Store: people,
		Label: func(value text) string {
			return value.Role
		},
	})

	_, body := b.get("/admin/people")
	if !strings.Contains(body, `href="/admin/trash?section=people"`) {
		t.Fatal("the overview does not link to its trash")
	}

	_, body = b.get("/admin/trash")
	if !strings.Contains(body, "De prullenbak is leeg.") || !strings.Contains(body, `href="/admin/trash"`) {
		t.Fatal("empty trash or its navigation link missing")
	}

	for _, id := range []string{"ada", "grace"} {
		status, body := b.post("/admin/people/"+id, "/admin/people/"+id+"/delete", url.Values{})
		if status != http.StatusOK || !strings.Contains(body, "Naar de prullenbak verplaatst.") {
			t.Fatalf("delete %s: %d", id, status)
		}
	}

	_, body = b.get("/admin/trash")
	for _, want := range []string{"Analyst", "Admiral", `value="people:ada"`, "Selectie herstellen"} {
		if !strings.Contains(body, want) {
			t.Fatalf("trash misses %q", want)
		}
	}

	// The search and the section filter narrow the list.
	if _, body = b.get(
		"/admin/trash?section=people&q=admiral",
	); strings.Contains(body, "Analyst") ||
		!strings.Contains(body, "Admiral") {
		t.Fatal("search of one section's trash")
	}

	// A conflict keeps the item in the trash and says why; the others
	// come back.
	people.undeleteErr["grace"] = content.ErrDuplicateSlug

	status, body := b.post("/admin/trash", "/admin/trash", url.Values{
		"action": {"restore"},
		"item":   {"people:ada", "people:grace"},
	})
	if status != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "1 item hersteld.") ||
		!strings.Contains(body, "Dit item kan niet worden hersteld: een ander item gebruikt nu hetzelfde webadres.") {
		t.Fatalf("restore with a conflict: %d", status)
	}

	if _, err := people.Load(context.Background(), "ada", ""); err != nil {
		t.Fatalf("ada not restored: %v", err)
	}

	// An editor who may only read sees the trash, but cannot change it.
	reader, err := accounts.CreateUser(context.Background(), "reader@example.test", "", "reader long passphrase", auth.Editor)
	if err != nil {
		t.Fatal(err)
	}

	readers, err := accounts.SaveGroup(context.Background(), auth.Group{
		Name:        "Readers",
		Permissions: auth.Permissions{"people": {auth.Read}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := accounts.SetUserGroups(context.Background(), reader.ID, []string{readers.ID}); err != nil {
		t.Fatal(err)
	}

	r := signIn(t, b.server, "reader@example.test", "reader long passphrase")

	_, body = r.get("/admin/trash")
	if !strings.Contains(body, "Admiral") || strings.Contains(body, `value="people:grace"`) ||
		strings.Contains(body, "Selectie herstellen") {
		t.Fatal("a reader may change the trash")
	}

	r.post("/admin/trash", "/admin/trash", url.Values{"action": {"purge"}, "item": {"people:grace"}})

	if trashed, _ := people.Trashed(context.Background()); len(trashed) != 1 {
		t.Fatal("a reader purged an item")
	}

	status, body = b.post("/admin/trash", "/admin/trash", url.Values{"action": {"purge"}, "item": {"people:grace"}})
	if status != http.StatusOK || !strings.Contains(body, "1 item definitief verwijderd.") {
		t.Fatalf("purge: %d", status)
	}

	if trashed, _ := people.Trashed(context.Background()); len(trashed) != 0 {
		t.Fatal("purged item still in the trash")
	}
}
