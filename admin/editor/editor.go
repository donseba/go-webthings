// Package editor is the ready-made admin for content stores (see the
// content package): an overview with language tabs, a go-form editor per
// language, publication and, for revisioned content, history, restore and
// preview. A module supplies a form struct and two mapping functions; extra
// screens can be added with Config.Routes.
package editor

import (
	"cmp"
	"context"
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"net/url"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/locales"

	form "github.com/donseba/go-form/v2"
	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/exp/flash"
)

//go:embed templates
var files embed.FS

// Config describes one editable content type. I is a go-form input struct for
// one language of an item.
type Config[F, T, I any] struct {
	// ID, Title, Path and Icon describe the admin section; see admin.Section.
	// Path is below the admin, such as "/news"; empty is "/" followed by ID.
	ID    string
	Title string
	Path  string
	Icon  string
	// NewLabel is the "new item" button text, for example "New post".
	NewLabel string
	// Links are extra buttons on the overview, for example to categories.
	Links []Link
	// Permission is the section whose rights apply, when shared with another
	// section (see admin.Section.Permission). Empty means ID.
	Permission string
	// Single allows one item, for modules where an instance is one thing (a
	// form). "New" disappears once it exists.
	Single bool

	// Store keeps the items, bound to the instance (see
	// content.Revisions.In and content.Collection.In).
	Store content.Store[F, T]
	// Locales gives the languages the editor offers tabs for.
	Locales locales.Provider

	// Form fills the input for one language of an item; item is empty for new content.
	Form func(ctx context.Context, item content.Item[F, T], locale string) (I, error)
	// Apply writes a validated submission into the draft for one language.
	Apply func(ctx context.Context, input I, draft *content.Draft[F, T], locale string) error
	// Label names an item in lists, from its text in one language.
	Label func(text T) string
	// ItemLabel names an item from the whole item, for content named by its
	// Fields (a person's name, a social network). It is used instead of Label
	// when set.
	ItemLabel func(item content.Item[F, T], locale string) string
	// Notes are shown muted after the labels in the overview, such as a
	// download count or "Expired", by item ID; a missing or empty note shows
	// nothing. It is called once per overview with all listed items, so a
	// module can read what it needs in one query. Notes are shown as written:
	// translate fixed texts with admin.T and admin.TN, which use ctx.
	Notes func(ctx context.Context, items []content.Item[F, T], locale string) map[string]string

	// PreviewURL is the address at which an editor previews a revision on
	// the website, in locale: its public address with
	// frontend.PreviewParam (see frontend.PagePreviewURL and
	// ModulePreviewURL); empty when it has none yet, for example while the
	// module is on no page. Nil offers no preview.
	PreviewURL func(ctx context.Context, item content.Item[F, T], locale string) string
	// Names maps user IDs to names for the revision history.
	Names func(ctx context.Context) (map[string]string, error)
	// Issue turns module errors of saving, publishing and deleting into
	// messages for the editor; empty means unknown. A refused delete because
	// the item is still referenced (a foreign key) defaults to "This item is
	// still in use.".
	Issue func(err error) string
	// Badge is a count next to the section in the navigation; see
	// admin.Section.Badge.
	Badge func(ctx context.Context) int
	// Stylesheet names a CSS file in Files that the admin links; see
	// admin.Section.Stylesheet.
	Stylesheet string

	// Files can override the editor templates and add module templates.
	Files fs.FS
	// Overview replaces the default overview, for content with a structure
	// of its own such as the page tree. Nil uses the default list.
	Overview func(w http.ResponseWriter, r *http.Request, editor Editor[F, T, I])
	// Permanent reports items that must stay on the website by their
	// nature, such as the home page: their editor offers no unpublish, no
	// planned unpublication and no delete, refuses them when they are
	// posted anyway, and offers no way to remove the mark. Administrators
	// can mark other items permanent, and remove that mark again, in the
	// item's editor when the admin keeps marks (see admin.Permanence).
	Permanent func(item content.Item[F, T]) bool
	// Routes adds module screens to the section.
	Routes func(routes *admin.Routes, editor Editor[F, T, I])
	// Edit adds to the edit screen of an item that exists, once the editor
	// has built it: it may add Notices, such as the pages that link to the
	// item, and change or replace the forms of view, such as with a
	// question before the item is unpublished (admin.Admin.Confirm) or a
	// publish form that posts to a route of its own (Config.Routes). Nil
	// adds nothing.
	Edit func(r *http.Request, editor Editor[F, T, I], item content.Item[F, T], locale string, view *EditView) error
	// Saved follows up the save of an item that existed, with the item
	// before and after: it may act on the change, such as redirecting the
	// item's old address (Linked.Saved), and returns the toast to show,
	// nil for the editor's own. Nil does nothing.
	Saved func(r *http.Request, editor Editor[F, T, I], before, after content.Item[F, T], locale string) *flash.Message
	// Trash adds the module's own deleted records to the section's trash,
	// such as a form's submissions, next to the deleted items of Store.
	Trash admin.Trash
}

