// Package stats counts the public site's page views without cookies and
// without storing anything about visitors. Add an instance with:
//
//	app.Use(stats.Module())
//
// The instance has the key "stats", used in the template functions below;
// stats.Module(stats.Named("visits", "Visits")) chooses another key and
// title.
//
// Every public HTML page that answers 200 to a GET is counted: views and
// visitors per day and per page, referrer hosts, device classes (desktop,
// mobile, tablet) and languages (the first path segment). Not counted are
// the paths WebThings serves itself (webthings.App.CorePaths: the admin, visitor
// actions (/_/), media, assets, theme files, sitemap.xml, robots.txt,
// /healthz, /readyz and /metrics), files with an extension, HEAD
// requests, prefetches, htmx fragments, bots and editors who are signed in to
// the admin (frontend.Editor). Nor are pages that ask not to be kept, so their
// addresses never show up in the admin or in "most read" lists: answers with
// "X-Robots-Tag: noindex" (or none), "Cache-Control: private" or
// <meta name="robots" content="noindex">, such as search results and
// registrations looked up by their code. A bare "Cache-Control: no-store"
// still counts, since forms, polls and comments send it to stay fresh.
//
// A visitor is recognised by a hash of a random salt, the IP address
// (frontend.ClientIP, so list a reverse proxy in Config.TrustedProxies) and
// the user agent. The salt lives in memory only and changes every day, so visitors
// cannot be followed from day to day and nothing in the database leads back
// to a person. After a restart the same day's visitors count once more.
//
// Views are counted in memory and written at most 30 seconds later, so a page
// never waits for the database. Closing the app writes what is left.
//
// The admin shows the last 7, 30 or 90 days with a chart per day, the top
// pages, referrers, devices and languages, and a retention setting that
// deletes older days. The module has no page of its own.
//
// Template functions:
//
//	{{pageViews "stats" .URL}}            views of a path, e.g. "read {{pageViews "stats" .URL}} times";
//	                                      an empty path is the current page
//	{{range popular "stats" 5 30}}        the 5 most viewed pages in the current language
//	    <a href="{{.Path}}">{{.Title}}</a>  of the last 30 days, without the home page;
//	{{end}}                               each has Path, Title, Views and Visitors;
//	                                      a limit of 0 lists all pages
//	{{range popular "stats" 5 30 "news"}} the same below the page of a module instance,
//	{{range popular "stats" 5 30 "/blog"}} or below a path (without the language prefix),
//	                                      without that page itself: "most read" posts
//
// Title is the page's HTML <title> when it was last viewed, without the site
// name the layout adds before or after it ("Hello · My site" is stored as
// "Hello"; the separators ·, |, -, –, —, :: and / are recognised).
package stats

import (
	"context"
	"embed"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
)

//go:embed templates translations
var files embed.FS

// Option configures the instance.
type Option func(*options)

type options struct {
	key   string
	title string
}

// Named gives the instance its own key, used in URLs of the admin and in the
// template functions, and title, shown in the admin, instead of "stats" and
// "Statistics".
func Named(key, title string) Option {
	return func(o *options) {
		o.key = key
		o.title = title
	}
}

// Module adds the Stats instance, with the key "stats" and the title
// "Statistics" (translated in the admin) unless Named says otherwise. A site
// needs one: it counts every public page.
func Module(opts ...Option) webthings.Module {
	config := options{key: "stats", title: "Statistics"}

	for _, option := range opts {
		option(&config)
	}

	return webthings.Module{
		Type:   "stats",
		Key:    config.key,
		Title:  config.title,
		Schema: schema(),
		Setup:  setup,
		Funcs:  funcs(),
	}
}

func setup(env webthings.Env) (webthings.Parts, error) {
	service := newService(env.Key, env.DB, env.Site)
	service.pages = env.Pages
	service.refreshLocation(context.Background())

	return webthings.Parts{
		Admin:      []admin.Section{section(env, service)},
		Service:    service,
		Middleware: service.middleware,
		Close:      service.close,
	}, nil
}

// Of returns the Stats instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}
