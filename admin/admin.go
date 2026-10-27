package admin

import (
	"cmp"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/donseba/go-webthings/admin/presence"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/render"

	form "github.com/donseba/go-form/v2"
	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/clients"
	"github.com/donseba/go-partial/connector"
	"github.com/donseba/go-partial/exp/flash"
	"github.com/donseba/go-partial/exp/interactions"
	"github.com/donseba/go-partial/exp/localization"
	"github.com/donseba/go-partial/exp/sse"
	"github.com/donseba/go-router"
	"github.com/donseba/go-router/middleware"
	translator "github.com/donseba/go-translator/v2"
)

//go:embed templates assets translations
var files embed.FS

const defaultBodyLimit = 512 << 10

// The patterns of the admin's own pages next to its sections, below its
// path (Options.Path); see also password.go. Code links to them by their
// route names, such as "admin.dashboard" and "admin.login" (see URL), never
// by these patterns or by path.
const (
	dashboardPath = "/"
	loginPath     = "/login"
	logoutPath    = "/logout"
	assetsPath    = "/assets/"
)

// DefaultPath is the admin's path when Options.Path is empty.
const DefaultPath = "/admin"

// pathPattern is an admin path: one or more segments of letters, digits
// and - . _ ~, without a trailing slash (see validPath).
var pathPattern = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)

