package frontend

import (
	"errors"
	"net/http"
	"path"
	"strings"

	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-router"
)

// Handler serves the public site: "/" redirects to the default language,
// "/<locale>/..." resolves through the page tree. Paths at and below a module
// page go to that module's handler (see Router).
func (f *Frontend) Handler(modules map[string]http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snapshot, err := f.site.Snapshot(r.Context())
		if err != nil {
			f.Fail(w, r, err)

			return
		}

		settings := snapshot.Settings()

		if r.URL.Path == "/" {
			target := "/" + settings.DefaultLocale + "/"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}

			http.Redirect(w, r, target, http.StatusFound)

			return
		}

		segment, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")

		locale, err := settings.LocaleConfig().Require(segment)
		if err != nil {
			f.NotFound(w, r.WithContext(withMount(r.Context(), Mount{Locale: settings.DefaultLocale})))

			return
		}

		// Canonical paths: "/nl" becomes "/nl/", and "//" or "/./" are
		// cleaned. The query stays, for example a search.
		clean := path.Clean("/" + rest)
		if segment != locale || r.URL.Path != "/"+locale+cleanSuffix(clean, rest) {
			target := "/" + locale + cleanSuffix(clean, rest)
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}

			http.Redirect(w, r, target, http.StatusMovedPermanently)

			return
		}

		// A visitor's post with mistakes shows again in its form (KeepForm).
		r = f.restoreForms(w, r)

		f.serve(w, r, locale, clean, modules)
	})
}

// Router returns a go-router for a module's public handler (Parts.Public),
// one per module instance. The site resolves the page a module is on and
// serves the router at the page's address for that request (go-router's
// ServeAt), so routes are relative to the page, whichever address the page
// has: "/{$}" is the page itself, "/{slug}" an item below it. Handlers see
// the request's whole path.
//
//	routes := env.Frontend.Router()
//	routes.Get("/{$}", list)
//	routes.Get("/{slug}", show).As("show")
//
// Every request without a route, any path or method, gets NotFound: the
// site's 404 page rather than a plain "404 page not found" or "405 method
// not allowed". That answer is go-router's NotFound and MethodNotAllowed,
// not an interception of statuses, so a route that answers 404 itself, such
// as a lookup of a code that does not exist, keeps its own page. Modules
// therefore register no "/" route of their own. Route names are the
// module's own; in a handler, routes.For(r) builds their addresses at the
// page the request is on, such as
// routes.For(r).RoutePathArgs("show", "slug", slug) for /en/news/hello.
// Other routers report them with the instance key in front, such as
// news.show.
func (f *Frontend) Router() *router.Router {
	// A path with routes for other methods is not found either: the page
	// says so, and go-router drops the Allow header of a 405 from it.
	notFound := http.HandlerFunc(f.NotFound)

	return router.NewSite(router.WithNotFound(notFound), router.WithMethodNotAllowed(notFound))
}

// pageRouter is a module's public handler that serves at its page's
// address for one request, such as a go-router of Router.
type pageRouter interface {
	ServeAt(w http.ResponseWriter, r *http.Request, prefix string, options ...router.MountOption)
}

// cleanSuffix keeps "/" for the home page and drops trailing slashes elsewhere.
func cleanSuffix(clean, rest string) string {
	if rest == "" {
		return "/"
	}

	return clean
}

func (f *Frontend) serve(w http.ResponseWriter, r *http.Request, locale, target string, modules map[string]http.Handler) {
	// An editor may preview a page revision at its address (see
	// PreviewParam), also of a page that is not published.
	preview, previewed := f.pagePreview(r, locale, target)
	if previewed && preview.Fields.Module == "" {
		f.previewPage(w, r, preview, locale, target)

		return
	}

	resolved, err := f.pages.Resolve(r.Context(), locale, target)
	if previewed {
		// A module page shows its module below the previewed page.
		resolved, err = previewMount(preview, locale, target), nil
		r = r.WithContext(withMountPreview(r.Context(), RevisionOf(r, preview, preview.Revision.ID)))
	}

	if errors.Is(err, pages.ErrNotFound) && target == SearchPath {
		// A published page at /search wins; otherwise this is the search screen.
		f.searchScreen(w, r, locale)

		return
	}

	if errors.Is(err, pages.ErrNotFound) {
		f.NotFound(w, r.WithContext(withMount(r.Context(), Mount{Locale: locale})))

		return
	}

	if err != nil {
		f.Fail(w, r, err)

		return
	}

	mount := Mount{
		Locale: locale,
		Path:   resolved.Route.Path,
		URL:    r.URL.Path,
		PageID: resolved.Page.ID,
		Module: resolved.Route.Module,
		Title:  resolved.Route.Title,
		page:   &resolved.Page,
		home:   resolved.Route.Depth == 0,
	}
	r = r.WithContext(withMount(r.Context(), mount))

	if mount.Module == "" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method_not_allowed", http.StatusMethodNotAllowed)

			return
		}

		f.page(w, r, resolved.Page, resolved.Route)

		return
	}

	handler, ok := modules[mount.Module]
	if !ok {
		// The page stays published while its module is not installed.
		http.Error(w, "module_unavailable", http.StatusServiceUnavailable)

		return
	}

	// A go-router is served at the page's address, its route names with
	// the instance key in front for the routers around it; any other
	// handler sees the request as it is, its page in MountFrom.
	routes, ok := handler.(pageRouter)
	if !ok {
		handler.ServeHTTP(w, r)

		return
	}

	routes.ServeAt(w, r, link(locale, mount.Path, ""), router.NamePrefix(mount.Module+"."))
}
