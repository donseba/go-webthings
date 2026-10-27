package registrations

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"

	form "github.com/donseba/go-form/v2"
)

func section(env webthings.Env, service *Service) admin.Section {
	return admin.Section{
		ID:    env.Key,
		Title: env.Title,
		Icon:  "calendar",
		Files: files,
		Trash: service.trash(),
		Routes: func(routes *admin.Routes) {
			screens := screens{service: service, admin: routes.Admin(), env: env, routes: routes}

			routes.Get("", admin.View, screens.overview).As("index")
			routes.Get("/new", admin.Create, screens.newSubject).As("new")
			routes.Post("/new", admin.Create, screens.createSubject)
			routes.Get("/{subject}", admin.View, screens.subject).As("subject")
			routes.Get("/{subject}/settings", admin.Edit, screens.settings).As("settings")
			routes.Post("/{subject}/settings", admin.Edit, screens.saveSettings)
			routes.Post("/{subject}/settings/texts", admin.Edit, screens.saveTexts).As("texts")
			routes.Get("/{subject}/export.csv", admin.Export, screens.export).As("export")
			routes.Get("/{subject}/{id}", admin.View, screens.registration).As("registration")
			routes.Post("/{subject}/{id}", admin.Edit, screens.updateRegistration)
			routes.Post("/{subject}/{id}/cancel", admin.Edit, screens.cancel).As("cancel")
			routes.Post("/{subject}/{id}/delete", admin.Delete, screens.delete).As("delete")
		},
	}
}

// screens are the admin pages of one instance.
type screens struct {
	service *Service
	admin   *admin.Admin
	env     webthings.Env
	routes  *admin.Routes
}

// OverviewView lists the subjects with their registrations.
type OverviewView struct {
	Title string
	// Events is set when the subjects are events; rows then have a date.
	Events bool
	// NewURL adds a subject; empty for events and without the right.
	NewURL string
	// TrashURL shows the deleted registrations in the admin's trash.
	TrashURL string
	Rows     []OverviewRow
}

// OverviewRow is one event or subject in the overview, with its number of
// registrations and the places taken, out of the capacity when there is
// one.
type OverviewRow struct {
	URL   string
	Title string
	Date  string
	// Status is "Open", "Closed" or "Full"; it is translated.
	Status        string
	Registrations int
	Places        string
}

func (s screens) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	subjects, all, err := s.subjects(ctx)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	summaries, err := s.service.Summaries(ctx)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	availabilities, err := s.service.availabilities(ctx, all, summaries)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	view := OverviewView{Title: s.env.Title, Events: s.service.linked(), TrashURL: s.admin.TrashURL(s.env.Key)}

	if !view.Events && s.admin.Can(r, s.permission(admin.Create)) {
		view.NewURL = s.routes.URL("new")
	}

	search := admin.SearchOf(r)

	for _, subject := range subjects {
		if !search.Matches(subject.Title) {
			continue
		}

		// A subject without settings is closed, as Availability tells.
		availability, ok := availabilities[subject.ID]
		if !ok {
			availability = Availability{SubjectID: subject.ID, Closed: true}
		}

		row := OverviewRow{
			URL:           s.subjectURL(subject.ID),
			Title:         subject.Title,
			Status:        status(availability),
			Registrations: summaries[subject.ID].Registrations,
			Places:        places(availability),
		}

		row.Date = admin.DateTime(ctx, subject.Start)

		view.Rows = append(view.Rows, row)
	}

	s.admin.Render(w, r, admin.Page{
		Title:    s.env.Title,
		Template: "templates/admin/overview.gohtml",
		Files:    files,
		Data:     view,
	})
}

