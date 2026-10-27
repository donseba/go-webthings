package admin

import (
	"errors"
	"net/http"

	"github.com/donseba/go-webthings/auth"

	form "github.com/donseba/go-form/v2"
)

// LoginView is the data of the sign-in screen.
type LoginView struct {
	Form   form.RenderModel
	Errors form.FieldErrors
	// Message says why the sign-in was refused.
	Message string
	// Setup links to the first-run setup: no account exists yet.
	Setup bool
	// ForgotPassword links to the password reset: the admin can send mail.
	ForgotPassword bool
}

type loginInput struct {
	Email    string `form:"input,email"    label:"Email"    required:"true" maxLength:"254"  autocomplete:"username"`
	Password string `form:"input,password" label:"Password" required:"true" maxLength:"1024" autocomplete:"current-password"`
}

func (a *Admin) loginForm(w http.ResponseWriter, r *http.Request) {
	a.renderLogin(w, r, loginInput{}, nil, "", http.StatusOK)
}

func (a *Admin) renderLogin(
	w http.ResponseWriter,
	r *http.Request,
	input loginInput,
	errs form.FieldErrors,
	message string,
	status int,
) {
	exists, err := a.auth.HasUsers(r.Context())
	if err != nil {
		a.Fail(w, r, err)

		return
	}

	input.Password = ""

	if !exists && a.options.RemindSetup != nil {
		a.options.RemindSetup()
	}

	a.Render(w, r, Page{
		Title:    "Sign in",
		Template: "templates/login.gohtml",
		Status:   status,
		Data: LoginView{
			Form:           a.Form(r, input, a.URL("admin.login"), "Sign in"),
			Errors:         errs,
			Message:        message,
			Setup:          !exists,
			ForgotPassword: exists && a.options.Mail != nil,
		},
	})
}

func (a *Admin) login(w http.ResponseWriter, r *http.Request) {
	var input loginInput

	errs, err := a.Bind(r, &input)
	if err != nil {
		a.BadRequest(w, r)

		return
	}

	var attempt auth.Attempt

	if len(errs) == 0 {
		attempt, err = a.auth.SignIn(r.Context(), input.Email, input.Password, auth.Token(r))
	}

	if len(errs) > 0 || errors.Is(err, auth.ErrCredentials) || errors.Is(err, auth.ErrRateLimit) {
		message, status := "Invalid email or password.", http.StatusUnprocessableEntity
		if errors.Is(err, auth.ErrRateLimit) {
			message, status = "Too many attempts. Try again in 15 minutes.", http.StatusTooManyRequests
		}

		a.renderLogin(w, r, input, errs, a.T(r, message), status)

		return
	}

	if err != nil {
		a.Fail(w, r, err)

		return
	}

	// With two-step sign-in on, the browser keeps the pending sign-in and
	// the next page asks for the code.
	if attempt.Pending != "" {
		auth.SetPendingCookie(w, attempt.Pending, a.options.Secure)
		a.Redirect(w, r, a.URL("admin.login-code"))

		return
	}

	auth.SetCookie(w, attempt.Session, a.options.Secure)
	a.Redirect(w, r, a.URL("admin.dashboard"))
}

func (a *Admin) logout(w http.ResponseWriter, r *http.Request) {
	// admin.js sends the tab with the sign-out, so others stop seeing this
	// tab at once instead of a minute later: its own leave report would
	// arrive after the session ended.
	if user, ok := auth.CurrentUser(r.Context()); ok {
		if tab := r.PostFormValue("tab"); validTab(tab) {
			a.leave(r, user, tab)
		}
	}

	if err := a.auth.Logout(r.Context(), auth.Token(r)); err != nil {
		a.Fail(w, r, err)

		return
	}

	auth.ClearCookie(w, a.options.Secure)
	a.Redirect(w, r, a.URL("admin.login"))
}

