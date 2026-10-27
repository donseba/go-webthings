package newsletter

import (
	"cmp"
	"context"
	"errors"
	"html/template"
	"log/slog"
	"strings"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/markdown"
)

// Mailings go out in the background, so a large list does not keep the
// editor's request waiting. Sending a draft records who it goes to and
// wakes the instance's sender, a goroutine that mails the confirmed
// subscribers in ID order. Before each message it moves the mailing's
// progress to that subscriber in the database, so after a restart the
// sender continues with the next one, and two processes of one site never
// mail the same subscriber: a crash between the two steps loses that one
// message rather than sending it twice. Close stops the sender after the
// message it is sending.

const (
	// sendBatch is the number of recipients read from the database at a time.
	sendBatch = 100
	// sendTimeout limits one message, which may outlive Close.
	sendTimeout = time.Minute
	// retryAfter is the pause after a database error before trying again.
	retryAfter = time.Minute
)

// sender is the background goroutine of an instance.
type sender struct {
	wake chan struct{}
	stop context.CancelFunc
	done chan struct{}
}

// job is a mailing being sent, with what the sender needs to continue it.
type job struct {
	ID      string `db:"id"`
	Locale  string `db:"locale"`
	Subject string `db:"subject"`
	Body    string `db:"body"`
	// Origin is the site's address for the links in the mail.
	Origin string `db:"origin"`
	// Progress is the ID of the last subscriber handed to the mail sender.
	Progress string `db:"progress"`
}

// start runs the sender until close. It begins by resuming the mailings a
// restart interrupted.
func (s *Service) start() {
	ctx, cancel := context.WithCancel(context.Background())

	s.sender = &sender{
		wake: make(chan struct{}, 1),
		stop: cancel,
		done: make(chan struct{}),
	}

	go s.run(ctx)
}

// close stops the sender after the message it is sending, waiting at most
// until ctx ends. The mailing continues at the next start.
func (s *Service) close(ctx context.Context) error {
	if s.sender == nil {
		return nil
	}

	s.sender.stop()

	select {
	case <-s.sender.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// wake tells the sender a mailing is waiting.
func (s *Service) wake() {
	if s.sender == nil {
		return
	}

	select {
	case s.sender.wake <- struct{}{}:
	default:
	}
}

func (s *Service) run(ctx context.Context) {
	defer close(s.sender.done)

	for {
		var retry <-chan time.Time

		err := s.deliverAll(ctx)
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "newsletter mailing", "module", s.key, "error", err)

			retry = time.After(retryAfter)
		}

		select {
		case <-ctx.Done():
			return
		case <-s.sender.wake:
		case <-retry:
		}
	}
}

// startSending marks a draft as sent and hands it to the sender. origin is
// the site's address for the links in the mail. It returns errAlreadySent
// for a mailing that was sent before and db.ErrNotFound for an unknown one.
func (s *Service) startSending(ctx context.Context, id, origin string) (Mailing, error) {
	err := s.db.Tx(ctx, func(ctx context.Context) error {
		mailing, err := s.Mailing(ctx, id)
		if err != nil {
			return err
		}

		if mailing.SentAt != nil {
			return errAlreadySent
		}

		total, err := s.audience(ctx, mailing.Locale)
		if err != nil {
			return err
		}

		now := time.Now().UTC()

		result, err := s.db.Exec(ctx, `
			UPDATE wt_newsletter_mailing
			SET sent_at=?, origin=?, total=?, updated_at=?, updated_by=?
			WHERE instance=? AND id=? AND sent_at IS NULL AND deleted_at IS NULL`,
			now, strings.TrimSuffix(origin, "/"), total, now, db.Actor(ctx), s.key, id,
		)

		// A second editor sent it at the same moment.
		err = db.Affected(result, err)
		if errors.Is(err, db.ErrNotFound) {
			return errAlreadySent
		}

		return err
	})
	if err != nil {
		return Mailing{}, err
	}

	s.wake()

	return s.Mailing(ctx, id)
}

// audience is the number of confirmed subscribers in locale, or in every
// language when locale is empty.
func (s *Service) audience(ctx context.Context, locale string) (int, error) {
	query := `SELECT COUNT(*) FROM wt_newsletter_subscriber WHERE instance=? AND status=? AND deleted_at IS NULL`
	args := []any{s.key, Confirmed}

	if locale != "" {
		query += ` AND locale=?`
		args = append(args, locale)
	}

	var total int

	err := s.db.Get(ctx, &total, query, args...)

	return total, err
}

// deliverAll continues every mailing that is being sent, oldest first.
func (s *Service) deliverAll(ctx context.Context) error {
	var jobs []job

	err := s.db.Select(ctx, &jobs, `
		SELECT id, locale, subject, body, origin, progress
		FROM wt_newsletter_mailing
		WHERE instance=? AND sent_at IS NOT NULL AND finished_at IS NULL AND deleted_at IS NULL
		ORDER BY sent_at, id`,
		s.key,
	)
	if err != nil {
		return err
	}

	for _, job := range jobs {
		if err := s.deliver(ctx, job); err != nil {
			return err
		}
	}

	return nil
}

