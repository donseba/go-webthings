package newsletter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/mail"

	form "github.com/donseba/go-form/v2"
)

var (
	// ErrInvalidEmail is returned by Add for an address that is not valid.
	ErrInvalidEmail = errors.New("newsletter_invalid_email")
	// ErrUnsubscribed is returned by Add for an address that left the list.
	// Only the person can put it back, by signing up again.
	ErrUnsubscribed = errors.New("newsletter_unsubscribed")

	errHeader = errors.New("newsletter_import_header")
	errCSV    = errors.New("newsletter_import_invalid_csv")
)

// maxImportBytes limits the requests to the instance's admin section, so
// CSV imports can be larger than the admin's default limit.
const maxImportBytes = 4 << 20

// Contact is a person to put on the list with Add or Import.
type Contact struct {
	Email string
	Name  string
	// Locale is the language of their mailings; empty or not one of the
	// site's languages is the site's default language.
	Locale string
	// ConfirmedAt is when they agreed to receive the newsletter; zero is now.
	ConfirmedAt time.Time
}

// What add did with a contact.
const (
	outcomeAdded     = "added"
	outcomeConfirmed = "confirmed"
	outcomeExisting  = "existing"
)

// Add puts a person on the list as a confirmed subscriber, without a
// confirmation mail, for example when moving a list from another service.
// Only add people who agreed to receive the newsletter: they never confirmed
// with this site, so keeping the proof of their consent is up to you.
//
// A pending subscriber is confirmed; a confirmed one is returned unchanged.
// Someone who unsubscribed is not added again (ErrUnsubscribed), and an
// address that is not valid returns ErrInvalidEmail. The provider
// (WithProvider) is told about every new confirmation.
func (s *Service) Add(ctx context.Context, contact Contact) (Subscriber, error) {
	subscriber, _, err := s.add(ctx, contact)

	return subscriber, err
}

// ImportResult tells what Import did.
type ImportResult struct {
	// Added are new subscribers.
	Added int
	// Confirmed were waiting for their confirmation and are confirmed now.
	Confirmed int
	// Existing were confirmed already and are unchanged.
	Existing int
	// Unsubscribed left the list before and were not added again.
	Unsubscribed int
	// Invalid are the addresses that are not valid, as given.
	Invalid []string
}

// Import adds contacts with Add, under the same condition: only people who
// agreed to receive the newsletter. Invalid addresses and people who
// unsubscribed are skipped and counted; other errors stop the import, with
// the contacts before it added.
func (s *Service) Import(ctx context.Context, contacts []Contact) (ImportResult, error) {
	var result ImportResult

	for _, contact := range contacts {
		_, outcome, err := s.add(ctx, contact)

		switch {
		case errors.Is(err, ErrInvalidEmail):
			result.Invalid = append(result.Invalid, contact.Email)
		case errors.Is(err, ErrUnsubscribed):
			result.Unsubscribed++
		case err != nil:
			return result, err
		case outcome == outcomeAdded:
			result.Added++
		case outcome == outcomeConfirmed:
			result.Confirmed++
		default:
			result.Existing++
		}
	}

	return result, nil
}

