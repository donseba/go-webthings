package webthings

import (
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/donseba/go-webthings/api"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/internal/ops"

	"github.com/donseba/go-router/middleware"
)

// outputCache keeps the public pages visitors get in memory and serves them
// again without rendering them (go-router's middleware.Cache), for as long
// as the site setting says (site.Settings.OutputCache, or
// Config.OutputCacheTTL).
//
// What it keeps: responses to GET requests for the site's pages, the
// modules' feeds (Parts.Feeds), sitemap.xml and the content API (Config.API,
// but for its searches with a query) with status 200, for visitors, by address and by the
// request headers the response varies on (such as HTMX's); not the
// admin, media, assets (which have their own caching) or the modules'
// visitor actions, not a search with a query, and not for a request that
// may get a page of its own: one with an editor's session, a kept post
// (frontend.KeptFormCookie) or a preview of a revision. A cookie a module
// registers with Env.PersonalCookie keeps a page per value of that
// cookie. Pages that a handler marks private or no-store, or that set a
// cookie, are never kept.
//
// When it is out of date: every entry is kept with the version of what
// visitors see (see version), which every change in this process moves
// on: publishing pages, saving settings or theme values, changes in the
// media library, and the module content generation, which successful posts
// to the admin sections of modules and the trash, visitors' posts to the
// site and its actions unless they changed nothing (see visitorChanges),
// scheduled publication and App.Changed and Env.Changed raise, which the
// bundled modules' stores and services call after their writes, also
// those made from Go.
// Changes by other processes, and what changes with time alone, such as
// content published at its planned time by another process or an event
// that has passed, show when the entry's time has passed.
type outputCache struct {
	app *App
	// content is the module content generation (see changed).
	content atomic.Uint64
	// current is the cache of the settings in use; mu guards replacing it,
	// and retired holds the counts of the caches it replaced, for the
	// metrics.
	current atomic.Pointer[outputCacheState]
	mu      sync.Mutex
	retired middleware.CacheStats
	// personal are the cookies that change what public pages show to the
	// visitor who has them (see Env.PersonalCookie), and changing the paths
	// in the admin where a successful post changes what visitors see (see
	// adminChanges); both are set at Start, before the app serves.
	personal []string
	changing []string
}

// outputCacheState is a cache with the time and memory it was made with.
type outputCacheState struct {
	ttl      time.Duration
	maxBytes int64
	cache    *middleware.ResponseCache
}

// cacheStatusHeader says, in development, how the output cache answered a
// request: hit, miss or skip.
const cacheStatusHeader = "X-Cache"

// middleware serves the requests below it from the cache in use, or passes
// them on when the cache is off.
func (o *outputCache) middleware(next http.Handler) http.Handler {
	// The handler of the cache in use, made again when the cache is
	// replaced.
	var wrapped atomic.Pointer[outputCacheHandler]

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := o.state(r)
		if state == nil {
			next.ServeHTTP(w, r)

			return
		}

		handler := wrapped.Load()
		if handler == nil || handler.cache != state.cache {
			handler = &outputCacheHandler{cache: state.cache, handler: state.cache.Middleware(next)}
			wrapped.Store(handler)
		}

		handler.handler.ServeHTTP(w, r)
	})
}

// outputCacheHandler is a handler behind a cache.
type outputCacheHandler struct {
	cache   *middleware.ResponseCache
	handler http.Handler
}