// validPath reports whether path is an admin path: pathPattern without the
// segments "." and "..", which browsers and Go's ServeMux resolve away, so
// the admin could never be reached at such a path.
func validPath(path string) bool {
	if !pathPattern.MatchString(path) {
		return false
	}

	for segment := range strings.SplitSeq(strings.TrimPrefix(path, "/"), "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}

	return true
}

// Options are the settings of the admin that the application decides,
// such as where it is served and how it sends mail. webthings.App fills
// them from its Config.
type Options struct {
	// Path is where the admin is served, such as "/admin" (DefaultPath,
	// when empty) or "/toolbox": every address of the admin is below it.
	// Nothing in the admin, its sections or modules assumes it: they link
	// to named routes (see URL and RouteName).
	Path string
	// Title is shown in the header and browser tab.
	Title string
	// Language is the admin UI language, for example "en" or "nl".
	Language string
	// CatalogueDir is an optional folder of catalogues, <locale>.po such as
	// nl_NL.po, whose translations win over the built-in ones of the admin
	// and its sections. Nothing is written to it.
	CatalogueDir string
	// Secure marks cookies HTTPS-only. Disable only for local plain HTTP.
	Secure bool
	// Authorize replaces the built-in rules, by which administrators may do
	// everything and editors what their groups' matrix allows (see
	// auth.Group). It decides every permission check, for administrators
	// too. Sections that are AdminOnly, such as Accounts and Groups, always
	// need the administrator role as well, and Authorize is asked about them
	// too, so it can keep them from administrators but never open them to
	// editors. Nil applies the built-in rules.
	//
	// Authorize must be safe for concurrent use: requests ask it at the same
	// time, and so do background work such as finding the reviewers to
	// notify of a submitted revision.
	Authorize func(ctx context.Context, user auth.User, permission Permission) bool
	// Mail sends password reset links. Nil offers no password reset.
	Mail mail.Sender
	// SetupToken guards the first-run setup ("admin.setup"), which creates
	// the first administrator while no account exists: the screen opens
	// only with ?token=<SetupToken>, and answers 404 otherwise. Empty
	// disables it. webthings.App makes a new one at every start of a site
	// without accounts and logs the link.
	SetupToken string
	// RemindSetup is called when the sign-in page is shown while no
	// account exists, so whoever runs the site can find the setup link
	// again; webthings.App logs it once more, at most once a minute. The
	// link itself is never shown to visitors: the first to open it would
	// get the site. Nil reminds nobody.
	RemindSetup func()
	// AbsoluteURL makes an admin path absolute, for links in e-mail; nil uses
	// the address of the request.
	AbsoluteURL func(r *http.Request, path string) string
	// Timezone is the site's timezone, in which editors enter and read
	// scheduled times; nil uses UTC.
	Timezone func(ctx context.Context) *time.Location
	// Diagnostics is what the admin's renderer reports, such as render
	// metrics, and whether it shows template errors in detail
	// (development); see render.Diagnostics.
	Diagnostics render.Diagnostics
	// Review reports whether a section's review workflow is on, by the
	// section's permission ID; signed-in requests carry it (see
	// content.WithReview). Nil turns review off everywhere.
	Review content.ReviewPolicy
	// Permanence keeps the items administrators marked permanent (see
	// Permanence and NewPermanence). Nil offers no permanent mark; items
	// that are permanent by their nature, such as the home page, stay
	// protected.
	Permanence Permanence
	// Router is the go-router the admin registers its routes on, at their
	// full paths below Path, such as one a site mounted at Path with
	// router.AbsolutePaths before it built the admin; nil makes one (see
	// Router).
	Router *router.Router
}

// Permission names an action in a section, for example {"news", "publish"}.
type Permission struct {
	Section string
	Action  string
}

// The actions of the permission matrix, by which routes say what they need
// (see auth.Read and the others for what each allows).
const (
	// View opens a section and its items.
	View = auth.Read
	// Create adds new items.
	Create = auth.Create
	// Edit changes items.
	Edit = auth.Edit
	// Publish makes changes visible on the public site, or hides them.
	Publish = auth.Publish
	// Delete moves items to the trash, and brings them back or deletes them
	// for good there.
	Delete = auth.Delete
	// Review approves or rejects revisions submitted for review.
	Review = auth.Review
	// Export downloads personal data, such as subscribers as CSV. A section
	// that registers a route with it offers the right in the permission
	// matrix; other sections do not.
	Export = auth.Export
)

// Admin is the back office: the sign-in, the shell with its navigation,
// and the sections of core and the modules, each behind its permissions.
// It is an http.Handler for everything below its Path. Sections use it to
// render their screens, build links and check permissions.
type Admin struct {
	auth    *auth.Service
	options Options
	locale  Locale
	tr      *translator.Translator
	// calendars keeps the names of months and days and the date patterns
	// Format looks up in tr.
	calendars frontend.Calendars
	forms     *form.Form
	views     *render.Renderer
	// base is Options.Path; router holds the admin's routes, at their full
	// paths below base, and builds their addresses by name (see URL).
	base     string
	router   *router.Router
	sections []Section
	// stylesheets are the sections' own CSS by section ID; stylesheetLinks
	// are their link tags for the layout.
	stylesheets map[string]sectionStylesheet
	// importMapHash is the policy hash of the layout's import map (see
	// ImportMapHash).
	importMapHash   string
	stylesheetLinks []template.HTML
	// assets are the shared assets and the import map (see importmap.go).
	assets adminAssets
	// presence knows who looks at which page (see presence.go).
	presence *presence.Tracker
	// exports are the sections, by permission ID, with a route that needs
	// the Export right; only they offer it in the permission matrix.
	exports map[string]bool
}

// New builds the admin with its sections. Serve the result at its Path and
// below it as it is (see Path): it serves the full paths, such as
// <Path>/login.
func New(accounts *auth.Service, options Options, sections ...Section) (*Admin, error) {
	if accounts == nil {
		return nil, errors.New("admin needs an auth service")
	}

	if options.Title == "" {
		options.Title = "WebThings"
	}

	if options.Path == "" {
		options.Path = DefaultPath
	}

	if !validPath(options.Path) {
		return nil, fmt.Errorf("invalid admin path %q: it needs a leading slash, no trailing slash, letters, "+
			"digits and - . _ ~ only, and no segment that is . or .., such as /admin", options.Path)
	}

	a := &Admin{
		base:    options.Path,
		auth:    accounts,
		options: options,
		locale:  newLocale(options.Language),
		router:  cmp.Or(options.Router, router.NewSite()),
		exports: map[string]bool{},
		presence: presence.New(presence.Options{
			TTL: presenceTTL,
		}),
	}

	a.sections = append([]Section{a.accountsSection(), a.groupsSection()}, sections...)

	// A section without path is at its ID.
	for i := range a.sections {
		if a.sections[i].Path == "" {
			a.sections[i].Path = "/" + a.sections[i].ID
		}
	}

	if err := a.loadTranslations(); err != nil {
		return nil, err
	}

	if err := configureTheme(); err != nil {
		return nil, err
	}

	a.forms = form.NewTranslatedForm(func(loc form.Localizer, key string, args ...any) string {
		return a.tr.Tl(loc, key, args...)
	})
	a.forms.SetTheme("webthings-admin")
	a.forms.RegisterLocalizedValidation(mail.Rule, mail.FormRule)

	shared, err := fs.Sub(files, "assets")
	if err != nil {
		return nil, err
	}

	// go-partial's browser clients are versioned with the other assets, and
	// so are the classes of focal points, for crop previews.
	a.assets, err = loadAssets(partial.Overlay(shared, clients.FS(), frontend.FocalPointsStylesheet()))
	if err != nil {
		return nil, err
	}

	// Templates link to the admin's routes by name with go-router's
	// functions: route, routePath, routePathWithParams and the like; to
	// the shared assets with go-importmap's importmap_url, which adds the
	// file's version, and the head loads {{importmap}}.
	a.views = render.New(
		files,
		"templates/layout.gohtml",
		a.tr.FuncMap(),
		a.forms.FuncMap(),
		a.router.FuncMap(),
		a.assets.importMap.FuncMap(),
		// Parts that load after the page, forms and links use go-partial's
		// interaction attributes, the header's presence its exp/sse client.
		interactions.AttrsFuncMap(),
		sse.FuncMap(),
		a.templateFuncs(),
	)
	a.views.SetDiagnostics(a.adminDiagnostics(options.Diagnostics))
	// Toasts are go-partial's flash messages in the admin's markup. A
	// request that fails shows one error toast, with the refusal's own
	// sentence (text/plain) when it has one, or only the toast the failed
	// response brings (templates/flash-messages.gohtml gives the text).
	a.views.Use(flash.Stage(
		flash.WithTemplate("templates/flash.gohtml"),
		flash.WithRequestErrors(flash.RequestErrors{Replace: true, Body: true, SkipFlash: true}),
	))

	if err := a.routes(); err != nil {
		return nil, err
	}

	// The assets' addresses are those of routes, so they come after the
	// routes.
	a.mapAssets()

	// The stylesheets' links are addresses of a route, so they come after
	// the routes.
	if err := a.loadStylesheets(); err != nil {
		return nil, err
	}

	// Browsers say where a form post comes from (Sec-Fetch-Site, Origin), so
	// posts from other websites are refused without tokens: forms keep
	// working across restarts and on every server of the site.
	protection := http.NewCrossOriginProtection()
	protection.SetDenyHandler(http.HandlerFunc(a.crossOrigin))

	// Around every request of the router, also one no route answers and
	// one of a router that mounts it: T and TN find the admin's messages.
	a.router.UseAll(a.messages, protection.Handler)

	return a, nil
}

// loadTranslations loads the admin's catalogues, <locale>.po: those in
// CatalogueDir, when it has any, win over the admin's own (translations/),
// then come the sections' (translations/ in Section.Files), each filling in
// the messages the earlier catalogues of its locale miss (go-translator's
// LoadLanguages). An entry left empty counts as missing. Lookups in a locale
// without a catalogue of its own use one of the same language (nl finds
// nl_NL, go-translator's language fallback), and a message without
// translation shows as written; go-form's own messages, such as its
// validation errors, in go-form's catalogue of the locale.
func (a *Admin) loadTranslations() error {
	// Messages without translation are not written to the catalogue while
	// the admin serves (New records nothing): that would put what editors
	// type, such as titles and file names, into translations.pot, with a
	// disk write per new value. Catalogues come from the embedded defaults
	// and the extraction (see package translations).
	a.tr = translator.New(
		translator.WithTranslationsDir(a.options.CatalogueDir),
		translator.WithUntranslated(translator.ShowOriginal),
		translator.WithLanguageFallback(true),
	)

	sources, err := a.catalogues()
	if err != nil {
		return err
	}

	if err := a.tr.LoadLanguages(sources...); err != nil {
		return fmt.Errorf("admin translations: %w", err)
	}

	return nil
}

// catalogues are the folders of the admin's catalogues, in the order of
// loadTranslations.
func (a *Admin) catalogues() ([]fs.FS, error) {
	var sources []fs.FS

	if a.options.CatalogueDir != "" {
		sources = append(sources, os.DirFS(a.options.CatalogueDir))
	}

	for _, source := range append([]fs.FS{files}, a.sectionFiles()...) {
		catalogues, err := fs.Sub(source, "translations")
		if err != nil {
			return nil, err
		}

		sources = append(sources, catalogues)
	}

	return sources, nil
}

// sectionFiles are the Files of the sections that have them.
func (a *Admin) sectionFiles() []fs.FS {
	var sections []fs.FS

	for _, section := range a.sections {
		if section.Files != nil {
			sections = append(sections, section.Files)
		}
	}

	return sections
}

// routes registers the admin's pages below its path. The shared assets, the
// sections' stylesheets and go-partial's stream client are served without a
// session and may be cached; every other page gets the security headers and
// no-store (pageHeaders), and its form parsed within the body limit (body).
// Pages that need a session check it before the body is read.
func (a *Admin) routes() error {
	a.ownRoutes()

	var sectionErr error

	a.router.Group(a.base, func(base *router.Router) {
		base.With(func(pages *router.Router) {
			pages.Use(a.pageHeaders)

			sectionErr = a.sectionRoutes(pages)
		})
	})

	return sectionErr
}

// ownRoutes registers the admin's addresses outside its sections.
func (a *Admin) ownRoutes() {
	// The bare path leads to the dashboard, whatever the method: a router
	// that mounts the admin (see Router) sends it here too.
	a.router.Handle(a.base, http.HandlerFunc(a.toDashboard))

	a.router.Group(a.base, func(base *router.Router) {
		a.assetRoutes(base)
		a.pageRoutes(base)
	})
}

// ownPages are the first segments of the admin's addresses outside its
// sections, below its path, once ownRoutes registered them: sections cannot
// take them.
func (a *Admin) ownPages() []string {
	var pages []string

	_ = a.router.Walk(func(route router.RouteInfo) error {
		rest, below := strings.CutPrefix(route.Path, a.base+"/")
		first, _, _ := strings.Cut(rest, "/")

		if below && first != "" && !strings.HasPrefix(first, "{") && !slices.Contains(pages, first) {
			pages = append(pages, first)
		}

		return nil
	})

	return pages
}

// assetRoutes serves the shared assets ("admin.asset", which templates link
// to with their version as {{importmap_url "icons.svg"}}), go-partial's
// browser clients among them, and the sections' stylesheets.
func (a *Admin) assetRoutes(base *router.Router) {
	base.Get(assetsPath+"{file...}", a.serveAsset).As("admin.asset")
	base.Get(stylesheetPrefix+"{file}", a.serveStylesheet).As("admin.stylesheet")
}

// pageRoutes registers the admin's own pages.
func (a *Admin) pageRoutes(base *router.Router) {
	base.With(func(pages *router.Router) {
		pages.Use(a.pageHeaders)

		pages.With(func(public *router.Router) {
			public.Use(a.body(defaultBodyLimit))
			public.Get(loginPath, a.loginForm).As("admin.login")
			public.Post(loginPath, a.login)
			a.passwordRoutes(public)
		})

		pages.With(func(own *router.Router) {
			own.Use(a.signedIn)
			own.Use(a.body(defaultBodyLimit))
			own.Get(dashboardPath+"{$}", a.dashboard).As("admin.dashboard")
			own.Post(logoutPath, a.logout).As("admin.logout")
			own.Get(trashPath, a.trash).As("admin.trash")
			own.Post(trashPath, a.trashChange)
			a.accountRoutes(own)
		})

		a.presenceRoutes(pages)
	})
}

// sectionRoutes checks the sections and registers each one's routes in a
// group at its path, behind a session and its body limit.
func (a *Admin) sectionRoutes(pages *router.Router) error {
	seen := map[string]bool{}
	paths := map[string]string{}
	ownPages := a.ownPages()

	for _, section := range a.sections {
		// The admin's own pages are not sections, but their addresses are taken.
		reservedPath := slices.ContainsFunc(ownPages, func(page string) bool {
			return section.Path == "/"+page || strings.HasPrefix(section.Path, "/"+page+"/")
		})

		switch {
		case section.ID == "" || !validPath(section.Path) || section.Routes == nil:
			return fmt.Errorf("invalid admin section %q: it needs an ID, a path such as /news below the admin "+
				"and routes", section.ID)
		case reservedPath:
			return fmt.Errorf("admin section %q: %s is one of the admin's own pages", section.ID, section.Path)
		case seen[section.ID]:
			// Modules add sections named after their key, such as
			// <key>-categories, so two keys can claim the same section.
			return fmt.Errorf("admin section %q exists twice: choose module keys that differ from the "+
				"sections other modules add, such as news-categories for a module with key news", section.ID)
		case paths[section.Path] != "":
			return fmt.Errorf("admin sections %q and %q both use %s", paths[section.Path], section.ID, section.Path)
		}

		seen[section.ID] = true
		paths[section.Path] = section.ID

		limit := section.MaxBodyBytes
		if limit <= 0 {
			limit = defaultBodyLimit
		}

		var routesErr error

		pages.Group(section.Path, func(group *router.Router) {
			group.Use(a.signedIn)
			group.Use(a.body(limit))

			routesErr = registerRoutes(section, &Routes{admin: a, section: section, router: group})
		})

		if routesErr != nil {
			return fmt.Errorf("admin section %q: %w", section.ID, routesErr)
		}
	}

	return nil
}

// registerRoutes runs the section's Routes and returns what go-router
// refused of them, such as a route or route name registered twice.
func registerRoutes(section Section, routes *Routes) error {
	section.Routes(routes)

	return routes.err()
}

// Path is where the admin is served, Options.Path, such as "/admin": the
// admin answers its own addresses below it. Links into the admin are built
// by route name (see URL), not from Path.
func (a *Admin) Path() string {
	return a.base
}

// Router is the admin's go-router (Options.Router), whose routes have their
// full paths below Path. A site mounts it on its own router with them, so
// its route names, such as admin.dashboard, are the site's too; the
// admin's own middleware runs around the router (UseAll), so the router is
// all a site mounts:
//
//	site.Mount(backoffice.Path(), backoffice.Router(), router.AbsolutePaths())
func (a *Admin) Router() *router.Router {
	return a.router
}

// toDashboard sends a request for the bare Path to the dashboard.
func (a *Admin) toDashboard(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, a.URL("admin.dashboard"), http.StatusSeeOther)
}

