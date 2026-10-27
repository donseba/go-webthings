package admin

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/donseba/go-webthings/auth"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-partial/exp/flash"
	"github.com/donseba/go-router"

	"rsc.io/qr"
)

// TwoStepView is the card "Two-step sign-in" of the own account page.
type TwoStepView struct {
	// On means that signing in asks for a code after the password.
	On bool
	// Start turns two-step sign-in on; nil while it is on or a setup waits
	// for its first code.
	Start *form.RenderModel
	// Setup is the setup that waits for its first code.
	Setup *TwoStepSetup
	// RecoveryCodes is the number of recovery codes not used yet.
	RecoveryCodes int
	// Codes are new recovery codes, shown on this page only: only their
	// digests are stored.
	Codes []string
	// Off turns two-step sign-in off and Recovery makes new recovery
	// codes; both are nil while it is off.
	Off            *form.RenderModel
	OffErrors      form.FieldErrors
	Recovery       *form.RenderModel
	RecoveryErrors form.FieldErrors
}

// TwoStepSetup is a setup of two-step sign-in that waits for its first
// code: the QR code and the key for the authenticator app, and the form for
// the code the app then shows.
type TwoStepSetup struct {
	// QR is the address of the QR code, a PNG.
	QR string
	// Key is the secret in groups of four characters, to type by hand.
	Key     string
	Confirm form.RenderModel
	Errors  form.FieldErrors
	Cancel  form.RenderModel
}

// codeInput is the second step of signing in.
type codeInput struct {
	Code string `form:"input,text" label:"Code" required:"true" maxLength:"32" autocomplete:"one-time-code" description:"Lost your phone? Enter one of your recovery codes instead."`
}

// confirmTwoStepInput is the first code of the app, which turns two-step
// sign-in on.
type confirmTwoStepInput struct {
	Code string `form:"input,text" label:"Code from the app" required:"true" maxLength:"32" autocomplete:"one-time-code"`
}

// turnOffTwoStepInput turns two-step sign-in off.
type turnOffTwoStepInput struct {
	Password string `form:"input,password" label:"Current password" required:"true" maxLength:"1024" autocomplete:"current-password"`
	Code     string `form:"input,text"     label:"Code"             required:"true" maxLength:"32"   autocomplete:"one-time-code"    description:"A code from the app or one of your recovery codes."`
}

// recoveryCodesInput asks for new recovery codes.
type recoveryCodesInput struct {
	Code string `form:"input,text" label:"Code" required:"true" maxLength:"32" autocomplete:"one-time-code" description:"A code from the app or one of your recovery codes."`
}

// The patterns of two-step sign-in, below the admin's path: the second
// step of signing in, and the own account's setup.
const (
	loginCodePath = loginPath + "/code"
	twoStepPath   = ownPath + "/two-step"
)

const (
	wrongCode     = "This code is not correct."
	tooMany       = "Too many attempts. Try again in 15 minutes."
	signInExpired = "The sign-in expired. Enter your password again."
	// qrScale is the size of a module of the QR code in pixels: about 200
	// pixels for the whole code, which browsers double on sharp screens.
	qrScale = 4
)

// codeRoutes registers the second step of signing in, for accounts with
// two-step sign-in on.
func (a *Admin) codeRoutes(public *router.Router) {
	public.Get(loginCodePath, a.codeForm).As("admin.login-code")
	public.Post(loginCodePath, a.loginCode)
}

// twoStepRoutes registers the signed-in user's own two-step sign-in.
func (a *Admin) twoStepRoutes(own *router.Router) {
	own.Post(twoStepPath, a.startTwoStep).As("admin.account.two-step")
	own.Get(twoStepPath+"/qr", a.twoStepQR).As("admin.account.two-step-qr")
	own.Post(twoStepPath+"/confirm", a.confirmTwoStep).As("admin.account.two-step-confirm")
	own.Post(twoStepPath+"/cancel", a.cancelTwoStep).As("admin.account.two-step-cancel")
	own.Post(twoStepPath+"/off", a.turnOffTwoStep).As("admin.account.two-step-off")
	own.Post(twoStepPath+"/recovery-codes", a.newRecoveryCodes).As("admin.account.recovery-codes")
}

