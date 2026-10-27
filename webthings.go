// Package webthings runs a website's backend: database, accounts, pages,
// media, site settings, the admin and the selected modules. The website itself
// only supplies templates (or its own Renderer) and assets.
//
//	app := webthings.New(webthings.Config{Name: "My site", Templates: os.DirFS("templates")})
//	app.Use(publishing.Module("news", "News"))
//
//	if err := app.ListenAndServe(ctx, ":8080"); err != nil {
//		log.Fatal(err)
//	}
//
// ListenAndServe returns nil once ctx is cancelled and the app has shut
// down, so a clean stop, for example on SIGTERM, exits without an error.
//
// # Routing
//
// Once started, every request passes one go-router, the app's root router
// (App.ServeHTTP):
//
//   - Around it, with UseAll, the request-wide middleware: the visitor's
//     address (Config.TrustedProxies), the request ID (RequestID), the
//     security headers X-Content-Type-Options: nosniff and
//     Referrer-Policy: strict-origin-when-cross-origin, which every
//     response gets, also go-router's own redirects to clean paths,
//     telemetry, the access log and metrics for every request but those to
//     the endpoints for operations, and recovery from panics, which they
//     see as a 500.
//   - The endpoints for operations: /healthz, /readyz and /metrics.
//   - Behind compression, the core routes: the admin, whose router is
//     mounted at Config.AdminPath, so its routes and their names are the
//     root router's, the media library's routes at /media, named
//     media.asset and media.variant, sitemap.xml, robots.txt, /assets,
//     /theme and /_/webthings.css.
//   - The modules' visitor actions, each module's action router
//     (Env.Actions) mounted at /_/<key>/ behind the protection against
//     cross-site posts, the framing headers of the site setting
//     (site.Settings.Framing) and the body limit (Parts.ActionBodyLimit);
//     its routes become part of the root router.
//   - With Config.API, the content API at /api/v1/ (package api), with
//     each module's endpoints (Parts.API) mounted at /api/v1/<key>/, behind
//     its rate limit and CORS, the output cache, compression and an ETag.
//   - The site, as the catch-all "/", behind the framing headers of the
//     site setting. Pages live in the database and can move, so the site
//     resolves the page itself and serves the module on it there: each
//     module's Parts.Public, a go-router of its own from frontend.Router,
//     whose routes are relative to the page, served at the page's address
//     for that request (go-router's ServeAt), with the routes of its Atom
//     and RSS feeds (Parts.Feeds). The site's requests pass the
//     editors' recognition and the modules' middleware (Parts.Middleware)
//     first, then the output cache, which keeps pages for visitors in
//     memory, compressed (see Config.OutputCacheTTL), then compression.
//
// Before Start a small router answers instead: the endpoints for
// operations, with request IDs and recovery, and 503 for everything else.
// Every registration of the root router but the site's is labelled with its
// route class, such as admin, media or asset: they are CorePaths, known from
// New on, and the metrics count requests by them.
package webthings

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/api"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/internal/ops"
	"github.com/donseba/go-webthings/links"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/media/mediaadmin"
	"github.com/donseba/go-webthings/pages"
	"github.com/donseba/go-webthings/pages/pageadmin"
	"github.com/donseba/go-webthings/render"
	"github.com/donseba/go-webthings/site"
	"github.com/donseba/go-webthings/site/siteadmin"

	"github.com/donseba/go-importmap"
	"github.com/donseba/go-router"
	"github.com/donseba/go-router/middleware"
	translator "github.com/donseba/go-translator/v2"
)

// Config is everything a site decides in code when it creates its App: its
// name, where it keeps its state, which database it uses, its templates and
// languages, and the limits it runs with. Everything an editor may change
// later, such as the site title or the theme, lives in the Site settings
// instead. The zero value of every field is a working default.
type Config struct {
	// Name is the initial site title and the admin title.
	Name string
	// StateDir holds the SQLite database and uploaded media, and may hold
	// translations/<locale>.po files that override the admin's own texts.
	StateDir string
	// Driver is sqlite (default; "sqlite3" is accepted too), pgx or mysql.
	// SQLite is pure Go, so no build needs cgo. An empty DSN with SQLite uses
	// site.sqlite in StateDir; db.Open adds foreign keys, a busy timeout and
	// the other settings WebThings needs to any SQLite DSN that lacks them.
	Driver string
	// DSN is the data source name the driver connects with, such as
	// "postgres://user:pass@host/site"; see Driver for SQLite's default.
	DSN string
	// Locales are the initial content languages, used on first start only.
	// Later changes happen in the admin's site settings.
	Locales locales.Config
	// AdminLanguage is the admin UI language, for example "en" or "nl";
	// "en" when empty.
	AdminLanguage string
	// AdminPath is where the admin is served, such as "/admin" (the default,
	// when empty) or "/toolbox": a leading slash, no trailing slash,
	// letters, digits and - . _ ~ only, and no "." or ".." segment. The
	// admin, the core sections and the modules link to its pages by route
	// name (see Env.AdminURL), so
	// nothing depends on it, and robots.txt does not name it: admin pages
	// answer with X-Robots-Tag: noindex instead. It cannot be one of the
	// other core paths (see App.CorePaths), such as /media.
	AdminPath string
	// AdminEmail and AdminPassword create the first admin account on an empty
	// database. They are ignored once any account exists. Without them, a
	// start without accounts logs a one-time link to the first-run setup,
	// <AdminPath>/setup?token=… on BaseURL or the address ListenAndServe listens
	// on, where the first administrator account is created (see
	// App.SetupToken). The setup answers 404 without that link's token and
	// once any account exists.
	AdminEmail    string
	AdminPassword string
	// Insecure allows session cookies over plain HTTP, for local development.
	Insecure bool
	// Authorize decides admin permissions. Nil applies the built-in rules:
	// administrators may do everything, editors what their groups' matrix
	// allows (see auth.Group). When set, it decides every permission check,
	// for administrators too. The Accounts and Groups screens always need the
	// administrator role as well, and Authorize is asked about them too, so
	// it can keep them from administrators but never open them to editors.
	// It must be safe for concurrent use: requests and background work ask
	// it at the same time.
	Authorize func(ctx context.Context, user auth.User, permission admin.Permission) bool

	// Templates are the public site's templates: one folder per theme, such as
	// templates/default/, or a single theme at the root (see frontend.Themes).
	Templates fs.FS
	// Translations are the site's own translations of the texts of its
	// templates and of the modules' default templates and visitor forms,
	// usually frontend.Translations("translations"): templates get its tl,
	// tn, ctl and ctn, and core translates with it its own texts (such as
	// the 404 page's title), go-form's labels and messages and the names of
	// months and days and the date patterns. Those replace the built-in ones
	// in every language, English and Dutch included; what it leaves out is
	// built in, or English for a language without built-in ones (see
	// frontend.BuiltInCalendar). Nil shows every text as written.
	// App.Translations extracts the texts to translate.
	Translations *translator.Translator
	// Funcs are added to the templates.
	Funcs []template.FuncMap
	// Renderer replaces Templates with another template engine; see
	// frontend.Renderer for what it renders and frontend.TemplateFuncs for
	// the template functions. Funcs and the modules' functions still serve
	// components, mail and the site's translations.
	Renderer frontend.Renderer
	// ReloadTemplates reads Templates again for every request, so edits to
	// template files show without a restart; for development. Otherwise
	// every template is parsed once.
	ReloadTemplates bool
	// Assets are served at /assets/ (the route "assets") like the files of
	// an ImportMap of their own: templates get {{importmap_url "file"}}
	// for a file's versioned URL, which may be cached for a year, and a
	// plain /assets/<file> revalidates with a strong ETag. They are
	// fingerprinted at Start, and again after a change while templates
	// reload. Set Assets or ImportMap, not both.
	Assets fs.FS
	// ImportMap is the site's import map of go-importmap, such as one of
	// packages vendored with its importmap command and loaded with
	// importmap.FromFS, which may hold the site's own scripts and
	// stylesheets too. Its files are served at /assets/ (the route
	// "assets") with versioned URLs and strong ETags, and templates get
	// {{importmap}} for the head and {{importmap_url "file"}} for the
	// versioned URL of one of its files.
	//
	// Core adds go-partial's browser clients to either import map, under
	// the names scripts import them by: partial-flash, partial-sse and
	// partial-interactions, unless the site maps such a name itself. The
	// site needs no copy of them.
	ImportMap *importmap.ImportMap
	// FormTheme is the go-form theme the visitor forms of templates render
	// with ({{form_field .Form "name" nil}} and the other form functions),
	// such as one the site registers with go-form's
	// templates.RegisterClassTheme with the classes of its CSS. Empty uses
	// frontend.FormTheme, the classes of webthings.css.
	FormTheme string
	// BaseURL is the public address, for example "https://example.com", used in
	// sitemap.xml, share links, {{absURL}} and links sent by e-mail. Empty
	// uses the address of each request: its Host header, which the client
	// chooses, and X-Forwarded-Proto and X-Forwarded-Host (or Forwarded)
	// from TrustedProxies only. Set it in production; Start warns without it
	// unless Insecure.
	BaseURL string
	// TrustedProxies are the reverse proxies (IP addresses or CIDR ranges,
	// for example "127.0.0.1" or "10.0.0.0/8") whose X-Forwarded-For,
	// X-Real-IP and RFC 7239 Forwarded (for=) headers name the visitor (see
	// frontend.ClientIP) and whose X-Forwarded-Proto and X-Forwarded-Host
	// headers, with Forwarded's proto= and host= as a fallback, name the
	// address visitors used (see frontend.AbsoluteURL). Empty trusts none,
	// so visitors are known by the address they connect from.
	TrustedProxies []string
	// Mail sends modules' e-mail; nil logs messages instead (mail.Log).
	Mail mail.Sender
	// Storage keeps uploaded media; nil stores files in StateDir/media.
	Storage media.Storage
	// CacheTTL is how long site settings and published routes are kept in
	// memory: changes in this process apply at once, changes by other
	// processes (several servers on one database) within this time. Zero
	// means 30 seconds; negative disables the cache.
	CacheTTL time.Duration
	// OutputCacheTTL overrides the site setting of the output cache, which
	// keeps the public pages visitors get in memory and serves them again
	// without rendering them (see site.Settings.OutputCache): a positive
	// time keeps them that long, also in Development, where responses say
	// in X-Cache whether the cache answered (hit, miss or skip); negative
	// turns the cache off. Zero follows the site setting, except that
	// Development and ReloadTemplates turn the cache off. Site settings ›
	// Performance then shows the time that applies, which editors cannot
	// change there.
	OutputCacheTTL time.Duration
	// ScheduleInterval is how often scheduled publication changes are carried
	// out. Zero means 30 seconds; negative stops the background scheduler, for
	// example in tests that call RunSchedules themselves.
	ScheduleInterval time.Duration

	// Development shows failing templates in detail, with the template and
	// line, on the site and in the admin, and makes {{debug .}} show a
	// value; for local development only. Otherwise a failing template is
	// logged and answered with the theme's 500.gohtml (or go-partial's
	// plain error page when the theme has none), or in the admin with its own
	// error page, and {{debug .}} shows nothing.
	//
	// Start checks every template of every theme (see ErrTemplates): in
	// development it fails with every problem the check finds, and a
	// template call that would show nothing, such as {{posts "nieuws" 3}}
	// for an instance key the site does not have, an asset file the theme
	// does not have or an unknown theme setting, fails the page. Otherwise
	// the problems and such calls are logged at WARN, and Start fails only
	// when every page of the active theme would fail.
	Development bool
	// LogFormat, "text" or "json", makes Start log to stderr in that format,
	// at LogLevel and up, with the request ID (see RequestID) on every line
	// logged with a request's context; Start makes it slog's default. Empty
	// leaves logging to the site (see LogHandler).
	LogFormat string
	// LogLevel is the lowest level LogFormat logs; the zero value is info.
	LogLevel slog.Level
	// AccessLog logs a line for every request: method, path without the
	// query, status, size, duration and request ID.
	AccessLog bool
	// MetricsToken serves the metrics (see App.Metrics) at /metrics to
	// requests that carry it as "Authorization: Bearer <token>", such as
	// the output cache's counters:
	//
	//	curl -H "Authorization: Bearer $TOKEN" https://example.com/metrics | grep output_cache
	//
	// Empty serves no metrics at /metrics: it answers 404. /healthz and
	// /readyz are always served (see App.Ready).
	MetricsToken string
	// Telemetry plugs in tracing and other instrumentation.
	Telemetry Telemetry

	// ContentSecurityPolicy overrides the site setting of the
	// Content-Security-Policy (site.Settings.ContentSecurity), for tests
	// and deployments that decide it in code: "off", "report-only" or
	// "enforce" (site.CSPOff, site.CSPReportOnly, site.CSPEnforce). Empty
	// follows the site setting, which is off until an administrator
	// changes it; otherwise Site settings › Security shows this value,
	// which cannot be changed there. The policy lets pages run only the
	// scripts and styles the site serves, by go-router's
	// middleware.StrictCSP and the hashes of the import maps; see the
	// package documentation.
	ContentSecurityPolicy string
	// ContentSecuritySources are sources the policy allows by directive,
	// besides the site's own, such as {"font-src":
	// {"https://fonts.example.com"}}, added to those of the site setting
	// and the theme. Start refuses what site.CheckCSPSources does not
	// allow, such as 'unsafe-inline' or *.
	ContentSecuritySources map[string][]string

	// API serves the published content as JSON at /api/v1/ (package api),
	// with the modules' endpoints (Parts.API), for apps and decoupled
	// frontends: its rate limit and the sites that may read it in the
	// browser (CORS). Off by default.
	API api.Config
}

