package siteadmin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/pages"
	"github.com/donseba/go-webthings/pages/pageadmin"
	"github.com/donseba/go-webthings/site"

	form "github.com/donseba/go-form/v2"
)

// The setup guide leads administrators through what a new site needs, one
// step at a time: its languages, timezone, title and tagline (Site), its
// look (Theme), e-mail (Mail), pages for its modules (Pages) and the
// protection of its pages (Security). It is a frame around the cards of
// the Site settings and Theme screens, not a second set of settings: each
// step shows the same cards, which post to their own handlers with
// ?guide=<step>. Those handlers render a refusal inside the guide, with the
// same status and messages, and go on to the next open step after a save
// (guide.done).
//
// Which steps are finished or skipped is one setting, site.Settings.Setup.
// Where the data says so, a step counts as finished without it: a theme
// chosen, every module on a page, a Content-Security-Policy chosen. Only
// administrators see the guide, and its card on the dashboard
// (admin.Section.Callout) until they finish it.

// Options are what the Site settings, Theme and Backup sections need
// besides the settings themselves.
type Options struct {
	// Media offers the images, such as the logo, that the settings can
	// use; Backup downloads its files.
	Media *media.Service
	// DB is the site's database, which Backup copies on SQLite and whose
	// schema versions its manifest names.
	DB *db.DB
	// Fixed are the settings the server configuration decides.
	Fixed Fixed
	// Frontend has the themes, with their pictures; Public returns the
	// public site handler, for previews.
	Frontend *frontend.Frontend
	Public   func() http.Handler
	// Pages is the page tree, on which the guide's Pages step places the
	// Modules: the module instances that can be on a page.
	Pages   *pages.Service
	Modules func() []pageadmin.Module
	// Mail is the site's sender, which the Mail step describes
	// (mail.Describer) and tests, and BaseURL the site's public address
	// (webthings.Config.BaseURL).
	Mail    mail.Sender
	BaseURL string
}

// The steps of the guide, by ID. Welcome and Done frame the steps that
// count: Site to Security.
const (
	stepWelcome  = "welcome"
	stepSite     = "site"
	stepTheme    = "theme"
	stepMail     = "mail"
	stepPages    = "pages"
	stepSecurity = "security"
	stepDone     = "done"
)

const (
	// guideParam is the query parameter by which a card's post belongs to
	// the guide's step, such as ?guide=site.
	guideParam = "guide"
	// skippedMark marks a skipped step in site.Settings.Setup, before its
	// ID; a finished step is its ID alone, and stepDone the finished guide.
	skippedMark = "skip:"
)

// stepTitles are the steps' names in the guide's rail.
var stepTitles = map[string]string{
	stepSite:     "Site",
	stepTheme:    "Theme",
	stepMail:     "E-mail",
	stepPages:    "Pages",
	stepSecurity: "Security",
	stepDone:     "Finish",
}

// guide is the setup guide of a site; see the comment at the top.
type guide struct {
	admin    *admin.Admin
	settings *site.Service
	options  Options
	// site and theme show the cards of the Site settings and Theme
	// screens.
	site  section
	theme themeSection
	// tests holds when each administrator last sent a test e-mail (see
	// testMail).
	mu    sync.Mutex
	tests map[string]time.Time
}

// newGuide returns the guide, with the Site settings and Theme sections
// that show its cards.
func newGuide(backoffice *admin.Admin, settings *site.Service, options Options) *guide {
	g := &guide{admin: backoffice, settings: settings, options: options, tests: map[string]time.Time{}}
	g.site = section{admin: backoffice, settings: settings, media: options.Media, fixed: options.Fixed, guide: g}
	g.theme = themeSection{
		admin:    backoffice,
		settings: settings,
		frontend: options.Frontend,
		themes:   options.Frontend.Themes(),
		public:   options.Public,
		guide:    g,
	}

	return g
}

// routes registers the guide's routes in the Site settings section; they
// are for administrators only.
func (g *guide) routes(routes *admin.Routes) {
	routes.Get("/guide", admin.View, g.resume).As("guide").Use(admin.AdministratorsOnly)
	routes.Get("/guide/{step}", admin.View, g.show).As("guide-step").Use(admin.AdministratorsOnly)
	routes.Post("/guide/{step}/skip", admin.Edit, g.skip).As("guide-skip").Use(admin.AdministratorsOnly)
	routes.Post("/guide/pages", admin.Edit, g.createPages).As("guide-pages").Use(admin.AdministratorsOnly)
	routes.Post("/guide/mail", admin.Edit, g.testMail).As("guide-mail").Use(admin.AdministratorsOnly)
	routes.Post("/guide/finish", admin.Edit, g.finish).As("guide-finish").Use(admin.AdministratorsOnly)
	routes.Post("/guide/reopen", admin.Edit, g.reopen).As("guide-reopen").Use(admin.AdministratorsOnly)
}

