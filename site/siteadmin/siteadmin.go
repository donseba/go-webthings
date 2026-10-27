// Package siteadmin is the Site settings section of the admin: the site's
// languages, time zone, texts, social media profiles, performance, security,
// review and trash settings, and the Theme screen where editors choose the
// theme and set its values.
package siteadmin

import (
	"context"
	"embed"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/site"

	form "github.com/donseba/go-form/v2"
)

//go:embed templates translations
var files embed.FS

// settingsInput is the general form. Locales are the listed languages and
// OtherLocales the codes of others, such as "af, pt-BR". The default
// language offers every listed language, so a language ticked in the same
// save can become the default; saving checks that it is ticked.
type settingsInput struct {
	DefaultLocale form.SortedSelect[string]      `form:"dropdown"      label:"Default language" description:"One of the ticked languages."                                                                                 required:"true" requiredMsg:"Choose the default language." invalidMsg:"Choose the default language."`
	Locales       form.SortedMultiSelect[string] `form:"multicheckbox" label:"Languages"                                                                                                                                                                                                                                        class:"grid sm:grid-cols-2 lg:grid-cols-4 gap-x-6"`
	OtherLocales  string                         `form:"input,text"    label:"Other languages"  description:"Codes of languages that are not listed, comma separated, such as af or pt-BR."                                                                                                                                                                                        maxLength:"200"`
	Timezone      form.SortedSelect[string]      `form:"dropdown"      label:"Timezone"                                                                                                                                    required:"true" requiredMsg:"Choose a timezone."           invalidMsg:"Choose a timezone."`
	Logo          form.SortedSelect[string]      `form:"dropdown"      label:"Logo"             description:"Shown by the theme, usually at the top of every page. Upload the image under Media first."                                                                                                                                                                                             placeholder:"—"`
	Favicon       form.SortedSelect[string]      `form:"dropdown"      label:"Favicon"          description:"The small icon in the browser tab. A square image works best. Upload the image under Media first."                                                                                                                                                                                     placeholder:"—"`
	SocialImage   form.SortedSelect[string]      `form:"dropdown"      label:"Social image"     description:"Shown when a page without an image of its own is shared on social media. Upload the image under Media first."                                                                                                                                                                          placeholder:"—"`
	Organisation  string                         `form:"input,text"    label:"Organisation"                                                                                                                                                                                                                                                                                        maxLength:"200"`
	Email         string                         `form:"input,email"   label:"Email"                                                                                                                                                                                                                                                                                               maxLength:"254"`
	Phone         string                         `form:"input,text"    label:"Phone"                                                                                                                                                                                                                                                                                               maxLength:"100"`
	Address       string                         `form:"textarea"      label:"Address"                                                                                                                                                                                                                                                                                             maxLength:"1000"`
	FeedSize      int                            `form:"dropdown"      label:"Items in feeds"   description:"How many of the newest items each Atom and RSS feed of the site lists, such as the posts of a news page."                                                                                                                                                                                              values:"10:10 items;20:20 items;50:50 items;100:100 items"`
}

type textInput struct {
	Title          string `form:"input,text" label:"Site title"      required:"true" maxLength:"200"`
	Tagline        string `form:"input,text" label:"Tagline"                         maxLength:"200"`
	SEOTitle       string `form:"input,text" label:"SEO title"                       maxLength:"200"  description:"The title for search engines, when it should differ from the site title."`
	SEODescription string `form:"textarea"   label:"SEO description"                 maxLength:"1000" description:"A short description of the site for search engines, used on pages without a summary of their own."`
	Footer         string `form:"textarea"   label:"Footer"                          maxLength:"2000" description:"Shown at the bottom of every page."                                                                class:"markdown"`
}

