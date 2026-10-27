package newsletter

import (
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/media"

	form "github.com/donseba/go-form/v2"
)

const pageSize = 50

// screens are the admin pages of one instance.
type screens struct {
	service *Service
	admin   *admin.Admin
	routes  *admin.Routes
	title   string
}

// SubscribersView is the data of templates/admin/subscribers.gohtml: the
// subscribers, filtered by status.
type SubscribersView struct {
	Title   string
	Filters admin.Tabs
	Rows    []SubscriberRow
	// ExportURL is empty without the right to export.
	ExportURL   string
	MailingsURL string
	TextsURL    string
	// TrashURL shows the deleted subscribers and mailings in the admin's trash.
	TrashURL string
	// ImportURL is set for editors who may add subscribers.
	ImportURL string
	Pager     admin.Pager
}

// SubscriberRow is one subscriber in the list, with its dates as shown.
type SubscriberRow struct {
	Email     string
	Name      string
	Locale    string
	Status    string
	Created   string
	Confirmed string
	Delete    *form.RenderModel
}

// MailingsView is the data of templates/admin/mailings.gohtml: drafts and
// sent mailings.
type MailingsView struct {
	Title   string
	BackURL string
	NewURL  string
	// TrashURL shows the deleted subscribers and mailings in the admin's trash.
	TrashURL string
	Rows     []MailingRow
}

// MailingRow is one mailing in the list; Sent is empty for drafts.
type MailingRow struct {
	Subject  string
	URL      string
	Language string
	Sent     string
	// Sending is set while the messages go out; Recipients counts them so far.
	Sending    bool
	Recipients int
}

// MailingView is the data of templates/admin/mailing.gohtml: a draft to
// edit and send, or a sent mailing with its progress.
type MailingView struct {
	Title   string
	BackURL string
	// Form edits a draft; nil for sent mailings and without edit rights.
	Form   *form.RenderModel
	Errors form.FieldErrors
	// Preview is the rendered body, for saved mailings.
	Preview  template.HTML
	Subject  string
	Language string
	// Audience is the number of subscribers a draft would go to now.
	Audience int
	// URL is the mailing's page, which shows the progress while it is sent.
	URL string
	// Sent is when sending started; Finished when every message went out.
	Sent     string
	Finished string
	// Sending is set while the messages go out. Total is the audience when
	// sending started; Recipients and Failed count the messages so far.
	Sending    bool
	Total      int
	Recipients int
	Failed     int
	Send       *form.RenderModel
	Delete     *form.RenderModel
}

type mailingInput struct {
	Subject  string                    `form:"input,text" label:"Subject"  required:"true" maxLength:"200" minLength:"5" trim:"true"`
	Language form.SortedSelect[string] `form:"dropdown"   label:"Language"                                                           placeholder:"All languages"`

	Body string `form:"textarea" label:"Message" required:"true" maxLength:"100000" rows:"14" class:"markdown" description:"A personal unsubscribe link is added below the message."`
}

// statuses are the filter tabs, in order; "" is all subscribers.
var statuses = []struct {
	status string
	label  string
}{
	{"", "All"},
	{Pending, "Pending"},
	{Confirmed, "Confirmed"},
	{Unsubscribed, "Unsubscribed"},
}

func statusLabel(status string) string {
	for _, entry := range statuses {
		if entry.status == status {
			return entry.label
		}
	}

	return status
}

// validStatus returns status when it is a known filter, else "".
func validStatus(status string) string {
	for _, entry := range statuses {
		if entry.status == status {
			return status
		}
	}

	return ""
}