// Module is one capability instance. Type names the module's schema and its
// template folder; Key identifies this instance, for example "news" and
// "blog" for two Publishing instances.
type Module struct {
	// Type is the kind of module, such as "publishing"; it is the same for
	// every instance.
	Type string
	// Key names this instance in addresses, template folders and the
	// admin, such as "news": lowercase letters, digits and dashes.
	Key string
	// Title is the instance's name in the admin, such as "News".
	Title string
	// Schema holds the module's schema files, applied at Start once for
	// all instances of the Type (see db.ApplySchema); nil for modules
	// without tables.
	Schema fs.FS
	// Setup builds the instance once the core services are ready, and
	// returns what it adds to the site and the admin.
	Setup func(Env) (Parts, error)
	// Templates are the public templates the module renders, looked up as
	// <key>/<name>.gohtml, then <type>/<name>.gohtml.
	Templates []frontend.Template
	// Funcs are template functions the module offers to every template, such
	// as {{range posts "news" 3}}. A first context.Context parameter receives
	// the request; frontend.Service gives the instance for a key.
	Funcs template.FuncMap
	// Mail are the e-mail templates the module writes with Frontend.Mail, with
	// the default text as Example; themes replace them with
	// mail/<key>/<name>.gohtml or mail/<type>/<name>.gohtml.
	Mail []frontend.Template
	// Components are parts of a page the module offers to every template,
	// such as a comment thread or a signup form, with the default markup as
	// Example. The module's template functions render them with
	// frontend.Component; themes replace them with <key>/<name>.gohtml or
	// <type>/<name>.gohtml, so their names differ from those of Templates.
	Components []frontend.Template
	// Forms are the visitor forms the module offers to templates, values of
	// structs with go-form's tags such as comments.PostInput{}, which
	// themes render with form_open and form_field. Their texts (labels,
	// descriptions, placeholders, option labels, submit texts and messages,
	// go-form's Texts) are translated with the site's translations, so
	// extraction finds them (see TranslationCatalog).
	Forms []any

	// group is the admin navigation heading of the module's sections; see
	// Group.
	group string
}

// Group lists the module's admin sections under a heading of the
// navigation, for example:
//
//	app.Use(banners.Module("notice", "Notices").Group("Website"))
//
// All sections of the module get it, also the ones it adds next to its
// main section, such as categories. The name is translated by the admin
// when its catalogue has it: Agenda, Content, Visitors, Website,
// Communication, Shop and the admin's own groups are; other names are
// shown as written. Modules without a group are listed under "Modules"
// (admin.GroupModules), unless they set Section.Group themselves.
func (m Module) Group(name string) Module {
	m.group = strings.TrimSpace(name)

	return m
}

// Env gives a module the shared services.
type Env struct {
	// Key and Title are those of the Module being set up.
	Key   string
	Title string
	// DB is the site's database.
	DB *db.DB
	// Locales gives the content languages, as the Site settings have them
	// at the time of asking.
	Locales locales.Provider
	// Site holds the Site settings.
	Site *site.Service
	// Auth holds the editors' accounts, for names in the admin and for
	// notifying editors.
	Auth *auth.Service
	// Media is the media library.
	Media *media.Service
	// Pages is the page tree, for links to the pages a module is placed on.
	Pages *pages.Service
	// Frontend renders templates, components and e-mail with the theme.
	Frontend *frontend.Frontend
	// Mail sends e-mail; it logs messages when the site has no Config.Mail.
	Mail mail.Sender
	// BaseURL is Config.BaseURL, for absolute links in e-mail.
	BaseURL string
	// AdminLanguage is Config.AdminLanguage, the language of the admin, for
	// text staff read, such as notification e-mail; "en" when not set.
	AdminLanguage string

	app *App
	// actions is the module's action router (see Actions), shared by the
	// copies of this Env.
	actions *actionMount
}