// profilesInput has one address field per network of site.Networks; empty
// fields are no profile.
type profilesInput struct {
	Facebook  string `form:"input,text" label:"Facebook"  maxLength:"1000" placeholder:"https://www.facebook.com/…"`
	Instagram string `form:"input,text" label:"Instagram" maxLength:"1000" placeholder:"https://www.instagram.com/…"`
	LinkedIn  string `form:"input,text" label:"LinkedIn"  maxLength:"1000" placeholder:"https://www.linkedin.com/company/…"`
	X         string `form:"input,text" label:"X"         maxLength:"1000" placeholder:"https://x.com/…"`
	Mastodon  string `form:"input,text" label:"Mastodon"  maxLength:"1000" placeholder:"https://mastodon.social/@…"`
	Bluesky   string `form:"input,text" label:"Bluesky"   maxLength:"1000" placeholder:"https://bsky.app/profile/…"`
	Threads   string `form:"input,text" label:"Threads"   maxLength:"1000" placeholder:"https://www.threads.net/@…"`
	YouTube   string `form:"input,text" label:"YouTube"   maxLength:"1000" placeholder:"https://www.youtube.com/@…"`
	TikTok    string `form:"input,text" label:"TikTok"    maxLength:"1000" placeholder:"https://www.tiktok.com/@…"`
	Pinterest string `form:"input,text" label:"Pinterest" maxLength:"1000" placeholder:"https://www.pinterest.com/…"`
	GitHub    string `form:"input,text" label:"GitHub"    maxLength:"1000" placeholder:"https://github.com/…"`
	WhatsApp  string `form:"input,text" label:"WhatsApp"  maxLength:"1000" placeholder:"https://wa.me/…"`
	Telegram  string `form:"input,text" label:"Telegram"  maxLength:"1000" placeholder:"https://t.me/…"`
}

// fields maps the networks to their field, with the field's name for errors.
func (in *profilesInput) fields() []profileField {
	return []profileField{
		{"facebook", "Facebook", &in.Facebook},
		{"instagram", "Instagram", &in.Instagram},
		{"linkedin", "LinkedIn", &in.LinkedIn},
		{"x", "X", &in.X},
		{"mastodon", "Mastodon", &in.Mastodon},
		{"bluesky", "Bluesky", &in.Bluesky},
		{"threads", "Threads", &in.Threads},
		{"youtube", "YouTube", &in.YouTube},
		{"tiktok", "TikTok", &in.TikTok},
		{"pinterest", "Pinterest", &in.Pinterest},
		{"github", "GitHub", &in.GitHub},
		{"whatsapp", "WhatsApp", &in.WhatsApp},
		{"telegram", "Telegram", &in.Telegram},
	}
}

type profileField struct {
	network string
	name    string
	value   *string
}

// View is the data of the Site settings screen: one form per group of
// settings, the translated texts in the language of the selected tab.
type View struct {
	Tabs           []admin.LocaleTab
	Settings       form.RenderModel
	SettingsErrors form.FieldErrors
	Text           form.RenderModel
	TextErrors     form.FieldErrors
	Profiles       form.RenderModel
	ProfilesErrors form.FieldErrors
	// Performance is the form of compression and page caching.
	Performance       form.RenderModel
	PerformanceErrors form.FieldErrors
	// Security is the form of which sites may frame the site's pages. Only
	// administrators may change it: SecurityReadOnly shows it to others
	// without its button.
	Security         form.RenderModel
	SecurityErrors   form.FieldErrors
	SecurityReadOnly bool
	// Review turns the review workflow on per section; nil when no section
	// can be reviewed. Only administrators may change it: ReviewReadOnly
	// shows it to others without its button.
	Review         *form.RenderModel
	ReviewErrors   form.FieldErrors
	ReviewReadOnly bool
	// Trash is the form of after how many days the trash deletes items
	// for good.
	Trash       form.RenderModel
	TrashErrors form.FieldErrors
	// ReadOnly is set for users who may view the settings but not edit
	// them: the forms are then read-only (disabled, without their buttons).
	ReadOnly bool
	// GeneralFields and TextFields limit the General and Texts cards to
	// the fields named, as the setup guide shows them; empty shows every
	// field.
	GeneralFields []string
	TextFields    []string
	// GeneralHeading replaces the General card's heading, a translation
	// key, such as "Logo" when the guide shows only the logo.
	GeneralHeading string
	// GuideURL continues the setup guide while it is not finished, and
	// Reopen opens it again once it is; both are for administrators only.
	GuideURL string
	Reopen   *form.RenderModel
}

