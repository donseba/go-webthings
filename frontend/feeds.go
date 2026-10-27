package frontend

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/donseba/go-webthings/feed"
	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/site"

	"github.com/donseba/go-partial/exp/localization"
	"github.com/donseba/go-router"
)

// feedTitle is how a feed's title shows in feed readers and in the links
// to it ({{feedLinks}}): its own title and the site's name, translated with
// the site's translations, as "News from Atelier Noord".
const feedTitle = "%[1]s from %[2]s"

// Feed is a feed a module offers below the page it is on
// (webthings.Parts.Feeds), in Atom and RSS (see package feed). The app
// serves it on the module's router (Router) at Path below the page, with
// the extension of each format, as routes named Name and the format:
//
//	webthings.Parts{
//		Public: routes,
//		Feeds: []frontend.Feed{
//			// <page>/feed.atom and <page>/feed.rss, routes feed.atom and feed.rss
//			{Build: s.feed},
//			// <page>/category/<slug>/feed.atom, linked on the category's page
//			{Name: "category-feed", Path: "/category/{slug}/feed", Page: "category", Build: s.categoryFeed},
//		},
//	}
//
// The addresses of a feed come from its route names, as those of the
// module's other routes: routes.For(r).RoutePath("feed.atom") in a
// handler. The layout links to the feeds of the page it shows with
// {{feedLinks}}. A feed is a page of the site for the output cache, which
// keeps it for visitors until what visitors see changes, and for the
// caching of pages (ETag and the site's Cache-Control, see
// site.Settings.PageCache). Both answer a reader's If-None-Match, and an
// If-Modified-Since without it, with 304 Not Modified while the feed is
// unchanged. Its Last-Modified never goes back, also when the newest entry
// is taken off the site: it is when the app first served the feed as it is
// now, or its Updated time when that is later. Each process of the app
// keeps these times itself, so after a restart, or from another process,
// a reader that goes by the date gets the whole feed once more. It is not
// in sitemap.xml.
type Feed struct {
	// Name names the feed's routes on the module's router: Name, a dot
	// and the format, such as feed.atom and feed.rss for the default name
	// "feed".
	Name string
	// Path is where the feed is below the module's page, without the
	// extension, as the patterns of the module's routes: "/feed", the
	// default, serves <page>/feed.atom and <page>/feed.rss, and
	// "/category/{slug}/feed" a feed per category. Its last segment is not
	// a wildcard.
	Path string
	// Page names the module's route whose pages link to the feed in their
	// head ({{feedLinks}}), such as "category", the feed's wildcards
	// filled in with those of the page. Empty links the feed on every page
	// of the module; its Path then has no wildcards.
	Page string
	// Limit is how many of the newest entries the feed lists at most, such
	// as a module's option for its own feeds, up to site.MaxFeedSize. Zero,
	// the default, is the site's setting (site.Settings.FeedLimit).
	Limit int
	// Build returns the feed for a request of it.
	Build FeedFunc
}

// FeedFunc returns a module's feed (Feed.Build) for a request of it, which
// the module's router serves: routes.For(r) builds addresses on the page,
// r.PathValue gives the wildcards of the feed's Path and
// MountFrom(r.Context()) the page and its language. limit is how many of
// the newest entries the feed lists, the feed's Limit or else the site
// setting (site.Settings.FeedLimit); the app leaves out entries beyond it.
//
// The app fills in what the feed leaves empty: Title is the page's title,
// Link the module's page, ID the address of the feed's Atom document,
// Language the page's language, Author the site's name and Subtitle its
// tagline; Updated, without entries, is when the page was published. It
// sets Self to the address of the format served, makes links and image
// addresses that are paths on the site absolute, and gives an image
// without a type the one of its extension. Readers see the Title with the
// site's name, "News from Atelier Noord", translated with the site's
// translations. Entry content is HTML with absolute addresses, such as
// AbsoluteMarkdown gives. An error that wraps db.ErrNotFound, such as for a
// category that does not exist, answers with the site's 404 page; other
// errors are logged and answered with its error page (see Fail).
type FeedFunc func(r *http.Request, limit int) (feed.Feed, error)

