package frontend

import (
	"maps"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/donseba/go-webthings/pages"

	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/connector"
	"github.com/donseba/go-router/middleware"
)

// menuRegion is the theme's region of the menu, regions/menu.gohtml: an
// HTMX navigation sends only the items of it that change (see
// menuUpdate).
const menuRegion = "menu"

// menuIDPrefix starts the ElementID of a menu item.
const menuIDPrefix = "menu-"

// menus keeps the menu of each language, made once per route table: the
// menus of one pages.Table, replaced when the table is.
type menus struct {
	current atomic.Pointer[menuSet]
}

// menuSet is the menus of one route table by locale. It never changes: a
// menu of another locale makes a new set.
type menuSet struct {
	table *pages.Table
	trees map[string]*menuTree
}

// tree returns the menu of locale in table, making it the first time.
func (m *menus) tree(table *pages.Table, locale string) *menuTree {
	set := m.current.Load()
	if set != nil && set.table == table {
		if tree, ok := set.trees[locale]; ok {
			return tree
		}
	}

	tree := newMenuTree(table.Routes(locale), locale)

	trees := map[string]*menuTree{}
	if set != nil && set.table == table {
		maps.Copy(trees, set.trees)
	}

	trees[locale] = tree

	// Two requests that make a menu at once each store theirs; the menus
	// are the same.
	m.current.Store(&menuSet{table: table, trees: trees})

	return tree
}

// menuTree is the menu of one language: its items, which no request
// changes, and where the item of each published page is.
type menuTree struct {
	items []MenuItem
	// places maps the path of every published page to its place in the
	// menu.
	places map[string]menuPlace
}

// menuPlace is where a page is in the menu: the index of its item, and of
// the items above it, in each level from the top; empty for the home page,
// which the menu is below. module is set for a module page, which owns the
// paths below its own.
type menuPlace struct {
	index  []int
	module bool
}

// newMenuTree makes the menu of routes, the published routes of locale in
// tree order: the pages below the home page.
func newMenuTree(routes []pages.Route, locale string) *menuTree {
	tree := &menuTree{places: make(map[string]menuPlace, len(routes))}

	// Grouped by parent once, so the menu takes one pass over the routes
	// rather than one per page.
	children := map[string][]int{}

	home := ""

	for i, route := range routes {
		if route.Depth == 0 {
			home = route.PageID
			tree.places[route.Path] = menuPlace{}

			continue
		}

		children[route.ParentID] = append(children[route.ParentID], i)
	}

	if home != "" {
		tree.items = tree.below(routes, children, locale, home, nil)
	}

	return tree
}

// below makes the items below parent, at index in the menu.
func (t *menuTree) below(
	routes []pages.Route,
	children map[string][]int,
	locale, parent string,
	index []int,
) []MenuItem {
	below := children[parent]
	if len(below) == 0 {
		return nil
	}

	items := make([]MenuItem, len(below))

	for i, at := range below {
		route := routes[at]
		place := append(slices.Clip(index), i)

		t.places[route.Path] = menuPlace{index: place, module: route.Module != ""}

		items[i] = MenuItem{
			ID:        route.PageID,
			ElementID: menuIDPrefix + route.PageID,
			Path:      route.Path,
			Title:     route.Title,
			URL:       link(locale, route.Path, ""),
			Children:  t.below(routes, children, locale, route.PageID, place),
		}
	}

	return items
}

// marked is the menu with the item of the page at current Active, and the
// items above it: the page's own path, or a path below it, such as that of
// a page being previewed before it is published. Only the levels on the
// way to the item are copied; the rest of the menu is the tree's own.
func (t *menuTree) marked(current string) []MenuItem {
	index := t.near(current)
	if len(index) == 0 {
		return t.items
	}

	items := slices.Clone(t.items)
	level := items

	for depth, i := range index {
		level[i].Active = true

		if depth == len(index)-1 {
			break
		}

		level[i].Children = slices.Clone(level[i].Children)
		level = level[i].Children
	}

	return items
}

