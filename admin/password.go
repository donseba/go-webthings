package admin

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/mail"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
	"github.com/donseba/go-router/middleware"
)

// FormPageView is a page with one form outside the admin's sections: the
// first-run setup and the password reset screens.
type FormPageView struct {
	Title string
	// Intro explains the form; Message reports the outcome of a post, and
	// Error why the page cannot be used, such as an expired link. Refusals
	// of single fields show at the field (see FieldError).
	Intro   string
	Message string
	Error   string
	// Form is nil when there is nothing to fill in, for example after a
	// reset link was requested.
	Form   *form.RenderModel
	Errors form.FieldErrors
	Links  []PageLink
}

// PageLink is a link below the form, such as "Back to sign in".
type PageLink struct {
	URL   string
	Title string
}

// AccountView is the signed-in user's own account page.
type AccountView struct {
	Email    string
	Name     string
	Role     string
	Password form.RenderModel
	Errors   form.FieldErrors
}

type setupInput struct {
	Token    string `form:"input,hidden"`
	Email    string `form:"input,email"    label:"Email"           required:"true" maxLength:"254"  autocomplete:"username"`
	Name     string `form:"input,text"     label:"Name"                            maxLength:"200"  autocomplete:"name"`
	Password string `form:"input,password" label:"Password"        required:"true" maxLength:"1024" autocomplete:"new-password" minLength:"12"`
	Repeat   string `form:"input,password" label:"Repeat password" required:"true" maxLength:"1024" autocomplete:"new-password"                equal:"Password" equalMsg:"The passwords do not match."`
}

type forgotInput struct {
	Email string `form:"input,email" label:"Email" required:"true" maxLength:"254" autocomplete:"username"`
}

type resetInput struct {
	Token    string `form:"input,hidden"`
	Password string `form:"input,password" label:"New password"        required:"true" maxLength:"1024" minLength:"12" autocomplete:"new-password"`
	Repeat   string `form:"input,password" label:"Repeat new password" required:"true" maxLength:"1024"                autocomplete:"new-password" equal:"Password" equalMsg:"The passwords do not match."`
}

type passwordInput struct {
	Current  string `form:"input,password" label:"Current password"    required:"true" maxLength:"1024" autocomplete:"current-password"`
	Password string `form:"input,password" label:"New password"        required:"true" maxLength:"1024" autocomplete:"new-password"     minLength:"12"`
	Repeat   string `form:"input,password" label:"Repeat new password" required:"true" maxLength:"1024" autocomplete:"new-password"                    equal:"Password" equalMsg:"The passwords do not match."`
}

// The patterns of the admin's own pages next to sign-in, below its path;
// code links to them by route name.
const (
	setupPath  = "/setup"
	forgotPath = "/forgot-password"
	resetPath  = "/reset-password"
	ownPath    = "/account"
)

const (
	passwordTooShort  = "Passwords need at least 12 characters."
	invalidEmail      = "Enter a valid email address."
	invalidResetLink  = "This link is invalid or has expired. Ask for a new one."
	resetLinkSent     = "If an account exists for this address, it receives an e-mail with a link to choose a new password. The link works for one hour."
	passwordChanged   = "Your password was changed. Sign in with your new password."
	ownPasswordChange = "Your password was changed. Your other sessions were signed out."
)

// reservedSections are the section IDs the admin uses itself (see
// ReservedSections), from the routes of an admin without sections.
var reservedSections = sync.OnceValue(func() []string {
	own := &Admin{base: DefaultPath, router: router.NewSite()}
	own.ownRoutes()

	return append([]string{own.accountsSection().ID, own.groupsSection().ID}, own.ownPages()...)
})

// ReservedSections are the section IDs the admin uses itself: its own
// sections (Accounts and Groups) and the first segments of its addresses
// outside them, such as the sign-in and password reset pages, which its
// routes give. Modules cannot use them as keys.
func ReservedSections() []string {
	return slices.Clone(reservedSections())
}

