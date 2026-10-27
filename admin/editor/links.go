package editor

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/links"
	"github.com/donseba/go-webthings/pages"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-partial/exp/flash"
)

// Links to items: the editor of a published item with an address of its
// own, such as a post, lists the published content that links to it (see
// links.Check), asks before the item is unpublished or moved to the trash,
// and tells when publishing a revision changes the item's address. With a
// module that redirects old addresses (links.Redirector) it offers to
// unpublish the item and redirect its address to a page, redirects the old
// address to the new one when a revision that changes it is published, and,
// for content without revisions, when the item is saved with a new address.
// Linked does this for a module's editor; the page tree does the same for
// pages.

// RedirectInput chooses the page that the addresses a page or an item
// leaves redirect to.
type RedirectInput struct {
	Target form.SortedSelect[string] `form:"dropdown" label:"Redirect to" required:"true" placeholder:"—"`
}

// MovedInput publishes a revision that changes the address of a page or an
// item, redirecting the old addresses to the new ones when Redirect is set.
type MovedInput struct {
	Redirect bool `form:"checkbox" label:"Redirect the old addresses to the new ones" description:"Links and search engines that use an old address then lead to the new one."`
}

// Questions before an item that published content links to leaves its
// address.
const (
	unpublishLinked = "Published items link to this item. Unpublish it anyway?"
	deleteLinked    = "Published items link to this item. Move it to the trash anyway?"
	// The questions when published content links only to addresses below
	// the item (Linked.Below).
	unpublishBelow = "Published items link to addresses below this item. Unpublish it anyway?"
	deleteBelow    = "Published items link to addresses below this item. Move it to the trash anyway?"
	// PublishLinked asks before a revision is published that moves a page
	// or an item away from the address that published content links to.
	PublishLinked = "Published items link to the old address, and their links will lead nowhere. Publish anyway?"
)

// Linked adds the links to an item to the editor of a module whose items
// have an address of their own, such as posts or events. Its methods are
// the editor's hooks:
//
//	linked := editor.Linked[PostFields, PostText, postInput]{Check: env.Links(), Address: service.address}
//
//	editor.New(editor.Config[PostFields, PostText, postInput]{
//		// ...
//		Edit:   linked.Edit,
//		Routes: linked.Routes,
//		Saved:  linked.Saved,
//	})
//
// A module with hooks of its own calls these from them.
type Linked[F, T, I any] struct {
	// Check is the site's link check (webthings.Env.Links); nil adds
	// nothing.
	Check *links.Check
	// Address is the address of item, which may be any revision of it, on
	// the site in locale, such as "/en/news/hello": where visitors find it
	// once it is published. Empty when it has none there, such as an item
	// of an instance on no page. It is asked only for languages in which
	// the item is complete (content.Spec.Available).
	Address func(ctx context.Context, item content.Item[F, T], locale string) (string, error)
	// Below, if set, are the addresses below the item's address in locale
	// that leave the website with it, such as the entries of a directory's
	// category, which go offline when the category is unpublished but keep
	// working at their old addresses when its slug changes. The editor then
	// counts the links below the item's address apart from those to the
	// item itself, and unpublishing it with redirects redirects these
	// addresses too, to the same page. Without it, links below the item's
	// address count as links to the item. It is asked only for languages in
	// which the item is complete.
	Below func(ctx context.Context, item content.Item[F, T], locale string) ([]string, error)
}

// enabled reports whether l has what it needs to know the links.
func (l Linked[F, T, I]) enabled() bool {
	return l.Check != nil && l.Address != nil
}