// Service returns the API of another module instance, for example
// env.Service("agenda").(*events.Service). It works once the app has
// started, so modules resolve it when they need it (in a handler) rather
// than in Setup; before, and for unknown keys, it returns nil.
func (e Env) Service(key string) any {
	if e.app == nil || !e.app.started.Load() {
		return nil
	}

	return e.app.apis[key]
}

var secretName = regexp.MustCompile(`^[a-z0-9_-]{1,35}$`)

// Secret returns a random 32-byte key of this module instance, the same in
// every process of the site, for example to hash visitors' addresses with
// HMAC. name tells a module's keys apart: lowercase letters, digits, - and
// _, at most 35.
func (e Env) Secret(ctx context.Context, name string) ([]byte, error) {
	if !secretName.MatchString(name) {
		return nil, fmt.Errorf("invalid secret name %q", name)
	}

	return e.Site.Secret(ctx, e.Key+"/"+name)
}

// Links returns the site's link check (App.Links), which knows the
// published content that links to an address, for the editor of a module
// whose items have an address of their own (see editor.Linked). It is nil
// on an Env made without an app.
func (e Env) Links() *links.Check {
	if e.app == nil {
		return nil
	}

	return e.app.Links
}

// AbsoluteURL makes path absolute with BaseURL, or with the address of r
// when BaseURL is empty; for links in e-mail.
func (e Env) AbsoluteURL(r *http.Request, path string) string {
	return frontend.AbsoluteURL(e.BaseURL, r, path)
}

// IsCorePath reports whether path is one of the app's CorePaths or below
// one of them, such as the admin's pages wherever it is (Config.AdminPath).
func (e Env) IsCorePath(path string) bool {
	if e.app == nil {
		return defaultApp().IsCorePath(path)
	}

	return e.app.IsCorePath(path)
}

// AdminURL is the address of a named admin route, as admin.Admin.URL builds
// it, for links into the admin from outside it, such as an edit link on the
// site or a link in an e-mail to staff:
//
//	env.AdminURL(admin.RouteName(env.Key, "edit"), "id", id)
//
// Modules never build admin addresses from paths: the admin may be anywhere
// (Config.AdminPath). Section routes are named "admin.<section ID>.<name>"
// (see admin.RouteName and admin.Route.As); sections built with
// admin/editor name theirs, such as index, new and edit, and a module names
// the routes it adds with Route.As. Inside the admin, handlers use
// admin.Routes.URL or admin.Admin.URL and templates the route function. It
// works once the app has started; before, it returns "". Like
// admin.Admin.URL, it panics on an unknown route or a missing or unknown
// parameter.
func (e Env) AdminURL(name string, args ...any) string {
	if e.app == nil {
		return ""
	}

	return e.app.AdminURL(name, args...)
}

// Actions is the go-router of the module's visitor actions: the forms
// visitors post to and links they follow, wherever the site shows them. It is
// mounted at /_/<key>/ behind the protection against cross-site posts and
// the body limit (Parts.ActionBodyLimit), so its routes are relative to that
// and become part of the app's root router, their names with the key in
// front, such as <key>.post for a route named post. A module that runs more
// than once names its routes alike.
//
//	actions := env.Actions()
//	actions.Use(frontend.Limits(env.Key, frontend.ActionLimits{Body: 64 << 10, Posts: 5, Window: time.Minute}))
//	actions.Post("/post", post).As("post")
//
// Modules keep the router and build the addresses of their actions from it
// by route name, also without a request, such as for links in e-mail:
// actions.RoutePath("post") is "/_/<key>/post", and
// actions.RoutePathArgs("download", "id", id) fills in a wildcard. Handlers
// see the whole request path, and PathValue gives the route's wildcards; a
// request without a route gets a plain 404, or 405 for a path that has
// routes for other methods.
//
// On an Env the app made, every call returns the same router; the app mounts
// it on the first. An Env made without an app, as in tests and tools, gets a
// new router on every call, mounted on a private root router with the app's
// own routes, so the addresses are the same.
func (e Env) Actions() *router.Router {
	if e.actions == nil {
		return New(Config{}).actionMount(e.Key).router()
	}

	return e.actions.router()
}

// Parts is what a module adds to the site.
type Parts struct {
	// Admin sections, usually one with the module's key as ID, at
	// <AdminPath>/<key> (see admin.Section.Path).
	Admin []admin.Section
	// Public serves the module at and below the page it is mounted on. Nil
	// means the module cannot be mounted. A go-router of frontend.Router is
	// served at the page's address (see frontend.Router); any other handler
	// sees the request's whole path, its page in frontend.MountFrom.
	Public http.Handler
	// Service is the module's API for the website's own code; see App.Service.
	Service any
	// Sitemap lists the module's public paths below its page, for sitemap.xml.
	Sitemap frontend.SitemapFunc
	// Feeds are the module's Atom and RSS feeds, served below its page on
	// Public, which is then a router of frontend.Router, as routes of
	// their own, such as <page>/feed.atom named feed.atom (see
	// frontend.Feed). The layout links to those of the page shown with
	// {{feedLinks}}.
	Feeds []frontend.Feed
	// ActionBodyLimit is the largest request body the module's visitor
	// actions (Env.Actions) accept in bytes, for example more for an upload;
	// zero means DefaultActionBodyLimit.
	ActionBodyLimit int64
	// Middleware wraps the public site, for example to redirect old URLs.
	// It sees every request for the site's pages, also those the output
	// cache answers, outside compression: a response it reads may be
	// gzip-compressed (Content-Encoding).
	Middleware func(http.Handler) http.Handler
	// Close runs when the app closes, before the database does, for example
	// to write buffered data.
	Close func(ctx context.Context) error
	// Search finds the module's published content for the site search (see
	// frontend.Search); results need a URL.
	Search frontend.SearchFunc
	// Scheduled is content whose publication editors can plan, carried out by
	// the app's scheduler; see content.Revisions.Scheduler.
	Scheduled []content.Scheduler
	// Links lists the module's published content in a language with the
	// links in it, such as Markdown texts and link fields (see
	// links.Source), so the admin knows which content links to a page: it
	// warns editors before the page leaves its address and lists the
	// links that lead nowhere (App.Links).
	Links links.Func
	// Redirects is the module's service when it redirects old addresses,
	// such as the redirects module's: the admin then offers to redirect
	// the addresses links lead to when a page leaves them. The first
	// module that sets it is used.
	Redirects links.Redirector
	// API serves the module's published content as JSON below
	// /api/v1/<key>/ while the site's content API is on (Config.API): a
	// router of api.NewRouter, whose routes are relative to that and named
	// api.<key>.<name> for the app's other routers. Its handlers answer in
	// the request's language (api.Locale) with the helpers of package api,
	// and never show drafts or personal data, such as entries of visitors.
	// It is not mounted while the API is off.
	API *router.Router
	// StructuredData adds to the structured data of the site's pages, or
	// changes it, such as the offers of an event visitors register for.
	// A module's own pages give theirs with frontend.Content's
	// StructuredData instead.
	StructuredData frontend.StructuredDataFunc
}

