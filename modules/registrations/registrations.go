// Package registrations lets visitors sign up for events, or for any other
// subject: places with an optional capacity, adults and children with prices,
// a deadline, a confirmation e-mail with a personal code, and a page where
// visitors look up and cancel their registration. Editors open registration
// per subject, follow sign-ups and payments, and export them as CSV.
//
// Link an instance to an events instance so editors pick events in the admin,
// and registration closes when an event starts:
//
//	app.Use(
//		events.Module("agenda", "Agenda"),
//		registrations.Module("signup", "Sign-ups", registrations.ForEvents("agenda")),
//	)
//
// Without ForEvents, editors add subjects by ID (any text up to 64 characters,
// such as a page or product ID) and name.
//
// # Registering
//
// The sign-up form can sit on any page, typically events/event.gohtml. It
// posts to the module's action (registerURL) with these fields
// (SignupInput):
//
//	subject   the subject ID, for example the event's .ID (required)
//	name      the visitor's name (required)
//	email     the visitor's e-mail address (required)
//	adults    number of adults, at least 1; 1 when the field is missing
//	children  number of children; 0 when missing, only when allowed
//	notes     optional remarks for the organiser
//
// Availability.Form is its model for go-form's functions, with the adults
// and children at most the places left; form_open adds the spam trap and
// the return address, the element with the instance key as its id. A
// minimal form:
//
//	{{with registration "signup" .ID}}
//	  <div id="signup">
//	    {{if .Open}}
//	      {{form_open .Form}}
//	        {{form_field .Form "subject" nil}}
//	        {{form_field .Form "name" nil}}
//	        {{form_field .Form "email" nil}}
//	        {{form_field .Form "adults" nil}}
//	        {{if .AllowChildren}}{{form_field .Form "children" nil}}{{end}}
//	        <button>Register</button>
//	      {{form_close}}
//	      {{if .Capacity}}<p>{{.Left}} places left</p>{{end}}
//	    {{else if .Full}}<p>This event is full.</p>
//	    {{else if .Configured}}<p>Registration is closed.</p>{{end}}
//	  </div>
//	{{end}}
//
// A successful registration sends a confirmation e-mail with the code, the
// total, the payment link and a link to the registration page. The visitor
// then lands on that page (/<page>/<code>?<key>=registered) when the module
// is placed on a page, and otherwise back on the form's page with
// ?<key>=registered&code=<code> (registrationCode reads it). Other results
// return to the form's page: ?<key>=full (not enough places left),
// ?<key>=closed (registration is not open), ?<key>=invalid (missing or wrong
// fields) or ?<key>=throttled (too many posts from the visitor).
// registrationResult reads the result.
//
// # Component
//
// Instead of writing the form, a template can include the form component
// (see webthings.Module.Components): the sign-up box of a subject with plain,
// accessible default markup: prices, places left, the deadline, the result
// of the visitor's last post (with the code) and the form, or a notice when
// the subject is full or closed. Subjects without registration settings
// render nothing.
//
//	{{registrationForm "signup" .ID}}
//
// It gets a FormView. A theme replaces it with <key>/form.gohtml, or
// registrations/form.gohtml for every instance; App.WriteTemplates(dir, key)
// writes the default there to start from. The default uses the classes
// wt-registration, wt-registration__title, wt-registration__facts and
// wt-registration__notes with the shared wt-form, wt-form__field,
// wt-form__row, wt-notice and wt-button, styled by the optional {{asset
// "webthings.css"}}; its texts go through tl, so the site's translations
// translate them.
//
// # Looking up and cancelling
//
// Place the module on a page to let visitors find their registration by code.
// Below the page it serves:
//
//	/          registrations/lookup.gohtml        LookupView: a form asking for the code (?code=)
//	/{code}    registrations/registration.gohtml  RegistrationView: one registration
//
// Every answer below the page, "not found" for a mistyped code included, is
// sent with "Cache-Control: private, no-store" and "X-Robots-Tag: noindex":
// the address holds the code and the page personal details, so no cache
// keeps them, no search engine indexes them, and neither the stats module
// nor the missed paths of the redirects module record them.
//
// The cancel form posts to RegistrationView.CancelAction with the fields
// code and email (the address used to register, as confirmation):
// CancelInput, whose model is RegistrationView.Form. It returns to the
// element with the instance key as its id, with ?<key>=cancelled, or
// ?<key>=invalid when the address does not match.
//
// # Template functions
//
//	{{with registration "signup" .ID}}…{{end}}   Availability of a subject in the page's language
//	{{registerURL "signup"}}                      the address sign-up forms post to
//	{{registrationResult "signup"}}               the result of the visitor's last post on this page
//	{{registrationCode "signup"}}                 the code after registering on a page without the module
//	{{registrationForm "signup" .ID}}             the form component, rendered
//
// Amounts are Cents in the instance's currency (Currency, EUR by default):
// {{money .Prices.Adult .Currency}} shows € 12,50 on a Dutch page.
//
// # Mail
//
// The confirmation and the cancellation are mail templates (see
// webthings.Module.Mail) named confirmation and cancellation, with MailView
// as data, in the visitor's language. The defaults are English text in tl,
// so the site's translations translate them; a theme replaces them with
// mail/<key>/<name>.gohtml or mail/registrations/<name>.gohtml. Editors
// write the notes for registrants and the payment link per language.
//
// # Trash
//
// Deleting a registration in the admin moves it to the admin's trash: it no
// longer takes places and leaves the lists, the counts, the CSV export and
// the lookup by code. Restoring it fails when its places are no longer free.
// Cancelling is something else: a cancelled registration stays in the list.
package registrations