// subjects are the events of the linked instance followed by subjects with
// settings that are no event, or every subject with settings; all are the
// settings of every subject (see AllSettings).
func (s screens) subjects(ctx context.Context) (subjects []Subject, all []Settings, err error) {
	all, err = s.service.AllSettings(ctx)
	if err != nil {
		return nil, nil, err
	}

	if s.service.linked() {
		subjects, err = s.service.eventSubjects(ctx, s.contentLocale(ctx))
		if err != nil {
			return nil, nil, err
		}
	}

	known := map[string]bool{}

	for _, subject := range subjects {
		known[subject.ID] = true
	}

	for _, settings := range all {
		if known[settings.SubjectID] {
			continue
		}

		title := settings.Title
		if title == "" {
			title = settings.SubjectID
		}

		subjects = append(subjects, Subject{ID: settings.SubjectID, Title: title})
	}

	return subjects, all, nil
}

// contentLocale is the site's default language, for event titles.
func (s screens) contentLocale(ctx context.Context) string {
	config, err := s.env.Locales.Configuration(ctx)
	if err != nil {
		return ""
	}

	return config.Default
}

func status(availability Availability) string {
	switch {
	case availability.Open:
		return "Open"
	case availability.Full:
		return "Full"
	}

	return "Closed"
}

// places shows the places taken, out of the capacity when there is one.
func places(availability Availability) string {
	taken := strconv.Itoa(availability.Taken)
	if availability.Capacity == 0 {
		return taken
	}

	return taken + " / " + strconv.Itoa(availability.Capacity)
}

// NewSubjectView adds a subject that is not an event.
type NewSubjectView struct {
	Title   string
	BackURL string
	Form    form.RenderModel
	Errors  form.FieldErrors
}

type newSubjectInput struct {
	Subject string `form:"input,text" label:"Subject ID" required:"true" maxLength:"64"  trim:"true" pattern:"^[A-Za-z0-9_.:-]+$" patternMsg:"Use letters, digits, dashes, dots, colons and underscores." description:"Letters, digits, dashes, dots, colons and underscores, for example a page or product ID. Sign-up forms send it in the subject field."`
	Title   string `form:"input,text" label:"Name"       required:"true" maxLength:"200" trim:"true"`
}

func (s screens) newSubject(w http.ResponseWriter, r *http.Request) {
	s.renderNew(w, r, newSubjectInput{}, nil, http.StatusOK)
}

func (s screens) createSubject(w http.ResponseWriter, r *http.Request) {
	// Events are the subjects of instances linked to them.
	if s.service.linked() {
		http.NotFound(w, r)

		return
	}

	var input newSubjectInput

	errs, err := s.admin.Bind(r, &input)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	if len(errs) == 0 {
		_, err := s.service.Settings(r.Context(), input.Subject)
		if err == nil {
			errs = admin.FieldError("Subject", s.admin.T(r, "This subject already exists."))
		} else if !errors.Is(err, ErrNotFound) {
			s.admin.Fail(w, r, err)

			return
		}
	}

	if len(errs) > 0 {
		s.renderNew(w, r, input, errs, http.StatusUnprocessableEntity)

		return
	}

	err = s.service.SaveSettings(r.Context(), Settings{SubjectID: input.Subject, Title: input.Title})
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Redirect(w, r, s.routes.URL("settings", "subject", input.Subject))
}

func (s screens) renderNew(w http.ResponseWriter, r *http.Request, input newSubjectInput, errs form.FieldErrors, status int) {
	if s.service.linked() {
		http.NotFound(w, r)

		return
	}

	view := NewSubjectView{
		Title:   s.env.Title,
		BackURL: s.routes.URL("index"),
		Form:    s.admin.Form(r, input, s.routes.URL("new"), "Create"),
		Errors:  errs,
	}

	s.admin.Render(w, r, admin.Page{
		Title:    "New subject",
		Template: "templates/admin/new.gohtml",
		Files:    files,
		Data:     view,
		Status:   status,
	})
}

