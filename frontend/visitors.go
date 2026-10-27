package frontend

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/donseba/go-router"
	"github.com/donseba/go-router/middleware"
)

// ResultThrottled is the result Actions returns a visitor with who posted
// more often than its limits allow: Back(w, r, key, ResultThrottled).
const ResultThrottled = "throttled"

// ActionLimits are the limits of a module's visitor actions (see Actions).
type ActionLimits struct {
	// Body is the largest request body in bytes. It can only lower the
	// limit: the site's limit of module actions
	// (webthings.DefaultActionBodyLimit, or the module's
	// webthings.Parts.ActionBodyLimit) applies before Actions, so a larger
	// Body has no effect; raise Parts.ActionBodyLimit instead. Zero keeps
	// the site's limit.
	Body int64
	// Posts is how many posts one visitor may send per Window (see
	// Throttle); zero does not throttle.
	Posts  int
	Window time.Duration
	// Throttled answers a visitor over the limit. By default it returns the
	// visitor to the form's page with ResultThrottled, as Back does.
	Throttled http.Handler
}

// Actions wraps next, a handler of a module's visitor actions
// (webthings.Env.Actions, at /_/<key>/...), with limits, always in the same
// order: the body is limited first, so that even the answer to a throttled
// visitor, which reads the form to find the way back, reads no more than
// Body bytes; then each visitor gets Posts posts per Window. key is the
// module instance, which names the result in the page address.
func Actions(key string, next http.Handler, limits ActionLimits) http.Handler {
	handler := next

	if limits.Posts > 0 {
		throttled := limits.Throttled
		if throttled == nil {
			throttled = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Back(w, r, key, ResultThrottled)
			})
		}

		handler = Throttle(limits.Posts, limits.Window, handler, OnLimit(throttled))
	}

	if limits.Body > 0 {
		handler = middleware.BodyLimit(limits.Body)(handler)
	}

	return handler
}

// Limits is Actions as middleware, for the routes of a module's action
// router (webthings.Env.Actions): all of them with Use, or one with
// RouteRegistration.Use.
func Limits(key string, limits ActionLimits) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return Actions(key, next, limits)
	}
}

// Result returns the result of a visitor action that the page address
// carries as ?<key>=<result> (see Back), when it is one of allowed, and ""
// otherwise, so a page never shows a made-up result from the address bar.
func Result(r *http.Request, key string, allowed ...string) string {
	result := r.URL.Query().Get(key)
	if !slices.Contains(allowed, result) {
		return ""
	}

	return result
}

// BackPath returns the path of the page a visitor's form was on (the back
// field of VisitorForm, or the Referer when the form has none, see Back),
// without query or fragment, or "" when it is not a path on this site.
func BackPath(r *http.Request) string {
	page, ok := router.LocalURL(backTarget(r))
	if !ok {
		return ""
	}

	return page.Path
}

// backTarget is the page a visitor's form was on: its back field (see
// VisitorForm), or, when the form has no back field that is a path on this
// site, the path and query of the Referer when that is an address on this
// site. A post over the body limit cannot be read, so without the Referer
// the visitor would land on the home page instead of the form.
func backTarget(r *http.Request) string {
	back := r.PostFormValue("back")
	if _, ok := router.LocalURL(back); ok {
		return back
	}

	// The Referer counts when it is an address on the host the request
	// arrived on (see AbsoluteURL).
	referer, _ := middleware.SameOriginReferer(r)

	return referer
}

// BackLocale returns the language of the page a visitor's form was on: the
// first segment of its path (see BackPath) when that is an enabled
// language, and the site's default language otherwise. It returns "" when
// the languages cannot be read.
func (f *Frontend) BackLocale(r *http.Request) string {
	if f.site == nil {
		return ""
	}

	config, err := f.site.Configuration(r.Context())
	if err != nil {
		return ""
	}

	segment, _, _ := strings.Cut(strings.TrimPrefix(BackPath(r), "/"), "/")

	locale, err := config.Require(segment)
	if err != nil {
		return config.Default
	}

	return locale
}