// Edit adds the notices about links to the editor of a published item
// (Config.Edit).
func (l Linked[F, T, I]) Edit(r *http.Request, e Editor[F, T, I], item content.Item[F, T], locale string, view *EditView) error {
	if !l.enabled() {
		return nil
	}

	ctx := r.Context()

	_, config, err := e.Locale(r)
	if err != nil {
		return err
	}

	published, ok, err := e.published(ctx, item)
	if err != nil || !ok {
		return err
	}

	addresses, err := l.addresses(ctx, e, published, config.Enabled)
	if err != nil || len(addresses) == 0 {
		return err
	}

	index, err := l.Check.Index(ctx)
	if err != nil {
		return err
	}

	found := index.To(sortedValues(addresses)...)

	var below []NoticeItem

	if l.Below != nil {
		var under []links.Link

		found, under = splitBelow(found, addresses)
		below = LinkItems(r, e.admin, l.Check, under)
	}

	referrers := LinkItems(r, e.admin, l.Check, found)

	if len(referrers) > 0 || len(below) > 0 {
		if err := l.linkedNotice(r, e, item, locale, addresses, referrers, below, view); err != nil {
			return err
		}
	}

	if e.config.Store.Revisioned() {
		return l.movedNotice(r, e, item, locale, addresses, len(referrers), view)
	}

	return nil
}

// Routes adds the editor's routes that redirect: "unpublish-redirect", and
// "revision-publish-moved" for content with revisions (Config.Routes).
func (l Linked[F, T, I]) Routes(routes *admin.Routes, e Editor[F, T, I]) {
	if !l.enabled() {
		return
	}

	routes.Post("/{id}/unpublish-redirect", admin.Publish, l.unpublishRedirect(e)).As("unpublish-redirect")

	if e.config.Store.Revisioned() {
		routes.Post("/{id}/revisions/{revision}/publish-moved", admin.Publish, l.publishMoved(e)).
			As("revision-publish-moved")
	}
}

// Saved redirects the old addresses of a published item without revisions
// to its new ones when saving changed them, such as with a new slug, and
// the user may add redirects (Config.Saved). Content with revisions changes
// its address when a revision is published instead (Routes).
func (l Linked[F, T, I]) Saved(r *http.Request, e Editor[F, T, I], before, after content.Item[F, T], _ string) *flash.Message {
	if !l.enabled() || e.config.Store.Revisioned() || !before.Published || !after.Published {
		return nil
	}

	redirects := Redirector(r, e.admin, l.Check)
	if redirects == nil {
		return nil
	}

	ctx := r.Context()

	count, err := l.redirectMoved(ctx, r, e, redirects, before, after)
	if count == 0 && err == nil {
		return nil
	}

	notice := RedirectedNotice(r, e.admin, "Changes saved.", count, err)

	return &notice
}

// redirectMoved redirects the addresses of before to those of after where
// they differ, and returns how many it redirected.
func (l Linked[F, T, I]) redirectMoved(
	ctx context.Context,
	r *http.Request,
	e Editor[F, T, I],
	redirects links.Redirector,
	before, after content.Item[F, T],
) (int, error) {
	_, config, err := e.Locale(r)
	if err != nil {
		return 0, err
	}

	old, err := l.addresses(ctx, e, before, config.Enabled)
	if err != nil {
		return 0, err
	}

	next, err := l.addresses(ctx, e, after, config.Enabled)
	if err != nil {
		return 0, err
	}

	return redirectAddresses(ctx, redirects, old, func(code string) string {
		return next[code]
	})
}

// published returns the published state of item: item itself for content
// without revisions, else its published revision; ok is false when the
// item is not on the website.
func (e Editor[F, T, I]) published(ctx context.Context, item content.Item[F, T]) (content.Item[F, T], bool, error) {
	if !e.config.Store.Revisioned() {
		return item, item.Published, nil
	}

	if item.PublishedID == "" {
		return item, false, nil
	}

	if item.Revision.ID == item.PublishedID {
		return item, true, nil
	}

	published, err := e.config.Store.Load(ctx, item.ID, item.PublishedID)
	if err != nil {
		return item, false, err
	}

	return published, true, nil
}

