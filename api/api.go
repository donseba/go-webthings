// Package api is the site's content API: its published content as JSON,
// for apps and decoupled frontends, at /api/v1/ (Path). It is off by
// default; Config.Enabled, as webthings.Config.API, turns it on.
//
// Core serves the site, its search and its pages:
//
//	GET /api/v1/site                       the site: name, languages, menu and the modules with an API
//	GET /api/v1/site/search?q=harbour      the site search
//	GET /api/v1/pages                      the published page tree
//	GET /api/v1/pages/resolve?path=/en/about  the page at a site address, as the site resolves it
//	GET /api/v1/pages/{id}                 a page by its ID
//
// Every endpoint answers in one language: the locale parameter, such as
// ?locale=nl, or the site's default language without it; /pages/resolve
// takes the language from the address. A module adds its own endpoints
// below /api/v1/<key>/ with a router of NewRouter (webthings.Parts.API),
// such as publishing's /api/v1/news/posts.
//
// # Responses
//
// Responses are JSON objects with snake_case names, documented by the
// types of this package and the modules' API types: dates in RFC 3339 (ISO
// 8601), addresses absolute, on webthings.Config.BaseURL or the address of
// the request, images as an Image with their sizes and alt text, and
// Markdown content rendered as HTML with absolute addresses (body_html).
// A list is a List, which pages with limit (DefaultLimit, at most MaxLimit)
// and offset. A field without a value is null rather than left out, but
// for the few that only one endpoint gives, such as the body_html of a
// module's item, which its list leaves out.
//
// An error is an ErrorResponse: its HTTP status, a stable code and the
// status text in the request's language, translated with the site's
// translations (see frontend.CoreTexts). Content that is not published, or not in the
// language asked for, and an address without an endpoint, also with another
// method than GET or HEAD, answer 404 not_found; a language the site does
// not offer answers 404 unknown_locale.
//
// # Security
//
// The API shows what visitors see on the site, nothing else: only
// published content, never a draft, a scheduled revision or a preview, also
// not to signed-in editors, so every response is the same for everyone and
// the output cache keeps it. Modules expose no personal data, such as
// subscribers, registrations or form entries. Each client may make
// Config.RateLimit requests per Config.RateWindow; browsers of other sites
// may read the API only from Config.AllowedOrigins (CORS).
package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/pages"
	"github.com/donseba/go-webthings/site"

	"github.com/donseba/go-router"
	translator "github.com/donseba/go-translator/v2"
)

// Path is where the API is served: version 1 of it.
const Path = "/api/v1"

// The pages of a List: DefaultLimit items when the request names no limit,
// at most MaxLimit.
const (
	DefaultLimit = 20
	MaxLimit     = 100
)

// DefaultRateLimit is how many requests one client may make per minute
// unless Config.RateLimit says otherwise.
const DefaultRateLimit = 300

// Config is the content API of webthings.Config.API.
type Config struct {
	// Enabled serves the API at Path. Off, the default, /api/v1/ is an
	// address of the site like any other, which has no page there (404).
	Enabled bool
	// AllowedOrigins are the sites whose pages may read the API in the
	// browser (CORS), such as "https://app.example.com"; "*" allows every
	// site and "*.example.com" its subdomains, on any scheme. Empty, the default,
	// sends no CORS headers: the site's own pages, apps and servers read the
	// API, other sites' pages cannot. A request from an origin not allowed
	// is answered 403 when there are AllowedOrigins.
	AllowedOrigins []string
	// RateLimit is how many requests one client, known by its address (see
	// webthings.Config.TrustedProxies), may make per RateWindow, answers
	// from the output cache included; more are answered 429 rate_limited.
	// Zero means DefaultRateLimit, negative no limit.
	RateLimit int
	// RateWindow is the time RateLimit counts in; zero means a minute.
	RateWindow time.Duration
}

// Limit is the rate limit of the configuration: requests per window, zero
// for none.
func (c Config) Limit() (int, time.Duration) {
	limit := c.RateLimit

	switch {
	case limit < 0:
		return 0, 0
	case limit == 0:
		limit = DefaultRateLimit
	}

	window := c.RateWindow
	if window <= 0 {
		window = time.Minute
	}

	return limit, window
}

// Module is a module instance as the API knows it, for the site's list of
// modules and the module of a page.
type Module struct {
	Key   string
	Title string
	Type  string
	// API is set when the instance has endpoints of its own
	// (webthings.Parts.API), below Path/<key>.
	API bool
}

// Options are the services the API serves the site's content from.
type Options struct {
	Site     *site.Service
	Pages    *pages.Service
	Media    *media.Service
	Frontend *frontend.Frontend
	// BaseURL is webthings.Config.BaseURL, for absolute addresses; empty
	// uses the address of each request.
	BaseURL string
	// Translations are the site's (webthings.Config.Translations), which
	// translate the messages of errors; nil shows them as written.
	Translations *translator.Translator
	// Modules are the site's module instances, in the order they were
	// added.
	Modules []Module
}

