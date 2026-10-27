package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/donseba/go-partial/exp/flash"
)

// Deleting in the admin moves items to the trash: they leave the website and
// every list, and come back with Restore or go for good with Purge. The
// trash ("admin.trash") lists the deleted items of every section the user
// may view, grouped by section; ?section=<id> shows one section with pages.
// Restoring and deleting for good need the Delete right of the section.

// Trash holds a section's deleted items (see Section.Trash). Sections built
// with admin/editor have one; modules with tables of their own implement it
// for their records, and JoinTrash combines several.
type Trash interface {
	// List returns the deleted items whose title or kind matches query (see
	// Matches; empty matches all), most recently deleted first: at most limit
	// of them from offset, and how many match in all.
	List(ctx context.Context, query string, limit, offset int) ([]TrashItem, int, error)
	// Restore brings items back. Items that cannot come back, such as an item
	// whose slug another item took meanwhile, are reported as TrashErrors
	// (see EachTrashItem) while the others are restored.
	Restore(ctx context.Context, ids []string) error
	// Purge removes items for good, with what belongs to them.
	Purge(ctx context.Context, ids []string) error
}

// TrashItem is a deleted item.
type TrashItem struct {
	ID    string
	Title string
	// Kind names the type of item when a section keeps several, such as
	// "Submission"; it is translated. Empty shows none.
	Kind      string
	DeletedAt time.Time
	// DeletedBy is the ID of the user who deleted it; empty when unknown.
	DeletedBy string
	// URL shows the item somewhere, if it can be seen while deleted; empty
	// shows the title without link.
	URL string
}

// TrashError is an item that Restore or Purge could not handle.
type TrashError struct {
	ID string
	// Reason explains it to the editor; it is translated, for example "Its
	// slug is used by another item now.".
	Reason string
	Err    error
}

// Error names the item and the underlying error, for the log; the editor
// sees Reason.
func (e *TrashError) Error() string {
	return "trash item " + e.ID + ": " + e.Err.Error()
}

// Unwrap returns the underlying error, so errors.Is sees through it.
func (e *TrashError) Unwrap() error {
	return e.Err
}

// EachTrashItem runs fn for every ID, for implementations of Restore and
// Purge. Errors for which reason returns a message become TrashErrors and do
// not stop the others; any other error stops at once and is returned.
func EachTrashItem(ids []string, fn func(id string) error, reason func(err error) string) error {
	var failures []error

	for _, id := range ids {
		err := fn(id)
		if err == nil {
			continue
		}

		message := ""
		if reason != nil {
			message = reason(err)
		}

		if message == "" {
			return err
		}

		failures = append(failures, &TrashError{ID: id, Reason: message, Err: err})
	}

	return errors.Join(failures...)
}

// trashErrors returns the TrashErrors in err, and whether err holds nothing
// else.
func trashErrors(err error) ([]*TrashError, bool) {
	if err == nil {
		return nil, true
	}

	var single *TrashError
	if errors.As(err, &single) && single == err {
		return []*TrashError{single}, true
	}

	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return nil, false
	}

	var out []*TrashError

	for _, part := range joined.Unwrap() {
		found, only := trashErrors(part)
		if !only {
			return nil, false
		}

		out = append(out, found...)
	}

	return out, true
}

// JoinTrash lists the items of several trashes as one, for a section that
// keeps several kinds of records, such as forms and their submissions.
func JoinTrash(trashes ...Trash) Trash {
	return joinedTrash(trashes)
}

type joinedTrash []Trash

// joinedID prefixes the ID of an item with the index of its trash.
func joinedID(index int, id string) string {
	return strconv.Itoa(index) + ":" + id
}

func (j joinedTrash) List(ctx context.Context, query string, limit, offset int) ([]TrashItem, int, error) {
	var (
		all   []TrashItem
		total int
	)

	// Every part lists the first offset+limit items; merged by time, the
	// page is the same as in one list.
	for index, trash := range j {
		items, count, err := trash.List(ctx, query, offset+limit, 0)
		if err != nil {
			return nil, 0, err
		}

		total += count

		for _, item := range items {
			item.ID = joinedID(index, item.ID)
			all = append(all, item)
		}
	}

	slices.SortStableFunc(all, func(x, y TrashItem) int {
		return y.DeletedAt.Compare(x.DeletedAt)
	})

	page, _ := Paginate(all[min(offset, len(all)):], 1, limit)

	return page, total, nil
}