// passwordRoutes registers the pages for people who are not signed in: the
// first-run setup and the password reset.
func (a *Admin) passwordRoutes(public *router.Router) {
	// The setup and reset links have their token in the address; it must
	// not reach other sites.
	public.With(func(tokens *router.Router) {
		tokens.Use(middleware.SecurityHeaders(middleware.SecurityHeadersOptions{ReferrerPolicy: "no-referrer"}))
		tokens.Get(setupPath, a.setupForm).As("admin.setup")
		tokens.Post(setupPath, a.setup)
		tokens.Get(resetPath, a.resetForm).As("admin.reset-password")
		tokens.Post(resetPath, a.reset)
	})

	public.Get(forgotPath, a.forgotForm).As("admin.forgot-password")
	public.Post(forgotPath, a.forgot)
}

// accountRoutes registers the signed-in user's own account page.
func (a *Admin) accountRoutes(own *router.Router) {
	own.Get(ownPath, a.account).As("admin.account")
	own.Post(ownPath, a.changePassword)
}

// renderFormPage shows a page with one form, without the admin navigation.
func (a *Admin) renderFormPage(w http.ResponseWriter, r *http.Request, view FormPageView, status int) {
	a.Render(w, r, Page{Title: view.Title, Template: "templates/form-page.gohtml", Data: view, Status: status})
}

// firstRun reports whether the setup screen is open, answering 404
// otherwise: it exists only on a site without accounts, and only for the
// setup token of this start (Options.SetupToken), which the link in the
// server's log carries.
func (a *Admin) firstRun(w http.ResponseWriter, r *http.Request, token string) bool {
	want := a.options.SetupToken
	if want == "" || subtle.ConstantTimeCompare([]byte(token), []byte(want)) != 1 {
		http.NotFound(w, r)

		return false
	}

	exists, err := a.auth.HasUsers(r.Context())
	if err != nil {
		a.Fail(w, r, err)

		return false
	}

	if exists {
		http.NotFound(w, r)

		return false
	}

	return true
}

func (a *Admin) setupForm(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if !a.firstRun(w, r, token) {
		return
	}

	a.renderSetup(w, r, setupInput{Token: token}, nil, http.StatusOK)
}

func (a *Admin) renderSetup(
	w http.ResponseWriter,
	r *http.Request,
	input setupInput,
	errs form.FieldErrors,
	status int,
) {
	input.Password, input.Repeat = "", ""
	model := a.Form(r, input, a.URL("admin.setup"), "Create account")

	a.renderFormPage(w, r, FormPageView{
		Title:  "Set up",
		Intro:  a.T(r, "No account exists yet. Create the first administrator account."),
		Form:   &model,
		Errors: errs,
	}, status)
}

// setup creates the first administrator and signs them in.
func (a *Admin) setup(w http.ResponseWriter, r *http.Request) {
	var input setupInput

	if errs, err := a.MapForm(r, &input); err != nil || len(errs) > 0 {
		http.NotFound(w, r)

		return
	}

	if !a.firstRun(w, r, input.Token) {
		return
	}

	if errs := a.Validate(r, &input); len(errs) > 0 {
		a.renderSetup(w, r, input, errs, http.StatusUnprocessableEntity)

		return
	}

	_, err := a.auth.SetupAdmin(r.Context(), input.Email, input.Name, input.Password)

	switch {
	case errors.Is(err, auth.ErrBootstrap):
		http.NotFound(w, r)

		return
	case errors.Is(err, auth.ErrEmail):
		a.renderSetup(w, r, input, FieldError("Email", a.T(r, invalidEmail)), http.StatusUnprocessableEntity)

		return
	case errors.Is(err, auth.ErrPassword):
		a.renderSetup(w, r, input, FieldError("Password", a.T(r, passwordTooShort)), http.StatusUnprocessableEntity)

		return
	case err != nil:
		a.Fail(w, r, err)

		return
	}

	token, err := a.auth.Login(r.Context(), input.Email, input.Password, auth.Token(r))
	if err != nil {
		a.Fail(w, r, err)

		return
	}

	// The site's setup guide, when it has one, takes the new administrator
	// through the settings a new site needs.
	next, err := a.TryURL("admin.site.guide")
	if err != nil {
		next = a.URL("admin.dashboard")
	}

	auth.SetCookie(w, token, a.options.Secure)
	a.RedirectNotice(w, r, next, "Your administrator account is ready.")
}

