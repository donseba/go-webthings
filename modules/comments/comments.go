// Package comments lets visitors leave messages on the website: comments
// below news posts or pages, and guestbooks. Messages belong to a subject, a
// string the template chooses, such as a post ID, a page path or a fixed
// word. New messages wait for approval in the admin unless moderation is
// switched off; staff can reply from the admin. Add an instance per kind of
// conversation:
//
//	app.Use(
//		comments.Module("remarks", "Comments"),
//		comments.Module("guestbook", "Guestbook"),
//	)
//
// # Comments in any template
//
// The template functions work in every template, for example below a post of
// a Publishing instance:
//
//	{{range comments "remarks" .ID}}
//	  <article>
//	    <strong>{{.Name}}</strong> {{date .CreatedAt}}
//	    {{.HTML}}
//	    {{range .Replies}}
//	      <blockquote>{{if .Staff}}Reply from the site:{{else}}{{.Name}}:{{end}} {{.HTML}}</blockquote>
//	    {{end}}
//	  </article>
//	{{end}}
//
//	<div id="remarks">
//	  {{with commentResult "remarks"}}<p class="notice">{{.}}</p>{{end}}
//
//	  {{if not (commentsClosed "remarks")}}
//	    {{with commentInput "remarks" .ID}}
//	      {{form_open .}}
//	        {{form_field . "subject" nil}}
//	        {{form_field . "name" nil}}
//	        {{form_field . "email" nil}}
//	        {{form_field . "body" nil}}
//	        <button>Send</button>
//	      {{form_close}}
//	    {{end}}
//	  {{end}}
//	</div>
//
// Functions, all with the instance key first:
//
//   - comments key subject: the approved comments on subject, oldest first,
//     each with its approved Replies (see Comment).
//   - commentCount key subject: the number of approved comments and replies.
//   - commentURL key: the address the form posts to.
//   - commentInput key subject: the form's model (PostInput) for go-form's
//     functions, as above.
//   - commentResult key: the outcome of the visitor's last post (see below),
//     or "".
//   - commentsClosed key: whether the instance accepts no new comments.
//   - commentThread key subject: the thread component, rendered (below).
//   - commentForm key subject: the form component, rendered (below).
//
// # Components
//
// Instead of writing the markup above, a template can include the module's
// components (see webthings.Module.Components), which come with plain,
// accessible default markup:
//
//	{{commentThread "remarks" .ID}}   thread: the comments with their replies, then the form
//	{{commentForm "remarks" .ID}}     form: the form with the result of the last post
//
// thread gets a ThreadView, form a FormView. A theme replaces them with
// <key>/thread.gohtml and <key>/form.gohtml, or comments/thread.gohtml and
// comments/form.gohtml for every instance; App.WriteTemplates(dir, key)
// writes the defaults there to start from. The defaults use the classes
// wt-comments, wt-comments__title, wt-comments__list, wt-comments__empty,
// wt-comment, wt-comment--reply, wt-comment--staff, wt-comment__header,
// wt-comment__name, wt-comment__badge, wt-comment__date, wt-comment__body,
// wt-comment__replies and wt-comment-form, and the shared wt-form,
// wt-form__title, wt-form__field, wt-form__label, wt-form__input,
// wt-form__help, wt-notice, wt-meta and wt-button, styled by the optional
// {{asset "webthings.css"}}; their texts go through tl, so the site's
// translations translate them.
//
// # Form fields
//
// The form posts subject (required), name (required, at most 100
// characters), email (optional, never shown on the website), body (required,
// plain text, at most 5000 characters) and optionally parent, the ID of the
// comment to reply to: PostInput, whose fields form_field renders with
// these limits. form_open adds the spam trap and the page to return to:
// the element with the instance key as its ID, and the fields' IDs start
// with the key, such as remarks-name. Each visitor can post 5 times per 10
// minutes.
//
// After a post the visitor returns to the page with ?<key>=<result>, which
// commentResult reads: "sent" (shown right away), "pending" (waiting for
// approval), "invalid" (a field is missing or too long), "closed" (the
// instance accepted no comments at that moment) or "throttled" (too many
// posts).
//
// # Importing
//
// Of(app, key).Add stores comments from another system: set Approved, and
// CreatedAt to keep their date; a reply has the ID of the Comment Add
// returned for its parent as ParentID.
//
// Comment.CreatedAt is in UTC; {{date .CreatedAt}} and {{time .CreatedAt}}
// show it in the page's language and the site's timezone.
//
// # Guestbook page
//
// Placed on a page, an instance renders comments/list.gohtml (or
// <key>/list.gohtml) with a ListView: every approved message of the instance,
// newest first, 20 per page, and the details for a form posting with subject
// ListView.Subject (the instance key).
//
// # Settings
//
// The admin's settings screen per instance switches moderation (on by
// default) and closes the instance for new comments. With a notification
// address set, every new message sends an e-mail with a link to the admin:
// the mail template notify (see webthings.Module.Mail) with NotifyView as
// data, in the admin's language. A theme replaces it with
// mail/<key>/notify.gohtml or mail/comments/notify.gohtml.
//
// Visitors are told apart for a day by an HMAC of their address with a key
// of the instance (webthings.Env.Secret), so the address is never stored.
package comments

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"strings"
	"time"
	"unicode/utf8"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/mail"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// maxPath is the longest page address kept with a message, in characters.