// SubjectView shows one subject and its registrations.
type SubjectView struct {
	Title   string
	Date    string
	BackURL string
	// BackLabel names the overview BackURL leads to, as a translation key.
	BackLabel string
	// SettingsURL is empty without the right to change settings.
	SettingsURL string
	// ExportURL is empty without the right to export.
	ExportURL string
	Status    string
	Facts     []Fact
	Rows      []RegistrationRow
	// Registrations, Places and Total count active registrations.
	Registrations int
	Places        int
	Total         string
}

// Fact is one setting in the summary; Label is translated.
type Fact struct {
	Label string
	Value string
}

// RegistrationRow is one registration in the list of an event or
// subject; Cancelled registrations stay in the list.
type RegistrationRow struct {
	URL       string
	Code      string
	Name      string
	Email     string
	Adults    int
	Children  int
	Total     string
	Payment   string
	Date      string
	Cancelled bool
}

func (s screens) subject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	subject, settings, ok := s.load(w, r)
	if !ok {
		return
	}

	availability, err := s.service.Availability(ctx, subject.ID, "")
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	registrations, err := s.service.Registrations(ctx, subject.ID)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	view := SubjectView{
		Title:     subject.Title,
		BackURL:   s.routes.URL("index"),
		BackLabel: s.env.Title,
		Status:    status(availability),
		Facts:     s.facts(r, settings, availability),
	}

	view.Date = admin.DateTime(ctx, subject.Start)

	if s.admin.Can(r, s.permission(admin.Edit)) {
		view.SettingsURL = s.routes.URL("settings", "subject", subject.ID)
	}

	// Exporting personal data needs its own right.
	if s.admin.Can(r, s.permission(admin.Export)) {
		view.ExportURL = s.routes.URL("export", "subject", subject.ID)
	}

	var total Cents

	// The totals count every registration; a search only narrows the rows.
	search := admin.SearchOf(r)

	for _, registration := range registrations {
		if !registration.Cancelled() {
			view.Registrations++
			view.Places += registration.Places()
			total += registration.Total
		}

		if !search.Matches(registration.Name, registration.Email, registration.Code) {
			continue
		}

		view.Rows = append(view.Rows, RegistrationRow{
			URL:       s.registrationURL(subject.ID, registration.ID),
			Code:      registration.Code,
			Name:      registration.Name,
			Email:     registration.Email,
			Adults:    registration.Adults,
			Children:  registration.Children,
			Total:     s.money(ctx, registration.Total),
			Payment:   paymentLabel(ctx, registration.Payment),
			Date:      admin.DateTime(ctx, registration.CreatedAt),
			Cancelled: registration.Cancelled(),
		})
	}

	view.Total = s.money(ctx, total)

	// The subject's title is shown as written, never translated.
	s.admin.Render(w, r, admin.Page{
		Title:    s.env.Title,
		Label:    subject.Title,
		Template: "templates/admin/subject.gohtml",
		Files:    files,
		Data:     view,
	})
}

// facts summarise the settings of a subject.
func (s screens) facts(r *http.Request, settings Settings, availability Availability) []Fact {
	capacity := s.admin.T(r, "Unlimited")
	if settings.Capacity > 0 {
		capacity = strconv.Itoa(settings.Capacity)
	}

	ctx := r.Context()

	deadline := "—"
	if !availability.ClosesAt.IsZero() {
		deadline = admin.DateTime(ctx, availability.ClosesAt)
	}

	facts := []Fact{
		{Label: "Places", Value: capacity},
		{Label: "Places taken", Value: strconv.Itoa(availability.Taken)},
		{Label: "Registration closes", Value: deadline},
		{Label: "Price per adult", Value: s.money(ctx, settings.AdultPrice)},
	}

	if settings.AllowChildren {
		facts = append(facts, Fact{Label: "Price per child", Value: s.money(ctx, settings.ChildPrice)})
	}

	// What registrants read is shown in the site's default language.
	text := s.service.text(r.Context(), settings, "")
	if text.PaymentLink != "" {
		facts = append(facts, Fact{Label: "Payment link", Value: text.PaymentLink})
	}

	return facts
}