// Editor is the editor of one content type inside the admin, handed to
// Config.Routes so a module's own screens can link to its items and check
// its permissions.
type Editor[F, T, I any] struct {
	config Config[F, T, I]
	admin  *admin.Admin
	// templates are config.Files over the editor's own.
	templates fs.FS
}

// New returns the admin section for a content type.
func New[F, T, I any](config Config[F, T, I]) admin.Section {
	templates := partial.Overlay(config.Files, files)

	section := admin.Section{
		ID:         config.ID,
		Permission: config.Permission,
		Title:      config.Title,
		Path:       config.Path,
		Icon:       config.Icon,
		Badge:      config.Badge,
		Files:      templates,
		Stylesheet: config.Stylesheet,
		Recent:     Editor[F, T, I]{config: config}.recent,
		Trash:      trash(Editor[F, T, I]{config: config}),
		Routes: func(routes *admin.Routes) {
			e := Editor[F, T, I]{config: config, admin: routes.Admin(), templates: templates}

			overview := e.index
			if config.Overview != nil {
				overview = func(w http.ResponseWriter, r *http.Request) {
					config.Overview(w, r, e)
				}
			}

			// The routes are named "admin.<ID>.<name>", such as
			// "admin.news.edit" (see admin.Section.RouteName).
			routes.Get("", admin.View, overview).As("index")
			routes.Get("/new", admin.Create, e.edit).As("new")
			routes.Post("/new", admin.Create, e.save)
			routes.Get("/{id}", admin.View, e.edit).As("edit")
			routes.Post("/{id}", admin.Edit, e.save)
			routes.Post("/{id}/delete", admin.Delete, e.delete).As("delete")
			routes.Post("/{id}/publish", admin.Publish, e.publish).As("publish")
			routes.Post("/{id}/unpublish", admin.Publish, e.unpublish).As("unpublish")
			routes.Post("/{id}/revisions/{revision}/publish", admin.Publish, e.publish).As("revision-publish")
			routes.Post("/{id}/revisions/{revision}/restore", admin.Edit, e.restore).As("revision-restore")
			routes.Get("/{id}/revisions/{revision}/preview", admin.View, e.preview).As("revision-preview")

			// Only administrators mark items permanent or remove the mark.
			routes.Post("/{id}/permanent", admin.Edit, e.setPermanent(true)).
				As("permanent").
				Use(admin.AdministratorsOnly)
			routes.Post("/{id}/permanent/off", admin.Edit, e.setPermanent(false)).
				As("permanent-off").
				Use(admin.AdministratorsOnly)

			if _, ok := config.Store.(content.Reviewing[F, T]); ok {
				routes.Post("/{id}/revisions/{revision}/submit", admin.Edit, e.submit).As("revision-submit")
				routes.Post("/{id}/revisions/{revision}/approve", admin.Review, e.approve).As("revision-approve")
				routes.Post("/{id}/revisions/{revision}/reject", admin.Review, e.reject).As("revision-reject")
			}

			if _, ok := config.Store.(content.Scheduling); ok {
				routes.Post("/{id}/revisions/{revision}/schedule", admin.Publish, e.schedule).As("revision-schedule")
				routes.Post("/{id}/unschedule", admin.Publish, e.unschedule).As("unschedule")
			}

			if _, ok := config.Store.(content.Mover); ok {
				routes.Post("/{id}/up", admin.Edit, e.move(-1)).As("up")
				routes.Post("/{id}/down", admin.Edit, e.move(1)).As("down")
			}

			if config.Routes != nil {
				config.Routes(routes, e)
			}
		},
	}

	if config.Trash != nil {
		section.Trash = admin.JoinTrash(section.Trash, config.Trash)
	}

	// A single item, such as a form, is made once from its overview.
	if !config.Single && config.NewLabel != "" {
		section.NewLabel = config.NewLabel
		section.NewRoute = "new"
	}

	if _, ok := config.Store.(content.Scheduling); ok {
		section.Scheduled = Editor[F, T, I]{config: config}.planned
	}

	if _, ok := config.Store.(content.Reviewing[F, T]); ok {
		section.Review = Editor[F, T, I]{config: config}.inReview
	}

	return section
}