const maxPath = 500

// entryChecks checks entries against the limits of PostInput's tags, the
// visitor's form, also for entries that come another way, such as a staff
// reply or an import.
var entryChecks = form.NewForm()

// ErrInvalid is returned for a message with a missing or too long field, or a
// parent that does not exist.
var ErrInvalid = errors.New("comments_invalid")

// ErrParentDeleted is returned when restoring a reply whose comment is in
// the trash: restore the comment instead.
var ErrParentDeleted = errors.New("comments_parent_deleted")

// Comment is an approved message as templates see it. The e-mail address is
// never part of it.
type Comment struct {
	ID      string
	Subject string
	Name    string
	// Body is the plain text as written.
	Body string
	// HTML is Body escaped, with paragraphs and line breaks.
	HTML template.HTML
	// Staff is set for replies written in the admin.
	Staff  bool
	Locale string
	// CreatedAt is in UTC; {{date .CreatedAt}} shows it in the site's timezone.
	CreatedAt time.Time
	// Replies are the approved replies, oldest first. Replies have none.
	Replies []Comment
}

// Entry is a new message, from a visitor or an import.
type Entry struct {
	Subject string
	// ParentID makes the entry a reply. A reply to a reply is attached to the
	// top-level comment, so threads stay one level deep.
	ParentID string
	Name     string
	Email    string
	Body     string
	Locale   string
	// Path is the page the message was written on, for the admin.
	Path     string
	Approved bool
	Staff    bool
	// IPHash identifies the sender for a day; see Service.visitor.
	IPHash string
	// CreatedAt is when the message was written, for imports; zero is now.
	CreatedAt time.Time
}

// Settings are the options of one instance.
type Settings struct {
	// Moderated hides new messages until they are approved.
	Moderated bool `db:"moderated"`
	// Closed refuses new messages.
	Closed bool `db:"closed"`
	// Notify receives an e-mail for every new message when set.
	Notify string `db:"notify"`
}

// Service is one Comments instance, for the website's own code.
type Service struct {
	key      string
	title    string
	db       *db.DB
	mail     mail.Sender
	frontend *frontend.Frontend
	// actions receive the posts (webthings.Env.Actions).
	actions *router.Router
	// changed tells the app that what visitors see changed
	// (webthings.Env.Changed).
	changed func()
}

// Of returns the Comments instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// postURL is the address comment forms post to: the action named post.
func (s *Service) postURL() string {
	return s.actions.RoutePath("post")
}

// record is a stored message, as the queries return it.
type record struct {
	ID         string    `db:"id"`
	Subject    string    `db:"subject"`
	ParentID   db.Ref    `db:"parent_id"`
	ParentName string    `db:"parent_name"`
	Name       string    `db:"name"`
	Email      string    `db:"email"`
	Body       string    `db:"body"`
	Locale     string    `db:"locale"`
	Path       string    `db:"page_path"`
	Approved   bool      `db:"approved"`
	Staff      bool      `db:"is_staff"`
	IPHash     string    `db:"ip_hash"`
	CreatedAt  time.Time `db:"created_at"`
}

func (r record) comment() Comment {
	return Comment{
		ID:        r.ID,
		Subject:   r.Subject,
		Name:      r.Name,
		Body:      r.Body,
		HTML:      format(r.Body),
		Staff:     r.Staff,
		Locale:    r.Locale,
		CreatedAt: r.CreatedAt,
	}
}