// load returns the subject in the URL with its settings (empty when it has
// none yet), or answers not found.
func (s screens) load(w http.ResponseWriter, r *http.Request) (Subject, Settings, bool) {
	ctx := r.Context()
	id := r.PathValue("subject")

	if !validSubjectID(id) {
		http.NotFound(w, r)

		return Subject{}, Settings{}, false
	}

	settings, err := s.service.Settings(ctx, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		s.admin.Fail(w, r, err)

		return Subject{}, Settings{}, false
	}

	exists := err == nil

	if s.service.linked() {
		_, err := s.service.event(ctx, id, "")
		if err != nil && !errors.Is(err, ErrNotFound) {
			s.admin.Fail(w, r, err)

			return Subject{}, Settings{}, false
		}

		exists = exists || err == nil
	}

	if !exists {
		http.NotFound(w, r)

		return Subject{}, Settings{}, false
	}

	subject, err := s.service.Subject(ctx, id, s.contentLocale(ctx))
	if err != nil {
		s.admin.Fail(w, r, err)

		return Subject{}, Settings{}, false
	}

	settings.SubjectID = id

	return subject, settings, true
}

// SettingsView edits the settings of a subject.
type SettingsView struct {
	Title   string
	BackURL string
	Form    form.RenderModel
	Errors  form.FieldErrors
	// Tabs switch the language of Texts, what registrants read.
	Tabs       []admin.LocaleTab
	Texts      form.RenderModel
	TextErrors form.FieldErrors
}

type settingsInput struct {
	Title         string     `form:"input,text"           label:"Name"                       maxLength:"200" trim:"true" description:"Shown in the admin and in e-mail. Events use their own title when empty."`
	Open          bool       `form:"checkbox"             label:"Open for registration"`
	Capacity      int        `form:"input,number"         label:"Places"                                                 description:"Adults and children together. 0 means unlimited."                                            min:"0" max:"1000000"`
	ClosesAt      *time.Time `form:"input,datetime-local" label:"Registration closes"                                    description:"In the site's timezone. Registration for an event closes when it starts when this is empty."`
	AdultPrice    string     `form:"input,text"           label:"Price per adult"            maxLength:"12"                                                                                                                                              pattern:"^\\s*\\d{0,9}(?:[.,]\\d{0,2})?\\s*$" patternMsg:"Enter an amount such as 12.50." placeholder:"0.00"`
	AllowChildren bool       `form:"checkbox"             label:"Children can be registered"`
	ChildPrice    string     `form:"input,text"           label:"Price per child"            maxLength:"12"                                                                                                                                              pattern:"^\\s*\\d{0,9}(?:[.,]\\d{0,2})?\\s*$" patternMsg:"Enter an amount such as 12.50." placeholder:"0.00"`
}

type textInput struct {
	Notes       string `form:"textarea"   label:"Notes for registrants" maxLength:"5000" class:"markdown" description:"Shown with the sign-up form, on the registration page and in the confirmation."`
	PaymentLink string `form:"input,text" label:"Payment link"          maxLength:"500"                   description:"A payment URL or bank details, shown in the confirmation while payment is pending."`
}

func (s screens) settings(w http.ResponseWriter, r *http.Request) {
	subject, settings, ok := s.load(w, r)
	if !ok {
		return
	}

	s.renderSettings(w, r, subject, settings, settingsForm(settings), nil, nil, nil, http.StatusOK)
}

// settingsForm is the general form filled with the stored settings.
func settingsForm(settings Settings) settingsInput {
	return settingsInput{
		Title:         settings.Title,
		Open:          settings.Open,
		Capacity:      settings.Capacity,
		ClosesAt:      settings.ClosesAt,
		AdultPrice:    settings.AdultPrice.String(),
		AllowChildren: settings.AllowChildren,
		ChildPrice:    settings.ChildPrice.String(),
	}
}