// moduleFeeds are a module's feeds and the router that serves them.
type moduleFeeds struct {
	routes *router.Router
	feeds  []Feed
}

// ServeFeeds registers a module's feeds (webthings.Parts.Feeds) on its
// router, the module's Public router of Router, before the app serves. It
// returns an error for a feed it cannot serve: one without Build, a Path
// that does not start with a slash or ends in a wildcard, a Page the
// router has no route for, a feed with wildcards linked on every page, a
// Limit below zero or above site.MaxFeedSize, or routes whose path or name
// the router has already.
func (f *Frontend) ServeFeeds(key string, routes *router.Router, feeds []Feed) error {
	served := moduleFeeds{routes: routes}
	dates := newFeedDates()

	for _, declared := range feeds {
		declared.Name = cmp.Or(declared.Name, "feed")
		declared.Path = cmp.Or(declared.Path, "/feed")

		if err := checkFeed(routes, declared); err != nil {
			return fmt.Errorf("feed %s: %w", declared.Name, err)
		}

		for _, format := range feed.Formats {
			registration, err := routes.TryGet(declared.Path+"."+string(format), f.serveFeed(routes, declared, format, dates))
			if err == nil {
				err = registration.TryAs(declared.Name + "." + string(format))
			}

			if err != nil {
				return fmt.Errorf("feed %s: %w", declared.Name, err)
			}
		}

		served.feeds = append(served.feeds, declared)
	}

	if f.feeds == nil {
		f.feeds = map[string]moduleFeeds{}
	}

	f.feeds[key] = served

	return nil
}

// checkFeed checks a feed with its defaults filled in.
func checkFeed(routes *router.Router, declared Feed) error {
	last := declared.Path[strings.LastIndex(declared.Path, "/")+1:]

	switch {
	case declared.Build == nil:
		return errors.New("no Build")
	case !strings.HasPrefix(declared.Path, "/") || last == "" || strings.ContainsAny(last, "{}"):
		return fmt.Errorf("path %q must start with a slash and end in a name", declared.Path)
	case declared.Page == "" && len(wildcards(declared.Path)) > 0:
		return fmt.Errorf("path %q has wildcards: name the Page that links to it", declared.Path)
	case declared.Page != "" && !routes.HasRoute(declared.Page):
		return fmt.Errorf("the module has no route %q", declared.Page)
	case declared.Limit < 0 || declared.Limit > site.MaxFeedSize:
		return fmt.Errorf("limit %d is not between 0 and %d", declared.Limit, site.MaxFeedSize)
	default:
		return nil
	}
}

// wildcards are the names of the wildcards of a route pattern, such as
// slug for "/category/{slug}/feed".
func wildcards(pattern string) []string {
	var names []string

	for segment := range strings.SplitSeq(pattern, "/") {
		if !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") || segment == "{$}" {
			continue
		}

		names = append(names, strings.TrimSuffix(strings.Trim(segment, "{}"), "..."))
	}

	return names
}

// feedArgs are the values of the feed's wildcards in r, as name and value
// pairs for reverse routing; ok is false when r lacks one.
func feedArgs(declared Feed, r *http.Request) (args []any, ok bool) {
	for _, name := range wildcards(declared.Path) {
		value := r.PathValue(name)
		if value == "" {
			return nil, false
		}

		args = append(args, name, value)
	}

	return args, true
}

// serveFeed serves a feed in a format, with its date from dates.
func (f *Frontend) serveFeed(routes *router.Router, declared Feed, format feed.Format, dates *feedDates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snapshot, err := f.site.Snapshot(r.Context())
		if err != nil {
			f.Fail(w, r, err)

			return
		}

		settings := snapshot.Settings()

		limit := settings.FeedLimit()
		if declared.Limit > 0 {
			limit = declared.Limit
		}

		built, err := declared.Build(r, limit)
		if err != nil {
			f.Fail(w, r, err)

			return
		}

		if len(built.Entries) > limit {
			built.Entries = built.Entries[:limit]
		}

		if err := f.completeFeed(&built, r, routes, declared, format, settings); err != nil {
			f.Fail(w, r, err)

			return
		}

		var body bytes.Buffer

		if err := format.Write(&body, built); err != nil {
			f.Fail(w, r, err)

			return
		}

		w.Header().Set("Content-Type", format.ContentType())

		// Last-Modified never goes back (see feedDates), so the caches
		// answer If-Modified-Since by it. A date the clock has not reached
		// yet is not sent: a server's dates are never in its future.
		modified := dates.modified(r.Host+r.URL.RequestURI(), body.Bytes(), built.LastModified())
		if !modified.After(dates.now()) {
			w.Header().Set("Last-Modified", modified.UTC().Format(http.TimeFormat))
		}

		_, _ = w.Write(body.Bytes())
	}
}