const columns = `c.id, c.subject, c.parent_id, COALESCE(p.name, '') AS parent_name, c.name, c.email, c.body,
	c.locale, c.page_path, c.approved, c.is_staff, c.ip_hash, c.created_at`

// List returns the approved comments on subject, oldest first, with their
// approved replies.
func (s *Service) List(ctx context.Context, subject string) ([]Comment, error) {
	var records []record

	err := s.db.Select(ctx, &records, `
		SELECT `+columns+`
		FROM wt_comment c
		LEFT JOIN wt_comment p ON p.id = c.parent_id
		WHERE c.instance=? AND c.subject=? AND c.approved=? AND c.deleted_at IS NULL
		ORDER BY c.created_at, c.id`,
		s.key, subject, true,
	)
	if err != nil {
		return nil, err
	}

	return thread(records), nil
}

// Count returns the number of approved comments on subject, including
// replies to approved comments: the ones List shows.
func (s *Service) Count(ctx context.Context, subject string) (int, error) {
	var count int

	err := s.db.Get(ctx, &count, `
		SELECT COUNT(*)
		FROM wt_comment c
		LEFT JOIN wt_comment p ON p.id = c.parent_id
		WHERE c.instance=? AND c.subject=? AND c.approved=? AND c.deleted_at IS NULL
			AND (c.parent_id IS NULL OR (p.approved=? AND p.deleted_at IS NULL))`,
		s.key, subject, true, true,
	)

	return count, err
}

// Latest returns approved top-level messages of every subject, newest
// first, with their approved replies, skipping offset; and whether older ones
// exist. It is the guestbook page. A limit of 0 or less returns all of them.
func (s *Service) Latest(ctx context.Context, offset, limit int) ([]Comment, bool, error) {
	var records []record

	query := `
		SELECT ` + columns + `
		FROM wt_comment c
		LEFT JOIN wt_comment p ON p.id = c.parent_id
		WHERE c.instance=? AND c.approved=? AND c.parent_id IS NULL AND c.deleted_at IS NULL
		ORDER BY c.created_at DESC, c.id DESC`
	args := []any{s.key, true}

	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit+1, max(offset, 0))
	}

	if err := s.db.Select(ctx, &records, query, args...); err != nil {
		return nil, false, err
	}

	if limit <= 0 {
		records = records[min(max(offset, 0), len(records)):]
	}

	more := limit > 0 && len(records) > limit
	if more {
		records = records[:limit]
	}

	if len(records) == 0 {
		return nil, more, nil
	}

	ids := make([]string, 0, len(records))
	for _, r := range records {
		ids = append(ids, r.ID)
	}

	query, args, err := s.db.In(`
		SELECT `+columns+`
		FROM wt_comment c
		LEFT JOIN wt_comment p ON p.id = c.parent_id
		WHERE c.instance=? AND c.approved=? AND c.parent_id IN (?) AND c.deleted_at IS NULL
		ORDER BY c.created_at, c.id`,
		s.key, true, ids,
	)
	if err != nil {
		return nil, false, err
	}

	var replies []record

	if err := s.db.Select(ctx, &replies, query, args...); err != nil {
		return nil, false, err
	}

	return thread(append(records, replies...)), more, nil
}

// thread nests replies below their comments. Top-level comments keep their
// order; replies whose comment is missing (not approved) are left out.
func thread(records []record) []Comment {
	var (
		list     []Comment
		position = map[string]int{}
	)

	for _, r := range records {
		if r.ParentID == "" {
			position[r.ID] = len(list)
			list = append(list, r.comment())
		}
	}

	for _, r := range records {
		if r.ParentID == "" {
			continue
		}

		if i, ok := position[r.ParentID.String()]; ok {
			list[i].Replies = append(list[i].Replies, r.comment())
		}
	}

	return list
}