// state returns the cache for the settings r is served with, made when the
// settings changed; nil when the cache is off.
func (o *outputCache) state(r *http.Request) *outputCacheState {
	ttl, maxBytes := o.settings(r)
	if ttl <= 0 {
		return nil
	}

	current := o.current.Load()
	if current != nil && current.ttl == ttl && current.maxBytes == maxBytes {
		return current
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	current = o.current.Load()
	if current != nil && current.ttl == ttl && current.maxBytes == maxBytes {
		return current
	}

	if current != nil {
		o.retire(current.cache.Stats())
	}

	options := middleware.CacheOptions{
		Key:       o.key,
		Version:   o.version,
		Skip:      o.skip,
		TTL:       ttl,
		MaxBytes:  maxBytes,
		VaryValue: o.varyValue,
	}

	if o.app.config.Development {
		options.StatusHeader = cacheStatusHeader
	}

	current = &outputCacheState{ttl: ttl, maxBytes: maxBytes, cache: middleware.NewCache(options)}
	o.current.Store(current)

	return current
}

// retire adds the counts of a cache that is replaced to retired.
func (o *outputCache) retire(stats middleware.CacheStats) {
	o.retired.Hits += stats.Hits
	o.retired.Misses += stats.Misses
	o.retired.Skips += stats.Skips
	o.retired.Evictions += stats.Evictions
}

// settings are how long the cache keeps a page and how many bytes it keeps:
// Config.OutputCacheTTL when set, otherwise the site setting, which
// Development and ReloadTemplates turn off. A ttl of zero is off.
func (o *outputCache) settings(r *http.Request) (ttl time.Duration, maxBytes int64) {
	configured, fixed := configuredOutputCache(o.app.config)
	if fixed && configured == 0 {
		return 0, 0
	}

	snapshot, err := o.app.Site.Snapshot(r.Context())
	if err != nil {
		return 0, 0
	}

	ttl, maxBytes = snapshot.Settings().OutputCache()

	if fixed {
		return configured, maxBytes
	}

	return ttl, maxBytes
}

// configuredOutputCache is how long the output cache keeps pages when the
// configuration decides it rather than the site setting, zero when it
// turns the cache off: Config.OutputCacheTTL, or off in Development and
// with ReloadTemplates. fixed is false when the site setting decides.
func configuredOutputCache(config Config) (ttl time.Duration, fixed bool) {
	switch {
	case config.OutputCacheTTL < 0:
		return 0, true
	case config.OutputCacheTTL > 0:
		return config.OutputCacheTTL, true
	case config.Development || config.ReloadTemplates:
		// Templates and code change all the time.
		return 0, true
	default:
		return 0, false
	}
}

// version is the version of what visitors see now (see seen), for the
// cache.
func (o *outputCache) version(*http.Request) uint64 {
	return o.seen()
}

// seen is the version of what visitors see now: it moves on with every
// change in this process of the published pages, the settings and theme
// values, the media library and the modules' content. Each of them only
// counts up, so their sum is never that of an earlier state. The sitemap
// follows it too (see frontend.Frontend.Sitemap).
func (o *outputCache) seen() uint64 {
	a := o.app

	return a.Pages.Generation() + a.Site.Generation() + a.Media.Version() + o.content.Load()
}

// changed raises the module content generation: what visitors see of the
// modules changed, so every kept page is out of date.
func (o *outputCache) changed() {
	o.content.Add(1)
}

// skip reports the requests the cache leaves alone: those of routes that
// are not the site's pages, sitemap.xml and robots.txt or the content API,
// such as the admin (see App.class), those with a cookie that may give a
// page of the visitor's own, and previews. The content API answers
// everyone alike, editors too, but for its searches with a query.
func (o *outputCache) skip(r *http.Request) bool {
	class := o.app.class(r)

	switch class {
	case classAPI:
		return api.IsSearch(r)
	case classPublic, classSEO:
	default:
		return true
	}

	if r.URL.RawQuery != "" && r.URL.Query().Has(frontend.PreviewParam) {
		return true
	}

	for _, line := range r.Header.Values("Cookie") {
		for part := range strings.SplitSeq(line, ";") {
			name, _, _ := strings.Cut(strings.TrimSpace(part), "=")
			if ownPage(name) {
				return true
			}
		}
	}

	return false
}

// ownPage reports whether a request with the cookie name may get a page of
// its own: an editor's session or a visitor's kept post. The admin's flash
// messages have a cookie of the admin's path, which site pages never get;
// a site's own cookies that change its pages are personal cookies (see
// Env.PersonalCookie).
func ownPage(name string) bool {
	return name == auth.CookieName || strings.HasPrefix(name, frontend.KeptFormCookie)
}

// key is the key of a request's page: its address (go-router's
// middleware.CacheKey) with the values of the personal cookies it has; a
// search with a query is not kept.
func (o *outputCache) key(r *http.Request) (string, bool) {
	if search(r) {
		return "", false
	}

	key, ok := middleware.CacheKey(r)
	if !ok {
		return "", false
	}

	// The content API reads no cookies.
	if personal := o.personalValues(r); personal != "" && o.app.class(r) != classAPI {
		key += "\x00" + personal
	}

	return key, true
}

// search reports whether r is a search with a query: the search screen
// (frontend.SearchPath below the language) with a q parameter.
func search(r *http.Request) bool {
	if r.URL.RawQuery == "" {
		return false
	}

	_, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")

	return "/"+rest == frontend.SearchPath && r.URL.Query().Get("q") != ""
}

// varyValue tells the entries of a response that varies on the Cookie
// header apart by the personal cookies only (see Env.PersonalCookie):
// other cookies, such as a site's analytics, change nothing. A response
// that varies on Origin, one of the content API with CORS, is kept once for
// every origin: its CORS headers are set outside the cache, for each
// request (see apiGuard).
func (o *outputCache) varyValue(r *http.Request, name string) (string, bool) {
	switch name {
	case "Cookie":
		return o.personalValues(r), true
	case "Origin":
		return "", true
	default:
		return "", false
	}
}

// personalValues are the names and values of the personal cookies r has.
func (o *outputCache) personalValues(r *http.Request) string {
	if len(o.personal) == 0 {
		return ""
	}

	var values []string

	for _, name := range o.personal {
		if cookie, err := r.Cookie(name); err == nil {
			values = append(values, name+"="+cookie.Value)
		}
	}

	return strings.Join(values, "; ")
}

// visitorChanges raises the module content generation after a successful
// post of a visitor, such as a comment or a vote: what the site's pages
// show may have changed. A post that says it changed nothing
// (frontend.NothingChanged), such as spam, one with mistakes or one over
// the throttle, keeps the cache.
//
// Every post counts as a change unless it says otherwise, rather than only
// those that say they changed something: a change a module forgets to tell
// would show only once the cache's time has passed, while a post that
// changed nothing and does not say so only costs the pages a render.
func (o *outputCache) visitorChanges(next http.Handler) http.Handler {
	observed := middleware.Observe(func(r *http.Request, info middleware.ResponseInfo) {
		if info.Status < http.StatusBadRequest && frontend.Changed(r) {
			o.changed()
		}
	})(next)

	return posts(next, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed.ServeHTTP(w, frontend.TrackChanges(r))
	}))
}

