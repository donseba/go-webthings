// Package newsletter keeps a subscriber list with double opt-in and sends
// mailings to it. Add an instance with:
//
//	app.Use(newsletter.Module("news", "Newsletter"))
//
// Visitors subscribe with a form that posts to the instance's actions (see
// below). They receive a mail with a confirmation link; only confirmed
// subscribers receive mailings, and every mailing carries a personal
// unsubscribe link, also as List-Unsubscribe header with one-click
// unsubscribe (RFC 8058). The admin lists subscribers by status, exports
// them as CSV, imports them, and composes, previews and sends mailings. The
// signup block's title and introduction and the mailing footer are edited
// per language under "Texts"; until they are published the module uses plain
// English defaults.
//
// # Sending
//
// Sending a mailing starts it in the background; its admin page shows how
// many messages went out. The progress is kept in the database: after a
// restart the mailing continues with the next subscriber, and the app's
// Close stops it after the message being sent. Every subscriber gets the
// mailing at most once; one whose message the mail server refuses is
// counted as not delivered and logged.
//
// # Importing subscribers
//
// Add and Import put people on the list as confirmed subscribers without a
// confirmation mail, for example when moving a list from another service;
// the admin does the same with a CSV file (columns email, name and locale)
// once the editor confirms the consent. Only add people who agreed to
// receive the newsletter. People who unsubscribed are never added again.
//
// # Signup form
//
// The form can be placed in any template. It posts to {{newsletterURL "news"}}
// (/_/news/subscribe); {{newsletter "news"}} gives its model, whose
// form_open adds the spam trap and the way back, to the element with the
// instance key as its id:
//
//	{{with newsletter "news"}}
//	  <div id="news">
//	    {{form_open .Form}}
//	      {{form_field .Form "locale" nil}}
//	      {{form_field .Form "email" nil}}
//	      {{form_field .Form "name" nil}}
//	      <button>Subscribe</button>
//	    {{form_close}}
//	  </div>
//	{{end}}
//
// Fields (SubscribeInput): email (required), name (optional) and locale
// (hidden, the page's language; without it the language of the page the
// form is on). After a post the visitor returns
// to the page with ?news=check-mail, ?news=invalid for an address that is
// not valid, or ?news=throttled after too many signups from the visitor.
// Whether the address was already on the list is never revealed.
//
// The links in the mails, /_/news/confirm?token=… and
// /_/news/unsubscribe?token=…, lead to the page the instance is mounted on
// (or the home page in the subscriber's language) with ?news=confirmed,
// ?news=unsubscribed or ?news=invalid. Mail programs unsubscribe with a POST
// to the unsubscribe link (List-Unsubscribe-Post), answered with 200.
//
// # Mail
//
// The confirmation mail is a mail template (see webthings.Module.Mail) named
// confirmation, with ConfirmationView as data, in the subscriber's language.
// The default is English text in tl, so the site's translations translate
// it; a theme replaces it with mail/<key>/confirmation.gohtml or
// mail/newsletter/confirmation.gohtml.
//
// # Templates
//
// When the instance is mounted on a page, the website renders
// newsletter/signup.gohtml with a SignupView as its data.
//
// # Template functions
//
//	{{newsletterURL "news"}}     the address the signup form posts to
//	{{newsletterResult "news"}}  "check-mail", "confirmed", "unsubscribed", "invalid", "throttled" or ""
//	{{newsletterForm "news"}}    the form component, rendered
//	{{with newsletter "news"}}   a SignupView, for a form anywhere:
//	  <h2>{{.Title}}</h2>{{.Intro}}
//	  {{if eq .Result "check-mail"}}<p>Check your inbox.</p>{{end}}
//	{{end}}
//
// # Component
//
// The form component (see webthings.Module.Components) is a signup box for
// any page with plain, accessible default markup: the title, the
// introduction, the result of the visitor's signup and the form.
//
//	{{newsletterForm "news"}}
//
// It gets a SignupView. A theme replaces it with <key>/form.gohtml, or
// newsletter/form.gohtml for every instance; App.WriteTemplates(dir, key)
// writes the default there to start from. The default uses the classes
// wt-newsletter, wt-newsletter__title, wt-newsletter__intro and
// wt-newsletter__note with the shared wt-form, wt-form__field,
// wt-notice and wt-button, styled by the optional {{asset "webthings.css"}};
// its texts go through tl, so the site's translations translate them.
//
// # Trash
//
// Deleting a subscriber or a mailing in the admin moves it to the admin's
// trash, from where it can be restored or deleted for good. Deleted
// subscribers receive no mailings, are not counted or exported, and their
// links stop working; their address can sign up again as a new subscriber,
// and a deleted subscriber whose address signed up again meanwhile cannot be
// restored. Deleting a mailing that is being sent stops it: subscribers who
// did not have their turn yet do not get it, also when it is restored.
//
// # Provider sync
//
// WithProvider passes confirmations and unsubscriptions on to a mailing
// service, for example a Mailjet contact list (package mailjet).
package newsletter

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-router"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// Subscriber statuses.
const (
	// Pending subscribers signed up but have not confirmed their address
	// yet; they get no mailings.
	Pending = "pending"
	// Confirmed subscribers get the mailings.
	Confirmed = "confirmed"
	// Unsubscribed subscribers asked to get no more mailings; they are kept
	// so an import cannot sign them up again.
	Unsubscribed = "unsubscribed"
)

