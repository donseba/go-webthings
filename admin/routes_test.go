package admin_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/donseba/go-webthings/admin"

	"github.com/donseba/go-router"
)

var notesFiles = fstest.MapFS{
	"templates/note.gohtml": {
		Data: []byte(`<main id="content"><a id="note" href="{{route "admin.notes.show" "id" .}}">Note</a></main>`),
	},
}

// notesSection names its routes, builds addresses with Admin.URL and the
// route template function, and accepts bodies up to 2 KiB.
func notesSection() admin.Section {
	return admin.Section{
		ID:           "notes",
		Title:        "Notes",
		Path:         "/notes",
		Files:        notesFiles,
		MaxBodyBytes: 2 << 10,
		Routes: func(routes *admin.Routes) {
			a := routes.Admin()

			routes.Get("", admin.View, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, strings.Join([]string{
					a.URL("admin.notes.show", "id", "a b/c", url.Values{"tab": {"history"}}),
					a.URL(routes.Section().RouteName("index")),
					a.URL("admin.trash"),
					r.URL.Path,
				}, "\n"))
			}).As("index")

			routes.Get("/{id}", admin.View, func(w http.ResponseWriter, r *http.Request) {
				page := admin.Page{Title: "Notes", Template: "templates/note.gohtml", Files: notesFiles, Data: r.PathValue("id")}
				a.Render(w, r, page)
			}).As("show")

			routes.Post("/{id}", admin.Edit, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, "saved "+r.PostFormValue("text"))
			})
		},
	}
}

// TestNamedRoutes checks that sections name their routes, and that
// Admin.URL and the route template function build their addresses.
func TestNamedRoutes(t *testing.T) {
	t.Parallel()

	b := start(t, admin.Options{}, notesSection())
	b.signIn()

	status, body := b.get("/admin/notes")
	want := "/admin/notes/a%20b%2Fc?tab=history\n/admin/notes\n/admin/trash\n/admin/notes"

	if status != http.StatusOK || body != want {
		t.Fatalf("addresses: %d\n%s", status, body)
	}

	status, body = b.get("/admin/notes/42")
	if status != http.StatusOK || !strings.Contains(body, `<a id="note" href="/admin/notes/42">`) {
		t.Fatalf("route template function: %d\n%s", status, body)
	}
}

// TestRouteMistakes checks that a route name used twice fails New, and that
// URL panics on an unknown route or parameter rather than build a broken
// link.
func TestRouteMistakes(t *testing.T) {
	t.Parallel()

	twice := notesSection()
	twice.Routes = func(routes *admin.Routes) {
		routes.Get("", admin.View, func(http.ResponseWriter, *http.Request) {}).As("index")
		routes.Get("/other", admin.View, func(http.ResponseWriter, *http.Request) {}).As("index")
	}

	if _, err := admin.New(newAccounts(t), admin.Options{CatalogueDir: t.TempDir()}, twice); err == nil ||
		!strings.Contains(err.Error(), "admin.notes.index") {
		t.Fatalf("a name used twice: %v", err)
	}

	backoffice, err := admin.New(newAccounts(t), admin.Options{CatalogueDir: t.TempDir()}, notesSection())
	if err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]any{
		{"admin.nothing"},
		{"admin.notes.show"},
		{"admin.notes.show", "id", "1", "page", "2"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("URL%v did not panic", args)
				}
			}()

			_ = backoffice.URL(args[0].(string), args[1:]...)
		}()
	}
}

// TestSectionBodyLimit checks that a section's MaxBodyBytes applies to its
// own routes only, and that the admin's pages keep the 512 KiB default.
func TestSectionBodyLimit(t *testing.T) {
	t.Parallel()

	b := start(t, admin.Options{}, notesSection())
	b.signIn()

	large := strings.Repeat("x", 4<<10)

	status, body := b.post("/admin/notes/1", url.Values{"text": {"short"}})
	if status != http.StatusOK || body != "saved short" {
		t.Fatalf("small post: %d %s", status, body)
	}

	if status, _ := b.post("/admin/notes/1", url.Values{"text": {large}}); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("post over the section's limit: %d", status)
	}

	// Signing in is not part of the section: 4 KiB is well within its limit.
	if status, _ := b.post("/admin/login", url.Values{"Email": {large}}); status == http.StatusRequestEntityTooLarge {
		t.Fatal("the section's limit applies outside it")
	}
}

// TestReservedSectionsCoverOwnRoutes checks that every address the admin
// serves itself, outside its sections, is reserved: a module or section with
// that key would otherwise shadow it.
func TestReservedSectionsCoverOwnRoutes(t *testing.T) {
	accounts := newAccounts(t)

	backoffice, err := admin.New(accounts, admin.Options{Title: "Test site", CatalogueDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	reserved := map[string]bool{}
	for _, key := range admin.ReservedSections() {
		reserved[key] = true
	}

	err = backoffice.Router().Walk(func(route router.RouteInfo) error {
		rest := strings.TrimPrefix(route.Path, backoffice.Path()+"/")
		if rest == route.Path || rest == "" || strings.HasPrefix(rest, "{") {
			return nil
		}

		first, _, _ := strings.Cut(rest, "/")
		if !reserved[first] {
			t.Errorf("the admin serves %s, but %q is not reserved", route.Path, first)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