func (s screens) saveSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	subject, stored, ok := s.load(w, r)
	if !ok {
		return
	}

	var input settingsInput

	// The deadline is entered in the site's timezone.
	errs, err := s.admin.Bind(r, &input)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	settings := Settings{
		SubjectID:     subject.ID,
		Title:         input.Title,
		ClosesAt:      utc(input.ClosesAt),
		Open:          input.Open,
		Capacity:      input.Capacity,
		AllowChildren: input.AllowChildren,
	}

	// The prices' pattern only lets through what ParseCents reads, so an
	// error here comes with a field error already.
	settings.AdultPrice, _ = ParseCents(input.AdultPrice)
	settings.ChildPrice, _ = ParseCents(input.ChildPrice)

	if !s.service.linked() && settings.Title == "" {
		errs = append(errs, admin.FieldError("Title", s.admin.T(r, "Enter a name."))...)
	}

	if len(errs) > 0 {
		s.renderSettings(w, r, subject, stored, input, errs, nil, nil, http.StatusUnprocessableEntity)

		return
	}

	if err := s.service.SaveSettings(ctx, settings); err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Redirect(w, r, s.subjectURL(subject.ID))
}

// saveTexts stores what registrants read in the language of the tab. The
// subject gets settings (closed) when it has none yet.
func (s screens) saveTexts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	subject, stored, ok := s.load(w, r)
	if !ok {
		return
	}

	var input textInput

	errs, err := s.admin.Bind(r, &input)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	locale, _, err := s.locale(r)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	if len(errs) > 0 {
		s.renderSettings(w, r, subject, stored, settingsForm(stored), nil, &input, errs, http.StatusUnprocessableEntity)

		return
	}

	stored.Texts = []SubjectText{{Locale: locale, Notes: input.Notes, PaymentLink: input.PaymentLink}}

	if err := s.service.SaveSettings(ctx, stored); err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Redirect(w, r, s.routes.URL("settings", "subject", subject.ID, url.Values{"locale": {locale}}))
}

// locale is the language of the texts tab (?locale=) and the enabled
// languages.
func (s screens) locale(r *http.Request) (string, []string, error) {
	config, err := s.env.Locales.Configuration(r.Context())
	if err != nil {
		return "", nil, err
	}

	return admin.ContentLocale(r, config), config.Enabled, nil
}

// utc is t in UTC, as registration deadlines are stored; nil stays nil.
func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	inUTC := t.UTC()

	return &inUTC
}

func (s screens) renderSettings(
	w http.ResponseWriter,
	r *http.Request,
	subject Subject,
	settings Settings,
	input settingsInput,
	errs form.FieldErrors,
	texts *textInput,
	textErrors form.FieldErrors,
	status int,
) {
	locale, enabled, err := s.locale(r)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	if texts == nil {
		text, _ := settings.Text(locale)
		texts = &textInput{Notes: text.Notes, PaymentLink: text.PaymentLink}
	}

	base := s.routes.URL("settings", "subject", subject.ID)
	view := SettingsView{
		Title:   subject.Title,
		BackURL: s.subjectURL(subject.ID),
		Form:    s.admin.Form(r, input, base, "Save"),
		Errors:  errs,
		Texts: s.admin.Form(
			r,
			*texts,
			s.routes.URL("texts", "subject", subject.ID, url.Values{"locale": {locale}}),
			"Save",
		),
		TextErrors: textErrors,
	}

	view.Tabs = admin.LocaleTabs(enabled, locale, base, func(code string) (bool, bool) {
		text, written := settings.Text(code)

		return written && text.Notes != "", written
	})

	s.admin.Render(w, r, admin.Page{
		Title:    "Registration settings",
		Template: "templates/admin/settings.gohtml",
		Files:    files,
		Data:     view,
		Status:   status,
	})
}