// Results visitors return with, as ?<key>=<result>.
const (
	// ResultCheckMail asks the visitor to confirm with the link sent by
	// e-mail.
	ResultCheckMail = "check-mail"
	// ResultConfirmed is a subscription confirmed with that link.
	ResultConfirmed = "confirmed"
	// ResultUnsubscribed is an address taken off the list.
	ResultUnsubscribed = "unsubscribed"
	// ResultInvalid is an address that is not one, or a link that no longer
	// works.
	ResultInvalid = "invalid"
	// ResultThrottled is the answer to a visitor who signed up too often.
	ResultThrottled = frontend.ResultThrottled
)

var (
	errAlreadySent = errors.New("newsletter_mailing_already_sent")
	// errEmailTaken refuses to restore a subscriber whose address signed up
	// again after the subscriber was deleted.
	errEmailTaken = errors.New("newsletter_email_taken")
)

// Provider is a mailing service that mirrors the list, for example a Mailjet
// contact list. It is told about confirmed subscribers and about subscribers
// who leave. Errors are logged; the local list stays the reference.
type Provider interface {
	Subscribe(ctx context.Context, email, name, locale string) error
	Unsubscribe(ctx context.Context, email, name, locale string) error
}

// Option changes how an instance works.
type Option func(*options)

type options struct {
	provider Provider
}

// WithProvider syncs confirmations and unsubscriptions to provider.
func WithProvider(provider Provider) Option {
	return func(o *options) {
		o.provider = provider
	}
}

// Text is the instance's text in one language: the signup heading and
// introduction, and the footer of every mailing. Intro and Footer are
// Markdown.
type Text struct {
	Title  string `db:"title"`
	Intro  string `db:"intro"`
	Footer string `db:"footer"`
}

// Item is the stored texts of an instance, with their translations.
type Item = content.Item[struct{}, Text]

// defaultText is used until editors publish texts in the subscriber's language
// or the site's default language.
var defaultText = Text{
	Title:  "Newsletter",
	Footer: "You receive this e-mail because you subscribed to our newsletter. Unsubscribe:",
}

func textSpec() content.Spec[struct{}, Text] {
	return content.Spec[struct{}, Text]{
		Table: "wt_newsletter",
		Complete: func(text Text) bool {
			return text.Title != "" && text.Footer != ""
		},
		Check: func(_ struct{}, translations []content.Translation[Text]) error {
			for _, translation := range translations {
				text := translation.Text

				if content.TooLong(200, text.Title) || content.TooLong(5000, text.Intro) || content.TooLong(1000, text.Footer) {
					return content.ErrTooLong
				}
			}

			return nil
		},
		Media: func(_ struct{}, translations []content.Translation[Text]) []string {
			var ids []string

			for _, translation := range translations {
				ids = append(ids, markdown.MediaIDs(translation.Text.Intro)...)
				ids = append(ids, markdown.MediaIDs(translation.Text.Footer)...)
			}

			return ids
		},
	}
}

