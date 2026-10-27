package registrations

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
	"github.com/donseba/go-router/middleware"

	"golang.org/x/text/currency"
)

//go:embed templates translations
var files embed.FS

// Option configures an instance.
type Option func(*options)

type options struct {
	events   string
	currency string
}

// ForEvents links the instance to the events instance with key: editors pick
// its events in the admin, e-mail names the event, and registration closes
// when an event starts unless a deadline is set. Add that events instance to
// the app as well.
func ForEvents(key string) Option {
	return func(o *options) {
		o.events = key
	}
}

// Currency sets the ISO 4217 code of the prices, for example "GBP"; the
// default is "EUR". An unknown code stops the app from starting.
func Currency(code string) Option {
	return func(o *options) {
		o.currency = strings.ToUpper(strings.TrimSpace(code))
	}
}

// Module adds a Registrations instance. key is used in URLs of the admin and
// in form actions and must be unique on the site; title is shown in the admin
// and the page editor.
func Module(key, title string, opts ...Option) webthings.Module {
	config := options{currency: "EUR"}

	for _, opt := range opts {
		opt(&config)
	}

	return webthings.Module{
		Type:   "registrations",
		Key:    key,
		Title:  title,
		Schema: schema(),
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			return setup(env, config)
		},
		Funcs: funcs(),
		// The visitor forms themes render with form_field.
		Forms: []any{SignupInput{}, CancelInput{}, LookupInput{}},
		Templates: []frontend.Template{
			{Name: "lookup", Data: LookupView{}, Example: lookupExample},
			{Name: "registration", Data: RegistrationView{}, Example: registrationExample},
		},
		Mail:       mailTemplates(),
		Components: components(),
	}
}

func setup(env webthings.Env, config options) (webthings.Parts, error) {
	unit, err := currency.ParseISO(config.currency)
	if err != nil {
		return webthings.Parts{}, fmt.Errorf("registrations %q: currency %q is not an ISO 4217 code", env.Key, config.currency)
	}

	config.currency = unit.String()

	service := &Service{
		key:      env.Key,
		db:       env.DB,
		site:     env.Site,
		pages:    env.Pages,
		frontend: env.Frontend,
		mail:     env.Mail,
		currency: config.currency,
		actions:  env.Actions(),
		changed:  env.Changed,
	}

	parts := webthings.Parts{}

	if config.events != "" {
		service.events = linkedEvents(env.Service, config.events)
		parts.StructuredData = service.offers(config.events)
	}

	visitor := actions{service: service, absolute: env.AbsoluteURL}
	visitor.registerRoutes()

	service.public = service.handler()

	parts.Admin = []admin.Section{section(env, service)}
	parts.Public = service.public
	parts.Service = service

	return parts, nil
}

// Of returns the Registrations instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// Subject describes a subject in locale: the editor's title, or the event's.
func (s *Service) Subject(ctx context.Context, subjectID, locale string) (Subject, error) {
	subject := Subject{ID: subjectID}

	if s.linked() {
		event, err := s.event(ctx, subjectID, locale)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return subject, err
		}

		if err == nil {
			subject = event
		}
	}

	settings, err := s.Settings(ctx, subjectID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return subject, err
	}

	if settings.Title != "" {
		subject.Title = settings.Title
	}

	if subject.Title == "" {
		subject.Title = subjectID
	}

	return subject, nil
}

// LookupView is the data of registrations/lookup.gohtml: a form where
// visitors enter their code.
type LookupView struct {
	// Title is the title of the page the module is mounted on.
	Title string
	// Action is the address the form sends its code field to, with method get.
	Action string
	// Code is the code that was looked up, if any.
	Code string
	// NotFound is set when no registration has Code.
	NotFound bool
	// Form is the lookup form's model (LookupInput, with Code), sent to
	// Action with method get, for go-form's template functions, such as
	// {{form_open .Form}} and {{form_field .Form "code" nil}}.
	Form form.RenderModel
}

// LookupInput is the lookup form, LookupView.Form: the code from the
// confirmation, sent with method get as ?code=. Its field's id is
// registration-code.
type LookupInput struct {
	Code string `form:"input,text" name:"code" required:"true" maxLength:"20" line:"true" label:"Registration code" placeholder:"K7Q4-9M2X" autocomplete:"off"`
}

// lookupForm is the form of LookupInput with code, sent to action.
func lookupForm(action, code string) form.RenderModel {
	return form.WithInfo(LookupInput{Code: code}, form.Info{
		Target:   action,
		Method:   http.MethodGet,
		IDPrefix: "registration",
	})
}