// sectionURL is the address of a section: its path below the admin's.
func (a *Admin) sectionURL(section Section) string {
	return a.base + section.Path
}

// ServeHTTP serves the admin's pages at their full paths below Path.
func (a *Admin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.router.ServeHTTP(w, r)
}

// messages adds the admin's messages to the request context (see
// withMessages).
func (a *Admin) messages(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(a.withMessages(r.Context())))
	})
}

// pageSecurity are the security headers of admin pages, which no site may
// frame, the admin's own neither, and search engines never index:
// robots.txt does not name the admin, whose path may be the site's own.
// frame-ancestors is merged into the Content-Security-Policy the site
// sends, if any (see ImportMapHash).
var pageSecurity = middleware.SecurityHeaders(middleware.SecurityHeadersOptions{
	ContentTypeOptions:    "nosniff",
	FrameOptions:          "DENY",
	ContentSecurityPolicy: "frame-ancestors 'none'",
	XRobotsTag:            "noindex, nofollow",
})

// pageHeaders gives admin pages their security headers, keeps them from
// being stored, and keeps failed submissions at their current URL.
func (a *Admin) pageHeaders(next http.Handler) http.Handler {
	keepURL := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			connector.Apply(a.Connector(), w.Header(), connector.Response{PushURL: "false"})
		}

		next.ServeHTTP(w, r)
	})

	return middleware.NoStore(pageSecurity(keepURL))
}

