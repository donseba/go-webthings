package comments

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/db"

	form "github.com/donseba/go-form/v2"
)

// Filters of the admin list.
const (
	filterPending  = "pending"
	filterApproved = "approved"
	filterAll      = "all"
)

// reviewPage is the number of messages per admin page.
const reviewPage = 50

type replyInput struct {
	Body string `form:"textarea" label:"Reply" required:"true" maxLength:"5000" rows:"6"`
}

type settingsInput struct {
	Moderated bool   `form:"checkbox"    label:"Approve messages before they are shown"`
	Closed    bool   `form:"checkbox"    label:"Closed for new messages"`
	Notify    string `form:"input,email" label:"Notification address"                   maxLength:"254" validate:"mail" description:"Receives an e-mail for every new message. Leave empty for none."`
}

func section(env webthings.Env, service *Service) admin.Section {
	return admin.Section{
		ID:    env.Key,
		Title: env.Title,
		Icon:  "megaphone",
		Files: files,
		Trash: service.trash(),
		// The badge counts the messages that wait for approval.
		Badge: func(ctx context.Context) int {
			pending, err := service.Pending(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "count pending comments", "module", env.Key, "error", err)
			}

			return pending
		},
		Routes: func(routes *admin.Routes) {
			screens := screens{env: env, service: service, admin: routes.Admin(), routes: routes}

			routes.Get("", admin.View, screens.list).As("index")
			settings := screens.settingsScreen()
			routes.Get("/settings", admin.View, settings.Show).As("settings")
			routes.Post("/settings", admin.Edit, settings.Post)
			routes.Post("/{comment}/publish", admin.Publish, screens.approve(true)).As("publish")
			routes.Post("/{comment}/unpublish", admin.Publish, screens.approve(false)).As("unpublish")
			routes.Post("/{comment}/delete", admin.Delete, screens.delete).As("delete")
			routes.Get("/{comment}/reply", admin.Create, screens.reply).As("reply")
			routes.Post("/{comment}/reply", admin.Create, screens.saveReply)
		},
	}
}

// screens are the admin pages of one instance.
type screens struct {
	env     webthings.Env
	service *Service
	admin   *admin.Admin
	// routes build the addresses of the section's routes by name.
	routes *admin.Routes
}

// InboxView is the data of templates/admin/list.gohtml.
type InboxView struct {
	Title   string
	Pending int
	// Filters switch between pending, approved and all messages.
	Filters     admin.Tabs
	Groups      []InboxGroup
	SettingsURL string
	TrashURL    string
	Pager       admin.Pager
}

// InboxGroup is the messages on one subject.
type InboxGroup struct {
	Subject string
	// Title names the subject for people: the instance title for the
	// instance's own subject (a guestbook), otherwise the page the messages
	// were written on, or the subject key when no message knows its page.
	Title string
	Rows  []InboxRow
}

// InboxRow is one message in the admin list.
type InboxRow struct {
	Date   string
	Name   string
	Email  string
	Locale string
	Body   string
	// PageURL is the page the message was written on.
	PageURL  string
	ReplyTo  string
	Staff    bool
	Approved bool
	// Sender is a short code that is equal for messages from one address on
	// one day.
	Sender   string
	Approve  *form.RenderModel
	Hide     *form.RenderModel
	Delete   *form.RenderModel
	ReplyURL string
}

// ReplyView is the data of templates/admin/reply.gohtml.
type ReplyView struct {
	Title   string
	BackURL string
	Name    string
	Date    string
	Body    string
	Form    form.RenderModel
	Errors  form.FieldErrors
}

func (s screens) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	pending, approved, err := s.service.counts(ctx)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	filter := r.URL.Query().Get("filter")
	if filter != filterPending && filter != filterApproved && filter != filterAll {
		filter = filterAll
		if pending > 0 {
			filter = filterPending
		}
	}

	// A search looks through every message of the filter, then pages.
	page, err := admin.ListPage[record]{
		Path: s.routes.URL("index"),
		Size: reviewPage,
		Load: func(ctx context.Context, limit, offset int) ([]record, bool, error) {
			return s.service.review(ctx, filter, offset, limit)
		},
		Fields: func(record record) []string {
			return []string{record.Name, record.Email, record.Body, record.Path, record.Subject}
		},
	}.Read(r)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	view := InboxView{
		Title:       s.env.Title,
		Pending:     pending,
		SettingsURL: s.routes.URL("settings"),
		TrashURL:    s.admin.TrashURL(s.env.Key),
		Pager:       page.Pager,
		Filters: admin.FilterTabs(r, "Filter", s.routes.URL("index"), "filter", filter,
			admin.Tab{Label: "Pending", Value: filterPending, Count: pending, Counted: true},
			admin.Tab{Label: "Approved", Value: filterApproved, Count: approved, Counted: true},
			admin.Tab{Label: "All", Value: filterAll, Count: pending + approved, Counted: true},
		),
	}

	// Messages are grouped by subject; groups follow their newest message.
	position := map[string]int{}

	for _, record := range page.Rows {
		i, ok := position[record.Subject]
		if !ok {
			i = len(view.Groups)
			position[record.Subject] = i
			view.Groups = append(view.Groups, InboxGroup{Subject: record.Subject, Title: s.subjectTitle(record.Subject)})
		}

		group := &view.Groups[i]
		if group.Title == record.Subject && record.Path != "" {
			group.Title = record.Path
		}

		group.Rows = append(group.Rows, s.row(r, record, filter))
	}

	s.admin.Render(w, r, admin.Page{
		Title:    s.env.Title,
		Template: "templates/admin/list.gohtml",
		Files:    files,
		Data:     view,
	})
}