// siteCards and themeCards hold the cards of the Site settings and Theme
// screens, which the setup guide shows as well.
const (
	siteCards  = "templates/site-cards.gohtml"
	themeCards = "templates/theme-cards.gohtml"
)

// state is the page as a save left it: the forms as posted, with their
// errors. Forms left nil show the stored settings.
type state struct {
	settings          *settingsInput
	settingsErrors    form.FieldErrors
	text              *textInput
	textErrors        form.FieldErrors
	profiles          *profilesInput
	profilesErrors    form.FieldErrors
	performance       *performanceInput
	performanceErrors form.FieldErrors
	security          *securityInput
	securityErrors    form.FieldErrors
	review            *reviewInput
	reviewErrors      form.FieldErrors
	trash             *trashInput
	trashErrors       form.FieldErrors
	message           string
	status            int
}

type section struct {
	admin    *admin.Admin
	settings *site.Service
	media    *media.Service
	fixed    Fixed
	// guide is the setup guide, which shows the cards too (see guide.go).
	guide *guide
}

// Fixed are the settings the server configuration decides instead of the
// site settings (see webthings.Config). The cards show the value that
// applies, with the control disabled and the note "Set by the server
// configuration.", and saving a card keeps the stored setting.
type Fixed struct {
	// ContentSecurityPolicy is the Content-Security-Policy the
	// configuration sets: site.CSPOff, site.CSPReportOnly or
	// site.CSPEnforce. Empty follows the setting.
	ContentSecurityPolicy string
	// OutputCache is set when the configuration decides how long the
	// output cache keeps pages: OutputCacheTTL, which is zero when the
	// configuration turns the cache off.
	OutputCache    bool
	OutputCacheTTL time.Duration
}

// fixedNote is the description of a control the server configuration
// decides.
const fixedNote = "Set by the server configuration."

// New returns the Site settings section of the admin, with the setup
// guide that leads administrators through a new site's settings (see
// guide.go).
func New(settings *site.Service, options Options) admin.Section {
	// The guide's callout needs the admin, which only the routes give.
	var callout *guide

	return admin.Section{
		ID:    "site",
		Title: "Site settings",
		Icon:  "settings",
		Files: files,
		Callout: func(ctx context.Context) (admin.Callout, bool) {
			if callout == nil {
				return admin.Callout{}, false
			}

			return callout.callout(ctx)
		},
		Routes: func(routes *admin.Routes) {
			g := newGuide(routes.Admin(), settings, options)
			callout = g
			s := g.site

			routes.Get("", admin.View, s.show).As("index")
			routes.Post("", admin.Edit, s.saveSettings)
			routes.Post("/text", admin.Edit, s.saveText).As("text")
			routes.Post("/profiles", admin.Edit, s.saveProfiles).As("profiles")
			routes.Post("/performance", admin.Edit, s.savePerformance).As("performance")
			routes.Post("/trash", admin.Edit, s.saveTrash).As("trash")
			// Only administrators decide how pages protect visitors and
			// who may publish without review.
			routes.Post("/security", admin.Edit, s.saveSecurity).As("security").Use(admin.AdministratorsOnly)
			routes.Post("/review", admin.Edit, s.saveReview).As("review").Use(admin.AdministratorsOnly)
			g.routes(routes)
		},
	}
}

func (s section) show(w http.ResponseWriter, r *http.Request) {
	current, err := s.settings.Settings(r.Context())
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.render(w, r, current, state{status: http.StatusOK})
}

func (s section) locale(r *http.Request, current site.Settings) string {
	return admin.ContentLocale(r, current.LocaleConfig())
}

// render shows the Site settings screen as page left it, or the setup
// guide's step when the request is one of the guide's (see guideStep).
func (s section) render(w http.ResponseWriter, r *http.Request, current site.Settings, page state) {
	view, err := s.view(r, current, page)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	// The message shows as a toast; errors of single fields stay inline.
	r = admin.WithError(r, page.message)

	if step := s.guide.step(r); step != "" {
		s.guide.render(w, r, step, guideParts{site: &view, status: page.status})

		return
	}

	if user, _ := auth.CurrentUser(r.Context()); user.IsAdmin() {
		s.guide.links(r, current, &view)
	}

	s.admin.Render(w, r, admin.Page{
		Title:    "Site settings",
		Template: "templates/site.gohtml",
		Includes: []string{siteCards},
		Files:    files,
		Data:     view,
		Status:   page.status,
	})
}