// Admin returns the admin the editor belongs to, for module screens.
func (e Editor[F, T, I]) Admin() *admin.Admin {
	return e.admin
}

// Permission returns the permission for an action in the section whose
// rights apply to the editor.
func (e Editor[F, T, I]) Permission(action string) admin.Permission {
	return e.permission(action)
}

// permission is the section whose rights apply.
func (e Editor[F, T, I]) permission(action string) admin.Permission {
	section := e.config.Permission
	if section == "" {
		section = e.config.ID
	}

	return admin.Permission{Section: section, Action: action}
}

// Locale returns the content language of the request (?locale=), defaulting to
// the site's default language, and the language configuration.
func (e Editor[F, T, I]) Locale(r *http.Request) (string, locales.Config, error) {
	config, err := e.config.Locales.Configuration(r.Context())
	if err != nil {
		return "", config, err
	}

	return admin.ContentLocale(r, config), config, nil
}

// route is the full name of one of the editor's routes, such as
// "admin.news.edit" for "edit".
func (e Editor[F, T, I]) route(name string) string {
	return admin.RouteName(e.config.ID, name)
}

// itemRoute builds the address of the item's editor routes by their short
// name, for the revision panel (see admin.Revisions.URL).
func (e Editor[F, T, I]) itemRoute(id string) func(name string, args ...any) string {
	return func(name string, args ...any) string {
		args = append([]any{"id", id}, args...)

		return e.admin.URL(e.route(name), args...)
	}
}

// itemURL is the editor of an item in locale ("edit"), or of a new item
// ("new"). The dashboard's lists use it too, with the context of the
// dashboard's request (see admin.RouteURL).
func (e Editor[F, T, I]) itemURL(ctx context.Context, id, locale string) string {
	query := url.Values{"locale": {locale}}
	if id == "" {
		return admin.RouteURL(ctx, e.route("new"), query)
	}

	return admin.RouteURL(ctx, e.route("edit"), "id", id, query)
}

// revisionURL is the editor of one revision of an item in locale.
func (e Editor[F, T, I]) revisionURL(ctx context.Context, id, locale, revision string) string {
	return admin.RouteURL(ctx, e.route("edit"), "id", id, url.Values{"locale": {locale}, "revision": {revision}})
}

// Link is a button on the overview: to URL, or to the admin route named
// Route, such as admin.RouteName("news-categories", "index"), whose address
// is built when the overview is shown. Route is used instead of URL when set;
// an unknown route fails the overview.
type Link struct {
	Label string
	URL   string
	Route string
}

// links are the overview's buttons, with the addresses of their routes.
func (e Editor[F, T, I]) links() []Link {
	links := make([]Link, len(e.config.Links))

	for i, link := range e.config.Links {
		if link.Route != "" {
			link.URL = e.admin.URL(link.Route)
		}

		links[i] = link
	}

	return links
}

// IndexView is the data of the overview of a content type: one row per
// item, in one language.
type IndexView struct {
	Title    string
	NewURL   string
	NewLabel string
	Links    []Link
	// TrashURL shows the section's deleted items in the admin's trash.
	TrashURL   string
	Revisioned bool
	Tabs       []admin.LocaleTab
	Rows       []Row
	// Pager pages through long lists.
	Pager admin.Pager
}

// Row is one item in the overview, with its publication state (see
// Status).
type Row struct {
	Title string
	// Note is shown muted after the title; see Config.Notes.
	Note    string
	URL     string
	Status  string
	Missing bool
	// Moves are the item's up and down arrows, for ordered stores; see
	// admin.Moves.
	Moves []admin.Move
}