// codeForm asks for the code of a pending sign-in, whose password was
// right.
func (a *Admin) codeForm(w http.ResponseWriter, r *http.Request) {
	err := a.auth.CheckPending(r.Context(), auth.PendingToken(r))
	if errors.Is(err, auth.ErrPending) {
		a.signInAgain(w, r)

		return
	}

	if err != nil {
		a.Fail(w, r, err)

		return
	}

	a.renderCode(w, r, nil, "", http.StatusOK)
}

func (a *Admin) renderCode(w http.ResponseWriter, r *http.Request, errs form.FieldErrors, message string, status int) {
	model := a.Form(r, codeInput{}, a.URL("admin.login-code"), "Sign in")

	a.renderFormPage(w, r, FormPageView{
		Title:  "Two-step sign-in",
		Intro:  a.T(r, "Enter the code from your authenticator app."),
		Error:  message,
		Form:   &model,
		Errors: errs,
		Links:  a.resetLinks(r),
	}, status)
}

// signInAgain sends a browser without a pending sign-in back to the
// sign-in, and says why when its pending sign-in expired.
func (a *Admin) signInAgain(w http.ResponseWriter, r *http.Request) {
	if auth.PendingToken(r) == "" {
		a.redirect(w, r, a.URL("admin.login"))

		return
	}

	auth.ClearPendingCookie(w, a.options.Secure)
	a.RedirectFlash(w, r, a.URL("admin.login"), flash.Error(a.T(r, signInExpired)))
}

// loginCode completes a pending sign-in with a code of the authenticator
// app or a recovery code.
func (a *Admin) loginCode(w http.ResponseWriter, r *http.Request) {
	var input codeInput

	errs, err := a.Bind(r, &input)
	if err != nil {
		a.BadRequest(w, r)

		return
	}

	pending := auth.PendingToken(r)

	if len(errs) > 0 {
		err := a.auth.CheckPending(r.Context(), pending)
		if errors.Is(err, auth.ErrPending) {
			a.signInAgain(w, r)

			return
		}

		if err != nil {
			a.Fail(w, r, err)

			return
		}

		a.renderCode(w, r, errs, "", http.StatusUnprocessableEntity)

		return
	}

	token, err := a.auth.FinishSignIn(r.Context(), pending, input.Code, auth.Token(r))

	switch {
	case errors.Is(err, auth.ErrPending):
		a.signInAgain(w, r)

		return
	case errors.Is(err, auth.ErrCode):
		a.renderCode(w, r, FieldError("Code", a.T(r, wrongCode)), "", http.StatusUnprocessableEntity)

		return
	case errors.Is(err, auth.ErrRateLimit):
		a.renderCode(w, r, nil, a.T(r, tooMany), http.StatusTooManyRequests)

		return
	case err != nil:
		a.Fail(w, r, err)

		return
	}

	auth.ClearPendingCookie(w, a.options.Secure)
	auth.SetCookie(w, token, a.options.Secure)
	a.Redirect(w, r, a.URL("admin.dashboard"))
}