// steps are the IDs of the steps that count, in their order; Theme only
// when the site has themes to choose from.
func (g *guide) steps() []string {
	steps := []string{stepSite}

	if len(g.options.Frontend.Themes().List()) > 0 {
		steps = append(steps, stepTheme)
	}

	return append(steps, stepMail, stepPages, stepSecurity)
}

type guideStepKey struct{}

// withStep returns r for rendering step of the guide.
func withStep(r *http.Request, step string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), guideStepKey{}, step))
}

// step is the guide's step r belongs to: the step the guide renders, or a
// card's post with ?guide=site, theme or security. It is "" for other
// requests, and for users other than administrators, who never see the
// guide.
func (g *guide) step(r *http.Request) string {
	if user, _ := auth.CurrentUser(r.Context()); !user.IsAdmin() {
		return ""
	}

	if step, ok := r.Context().Value(guideStepKey{}).(string); ok {
		return step
	}

	switch step := r.URL.Query().Get(guideParam); step {
	case stepSite, stepTheme, stepSecurity:
		return step
	default:
		return ""
	}
}

// progress is how far the guide got.
type progress struct {
	finished map[string]bool
	skipped  map[string]bool
	// done is set once an administrator finished the guide.
	done bool
	// started is set once any step was finished or skipped.
	started bool
}

// progress reads site.Settings.Setup and adds the steps the data shows as
// finished: a theme chosen, every module on a page, a policy chosen.
func (g *guide) progress(ctx context.Context, current site.Settings) progress {
	p := progress{finished: map[string]bool{}, skipped: map[string]bool{}}

	for _, entry := range current.Setup {
		p.started = true

		switch skipped, ok := cutSkipped(entry); {
		case entry == stepDone:
			p.done = true
		case ok:
			p.skipped[skipped] = true
		default:
			p.finished[entry] = true
		}
	}

	if current.Theme != "" {
		p.finished[stepTheme] = true
	}

	if current.ContentSecurity != "" {
		p.finished[stepSecurity] = true
	}

	if g.allMounted(ctx, current) {
		p.finished[stepPages] = true
	}

	return p
}

// cutSkipped returns the step of a skipped entry of site.Settings.Setup.
func cutSkipped(entry string) (string, bool) {
	if len(entry) > len(skippedMark) && entry[:len(skippedMark)] == skippedMark {
		return entry[len(skippedMark):], true
	}

	return "", false
}

// handled reports whether step is finished or skipped.
func (p progress) handled(step string) bool {
	return p.finished[step] || p.skipped[step]
}

// count is how many of steps are finished or skipped.
func (p progress) count(steps []string) int {
	count := 0

	for _, step := range steps {
		if p.handled(step) {
			count++
		}
	}

	return count
}

// next is the first step after from that is neither finished nor skipped,
// then the first before it; stepDone when every step is.
func (p progress) next(steps []string, from string) string {
	at := slices.Index(steps, from)
	ordered := append(slices.Clone(steps[at+1:]), steps[:at+1]...)

	for _, step := range ordered {
		if !p.handled(step) {
			return step
		}
	}

	return stepDone
}

// allMounted reports whether every module instance that can be on a page
// is on one, in the default language; false without such modules.
func (g *guide) allMounted(ctx context.Context, current site.Settings) bool {
	if g.options.Pages == nil || g.options.Modules == nil {
		return false
	}

	modules := g.options.Modules()
	if len(modules) == 0 {
		return false
	}

	for _, module := range modules {
		if _, err := g.options.Pages.MountPath(ctx, module.Key, current.DefaultLocale); err != nil {
			return false
		}
	}

	return true
}

// mark changes site.Settings.Setup with change and saves it.
func (g *guide) mark(ctx context.Context, change func(entries []string) []string) error {
	current, err := g.settings.Settings(ctx)
	if err != nil {
		return err
	}

	current.Setup = change(current.Setup)

	return g.settings.Save(ctx, current)
}

