package pageadmin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/links"
	"github.com/donseba/go-webthings/pages"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-partial/exp/flash"
)

// Links to pages: the editor of a published page lists the published
// content that links to it (see links.Check), asks before the page is
// unpublished or moved to the trash, and tells when publishing a revision
// changes the page's address. With a module that redirects old addresses
// (links.Redirector) it offers to redirect them: to another page when the
// page is unpublished, to the new address when its address changes. The
// report at "admin.pages.links" lists the links that lead nowhere.

// addressInput redirects an address that links lead to, from the report.
type addressInput struct {
	Address string                    `form:"input,hidden" name:"address" required:"true" maxLength:"500"`
	Target  form.SortedSelect[string] `form:"dropdown"                    required:"true"                 label:"Redirect to" placeholder:"—"`
}

// Questions before a page that published content links to leaves the
// website.
const (
	unpublishLinked = "Published items link to this page. Unpublish it anyway?"
	deleteLinked    = "Published items link to this page. Move it to the trash anyway?"
)

// pageLinks is the page editor's knowledge of the links to pages.
type pageLinks struct {
	tree  *pages.Service
	check *links.Check
}

// moved is an address a page leaves: the address of a published page in
// locale, or a linked address below it (rest), such as an item of a module
// on the page.
type moved struct {
	page    string
	locale  string
	address string
	rest    string
}

// published returns the addresses of a page, by locale; none for a page
// that is not published.
func published(table *pages.Table, id string) map[string]string {
	addresses := map[string]string{}

	for locale, path := range table.Alternates(id) {
		addresses[locale] = links.Address(locale, path)
	}

	return addresses
}

// leaving lists the addresses that leave with the page id when it is
// unpublished or moved: its own, those of the published pages below it and
// those that links lead to below them, such as a module's items.
func leaving(table *pages.Table, index *links.Index, id string) []moved {
	var (
		list  []moved
		known = map[string]bool{}
		bases []string
	)

	for locale, base := range published(table, id) {
		bases = append(bases, base)

		for _, route := range table.Routes(locale) {
			address := links.Address(locale, route.Path)
			if !links.Within(address, base) {
				continue
			}

			list = append(list, moved{page: route.PageID, locale: locale, address: address})
			known[address] = true
		}
	}

	for _, link := range index.To(bases...) {
		if known[link.Address] || !link.Found || link.Page == "" {
			continue
		}

		locale, _, _ := strings.Cut(strings.TrimPrefix(link.Address, "/"), "/")
		page := links.Address(locale, table.Alternates(link.Page)[locale])
		known[link.Address] = true

		list = append(list, moved{
			page:    link.Page,
			locale:  locale,
			address: link.Address,
			rest:    strings.TrimPrefix(link.Address, page),
		})
	}

	slices.SortFunc(list, func(a, b moved) int {
		return strings.Compare(a.address, b.address)
	})

	return list
}

// scheduled reports whether page id has a planned publication change,
// which publishing or unpublishing it by hand cancels.
func (l pageLinks) scheduled(ctx context.Context, id string) bool {
	page, err := l.tree.Latest(ctx, id)

	return err == nil && !page.Schedule.IsZero()
}

// redirector is the module that redirects old addresses, when the user may
// add redirects to it; nil otherwise.
func (l pageLinks) redirector(r *http.Request, e pageEditor) links.Redirector {
	return editor.Redirector(r, e.Admin(), l.check)
}

// edit adds the notices about links to the editor of a published page
// (editor.Config.Edit).
func (l pageLinks) edit(r *http.Request, e pageEditor, page pages.Page, locale string, view *editor.EditView) error {
	ctx := r.Context()

	table, err := l.tree.Table(ctx)
	if err != nil {
		return err
	}

	addresses := published(table, page.ID)
	if len(addresses) == 0 {
		return nil
	}

	index, err := l.check.Index(ctx)
	if err != nil {
		return err
	}

	referrers := editor.LinkItems(r, e.Admin(), l.check, index.To(values(addresses)...))

	if len(referrers) > 0 {
		l.linkedNotice(r, e, table, page, locale, referrers, view)
	}

	l.movedNotice(r, e, table, page, locale, addresses, len(referrers), view)

	return nil
}