// view is the Site settings screen as page left it. In the setup guide
// the forms post back to the guide's step.
func (s section) view(r *http.Request, current site.Settings, page state) (View, error) {
	locale := s.locale(r, current)
	settings := page.settings
	text := page.text
	profiles := page.profiles
	performance := page.performance
	security := page.security
	trash := page.trash

	if settings == nil {
		filled, err := s.settingsForm(r, current)
		if err != nil {
			return View{}, err
		}

		settings = &filled
	}

	if text == nil {
		translation := current.Text(locale)
		text = &textInput{
			Title:          translation.Title,
			Tagline:        translation.Tagline,
			SEOTitle:       translation.SEOTitle,
			SEODescription: translation.SEODescription,
			Footer:         translation.Footer,
		}
	}

	if profiles == nil {
		filled := profilesForm(current)
		profiles = &filled
	}

	if performance == nil {
		filled := performanceForm(current)
		performance = &filled
	}

	if security == nil {
		filled := securityForm(current)
		security = &filled
	}

	if trash == nil {
		trash = &trashInput{Days: current.TrashDays}
	}

	// What the server configuration decides shows as it applies, also
	// after a save the card refused.
	if s.fixed.ContentSecurityPolicy != "" {
		security.ContentSecurity = s.fixed.ContentSecurityPolicy
	}

	if s.fixed.OutputCache {
		performance.OutputCache = int(s.fixed.OutputCacheTTL.Seconds())
	}

	query := url.Values{"locale": {locale}}
	if step := s.guide.step(r); step != "" {
		query.Set(guideParam, step)
	}

	view := View{
		Settings:          s.admin.Form(r, *settings, s.admin.URL("admin.site.index", query), "Save"),
		SettingsErrors:    page.settingsErrors,
		Text:              s.admin.Form(r, *text, s.admin.URL("admin.site.text", query), "Save"),
		TextErrors:        page.textErrors,
		Profiles:          s.admin.Form(r, *profiles, s.admin.URL("admin.site.profiles", query), "Save"),
		ProfilesErrors:    page.profilesErrors,
		Performance:       s.admin.Form(r, *performance, s.admin.URL("admin.site.performance", query), "Save"),
		PerformanceErrors: page.performanceErrors,
		Security:          s.admin.Form(r, *security, s.admin.URL("admin.site.security", query), "Save"),
		SecurityErrors:    page.securityErrors,
		Trash:             s.admin.Form(r, *trash, s.admin.URL("admin.site.trash", query), "Save"),
		TrashErrors:       page.trashErrors,
		ReadOnly:          !s.admin.Can(r, admin.Permission{Section: "site", Action: admin.Edit}),
	}

	view.Settings.Info.ReadOnly = view.ReadOnly
	view.Text.Info.ReadOnly = view.ReadOnly
	view.Profiles.Info.ReadOnly = view.ReadOnly
	view.Performance.Info.ReadOnly = view.ReadOnly
	view.Performance.Overrides = s.fixed.performanceOverrides()
	view.Trash.Info.ReadOnly = view.ReadOnly
	user, _ := auth.CurrentUser(r.Context())
	view.SecurityReadOnly = view.ReadOnly || !user.IsAdmin()
	view.Security.Info.ReadOnly = view.SecurityReadOnly
	view.Security.Overrides = s.fixed.securityOverrides()

	if review := s.reviewForm(r, current, page.review); review != nil {
		model := s.admin.Form(r, *review, s.admin.URL("admin.site.review", query), "Save")
		view.Review = &model
		view.ReviewErrors = page.reviewErrors
		view.ReviewReadOnly = view.ReadOnly || !user.IsAdmin()
		view.Review.Info.ReadOnly = view.ReviewReadOnly
	}

	tabs := s.admin.URL("admin.site.index")
	if step := s.guide.step(r); step != "" {
		tabs = s.admin.URL("admin.site.guide-step", "step", step)
	}

	view.Tabs = admin.LocaleTabs(current.Locales, locale, tabs, func(code string) (bool, bool) {
		return current.Text(code).Title != "", false
	})

	return view, nil
}