// posts passes the requests that only read (go-router's
// middleware.SafeMethod) to next, and the others to observed.
func posts(next, observed http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if middleware.SafeMethod(r) {
			next.ServeHTTP(w, r)

			return
		}

		observed.ServeHTTP(w, r)
	})
}

// adminChanges raises the module content generation after a successful
// post to an admin section of a module, or to the trash, where module
// content is restored: the core sections move the generations of their own
// services on (see version), and other posts, such as an editor's
// presence, change nothing visitors see.
func (o *outputCache) adminChanges(next http.Handler) http.Handler {
	return posts(next, middleware.Observe(func(r *http.Request, info middleware.ResponseInfo) {
		if info.Status >= http.StatusBadRequest {
			return
		}

		for _, path := range o.changing {
			if r.URL.Path == path || strings.HasPrefix(r.URL.Path, path+"/") {
				o.changed()

				return
			}
		}
	})(next))
}

// stats are the counts of the cache, with those of the caches it replaced.
func (o *outputCache) stats() middleware.CacheStats {
	o.mu.Lock()
	defer o.mu.Unlock()

	stats := o.retired

	if current := o.current.Load(); current != nil {
		now := current.cache.Stats()
		stats.Hits += now.Hits
		stats.Misses += now.Misses
		stats.Skips += now.Skips
		stats.Evictions += now.Evictions
		stats.Entries = now.Entries
		stats.Bytes = now.Bytes
	}

	return stats
}

// writeMetrics adds the counts of the cache to the metrics (App.Metrics,
// served at /metrics with Config.MetricsToken), with those of the caches
// it replaced when the settings changed: the counters
// webthings_output_cache_hits_total, _misses_total, _skips_total and
// _evictions_total, and the gauges webthings_output_cache_entries and
// _bytes. They are there, at zero, also while the cache is off.
func (o *outputCache) writeMetrics(out *ops.Exposition) {
	stats := o.stats()

	out.Counter("webthings_output_cache_hits_total", "Public pages served from the output cache.", float64(stats.Hits))
	out.Counter("webthings_output_cache_misses_total", "Public pages rendered for the output cache.", float64(stats.Misses))
	out.Counter("webthings_output_cache_skips_total", "Requests the output cache left alone, such as editors'.",
		float64(stats.Skips))
	out.Counter("webthings_output_cache_evictions_total",
		"Pages dropped from the output cache to stay within its memory or its kinds of variants per address.",
		float64(stats.Evictions))
	out.Gauge("webthings_output_cache_entries", "Pages in the output cache.", float64(stats.Entries))
	out.Gauge("webthings_output_cache_bytes", "Memory the pages in the output cache take, in bytes.", float64(stats.Bytes))
}

// Changed tells the app that what visitors see changed outside a request
// that tells it already, such as content the site's own code wrote to a
// module's store or a module's service from Go, or data the site's pages
// show from elsewhere: the output cache, which keeps public pages in
// memory, renders them again. The app tells it itself after every
// successful post to the admin and to the site and its actions that does
// not say it changed nothing (frontend.NothingChanged), after scheduled
// publication (see RunSchedules) and after the writes of the bundled
// modules' stores and services, also those made from Go. A change it
// misses shows when the output cache's time has passed
// (site.Settings.OutputCache).
func (a *App) Changed() {
	a.output.changed()
}

// Changed tells the app that what visitors see of the module changed
// outside a request that tells it already (see App.Changed). Modules call
// it after the writes their stores and services make, so a write made from
// Go, such as an import or a background job, shows at once; a store made
// with a content.Spec whose Changed is set to it tells it by itself. Call
// it once the write is committed (see db.AfterCommit).
func (e Env) Changed() {
	if e.app == nil {
		return
	}

	e.app.Changed()
}

// PersonalCookie names a cookie the module reads to show public pages
// differently to the visitor who has it, such as the polls' visitor ID,
// which marks the polls they voted on. The output cache, which keeps
// public pages in memory, keeps a page apart for every value of such
// cookies, also when a response varies on the Cookie header, where other
// cookies, such as a site's analytics, count for nothing. Call it in Setup.
// Pages that are a visitor's own altogether, such as a registration looked
// up by its code, say so instead with Cache-Control: private or no-store,
// which the output cache never keeps.
func (e Env) PersonalCookie(name string) {
	if e.app == nil || name == "" || slices.Contains(e.app.output.personal, name) {
		return
	}

	e.app.output.personal = append(e.app.output.personal, name)
}