// RegistrationView is the data of registrations/registration.gohtml.
type RegistrationView struct {
	// Key is the instance key; results arrive as ?<key>=<result>, and the
	// cancel form returns the visitor to the element with the id Key.
	Key string
	// Title is the title of the page the module is mounted on.
	Title string
	// Subject is what the visitor registered for; Start is set for events.
	Subject  Subject
	Code     string
	Name     string
	Adults   int
	Children int
	// Total is in Currency, for {{money .Total .Currency}}.
	Total    Cents
	Currency string
	// Payment is none, pending, paid or cancelled.
	Payment PaymentStatus
	// PaymentLink is the editor's payment link while payment is pending.
	PaymentLink string
	// Notes are the visitor's own remarks; Info the editor's notes for
	// registrants, in the page's language, rendered from Markdown.
	Notes string
	Info  template.HTML
	// CreatedAt is when the visitor registered, for {{date .CreatedAt}}.
	CreatedAt time.Time
	Cancelled bool
	// CancelAction is the address the cancel form posts to (fields code and
	// email); empty when the registration is cancelled.
	CancelAction string
	// Form is the cancel form's model (CancelInput, with Code) for go-form's
	// template functions, such as {{form_open .Form}} and {{form_field
	// .Form "email" nil}}; the zero value when the registration is
	// cancelled.
	Form form.RenderModel
	// Result is the outcome of the visitor's last post: "registered",
	// "cancelled", "invalid", "throttled" or empty.
	Result string
}

// handler serves the lookup pages below the page the module is mounted on.
// The redirects to a registration go to its route by name (router.For).
func (s *Service) handler() *router.Router {
	// Paths without a route get the site's 404 page.
	routes := s.frontend.Router()

	// Registration pages show personal details and their addresses hold
	// the code, so no cache may keep them, no search engine index them and
	// no statistic or list of missed paths record them. That holds for "not
	// found" too, for a mistyped code: UseAll, not Use, puts the headers on
	// every answer, also the 404 of a path without a route.
	routes.UseAll(
		middleware.CacheControl("private, no-store"),
		middleware.SecurityHeaders(middleware.SecurityHeadersOptions{XRobotsTag: "noindex"}),
	)

	routes.Get("/{$}", s.lookupPage).As("lookup")
	routes.Get("/{code}", s.registrationPage).As("registration")

	return routes
}

func (s *Service) lookupPage(w http.ResponseWriter, r *http.Request) {
	if code := r.URL.Query().Get("code"); code != "" {
		if clean := normalizeCode(code); clean != "" {
			s.redirect(w, r, clean)

			return
		}

		s.renderLookup(w, r, code, http.StatusNotFound)

		return
	}

	s.renderLookup(w, r, "", http.StatusOK)
}

// redirect sends the visitor to the page of the registration with code.
func (s *Service) redirect(w http.ResponseWriter, r *http.Request, code string) {
	target, err := s.public.For(r).RoutePathArgs("registration", "code", code)
	if err != nil {
		s.fail(w, r, err)

		return
	}

	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Service) renderLookup(w http.ResponseWriter, r *http.Request, code string, status int) {
	action := s.public.For(r).RoutePath("lookup")
	view := LookupView{
		Title:    frontend.MountFrom(r.Context()).Title,
		Action:   action,
		Code:     code,
		NotFound: code != "",
		Form:     lookupForm(action, code),
	}

	s.frontend.Render(w, r, frontend.Content{
		Template: "lookup",
		Title:    view.Title,
		Data:     view,
		Status:   status,
	})
}

func (s *Service) registrationPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := frontend.MountFrom(ctx).Locale

	registration, err := s.ByCode(ctx, r.PathValue("code"))
	if errors.Is(err, ErrNotFound) {
		s.renderLookup(w, r, r.PathValue("code"), http.StatusNotFound)

		return
	}

	if err != nil {
		s.fail(w, r, err)

		return
	}

	// Codes typed in lowercase or without the dash land on the canonical URL.
	if r.PathValue("code") != registration.Code {
		s.redirect(w, r, registration.Code)

		return
	}

	view, err := s.registrationView(ctx, registration, locale)
	if err != nil {
		s.fail(w, r, err)

		return
	}

	view.Key = s.key
	view.Title = frontend.MountFrom(r.Context()).Title
	view.Result = frontend.Result(r, s.key, results...)

	if !registration.Cancelled() {
		view.CancelAction = s.actions.RoutePath("cancel")
		view.Form = cancelForm(ctx, view.CancelAction, s.key, registration.Code)
	}

	s.frontend.Render(w, r, frontend.Content{
		Template: "registration",
		Title:    view.Title,
		Data:     view,
	})
}

