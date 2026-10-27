package admin

import (
	"context"
	"errors"
	"hash/fnv"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/donseba/go-webthings/admin/presence"
	"github.com/donseba/go-webthings/auth"

	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/exp/sse"
	"github.com/donseba/go-router"
)

// Presence shows who else looks at an admin page: every tab of a signed-in
// user follows the other viewers of the page it shows over a stream of
// server-sent events ("admin.presence") while it is visible, and reports
// the page to the same address every 15 seconds, also while hidden, and
// when it leaves: the header names the stream of its page with
// {{sseAttrs}}, whose go-partial exp/sse client follows it with the tab's
// id and sends the reports (sse.ReadBeacon). The patterns are below the
// admin's path.
const (
	presencePath = "/presence"
	// presenceHeartbeat keeps a stream open through proxies and checks the
	// session; presenceRetry is how long the browser waits before it
	// connects again after a stream broke.
	presenceHeartbeat = 15 * time.Second
	presenceRetry     = 5 * time.Second
	// presenceTTL is how long a tab counts as present after it last
	// reported; the browser reports every 15 seconds.
	presenceTTL = 45 * time.Second
	// presenceShown are the viewers shown as chips; more show as "+n".
	presenceShown = 5
)

// PresenceView is who else is on the current page, for the header.
type PresenceView struct {
	// Stream is the address of the page's stream, to which the browser adds
	// the tab's id; empty on pages without presence, such as the dashboard.
	Stream  string
	Viewers []PresenceViewer
	// More counts the viewers beyond those shown.
	More int
	// Label says who is on the page, for screen readers; MoreLabel names
	// the viewers counted in More.
	Label     string
	MoreLabel string
}

// PresenceViewer is another user on the page. Their e-mail address is never
// shown.
type PresenceViewer struct {
	Name     string
	Initials string
	// Title is "<name> is also on this page", the chip's tooltip.
	Title string
	// Hue colours the chip, the same for a user everywhere: one of
	// presenceHues, 0 to 11, the class presence-hue-<Hue> of the admin's
	// stylesheet, so the chip needs no style attribute, which a
	// Content-Security-Policy would block.
	Hue int
}

// presenceRoutes registers the presence stream and reports, behind present.
func (a *Admin) presenceRoutes(pages *router.Router) {
	pages.With(func(presence *router.Router) {
		presence.Use(a.present)
		presence.Use(a.body(defaultBodyLimit))
		presence.Get(presencePath, a.presenceStream).As("admin.presence")
		presence.Post(presencePath, a.presenceReport)
	})
}

// Close ends the open presence streams, so a server can shut down without
// waiting for them.
func (a *Admin) Close(context.Context) error {
	a.presence.Close()

	return nil
}

// present requires an active session like signedIn, but answers 401 rather
// than redirecting, and records no activity: a tab that stays open must
// not keep its user looking active.
func (a *Admin) present(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := a.auth.Peek(r.Context(), auth.Token(r))
		if errors.Is(err, auth.ErrSession) {
			http.Error(w, "signed_out", http.StatusUnauthorized)

			return
		}

		if err != nil {
			a.Fail(w, r, err)

			return
		}

		ctx, err := a.withPermissions(r.Context(), user)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// presenceStream follows the viewers of a page: it sends the current ones
// at once, and again whenever they change, as a go-partial patch of
// #admin-presence. A page without presence answers 204, which tells the
// browser not to connect again.
func (a *Admin) presenceStream(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r.Context())
	tab := r.URL.Query().Get("tab")

	if !validTab(tab) {
		http.Error(w, "invalid_tab", http.StatusBadRequest)

		return
	}

	page, ok := a.presencePage(r, r.URL.Query().Get("page"))
	if !ok {
		a.leave(r, user, tab)
		w.WriteHeader(http.StatusNoContent)

		return
	}

	// Opening the stream reports the page, so a navigation is known at once.
	a.visit(r, user, tab, page)

	subscription, err := a.presence.Subscribe(user.ID, page)
	if errors.Is(err, sse.ErrTooManySubscribers) {
		http.Error(w, "too_many_streams", http.StatusTooManyRequests)

		return
	}

	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)

		return
	}

	defer subscription.Close()

	// The viewers render under the admin's renderer, with its templates,
	// functions and the request's language.
	events := sse.NewWriter(w).SetParent(a.views.Root())

	if err := events.Retry(presenceRetry); err != nil {
		return
	}

	if err := a.patchPresence(events, r, page); err != nil {
		slog.ErrorContext(r.Context(), "presence", "page", page, "error", err)

		return
	}

	events.Flush()

	token := auth.Token(r)

	err = events.Serve(r.Context(), subscription, sse.ServeOptions{
		Heartbeat: presenceHeartbeat,
		// A stream ends with the session, also when it is signed out
		// elsewhere; the browser then gets 401 and stops.
		BeforeHeartbeat: func() error {
			_, err := a.auth.Peek(r.Context(), token)

			return err
		},
		Handle: func(events *sse.Writer, _ sse.Message) error {
			return a.patchPresence(events, r, page)
		},
	})

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, auth.ErrSession) {
		slog.DebugContext(r.Context(), "presence stream ended", "page", page, "error", err)
	}
}