// AccountsView is the data of the Accounts screen: the list of accounts
// and, with one selected, its forms and sessions.
type AccountsView struct {
	Users []AccountRow
	// Pager pages through many accounts.
	Pager       Pager
	Selected    *AccountRow
	Sessions    []SessionRow
	ProfileForm *form.RenderModel
	StatusForm  *form.RenderModel
	RoleForm    *form.RenderModel
	// DeleteForm is nil for the signed-in user's own account.
	DeleteForm *form.RenderModel
	// TwoStep says whether the selected account has two-step sign-in on,
	// as shown; empty when the accounts service cannot keep its secrets.
	TwoStep string
	// TwoStepForm turns two-step sign-in off for the selected account, for
	// someone who lost their phone; nil while it is off and for the
	// signed-in user's own account.
	TwoStepForm *form.RenderModel
	Membership  *MembershipForm
	Create      form.RenderModel
	Errors      form.FieldErrors
	// ProfileErrors and RoleErrors are the refusals of the selected
	// account's profile and role forms, shown at their fields.
	ProfileErrors form.FieldErrors
	RoleErrors    form.FieldErrors
}

// accountsState is the Accounts screen as a post left it: the forms as
// posted, with their errors. Nil forms show what is stored.
type accountsState struct {
	create        *createInput
	createErrors  form.FieldErrors
	profile       *profileInput
	profileErrors form.FieldErrors
	role          *roleInput
	roleErrors    form.FieldErrors
	// message shows as an error toast.
	message string
	status  int
}

// MembershipForm lists the groups with the account's memberships ticked.
type MembershipForm struct {
	// Form ticks the account's groups; nil while no group exists.
	Form *form.RenderModel
}

// membershipInput is an account's groups, posted as "group" per group.
type membershipInput struct {
	Groups form.SortedMultiSelect[string] `form:"multicheckbox" label:"Groups" name:"group" description:"Editors get the rights of all their groups."`
}

// groupChoices offers all groups, in the order the accounts service lists
// them, with selected ticked.
func (a *Admin) groupChoices(r *http.Request, selected ...string) (form.SortedMultiSelect[string], error) {
	groups, err := a.auth.Groups(r.Context())
	if err != nil {
		return form.SortedMultiSelect[string]{}, err
	}

	options := make([]form.Option[string], 0, len(groups))

	for _, group := range groups {
		options = append(options, form.Option[string]{Value: group.ID, Label: group.Name})
	}

	return form.NewOrderedMultiSelect(options...).Selected(selected...), nil
}

// AccountRow is one account. Role and Status are translated labels, such as
// "Beheerder" and "Actief"; Admin and Disabled hold what they say.
type AccountRow struct {
	ID, Email, Name, Role, Status, URL string
	Created                            string
	Admin                              bool
	Disabled                           bool
}

// SessionRow is one session of the selected account, with its times as
// shown. Revoke ends it; nil for sessions that ended already.
type SessionRow struct {
	Created, Expires, LastSeen string
	Active                     bool
	Revoke                     *form.RenderModel
}

type createInput struct {
	Email    string                    `form:"input,email"    label:"Email"    required:"true" maxLength:"254"  autocomplete:"off"`
	Name     string                    `form:"input,text"     label:"Name"                     maxLength:"200"  autocomplete:"off"`
	Password string                    `form:"input,password" label:"Password" required:"true" maxLength:"1024" autocomplete:"new-password" minLength:"12"`
	Role     form.SortedSelect[string] `form:"dropdown"       label:"Role"                                                                                 translate:"true"`
}

type profileInput struct {
	Email string `form:"input,email" label:"Email" required:"true" maxLength:"254"`
	Name  string `form:"input,text"  label:"Name"                  maxLength:"200"`
}

type roleInput struct {
	Role form.SortedSelect[string] `form:"dropdown" label:"Role" translate:"true"`
}

// roles are the choices of the role dropdowns; go-form translates the labels
// (translate:"true").
func (a *Admin) roles(_ *http.Request, selected auth.Role) form.SortedSelect[string] {
	return form.NewSortedSelect(map[string]string{
		string(auth.Editor): "Editor (rights from groups)",
		string(auth.Admin):  "Administrator (everything)",
	}).Selected(string(selected))
}

// roleLabel names a role in the admin language.
func (a *Admin) roleLabel(r *http.Request, role auth.Role) string {
	if role == auth.Admin {
		return a.T(r, "Administrator")
	}

	return a.T(r, "Editor")
}

// statusLabel names the status of an account in the admin language.
func (a *Admin) statusLabel(r *http.Request, status auth.Status) string {
	if status == auth.Disabled {
		return a.T(r, "Disabled")
	}

	return a.T(r, "Active")
}