func (e Editor[F, T, I]) index(w http.ResponseWriter, r *http.Request) {
	locale, config, err := e.Locale(r)
	if err != nil {
		e.admin.Fail(w, r, err)

		return
	}

	items, err := e.config.Store.List(r.Context())
	if err != nil {
		e.admin.Fail(w, r, err)

		return
	}

	view := IndexView{
		Title:      e.config.Title,
		NewURL:     e.itemURL(r.Context(), "", locale),
		NewLabel:   e.config.NewLabel,
		Links:      e.links(),
		TrashURL:   e.admin.TrashURL(e.config.ID),
		Revisioned: e.config.Store.Revisioned(),
	}

	// The overview shows every item, so its tabs carry no translation status.
	view.Tabs = admin.LocaleTabs(config.Enabled, locale, e.admin.URL(e.route("index")), nil)

	spec := e.config.Store.Spec()
	_, ordered := e.config.Store.(content.Mover)
	search := admin.SearchOf(r)
	// Moving needs the whole list in view: a search hides the arrows.
	canMove := ordered && e.admin.Can(r, e.permission(admin.Edit)) && !search.Active()

	if e.config.Single && len(items) > 0 || !e.admin.Can(r, e.permission(admin.Create)) {
		view.NewURL = ""
	}

	var notes map[string]string
	if e.config.Notes != nil {
		notes = e.config.Notes(r.Context(), items, locale)
	}

	for i, item := range items {
		row := Row{
			Title:   e.label(item, locale),
			URL:     e.itemURL(r.Context(), item.ID, locale),
			Status:  Status(item, e.config.Store.Revisioned()),
			Missing: !spec.Available(item, locale),
			Note:    notes[item.ID],
		}

		if row.Title == "" {
			row.Title = e.admin.T(r, "Untitled")
		}

		if !search.Matches(row.Title, row.Note) {
			continue
		}

		if canMove {
			var upURL, downURL string

			if i > 0 {
				upURL = e.admin.URL(e.route("up"), "id", item.ID, url.Values{"locale": {locale}})
			}

			if i < len(items)-1 {
				downURL = e.admin.URL(e.route("down"), "id", item.ID, url.Values{"locale": {locale}})
			}

			if len(items) > 1 {
				row.Moves = admin.Moves(upURL, downURL)
			}
		}

		view.Rows = append(view.Rows, row)
	}

	// The rows are read whole, as ordering needs the neighbours of every
	// item, and paged in memory.
	page := admin.PageNumber(r)
	rows, more := admin.Paginate(view.Rows, page, admin.DefaultPageSize)
	view.Rows = rows
	view.Pager = admin.NewPager(r, e.admin.URL(e.route("index")), page, more)

	e.admin.Render(w, r, admin.Page{
		Title:    e.config.Title,
		Template: "templates/index.gohtml",
		Files:    e.templates,
		Data:     view,
	})
}

// label names an item in one language: ItemLabel when set, otherwise Label
// on the item's text in that language.
func (e Editor[F, T, I]) label(item content.Item[F, T], locale string) string {
	if e.config.ItemLabel != nil {
		return e.config.ItemLabel(item, locale)
	}

	if e.config.Label == nil {
		return ""
	}

	text, _ := item.Text(locale)

	return e.config.Label(text)
}

// Status names the publication state of an item for overviews, as a
// translation key: "Scheduled", "In review", "Draft", "Changes not
// published" or "Published". "In review" and "Changes not published" apply
// to revisioned content only; all content can be "Scheduled", "Draft" or
// "Published".
func Status[F, T any](item content.Item[F, T], revisioned bool) string {
	switch {
	case !item.Schedule.PublishAt.IsZero():
		return "Scheduled"
	case revisioned && item.Revision.Review.State == content.ReviewPending:
		return "In review"
	case !item.Published:
		return "Draft"
	case revisioned && item.PublishedNumber != item.Revision.Number:
		return "Changes not published"
	default:
		return "Published"
	}
}

// EditView is the data of the edit screen of one item in one language: the
// form, its buttons and, for revisioned content, the revision panel.
type EditView struct {
	// Title is the item's label, or for a new item the translated "new"
	// label of the section; it is shown as written.
	Title   string
	BackURL string
	// BackLabel names the overview BackURL leads to, the section's title,
	// as a translation key.
	BackLabel string
	Tabs      []admin.LocaleTab
	Panel     *admin.RevisionPanel
	Publish   *form.RenderModel
	Unpublish *form.RenderModel
	Form      form.RenderModel
	Errors    form.FieldErrors
	// ReadOnly is set for users who may view the item but not edit it: Form
	// is then read-only (disabled, without its button).
	ReadOnly bool
	Delete   *form.RenderModel
	// Permanent items stay on the website: they cannot be unpublished,
	// scheduled to be unpublished or deleted. PermanentFixed items are
	// permanent by their nature (see Config.Permanent); the mark of others
	// was set by an administrator.
	Permanent      bool
	PermanentFixed bool
	// Permanence sets or removes the permanent mark, for administrators
	// when the admin keeps marks; nil otherwise.
	Permanence *form.RenderModel
	// Notices are warnings shown above the form (see Config.Edit).
	Notices []Notice
}

// Notice is a warning on the edit screen (see Config.Edit): a text, the
// content it concerns and forms that act on it.
type Notice struct {
	// Text is shown as written: translate it first (admin.T, admin.TN).
	Text string
	// Items are listed below the text, such as the pages that link to the
	// item.
	Items []NoticeItem
	// Forms act on the notice, such as one that creates a redirect.
	Forms []form.RenderModel
}