// body limits the request body to limit bytes and parses posted forms, also
// uploads: handlers read r.PostForm and r.MultipartForm directly.
func (a *Admin) body(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)

			defer func() {
				if r.MultipartForm != nil {
					_ = r.MultipartForm.RemoveAll()
				}
			}()

			if err := parseForm(r); err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					a.refuse(w, r, http.StatusRequestEntityTooLarge, "Upload request too large.")

					return
				}

				a.BadRequest(w, r)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// parseForm parses a multipart body, and the form of other posts.
func parseForm(r *http.Request) error {
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if strings.EqualFold(contentType, "multipart/form-data") {
		return r.ParseMultipartForm(1 << 20)
	}

	if r.Method == http.MethodPost && r.PostForm == nil {
		return r.ParseForm()
	}

	return nil
}

// signedIn requires an active session.
func (a *Admin) signedIn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := a.auth.Authenticate(r.Context(), auth.Token(r))
		if errors.Is(err, auth.ErrSession) {
			a.redirect(w, r, a.URL("admin.login"))

			return
		}

		if err != nil {
			a.Fail(w, r, err)

			return
		}

		ctx, err := a.withPermissions(r.Context(), user)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		if a.options.Review != nil {
			ctx = content.WithReview(ctx, a.options.Review)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Can reports whether the current user may perform an action.
func (a *Admin) Can(r *http.Request, permission Permission) bool {
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		return false
	}

	if a.options.Authorize != nil {
		return a.options.Authorize(r.Context(), user, permission)
	}

	if user.IsAdmin() {
		return true
	}

	permissions, _ := r.Context().Value(permissionsKey{}).(auth.Permissions)

	return permissions.Allows(permission.Section, permission.Action)
}

