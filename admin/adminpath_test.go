package admin_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/admin"
)

// TestAdminPathOption checks Options.Path: the admin answers and links below
// it, by route name, and refuses paths that are not one.
func TestAdminPathOption(t *testing.T) {
	t.Parallel()

	accounts := newAccounts(t)

	for _, path := range []string{"/", "admin", "/admin/", "/back office", "/a//b", "/x?y", "/.", "/..", "/a/./b", "/a/.."} {
		_, err := admin.New(accounts, admin.Options{Path: path, CatalogueDir: t.TempDir()})
		if err == nil {
			t.Errorf("path %q is accepted", path)
		}
	}

	backoffice, err := admin.New(accounts, admin.Options{Path: "/back/office", CatalogueDir: t.TempDir()}, pathSection())
	if err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]string{
		"admin.dashboard":  "/back/office/",
		"admin.login":      "/back/office/login",
		"admin.trash":      "/back/office/trash",
		"admin.notes.list": "/back/office/notes",
	} {
		if got := backoffice.URL(name); got != want {
			t.Errorf("URL(%q): want %s, got %s", name, want, got)
		}
	}

	if got := backoffice.URL("admin.asset", "file", "icons.svg"); got != "/back/office/assets/icons.svg" {
		t.Errorf("icons at %s", got)
	}

	if backoffice.Path() != "/back/office" || backoffice.TrashURL("notes") != "/back/office/trash?section=notes" {
		t.Errorf("path %s, trash %s", backoffice.Path(), backoffice.TrashURL("notes"))
	}

	if _, err := backoffice.TryURL("admin.nothing"); err == nil {
		t.Error("an unknown route has an address")
	}
}

// pathSection is a section without path, which is at its ID, with a named
// route.
func pathSection() admin.Section {
	return admin.Section{
		ID:    "notes",
		Title: "Notes",
		Routes: func(routes *admin.Routes) {
			routes.Get("", admin.View, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("notes"))
			}).As("list")
		},
	}
}

// TestSectionPaths checks that section paths are below the admin and cannot
// take one of the admin's own pages.
func TestSectionPaths(t *testing.T) {
	t.Parallel()

	accounts := newAccounts(t)

	for _, path := range []string{"/admin/notes", "notes", "/notes/", "/login", "/assets/x"} {
		section := pathSection()
		section.Path = path

		_, err := admin.New(accounts, admin.Options{CatalogueDir: t.TempDir()}, section)

		// "/admin/notes" is a valid path below the admin, at /admin/admin/notes.
		if path == "/admin/notes" {
			if err != nil {
				t.Errorf("path %q: %v", path, err)
			}

			continue
		}

		if err == nil {
			t.Errorf("section path %q is accepted", path)
		}
	}
}

// TestAdminPathPages checks a signed-in round through an admin at another
// path: its pages, assets and links are below it.
func TestAdminPathPages(t *testing.T) {
	t.Parallel()

	accounts := newAccounts(t)

	backoffice, err := admin.New(accounts, admin.Options{Path: "/toolbox", CatalogueDir: t.TempDir()}, pathSection())
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/toolbox/", backoffice)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	b := browser{t: t, client: newClient(server), server: server}

	status, page := b.get("/toolbox/login")
	if status != http.StatusOK || strings.Contains(page, `"/admin`) || !strings.Contains(page, `action="/toolbox/login"`) {
		t.Fatalf("login: %d\n%s", status, page)
	}

	status, page = b.post(
		"/toolbox/login",
		url.Values{"Email": {"owner@example.test"}, "Password": {"test-only long passphrase"}},
	)
	if status != http.StatusOK || strings.Contains(page, `"/admin`) {
		t.Fatalf("sign in: %d\n%s", status, page)
	}

	status, page = b.get("/toolbox/notes")
	if status != http.StatusOK || page != "notes" {
		t.Fatalf("notes: %d %s", status, page)
	}

	status, page = b.get("/toolbox/")
	if status != http.StatusOK || strings.Contains(page, `"/admin`) ||
		!strings.Contains(
			page,
			`href="/toolbox/assets/admin.css?v=`,
		) || !strings.Contains(page, `"partial-sse": "/toolbox/assets/partial-sse.js?v=`) {
		t.Fatalf("dashboard: %d\n%s", status, page)
	}

	if status, _ := b.get("/toolbox/assets/icons.svg"); status != http.StatusOK {
		t.Fatalf("icons: %d", status)
	}
}