// Subscriber is one address on the list. Token is the secret in the
// subscriber's confirmation and unsubscribe links. DeletedAt and DeletedBy
// are set for subscribers in the trash (see TrashedSubscribers).
type Subscriber struct {
	ID          string     `db:"id"`
	Email       string     `db:"email"`
	Name        string     `db:"name"`
	Locale      string     `db:"locale"`
	Status      string     `db:"status"`
	Token       string     `db:"token"`
	CreatedAt   time.Time  `db:"created_at"`
	ConfirmedAt *time.Time `db:"confirmed_at"`
	DeletedAt   *time.Time `db:"deleted_at"`
	DeletedBy   db.Ref     `db:"deleted_by"`
}

// Counts are the number of subscribers per status.
type Counts struct {
	Pending      int
	Confirmed    int
	Unsubscribed int
}

// Total is the number of subscribers in any status.
func (c Counts) Total() int {
	return c.Pending + c.Confirmed + c.Unsubscribed
}

// Mailing is a message to the confirmed subscribers. Body is Markdown. Locale
// limits the recipients to one language; empty means everyone.
//
// A mailing is a draft until it is sent: then SentAt is when sending
// started, and the messages go out in the background, one per subscriber.
// Total is the number of confirmed subscribers at that moment; Recipients
// and Failed count the messages the mail sender accepted and refused so far,
// and FinishedAt is set when every subscriber had their turn. DeletedAt and
// DeletedBy are set for mailings in the trash (see TrashedMailings).
type Mailing struct {
	ID         string     `db:"id"`
	Locale     string     `db:"locale"`
	Subject    string     `db:"subject"`
	Body       string     `db:"body"`
	CreatedAt  time.Time  `db:"created_at"`
	SentAt     *time.Time `db:"sent_at"`
	Total      int        `db:"total"`
	Recipients int        `db:"recipients"`
	Failed     int        `db:"failed"`
	FinishedAt *time.Time `db:"finished_at"`
	DeletedAt  *time.Time `db:"deleted_at"`
	DeletedBy  db.Ref     `db:"deleted_by"`
}

// Sending reports whether the mailing is being sent: started, not finished.
func (m Mailing) Sending() bool {
	return m.SentAt != nil && m.FinishedAt == nil
}

// Service is one Newsletter instance, for the website's own code.
type Service struct {
	key      string
	db       *db.DB
	texts    *content.Collection[struct{}, Text]
	frontend *frontend.Frontend
	pages    *pages.Service
	locales  locales.Provider
	mail     mail.Sender
	// absolute makes a site path absolute for links in e-mail (Env.AbsoluteURL).
	absolute func(r *http.Request, path string) string
	provider Provider
	// sender delivers mailings in the background; see sending.go.
	sender *sender
	// actions receive the signup form and the links in the mails
	// (webthings.Env.Actions).
	actions *router.Router
}

// Of returns the Newsletter instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// subscribeURL is the address signup forms post to: the action named
// subscribe.
func (s *Service) subscribeURL() string {
	return s.actions.RoutePath("subscribe")
}

// tokenPath is the address of the action name, confirm or unsubscribe,
// for the subscriber with token, for the links in the mails.
func (s *Service) tokenPath(name, token string) string {
	// The routes are the instance's own, so the name is known.
	path, _ := s.actions.RoutePathArgs(name, url.Values{"token": {token}})

	return path
}

// Texts gives write access to this instance's texts, for imports and seeds.
// Admin edits go through the same store.
func (s *Service) Texts() content.Store[struct{}, Text] {
	return s.texts.In(s.key)
}