// NoticeItem is an entry in a Notice's list, linked to URL when set. Label
// and Note, which is shown muted after it, are shown as written.
type NoticeItem struct {
	Label string
	URL   string
	Note  string
}

func (e Editor[F, T, I]) edit(w http.ResponseWriter, r *http.Request) {
	locale, _, err := e.Locale(r)
	if err != nil {
		e.admin.Fail(w, r, err)

		return
	}

	if r.PathValue("id") == "" && e.redirectSingle(w, r, locale) {
		return
	}

	item, err := e.load(r)
	if errors.Is(err, content.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		e.admin.Fail(w, r, err)

		return
	}

	input, err := e.config.Form(r.Context(), item, locale)
	if err != nil {
		e.admin.Fail(w, r, err)

		return
	}

	e.render(w, r, item, input, nil, "", http.StatusOK)
}

// redirectSingle sends "new" to the existing item of a Single editor.
func (e Editor[F, T, I]) redirectSingle(w http.ResponseWriter, r *http.Request, locale string) bool {
	if !e.config.Single {
		return false
	}

	items, err := e.config.Store.List(r.Context())
	if err != nil || len(items) == 0 {
		return false
	}

	http.Redirect(w, r, e.itemURL(r.Context(), items[0].ID, locale), http.StatusSeeOther)

	return true
}

// load returns the item in the URL (latest, or ?revision=), or an empty item for /new.
func (e Editor[F, T, I]) load(r *http.Request) (content.Item[F, T], error) {
	id := r.PathValue("id")
	if id == "" {
		return content.Item[F, T]{}, nil
	}

	revision := r.PathValue("revision")
	if revision == "" {
		revision = r.URL.Query().Get("revision")
	}

	return e.config.Store.Load(r.Context(), id, revision)
}

func (e Editor[F, T, I]) render(
	w http.ResponseWriter,
	r *http.Request,
	item content.Item[F, T],
	input I,
	errs form.FieldErrors,
	message string,
	status int,
) {
	locale, config, err := e.Locale(r)
	if err != nil {
		e.admin.Fail(w, r, err)

		return
	}

	// The message shows as a toast; errors of single fields stay inline.
	r = admin.WithError(r, message)

	view := EditView{
		Title:     e.title(r, item, locale),
		BackURL:   e.admin.URL(e.route("index"), url.Values{"locale": {locale}}),
		BackLabel: e.config.Title,
		Form:      e.admin.Form(r, input, e.itemURL(r.Context(), item.ID, locale), e.saveLabel()),
		Errors:    errs,
		// New items need Create to be shown at all; saving an existing one
		// needs Edit.
		ReadOnly: item.ID != "" && !e.admin.Can(r, e.permission(admin.Edit)),
	}

	// A read-only form shows its values disabled and without its button.
	view.Form.Info.ReadOnly = view.ReadOnly

	spec := e.config.Store.Spec()

	// The tabs keep the revision being looked at.
	tabURL := e.itemURL(r.Context(), item.ID, locale)
	if revision := r.URL.Query().Get("revision"); revision != "" {
		tabURL = e.revisionURL(r.Context(), item.ID, locale, revision)
	}

	view.Tabs = admin.LocaleTabs(config.Enabled, locale, tabURL, func(code string) (bool, bool) {
		_, started := item.Text(code)

		return spec.Available(item, code), started
	})

	if item.ID != "" {
		if err := e.publication(r, item, locale, &view); err != nil {
			e.admin.Fail(w, r, err)

			return
		}
	}

	if item.ID != "" && e.config.Edit != nil {
		if err := e.config.Edit(r, e, item, locale, &view); err != nil {
			e.admin.Fail(w, r, err)

			return
		}
	}

	// The title is a label editors typed, or translated already.
	e.admin.Render(w, r, admin.Page{
		Title:    e.config.Title,
		Label:    view.Title,
		Template: "templates/edit.gohtml",
		Files:    e.templates,
		Data:     view,
		Status:   status,
	})
}

// title is the editor's heading: the item's label, "Untitled" for an
// item without one, and the section's "new" label (or else its title),
// translated, for a new item.
func (e Editor[F, T, I]) title(r *http.Request, item content.Item[F, T], locale string) string {
	if item.ID == "" {
		return e.admin.T(r, cmp.Or(e.config.NewLabel, e.config.Title))
	}

	if label := e.label(item, locale); label != "" {
		return label
	}

	return e.admin.T(r, "Untitled")
}

