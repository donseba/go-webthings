package submissions

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/db"

	form "github.com/donseba/go-form/v2"
)

const pageSize = 50

// screens are the admin pages of one instance.
type screens struct {
	service *Service
	admin   *admin.Admin
	env     webthings.Env
	routes  *admin.Routes
}

// ListView is the data of templates/admin/list.gohtml: the proposals,
// filtered by status.
type ListView struct {
	Title string
	// SettingsURL is set for editors who may change the settings.
	SettingsURL string
	// TrashURL shows the instance's deleted proposals.
	TrashURL string
	// Filters switch between the statuses; new ones are counted.
	Filters admin.Tabs
	Rows    []Row
	Pager   admin.Pager
}

// Row is one proposal in the list.
type Row struct {
	URL    string
	Date   string
	Title  string
	Name   string
	Locale string
	// Status is the translated status label.
	Status string
}

// DetailView is the data of templates/admin/detail.gohtml: one proposal
// with the buttons to accept or reject it.
type DetailView struct {
	Title   string
	BackURL string
	Name    string
	Email   string
	Subject string
	Body    string
	// ImageURL is empty without an image.
	ImageURL string
	Locale   string
	Date     string
	Status   string
	// Decided is when and by whom the submission was accepted or rejected.
	Decided   string
	DecidedBy string
	// PostURL opens the draft post of an accepted submission.
	PostURL string
	Accept  *form.RenderModel
	Reject  *form.RenderModel
	Delete  *form.RenderModel
}

// choice is a list filter: a status with its label, a translation key.
type choice struct {
	status string
	label  string
}

// statuses are the list filters; the first is the default.
var statuses = []choice{
	{StatusNew, "New"},
	{StatusAccepted, "Accepted"},
	{StatusRejected, "Rejected"},
	{"all", "All"},
}

// label is the label of a status, a translation key.
func label(status string) string {
	for _, candidate := range statuses {
		if candidate.status == status {
			return candidate.label
		}
	}

	return status
}

// known reports whether status is one of the list filters.
func known(status string) bool {
	return slices.ContainsFunc(statuses, func(candidate choice) bool {
		return candidate.status == status
	})
}

func (s screens) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	status := r.URL.Query().Get("status")
	if !known(status) {
		status = StatusNew
	}

	query := status
	if status == "all" {
		query = ""
	}

	// A search looks through the submissions of the status, then pages.
	page, err := admin.ListPage[Submission]{
		Path: s.routes.URL("index"),
		Size: pageSize,
		Load: func(ctx context.Context, limit, offset int) ([]Submission, bool, error) {
			return s.service.List(ctx, query, offset, limit)
		},
		Fields: func(submission Submission) []string {
			return []string{submission.Title, submission.Name, submission.Email, submission.Body}
		},
	}.Read(r)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	fresh, err := s.service.Count(ctx, StatusNew)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	view := ListView{Title: s.env.Title, TrashURL: s.admin.TrashURL(s.env.Key), Pager: page.Pager}

	if s.admin.Can(r, admin.Permission{Section: s.env.Key, Action: admin.Edit}) {
		view.SettingsURL = s.routes.URL("settings")
	}

	tabs := make([]admin.Tab, 0, len(statuses))
	for _, candidate := range statuses {
		tab := admin.Tab{Label: candidate.label, Value: candidate.status}
		if candidate.status == StatusNew {
			// New ones are counted while there are any.
			tab.Count, tab.Counted = fresh, fresh > 0
		}

		tabs = append(tabs, tab)
	}

	view.Filters = admin.FilterTabs(r, "Status", s.routes.URL("index"), "status", status, tabs...)

	for _, submission := range page.Rows {
		view.Rows = append(view.Rows, Row{
			URL:    s.routes.URL("show", "id", submission.ID),
			Date:   admin.DateTime(ctx, submission.CreatedAt),
			Title:  submission.Title,
			Name:   submission.Name,
			Locale: submission.Locale,
			Status: label(submission.Status),
		})
	}

	s.admin.Render(w, r, admin.Page{
		Title:    s.env.Title,
		Template: "templates/admin/list.gohtml",
		Files:    files,
		Data:     view,
	})
}

// listURL is the list filtered by status, on page.
func (s screens) listURL(status string, page int) string {
	query := url.Values{}

	if status != StatusNew {
		query.Set("status", status)
	}

	if page > 1 {
		query.Set("page", strconv.Itoa(page))
	}

	return s.routes.URL("index", query)
}

func (s screens) show(w http.ResponseWriter, r *http.Request) {
	s.detail(w, r, "", http.StatusOK)
}