// Text returns the published texts in locale, falling back to the site's
// default language and then to built-in English.
func (s *Service) Text(ctx context.Context, locale string) Text {
	candidates := []string{locale}

	config, err := s.locales.Configuration(ctx)
	if err == nil && config.Default != locale {
		candidates = append(candidates, config.Default)
	}

	for _, candidate := range candidates {
		items, err := s.texts.ListPublished(ctx, s.key, candidate)
		if err != nil {
			slog.ErrorContext(ctx, "load newsletter texts", "module", s.key, "error", err)

			break
		}

		if len(items) > 0 {
			text, _ := items[0].Text(candidate)

			return text
		}
	}

	return defaultText
}

const subscriberColumns = `id, email, name, locale, status, token, created_at, confirmed_at`

// Subscribers returns subscribers with status (all when empty), newest
// first, skipping offset, and whether more exist. A limit of 0 or less
// returns all of them.
func (s *Service) Subscribers(ctx context.Context, status string, offset, limit int) ([]Subscriber, bool, error) {
	if limit <= 0 {
		list, err := s.list(ctx, status, 0, 0)

		return list[min(max(offset, 0), len(list)):], false, err
	}

	list, err := s.list(ctx, status, max(offset, 0), limit+1)
	if err != nil {
		return nil, false, err
	}

	if len(list) > limit {
		return list[:limit], true, nil
	}

	return list, false, nil
}

// list returns subscribers with status (all when empty), newest first; a
// limit below one returns all of them.
func (s *Service) list(ctx context.Context, status string, offset, limit int) ([]Subscriber, error) {
	query := `SELECT ` + subscriberColumns + ` FROM wt_newsletter_subscriber WHERE instance=? AND deleted_at IS NULL`
	args := []any{s.key}

	if status != "" {
		query += ` AND status=?`
		args = append(args, status)
	}

	query += ` ORDER BY created_at DESC, id DESC`

	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}

	var list []Subscriber

	err := s.db.Select(ctx, &list, query, args...)

	return list, err
}

// Recipients returns the confirmed subscribers in locale, or in every
// language when locale is empty, oldest first.
func (s *Service) Recipients(ctx context.Context, locale string) ([]Subscriber, error) {
	query := `SELECT ` + subscriberColumns + ` FROM wt_newsletter_subscriber WHERE instance=? AND status=? AND deleted_at IS NULL`
	args := []any{s.key, Confirmed}

	if locale != "" {
		query += ` AND locale=?`
		args = append(args, locale)
	}

	var list []Subscriber

	err := s.db.Select(ctx, &list, query+` ORDER BY created_at, id`, args...)

	return list, err
}

// Counts returns the number of subscribers per status.
func (s *Service) Counts(ctx context.Context) (Counts, error) {
	var rows []struct {
		Status string `db:"status"`
		Total  int    `db:"total"`
	}

	err := s.db.Select(ctx, &rows, `
		SELECT status, COUNT(*) AS total
		FROM wt_newsletter_subscriber
		WHERE instance=? AND deleted_at IS NULL
		GROUP BY status`,
		s.key,
	)
	if err != nil {
		return Counts{}, err
	}

	var counts Counts

	for _, row := range rows {
		switch row.Status {
		case Pending:
			counts.Pending = row.Total
		case Confirmed:
			counts.Confirmed = row.Total
		case Unsubscribed:
			counts.Unsubscribed = row.Total
		}
	}

	return counts, nil
}

// Subscriber returns one subscriber that is not in the trash, or
// db.ErrNotFound.
func (s *Service) Subscriber(ctx context.Context, id string) (Subscriber, error) {
	return s.one(ctx, `id=?`, id)
}

// one returns the subscriber matching where, leaving out the trash.
func (s *Service) one(ctx context.Context, where string, arg any) (Subscriber, error) {
	var subscriber Subscriber

	err := s.db.Get(ctx, &subscriber, `
		SELECT `+subscriberColumns+`
		FROM wt_newsletter_subscriber
		WHERE instance=? AND deleted_at IS NULL AND `+where,
		s.key, arg,
	)

	return subscriber, err
}

