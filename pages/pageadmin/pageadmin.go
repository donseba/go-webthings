// Package pageadmin is the Pages section of the admin: the page tree,
// where editors add, move and publish pages and place modules on them, and
// the editor of each page, built with package editor.
package pageadmin

import (
	"context"
	"embed"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/links"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/pages"

	form "github.com/donseba/go-form/v2"
)

// Module is a module instance that can be placed on a page.
type Module struct {
	Key   string
	Title string
}

//go:embed templates translations assets
var files embed.FS

type input struct {
	Title       string                    `form:"input,text" label:"Title"              required:"true" maxLength:"200"`
	Slug        string                    `form:"input,text" label:"Web address"                        maxLength:"190"    pattern:"^(?:[a-z0-9]+(?:-[a-z0-9]+)*)?$" patternMsg:"Use lowercase letters, digits and dashes for the web address." description:"The last part of the address, such as open-studio in /en/news/open-studio. Made from the title when left empty."`
	Description string                    `form:"textarea"   label:"Summary"                            maxLength:"1000"                                                                                                                        description:"A short text for lists, search results and search engines."`
	Body        string                    `form:"textarea"   label:"Content"                            maxLength:"262144"                                                                                                                                                                                                                                                    class:"markdown"`
	Parent      form.SortedSelect[string] `form:"dropdown"   label:"Parent page"                                                                                                                                                                description:"The page this page sits below, in the menu and in its address."`
	Module      form.SortedSelect[string] `form:"dropdown"   label:"Shows on this page"                                                                                                                                                         description:"A module, such as news or events, can show its content on this page. Such a page cannot have child pages."                        placeholder:"Nothing, a normal page" translate:"true"`
}

// New returns the Pages section of the admin: the page tree with an editor
// per page. accounts names the authors in the history, site links to
// previews, modules lists the module instances a page may show, and check
// knows the links to pages: the editor of a published page lists the
// content that links to it and offers redirects (see links.Check), and the
// report "admin.pages.links" lists the links that lead nowhere.
func New(
	tree *pages.Service,
	provider locales.Provider,
	accounts *auth.Service,
	site *frontend.Frontend,
	modules func() []Module,
	check *links.Check,
) admin.Section {
	linked := pageLinks{tree: tree, check: check}

	return editor.New(editor.Config[pages.Fields, pages.Text, input]{
		ID:       "pages",
		Title:    "Pages",
		Icon:     "pages",
		NewLabel: "New page",
		Store:    store{tree},
		Locales:  provider,
		Names:    accounts.Names,
		Files:    files,
		// The page tree's drag and drop (admin/assets/tree.js).
		Stylesheet: "assets/tree.css",
		Label: func(text pages.Text) string {
			return text.Title
		},
		Form: func(ctx context.Context, page pages.Page, locale string) (input, error) {
			return fill(ctx, tree, modules(), page, locale)
		},
		Apply:      apply,
		PreviewURL: site.PagePreviewURL,
		Issue:      issue,
		Overview: func(w http.ResponseWriter, r *http.Request, e editor.Editor[pages.Fields, pages.Text, input]) {
			overview(w, r, e, tree, modules(), 0)
		},
		Permanent: isHome,
		Edit:      linked.edit,
		Routes: func(routes *admin.Routes, e editor.Editor[pages.Fields, pages.Text, input]) {
			routes.Post("/{id}/up", admin.Edit, move(e, tree, -1)).As("up")
			routes.Post("/{id}/down", admin.Edit, move(e, tree, 1)).As("down")
			routes.Post("/{id}/move", admin.Edit, moveTo(e, tree, modules)).As("move")
			routes.Post("/{id}/unpublish-redirect", admin.Publish, linked.unpublishRedirect(e)).As("unpublish-redirect")
			routes.Post("/{id}/revisions/{revision}/publish-moved", admin.Publish, linked.publishMoved(e)).
				As("revision-publish-moved")
			routes.Get("/links", admin.View, linked.report(e)).As("links")
			routes.Post("/links/redirect", admin.View, linked.redirectAddress(e)).As("links-redirect")
		},
	})
}

// isHome reports whether page is the home page, the only page without a
// parent. It always stays published.
func isHome(page pages.Page) bool {
	return page.ID != "" && page.Fields.ParentID == ""
}

func fill(ctx context.Context, tree *pages.Service, modules []Module, page pages.Page, locale string) (input, error) {
	text, _ := page.Text(locale)
	in := input{
		Title:       text.Title,
		Slug:        text.Slug,
		Description: text.Description,
		Body:        text.Body,
	}

	nodes, err := tree.Tree(ctx, locale)
	if err != nil {
		return in, err
	}

	in.Parent = parents(ctx, nodes, page, locale)

	// Home cannot show a module. The modules are listed by their titles in
	// the admin language (translate:"true"), which go-form sorts as they
	// render.
	choices := map[string]string{}

	for _, module := range modules {
		if !isHome(page) {
			choices[module.Key] = module.Title
		}
	}

	in.Module = form.NewSortedSelect(choices).SortByLabel().Selected(page.Fields.Module)

	return in, nil
}

// parents lists the pages page can be placed below, in tree order and
// indented by depth: every page except module pages, the page itself and
// the pages below it. New pages start below Home; Home has no parent, which
// it shows as "None (top level)", translated.
func parents(ctx context.Context, nodes []pages.Node, page pages.Page, locale string) form.SortedSelect[string] {
	if isHome(page) {
		return form.NewOrderedSelect(form.Option[string]{Value: "", Label: admin.T(ctx, "None (top level)")}).Selected("")
	}

	var options []form.Option[string]

	selected := page.Fields.ParentID
	below := -1

	for _, node := range nodes {
		// The page's own subtree follows it with a greater depth.
		if below >= 0 && node.Depth > below {
			continue
		}

		below = -1

		if page.ID != "" && node.Page.ID == page.ID {
			below = node.Depth

			continue
		}

		if node.Depth == 0 && page.ID == "" {
			selected = node.Page.ID
		}

		if node.Page.Fields.Module != "" {
			continue
		}

		options = append(options, form.Option[string]{
			Value: node.Page.ID,
			Label: strings.Repeat("— ", node.Depth) + title(node.Page, locale),
		})
	}

	return form.NewOrderedSelect(options...).Selected(selected)
}