func (s screens) subscribers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status := validStatus(r.URL.Query().Get("status"))

	counts, err := s.service.Counts(ctx)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	// A search looks through the subscribers of the status, then pages.
	page, err := admin.ListPage[Subscriber]{
		Path: s.routes.URL("index"),
		Size: pageSize,
		Load: func(ctx context.Context, limit, offset int) ([]Subscriber, bool, error) {
			return s.service.Subscribers(ctx, status, offset, limit)
		},
		Fields: func(subscriber Subscriber) []string {
			return []string{subscriber.Email, subscriber.Name}
		},
	}.Read(r)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	view := SubscribersView{
		Title:       s.title,
		MailingsURL: s.routes.URL("mailings"),
		TextsURL:    s.admin.URL(admin.RouteName(s.service.key+"-texts", "index")),
		TrashURL:    s.admin.TrashURL(s.service.key),
		Pager:       page.Pager,
	}

	if s.can(r, admin.Create) {
		view.ImportURL = s.routes.URL("import")
	}

	// Exporting personal data needs its own right.
	if s.can(r, admin.Export) {
		export := url.Values{}
		if status != "" {
			export.Set("status", status)
		}

		view.ExportURL = s.routes.URL("export", export)
	}

	totals := map[string]int{
		"":           counts.Total(),
		Pending:      counts.Pending,
		Confirmed:    counts.Confirmed,
		Unsubscribed: counts.Unsubscribed,
	}

	tabs := make([]admin.Tab, 0, len(statuses))
	for _, entry := range statuses {
		tabs = append(tabs, admin.Tab{Label: entry.label, Value: entry.status, Count: totals[entry.status], Counted: true})
	}

	view.Filters = admin.FilterTabs(r, "Status", s.routes.URL("index"), "status", status, tabs...)

	canDelete := s.can(r, admin.Delete)

	for _, subscriber := range page.Rows {
		row := SubscriberRow{
			Email:     subscriber.Email,
			Name:      subscriber.Name,
			Locale:    subscriber.Locale,
			Status:    statusLabel(subscriber.Status),
			Created:   admin.DateTime(ctx, subscriber.CreatedAt),
			Confirmed: optionalDate(ctx, subscriber.ConfirmedAt),
		}

		if canDelete {
			remove := s.admin.Form(r, struct{}{}, s.routes.URL("subscriber-delete", "subscriber", subscriber.ID), "Move to trash")
			row.Delete = &remove
		}

		view.Rows = append(view.Rows, row)
	}

	s.admin.Render(w, r, admin.Page{
		Title:    s.title,
		Template: "templates/admin/subscribers.gohtml",
		Files:    files,
		Data:     view,
	})
}

// export downloads the subscribers with the selected status as CSV.
func (s screens) export(w http.ResponseWriter, r *http.Request) {
	status := validStatus(r.URL.Query().Get("status"))

	list, err := s.service.list(r.Context(), status, 0, 0)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	name := s.service.key + "-subscribers"
	if status != "" {
		name += "-" + status
	}

	rows := make([][]string, 0, len(list))

	for _, subscriber := range list {
		confirmed := ""
		if subscriber.ConfirmedAt != nil {
			confirmed = subscriber.ConfirmedAt.UTC().Format(time.RFC3339)
		}

		rows = append(rows, []string{
			subscriber.Email,
			subscriber.Name,
			subscriber.Locale,
			subscriber.Status,
			subscriber.CreatedAt.UTC().Format(time.RFC3339),
			confirmed,
		})
	}

	header := []string{"email", "name", "locale", "status", "created_at", "confirmed_at"}
	if err := admin.WriteCSV(w, name, header, rows); err != nil {
		slog.WarnContext(r.Context(), "export subscribers", "module", s.service.key, "error", err)
	}
}

