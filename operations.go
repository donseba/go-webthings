package webthings

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/donseba/go-webthings/internal/ops"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/render"
	"github.com/donseba/go-webthings/site"

	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/exp/metrics"
	"github.com/donseba/go-router"
	"github.com/donseba/go-router/middleware"
)

// Telemetry plugs instrumentation into the app, such as tracing with
// OpenTelemetry. Core brings no tracing library of its own: integrations
// such as go-webthings-contrib's provide these parts.
type Telemetry struct {
	// Middleware wraps every request below the request ID, outside the
	// access log, compression and the routes, for example to start a trace
	// span per request; the health and metrics endpoints are left out.
	Middleware func(http.Handler) http.Handler
	// RenderStages run in every render of the site's templates and the
	// admin's, for example go-partial's exp/metrics with a sink of its own.
	RenderStages []partial.RenderStage
	// RenderEvents receive go-partial's diagnostic events, for example
	// go-partial's exp/otel Sink, which adds them to the request's span.
	RenderEvents []partial.EventSink
}

// RequestID returns the ID of the request ctx belongs to: the X-Request-ID
// header a proxy sent, when it is a sensible one (at most 64 letters, digits
// and -_.:), or one WebThings made. Every response names it in X-Request-ID;
// empty outside a request. It is go-router's middleware.GetRequestID.
func RequestID(ctx context.Context) string {
	return middleware.GetRequestID(ctx)
}

// LogHandler wraps a log handler so every record logged with a request's
// context, as slog.InfoContext(r.Context(), …) does, carries the request's
// ID as request_id. Start installs it itself with Config.LogFormat; a site
// with a logger of its own wraps that one:
//
//	slog.SetDefault(slog.New(webthings.LogHandler(handler)))
//
// It is go-router's middleware.LogHandler.
func LogHandler(handler slog.Handler) slog.Handler {
	return middleware.LogHandler(handler)
}

// setupLogging makes the logger of Config.LogFormat the default, with
// request IDs, and returns the logger of the access log: nil without
// Config.AccessLog.
func (a *App) setupLogging() (*slog.Logger, error) {
	base := slog.Default()

	if a.config.LogFormat != "" {
		options := &slog.HandlerOptions{Level: a.config.LogLevel}

		var handler slog.Handler

		switch strings.ToLower(a.config.LogFormat) {
		case "text":
			handler = slog.NewTextHandler(os.Stderr, options)
		case "json":
			handler = slog.NewJSONHandler(os.Stderr, options)
		default:
			return nil, fmt.Errorf("log format %q: use text or json", a.config.LogFormat)
		}

		base = slog.New(handler)
		slog.SetDefault(slog.New(middleware.LogHandler(handler)))
	}

	if !a.config.AccessLog {
		return nil, nil
	}

	return base, nil
}

// diagnostics is what the renderers of the site and the admin report.
func (a *App) diagnostics() render.Diagnostics {
	// One record per render call, the page with everything in it: the
	// partials inside a page cost nothing. Telemetry.RenderStages can add a
	// stage that sees every partial, for example for tracing.
	stages := []partial.RenderStage{metrics.Stage(a.metrics, metrics.Outermost())}

	return render.Diagnostics{
		Development: a.config.Development,
		Stages:      append(stages, a.config.Telemetry.RenderStages...),
		Events:      a.config.Telemetry.RenderEvents,
	}
}

// requestWide is the middleware every request passes first, also one no
// route answers: the visitor's address, believed from
// Config.TrustedProxies only, and the request ID (see requestIDs). The root
// router runs it with UseAll, followed by observe and the recovery from
// panics, which are logged with slog's default logger and answered with
// 500 (see routes).
func (a *App) requestWide() ([]router.Middleware, error) {
	realIP, err := middleware.TryRealIP(middleware.RealIPOptions{TrustedProxies: a.config.TrustedProxies})
	if err != nil {
		return nil, fmt.Errorf("trusted proxies: %w", err)
	}

	return []router.Middleware{realIP, requestIDs()}, nil
}

// requestIDs gives every request an ID (see RequestID), also for
// go-partial's render metrics.
func requestIDs() router.Middleware {
	return middleware.RequestIDWithOptions(middleware.RequestIDOptions{WithContext: metrics.WithRequestID})
}