// feedDateLimit is how many addresses of a module's feeds feedDates keeps
// the date of. Beyond it, it forgets them all, and each gets the time it
// is served next, a newer date.
const feedDateLimit = 1000

// feedDates are the Last-Modified times of the addresses of a module's
// feeds, which never go back, so If-Modified-Since can tell a feed is
// unchanged. The feed's Updated time (feed.Feed.LastModified) cannot: it
// goes back when the newest entry is taken off the site. A feed's date is
// when this process first served its body as it is now, by its hash, or
// its Updated time when that is later; a body that changes gets a date at
// least a second after the one before, since HTTP dates count seconds.
//
// The dates are this process's own. After a restart, and from another
// process of the app, the same feed has a newer date, which costs a reader
// the whole feed once. A process that has not seen a change another
// process made yet (until its caches' time passes) may give the older feed
// a later date than the newer feed has elsewhere, and a reader that sends
// only If-Modified-Since then keeps the older feed until the feed changes
// again. Readers that send the ETag (If-None-Match), as browsers do, are
// answered by it alone.
type feedDates struct {
	// now is the clock: time.Now outside tests.
	now func() time.Time

	mu    sync.Mutex
	dates map[string]feedDate
}

// feedDate is the body an address of a feed served last, by its hash, and
// its date.
type feedDate struct {
	sum [sha256.Size]byte
	at  time.Time
}

// newFeedDates returns the dates of a module's feeds, none known yet.
func newFeedDates() *feedDates {
	return &feedDates{now: time.Now, dates: map[string]feedDate{}}
}

// modified returns the date of body, served at address, a feed whose
// Updated time is updated (see feedDates). The date is a whole second. It
// is ahead of the clock when the body changed twice within one second, or
// when updated is in the future.
func (d *feedDates) modified(address string, body []byte, updated time.Time) time.Time {
	sum := sha256.Sum256(body)

	d.mu.Lock()
	defer d.mu.Unlock()

	previous, known := d.dates[address]
	if known && previous.sum == sum {
		return previous.at
	}

	at := d.now().Truncate(time.Second)

	updated = updated.Truncate(time.Second)
	if updated.After(at) {
		at = updated
	}

	if known && !at.After(previous.at) {
		at = previous.at.Add(time.Second)
	}

	if !known && len(d.dates) >= feedDateLimit {
		clear(d.dates)
	}

	d.dates[address] = feedDate{sum: sum, at: at}

	return at
}

// completeFeed fills in what a module's feed leaves empty (see FeedFunc).
func (f *Frontend) completeFeed(
	built *feed.Feed,
	r *http.Request,
	routes *router.Router,
	declared Feed,
	format feed.Format,
	settings site.Settings,
) error {
	mount := MountFrom(r.Context())
	text := settings.Text(mount.Locale)
	here := routes.For(r)

	args, _ := feedArgs(declared, r)

	self, err := here.RoutePathArgs(declared.Name+"."+string(format), args...)
	if err != nil {
		return err
	}

	atom, err := here.RoutePathArgs(declared.Name+"."+string(feed.Atom), args...)
	if err != nil {
		return err
	}

	// Paths on the site become absolute; other addresses stay as they are.
	full := func(address string) string {
		if !strings.HasPrefix(address, "/") || strings.HasPrefix(address, "//") {
			return address
		}

		return AbsoluteURL(f.baseURL, r, address)
	}

	built.Self = full(self)
	built.ID = cmp.Or(built.ID, full(atom))
	built.Link = full(cmp.Or(built.Link, link(mount.Locale, mount.Path, "")))
	built.Language = cmp.Or(built.Language, mount.Locale)
	built.Author = cmp.Or(built.Author, text.Title, settings.Organisation)
	built.Subtitle = cmp.Or(built.Subtitle, text.Tagline)
	built.Title = f.feedTitle(mount.Locale, cmp.Or(built.Title, mount.Title), text.Title)

	if built.Updated.IsZero() && len(built.Entries) == 0 && mount.page != nil {
		built.Updated = cmp.Or(mount.page.Revision.CreatedAt, mount.page.UpdatedAt)
	}

	for i := range built.Entries {
		entry := &built.Entries[i]
		entry.Link = full(entry.Link)
		entry.Image.URL = full(entry.Image.URL)

		if entry.Image.URL != "" && entry.Image.Type == "" {
			entry.Image.Type = mime.TypeByExtension(path.Ext(entry.Image.URL))
		}
	}

	return nil
}

