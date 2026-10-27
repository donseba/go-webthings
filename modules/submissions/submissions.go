// Package submissions lets visitors propose a news item: a title, a text and
// an optional image, with their name and e-mail address. Editors review the
// proposals in the admin and accept one as a draft post in a Publishing
// instance, where it is edited and published as usual, or reject it. Add an
// instance with the key of the Publishing instance it feeds:
//
//	app.Use(publishing.Module("news", "News"), submissions.Module("tips", "News tips", "news"))
//
// # Form
//
// The form posts to the module's action, multipart because of the image;
// submitInput gives its model (ProposalInput) for go-form's functions:
//
//	<div id="tips">
//	    {{with submitInput "tips"}}
//	        {{form_open .}}
//	            {{form_field . "name" nil}} {{form_field . "email" nil}}
//	            {{form_field . "title" nil}} {{form_field . "text" nil}}
//	            {{form_field . "image" nil}}
//	            <button>Send</button>
//	        {{form_close}}
//	    {{end}}
//	</div>
//
// Fields: name (200 characters), email, title (200 characters), text (plain
// text or Markdown, 20000 characters) and the optional image (JPEG, PNG or
// GIF up to 5 MB, stored in the media library). form_open adds the
// multipart encoding, the spam trap and the page to return to: the element
// with the instance key as its id. The visitor returns to the form's page with
// ?<key>=sent, ?<key>=invalid when a field is missing or wrong, or
// ?<key>=throttled after too many proposals (five per hour); spam is answered
// with sent and dropped. The proposal keeps the language of that page.
//
// # Templates
//
// Placed on a page, the module renders submissions/form.gohtml with a FormView
// (App.WriteTemplates writes a working starting point). The form can also be
// placed in any other template with the functions:
//
//	{{submitURL "tips"}}     the address the form posts to
//	{{submitInput "tips"}}   the form's model, as above
//	{{submitResult "tips"}}  "sent", "invalid", "throttled" or "" after the visitor's post
//
// for example {{if eq (submitResult "tips") "sent"}}<p>Thank you!</p>{{end}}.
//
// # Mail
//
// In the admin settings of the instance editors set the address that receives
// every new proposal, and whether submitters get a confirmation after sending
// and a message when their proposal is accepted or rejected. The messages are
// mail templates (see webthings.Module.Mail) with MailView as data: received,
// accepted and rejected go to the submitter in the proposal's language,
// notify to the editors in the admin's language. The defaults are English
// text in tl, so the site's translations translate them; a theme replaces
// them with mail/<key>/<name>.gohtml or mail/submissions/<name>.gohtml.
package submissions

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/pages"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// Statuses of a submission. New ones wait for an editor's decision.
const (
	// StatusNew is a proposal no editor has decided on yet.
	StatusNew = "new"
	// StatusAccepted is a proposal that became a draft post.
	StatusAccepted = "accepted"
	// StatusRejected is a proposal an editor turned down.
	StatusRejected = "rejected"
)

var (
	// ErrInvalid is returned for a proposal with a missing or wrong field.
	ErrInvalid = errors.New("submissions_invalid")
	// ErrDecided is returned when accepting an accepted submission or
	// rejecting one that is no longer new.
	ErrDecided = errors.New("submissions_decided")
	// ErrNoTarget is returned when accepting a submission while the target
	// key names no Publishing instance.
	ErrNoTarget = errors.New("submissions_no_target")
)

// proposalChecks checks proposals against the limits of ProposalInput's
// tags, the visitor's form.
var proposalChecks = form.NewForm()

// Proposal is what a visitor sends.
type Proposal struct {
	Name  string
	Email string
	Title string
	// Body is plain text or Markdown.
	Body string
	// MediaID is the uploaded image in the media library, or empty.
	MediaID string
	Locale  string
}

// Submission is a stored proposal with the editors' decision.
type Submission struct {
	ID      string
	Name    string
	Email   string
	Title   string
	Body    string
	MediaID string
	Locale  string
	Status  string
	// PostID is the draft post of an accepted submission.
	PostID    string
	CreatedAt time.Time
	// DecidedAt and DecidedBy (a user ID) are set once accepted or rejected.
	DecidedAt time.Time
	DecidedBy string
}

// row is a submission as stored, with its nullable columns.
type row struct {
	ID        string     `db:"id"`
	Name      string     `db:"name"`
	Email     string     `db:"email"`
	Title     string     `db:"title"`
	Body      string     `db:"body"`
	MediaID   db.Ref     `db:"media_id"`
	Locale    string     `db:"locale"`
	Status    string     `db:"status"`
	PostID    db.Ref     `db:"post_id"`
	CreatedAt time.Time  `db:"created_at"`
	DecidedAt *time.Time `db:"decided_at"`
	DecidedBy db.Ref     `db:"decided_by"`
}