// URL is the address of a named admin route, such as "admin.accounts.show"
// or a section's Section.RouteName("edit"), with its parameters filled in
// and an optional query string. The arguments are those of the route
// template function: name/value pairs, maps of parameters and url.Values for
// the query:
//
//	a.URL("admin.accounts.show", "user", user.ID)
//	a.URL(section.RouteName("edit"), "id", id, url.Values{"locale": {locale}})
//
// An unknown route, or a parameter that is missing or that the route does
// not have, is a mistake in the code rather than in the request: URL panics,
// and the admin answers 500 and logs it.
func (a *Admin) URL(name string, args ...any) string {
	path, err := a.TryURL(name, args...)
	if err != nil {
		panic(fmt.Sprintf("admin URL: %v", err))
	}

	return path
}

// TryURL is URL returning an error, rather than panicking, for an unknown
// route or a parameter that is missing or that the route does not have; for
// links whose route may not exist, such as the edit link of a module
// without an admin section of that name.
func (a *Admin) TryURL(name string, args ...any) (string, error) {
	return a.router.RoutePathArgs(name, args...)
}

// Location is the site's timezone, for times editors enter and read.
func (a *Admin) Location(r *http.Request) *time.Location {
	return a.location(r.Context())
}

func (a *Admin) location(ctx context.Context) *time.Location {
	if a.options.Timezone == nil {
		return time.UTC
	}

	if location := a.options.Timezone(ctx); location != nil {
		return location
	}

	return time.UTC
}

