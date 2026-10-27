package pageadmin

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-partial/connector"
	"github.com/donseba/go-partial/exp/flash"
)

// movedAsDraft explains that a page dragged below another parent waits for
// publication before it moves on the website.
const movedAsDraft = "The new place is saved as a draft. Publish the page to move it on the website; its address changes then."

// moveInput is what the page tree posts when a page is dragged: the new
// parent, empty for none, and the place below it, from 0.
type moveInput struct {
	Parent string `form:"input,hidden" name:"parent"`
	Index  int    `form:"input,number" name:"index"  min:"0"`
}

// moveTo returns the handler behind dragging a page in the overview: it
// places the page at the posted index below the posted parent (see
// pages.Service.MoveTo) and shows the overview again. A reorder changes the
// menu at once, like the up and down buttons; a new parent is saved as a
// draft, which a warning points out. A refused place shows the reason.
func moveTo(e pageEditor, tree *pages.Service, modules func() []Module) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := e.Admin()

		locale, _, err := e.Locale(r)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		var in moveInput

		if errs, err := a.Bind(r, &in); err != nil || len(errs) > 0 {
			a.BadRequest(w, r)

			return
		}

		user, _ := auth.CurrentUser(r.Context())

		drafted, err := tree.MoveTo(r.Context(), r.PathValue("id"), in.Parent, in.Index, user.ID)
		if errors.Is(err, pages.ErrNotFound) {
			http.NotFound(w, r)

			return
		}

		if err != nil {
			message := issue(err)
			if message == "" {
				a.Fail(w, r, err)

				return
			}

			r = admin.WithError(r, a.T(r, message))
			overview(w, r, e, tree, modules(), http.StatusUnprocessableEntity)

			return
		}

		target := a.URL("admin.pages.index", url.Values{"locale": {locale}})

		if !drafted {
			a.Redirect(w, r, target)

			return
		}

		// The warning stays until it is dismissed. Without HTMX the browser
		// follows a redirect, which carries it.
		if !connector.IsRequest(a.Connector(), r) {
			a.RedirectFlash(w, r, target, flash.Warn(a.T(r, movedAsDraft)))

			return
		}

		r = r.WithContext(flash.Add(r.Context(), flash.Warn(a.T(r, movedAsDraft))))
		overview(w, r, e, tree, modules(), http.StatusOK)
	}
}