func (s screens) deleteSubscriber(w http.ResponseWriter, r *http.Request) {
	err := s.service.DeleteSubscriber(r.Context(), r.PathValue("subscriber"))
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

func (s screens) mailings(w http.ResponseWriter, r *http.Request) {
	list, err := s.service.Mailings(r.Context())
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	view := MailingsView{Title: s.title, BackURL: s.routes.URL("index"), TrashURL: s.admin.TrashURL(s.service.key)}
	ctx := r.Context()

	if s.can(r, admin.Create) {
		view.NewURL = s.routes.URL("mailing-new")
	}

	search := admin.SearchOf(r)

	for _, mailing := range list {
		if !search.Matches(mailing.Subject) {
			continue
		}

		view.Rows = append(view.Rows, MailingRow{
			Subject:    mailing.Subject,
			URL:        s.routes.URL("mailing", "mailing", mailing.ID),
			Language:   languageName(ctx, mailing.Locale),
			Sent:       optionalDate(ctx, mailing.SentAt),
			Sending:    mailing.Sending(),
			Recipients: mailing.Recipients,
		})
	}

	s.admin.Render(w, r, admin.Page{
		Title:    "Mailings",
		Template: "templates/admin/mailings.gohtml",
		Files:    files,
		Data:     view,
	})
}

func (s screens) newMailing(w http.ResponseWriter, r *http.Request) {
	input, err := s.input(r, Mailing{})
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.renderMailing(w, r, Mailing{}, &input, nil, "", http.StatusOK)
}

func (s screens) createMailing(w http.ResponseWriter, r *http.Request) {
	s.store(w, r, Mailing{})
}

func (s screens) saveMailing(w http.ResponseWriter, r *http.Request) {
	mailing, ok := s.load(w, r)
	if !ok {
		return
	}

	s.store(w, r, mailing)
}

// store validates the posted form and saves it into mailing.
func (s screens) store(w http.ResponseWriter, r *http.Request, mailing Mailing) {
	input, err := s.input(r, mailing)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	errs, err := s.admin.Bind(r, &input)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		s.renderMailing(w, r, mailing, &input, errs, "", http.StatusUnprocessableEntity)

		return
	}

	mailing.Subject = input.Subject
	mailing.Locale = input.Language.Get()
	mailing.Body = input.Body

	saved, err := s.service.SaveMailing(r.Context(), mailing)
	if errors.Is(err, media.ErrNotFound) {
		message := s.admin.T(r, "A referenced media file no longer exists.")
		s.renderMailing(w, r, mailing, &input, nil, message, http.StatusUnprocessableEntity)

		return
	}

	if errors.Is(err, db.ErrNotFound) {
		// The mailing was sent or deleted meanwhile.
		s.admin.Redirect(w, r, s.routes.URL("mailings"))

		return
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Redirect(w, r, s.routes.URL("mailing", "mailing", saved.ID))
}

func (s screens) mailing(w http.ResponseWriter, r *http.Request) {
	mailing, ok := s.load(w, r)
	if !ok {
		return
	}

	var input *mailingInput

	if mailing.SentAt == nil && s.can(r, admin.Edit) {
		draft, err := s.input(r, mailing)
		if err != nil {
			s.admin.Fail(w, r, err)

			return
		}

		input = &draft
	}

	s.renderMailing(w, r, mailing, input, nil, "", http.StatusOK)
}

// sendMailing starts sending a draft in the background; its page shows the
// progress.
func (s screens) sendMailing(w http.ResponseWriter, r *http.Request) {
	target := s.routes.URL("mailing", "mailing", r.PathValue("mailing"))

	_, err := s.service.startSending(r.Context(), r.PathValue("mailing"), s.service.absolute(r, ""))

	switch {
	case errors.Is(err, db.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, errAlreadySent):
		s.admin.Redirect(w, r, target)
	case err != nil:
		s.admin.Fail(w, r, err)
	default:
		s.admin.RedirectNotice(w, r, target, "The mailing is being sent.")
	}
}

// deleteMailing moves the mailing to the trash; one that is being sent
// stops.
func (s screens) deleteMailing(w http.ResponseWriter, r *http.Request) {
	err := s.service.DeleteMailing(r.Context(), r.PathValue("mailing"))
	if errors.Is(err, db.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Redirect(w, r, s.routes.URL("mailings"))
}

// load returns the mailing in the URL, answering not found itself.
func (s screens) load(w http.ResponseWriter, r *http.Request) (Mailing, bool) {
	mailing, err := s.service.Mailing(r.Context(), r.PathValue("mailing"))
	if errors.Is(err, db.ErrNotFound) {
		http.NotFound(w, r)

		return Mailing{}, false
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return Mailing{}, false
	}

	return mailing, true
}

// input fills the mailing form, with the site's languages as choices.
func (s screens) input(r *http.Request, mailing Mailing) (mailingInput, error) {
	config, err := s.service.locales.Configuration(r.Context())
	if err != nil {
		return mailingInput{}, err
	}

	choices := map[string]string{}

	for _, code := range config.Enabled {
		choices[code] = admin.LanguageLabel(code)
	}

	language := form.NewSortedSelect(choices).Selected(mailing.Locale)

	return mailingInput{
		Subject:  mailing.Subject,
		Language: language,
		Body:     mailing.Body,
	}, nil
}

func (s screens) renderMailing(
	w http.ResponseWriter,
	r *http.Request,
	mailing Mailing,
	input *mailingInput,
	errs form.FieldErrors,
	message string,
	status int,
) {
	// The message shows as a toast; errors of single fields stay inline.
	r = admin.WithError(r, message)

	ctx := r.Context()
	view := MailingView{
		Title:      s.admin.T(r, "New mailing"),
		BackURL:    s.routes.URL("mailings"),
		Errors:     errs,
		Subject:    mailing.Subject,
		Language:   languageName(ctx, mailing.Locale),
		Sent:       optionalDate(ctx, mailing.SentAt),
		Finished:   optionalDate(ctx, mailing.FinishedAt),
		Sending:    mailing.Sending(),
		Total:      mailing.Total,
		Recipients: mailing.Recipients,
		Failed:     mailing.Failed,
	}

	if mailing.ID != "" {
		view.URL = s.routes.URL("mailing", "mailing", mailing.ID)
	}

	if input != nil {
		action := s.routes.URL("mailing-new")
		if mailing.ID != "" {
			action = s.routes.URL("mailing-save", "mailing", mailing.ID)
		}

		model := s.admin.Form(r, *input, action, "Save")
		view.Form = &model
	}

	if mailing.ID != "" {
		view.Title = mailing.Subject
		view.Preview = s.service.frontend.Markdown(ctx, mailing.Body)

		if s.can(r, admin.Delete) {
			remove := s.admin.Form(r, struct{}{}, s.routes.URL("mailing-delete", "mailing", mailing.ID), "Move to trash")

			// Deleting stops a mailing that is being sent, for good.
			if mailing.Sending() {
				remove = s.admin.Confirm(
					r,
					remove,
					"Stop sending this mailing and move it to the trash? Subscribers who did not get it yet will not get it.",
				)
			}

			view.Delete = &remove
		}
	}

	if mailing.ID != "" && mailing.SentAt == nil {
		audience, err := s.service.audience(ctx, mailing.Locale)
		if err != nil {
			s.admin.Fail(w, r, err)

			return
		}

		view.Audience = audience

		if s.can(r, admin.Publish) && view.Audience > 0 {
			send := s.admin.Form(r, struct{}{}, s.routes.URL("mailing-send", "mailing", mailing.ID), "Send to subscribers")
			send = s.admin.Confirm(r, send, "Send this mailing now? This cannot be undone.")
			view.Send = &send
		}
	}

	// The subject, or the translated "New mailing", is shown as written.
	s.admin.Render(w, r, admin.Page{
		Title:    "Mailings",
		Label:    view.Title,
		Template: "templates/admin/mailing.gohtml",
		Files:    files,
		Data:     view,
		Status:   status,
	})
}

func (s screens) can(r *http.Request, action string) bool {
	return s.admin.Can(r, admin.Permission{Section: s.service.key, Action: action})
}

// optionalDate shows t in the admin language and the site's timezone, or
// nothing.
func optionalDate(ctx context.Context, t *time.Time) string {
	if t == nil {
		return ""
	}

	return admin.DateTime(ctx, *t)
}

// languageName names the language of a mailing in the admin language; empty
// (all languages) stays empty.
func languageName(ctx context.Context, code string) string {
	if code == "" {
		return ""
	}

	return admin.LanguageLabel(code)
}