// Add stores a message after checking its fields, for visitors and for
// imports: an import sets Approved, and CreatedAt for the original date, and
// passes the ID of the returned Comment as ParentID of its replies. It
// returns ErrInvalid for missing or too long fields and for a parent that is
// not a comment of this instance on the same subject.
func (s *Service) Add(ctx context.Context, entry Entry) (Comment, error) {
	entry, err := clean(entry)
	if err != nil {
		return Comment{}, err
	}

	var parent db.Ref

	if entry.ParentID != "" {
		root, err := s.get(ctx, entry.ParentID)
		if errors.Is(err, db.ErrNotFound) {
			return Comment{}, ErrInvalid
		}

		if err != nil {
			return Comment{}, err
		}

		if root.ParentID != "" {
			root, err = s.get(ctx, root.ParentID.String())
			if err != nil {
				return Comment{}, err
			}
		}

		if root.Subject != entry.Subject {
			return Comment{}, ErrInvalid
		}

		parent = db.Ref(root.ID)
	}

	stored := record{
		ID:        db.NewID(),
		Subject:   entry.Subject,
		Name:      entry.Name,
		Email:     entry.Email,
		Body:      entry.Body,
		Locale:    entry.Locale,
		Path:      entry.Path,
		Approved:  entry.Approved,
		Staff:     entry.Staff,
		IPHash:    entry.IPHash,
		CreatedAt: entry.CreatedAt.UTC(),
	}

	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now().UTC()
	}

	// Visitors have no actor; a staff reply records who wrote it. An import
	// keeps its original date as the creation and the last change.
	actor := db.Actor(ctx)

	_, err = s.db.Exec(ctx, `
		INSERT INTO wt_comment
			(id, instance, subject, parent_id, name, email, body, locale, page_path, approved, is_staff, ip_hash,
			created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		stored.ID, s.key, stored.Subject, parent, stored.Name, stored.Email, stored.Body, stored.Locale,
		stored.Path, stored.Approved, stored.Staff, stored.IPHash, stored.CreatedAt, actor, stored.CreatedAt, actor,
	)
	if err := db.AfterWrite(ctx, err, s.changed); err != nil {
		return Comment{}, err
	}

	return stored.comment(), nil
}

// Reply adds an approved staff reply to the comment with id, signed with
// name. Replying approves the comment too: the answer would be hidden
// otherwise.
func (s *Service) Reply(ctx context.Context, id, name, body string) (Comment, error) {
	parent, err := s.get(ctx, id)
	if err != nil {
		return Comment{}, err
	}

	if !parent.Approved {
		if err := s.Approve(ctx, parent.ID, true); err != nil {
			return Comment{}, err
		}
	}

	return s.Add(ctx, Entry{
		Subject:  parent.Subject,
		ParentID: parent.ID,
		Name:     name,
		Body:     body,
		Locale:   parent.Locale,
		Path:     parent.Path,
		Approved: true,
		Staff:    true,
	})
}

// Approve shows (true) or hides (false) the message with id on the website.
func (s *Service) Approve(ctx context.Context, id string, approved bool) error {
	at, by := db.Stamp(ctx)

	err := db.Affected(s.db.Exec(ctx, `
		UPDATE wt_comment
		SET approved=?, updated_at=?, updated_by=?
		WHERE instance=? AND id=? AND deleted_at IS NULL`,
		approved, at, by, s.key, id,
	))

	return db.AfterWrite(ctx, err, s.changed)
}

// rows is the instance's part of wt_comment, for the trash.
func (s *Service) rows() db.SoftTable {
	return db.SoftTable{DB: s.db, Table: "wt_comment", Scope: "instance=?", Args: []any{s.key}}
}

// Delete moves the message with id to the trash, with its replies: they
// leave the website and the admin's lists, and Undelete brings them back.
// The replies get the same deleted_at, which is how Undelete knows which of
// them went along.
func (s *Service) Delete(ctx context.Context, id string) error {
	err := s.db.Tx(ctx, func(ctx context.Context) error {
		if _, err := s.get(ctx, id); err != nil {
			return err
		}

		at, by := db.Stamp(ctx)

		_, err := s.db.Exec(ctx, `
			UPDATE wt_comment
			SET deleted_at=?, deleted_by=?
			WHERE instance=? AND (id=? OR parent_id=?) AND deleted_at IS NULL`,
			at, by, s.key, id, id,
		)

		return err
	})

	return db.AfterWrite(ctx, err, s.changed)
}

// Undelete brings the message with id back from the trash, with the
// replies that were deleted along with it. A reply whose comment is still
// in the trash cannot come back on its own (ErrParentDeleted);
// db.ErrNotFound means the message is not in the trash.
func (s *Service) Undelete(ctx context.Context, id string) error {
	err := s.db.Tx(ctx, func(ctx context.Context) error {
		var deleted struct {
			DeletedAt     time.Time  `db:"deleted_at"`
			ParentDeleted *time.Time `db:"parent_deleted_at"`
		}

		err := s.db.Get(ctx, &deleted, `
			SELECT c.deleted_at, p.deleted_at AS parent_deleted_at
			FROM wt_comment c
			LEFT JOIN wt_comment p ON p.id = c.parent_id
			WHERE c.instance=? AND c.id=? AND c.deleted_at IS NOT NULL`,
			s.key, id,
		)
		if err != nil {
			return err
		}

		if deleted.ParentDeleted != nil {
			return ErrParentDeleted
		}

		at, by := db.Stamp(ctx)

		return db.Affected(s.db.Exec(ctx, `
			UPDATE wt_comment
			SET deleted_at=NULL, deleted_by=NULL, updated_at=?, updated_by=?
			WHERE instance=? AND (id=? OR (parent_id=? AND deleted_at=?)) AND deleted_at IS NOT NULL`,
			at, by, s.key, id, id, deleted.DeletedAt,
		))
	})

	return db.AfterWrite(ctx, err, s.changed)
}

// Purge removes the message with id from the trash for good, with all its
// replies. db.ErrNotFound means the message is not in the trash.
func (s *Service) Purge(ctx context.Context, id string) error {
	return s.rows().Purge(ctx, id, func(ctx context.Context) error {
		// The foreign key cascades too; deleting the replies first does not
		// depend on it.
		_, err := s.db.Exec(ctx, `DELETE FROM wt_comment WHERE instance=? AND parent_id=?`, s.key, id)

		return err
	})
}

// trashed is a message in the trash.
type trashed struct {
	ID        string     `db:"id"`
	Subject   string     `db:"subject"`
	ParentID  db.Ref     `db:"parent_id"`
	Name      string     `db:"name"`
	Email     string     `db:"email"`
	Body      string     `db:"body"`
	Path      string     `db:"page_path"`
	DeletedAt *time.Time `db:"deleted_at"`
	DeletedBy db.Ref     `db:"deleted_by"`
}

// inTrash lists the messages of the trash: replies that went along with
// their comment are part of it and not listed.
const inTrash = `
	FROM wt_comment c
	LEFT JOIN wt_comment p ON p.id = c.parent_id
	WHERE c.instance=? AND c.deleted_at IS NOT NULL
		AND (p.id IS NULL OR p.deleted_at IS NULL OR p.deleted_at <> c.deleted_at)`

// trashed returns messages in the trash, most recently deleted first: at
// most limit of them from offset (all for a limit of 0), and how many the
// trash holds in all.
func (s *Service) trashed(ctx context.Context, limit, offset int) ([]trashed, int, error) {
	var total int

	if err := s.db.Get(ctx, &total, `SELECT COUNT(*)`+inTrash, s.key); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT c.id, c.subject, c.parent_id, c.name, c.email, c.body, c.page_path, c.deleted_at, c.deleted_by
		` + inTrash + `
		ORDER BY c.deleted_at DESC, c.id DESC`
	args := []any{s.key}

	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, max(offset, 0))
	}

	var list []trashed

	err := s.db.Select(ctx, &list, query, args...)

	return list, total, err
}

