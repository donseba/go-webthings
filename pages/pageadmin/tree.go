package pageadmin

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/pages"
)

// TreeView is the pages overview: the page tree in menu order.
type TreeView struct {
	Title    string
	NewURL   string
	NewLabel string
	// TrashURL shows the deleted pages in the admin's trash.
	TrashURL string
	// LinksURL is the report of the links that lead nowhere.
	LinksURL string
	Tabs     []admin.LocaleTab
	// Pages holds Home with the tree below it, followed by pages whose
	// parent no longer exists.
	Pages []TreeRow
}

// TreeRow is one page in the overview, with the pages below it.
type TreeRow struct {
	Title string
	URL   string
	// Path is the page's path in the shown language, empty while a slug on
	// the way is missing.
	Path string
	// Status is the publication state, as the other editors' overviews
	// name it (see editor.Status): Draft, In review, Published, Changes not
	// published or Scheduled.
	Status string
	// Module is the title of the module the page shows, if any.
	Module  string
	Home    bool
	Missing bool
	// Moves are the up and down buttons: for every page except Home, for
	// editors with the right to edit.
	Moves []Move
	// ID and ParentID let the browser tell where a dragged page lands.
	ID       string
	ParentID string
	// DragURL is where dragging the page posts its new place (see moveTo),
	// set with the up and down buttons.
	DragURL string
	// Leaf marks a module page, which cannot have child pages.
	Leaf bool
	// Match marks a page found by the search; the tree also shows the
	// pages above a match, without Match.
	Match    bool
	Children []TreeRow
}

// Move is a button that moves a page among its siblings with a POST to URL.
// URL is empty at the first or last place, where the button is disabled. It
// is the admin's shared arrow, shown with the "admin-moves" template.
type Move = admin.Move

type pageEditor = editor.Editor[pages.Fields, pages.Text, input]

// overview shows the latest page tree in the order of the website's menu,
// answering with code (0 is 200 OK).
func overview(w http.ResponseWriter, r *http.Request, e pageEditor, tree *pages.Service, modules []Module, code int) {
	a := e.Admin()

	locale, config, err := e.Locale(r)
	if err != nil {
		a.Fail(w, r, err)

		return
	}

	nodes, err := tree.Tree(r.Context(), locale)
	if err != nil {
		a.Fail(w, r, err)

		return
	}

	query := url.Values{"locale": {locale}}
	view := TreeView{
		Title:    "Pages",
		NewLabel: "New page",
		TrashURL: a.TrashURL("pages"),
		LinksURL: a.URL("admin.pages.links"),
	}

	if a.Can(r, e.Permission(admin.Create)) {
		view.NewURL = a.URL("admin.pages.new", query)
	}

	// The tree shows every page, so its tabs carry no translation status.
	view.Tabs = admin.LocaleTabs(config.Enabled, locale, a.URL("admin.pages.index"), nil)

	titles := map[string]string{}
	for _, module := range modules {
		titles[module.Key] = module.Title
	}

	search := admin.SearchOf(r)
	// Moving needs the whole tree in view: a search hides the arrows.
	canMove := a.Can(r, e.Permission(admin.Edit)) && !search.Active()
	spec := tree.Content().Spec()
	rows := map[string]TreeRow{}
	children := map[string][]string{}

	var roots []string

	for _, node := range nodes {
		page := node.Page
		row := TreeRow{
			Title:    title(page, locale),
			URL:      a.URL("admin.pages.edit", "id", page.ID, query),
			Path:     node.Path,
			Status:   editor.Status(page, true),
			Home:     isHome(page),
			Missing:  !spec.Available(page, locale),
			ID:       page.ID,
			ParentID: page.Fields.ParentID,
			Leaf:     page.Fields.Module != "",
		}

		if row.Title == "" {
			row.Title = a.T(r, "Untitled")
		}

		row.Match = search.Active() && search.Matches(row.Title, node.Path)

		if module := page.Fields.Module; module != "" {
			row.Module = titles[module]

			if row.Module == "" {
				row.Module = module
			}
		}

		if canMove && !row.Home {
			up := Move{Arrow: "↑", Label: "Move up"}
			down := Move{Arrow: "↓", Label: "Move down"}

			if !node.First {
				up.URL = a.URL("admin.pages.up", "id", page.ID, query)
			}

			if !node.Last {
				down.URL = a.URL("admin.pages.down", "id", page.ID, query)
			}

			row.Moves = []Move{up, down}
			row.DragURL = a.URL("admin.pages.move", "id", page.ID, query)
		}

		rows[page.ID] = row

		if _, ok := rows[page.Fields.ParentID]; ok && node.Depth > 0 {
			children[page.Fields.ParentID] = append(children[page.Fields.ParentID], page.ID)
		} else {
			roots = append(roots, page.ID)
		}
	}

	// nest builds the branch of id; with a search it keeps the matching
	// pages and the pages above them.
	var nest func(id string) (TreeRow, bool)

	nest = func(id string) (TreeRow, bool) {
		row := rows[id]
		keep := !search.Active() || row.Match

		for _, child := range children[id] {
			if branch, ok := nest(child); ok {
				row.Children = append(row.Children, branch)
				keep = true
			}
		}

		return row, keep
	}

	for _, id := range roots {
		if branch, ok := nest(id); ok {
			view.Pages = append(view.Pages, branch)
		}
	}

	a.Render(w, r, admin.Page{
		Title:    view.Title,
		Template: "templates/tree.gohtml",
		Files:    files,
		Data:     view,
		Status:   code,
	})
}

// title is the page's title in locale, or else in its first translation.
func title(page pages.Page, locale string) string {
	if text, ok := page.Text(locale); ok && text.Title != "" {
		return text.Title
	}

	for _, translation := range page.Translations {
		if translation.Text.Title != "" {
			return translation.Text.Title
		}
	}

	return ""
}

// move returns the handler that moves a page offset places among its
// siblings and returns to the overview.
func move(e pageEditor, tree *pages.Service, offset int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := e.Admin()

		locale, _, err := e.Locale(r)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		err = tree.Move(r.Context(), r.PathValue("id"), offset)
		if errors.Is(err, pages.ErrNotFound) {
			http.NotFound(w, r)

			return
		}

		if err != nil {
			a.Fail(w, r, err)

			return
		}

		a.Redirect(w, r, a.URL("admin.pages.index", url.Values{"locale": {locale}}))
	}
}