func (s section) settingsForm(r *http.Request, current site.Settings) (settingsInput, error) {
	assets, err := s.media.List(r.Context())
	if err != nil {
		return settingsInput{}, err
	}

	images := map[string]string{}

	for _, asset := range assets {
		if asset.IsImage() {
			images[asset.ID] = asset.Filename
		}
	}

	ctx := r.Context()
	listed := map[string]string{}

	for _, code := range append(slices.Clone(knownLanguages), current.Locales...) {
		listed[code] = languageLabel(ctx, code)
	}

	// Images are listed by name, as editor.MediaChoices does.
	in := settingsInput{
		DefaultLocale: form.NewSortedSelect(listed).Selected(current.DefaultLocale),
		Locales:       form.NewSortedMultiSelect(listed).Selected(current.Locales...),
		Timezone:      admin.TimezoneChoices(current.Timezone),
		Logo:          form.NewSortedSelect(images).SortByLabel().Selected(current.LogoMediaID),
		Favicon:       form.NewSortedSelect(images).SortByLabel().Selected(current.FaviconMediaID),
		SocialImage:   form.NewSortedSelect(images).SortByLabel().Selected(current.SocialImageMediaID),
		Organisation:  current.Organisation,
		Email:         current.Email,
		Phone:         current.Phone,
		Address:       current.Address,
		FeedSize:      current.FeedLimit(),
	}

	return in, nil
}