// near is the index of the item of the page at path, or of the nearest
// page above it; nil for none.
func (t *menuTree) near(path string) []int {
	for path != "" {
		if place, ok := t.places[path]; ok {
			return place.index
		}

		cut := strings.LastIndexByte(path, '/')
		if cut <= 0 {
			path = ""

			continue
		}

		path = path[:cut]
	}

	return nil
}

// resolve is the index of the item of the page that serves path, as
// pages.Service.Resolve finds it: the page at path, or the module page
// above it. ok is false when no page serves path, such as a path of the
// not-found page.
func (t *menuTree) resolve(path string) (index []int, ok bool) {
	if place, found := t.places[path]; found {
		return place.index, true
	}

	for {
		cut := strings.LastIndexByte(path, '/')
		if cut < 0 {
			return nil, false
		}

		path = path[:cut]
		if path == "" {
			path = "/"
		}

		if place, found := t.places[path]; found {
			return place.index, place.module
		}

		if path == "/" {
			return nil, false
		}
	}
}

// changes are the ElementIDs of the items whose Active differs between
// the menu marked at the item at from and at the item at to (see marked):
// the items of either below the part of the way they share.
func (t *menuTree) changes(from, to []int) []string {
	shared := 0
	for shared < len(from) && shared < len(to) && from[shared] == to[shared] {
		shared++
	}

	var ids []string

	ids = t.appendIDs(ids, from, shared)
	ids = t.appendIDs(ids, to, shared)

	return ids
}

// appendIDs appends the ElementIDs of the items on the way to index,
// from level start.
func (t *menuTree) appendIDs(ids []string, index []int, start int) []string {
	level := t.items

	for depth, i := range index {
		if depth >= start {
			ids = append(ids, level[i].ElementID)
		}

		level = level[i].Children
	}

	return ids
}

// menuUpdate is what an HTMX navigation sends of the theme's menu region:
// the ElementIDs of the items whose Active changes from the page the
// visitor comes from (the connector's current URL, HX-Current-URL) to the
// page at current, so the items with those IDs alone go out of band
// (render.View.RegionElementsFunc). A navigation that changes no item
// sends the first item of the way to the page, or of the menu, again, so a
// menu whose items have no IDs is still told apart: it goes whole. The IDs
// are none, and the region goes whole as on any other request, when the
// visitor comes from an address that is not of this site in locale or
// that no page serves, such as one the not-found page answered. The
// response varies with the current URL's header either way. go-partial
// asks for it only when the menu region goes out of band.
func (f *Frontend) menuUpdate(r *http.Request, conn connector.Connector, parts *layoutParts) partial.OOBElements {
	update := partial.OOBElements{Vary: []string{connector.CurrentURLHeader(conn)}}

	from, ok := comingFrom(connector.CurrentURL(conn, r), r, parts.locale)
	if !ok {
		return update
	}

	tree := f.menus.tree(parts.table, parts.locale)
	if len(tree.items) == 0 {
		return update
	}

	before, ok := tree.resolve(from)
	if !ok {
		return update
	}

	after := tree.near(parts.current)

	update.IDs = tree.changes(before, after)
	if len(update.IDs) > 0 {
		return update
	}

	first := []int{0}
	if len(after) > 0 {
		first = after[:1]
	}

	update.IDs = tree.appendIDs(nil, first, 0)

	return update
}

// comingFrom is the page path, without language prefix, of the address
// the visitor comes from, when it is on this site in locale.
func comingFrom(address *url.URL, r *http.Request, locale string) (string, bool) {
	// The host the visitor used, also behind a trusted proxy that rewrites
	// Host; a relative address is on this site.
	if address == nil || address.Host != "" && !middleware.SameOrigin(r, address) {
		return "", false
	}

	prefix := "/" + locale
	if address.Path == prefix {
		return "/", true
	}

	rest, ok := strings.CutPrefix(address.Path, prefix+"/")
	if !ok {
		return "", false
	}

	return cleanSuffix(path.Clean("/"+rest), rest), true
}
