package webthings

import (
	"net/http"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/donseba/go-router"
)

// The endpoints for operations, which App answers before the site and its
// middleware, without sessions.
const (
	healthPath  = "/healthz"
	readyPath   = "/readyz"
	metricsPath = "/metrics"
)

// The route classes: the labels of the root router's own registrations
// (see routes), by which the metrics and the access log count requests and
// IsCorePath tells the app's addresses from the site's. classPublic is the
// class of everything the site answers, which has no label.
const (
	classPublic     = "public"
	classAdmin      = "admin"
	classAction     = "action"
	classMedia      = "media"
	classAsset      = "asset"
	classSEO        = "seo"
	classOperations = "operations"
	classAPI        = "api"
)

// classes are the route classes but public.
var classes = []string{classAdmin, classAction, classMedia, classAsset, classSEO, classOperations, classAPI}

// routeClass is the class of a route of the root router: its first label
// that is a class, or public. A route of a mounted router, such as an admin
// section's, has the mount's labels before its own.
func routeClass(route router.RouteInfo) string {
	for _, label := range route.Labels {
		if slices.Contains(classes, label) {
			return label
		}
	}

	return classPublic
}

// pathClass is the class of the registration of the root router that
// receives p, with any method. Every core address answers GET or is a
// mount, which takes every method, so one lookup with GET finds it. A path
// the ServeMux redirects, such as "/admin//x" to the clean path or "/theme"
// to the path with a slash, has the class of where the redirect leads
// (router.Router.MatchLabel).
func pathClass(root *router.Router, p string) string {
	class, _ := root.MatchLabel(http.MethodGet, p, classes...)

	return orPublic(class)
}

// class is the route class of a request, for the metrics and the access
// log: the class of the route that served it (router.MatchedLabel, which
// the root router tracks), also of a mount whose router had no route for
// it, such as an admin 404. A request no route served, such as the
// ServeMux's redirect of an unclean path, has the class of the path it is
// redirected to.
func (a *App) class(r *http.Request) string {
	class, served := router.MatchedLabel(r, classes...)
	if !served {
		return pathClass(a.root, r.URL.Path)
	}

	return orPublic(class)
}

// orPublic is class, or public for a route without a class label.
func orPublic(class string) string {
	if class == "" {
		return classPublic
	}

	return class
}

// corePath is the address a registration of the root router takes: its
// path up to the first wildcard, without the trailing slash, such as
// "/theme" for "/theme/{file...}" and "/admin" for the admin's mount.
func corePath(route router.RouteInfo) string {
	p, _, _ := strings.Cut(route.Path, "{")

	return strings.TrimSuffix(p, "/")
}

// cleanAdminPath reports whether path can be mounted as the admin's: an
// absolute, clean path other than the root. The admin checks its path in
// full when it is built.
func cleanAdminPath(p string) bool {
	return strings.HasPrefix(p, "/") && p != "/" && path.Clean(p) == p
}

// defaultApp is an app with the default configuration, whose routes an Env
// made without an app asks about.
var defaultApp = sync.OnceValue(func() *App {
	return New(Config{})
})

// CorePaths are the addresses the app serves itself, next to the site's
// pages: the admin (Config.AdminPath), visitor actions (/_/), media, the
// site's assets, theme files, sitemap.xml, robots.txt, the endpoints for
// operations (/healthz, /readyz and /metrics) and, while Config.API turns it
// on, the content API (/api/v1). Requests below most of them
// never reach module middleware, so modules use Env.IsCorePath to keep away
// from them, for example a redirect that could never fire or a statistic of
// a file. They are the root router's own routes, labelled with the class
// the metrics count their requests under, so they and the routes cannot
// drift apart; they are known from New on. The admin comes first.
func (a *App) CorePaths() []string {
	return corePaths(a.root)
}

// corePaths are the addresses of root's labelled registrations (see
// CorePaths).
func corePaths(root *router.Router) []string {
	var admin, fixed []string

	_ = root.Walk(func(route router.RouteInfo) error {
		class := routeClass(route)
		p := corePath(route)

		switch {
		case class == classPublic || p == "" || slices.Contains(admin, p) || slices.Contains(fixed, p):
		case class == classAdmin:
			admin = append(admin, p)
		default:
			fixed = append(fixed, p)
		}

		return nil
	})

	// The routes of mounted routers, and routes below another core address,
	// such as /_/webthings.css below /_, are part of that address.
	paths := append(admin, fixed...)

	return slices.DeleteFunc(slices.Clone(paths), func(p string) bool {
		return slices.ContainsFunc(paths, func(core string) bool {
			return strings.HasPrefix(p, core+"/")
		})
	})
}

// coreSegments are the first segments of CorePaths, such as "admin",
// "media" and "sitemap.xml".
func (a *App) coreSegments() []string {
	var segments []string

	for _, core := range a.CorePaths() {
		segment, _, _ := strings.Cut(strings.TrimPrefix(core, "/"), "/")
		if !slices.Contains(segments, segment) {
			segments = append(segments, segment)
		}
	}

	return segments
}

// IsCorePath reports whether path is one of CorePaths or below one of them:
// "/theme" and "/theme/site.css" are, "/themes" is not. It asks the root
// router which registration receives path.
func (a *App) IsCorePath(path string) bool {
	return pathClass(a.root, path) != classPublic
}

// AdminURL is the address of a named admin route, as Env.AdminURL builds it,
// for the site's own code, tests and tools: AdminURL("admin.login"),
// AdminURL("admin.pages.edit", "id", id). It works once the app has
// started; before, it returns "".
func (a *App) AdminURL(name string, args ...any) string {
	if !a.started.Load() {
		return ""
	}

	return a.backoffice.URL(name, args...)
}

// AdminPath is where the admin is served, Config.AdminPath, such as
// "/admin". Code links into the admin by route name (Env.AdminURL), not
// from this path; tests and tools use it to reach the admin.
func (a *App) AdminPath() string {
	return a.config.AdminPath
}