// feedTitle is a feed's title as readers see it: title with the site's
// name, in locale (see feedTitle), or title alone when the site has no
// name or it is the site's name.
func (f *Frontend) feedTitle(locale, title, siteName string) string {
	if siteName == "" || title == "" || title == siteName {
		return cmp.Or(title, siteName)
	}

	return f.tr().Tl(localization.New(locale), feedTitle, title, siteName)
}

// TagURI is an ID for a feed or entry that stays the same when its address
// changes (see feed.TagURI): a tag: URI on the site's host, that of BaseURL
// or else of r, without its port. date is a day the site had the host, such
// as when the item was created, and specific tells the item apart from
// every other the site named that day, such as "news/<post ID>".
func (f *Frontend) TagURI(r *http.Request, date time.Time, specific string) string {
	host := r.Host

	if parsed, err := url.Parse(origin(r, f.baseURL)); err == nil && parsed.Host != "" {
		host = parsed.Host
	}

	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}

	return feed.TagURI(strings.Trim(host, "[]"), date, specific)
}

// AbsoluteMarkdown renders editor content for readers away from the site,
// such as entries of a feed: as Markdown does, in the language of the
// request's page, but with every address of the site absolute (the site's
// BaseURL, or the address of r) and images as e-mail shows them
// (MailImage), a copy every reader can show, with its size. Relative links
// lead from the address of r, such as the feed's, as a reader resolves
// them in a document without a base (see markdown.Options.Base).
func (f *Frontend) AbsoluteMarkdown(r *http.Request, source string) template.HTML {
	ctx := r.Context()
	base := AbsoluteURL(f.baseURL, r, r.URL.EscapedPath())

	return markdown.Render(source, f.MarkdownOptions(ctx, MountFrom(ctx).Locale, base, true))
}

// FeedLink is a link to a feed of the page shown, as {{feedLinks}} writes
// it in the page's head: the feed's media type, its title as readers see
// it and its absolute address.
type FeedLink struct {
	Type  string
	Title string
	URL   string
}

// FeedLinks are the links to the feeds of the page r shows, both formats
// of each, Atom first: the module's feeds linked on every page of it, and
// those linked on the page's route (Feed.Page). title is the page's title,
// the title of the links to the latter; the others have the title of the
// module's page. As for {{feedLinks}}, there are none on an error page,
// such as a category that does not exist, whose feed does not exist
// either: the request a Renderer gets knows the page's status.
func (f *Frontend) FeedLinks(r *http.Request, title string) []FeedLink {
	mount := MountFrom(r.Context())

	here, shown := f.pageFeeds(r)
	if len(shown) == 0 {
		return nil
	}

	snapshot, err := f.site.Snapshot(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "feed links", "module", mount.Module, "error", err)

		return nil
	}

	siteName := snapshot.Settings().Text(mount.Locale).Title

	var links []FeedLink

	for _, current := range shown {
		name := mount.Title
		if current.own {
			name = title
		}

		for _, format := range feed.Formats {
			address, err := here.RoutePathArgs(current.declared.Name+"."+string(format), current.args...)
			if err != nil {
				continue
			}

			links = append(links, FeedLink{
				Type:  format.MediaType(),
				Title: f.feedTitle(mount.Locale, name, siteName),
				URL:   AbsoluteURL(f.baseURL, r, address),
			})
		}
	}

	return links
}