// Pending returns the number of messages waiting for approval.
func (s *Service) Pending(ctx context.Context) (int, error) {
	var count int

	err := s.db.Get(ctx, &count, `
		SELECT COUNT(*) FROM wt_comment WHERE instance=? AND approved=? AND deleted_at IS NULL`,
		s.key, false,
	)

	return count, err
}

// Settings returns the instance's options; the defaults when none are saved.
func (s *Service) Settings(ctx context.Context) (Settings, error) {
	var settings Settings

	err := s.db.Get(ctx, &settings, `SELECT moderated, closed, notify FROM wt_comment_setting WHERE instance=?`, s.key)
	if errors.Is(err, db.ErrNotFound) {
		return Settings{Moderated: true}, nil
	}

	return settings, err
}

// SaveSettings replaces the instance's options. Notify must be empty or one
// e-mail address.
func (s *Service) SaveSettings(ctx context.Context, settings Settings) error {
	settings.Notify = strings.TrimSpace(settings.Notify)
	if settings.Notify != "" && !mail.Valid(settings.Notify) {
		return ErrInvalid
	}

	err := s.db.Tx(ctx, func(ctx context.Context) error {
		if _, err := s.db.Exec(ctx, `DELETE FROM wt_comment_setting WHERE instance=?`, s.key); err != nil {
			return err
		}

		_, err := s.db.Exec(ctx, `
			INSERT INTO wt_comment_setting (instance, moderated, closed, notify, updated_at, updated_by)
			VALUES (?, ?, ?, ?, ?, ?)`,
			s.key, settings.Moderated, settings.Closed, settings.Notify, time.Now().UTC(), db.Actor(ctx),
		)

		return err
	})

	return db.AfterWrite(ctx, err, s.changed)
}

