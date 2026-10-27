package frontend

import (
	"context"
	"net/http"
	"net/url"
	"time"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
	"github.com/donseba/go-router/middleware"
)

// Visitors submit forms to modules (a comment, a vote, a guestbook entry) at
// /_/<key>/..., the module's actions. WebThings rejects cross-site posts there
// (http.CrossOriginProtection) and bodies over a size limit
// (webthings.DefaultActionBodyLimit, or the module's
// webthings.Parts.ActionBodyLimit); modules offer their forms with a
// honeypot field and the page to return to (VisitorForm) and check the
// honeypot with IsSpam. A module registers its actions on
// its action router (webthings.Env.Actions), which also gives their
// addresses by route name, and limits the body and throttles per visitor
// with Limits as its middleware; Actions does the same around a plain
// handler, and Throttle throttles alone.
// Back and BackWith return the visitor to the form's page with the result;
// Result reads it there, and BackPath and Frontend.BackLocale tell the page
// and its language. Frontend.BindForm reads and checks a post, and
// Frontend.KeepForm has the form show a post with mistakes again on its
// page, with the messages at their fields. Frontend.SendMail mails what a
// post caused.

// Honeypot is the name of the hidden field that people leave empty and many
// bots fill in: go-form's spam trap (form.Info.Honeypot).
const Honeypot = "website"

// VisitorForm is model, a struct with go-form's tags, as a visitor's form
// for templates, which render it with go-form's functions:
//
//	{{form_open .Form}}
//	    {{form_field .Form "name" nil}}
//	    <button type="submit">{{tl localizer "Send"}}</button>
//	{{form_close}}
//
// It posts to action, with the spam trap (Honeypot) and, as its back field,
// the page being served or rendered (see Back): the element with the id key
// on that page, so a theme puts the form and its result in an element with
// that id. key, such as the module instance's key, also goes before the
// ids of the form's fields (form.Info.IDPrefix), so that the forms of
// several instances on one page keep ids of their own: the name field of
// the instance "guestbook" has the id guestbook-name. When the visitor's
// last post of this form, to action, had mistakes (KeepForm), the form
// shows that post again: the visitor's values and the messages at their
// fields. Of several such forms on the page, such as a comment form per
// post, only the one whose hidden fields hold the posted values does. A
// form in a fragment that loads after the page ({{deferredSlot}}) does not:
// the request for the page takes the kept post before the fragment's own
// request comes, so place such a form with {{slot}} or {{requiredSlot}}.
func VisitorForm(ctx context.Context, model any, action, key string) form.RenderModel {
	info := form.Info{
		Target:   action,
		Method:   http.MethodPost,
		Honeypot: Honeypot,
		IDPrefix: key,
	}

	if back := pagePath(ctx); back != "" {
		if key != "" {
			back += "#" + url.PathEscape(key)
		}

		info.Hidden = url.Values{"back": {back}}
	}

	visitorForm := form.WithInfo(model, info)

	if carry, ok := keptCarry(ctx, key, action); ok {
		visitorForm.Carry = carry
	}

	return visitorForm
}

// pagePath is the full path of the page being rendered, or of the module
// page being served (Mount.URL).
func pagePath(ctx context.Context) string {
	if current, ok := renderingFrom(ctx); ok {
		if current.path != "" {
			return current.path
		}

		if current.request != nil {
			return current.request.URL.Path
		}
	}

	return MountFrom(ctx).URL
}

// actionModuleKey is the context key of the module whose visitor action a
// request is for.
type actionModuleKey struct{}

// WithActionModule is ctx of a request for a visitor action of the module
// instance key, below /_/<key>: the app sets it for every action, and Fail
// logs a failing action with that module, as it logs a module's page with
// the module on the page (Mount.Module).
func WithActionModule(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, actionModuleKey{}, key)
}

// actionModule is the module of the visitor action of ctx (see
// WithActionModule); "" outside an action.
func actionModule(ctx context.Context) string {
	key, _ := ctx.Value(actionModuleKey{}).(string)

	return key
}

