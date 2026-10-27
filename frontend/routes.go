package frontend

import (
	"log/slog"

	"github.com/donseba/go-router"
)

// SetRoutes gives the frontend the app's root router, whose named routes
// Route builds addresses of. webthings.App sets it before it serves.
func (f *Frontend) SetRoutes(routes *router.Router) {
	f.routes = routes
}

// Route is the address of a named route of the app's root router, with its
// parameters as name and value pairs, as go-router's RoutePathArgs fills
// them in: Route("theme", "file", "default/site.css"), or a module's
// visitor action, whose names have the instance key in front, such as
// Route("downloads.download", "id", id). It is "" for an unknown route or
// a missing parameter, which is logged, and before the app has built its
// routes; they exist by the time a module's Setup runs.
func (f *Frontend) Route(name string, args ...any) string {
	if f.routes == nil {
		return ""
	}

	path, err := f.routes.RoutePathArgs(name, args...)
	if err != nil {
		slog.Error("route address", "route", name, "error", err)

		return ""
	}

	return path
}