// twoStepView is the card of two-step sign-in on the own account page, as
// page left it.
func (a *Admin) twoStepView(r *http.Request, user auth.User, page accountState) (*TwoStepView, error) {
	state, err := a.auth.TwoStep(r.Context(), user.ID)
	if err != nil {
		return nil, err
	}

	view := &TwoStepView{On: state.On, RecoveryCodes: state.RecoveryCodes, Codes: page.codes}

	switch {
	case state.On:
		off := a.Form(r, turnOffTwoStepInput{}, a.URL("admin.account.two-step-off"), "Turn off two-step sign-in")
		off.Info.IDPrefix = "two-step-off"

		recovery := a.Form(r, recoveryCodesInput{}, a.URL("admin.account.recovery-codes"), "Make new recovery codes")
		recovery.Info.IDPrefix = "recovery-codes"

		view.Off, view.OffErrors = &off, page.offErrors
		view.Recovery, view.RecoveryErrors = &recovery, page.recoveryErrors
	case state.Setup:
		secret, err := a.auth.SetupSecret(r.Context(), user.ID)
		if err != nil {
			return nil, err
		}

		confirm := a.Form(r, confirmTwoStepInput{}, a.URL("admin.account.two-step-confirm"), "Turn on")
		confirm.Info.IDPrefix = "two-step-confirm"

		view.Setup = &TwoStepSetup{
			QR:      a.URL("admin.account.two-step-qr"),
			Key:     keyGroups(auth.SecretText(secret)),
			Confirm: confirm,
			Errors:  page.confirmErrors,
			Cancel:  a.Form(r, struct{}{}, a.URL("admin.account.two-step-cancel"), "Cancel"),
		}
	default:
		start := a.Form(r, struct{}{}, a.URL("admin.account.two-step"), "Turn on two-step sign-in")
		view.Start = &start
	}

	return view, nil
}

// keyGroups writes a secret in groups of four characters, which are easier
// to type.
func keyGroups(key string) string {
	var groups []string

	for len(key) > 4 {
		groups = append(groups, key[:4])
		key = key[4:]
	}

	return strings.Join(append(groups, key), " ")
}

// startTwoStep starts turning on two-step sign-in for the signed-in user:
// the account page then shows the QR code and asks for the first code.
func (a *Admin) startTwoStep(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r.Context())

	err := a.auth.BeginTwoStep(r.Context(), user.ID)

	switch {
	case errors.Is(err, auth.ErrNoSecrets):
		http.NotFound(w, r)

		return
	case err != nil && !errors.Is(err, auth.ErrTwoStepOn):
		a.Fail(w, r, err)

		return
	}

	a.RedirectNotice(w, r, a.URL("admin.account"), "")
}

// twoStepQR serves the QR code of the signed-in user's setup that waits for
// its first code, as a PNG of the admin's own address, which its
// Content-Security-Policy allows. It is never stored, and there is none
// once two-step sign-in is on.
func (a *Admin) twoStepQR(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r.Context())

	secret, err := a.auth.SetupSecret(r.Context(), user.ID)
	if errors.Is(err, auth.ErrTwoStepOff) || errors.Is(err, auth.ErrNoSecrets) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		a.Fail(w, r, err)

		return
	}

	code, err := qr.Encode(auth.KeyURI(a.options.Title, user.Email, secret), qr.M)
	if err != nil {
		a.Fail(w, r, err)

		return
	}

	code.Scale = qrScale

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(code.PNG())
}