// split sorts joined IDs per trash.
func (j joinedTrash) split(ids []string) map[int][]string {
	parts := map[int][]string{}

	for _, id := range ids {
		prefix, rest, ok := strings.Cut(id, ":")

		index, err := strconv.Atoi(prefix)
		if !ok || err != nil || index < 0 || index >= len(j) {
			continue
		}

		parts[index] = append(parts[index], rest)
	}

	return parts
}

func (j joinedTrash) each(ids []string, fn func(trash Trash, ids []string) error) error {
	var errs []error

	for index, part := range j.split(ids) {
		err := fn(j[index], part)

		failures, only := trashErrors(err)
		if !only {
			return err
		}

		for _, failure := range failures {
			failure.ID = joinedID(index, failure.ID)
			errs = append(errs, failure)
		}
	}

	return errors.Join(errs...)
}

func (j joinedTrash) Restore(ctx context.Context, ids []string) error {
	return j.each(ids, func(trash Trash, ids []string) error {
		return trash.Restore(ctx, ids)
	})
}

func (j joinedTrash) Purge(ctx context.Context, ids []string) error {
	return j.each(ids, func(trash Trash, ids []string) error {
		return trash.Purge(ctx, ids)
	})
}

// TrashURL is the admin's trash showing one section, for a link on the
// section's overview.
func (a *Admin) TrashURL(section string) string {
	return a.URL("admin.trash", url.Values{"section": {section}})
}

const (
	// trashPath is the trash's pattern below the admin's path.
	trashPath = "/trash"
	// trashPreview is how many items of each section the overview shows;
	// trashPage is the page size of one section's trash.
	trashPreview = 10
	trashPage    = 50
)

// TrashView is the trash page.
type TrashView struct {
	// Filters are the sections with a trash, for the filter tabs; the first
	// is all sections.
	Filters Tabs
	// Section is the section shown alone; empty shows all.
	Section string
	Groups  []TrashGroup
	// Action is where the selection is posted; it keeps the filter and the
	// search.
	Action string
	// Restore and Purge are offered when some listed item may be changed.
	CanChange bool
	// PurgeQuestion asks before deleting for good.
	PurgeQuestion string
	// Pager pages through one section.
	Pager Pager
}

// TrashGroup are the deleted items of one section.
type TrashGroup struct {
	Section string
	Title   string
	Icon    string
	Items   []TrashEntry
	Total   int
	// MoreURL shows all items of the section when the overview shows only
	// the first ones.
	MoreURL string
	// Error is set when the section could not list its trash.
	Error bool
}

// TrashEntry is a deleted item as listed.
type TrashEntry struct {
	// Value identifies the item in the posted selection; empty when the user
	// may not restore or purge it.
	Value     string
	Title     string
	Kind      string
	URL       string
	DeletedBy string
	When      string
	Time      string
	author    string
}

// trashSections are the sections with a trash the user may view, in
// navigation order.
func (a *Admin) trashSections(r *http.Request) []*Section {
	var out []*Section

	for i := range a.sections {
		section := &a.sections[i]
		if section.Trash != nil && a.mayView(r, *section) {
			out = append(out, section)
		}
	}

	return out
}

// mayDelete reports whether the user may restore and purge in section.
func (a *Admin) mayDelete(r *http.Request, section *Section) bool {
	return a.may(r, *section, Delete)
}

func (a *Admin) trash(w http.ResponseWriter, r *http.Request) {
	a.renderTrash(w, r, http.StatusOK)
}

