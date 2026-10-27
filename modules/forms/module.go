package forms

import (
	"context"
	"embed"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
)

//go:embed templates translations
var files embed.FS

const pageSize = 50

// Module adds a form. key is used in URLs of the admin and must be unique on
// the site; title is shown in the admin and the page editor.
func Module(key, title string) webthings.Module {
	return webthings.Module{
		Type:   "forms",
		Key:    key,
		Title:  title,
		Schema: schema(),
		Setup:  setup,
		Templates: []frontend.Template{
			{Name: "form", Data: FormView{}, Example: formExample},
		},
		Mail: []frontend.Template{
			{Name: mailNotify, Data: NotifyView{}, Example: notifyMail},
		},
		// The editors write the fields in every language; only the submit
		// button's text is the site's to translate.
		Forms: []any{form.WithInfo(form.Schema{}, form.Info{SubmitText: submitText})},
	}
}

func setup(env webthings.Env) (webthings.Parts, error) {
	formsSpec := spec()
	formsSpec.Changed = env.Changed

	forms, err := content.NewCollection(env.DB, formsSpec)
	if err != nil {
		return webthings.Parts{}, err
	}

	service := &Service{
		key:           env.Key,
		db:            env.DB,
		forms:         forms,
		frontend:      env.Frontend,
		pages:         env.Pages,
		mail:          env.Mail,
		absolute:      env.AbsoluteURL,
		adminURL:      env.AdminURL,
		adminLanguage: env.AdminLanguage,
	}

	service.public = service.handler()

	return webthings.Parts{
		Admin:   []admin.Section{section(env, service)},
		Public:  service.public,
		Service: service,
		Links:   service.linkSources,
	}, nil
}

type definitionInput struct {
	Title   string `form:"input,text"  label:"Title"               required:"true" maxLength:"200"`
	Intro   string `form:"textarea"    label:"Introduction"                        maxLength:"5000"  class:"markdown"`
	Fields  string `form:"textarea"    label:"Fields"              required:"true" maxLength:"20000"                  rows:"10" description:"One field per line: name | type | label | placeholder | help. Add * after the name of a required field. Types: text, email, textarea, select, checkbox, number, date. Options of a select follow on their own lines: - value | label"`
	Success string `form:"textarea"    label:"Success message"     required:"true" maxLength:"5000"  class:"markdown"`
	Notify  string `form:"input,email" label:"Notification e-mail"                 maxLength:"254"                              description:"New submissions are mailed to this address. Leave empty to only collect them here."                                                                                                                                                   validate:"mail" trim:"true"`
}

func section(env webthings.Env, service *Service) admin.Section {
	return editor.New(editor.Config[Definition, Text, definitionInput]{
		ID:       env.Key,
		Title:    env.Title,
		Icon:     "form",
		NewLabel: "New form",
		Single:   true,
		Links:    []editor.Link{{Label: "Submissions", Route: admin.RouteName(env.Key, "inbox")}},
		Badge:    service.badge,
		Store:    service.forms.In(env.Key),
		Trash:    service.trash(),
		Locales:  env.Locales,
		Files:    files,
		Label: func(text Text) string {
			return text.Title
		},
		Form: func(_ context.Context, item Item, locale string) (definitionInput, error) {
			text, _ := item.Text(locale)
			input := definitionInput{
				Title:   text.Title,
				Intro:   text.Intro,
				Fields:  formatFields(item.Fields.Fields, text.Labels),
				Success: text.Success,
				Notify:  item.Fields.Notify,
			}

			if item.ID == "" {
				input.Fields = exampleFields
			}

			return input, nil
		},
		Apply: func(ctx context.Context, in definitionInput, draft *content.Draft[Definition, Text], locale string) error {
			if draft.ID == "" {
				existing, err := service.forms.List(ctx, service.key)
				if err != nil {
					return err
				}

				if len(existing) > 0 {
					return errSecondForm
				}
			}

			fields, labels, err := parseFields(in.Fields)
			if err != nil {
				return err
			}

			draft.Fields = Definition{Fields: fields, Notify: in.Notify}
			draft.Translations = content.SetText(draft.Translations, locale, Text{
				Title:   in.Title,
				Intro:   in.Intro,
				Success: in.Success,
				Labels:  labels,
			})

			return nil
		},
		Issue: func(err error) string {
			var problem definitionError
			if errors.As(err, &problem) {
				return string(problem)
			}

			return ""
		},
		Routes: func(routes *admin.Routes, _ editor.Editor[Definition, Text, definitionInput]) {
			screens := inbox{service: service, admin: routes.Admin(), routes: routes, title: env.Title}

			// The editor names the form's own routes, such as "delete", so
			// those of the inbox say what they are about.
			routes.Get("/inbox", admin.View, screens.list).As("inbox")
			routes.Get("/inbox/{submission}", admin.View, screens.show).As("submission")
			routes.Post("/inbox/{submission}/archive", admin.Edit, screens.archive).As("submission-archive")
			routes.Post("/inbox/{submission}/unarchive", admin.Edit, screens.unarchive).As("submission-unarchive")
			routes.Post("/inbox/{submission}/delete", admin.Delete, screens.delete).As("submission-delete")
		},
	})
}

