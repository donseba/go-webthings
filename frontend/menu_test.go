package frontend

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/pages"
)

// menuRoutes are published routes in tree order: Home, About with Team
// (with Ada) and Contact, News (a module) and Work.
func menuRoutes() []pages.Route {
	return []pages.Route{
		{Path: "/", PageID: "home", Depth: 0},
		{Path: "/about", PageID: "about", ParentID: "home", Depth: 1, Title: "About"},
		{Path: "/about/team", PageID: "team", ParentID: "about", Depth: 2, Title: "Team"},
		{Path: "/about/team/ada", PageID: "ada", ParentID: "team", Depth: 3, Title: "Ada"},
		{Path: "/about/contact", PageID: "contact", ParentID: "about", Depth: 2, Title: "Contact"},
		{Path: "/news", PageID: "news", ParentID: "home", Depth: 1, Title: "News", Module: "news"},
		{Path: "/work", PageID: "work", ParentID: "home", Depth: 1, Title: "Work"},
	}
}

// referenceMenu is the menu as it was made for every request: the whole
// tree, each item Active when current is its path or below it.
func referenceMenu(routes []pages.Route, locale, parent, current string) []MenuItem {
	var items []MenuItem

	for _, route := range routes {
		if route.Depth == 0 || route.ParentID != parent {
			continue
		}

		items = append(items, MenuItem{
			ID:        route.PageID,
			ElementID: "menu-" + route.PageID,
			Path:      route.Path,
			Title:     route.Title,
			URL:       link(locale, route.Path, ""),
			Active:    current == route.Path || strings.HasPrefix(current, route.Path+"/"),
			Children:  referenceMenu(routes, locale, route.PageID, current),
		})
	}

	return items
}

// TestMenuMarked checks that the menu made once and marked for a request is
// the menu made for every request before, and that marking leaves the
// tree as it is.
func TestMenuMarked(t *testing.T) {
	routes := menuRoutes()
	tree := newMenuTree(routes, "en")
	unmarked := referenceMenu(routes, "en", "home", "")

	for _, current := range []string{"", "/", "/about", "/about/team", "/about/team/ada", "/about/team/new", "/about/contact", "/news", "/work", "/search", "/missing/deeper"} {
		if got, want := tree.marked(current), referenceMenu(routes, "en", "home", current); !reflect.DeepEqual(got, want) {
			t.Fatalf("%q:\n got %+v\nwant %+v", current, got, want)
		}

		if !reflect.DeepEqual(tree.items, unmarked) {
			t.Fatalf("%q: the tree changed: %+v", current, tree.items)
		}
	}
}

// TestMenuChanges checks which items an HTMX navigation sends.
func TestMenuChanges(t *testing.T) {
	tree := newMenuTree(menuRoutes(), "en")

	for _, tc := range []struct {
		from, to string
		ok       bool
		want     string
	}{
		{"/about/team/ada", "/work", true, "menu-about menu-team menu-ada menu-work"},
		{"/about/team", "/about/contact", true, "menu-team menu-contact"},
		{"/about/team", "/about", true, "menu-team"},
		{"/", "/about/team", true, "menu-about menu-team"},
		{"/news/hello", "/work", true, "menu-news menu-work"},
		{"/about/team/ada", "/about/team/ada", true, ""},
		{"/about/missing", "/work", false, ""},
		{"/missing", "/work", false, ""},
	} {
		from, ok := tree.resolve(tc.from)
		if ok != tc.ok {
			t.Fatalf("%s: resolved %v, want %v", tc.from, ok, tc.ok)
		}

		if !ok {
			continue
		}

		if got := strings.Join(tree.changes(from, tree.near(tc.to)), " "); got != tc.want {
			t.Fatalf("%s to %s: %q, want %q", tc.from, tc.to, got, tc.want)
		}
	}
}

// TestMenusPerTable checks that a menu is made once per route table and
// language.
func TestMenusPerTable(t *testing.T) {
	var kept menus

	first, second := &pages.Table{}, &pages.Table{}

	en := kept.tree(first, "en")
	nl := kept.tree(first, "nl")

	if kept.tree(first, "en") != en || kept.tree(first, "nl") != nl || en == nl {
		t.Fatal("menus of one table are made again")
	}

	if kept.tree(second, "en") == en {
		t.Fatal("menu of another table is not made again")
	}
}

// TestComingFrom checks which HX-Current-URL addresses navigation reads.
func TestComingFrom(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://example.com/en/work", nil)

	for address, want := range map[string]string{
		"http://example.com/en/about":        "/about",
		"http://example.com/en/about/":       "/about",
		"http://example.com/en":              "/",
		"http://example.com/en/":             "/",
		"/en/about/team?q=1#top":             "/about/team",
		"http://elsewhere.example/en/about":  "",
		"http://example.com/nl/about":        "",
		"http://example.com/english":         "",
		"http://example.com/admin/pages/123": "",
		"HTTPS://Example.COM/en/about":       "/about",
		"ftp://example.com/en/about":         "",
	} {
		parsed, err := url.Parse(address)
		if err != nil {
			t.Fatal(err)
		}

		from, ok := comingFrom(parsed, request, "en")
		if from != want || ok != (want != "") {
			t.Fatalf("%s: %q %v, want %q", address, from, ok, want)
		}
	}

	if _, ok := comingFrom(nil, request, "en"); ok {
		t.Fatal("no address")
	}
}