// addresses are the addresses of item, by locale, in the languages in
// which it is complete and has one.
func (l Linked[F, T, I]) addresses(
	ctx context.Context,
	e Editor[F, T, I],
	item content.Item[F, T],
	enabled []string,
) (map[string]string, error) {
	spec := e.config.Store.Spec()
	addresses := map[string]string{}

	for _, code := range enabled {
		if !spec.Available(item, code) {
			continue
		}

		address, err := l.Address(ctx, item, code)
		if err != nil {
			return nil, err
		}

		if address != "" {
			addresses[code] = path.Clean(address)
		}
	}

	return addresses, nil
}

// splitBelow divides the links found to the addresses of an item into
// those to one of the addresses itself and those to an address below one.
func splitBelow(found []links.Link, addresses map[string]string) (own, below []links.Link) {
	values := sortedValues(addresses)

	for _, link := range found {
		if slices.Contains(values, path.Clean(link.Address)) {
			own = append(own, link)

			continue
		}

		below = append(below, link)
	}

	return own, below
}

// below are the addresses below item that leave the website with it (see
// Linked.Below), by locale, in the languages in which it is complete; none
// without Below.
func (l Linked[F, T, I]) below(
	ctx context.Context,
	e Editor[F, T, I],
	item content.Item[F, T],
	enabled []string,
) (map[string][]string, error) {
	below := map[string][]string{}

	if l.Below == nil {
		return below, nil
	}

	spec := e.config.Store.Spec()

	for _, code := range enabled {
		if !spec.Available(item, code) {
			continue
		}

		addresses, err := l.Below(ctx, item, code)
		if err != nil {
			return nil, err
		}

		below[code] = addresses
	}

	return below, nil
}

// linkedNotice lists the content that links to the item, referrers, and to
// addresses below it, below (see Linked.Below), asks before it is
// unpublished or deleted and offers to unpublish it with redirects.
func (l Linked[F, T, I]) linkedNotice(
	r *http.Request,
	e Editor[F, T, I],
	item content.Item[F, T],
	locale string,
	addresses map[string]string,
	referrers, below []NoticeItem,
	view *EditView,
) error {
	a := e.admin
	redirects := Redirector(r, a, l.Check)

	var notices []Notice

	if count := len(referrers); count > 0 {
		linked := admin.TN(
			r.Context(),
			"%d published item links to this item.",
			"%d published items link to this item.",
			count,
			count,
		)
		notice := Notice{
			Text:  linked + " " + a.T(r, "Unpublishing it, moving it to the trash or changing its address breaks these links."),
			Items: referrers,
		}

		if redirects != nil && !e.config.Store.Revisioned() {
			notice.Text += " " + a.T(r, "Saving it with another address redirects the old address to the new one.")
		}

		notices = append(notices, notice)
	}

	if count := len(below); count > 0 {
		linked := admin.TN(
			r.Context(),
			"%d published item links to an address below this item.",
			"%d published items link to addresses below this item.",
			count,
			count,
		)
		notices = append(notices, Notice{
			Text:  linked + " " + a.T(r, "Unpublishing it or moving it to the trash breaks these links."),
			Items: below,
		})
	}

	unpublishQuestion, deleteQuestion := unpublishLinked, deleteLinked
	if len(referrers) == 0 {
		unpublishQuestion, deleteQuestion = unpublishBelow, deleteBelow
	}

	unpublishing := false

	if view.Panel != nil && view.Panel.CanUnpublish {
		view.Panel.Unpublish = a.Confirm(r, view.Panel.Unpublish, unpublishQuestion)
		unpublishing = true
	}

	if view.Unpublish != nil {
		unpublish := a.Confirm(r, *view.Unpublish, unpublishQuestion)
		view.Unpublish = &unpublish
		unpublishing = true
	}

	// The form to unpublish with redirects goes with the last notice.
	notice := &notices[len(notices)-1]

	if unpublishing && redirects != nil {
		table, err := l.Check.Table(r.Context())
		if err != nil {
			return err
		}

		in := RedirectInput{Target: RedirectTargets(table, locale, "", pageAt(table, locale, addresses))}
		action := a.URL(e.route("unpublish-redirect"), "id", item.ID, url.Values{"locale": {locale}})
		model := a.Form(r, in, action, "Unpublish and redirect")
		model.Info.IDPrefix = "unpublish"
		notice.Forms = append(notice.Forms, model)
	}

	if view.Delete != nil {
		remove := a.Confirm(r, *view.Delete, deleteQuestion)
		view.Delete = &remove
	}

	view.Notices = append(view.Notices, notices...)

	return nil
}