// register adds a signup as pending, or renews a pending or unsubscribed one,
// and returns the subscriber with send set when a confirmation mail is due.
// Confirmed subscribers are left alone.
func (s *Service) register(ctx context.Context, email, name, locale string) (Subscriber, bool, error) {
	var (
		subscriber Subscriber
		send       bool
	)

	err := s.db.Tx(ctx, func(ctx context.Context) error {
		existing, err := s.one(ctx, `email=?`, email)

		switch {
		case errors.Is(err, db.ErrNotFound):
			subscriber = Subscriber{
				ID:        db.NewID(),
				Email:     email,
				Name:      name,
				Locale:    locale,
				Status:    Pending,
				Token:     newToken(),
				CreatedAt: time.Now().UTC(),
			}
			send = true
			actor := db.Actor(ctx)

			_, err = s.db.Exec(ctx, `
				INSERT INTO wt_newsletter_subscriber
					(id, instance, email, name, locale, status, token, created_at, created_by, updated_at, updated_by)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				subscriber.ID, s.key, email, name, locale, Pending, subscriber.Token,
				subscriber.CreatedAt, actor, subscriber.CreatedAt, actor,
			)

			return err
		case err != nil:
			return err
		case existing.Status == Confirmed:
			subscriber = existing

			return nil
		}

		// A new signup after unsubscribing needs a fresh opt-in, so old links stop working.
		subscriber = existing
		subscriber.Name = name
		subscriber.Locale = locale
		subscriber.Status = Pending
		send = true

		if existing.Status == Unsubscribed {
			subscriber.Token = newToken()
		}

		at, by := db.Stamp(ctx)

		_, err = s.db.Exec(ctx, `
			UPDATE wt_newsletter_subscriber
			SET name=?, locale=?, status=?, token=?, confirmed_at=NULL, updated_at=?, updated_by=?
			WHERE instance=? AND id=? AND deleted_at IS NULL`,
			name, locale, Pending, subscriber.Token, at, by, s.key, existing.ID,
		)

		return err
	})

	// Two signups for one address at the same time: the other one sends the mail.
	if db.IsUnique(err) {
		return Subscriber{}, false, nil
	}

	return subscriber, send, err
}

// Confirm confirms the subscriber with token. It returns db.ErrNotFound for
// unknown tokens and for subscribers who left.
func (s *Service) Confirm(ctx context.Context, token string) (Subscriber, error) {
	subscriber, err := s.byToken(ctx, token)
	if err != nil {
		return subscriber, err
	}

	switch subscriber.Status {
	case Confirmed:
		return subscriber, nil
	case Unsubscribed:
		return Subscriber{}, db.ErrNotFound
	}

	now := time.Now().UTC()

	_, err = s.db.Exec(ctx, `
		UPDATE wt_newsletter_subscriber
		SET status=?, confirmed_at=?, updated_at=?, updated_by=?
		WHERE instance=? AND id=? AND status=? AND deleted_at IS NULL`,
		Confirmed, now, now, db.Actor(ctx), s.key, subscriber.ID, Pending,
	)
	if err != nil {
		return Subscriber{}, err
	}

	subscriber.Status = Confirmed
	subscriber.ConfirmedAt = &now

	s.sync(ctx, subscriber, Confirmed)

	return subscriber, nil
}

// Unsubscribe takes the subscriber with token off the list. The record stays,
// so the link keeps working and the admin sees who left.
func (s *Service) Unsubscribe(ctx context.Context, token string) (Subscriber, error) {
	subscriber, err := s.byToken(ctx, token)
	if err != nil || subscriber.Status == Unsubscribed {
		return subscriber, err
	}

	at, by := db.Stamp(ctx)

	_, err = s.db.Exec(ctx, `
		UPDATE wt_newsletter_subscriber
		SET status=?, updated_at=?, updated_by=?
		WHERE instance=? AND id=? AND deleted_at IS NULL`,
		Unsubscribed, at, by, s.key, subscriber.ID,
	)
	if err != nil {
		return Subscriber{}, err
	}

	if subscriber.Status == Confirmed {
		s.sync(ctx, subscriber, Unsubscribed)
	}

	subscriber.Status = Unsubscribed

	return subscriber, nil
}

// DeleteSubscriber moves a subscriber to the trash: off the list, the counts
// and the export, with links that no longer work. The provider hears of it
// as an unsubscription. It returns db.ErrNotFound for a subscriber that is
// not on the list.
func (s *Service) DeleteSubscriber(ctx context.Context, id string) error {
	subscriber, err := s.Subscriber(ctx, id)
	if err != nil {
		return err
	}

	if err := s.subscriberRows().Delete(ctx, id); err != nil {
		return err
	}

	if subscriber.Status == Confirmed {
		s.sync(ctx, subscriber, Unsubscribed)
	}

	return nil
}

// subscriberRows is the instance's part of wt_newsletter_subscriber, for
// moving subscribers to the trash and back.
func (s *Service) subscriberRows() db.SoftTable {
	return db.SoftTable{DB: s.db, Table: "wt_newsletter_subscriber", Scope: "instance=?", Args: []any{s.key}}
}

// TrashedSubscribers returns subscribers in the trash, most recently
// deleted first: at most limit of them from offset (all for a limit of 0),
// and how many the trash holds in all.
func (s *Service) TrashedSubscribers(ctx context.Context, limit, offset int) ([]Subscriber, int, error) {
	var list []Subscriber

	total, err := s.subscriberRows().Trashed(ctx, &list, subscriberColumns+`, deleted_at, deleted_by`, limit, offset)

	return list, total, err
}

// UndeleteSubscriber brings a subscriber back from the trash with the status
// it had; a confirmed one is passed on to the provider again. It returns
// db.ErrNotFound when the subscriber is not in the trash and errEmailTaken
// when the address signed up again meanwhile.
func (s *Service) UndeleteSubscriber(ctx context.Context, id string) error {
	var subscriber Subscriber

	err := s.subscriberRows().Undelete(ctx, id, func(ctx context.Context) error {
		return s.subscriberRows().GetTrashed(ctx, &subscriber, subscriberColumns, id)
	})
	if db.IsUnique(err) {
		return fmt.Errorf("%w: %w", errEmailTaken, err)
	}

	if err != nil {
		return err
	}

	if subscriber.Status == Confirmed {
		s.sync(ctx, subscriber, Confirmed)
	}

	return nil
}

// PurgeSubscriber removes a subscriber in the trash for good. It returns
// db.ErrNotFound when the subscriber is not in the trash.
func (s *Service) PurgeSubscriber(ctx context.Context, id string) error {
	return s.subscriberRows().Purge(ctx, id)
}

func (s *Service) byToken(ctx context.Context, token string) (Subscriber, error) {
	if len(token) != 64 {
		return Subscriber{}, db.ErrNotFound
	}

	return s.one(ctx, `token=?`, token)
}

// sync tells the provider about a confirmation or an unsubscription.
func (s *Service) sync(ctx context.Context, subscriber Subscriber, status string) {
	if s.provider == nil {
		return
	}

	var err error

	if status == Confirmed {
		err = s.provider.Subscribe(ctx, subscriber.Email, subscriber.Name, subscriber.Locale)
	} else {
		err = s.provider.Unsubscribe(ctx, subscriber.Email, subscriber.Name, subscriber.Locale)
	}

	if err != nil {
		slog.ErrorContext(ctx, "newsletter provider sync", "module", s.key, "status", status, "error", err)
	}
}

const (
	mailingTable   = "wt_newsletter_mailing"
	mailingColumns = `id, locale, subject, body, created_at, sent_at, total, recipients, failed, finished_at`
)

// Mailings returns drafts and sent mailings, newest first.
func (s *Service) Mailings(ctx context.Context) ([]Mailing, error) {
	var list []Mailing

	err := s.db.Select(ctx, &list, `
		SELECT `+mailingColumns+`
		FROM wt_newsletter_mailing
		WHERE instance=? AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC`,
		s.key,
	)

	return list, err
}

// Mailing returns one mailing that is not in the trash, or db.ErrNotFound.
func (s *Service) Mailing(ctx context.Context, id string) (Mailing, error) {
	var mailing Mailing

	err := s.db.Get(ctx, &mailing, `
		SELECT `+mailingColumns+`
		FROM wt_newsletter_mailing
		WHERE instance=? AND id=? AND deleted_at IS NULL`,
		s.key, id,
	)

	return mailing, err
}

// SaveMailing creates a draft (empty ID) or changes one. Sent mailings cannot
// be changed. Images in the body are tracked, so they stay available for the
// mail; a missing one returns media.ErrNotFound.
func (s *Service) SaveMailing(ctx context.Context, mailing Mailing) (Mailing, error) {
	now, actor := db.Stamp(ctx)

	err := s.db.Tx(ctx, func(ctx context.Context) error {
		if mailing.ID == "" {
			mailing.ID = db.NewID()

			_, err := s.db.Exec(ctx, `
				INSERT INTO wt_newsletter_mailing
					(id, instance, locale, subject, body, created_at, created_by, updated_at, updated_by)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				mailing.ID, s.key, mailing.Locale, mailing.Subject, mailing.Body, now, actor, now, actor,
			)
			if err != nil {
				return err
			}
		} else {
			result, err := s.db.Exec(ctx, `
				UPDATE wt_newsletter_mailing
				SET locale=?, subject=?, body=?, updated_at=?, updated_by=?
				WHERE instance=? AND id=? AND sent_at IS NULL AND deleted_at IS NULL`,
				mailing.Locale, mailing.Subject, mailing.Body, now, actor, s.key, mailing.ID,
			)

			err = db.Affected(result, err)
			if err != nil {
				return err
			}
		}

		return media.Track(ctx, s.db, mailingTable, mailing.ID, markdown.MediaIDs(mailing.Body))
	})
	if err != nil {
		return Mailing{}, err
	}

	return s.Mailing(ctx, mailing.ID)
}