// confirmTwoStep turns two-step sign-in on with the first code of the app,
// and shows the recovery codes, this once.
func (a *Admin) confirmTwoStep(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r.Context())

	var input confirmTwoStepInput

	errs, err := a.Bind(r, &input)
	if err != nil {
		a.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		a.renderAccount(w, r, accountState{confirmErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	codes, err := a.auth.ConfirmTwoStep(r.Context(), user.ID, input.Code)

	switch {
	case errors.Is(err, auth.ErrCode):
		errs := FieldError("Code", a.T(r, wrongCode))
		a.renderAccount(w, r, accountState{confirmErrors: errs, status: http.StatusUnprocessableEntity})

		return
	case errors.Is(err, auth.ErrTwoStepOn), errors.Is(err, auth.ErrTwoStepOff):
		a.RedirectNotice(w, r, a.URL("admin.account"), "")

		return
	case err != nil:
		a.Fail(w, r, err)

		return
	}

	a.renderAccount(w, r, accountState{codes: codes, notice: "Two-step sign-in is on."})
}

// cancelTwoStep drops the signed-in user's setup that waits for its first
// code.
func (a *Admin) cancelTwoStep(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r.Context())

	if err := a.auth.CancelTwoStep(r.Context(), user.ID); err != nil {
		a.Fail(w, r, err)

		return
	}

	a.RedirectNotice(w, r, a.URL("admin.account"), "")
}

// turnOffTwoStep turns two-step sign-in off for the signed-in user, with
// the password and a code.
func (a *Admin) turnOffTwoStep(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r.Context())

	var input turnOffTwoStepInput

	errs, err := a.Bind(r, &input)
	if err != nil {
		a.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		a.renderAccount(w, r, accountState{offErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	err = a.auth.DisableTwoStep(r.Context(), user.ID, input.Password, input.Code)

	switch {
	case errors.Is(err, auth.ErrCredentials):
		errs := FieldError("Password", a.T(r, "Your current password is not correct."))
		a.renderAccount(w, r, accountState{offErrors: errs, status: http.StatusUnprocessableEntity})

		return
	case errors.Is(err, auth.ErrCode):
		errs := FieldError("Code", a.T(r, wrongCode))
		a.renderAccount(w, r, accountState{offErrors: errs, status: http.StatusUnprocessableEntity})

		return
	case errors.Is(err, auth.ErrRateLimit):
		a.renderAccount(w, r, accountState{message: a.T(r, tooMany), status: http.StatusTooManyRequests})

		return
	case err != nil && !errors.Is(err, auth.ErrTwoStepOff):
		a.Fail(w, r, err)

		return
	}

	a.RedirectNotice(w, r, a.URL("admin.account"), "Two-step sign-in is off.")
}

// newRecoveryCodes replaces the signed-in user's recovery codes with new
// ones, shown this once.
func (a *Admin) newRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r.Context())

	var input recoveryCodesInput

	errs, err := a.Bind(r, &input)
	if err != nil {
		a.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		a.renderAccount(w, r, accountState{recoveryErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	codes, err := a.auth.NewRecoveryCodes(r.Context(), user.ID, input.Code)

	switch {
	case errors.Is(err, auth.ErrCode):
		errs := FieldError("Code", a.T(r, wrongCode))
		a.renderAccount(w, r, accountState{recoveryErrors: errs, status: http.StatusUnprocessableEntity})

		return
	case errors.Is(err, auth.ErrRateLimit):
		a.renderAccount(w, r, accountState{message: a.T(r, tooMany), status: http.StatusTooManyRequests})

		return
	case errors.Is(err, auth.ErrTwoStepOff):
		a.RedirectNotice(w, r, a.URL("admin.account"), "")

		return
	case err != nil:
		a.Fail(w, r, err)

		return
	}

	a.renderAccount(w, r, accountState{codes: codes, notice: "New recovery codes are ready. The old ones no longer work."})
}

// removeTwoStep turns two-step sign-in off for another account, such as
// one whose owner lost both their phone and their recovery codes. It is
// logged with who did it for whom.
func (a *Admin) removeTwoStep(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.CurrentUser(r.Context())
	id := r.PathValue("user")

	// The own account turns it off on its own page, with a code.
	if current.ID == id {
		http.NotFound(w, r)

		return
	}

	target, err := a.auth.User(r.Context(), id)
	if errors.Is(err, auth.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		a.Fail(w, r, err)

		return
	}

	err = a.auth.RemoveTwoStep(r.Context(), target.ID)

	switch {
	case errors.Is(err, auth.ErrTwoStepOff):
		// Off already, perhaps by another administrator.
	case err != nil:
		a.Fail(w, r, err)

		return
	default:
		slog.InfoContext(r.Context(), "two-step sign-in turned off by an administrator",
			"user", target.ID,
			"email", target.Email,
			"administrator", current.ID,
			"administrator_email", current.Email,
		)
	}

	a.RedirectNotice(w, r, a.URL("admin.accounts.show", "user", target.ID), "Two-step sign-in is off for this account.")
}