// FeedURL is the address of the feed of the page r shows in format,
// "atom" or "rss", such as "/en/news/feed.atom", for a visible link to
// it: the feed linked on the page's route (Feed.Page), such as a
// category's on the category's page, or else the one linked on every page
// of the module; the first the module declared of either. A name picks the
// feed with that Name instead. It is "" for a page without such a feed, an
// unknown format and, as for FeedLinks, an error page.
func (f *Frontend) FeedURL(r *http.Request, format string, name ...string) string {
	here, shown := f.pageFeeds(r)

	var chosen *pageFeed

	for i := range shown {
		current := &shown[i]

		switch {
		case len(name) > 0:
			if current.declared.Name == name[0] {
				chosen = current
			}
		case chosen == nil, current.own && !chosen.own:
			chosen = current
		}
	}

	if chosen == nil || !slices.Contains(feed.Formats, feed.Format(format)) {
		return ""
	}

	address, err := here.RoutePathArgs(chosen.declared.Name+"."+format, chosen.args...)
	if err != nil {
		return ""
	}

	return address
}

// pageFeed is a feed of the page a request shows, with the values of its
// wildcards as name and value pairs; own is set for a feed linked on the
// page's route (Feed.Page) rather than on every page of the module.
type pageFeed struct {
	declared Feed
	args     []any
	own      bool
}

// pageFeeds are the feeds of the page r shows, in the order the module
// declared them, and the module's router for r, which builds their
// addresses. An error page has none.
func (f *Frontend) pageFeeds(r *http.Request) (*router.Router, []pageFeed) {
	mount := MountFrom(r.Context())

	served, ok := f.feeds[mount.Module]
	if mount.Module == "" || !ok || failed(r) {
		return nil, nil
	}

	here := served.routes.For(r)

	var shown []pageFeed

	for _, declared := range served.feeds {
		args, ok := feedArgs(declared, r)
		if !ok {
			continue
		}

		if declared.Page != "" {
			page, err := here.RoutePathArgs(declared.Page, args...)
			if err != nil || page != r.URL.EscapedPath() {
				continue
			}
		}

		shown = append(shown, pageFeed{declared: declared, args: args, own: declared.Page != ""})
	}

	return here, shown
}

// failed reports whether r renders an error page: the request a Renderer
// gets carries the page it renders (see withRendering), with its status.
func failed(r *http.Request) bool {
	current, ok := renderingFrom(r.Context())

	return ok && current.view.Status >= http.StatusBadRequest
}

// feedLinks is {{feedLinks}} in the head of the layout: a
// <link rel="alternate"> for each feed of the page shown (see FeedLinks),
// so browsers and feed readers find them, and nothing on pages without
// feeds and on error pages, such as a category that does not exist, whose
// feed does not exist either.
func feedLinks(ctx context.Context) template.HTML {
	current, ok := renderingFrom(ctx)
	if !ok || current.request == nil || current.view.Status >= 400 {
		return ""
	}

	var b strings.Builder

	for i, link := range current.frontend.FeedLinks(current.request, current.view.Title) {
		if i > 0 {
			b.WriteString("\n")
		}

		fmt.Fprintf(&b, `<link rel="alternate" type="%s" title="%s" href="%s">`,
			template.HTMLEscapeString(link.Type),
			template.HTMLEscapeString(link.Title),
			template.HTMLEscapeString(link.URL),
		)
	}

	//nolint:gosec // every value is escaped.
	return template.HTML(b.String())
}

// feedURL is {{feedURL "atom"}}: the address of the feed of the page shown
// in a format, for a visible link to it (see Frontend.FeedURL), such as
// <a href="{{feedURL "atom"}}">; "" on a page without one and, as for
// {{feedLinks}}, on an error page.
func feedURL(ctx context.Context, format string, name ...string) string {
	current, ok := renderingFrom(ctx)
	if !ok || current.request == nil || current.view.Status >= 400 {
		return ""
	}

	return current.frontend.FeedURL(current.request, format, name...)
}