// get returns one message of this instance that is not in the trash, or
// db.ErrNotFound.
func (s *Service) get(ctx context.Context, id string) (record, error) {
	var r record

	err := s.db.Get(ctx, &r, `
		SELECT `+columns+`
		FROM wt_comment c
		LEFT JOIN wt_comment p ON p.id = c.parent_id
		WHERE c.instance=? AND c.id=? AND c.deleted_at IS NULL`,
		s.key, id,
	)

	return r, err
}

// review lists messages for the admin, newest first: pending, approved or
// all (filter ""), skipping offset; and whether older ones exist.
func (s *Service) review(ctx context.Context, filter string, offset, limit int) ([]record, bool, error) {
	where := " AND c.deleted_at IS NULL"
	args := []any{s.key}

	switch filter {
	case filterPending:
		where += " AND c.approved=?"

		args = append(args, false)
	case filterApproved:
		where += " AND c.approved=?"

		args = append(args, true)
	}

	var records []record

	err := s.db.Select(ctx, &records, `
		SELECT `+columns+`
		FROM wt_comment c
		LEFT JOIN wt_comment p ON p.id = c.parent_id
		WHERE c.instance=?`+where+`
		ORDER BY c.created_at DESC, c.id DESC
		LIMIT ? OFFSET ?`,
		append(args, limit+1, offset)...,
	)
	if err != nil {
		return nil, false, err
	}

	if len(records) > limit {
		return records[:limit], true, nil
	}

	return records, false, nil
}

// counts returns the number of pending and approved messages.
func (s *Service) counts(ctx context.Context) (int, int, error) {
	var rows []struct {
		Approved bool `db:"approved"`
		Count    int  `db:"total"`
	}

	err := s.db.Select(ctx, &rows, `
		SELECT approved, COUNT(*) AS total
		FROM wt_comment
		WHERE instance=? AND deleted_at IS NULL
		GROUP BY approved`,
		s.key,
	)
	if err != nil {
		return 0, 0, err
	}

	var pending, approved int

	for _, row := range rows {
		if row.Approved {
			approved = row.Count
		} else {
			pending = row.Count
		}
	}

	return pending, approved, nil
}

// clean trims an entry and checks its fields.
func clean(entry Entry) (Entry, error) {
	entry.Subject = strings.TrimSpace(entry.Subject)
	entry.ParentID = strings.TrimSpace(entry.ParentID)
	entry.Name = strings.TrimSpace(entry.Name)
	entry.Email = strings.TrimSpace(entry.Email)
	entry.Body = strings.TrimSpace(strings.ReplaceAll(entry.Body, "\r\n", "\n"))
	entry.Path = strings.TrimSpace(entry.Path)

	// The page is only a hint for the admin; a very long address is dropped
	// rather than refusing the message.
	if utf8.RuneCountInString(entry.Path) > maxPath {
		entry.Path = ""
	}

	input := PostInput{
		Subject: entry.Subject,
		Parent:  entry.ParentID,
		Name:    entry.Name,
		Email:   entry.Email,
		Body:    entry.Body,
	}

	switch {
	case len(entryChecks.ValidateForm(input)) > 0:
		return entry, ErrInvalid
	case entry.Email != "" && !mail.Valid(entry.Email):
		return entry, ErrInvalid
	case entry.Locale == "":
		return entry, ErrInvalid
	}

	return entry, nil
}

// format escapes plain text and keeps its paragraphs and line breaks.
func format(text string) template.HTML {
	var b strings.Builder

	for paragraph := range strings.SplitSeq(text, "\n\n") {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}

		lines := strings.Split(paragraph, "\n")
		for i := range lines {
			lines[i] = template.HTMLEscapeString(strings.TrimSpace(lines[i]))
		}

		b.WriteString("<p>" + strings.Join(lines, "<br>") + "</p>")
	}

	//nolint:gosec // Every line is escaped above.
	return template.HTML(b.String())
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
