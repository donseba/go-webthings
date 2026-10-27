package admin

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"time"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-partial/exp/localization"
)

// localizer is the localizer of admin requests: the admin language, and the
// site's timezone, in which go-form shows date-times (datetime-local inputs)
// as a form.TimeLocalizer.
type localizer struct {
	Locale

	zone func() *time.Location
}

// Location is the site's timezone.
func (l localizer) Location() *time.Location {
	return l.zone()
}

// localizer returns the localizer of a request with ctx. The site's timezone
// is looked up once, when a form first needs it.
func (a *Admin) localizer(ctx context.Context) localizer {
	return localizer{
		Locale: a.locale,
		zone: sync.OnceValue(func() *time.Location {
			return a.location(ctx)
		}),
	}
}

// MapForm maps a submitted form onto input, a pointer to the form's struct,
// strictly: a value that cannot be read as its field's type, such as "abc"
// for a number, a date in the wrong format or a choice the form does not
// offer, is not mapped and comes back as a field error, translated, for the
// form to show at its field. Date-times of datetime-local inputs are read
// in the site's timezone (Location), in which the admin shows them;
// time.Time fields so hold the instant the editor meant. A time the clocks
// skip moves forward by the change and a time they show twice is the first
// of the two (see form.ParseDateTimeLocal). err is only set
// when the submission cannot be mapped at all, such as a multiple choice
// with an option the form does not offer: answer it with BadRequest.
func (a *Admin) MapForm(r *http.Request, input any) (form.FieldErrors, error) {
	options := form.MapOptions{Location: a.Location(r)}

	return a.forms.MapFormLocalized(r, input, options, localization.FromContext(r.Context()))
}

// Bind maps a submitted form onto input like MapForm and validates it like
// Validate (go-form's Bind): the field errors of both, one field's
// unreadable value taking the place of what validation says about the value
// it left. err is MapForm's:
//
//	errs, err := a.Bind(r, &input)
//	if err != nil {
//		a.BadRequest(w, r)
//
//		return
//	}
//
//	if len(errs) > 0 {
//		// Show the form again with errs.
//	}
func (a *Admin) Bind(r *http.Request, input any) (form.FieldErrors, error) {
	options := form.MapOptions{Location: a.Location(r)}

	return a.forms.Bind(r, input, options, localization.FromContext(r.Context()))
}

// ValidateSchema checks the values of a form defined at runtime, such as
// the settings a theme declares, as Validate checks a struct.
func (a *Admin) ValidateSchema(r *http.Request, schema form.Schema, values url.Values) form.FieldErrors {
	return a.forms.ValidateSchemaLocalized(schema, values, localization.FromContext(r.Context()))
}