// deliver mails one message to each remaining recipient of job, then marks
// it finished. It returns early, without error, when ctx ends or when
// another process took over the mailing.
func (s *Service) deliver(ctx context.Context, job job) error {
	footers := map[string]string{}

	for {
		recipients, err := s.remaining(ctx, job)
		if err != nil {
			return err
		}

		if len(recipients) == 0 {
			return s.finish(ctx, job.ID)
		}

		for _, subscriber := range recipients {
			if ctx.Err() != nil {
				return nil
			}

			claimed, err := s.advance(ctx, job, subscriber.ID)
			if err != nil || !claimed {
				return err
			}

			job.Progress = subscriber.ID

			footer, ok := footers[subscriber.Locale]
			if !ok {
				footer = s.Text(ctx, subscriber.Locale).Footer
				footers[subscriber.Locale] = footer
			}

			if err := s.deliverOne(ctx, job, subscriber, footer); err != nil {
				return err
			}
		}
	}
}

// deliverOne mails job to subscriber and counts the result. The message and
// the count finish even when ctx ends meanwhile, so a closing app neither
// loses the message nor miscounts it.
func (s *Service) deliverOne(ctx context.Context, job job, subscriber Subscriber, footer string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sendTimeout)
	defer cancel()

	column := "recipients"

	if err := s.mail.Send(ctx, s.message(ctx, job, subscriber, footer)); err != nil {
		slog.ErrorContext(ctx, "newsletter mailing", "module", s.key, "mailing", job.ID, "error", err)

		column = "failed"
	}

	_, err := s.db.Exec(ctx, `
		UPDATE wt_newsletter_mailing
		SET `+column+`=`+column+`+1
		WHERE instance=? AND id=?`,
		s.key, job.ID,
	)

	return err
}

// remaining returns the next confirmed subscribers of job after its
// progress, in ID order.
func (s *Service) remaining(ctx context.Context, job job) ([]Subscriber, error) {
	query := `SELECT ` + subscriberColumns + ` FROM wt_newsletter_subscriber WHERE instance=? AND status=? AND deleted_at IS NULL`
	args := []any{s.key, Confirmed}

	if job.Locale != "" {
		query += ` AND locale=?`
		args = append(args, job.Locale)
	}

	if job.Progress != "" {
		query += ` AND id > ?`
		args = append(args, job.Progress)
	}

	var list []Subscriber

	err := s.db.Select(ctx, &list, query+` ORDER BY id LIMIT ?`, append(args, sendBatch)...)

	return list, err
}

// advance moves job's progress to the subscriber with id, if no other process
// moved it since job was read, and reports whether it did.
func (s *Service) advance(ctx context.Context, job job, id string) (bool, error) {
	result, err := s.db.Exec(ctx, `
		UPDATE wt_newsletter_mailing
		SET progress=?
		WHERE instance=? AND id=? AND progress=? AND finished_at IS NULL`,
		id, s.key, job.ID, job.Progress,
	)

	err = db.Affected(result, err)
	if errors.Is(err, db.ErrNotFound) {
		return false, nil
	}

	return err == nil, err
}

// finish marks job as sent to everyone.
func (s *Service) finish(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE wt_newsletter_mailing
		SET finished_at=?
		WHERE instance=? AND id=? AND finished_at IS NULL`,
		time.Now().UTC(), s.key, id,
	)

	return err
}

// message is a mailing for one subscriber: the Markdown body as text and as
// HTML, both ending with the footer and the subscriber's unsubscribe link,
// which the List-Unsubscribe headers offer mail programs as well (RFC 2369
// and RFC 8058). Images in the HTML are mail-sized copies, not the uploads.
// Every address of the site in both parts is absolute: a mailing has no
// address of its own, so its relative links lead from the home page of its
// language, as the links check reads them (see package links).
func (s *Service) message(ctx context.Context, job job, subscriber Subscriber, footer string) mail.Message {
	link := job.Origin + s.tokenPath("unsubscribe", subscriber.Token)

	// The library's alt texts and the relative links in the mailing's
	// language, or the subscriber's for a mailing to every language.
	locale := cmp.Or(job.Locale, subscriber.Locale)

	// Links to media give the file; images are copies the size of a mail,
	// in a format every mail program shows (see Frontend.MailImage), with
	// their size, so a mail program does not show them larger.
	options := s.frontend.MarkdownOptions(ctx, locale, home(job.Origin, locale), true)

	body := markdown.Render(job.Body, options)
	unsubscribe := `<a href="` + template.HTMLEscapeString(link) + `">` + template.HTMLEscapeString(link) + "</a>"

	// The text part is the Markdown with media as links, rather than
	// references only the library understands.
	text := markdown.PlainMedia(job.Body, options) + "\n\n-- \n" +
		markdown.PlainMedia(footer, options) + "\n" + link + "\n"

	html := "<!doctype html>\n<html><body>\n" + string(body) +
		"<hr>\n" + footerHTML(string(markdown.Render(footer, options)), unsubscribe) +
		"</body></html>\n"

	return mail.Message{
		To:      []string{subscriber.Email},
		Subject: job.Subject,
		Text:    text,
		HTML:    html,
		Headers: map[string]string{
			"List-Unsubscribe":      "<" + link + ">",
			"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
		},
	}
}

// home is the address of the home page in locale on the site at origin,
// such as "https://example.com/en/", or of the site without a locale.
func home(origin, locale string) string {
	if locale == "" {
		return origin + "/"
	}

	return origin + "/" + locale + "/"
}

// footerHTML is the rendered Markdown footer with the unsubscribe link: in
// its last paragraph, so a footer ending in "Unsubscribe:" reads on into the
// link, or in a paragraph of its own after a footer that ends otherwise,
// such as with a list.
func footerHTML(footer, unsubscribe string) string {
	trimmed := strings.TrimRight(footer, "\n")

	if before, ok := strings.CutSuffix(trimmed, "</p>"); ok {
		return before + " " + unsubscribe + "</p>\n"
	}

	return footer + "<p>" + unsubscribe + "</p>\n"
}