// RegistrationAdminView shows one registration to editors.
type RegistrationAdminView struct {
	Title        string
	BackURL      string
	Registration Registration
	// Total is the amount in the admin language, such as "€ 120,00";
	// Payment is the translated payment status.
	Total     string
	Payment   string
	Date      string
	Cancelled string
	// Form changes payment and admin notes; nil without the right.
	Form   *form.RenderModel
	Errors form.FieldErrors
	Cancel *form.RenderModel
	Delete *form.RenderModel
}

type registrationInput struct {
	Payment    form.SortedSelect[string] `form:"dropdown" label:"Payment"     translate:"true" required:"true" requiredMsg:"Choose a payment status." invalidMsg:"Choose a payment status."`
	AdminNotes string                    `form:"textarea" label:"Admin notes"                                                                                                               maxLength:"5000" trim:"true" description:"Only visible in the admin."`
}

// paymentChoices are the payment statuses in their natural order; the form
// translates their names (translate:"true").
func paymentChoices(selected PaymentStatus) form.SortedSelect[string] {
	var options []form.Option[string]

	for _, status := range []PaymentStatus{PaymentNone, PaymentPending, PaymentPaid, PaymentCancelled} {
		options = append(options, form.Option[string]{Value: string(status), Label: paymentName(status)})
	}

	return form.NewOrderedSelect(options...).Selected(string(selected))
}

// paymentName is the name of a payment status, an admin translation key.
func paymentName(status PaymentStatus) string {
	switch status {
	case PaymentNone:
		return "No payment"
	case PaymentPending:
		return "Pending"
	case PaymentPaid:
		return "Paid"
	case PaymentCancelled:
		return "Cancelled"
	}

	return string(status)
}

// paymentLabel names a payment status in the admin language.
func paymentLabel(ctx context.Context, status PaymentStatus) string {
	return admin.T(ctx, paymentName(status))
}

// money shows an amount in the instance's currency in the admin language,
// such as "€ 120,00" for a Dutch editor.
func (s screens) money(ctx context.Context, amount Cents) string {
	return admin.Format(ctx).Money(int64(amount), s.service.currency)
}

func (s screens) registration(w http.ResponseWriter, r *http.Request) {
	registration, ok := s.loadRegistration(w, r)
	if !ok {
		return
	}

	input := registrationInput{
		Payment:    paymentChoices(registration.Payment),
		AdminNotes: registration.AdminNotes,
	}

	s.renderRegistration(w, r, registration, input, nil, http.StatusOK)
}

func (s screens) updateRegistration(w http.ResponseWriter, r *http.Request) {
	registration, ok := s.loadRegistration(w, r)
	if !ok {
		return
	}

	input := registrationInput{Payment: paymentChoices(registration.Payment)}

	errs, err := s.admin.Bind(r, &input)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		s.renderRegistration(w, r, registration, input, errs, http.StatusUnprocessableEntity)

		return
	}

	err = s.service.Update(r.Context(), registration.ID, PaymentStatus(input.Payment.Get()), input.AdminNotes)
	if errors.Is(err, ErrInvalid) {
		errs := admin.FieldError("Payment", s.admin.T(r, "Choose a payment status."))
		s.renderRegistration(w, r, registration, input, errs, http.StatusUnprocessableEntity)

		return
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Redirect(w, r, s.registrationURL(registration.SubjectID, registration.ID))
}

func (s screens) cancel(w http.ResponseWriter, r *http.Request) {
	registration, ok := s.loadRegistration(w, r)
	if !ok {
		return
	}

	if err := s.service.Cancel(r.Context(), registration.ID); err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Redirect(w, r, s.registrationURL(registration.SubjectID, registration.ID))
}

func (s screens) delete(w http.ResponseWriter, r *http.Request) {
	registration, ok := s.loadRegistration(w, r)
	if !ok {
		return
	}

	if err := s.service.Delete(r.Context(), registration.ID); err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Redirect(w, r, s.subjectURL(registration.SubjectID))
}