const columns = `id, name, email, title, body, media_id, locale, status, post_id, created_at, decided_at, decided_by`

func (r row) submission() Submission {
	submission := Submission{
		ID:        r.ID,
		Name:      r.Name,
		Email:     r.Email,
		Title:     r.Title,
		Body:      r.Body,
		MediaID:   r.MediaID.String(),
		Locale:    r.Locale,
		Status:    r.Status,
		PostID:    r.PostID.String(),
		CreatedAt: r.CreatedAt,
		DecidedBy: r.DecidedBy.String(),
	}

	if r.DecidedAt != nil {
		submission.DecidedAt = *r.DecidedAt
	}

	return submission
}

// Settings are the instance's mail settings.
type Settings struct {
	// Notify receives every new proposal; empty sends nothing.
	Notify string `db:"notify"`
	// ConfirmMail sends submitters a confirmation of their proposal.
	ConfirmMail bool `db:"confirm_mail"`
	// DecisionMail tells submitters when their proposal is accepted or rejected.
	DecisionMail bool `db:"decision_mail"`
}

// drafter saves draft posts; *publishing.Service is one.
type drafter interface {
	Draft(ctx context.Context, input publishing.DraftInput) (string, error)
}

// Service is one Submissions instance, for the website's own code.
type Service struct {
	key    string
	target string
	db     *db.DB
	media  *media.Service
	// drafts returns the target Publishing instance, where accepted
	// proposals become drafts. It is resolved when needed, since other
	// instances are only reachable once the app runs.
	drafts   func() (drafter, error)
	mail     mail.Sender
	pages    *pages.Service
	frontend *frontend.Frontend
	// adminLanguage is the language of mail to editors.
	adminLanguage string
	// baseURL makes paths absolute for links in e-mail; see webthings.Env.AbsoluteURL.
	baseURL func(r *http.Request, path string) string
	// adminURL builds admin addresses by route name (webthings.Env.AdminURL).
	adminURL func(name string, args ...any) string
	// actions receive the proposals (webthings.Env.Actions).
	actions *router.Router
}

// Of returns the Submissions instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// submitURL is the address proposal forms post to: the action named submit.
func (s *Service) submitURL() string {
	return s.actions.RoutePath("submit")
}

// absolute makes a site path absolute for e-mail; addresses that already are
// absolute, such as media on a CDN, stay as they are.
func (s *Service) absolute(r *http.Request, path string) string {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || s.baseURL == nil {
		return path
	}

	return s.baseURL(r, path)
}