// observe is what every request passes but those to the endpoints for
// operations: telemetry, the access log and metrics, which class requests
// by the route that served them (see class). It wraps the whole router
// (router.UseAll), so it also sees the answers no route gives, such as the
// redirect of "//en/" to its clean path. The endpoints for operations are
// the root router's routes labelled operations: they take no wildcards, so
// their paths are known before a request is served.
func (a *App) observe(accessLog *slog.Logger) router.Middleware {
	record := ops.Observe(a.metrics, accessLog, a.class)
	operations := a.operationPaths()

	return func(next http.Handler) http.Handler {
		observed := record(next)
		if a.config.Telemetry.Middleware != nil {
			observed = a.config.Telemetry.Middleware(observed)
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if operations[r.URL.Path] {
				next.ServeHTTP(w, r)

				return
			}

			observed.ServeHTTP(w, r)
		})
	}
}

// operationPaths are the paths of the root router's routes labelled
// operations (see operations).
func (a *App) operationPaths() map[string]bool {
	paths := map[string]bool{}

	_ = a.root.Walk(func(route router.RouteInfo) error {
		if route.HasLabel(classOperations) {
			paths[route.Path] = true
		}

		return nil
	})

	return paths
}

// snapshots gives every request one snapshot of the site settings, which
// everything that serves it reads (see site.Service.WithSnapshot).
func (a *App) snapshots(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(a.Site.WithSnapshot(r.Context())))
	})
}

// compress reports whether responses are compressed: the site setting,
// on unless the settings cannot be read.
func (a *App) compress(r *http.Request) bool {
	snapshot, err := a.Site.Snapshot(r.Context())

	return err != nil || snapshot.Settings().Compress()
}

// pageCache is the caching of public pages and feeds for visitors (see
// ops.PageCache: an ETag, and 304 Not Modified by it or by Last-Modified),
// from the site settings: browsers and caches check every time when they
// cannot be read.
func (a *App) pageCache(next http.Handler) http.Handler {
	cache := ops.PageCache(func(r *http.Request) ops.CachePolicy {
		snapshot, err := a.Site.Snapshot(r.Context())
		if err != nil {
			return ops.CachePolicy{}
		}

		maxAge, stale := snapshot.Settings().PageCache()

		return ops.CachePolicy{MaxAge: maxAge, Stale: stale}
	})

	return cache(next)
}

// framingHeaders are the headers of each site.Settings.Framing value:
// X-Frame-Options for older browsers and the frame-ancestors of a
// Content-Security-Policy, which newer browsers follow instead. Any site
// may frame pages without them.
var framingHeaders = map[string]func(http.Handler) http.Handler{
	site.FramingSameOrigin: middleware.SecurityHeaders(middleware.SecurityHeadersOptions{
		FrameOptions:          "SAMEORIGIN",
		ContentSecurityPolicy: "frame-ancestors 'self'",
	}),
	site.FramingNone: middleware.SecurityHeaders(middleware.SecurityHeadersOptions{
		FrameOptions:          "DENY",
		ContentSecurityPolicy: "frame-ancestors 'none'",
	}),
}

// framing gives responses the framing headers of the site setting: only
// the site itself may frame its pages when the settings cannot be read.
func (a *App) framing(next http.Handler) http.Handler {
	framed := map[string]http.Handler{site.FramingAny: next}

	for policy, headers := range framingHeaders {
		framed[policy] = headers(next)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		policy := site.FramingSameOrigin

		snapshot, err := a.Site.Snapshot(r.Context())
		if err == nil {
			policy = snapshot.Settings().FramingPolicy()
		}

		framed[policy].ServeHTTP(w, r)
	})
}

// operations registers the endpoints for operations, answered before the
// site and its middleware, without sessions, labelled operations: /healthz,
// /readyz and /metrics, which serves the metrics with Config.MetricsToken
// and answers 404 without. They answer before Start too (see ServeHTTP).
// What they answer is about the moment: no cache keeps it.
func (a *App) operations(routes *router.Router) {
	routes.Get(healthPath, a.health).Label(classOperations).Use(middleware.NoStore)
	routes.Get(readyPath, a.readiness).Label(classOperations).Use(middleware.NoStore)

	metrics := http.NotFound
	if a.config.MetricsToken != "" {
		metrics = a.authorizedMetrics
	}

	routes.Get(metricsPath, metrics).Label(classOperations).Use(middleware.NoStore)
}

// health is liveness: the process answers requests.
func (a *App) health(w http.ResponseWriter, _ *http.Request) {
	plain(w, http.StatusOK, "ok")
}