func (s *Service) registrationView(ctx context.Context, registration Registration, locale string) (RegistrationView, error) {
	subject, err := s.Subject(ctx, registration.SubjectID, locale)
	if err != nil {
		return RegistrationView{}, err
	}

	settings, err := s.Settings(ctx, registration.SubjectID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return RegistrationView{}, err
	}

	text := s.text(ctx, settings, locale)
	view := RegistrationView{
		Subject:   subject,
		Code:      registration.Code,
		Name:      registration.Name,
		Adults:    registration.Adults,
		Children:  registration.Children,
		Total:     registration.Total,
		Currency:  s.currency,
		Payment:   registration.Payment,
		Notes:     registration.Notes,
		Info:      s.renderNotes(ctx, text.Notes),
		CreatedAt: registration.CreatedAt,
		Cancelled: registration.Cancelled(),
	}

	if registration.Payment == PaymentPending {
		view.PaymentLink = text.PaymentLink
	}

	return view, nil
}

// lookupURL is the registration's page when the module is placed on a page
// in locale, or "".
func (s *Service) lookupURL(ctx context.Context, locale, code string) string {
	page, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil || page == "" {
		return ""
	}

	// The code is the only parameter of the route, so it has a path.
	path, _ := s.public.At(page).RoutePathArgs("registration", "code", code)

	return path
}

// fail answers a request that failed with the theme's error page
// (frontend.Frontend.Fail), which logs err.
func (s *Service) fail(w http.ResponseWriter, r *http.Request, err error) {
	s.frontend.Fail(w, r, fmt.Errorf("registrations: %w", err))
}

// lookupExample is the starting point for registrations/lookup.gohtml.
const lookupExample = `{{/* @dot github.com/donseba/go-webthings/modules/registrations.LookupView */}}
{{/* Starting point generated by WebThings: find a registration by its code. */}}
<main id="content">
    <h1>{{.Title}}</h1>

    {{if .NotFound}}<p>No registration has the code {{.Code}}.</p>{{end}}

    {{form_open .Form}}
        {{form_field .Form "code" nil}}
        <button type="submit">Look up</button>
    {{form_close}}
</main>
`

// registrationExample is the starting point for
// registrations/registration.gohtml.
const registrationExample = `{{/* @dot github.com/donseba/go-webthings/modules/registrations.RegistrationView */}}
{{/* Starting point generated by WebThings: one registration with its cancel form. */}}
<main id="content">
    <h1>{{.Subject.Title}}</h1>

    {{if eq .Result "registered"}}<p>Thank you, you are registered. We sent you a confirmation.</p>{{end}}

    {{/* When the event takes place (frontend.When); zero for other subjects. */}}
    {{with .Subject.When}}{{if not .IsZero}}
        <p>{{if .AllDay}}{{dateRange .Start .End "full"}}{{else if .SameDay}}{{date .Start "full"}} · {{timeRange .Start .End}}{{else}}{{date .Start "full"}}, {{time .Start}} – {{date .End "full"}}, {{time .End}}{{end}}</p>
    {{end}}{{end}}

    <dl>
        <dt>Code</dt><dd>{{.Code}}</dd>
        <dt>Name</dt><dd>{{.Name}}</dd>
        <dt>Adults</dt><dd>{{.Adults}}</dd>
        {{if .Children}}<dt>Children</dt><dd>{{.Children}}</dd>{{end}}
        {{if .Total}}<dt>Total</dt><dd>{{money .Total .Currency}} ({{.Payment}})</dd>{{end}}
        {{with .Notes}}<dt>Your notes</dt><dd>{{.}}</dd>{{end}}
        <dt>Registered on</dt><dd>{{date .CreatedAt}}</dd>
    </dl>

    {{with .PaymentLink}}<p>Payment: {{.}}</p>{{end}}
    {{with .Info}}<div class="registration-info">{{.}}</div>{{end}}

    {{/* The cancel form returns the visitor here, to the element with the id {{.Key}}. */}}
    <section id="{{.Key}}">
        {{if eq .Result "cancelled"}}<p>Your registration is cancelled.</p>{{end}}
        {{if eq .Result "invalid"}}<p>That e-mail address does not match this registration.</p>{{end}}
        {{if eq .Result "throttled"}}<p>Too many attempts. Please try again later.</p>{{end}}

        {{if .Cancelled}}
            <p>This registration is cancelled.</p>
        {{else}}
            {{form_open .Form}}
                {{form_field .Form "code" nil}}
                {{form_field .Form "email" nil}}

                <button type="submit">Cancel registration</button>
            {{form_close}}
        {{end}}
    </section>
</main>
`