// publication adds the revision panel or the publish toggle, delete and
// the permanent mark.
func (e Editor[F, T, I]) publication(r *http.Request, item content.Item[F, T], locale string, view *EditView) error {
	locales := url.Values{"locale": {locale}}
	section := e.permission(admin.View).Section

	permanent, fixed, err := e.permanence(r.Context(), item)
	if err != nil {
		return err
	}

	view.Permanent, view.PermanentFixed = permanent, fixed
	e.permanenceForm(r, item, locale, view)

	if !permanent && e.admin.Can(r, e.permission(admin.Delete)) {
		remove := e.admin.Form(r, struct{}{}, e.admin.URL(e.route("delete"), "id", item.ID, locales), "Move to trash")
		view.Delete = &remove
	}

	if !e.config.Store.Revisioned() {
		if !e.admin.Can(r, e.permission(admin.Publish)) {
			return nil
		}

		if item.Published && permanent {
			return nil
		}

		if item.Published {
			unpublish := e.admin.Form(r, struct{}{}, e.admin.URL(e.route("unpublish"), "id", item.ID, locales), "Unpublish")
			view.Unpublish = &unpublish
		} else {
			publish := e.admin.Form(r, struct{}{}, e.admin.URL(e.route("publish"), "id", item.ID, locales), "Publish")
			view.Publish = &publish
		}

		return nil
	}

	history, err := e.config.Store.History(r.Context(), item.ID)
	if err != nil {
		return err
	}

	names := map[string]string{}
	if e.config.Names != nil {
		if names, err = e.config.Names(r.Context()); err != nil {
			return err
		}
	}

	latest := 0
	if len(history) > 0 {
		latest = history[0].Number
	}

	revisions := admin.Revisions{
		Section:     section,
		URL:         e.itemRoute(item.ID),
		Locale:      locale,
		Selected:    item.Revision,
		Latest:      latest,
		PublishedID: item.PublishedID,
		Permanent:   permanent,
		History:     history,
		Names:       names,
		Preview:     e.config.PreviewURL != nil,
		Review:      e.reviewing(r.Context()),
	}

	if _, ok := e.config.Store.(content.Scheduling); ok {
		schedule := item.Schedule
		revisions.Schedule = &schedule
	}

	panel := e.admin.RevisionPanel(r, revisions)
	view.Panel = &panel

	return nil
}

func (e Editor[F, T, I]) save(w http.ResponseWriter, r *http.Request) {
	locale, _, err := e.Locale(r)
	if err != nil {
		e.admin.Fail(w, r, err)

		return
	}

	// Edits always build on the latest state, also when an older revision is shown.
	var base content.Item[F, T]

	if id := r.PathValue("id"); id != "" {
		base, err = e.config.Store.Load(r.Context(), id, "")
		if err != nil {
			e.fail(w, r, err)

			return
		}
	}

	input, err := e.config.Form(r.Context(), base, locale)
	if err != nil {
		e.admin.Fail(w, r, err)

		return
	}

	errs, err := e.admin.Bind(r, &input)
	if err != nil {
		e.admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		e.render(w, r, base, input, errs, "", http.StatusUnprocessableEntity)

		return
	}

	user, _ := auth.CurrentUser(r.Context())
	draft := content.Draft[F, T]{
		ID:           base.ID,
		Fields:       base.Fields,
		Translations: base.Translations,
		Author:       user.ID,
	}

	if err := e.config.Apply(r.Context(), input, &draft, locale); err != nil {
		e.invalid(w, r, base, input, err)

		return
	}

	saved, err := e.config.Store.Save(r.Context(), draft)
	if err != nil {
		e.invalid(w, r, base, input, err)

		return
	}

	target := e.itemURL(r.Context(), saved.ID, locale)

	if base.ID != "" && e.config.Saved != nil {
		if message := e.config.Saved(r, e, base, saved, locale); message != nil {
			e.admin.RedirectFlash(w, r, target, *message)

			return
		}
	}

	// Content with revisions saved a draft, which the website shows only
	// once it is published.
	if e.config.Store.Revisioned() {
		e.admin.RedirectNotice(w, r, target, draftSaved)

		return
	}

	e.admin.Redirect(w, r, target)
}

// draftSaved is the toast of a save of content with revisions.
const draftSaved = "Saved as a draft. Publish it to show it on the website."

// saveLabel is the button of the edit form: "Save draft" for content with
// revisions, where saving does not change the website, else "Save".
func (e Editor[F, T, I]) saveLabel() string {
	if e.config.Store.Revisioned() {
		return "Save draft"
	}

	return "Save"
}

// invalid re-renders the editor for expected errors and fails otherwise.
func (e Editor[F, T, I]) invalid(w http.ResponseWriter, r *http.Request, item content.Item[F, T], input I, err error) {
	e.explain(w, r, item, input, err, e.Issue(r, err))
}