// pageAt is the page that answers the item's address in locale, or else in
// another language, such as the module page the item is shown on: the
// target its address redirects to unless the editor chooses another.
func pageAt(table *pages.Table, locale string, addresses map[string]string) string {
	codes := sortedKeys(addresses)
	if _, ok := addresses[locale]; ok {
		codes = append([]string{locale}, codes...)
	}

	for _, code := range codes {
		pagePath := strings.TrimPrefix(addresses[code], "/"+code)

		if route, _, ok := table.Lookup(code, pagePath); ok {
			return route.PageID
		}
	}

	return ""
}

// movedNotice tells, in the editor of a revision that is not published,
// which addresses publishing it changes, and publishes it with redirects
// from the old addresses when the site has a module for that.
func (l Linked[F, T, I]) movedNotice(
	r *http.Request,
	e Editor[F, T, I],
	item content.Item[F, T],
	locale string,
	addresses map[string]string,
	linked int,
	view *EditView,
) error {
	if view.Panel == nil || item.Revision.ID == item.PublishedID {
		return nil
	}

	a := e.admin
	ctx := r.Context()
	spec := e.config.Store.Spec()

	var items []NoticeItem

	for _, code := range sortedKeys(addresses) {
		next := ""

		if spec.Available(item, code) {
			address, err := l.Address(ctx, item, code)
			if err != nil {
				return err
			}

			if address != "" {
				next = path.Clean(address)
			}
		}

		if next == addresses[code] {
			continue
		}

		label := addresses[code] + " → " + next
		if next == "" {
			label = addresses[code] + " → " + a.T(r, "not on the website")
		}

		items = append(items, NoticeItem{Label: label})
	}

	if len(items) == 0 {
		return nil
	}

	notice := Notice{
		Text:  a.T(r, "Publishing this revision changes the address of this item."),
		Items: items,
	}

	if linked > 0 {
		notice.Text += " " + admin.TN(ctx, "%d published item links to the old address.",
			"%d published items link to the old address.", linked, linked)
	}

	view.Notices = append(view.Notices, notice)

	if !view.Panel.CanPublish {
		return nil
	}

	if Redirector(r, a, l.Check) == nil {
		if linked > 0 {
			view.Panel.Publish = a.Confirm(r, view.Panel.Publish, PublishLinked)
		}

		return nil
	}

	// The panel's own publish form, with its question when publishing
	// cancels a schedule, makes way for one that redirects too.
	query := url.Values{"locale": {locale}}
	action := a.URL(e.route("revision-publish-moved"), "id", item.ID, "revision", item.Revision.ID, query)
	publish := a.Form(r, MovedInput{Redirect: true}, action, "Publish this revision")
	publish.Info.Attributes = view.Panel.Publish.Info.Attributes
	view.Panel.Publish = publish

	return nil
}