// detail renders one submission with the actions the editor may take, and
// message when an action could not be done.
func (s screens) detail(w http.ResponseWriter, r *http.Request, message string, status int) {
	ctx := r.Context()

	submission, err := s.service.Get(ctx, r.PathValue("id"))
	if errors.Is(err, db.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	// The message shows as a toast.
	r = admin.WithError(r, message)

	view := DetailView{
		Title:   s.env.Title,
		BackURL: s.listURL(submission.Status, 1),
		Name:    submission.Name,
		Email:   submission.Email,
		Subject: submission.Title,
		Body:    submission.Body,
		Locale:  submission.Locale,
		Date:    admin.DateTime(ctx, submission.CreatedAt),
		Status:  label(submission.Status),
	}

	if submission.MediaID != "" {
		view.ImageURL = s.env.Media.URL(ctx, submission.MediaID)
	}

	if !submission.DecidedAt.IsZero() {
		view.Decided = admin.DateTime(ctx, submission.DecidedAt)
		view.DecidedBy = s.name(r, submission.DecidedBy)
	}

	if submission.PostID != "" {
		// The post is in the target's editor; a target that is no editor
		// section, which Accept refuses, shows no link.
		view.PostURL, _ = s.admin.TryURL(admin.RouteName(s.service.target, "edit"),
			"id", submission.PostID, url.Values{"locale": {submission.Locale}})
	}

	if s.admin.Can(r, admin.Permission{Section: s.env.Key, Action: admin.Publish}) {
		if submission.Status != StatusAccepted {
			accept := s.admin.Form(r, struct{}{}, s.routes.URL("accept", "id", submission.ID), "Accept as draft post")
			view.Accept = &accept
		}

		if submission.Status == StatusNew {
			reject := s.admin.Form(r, struct{}{}, s.routes.URL("reject", "id", submission.ID), "Reject")
			view.Reject = &reject
		}
	}

	if s.admin.Can(r, admin.Permission{Section: s.env.Key, Action: admin.Delete}) {
		remove := s.admin.Form(r, struct{}{}, s.routes.URL("delete", "id", submission.ID), "Move to trash")
		view.Delete = &remove
	}

	s.admin.Render(w, r, admin.Page{
		Title:    "Proposal",
		Template: "templates/admin/detail.gohtml",
		Files:    files,
		Data:     view,
		Status:   status,
	})
}

// name is the display name of a user, or the ID when the account is gone.
func (s screens) name(r *http.Request, id string) string {
	if id == "" || s.env.Auth == nil {
		return id
	}

	names, err := s.env.Auth.Names(r.Context())
	if err != nil || names[id] == "" {
		return id
	}

	return names[id]
}

func (s screens) accept(w http.ResponseWriter, r *http.Request) {
	s.decide(w, r, mailAccepted)
}

func (s screens) reject(w http.ResponseWriter, r *http.Request) {
	s.decide(w, r, mailRejected)
}

// decide accepts or rejects a submission and tells the submitter when the
// settings ask for it.
func (s screens) decide(w http.ResponseWriter, r *http.Request, decision string) {
	ctx := r.Context()
	id := r.PathValue("id")
	user, _ := auth.CurrentUser(ctx)

	var (
		submission Submission
		err        error
	)

	if decision == mailAccepted {
		submission, err = s.service.Accept(ctx, id, user.ID)
	} else {
		submission, err = s.service.Reject(ctx, id, user.ID)
	}

	switch {
	case errors.Is(err, db.ErrNotFound):
		http.NotFound(w, r)

		return
	case errors.Is(err, ErrDecided):
		s.detail(w, r, s.admin.T(r, "This proposal was already decided."), http.StatusConflict)

		return
	case errors.Is(err, ErrNoTarget):
		// A configuration mistake of the site: say so instead of failing.
		message := s.admin.T(
			r,
			"Accepted proposals go to %s, which is not a news or blog module. Check the site's configuration.",
			s.service.target,
		)
		s.detail(w, r, message, http.StatusInternalServerError)

		return
	case err != nil:
		s.admin.Fail(w, r, err)

		return
	}

	settings, err := s.service.Settings(ctx)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.service.tellSubmitter(ctx, settings, submission, decision)
	s.admin.Redirect(w, r, s.routes.URL("show", "id", id))
}

func (s screens) delete(w http.ResponseWriter, r *http.Request) {
	err := s.service.Delete(r.Context(), r.PathValue("id"))
	if errors.Is(err, db.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Redirect(w, r, s.routes.URL("index"))
}

type settingsInput struct {
	Notify       string `form:"input,email" label:"Notification e-mail"                                         maxLength:"254" validate:"mail" description:"New proposals are mailed to this address. Leave empty to send none."`
	ConfirmMail  bool   `form:"checkbox"    label:"Confirm receipt to submitters"`
	DecisionMail bool   `form:"checkbox"    label:"Tell submitters when their proposal is accepted or rejected"`
}

// settingsScreen edits the instance's settings: the notification address
// and the mail to submitters.
func (s screens) settingsScreen() admin.FormScreen[settingsInput] {
	return admin.FormScreen[settingsInput]{
		Admin:     s.admin,
		Title:     "Settings",
		BackURL:   s.routes.URL("index"),
		BackLabel: s.env.Title,
		Heading:   "Mail",
		Intro:     "The texts of the mails are templates of the site and follow its translations.",
		Submit:    "Save",
		Load: func(ctx context.Context) (settingsInput, error) {
			current, err := s.service.Settings(ctx)

			return settingsInput(current), err
		},
		Save: func(r *http.Request, in settingsInput) (form.FieldErrors, error) {
			err := s.service.SaveSettings(r.Context(), Settings(in))
			if errors.Is(err, ErrInvalid) {
				message := s.admin.T(r, "Enter one valid e-mail address for notifications, or leave it empty.")

				return admin.FieldError("Notify", message), nil
			}

			return nil, err
		},
	}
}