// loadRegistration returns the registration in the URL, or answers not found.
func (s screens) loadRegistration(w http.ResponseWriter, r *http.Request) (Registration, bool) {
	registration, err := s.service.Registration(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrNotFound) || err == nil && registration.SubjectID != r.PathValue("subject") {
		http.NotFound(w, r)

		return Registration{}, false
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return Registration{}, false
	}

	return registration, true
}

func (s screens) renderRegistration(
	w http.ResponseWriter,
	r *http.Request,
	registration Registration,
	input registrationInput,
	errs form.FieldErrors,
	status int,
) {
	ctx := r.Context()
	base := s.registrationURL(registration.SubjectID, registration.ID)

	view := RegistrationAdminView{
		Title:        registration.Code,
		BackURL:      s.subjectURL(registration.SubjectID),
		Registration: registration,
		Total:        s.money(ctx, registration.Total),
		Payment:      paymentLabel(ctx, registration.Payment),
		Date:         admin.DateTime(ctx, registration.CreatedAt),
		Errors:       errs,
	}

	if registration.CancelledAt != nil {
		view.Cancelled = admin.DateTime(ctx, *registration.CancelledAt)
	}

	if s.admin.Can(r, s.permission(admin.Edit)) {
		edit := s.admin.Form(r, input, base, "Save")
		view.Form = &edit

		if !registration.Cancelled() {
			cancel := s.admin.Form(
				r,
				struct{}{},
				s.routes.URL("cancel", "subject", registration.SubjectID, "id", registration.ID),
				"Cancel registration",
			)
			view.Cancel = &cancel
		}
	}

	if s.admin.Can(r, s.permission(admin.Delete)) {
		remove := s.admin.Form(
			r,
			struct{}{},
			s.routes.URL("delete", "subject", registration.SubjectID, "id", registration.ID),
			"Move to trash",
		)
		view.Delete = &remove
	}

	s.admin.Render(w, r, admin.Page{
		Title:    "Registration",
		Template: "templates/admin/registration.gohtml",
		Files:    files,
		Data:     view,
		Status:   status,
	})
}

// export downloads the registrations of a subject as CSV.
func (s screens) export(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	subject, _, ok := s.load(w, r)
	if !ok {
		return
	}

	registrations, err := s.service.Registrations(ctx, subject.ID)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	location := s.service.timezone(ctx)
	rows := make([][]string, 0, len(registrations))

	for _, registration := range registrations {
		cancelled := ""
		if registration.CancelledAt != nil {
			cancelled = date(location, *registration.CancelledAt)
		}

		rows = append(rows, []string{
			registration.Code,
			registration.Name,
			registration.Email,
			strconv.Itoa(registration.Adults),
			strconv.Itoa(registration.Children),
			registration.Total.String(),
			string(registration.Payment),
			registration.Notes,
			registration.AdminNotes,
			registration.Locale,
			date(location, registration.CreatedAt),
			cancelled,
		})
	}

	header := []string{
		"code", "name", "email", "adults", "children", "total", "payment",
		"notes", "admin_notes", "locale", "registered_at", "cancelled_at",
	}

	if err := admin.WriteCSV(w, s.env.Key+"-"+subject.ID, header, rows); err != nil {
		slog.WarnContext(ctx, "export registrations", "module", s.env.Key, "error", err)
	}
}

func (s screens) subjectURL(id string) string {
	return s.routes.URL("subject", "subject", id)
}

func (s screens) registrationURL(subject, id string) string {
	return s.routes.URL("registration", "subject", subject, "id", id)
}

func (s screens) permission(action string) admin.Permission {
	return admin.Permission{Section: s.env.Key, Action: action}
}

// date shows a time in location, the site's timezone.
func date(location *time.Location, t time.Time) string {
	return t.In(location).Format("2006-01-02 15:04")
}
