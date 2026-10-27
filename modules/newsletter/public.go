package newsletter

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/mail"

	form "github.com/donseba/go-form/v2"
)

const bodyLimit = 16 << 10

// SignupView is the data of newsletter/signup.gohtml and of the newsletter
// template function.
type SignupView struct {
	// Key is the instance key; results arrive as ?<key>=<result>.
	Key   string
	Title string
	Intro template.HTML
	// Action is the address the signup form posts to.
	Action string
	// Result is "check-mail", "confirmed", "unsubscribed", "invalid",
	// "throttled" or empty.
	Result string
	// Form is the signup form's model (SubscribeInput) for go-form's
	// template functions, such as {{form_open .Form}} and {{form_field
	// .Form "email" nil}}; it returns the visitor to the element with the
	// id Key.
	Form form.RenderModel
}

// SubscribeInput is the signup form a visitor posts to /_/<key>/subscribe:
// an e-mail address, optionally a name, and the language of the mail, a
// hidden field that SignupView.Form sets to the page's language. Themes
// render its fields with form_field, with their limits (SignupView.Form),
// or write inputs with these names; without a locale the mail is in the
// language of the page the form was on.
type SubscribeInput struct {
	Email  string `form:"input,email"  name:"email"  label:"E-mail address" required:"true" maxLength:"254" validate:"mail" autocomplete:"email" placeholder:"you@example.com"`
	Name   string `form:"input,text"   name:"name"   label:"Name"                           maxLength:"200"                 autocomplete:"name"                                line:"true"`
	Locale string `form:"input,hidden" name:"locale"                                        maxLength:"35"                                                                     line:"true"`
}

// view builds the signup data in locale, with the result of the current request.
func (s *Service) view(ctx context.Context, r *http.Request, locale string) SignupView {
	text := s.Text(ctx, locale)

	return SignupView{
		Key:    s.key,
		Title:  text.Title,
		Intro:  s.frontend.Markdown(ctx, text.Intro),
		Action: s.subscribeURL(),
		Result: result(r, s.key),
		Form:   frontend.VisitorForm(ctx, SubscribeInput{Locale: locale}, s.subscribeURL(), s.key),
	}
}

func (s *Service) signupPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mount := frontend.MountFrom(ctx)
	view := s.view(ctx, r, mount.Locale)

	s.frontend.Render(w, r, frontend.Content{
		Template: "signup",
		Title:    view.Title,
		Data:     view,
	})
}

// result is the visitor result in the request's query, or "" when there is
// none or it is not one of ours.
func result(r *http.Request, key string) string {
	if r == nil {
		return ""
	}

	return frontend.Result(r, key, ResultCheckMail, ResultConfirmed, ResultUnsubscribed, ResultInvalid, ResultThrottled)
}

// subscribe handles the signup form. Every valid address gets the same answer,
// so the form does not reveal who is on the list.
func (s *Service) subscribe(w http.ResponseWriter, r *http.Request) {
	// No page shows the subscribers, so a signup leaves the output cache.
	frontend.NothingChanged(r)

	if frontend.IsSpam(r) {
		frontend.Back(w, r, s.key, ResultCheckMail)

		return
	}

	ctx := r.Context()

	var in SubscribeInput

	errs, err := s.frontend.BindForm(r, &in)
	if err != nil || len(errs) > 0 {
		s.frontend.KeepForm(w, r, s.key, in, errs)
		frontend.Back(w, r, s.key, ResultInvalid)

		return
	}

	email := normalizeEmail(in.Email)
	name := strings.Join(strings.Fields(in.Name), " ")

	// A backstop: the form's mail rule (validate:"mail") already refuses an
	// address mail cannot be sent to, with a message at the field.
	if !mail.Valid(email) {
		s.frontend.KeepForm(w, r, s.key, in, nil)
		frontend.Back(w, r, s.key, ResultInvalid)

		return
	}

	subscriber, send, err := s.register(ctx, email, name, s.visitorLocale(r, in.Locale))
	if err != nil {
		s.frontend.Fail(w, r, fmt.Errorf("newsletter signup: %w", err))

		return
	}

	if send {
		s.sendConfirmation(r, subscriber)
	}

	frontend.Back(w, r, s.key, ResultCheckMail)
}