// DefaultActionBodyLimit is the largest request body modules' visitor
// actions (Env.Actions) accept unless Parts.ActionBodyLimit says otherwise:
// 1 MiB, ample for a form.
const DefaultActionBodyLimit = 1 << 20

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// App is a running site: the core services, the modules chosen with Use,
// the admin and the public site, served as one http.Handler. Create it with
// New, choose modules with Use, then Start it, or let ListenAndServe do
// both. The services in its exported fields are ready once Start has
// returned.
type App struct {
	config   Config
	modules  []Module
	mounted  map[string]http.Handler
	wrappers []func(http.Handler) http.Handler
	closers  []func(context.Context) error
	// shutdowns end long-lived responses, such as the admin's presence
	// streams, when the server shuts down, so Shutdown need not wait for them.
	shutdowns  []func()
	apis       map[string]any
	sitemaps   map[string]frontend.SitemapFunc
	searchers  map[string]frontend.SearchFunc
	schedulers []content.Scheduler
	storage    media.Storage
	// backoffice is the admin, from Start on.
	backoffice *admin.Admin
	// root is the root router (see routes), built by New, so CorePaths
	// are known before Start; handler is root once the app has started,
	// and idle serves before (see ServeHTTP).
	root    *router.Router
	handler http.Handler
	idle    http.Handler
	// lazy are the handlers of root's routes that Start builds (see
	// routes).
	lazy lazyHandlers
	// output keeps public pages for visitors (see outputCache).
	output *outputCache
	// csp is the Content-Security-Policy of the last snapshot of the
	// settings (see policy); mediaOrigin the origin the media storage
	// serves files at itself, from Start on (see storageOrigin).
	csp         atomic.Pointer[keptPolicy]
	mediaOrigin string
	// contentAPI is the content API of Config.API (see apiRoutes).
	contentAPI contentAPI
	// metrics counts requests and renders; schemaVersions are the schema
	// versions Start applied by owner, which Ready checks.
	metrics        *ops.Metrics
	schemaVersions map[string]int64
	// setupToken guards the first-run setup; see SetupToken.
	setupToken string
	// listening is the address Serve listens on, for the setup link, and
	// setupReminded when remindSetup last logged it, in Unix nanoseconds.
	listening     string
	setupReminded atomic.Int64
	// started is set at the end of Start; Env.Service works from then on.
	started atomic.Bool
	// stop ends the background work (scheduler, cleanup and the variants
	// of uploaded images); stopped waits until it has.
	stop    context.CancelFunc
	stopped sync.WaitGroup

	// DB is the database, for a site's own tables and queries.
	DB *db.DB
	// Auth holds the accounts, sessions and groups of editors.
	Auth *auth.Service
	// Media is the media library of uploaded files.
	Media *media.Service
	// Site holds the Site settings, such as the title, languages and theme.
	Site *site.Service
	// Pages is the page tree with its revisions.
	Pages *pages.Service
	// Frontend renders the public site with the active theme.
	Frontend *frontend.Frontend
	// Links knows which published content links to which address on the
	// site, the pages' and the modules' (Parts.Links).
	Links *links.Check
}

// New returns an App for config, with the defaults filled in: the name
// "WebThings", state in "var", SQLite in var/site.sqlite, English as the
// only language and the admin at "/admin". It opens nothing yet; Start
// does, so New cannot fail.
func New(config Config) *App {
	if config.Name == "" {
		config.Name = "WebThings"
	}

	if config.StateDir == "" {
		config.StateDir = "var"
	}

	if config.Driver == "" {
		config.Driver = "sqlite"
	}

	if config.DSN == "" && (config.Driver == "sqlite" || config.Driver == "sqlite3") {
		file := filepath.Join(config.StateDir, "site.sqlite")
		config.DSN = "file:" + file
	}

	if config.Locales.Default == "" {
		config.Locales = locales.Config{Default: "en", Enabled: []string{"en"}}
	}

	// Modules write staff e-mail in the admin language (Env.AdminLanguage).
	if config.AdminLanguage == "" {
		config.AdminLanguage = "en"
	}

	if config.AdminPath == "" {
		config.AdminPath = admin.DefaultPath
	}

	a := &App{config: config}
	a.output = &outputCache{app: a}
	a.idle = a.idleRoutes()
	a.root = a.routes()

	return a
}

// Use selects modules. Call it before Start.
func (a *App) Use(modules ...Module) error {
	for _, module := range modules {
		// Keys name admin sections (<AdminPath>/<key>) and template folders;
		// webthings/ holds core's templates, such as its head tags.
		reserved := append(
			[]string{"pages", "media", "site", "theme", "mail", "partials", "webthings"},
			admin.ReservedSections()...)
		if !keyPattern.MatchString(module.Key) || slices.Contains(reserved, module.Key) || module.Setup == nil {
			return fmt.Errorf("invalid module %q", module.Key)
		}

		for _, existing := range a.modules {
			if existing.Key == module.Key {
				return fmt.Errorf("module key %q is used twice", module.Key)
			}
		}

		for _, component := range module.Components {
			for _, screen := range module.Templates {
				if component.Name == screen.Name {
					return fmt.Errorf("module %q: component %q has the name of a template", module.Key, component.Name)
				}
			}
		}

		a.modules = append(a.modules, module)
	}

	return nil
}

// Start opens the database, applies schemas and builds the site and admin.
// Without accounts it logs the link to the first-run setup (see
// SetupToken).
func (a *App) Start(ctx context.Context) error {
	return a.start(ctx, "")
}

// start is Start; addr is the address ListenAndServe will listen on, for
// the setup link, or empty.
func (a *App) start(ctx context.Context, addr string) error {
	// The root router takes the routes of one start.
	if a.handler != nil {
		return errors.New("the app has started already")
	}

	if err := os.MkdirAll(a.config.StateDir, 0o700); err != nil {
		return err
	}

	a.mounted = map[string]http.Handler{}
	a.apis = map[string]any{}
	a.sitemaps = map[string]frontend.SitemapFunc{}
	a.searchers = map[string]frontend.SearchFunc{}
	a.schemaVersions = map[string]int64{}

	// The admin cannot take the place of another core address; an admin
	// path that is not clean is refused by the admin itself.
	if a.lazy.adminErr != nil {
		return a.lazy.adminErr
	}

	if err := a.assetsImportMap(); err != nil {
		return err
	}

	if err := a.checkContentSecurity(); err != nil {
		return err
	}

	a.metrics = ops.NewMetrics()

	accessLog, err := a.setupLogging()
	if err != nil {
		return err
	}

	a.DB, err = db.Open(ctx, a.config.Driver, a.config.DSN)
	if err != nil {
		return err
	}

	if err := a.schemas(ctx); err != nil {
		return err
	}

	if err := a.services(ctx); err != nil {
		return err
	}

	exists, err := a.Auth.HasUsers(ctx)
	if err != nil {
		return err
	}

	a.setupToken = ""
	if !exists {
		a.setupToken = rand.Text()
	}

	requestWide, err := a.requestWide()
	if err != nil {
		return err
	}

	if a.config.BaseURL == "" && !a.config.Insecure {
		slog.Warn("BaseURL is empty: links in e-mail, sitemap.xml and share links use the address each request names",
			"set", "Config.BaseURL, for example https://example.com",
		)
	}

	mails := map[string][]frontend.Template{}
	components := map[string][]frontend.Template{}

	for _, module := range a.modules {
		mails[module.Key] = module.Mail
		components[module.Key] = module.Components
	}

	a.Frontend.SetMail(mails)
	a.Frontend.SetComponents(components)

	// Pages and Media head the admin navigation; the settings join the
	// admin's own sections at its end.
	fixed := siteadmin.Fixed{ContentSecurityPolicy: a.config.ContentSecurityPolicy}
	fixed.OutputCacheTTL, fixed.OutputCache = configuredOutputCache(a.config)

	// A preview is a page of the site in the admin: under an active policy
	// it allows what the site's pages need. Without one, nothing is added:
	// the sources would make directives of their own that allow only them.
	preview := func() http.Handler {
		pages := a.Frontend.Handler(a.mounted)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if mode := a.policy(r).mode; mode == site.CSPEnforce || mode == site.CSPReportOnly {
				r = a.sitePolicy(w, r)
			}

			pages.ServeHTTP(w, r)
		})
	}

	// The settings and the theme share the setup guide, which places the
	// mountable modules on pages and tests the site's mail.
	siteOptions := siteadmin.Options{
		Media:    a.Media,
		Fixed:    fixed,
		Frontend: a.Frontend,
		Public:   preview,
		Pages:    a.Pages,
		Modules:  a.mountable,
		Mail:     a.mail(),
		BaseURL:  a.config.BaseURL,
	}

	settings := siteadmin.New(a.Site, siteOptions)
	settings.Group = admin.GroupSystem
	theme := siteadmin.NewTheme(a.Site, siteOptions)
	theme.Group = admin.GroupSystem

	// The modules add their links as they are set up.
	a.Links = links.New(a.Pages, a.Site, links.Options{
		BaseURL:    a.config.BaseURL,
		IsCorePath: a.IsCorePath,
		Version:    a.output.seen,
		TTL:        cmp.Or(a.cacheTTL(), -1),
	})

	sections := []admin.Section{
		pageadmin.New(a.Pages, a.Site, a.Auth, a.Frontend, a.mountable, a.Links),
		mediaadmin.New(a.Media, a.Site),
		settings,
		theme,
	}

	// The modules' additions to the pages' structured data, in their order.
	var structured []frontend.StructuredDataFunc

	for _, module := range a.modules {
		actions := a.actionMount(module.Key)

		parts, err := module.Setup(Env{
			Key:           module.Key,
			Title:         module.Title,
			DB:            a.DB,
			Locales:       a.Site,
			Site:          a.Site,
			Auth:          a.Auth,
			Media:         a.Media,
			Pages:         a.Pages,
			Frontend:      a.Frontend,
			Mail:          a.mail(),
			BaseURL:       a.config.BaseURL,
			AdminLanguage: a.config.AdminLanguage,
			app:           a,
			actions:       actions,
		})
		if err != nil {
			return fmt.Errorf("module %s: %w", module.Key, err)
		}

		for _, section := range parts.Admin {
			section.Group = cmp.Or(module.group, section.Group, admin.GroupModules)
			sections = append(sections, section)

			// A successful post there changes what visitors may see.
			a.output.changing = append(a.output.changing, a.config.AdminPath+cmp.Or(section.Path, "/"+section.ID))
		}

		if len(parts.Feeds) > 0 {
			routes, ok := parts.Public.(*router.Router)
			if !ok {
				return fmt.Errorf("module %s: feeds need a Public router of frontend.Router", module.Key)
			}

			if err := a.Frontend.ServeFeeds(module.Key, routes, parts.Feeds); err != nil {
				return fmt.Errorf("module %s: %w", module.Key, err)
			}
		}

		if parts.Public != nil {
			a.mounted[module.Key] = parts.Public
		}

		a.apis[module.Key] = parts.Service

		actions.limit(parts.ActionBodyLimit)

		if parts.Middleware != nil {
			a.wrappers = append(a.wrappers, parts.Middleware)
		}

		if parts.Close != nil {
			a.closers = append(a.closers, parts.Close)
		}

		if parts.Sitemap != nil {
			a.sitemaps[module.Key] = parts.Sitemap
		}

		if parts.Search != nil {
			a.searchers[module.Key] = parts.Search
		}

		a.schedulers = append(a.schedulers, parts.Scheduled...)
		a.addAPI(module.Key, parts.API)

		if parts.StructuredData != nil {
			structured = append(structured, parts.StructuredData)
		}

		a.Links.Use(module.Key, module.Title, parts.Links)

		if redirects, _ := a.Links.Redirects(); parts.Redirects != nil && redirects == nil {
			a.Links.SetRedirects(module.Key, parts.Redirects)
		}
	}

	a.checkModuleTemplates()

	if err := a.checkTemplates(ctx); err != nil {
		return err
	}

	a.Frontend.SetSearch(a.searchers)

	if err := a.serveAPI(); err != nil {
		return err
	}

	a.Frontend.SetStructuredData(structured)

	backoffice, err := admin.New(a.Auth, admin.Options{
		Path:         a.config.AdminPath,
		Title:        a.config.Name,
		Language:     a.config.AdminLanguage,
		CatalogueDir: filepath.Join(a.config.StateDir, "translations"),
		Secure:       !a.config.Insecure,
		Authorize:    a.config.Authorize,
		Timezone:     a.timezone,
		Diagnostics:  a.diagnostics(),
		Mail:         a.resetMail(),
		SetupToken:   a.setupToken,
		RemindSetup:  a.remindSetup,
		Review:       a.reviewed,
		Permanence:   admin.NewPermanence(a.DB),
		// The admin registers its routes on the router New mounted at its
		// path, so they become the root router's.
		Router: a.lazy.admin,
		AbsoluteURL: func(r *http.Request, path string) string {
			return frontend.AbsoluteURL(a.config.BaseURL, r, path)
		},
	}, sections...)
	if err != nil {
		return err
	}

	// Closing ends the admin's open streams, so a shutdown need not wait
	// for them.
	a.closers = append(a.closers, backoffice.Close)
	a.shutdowns = append(a.shutdowns, func() {
		_ = backoffice.Close(context.Background())
	})

	a.Frontend.SetEditors(func(r *http.Request) (frontend.SignedIn, bool) {
		user, can, ok := backoffice.Editor(r)
		if !ok {
			return frontend.SignedIn{}, false
		}

		return frontend.SignedIn{
			UserID: user.ID,
			Name:   user.DisplayName(),
			CanEdit: func(adminURL string) bool {
				return can(adminURL, admin.Edit)
			},
			CanView: func(adminURL string) bool {
				return can(adminURL, admin.View)
			},
			AdminURL: backoffice.TryURL,
		}, true
	})

	// Restoring from the trash may bring back module content.
	a.output.changing = append(a.output.changing, backoffice.URL("admin.trash"))

	a.serveRoutes(requestWide, accessLog)
	a.backoffice = backoffice

	a.handler = a.root
	a.started.Store(true)

	background, stop := context.WithCancel(context.Background())
	a.stop = stop

	a.startScheduler(background)
	a.startCleanup(background)
	a.stopped.Go(func() {
		a.Media.Prepare(background)
	})
	a.announceSetup(addr)

	return nil
}