// finishStep marks step finished, also when it was skipped before.
func (g *guide) finishStep(ctx context.Context, step string) error {
	return g.mark(ctx, func(entries []string) []string {
		entries = slices.DeleteFunc(entries, func(entry string) bool {
			return entry == skippedMark+step
		})

		if !slices.Contains(entries, step) {
			entries = append(entries, step)
		}

		return entries
	})
}

// callout is the guide's card on the dashboard, for administrators until
// they finish the guide: "Finish setting up your site", with the steps
// done.
func (g *guide) callout(ctx context.Context) (admin.Callout, bool) {
	if user, _ := auth.CurrentUser(ctx); !user.IsAdmin() {
		return admin.Callout{}, false
	}

	current, err := g.settings.Settings(ctx)
	if err != nil {
		return admin.Callout{}, false
	}

	p := g.progress(ctx, current)
	if p.done {
		return admin.Callout{}, false
	}

	steps := g.steps()
	done := p.count(steps)

	// Every step is done or skipped: only finishing is left.
	if done == len(steps) {
		return admin.Callout{
			Title:  admin.T(ctx, "Everything is ready — finish setting up"),
			Text:   admin.T(ctx, "All steps done"),
			URL:    g.admin.URL("admin.site.guide-step", "step", stepDone),
			Action: "Finish",
			Done:   done,
			Total:  len(steps),
		}, true
	}

	return admin.Callout{
		Title:  admin.T(ctx, "Finish setting up your site"),
		Text:   admin.T(ctx, "%d of %d steps done", done, len(steps)),
		URL:    g.admin.URL("admin.site.guide"),
		Action: "Continue setup",
		Done:   done,
		Total:  len(steps),
	}, true
}

// links adds the guide to the Site settings screen of an administrator:
// "Continue setup" while it is not finished, a button to open it again
// once it is.
func (g *guide) links(r *http.Request, current site.Settings, view *View) {
	if !slices.Contains(current.Setup, stepDone) {
		view.GuideURL = g.admin.URL("admin.site.guide")

		return
	}

	reopen := g.admin.Form(r, struct{}{}, g.admin.URL("admin.site.guide-reopen"), "Open the setup guide")
	view.Reopen = &reopen
}

// resume opens the guide where it was left: the welcome before any step
// was finished or skipped, then the first open step, and the last page
// once every step is.
func (g *guide) resume(w http.ResponseWriter, r *http.Request) {
	current, err := g.settings.Settings(r.Context())
	if err != nil {
		g.admin.Fail(w, r, err)

		return
	}

	p := g.progress(r.Context(), current)
	step := stepWelcome

	if p.started {
		steps := g.steps()
		step = p.next(steps, steps[len(steps)-1])
	}

	g.admin.Redirect(w, r, g.admin.URL("admin.site.guide-step", "step", step))
}

// show renders one step of the guide.
func (g *guide) show(w http.ResponseWriter, r *http.Request) {
	step := r.PathValue("step")
	if step != stepWelcome && step != stepDone && !slices.Contains(g.steps(), step) {
		http.NotFound(w, r)

		return
	}

	r = withStep(r, step)

	switch step {
	case stepTheme:
		// The Theme step shows the settings of the theme its links chose.
		g.theme.render(w, r, r.URL.Query().Get("theme"), nil, nil, http.StatusOK)
	default:
		g.render(w, r, step, guideParts{status: http.StatusOK})
	}
}

// skip marks a step skipped, unless it is finished, and goes on to the
// next open step.
func (g *guide) skip(w http.ResponseWriter, r *http.Request) {
	step := r.PathValue("step")
	if !slices.Contains(g.steps(), step) {
		http.NotFound(w, r)

		return
	}

	err := g.mark(r.Context(), func(entries []string) []string {
		if slices.Contains(entries, step) || slices.Contains(entries, skippedMark+step) {
			return entries
		}

		return append(entries, skippedMark+step)
	})
	if err != nil {
		g.admin.Fail(w, r, err)

		return
	}

	g.redirectNext(w, r, step, "")
}

// finish marks the guide finished: the dashboard no longer shows its card.
func (g *guide) finish(w http.ResponseWriter, r *http.Request) {
	err := g.mark(r.Context(), func(entries []string) []string {
		if slices.Contains(entries, stepDone) {
			return entries
		}

		return append(entries, stepDone)
	})
	if err != nil {
		g.admin.Fail(w, r, err)

		return
	}

	g.admin.RedirectNotice(w, r, g.admin.URL("admin.dashboard"), "Setup finished. Site settings opens the guide again.")
}