func (s section) saveSettings(w http.ResponseWriter, r *http.Request) {
	current, err := s.settings.Settings(r.Context())
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	in, err := s.settingsForm(r, current)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	errs, err := s.admin.Bind(r, &in)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	enabled := enabledLocales(current.Locales, in.Locales.Get(), splitList(in.OtherLocales))

	// The guide's steps show some of the card's fields (generalFields).
	// Those a step leaves out are not posted and keep the stored values
	// the form was filled with, except the languages: a post without
	// their boxes clears them, so the Theme step, which shows only the
	// logo, leaves them as they are.
	languages := s.guide.step(r) != stepTheme

	// The default language is required by its tag; it must also be one of
	// the languages ticked in the same save.
	if languages && in.DefaultLocale.Get() != "" && !slices.Contains(enabled, in.DefaultLocale.Get()) {
		errs = append(errs, form.FieldValidationError{
			Field: "DefaultLocale",
			Err:   s.admin.T(r, "Tick the default language under Languages as well."),
		})
	}

	if len(errs) > 0 {
		s.render(w, r, current, state{settings: &in, settingsErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	updated := current
	updated.DefaultLocale = in.DefaultLocale.Get()
	updated.Timezone = in.Timezone.Get()
	updated.LogoMediaID = in.Logo.Get()
	updated.FaviconMediaID = in.Favicon.Get()
	updated.SocialImageMediaID = in.SocialImage.Get()
	updated.Organisation = in.Organisation
	updated.Email = in.Email
	updated.Phone = in.Phone
	updated.Address = in.Address
	updated.FeedSize = in.FeedSize

	if languages {
		updated.Locales = enabled
	}

	if err := s.settings.Save(r.Context(), updated); err != nil {
		s.invalid(w, r, current, state{settings: &in}, err)

		return
	}

	// In the guide's Site step the texts follow the languages.
	query := url.Values{"locale": {s.locale(r, updated)}}
	s.guide.done(w, r, s.admin.URL("admin.site.index", query), s.guide.stay(r, stepSite, query))
}

func (s section) saveText(w http.ResponseWriter, r *http.Request) {
	current, err := s.settings.Settings(r.Context())
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	// The fields a form leaves out, as the guide's does (TextFields), keep
	// their stored text.
	locale := s.locale(r, current)
	translation := current.Text(locale)
	in := textInput{
		Title:          translation.Title,
		Tagline:        translation.Tagline,
		SEOTitle:       translation.SEOTitle,
		SEODescription: translation.SEODescription,
		Footer:         translation.Footer,
	}

	errs, err := s.admin.Bind(r, &in)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		s.render(w, r, current, state{text: &in, textErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	current.Translations = current.WithText(site.Translation{
		Locale:         locale,
		Title:          in.Title,
		Tagline:        in.Tagline,
		SEOTitle:       in.SEOTitle,
		SEODescription: in.SEODescription,
		Footer:         in.Footer,
	})

	if err := s.settings.Save(r.Context(), current); err != nil {
		s.invalid(w, r, current, state{text: &in}, err)

		return
	}

	// In the guide's Site step the texts of the next language follow; the
	// last language's finish the step.
	var more string

	if at := slices.Index(current.Locales, locale); at >= 0 && at+1 < len(current.Locales) {
		more = s.guide.stay(r, stepSite, url.Values{"locale": {current.Locales[at+1]}})
	}

	s.guide.done(w, r, s.admin.URL("admin.site.index", url.Values{"locale": {locale}}), more)
}

func (s section) invalid(w http.ResponseWriter, r *http.Request, current site.Settings, page state, err error) {
	var message string

	switch {
	case errors.Is(err, site.ErrInvalid):
		message = "Check the languages and timezone: the default language must be enabled and the timezone must exist."
	case errors.Is(err, site.ErrReservedLocale):
		message = "A language cannot have the code that one of the website's own addresses starts with, such as the admin's: its pages could not be reached."
	case errors.Is(err, media.ErrNotFound):
		message = "The selected image no longer exists."
	case errors.Is(err, site.ErrProfile):
		message = "Enter a web address, such as https://example.com/name."
	default:
		s.admin.Fail(w, r, err)

		return
	}

	page.message = s.admin.T(r, message)
	page.status = http.StatusUnprocessableEntity
	s.render(w, r, current, page)
}

// profilesForm fills the profile fields from the settings.
func profilesForm(current site.Settings) profilesInput {
	var in profilesInput

	for _, field := range in.fields() {
		*field.value = current.Profile(field.network)
	}

	return in
}

// saveProfiles stores the profile addresses; each must be a web address.
func (s section) saveProfiles(w http.ResponseWriter, r *http.Request) {
	current, err := s.settings.Settings(r.Context())
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	var in profilesInput

	errs, err := s.admin.Bind(r, &in)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	var profiles []site.Profile

	for _, field := range in.fields() {
		if strings.TrimSpace(*field.value) == "" {
			continue
		}

		address, err := site.CleanProfileURL(*field.value)
		if err != nil {
			errs = append(errs, form.FieldValidationError{
				Field: field.name,
				Err:   s.admin.T(r, "Enter a web address, such as https://example.com/name."),
			})

			continue
		}

		profiles = append(profiles, site.Profile{Network: field.network, URL: address})
	}

	if len(errs) > 0 {
		s.render(w, r, current, state{profiles: &in, profilesErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	current.Profiles = profiles

	if err := s.settings.Save(r.Context(), current); err != nil {
		s.invalid(w, r, current, state{profiles: &in}, err)

		return
	}

	s.admin.Redirect(w, r, s.admin.URL("admin.site.index", url.Values{"locale": {s.locale(r, current)}}))
}

// enabledLocales are the languages ticked and written in other, in the
// order of before (the order the site had), followed by new ones in the
// order of the list.
func enabledLocales(before, ticked, other []string) []string {
	// Written codes are normalized, so "PT-br" is the listed "pt-BR"; codes
	// that are no language stay as written for site.Service.Save to refuse.
	for i, code := range other {
		if normalized, err := locales.Normalize(code); err == nil {
			other[i] = normalized
		}
	}

	chosen := map[string]bool{}

	for _, code := range append(slices.Clone(ticked), other...) {
		chosen[code] = true
	}

	var out []string

	for _, code := range before {
		if chosen[code] {
			out = append(out, code)
			delete(chosen, code)
		}
	}

	for _, code := range append(slices.Clone(ticked), other...) {
		if chosen[code] {
			out = append(out, code)
			delete(chosen, code)
		}
	}

	return out
}

// languageLabel is a language's own name with its code, such as
// "Nederlands (nl)".
func languageLabel(_ context.Context, code string) string {
	return admin.LanguageLabel(code)
}

func splitList(value string) []string {
	var out []string

	for part := range strings.SplitSeq(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}

	return out
}