import (
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/pages"
	"github.com/donseba/go-webthings/site"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

var (
	// ErrNotFound is returned when a subject or registration does not exist.
	ErrNotFound = db.ErrNotFound
	// ErrInvalid is returned for sign-ups with missing or wrong fields.
	ErrInvalid = errors.New("registrations_invalid")
	// ErrClosed is returned when registration is not open for the subject.
	ErrClosed = errors.New("registrations_closed")
	// ErrFull is returned when not enough places are left.
	ErrFull = errors.New("registrations_full")
	// ErrNoPlaces is returned when a registration in the trash cannot be
	// restored because its places were taken meanwhile.
	ErrNoPlaces = errors.New("registrations_no_places")

	errCodeTaken = errors.New("registrations_code_taken")
)

// maxPlaces is the most places one sign-up takes, adults and children
// together; SignupInput limits each of them.
const maxPlaces = 50

// signupChecks checks sign-ups against the limits of SignupInput's tags, the
// visitor's form, also for sign-ups that come another way.
var signupChecks = form.NewForm()

// Cents is an amount of money in hundredths, for example 1250 for 12.50.
// Templates show it with money, for example {{money .Total .Currency}}.
type Cents int64

// String formats the amount with two decimals, for example "12.50", as
// editors type it.
func (c Cents) String() string {
	sign := ""
	if c < 0 {
		sign = "-"
		c = -c
	}

	return fmt.Sprintf("%s%d.%02d", sign, c/100, c%100)
}

// ParseCents reads an amount such as "12", "12.5", "12.50" or "12,50". Empty
// is zero.
func ParseCents(value string) (Cents, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), ",", ".")
	if value == "" {
		return 0, nil
	}

	whole, fraction, _ := strings.Cut(value, ".")
	if whole == "" {
		whole = "0"
	}

	if len(fraction) > 2 || len(whole) > 9 {
		return 0, ErrInvalid
	}

	units, err := strconv.ParseUint(whole, 10, 32)
	if err != nil {
		return 0, ErrInvalid
	}

	fraction += strings.Repeat("0", 2-len(fraction))

	hundredths, err := strconv.ParseUint(fraction, 10, 8)
	if err != nil {
		return 0, ErrInvalid
	}

	return Cents(units*100 + hundredths), nil
}

// PaymentStatus tells whether a registration was paid.
type PaymentStatus string

const (
	// PaymentNone is the status of free registrations.
	PaymentNone PaymentStatus = "none"
	// PaymentPending is the status of new registrations with a total.
	PaymentPending PaymentStatus = "pending"
	// PaymentPaid is set by an editor once the payment came in.
	PaymentPaid PaymentStatus = "paid"
	// PaymentCancelled is set when an unpaid registration is cancelled.
	PaymentCancelled PaymentStatus = "cancelled"
)

// Valid reports whether status is one of the known statuses.
func (p PaymentStatus) Valid() bool {
	switch p {
	case PaymentNone, PaymentPending, PaymentPaid, PaymentCancelled:
		return true
	}

	return false
}

// Settings decide whether and how visitors can register for one subject.
type Settings struct {
	SubjectID string `db:"subject_id"`
	// Title names the subject in the admin and e-mail. Events use their own
	// title when it is empty.
	Title string `db:"title"`
	// Open switches registration on.
	Open bool `db:"is_open"`
	// Capacity is the number of places (adults and children); 0 is unlimited.
	Capacity int `db:"capacity"`
	// ClosesAt is the registration deadline. Without one, registration for an
	// event closes when it starts.
	ClosesAt      *time.Time `db:"closes_at"`
	AdultPrice    Cents      `db:"adult_price"`
	ChildPrice    Cents      `db:"child_price"`
	AllowChildren bool       `db:"allow_children"`
	// UpdatedAt and UpdatedBy are the last change by an editor.
	UpdatedAt time.Time `db:"updated_at"`
	UpdatedBy db.Ref    `db:"updated_by"`
	// Texts are what registrants read, per language. SaveSettings stores the
	// languages given and keeps the others.
	Texts []SubjectText
}

