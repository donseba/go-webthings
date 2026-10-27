package admin

import (
	"context"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/render"

	form "github.com/donseba/go-form/v2"
	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/connector"
	"github.com/donseba/go-partial/exp/flash"
	"github.com/donseba/go-router"
	"github.com/donseba/go-router/middleware"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// Page is a page rendered inside the admin shell.
type Page struct {
	// Title is the page title, shown in the header and browser tab: a
	// translation key, such as "Pages", or a name chosen by the site owner,
	// such as a module title, which shows as written without translation.
	Title string
	// Label is a page title that is shown exactly as written and never
	// translated, such as the title of the item being edited, a file name or
	// a group name: what editors type must not be looked up in the
	// catalogue. It replaces Title when set; Title stays the fallback for
	// an empty Label.
	Label string
	// Template is a template path in Files, or in the shared admin templates.
	Template string
	// Files are the section's templates; shared templates are found as well.
	Files fs.FS
	// Includes are extra templates the page calls, as paths or fs.Glob
	// patterns (see render.View.Includes).
	Includes []string
	Data     any
	// Dot, when set, replaces Data, such as a dot computed only when the
	// page's content renders, which a request for a fragment alone leaves
	// out (partial.DotFuncOf; see render.View.Dot).
	Dot    partial.Dot
	Status int
	// Fragments are parts of the page that can be asked for on their own,
	// by the ID of their element (see render.Fragment), such as the panels
	// of the dashboard that load after the page.
	Fragments []render.Fragment
}

// ShellView is the layout's data.
type ShellView struct {
	Title        string
	PageTitle    string
	SectionTitle string
	SectionURL   string
	// Dashboard is set on the dashboard, where the breadcrumbs, which start
	// at the dashboard, show only the page.
	Dashboard bool
	Language  string
	// Stylesheets are the link tags of the sections' own stylesheets, with
	// their versions and integrity, on every page.
	Stylesheets []template.HTML
	// Navigation lists the links of Groups in their order.
	Navigation []NavigationLink
	// Groups are the navigation as shown: first the links without heading
	// (Dashboard, Pages, Media), then the named groups by name, and
	// GroupSystem last.
	Groups []NavigationGroup
	User   *auth.User
	Logout form.RenderModel
	// Presence is who else is on this page (see presence.go).
	Presence PresenceView
}

// NavigationGroup is a heading of the navigation with its links.
type NavigationGroup struct {
	// Name is the translated heading; empty for the links at the top.
	Name string
	// Key is the untranslated name, by which the browser remembers that the
	// editor collapsed the group.
	Key   string
	Links []NavigationLink
	// Active groups hold the current page; they are never collapsed.
	Active bool
	// Badge is the total of the links' badges, shown while collapsed.
	Badge int
}

// NavigationLink is one entry of the admin's navigation: a section or one
// of the admin's own screens, such as the dashboard.
type NavigationLink struct {
	Title  string
	Path   string
	Icon   string
	Active bool
	// Badge is the section's count, such as new submissions; zero hides it.
	Badge int

	// section is the section the link opens, for the dashboard; nil for the
	// admin's own pages.
	section *Section
}

// Render shows a page in the admin shell. Shared components (revision panel,
// locale tabs, move arrows) can be included by any section template.
//
// The notice of the Redirect that led here, and messages a handler added to
// the request with go-partial's flash.Add, show as toasts; an HTMX response
// appends them to the toasts already shown.
func (a *Admin) Render(w http.ResponseWriter, r *http.Request, view Page) {
	// The toasts of the Redirect that led here.
	if r.Method == http.MethodGet {
		r = a.flashCookie().Restore(w, r)
	}

	// The layout, header and sidebar share the shell's data, computed once
	// per response, and only when a part of the response shows it: a
	// request for a fragment alone, such as a dashboard panel, counts no
	// navigation.
	shell := partial.DotFuncOf(func(context.Context) (ShellView, error) {
		return a.shellView(r, view, a.navigation(r, true)), nil
	})

	// The search box of list screens, which their templates declare as
	// @model Search github.com/donseba/go-webthings/admin.SearchBox.
	search := partial.ModelFuncOf(func(context.Context) (SearchBox, error) {
		current := a.currentURL(r)
		box := searchBox(current)
		box.NewLabel, box.NewURL = a.newAction(r, current)

		return box, nil
	})

	err := a.views.Render(w, r, render.View{
		Template:  view.Template,
		Includes:  slices.Concat(sharedTemplates, view.Includes),
		Files:     partial.Overlay(view.Files, files),
		Data:      view.Data,
		Dot:       view.Dot,
		ShellDot:  shell,
		Models:    []any{search},
		Status:    view.Status,
		Fragments: view.Fragments,
		Regions: []render.Region{
			// The browser tab's title, from which HTMX sets the document's
			// title after a navigation.
			{ID: "admin-title", Template: "templates/title.gohtml", Dot: shell},
			{ID: "admin-header", Template: "templates/header.gohtml", Dot: shell},
			{ID: "admin-sidebar", Template: "templates/sidebar.gohtml", Dot: shell},
			// The toasts: messages of the request's flash store, shown by
			// go-partial's exp/flash with templates/flash.gohtml.
			{ID: "flash-messages", Template: "templates/flash-messages.gohtml"},
		},
	})
	if err != nil {
		// A failing template wrote nothing (see render.Renderer.Render),
		// unless the error page (templates/error.gohtml), the error toast
		// or, in development, go-partial's detailed page answered.
		slog.ErrorContext(r.Context(), "render admin", "template", view.Template, "error", err)

		if !errors.Is(err, render.ErrShown) {
			a.refuse(w, r, http.StatusInternalServerError, requestFailed)
		}
	}
}

// requestFailed is the toast of a request the admin could not answer.
const requestFailed = "The request failed. Please try again."

// refuse answers r with status and the translated message: a toast for an
// HTMX request, the text otherwise (render.Renderer.Respond).
func (a *Admin) refuse(w http.ResponseWriter, r *http.Request, status int, message string) {
	err := a.views.Respond(w, r, status, flash.Error(a.T(r, message)))
	if err != nil {
		slog.ErrorContext(r.Context(), "admin refusal", "status", status, "error", err)
	}
}

// shellView is the layout's data for view, with its navigation groups.
func (a *Admin) shellView(r *http.Request, view Page, groups []NavigationGroup) ShellView {
	// An empty key would find the catalogue's header.
	pageTitle := view.Label
	if pageTitle == "" && view.Title != "" {
		pageTitle = a.T(r, view.Title)
	}

	shell := ShellView{
		Title:       a.options.Title,
		PageTitle:   pageTitle,
		Language:    a.locale.Language(),
		Stylesheets: a.stylesheetLinks,
		Groups:      groups,
	}

	for _, group := range groups {
		shell.Navigation = append(shell.Navigation, group.Links...)
	}

	if user, ok := auth.CurrentUser(r.Context()); ok {
		shell.User = &user
		shell.Logout = a.Form(r, struct{}{}, a.URL("admin.logout"), "Sign out")
		shell.Presence = a.presenceView(r)
	}

	dashboard := a.URL("admin.dashboard")
	shell.Dashboard = a.routeName(r) == "admin.dashboard"

	for _, link := range shell.Navigation {
		if link.Active && link.Path != dashboard && link.Title != shell.PageTitle {
			shell.SectionTitle = link.Title
			shell.SectionURL = link.Path

			break
		}
	}

	return shell
}

// sharedTemplates are the shared components every admin page can call.
var sharedTemplates = []string{
	"templates/revision-panel.gohtml",
	"templates/locale-tabs.gohtml",
	"templates/move.gohtml",
	"templates/search.gohtml",
	"templates/presence.gohtml",
}

// adminDiagnostics are the site's diagnostics with the admin's error
// answers: outside development, a page that fails to render shows
// templates/error.gohtml, and a partial request that fails adds the
// requestFailed toast (go-partial's ext/errors).
func (a *Admin) adminDiagnostics(diagnostics render.Diagnostics) render.Diagnostics {
	diagnostics.ErrorPage = "templates/error.gohtml"
	diagnostics.ErrorFlash = func(r *http.Request) string {
		return a.T(r, requestFailed)
	}

	// The error page's lang, as the layout's (ShellView.Language).
	diagnostics.ErrorLanguage = func(*http.Request) string {
		return a.locale.Language()
	}

	// The status heading of go-partial's built-in page, shown when
	// templates/error.gohtml fails itself, in the admin's language.
	diagnostics.ErrorTitle = func(r *http.Request, title string) string {
		return a.T(r, title)
	}

	return diagnostics
}

// navigation lists the sections the current user may view, with their
// badges when counted, and the user's own account page, in groups: the Dashboard and the
// sections without group at the top in their order, then the groups by
// translated name with GroupSystem last. Links in a group are sorted by
// title.
func (a *Admin) navigation(r *http.Request, counted bool) []NavigationGroup {
	current := a.currentURL(r)
	user, _ := auth.CurrentUser(r.Context())

	top := NavigationGroup{Links: []NavigationLink{{
		Title:  a.T(r, "Dashboard"),
		Path:   a.URL("admin.dashboard"),
		Icon:   "window",
		Active: a.activeRoute(&current, "admin.dashboard", true),
	}}}

	named := map[string]*NavigationGroup{}

	add := func(key string, link NavigationLink) {
		group, ok := named[key]
		if !ok {
			group = &NavigationGroup{Name: a.T(r, key), Key: key}
			named[key] = group
		}

		group.Links = append(group.Links, link)
	}

	// A hidden section, such as a module's categories, keeps the group it
	// belongs to open while it is shown.
	var hiddenGroup string

	var counts map[*Section]int
	if counted {
		counts = a.badges(r)
	}

	for i := range a.sections {
		section := &a.sections[i]
		address := a.sectionURL(*section)
		shown := a.sectionActive(&current, *section)

		if section.Hidden {
			if shown && a.mayView(r, *section) {
				hiddenGroup = section.Group
			}

			continue
		}

		if !a.mayView(r, *section) {
			continue
		}

		icon := section.Icon
		if icon == "" {
			icon = "box"
		}

		link := NavigationLink{
			Title:   a.T(r, section.Title),
			Path:    address,
			Icon:    icon,
			Active:  shown,
			section: section,
		}

		link.Badge = counts[section]

		if section.Group == "" {
			top.Links = append(top.Links, link)

			continue
		}

		add(section.Group, link)
	}

	if user.ID != "" {
		add(GroupSystem, NavigationLink{
			Title:  a.T(r, "Your account"),
			Path:   a.URL("admin.account"),
			Icon:   "users",
			Active: a.activeRoute(&current, "admin.account", false),
		})
	}

	if a.mayUseTrash(r) {
		add(GroupSystem, NavigationLink{
			Title:  a.T(r, "Trash"),
			Path:   a.URL("admin.trash"),
			Icon:   "trash",
			Active: a.activeRoute(&current, "admin.trash", false),
		})
	}

	order := a.collator()
	groups := make([]NavigationGroup, 0, len(named)+1)

	for _, group := range named {
		slices.SortStableFunc(group.Links, func(x, y NavigationLink) int {
			return order.CompareString(x.Title, y.Title)
		})

		groups = append(groups, *group)
	}

	slices.SortFunc(groups, func(x, y NavigationGroup) int {
		if (x.Key == GroupSystem) != (y.Key == GroupSystem) {
			if x.Key == GroupSystem {
				return 1
			}

			return -1
		}

		if c := order.CompareString(x.Name, y.Name); c != 0 {
			return c
		}

		return strings.Compare(x.Key, y.Key)
	})

	groups = append([]NavigationGroup{top}, groups...)

	for i := range groups {
		groups[i].Active = hiddenGroup != "" && groups[i].Key == hiddenGroup

		for _, link := range groups[i].Links {
			groups[i].Badge += link.Badge
			groups[i].Active = groups[i].Active || link.Active
		}
	}

	return groups
}

// activeRoute reports whether the page the user looks at is the named
// route's page or, unless exact, a page below it, by path segments: the
// dashboard is exact, since every admin page is below it.
func (a *Admin) activeRoute(current *url.URL, name string, exact bool) bool {
	is := a.router.IsActiveRoute
	if exact {
		is = a.router.IsActiveRouteExact
	}

	// The error is for arguments that are not key value pairs or parameters
	// the route does not have; no arguments are given.
	active, _ := is(current, name)

	return active
}

// signingInOrOut reports whether the request is the sign-in, its second
// step or the sign-out itself, by the name of its route rather than by the
// end of the path: a section may have a page whose address ends in /login
// too.
func (a *Admin) signingInOrOut(r *http.Request) bool {
	name := a.routeName(r)

	return name == "admin.login" || name == "admin.login-code" || name == "admin.logout"
}

// sectionActive reports whether the page the user looks at belongs to the
// section: its own page, the route named "index" (see RouteName), or a
// page below it. A section without an index route is matched by the label
// of the route that serves the page (see Routes.handle).
func (a *Admin) sectionActive(current *url.URL, section Section) bool {
	index := section.RouteName("index")
	if a.router.HasRoute(index) {
		return a.activeRoute(current, index, false)
	}

	label, _ := a.router.OwnsLabel(current.Path, sectionLabel(section.ID))

	return label != ""
}

// mayView reports whether the signed-in user may open the section.
func (a *Admin) mayView(r *http.Request, section Section) bool {
	return a.may(r, section, View)
}

// collator sorts names as readers of the admin language expect, ignoring
// case. A collator is not safe for concurrent use, so every request gets
// its own.
func (a *Admin) collator() *collate.Collator {
	return collate.New(language.Make(a.locale.Language()), collate.Loose)
}

// Redirect sends the browser to target after a successful change and shows a
// one-time notice there, chosen by the name of the route of the post (see
// RouteName), the part after its last dot and dash: publish shows
// "Published.", unpublish "Unpublished.", restore (such as
// revision-restore) "Revision restored as a new draft.", delete (such as
// mailing-delete) "Moved to the trash.", schedule "Publication scheduled."
// and unschedule "Schedule cancelled."; other posts show "Changes saved.",
// and signing in or out none. HTMX requests swap the content area.
func (a *Admin) Redirect(w http.ResponseWriter, r *http.Request, target string) {
	if r.Method == http.MethodPost {
		a.setNotice(w, r)
	}

	a.redirect(w, r, target)
}

// RedirectNotice is Redirect with the notice to show, as a success toast:
// message is a translation key, such as "Mailing sent.", translated for
// the toast. An empty message shows none. A message that is already
// translated or formatted, or a warning or an error, goes through
// RedirectFlash.
func (a *Admin) RedirectNotice(w http.ResponseWriter, r *http.Request, target, message string) {
	var messages []flash.Message
	if message != "" {
		messages = append(messages, flash.Success(a.T(r, message)))
	}

	a.flashCookie().Persist(w, messages...)
	a.redirect(w, r, target)
}

// RedirectFlash is Redirect with a toast of any level, such as
// flash.Warn(a.T(r, "Saved as a draft.")). The text is shown exactly as
// given, so translate it first (Admin.T, admin.TN); a very long text is
// shortened to fit the cookie that carries it. Success and info toasts hide
// after a while, warnings and errors stay until they are dismissed.
func (a *Admin) RedirectFlash(w http.ResponseWriter, r *http.Request, target string, message flash.Message) {
	a.flashCookie().Persist(w, message)
	a.redirect(w, r, target)
}

// redirect sends the browser to target (go-partial's connector.Redirect):
// an HTMX request is answered with HX-Location, which swaps the content
// area, or the whole body when signing in or out changes the shell; any
// other with 303 See Other.
func (a *Admin) redirect(w http.ResponseWriter, r *http.Request, target string) {
	location := connector.Location{Path: target, Target: "#content", Swap: "outerHTML"}

	// Signing in or out, also by setting up the first account, changes the
	// shell.
	if target == a.URL("admin.login") || a.signingInOrOut(r) || a.routeName(r) == "admin.setup" {
		location.Target, location.Swap = "body", "outerSync"
	}

	connector.Redirect(a.views.Connector(), w, r, location)
}

// Connector is the go-partial connector of the admin's pages (HTMX 4), for
// handlers that inspect a request, such as connector.IsRequest(a.Connector(),
// r) for one HTMX made.
func (a *Admin) Connector() connector.Connector {
	return a.views.Connector()
}

// currentURL is the page the user looks at; for HTMX posts that is the page
// that submitted the form, when it is an admin page on this site: a
// relative address, or one on the host the user used, also behind a
// trusted proxy (go-router's middleware.SameOrigin).
func (a *Admin) currentURL(r *http.Request) url.URL {
	if r.Method != http.MethodGet {
		current := connector.CurrentURL(a.views.Connector(), r)
		if current != nil && (current.Host == "" || middleware.SameOrigin(r, current)) && a.serves(current.Path) {
			return url.URL{Path: current.Path, RawQuery: current.RawQuery}
		}
	}

	return url.URL{Path: r.URL.Path, RawQuery: r.URL.RawQuery}
}

// serves reports whether the admin has a page at path itself, not one it
// redirects to, such as the path with a trailing slash.
func (a *Admin) serves(path string) bool {
	route, ok := a.router.Match(http.MethodGet, path)

	return ok && route.Redirect == ""
}

// notices are the toasts of Redirect, by what the route of the post does:
// the last part of its name, such as "delete" for "admin.news.delete" and
// "restore" for "admin.news.revision-restore"; other posts show "Changes
// saved."
var notices = map[string]string{
	"publish":    "Published.",
	"unpublish":  "Unpublished.",
	"restore":    "Revision restored as a new draft.",
	"delete":     "Moved to the trash.",
	"schedule":   "Publication scheduled.",
	"unschedule": "Schedule cancelled.",
}

// setNotice keeps the toast of a successful post for the page Redirect
// leads to. Signing in or out shows none.
func (a *Admin) setNotice(w http.ResponseWriter, r *http.Request) {
	if a.signingInOrOut(r) {
		return
	}

	notice, ok := notices[a.routeAction(r)]
	if !ok {
		notice = "Changes saved."
	}

	a.flashCookie().Persist(w, flash.Success(a.T(r, notice)))
}

// routeAction is what the route that serves r does by its name (see
// nameAction): the route the router matched (router.MatchedRoute), or else
// the one for its method and path.
func (a *Admin) routeAction(r *http.Request) string {
	return nameAction(a.routeName(r))
}

// routeName is the name of the route that serves r (router.MatchedRoute),
// or of the route for its method and path (see pathName) when that has
// none or the routes are not tracked.
func (a *Admin) routeName(r *http.Request) string {
	route, ok := router.MatchedRoute(r)
	if !ok || route.Name == "" {
		return a.pathName(r.Method, r.URL.Path)
	}

	return route.Name
}

// pathAction is what the route for method and path does by its name (see
// nameAction and pathName).
func (a *Admin) pathAction(method, path string) string {
	return nameAction(a.pathName(method, path))
}

// pathName is the name of the route for method and path. A post to the
// address of a named GET route, which needs no name of its own, has that
// route's.
func (a *Admin) pathName(method, path string) string {
	route, _ := a.router.Match(method, path)
	if route.Name == "" {
		route, _ = a.router.Match(http.MethodGet, path)
	}

	return route.Name
}

// nameAction is what a route does by its name (see RouteName): the part
// after its last dot and dash, such as "delete" for "admin.news.delete" and
// "new" for "admin.news.mailing-new".
func nameAction(name string) string {
	name = name[strings.LastIndex(name, ".")+1:]

	return name[strings.LastIndex(name, "-")+1:]
}

// flashCookie carries the toasts of a redirect to the page it leads to,
// with their level (go-partial's exp/flash); Render restores them.
func (a *Admin) flashCookie() flash.Cookie {
	return flash.Cookie{Name: "wt_admin_flash", Path: a.base, Secure: a.options.Secure}
}

// crossOrigin refuses a form post sent by another website. Nothing is saved.
func (a *Admin) crossOrigin(w http.ResponseWriter, r *http.Request) {
	slog.WarnContext(r.Context(), "cross-origin admin request refused", "path", r.URL.Path, "origin", r.Header.Get("Origin"))
	a.refuse(w, r, http.StatusForbidden, "This request came from another website and was refused.")
}
