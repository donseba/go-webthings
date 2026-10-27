package webthings

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/donseba/go-webthings/api"

	"github.com/donseba/go-router"
	"github.com/donseba/go-router/middleware"
)

// contentAPI is the content API of Config.API (package api): its router,
// mounted at api.Path from New on while the API is on, the modules'
// routers (Parts.API), mounted on it at Start, and the API itself.
type contentAPI struct {
	routes  *router.Router
	modules []moduleAPI
	api     *api.API
}

// moduleAPI is the router of a module instance's endpoints.
type moduleAPI struct {
	key    string
	routes *router.Router
}

// apiRoutes mounts the content API's router at api.Path, labelled api,
// when Config.API turns it on; off, the address stays the site's. Its
// requests pass, in this order: the rate limit and CORS (see apiGuard),
// outside the output cache, so they count and answer every request, also
// one the cache answers; the output cache, which keeps the API's answers as
// it keeps pages; compression; and an ETag, with the Cache-Control of the
// site's page cache (see apiCacheControl), and go-router's LastModified for
// an endpoint that sends Last-Modified.
func (a *App) apiRoutes(root *router.Router) {
	if !a.config.API.Enabled {
		return
	}

	root.With(func(routes *router.Router) {
		routes.Use(a.snapshots)
		routes.Use(a.apiGuard())
		routes.Use(a.output.middleware)
		routes.Use(middleware.Compress(middleware.CompressOptions{Enabled: a.compress}))
		routes.Use(middleware.ETag(middleware.ETagOptions{
			Types:        []string{"application/json"},
			CacheControl: a.apiCacheControl,
		}))
		routes.Use(middleware.LastModified)

		a.contentAPI.routes = api.NewRouter()
		routes.Mount(api.Path, a.contentAPI.routes, router.NamePrefix("api.")).Label(classAPI)
	})
}

// addAPI keeps the router of a module instance's endpoints, mounted at
// Start (see serveAPI); nothing while the API is off.
func (a *App) addAPI(key string, routes *router.Router) {
	if a.contentAPI.routes == nil || routes == nil {
		return
	}

	a.contentAPI.modules = append(a.contentAPI.modules, moduleAPI{key: key, routes: routes})
}

// serveAPI registers the API's endpoints at Start, then mounts the
// modules' routers at api.Path/<key>, their route names with
// "api.<key>." in front.
func (a *App) serveAPI() error {
	if a.contentAPI.routes == nil {
		return nil
	}

	var modules []api.Module

	for _, module := range a.modules {
		described := api.Module{Key: module.Key, Title: module.Title, Type: module.Type}

		for _, served := range a.contentAPI.modules {
			described.API = described.API || served.key == module.Key
		}

		modules = append(modules, described)
	}

	a.contentAPI.api = api.New(api.Options{
		Site:         a.Site,
		Pages:        a.Pages,
		Media:        a.Media,
		Frontend:     a.Frontend,
		BaseURL:      a.config.BaseURL,
		Translations: a.config.Translations,
		Modules:      modules,
	})
	a.contentAPI.api.Register(a.contentAPI.routes)

	for _, module := range a.contentAPI.modules {
		_, err := a.contentAPI.routes.TryMount("/"+module.key, module.routes, router.NamePrefix(module.key+"."))
		if err != nil {
			return fmt.Errorf("module %s: api: %w", module.key, err)
		}
	}

	return nil
}

// corsMaxAge is how long, in seconds, browsers may keep the answer to a
// CORS preflight request of the content API: ten minutes.
const corsMaxAge = 600

// apiGuard is the rate limit of Config.API, which answers a client over it
// 429 rate_limited, and, with AllowedOrigins, the CORS headers around it,
// so browsers of the allowed sites can read that answer too. The responses
// then say Vary: Origin, also to requests without an Origin, so caches
// between the site and its visitors keep them apart; the output cache
// keeps one answer for every origin (see outputCache.varyValue), as the
// CORS headers are set here, outside it, for each request.
func (a *App) apiGuard() router.Middleware {
	config := a.config.API
	limit, window := config.Limit()

	limited := middleware.RateLimit(middleware.RateLimitOptions{
		Limit:  limit,
		Window: window,
		OnLimit: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
			a.contentAPI.api.Error(w, r, http.StatusTooManyRequests, api.CodeRateLimited)
		}),
	})

	if len(config.AllowedOrigins) == 0 {
		return limited
	}

	cors := middleware.CORS(middleware.CORSOptions{
		AllowedOrigins: config.AllowedOrigins,
		AllowedMethods: []string{http.MethodGet, http.MethodHead},
		ExposedHeaders: []string{"ETag", "Last-Modified", "Retry-After"},
		MaxAge:         corsMaxAge,
	})

	return func(next http.Handler) http.Handler {
		return cors(limited(next))
	}
}

// apiCacheControl is the Cache-Control of the API's answers: that of the
// site's pages (site.Settings.PageCache), public with their max-age, but
// for a search with a query, which, like a page that cannot be kept,
// browsers and caches check every time.
func (a *App) apiCacheControl(r *http.Request) string {
	if api.IsSearch(r) {
		return "no-cache"
	}

	snapshot, err := a.Site.Snapshot(r.Context())
	if err != nil {
		return "no-cache"
	}

	maxAge, stale := snapshot.Settings().PageCache()
	if maxAge <= 0 {
		return "no-cache"
	}

	value := "public, max-age=" + strconv.Itoa(maxAge)
	if stale > 0 {
		value += ", stale-while-revalidate=" + strconv.Itoa(stale)
	}

	return value
}