// badge counts the new submissions for the admin navigation. A failing count
// is logged and hides the badge rather than the page.
func (s *Service) badge(ctx context.Context) int {
	count, err := s.Count(ctx, StatusNew)
	if err != nil {
		slog.ErrorContext(ctx, "count new form submissions", "module", s.key, "error", err)

		return 0
	}

	return count
}

// inbox shows the submissions of one form.
type inbox struct {
	service *Service
	admin   *admin.Admin
	routes  *admin.Routes
	title   string
}

// InboxView is the data of templates/admin/inbox.gohtml: the submissions of a
// form, filtered by status.
type InboxView struct {
	Title   string
	BackURL string
	// TrashURL shows the deleted submissions in the admin's trash.
	TrashURL string
	// Filters switch between new, archived and all submissions.
	Filters admin.Tabs
	Rows    []InboxRow
	Pager   admin.Pager
}

// InboxRow is one submission in the inbox; Summary shows its first
// answers.
type InboxRow struct {
	URL      string
	Date     string
	Locale   string
	Summary  string
	Archived bool
}

// SubmissionView is the data of templates/admin/submission.gohtml: one
// submission with every answer.
type SubmissionView struct {
	Title    string
	BackURL  string
	Date     string
	Locale   string
	Archived bool
	Answers  Answers
	// Archive or Unarchive is set, depending on the status, for editors.
	Archive   *form.RenderModel
	Unarchive *form.RenderModel
	Delete    *form.RenderModel
}

// filter is a status the inbox can show, with its translated label.
type filter struct {
	status string
	label  string
}

// filters are the statuses the inbox can show; the first is the default.
var filters = []filter{
	{StatusNew, "New"},
	{StatusArchived, "Archived"},
	{"all", "All"},
}

func (i inbox) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status := r.URL.Query().Get("status")

	known := slices.ContainsFunc(filters, func(candidate filter) bool {
		return candidate.status == status
	})
	if !known {
		status = StatusNew
	}

	query := status
	if status == "all" {
		query = ""
	}

	// A search looks through the answers of the status, then pages.
	page, err := admin.ListPage[Submission]{
		Path: i.routes.URL("inbox"),
		Size: pageSize,
		Load: func(ctx context.Context, limit, offset int) ([]Submission, bool, error) {
			return i.service.SubmissionsByStatus(ctx, query, offset, limit)
		},
		Fields: func(submission Submission) []string {
			values := []string{""}
			for _, answer := range submission.Answers {
				values = append(values, answer.Value)
			}

			return values
		},
	}.Read(r)
	if err != nil {
		i.admin.Fail(w, r, err)

		return
	}

	fresh, err := i.service.Count(ctx, StatusNew)
	if err != nil {
		i.admin.Fail(w, r, err)

		return
	}

	view := InboxView{
		Title:    i.title,
		BackURL:  i.routes.URL("index"),
		TrashURL: i.admin.TrashURL(i.service.key),
		Pager:    page.Pager,
	}

	tabs := make([]admin.Tab, 0, len(filters))
	for _, filter := range filters {
		tab := admin.Tab{Label: filter.label, Value: filter.status}
		if filter.status == StatusNew {
			// New ones are counted while there are any.
			tab.Count, tab.Counted = fresh, fresh > 0
		}

		tabs = append(tabs, tab)
	}

	view.Filters = admin.FilterTabs(r, "Status", i.routes.URL("inbox"), "status", status, tabs...)

	for _, submission := range page.Rows {
		view.Rows = append(view.Rows, InboxRow{
			URL:      i.routes.URL("submission", "submission", submission.ID),
			Date:     admin.DateTime(ctx, submission.CreatedAt),
			Locale:   submission.Locale,
			Summary:  summary(submission.Answers),
			Archived: submission.Status == StatusArchived,
		})
	}

	i.admin.Render(w, r, admin.Page{
		Title:    "Submissions",
		Template: "templates/admin/inbox.gohtml",
		Files:    files,
		Data:     view,
	})
}