// Create stores a proposal as a new submission. It returns ErrInvalid when a
// field is missing or too long; the image must already be in the media
// library.
func (s *Service) Create(ctx context.Context, proposal Proposal) (Submission, error) {
	proposal, err := clean(proposal)
	if err != nil {
		return Submission{}, err
	}

	id := db.NewID()
	// Visitors have no actor; a proposal stored by the site's own code may.
	now, actor := db.Stamp(ctx)

	err = s.db.Tx(ctx, func(ctx context.Context) error {
		_, err := s.db.Exec(ctx, `
			INSERT INTO wt_submission (id, instance, name, email, title, body, media_id, locale, status,
				created_at, created_by, updated_at, updated_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id,
			s.key,
			proposal.Name,
			proposal.Email,
			proposal.Title,
			proposal.Body,
			db.Ref(proposal.MediaID),
			proposal.Locale,
			StatusNew,
			now,
			actor,
			now,
			actor,
		)
		if err != nil {
			return err
		}

		// The image cannot be deleted from the library while it is proposed.
		return media.Track(ctx, s.db, "wt_submission", id, []string{proposal.MediaID})
	})
	if err != nil {
		return Submission{}, err
	}

	return s.Get(ctx, id)
}

// clean trims a proposal and checks its fields.
func clean(proposal Proposal) (Proposal, error) {
	proposal.Name = strings.TrimSpace(proposal.Name)
	proposal.Email = strings.TrimSpace(proposal.Email)
	proposal.Title = strings.TrimSpace(proposal.Title)
	proposal.Body = strings.TrimSpace(proposal.Body)

	input := ProposalInput{
		Name:  proposal.Name,
		Email: proposal.Email,
		Title: proposal.Title,
		Text:  proposal.Body,
	}

	if len(proposalChecks.ValidateForm(input)) > 0 || !mail.Valid(proposal.Email) {
		return proposal, ErrInvalid
	}

	locale, err := locales.Normalize(proposal.Locale)
	if err != nil {
		return proposal, ErrInvalid
	}

	proposal.Locale = locale

	return proposal, nil
}

// List returns submissions with status (all when empty) newest first,
// skipping offset, and whether older ones exist.
func (s *Service) List(ctx context.Context, status string, offset, limit int) ([]Submission, bool, error) {
	var rows []row

	where, args := s.filter(status)

	err := s.db.Select(ctx, &rows, `
		SELECT `+columns+`
		FROM wt_submission
		WHERE `+where+`
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?`,
		append(args, limit+1, offset)...,
	)
	if err != nil {
		return nil, false, err
	}

	more := len(rows) > limit
	rows = rows[:min(limit, len(rows))]
	list := make([]Submission, 0, len(rows))

	for _, stored := range rows {
		list = append(list, stored.submission())
	}

	return list, more, nil
}

// Count returns the number of submissions with status; all when empty.
func (s *Service) Count(ctx context.Context, status string) (int, error) {
	var count int

	where, args := s.filter(status)

	err := s.db.Get(ctx, &count, `SELECT COUNT(*) FROM wt_submission WHERE `+where, args...)

	return count, err
}

// filter is the WHERE clause for this instance's submissions with status, or
// all of them when status is empty; submissions in the trash are left out.
func (s *Service) filter(status string) (string, []any) {
	if status == "" {
		return "instance=? AND deleted_at IS NULL", []any{s.key}
	}

	return "instance=? AND status=? AND deleted_at IS NULL", []any{s.key, status}
}

// Get returns one submission that is not in the trash, or db.ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (Submission, error) {
	var stored row

	err := s.db.Get(ctx, &stored, `
		SELECT `+columns+`
		FROM wt_submission
		WHERE instance=? AND id=? AND deleted_at IS NULL`,
		s.key, id,
	)
	if err != nil {
		return Submission{}, err
	}

	return stored.submission(), nil
}

// Accept turns a submission into a draft post in the target Publishing
// instance, in the submission's language, and returns it with the post's ID.
// user is the deciding editor's ID. An accepted submission returns
// ErrDecided, a target that is no Publishing instance ErrNoTarget.
func (s *Service) Accept(ctx context.Context, id, user string) (Submission, error) {
	posts, err := s.drafts()
	if err != nil {
		return Submission{}, err
	}

	err = s.db.Tx(ctx, func(ctx context.Context) error {
		submission, err := s.Get(ctx, id)
		if err != nil {
			return err
		}

		if submission.Status == StatusAccepted {
			return ErrDecided
		}

		// The post is dated today; the revision note names the submitter.
		post, err := posts.Draft(ctx, publishing.DraftInput{
			Locale:  submission.Locale,
			Title:   submission.Title,
			Body:    submission.Body,
			ImageID: submission.MediaID,
			Author:  user,
			Note:    submission.Name + " <" + submission.Email + ">",
		})
		if err != nil {
			return err
		}

		// The condition keeps two editors from accepting it twice at once;
		// the second one's post is rolled back.
		now, actor := db.Stamp(ctx)
		result, err := s.db.Exec(ctx, `
			UPDATE wt_submission
			SET status=?, post_id=?, decided_at=?, decided_by=?, updated_at=?, updated_by=?
			WHERE instance=? AND id=? AND status<>? AND deleted_at IS NULL`,
			StatusAccepted, post, now, db.Ref(user), now, actor, s.key, id, StatusAccepted,
		)

		return decided(result, err)
	})
	if err != nil {
		return Submission{}, err
	}

	return s.Get(ctx, id)
}

// Reject marks a new submission as rejected; user is the deciding editor's
// ID. A submission that is no longer new returns ErrDecided.
func (s *Service) Reject(ctx context.Context, id, user string) (Submission, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return Submission{}, err
	}

	now, actor := db.Stamp(ctx)

	result, err := s.db.Exec(ctx, `
		UPDATE wt_submission
		SET status=?, decided_at=?, decided_by=?, updated_at=?, updated_by=?
		WHERE instance=? AND id=? AND status=? AND deleted_at IS NULL`,
		StatusRejected, now, db.Ref(user), now, actor, s.key, id, StatusNew,
	)
	if err := decided(result, err); err != nil {
		return Submission{}, err
	}

	return s.Get(ctx, id)
}

// decided turns an update that changed no row into ErrDecided.
func decided(result sql.Result, err error) error {
	err = db.Affected(result, err)
	if errors.Is(err, db.ErrNotFound) {
		return ErrDecided
	}

	return err
}

// rows is the instance's part of wt_submission, for moving submissions to
// the trash and back.
func (s *Service) rows() db.SoftTable {
	return db.SoftTable{DB: s.db, Table: "wt_submission", Scope: "instance=?", Args: []any{s.key}}
}

// Delete moves a submission to the trash: it leaves the admin's lists and
// counts, and Undelete brings it back. Its image stays in the media library
// meanwhile, and the post of an accepted submission is not touched.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.rows().Delete(ctx, id)
}

// Undelete brings a submission back from the trash; db.ErrNotFound means
// it is not in the trash.
func (s *Service) Undelete(ctx context.Context, id string) error {
	return s.rows().Undelete(ctx, id)
}

// Purge removes a submission in the trash for good; db.ErrNotFound means it
// is not in the trash. The image of a submission that was not accepted goes
// to the media library's trash too, unless other content uses it; the post
// of an accepted one keeps its image and is not touched.
func (s *Service) Purge(ctx context.Context, id string) error {
	var submission row

	err := s.rows().Purge(ctx, id, func(ctx context.Context) error {
		if err := s.rows().GetTrashed(ctx, &submission, columns, id); err != nil {
			return err
		}

		return media.Untrack(ctx, s.db, "wt_submission", id)
	})
	if err != nil {
		return err
	}

	return s.dropImage(ctx, submission.submission())
}

// dropImage moves the image of a purged submission to the media library's
// trash, unless the submission was accepted or other content uses it.
func (s *Service) dropImage(ctx context.Context, submission Submission) error {
	if submission.MediaID == "" || submission.Status == StatusAccepted || s.media == nil {
		return nil
	}

	err := s.media.Delete(ctx, submission.MediaID)
	if errors.Is(err, media.ErrInUse) || errors.Is(err, media.ErrNotFound) {
		return nil
	}

	return err
}

// trashed is a submission in the trash.
type trashed struct {
	ID        string     `db:"id"`
	Name      string     `db:"name"`
	Email     string     `db:"email"`
	Title     string     `db:"title"`
	Body      string     `db:"body"`
	DeletedAt *time.Time `db:"deleted_at"`
	DeletedBy db.Ref     `db:"deleted_by"`
}

// trashed returns submissions in the trash, most recently deleted first: at
// most limit of them from offset (all for a limit of 0), and how many the
// trash holds in all.
func (s *Service) trashed(ctx context.Context, limit, offset int) ([]trashed, int, error) {
	var list []trashed

	total, err := s.rows().Trashed(ctx, &list, `id, name, email, title, body, deleted_at, deleted_by`, limit, offset)

	return list, total, err
}

// Settings returns the instance's mail settings; empty when never saved.
func (s *Service) Settings(ctx context.Context) (Settings, error) {
	var settings Settings

	err := s.db.Get(ctx, &settings, `
		SELECT notify, confirm_mail, decision_mail
		FROM wt_submission_setting
		WHERE instance=?`,
		s.key,
	)
	if errors.Is(err, db.ErrNotFound) {
		return Settings{}, nil
	}

	return settings, err
}

// SaveSettings stores the notification address and which mails submitters
// get. An invalid address returns ErrInvalid.
func (s *Service) SaveSettings(ctx context.Context, settings Settings) error {
	settings.Notify = strings.TrimSpace(settings.Notify)
	if settings.Notify != "" && !mail.Valid(settings.Notify) {
		return ErrInvalid
	}

	return s.db.Tx(ctx, func(ctx context.Context) error {
		if err := s.ensureSettings(ctx); err != nil {
			return err
		}

		_, err := s.db.Exec(ctx, `
			UPDATE wt_submission_setting
			SET notify=?, confirm_mail=?, decision_mail=?, updated_at=?, updated_by=?
			WHERE instance=?`,
			settings.Notify, settings.ConfirmMail, settings.DecisionMail, time.Now().UTC(), db.Actor(ctx), s.key,
		)

		return err
	})
}

// ensureSettings creates the instance's settings row when it is missing.
func (s *Service) ensureSettings(ctx context.Context) error {
	var count int

	err := s.db.Get(ctx, &count, `SELECT COUNT(*) FROM wt_submission_setting WHERE instance=?`, s.key)
	if err != nil || count > 0 {
		return err
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO wt_submission_setting (instance, notify, confirm_mail, decision_mail, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?)`,
		s.key, "", false, false, time.Now().UTC(), db.Actor(ctx),
	)

	return err
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