func (a *Admin) renderTrash(w http.ResponseWriter, r *http.Request, status int) {
	sections := a.trashSections(r)
	if len(sections) == 0 {
		a.Forbidden(w, r)

		return
	}

	query := r.URL.Query()
	selected := query.Get("section")
	search := Query(r)
	view := TrashView{
		Section:       selected,
		Action:        a.URL("admin.trash"),
		PurgeQuestion: a.T(r, "Delete the selected items for good? This cannot be undone."),
	}

	tabs := []Tab{{Label: "All sections"}}
	for _, section := range sections {
		tabs = append(tabs, Tab{Label: section.Title, Value: section.ID})
	}

	view.Filters = FilterTabs(r, "Sections", a.URL("admin.trash"), "section", selected, tabs...)

	limit, offset := trashPreview, 0
	page := 1

	if selected != "" {
		page = PageNumber(r)
		limit, offset = trashPage, (page-1)*trashPage
		view.Action += "?" + url.Values{"section": {selected}}.Encode()
	}

	// Restoring or purging comes back to the same search.
	view.Action = KeepSearch(r, view.Action)

	var entries []*TrashEntry

	for _, section := range sections {
		if selected != "" && section.ID != selected {
			continue
		}

		group := a.trashGroup(r, section, search, limit, offset)
		if group.Total == 0 && !group.Error && selected == "" {
			continue
		}

		if selected == "" && group.Total > len(group.Items) {
			group.MoreURL = KeepSearch(r, a.TrashURL(section.ID))
		}

		if selected != "" {
			view.Pager = NewPager(r, a.URL("admin.trash"), page, page*trashPage < group.Total)
		}

		view.Groups = append(view.Groups, group)
	}

	for i := range view.Groups {
		for j := range view.Groups[i].Items {
			entry := &view.Groups[i].Items[j]
			entries = append(entries, entry)
			view.CanChange = view.CanChange || entry.Value != ""
		}
	}

	a.nameDeleters(r, entries)

	a.Render(w, r, Page{Title: "Trash", Template: "templates/trash.gohtml", Data: view, Status: status})
}

// trashGroup lists one section's trash.
func (a *Admin) trashGroup(r *http.Request, section *Section, search string, limit, offset int) TrashGroup {
	icon := section.Icon
	if icon == "" {
		icon = "box"
	}

	group := TrashGroup{Section: section.ID, Title: a.T(r, section.Title), Icon: icon}

	items, total, err := section.Trash.List(r.Context(), search, limit, offset)
	if err != nil {
		slog.ErrorContext(r.Context(), "trash list", "section", section.ID, "error", err)

		group.Error = true

		return group
	}

	group.Total = total
	changeable := a.mayDelete(r, section)

	for _, item := range items {
		entry := TrashEntry{
			Title:  item.Title,
			URL:    item.URL,
			author: item.DeletedBy,
		}

		if entry.Title == "" {
			entry.Title = a.T(r, "Untitled")
		}

		if item.Kind != "" {
			entry.Kind = a.T(r, item.Kind)
		}

		if changeable {
			entry.Value = section.ID + ":" + item.ID
		}

		entry.When, entry.Time = a.when(r, item.DeletedAt)
		group.Items = append(group.Items, entry)
	}

	return group
}

// nameDeleters fills in the names of who deleted the entries.
func (a *Admin) nameDeleters(r *http.Request, entries []*TrashEntry) {
	if !slices.ContainsFunc(entries, func(entry *TrashEntry) bool { return entry.author != "" }) {
		return
	}

	names, err := a.auth.Names(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "trash names", "error", err)

		return
	}

	for _, entry := range entries {
		entry.DeletedBy = names[entry.author]
	}
}

// trashChange restores or purges the posted selection. Items of sections
// where the user may not delete are skipped; an empty selection is
// refused.
func (a *Admin) trashChange(w http.ResponseWriter, r *http.Request) {
	purge := r.PostForm.Get("action") == "purge"
	selection := map[string][]string{}

	for _, value := range r.PostForm["item"] {
		section, id, ok := strings.Cut(value, ":")
		if ok && id != "" {
			selection[section] = append(selection[section], id)
		}
	}

	// Nothing ticked: say so, rather than that no item was changed.
	if len(selection) == 0 {
		r = WithError(r, a.T(r, "Select at least one item."))
		r.Method = http.MethodGet

		a.renderTrash(w, r, http.StatusUnprocessableEntity)

		return
	}

	var (
		done     int
		failures []string
	)

	for _, section := range a.trashSections(r) {
		ids := selection[section.ID]
		if len(ids) == 0 || !a.mayDelete(r, section) {
			continue
		}

		var err error
		if purge {
			err = section.Trash.Purge(r.Context(), ids)
		} else {
			err = section.Trash.Restore(r.Context(), ids)
		}

		failed, only := trashErrors(err)
		if !only {
			a.Fail(w, r, err)

			return
		}

		done += len(ids) - len(failed)

		for _, failure := range failed {
			failures = append(failures, a.T(r, failure.Reason))
		}
	}

	notice := a.trashNotice(r, purge, done)
	target := KeepSearch(r, a.URL("admin.trash"))

	if section := r.URL.Query().Get("section"); section != "" {
		target = KeepSearch(r, a.TrashURL(section))
	}

	// The notice is translated and counted already: it is shown as it is.
	if len(failures) == 0 {
		a.RedirectFlash(w, r, target, flash.Success(notice))

		return
	}

	// Some items stayed: show why on the trash itself.
	ctx := r.Context()
	if done > 0 {
		ctx = flash.Add(ctx, flash.Success(notice))
	}

	for _, failure := range failures {
		ctx = flash.Add(ctx, flash.Error(failure))
	}

	r = r.WithContext(ctx)
	r.Method = http.MethodGet

	a.renderTrash(w, r, http.StatusUnprocessableEntity)
}