// UserName names the user with id as the revision history, the trash and
// the dashboard do: by the account's name, or its e-mail address without
// one. It is empty for an empty id, such as that of a change the system
// made, and for an account that no longer exists.
func (a *Admin) UserName(r *http.Request, id string) string {
	if id == "" {
		return ""
	}

	user, err := a.auth.User(r.Context(), id)
	if errors.Is(err, auth.ErrNotFound) {
		return ""
	}

	if err != nil {
		slog.ErrorContext(r.Context(), "user name", "user", id, "error", err)

		return ""
	}

	return user.DisplayName()
}

// Editor recognises the user signed in to the admin on a request outside it,
// such as a page of the public site, from the session cookie. It records no
// activity. can reports whether the user may perform an action, such as
// Edit, in the section of an admin URL; it loads the user's permissions on
// its first use.
func (a *Admin) Editor(r *http.Request) (user auth.User, can func(adminURL, action string) bool, ok bool) {
	token := auth.Token(r)
	if token == "" {
		return auth.User{}, nil, false
	}

	user, err := a.auth.Peek(r.Context(), token)
	if err != nil {
		if !errors.Is(err, auth.ErrSession) {
			slog.ErrorContext(r.Context(), "recognise editor", "error", err)
		}

		return auth.User{}, nil, false
	}

	signedIn := sync.OnceValue(func() *http.Request {
		ctx, err := a.withPermissions(r.Context(), user)
		if err != nil {
			slog.ErrorContext(ctx, "editor permissions", "error", err)
		}

		return r.WithContext(ctx)
	})

	can = func(adminURL, action string) bool {
		section, ok := a.sectionFor(adminURL)

		return ok && a.may(signedIn(), section, action)
	}

	return user, can, true
}