// listURL is the inbox filtered by status, on page.
func (i inbox) listURL(status string, page int) string {
	query := url.Values{}

	if status != StatusNew {
		query.Set("status", status)
	}

	if page > 1 {
		query.Set("page", strconv.Itoa(page))
	}

	return i.routes.URL("inbox", query)
}

func (i inbox) show(w http.ResponseWriter, r *http.Request) {
	submission, err := i.service.Submission(r.Context(), r.PathValue("submission"))
	if errors.Is(err, content.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		i.admin.Fail(w, r, err)

		return
	}

	archived := submission.Status == StatusArchived
	view := SubmissionView{
		Title:    i.title,
		BackURL:  i.routes.URL("inbox"),
		Date:     admin.DateTime(r.Context(), submission.CreatedAt),
		Locale:   submission.Locale,
		Archived: archived,
		Answers:  submission.Answers,
	}

	if archived {
		view.BackURL = i.listURL(StatusArchived, 1)
	}

	if i.admin.Can(r, admin.Permission{Section: i.service.key, Action: admin.Edit}) {
		if archived {
			unarchive := i.admin.Form(
				r,
				struct{}{},
				i.routes.URL("submission-unarchive", "submission", submission.ID),
				"Move back to new",
			)
			view.Unarchive = &unarchive
		} else {
			archive := i.admin.Form(r, struct{}{}, i.routes.URL("submission-archive", "submission", submission.ID), "Archive")
			view.Archive = &archive
		}
	}

	if i.admin.Can(r, admin.Permission{Section: i.service.key, Action: admin.Delete}) {
		remove := i.admin.Form(r, struct{}{}, i.routes.URL("submission-delete", "submission", submission.ID), "Move to trash")
		view.Delete = &remove
	}

	i.admin.Render(w, r, admin.Page{
		Title:    "Submission",
		Template: "templates/admin/submission.gohtml",
		Files:    files,
		Data:     view,
	})
}

// archive files a handled submission and returns to the new ones.
func (i inbox) archive(w http.ResponseWriter, r *http.Request) {
	err := i.service.Archive(r.Context(), r.PathValue("submission"))
	if !i.done(w, r, err) {
		return
	}

	i.admin.Redirect(w, r, i.routes.URL("inbox"))
}

func (i inbox) unarchive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("submission")

	err := i.service.Unarchive(r.Context(), id)
	if !i.done(w, r, err) {
		return
	}

	i.admin.Redirect(w, r, i.routes.URL("submission", "submission", id))
}

// delete moves the submission to the trash.
func (i inbox) delete(w http.ResponseWriter, r *http.Request) {
	err := i.service.DeleteSubmission(r.Context(), r.PathValue("submission"))
	if !i.done(w, r, err) {
		return
	}

	i.admin.Redirect(w, r, i.routes.URL("inbox"))
}

// done answers a failed change itself and reports whether it succeeded. A
// submission that is gone, also to the trash, is not found.
func (i inbox) done(w http.ResponseWriter, r *http.Request, err error) bool {
	if errors.Is(err, content.ErrNotFound) {
		http.NotFound(w, r)

		return false
	}

	if err != nil {
		i.admin.Fail(w, r, err)

		return false
	}

	return true
}

// summary shortens the first answers to one line for the inbox list.
func summary(answers Answers) string {
	var parts []string

	for _, answer := range answers {
		if answer.Value != "" && len(parts) < 3 {
			parts = append(parts, answer.Value)
		}
	}

	line := strings.Join(strings.Fields(strings.Join(parts, " · ")), " ")
	if runes := []rune(line); len(runes) > 100 {
		line = string(runes[:100]) + "…"
	}

	return line
}

// formExample is the starting point for forms/form.gohtml: a working form
// with go-form's form_render (every field, the hidden spam trap and the
// errors), the throttle notice and the success message.
const formExample = `{{/* @dot github.com/donseba/go-webthings/modules/forms.FormView */}}
{{/* Starting point generated by WebThings: a complete working form. */}}
<main id="content">
    <h1>{{.Title}}</h1>

    {{if .Sent}}
        <div>{{.Success}}</div>
    {{else}}
        <div>{{.Intro}}</div>

        {{if .Throttled}}<p>You sent this form several times already. Please try again later.</p>{{end}}
        {{if .Invalid}}<p>Please check the marked fields.</p>{{end}}

        {{form_render .Form .Errors}}
    {{end}}
</main>
`
