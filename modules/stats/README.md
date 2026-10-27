# Stats

Page view statistics without cookies: views and visitors per day and per page, referring sites, devices and languages, and "most read" lists for templates.

It is a site feature: one instance counts the whole public site. It sets no cookies and stores no IP addresses or visitor IDs, only totals per day.

## Add it to the site

```go
app.Use(stats.Module())
```

The instance gets the key `stats` and the title Statistics, translated in the admin; its dashboard is at `/admin/stats` (`/admin` is the default [admin path](../../docs/configuration.md#admin-path)). `Named` chooses another key and title; the template functions then take that key:

```go
app.Use(stats.Module(stats.Named("visits", "Visits")))
```

## What is counted

Every public HTML page that answers a GET request with 200 counts as a view. Per day the module keeps the views and visitors of the site and of each page, with the page's title, and of:

- referring sites, by host without `www.`; links within the site do not count, also behind a proxy listed in `Config.TrustedProxies`, which names the site's host;
- device classes: desktop, mobile or tablet, from the user agent;
- languages: the first segment of the path, such as `en`.

A day is a date in the site's timezone (**Site settings › Timezone**), also on a day its clocks skip midnight. The days of the dashboard, of `popular` and of **Keep statistics for (days)** count back from today's date there.

Not counted are the paths WebThings serves itself: the admin, visitor actions (`/_/`), `/media/`, `/assets/`, `/theme/`, `/sitemap.xml`, `/robots.txt`, `/healthz`, `/readyz` and `/metrics`. Nor are other paths with a file extension; HEAD requests, prefetches and htmx fragments (boosted links do count); bots, link previews, monitors and scripts, recognised by their user agent, and requests without one; and editors who are signed in to the admin.

Pages that ask not to be kept do not count either: answers with `X-Robots-Tag: noindex` or `none`, also for a single crawler, with `Cache-Control: private`, or with `<meta name="robots" content="noindex">` in the page. Search results and registrations looked up by their code are such pages, so their addresses never show up in the dashboard or in "most read" lists. A bare `Cache-Control: no-store` still counts.

## Visitors and privacy

A visitor is recognised by a hash of a random salt, the IP address and the user agent. The salt lives in memory only and changes every day, in the site's timezone, so a visitor cannot be followed from one day to the next. Neither the address nor the hash is stored. Visitors over a period are therefore the sum of each day's visitors.

Each process has its own salt: after a restart, or on another server behind a load balancer, the same visitor counts again that day. Behind a reverse proxy, list it in `Config.TrustedProxies`; otherwise every visitor has the proxy's address.

## Show it on any page

`pageViews` is how often a path was viewed in the kept statistics; an empty path is the current page:

```gotemplate
<p>Read {{pageViews "stats" ""}} times</p>
```

`popular` lists the most viewed pages in the current language, without the home page. Its arguments are the number of pages, where 0 or less lists all, and the number of days, today included. An optional last argument limits the list to the pages below a module instance's page (`"news"`) or below a path without the language prefix (`"/blog"`), without that page itself:

```gotemplate
<h2>Most read</h2>
{{range popular "stats" 5 30 "news"}}
    <a href="{{.Path}}">{{.Title}}</a>
{{end}}
```

`Title` is the page's HTML `<title>` without the site name the head region adds: `Hello · My site` is kept as `Hello`. The separators `·`, `|`, `-`, `–`, `—`, `::` and `/` are recognised. A page whose title changed shows the title of the last day it was viewed. An instance that is not placed on a page has no popular pages.

## In the admin

The dashboard shows the last 7, 30 or 90 days: the totals, a chart of the page views per day, the top 10 pages, the top 10 referring sites, the devices and the languages. It writes the views counted in memory first, so its numbers are up to date.

Editors who may edit the instance set **Keep statistics for (days)**. Older days are deleted right away and then once a day; 0 keeps everything.

## How it works

The module wraps the public site and counts a view after the page was sent, in memory. It runs before the site's output cache, so pages the cache answers without rendering them count too (see [Modules and the output cache](../../docs/modules.md#modules-and-the-output-cache)), and it reads the head of a compressed page unpacked. Views are written to the database at most 30 seconds later, so a page never waits for it; closing the app writes what is left. `pageViews` includes the views that are not written yet.

## From Go

`stats.Of(app, "stats")` returns the instance once the app has started:

```go
counter := stats.Of(app, "stats")

report, err := counter.Report(ctx, 30)
```

`Report` sums up the last days as the dashboard shows them. `Popular`, `PopularIn` and `Views` are what the template functions use. `Flush` writes the views counted in memory, for example in tests, and `SetRetention` sets how many days are kept.

## Related modules

- [Publishing](../publishing/README.md) posts make good "most read" lists.
- [Redirects](../redirects/README.md) lists the paths visitors asked for that do not exist.