// visitorLocale is posted, the locale field of the form, when it is an
// enabled language, else the language of the page the form was on, else the
// site's default language.
func (s *Service) visitorLocale(r *http.Request, posted string) string {
	config, err := s.locales.Configuration(r.Context())
	if err != nil {
		return ""
	}

	if locale, err := config.Require(posted); err == nil {
		return locale
	}

	return s.frontend.BackLocale(r)
}

// mailConfirmation is the mail template that asks a new subscriber to
// confirm, see webthings.Module.Mail.
const mailConfirmation = "confirmation"

// ConfirmationView is the data of the mail template confirmation, in the
// subscriber's language.
type ConfirmationView struct {
	Email string
	// Name is empty when the subscriber gave none.
	Name string
	// Title is the newsletter's title in the subscriber's language.
	Title string
	// URL confirms the subscription.
	URL string
}

const confirmationMail = `{{/* @dot github.com/donseba/go-webthings/modules/newsletter.ConfirmationView */}}
{{/* Sent to a new subscriber; only confirmed subscribers receive mailings. */}}
{{define "subject"}}{{tl localizer "Please confirm your subscription"}}{{end}}

{{define "text"}}
{{with .Name}}{{tl localizer "Hello %s," .}}

{{end -}}
{{tl localizer "Thank you for subscribing to our newsletter. Please confirm your e-mail address with the link below."}}

{{.URL}}

{{tl localizer "If you did not subscribe, you can ignore this message."}}
{{end}}
`

// sendConfirmation mails the confirmation link. A failure is logged only: the
// visitor can simply sign up again.
func (s *Service) sendConfirmation(r *http.Request, subscriber Subscriber) {
	ctx := r.Context()
	view := ConfirmationView{
		Email: subscriber.Email,
		Name:  subscriber.Name,
		Title: s.Text(ctx, subscriber.Locale).Title,
		URL:   s.absolute(r, s.tokenPath("confirm", subscriber.Token)),
	}

	_ = s.frontend.SendMail(ctx, s.mail, frontend.Letter{
		Locale: subscriber.Locale,
		Key:    s.key,
		Name:   mailConfirmation,
		Data:   view,
		To:     []string{subscriber.Email},
	})
}

func (s *Service) confirm(w http.ResponseWriter, r *http.Request) {
	subscriber, err := s.Confirm(r.Context(), r.URL.Query().Get("token"))
	s.land(w, r, subscriber, err, ResultConfirmed)
}

func (s *Service) unsubscribe(w http.ResponseWriter, r *http.Request) {
	subscriber, err := s.Unsubscribe(r.Context(), r.URL.Query().Get("token"))
	s.land(w, r, subscriber, err, ResultUnsubscribed)
}

// oneClick unsubscribes with a POST to the link in the List-Unsubscribe
// header, as mail programs do for RFC 8058 (the body is
// List-Unsubscribe=One-Click). It answers without a redirect: nobody looks
// at the page.
func (s *Service) oneClick(w http.ResponseWriter, r *http.Request) {
	// No page shows the subscribers, so leaving the list keeps the output cache.
	frontend.NothingChanged(r)

	ctx := r.Context()

	_, err := s.Unsubscribe(ctx, r.URL.Query().Get("token"))

	switch {
	case errors.Is(err, db.ErrNotFound):
		http.Error(w, "not_found", http.StatusNotFound)
	case err != nil:
		s.frontend.Fail(w, r, fmt.Errorf("newsletter one-click unsubscribe: %w", err))
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("unsubscribed\n"))
	}
}

// land sends a visitor who followed a link from a mail to the newsletter page
// in their language, or to the home page when the instance is not mounted.
func (s *Service) land(w http.ResponseWriter, r *http.Request, subscriber Subscriber, err error, outcome string) {
	ctx := r.Context()

	if errors.Is(err, db.ErrNotFound) {
		outcome = ResultInvalid
	} else if err != nil {
		s.frontend.Fail(w, r, fmt.Errorf("newsletter link: %w", err))

		return
	}

	target := s.landing(ctx, subscriber.Locale)

	http.Redirect(w, r, target+"?"+url.Values{s.key: {outcome}}.Encode(), http.StatusSeeOther)
}

// landing is the page the instance is mounted on in locale, or the home page
// in locale; unknown or disabled locales use the site's default language.
func (s *Service) landing(ctx context.Context, locale string) string {
	config, err := s.locales.Configuration(ctx)
	if err != nil {
		return "/"
	}

	locale, err = config.Require(locale)
	if err != nil {
		locale = config.Default
	}

	page, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil || page == "" {
		return "/" + locale + "/"
	}

	return page
}