// publishMoved publishes the revision in the URL and, when the editor asks
// for it, redirects the addresses the item leaves to its new ones.
func (l Linked[F, T, I]) publishMoved(e Editor[F, T, I]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := e.admin
		ctx := r.Context()

		locale, config, err := e.Locale(r)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		var in MovedInput

		if errs, err := a.Bind(r, &in); err != nil || len(errs) > 0 {
			a.BadRequest(w, r)

			return
		}

		id, revision := r.PathValue("id"), r.PathValue("revision")

		latest, err := e.config.Store.Load(ctx, id, "")
		if err != nil {
			e.fail(w, r, err)

			return
		}

		before, published, err := e.published(ctx, latest)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		done := "Published."
		if e.scheduled(r) {
			done = "Published. The schedule was cancelled."
		}

		if err := e.config.Store.Publish(ctx, id, revision, config.Default); err != nil {
			e.refuse(w, r, err, e.Issue(r, err))

			return
		}

		back := e.itemURL(ctx, id, locale)
		redirects := Redirector(r, a, l.Check)

		if !in.Redirect || !published || redirects == nil {
			a.RedirectNotice(w, r, back, done)

			return
		}

		after, err := e.config.Store.Load(ctx, id, revision)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		count, err := l.redirectMoved(ctx, r, e, redirects, before, after)
		a.RedirectFlash(w, r, back, RedirectedNotice(r, a, done, count, err))
	}
}

// unpublishRedirect takes the item in the URL off the website and
// redirects its addresses to the page the editor chose, in the same
// language or else in the default one.
func (l Linked[F, T, I]) unpublishRedirect(e Editor[F, T, I]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := e.admin
		ctx := r.Context()

		locale, config, err := e.Locale(r)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		redirects := Redirector(r, a, l.Check)
		if redirects == nil {
			a.Forbidden(w, r)

			return
		}

		if e.RefusePermanent(w, r) {
			return
		}

		id := r.PathValue("id")

		table, err := l.Check.Table(ctx)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		in := RedirectInput{Target: RedirectTargets(table, locale, "", "")}

		if errs, err := a.Bind(r, &in); err != nil || len(errs) > 0 || in.Target.Get() == "" {
			a.BadRequest(w, r)

			return
		}

		latest, err := e.config.Store.Load(ctx, id, "")
		if err != nil {
			e.fail(w, r, err)

			return
		}

		published, ok, err := e.published(ctx, latest)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		addresses := map[string]string{}
		below := map[string][]string{}

		if ok {
			addresses, err = l.addresses(ctx, e, published, config.Enabled)
			if err != nil {
				a.Fail(w, r, err)

				return
			}

			below, err = l.below(ctx, e, published, config.Enabled)
			if err != nil {
				a.Fail(w, r, err)

				return
			}
		}

		done := "Unpublished."
		if e.scheduled(r) {
			done = "Unpublished. The schedule was cancelled."
		}

		if err := e.config.Store.Unpublish(ctx, id); err != nil {
			e.refuse(w, r, err, e.Issue(r, err))

			return
		}

		destination := table.Alternates(in.Target.Get())
		to := func(code string) string {
			if pagePath, ok := destination[code]; ok {
				return links.Address(code, pagePath)
			}

			if pagePath, ok := destination[config.Default]; ok {
				return links.Address(config.Default, pagePath)
			}

			return ""
		}

		count, err := redirectAddresses(ctx, redirects, addresses, to)
		if err == nil {
			var redirected int

			redirected, err = redirectBelow(ctx, redirects, below, to)
			count += redirected
		}

		a.RedirectFlash(w, r, e.itemURL(ctx, id, locale), RedirectedNotice(r, a, done, count, err))
	}
}

// redirectAddresses sends the visitors of each address, by locale, to the
// address to gives its locale, skipping those it gives none or the same,
// and returns how many it redirected.
func redirectAddresses(
	ctx context.Context,
	redirects links.Redirector,
	addresses map[string]string,
	to func(locale string) string,
) (int, error) {
	count := 0

	for _, code := range sortedKeys(addresses) {
		address := addresses[code]

		target := to(code)
		if target == "" || target == address {
			continue
		}

		if err := redirects.Redirect(ctx, address, target); err != nil {
			return count, fmt.Errorf("redirect %s: %w", address, err)
		}

		count++
	}

	return count, nil
}