// resetLinks is the link back to sign-in on the password reset pages.
func (a *Admin) resetLinks(r *http.Request) []PageLink {
	return []PageLink{{URL: a.URL("admin.login"), Title: a.T(r, "Back to sign in")}}
}

func (a *Admin) forgotForm(w http.ResponseWriter, r *http.Request) {
	if a.options.Mail == nil {
		http.NotFound(w, r)

		return
	}

	a.renderForgot(w, r, forgotInput{}, nil, http.StatusOK)
}

func (a *Admin) renderForgot(w http.ResponseWriter, r *http.Request, input forgotInput, errs form.FieldErrors, status int) {
	model := a.Form(r, input, a.URL("admin.forgot-password"), "Send link")

	a.renderFormPage(w, r, FormPageView{
		Title:  "Forgot your password?",
		Intro:  a.T(r, "Enter the e-mail address of your account. You receive a link to choose a new password."),
		Form:   &model,
		Errors: errs,
		Links:  a.resetLinks(r),
	}, status)
}

// forgot sends a reset link. Its answer is the same whether or not the
// address has an account, and the e-mail is sent in the background, so
// neither the answer nor its timing tells which addresses have one.
func (a *Admin) forgot(w http.ResponseWriter, r *http.Request) {
	if a.options.Mail == nil {
		http.NotFound(w, r)

		return
	}

	var input forgotInput

	errs, err := a.Bind(r, &input)
	if err != nil {
		a.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		a.renderForgot(w, r, input, errs, http.StatusUnprocessableEntity)

		return
	}

	token, user, err := a.auth.RequestPasswordReset(r.Context(), input.Email)

	switch {
	case err == nil:
		a.sendResetLink(r, user, token)
	case errors.Is(err, auth.ErrRateLimit):
		// The address stays out of the log; the answer stays the same.
		slog.WarnContext(r.Context(), "password reset links throttled")
	case !errors.Is(err, auth.ErrNotFound):
		a.Fail(w, r, err)

		return
	}

	a.renderFormPage(w, r, FormPageView{
		Title:   "Forgot your password?",
		Message: a.T(r, resetLinkSent),
		Links:   a.resetLinks(r),
	}, http.StatusOK)
}

// sendResetLink mails the link in the admin's language. The message is
// plain text: the admin has no mail templates, and its catalogue holds the
// text.
func (a *Admin) sendResetLink(r *http.Request, user auth.User, token string) {
	link := a.absoluteURL(r, a.URL("admin.reset-password", url.Values{"token": {token}}))
	message := mail.Message{
		To:      []string{user.Email},
		Subject: a.T(r, "Choose a new password for %s", a.options.Title),
		Text: strings.Join([]string{
			a.T(r, "Someone asked for a new password for your account at %s.", a.options.Title),
			a.T(r, "Open this link within an hour to choose a new password:") + "\n" + link,
			a.T(r, "If you did not ask for this, ignore this e-mail: your password stays the same."),
		}, "\n\n") + "\n",
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Minute)

	go func() {
		defer cancel()

		if err := a.options.Mail.Send(ctx, message); err != nil {
			slog.ErrorContext(ctx, "send password reset link", "error", err)
		}
	}()
}

// absoluteURL makes an admin path absolute for a link in e-mail: with
// Options.AbsoluteURL, or else with the address the request arrived on
// (go-router's middleware.Origin), over https when the admin is Secure.
func (a *Admin) absoluteURL(r *http.Request, path string) string {
	if a.options.AbsoluteURL != nil {
		return a.options.AbsoluteURL(r, path)
	}

	if a.options.Secure {
		return "https://" + middleware.Host(r) + path
	}

	return middleware.Origin(r) + path
}

func (a *Admin) resetForm(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")

	err := a.auth.CheckResetToken(r.Context(), token)
	if errors.Is(err, auth.ErrResetToken) {
		a.renderInvalidReset(w, r, http.StatusOK)

		return
	}

	if err != nil {
		a.Fail(w, r, err)

		return
	}

	a.renderReset(w, r, resetInput{Token: token}, nil, http.StatusOK)
}

