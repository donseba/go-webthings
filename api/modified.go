package api

import (
	"context"
	"net/http"
	"time"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/pages"
)

// Modified gives the response a Last-Modified: the latest of dates, which
// are the dates of everything the answer shows, such as a post's, those of
// its categories, of the page its module is on (MountModified) and of its
// media (MediaModified). A client that asks with If-Modified-Since is then
// answered 304 Not Modified while none of them is newer: by go-router's
// LastModified, or by the output cache from its entry. A zero date is
// something whose date is unknown, such as a category that was deleted,
// and then the response gets none, since a date that can miss a change
// would keep that change from the client; so does a date ahead of the
// clock. Call it before Write.
//
// An answer that changes without a newer date among what it shows, such as
// a list, which an item leaves when it is deleted, unpublished or past,
// gets no Last-Modified; its ETag still answers If-None-Match.
func Modified(w http.ResponseWriter, dates ...time.Time) {
	var latest time.Time

	for _, date := range dates {
		if date.IsZero() {
			return
		}

		if date.After(latest) {
			latest = date
		}
	}

	if latest.IsZero() || latest.After(time.Now()) {
		return
	}

	w.Header().Set("Last-Modified", latest.UTC().Format(http.TimeFormat))
}

// ItemDates are the dates of what the answer of a module's single item
// shows, for Modified: the item's last change, the revision it shows for
// content with revisions, the page the instance module is on
// (MountModified) and the media of its Markdown source and of the images
// ids (MediaModified). A module adds the dates of anything else the answer
// shows, such as the categories of a post:
//
//	api.Modified(w, api.ItemDates(r, s.key, item, text.Body, item.Fields.Image)...)
func ItemDates[F, T any](r *http.Request, module string, item content.Item[F, T], source string, ids ...string) []time.Time {
	dates := []time.Time{
		item.UpdatedAt,
		MountModified(r, module),
		MediaModified(r, source, ids...),
	}

	// A collection's items have no revision, and no date for one.
	if item.Revision.ID != "" {
		dates = append(dates, item.Revision.CreatedAt)
	}

	return dates
}

// MountModified is the date of the published page a module instance is on
// in the request's language, for the addresses of its content (see
// Modified): the latest change of that page and of the pages above it,
// whose slugs its address holds. It is zero while the instance is on no
// page, as the date of the page it was taken off is gone, and outside the
// API.
func MountModified(r *http.Request, module string) time.Time {
	current, ok := scopeOf(r)
	if !ok {
		return time.Time{}
	}

	ctx := r.Context()
	service := current.api.options.Pages

	address, err := service.MountPath(ctx, module, current.locale)
	if err != nil {
		return time.Time{}
	}

	table, err := service.Table(ctx)
	if err != nil {
		return time.Time{}
	}

	route, _, ok := table.Lookup(current.locale, address)
	if !ok {
		return time.Time{}
	}

	modified, err := current.api.pageModified(ctx, table, route)
	if err != nil {
		return time.Time{}
	}

	return modified
}

// MediaModified is the date of the media assets a text shows, the media
// references in its Markdown source, and of the images ids, such as a
// post's featured image (see Modified): the latest change of their files,
// alt texts and focal points. It is zero when one of them is not in the
// library, such as one that was deleted, and outside the API; without any
// media it is the oldest date there is, which changes nothing.
func MediaModified(r *http.Request, source string, ids ...string) time.Time {
	current, ok := scopeOf(r)
	if !ok {
		return time.Time{}
	}

	modified := time.Unix(0, 0).UTC()

	for _, id := range append(markdown.MediaIDs(source), ids...) {
		if id == "" {
			continue
		}

		asset, err := current.api.options.Media.Get(r.Context(), id)
		if err != nil {
			return time.Time{}
		}

		if asset.UpdatedAt.After(modified) {
			modified = asset.UpdatedAt
		}
	}

	return modified
}

// pageModified is the latest change of the published page at route and of
// the pages above it, zero when one of them is not published: the slugs of
// each make its address and their titles its breadcrumbs. Publishing a
// revision, such as one with another slug or parent, changes its date.
func (a *API) pageModified(ctx context.Context, table *pages.Table, route pages.Route) (time.Time, error) {
	var modified time.Time

	at := route

	for {
		page, err := a.options.Pages.Published(ctx, at)
		if err != nil {
			return time.Time{}, err
		}

		for _, date := range []time.Time{page.UpdatedAt, page.Revision.CreatedAt} {
			if date.After(modified) {
				modified = date
			}
		}

		if at.ParentID == "" {
			return modified, nil
		}

		// A page above that is not published leaves the date unknown.
		parent, ok := a.route(table, route.Locale, at.ParentID)
		if !ok {
			return time.Time{}, nil
		}

		at = parent
	}
}