// linkedNotice lists the content that links to the page, asks before it is
// unpublished or deleted and offers to unpublish it with redirects.
func (l pageLinks) linkedNotice(
	r *http.Request,
	e pageEditor,
	table *pages.Table,
	page pages.Page,
	locale string,
	referrers []editor.NoticeItem,
	view *editor.EditView,
) {
	a := e.Admin()
	count := len(referrers)
	linked := admin.TN(
		r.Context(),
		"%d published item links to this page.",
		"%d published items link to this page.",
		count,
		count,
	)
	notice := editor.Notice{
		Text:  linked + " " + a.T(r, "Unpublishing the page, moving it to the trash or changing its address breaks these links."),
		Items: referrers,
	}

	if view.Panel != nil && view.Panel.CanUnpublish {
		view.Panel.Unpublish = a.Confirm(r, view.Panel.Unpublish, unpublishLinked)

		if l.redirector(r, e) != nil {
			in := editor.RedirectInput{Target: editor.RedirectTargets(table, locale, page.ID, parentOf(table, page.ID, locale))}
			model := a.Form(r, in, a.URL("admin.pages.unpublish-redirect", "id", page.ID, url.Values{"locale": {locale}}),
				"Unpublish and redirect")
			model.Info.IDPrefix = "unpublish"
			notice.Forms = append(notice.Forms, model)
		}
	}

	if view.Delete != nil {
		remove := a.Confirm(r, *view.Delete, deleteLinked)
		view.Delete = &remove
	}

	view.Notices = append(view.Notices, notice)
}

// movedNotice tells, in the editor of a revision that is not published,
// which addresses publishing it changes, and publishes it with redirects
// from the old addresses when the site has a module for that.
func (l pageLinks) movedNotice(
	r *http.Request,
	e pageEditor,
	table *pages.Table,
	page pages.Page,
	locale string,
	addresses map[string]string,
	linked int,
	view *editor.EditView,
) {
	if view.Panel == nil || page.Revision.ID == page.PublishedID || isHome(page) {
		return
	}

	a := e.Admin()

	var items []editor.NoticeItem

	for _, code := range sortedKeys(addresses) {
		next := predicted(table, page, code)
		if next == addresses[code] {
			continue
		}

		label := addresses[code] + " → " + next
		if next == "" {
			label = addresses[code] + " → " + a.T(r, "not on the website")
		}

		items = append(items, editor.NoticeItem{Label: label})
	}

	if len(items) == 0 {
		return
	}

	notice := editor.Notice{
		Text:  a.T(r, "Publishing this revision changes the address of the page and of the pages below it."),
		Items: items,
	}

	if linked > 0 {
		notice.Text += " " + admin.TN(r.Context(), "%d published item links to the old address.",
			"%d published items link to the old address.", linked, linked)
	}

	view.Notices = append(view.Notices, notice)

	if !view.Panel.CanPublish {
		return
	}

	if l.redirector(r, e) == nil {
		if linked > 0 {
			view.Panel.Publish = a.Confirm(r, view.Panel.Publish, editor.PublishLinked)
		}

		return
	}

	// The panel's own publish form, with its question when publishing
	// cancels a schedule, makes way for one that redirects too.
	query := url.Values{"locale": {locale}}
	action := a.URL("admin.pages.revision-publish-moved", "id", page.ID, "revision", page.Revision.ID, query)
	publish := a.Form(r, editor.MovedInput{Redirect: true}, action, "Publish this revision")
	publish.Info.Attributes = view.Panel.Publish.Info.Attributes
	view.Panel.Publish = publish
}

// predicted is the address page would have in locale once published, from
// its parent's published address and its slug; empty when it would not be
// on the website in locale.
func predicted(table *pages.Table, page pages.Page, locale string) string {
	text, ok := page.Text(locale)
	if !ok || text.Title == "" {
		return ""
	}

	parent, ok := table.Alternates(page.Fields.ParentID)[locale]
	if !ok {
		return ""
	}

	return links.Address(locale, strings.TrimSuffix(parent, "/")+"/"+text.Slug)
}

// parentOf is the published parent of a page in locale, the page's
// redirect target unless the editor chooses another.
func parentOf(table *pages.Table, id, locale string) string {
	for _, route := range table.Routes(locale) {
		if route.PageID == id {
			return route.ParentID
		}
	}

	return ""
}

// values are the values of a map, in the order of its sorted keys.
func values(addresses map[string]string) []string {
	list := make([]string, 0, len(addresses))

	for _, key := range sortedKeys(addresses) {
		list = append(list, addresses[key])
	}

	return list
}

