package registrations

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
)

const (
	bodyLimit = 64 << 10
	// Visitors may post this many sign-ups or cancellations per window.
	throttleLimit  = 10
	throttleWindow = 10 * time.Minute
)

// Results of visitor posts, sent back as ?<key>=<result>.
const (
	// ResultRegistered is a sign-up that was taken.
	ResultRegistered = "registered"
	// ResultFull is a sign-up for more places than are left.
	ResultFull = "full"
	// ResultClosed is a sign-up for an event that takes none (any more).
	ResultClosed = "closed"
	// ResultInvalid is a sign-up with a missing or wrong field.
	ResultInvalid = "invalid"
	// ResultCancelled is a registration the visitor cancelled.
	ResultCancelled = "cancelled"
	// ResultThrottled is the answer to a visitor who posted too often.
	ResultThrottled = frontend.ResultThrottled
)

// results are the results a page shows; see frontend.Result.
var results = []string{ResultRegistered, ResultFull, ResultClosed, ResultInvalid, ResultCancelled, ResultThrottled}

// SignupInput is the sign-up form a visitor posts to /_/<key>/register: the
// subject, a name, an e-mail address, the number of adults (1 when empty)
// and children, and remarks. Themes render its fields with form_field, with
// their limits (Availability.Form), or write inputs with these names.
type SignupInput struct {
	Subject  string `form:"input,hidden" name:"subject"  required:"true" maxLength:"64"   pattern:"^[A-Za-z0-9_.:\\-]+$"`
	Name     string `form:"input,text"   name:"name"     required:"true" maxLength:"200"                                 label:"Name"           line:"true" autocomplete:"name"`
	Email    string `form:"input,email"  name:"email"    required:"true" maxLength:"254"                                 label:"E-mail address"             autocomplete:"email" validate:"mail"`
	Adults   int    `form:"input,number" name:"adults"                                                                   label:"Adults"                                                          min:"1" max:"50"`
	Children int    `form:"input,number" name:"children"                                                                 label:"Children"                                                        min:"0" max:"50"`
	Notes    string `form:"textarea"     name:"notes"                    maxLength:"2000"                                label:"Remarks"                                                                          placeholder:"Allergies, accessibility, questions…"`
}

// signupForm is the form of SignupInput for subject, posting to action,
// the instance's address for sign-ups (see Service.registerURL), for
// templates (frontend.VisitorForm): it returns to the element with the id
// key, and key goes before its fields' ids, such as signup-name. With left
// places, 0 when they are unlimited, the adults and the children are each
// at most left.
func signupForm(ctx context.Context, action, key, subject string, left int) form.RenderModel {
	model := frontend.VisitorForm(ctx, SignupInput{Subject: subject, Adults: 1}, action, key)

	if left > 0 {
		limit := strconv.Itoa(min(left, maxPlaces))

		model.Overrides = form.Overrides{
			"adults":   {"max": limit},
			"children": {"max": limit},
		}
	}

	return model
}

// CancelInput is the cancel form a visitor posts to /_/<key>/cancel: the
// registration's code and the e-mail address it was made with.
type CancelInput struct {
	Code  string `form:"input,hidden" name:"code"  required:"true" maxLength:"20"  line:"true"`
	Email string `form:"input,email"  name:"email" required:"true" maxLength:"254"             label:"The e-mail address you signed up with" autocomplete:"email"`
}

// cancelForm is the form of CancelInput for the registration with code,
// posting to action, the instance's address for cancellations, for
// templates (frontend.VisitorForm), as signupForm.
func cancelForm(ctx context.Context, action, key, code string) form.RenderModel {
	return frontend.VisitorForm(ctx, CancelInput{Code: code}, action, key)
}

// actions receive the sign-up and cancel forms at /_/<key>/.
type actions struct {
	service *Service
	// absolute makes links in e-mail absolute (webthings.Env.AbsoluteURL).
	absolute func(r *http.Request, path string) string
}

