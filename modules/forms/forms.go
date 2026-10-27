// Package forms adds a public form, such as a contact form or a quote
// request, with an inbox for its submissions. Each instance is one form; add
// one instance per form and place it on a page:
//
//	app.Use(forms.Module("contact", "Contact"), forms.Module("quote", "Quote request"))
//
// Editors define the fields in the admin, one per line (see the Fields help
// text in the editor), with labels per language.
//
// The website renders forms/form.gohtml with a FormView as its data. A minimal
// template:
//
//	{{/* @dot github.com/donseba/go-webthings/modules/forms.FormView */}}
//	<h1>{{.Title}}</h1>
//	{{if .Sent}}{{.Success}}{{else}}
//	  {{.Intro}}
//	  {{form_render .Form .Errors}}
//	{{end}}
//
// form_render renders every field with go-form (see FormView.Form): its
// label, help text, placeholder and limits, the visitor's answer and the
// errors of a post at their fields, in the page's language, with the spam
// trap and a Send button. A template that lays out the fields itself uses
// {{form_open .Form}}, {{form_field .Form name .Errors}} per name of Fields
// and {{form_close}}.
//
// The form posts to its own page, so a post with errors shows the form again
// with the visitor's answers, Errors and Invalid set. Like the visitor
// actions of other modules (see webthings.Env.Actions) it rejects cross-site
// posts (http.CrossOriginProtection), tells bots that fill in the trap that
// the form was sent without storing anything, and accepts 10 posts per
// visitor in 10 minutes: after that the form is shown again with Throttled
// set and nothing stored. After a valid submission the visitor returns to
// the page with ?sent=1 and Sent is set.
//
// When the form has a notification address, every submission is mailed there
// with the answers and a link to it in the admin; replying goes to the
// visitor's address when the form has an email field. The mail is the mail
// template notify (see webthings.Module.Mail) with NotifyView as data, in
// the admin's language; a theme replaces it with mail/<key>/notify.gohtml or
// mail/forms/notify.gohtml. Submissions arrive as new and can be archived in
// the admin once handled.
package forms

import (
	"context"
	"database/sql/driver"
	"embed"
	"io/fs"
	"net/http"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-router"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// Field is one input of the form. Labels are translated; see Label.
type Field struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required,omitempty"`
	Options  []string `json:"options,omitempty"`
}

// FieldList is stored as JSON in one column.
type FieldList []Field

// Label is the text of one field in one language. Options maps option values
// to their labels.
type Label struct {
	Label       string            `json:"label"`
	Placeholder string            `json:"placeholder,omitempty"`
	Help        string            `json:"help,omitempty"`
	Options     map[string]string `json:"options,omitempty"`
}

// Labels maps field names to their text; stored as JSON in one column.
type Labels map[string]Label

// Definition is the language-independent part of a form. Notify is the
// address new submissions are mailed to; empty sends no mail.
type Definition struct {
	Fields FieldList `db:"form_fields"`
	Notify string    `db:"notify"`
}

// Text is a form in one language. Intro and Success are Markdown.
type Text struct {
	Title   string `db:"title"`
	Intro   string `db:"intro"`
	Success string `db:"success"`
	Labels  Labels `db:"field_labels"`
}

// Item is a stored form with its translations.
type Item = content.Item[Definition, Text]

func spec() content.Spec[Definition, Text] {
	return content.Spec[Definition, Text]{
		Table: "wt_forms_form",
		Complete: func(text Text) bool {
			return text.Title != "" && text.Success != "" && len(text.Labels) > 0
		},
		Check: func(definition Definition, translations []content.Translation[Text]) error {
			if err := validateFields(definition.Fields); err != nil {
				return err
			}

			if definition.Notify != "" && !mail.Valid(definition.Notify) {
				return errNotify
			}

			for _, translation := range translations {
				if err := validateText(translation.Text); err != nil {
					return err
				}
			}

			return nil
		},
		Media: func(_ Definition, translations []content.Translation[Text]) []string {
			var ids []string

			for _, translation := range translations {
				ids = append(ids, markdown.MediaIDs(translation.Text.Intro)...)
				ids = append(ids, markdown.MediaIDs(translation.Text.Success)...)
			}

			return ids
		},
	}
}

// Answer is one submitted value, with the label the visitor saw.
type Answer struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// Answers is stored as JSON in one column.
type Answers []Answer

// Statuses of a submission: new until an editor archives it.
const (
	// StatusNew is a submission no editor has handled yet; the admin
	// counts them in a badge.
	StatusNew = "new"
	// StatusArchived is a submission an editor has handled.
	StatusArchived = "archived"
)

// Submission is one sent form. DeletedAt and DeletedBy are set for
// submissions in the trash (see TrashedSubmissions).
type Submission struct {
	ID        string     `db:"id"`
	Locale    string     `db:"locale"`
	Status    string     `db:"status"`
	Answers   Answers    `db:"answers"`
	CreatedAt time.Time  `db:"created_at"`
	DeletedAt *time.Time `db:"deleted_at"`
	DeletedBy db.Ref     `db:"deleted_by"`
}

// Service is one form, for the website's own code.
type Service struct {
	key      string
	db       *db.DB
	forms    *content.Collection[Definition, Text]
	frontend *frontend.Frontend
	pages    *pages.Service
	mail     mail.Sender
	// absolute makes admin paths absolute for links in e-mail; adminURL
	// builds them from route names (webthings.Env.AdminURL).
	absolute func(r *http.Request, path string) string
	adminURL func(name string, args ...any) string
	// adminLanguage is the language of the notification mail.
	adminLanguage string
	// public are the routes below the form's page (see handler).
	public *router.Router
}