// SetupToken is the secret of this start's first-run setup link,
// <AdminPath>/setup?token=<SetupToken>, which Start makes when the site has no
// accounts and logs; empty when accounts existed. A new one is made at every
// start, and the setup answers 404 once any account exists. Tests and tools
// that create the first account through the admin use it.
func (a *App) SetupToken() string {
	return a.setupToken
}

// announceSetup logs the first-run setup link, on BaseURL or else on addr,
// the address ListenAndServe listens on; with neither it logs the path.
func (a *App) announceSetup(addr string) {
	if addr != "" {
		a.listening = addr
	}

	a.logSetup(slog.LevelInfo, addr)
}

// logSetup logs the first-run setup link at level, as announceSetup
// describes it. The link is never made from a request: a forged Host
// header would send the token to another site.
func (a *App) logSetup(level slog.Level, addr string) {
	if a.setupToken == "" {
		return
	}

	origin := strings.TrimSuffix(a.config.BaseURL, "/")
	if origin == "" && addr != "" {
		origin = "http://" + localAddress(addr)
	}

	slog.Log(context.Background(), level, "no account exists yet: open this link to create the first administrator account",
		"setup", origin+a.backoffice.URL("admin.setup", url.Values{"token": {a.setupToken}}),
	)
}

// setupReminder is how often remindSetup logs the setup link at most.
const setupReminder = time.Minute

// remindSetup logs the first-run setup link again, at warning level, when
// the sign-in page is shown while no account exists: whoever runs the site
// finds it at the end of the log. It logs at most once per setupReminder,
// so visitors cannot flood the log.
func (a *App) remindSetup() {
	now := time.Now().UnixNano()
	last := a.setupReminded.Load()

	if last != 0 && time.Duration(now-last) < setupReminder {
		return
	}

	if !a.setupReminded.CompareAndSwap(last, now) {
		return
	}

	a.logSetup(slog.LevelWarn, a.listening)
}

// localAddress is the host and port to open a listening address addr in a
// browser on the same machine: ":8080" and "0.0.0.0:8080" are
// localhost:8080.
func localAddress(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}

	if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
		host = "localhost"
	}

	return net.JoinHostPort(host, port)
}

// resetMail sends the admin's password reset links. Outside local
// development (Insecure) they need Mail and a BaseURL: links made from the
// request's Host header could be pointed at another site by whoever asks for
// one. Without them the admin offers no password reset.
func (a *App) resetMail() mail.Sender {
	switch {
	case a.config.Insecure:
		return a.mail()
	case a.config.Mail == nil || a.config.BaseURL == "":
		return nil
	default:
		return a.config.Mail
	}
}

// cleanupInterval is how often expired sessions, login attempts, password
// reset links and visitors' kept posts are removed.
const cleanupInterval = time.Hour

// startCleanup removes expired sessions, login attempts, reset links and
// visitors' kept posts (frontend.Frontend.CleanupForms) in the background:
// once at start, then every hour until Close.
func (a *App) startCleanup(ctx context.Context) {
	a.stopped.Go(func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for {
			if err := a.Auth.Cleanup(ctx); err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "remove expired sessions", "error", err)
			}

			if err := a.Frontend.CleanupForms(ctx); err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "remove expired visitor forms", "error", err)
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
}

// timezone is the site's timezone from its settings, UTC when unknown.
func (a *App) timezone(ctx context.Context) *time.Location {
	settings, err := a.Site.Settings(ctx)
	if err != nil {
		return time.UTC
	}

	return settings.Location()
}

// startScheduler carries out scheduled publication changes in the
// background, every ScheduleInterval, until ctx ends (at Close).
func (a *App) startScheduler(ctx context.Context) {
	interval := a.config.ScheduleInterval
	if interval == 0 {
		interval = 30 * time.Second
	}

	if interval < 0 {
		return
	}

	a.stopped.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := a.RunSchedules(ctx); err != nil && ctx.Err() == nil {
					slog.ErrorContext(ctx, "scheduled publication", "error", err)
				}
			}
		}
	})
}