// readiness answers 200 when the app can serve (see Ready), 503 otherwise.
func (a *App) readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := a.Ready(ctx); err != nil {
		slog.WarnContext(ctx, "not ready", "error", err)

		// The reason stays in the log: it may name internals.
		plain(w, http.StatusServiceUnavailable, "not ready")

		return
	}

	plain(w, http.StatusOK, "ready")
}

// authorizedMetrics serves the metrics to requests with Config.MetricsToken.
func (a *App) authorizedMetrics(w http.ResponseWriter, r *http.Request) {
	if !a.metricsAuthorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
		plain(w, http.StatusUnauthorized, "unauthorized")

		return
	}

	a.Metrics().ServeHTTP(w, r)
}

func (a *App) metricsAuthorized(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(token), []byte(a.config.MetricsToken)) == 1
}

func plain(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(text + "\n"))
}

// errNotStarted is the readiness of an app that has not started (yet).
var errNotStarted = errors.New("not started")

// Ready reports whether the app can serve: it has started, the database
// answers, every schema it applied is still applied (another version of
// the site may have moved the database on, or it was restored from an old
// backup) and the media storage can be reached, when it can tell (see
// media.Pinger). /readyz answers 200 when it returns nil, 503 otherwise.
func (a *App) Ready(ctx context.Context) error {
	if !a.started.Load() || a.DB == nil {
		return errNotStarted
	}

	if err := a.DB.Ping(ctx); err != nil {
		return fmt.Errorf("database: %w", err)
	}

	applied, err := a.DB.SchemaVersions(ctx)
	if err != nil {
		return fmt.Errorf("schema: %w", err)
	}

	for owner, version := range a.schemaVersions {
		if applied[owner] < version {
			return fmt.Errorf("schema %s: version %d is not applied", owner, version)
		}
	}

	if pinger, ok := a.storage.(media.Pinger); ok {
		if err := pinger.Ping(ctx); err != nil {
			return fmt.Errorf("media storage: %w", err)
		}
	}

	return nil
}

// Metrics serves the app's metrics in the Prometheus text format: requests
// by route class, method and status with their durations, renders by kind
// with theirs, the database's connection pool, the output cache (hits,
// misses, skips, evictions, entries and bytes) and a few of the process.
// The site serves it at /metrics with Config.MetricsToken; without a token
// a site can serve it itself, for example on an address only its
// monitoring reaches.
func (a *App) Metrics() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Wherever a site serves it, no cache keeps it.
		w.Header().Set("Cache-Control", "no-store")

		if a.metrics == nil {
			plain(w, http.StatusServiceUnavailable, "not started")

			return
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		_ = a.metrics.WriteTo(w, a.writeDatabaseMetrics, a.output.writeMetrics, writeProcessMetrics)
	})
}

func (a *App) writeDatabaseMetrics(out *ops.Exposition) {
	if a.DB == nil {
		return
	}

	stats := a.DB.Stats()

	out.Gauge("webthings_db_open_connections", "Open database connections.", float64(stats.OpenConnections))
	out.Gauge("webthings_db_in_use_connections", "Database connections in use.", float64(stats.InUse))
	out.Gauge("webthings_db_idle_connections", "Idle database connections.", float64(stats.Idle))
	out.Gauge("webthings_db_max_open_connections", "The most database connections allowed; 0 is no limit.",
		float64(stats.MaxOpenConnections))
	out.Counter("webthings_db_wait_count_total", "Times a query waited for a free connection.", float64(stats.WaitCount))
	out.Counter("webthings_db_wait_duration_seconds_total", "Time queries waited for a free connection.",
		stats.WaitDuration.Seconds())
	out.Counter("webthings_db_statements_total", "Statements sent to the database.", float64(a.DB.Statements()))
}

func writeProcessMetrics(out *ops.Exposition) {
	out.Gauge("go_goroutines", "Goroutines that exist.", float64(runtime.NumGoroutine()))
}

// applySchema applies an owner's schema and notes the version it brought
// the owner to, for Ready.
func (a *App) applySchema(ctx context.Context, owner string, files fs.FS) error {
	if err := a.DB.ApplySchema(ctx, owner, files); err != nil {
		return err
	}

	version, err := a.DB.SchemaVersion(files)
	if err != nil {
		return err
	}

	a.schemaVersions[owner] = version

	return nil
}
