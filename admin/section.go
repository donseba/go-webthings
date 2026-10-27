package admin

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/donseba/go-router"
)

// Navigation groups the admin names itself. Their names are translated, as
// are some common names sites may choose for their modules: Agenda, Content,
// Visitors, Website, Communication and Shop.
const (
	// GroupSystem holds the admin's own sections (Accounts, Groups, Your
	// account) and the site's settings and theme; it is listed last.
	GroupSystem = "System"
	// GroupModules holds the sections of modules that have no group of their
	// own (see webthings.Module.Group).
	GroupModules = "Modules"
)

// Section is one area of the admin, such as Pages, Media or a module instance.
type Section struct {
	// ID names the section, for example "pages" or "news".
	ID string
	// Permission is the section whose rights apply, when they are shared with
	// another section (News categories use "news"). Empty means ID.
	Permission string
	// AdminOnly sections are for administrators only and not in the matrix.
	AdminOnly bool
	// Title is the navigation label; it is translated.
	Title string
	// Path is the section's address below the admin, for example "/news"
	// for <admin path>/news; empty is "/" followed by ID. Links to
	// the section use its route names (see RouteName), so nothing depends
	// on where the admin is (see Options.Path).
	Path string
	// Icon is an icon name from assets/icons.svg; "box" when empty.
	Icon string
	// Group is the heading the section is listed under in the navigation,
	// such as "Website"; it is translated. Groups are sorted by name, and
	// GroupSystem comes last. Empty lists the section at the top, without
	// heading, as Pages and Media are. Modules set it with
	// webthings.Module.Group.
	Group string
	// Hidden keeps the section out of navigation.
	Hidden bool
	// Badge returns a count shown next to the section in the navigation, such
	// as new submissions; zero shows nothing. It runs on every admin page for
	// the sections the user may see, so it must be cheap. The dashboard lists
	// the sections with a count under "Needs attention".
	Badge func(ctx context.Context) int
	// Callout returns a card the dashboard shows above its panels, such as
	// the Site settings' "Finish setting up your site", and false to show
	// none. The dashboard asks it for users who may view the section, with
	// the request's context, which carries the signed-in user
	// (auth.CurrentUser); it runs on every dashboard view, so it must be
	// cheap.
	Callout func(ctx context.Context) (Callout, bool)
	// NewLabel with NewRoute or NewPath offers a quick action on the
	// dashboard, such as "New post", to users who may create in the
	// section. NewLabel is translated. NewRoute is the name of the
	// section's route for a new item, such as "new" (see RouteName);
	// NewPath is its path below the admin instead, such as "/news/new",
	// and is used only without NewRoute. Editor sections set NewLabel and
	// NewRoute.
	NewLabel string
	NewRoute string
	NewPath  string
	// Recent lists the section's most recently changed items, newest first
	// and at most limit, for the dashboard's "Recently edited". It runs on
	// every dashboard view for the sections the user may see, so it must
	// read no more than limit items (content stores have Recent) and leave
	// deleted items out. Editor sections set it.
	//
	// The dashboard's panels load after the page, each on its own, and ask
	// every section at the same time, so a slow section delays only its
	// panel. Recent, Scheduled, Review and Badge get a context that ends
	// after a few seconds; a section that has not answered by then is
	// named in the panel as one that could not be loaded.
	Recent func(ctx context.Context, limit int) ([]RecentItem, error)
	// Scheduled lists the section's planned publication changes, the
	// earliest first and at most limit, for the dashboard's "Scheduled".
	// Editor sections whose store can plan publication set it.
	Scheduled func(ctx context.Context, limit int) ([]ScheduledItem, error)
	// Review lists the section's revisions waiting for review, oldest
	// request first and at most limit, for the dashboard's "Needs
	// attention". The dashboard calls it for users who may review in the
	// section; it returns nothing while the section's review workflow is
	// off. Sections that set it are revisioned content that can be
	// reviewed: the permission matrix offers the Review right for them
	// only. Editor sections of revisioned stores set it.
	Review func(ctx context.Context, limit int) ([]ReviewItem, error)
	// Trash holds the section's deleted items, for the admin's trash (see
	// Trash). Editor sections set it; modules with records of their own
	// implement it.
	Trash Trash
	// Files holds the section's templates and translations/<locale>.po.
	Files fs.FS
	// Stylesheet names a CSS file in Files, for example "assets/stats.css",
	// that every admin page links. Scope its rules under classes of your own:
	// all sections share the page.
	Stylesheet string
	// MaxBodyBytes raises the 512 KiB request limit, for uploads.
	MaxBodyBytes int64
	// Routes registers the section's handlers.
	Routes func(*Routes)
}

// Callout is a card on the dashboard that asks the user to finish
// something, with how far they got (see Section.Callout).
type Callout struct {
	// Title and Text are shown as written: translate them first (T).
	Title string
	Text  string
	// URL is where the card's button leads; Action is the button's text,
	// a translation key, such as "Continue setup".
	URL    string
	Action string
	// Done of Total show as a progress bar; a zero Total shows none.
	Done  int
	Total int
}