func (a *Admin) accountsSection() Section {
	return Section{
		ID:        "accounts",
		Title:     "Accounts",
		Icon:      "users",
		AdminOnly: true,
		Group:     GroupSystem,
		Routes: func(routes *Routes) {
			routes.Get("", View, a.accounts).As("index")
			routes.Post("", Edit, a.createAccount)
			routes.Get("/{user}", View, a.accounts).As("show")
			routes.Post("/{user}/profile", Edit, a.updateProfile).As("profile")
			routes.Post("/{user}/delete", Delete, a.deleteAccount).As("delete")
			routes.Post("/{user}/status", Edit, a.toggleStatus).As("status")
			routes.Post("/{user}/role", Edit, a.setRole).As("role")
			routes.Post("/{user}/groups", Edit, a.setGroups).As("groups")
			routes.Post("/{user}/sessions/{session}/revoke", Edit, a.revokeSession).As("revoke")
			routes.Post("/{user}/two-step/off", Edit, a.removeTwoStep).As("two-step-off")
		},
	}
}

func (a *Admin) accounts(w http.ResponseWriter, r *http.Request) {
	a.renderAccounts(w, r, accountsState{status: http.StatusOK})
}

func (a *Admin) renderAccounts(w http.ResponseWriter, r *http.Request, page accountsState) {
	users, err := a.auth.Users(r.Context())
	if err != nil {
		a.Fail(w, r, err)

		return
	}

	// The message shows as a toast; errors of single fields stay inline.
	r = WithError(r, page.message)

	create := createInput{Role: a.roles(r, auth.Editor)}
	if page.create != nil {
		create = *page.create
	}

	view := AccountsView{
		Create:        a.Form(r, create, a.URL("admin.accounts.index"), "Create account"),
		Errors:        page.createErrors,
		ProfileErrors: page.profileErrors,
		RoleErrors:    page.roleErrors,
	}

	search := SearchOf(r)

	for _, user := range users {
		row := AccountRow{
			ID:       user.ID,
			Email:    user.Email,
			Name:     user.Name,
			Role:     a.roleLabel(r, user.Role),
			Status:   a.statusLabel(r, user.Status),
			URL:      a.URL("admin.accounts.show", "user", user.ID),
			Created:  DateTime(r.Context(), user.CreatedAt),
			Admin:    user.IsAdmin(),
			Disabled: user.Status == auth.Disabled,
		}

		if user.ID == r.PathValue("user") {
			view.Selected = &row
		}

		if search.Matches(user.Email, user.Name) {
			view.Users = append(view.Users, row)
		}
	}

	// The pager stays on the selected account, if any.
	listPath := a.URL("admin.accounts.index")
	if view.Selected != nil {
		listPath = view.Selected.URL
	}

	number := PageNumber(r)
	rows, more := Paginate(view.Users, number, DefaultPageSize)
	view.Users = rows
	view.Pager = NewPager(r, listPath, number, more)

	if view.Selected != nil {
		if err := a.selectedAccount(r, &view, page); err != nil {
			a.Fail(w, r, err)

			return
		}
	}

	a.Render(w, r, Page{Title: "Accounts", Template: "templates/accounts.gohtml", Data: view, Status: page.status})
}

// selectedAccount adds the forms of the selected account, as posted when
// page holds them.
func (a *Admin) selectedAccount(r *http.Request, view *AccountsView, page accountsState) error {
	label := "Disable account"
	if view.Selected.Disabled {
		label = "Enable account"
	}

	profile := profileInput{Email: view.Selected.Email, Name: view.Selected.Name}
	if page.profile != nil {
		profile = *page.profile
	}

	id := view.Selected.ID
	profileForm := a.Form(r, profile, a.URL("admin.accounts.profile", "user", id), "Save")
	view.ProfileForm = &profileForm

	if current, _ := auth.CurrentUser(r.Context()); current.ID != view.Selected.ID {
		deleteForm := a.Confirm(
			r,
			a.Form(r, struct{}{}, a.URL("admin.accounts.delete", "user", id), "Delete account"),
			"Delete this account? Its sessions end and it can no longer sign in. This cannot be undone.",
		)
		view.DeleteForm = &deleteForm
	}

	statusForm := a.Form(r, struct{}{}, a.URL("admin.accounts.status", "user", id), label)
	view.StatusForm = &statusForm

	role := auth.Editor
	if view.Selected.Admin {
		role = auth.Admin
	}

	roleChoice := roleInput{Role: a.roles(r, role)}
	if page.role != nil {
		roleChoice = *page.role
	}

	roleForm := a.Form(r, roleChoice, a.URL("admin.accounts.role", "user", id), "Change role")
	view.RoleForm = &roleForm

	if err := a.membership(r, view); err != nil {
		return err
	}

	if err := a.accountTwoStep(r, view); err != nil {
		return err
	}

	sessions, err := a.auth.Sessions(r.Context(), view.Selected.ID)
	if err != nil {
		return err
	}

	for _, session := range sessions {
		row := SessionRow{
			Created: DateTime(r.Context(), session.CreatedAt),
			Expires: DateTime(r.Context(), session.ExpiresAt),
			Active:  session.RevokedAt == nil,
		}

		if session.LastSeenAt != nil {
			row.LastSeen = DateTime(r.Context(), *session.LastSeenAt)
		}

		if row.Active {
			revoke := a.Form(r, struct{}{}, a.URL("admin.accounts.revoke", "user", id, "session", session.ID), "Revoke session")
			row.Revoke = &revoke
		}

		view.Sessions = append(view.Sessions, row)
	}

	return nil
}

