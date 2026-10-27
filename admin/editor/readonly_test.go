package editor_test

import (
	"context"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/content"
)

// TestReadOnlyEditor checks that a user who may only view an item sees its
// form disabled and without its button, while an editor gets the form.
func TestReadOnlyEditor(t *testing.T) {
	t.Parallel()

	b, accounts := serve(t, peopleConfig(newStore(ada())))
	ctx := context.Background()

	reader, err := accounts.CreateUser(ctx, "reader@example.test", "", "reader long passphrase", auth.Editor)
	if err != nil {
		t.Fatal(err)
	}

	readers, err := accounts.SaveGroup(ctx, auth.Group{Name: "Readers", Permissions: auth.Permissions{"people": {auth.Read}}})
	if err != nil {
		t.Fatal(err)
	}

	if err := accounts.SetUserGroups(ctx, reader.ID, []string{readers.ID}); err != nil {
		t.Fatal(err)
	}

	_, body := b.get("/admin/people/ada")
	if disabledRole.MatchString(body) || !strings.Contains(body, `>Concept opslaan</button>`) {
		t.Fatal("the owner gets a read-only form")
	}

	r := signIn(t, b.server, "reader@example.test", "reader long passphrase")

	_, body = r.get("/admin/people/ada")
	if !disabledRole.MatchString(body) ||
		strings.Contains(body, `>Concept opslaan</button>`) ||
		!strings.Contains(body, "Je kunt dit item bekijken, maar niet wijzigen.") {
		t.Fatalf("a reader does not get the item read-only:\n%s", body)
	}
}

// disabledRole matches the role field of the people editor, disabled.
var disabledRole = regexp.MustCompile(`<input[^>]*value="Analyst"[^>]*\sdisabled\s`)

var heading = regexp.MustCompile(`<h1 class="admin-page-title">([^<]*)</h1>`)

// TestEditorTitles checks that the heading of a new item is the section's
// translated "new" label, and of an item without label "Untitled"; the tab
// title follows it.
func TestEditorTitles(t *testing.T) {
	t.Parallel()

	config := peopleConfig(newStore(item{ID: "nameless", Revision: content.Revision{ID: "r1", Number: 1}}))
	config.NewLabel = "New page"
	b, _ := serve(t, config)

	cases := map[string]string{
		"/admin/people/new":      "Nieuwe pagina",
		"/admin/people/nameless": "Zonder titel",
	}

	for path, want := range cases {
		_, body := b.get(path)

		match := heading.FindStringSubmatch(body)
		if match == nil || match[1] != want || !strings.Contains(body, "<title>"+want+" ·") {
			t.Fatalf("%s: heading %v, want %q", path, match, want)
		}
	}
}

// TestPreviewWithoutAddress checks that a refused preview returns to the
// editor with a warning, not a success toast.
func TestPreviewWithoutAddress(t *testing.T) {
	t.Parallel()

	config := peopleConfig(newStore(ada()))
	config.PreviewURL = func(context.Context, item, string) string {
		return ""
	}

	b := run(t, config)

	status, body := b.get("/admin/people/ada/revisions/r1/preview?locale=en")

	warning := regexp.MustCompile(`(?s)toast toast-warn".*?<p class="toast-text">` +
		regexp.QuoteMeta("Deze revisie heeft nog geen adres op de website, dus er is daar geen voorbeeld van.") + `</p>`)
	if status != http.StatusOK || !warning.MatchString(body) {
		t.Fatalf("preview without address: %d\n%s", status, body)
	}
}

// TestNoCatalogueRecording checks that showing what editors typed, such
// as an item's label in the title, the overview and a note, writes nothing
// into the admin's catalogue folder.
func TestNoCatalogueRecording(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	config := peopleConfig(newStore(ada()))
	config.Notes = func(context.Context, []item, string) map[string]string {
		return map[string]string{"ada": "Typed by an editor"}
	}

	b, _ := serveWith(t, config, admin.Options{CatalogueDir: dir})

	for _, path := range []string{"/admin/people", "/admin/people/ada", "/admin/people/new"} {
		if status, _ := b.get(path); status != http.StatusOK {
			t.Fatalf("%s: %d", path, status)
		}
	}

	written, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(written) > 0 {
		t.Fatalf("the admin wrote to its catalogue folder: %v", written)
	}
}