// API serves the content API's own endpoints and the request scope every
// endpoint, a module's too, reads its language and helpers from.
type API struct {
	options Options
	// routes is the router the API is registered on, mounted at Path, for
	// the addresses of its endpoints by name.
	routes *router.Router
}

// New returns the API for the services of options.
func New(options Options) *API {
	return &API{options: options}
}

// NewRouter returns a router for a module's endpoints (webthings.Parts.API),
// mounted at Path/<key> on the router the API registered on, so its routes
// are relative to that and their names get "api.<key>." in front for the
// app's other routers: routes.RoutePathArgs("post", "id", id) is
// "/api/v1/news/posts/<id>". Every request without a route, any path or
// method, gets the API's 404 not_found, as JSON.
//
//	routes := api.NewRouter()
//	routes.Get("/posts", list).As("posts")
//	routes.Get("/posts/{id}", show).As("post")
func NewRouter() *router.Router {
	notFound := http.HandlerFunc(NotFound)

	return router.NewSite(router.WithNotFound(notFound), router.WithMethodNotAllowed(notFound))
}

// Register registers the API's endpoints on routes, the router mounted at
// Path (see NewRouter), behind the request scope, which modules' routers
// mounted on it later get as well. Their names are site, search, pages,
// resolve and page.
func (a *API) Register(routes *router.Router) {
	a.routes = routes

	routes.Use(a.scope)
	routes.Get("/site", a.site).As("site")
	routes.Get(strings.TrimPrefix(searchPath, Path), a.search).As("search")
	routes.Get("/pages", a.tree).As("pages")
	routes.Get("/pages/resolve", a.resolve).As("resolve")
	routes.Get("/pages/{id}", a.page).As("page")
}

// searchPath is the address of the site search (see IsSearch).
const searchPath = Path + "/site/search"

// IsSearch reports whether r is a search of the site search with a query,
// which caches leave alone: its answers are as many as the queries.
func IsSearch(r *http.Request) bool {
	return r.URL.Path == searchPath && r.URL.RawQuery != "" && r.URL.Query().Get("q") != ""
}

// Error answers a request outside the API's routes, such as one over the
// rate limit, with an error (see the function Error), in the language of
// its locale parameter when the site offers it, else in the site's
// default language.
func (a *API) Error(w http.ResponseWriter, r *http.Request, status int, code string) {
	snapshot, err := a.options.Site.Snapshot(r.Context())
	if err != nil {
		Error(w, r, status, code)

		return
	}

	settings := snapshot.Settings()
	current := scope{api: a, settings: settings, locale: settings.DefaultLocale}

	if locale, err := settings.LocaleConfig().Require(r.URL.Query().Get("locale")); err == nil {
		current.locale = locale
	}

	Error(w, r.WithContext(withScope(r.Context(), current)), status, code)
}

// scope is what a request of the API knows: the API, the site settings it
// is served with and its language.
type scope struct {
	api      *API
	settings site.Settings
	locale   string
}

type scopeKey struct{}

// scope puts the request's scope in its context: its language is the
// locale parameter, or the site's default language without one; a
// language the site does not offer answers 404 unknown_locale.
func (a *API) scope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snapshot, err := a.options.Site.Snapshot(r.Context())
		if err != nil {
			Fail(w, r, err)

			return
		}

		settings := snapshot.Settings()
		current := scope{api: a, settings: settings, locale: settings.DefaultLocale}

		if asked := r.URL.Query().Get("locale"); asked != "" {
			locale, err := settings.LocaleConfig().Require(asked)
			if err != nil {
				Error(w, r.WithContext(withScope(r.Context(), current)), http.StatusNotFound, CodeUnknownLocale)

				return
			}

			current.locale = locale
		}

		next.ServeHTTP(w, r.WithContext(withScope(r.Context(), current)))
	})
}

// withScope is ctx with the scope of a request.
func withScope(ctx context.Context, current scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, current)
}

// scopeOf is the scope of a request of the API; ok is false outside the
// API, such as in a test that calls a handler itself.
func scopeOf(r *http.Request) (scope, bool) {
	current, ok := r.Context().Value(scopeKey{}).(scope)

	return current, ok
}

// Locale is the language a request of the API is answered in: its locale
// parameter, or the site's default language. Outside the API it is "".
func Locale(r *http.Request) string {
	current, _ := scopeOf(r)

	return current.locale
}

// URL makes path, an address of the site such as "/en/news/hello", absolute
// on webthings.Config.BaseURL or the address of the request; "" stays "".
func URL(r *http.Request, path string) string {
	if path == "" {
		return ""
	}

	base := ""
	if current, ok := scopeOf(r); ok {
		base = current.api.options.BaseURL
	}

	return frontend.AbsoluteURL(base, r, path)
}