func apply(_ context.Context, in input, draft *content.Draft[pages.Fields, pages.Text], locale string) error {
	text := pages.Text{
		Title:       in.Title,
		Slug:        in.Slug,
		Description: in.Description,
		Body:        in.Body,
	}

	// The home page (an existing page without parent) keeps its place and has no slug.
	if draft.ID != "" && draft.Fields.ParentID == "" {
		text.Slug = ""
	} else {
		draft.Fields = pages.Fields{
			ParentID: in.Parent.Get(),
			Module:   in.Module.Get(),
			// The page keeps its place; the overview moves it.
			Position: draft.Fields.Position,
		}
	}

	draft.Translations = content.SetText(draft.Translations, locale, text)

	return nil
}

func issue(err error) string {
	switch {
	case errors.Is(err, pages.ErrCycle):
		return "A page cannot be placed below itself."
	case errors.Is(err, pages.ErrModuleParent):
		return "Pages with a module cannot have child pages."
	case errors.Is(err, pages.ErrDuplicate):
		return "Another page at this level already uses this web address, or the address is reserved."
	case errors.Is(err, pages.ErrHasChildren):
		return "Move or delete the child pages first."
	case errors.Is(err, pages.ErrParentDeleted):
		return "This page cannot be restored while its parent page is in the trash. Restore the parent first."
	case errors.Is(err, pages.ErrMounted):
		return "This module is already placed on another page."
	case errors.Is(err, pages.ErrUnknownMount):
		return "This module is not available."
	case errors.Is(err, pages.ErrHome), errors.Is(err, editor.ErrPermanent):
		return "The home page always stays on the website: it cannot be unpublished, scheduled to be unpublished or deleted."
	case errors.Is(err, pages.ErrSlug):
		return "Give the page a web address of lowercase letters, digits and dashes, or a title it can be made from."
	case errors.Is(err, pages.ErrParent):
		return "Choose a parent page."
	case errors.Is(err, pages.ErrInvalid):
		return "Some fields are not valid."
	}

	return ""
}

// store is the page tree as the editor's store: the page service keeps the
// rules of the tree, and its review workflow is the tree's (see
// pages.Service.Reviews).
type store struct {
	tree *pages.Service
}

func (s store) ReviewRequired(ctx context.Context) bool {
	return s.tree.Reviews().ReviewRequired(ctx)
}

func (s store) Submit(ctx context.Context, id, revision string) error {
	return s.tree.Reviews().Submit(ctx, id, revision)
}

func (s store) Approve(ctx context.Context, id, revision, note string) error {
	return s.tree.Reviews().Approve(ctx, id, revision, note)
}

func (s store) Reject(ctx context.Context, id, revision, note string) error {
	return s.tree.Reviews().Reject(ctx, id, revision, note)
}

func (s store) InReview(ctx context.Context, limit int) ([]pages.Page, error) {
	return s.tree.Reviews().InReview(ctx, limit)
}

func (s store) Spec() content.Spec[pages.Fields, pages.Text] {
	return s.tree.Content().Spec()
}

func (s store) List(ctx context.Context) ([]pages.Page, error) {
	return s.tree.ListLatest(ctx)
}

func (s store) Load(ctx context.Context, id, revision string) (pages.Page, error) {
	if revision != "" {
		return s.tree.Revision(ctx, id, revision)
	}

	return s.tree.Latest(ctx, id)
}

func (s store) Save(ctx context.Context, draft pages.Draft) (pages.Page, error) {
	return s.tree.Save(ctx, draft)
}

func (s store) Delete(ctx context.Context, id string) error {
	return s.tree.Delete(ctx, id)
}

func (s store) Trashed(ctx context.Context) ([]pages.Page, error) {
	return s.tree.Trashed(ctx)
}

func (s store) Undelete(ctx context.Context, id string) error {
	return s.tree.Undelete(ctx, id)
}

func (s store) Purge(ctx context.Context, id string) error {
	return s.tree.Purge(ctx, id)
}

func (s store) Recent(ctx context.Context, limit int) ([]pages.Page, error) {
	return s.tree.Recent(ctx, limit)
}

func (s store) Planned(ctx context.Context) ([]pages.Page, error) {
	return s.tree.Planned(ctx)
}

func (s store) Publish(ctx context.Context, id, revision, _ string) error {
	return s.tree.Publish(ctx, id, revision)
}

func (s store) Unpublish(ctx context.Context, id string) error {
	return s.tree.Unpublish(ctx, id)
}

func (s store) Revisioned() bool {
	return true
}

func (s store) History(ctx context.Context, id string) ([]content.Revision, error) {
	return s.tree.History(ctx, id)
}

func (s store) Restore(ctx context.Context, id, revision, author string) (pages.Page, error) {
	return s.tree.Restore(ctx, id, revision, author)
}

func (s store) Schedule(ctx context.Context, id string, schedule content.Schedule, _ string) error {
	return s.tree.Schedule(ctx, id, schedule)
}

func (s store) Unschedule(ctx context.Context, id string) error {
	return s.tree.Unschedule(ctx, id)
}

func (s store) RunDue(ctx context.Context, now time.Time, required string) (int, error) {
	return s.tree.RunDue(ctx, now, required)
}