// SubjectText is what registrants of a subject read in one language.
type SubjectText struct {
	Locale string `db:"locale"`
	// Notes are shown with the sign-up form, on the registration page and in
	// the confirmation. They are Markdown.
	Notes string `db:"notes"`
	// PaymentLink is shown while payment is pending, for example a payment
	// request URL or bank details.
	PaymentLink string `db:"payment_link"`
}

// Text returns the texts in locale, if written.
func (s Settings) Text(locale string) (SubjectText, bool) {
	for _, text := range s.Texts {
		if text.Locale == locale {
			return text, true
		}
	}

	return SubjectText{Locale: locale}, false
}

// Registration is one sign-up.
type Registration struct {
	ID        string `db:"id"`
	SubjectID string `db:"subject_id"`
	// Code is the visitor's reference, for example "K7Q4-9M2X".
	Code       string        `db:"code"`
	Name       string        `db:"name"`
	Email      string        `db:"email"`
	Adults     int           `db:"adults"`
	Children   int           `db:"children"`
	Total      Cents         `db:"total"`
	Payment    PaymentStatus `db:"payment_status"`
	Notes      string        `db:"notes"`
	AdminNotes string        `db:"admin_notes"`
	// Locale is the language the visitor registered in.
	Locale      string     `db:"locale"`
	CreatedAt   time.Time  `db:"created_at"`
	CancelledAt *time.Time `db:"cancelled_at"`
	// CreatedBy is the editor who added the registration; empty for visitors.
	CreatedBy db.Ref `db:"created_by"`
	// UpdatedAt and UpdatedBy are the last change, by an editor or, when
	// cancelling, by the visitor (UpdatedBy empty).
	UpdatedAt time.Time `db:"updated_at"`
	UpdatedBy db.Ref    `db:"updated_by"`
	// DeletedAt is set while the registration is in the trash, with
	// DeletedBy.
	DeletedAt *time.Time `db:"deleted_at"`
	DeletedBy db.Ref     `db:"deleted_by"`
}

// Places is the number of places the registration takes.
func (r Registration) Places() int {
	return r.Adults + r.Children
}

// Cancelled reports whether the registration was cancelled.
func (r Registration) Cancelled() bool {
	return r.CancelledAt != nil
}

// Signup is what a visitor submits.
type Signup struct {
	SubjectID string
	Name      string
	Email     string
	Adults    int
	Children  int
	Notes     string
	Locale    string
}

// Prices are the prices per place.
type Prices struct {
	Adult Cents
	Child Cents
}

// Free reports whether registration costs nothing.
func (p Prices) Free() bool {
	return p.Adult == 0 && p.Child == 0
}

// Availability tells templates whether and how visitors can register for a
// subject.
type Availability struct {
	SubjectID string
	// Configured is true when editors set up registration for the subject,
	// open or not; without it a page shows no sign-up at all.
	Configured bool
	// Open is true while visitors can register: registration is switched on,
	// the deadline has not passed and places are left.
	Open bool
	// Closed is true when registration is switched off or its deadline
	// passed, and for subjects that are not Configured.
	Closed bool
	// Full is true when every place is taken.
	Full bool
	// Capacity is the number of places; 0 is unlimited.
	Capacity int
	// Taken is the number of places registered so far.
	Taken int
	// Left is the number of places left; 0 when Capacity is 0.
	Left int
	// ClosesAt is the deadline in the site's timezone; zero when there is none.
	ClosesAt      time.Time
	AllowChildren bool
	Prices        Prices
	// Currency is the ISO 4217 code of the prices, for {{money .Prices.Adult .Currency}}.
	Currency string
	// Notes are the editor's notes for registrants in the requested language,
	// or in the site's default language, rendered from Markdown.
	Notes template.HTML
	// Action is the address the sign-up form posts to.
	Action string
	// Form is the sign-up form's model (SignupInput) for go-form's
	// template functions, such as {{form_open .Form}} and {{form_field
	// .Form "name" nil}}, with the adults and children at most Left when
	// places are limited; it returns the visitor to the element with the
	// instance key as its id.
	Form form.RenderModel
}

// Summary counts the active registrations of one subject.
type Summary struct {
	SubjectID     string `db:"subject_id"`
	Registrations int    `db:"registrations"`
	Places        int    `db:"places"`
}

// Service is one Registrations instance, for the website's own code.
type Service struct {
	key      string
	db       *db.DB
	site     *site.Service
	pages    *pages.Service
	frontend *frontend.Frontend
	mail     mail.Sender
	// currency is the ISO 4217 code of the prices.
	currency string
	// events returns the linked events instance; nil when the instance is
	// not linked to one.
	events func() (eventSource, error)
	// actions receive the sign-up and cancel forms (webthings.Env.Actions).
	actions *router.Router
	// public are the lookup pages below the module's page (see handler).
	public *router.Router
	// changed tells the app that what visitors see changed
	// (webthings.Env.Changed), such as the places left.
	changed func()
}

