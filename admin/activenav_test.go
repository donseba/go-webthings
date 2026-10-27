package admin_test

import (
	"net/http"
	"regexp"
	"slices"
	"testing"

	"github.com/donseba/go-webthings/admin"
)

var currentLink = regexp.MustCompile(`class="nav-link" href="([^"]+)" aria-current="page"`)

// indexSection is a section whose own page is its route "index", with a page
// for each item below it.
func indexSection(id, title string) admin.Section {
	section := plainSection(id, title, "")
	section.Routes = func(routes *admin.Routes) {
		a := routes.Admin()
		page := func(w http.ResponseWriter, r *http.Request) {
			a.Render(w, r, admin.Page{Title: title, Template: "templates/plain.gohtml", Files: plainFiles, Data: title})
		}

		routes.Get("", admin.View, page).As("index")
		routes.Get("/{id}", admin.View, page).As("edit")
	}

	return section
}

// TestNavigationActive checks which navigation link is the current page:
// the dashboard on its own page only, a section on its own page and the
// pages below it, by path segments, also for a section without an index
// route, and the user's own account page.
func TestNavigationActive(t *testing.T) {
	t.Parallel()

	b := start(
		t,
		admin.Options{Language: "en"},
		indexSection("news", "News"),
		indexSection("newsletter", "Newsletter"),
		plainSection("notes", "Notes", ""),
	)
	b.signIn()

	for path, want := range map[string][]string{
		"/admin/":           {"/admin/"},
		"/admin/news":       {"/admin/news"},
		"/admin/news/7":     {"/admin/news"},
		"/admin/newsletter": {"/admin/newsletter"},
		"/admin/notes":      {"/admin/notes"},
		"/admin/account":    {"/admin/account"},
	} {
		status, body := b.get(path)
		if status != http.StatusOK {
			t.Fatalf("%s: %d", path, status)
		}

		var current []string

		for _, match := range currentLink.FindAllStringSubmatch(body, -1) {
			current = append(current, match[1])
		}

		if !slices.Equal(current, want) {
			t.Errorf("%s: current links %v, want %v", path, current, want)
		}
	}
}
