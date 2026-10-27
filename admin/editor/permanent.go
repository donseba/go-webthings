package editor

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/content"
)

// Permanent items stay on the website. Some are permanent by their nature
// (Config.Permanent, such as the home page); administrators mark others in
// their editor (admin.Permanence). The editor offers no unpublish, no
// planned unpublication and no delete for them, and refuses those changes
// when they are posted anyway, such as from a page opened before the item
// became permanent.

// ErrPermanent refuses a change that would take an item that is permanent
// by its nature (Config.Permanent) off the website. Config.Issue can name
// it, as the page tree does for the home page; the default message is
// general.
var ErrPermanent = errors.New("permanent_item")

// errMarked refuses such a change for an item an administrator marked
// permanent.
var errMarked = errors.New("permanent_mark")

const (
	// permanentFixed explains items that are permanent by their nature.
	permanentFixed = "This item always stays on the website: it cannot be unpublished, scheduled to be unpublished or deleted."
	// permanentMarked explains items an administrator marked permanent.
	permanentMarked = "This item is permanent: it cannot be unpublished, scheduled to be unpublished or deleted " +
		"until an administrator removes the mark."
)

// permanence reports whether item is permanent, and whether it is so by its
// nature (fixed) rather than by an administrator's mark.
func (e Editor[F, T, I]) permanence(ctx context.Context, item content.Item[F, T]) (permanent, fixed bool, err error) {
	if item.ID == "" {
		return false, false, nil
	}

	if e.config.Permanent != nil && e.config.Permanent(item) {
		return true, true, nil
	}

	marks := e.admin.Permanence()
	if marks == nil {
		return false, false, nil
	}

	permanent, err = marks.Permanent(ctx, e.config.ID, item.ID)

	return permanent, false, err
}

// RefusePermanent shows the editor with the reason when the item in the
// URL is permanent, for a change that would take it off the website, and
// reports whether it did. Module screens (Config.Routes) that take an item
// off the website call it first, as the editor's own do.
func (e Editor[F, T, I]) RefusePermanent(w http.ResponseWriter, r *http.Request) bool {
	item, err := e.config.Store.Load(r.Context(), r.PathValue("id"), "")
	if err != nil {
		e.fail(w, r, err)

		return true
	}

	permanent, fixed, err := e.permanence(r.Context(), item)
	if err != nil {
		e.admin.Fail(w, r, err)

		return true
	}

	if !permanent {
		return false
	}

	if fixed {
		e.refuse(w, r, ErrPermanent, e.fixedMessage(r))

		return true
	}

	e.refuse(w, r, errMarked, e.admin.T(r, permanentMarked))

	return true
}

// fixedMessage explains that an item is permanent by its nature: Issue's
// message for ErrPermanent, or a general one.
func (e Editor[F, T, I]) fixedMessage(r *http.Request) string {
	if message := e.Issue(r, ErrPermanent); message != "" {
		return message
	}

	return e.admin.T(r, permanentFixed)
}

// permanenceForm offers administrators to set or remove the permanent mark,
// with a confirmation, when the admin keeps marks and the item is not
// permanent by its nature.
func (e Editor[F, T, I]) permanenceForm(r *http.Request, item content.Item[F, T], locale string, view *EditView) {
	user, _ := auth.CurrentUser(r.Context())
	if e.admin.Permanence() == nil || view.PermanentFixed || !user.IsAdmin() || !e.admin.Can(r, e.permission(admin.Edit)) {
		return
	}

	locales := url.Values{"locale": {locale}}

	model := e.admin.Confirm(r,
		e.admin.Form(r, struct{}{}, e.admin.URL(e.route("permanent"), "id", item.ID, locales), "Make permanent"),
		"Make this item permanent? It can then no longer be unpublished, scheduled to be unpublished or deleted.",
	)

	if view.Permanent {
		model = e.admin.Confirm(r,
			e.admin.Form(r, struct{}{}, e.admin.URL(e.route("permanent-off"), "id", item.ID, locales), "Remove permanent mark"),
			"Remove the permanent mark? Editors with the right to publish or delete can then take this item off the website.",
		)
	}

	view.Permanence = &model
}

// setPermanent returns the handler that marks the item in the URL
// permanent, or removes the mark, for administrators (see
// admin.AdministratorsOnly on its route). An item with a
// planned unpublication is not marked until the plan is cancelled; items
// that are permanent by their nature keep their protection.
func (e Editor[F, T, I]) setPermanent(permanent bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		marks := e.admin.Permanence()
		if marks == nil {
			http.NotFound(w, r)

			return
		}

		locale, _, err := e.Locale(r)
		if err != nil {
			e.admin.Fail(w, r, err)

			return
		}

		item, err := e.config.Store.Load(r.Context(), r.PathValue("id"), "")
		if err != nil {
			e.fail(w, r, err)

			return
		}

		if e.config.Permanent != nil && e.config.Permanent(item) {
			e.refuse(w, r, ErrPermanent, e.fixedMessage(r))

			return
		}

		if permanent && !item.Schedule.UnpublishAt.IsZero() {
			e.refuse(
				w,
				r,
				errMarked,
				e.admin.T(r, "Cancel the planned unpublication first: a permanent item stays on the website."),
			)

			return
		}

		if err := marks.SetPermanent(r.Context(), e.config.ID, item.ID, permanent); err != nil {
			e.admin.Fail(w, r, err)

			return
		}

		notice := "This item is no longer permanent."
		if permanent {
			notice = "This item is now permanent."
		}

		e.admin.RedirectNotice(w, r, e.itemURL(r.Context(), item.ID, locale), notice)
	}
}
