// Package ops holds the operational parts of serving a site that are
// WebThings' own: its metrics in the Prometheus text format, with the
// access log, and the caching policy of public pages. The HTTP parts, such
// as compression, ETags, request IDs, the client's address and panic
// recovery, are go-router's middleware; App (webthings.go) puts them
// around its routes.
package ops

import (
	"net/http"
	"strconv"

	"github.com/donseba/go-router/middleware"
)

// CachePolicy is how long browsers and caches may keep public pages for
// visitors, in seconds. MaxAge zero has them ask the site every time, which
// the ETag makes cheap: an unchanged page is answered with 304 Not Modified.
// Stale is how long after MaxAge a cache may still show a page while it
// fetches a fresh one (stale-while-revalidate).
type CachePolicy struct {
	MaxAge int
	Stale  int
}

// CacheControl is the Cache-Control header of the policy for a request:
// "no-cache" when pages must be checked every time, also for addresses with
// a query, which often show the result of a visitor's action (such as
// ?comment=sent) or a search; otherwise public with max-age and
// stale-while-revalidate.
func (p CachePolicy) CacheControl(r *http.Request) string {
	if p.MaxAge <= 0 || r.URL.RawQuery != "" {
		return "no-cache"
	}

	value := "public, max-age=" + strconv.Itoa(p.MaxAge)
	if p.Stale > 0 {
		value += ", stale-while-revalidate=" + strconv.Itoa(p.Stale)
	}

	return value
}

// PageCache is go-router's ETag middleware with the Cache-Control of
// policy: successful HTML responses to GET and HEAD requests, and the
// site's Atom and RSS feeds, get an ETag, the hash of their body, and
// requests that name it in If-None-Match get 304 Not Modified. Responses
// without a Cache-Control of their own get the one of policy. Responses
// that are private or not to be stored (pages for signed-in editors, forms
// with a visitor's result) are left as they are, and so are responses that
// set a cookie, are streamed (flushed) or carry an ETag already.
//
// Inside it, go-router's LastModified answers a request with
// If-Modified-Since and no If-None-Match with 304 Not Modified when the
// response has a Last-Modified that is not newer, such as the feeds'
// (frontend.Feed). Pages send no Last-Modified, so they are answered by
// their ETag alone.
func PageCache(policy func(*http.Request) CachePolicy) func(http.Handler) http.Handler {
	etag := middleware.ETag(middleware.ETagOptions{
		Types: []string{"text/html", "application/atom+xml", "application/rss+xml"},
		CacheControl: func(r *http.Request) string {
			return policy(r).CacheControl(r)
		},
	})

	return func(next http.Handler) http.Handler {
		return etag(middleware.LastModified(next))
	}
}