// ReviewItem is a revision waiting for review, for the dashboard (see
// Section.Review).
type ReviewItem struct {
	// Title names the item in the site's default language.
	Title string
	// URL opens the revision in its editor.
	URL string
	// SubmittedBy is the ID of the user who asked for the review, like
	// RecentItem.Author; SubmittedAt is when.
	SubmittedBy string
	SubmittedAt time.Time
}

// PermissionID is the section whose rights apply to this section.
func (s Section) PermissionID() string {
	if s.Permission != "" {
		return s.Permission
	}

	return s.ID
}

// RouteName is the full name of a route the section named name with
// Route.As: "admin.<section ID>.<name>", for example "admin.news.edit".
// Admin.URL and the route template function build addresses from it.
func (s Section) RouteName(name string) string {
	return RouteName(s.ID, name)
}

// RouteName is the full name of the route named name with Route.As in the
// section with the given ID: "admin.<section>.<name>", for example
// RouteName("news", "edit") is "admin.news.edit". Every section names its
// routes this way, the admin's own (Accounts, Groups), those of
// admin/editor and those of modules, so code anywhere can link to them by
// name with Admin.URL, Routes.URL, webthings.Env.AdminURL or the route
// template function, without knowing their paths. By convention the
// section's own page is "index", the form for a new item "new", an item's
// page "edit" or "show", and other routes are named after what they do,
// such as "delete" or "settings"; a POST to the address of a named GET
// route needs no name of its own.
func RouteName(section, name string) string {
	return "admin." + section + "." + name
}

// Routes registers section handlers. Every route requires a signed-in user who
// is allowed the given action in this section, and reads the body within
// the section's MaxBodyBytes.
type Routes struct {
	admin   *Admin
	section Section
	// router is the section's group at its path, behind a session and the
	// body limit.
	router *router.Router
	// errs are go-router's refusals of registrations, such as a route or a
	// route name registered twice, which make New fail (see err).
	errs []error
}

// Admin returns the admin the routes belong to, for handlers that render
// or check permissions.
func (r *Routes) Admin() *Admin {
	return r.admin
}

// Section returns the section the routes were registered for.
func (r *Routes) Section() Section {
	return r.section
}

// URL is the address of one of this section's routes by the name it got
// with Route.As, as Admin.URL builds it:
//
//	routes.URL("edit", "id", id, url.Values{"locale": {locale}})
//
// is Admin.URL(RouteName(section ID, "edit"), ...). Handlers may keep the
// Routes to build their links while they serve; like Admin.URL, it panics
// on an unknown route or parameter.
func (r *Routes) URL(name string, args ...any) string {
	return r.admin.URL(r.section.RouteName(name), args...)
}

// Get registers a GET handler for a pattern below the section path.
// An empty pattern is the section's own page.
func (r *Routes) Get(pattern, action string, handler http.HandlerFunc) *Route {
	return r.handle(http.MethodGet, pattern, action, handler)
}

// Post registers a POST handler for a pattern below the section path.
func (r *Routes) Post(pattern, action string, handler http.HandlerFunc) *Route {
	return r.handle(http.MethodPost, pattern, action, handler)
}

func (r *Routes) handle(method, pattern, action string, handler http.HandlerFunc) *Route {
	if pattern = strings.Trim(pattern, "/"); pattern != "" {
		pattern = "/" + pattern
	}

	if action == Export {
		r.admin.exports[r.section.PermissionID()] = true
	}

	var (
		registration *router.RouteRegistration
		err          error
	)

	switch method {
	case http.MethodPost:
		registration, err = r.router.TryPost(pattern, handler)
	default:
		registration, err = r.router.TryGet(pattern, handler)
	}

	if err != nil {
		r.errs = append(r.errs, err)

		return &Route{routes: r}
	}

	// The permission check is the route's own middleware, inside the
	// group's session and body limit; the label tells which section an
	// address belongs to (see Admin.sectionFor).
	r.errs = append(r.errs,
		registration.TryUse(r.admin.allow(r.section, action)),
		registration.TryLabel(sectionLabel(r.section.ID)),
	)

	return &Route{registration: registration, routes: r}
}

// err is what go-router refused of the registrations, or nil.
func (r *Routes) err() error {
	return errors.Join(r.errs...)
}

// Route is a route a section registered.
type Route struct {
	// registration is nil for a route go-router refused; Routes keeps why.
	registration *router.RouteRegistration
	routes       *Routes
}

// As names the route, so Admin.URL and the route template function build
// its address: the full name is Section.RouteName(name), for example
// "admin.news.edit" for As("edit") in the section news. A name used twice
// makes New fail.
func (r *Route) As(name string) *Route {
	if r.registration == nil {
		return r
	}

	if err := r.registration.TryAs(r.routes.section.RouteName(name)); err != nil {
		r.routes.errs = append(r.routes.errs, err)
	}

	return r
}

// Use adds middleware to the route, inside its permission check, such as
// AdministratorsOnly.
func (r *Route) Use(middleware ...func(http.Handler) http.Handler) *Route {
	if r.registration == nil {
		return r
	}

	for _, m := range middleware {
		if err := r.registration.TryUse(m); err != nil {
			r.routes.errs = append(r.routes.errs, err)
		}
	}

	return r
}