// Store gives write access to this instance's form definition, for imports and seeds.
// Admin edits go through the same store.
func (s *Service) Store() content.Store[Definition, Text] {
	return s.forms.In(s.key)
}

// Of returns the Forms instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// SubmissionsByStatus returns the submissions with one status, StatusNew or
// StatusArchived, newest first, skipping offset, and whether older ones
// exist; an empty status lists all. A limit of 0 or less returns every
// submission after offset.
func (s *Service) SubmissionsByStatus(ctx context.Context, status string, offset, limit int) ([]Submission, bool, error) {
	var list []Submission

	where, args := s.filter(status)
	query := `
		SELECT id, locale, status, answers, created_at
		FROM wt_forms_submission
		WHERE ` + where + `
		ORDER BY created_at DESC, id DESC`

	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit+1, max(offset, 0))
	}

	err := s.db.Select(ctx, &list, query, args...)
	if err != nil {
		return nil, false, err
	}

	if limit <= 0 {
		return list[min(max(offset, 0), len(list)):], false, nil
	}

	if len(list) > limit {
		return list[:limit], true, nil
	}

	return list, false, nil
}

// Count returns the number of submissions with status; an empty status
// counts all.
func (s *Service) Count(ctx context.Context, status string) (int, error) {
	var count int

	where, args := s.filter(status)

	err := s.db.Get(ctx, &count, `SELECT COUNT(*) FROM wt_forms_submission WHERE `+where, args...)

	return count, err
}

// filter is the WHERE clause for this instance's submissions with status, or
// all of them when status is empty. Submissions in the trash are left out.
func (s *Service) filter(status string) (string, []any) {
	if status == "" {
		return "instance=? AND deleted_at IS NULL", []any{s.key}
	}

	return "instance=? AND status=? AND deleted_at IS NULL", []any{s.key, status}
}

// Submission returns one submission that is not in the trash, or
// db.ErrNotFound.
func (s *Service) Submission(ctx context.Context, id string) (Submission, error) {
	var submission Submission

	err := s.db.Get(ctx, &submission, `
		SELECT id, locale, status, answers, created_at
		FROM wt_forms_submission
		WHERE instance=? AND id=? AND deleted_at IS NULL`,
		s.key, id,
	)

	return submission, err
}

// Archive marks a submission as handled; it leaves the list of new ones.
func (s *Service) Archive(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, StatusArchived)
}

// Unarchive returns an archived submission to the new ones.
func (s *Service) Unarchive(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, StatusNew)
}

// setStatus changes the status of a submission that is not in the trash,
// or returns db.ErrNotFound.
func (s *Service) setStatus(ctx context.Context, id, status string) error {
	at, by := db.Stamp(ctx)

	return db.Affected(s.db.Exec(ctx, `
		UPDATE wt_forms_submission
		SET status=?, updated_at=?, updated_by=?
		WHERE instance=? AND id=? AND deleted_at IS NULL`,
		status, at, by, s.key, id,
	))
}

// submissionRows is the form's part of wt_forms_submission, for moving
// submissions to the trash and back.
func (s *Service) submissionRows() db.SoftTable {
	return db.SoftTable{DB: s.db, Table: "wt_forms_submission", Scope: "instance=?", Args: []any{s.key}}
}

// DeleteSubmission moves a submission to the trash: it leaves the inbox and
// the counts, and UndeleteSubmission brings it back. It returns
// db.ErrNotFound when the submission is not in the inbox.
func (s *Service) DeleteSubmission(ctx context.Context, id string) error {
	return s.submissionRows().Delete(ctx, id)
}

// TrashedSubmissions returns submissions in the trash, most recently
// deleted first: at most limit of them from offset (all for a limit of 0),
// and how many the trash holds in all.
func (s *Service) TrashedSubmissions(ctx context.Context, limit, offset int) ([]Submission, int, error) {
	var list []Submission

	total, err := s.submissionRows().Trashed(ctx, &list,
		`id, locale, status, answers, created_at, deleted_at, deleted_by`, limit, offset)

	return list, total, err
}

// UndeleteSubmission brings a submission back from the trash, with the
// status it had. It returns db.ErrNotFound when it is not in the trash.
func (s *Service) UndeleteSubmission(ctx context.Context, id string) error {
	return s.submissionRows().Undelete(ctx, id)
}

// PurgeSubmission removes a submission in the trash for good. It returns
// db.ErrNotFound when it is not in the trash.
func (s *Service) PurgeSubmission(ctx context.Context, id string) error {
	return s.submissionRows().Purge(ctx, id)
}

// save stores a new submission and returns its ID. Visitors send forms, so
// created_by is usually empty.
func (s *Service) save(ctx context.Context, locale string, answers Answers) (string, error) {
	id := db.NewID()
	now, actor := db.Stamp(ctx)

	_, err := s.db.Exec(ctx, `
		INSERT INTO wt_forms_submission
			(id, instance, locale, status, answers, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, s.key, locale, StatusNew, answers, now, actor, now, actor,
	)

	return id, err
}

// Value stores the fields as JSON.
func (f FieldList) Value() (driver.Value, error) {
	return db.JSONValue(f)
}

// Scan reads fields stored by Value.
func (f *FieldList) Scan(src any) error {
	return db.ScanJSON(src, f)
}

// Value stores the labels as JSON.
func (l Labels) Value() (driver.Value, error) {
	return db.JSONValue(l)
}

// Scan reads labels stored by Value.
func (l *Labels) Scan(src any) error {
	return db.ScanJSON(src, l)
}

// Value stores the answers as JSON.
func (a Answers) Value() (driver.Value, error) {
	return db.JSONValue(a)
}

// Scan reads answers stored by Value.
func (a *Answers) Scan(src any) error {
	return db.ScanJSON(src, a)
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