// IsSpam reports whether the honeypot field was filled in (form.IsSpam).
// Answer such requests as if they succeeded, but store nothing: when it
// reports true, it tells the app that the post changed nothing
// (NothingChanged).
func IsSpam(r *http.Request) bool {
	if !form.IsSpam(r, Honeypot) {
		return false
	}

	NothingChanged(r)

	return true
}

// Back redirects to the page the form was on, adding result to its query,
// for example Back(w, r, "comment", "sent") returns to /en/news/hello?comment=sent.
// A #fragment of the page is kept. Only paths on this site are followed.
// Without a back field, as when a post over the body limit could not be
// read, the visitor returns to the Referer when that is an address on this
// site, and to the home page otherwise.
func Back(w http.ResponseWriter, r *http.Request, key, result string) {
	BackWith(w, r, key, result, nil)
}

// BackWith is Back with more query values, for example a code the page
// shows: BackWith(w, r, "signup", "registered", url.Values{"code": {code}}).
func BackWith(w http.ResponseWriter, r *http.Request, key, result string, extra url.Values) {
	// Only a path on this site is followed (router.LocalURL).
	parsed, ok := router.LocalURL(backTarget(r))
	if !ok {
		parsed = &url.URL{Path: "/"}
	}

	query := parsed.Query()

	for name, values := range extra {
		query[name] = values
	}

	query.Set(key, result)
	parsed.RawQuery = query.Encode()

	http.Redirect(w, r, parsed.String(), http.StatusSeeOther)
}

// ThrottleOption changes how Throttle answers.
type ThrottleOption func(*throttle)

type throttle struct {
	limited http.Handler
}

// OnLimit answers requests over the limit with handler instead of a bare 429,
// for example to return to the form: OnLimit(http.HandlerFunc(func(w, r) {
// frontend.Back(w, r, "comment", "throttled") })).
func OnLimit(handler http.Handler) ThrottleOption {
	return func(t *throttle) {
		t.limited = handler
	}
}

// Throttle allows each visitor (by ClientIP) at most limit posts per window
// through next; others get 429, or the OnLimit handler, and change nothing
// (NothingChanged). Requests that only read (go-router's
// middleware.SafeMethod: GET, HEAD, OPTIONS and TRACE) are not counted, and
// a limit or window of zero or less does not throttle. Posts are counted in memory, per process: a site on several
// servers allows limit posts per server, and a restart forgets them. It is
// go-router's RateLimit.
func Throttle(limit int, window time.Duration, next http.Handler, options ...ThrottleOption) http.Handler {
	settings := throttle{
		limited: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "too_many_requests", http.StatusTooManyRequests)
		}),
	}

	for _, option := range options {
		option(&settings)
	}

	limited := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		NothingChanged(r)
		settings.limited.ServeHTTP(w, r)
	})

	throttled := middleware.RateLimit(middleware.RateLimitOptions{
		Limit:   limit,
		Window:  window,
		Skip:    middleware.SafeMethod,
		OnLimit: limited,
	})

	return throttled(next)
}

// ClientIP is the visitor's IP address: the address the request came from,
// or, when that is a trusted proxy (webthings.Config.TrustedProxies), the
// address the proxy reports in X-Forwarded-For, X-Real-IP or Forwarded.
// Without trusted proxies those headers are ignored, since anyone can send
// them. App finds it with go-router's RealIP middleware; this is
// middleware.ClientIP.
func ClientIP(r *http.Request) string {
	return middleware.ClientIP(r)
}

// Secure reports whether the request arrived over HTTPS: its own TLS state,
// or what a trusted proxy reports with X-Forwarded-Proto or Forwarded. A
// cookie set for the visitor can then be marked Secure; the headers alone
// are not believed, since anyone can send them. It is go-router's
// middleware.Scheme.
func Secure(r *http.Request) bool {
	return middleware.Scheme(r) == "https"
}