// presenceReport records a report of a tab, as go-partial's exp/sse client
// sends it (sse.Beacon): the page of its stream, or that it left it. Pages
// without presence, or that the user may not see, take the tab off the page
// it was on.
func (a *Admin) presenceReport(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r.Context())

	beacon, err := sse.ReadBeacon(r)
	if err != nil {
		http.Error(w, "invalid_beacon", http.StatusBadRequest)

		return
	}

	tab := beacon.Params.Get("tab")
	if !validTab(tab) {
		http.Error(w, "invalid_tab", http.StatusBadRequest)

		return
	}

	page, ok := a.presencePage(r, beacon.Params.Get("page"))

	// A tab whose stream closed while it is hidden is still there; one that
	// left the page or navigated to a page without presence is not.
	if beacon.Reason.Leaving() || !ok {
		a.leave(r, user, tab)
	} else {
		a.visit(r, user, tab, page)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *Admin) visit(r *http.Request, user auth.User, tab, page string) {
	err := a.presence.Visit(r.Context(), presence.Visit{
		Tab:  tab,
		User: user.ID,
		// Only the name: the e-mail address DisplayName falls back to is
		// never shown to others.
		Name: user.Name,
		Page: page,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "presence visit", "error", err)
	}
}

func (a *Admin) leave(r *http.Request, user auth.User, tab string) {
	if err := a.presence.Leave(r.Context(), user.ID, tab); err != nil {
		slog.ErrorContext(r.Context(), "presence leave", "error", err)
	}
}

// patchPresence sends the viewers of page as a patch of #admin-presence.
func (a *Admin) patchPresence(events *sse.Writer, r *http.Request, page string) error {
	viewers := partial.NewID("admin-presence", "templates/presence.gohtml").SetDot(a.presenceOf(r, page))

	return events.PatchPartial(r.Context(), r, "#admin-presence", viewers)
}

// presenceView is the presence of the page being rendered.
func (a *Admin) presenceView(r *http.Request) PresenceView {
	current := a.currentURL(r)

	page, ok := a.presencePage(r, current.Path)
	if !ok {
		return PresenceView{}
	}

	view := a.presenceOf(r, page)
	view.Stream = a.URL("admin.presence", url.Values{"page": {page}})

	return view
}

// presenceOf lists the other users on page.
func (a *Admin) presenceOf(r *http.Request, page string) PresenceView {
	user, _ := auth.CurrentUser(r.Context())
	var view PresenceView

	viewers, err := a.presence.Viewers(r.Context(), page, user.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "presence viewers", "page", page, "error", err)

		return view
	}

	var names []string

	for i, viewer := range viewers {
		name := viewer.Name
		if name == "" {
			name = a.T(r, "Another user")
		}

		names = append(names, name)

		if i >= presenceShown {
			view.More++

			continue
		}

		view.Viewers = append(view.Viewers, PresenceViewer{
			Name:     name,
			Initials: viewer.Initials,
			Title:    a.T(r, "%s is also on this page", name),
			Hue:      hue(viewer.ID),
		})
	}

	switch {
	case len(names) == 1:
		view.Label = a.T(r, "%s is also on this page", names[0])
	case len(names) > 1:
		view.Label = a.T(r, "Also on this page: %s", strings.Join(names, ", "))
	}

	if view.More > 0 {
		view.MoreLabel = strings.Join(names[presenceShown:], ", ")
	}

	return view
}

// presencePage is the page an admin URL shows, as presence tracks it: the
// path of a section page the current user may view, without its query, so
// the language tabs of an editor are one page. Pages outside sections, such
// as the dashboard or the user's own account, and forms for new items have
// no presence.
func (a *Admin) presencePage(r *http.Request, raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Path == "" {
		return "", false
	}

	// The form for a new item is the route "new" of its section (see
	// RouteName), or one named after it, such as "mailing-new".
	route, ok := a.router.Match(http.MethodGet, parsed.Path)
	if !ok || nameAction(route.Name) == "new" {
		return "", false
	}

	// A path that is not clean is the page the router redirects it to.
	page := parsed.Path
	if route.Redirect != "" {
		page = route.Redirect
	}

	section, ok := a.routeSection(route)
	if !ok {
		return "", false
	}

	if _, ok := auth.CurrentUser(r.Context()); !ok || !a.mayView(r, section) {
		return "", false
	}

	return page, true
}

// validTab accepts the IDs browsers choose for their tabs: UUIDs and the
// like.
func validTab(tab string) bool {
	if len(tab) < 8 || len(tab) > 64 {
		return false
	}

	for _, r := range tab {
		letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if !letter && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}

	return true
}

// presenceHues is how many colours chips have: hues 30 degrees apart.
const presenceHues = 12

// hue is the colour of a user's chip, derived from the user's ID (see
// PresenceViewer.Hue).
func hue(id string) int {
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(id))

	return int(sum.Sum32() % presenceHues)
}