// reopen opens a finished guide again, with its card on the dashboard; the
// steps keep what was finished or skipped.
func (g *guide) reopen(w http.ResponseWriter, r *http.Request) {
	err := g.mark(r.Context(), func(entries []string) []string {
		return slices.DeleteFunc(entries, func(entry string) bool {
			return entry == stepDone
		})
	})
	if err != nil {
		g.admin.Fail(w, r, err)

		return
	}

	g.admin.RedirectNotice(w, r, g.admin.URL("admin.site.guide"), "")
}

// stay is the address of step for a card after which the step goes on,
// such as the languages before the texts, when r is in that step; ""
// otherwise.
func (g *guide) stay(r *http.Request, step string, query url.Values) string {
	if g.step(r) != step {
		return ""
	}

	return g.admin.URL("admin.site.guide-step", "step", step, query)
}

// done ends a successful save of a card. Outside the guide it redirects
// to fallback. In the guide it redirects to more, the step itself for a
// card after which the step goes on (see stay), or, when more is empty,
// finishes the step and goes on to the next open one.
func (g *guide) done(w http.ResponseWriter, r *http.Request, fallback, more string) {
	step := g.step(r)

	switch {
	case step == "":
		g.admin.Redirect(w, r, fallback)
	case more != "":
		g.admin.Redirect(w, r, more)
	default:
		if err := g.finishStep(r.Context(), step); err != nil {
			g.admin.Fail(w, r, err)

			return
		}

		g.redirectNext(w, r, step, "Changes saved.")
	}
}

// redirectNext goes on to the next open step after step, with notice as
// the toast.
func (g *guide) redirectNext(w http.ResponseWriter, r *http.Request, step, notice string) {
	current, err := g.settings.Settings(r.Context())
	if err != nil {
		g.admin.Fail(w, r, err)

		return
	}

	next := g.progress(r.Context(), current).next(g.steps(), step)
	g.admin.RedirectNotice(w, r, g.admin.URL("admin.site.guide-step", "step", next), notice)
}

// GuideView is a page of the setup guide: the rail of its steps and the
// cards of the current one.
type GuideView struct {
	// Step is the current step's ID: welcome, site, theme, mail, pages,
	// security or done.
	Step  string
	Steps []GuideStep
	// Number is the current step's place among the steps that count, from
	// 1, and Total their number; Handled how many are finished or skipped.
	Number  int
	Total   int
	Handled int
	// Site has the cards of the Site, Theme (the logo) and Security steps,
	// Theme those of the Theme step.
	Site  *View
	Theme *ThemeView
	// Mail and Pages are the Mail and Pages steps.
	Mail  *MailView
	Pages *PagesView
	// Skip is the "Skip for now" button of a step that is neither
	// finished nor skipped; Next leads on to the next step once it is.
	Skip *form.RenderModel
	Next string
	// UploadURL leads to the media library, for a logo, while it has no
	// images.
	UploadURL string
	// Start begins the guide on the welcome page; Dashboard leaves it.
	Start     string
	Dashboard string
	// Website, NewPage and Accounts are the last page's next steps, and
	// Finish its button.
	Website  string
	NewPage  string
	Accounts string
	Finish   *form.RenderModel
}

// GuideStep is a step in the guide's rail.
type GuideStep struct {
	ID string
	// Title is a translation key.
	Title   string
	URL     string
	Number  int
	Current bool
	Done    bool
	Skipped bool
}

// guideParts are the parts of a step a handler has already made, such as
// a card refused with its errors; render makes the others.
type guideParts struct {
	site   *View
	theme  *ThemeView
	mail   *MailView
	pages  *PagesView
	status int
}

// generalFields are the General card's fields the guide's steps show.
var generalFields = map[string][]string{
	stepSite:  {"DefaultLocale", "Locales", "OtherLocales", "Timezone"},
	stepTheme: {"Logo"},
}