// add confirms contact and returns the subscriber with what it did: one of
// the outcomes.
func (s *Service) add(ctx context.Context, contact Contact) (Subscriber, string, error) {
	email := normalizeEmail(contact.Email)
	if !mail.Valid(email) || len(email) > 254 {
		return Subscriber{}, "", ErrInvalidEmail
	}

	name := strings.Join(strings.Fields(strings.ToValidUTF8(contact.Name, "")), " ")
	if runes := []rune(name); len(runes) > 200 {
		name = string(runes[:200])
	}

	locale, err := s.siteLocale(ctx, contact.Locale)
	if err != nil {
		return Subscriber{}, "", err
	}

	confirmed := contact.ConfirmedAt.UTC()
	if confirmed.IsZero() {
		confirmed = time.Now().UTC()
	}

	var (
		subscriber Subscriber
		outcome    string
	)

	err = s.db.Tx(ctx, func(ctx context.Context) error {
		existing, err := s.one(ctx, `email=?`, email)

		switch {
		case errors.Is(err, db.ErrNotFound):
			// Someone who unsubscribed stays off the list while the record is
			// in the trash; only purging it forgets the unsubscribe.
			left, err := s.unsubscribedInTrash(ctx, email)
			if err != nil {
				return err
			}

			if left {
				return ErrUnsubscribed
			}

			subscriber = Subscriber{
				ID:          db.NewID(),
				Email:       email,
				Name:        name,
				Locale:      locale,
				Status:      Confirmed,
				Token:       newToken(),
				CreatedAt:   confirmed,
				ConfirmedAt: &confirmed,
			}
			outcome = outcomeAdded
			actor := db.Actor(ctx)

			// created_at is when the person agreed elsewhere; updated_at is
			// the import.
			_, err = s.db.Exec(ctx, `
				INSERT INTO wt_newsletter_subscriber
					(id, instance, email, name, locale, status, token, created_at, created_by, updated_at, updated_by, confirmed_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				subscriber.ID, s.key, email, name, locale, Confirmed, subscriber.Token,
				confirmed, actor, time.Now().UTC(), actor, confirmed,
			)

			return err
		case err != nil:
			return err
		case existing.Status == Confirmed:
			subscriber = existing
			outcome = outcomeExisting

			return nil
		case existing.Status == Unsubscribed:
			return ErrUnsubscribed
		}

		// A pending signup keeps its own language unless the contact has one.
		subscriber = existing
		subscriber.Status = Confirmed
		subscriber.ConfirmedAt = &confirmed
		outcome = outcomeConfirmed

		if name != "" {
			subscriber.Name = name
		}

		if contact.Locale != "" {
			subscriber.Locale = locale
		}

		_, err = s.db.Exec(ctx, `
			UPDATE wt_newsletter_subscriber
			SET name=?, locale=?, status=?, confirmed_at=?, updated_at=?, updated_by=?
			WHERE instance=? AND id=? AND deleted_at IS NULL`,
			subscriber.Name, subscriber.Locale, Confirmed, confirmed, time.Now().UTC(), db.Actor(ctx), s.key, existing.ID,
		)

		return err
	})
	if err != nil {
		return Subscriber{}, "", err
	}

	if outcome != outcomeExisting {
		s.sync(ctx, subscriber, Confirmed)
	}

	return subscriber, outcome, nil
}

// unsubscribedInTrash reports whether email unsubscribed and its record was
// moved to the trash.
func (s *Service) unsubscribedInTrash(ctx context.Context, email string) (bool, error) {
	var count int

	err := s.db.Get(ctx, &count, `
		SELECT COUNT(*)
		FROM wt_newsletter_subscriber
		WHERE instance=? AND email=? AND status=? AND deleted_at IS NOT NULL`,
		s.key, email, Unsubscribed,
	)

	return count > 0, err
}

// siteLocale is locale when it is one of the site's languages, else the
// site's default language.
func (s *Service) siteLocale(ctx context.Context, locale string) (string, error) {
	config, err := s.locales.Configuration(ctx)
	if err != nil {
		return "", err
	}

	if locale, err := config.Require(locale); err == nil {
		return locale, nil
	}

	return config.Default, nil
}

// readContacts reads contacts from CSV whose first row names the columns:
// email (required), name and locale. A status column, as in the export,
// leaves out everyone who is not confirmed. The separator is whichever of
// comma, semicolon or tab the header row uses most, because spreadsheets in
// many countries save CSV with semicolons. It returns the contacts and how
// many rows were left out for their status.
func readContacts(source io.Reader, locale string) ([]Contact, int, error) {
	rows, err := admin.ReadCSV(source)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", errCSV, err)
	}

	if len(rows) == 0 {
		return nil, 0, errHeader
	}

	columns := map[string]int{}

	for i, name := range rows[0].Values {
		columns[strings.ToLower(strings.TrimSpace(name))] = i
	}

	if _, ok := columns["email"]; !ok {
		return nil, 0, errHeader
	}

	value := func(row []string, column string) string {
		i, ok := columns[column]
		if !ok || i >= len(row) {
			return ""
		}

		// The export protects cells against spreadsheet formulas with a quote.
		return strings.TrimPrefix(strings.TrimSpace(row[i]), "'")
	}

	var (
		contacts []Contact
		skipped  int
	)

	for _, line := range rows[1:] {
		row := line.Values

		email := value(row, "email")
		if email == "" {
			continue
		}

		if status := value(row, "status"); status != "" && status != Confirmed {
			skipped++

			continue
		}

		contact := Contact{Email: email, Name: value(row, "name"), Locale: value(row, "locale")}
		if contact.Locale == "" {
			contact.Locale = locale
		}

		contacts = append(contacts, contact)
	}

	return contacts, skipped, nil
}

// ImportView is the data of the subscriber import screen.
type ImportView struct {
	Title   string
	BackURL string
	Form    form.RenderModel
	Errors  form.FieldErrors
	Result  *ImportResult
	// Skipped counts rows of the file left out because their status is not
	// confirmed.
	Skipped int
}

// importInput is the import form: a CSV file, or CSV pasted into the text
// field, the language of rows without one, and the editor's confirmation
// that everyone agreed. The file's size is limited like the request
// (maxImportBytes).
type importInput struct {
	File    *multipart.FileHeader     `form:"input,file" label:"CSV file"                                                                              name:"file"    accept:".csv,.tsv,text/csv,text/tab-separated-values,text/plain" maxSize:"4MB"`
	CSV     string                    `form:"textarea"   label:"Or paste CSV"                                                                          name:"csv"                                                                                    rows:"8"`
	Locale  form.SortedSelect[string] `form:"dropdown"   label:"Language of rows without a locale"                                                     name:"locale"`
	Consent bool                      `form:"checkbox"   label:"Everyone on this list agreed to receive the newsletter, and I can show their consent." name:"consent"                                                                                         required:"true" requiredMsg:"Confirm that everyone on the list agreed to receive the newsletter."`
}

// importForm shows the import screen.
func (s screens) importForm(w http.ResponseWriter, r *http.Request) {
	input, err := s.importInput(r)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.renderImport(w, r, input, nil, nil, 0, "", http.StatusOK)
}

// importInput is the empty import form: the default language first, then
// the site's other languages.
func (s screens) importInput(r *http.Request) (importInput, error) {
	config, err := s.service.locales.Configuration(r.Context())
	if err != nil {
		return importInput{}, err
	}

	options := []form.Option[string]{{Value: config.Default, Label: admin.LanguageLabel(config.Default)}}

	for _, locale := range config.Enabled {
		if locale != config.Default {
			options = append(options, form.Option[string]{Value: locale, Label: admin.LanguageLabel(locale)})
		}
	}

	return importInput{Locale: form.NewOrderedSelect(options...).Selected(config.Default)}, nil
}

// importSubscribers adds the subscribers of an uploaded file, or of CSV
// pasted into the text field, once the editor confirmed their consent.
func (s screens) importSubscribers(w http.ResponseWriter, r *http.Request) {
	input, err := s.importInput(r)
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
		s.renderImport(w, r, input, errs, nil, 0, "", http.StatusUnprocessableEntity)

		return
	}

	source := io.Reader(strings.NewReader(input.CSV))

	if input.File != nil {
		file, err := input.File.Open()
		if err != nil {
			s.admin.Fail(w, r, err)

			return
		}

		defer func() { _ = file.Close() }()

		source = file
	}

	contacts, skipped, err := readContacts(source, input.Locale.Get())
	if errors.Is(err, errHeader) {
		message := s.admin.T(r, "The first row must name the columns, including email.")
		s.renderImport(w, r, input, nil, nil, 0, message, http.StatusUnprocessableEntity)

		return
	}

	if errors.Is(err, errCSV) {
		s.renderImport(w, r, input, nil, nil, 0, s.admin.T(r, "The file is not valid CSV."), http.StatusUnprocessableEntity)

		return
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	result, err := s.service.Import(r.Context(), contacts)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.renderImport(w, r, input, nil, &result, skipped, "", http.StatusOK)
}

func (s screens) renderImport(
	w http.ResponseWriter,
	r *http.Request,
	input importInput,
	errs form.FieldErrors,
	result *ImportResult,
	skipped int,
	message string,
	status int,
) {
	// The message shows as a toast.
	r = admin.WithError(r, message)

	// A file is not sent back to the browser.
	input.File = nil

	s.admin.Render(w, r, admin.Page{
		Title:    "Import subscribers",
		Template: "templates/admin/import.gohtml",
		Files:    files,
		Status:   status,
		Data: ImportView{
			Title:   s.title,
			BackURL: s.routes.URL("index"),
			Form:    s.admin.Form(r, input, s.routes.URL("import"), "Import"),
			Errors:  errs,
			Result:  result,
			Skipped: skipped,
		},
	})
}