func (a *Admin) renderReset(
	w http.ResponseWriter,
	r *http.Request,
	input resetInput,
	errs form.FieldErrors,
	status int,
) {
	input.Password, input.Repeat = "", ""
	model := a.Form(r, input, a.URL("admin.reset-password"), "Save password")

	a.renderFormPage(w, r, FormPageView{
		Title:  "Choose a new password",
		Intro:  a.T(r, "Passwords need at least 12 characters."),
		Form:   &model,
		Errors: errs,
		Links:  a.resetLinks(r),
	}, status)
}

func (a *Admin) renderInvalidReset(w http.ResponseWriter, r *http.Request, status int) {
	links := a.resetLinks(r)
	if a.options.Mail != nil {
		links = append([]PageLink{{URL: a.URL("admin.forgot-password"), Title: a.T(r, "Ask for a new link")}}, links...)
	}

	a.renderFormPage(w, r, FormPageView{
		Title: "Choose a new password",
		Error: a.T(r, invalidResetLink),
		Links: links,
	}, status)
}

// reset sets the new password and signs the account out everywhere.
func (a *Admin) reset(w http.ResponseWriter, r *http.Request) {
	var input resetInput

	errs, err := a.Bind(r, &input)
	if err != nil {
		a.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		a.renderReset(w, r, input, errs, http.StatusUnprocessableEntity)

		return
	}

	err = a.auth.ResetPassword(r.Context(), input.Token, input.Password)

	switch {
	case errors.Is(err, auth.ErrResetToken):
		a.renderInvalidReset(w, r, http.StatusUnprocessableEntity)

		return
	case errors.Is(err, auth.ErrPassword):
		a.renderReset(w, r, input, FieldError("Password", a.T(r, passwordTooShort)), http.StatusUnprocessableEntity)

		return
	case err != nil:
		a.Fail(w, r, err)

		return
	}

	auth.ClearCookie(w, a.options.Secure)
	a.RedirectNotice(w, r, a.URL("admin.login"), passwordChanged)
}

func (a *Admin) account(w http.ResponseWriter, r *http.Request) {
	a.renderAccount(w, r, nil, "", http.StatusOK)
}

func (a *Admin) renderAccount(w http.ResponseWriter, r *http.Request, errs form.FieldErrors, message string, status int) {
	user, _ := auth.CurrentUser(r.Context())

	// The message shows as a toast; errors of single fields stay inline.
	r = WithError(r, message)

	a.Render(w, r, Page{
		Title:    "Your account",
		Template: "templates/account.gohtml",
		Status:   status,
		Data: AccountView{
			Email:    user.Email,
			Name:     user.Name,
			Role:     a.roleLabel(r, user.Role),
			Password: a.Form(r, passwordInput{}, a.URL("admin.account"), "Change password"),
			Errors:   errs,
		},
	})
}

// changePassword sets the signed-in user's password; the other sessions of
// the account are signed out.
func (a *Admin) changePassword(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r.Context())

	var input passwordInput

	errs, err := a.Bind(r, &input)
	if err != nil {
		a.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		a.renderAccount(w, r, errs, "", http.StatusUnprocessableEntity)

		return
	}

	err = a.auth.ChangePassword(r.Context(), user.ID, input.Current, input.Password, auth.Token(r))

	switch {
	case errors.Is(err, auth.ErrCredentials):
		a.renderAccount(
			w,
			r,
			FieldError("Current", a.T(r, "Your current password is not correct.")),
			"",
			http.StatusUnprocessableEntity,
		)

		return
	case errors.Is(err, auth.ErrRateLimit):
		a.renderAccount(w, r, nil, a.T(r, "Too many attempts. Try again in 15 minutes."), http.StatusTooManyRequests)

		return
	case errors.Is(err, auth.ErrPassword):
		a.renderAccount(w, r, FieldError("Password", a.T(r, passwordTooShort)), "", http.StatusUnprocessableEntity)

		return
	case err != nil:
		a.Fail(w, r, err)

		return
	}

	a.RedirectNotice(w, r, a.URL("admin.account"), ownPasswordChange)
}
