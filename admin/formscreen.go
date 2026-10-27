package admin

import (
	"context"
	"net/http"

	form "github.com/donseba/go-form/v2"
)

// FormScreen is a page with one form, such as the settings of a module
// instance. Show fills the form with Load; Post reads and validates it,
// stores it with Save and returns to the page with "Changes saved.". Field
// errors, of the validation or of Save, show at their fields. Register both
// on the section's routes, with the rights the module asks:
//
//	settings := admin.FormScreen[settingsInput]{Admin: routes.Admin(), …}
//	routes.Get("/settings", admin.View, settings.Show).As("settings")
//	routes.Post("/settings", admin.Edit, settings.Post)
type FormScreen[In any] struct {
	Admin *Admin
	// Title is the page heading: a translation key such as "Settings".
	Title string
	// BackURL links back to where the screen was opened from, such as the
	// module's list, with BackLabel as text (a translation key, or a name
	// chosen by the site owner, which shows as written).
	BackURL   string
	BackLabel string
	// Heading is the title of the form's box and Intro a muted paragraph
	// above the form, both translation keys; either may be empty.
	Heading string
	Intro   string
	// Action is where the form posts to, and where a saved form returns.
	// Empty is the address the screen is shown at, so a screen whose Show
	// and Post share an address, as above, needs none.
	Action string
	// Submit is the label of the button, a translation key; "Save" when
	// empty.
	Submit string
	// Load returns the stored values the form shows.
	Load func(ctx context.Context) (In, error)
	// Save stores a valid post. Refusals of single fields come back as
	// field errors (see FieldError); other errors fail the request.
	Save func(r *http.Request, in In) (form.FieldErrors, error)
}

// FormScreenView is the data of templates/form-screen.gohtml.
type FormScreenView struct {
	Title     string
	BackURL   string
	BackLabel string
	Heading   string
	Intro     string
	Form      form.RenderModel
	Errors    form.FieldErrors
}

// Show renders the form with the stored values.
func (s FormScreen[In]) Show(w http.ResponseWriter, r *http.Request) {
	in, err := s.Load(r.Context())
	if err != nil {
		s.Admin.Fail(w, r, err)

		return
	}

	s.render(w, r, in, nil, http.StatusOK)
}

// Post saves the posted form, or shows it again with its errors.
func (s FormScreen[In]) Post(w http.ResponseWriter, r *http.Request) {
	var in In

	// Admin.MapForm reads date-times in the site's timezone, the zone the
	// form shows them in.
	errs, err := s.Admin.Bind(r, &in)
	if err != nil {
		s.Admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		s.render(w, r, in, errs, http.StatusUnprocessableEntity)

		return
	}

	errs, err = s.Save(r, in)
	if err != nil {
		s.Admin.Fail(w, r, err)

		return
	}

	if len(errs) > 0 {
		s.render(w, r, in, errs, http.StatusUnprocessableEntity)

		return
	}

	s.Admin.RedirectNotice(w, r, s.action(r), "Changes saved.")
}

// action is Action, or the address of the request.
func (s FormScreen[In]) action(r *http.Request) string {
	if s.Action != "" {
		return s.Action
	}

	return r.URL.RequestURI()
}

// render shows the form with in and errs, answering with status.
func (s FormScreen[In]) render(w http.ResponseWriter, r *http.Request, in In, errs form.FieldErrors, status int) {
	submit := s.Submit
	if submit == "" {
		submit = "Save"
	}

	s.Admin.Render(w, r, Page{
		Title:    s.Title,
		Template: "templates/form-screen.gohtml",
		Status:   status,
		Data: FormScreenView{
			Title:     s.Title,
			BackURL:   s.BackURL,
			BackLabel: s.BackLabel,
			Heading:   s.Heading,
			Intro:     s.Intro,
			Form:      s.Admin.Form(r, in, s.action(r), submit),
			Errors:    errs,
		},
	})
}