// registerURL is the address sign-up forms post to: the action named
// register.
func (s *Service) registerURL() string {
	return s.actions.RoutePath("register")
}

// Currency is the ISO 4217 code of the instance's prices, for example "EUR".
func (s *Service) Currency() string {
	return s.currency
}

const settingsColumns = `subject_id, title, is_open, capacity, closes_at, adult_price, child_price,
	allow_children, updated_at, updated_by`

// Settings returns the settings of a subject with its texts, or ErrNotFound.
func (s *Service) Settings(ctx context.Context, subjectID string) (Settings, error) {
	var settings Settings

	err := s.db.Get(ctx, &settings, `
		SELECT `+settingsColumns+`
		FROM wt_registration_subject
		WHERE instance=? AND subject_id=?`,
		s.key, subjectID,
	)
	if err != nil {
		return settings, err
	}

	err = s.db.Select(ctx, &settings.Texts, `
		SELECT locale, notes, payment_link
		FROM wt_registration_subject_text
		WHERE instance=? AND subject_id=?
		ORDER BY locale`,
		s.key, subjectID,
	)

	return settings, err
}

// AllSettings returns the settings of every subject, by title, without
// their texts.
func (s *Service) AllSettings(ctx context.Context) ([]Settings, error) {
	var list []Settings

	err := s.db.Select(ctx, &list, `
		SELECT `+settingsColumns+`
		FROM wt_registration_subject
		WHERE instance=?
		ORDER BY title, subject_id`,
		s.key,
	)

	return list, err
}

// text returns what registrants read in locale, or in the site's default
// language when locale has nothing written.
func (s *Service) text(ctx context.Context, settings Settings, locale string) SubjectText {
	text, ok := settings.Text(locale)
	if ok || s.site == nil {
		return text
	}

	config, err := s.site.Configuration(ctx)
	if err != nil {
		return text
	}

	fallback, _ := settings.Text(config.Default)

	return fallback
}

// defaultLocale is the site's default language, in which the admin names
// subjects; empty when it cannot be read.
func (s *Service) defaultLocale(ctx context.Context) string {
	if s.site == nil {
		return ""
	}

	config, err := s.site.Configuration(ctx)
	if err != nil {
		return ""
	}

	return config.Default
}

// SaveSettings creates or replaces the settings of a subject.
func (s *Service) SaveSettings(ctx context.Context, settings Settings) error {
	if err := checkSettings(settings); err != nil {
		return err
	}

	now, actor := db.Stamp(ctx)

	var closesAt *time.Time

	if settings.ClosesAt != nil {
		deadline := settings.ClosesAt.UTC()
		closesAt = &deadline
	}

	err := s.db.Tx(ctx, func(ctx context.Context) error {
		result, err := s.db.Exec(ctx, `
			UPDATE wt_registration_subject
			SET title=?, is_open=?, capacity=?, closes_at=?, adult_price=?, child_price=?,
				allow_children=?, updated_at=?, updated_by=?
			WHERE instance=? AND subject_id=?`,
			settings.Title, settings.Open, settings.Capacity, closesAt, settings.AdultPrice, settings.ChildPrice,
			settings.AllowChildren, now, actor,
			s.key, settings.SubjectID,
		)
		if err != nil {
			return err
		}

		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}

		if changed == 0 {
			_, err = s.db.Exec(ctx, `
				INSERT INTO wt_registration_subject
					(instance, subject_id, title, is_open, capacity, closes_at, adult_price, child_price,
					allow_children, created_at, created_by, updated_at, updated_by)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				s.key, settings.SubjectID, settings.Title, settings.Open, settings.Capacity, closesAt,
				settings.AdultPrice, settings.ChildPrice, settings.AllowChildren, now, actor, now, actor,
			)
			if err != nil {
				return err
			}
		}

		for _, text := range settings.Texts {
			if err := s.saveText(ctx, settings.SubjectID, text); err != nil {
				return err
			}
		}

		return s.trackMedia(ctx, settings.SubjectID)
	})

	return db.AfterWrite(ctx, err, s.changed)
}

// subjectTextOwner is the owner of the media the notes of a subject use, see
// media.Track.
const subjectTextOwner = "wt_registration_subject_text"

// trackMedia records the media the notes of a subject use, in every
// language, so they cannot be deleted while the notes show them.
func (s *Service) trackMedia(ctx context.Context, subjectID string) error {
	var notes []string

	err := s.db.Select(ctx, &notes, `
		SELECT notes FROM wt_registration_subject_text
		WHERE instance=? AND subject_id=?`,
		s.key, subjectID,
	)
	if err != nil {
		return err
	}

	var ids []string

	for _, text := range notes {
		ids = append(ids, markdown.MediaIDs(text)...)
	}

	return media.Track(ctx, s.db, subjectTextOwner, s.key+"/"+subjectID, ids)
}

// renderNotes renders the editor's notes for registrants for the website.
func (s *Service) renderNotes(ctx context.Context, source string) template.HTML {
	if s.frontend == nil {
		return template.HTML(template.HTMLEscapeString(source)) //nolint:gosec // Escaped.
	}

	return s.frontend.Markdown(ctx, source)
}

// saveText replaces the texts of one language of a subject.
func (s *Service) saveText(ctx context.Context, subjectID string, text SubjectText) error {
	_, err := s.db.Exec(ctx, `
		DELETE FROM wt_registration_subject_text
		WHERE instance=? AND subject_id=? AND locale=?`,
		s.key, subjectID, text.Locale,
	)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO wt_registration_subject_text (instance, subject_id, locale, notes, payment_link)
		VALUES (?, ?, ?, ?, ?)`,
		s.key, subjectID, text.Locale, strings.TrimSpace(text.Notes), strings.TrimSpace(text.PaymentLink),
	)

	return err
}

func checkSettings(settings Settings) error {
	switch {
	case !validSubjectID(settings.SubjectID):
		return ErrInvalid
	case settings.Capacity < 0 || settings.Capacity > 1_000_000:
		return ErrInvalid
	case settings.AdultPrice < 0 || settings.ChildPrice < 0:
		return ErrInvalid
	case utf8.RuneCountInString(settings.Title) > 200:
		return ErrInvalid
	}

	for _, text := range settings.Texts {
		if _, err := locales.Normalize(text.Locale); err != nil {
			return ErrInvalid
		}

		// Count characters, as the form's maxLength does.
		if utf8.RuneCountInString(text.PaymentLink) > 500 || utf8.RuneCountInString(text.Notes) > 5000 {
			return ErrInvalid
		}
	}

	return nil
}

// validSubjectID accepts event IDs and other simple identifiers that are safe
// in URLs.
func validSubjectID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}

	for _, r := range id {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:", r)
		if !ok {
			return false
		}
	}

	return true
}

