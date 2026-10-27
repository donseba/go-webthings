# Feeds, structured data, API and links of a module

This guide continues the links module of [Writing a module](writing-a-module.md) beyond its pages: Atom and RSS feeds of its content, structured data for search engines, endpoints in the content API, and the links in its content to the site's pages, which the admin keeps track of.

## Feeds

A module offers Atom and RSS feeds of its content with `Parts.Feeds`, a list of `frontend.Feed`. Each feed has a `Name`, `"feed"` by default, a `Path` below the instance's page without the extension, `"/feed"` by default, an optional `Page`, explained below, and `Build`, a `frontend.FeedFunc` that returns the feed for a request. `Setup` in [Module and Setup](writing-a-module.md#module-and-setup) adds the links module's feed, its newest links at `/en/partners/feed.atom` and `/en/partners/feed.rss`, with `Feeds: service.feeds()`:

```go
// feeds are the instance's feeds below its page: its newest links at
// feed.atom and feed.rss.
func (s *Service) feeds() []frontend.Feed {
    return []frontend.Feed{{Build: s.feed}}
}

// feed is the newest published links in the page's language.
func (s *Service) feed(r *http.Request, limit int) (feed.Feed, error) {
    ctx := r.Context()
    locale := frontend.MountFrom(ctx).Locale

    items, err := s.items.ListPublished(ctx, s.key, locale)
    if err != nil {
        return feed.Feed{}, err
    }

    slices.SortStableFunc(items, func(a, b Item) int {
        return b.CreatedAt.Compare(a.CreatedAt)
    })

    var built feed.Feed

    for _, item := range items[:min(limit, len(items))] {
        text, _ := item.Text(locale)

        built.Entries = append(built.Entries, feed.Entry{
            ID:        s.frontend.TagURI(r, item.CreatedAt, s.key+"/"+item.ID),
            Title:     text.Title,
            Link:      item.Fields.URL,
            Summary:   text.Description,
            Published: item.CreatedAt,
            Updated:   item.UpdatedAt,
        })
    }

    return built, nil
}
```