// RunSchedules publishes and unpublishes the pages and module content whose
// scheduled time has come, as the background scheduler does every
// ScheduleInterval, and returns the number of changes. Every change has the
// same effects as publishing by hand. Several processes may run it at once:
// each change is made by one of them.
func (a *App) RunSchedules(ctx context.Context) (int, error) {
	settings, err := a.Site.Settings(ctx)
	if err != nil {
		return 0, err
	}

	now := time.Now()

	// Scheduled publications follow the review workflow like publishing by
	// hand: while it is on, only approved revisions go live.
	ctx = content.WithReview(ctx, a.reviewed)

	var (
		changes int
		errs    []error
	)

	for _, scheduler := range append([]content.Scheduler{a.Pages}, a.schedulers...) {
		count, err := scheduler.RunDue(ctx, now, settings.DefaultLocale)
		changes += count

		if err != nil {
			errs = append(errs, err)
		}
	}

	// The pages move their own generation on; the modules' content may have
	// changed too.
	if changes > 0 {
		a.output.changed()
	}

	return changes, errors.Join(errs...)
}

// reviewed is the site's review policy (see content.WithReview): a
// section's review workflow is on when the site settings list it (see
// site.Settings.Review). Revisioned content is reviewed per instance, whose
// admin section has the instance's key. When the settings cannot be read,
// publishing waits for approval rather than skipping it.
func (a *App) reviewed(ctx context.Context, section string) bool {
	settings, err := a.Site.Settings(ctx)
	if err != nil {
		return true
	}

	return settings.Reviewed(section)
}

func (a *App) schemas(ctx context.Context) error {
	core := []struct {
		owner  string
		schema fs.FS
	}{
		{"auth", auth.Schema()},
		{"media", media.Schema()},
		{"site", site.Schema()},
		{"pages", pages.Schema()},
		{"admin", admin.Schema()},
		{"frontend", frontend.Schema()},
	}

	for _, part := range core {
		if err := a.applySchema(ctx, part.owner, part.schema); err != nil {
			return err
		}
	}

	// Instances of one module type share its tables.
	for _, module := range a.modules {
		if module.Schema == nil {
			continue
		}

		if err := a.applySchema(ctx, "module:"+module.Type, module.Schema); err != nil {
			return err
		}
	}

	return nil
}

func (a *App) services(ctx context.Context) error {
	var err error

	a.Auth, err = auth.New(a.DB)
	if err != nil {
		return err
	}

	if a.config.AdminEmail != "" {
		err := a.Auth.Bootstrap(ctx, a.config.AdminEmail, a.config.AdminPassword)
		if err != nil && !errors.Is(err, auth.ErrBootstrap) {
			return fmt.Errorf("create first admin: %w", err)
		}
	}

	a.storage = a.config.Storage
	if a.storage == nil {
		// The media routes serve the files (see routes).
		a.storage, err = media.NewFilesystem(filepath.Join(a.config.StateDir, "media"), "")
		if err != nil {
			return err
		}
	}

	a.Media = media.New(a.DB, a.storage)
	a.Media.RegisterRoutes(a.lazy.media)
	a.mediaOrigin = storageOrigin(a.storage)
	a.Site = site.New(a.DB)
	a.Site.SetCacheTTL(a.cacheTTL())

	// The app's own addresses would hide the pages of a language whose
	// code is their first segment, such as an admin at /en: no such
	// language can be enabled.
	a.Site.ReserveLocales(a.coreSegments()...)

	if err := a.Site.Init(ctx, a.config.Name, a.config.Locales); err != nil {
		// On a first start the languages come from Config.Locales: say why
		// the reserved one is refused.
		code, reserved := a.Site.ReservedLocale(a.config.Locales.Enabled)
		if reserved && errors.Is(err, site.ErrReservedLocale) {
			return a.reservedLocale(code)
		}

		return err
	}

	settings, err := a.Site.Settings(ctx)
	if err != nil {
		return err
	}

	if code, reserved := a.Site.ReservedLocale(settings.Locales); reserved {
		return a.reservedLocale(code)
	}

	// Variant URLs must be the same in every process of the site.
	key, err := a.Site.Secret(ctx, "media_variants")
	if err != nil {
		return err
	}

	a.Media.SetSigningKey(key)

	// Top-level pages keep away from the app's own addresses.
	a.Pages, err = pages.New(a.DB, a.Site, a.mountableKeys, a.coreSegments()...)
	if err != nil {
		return err
	}

	a.Pages.SetCacheTTL(a.cacheTTL())

	if err := a.Pages.Init(ctx, nil); err != nil {
		return err
	}

	// Also a Renderer of its own gets the functions: components, mail and
	// the site's translations of labels use them.
	funcs, err := a.templateFuncs()
	if err != nil {
		return err
	}

	switch {
	case a.config.Renderer != nil:
		a.Frontend = frontend.New(a.Site, a.Pages, a.Media, a.config.Renderer, funcs...)
	case a.config.Templates != nil:
		themes, err := frontend.LoadThemes(a.config.Templates)
		if err != nil {
			return err
		}

		a.Frontend = frontend.Themed(a.Site, a.Pages, a.Media, themes, funcs...)
	default:
		a.Frontend = frontend.New(a.Site, a.Pages, a.Media, frontend.Missing(), funcs...)
	}

	a.Frontend.SetReload(a.config.ReloadTemplates)
	a.Frontend.SetFormTheme(a.config.FormTheme)
	a.Frontend.SetTranslations(a.config.Translations)
	a.Frontend.SetBaseURL(a.config.BaseURL)
	a.Frontend.SetRoutes(a.root)
	a.Frontend.SetDiagnostics(a.diagnostics())
	a.Frontend.SetDatabase(a.DB)

	return nil
}

// reservedLocale says why the enabled language code cannot be served: the
// admin's path starts with it, or another of the app's own addresses is
// /<code>, such as /media.
func (a *App) reservedLocale(code string) error {
	segment, _, _ := strings.Cut(strings.TrimPrefix(a.config.AdminPath, "/"), "/")
	if strings.EqualFold(code, segment) {
		return fmt.Errorf("admin path %s is the address of the enabled language %s (%w): choose another admin path",
			a.config.AdminPath, code, site.ErrReservedLocale)
	}

	return fmt.Errorf("the enabled language %s has the address of one of the app's own paths, /%s (%w)",
		code, code, site.ErrReservedLocale)
}

// cacheTTL is Config.CacheTTL for the services: 0 disables their cache.
func (a *App) cacheTTL() time.Duration {
	switch {
	case a.config.CacheTTL < 0:
		return 0
	case a.config.CacheTTL == 0:
		return 30 * time.Second
	default:
		return a.config.CacheTTL
	}
}

// templateFuncs are the modules' functions followed by the site's own, which
// may override them. Two module types offering one name is an error, and so
// is a name go-partial keeps for itself, such as url, content, locale or
// localizer (see render.Reserved), which templates would never see.
func (a *App) templateFuncs() ([]template.FuncMap, error) {
	modules := template.FuncMap{}
	owners := map[string]string{}

	for _, module := range a.modules {
		for name, fn := range module.Funcs {
			if owner, ok := owners[name]; ok && owner != module.Type {
				return nil, fmt.Errorf("template function %q is offered by %s and %s", name, owner, module.Type)
			}

			if render.Reserved(name) {
				return nil, fmt.Errorf("module %s: %w: %q", module.Type, render.ErrReserved, name)
			}

			owners[name] = module.Type
			modules[name] = fn
		}
	}

	for _, funcs := range a.config.Funcs {
		for name := range funcs {
			if render.Reserved(name) {
				return nil, fmt.Errorf("site template functions: %w: %q", render.ErrReserved, name)
			}
		}
	}

	funcs := []template.FuncMap{modules}

	// The site's own functions may replace those of the import map.
	// {{importmap}} writes the import map whose hash the page's policy
	// has (see importMapHead).
	if a.config.ImportMap != nil {
		funcs = append(funcs, a.config.ImportMap.FuncMap(), template.FuncMap{"importmap": a.importMapHead})
	}

	return append(funcs, a.config.Funcs...), nil
}

// actionMount is a module's action router (Env.Actions), mounted on the
// action router of the root (see routes) at the module's key when the
// module first asks for it.
type actionMount struct {
	parent *router.Router
	key    string
	routes *router.Router
	mount  *router.RouteRegistration
}

// actionMount is the action mount of the module with key, mounted on the
// first call of its router.
func (a *App) actionMount(key string) *actionMount {
	return &actionMount{parent: a.lazy.actions, key: key}
}

// router is the module's action router, mounted at /<key> below the
// action router, its route names with the key in front.
func (m *actionMount) router() *router.Router {
	if m.routes == nil {
		m.routes = router.NewSite()
		m.mount = m.parent.Mount("/"+m.key, m.routes, router.NamePrefix(m.key+".")).Use(m.module)
	}

	return m.routes
}

// module tells the requests of the module's actions which module they are
// for (frontend.WithActionModule), so a failing action is logged with it.
func (m *actionMount) module(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(frontend.WithActionModule(r.Context(), m.key)))
	})
}