// DeleteMailing moves a draft or the record of a sent mailing to the trash.
// Its images stay tracked, so they are kept for a restore. A mailing that is
// being sent stops: it is marked finished, so the sender leaves the
// subscribers who did not have their turn yet, also after a restore, which
// must never send mail by itself.
func (s *Service) DeleteMailing(ctx context.Context, id string) error {
	now, _ := db.Stamp(ctx)

	return s.mailingRows().DeleteWith(ctx, id,
		`finished_at=CASE WHEN sent_at IS NOT NULL AND finished_at IS NULL THEN ? ELSE finished_at END`,
		now,
	)
}

// mailingRows is the instance's part of wt_newsletter_mailing, for moving
// mailings to the trash and back.
func (s *Service) mailingRows() db.SoftTable {
	return db.SoftTable{DB: s.db, Table: mailingTable, Scope: "instance=?", Args: []any{s.key}}
}

// TrashedMailings returns mailings in the trash, most recently deleted
// first: at most limit of them from offset (all for a limit of 0), and how
// many the trash holds in all.
func (s *Service) TrashedMailings(ctx context.Context, limit, offset int) ([]Mailing, int, error) {
	var list []Mailing

	total, err := s.mailingRows().Trashed(ctx, &list, mailingColumns+`, deleted_at, deleted_by`, limit, offset)

	return list, total, err
}

// UndeleteMailing brings a mailing back from the trash. It returns
// db.ErrNotFound when the mailing is not in the trash.
func (s *Service) UndeleteMailing(ctx context.Context, id string) error {
	return s.mailingRows().Undelete(ctx, id)
}

// PurgeMailing removes a mailing in the trash for good, with its claim on
// the images it uses. It returns db.ErrNotFound when the mailing is not in
// the trash.
func (s *Service) PurgeMailing(ctx context.Context, id string) error {
	return s.mailingRows().Purge(ctx, id, func(ctx context.Context) error {
		return media.Untrack(ctx, s.db, mailingTable, id)
	})
}

// newToken returns 32 random bytes as hex, for confirmation and unsubscribe links.
func newToken() string {
	buffer := make([]byte, 32)
	_, _ = rand.Read(buffer)

	return hex.EncodeToString(buffer)
}

// normalizeEmail trims an address and lowercases it, so one person cannot be
// on the list twice with different capitals.
func normalizeEmail(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