// registerRoutes registers the actions on the instance's action router
// (webthings.Env.Actions).
func (a actions) registerRoutes() {
	routes := a.service.actions

	routes.Use(frontend.Limits(a.service.key, frontend.ActionLimits{
		Body:   bodyLimit,
		Posts:  throttleLimit,
		Window: throttleWindow,
	}))
	routes.Post("/register", a.register).As("register")
	routes.Post("/cancel", a.cancel).As("cancel")
}

func (a actions) register(w http.ResponseWriter, r *http.Request) {
	key := a.service.key

	// Bots that fill in the trap are told it worked.
	if frontend.IsSpam(r) {
		frontend.Back(w, r, key, ResultRegistered)

		return
	}

	ctx := r.Context()

	// Without the field, one adult registers.
	in := SignupInput{Adults: 1}

	// The service checks the sign-up again, the number of places in total
	// among it, for code that registers without a form.
	if errs, err := a.service.frontend.BindForm(r, &in); err != nil || len(errs) > 0 {
		a.service.frontend.KeepForm(w, r, key, in, errs)
		frontend.Back(w, r, key, ResultInvalid)

		return
	}

	locale := a.service.frontend.BackLocale(r)

	registration, err := a.service.Register(ctx, Signup{
		SubjectID: in.Subject,
		Name:      in.Name,
		Email:     in.Email,
		Adults:    in.Adults,
		Children:  in.Children,
		Notes:     in.Notes,
		Locale:    locale,
	})

	switch {
	case errors.Is(err, ErrInvalid):
		a.service.frontend.KeepForm(w, r, key, in, nil)
		frontend.Back(w, r, key, ResultInvalid)

		return
	case errors.Is(err, ErrClosed):
		frontend.NothingChanged(r)
		frontend.Back(w, r, key, ResultClosed)

		return
	case errors.Is(err, ErrFull):
		a.service.frontend.KeepForm(w, r, key, in, nil)
		frontend.Back(w, r, key, ResultFull)

		return
	case err != nil:
		a.service.fail(w, r, err)

		return
	}

	page := a.service.lookupURL(ctx, locale, registration.Code)

	link := ""
	if page != "" {
		link = a.absolute(r, page)
	}

	a.service.sendMail(ctx, mailConfirmation, registration, link)

	// Without a page for registrations, the form's page shows the code.
	if page == "" {
		frontend.BackWith(w, r, key, ResultRegistered, url.Values{"code": {registration.Code}})

		return
	}

	http.Redirect(w, r, page+"?"+url.Values{key: {ResultRegistered}}.Encode(), http.StatusSeeOther)
}

func (a actions) cancel(w http.ResponseWriter, r *http.Request) {
	key := a.service.key

	if frontend.IsSpam(r) {
		frontend.Back(w, r, key, ResultCancelled)

		return
	}

	ctx := r.Context()

	var in CancelInput

	if errs, err := a.service.frontend.BindForm(r, &in); err != nil || len(errs) > 0 {
		a.service.frontend.KeepForm(w, r, key, in, errs)
		frontend.Back(w, r, key, ResultInvalid)

		return
	}

	registration, err := a.service.ByCode(ctx, in.Code)
	if errors.Is(err, ErrNotFound) {
		a.service.frontend.KeepForm(w, r, key, in, nil)
		frontend.Back(w, r, key, ResultInvalid)

		return
	}

	if err != nil {
		a.service.fail(w, r, err)

		return
	}

	// The e-mail address confirms that the visitor owns the code.
	if !strings.EqualFold(registration.Email, in.Email) {
		a.service.frontend.KeepForm(w, r, key, in, nil)
		frontend.Back(w, r, key, ResultInvalid)

		return
	}

	if !registration.Cancelled() {
		if err := a.service.Cancel(ctx, registration.ID); err != nil {
			a.service.fail(w, r, err)

			return
		}

		a.service.sendMail(ctx, mailCancellation, registration, "")
	}

	frontend.Back(w, r, key, ResultCancelled)
}