// explain re-renders the editor with message, or fails without one.
func (e Editor[F, T, I]) explain(
	w http.ResponseWriter,
	r *http.Request,
	item content.Item[F, T],
	input I,
	err error,
	message string,
) {
	if message == "" {
		e.admin.Fail(w, r, err)

		return
	}

	e.render(w, r, item, input, nil, message, http.StatusUnprocessableEntity)
}

// refuse shows the editor of the item in the URL with the reason a change
// was refused; unexpected errors fail.
func (e Editor[F, T, I]) refuse(w http.ResponseWriter, r *http.Request, err error, message string) {
	locale, _, localeErr := e.Locale(r)
	if localeErr != nil {
		e.admin.Fail(w, r, localeErr)

		return
	}

	item, loadErr := e.load(r)
	if loadErr != nil {
		e.fail(w, r, loadErr)

		return
	}

	input, formErr := e.config.Form(r.Context(), item, locale)
	if formErr != nil {
		e.admin.Fail(w, r, formErr)

		return
	}

	e.explain(w, r, item, input, err, message)
}

// Issue is the message for editors of an error of saving, publishing or
// deleting an item, translated: Config.Issue's, or the editor's own for the
// errors of the content package; empty for an unexpected error. Module
// screens (Config.Routes) explain refusals with it.
func (e Editor[F, T, I]) Issue(r *http.Request, err error) string {
	if e.config.Issue != nil {
		if message := e.config.Issue(err); message != "" {
			return e.admin.T(r, message)
		}
	}

	switch {
	case errors.Is(err, content.ErrDuplicateSlug):
		return e.admin.T(r, "This web address is already used.")
	case errors.Is(err, content.ErrIncomplete):
		return e.admin.T(r, "Complete the default language before publishing.")
	case errors.Is(err, content.ErrMediaMissing):
		return e.admin.T(r, "A referenced media file no longer exists.")
	case errors.Is(err, content.ErrInvalid):
		return e.admin.T(r, "Some fields are not valid.")
	case errors.Is(err, content.ErrTooLong):
		return e.admin.T(r, "The text is too long.")
	case errors.Is(err, content.ErrSchedule):
		return e.admin.T(r, "Choose when to publish or unpublish; unpublishing must come after publishing.")
	case errors.Is(err, content.ErrNotApproved):
		return e.admin.T(r, "This revision needs a reviewer's approval before it can be published.")
	case errors.Is(err, content.ErrReviewState):
		return e.admin.T(r, "This revision is not ready for that review step.")
	}

	return ""
}

// action runs a publication change and returns to the editor.
func (e Editor[F, T, I]) action(w http.ResponseWriter, r *http.Request, change func(locale string) error) {
	e.actionNotice(w, r, change, "")
}

// actionNotice is action with an explicit notice; empty uses the default
// notice of the action.
func (e Editor[F, T, I]) actionNotice(w http.ResponseWriter, r *http.Request, change func(locale string) error, notice string) {
	locale, config, err := e.Locale(r)
	if err != nil {
		e.admin.Fail(w, r, err)

		return
	}

	if err := change(config.Default); err != nil {
		e.refuse(w, r, err, e.Issue(r, err))

		return
	}

	target := e.itemURL(r.Context(), r.PathValue("id"), locale)
	if notice != "" {
		e.admin.RedirectNotice(w, r, target, notice)

		return
	}

	e.admin.Redirect(w, r, target)
}

// scheduled reports whether the item in the URL has a planned publication
// change, which publishing or unpublishing by hand cancels.
func (e Editor[F, T, I]) scheduled(r *http.Request) bool {
	if _, ok := e.config.Store.(content.Scheduling); !ok {
		return false
	}

	item, err := e.config.Store.Load(r.Context(), r.PathValue("id"), "")

	return err == nil && !item.Schedule.IsZero()
}

func (e Editor[F, T, I]) publish(w http.ResponseWriter, r *http.Request) {
	notice := ""
	if e.scheduled(r) {
		notice = "Published. The schedule was cancelled."
	}

	e.actionNotice(w, r, func(required string) error {
		return e.config.Store.Publish(r.Context(), r.PathValue("id"), r.PathValue("revision"), required)
	}, notice)
}

func (e Editor[F, T, I]) unpublish(w http.ResponseWriter, r *http.Request) {
	if e.RefusePermanent(w, r) {
		return
	}

	notice := ""
	if e.scheduled(r) {
		notice = "Unpublished. The schedule was cancelled."
	}

	e.actionNotice(w, r, func(string) error {
		return e.config.Store.Unpublish(r.Context(), r.PathValue("id"))
	}, notice)
}

