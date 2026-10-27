package editor

import (
	"maps"
	"net/http"
	"net/url"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/content"

	form "github.com/donseba/go-form/v2"
)

// A save that would overwrite a newer save is refused. The edit form of an
// item remembers the version of the item it was opened on (see version) in
// the hidden field versionField. A save whose version is no longer the
// item's shows the form again as the editor typed it, with a Conflict that
// says who saved the item and when, and a "Save anyway" button: it submits
// the form, whose id is formID, with the item's version as overwriteField,
// and so saves over that version only. A save made after it is refused
// again.
const (
	versionField   = "_version"
	overwriteField = "_overwrite"
	formID         = "edit-form"
)

// The text of a Conflict, with and without the name of who saved.
const (
	conflictNamed   = "%s saved this item at %s while you were editing it. Open their version in a new tab to compare, or save yours over it."
	conflictUnnamed = "This item was saved at %s while you were editing it. Open the saved version in a new tab to compare, or save yours over it."
)

// Conflict is the notice of a save that was refused because someone saved
// the item after its form was opened (see EditView.Conflict).
type Conflict struct {
	// Text says who saved the item and when; it is shown as written.
	Text string
	// URL is the editor of the saved version, which the notice opens in a
	// new tab to compare.
	URL string
	// Form is the id of the edit form; Name and Value are those of the
	// "Save anyway" button that submits it. Value is the saved version,
	// which the form may then save over.
	Form  string
	Name  string
	Value string
}

// version is what changes with every save of an item, which its edit form
// remembers: the ID of the latest revision for content with revisions,
// and the time of the last change otherwise. latest is the item's latest
// state. It is empty for a new item and for a store that keeps neither,
// whose saves are not checked.
func (e Editor[F, T, I]) version(latest content.Item[F, T]) string {
	if e.config.Store.Revisioned() {
		return latest.Revision.ID
	}

	if latest.UpdatedAt.IsZero() {
		return ""
	}

	return latest.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

// latest returns the latest state of item, which saves build on: item
// itself, unless the request shows an older revision of it (see load).
func (e Editor[F, T, I]) latest(r *http.Request, item content.Item[F, T]) (content.Item[F, T], error) {
	if item.ID == "" || !e.config.Store.Revisioned() {
		return item, nil
	}

	if r.PathValue("revision") == "" && r.URL.Query().Get("revision") == "" {
		return item, nil
	}

	return e.config.Store.Load(r.Context(), item.ID, "")
}

// basis is the version a submitted edit form builds on: the one it was
// opened on or, after "Save anyway", current, the item's version, which
// the editor chose to save over.
func basis(r *http.Request, current string) string {
	if overwrite := r.PostFormValue(overwriteField); overwrite != "" && overwrite == current {
		return current
	}

	return r.PostFormValue(versionField)
}

// conflict is the notice of a save refused because latest, the item's
// latest state, is not the version the form builds on: who saved it and
// when, a link to it and the button that saves over it. A save without an
// author, such as an import, is told without a name.
func (e Editor[F, T, I]) conflict(r *http.Request, latest content.Item[F, T], locale string) *Conflict {
	author, at := latest.UpdatedBy, latest.UpdatedAt
	link := e.itemURL(r.Context(), latest.ID, locale)

	if e.config.Store.Revisioned() {
		author, at = latest.Revision.Author, latest.Revision.CreatedAt
		link = e.revisionURL(r.Context(), latest.ID, locale, latest.Revision.ID)
	}

	text := e.admin.T(r, conflictUnnamed, e.savedAt(r, at))
	if name := e.admin.UserName(r, author); name != "" {
		text = e.admin.T(r, conflictNamed, name, e.savedAt(r, at))
	}

	return &Conflict{
		Text:  text,
		URL:   link,
		Form:  formID,
		Name:  overwriteField,
		Value: e.version(latest),
	}
}

// savedAt shows when a save was made, in the admin language and the site's
// timezone: its time of day, such as "14:32", when that was today, and its
// date and time otherwise.
func (e Editor[F, T, I]) savedAt(r *http.Request, at time.Time) string {
	location := e.admin.Location(r)
	today := time.Now().In(location)
	day := at.In(location)

	if day.Year() == today.Year() && day.YearDay() == today.YearDay() {
		return admin.Format(r.Context()).Time(at)
	}

	return admin.DateTime(r.Context(), at)
}

// remember gives the edit form model the id by which the "Save anyway"
// button submits it and, in a hidden field, version: the version of the
// item it builds on.
func remember(model *form.RenderModel, version string) {
	attributes := maps.Clone(model.Info.Attributes)
	if attributes == nil {
		attributes = map[string]string{}
	}

	attributes["id"] = formID
	model.Info.Attributes = attributes

	if version == "" {
		return
	}

	hidden := url.Values{}
	maps.Copy(hidden, model.Info.Hidden)
	hidden.Set(versionField, version)
	model.Info.Hidden = hidden
}