// Availability tells whether visitors can register for a subject now, with
// the notes for registrants in locale. A subject without settings is closed
// and not Configured.
func (s *Service) Availability(ctx context.Context, subjectID, locale string) (Availability, error) {
	availability := Availability{
		SubjectID: subjectID,
		Closed:    true,
		Currency:  s.currency,
		Action:    s.registerURL(),
		Form:      signupForm(ctx, s.registerURL(), s.key, subjectID, 0),
	}

	settings, err := s.Settings(ctx, subjectID)
	if errors.Is(err, ErrNotFound) {
		return availability, nil
	}

	if err != nil {
		return availability, err
	}

	deadline, err := s.deadline(ctx, settings)
	if err != nil {
		return availability, err
	}

	taken, err := s.taken(ctx, subjectID)
	if err != nil {
		return availability, err
	}

	availability.Configured = true
	availability.Capacity = settings.Capacity
	availability.Taken = taken
	availability.Full = settings.Capacity > 0 && taken >= settings.Capacity
	availability.Closed = !settings.Open || deadline != nil && !time.Now().Before(*deadline)
	availability.Open = !availability.Closed && !availability.Full
	availability.AllowChildren = settings.AllowChildren
	availability.Prices = Prices{Adult: settings.AdultPrice, Child: settings.ChildPrice}
	availability.Notes = s.renderNotes(ctx, s.text(ctx, settings, locale).Notes)

	if settings.Capacity > 0 {
		availability.Left = max(settings.Capacity-taken, 0)
		availability.Form = signupForm(ctx, s.registerURL(), s.key, subjectID, availability.Left)
	}

	if deadline != nil && !deadline.IsZero() {
		availability.ClosesAt = deadline.In(s.timezone(ctx))
	}

	return availability, nil
}

// availabilities tells for every subject of settings (see AllSettings) what
// Availability tells for one, from the places taken in summaries (see
// Summaries), without the notes for registrants and the closing time: what
// the admin overview shows, without queries per subject. Only an open
// subject of a linked events instance without a deadline of its own looks
// up its event, whose start closes registration.
func (s *Service) availabilities(
	ctx context.Context,
	settings []Settings,
	summaries map[string]Summary,
) (map[string]Availability, error) {
	list := make(map[string]Availability, len(settings))

	for _, subject := range settings {
		deadline := subject.ClosesAt

		// A closed subject stays closed whatever its deadline.
		if subject.Open {
			var err error

			deadline, err = s.deadline(ctx, subject)
			if err != nil {
				return nil, err
			}
		}

		taken := summaries[subject.SubjectID].Places
		availability := Availability{
			SubjectID:     subject.SubjectID,
			Configured:    true,
			Currency:      s.currency,
			Action:        s.registerURL(),
			Capacity:      subject.Capacity,
			Taken:         taken,
			Full:          subject.Capacity > 0 && taken >= subject.Capacity,
			Closed:        !subject.Open || deadline != nil && !time.Now().Before(*deadline),
			AllowChildren: subject.AllowChildren,
			Prices:        Prices{Adult: subject.AdultPrice, Child: subject.ChildPrice},
		}
		availability.Open = !availability.Closed && !availability.Full

		if subject.Capacity > 0 {
			availability.Left = max(subject.Capacity-taken, 0)
		}

		availability.Form = signupForm(ctx, s.registerURL(), s.key, subject.SubjectID, availability.Left)
		list[subject.SubjectID] = availability
	}

	return list, nil
}