// schedule plans the publication of the revision in the URL and/or the
// unpublication of the item, at times given in the site's timezone.
func (e Editor[F, T, I]) schedule(w http.ResponseWriter, r *http.Request) {
	scheduling, _ := e.config.Store.(content.Scheduling)

	// Publishing a permanent item later is fine; taking it offline is not.
	_, unpublishAt, err := e.admin.ParseSchedule(r)
	if err == nil && !unpublishAt.IsZero() && e.RefusePermanent(w, r) {
		return
	}

	e.action(w, r, func(required string) error {
		publishAt, unpublishAt, err := e.admin.ParseSchedule(r)
		if err != nil {
			return err
		}

		plan := content.Schedule{PublishAt: publishAt, UnpublishAt: unpublishAt}
		if !publishAt.IsZero() {
			plan.RevisionID = r.PathValue("revision")
		}

		return scheduling.Schedule(r.Context(), r.PathValue("id"), plan, required)
	})
}

func (e Editor[F, T, I]) unschedule(w http.ResponseWriter, r *http.Request) {
	scheduling, _ := e.config.Store.(content.Scheduling)

	e.action(w, r, func(string) error {
		return scheduling.Unschedule(r.Context(), r.PathValue("id"))
	})
}

func (e Editor[F, T, I]) restore(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r.Context())

	e.action(w, r, func(string) error {
		_, err := e.config.Store.Restore(r.Context(), r.PathValue("id"), r.PathValue("revision"), user.ID)

		return err
	})
}

func (e Editor[F, T, I]) delete(w http.ResponseWriter, r *http.Request) {
	locale, _, err := e.Locale(r)
	if err != nil {
		e.admin.Fail(w, r, err)

		return
	}

	if e.RefusePermanent(w, r) {
		return
	}

	if err := e.config.Store.Delete(r.Context(), r.PathValue("id")); err != nil {
		e.refused(w, r, err)

		return
	}

	e.admin.Redirect(w, r, e.admin.URL(e.route("index"), url.Values{"locale": {locale}}))
}

// refused explains why a delete was refused, through Issue; an item that is
// still referenced elsewhere is "in use". Other errors fail.
func (e Editor[F, T, I]) refused(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, content.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	message := e.Issue(r, err)
	if message == "" && db.IsForeignKey(err) {
		message = e.admin.T(r, "This item is still in use.")
	}

	e.refuse(w, r, err, message)
}

// move returns a handler that moves an item one place up (-1) or down (+1).
func (e Editor[F, T, I]) move(step int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locale, _, err := e.Locale(r)
		if err != nil {
			e.admin.Fail(w, r, err)

			return
		}

		items, err := e.config.Store.List(r.Context())
		if err != nil {
			e.admin.Fail(w, r, err)

			return
		}

		for i, item := range items {
			if item.ID != r.PathValue("id") {
				continue
			}

			if err := e.config.Store.(content.Mover).Move(r.Context(), item.ID, i+step); err != nil {
				e.fail(w, r, err)

				return
			}
		}

		e.admin.Redirect(w, r, e.admin.URL(e.route("index"), url.Values{"locale": {locale}}))
	}
}

// preview sends the editor to the revision in the URL on the website (see
// Config.PreviewURL), or back with a notice when it has no address yet.
func (e Editor[F, T, I]) preview(w http.ResponseWriter, r *http.Request) {
	if e.config.PreviewURL == nil {
		http.NotFound(w, r)

		return
	}

	locale, _, err := e.Locale(r)
	if err != nil {
		e.admin.Fail(w, r, err)

		return
	}

	item, err := e.load(r)
	if err != nil {
		e.fail(w, r, err)

		return
	}

	target := e.config.PreviewURL(r.Context(), item, locale)
	if target == "" {
		back := e.revisionURL(r.Context(), item.ID, locale, item.Revision.ID)
		message := e.admin.T(r, "This revision has no address on the website yet, so it cannot be previewed there.")
		e.admin.RedirectFlash(w, r, back, flash.Warn(message))

		return
	}

	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (e Editor[F, T, I]) fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, content.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	e.admin.Fail(w, r, err)
}

// Slug is the web address an editor typed into an item's slug field, or,
// when the field is left empty, one made from title (content.Slug), for an
// editor's Apply:
//
//	text.Slug = editor.Slug(in.Slug, in.Title)
//
// A title without letters content.Slug can use, such as one in another
// script, gives "".
func Slug(typed, title string) string {
	if typed != "" {
		return typed
	}

	return content.Slug(title)
}