// limit limits the request bodies of the module's actions to bytes, or to
// DefaultActionBodyLimit when bytes is zero or less; a module without
// actions has nothing to limit.
func (m *actionMount) limit(bytes int64) {
	if m.mount == nil {
		return
	}

	if bytes <= 0 {
		bytes = DefaultActionBodyLimit
	}

	m.mount.Use(middleware.BodyLimit(bytes))
}

// checkModuleTemplates warns about public templates the site does not have
// yet, with the command that writes starting points for them in a site
// made from go-webthings-clone: task templates -- <key>, which calls
// WriteTemplates.
func (a *App) checkModuleTemplates() {
	types := map[string]string{}

	for _, module := range a.modules {
		types[module.Key] = module.Type
	}

	a.Frontend.SetModules(types, a.apis)

	themes := []string{frontend.DefaultTheme}
	if list := a.Frontend.Themes().List(); len(list) > 0 {
		themes = themes[:0]
		for _, theme := range list {
			themes = append(themes, theme.Name)
		}
	}

	for _, module := range a.modules {
		if a.mounted[module.Key] == nil {
			continue
		}

		for _, template := range module.Templates {
			for _, theme := range themes {
				name := a.Frontend.Template(theme, module.Key, template.Name)
				if a.Frontend.Has(theme, name) {
					continue
				}

				slog.Warn("missing template",
					"theme", theme,
					"template", module.Key+"/"+template.Name+".gohtml",
					"or", module.Type+"/"+template.Name+".gohtml",
					"generate", "task templates -- "+module.Key,
				)
			}
		}
	}
}

// WriteTemplates writes starting points for templates into dir: for a module
// instance key the module's templates and components in dir/<key>/ and its
// mail templates in dir/mail/<key>/, for "site" the layout with its head
// region (regions/head.gohtml, core's tags and room for the theme's own;
// see frontend.HeadScaffold), the stylesheet the layout links
// (assets/site.css), the page, 404 and search templates and core's
// components in dir/site/. Existing files are never overwritten.
// It returns the files it wrote and needs no Start.
func (a *App) WriteTemplates(dir, key string) ([]string, error) {
	files := map[string]string{}

	if key == "site" {
		files["layout.gohtml"] = frontend.LayoutScaffold()
		files[filepath.Join("regions", "head.gohtml")] = frontend.HeadScaffold()
		files[filepath.Join("assets", "site.css")] = frontend.StyleScaffold()
		files["page.gohtml"] = frontend.Scaffold(frontend.Template{Data: frontend.Page{}})
		files["404.gohtml"] = frontend.Scaffold(frontend.Template{Data: frontend.NotFound{}})
		files["search.gohtml"] = frontend.Scaffold(frontend.SearchTemplate())

		for _, component := range frontend.CoreComponents() {
			files[filepath.Join("site", component.Name+".gohtml")] = frontend.Scaffold(component)
		}
	}

	for _, module := range a.modules {
		if module.Key != key {
			continue
		}

		for _, template := range module.Templates {
			files[filepath.Join(key, template.Name+".gohtml")] = frontend.Scaffold(template)
		}

		for _, template := range module.Mail {
			files[filepath.Join("mail", key, template.Name+".gohtml")] = frontend.MailScaffold(template)
		}

		for _, component := range module.Components {
			files[filepath.Join(key, component.Name+".gohtml")] = frontend.Scaffold(component)
		}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no templates for %q: use one of %s", key, strings.Join(a.TemplateKeys(), ", "))
	}

	var written []string

	for name, source := range files {
		path := filepath.Join(dir, name)

		if _, err := os.Stat(path); err == nil {
			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return written, err
		}

		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			return written, err
		}

		written = append(written, path)
	}

	slices.Sort(written)

	return written, nil
}

// TemplateKeys lists the keys WriteTemplates accepts: "site", then the keys
// of the modules with templates, in the order they were added. A site's
// command line can show them when no key is given.
func (a *App) TemplateKeys() []string {
	keys := []string{"site"}

	for _, module := range a.modules {
		if len(module.Templates)+len(module.Mail)+len(module.Components) > 0 {
			keys = append(keys, module.Key)
		}
	}

	return keys
}

// mountableKeys lists the modules that can be placed on a page.
func (a *App) mountableKeys() []string {
	var keys []string

	for _, module := range a.mountable() {
		keys = append(keys, module.Key)
	}

	return keys
}

// mountable lists modules with a public handler, in the order they were added.
func (a *App) mountable() []pageadmin.Module {
	var modules []pageadmin.Module

	for _, module := range a.modules {
		if a.mounted[module.Key] != nil {
			modules = append(modules, pageadmin.Module{Key: module.Key, Title: module.Title})
		}
	}

	return modules
}

// lazyHandlers are the handlers of the root router's routes that need what
// Start builds. New registers the routes with handlers that pass requests
// on to these (see lazy), so the routes, and with them CorePaths, exist
// from New on; Start sets them before the root router serves.
type lazyHandlers struct {
	sitemap http.Handler
	assets  http.Handler
	theme   http.Handler
	// themeStyles serves the themes' values stylesheets (see
	// frontend.Frontend.ThemeStyles).
	themeStyles http.Handler
	// site is the public site, the catch-all; siteRoute is its route,
	// which Start gives the site's middleware.
	site      http.Handler
	siteRoute *router.RouteRegistration
	// admin is the router mounted at Config.AdminPath that the admin
	// registers its routes on; adminErr is why the admin path could not be
	// mounted, which Start reports.
	admin    *router.Router
	adminErr error
	// actions is the router mounted at /_ that the modules' visitor
	// actions are mounted on (see Env.Actions).
	actions *router.Router
	// media is the router mounted at /media that the media library
	// registers its routes on.
	media *router.Router
}

// lazy passes requests on to the handler *handler holds when they come.
func lazy(handler *http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		(*handler).ServeHTTP(w, r)
	})
}

// routes builds the app's root router, the one router every request passes
// once the app has started (see the package documentation): the endpoints
// for operations, then behind compression the modules' visitor actions
// below /_, media, the site's assets, theme files, core's stylesheet,
// sitemap.xml and robots.txt, the admin at Config.AdminPath and, as the
// catch-all "/", the site. Every registration but the site's is labelled
// with its route class, which makes it one of CorePaths and counts its
// requests in the metrics (see class). Start adds what needs its services
// (see serveRoutes).
func (a *App) routes() *router.Router {
	root := router.NewSite()

	// The metrics and the access log class requests by the route that
	// served them (see class).
	root.TrackRoutes()

	// First, so the admin path is checked against them too (see
	// mountAdmin).
	a.operations(root)
	a.apiRoutes(root)

	root.With(func(routes *router.Router) {
		// Everything below reads one snapshot of the site settings per
		// request.
		routes.Use(a.snapshots)

		// The site's middleware sees every request for a page, also one
		// the output cache answers, such as the modules' statistics (see
		// visitors).
		routes.Use(a.visitors)

		// Pages for visitors are kept in memory, compressed and with their
		// ETag (see outputCache).
		routes.Use(a.output.middleware)

		// Responses are compressed by the site setting.
		routes.Use(middleware.Compress(middleware.CompressOptions{Enabled: a.compress}))

		// Each module's actions are mounted below /_ when the module asks
		// for them (Env.Actions), behind the protection against cross-site
		// posts. What they answer may be a page, which other sites may
		// frame as the site setting says (see framing).
		a.lazy.actions = router.NewSite()
		routes.Mount("/_", a.lazy.actions).
			Label(classAction).
			Use(a.output.visitorChanges, http.NewCrossOriginProtection().Handler, a.contentSecurity(a.sitePolicy), a.framing)

		// Browsers report what the Content-Security-Policy blocks here; the
		// reports are logged, and change nothing.
		routes.Post(cspReportPath, cspReports().ServeHTTP).As("csp.report").Label(classAction)

		// The media library registers its routes at Start, their names
		// starting with media., such as media.variant.
		a.lazy.media = router.NewSite()
		routes.Mount("/media", a.lazy.media, router.NamePrefix("media.")).
			Label(classMedia)

		routes.Get("/assets/{file...}", lazy(&a.lazy.assets).ServeHTTP).As("assets").Label(classAsset)
		routes.Get("/theme/{file...}", lazy(&a.lazy.theme).ServeHTTP).As("theme").Label(classAsset)
		routes.Get("/theme/{theme}/values.css", lazy(&a.lazy.themeStyles).ServeHTTP).As("theme.values").Label(classAsset)
		routes.Get("/_/webthings.css", frontend.CoreAssets("webthings.css").ServeHTTP).As("webthings.css").Label(classAsset)
		routes.Get("/_/focal-points.css", frontend.CoreAssets("focal-points.css").ServeHTTP).
			As("focal-points.css").
			Label(classAsset)
		routes.Get("/_/partial-errors.css", frontend.CoreAssets("partial-errors.css").ServeHTTP).
			As("partial-errors.css").
			Label(classAsset)
		routes.Get("/sitemap.xml", lazy(&a.lazy.sitemap).ServeHTTP).As("sitemap").Label(classSEO)
		robots := frontend.Robots(a.config.BaseURL, routes.RoutePath("sitemap"))
		routes.Get("/robots.txt", robots.ServeHTTP).As("robots").Label(classSEO)

		a.mountAdmin(routes, root)

		// The site is the catch-all: pages live in the database and can
		// move, so it resolves the page itself and serves the module on it
		// below the page (see frontend.Router). Other sites may frame its
		// pages, also its 404, as the site setting says (see framing). A
		// visitor's post, such as a module's form on its page, may change
		// what the pages show.
		a.lazy.siteRoute = routes.Handle("/", lazy(&a.lazy.site)).
			Use(a.output.visitorChanges, a.contentSecurity(a.sitePolicy), a.framing)
	})

	return root
}