// deadline is when registration closes: the configured deadline, or the start
// of the event. A linked event that no longer exists closes registration.
func (s *Service) deadline(ctx context.Context, settings Settings) (*time.Time, error) {
	if settings.ClosesAt != nil || !s.linked() {
		return settings.ClosesAt, nil
	}

	subject, err := s.event(ctx, settings.SubjectID, "")
	if errors.Is(err, ErrNotFound) {
		past := time.Time{}

		return &past, nil
	}

	if err != nil {
		return nil, err
	}

	return &subject.Start, nil
}

// timezone is the site's timezone, for deadlines shown to people.
func (s *Service) timezone(ctx context.Context) *time.Location {
	settings, err := s.site.Settings(ctx)
	if err != nil {
		return time.UTC
	}

	return settings.Location()
}

// taken counts the places of active registrations: neither cancelled nor
// in the trash.
func (s *Service) taken(ctx context.Context, subjectID string) (int, error) {
	var taken int

	err := s.db.Get(ctx, &taken, `
		SELECT COALESCE(SUM(adults + children), 0)
		FROM wt_registration
		WHERE instance=? AND subject_id=? AND cancelled_at IS NULL AND deleted_at IS NULL`,
		s.key, subjectID,
	)

	return taken, err
}

// Register signs a visitor up. It returns ErrInvalid for incomplete sign-ups,
// ErrClosed when registration is not open and ErrFull when not enough places
// are left. Concurrent sign-ups for one subject never exceed its capacity.
func (s *Service) Register(ctx context.Context, signup Signup) (Registration, error) {
	signup, err := cleanSignup(signup)
	if err != nil {
		return Registration{}, err
	}

	// A new code is drawn when it happens to exist already.
	for range 5 {
		registration, err := s.register(ctx, signup)
		if !errors.Is(err, errCodeTaken) {
			return registration, err
		}
	}

	return Registration{}, errCodeTaken
}

func (s *Service) register(ctx context.Context, signup Signup) (Registration, error) {
	var registration Registration

	err := s.db.Tx(ctx, func(ctx context.Context) error {
		if err := s.lockSubject(ctx, signup.SubjectID); err != nil {
			return err
		}

		settings, err := s.Settings(ctx, signup.SubjectID)
		if errors.Is(err, ErrNotFound) {
			return ErrClosed
		}

		if err != nil {
			return err
		}

		registration, err = s.admit(ctx, settings, signup)
		if err != nil {
			return err
		}

		return s.insert(ctx, registration)
	})
	if err := db.AfterWrite(ctx, err, s.changed); err != nil {
		return Registration{}, err
	}

	return registration, nil
}

// lockSubject touches the settings row of a subject, which locks it: concurrent
// sign-ups and restores for the same subject wait here and count the places
// committed before them. It is no change, so updated_at stays.
func (s *Service) lockSubject(ctx context.Context, subjectID string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE wt_registration_subject SET capacity=capacity WHERE instance=? AND subject_id=?`,
		s.key, subjectID,
	)

	return err
}

// admit checks the settings and capacity and returns the registration to store.
func (s *Service) admit(ctx context.Context, settings Settings, signup Signup) (Registration, error) {
	if signup.Children > 0 && !settings.AllowChildren {
		return Registration{}, ErrInvalid
	}

	deadline, err := s.deadline(ctx, settings)
	if err != nil {
		return Registration{}, err
	}

	if !settings.Open || deadline != nil && !time.Now().Before(*deadline) {
		return Registration{}, ErrClosed
	}

	places := signup.Adults + signup.Children

	if settings.Capacity > 0 {
		taken, err := s.taken(ctx, signup.SubjectID)
		if err != nil {
			return Registration{}, err
		}

		if taken+places > settings.Capacity {
			return Registration{}, ErrFull
		}
	}

	code, err := newCode()
	if err != nil {
		return Registration{}, err
	}

	total := Cents(signup.Adults)*settings.AdultPrice + Cents(signup.Children)*settings.ChildPrice

	payment := PaymentNone
	if total > 0 {
		payment = PaymentPending
	}

	return Registration{
		ID:        db.NewID(),
		SubjectID: signup.SubjectID,
		Code:      code,
		Name:      signup.Name,
		Email:     signup.Email,
		Adults:    signup.Adults,
		Children:  signup.Children,
		Total:     total,
		Payment:   payment,
		Notes:     signup.Notes,
		Locale:    signup.Locale,
		CreatedAt: time.Now().UTC(),
		CreatedBy: db.Actor(ctx),
	}, nil
}

func (s *Service) insert(ctx context.Context, registration Registration) error {
	// Postgres aborts the transaction on a duplicate key, so the code is
	// checked first; the unique index still guards against races. Codes in
	// the trash count too: a restored registration keeps its code.
	var existing int

	err := s.db.Get(ctx, &existing, `SELECT COUNT(*) FROM wt_registration WHERE code=?`, registration.Code)
	if err != nil {
		return err
	}

	if existing > 0 {
		return errCodeTaken
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO wt_registration
			(id, instance, subject_id, code, name, email, adults, children, total, payment_status,
			notes, admin_notes, locale, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?)`,
		registration.ID, s.key, registration.SubjectID, registration.Code, registration.Name, registration.Email,
		registration.Adults, registration.Children, registration.Total, registration.Payment,
		registration.Notes, registration.Locale, registration.CreatedAt, registration.CreatedBy,
		registration.CreatedAt, registration.CreatedBy,
	)
	if db.IsUnique(err) {
		return errCodeTaken
	}

	return err
}