The app registers each feed on the instance's `Public` router, which must then be a `Frontend.Router`, as two routes: `<Name>.atom` at `<Path>.atom` below the page and `<Name>.rss` at `<Path>.rss`, here `feed.atom` and `feed.rss`. Links to a feed come from its route names, as those to the module's other routes: `routes.For(r).RoutePath("feed.atom")` in a handler is `/en/partners/feed.atom`, and `paths("feed.atom")` of [The public handler](writing-a-module.md#the-public-handler) builds it outside a request. `Build` runs as the router's handlers do: `frontend.MountFrom(r.Context())` gives the page and its language, and `r.PathValue` the wildcards of the feed's `Path`. An instance on no page has no feeds; their addresses get the 404 page.

A `Path` with wildcards gives a feed per item of a route, such as Publishing's feed per category: `{Name: "category-feed", Path: "/category/{slug}/feed", Page: "category", Build: s.feed}` is at `/en/news/category/<slug>/feed.atom` and `.rss`, and `routes.For(r).RoutePathArgs("category-feed.atom", "slug", slug)` builds its address. `Page` names the route whose pages link to the feed, here the list of a category at `/category/{slug}`, and the feed's wildcards are filled in with those of that page, so a `Path` with wildcards needs a `Page`. A feed without one is linked from every page of the instance. The last segment of a `Path` is a name, never a wildcard. `Start` fails for a feed the app cannot serve: one without `Build`, a `Path` that does not start with a slash or that ends in a wildcard, wildcards without a `Page`, a `Page` the router has no route for, or a route name or path the router has already.

`limit` is how many entries the feed lists: the feed's `Limit`, or, while that is zero, the site setting **Items in feeds** in **Site settings › General** (`site.Settings.FeedLimit`: 20 by default, at most 100). `Limit` gives a feed a size of its own, such as from an option of the module: Publishing's `publishing.WithFeedSize(n)` sets it on the feeds of an instance, its category feeds included, and Events' `events.WithFeedSize(n)` on its feed. `Start` fails for a `Limit` below zero or above `site.MaxFeedSize` (100). Give the entries in the order readers should see them, usually the newest first; the app leaves out those beyond the limit. It also fills in what the feed leaves empty: `Title` is the page's title, `Link` the instance's page, `ID` the absolute address of the feed's Atom document, `Language` the page's language, `Author` the site's name, or else its organisation, and `Subtitle` the site's tagline; a feed without entries gets the time of the page's published revision as `Updated`. It sets `Self` to the address of the format served, makes the links and image addresses that are paths on the site absolute, such as an entry's `Link` from `routes.For(r)`, and gives an image without a `Type` the one of its extension. Readers see the title with the site's name, such as "Partners from My site", translated with the site's translations as `%[1]s from %[2]s`; a title that is the site's name stays as it is. An error from `Build` that wraps `db.ErrNotFound`, such as for a category that does not exist, gets the site's 404 page. Other errors, and a feed that is still not valid, are logged and get the error page, as with `Fail`.

Each entry needs an absolute `ID`, a `Title`, a `Published` or `Updated` time and a `Link` or `Content`. `Frontend.TagURI(r, date, specific)` makes an ID that stays the same when the entry's address changes: a `tag:` URI such as `tag:example.com,2026-10-07:partners/<ID>`, on the host of `Config.BaseURL`, or else of the request, without its port. `date` is a day the site had that host, such as when the item was created, and `specific` tells the item apart from everything else the site named that day, such as the key and the item's ID. Set `BaseURL` in production, so the IDs do not change with the address a reader used. `Content` is HTML with absolute addresses, as readers show it away from the site: `Frontend.AbsoluteMarkdown(r, source)` renders editor content as `Markdown` does, in the page's language, but with every address on the site absolute and images as e-mail shows them (`MailImage`). Relative links lead from the feed's own address, as a reader resolves them. Publishing and Events give their text this way, with their image as `Image`, an enclosure; see [`../modules/publishing/feed.go`](../modules/publishing/feed.go) and [`../modules/events/feed.go`](../modules/events/feed.go).

`Frontend.MarkdownOptions(ctx, locale, base, mail)` gives the `markdown.Options` the site renders editor content with, for `markdown.Render` and `markdown.PlainMedia` in your own code: links to media give the file, and an image without a description in the Markdown gets its alternative text in `locale` from the media library. `base` is `Options.Base`, such as `https://example.com/en/`, or `""` for the site's own pages (see [Media and images](media.md#from-go)). Without `mail`, images are shown as a page shows them (`Image`), with a `srcset`; with `mail`, as e-mail shows them (`MailImage`), one copy with its size. `AbsoluteMarkdown` renders with `mail` and the feed's address as `base`, and the newsletter its mailings with `mail` and the home page of the mailing's language as `base`; see [`../modules/newsletter/sending.go`](../modules/newsletter/sending.go).

`{{feedLinks}}`, which core's head tags have, writes a `<link rel="alternate">` in the page's `<head>` for each format of each feed of the page shown, with absolute addresses, so browsers and feed readers find them; see [Themes](themes.md#feeds). An error page gets none, also the 404 that `NotFound` or `Fail` renders for an address below the module's page. `{{feedURL "atom"}}` gives the address of the page's feed for a visible link, and `Frontend.FeedURL(r, format, name...)` the same in Go; see [feedURL](template-functions.md#feedurl). Feeds are pages of the site for caching: the output cache keeps them for visitors and renders them again after the same changes as the pages (see [The output cache](module-caching-and-testing.md#the-output-cache)), and they get an ETag, the site's `Cache-Control` for pages (**Site settings › Performance**) and a `Last-Modified` that never goes back: when the app first served the feed as it is now, or the feed's own time when that is later (`feed.Feed.LastModified`: `Updated`, or else the newest time of an entry). Unpublishing the newest item therefore moves it forward too. A reader that sends the ETag in `If-None-Match`, or only `If-Modified-Since`, gets 304 Not Modified while the feed is unchanged, and the whole feed after a change. Each process of the app keeps these times itself, so after a restart, or from another process, a reader that asks by the date gets the whole feed once more; see [Deployment](deployment.md#caching-and-compression). A time the clock has not reached yet is not sent, such as an `Updated` in the future. Feeds are never in `sitemap.xml`. Pages send no `Last-Modified`. A handler of the module that sets one itself gets the same 304 Not Modified by it, from go-router's `middleware.LastModified` and the output cache, so it must never go back.

Package `feed` writes the documents from one model, a `feed.Feed` with its `feed.Entry` values: Atom 1.0 with `feed.Atom.Write(w, f)` and RSS 2.0 with `feed.RSS.Write(w, f)`. Both call `Validate` first, which checks what both formats require, and write nothing for a feed that misses it: the error wraps `feed.ErrInvalid` and names each problem. The app writes a module's feeds itself. The package uses the standard library only.

## Structured data

Search engines read what a page is about from its structured data: schema.org items in JSON-LD, which core's head tags write in the page's `<head>` with `{{structuredData}}`; see [Search and SEO](search-and-seo.md#structured-data). Core describes the site, its organisation, the page and its place in the page tree. A module describes the content its page shows with `frontend.Content.StructuredData`, a `[]frontend.Thing`. The first item is the page's main entity: core gives it a `mainEntityOfPage` with the page's canonical address, unless it has one.

A `frontend.Thing` is a `map[string]any`: the item's `"@type"` and its properties, as `encoding/json` writes them. Empty values, such as `""`, nil, a zero `time.Time` and empty lists, are left out, so a module can name every property without checking for content first; an item with nothing but its type is left out altogether. Addresses may be paths in the properties that hold them, `@id`, `url`, `image`, `logo`, `item`, `sameAs`, `contentUrl`, `thumbnailUrl` and `urlTemplate`: they are made absolute, on `Config.BaseURL` or the request's address. Give dates as text, in the content's own timezone, such as `"2026-10-07"` or `"2026-10-07T19:30:00+02:00"`. An item refers to the site's organisation, which core describes from the site settings, with `frontend.Thing{"@id": frontend.OrganizationID}`. `env.Frontend.SocialImageURL(ctx, ref)` is the image shown when a page is shared, a 1200 × 630 JPEG cut around the focal point, of an asset ID or a media URL, for an item's `image`.

With the posts of [Previews and review](module-admin.md#previews-and-review) as `PostItem` values, a `content.Item` whose fields have a `Date` and an `Image` and whose text an `Excerpt`, a post's page describes the post as an article by the site's organisation, as the posts name no author:

```go
// article is the structured data of a post's page: an Article in locale,
// published on the post's date by the site's organisation.
func (s *Service) article(ctx context.Context, item PostItem, locale string) frontend.Thing {
    text, _ := item.Text(locale)
    organization := frontend.Thing{"@id": frontend.OrganizationID}

    return frontend.Thing{
        "@type":         "Article",
        "headline":      text.Title,
        "description":   text.Excerpt,
        "image":         s.frontend.SocialImageURL(ctx, item.Fields.Image),
        "datePublished": item.Fields.Date.Format(time.DateOnly),
        "dateModified":  item.Revision.CreatedAt.Format(time.RFC3339),
        "inLanguage":    locale,
        "author":        organization,
        "publisher":     organization,
    }
}
```

The post's page gives it with its content: `StructuredData: []frontend.Thing{s.article(ctx, item, mount.Locale)}` in the `frontend.Content` of `postPage`. Pages that are not found or fail have no structured data: the 404 page and the theme's error page, `500.gohtml`, write none, whether `Fail` answered with it or a template failed. A page that lists items with pages of their own, such as a list of posts, gives `frontend.ItemList(name, urls...)`: an `ItemList` named `name` of the items' addresses, in the order the page shows them. Empty addresses are left out, and without any it returns nil, which adds nothing. [`../modules/publishing/structured.go`](../modules/publishing/structured.go) builds a post's Article this way, and [`../modules/events/structured.go`](../modules/events/structured.go) an event's Event.

A module that adds to the pages of other modules, or changes their items, sets `Parts.StructuredData` to a `frontend.StructuredDataFunc`, `func(ctx, view, items) []frontend.Thing`. It runs for every page that has structured data, in the order of the modules, with the page's `frontend.View` and the items of the page's module and of the functions before it, and returns them with its own. `view.Module` is the key of the instance whose screen the page is, and `view.Content` its data, such as an `events.Event`. The items are the other module's, so change one in a copy, such as with `maps.Clone`. The registrations module gives an event its `offers` this way; see [`../modules/registrations/structured.go`](../modules/registrations/structured.go).

## The content API

While the site's [content API](api.md) is on (`Config.API`), a module can serve its published content as JSON below `/api/v1/<key>/`, such as Publishing's posts at `/api/v1/news/posts`. `Parts.API` is a go-router of `api.NewRouter()`. The app mounts it at `/api/v1/<key>` when it starts, and leaves it out while the API is off. Its routes are relative to that address, and their names get `api.<key>.` in front for the app's other routers. A request without a route, any path or method, gets the API's 404 `not_found` as JSON. The links module's endpoints, its published links in their order and one link by its ID:

```go
// APILink is a published link in the content API.
type APILink struct {
    ID          string `json:"id"`
    Title       string `json:"title"`
    Description string `json:"description"`
    URL         string `json:"url"`
    APIURL      string `json:"api_url"`
}

// apiRoutes are the instance's endpoints below /api/v1/<key>.
func (s *Service) apiRoutes() *router.Router {
    routes := api.NewRouter()

    routes.Get("/links", s.apiLinks).As("links")
    routes.Get("/links/{id}", s.apiLink).As("link")

    return routes
}

// apiLinks answers /links: a page of the published links.
func (s *Service) apiLinks(w http.ResponseWriter, r *http.Request) {
    links, err := s.Links(r.Context(), api.Locale(r))
    if err != nil {
        api.Fail(w, r, err)

        return
    }

    items := make([]APILink, 0, len(links))

    for _, link := range links {
        items = append(items, s.apiItem(r, link))
    }

    api.Write(w, r, api.Paginate(r, items))
}

// apiLink answers /links/{id}: a published link in the request's language.
func (s *Service) apiLink(w http.ResponseWriter, r *http.Request) {
    locale := api.Locale(r)

    item, err := s.items.Published(r.Context(), s.key, r.PathValue("id"), locale)
    if errors.Is(err, content.ErrNotFound) {
        api.NotFound(w, r)

        return
    }

    if err != nil {
        api.Fail(w, r, err)

        return
    }

    text, _ := item.Text(locale)
    link := Link{ID: item.ID, Title: text.Title, Description: text.Description, URL: item.Fields.URL}

    api.Modified(w, item.UpdatedAt)
    api.Write(w, r, s.apiItem(r, link))
}

// apiItem is a link in the content API, with its own address there.
func (s *Service) apiItem(r *http.Request, link Link) APILink {
    self, _ := s.api.RoutePathArgs("link", "id", link.ID)

    return APILink{
        ID:          link.ID,
        Title:       link.Title,
        Description: link.Description,
        URL:         link.URL,
        APIURL:      api.URL(r, self+"?"+url.Values{"locale": {api.Locale(r)}}.Encode()),
    }
}
```

The `Service` keeps the router in a field `api *router.Router`: `Setup` sets it with `service.api = service.apiRoutes()` and returns it with `API: service.api`. With the instance `partners`, `/api/v1/partners/links` lists the links, and `s.api.RoutePathArgs("link", "id", id)` is `/api/v1/partners/links/<id>`, as is `app.Frontend.Route("api.partners.link", "id", id)` for the site's own code. The site lists the instances with endpoints at `/api/v1/site`, each with its `api_base`, such as `https://example.com/api/v1/partners`.

Package `api` answers as the core endpoints do; see [Content API](api.md#responses):

- `api.Locale(r)` is the language of the request: its `locale` parameter, or the site's default language. A language the site does not offer is answered with 404 `unknown_locale` before the handler runs.
- `api.URL(r, path)` makes an address of the site absolute, on `Config.BaseURL` or the request's address. Links to the instance's pages come from `pages.Links(s.public, base)`, as in [The public handler](writing-a-module.md#the-public-handler); they are empty while the instance is on no page, and `api.Optional` makes such a value `null`.
- `api.HTML(r, source)` renders editor content, which is Markdown, as HTML for readers away from the site, for a `body_html` field: in the request's language, with every address of the site absolute and images with their `srcset`, size and alt text. Relative links lead from the home page of that language, such as `https://example.com/en/`.
- `api.ImageOf(r, id)` is an image of the media library as an `*api.Image`, with its address, `srcset`, size, alt text and original, all absolute; nil, which is `null`, without one.
- `api.Paginate(r, items)` is the page of a list that the request asks for with `limit` and `offset`, as an `api.List`, with the address of the next page in `next`.
- `api.Modified(w, dates...)` gives the answer for one item a `Last-Modified`, the latest of the dates of everything it shows, so a client that asks with `If-Modified-Since` gets `304 Not Modified` while none of them changed; call it before `api.Write`. A zero date, of something whose date is unknown, such as a category that was deleted, sends none, and so does a date ahead of the clock. `api.MountModified(r, key)` is the date of the page the instance is on and of the pages above it, which the item's `url` holds, zero while the instance is on no page, and `api.MediaModified(r, source, ids...)` that of the media in a Markdown text and of images by their ID, such as a featured image. `api.ItemDates(r, key, item, source, ids...)` gives these dates for one item of a content store: its last change, the revision it shows for content with revisions, the instance's page (`MountModified`) and the media of `source` and `ids` (`MediaModified`). Add the dates of anything else the answer shows: `api.Modified(w, api.ItemDates(r, s.key, item, text.Body, item.Fields.Image)...)`. The gallery, lists, people and testimonials date an item this way, and [`../modules/publishing/api.go`](../modules/publishing/api.go) adds a post's categories. A link of the guide shows neither its page nor media, so `apiLink` above passes `item.UpdatedAt` alone. Give lists none: an item that leaves a list leaves no newer date behind. An item whose answer counts other items gets none either, such as a directory entry or a document, whose category holds the number of its published items. See [Caching](api.md#caching).
- `api.Write(w, r, v)` answers with `v` as JSON. `api.NotFound(w, r)` answers 404 `not_found`, `api.Fail(w, r, err)` logs the error and answers 500 `internal_error`, and `api.Error(w, r, status, code)` answers another error.

Name the fields in snake_case and give times as `time.Time`, which JSON writes in RFC 3339. Show only what visitors see on the site: published content, as `ListPublished` and `Published` return it, never drafts or previews, and no personal data, such as visitors' entries. Every answer is then the same for everyone, and the output cache keeps it as it keeps pages; `env.Changed`, through the store's `Changed`, renders it again after a change. The API's rate limit and CORS apply to the module's endpoints too. [`../modules/publishing/api.go`](../modules/publishing/api.go) and [`../modules/events/api.go`](../modules/events/api.go) serve posts, categories and events this way. A test turns the API on with `sitetest.Options.Configure`, setting `config.API` to `api.Config{Enabled: true}`.

## Links to pages

The admin knows which published content links to which page. The editor of a published page lists that content and asks before the page leaves its address, and **Pages › Broken links** lists the links that lead to no page; see [Editing](editing.md#links-to-a-page). The app reads the text of the published pages itself. A module reports the links in its published content with `Parts.Links`, a `links.Func`: `func(ctx, locale) ([]links.Source, error)`, which lists what visitors can see in `locale`, never drafts, with a `links.Source` per item:

| Field | |
| --- | --- |
| `ID`, `Title` | The item, as editors know it. |
| `URL` | The item's own address on the site, such as `/en/news/hello`. Its relative links lead from there, as a browser follows them; empty, they lead from the home page of `locale`, as for a banner, which has no address of its own. |
| `EditURL` | The item's editor, which the editors of pages and items and the report link to: `env.AdminURL(admin.RouteName(env.Key, "edit"), "id", id, url.Values{"locale": {locale}})`. |
| `Markdown` | Texts in Markdown, such as a body. Their links count, images do not (`markdown.Links`). |
| `Addresses` | Link fields, such as the address of a button. |
| `Revision` | The revision `Markdown` comes from, for a `content.Revisions`: the links found in it are kept with the revision, as those of pages are, and not looked for again. Empty finds them again for every index. |

Only links to an address on the site count: a path, a relative link or a full URL on the host of `Config.BaseURL`; without `BaseURL`, no full URL counts. Links to other websites, `mailto:` and `tel:` links, media files (`media:`) and the addresses core serves itself (`env.IsCorePath`) are left out, and the query and the fragment are dropped. A link below a module page counts as found, since the module answers there. With `env.Pages` kept as `s.pages` and `env.AdminURL` as `s.adminURL`, as in [The public handler](writing-a-module.md#the-public-handler) and [Previews and review](module-admin.md#previews-and-review), the links module reports the address of each link, which counts when it leads to the site itself:

```go
// linkSources lists the published links in locale for the admin: an
// address counts when it leads to a page of the site.
func (s *Service) linkSources(ctx context.Context, locale string) ([]links.Source, error) {
    base, err := s.pages.MountURL(ctx, s.key, locale)
    if err != nil {
        return nil, err
    }

    items, err := s.items.ListPublished(ctx, s.key, locale)
    if err != nil {
        return nil, err
    }

    sources := make([]links.Source, 0, len(items))

    for _, item := range items {
        text, _ := item.Text(locale)

        sources = append(sources, links.Source{
            ID:        item.ID,
            Title:     text.Title,
            URL:       base,
            EditURL:   s.adminURL(admin.RouteName(s.key, "edit"), "id", item.ID, url.Values{"locale": {locale}}),
            Addresses: []string{item.Fields.URL},
        })
    }

    return sources, nil
}
```

`Setup` returns it with `Links: service.linkSources`. `links.Source` is of go-webthings' package [`links`](../links/links.go): the example's package has the same name, and Go lets it import that package as it is. The app calls these functions only to make its index of links, for the editor of a published page or item and for the report, never for a visitor's request. It keeps the index until what visitors see changes on this server, as for the output cache, or until `Config.CacheTTL` has passed, so changes on other servers show too. A function that fails is logged, and the links of the other modules still count. [`../modules/publishing/links.go`](../modules/publishing/links.go) reports posts with their revision and [`../modules/banners/links.go`](../modules/banners/links.go) the address of a banner's button.

A module that redirects old addresses, as the [redirects module](../modules/redirects/README.md) does, sets `Parts.Redirects` to a `links.Redirector`: `Redirected(ctx, address)` returns where a redirect sends the visitors of an address on the site, such as `/en/about`, and `Redirect(ctx, address, target)` sends them to `target` with a permanent redirect from now on. The admin then offers redirects in the editors of pages and items and in the report to users with the create right in the section whose ID is the instance's key. The first module that sets it is used.

The page editor shows its notices through `editor.Config.Edit`, which every editor section can set. It runs when the edit screen of an existing item is built, before it renders, with the request, the `Editor`, the item, the language and the `*editor.EditView`. It may add `Notices`, shown above the form: each an `editor.Notice` with a `Text`, `Items`, which are `editor.NoticeItem` values with a `Label`, an optional `URL` and a `Note` shown muted after it, and `Forms`. It may also change or replace the view's forms, such as with a question before **Unpublish** (`Admin.Confirm`) or with a form that posts to a route of the module's own in `Routes`. Texts are shown as written, so translate them first, such as with `admin.T`. An error from `Edit` answers the request with status 500. A route of the module's own explains a refused change with `e.Issue(r, err)`, the translated message the editor shows for an error: that of `Config.Issue`, or the editor's own for the errors of package `content`, or `""` for an unexpected error. It checks a change that takes the item off the website with `e.RefusePermanent(w, r)` first; see [Deleting and the trash](module-admin.md#deleting-and-the-trash).

The editor of items with an address of their own, such as posts, gets the same notices and redirects as a page's editor from `editor.Linked`; see [Links to an item](editing.md#links-to-an-item). Its `Check` is the site's link check, which `env.Links()` returns, and its `Address` returns where visitors find an item on the site in a language once it is published, for any revision of it: `""` when it has none there, such as while the instance is on no page. `Linked` asks it only for the languages in which the item is complete (`content.Spec.Available`). With the posts of [Previews and review](module-admin.md#previews-and-review) at `routes.Get("/{slug}", s.postPage).As("post")` and the `paths` method of [The public handler](writing-a-module.md#the-public-handler):

```go
// postAddress is the address of a post, in any revision, on the site in
// locale, such as /en/partners/hello; "" while the instance is on no page.
func (s *Service) postAddress(ctx context.Context, item PostItem, locale string) (string, error) {
    paths, err := s.paths(ctx, locale)
    if err != nil {
        return "", err
    }

    text, _ := item.Text(locale)

    // A post without a slug in locale has no address there.
    address, _ := paths("post", "slug", text.Slug)

    return address, nil
}
```

The methods of `Linked` are hooks of the `editor.Config` of the posts, whose form for one language is a `postInput`:

```go
linked := editor.Linked[PostFields, PostText, postInput]{Check: env.Links(), Address: service.postAddress}

posts := editor.New(editor.Config[PostFields, PostText, postInput]{
    // ID, Store, Form, Apply and the rest, as for the links.
    Edit:   linked.Edit,
    Routes: linked.Routes,
    Saved:  linked.Saved,
})
```

- `Edit` lists the published content that links to the item's address and asks before the item is unpublished or moved to the trash. Users who may unpublish it and add redirects (`editor.Redirector`) also get **Unpublish and redirect**. For a `content.Revisions`, it shows how publishing the revision changes the address. The publish form then redirects the old addresses for users who may add redirects, and asks first for other users while content links to the old address.
- `Routes` adds the routes these forms post to: `unpublish-redirect`, and `revision-publish-moved` for a `content.Revisions`, both with `admin.Publish`. A user without the create right in the redirects module gets status 403 from the first; the second then publishes without redirects.
- `Saved` redirects the old addresses of a published item of a `content.Collection` to its new ones when it is saved with other addresses, such as a new slug, and the user may add redirects.

`Below`, when set, returns the addresses below the item's address in a language that go offline with the item, such as the entries of a directory's category: `func(ctx, item, locale) ([]string, error)`, asked only for the languages in which the item is complete. `Edit` then counts the links to them apart from those to the item itself, as "1 published item links to an address below this item.", and asks "Published items link to addresses below this item. Unpublish it anyway?" while only such links exist. **Unpublish and redirect** redirects them too, to the same page. `Saved` gives only the item's own address a redirect, so a module whose addresses below the item keep working under its new slug sends them on itself, as the directory does for an entry whose category was renamed. Without `Below`, links below the item's address count as links to the item. [`../modules/directory/module.go`](../modules/directory/module.go) sets `Below: service.entryAddresses`.

`editor.Config.Saved` runs after an existing item is saved, with the request, the `Editor`, the item before and after the save and the language. It may act on the change and returns the toast, a `*flash.Message` of go-partial's `flash` package, or nil for the editor's own: "Changes saved." for a `content.Collection` and "Saved as a draft. Publish it to show it on the website." for a `content.Revisions`. A module with hooks of its own calls the methods of `Linked` from them, as [`../modules/gallery/module.go`](../modules/gallery/module.go) does with `Routes`. [`../modules/publishing/links.go`](../modules/publishing/links.go) gives the addresses of a post and of a category, whose editor uses `Linked` too, and [`../modules/directory/links.go`](../modules/directory/links.go) those of an entry, below its category, and of a category.

Screens of a module's own use the same parts. `editor.Redirector(r, admin, check)` returns the module that redirects when the user may add redirects to it, and nil otherwise. `env.Links().Table(ctx)` returns the published page table, from which `editor.RedirectTargets(table, locale, leaving, selected)` lists the pages to redirect to, for the `Target` of an `editor.RedirectInput`: without the page `leaving` and the pages below it, unless it is empty, and with `selected` chosen. `editor.MovedInput` is the publish form with the box **Redirect the old addresses to the new ones**, and `editor.PublishLinked` the question before a revision is published that leaves an address published content links to. `editor.LinkItems` lists the content that links, from `Index.To`, as notice items linked to its editor, and `editor.LinkItem` names one. `editor.RedirectedNotice` is the toast after a change, which says how many old addresses redirect now.
