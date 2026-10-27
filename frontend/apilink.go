package frontend

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
)

// The routes of the content API's page endpoints on the app's root router
// (package api, mounted while webthings.Config.API is on).
const (
	apiPageRoute    = "api.page"
	apiResolveRoute = "api.resolve"
)

// APILink is the absolute address of the page r shows in the content API
// (package api), for a <link rel="alternate" type="application/json">:
// /api/v1/pages/<id>?locale=<locale> for the page itself, and
// /api/v1/pages/resolve?path=<address> for an address below a module's
// page, such as a post's. It is "" while the API is off, and for a request
// on no page, such as an error page.
func (f *Frontend) APILink(r *http.Request) string {
	mount := MountFrom(r.Context())
	if f.routes == nil || mount.PageID == "" || !f.routes.HasRoute(apiPageRoute) {
		return ""
	}

	address := r.URL.EscapedPath()

	if address == link(mount.Locale, mount.Path, "") {
		page, err := f.routes.RoutePathArgs(apiPageRoute, "id", mount.PageID, url.Values{"locale": {mount.Locale}})
		if err != nil {
			return ""
		}

		return AbsoluteURL(f.baseURL, r, page)
	}

	resolve, err := f.routes.RoutePathArgs(apiResolveRoute, url.Values{"path": {r.URL.Path}})
	if err != nil {
		return ""
	}

	return AbsoluteURL(f.baseURL, r, resolve)
}

// apiLink is {{apiLink}} in the head of the layout: a
// <link rel="alternate" type="application/json"> to the page shown in the
// content API (see Frontend.APILink), so apps find its content, and
// nothing while the API is off, on a page without one, on an error page,
// such as a module's answer for a post that does not exist, whose address
// the API does not find either, or in a preview, which the API never
// shows.
func apiLink(ctx context.Context) template.HTML {
	current, ok := renderingFrom(ctx)
	if !ok || current.request == nil || current.view.Preview || current.view.Status >= 400 {
		return ""
	}

	address := current.frontend.APILink(current.request)
	if address == "" {
		return ""
	}

	//nolint:gosec // the address is escaped.
	return template.HTML(`<link rel="alternate" type="application/json" href="` + template.HTMLEscapeString(address) + `">`)
}