// redirectBelow is redirectAddresses for the addresses below an item (see
// Linked.Below), by locale: each goes where to sends its language.
func redirectBelow(
	ctx context.Context,
	redirects links.Redirector,
	below map[string][]string,
	to func(locale string) string,
) (int, error) {
	count := 0

	for _, code := range slices.Sorted(maps.Keys(below)) {
		target := to(code)

		for _, address := range below[code] {
			address = path.Clean(address)
			if target == "" || target == address {
				continue
			}

			if err := redirects.Redirect(ctx, address, target); err != nil {
				return count, fmt.Errorf("redirect %s: %w", address, err)
			}

			count++
		}
	}

	return count, nil
}

// Redirector returns the module that redirects old addresses (see
// links.Check.Redirects) when the user of r may add redirects to it; nil
// otherwise, also for a nil check.
func Redirector(r *http.Request, a *admin.Admin, check *links.Check) links.Redirector {
	if check == nil {
		return nil
	}

	redirects, key := check.Redirects()
	if redirects == nil || !a.Can(r, admin.Permission{Section: key, Action: admin.Create}) {
		return nil
	}

	return redirects
}

// RedirectedNotice is the toast after a change that redirected count old
// addresses: done, a translation key such as "Published.", and how many
// redirect now. A failed redirect, err, makes it a warning.
func RedirectedNotice(r *http.Request, a *admin.Admin, done string, count int, err error) flash.Message {
	text := a.T(r, done)
	if count > 0 {
		text += " " + admin.TN(r.Context(), "%d old address redirects now.", "%d old addresses redirect now.", count, count)
	}

	if err != nil {
		return flash.Warn(text + " " + a.T(r, "Not every old address could be redirected; see the redirects."))
	}

	return flash.Success(text)
}

// RedirectTargets lists the published pages of locale that addresses can
// redirect to, in tree order and indented by depth, with selected chosen:
// all of them, or without the page leaving and the pages below it.
func RedirectTargets(table *pages.Table, locale, leaving, selected string) form.SortedSelect[string] {
	var (
		options []form.Option[string]
		below   = -1
	)

	for _, route := range table.Routes(locale) {
		if below >= 0 && route.Depth > below {
			continue
		}

		below = -1

		if leaving != "" && route.PageID == leaving {
			below = route.Depth

			continue
		}

		options = append(options, form.Option[string]{
			Value: route.PageID,
			Label: strings.Repeat("— ", route.Depth) + route.Title,
		})
	}

	return form.NewOrderedSelect(options...).Selected(selected)
}

// LinkItems lists the content that the links found are in, once each, as
// notice items: linked to its editor, with its kind and language (see
// LinkItem).
func LinkItems(r *http.Request, a *admin.Admin, check *links.Check, found []links.Link) []NoticeItem {
	var (
		items []NoticeItem
		seen  = map[string]bool{}
	)

	for _, link := range found {
		key := link.Module + "\x00" + link.Source.ID + "\x00" + link.Locale
		if seen[key] {
			continue
		}

		seen[key] = true
		items = append(items, LinkItem(r, a, check, link))
	}

	return items
}

// LinkItem names the content a link is in, linked to its editor, with a
// note of its kind, a page or the title of its module, and its language,
// such as "News · en".
func LinkItem(r *http.Request, a *admin.Admin, check *links.Check, link links.Link) NoticeItem {
	item := NoticeItem{
		Label: link.Source.Title,
		URL:   link.Source.EditURL,
		Note:  a.T(r, "Page") + " · " + link.Locale,
	}

	if link.Module == "" {
		item.URL = a.URL("admin.pages.edit", "id", link.Source.ID, url.Values{"locale": {link.Locale}})
	} else {
		item.Note = check.Title(link.Module) + " · " + link.Locale
	}

	if item.Label == "" {
		item.Label = a.T(r, "Untitled")
	}

	return item
}

// sortedValues are the values of a map, in the order of its sorted keys.
func sortedValues(addresses map[string]string) []string {
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