// cleanSignup trims a sign-up and checks its fields.
func cleanSignup(signup Signup) (Signup, error) {
	signup.SubjectID = strings.TrimSpace(signup.SubjectID)
	signup.Name = strings.Join(strings.Fields(signup.Name), " ")
	signup.Email = strings.TrimSpace(signup.Email)
	signup.Notes = strings.TrimSpace(signup.Notes)

	input := SignupInput{
		Subject:  signup.SubjectID,
		Name:     signup.Name,
		Email:    signup.Email,
		Adults:   signup.Adults,
		Children: signup.Children,
		Notes:    signup.Notes,
	}

	switch {
	case len(signupChecks.ValidateForm(input)) > 0:
		return signup, ErrInvalid
	case !mail.Valid(signup.Email):
		return signup, ErrInvalid
	case signup.Adults+signup.Children > maxPlaces:
		return signup, ErrInvalid
	case len(signup.Locale) > 35:
		return signup, ErrInvalid
	}

	return signup, nil
}

// codeAlphabet leaves out characters that are easily confused, such as 0 and O.
const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// newCode returns a random code such as "K7Q4-9M2X".
func newCode() (string, error) {
	random := make([]byte, 8)

	if _, err := rand.Read(random); err != nil {
		return "", err
	}

	code := make([]byte, 0, 9)

	for i, b := range random {
		if i == 4 {
			code = append(code, '-')
		}

		code = append(code, codeAlphabet[int(b)%len(codeAlphabet)])
	}

	return string(code), nil
}

// normalizeCode accepts codes as people type them: lowercase, with spaces or
// without the dash.
func normalizeCode(code string) string {
	var b strings.Builder

	for _, r := range strings.ToUpper(code) {
		if strings.ContainsRune(codeAlphabet, r) {
			b.WriteRune(r)
		}
	}

	clean := b.String()
	if len(clean) != 8 {
		return ""
	}

	return clean[:4] + "-" + clean[4:]
}

const registrationColumns = `id, subject_id, code, name, email, adults, children, total, payment_status,
	notes, admin_notes, locale, created_at, cancelled_at, created_by, updated_at, updated_by, deleted_at, deleted_by`

// ByCode returns the registration with code, or ErrNotFound; also when it is
// in the trash. The code may be typed loosely, for example "k7q4 9m2x".
func (s *Service) ByCode(ctx context.Context, code string) (Registration, error) {
	code = normalizeCode(code)
	if code == "" {
		return Registration{}, ErrNotFound
	}

	var registration Registration

	err := s.db.Get(ctx, &registration, `
		SELECT `+registrationColumns+`
		FROM wt_registration
		WHERE instance=? AND code=? AND deleted_at IS NULL`,
		s.key, code,
	)

	return registration, err
}

// Registration returns one registration, or ErrNotFound; also when it is in
// the trash.
func (s *Service) Registration(ctx context.Context, id string) (Registration, error) {
	var registration Registration

	err := s.db.Get(ctx, &registration, `
		SELECT `+registrationColumns+`
		FROM wt_registration
		WHERE instance=? AND id=? AND deleted_at IS NULL`,
		s.key, id,
	)

	return registration, err
}

// Registrations returns every registration of a subject, cancelled ones
// included and those in the trash left out, in the order they came in.
func (s *Service) Registrations(ctx context.Context, subjectID string) ([]Registration, error) {
	var list []Registration

	err := s.db.Select(ctx, &list, `
		SELECT `+registrationColumns+`
		FROM wt_registration
		WHERE instance=? AND subject_id=? AND deleted_at IS NULL
		ORDER BY created_at, id`,
		s.key, subjectID,
	)

	return list, err
}