// subjectTitle is the title of a subject before its messages are read: the
// instance title for the instance's own subject, such as a guestbook's, and
// the key otherwise.
func (s screens) subjectTitle(subject string) string {
	if subject == s.env.Key && s.env.Title != "" {
		return s.env.Title
	}

	return subject
}

func (s screens) row(r *http.Request, record record, filter string) InboxRow {
	query := url.Values{"filter": {filter}}

	row := InboxRow{
		Date:     admin.DateTime(r.Context(), record.CreatedAt),
		Name:     record.Name,
		Email:    record.Email,
		Locale:   record.Locale,
		Body:     record.Body,
		PageURL:  record.Path,
		ReplyTo:  record.ParentName,
		Staff:    record.Staff,
		Approved: record.Approved,
	}

	if len(record.IPHash) >= 8 {
		row.Sender = record.IPHash[:8]
	}

	if s.can(r, admin.Publish) {
		if record.Approved {
			hide := s.admin.Form(r, struct{}{}, s.routes.URL("unpublish", "comment", record.ID, query), "Hide")
			row.Hide = &hide
		} else {
			approve := s.admin.Form(r, struct{}{}, s.routes.URL("publish", "comment", record.ID, query), "Approve")
			row.Approve = &approve
		}
	}

	if s.can(r, admin.Delete) {
		remove := s.admin.Form(r, struct{}{}, s.routes.URL("delete", "comment", record.ID, query), "Move to trash")
		row.Delete = &remove
	}

	// A reply to a reply joins the same thread; staff do not answer themselves.
	if s.can(r, admin.Create) && !record.Staff {
		row.ReplyURL = s.routes.URL("reply", "comment", record.ID)
	}

	return row
}

func (s screens) approve(approved bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := s.service.Approve(r.Context(), r.PathValue("comment"), approved)
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)

			return
		}

		if err != nil {
			s.admin.Fail(w, r, err)

			return
		}

		s.admin.Redirect(w, r, s.listURL(r.URL.Query().Get("filter"), 1))
	}
}

func (s screens) delete(w http.ResponseWriter, r *http.Request) {
	err := s.service.Delete(r.Context(), r.PathValue("comment"))
	if errors.Is(err, db.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Redirect(w, r, s.listURL(r.URL.Query().Get("filter"), 1))
}

func (s screens) reply(w http.ResponseWriter, r *http.Request) {
	s.renderReply(w, r, replyInput{}, nil, http.StatusOK)
}

func (s screens) saveReply(w http.ResponseWriter, r *http.Request) {
	var in replyInput

	errs, err := s.admin.Bind(r, &in)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		s.renderReply(w, r, in, errs, http.StatusUnprocessableEntity)

		return
	}

	ctx := r.Context()

	parent, err := s.service.get(ctx, r.PathValue("comment"))
	if errors.Is(err, db.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	name, err := s.staffName(r, parent.Locale)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	_, err = s.service.Reply(ctx, parent.ID, name, in.Body)
	if errors.Is(err, ErrInvalid) {
		s.renderReply(w, r, in, nil, http.StatusUnprocessableEntity)

		return
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Redirect(w, r, s.listURL(filterAll, 1))
}

func (s screens) renderReply(w http.ResponseWriter, r *http.Request, in replyInput, errs form.FieldErrors, status int) {
	parent, err := s.service.get(r.Context(), r.PathValue("comment"))
	if errors.Is(err, db.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Render(w, r, admin.Page{
		Title:    "Reply",
		Template: "templates/admin/reply.gohtml",
		Files:    files,
		Status:   status,
		Data: ReplyView{
			Title:   s.env.Title,
			BackURL: s.routes.URL("index"),
			Name:    parent.Name,
			Date:    admin.DateTime(r.Context(), parent.CreatedAt),
			Body:    parent.Body,
			Form:    s.admin.Form(r, in, s.routes.URL("reply", "comment", parent.ID), "Send reply"),
			Errors:  errs,
		},
	})
}

// staffName signs replies with the site title in the comment's language, or
// the organisation, or the user's own name; reply.gohtml says so.
func (s screens) staffName(r *http.Request, locale string) (string, error) {
	settings, err := s.env.Site.Settings(r.Context())
	if err != nil {
		return "", err
	}

	if title := settings.Text(locale).Title; title != "" {
		return title, nil
	}

	if settings.Organisation != "" {
		return settings.Organisation, nil
	}

	user, _ := auth.CurrentUser(r.Context())

	return user.DisplayName(), nil
}

// settingsScreen edits the instance's settings; it posts to its own address.
func (s screens) settingsScreen() admin.FormScreen[settingsInput] {
	return admin.FormScreen[settingsInput]{
		Admin:     s.admin,
		Title:     "Settings",
		BackURL:   s.routes.URL("index"),
		BackLabel: s.env.Title,
		Submit:    "Save",
		Load: func(ctx context.Context) (settingsInput, error) {
			current, err := s.service.Settings(ctx)

			return settingsInput(current), err
		},
		Save: func(r *http.Request, in settingsInput) (form.FieldErrors, error) {
			err := s.service.SaveSettings(r.Context(), Settings(in))
			if errors.Is(err, ErrInvalid) {
				return admin.FieldError("Notify", s.admin.T(r, "Enter one e-mail address.")), nil
			}

			return nil, err
		},
	}
}

func (s screens) can(r *http.Request, action string) bool {
	return s.admin.Can(r, admin.Permission{Section: s.service.key, Action: action})
}

func (s screens) listURL(filter string, page int) string {
	query := url.Values{}

	if filter != "" {
		query.Set("filter", filter)
	}

	if page > 1 {
		query.Set("page", strconv.Itoa(page))
	}

	return s.routes.URL("index", query)
}