// sortedKeys are the keys of a map, sorted.
func sortedKeys(addresses map[string]string) []string {
	keys := make([]string, 0, len(addresses))

	for key := range addresses {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys
}

// redirect sends the visitors of each address that moves to the address to
// gives it, skipping those it gives none, and returns how many it
// redirected.
func redirect(ctx context.Context, redirects links.Redirector, list []moved, to func(moved) string) (int, error) {
	count := 0

	for _, m := range list {
		target := to(m)
		if target == "" || target == m.address {
			continue
		}

		if err := redirects.Redirect(ctx, m.address, target); err != nil {
			return count, fmt.Errorf("redirect %s: %w", m.address, err)
		}

		count++
	}

	return count, nil
}

// refuse returns to the page's editor with the reason a change was
// refused, or fails for an unexpected error.
func refuse(w http.ResponseWriter, r *http.Request, e pageEditor, target string, err error) {
	a := e.Admin()

	if errors.Is(err, pages.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	message := e.Issue(r, err)
	if message == "" {
		a.Fail(w, r, err)

		return
	}

	a.RedirectFlash(w, r, target, flash.Error(message))
}

// publishMoved publishes the revision in the URL, which changes the page's
// address, and redirects the addresses it left to the new ones when the
// editor asks for that.
func (l pageLinks) publishMoved(e pageEditor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := e.Admin()
		ctx := r.Context()

		locale, _, err := e.Locale(r)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		id := r.PathValue("id")
		back := a.URL("admin.pages.edit", "id", id, url.Values{"locale": {locale}})

		var in editor.MovedInput

		if errs, err := a.Bind(r, &in); err != nil || len(errs) > 0 {
			a.BadRequest(w, r)

			return
		}

		before, err := l.tree.Table(ctx)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		index, err := l.check.Index(ctx)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		moving := leaving(before, index, id)

		done := "Published."
		if l.scheduled(ctx, id) {
			done = "Published. The schedule was cancelled."
		}

		if err := l.tree.Publish(ctx, id, r.PathValue("revision")); err != nil {
			refuse(w, r, e, back, err)

			return
		}

		redirects := l.redirector(r, e)
		if !in.Redirect || redirects == nil {
			a.RedirectNotice(w, r, back, done)

			return
		}

		after, err := l.tree.Table(ctx)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		count, err := redirect(ctx, redirects, moving, func(m moved) string {
			path, ok := after.Alternates(m.page)[m.locale]
			if !ok {
				return ""
			}

			return links.Address(m.locale, path) + m.rest
		})

		a.RedirectFlash(w, r, back, editor.RedirectedNotice(r, a, done, count, err))
	}
}

// unpublishRedirect takes the page in the URL off the website and
// redirects the addresses it left to the page the editor chose, in the
// same language or else in the default one.
func (l pageLinks) unpublishRedirect(e pageEditor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := e.Admin()
		ctx := r.Context()

		locale, config, err := e.Locale(r)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		redirects := l.redirector(r, e)
		if redirects == nil {
			a.Forbidden(w, r)

			return
		}

		if e.RefusePermanent(w, r) {
			return
		}

		id := r.PathValue("id")
		back := a.URL("admin.pages.edit", "id", id, url.Values{"locale": {locale}})

		before, err := l.tree.Table(ctx)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		in := editor.RedirectInput{Target: editor.RedirectTargets(before, locale, id, "")}

		if errs, err := a.Bind(r, &in); err != nil || len(errs) > 0 || in.Target.Get() == "" {
			a.BadRequest(w, r)

			return
		}

		index, err := l.check.Index(ctx)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		moving := leaving(before, index, id)

		done := "Unpublished."
		if l.scheduled(ctx, id) {
			done = "Unpublished. The schedule was cancelled."
		}

		if err := l.tree.Unpublish(ctx, id); err != nil {
			refuse(w, r, e, back, err)

			return
		}

		after, err := l.tree.Table(ctx)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		destination := after.Alternates(in.Target.Get())

		count, err := redirect(ctx, redirects, moving, func(m moved) string {
			if path, ok := destination[m.locale]; ok {
				return links.Address(m.locale, path)
			}

			if path, ok := destination[config.Default]; ok {
				return links.Address(config.Default, path)
			}

			return ""
		})

		a.RedirectFlash(w, r, back, editor.RedirectedNotice(r, a, done, count, err))
	}
}