// sectionLabel is the label of a section's routes (see Routes.handle).
func sectionLabel(id string) string {
	return "section:" + id
}

// sectionFor finds the section that serves an admin URL: the section of the
// route the router has for its path, with any method. An address no route
// of a section serves, such as a mistyped one, belongs to none.
func (a *Admin) sectionFor(adminURL string) (Section, bool) {
	parsed, err := url.Parse(adminURL)
	if err != nil {
		return Section{}, false
	}

	route, ok := a.router.Owns(parsed.Path)
	if !ok {
		return Section{}, false
	}

	return a.routeSection(route)
}

// routeSection is the section a route of the admin's router belongs to, by
// its label.
func (a *Admin) routeSection(route router.RouteInfo) (Section, bool) {
	for _, section := range a.sections {
		if route.HasLabel(sectionLabel(section.ID)) {
			return section, true
		}
	}

	return Section{}, false
}

// Fail logs an unexpected error and answers 500 with "The request failed.
// Please try again.": an error toast for an HTMX request, the text for any
// other.
func (a *Admin) Fail(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "admin request", "path", r.URL.Path, "error", err)
	a.refuse(w, r, http.StatusInternalServerError, requestFailed)
}

// BadRequest answers 400 with "This request could not be read." in the admin
// language, for a post that no form of the admin sends, such as one whose
// fields do not fit the form: an error toast for an HTMX request, the text
// for any other.
func (a *Admin) BadRequest(w http.ResponseWriter, r *http.Request) {
	a.refuse(w, r, http.StatusBadRequest, badRequest)
}

// Forbidden answers 403 with "You are not allowed to do this." in the
// admin language, as routes do for users without the right: an error toast
// for an HTMX request, the text for any other.
func (a *Admin) Forbidden(w http.ResponseWriter, r *http.Request) {
	a.refuse(w, r, http.StatusForbidden, forbidden)
}

