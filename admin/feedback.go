package admin

import (
	"net/http"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-partial/exp/flash"
)

// The admin tells editors why a post was refused in one of two places. A
// problem with one field shows at that field, next to what they typed:
// render the form again with FieldError (or the errors of Admin.Validate)
// and status 422. A problem with the whole request, such as a file that
// cannot be read, shows as an error toast: render the page with WithError.
// Successful posts redirect with Admin.Redirect, Admin.RedirectNotice or
// Admin.RedirectFlash, whose notice shows as a toast on the next page.

// FieldError is the refusal of one form field, shown at the field by
// form_render_localized: field is the field's name in the form's input
// struct, such as "Email", and text the message, translated already, such
// as Admin.T(r, "Enter a valid email address.").
func FieldError(field, text string) form.FieldErrors {
	return form.FieldErrors{form.FieldValidationError{Field: field, Err: text}}
}

// WithError returns r with text, translated already, as an error toast on
// the page rendered for it (see Admin.Render); an empty text returns r
// unchanged. Error toasts stay until the editor dismisses them and are
// announced by screen readers at once.
func WithError(r *http.Request, text string) *http.Request {
	if text == "" {
		return r
	}

	return r.WithContext(flash.Add(r.Context(), flash.Error(text)))
}