// render shows step of the guide with parts.
func (g *guide) render(w http.ResponseWriter, r *http.Request, step string, parts guideParts) {
	ctx := r.Context()
	r = withStep(r, step)

	current, err := g.settings.Settings(ctx)
	if err != nil {
		g.admin.Fail(w, r, err)

		return
	}

	p := g.progress(ctx, current)
	steps := g.steps()
	view := GuideView{
		Step:      step,
		Total:     len(steps),
		Handled:   p.count(steps),
		Site:      parts.site,
		Theme:     parts.theme,
		Mail:      parts.mail,
		Pages:     parts.pages,
		Dashboard: g.admin.URL("admin.dashboard"),
	}

	for i, id := range append(slices.Clone(steps), stepDone) {
		entry := GuideStep{
			ID:      id,
			Title:   stepTitles[id],
			URL:     g.admin.URL("admin.site.guide-step", "step", id),
			Current: id == step,
			Done:    p.finished[id] || id == stepDone && p.done,
			Skipped: p.skipped[id] && !p.finished[id],
		}

		if id != stepDone {
			entry.Number = i + 1
		}

		if entry.Current {
			view.Number = entry.Number
		}

		view.Steps = append(view.Steps, entry)
	}

	if err := g.fill(r, step, current, p, &view); err != nil {
		g.admin.Fail(w, r, err)

		return
	}

	g.admin.Render(w, r, admin.Page{
		Title:    "Getting started",
		Template: "templates/guide.gohtml",
		Includes: []string{siteCards, themeCards},
		Files:    files,
		Data:     view,
		Status:   parts.status,
	})
}

// fill makes the parts of step that view still misses, and its buttons.
func (g *guide) fill(r *http.Request, step string, current site.Settings, p progress, view *GuideView) error {
	steps := g.steps()

	switch step {
	case stepWelcome:
		view.Start = g.admin.URL("admin.site.guide-step", "step", p.next(steps, steps[len(steps)-1]))

		return nil
	case stepDone:
		view.Website = "/"
		view.NewPage, _ = g.admin.TryURL("admin.pages.new")
		view.Accounts, _ = g.admin.TryURL("admin.accounts.index")
		finish := g.admin.Form(r, struct{}{}, g.admin.URL("admin.site.guide-finish"), "Finish setup")
		view.Finish = &finish

		return nil
	}

	if !p.handled(step) {
		skip := g.admin.Form(r, struct{}{}, g.admin.URL("admin.site.guide-skip", "step", step), "Skip for now")
		view.Skip = &skip
	} else {
		view.Next = g.admin.URL("admin.site.guide-step", "step", p.next(steps, step))
	}

	switch step {
	case stepSite, stepSecurity:
		return g.fillSite(r, step, current, view)
	case stepTheme:
		if view.Theme == nil {
			theme, err := g.theme.view(r, r.URL.Query().Get("theme"), nil, nil)
			if err != nil && !errors.Is(err, errNoTheme) {
				return err
			}

			view.Theme = &theme
		}

		return g.fillSite(r, step, current, view)
	case stepMail:
		if view.Mail == nil {
			view.Mail = g.mailView(r)
		}
	case stepPages:
		if view.Pages == nil {
			pagesView, err := g.pagesView(r, current, nil, nil)
			if err != nil {
				return err
			}

			view.Pages = pagesView
		}
	}

	return nil
}

// fillSite makes the cards of the Site settings a step shows, limited to
// the step's fields, with the guide's buttons.
func (g *guide) fillSite(r *http.Request, step string, current site.Settings, view *GuideView) error {
	if view.Site == nil {
		site, err := g.site.view(r, current, state{})
		if err != nil {
			return err
		}

		view.Site = &site
	}

	view.Site.GeneralFields = generalFields[step]

	switch step {
	case stepSite:
		view.Site.TextFields = []string{"Title", "Tagline"}
		view.Site.Settings.Info.SubmitText = "Save languages"
		view.Site.Text.Info.SubmitText = "Save and continue"
	case stepTheme:
		view.Site.GeneralHeading = "Logo"
		view.Site.Settings.Info.SubmitText = "Save and continue"

		if view.Theme != nil && view.Theme.Form != nil {
			view.Theme.Form.Info.SubmitText = "Save colours and settings"
		}

		if images, err := g.hasImages(r.Context()); err != nil {
			return err
		} else if !images {
			view.UploadURL, _ = g.admin.TryURL("admin.media.index")
		}
	case stepSecurity:
		view.Site.Security.Info.SubmitText = "Save and continue"
	}

	return nil
}

// hasImages reports whether the media library has an image, for a logo.
func (g *guide) hasImages(ctx context.Context) (bool, error) {
	if g.options.Media == nil {
		return false, nil
	}

	assets, err := g.options.Media.List(ctx)
	if err != nil {
		return false, err
	}

	return slices.ContainsFunc(assets, media.Asset.IsImage), nil
}
