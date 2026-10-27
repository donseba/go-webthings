# Content API

The content API serves the site's published content as JSON at `/api/v1/`, for apps and other frontends that show it.

## Turn it on

The API is off by default. `Config.API`, an `api.Config` of package [`api`](../api/api.go), turns it on:

```go
// in the Config:
API: api.Config{Enabled: true},
```

| Field | Default | What it does |
| --- | --- | --- |
| `Enabled` | `false` | Serves the API at `/api/v1/` |
| `AllowedOrigins` | none: no CORS headers | The sites whose pages may read the API in the browser; see [Browsers on other sites](#browsers-on-other-sites) |
| `RateLimit` | 300 | How many requests one client may make per `RateWindow`; negative turns the limit off; see [Rate limit](#rate-limit) |
| `RateWindow` | a minute | The time `RateLimit` counts in |

While the API is off, `/api/v1/` is an address of the site like any other, which has no page there: it gets the site's 404 page. While it is on, `/api/v1` is one of the app's own addresses (`App.CorePaths()`), so no language can have the code `api` and no top-level page the slug `api`. Set `Config.BaseURL`, so the addresses in the answers do not depend on the address a client used.

## Endpoints

Core serves the site, its search and its pages:

| Endpoint | Route name | Answers with |
| --- | --- | --- |
| `GET /api/v1/site` | `api.site` | The site in one language: its name, tagline and description, the languages, the menu and the module instances that have endpoints |
| `GET /api/v1/site/search?q=harbour` | `api.search` | The matches of the site search, best first, as a [list](#lists); without `q`, none |
| `GET /api/v1/pages` | `api.pages` | The published page tree: the home page with the pages below it |
| `GET /api/v1/pages/resolve?path=/en/about` | `api.resolve` | The page at an address of the site, as the site finds it |
| `GET /api/v1/pages/{id}` | `api.page` | A page by its ID |
| `GET /api/v1/<key>/…` | `api.<key>.<name>` | The endpoints of a module instance |

The route names build the addresses in Go: `app.Frontend.Route("api.page", "id", id)` is `/api/v1/pages/<id>`, and `app.Frontend.Route("api.news.post", "id", id)` is `/api/v1/news/posts/<id>`. The answers link to each other, so a client can also follow their `api_url` and `api_base` fields from `/api/v1/site`.

These bundled modules have endpoints below `/api/v1/<key>/`: their published items as a [list](#lists), and each item by its ID:

| Module | Endpoints |
| --- | --- |
| [Publishing](../modules/publishing/README.md#content-api) | `/posts`, newest first, also of one category with `?category=<slug>`; `/posts/{id}`; `/categories` |
| [Events](../modules/events/README.md#content-api) | `/events`, the upcoming and running events, soonest first, or the past ones with `?past=true`; `/events/{id}` |
| [Gallery](../modules/gallery/README.md#content-api) | `/albums`, in the editors' order; `/albums/{id}`, with its images |
| [Lists](../modules/lists/README.md#content-api) | `/entries`, in the editors' order; `/entries/{id}` |
| [People](../modules/people/README.md#content-api) | `/people`, in the editors' order, also of one group with `?group=<name>`; `/people/{id}` |
| [Testimonials](../modules/testimonials/README.md#content-api) | `/testimonials`, in the editors' order; `/testimonials/{id}` |
| [Directory](../modules/directory/README.md#content-api) | `/entries`, by name, also of one category with `?category=<slug>`; `/entries/{id}`; `/categories` |
| [Documents](../modules/documents/README.md#content-api) | `/documents`, newest first, also of one category with `?category=<slug>`; `/documents/{id}`; `/categories` |

The lists of Publishing, Events, Gallery, People and Directory also take `?slug=<slug>`, for the item at an address below the instance's page (see [Pages and addresses](#pages-and-addresses)). A `?category=` that names no published category is answered with 404 `not_found`, as the site's page of such a category is. Lists, Testimonials and Documents, whose items have no page of their own, give each item whole in their lists; Publishing, Events, Gallery, People and Directory give an item's text only at `/{id}`. The modules' READMEs describe their fields. The other bundled modules have no endpoints. A module of your own adds endpoints with `Parts.API`; see [The content API](module-feeds-and-links.md#the-content-api) for modules.

## Languages

Every endpoint answers in one language: that of its `locale` parameter, such as `?locale=nl`, or the site's default language without it. `/pages/resolve` takes the language from the address instead. A `locale` the site does not offer is answered with 404 `unknown_locale`, and at `/pages/resolve` an address without one of the site's languages with 404 `not_found`. Content that is not published in the language asked for, such as a page without a Dutch translation, is answered with 404 `not_found`.

`/site` lists the languages with the site's address and `api_url` in each, and a page lists its `alternates`, the page in the other languages it is published in. `GET /api/v1/site`, abridged to two pages of the menu and two modules:

```json
{
  "name": "Test site",
  "tagline": "",
  "description": "",
  "locale": "en",
  "default_locale": "en",
  "url": "https://example.com/en/",
  "logo": null,
  "languages": [
    {
      "locale": "en",
      "url": "https://example.com/en/",
      "api_url": "https://example.com/api/v1/site?locale=en",
      "default": true
    },
    {
      "locale": "nl",
      "url": "https://example.com/nl/",
      "api_url": "https://example.com/api/v1/site?locale=nl",
      "default": false
    }
  ],
  "menu": [
    {
      "id": "01a115cb-afc4-795c-95cc-9c19bc764806",
      "title": "About",
      "path": "/about",
      "url": "https://example.com/en/about",
      "api_url": "https://example.com/api/v1/pages/01a115cb-afc4-795c-95cc-9c19bc764806?locale=en",
      "module": null,
      "children": [
        {
          "id": "01a115cb-b00a-767a-9b00-358490a550c7",
          "title": "Team",
          "path": "/about/team",
          "url": "https://example.com/en/about/team",
          "api_url": "https://example.com/api/v1/pages/01a115cb-b00a-767a-9b00-358490a550c7?locale=en",
          "module": null,
          "children": []
        }
      ]
    },
    {
      "id": "01a115cb-b03c-77a9-b00f-912918ca4ebc",
      "title": "News",
      "path": "/news",
      "url": "https://example.com/en/news",
      "api_url": "https://example.com/api/v1/pages/01a115cb-b03c-77a9-b00f-912918ca4ebc?locale=en",
      "module": {
        "key": "news",
        "title": "News",
        "type": "publishing",
        "url": "https://example.com/en/news",
        "api_base": "https://example.com/api/v1/news"
      },
      "children": []
    }
  ],
  "modules": [
    {
      "key": "news",
      "title": "News",
      "type": "publishing",
      "url": "https://example.com/en/news",
      "api_base": "https://example.com/api/v1/news"
    },
    {
      "key": "agenda",
      "title": "Agenda",
      "type": "events",
      "url": "https://example.com/en/agenda",
      "api_base": "https://example.com/api/v1/agenda"
    }
  ]
}
```

`menu` holds the pages below the home page, as the site's menu shows them; `/pages` gives the same tree with the home page at the top. A page with a module instance names it in `module`. `modules` lists the instances with endpoints; `url` is the page an instance is on, `null` while it is on none.

## Pages and addresses

`/pages/resolve` finds the published page at an address of the site, as the site does. `GET /api/v1/pages/resolve?path=/en/about`, with the long `srcset` shortened:

```json
{
  "id": "01a115cb-afc4-795c-95cc-9c19bc764806",
  "locale": "en",
  "title": "About",
  "description": "Who we are",
  "slug": "about",
  "path": "/about",
  "url": "https://example.com/en/about",
  "api_url": "https://example.com/api/v1/pages/01a115cb-afc4-795c-95cc-9c19bc764806?locale=en",
  "parent_id": "01a115cb-af90-727a-acc1-8df00267c0cc",
  "is_home": false,
  "body_html": "\u003cp\u003eRead the \u003ca href=\"https://example.com/en/news\"\u003enews\u003c/a\u003e.\u003c/p\u003e\n\u003cp\u003e\u003cimg src=\"https://example.com/media/v/wlN_-0Ps0_NzcXkN/01a115cb-afb9-7d37-a62d-7512dda0e551_1600x0.webp\" alt=\"The harbour\" srcset=\"https://example.com/media/v/hJrpW0UZS6JpKfmT/01a115cb-afb9-7d37-a62d-7512dda0e551_480x0.webp 480w, …\" sizes=\"100vw\" width=\"1600\" height=\"900\"\u003e\u003c/p\u003e\n",
  "updated_at": "2026-10-07T09:57:12.791656Z",
  "alternates": [
    {
      "locale": "nl",
      "url": "https://example.com/nl/over-ons",
      "api_url": "https://example.com/api/v1/pages/01a115cb-afc4-795c-95cc-9c19bc764806?locale=nl"
    }
  ],
  "breadcrumbs": [
    {
      "id": "01a115cb-af90-727a-acc1-8df00267c0cc",
      "title": "Home",
      "url": "https://example.com/en/",
      "api_url": "https://example.com/api/v1/pages/01a115cb-af90-727a-acc1-8df00267c0cc?locale=en"
    },
    {
      "id": "01a115cb-afc4-795c-95cc-9c19bc764806",
      "title": "About",
      "url": "https://example.com/en/about",
      "api_url": "https://example.com/api/v1/pages/01a115cb-afc4-795c-95cc-9c19bc764806?locale=en"
    }
  ],
  "module": null
}
```

`path` is the page's address without the language, `/` for the home page, whose `slug` is empty and `is_home` true. `parent_id` is `null` for the home page. `updated_at` is when the published revision was saved. `breadcrumbs` lead from the home page to the page, both included. `/pages/{id}` answers with the same page by its ID, in the language of `?locale=`.

An address below a page with a module instance, such as a post at `/en/news/hello-harbour`, resolves to that page, with the rest of the address in `module.path`:

```json
  "module": {
    "key": "news",
    "title": "News",
    "type": "publishing",
    "url": "https://example.com/en/news",
    "api_base": "https://example.com/api/v1/news",
    "path": "/hello-harbour"
  }
```

`module.path` is `/` for the page itself and is left out of every other answer. The instance's endpoints, below `api_base`, give its content: `GET /api/v1/news/posts?slug=hello-harbour` lists the post with that slug. An address without a published page is answered with 404 `not_found`.

Core's head tags point each page of the site to its answer here with [`{{apiLink}}`](template-functions.md#apilink): a `<link rel="alternate" type="application/json">` to `/pages/{id}` for a page, and to `/pages/resolve` for an address below a module's page, such as a post's. `Frontend.APILink(r)` gives the same address in Go.

## Responses

The answers follow the same rules everywhere:

- Names are in snake_case, such as `api_url` and `body_html`.
- Times are in RFC 3339, such as `"2026-10-07T09:57:12.791656Z"`. Times the site records, such as `updated_at`, are in UTC; an event's `start` and `end` carry the offset of its timezone, such as `"2027-06-12T14:30:00+02:00"`.
- Addresses are absolute, on `Config.BaseURL` or else the address of the request: `url` is the content on the site, `api_url` the same content in the API.
- A field without a value is `null`, such as the `image` of content without one or the `url` of content whose instance is on no page. Text without a value is `""` and an empty list `[]`. Two fields are left out instead: `module.path` outside `/pages/resolve`, and the text of a module's item with a page of its own, such as a post's `body_html` or an album's `description_html`, which only the item's own endpoint gives, when the item has text.
- Editors write text in Markdown. The API gives it as HTML in `body_html`, or in a field named after the text, such as `description_html` and `bio_html`, as the site shows it: in the language of the answer, with every address of the site absolute and images with their `srcset`, `sizes`, `width`, `height` and alt text. Relative links lead from the home page of that language, such as `https://example.com/en/`.
- Images of the media library are objects, as the site shows them:

| Field | |
| --- | --- |
| `url` | A WebP copy at most 1600 pixels wide, never wider than the upload |
| `srcset` | The smaller copies, for an `img` tag's `srcset` |
| `width`, `height` | The size of `url` |
| `alt` | The alt text in the language of the answer, as editors wrote it in the media library |
| `original_url` | The uploaded file |

JSON writes `<`, `>` and `&` in text as `\u003c`, `\u003e` and `\u0026`, as the examples show; a JSON parser reads them as the characters. Answers of 1 KiB or more are compressed for clients that accept gzip, as pages are (**Site settings › Performance**).

## Lists

A list is one page of the items, newest or best first as the endpoint says:

| Field | |
| --- | --- |
| `items` | The items of this page |
| `total` | How many items there are in all |
| `limit` | How many items a page holds: the `limit` parameter, 20 by default, at most 100 |
| `offset` | Where the page starts: the `offset` parameter, 0 by default |
| `next` | The absolute address of the next page, with the same parameters; `null` on the last page |

A `limit` or `offset` that is not a whole number, or is out of range, is the default or the nearest value allowed, so paging never fails. `GET /api/v1/site/search?q=harbour&limit=1`:

```json
{
  "items": [
    {
      "title": "Harbour fair",
      "summary": "Games and food",
      "url": "https://example.com/en/agenda/harbour-fair",
      "type": "events",
      "module": "agenda"
    }
  ],
  "total": 4,
  "limit": 1,
  "offset": 0,
  "next": "https://example.com/api/v1/site/search?limit=1\u0026offset=1\u0026q=harbour"
}
```

A search result's `type` is `page` or the module type, such as `publishing`, and `module` the instance key, `null` for a page. The search finds at most 100 matches, as the [site search](search-and-seo.md#the-search-screen) does in the visitor's language, but without its limit of 30 searches a minute: the API's [rate limit](#rate-limit) applies instead.

## Errors

An error is an object with the HTTP status, a code that stays the same and a message for people:

```json
{
  "error": {
    "status": 404,
    "code": "unknown_locale",
    "message": "Not Found"
  }
}
```

| Code | Status | When |
| --- | --- | --- |
| `not_found` | 404 | No published content at the address or in the language asked for, an address without an endpoint, or another method than `GET` or `HEAD` |
| `unknown_locale` | 404 | The site does not offer the language of `?locale=` |
| `rate_limited` | 429 | The client made more requests than the [rate limit](#rate-limit) allows |
| `internal_error` | 500 | The server failed; the log has the details |

The message is the status text, translated with the site's translations (`Config.Translations`) into the language of the answer. Errors are sent with `Cache-Control: no-cache` and without an ETag. The API answers no 400 or 405: a wrong method is not found, and paging parameters are corrected (see [Lists](#lists)).

## What the API shows

The API shows what visitors see on the site, nothing else: only published content, never a draft, a scheduled revision or a preview, also not to signed-in editors. Every answer is the same for everyone, so the API needs no session and the output cache can keep its answers. The modules show what their pages show, and no data of visitors: Registrations, Submissions, Forms, Comments, Polls and Newsletter have no endpoints, so sign-ups, proposals, form entries, comments, votes and subscribers never appear.

## Caching

The API's answers go through the [output cache](deployment.md#the-output-cache) as pages do: they are kept per address and query, and every change visitors see, such as a post that is published, has them made again. Searches with a query are not kept. Unlike pages, answers to signed-in editors are kept and served from the cache too, since they are the same.

Answers with status 200 have a weak `ETag`. A client that sends it again in `If-None-Match` gets `304 Not Modified` without a body while the answer has not changed. Their `Cache-Control` is that of pages, **Browsers and caches keep pages for** in **Site settings › Performance**: `no-cache` by default, so clients ask every time, or `public, max-age=…` with a time, with `stale-while-revalidate=…` when that is set too. Searches with a query and errors are always `no-cache`. See [Deployment](deployment.md#the-content-api).

A single page, post, event, album, list entry, person or testimonial also has a `Last-Modified`: `/pages/{id}`, `/pages/resolve`, `/<publishing>/posts/{id}`, `/<events>/events/{id}`, `/<gallery>/albums/{id}`, `/<lists>/entries/{id}`, `/<people>/people/{id}` and `/<testimonials>/testimonials/{id}`. It is the latest change of the item and of what the answer shows of other content: the pages above a page, the page a module instance is on, a post's categories, the media files in the text and the item's images, such as a featured image, a photo or an album's images, with their alt texts and focal points, and when an event ended, once it is past. A client that sends it in `If-Modified-Since` gets `304 Not Modified` while none of them changed, also from the output cache. An answer that shows something without a date gets none, such as a post in a category that was deleted, an item whose image was deleted, an instance on no page or a page below one that is not published, and so does a date ahead of the server's clock. Lists, the page tree, `/site` and the categories have none either, since an item that leaves a list leaves no newer date behind, and neither does a single directory entry or document: its category counts the published items in it, which another item changes. Their ETag answers `If-None-Match`. Changes to the site settings, such as the languages offered, do not move the date; the ETag covers them.

## Rate limit

Each client, known by its address, may make `RateLimit` requests per `RateWindow`: 300 a minute unless the configuration says otherwise. Answers from the output cache count too. More requests in the window are answered with 429 `rate_limited` and `Retry-After`, the length of the window in seconds, until earlier requests leave the window:

```go
API: api.Config{
    Enabled:    true,
    RateLimit:  60,
    RateWindow: 10 * time.Second,
},
```

A negative `RateLimit` turns the limit off. Each process counts in its own memory, so with several servers a client may make `RateLimit` requests on each. Behind a reverse proxy, list it in `Config.TrustedProxies`: otherwise every client has the proxy's address and they share one limit.

## Browsers on other sites

Without `AllowedOrigins`, the API sends no CORS headers: the site's own pages, apps and servers read it, the pages of other sites in a browser cannot. `AllowedOrigins` names the sites whose pages may:

```go
API: api.Config{
    Enabled:        true,
    AllowedOrigins: []string{"https://app.example.com", "*.example.org"},
},
```

An entry is an origin, such as `https://app.example.com`; `*` allows every site, and `*.example.org` the subdomains of `example.org`, over HTTP and HTTPS, but not `example.org` itself. Then:

- A request from an allowed origin gets `Access-Control-Allow-Origin`, with that origin or `*`, and `Access-Control-Expose-Headers: ETag, Last-Modified, Retry-After`, so scripts can read those headers.
- A request from another origin is answered with 403 without a body.
- A preflight request (`OPTIONS`) from an allowed origin is answered with 204, `Access-Control-Allow-Methods: GET, HEAD` and `Access-Control-Max-Age: 600`, so browsers keep it for ten minutes. Preflight requests do not count for the rate limit.
- Requests without an `Origin`, such as an app's or a server's, are answered as without `AllowedOrigins`.
- Every answer says `Vary: Origin`, also one to a request without an `Origin`, so caches between the site and its clients keep the answers for each origin apart and never give an answer without CORS headers to an allowed site. The output cache keeps one answer for all origins and adds the CORS headers to each request.

The API reads no cookies and needs no credentials, so `Access-Control-Allow-Credentials` is never sent.

## Endpoints of a module

A module serves its own content below `/api/v1/<key>/` with `Parts.API`, a router of `api.NewRouter()`, and answers with the helpers of package `api`, such as `api.Paginate` for lists, `api.ImageOf` for images and `api.Modified` with `api.ItemDates` for the `Last-Modified` of an item, so its answers follow the rules above. See [The content API](module-feeds-and-links.md#the-content-api) for modules.
