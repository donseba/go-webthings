package admin_test

import (
	"context"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
)

var plainFiles = fstest.MapFS{
	"templates/plain.gohtml": {Data: []byte(`<main id="content"><h1>{{.}}</h1></main>`)},
}

// plainSection is a section with one page, in a navigation group.
func plainSection(id, title, group string) admin.Section {
	return admin.Section{
		ID:    id,
		Title: title,
		Path:  "/" + id,
		Group: group,
		Files: plainFiles,
		Routes: func(routes *admin.Routes) {
			a := routes.Admin()

			show := func(w http.ResponseWriter, r *http.Request) {
				a.Render(w, r, admin.Page{Title: title, Template: "templates/plain.gohtml", Files: plainFiles, Data: title})
			}

			// An item's page, such as the one editors look at together (see
			// presence_test.go).
			routes.Get("", admin.View, show)
			routes.Get("/{id}", admin.View, show)
		},
	}
}

func withBadge(section admin.Section, count int) admin.Section {
	section.Badge = func(context.Context) int {
		return count
	}

	return section
}

var (
	navigationLink = regexp.MustCompile(`class="nav-link" href="([^"]+)"`)
	navigationKey  = regexp.MustCompile(`data-nav-group="([^"]+)"`)
)

// sidebar returns the links and the group keys of the navigation, in order.
func sidebar(t *testing.T, body string) (links, groups []string) {
	t.Helper()

	start := strings.Index(body, `<aside id="admin-sidebar"`)
	end := strings.Index(body, `</aside>`)

	if start < 0 || end < start {
		t.Fatal("no sidebar")
	}

	body = body[start:end]

	for _, match := range navigationLink.FindAllStringSubmatch(body, -1) {
		links = append(links, match[1])
	}

	for _, match := range navigationKey.FindAllStringSubmatch(body, -1) {
		groups = append(groups, match[1])
	}

	return links, groups
}

// TestNavigationGroups checks the order of the navigation: the sections
// without group at the top in their order, then the groups by translated
// name with System last, and the links of a group by translated title,
// ignoring case.
func TestNavigationGroups(t *testing.T) {
	t.Parallel()

	sections := []admin.Section{
		plainSection("pages", "Pages", ""),
		plainSection("media", "Media", ""),
		withBadge(plainSection("zeta", "Zeta", "Website"), 2),
		withBadge(plainSection("alpha", "alpha", "Website"), 3),
		plainSection("news", "News", "Content"),
		plainSection("guestbook", "Guestbook", "Visitors"),
		plainSection("secret", "Secret", "Visitors"),
		plainSection("plans", "Plans", "Pleasantries"),
		func() admin.Section {
			hidden := plainSection("archive", "Archive", "Content")
			hidden.Hidden = true

			return hidden
		}(),
	}

	authorize := func(_ context.Context, _ auth.User, permission admin.Permission) bool {
		return permission.Section != "secret"
	}

	tests := []struct {
		language string
		links    []string
		groups   []string
		names    []string
	}{
		{
			language: "en",
			links: []string{
				"/admin/", "/admin/pages", "/admin/media",
				"/admin/news",
				"/admin/plans",
				"/admin/guestbook",
				"/admin/alpha", "/admin/zeta",
				"/admin/accounts", "/admin/groups", "/admin/account",
			},
			groups: []string{"Content", "Pleasantries", "Visitors", "Website", "System"},
			names:  []string{"Content", "Pleasantries", "Visitors", "Website", "System"},
		},
		{
			// Dutch sorts by the translated names: Bezoekers, Inhoud, and
			// Accounts, Groepen, Je account; Pleasantries has no translation
			// and keeps its name.
			language: "nl",
			links: []string{
				"/admin/", "/admin/pages", "/admin/media",
				"/admin/guestbook",
				"/admin/news",
				"/admin/plans",
				"/admin/alpha", "/admin/zeta",
				"/admin/accounts", "/admin/groups", "/admin/account",
			},
			groups: []string{"Visitors", "Content", "Pleasantries", "Website", "System"},
			names:  []string{"Bezoekers", "Inhoud", "Pleasantries", "Website", "Systeem"},
		},
	}

	for _, test := range tests {
		t.Run(test.language, func(t *testing.T) {
			b := start(t, admin.Options{Language: test.language, Authorize: authorize}, sections...)
			b.signIn()

			status, body := b.get("/admin/zeta")
			if status != http.StatusOK {
				t.Fatalf("zeta: %d", status)
			}

			links, groups := sidebar(t, body)
			if !slices.Equal(links, test.links) {
				t.Errorf("links = %v\nwant    %v", links, test.links)
			}

			if !slices.Equal(groups, test.groups) {
				t.Errorf("groups = %v, want %v", groups, test.groups)
			}

			for _, name := range test.names {
				if !strings.Contains(body, "<span>"+name+"</span>") {
					t.Errorf("group heading %q missing", name)
				}
			}

			// The group of the current page is marked, so it stays open; the
			// heading counts the badges of its links.
			if !strings.Contains(body, `data-nav-group="Website" data-active`) {
				t.Error("the group of the current page is not active")
			}

			if strings.Contains(body, `data-nav-group="Content" data-active`) {
				t.Error("another group is active")
			}

			if !strings.Contains(body, `<span class="nav-group-badge">5</span>`) {
				t.Error("the group badge does not count its links")
			}

			if !strings.Contains(body, `aria-expanded="true" aria-controls="nav-group-`) {
				t.Error("the group toggle is not accessible")
			}

			// A hidden section keeps its group open.
			_, body = b.get("/admin/archive")
			if !strings.Contains(body, `data-nav-group="Content" data-active`) {
				t.Error("the group of a hidden section is not active on its page")
			}
		})
	}
}

// TestNavigationForEditors checks that editors see neither the admin's own
// sections nor groups without a section they may open.
func TestNavigationForEditors(t *testing.T) {
	t.Parallel()

	sections := []admin.Section{
		plainSection("pages", "Pages", ""),
		plainSection("news", "News", "Content"),
		plainSection("secret", "Secret", "Visitors"),
	}

	accounts := newAccounts(t)
	ctx := context.Background()

	editor, err := accounts.CreateUser(ctx, "writer@example.test", "Writer", "writer long passphrase", auth.Editor)
	if err != nil {
		t.Fatal(err)
	}

	group, err := accounts.SaveGroup(ctx, auth.Group{
		Name:        "Writers",
		Permissions: auth.Permissions{"news": {auth.Read}, "pages": {auth.Read}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := accounts.SetUserGroups(ctx, editor.ID, []string{group.ID}); err != nil {
		t.Fatal(err)
	}

	b := startWith(t, accounts, admin.Options{Language: "en"}, sections...)
	b.signInAs("writer@example.test", "writer long passphrase")

	_, body := b.get("/admin/")

	links, groups := sidebar(t, body)
	if want := []string{"/admin/", "/admin/pages", "/admin/news", "/admin/account"}; !slices.Equal(links, want) {
		t.Fatalf("links = %v, want %v", links, want)
	}

	if want := []string{"Content", "System"}; !slices.Equal(groups, want) {
		t.Fatalf("groups = %v, want %v", groups, want)
	}
}