func (a *Admin) trashNotice(r *http.Request, purge bool, count int) string {
	if purge {
		return TN(r.Context(), "%d item deleted for good.", "%d items deleted for good.", count, count)
	}

	return TN(r.Context(), "%d item restored.", "%d items restored.", count, count)
}

// mayUseTrash reports whether the user may open the trash.
func (a *Admin) mayUseTrash(r *http.Request) bool {
	return len(a.trashSections(r)) > 0
}

// expiredPage is how many items of a section's trash PurgeTrash reads at a
// time while it looks for expired ones.
const expiredPage = 100

// PurgeTrash deletes for good the items that went to the trash before
// cutoff, in every section with a trash, the way Delete selected for good
// does (Trash.Purge), and returns how many it deleted. It logs, at Info,
// how many items each section deleted. webthings.App calls it in the
// background with the cutoff of the Site settings (see
// webthings.App.PurgeTrash).
//
// Items a section keeps, such as a file that a deleted item still uses
// (see TrashError), are tried again after the other sections, as long as
// a round deletes something, and otherwise stay in the trash. A section
// that fails does not stop the others: their errors are returned together.
func (a *Admin) PurgeTrash(ctx context.Context, cutoff time.Time) (int, error) {
	var (
		pending []*Section
		errs    []error
	)

	for i := range a.sections {
		if a.sections[i].Trash != nil {
			pending = append(pending, &a.sections[i])
		}
	}

	purged := map[*Section]int{}

	for len(pending) > 0 {
		var (
			kept  []*Section
			round int
		)

		for _, section := range pending {
			count, refused, err := purgeExpired(ctx, section, cutoff)
			if err != nil {
				errs = append(errs, err)
			}

			purged[section] += count
			round += count

			if refused {
				kept = append(kept, section)
			}
		}

		if round == 0 {
			break
		}

		pending = kept
	}

	var total int

	for i := range a.sections {
		section := &a.sections[i]
		if purged[section] == 0 {
			continue
		}

		total += purged[section]
		slog.InfoContext(ctx, "deleted expired items from the trash", "section", section.ID, "count", purged[section])
	}

	return total, errors.Join(errs...)
}

// purgeExpired deletes for good the items of section's trash that went to
// the trash before cutoff, and returns how many it deleted and whether the
// section kept some of them (see TrashError).
func purgeExpired(ctx context.Context, section *Section, cutoff time.Time) (int, bool, error) {
	var ids []string

	for offset := 0; ; offset += expiredPage {
		items, total, err := section.Trash.List(ctx, "", expiredPage, offset)
		if err != nil {
			return 0, false, fmt.Errorf("trash of %s: %w", section.ID, err)
		}

		for _, item := range items {
			if item.DeletedAt.Before(cutoff) {
				ids = append(ids, item.ID)
			}
		}

		if len(items) == 0 || offset+len(items) >= total {
			break
		}
	}

	var (
		purged  int
		refused bool
	)

	// One item at a time, so the count is exact when an error stops the
	// section, as it stops Delete selected for good.
	for _, id := range ids {
		err := section.Trash.Purge(ctx, []string{id})

		failures, only := trashErrors(err)
		if !only {
			return purged, refused, fmt.Errorf("trash of %s: %w", section.ID, err)
		}

		if len(failures) == 0 {
			purged++

			continue
		}

		refused = true

		for _, failure := range failures {
			slog.DebugContext(ctx, "expired trash item kept", "section", section.ID, "id", failure.ID, "error", failure.Err)
		}
	}

	return purged, refused, nil
}