// Summaries counts active registrations and places per subject.
func (s *Service) Summaries(ctx context.Context) (map[string]Summary, error) {
	var list []Summary

	err := s.db.Select(ctx, &list, `
		SELECT subject_id, COUNT(*) AS registrations, COALESCE(SUM(adults + children), 0) AS places
		FROM wt_registration
		WHERE instance=? AND cancelled_at IS NULL AND deleted_at IS NULL
		GROUP BY subject_id`,
		s.key,
	)
	if err != nil {
		return nil, err
	}

	summaries := make(map[string]Summary, len(list))

	for _, summary := range list {
		summaries[summary.SubjectID] = summary
	}

	return summaries, nil
}

// Update changes the payment status and the admin notes of a registration.
func (s *Service) Update(ctx context.Context, id string, payment PaymentStatus, adminNotes string) error {
	if !payment.Valid() || utf8.RuneCountInString(adminNotes) > 5000 {
		return ErrInvalid
	}

	at, by := db.Stamp(ctx)

	return db.Affected(s.db.Exec(ctx, `
		UPDATE wt_registration SET payment_status=?, admin_notes=?, updated_at=?, updated_by=?
		WHERE instance=? AND id=? AND deleted_at IS NULL`,
		payment, adminNotes, at, by, s.key, id,
	))
}

// Cancel cancels a registration, freeing its places. Unpaid registrations
// get payment status cancelled; paid ones stay paid so they can be refunded.
// Cancelling twice is harmless; a registration in the trash is ErrNotFound.
func (s *Service) Cancel(ctx context.Context, id string) error {
	now, actor := db.Stamp(ctx)

	err := db.Affected(s.db.Exec(ctx, `
		UPDATE wt_registration
		SET cancelled_at=?,
			payment_status=CASE WHEN payment_status='pending' THEN 'cancelled' ELSE payment_status END,
			updated_at=?, updated_by=?
		WHERE instance=? AND id=? AND cancelled_at IS NULL AND deleted_at IS NULL`,
		now, now, actor, s.key, id,
	))
	if !errors.Is(err, ErrNotFound) {
		return db.AfterWrite(ctx, err, s.changed)
	}

	// Nothing changed: the registration was cancelled already, or is not
	// there at all.
	_, err = s.Registration(ctx, id)

	return err
}

// rows is the instance's part of wt_registration, for moving registrations
// to the trash and back.
func (s *Service) rows() db.SoftTable {
	return db.SoftTable{DB: s.db, Table: "wt_registration", Scope: "instance=?", Args: []any{s.key}}
}

// Delete moves a registration to the trash, freeing its places. One that is
// unknown or already in the trash is ErrNotFound.
func (s *Service) Delete(ctx context.Context, id string) error {
	return db.AfterWrite(ctx, s.rows().Delete(ctx, id), s.changed)
}

// Trashed returns registrations in the trash of every subject, most
// recently deleted first: at most limit of them from offset (all for a
// limit of 0), and how many the trash holds in all.
func (s *Service) Trashed(ctx context.Context, limit, offset int) ([]Registration, int, error) {
	var list []Registration

	total, err := s.rows().Trashed(ctx, &list, registrationColumns, limit, offset)

	return list, total, err
}

// Undelete brings a registration back from the trash. An active one takes
// its places again, so it stays in the trash with ErrNoPlaces when the
// subject has not enough places left; a cancelled one comes back as it was.
// One that is not in the trash is ErrNotFound.
func (s *Service) Undelete(ctx context.Context, id string) error {
	err := s.rows().Undelete(ctx, id, func(ctx context.Context) error {
		var registration Registration

		if err := s.rows().GetTrashed(ctx, &registration, registrationColumns, id); err != nil {
			return err
		}

		if registration.Cancelled() {
			return nil
		}

		return s.checkPlaces(ctx, registration)
	})

	return db.AfterWrite(ctx, err, s.changed)
}

// checkPlaces returns ErrNoPlaces when the capacity of the subject leaves no
// room for the places of a registration that comes back from the trash.
func (s *Service) checkPlaces(ctx context.Context, registration Registration) error {
	if err := s.lockSubject(ctx, registration.SubjectID); err != nil {
		return err
	}

	settings, err := s.Settings(ctx, registration.SubjectID)
	if errors.Is(err, ErrNotFound) || err == nil && settings.Capacity == 0 {
		return nil
	}

	if err != nil {
		return err
	}

	taken, err := s.taken(ctx, registration.SubjectID)
	if err != nil {
		return err
	}

	if taken+registration.Places() > settings.Capacity {
		return ErrNoPlaces
	}

	return nil
}

// Purge removes a registration in the trash for good; one that is not in
// the trash is ErrNotFound.
func (s *Service) Purge(ctx context.Context, id string) error {
	return s.rows().Purge(ctx, id)
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