// accountTwoStep says whether the selected account has two-step sign-in
// on, with the form that turns it off for another account.
func (a *Admin) accountTwoStep(r *http.Request, view *AccountsView) error {
	if !a.auth.TwoStepAvailable() {
		return nil
	}

	state, err := a.auth.TwoStep(r.Context(), view.Selected.ID)
	if err != nil {
		return err
	}

	view.TwoStep = a.T(r, "Off")

	if !state.On {
		return nil
	}

	view.TwoStep = a.T(r, "On")

	if current, _ := auth.CurrentUser(r.Context()); current.ID != view.Selected.ID {
		off := a.Confirm(
			r,
			a.Form(r, struct{}{}, a.URL("admin.accounts.two-step-off", "user", view.Selected.ID), "Turn off two-step sign-in"),
			"Turn off two-step sign-in for this account? It then signs in with its password alone, until its owner turns it on again.",
		)
		view.TwoStepForm = &off
	}

	return nil
}

func (a *Admin) createAccount(w http.ResponseWriter, r *http.Request) {
	input := createInput{Role: a.roles(r, auth.Editor)}

	errs, err := a.Bind(r, &input)
	if err != nil {
		a.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		input.Password = ""
		a.renderAccounts(w, r, accountsState{create: &input, createErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	role := auth.Role(input.Role.Get())
	if role != auth.Admin {
		role = auth.Editor
	}

	user, err := a.auth.CreateUser(r.Context(), input.Email, input.Name, input.Password, role)

	// The refusals belong to one field each, so they show at that field.
	var field, message string

	switch {
	case errors.Is(err, auth.ErrDuplicate):
		field, message = "Email", "This email address already has an account."
	case errors.Is(err, auth.ErrEmail):
		field, message = "Email", "Enter a valid email address."
	case errors.Is(err, auth.ErrPassword):
		field, message = "Password", "Passwords need at least 12 characters."
	case err != nil:
		a.Fail(w, r, err)

		return
	}

	if message != "" {
		input.Password = ""
		errs := FieldError(field, a.T(r, message))
		a.renderAccounts(w, r, accountsState{create: &input, createErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	a.Redirect(w, r, a.URL("admin.accounts.show", "user", user.ID))
}

// lastAdmin refuses a change that would leave no active administrator.
const lastAdmin = "At least one active administrator must remain."

func (a *Admin) toggleStatus(w http.ResponseWriter, r *http.Request) {
	users, err := a.auth.Users(r.Context())
	if err != nil {
		a.Fail(w, r, err)

		return
	}

	status := auth.Disabled

	for _, user := range users {
		if user.ID == r.PathValue("user") && user.Status == auth.Disabled {
			status = auth.Active
		}
	}

	err = a.auth.SetStatus(r.Context(), r.PathValue("user"), status)
	if errors.Is(err, auth.ErrLastAdmin) {
		a.renderAccounts(w, r, accountsState{message: a.T(r, lastAdmin), status: http.StatusConflict})

		return
	}

	if err != nil {
		a.Fail(w, r, err)

		return
	}

	a.Redirect(w, r, a.URL("admin.accounts.show", "user", r.PathValue("user")))
}

func (a *Admin) revokeSession(w http.ResponseWriter, r *http.Request) {
	err := a.auth.RevokeSession(r.Context(), r.PathValue("user"), r.PathValue("session"))
	if err != nil && !errors.Is(err, auth.ErrNotFound) {
		a.Fail(w, r, err)

		return
	}

	a.Redirect(w, r, a.URL("admin.accounts.show", "user", r.PathValue("user")))
}

func (a *Admin) membership(r *http.Request, view *AccountsView) error {
	member, err := a.auth.UserGroups(r.Context(), view.Selected.ID)
	if err != nil {
		return err
	}

	groups, err := a.groupChoices(r, member...)
	if err != nil {
		return err
	}

	view.Membership = &MembershipForm{}

	if len(groups.SortedMapper()) > 0 {
		model := a.Form(
			r,
			membershipInput{Groups: groups},
			a.URL("admin.accounts.groups", "user", view.Selected.ID),
			"Save groups",
		)
		view.Membership.Form = &model
	}

	return nil
}

func (a *Admin) setRole(w http.ResponseWriter, r *http.Request) {
	input := roleInput{Role: a.roles(r, auth.Editor)}

	if errs, err := a.MapForm(r, &input); err != nil || len(errs) > 0 {
		a.BadRequest(w, r)

		return
	}

	err := a.auth.SetRole(r.Context(), r.PathValue("user"), auth.Role(input.Role.Get()))
	if errors.Is(err, auth.ErrLastAdmin) {
		// The refusal belongs to the role field, which keeps the choice.
		errs := FieldError("Role", a.T(r, lastAdmin))
		a.renderAccounts(w, r, accountsState{role: &input, roleErrors: errs, status: http.StatusConflict})

		return
	}

	if err != nil {
		a.Fail(w, r, err)

		return
	}

	a.Redirect(w, r, a.URL("admin.accounts.show", "user", r.PathValue("user")))
}

func (a *Admin) setGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := a.groupChoices(r)
	if err != nil {
		a.Fail(w, r, err)

		return
	}

	// Groups that do not exist are refused.
	input := membershipInput{Groups: groups}

	if errs, err := a.MapForm(r, &input); err != nil || len(errs) > 0 {
		a.BadRequest(w, r)

		return
	}

	err = a.auth.SetUserGroups(r.Context(), r.PathValue("user"), input.Groups.Get())
	if err != nil && !errors.Is(err, auth.ErrNotFound) {
		a.Fail(w, r, err)

		return
	}

	a.Redirect(w, r, a.URL("admin.accounts.show", "user", r.PathValue("user")))
}

// updateProfile changes an account's e-mail address and name.
func (a *Admin) updateProfile(w http.ResponseWriter, r *http.Request) {
	var input profileInput

	errs, err := a.Bind(r, &input)
	if err != nil {
		a.BadRequest(w, r)

		return
	}

	// Refusals show at their fields, with the input as posted.
	if len(errs) > 0 {
		a.renderAccounts(w, r, accountsState{profile: &input, profileErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	err = a.auth.UpdateUser(r.Context(), r.PathValue("user"), input.Email, input.Name)

	var message string

	switch {
	case errors.Is(err, auth.ErrNotFound):
		http.NotFound(w, r)

		return
	case errors.Is(err, auth.ErrDuplicate):
		message = "This email address already has an account."
	case errors.Is(err, auth.ErrEmail):
		message = invalidEmail
	case err != nil:
		a.Fail(w, r, err)

		return
	}

	if message != "" {
		errs := FieldError("Email", a.T(r, message))
		a.renderAccounts(w, r, accountsState{profile: &input, profileErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	a.Redirect(w, r, a.URL("admin.accounts.show", "user", r.PathValue("user")))
}

// deleteAccount removes an account. Nobody deletes their own, and the last
// active administrator stays.
func (a *Admin) deleteAccount(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.CurrentUser(r.Context())

	if current.ID == r.PathValue("user") {
		a.renderAccounts(w, r, accountsState{message: a.T(r, "You cannot delete your own account."), status: http.StatusConflict})

		return
	}

	err := a.auth.DeleteUser(r.Context(), r.PathValue("user"))
	if errors.Is(err, auth.ErrLastAdmin) {
		a.renderAccounts(w, r, accountsState{message: a.T(r, lastAdmin), status: http.StatusConflict})

		return
	}

	if err != nil && !errors.Is(err, auth.ErrNotFound) {
		a.Fail(w, r, err)

		return
	}

	// Accounts are deleted for good, not moved to the trash.
	a.RedirectNotice(w, r, a.URL("admin.accounts.index"), "Account deleted.")
}