const (
	badRequest = "This request could not be read."
	forbidden  = "You are not allowed to do this."
)

// Language is the admin UI locale, as Options.Language names it, for example
// "nl"; "en" when it names none.
func (a *Admin) Language() string {
	return a.locale.GetLocale()
}

// T translates a UI message.
func (a *Admin) T(r *http.Request, key string, args ...any) string {
	return a.tr.Tl(localization.FromContext(r.Context()), key, args...)
}

// templateFuncs are the admin's own template functions, next to
// go-translator's and go-router's (see New). .go-clue/config.json lists it
// under functionMaps.
func (a *Admin) templateFuncs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(github.com/donseba/go-translator/v2.Localizer, github.com/donseba/go-form/v2.RenderModel, string) github.com/donseba/go-form/v2.RenderModel
		"confirm": a.confirm,
		//go-clue:sig func(github.com/donseba/go-translator/v2.Localizer) (html/template.HTML, error)
		"markdownEditor": a.markdownEditor,
	}
}

// Form builds a go-form model posting to action. Forms posting to a route
// named delete, or ending in -delete such as mailing-delete (see
// RouteName), ask "Move this item to the trash?" before they are sent, and
// those posting to one named unpublish ask "Take this off the website?";
// Confirm sets another question, for example for a delete that cannot be
// undone.
func (a *Admin) Form(r *http.Request, input any, action, submit string) form.RenderModel {
	info := form.Info{Target: action, Method: "post", SubmitText: submit}

	// The attributes are go-partial's connector attributes, in the syntax of
	// the admin's HTMX: signing in or out swaps the whole body.
	switch {
	case action == a.URL("admin.login") || action == a.URL("admin.logout") || action == a.URL("admin.setup"):
		info.Attributes = a.attrs(connector.Attr{Boost: connector.BoostOn, Target: "body", Swap: "outerSync"})
	case a.pathAction(http.MethodPost, action) == "delete":
		info.Attributes = a.attrs(connector.Attr{Confirm: a.T(r, deleteConfirmation)})
	case a.pathAction(http.MethodPost, action) == "unpublish":
		info.Attributes = a.attrs(connector.Attr{Confirm: a.T(r, unpublishConfirmation)})
	}

	return form.WithRequestInfo(r, input, info)
}

const (
	deleteConfirmation    = "Move this item to the trash?"
	unpublishConfirmation = "Take this off the website?"
)

// Confirm returns the form with a question the editor must confirm before it
// is sent, such as "Reset the votes? This cannot be undone." The message is a
// translation key. An empty message removes the question, also the default
// one of delete forms. Templates can do the same with the confirm function:
//
//	{{form_render_localized localizer (confirm localizer .Reset "Reset the votes?") nil}}
func (a *Admin) Confirm(r *http.Request, model form.RenderModel, message string) form.RenderModel {
	return a.confirm(localization.FromContext(r.Context()), model, message)
}

func (a *Admin) confirm(loc translator.Localizer, model form.RenderModel, message string) form.RenderModel {
	var question connector.Attr
	if message != "" {
		question.Confirm = a.tr.Tl(loc, message)
	}

	// The connector's question attribute, such as hx-confirm, is replaced
	// in a copy: models built from one Form may share the map.
	model.Info.Attributes = connector.MergeAttrs(a.views.Connector(), model.Info.Attributes, question, connector.AttrConfirm)

	return model
}

// attrs are the attributes of attr in the syntax of the admin's connector
// (go-partial's connector.Attrs), for forms and links of the admin's own.
func (a *Admin) attrs(attr connector.Attr) map[string]string {
	return connector.Attrs(a.views.Connector(), attr)
}

// Validate checks a submitted form; input must be a pointer.
func (a *Admin) Validate(r *http.Request, input any) form.FieldErrors {
	return a.forms.ValidateFormLocalized(input, localization.FromContext(r.Context()))
}