// mountAdmin mounts the router the admin registers its routes on, whose
// patterns are their full paths, at Config.AdminPath. A path that is not
// clean is left to the admin, which refuses it; one that is another core
// address, or above or below one, such as /healthz or /readyz/admin, Start
// refuses. The core addresses are the root router's registrations so far,
// so the routes registers the endpoints for operations first. Every core
// address is checked by its path rather than with router.Router.OwnsLabel,
// which does not see an admin below a route without a wildcard, such as
// /readyz/admin.
func (a *App) mountAdmin(routes, root *router.Router) {
	adminPath := a.config.AdminPath
	if !cleanAdminPath(adminPath) {
		return
	}

	for _, core := range corePaths(root) {
		if adminPath == core || strings.HasPrefix(adminPath, core+"/") || strings.HasPrefix(core, adminPath+"/") {
			a.lazy.adminErr = fmt.Errorf("admin path %s is one of the core paths", adminPath)

			return
		}
	}

	admin := router.NewSite()

	mount, err := routes.TryMount(adminPath, admin, router.AbsolutePaths())
	if err == nil {
		err = mount.TryLabel(classAdmin)
	}

	if err != nil {
		a.lazy.adminErr = fmt.Errorf("admin path %s: %w", adminPath, err)

		return
	}

	// Saves in the admin sections of modules change what visitors see.
	// The admin has the site's policy with the hash of its own import map;
	// it sets frame-ancestors itself.
	mount.Use(a.output.adminChanges, a.contentSecurity(a.adminPolicy))

	a.lazy.admin = admin
}

// serveRoutes completes the root router at Start: the request-wide
// middleware, the security headers (see securityHeaders), the observation
// of every request but those to the endpoints for operations (see observe)
// and the recovery from panics around it with UseAll, the handlers New's routes pass requests on to and the site's
// middleware.
func (a *App) serveRoutes(requestWide []router.Middleware, accessLog *slog.Logger) {
	// Recovery runs inside the observation, so a request whose handler
	// panicked is logged, counted and traced with its 500.
	all := slices.Concat(requestWide, []router.Middleware{securityHeaders, a.observe(accessLog), middleware.Recover})
	a.root.UseAll(all...)

	// The sitemap follows what visitors see, so the output cache never
	// keeps an older one for a newer version.
	a.lazy.sitemap = a.Frontend.Sitemap(a.config.BaseURL, a.sitemaps, a.output.seen)
	a.lazy.theme = a.Frontend.Assets()
	a.lazy.themeStyles = a.Frontend.ThemeStyles()
	a.lazy.assets = a.assetFiles()
	a.lazy.site = a.Frontend.Handler(a.mounted)

	// The page cache sees the headers the handlers set; the editors and
	// the modules' middleware are outside the output cache (see visitors).
	a.lazy.siteRoute.Use(a.pageCache)
}

// visitors runs the site's own middleware for the requests of the site's
// pages, before the output cache, so it sees every request, also one the
// cache answers: Frontend.Editors first, so module middleware can tell
// editors from visitors, then the modules' middleware (Parts.Middleware),
// in the order the modules were added. Their responses come from the output
// cache, compressed when the visitor accepts it (Content-Encoding). Requests
// of the core routes, such as the admin and the modules' actions, pass
// alone.
func (a *App) visitors(next http.Handler) http.Handler {
	var (
		once sync.Once
		site http.Handler
	)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.class(r) != classPublic {
			next.ServeHTTP(w, r)

			return
		}

		// The middleware is known from Start on, before the app serves.
		once.Do(func() {
			site = next

			for i := len(a.wrappers) - 1; i >= 0; i-- {
				site = a.wrappers[i](site)
			}

			site = a.Frontend.Editors(site)
		})

		site.ServeHTTP(w, r)
	})
}

// securityHeaders are the headers of every response once the app has
// started, also of go-router's own redirects to clean paths and of the
// endpoints for operations: browsers must not take a response, such as an
// upload, for another type than it has, also not a 404, and tell other
// sites only the site's address when visitors follow a link there. The
// framing headers depend on the route (see framing).
var securityHeaders = middleware.SecurityHeaders(middleware.SecurityHeadersOptions{
	ContentTypeOptions: "nosniff",
	ReferrerPolicy:     "strict-origin-when-cross-origin",
})

// Service returns the API of a started module instance, for the website's own
// code. Module packages offer typed helpers, for example publishing.Of(app, "news").
func (a *App) Service(key string) any {
	return a.apis[key]
}

// ServeHTTP serves the site through the root router once the app has
// started; before that only the endpoints for operations answer, and every
// other address with 503 (see idleRoutes).
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if a.handler == nil {
		a.idle.ServeHTTP(w, r)

		return
	}

	a.handler.ServeHTTP(w, r)
}

// idleRoutes answer before Start: the endpoints for operations (readiness
// reports not started) and 503 not_started for everything else. Requests get
// an ID here too, so a panic before Start is logged with its request_id;
// the visitor's address is not needed, and Start reports trusted proxies
// it cannot read.
func (a *App) idleRoutes() http.Handler {
	idle := router.NewSite()

	idle.UseAll(requestIDs(), middleware.Recover)
	a.operations(idle)
	idle.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not_started", http.StatusServiceUnavailable)
	}))

	return idle
}

func (a *App) mail() mail.Sender {
	if a.config.Mail != nil {
		return a.config.Mail
	}

	return mail.Log{}
}

// Close stops the background work (scheduler, cleanup and the variants of
// uploaded images, see media.Service.Prepare) and the modules, last added
// first, then closes storage and the database.
func (a *App) Close() error {
	var errs []error

	if a.stop != nil {
		a.stop()
		a.stopped.Wait()
		a.stop = nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := len(a.closers) - 1; i >= 0; i-- {
		errs = append(errs, a.closers[i](ctx))
	}

	a.closers = nil

	if closer, ok := a.storage.(interface{ Close() error }); ok {
		errs = append(errs, closer.Close())
	}

	if a.DB != nil {
		errs = append(errs, a.DB.Close())
	}

	return errors.Join(errs...)
}

// ListenAndServe starts the app (unless Start was already called, for example
// to import content first) and serves on addr until ctx is cancelled; see
// Serve.
func (a *App) ListenAndServe(ctx context.Context, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		_ = a.Close()

		return err
	}

	return a.Serve(ctx, listener)
}

// Serve is ListenAndServe on a listener the caller opened: for example one
// handed over by systemd socket activation, or one on port 0, whose address
// a test knows before the app serves. It starts the app unless Start was
// already called, serves until ctx is cancelled, lets running requests
// finish, and then closes the listener and the app.
func (a *App) Serve(ctx context.Context, listener net.Listener) error {
	addr := listener.Addr().String()

	if a.handler == nil {
		if err := a.start(ctx, addr); err != nil {
			_ = listener.Close()
			_ = a.Close()

			return err
		}
	} else if a.config.BaseURL == "" {
		// Start logged the setup link as a path; now the address is known.
		a.announceSetup(addr)
	}

	defer func() { _ = a.Close() }()

	server := &http.Server{
		Handler:           a,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       time.Minute,
	}

	for _, shutdown := range a.shutdowns {
		server.RegisterOnShutdown(shutdown)
	}

	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		<-ctx.Done()

		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = server.Shutdown(shutdown)
	}()

	slog.Info("serving", "address", addr, "admin", a.backoffice.URL("admin.dashboard"))

	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	// Serve returns as soon as Shutdown starts. Running requests
	// get their grace period before the deferred Close shuts the app.
	<-stopped

	return nil
}
