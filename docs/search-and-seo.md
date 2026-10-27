# Search and SEO

The site search finds published pages and module content in the visitor's language. WebThings also serves sitemap.xml and robots.txt, and writes the tags in `<head>` for titles, descriptions, links between languages, links to feeds and to the content API, sharing and structured data, or gives a theme's own head region what it needs for them.

## The search screen

`/<locale>/search?q=…`, such as `/en/search?q=glaze`, searches the published content of that language and renders `search.gohtml` in the layout. Write a starting point with:

```sh
task templates -- site
```

The template gets a `frontend.SearchView`:

| Field | Meaning |
| --- | --- |
| `Query` | The search as typed, trimmed to at most 200 characters. |
| `Results` | At most 20 results, best first. |
| `URL` | The search screen in this language, for the form's `action`. |
| `Throttled` | Set when the visitor searched too often in a short time: the search did not run and `Results` is empty. |

Each result is a `frontend.SearchResult`:

| Field | Meaning |
| --- | --- |
| `Title` | The page's or item's title. |
| `Summary` | About 200 characters of plain text around the first match. |
| `URL` | The address of the page or item. |
| `Module` | The module instance key, such as `news`; empty for pages. |
| `Type` | The module type, such as `publishing`, or `page`. |
| `Score` | How well it matches; results are sorted by it. |

```gotemplate
{{/* @dot github.com/donseba/go-webthings/frontend.SearchView */}}
<main id="content">
    <form action="{{.URL}}" method="get" role="search">
        <input type="search" name="q" value="{{.Query}}" aria-label="{{tl localizer "Search"}}">
        <button>{{tl localizer "Search"}}</button>
    </form>

    {{if .Throttled}}
        <p>{{tl localizer "Too many searches in a short time. Wait a moment and try again."}}</p>
    {{else if .Query}}
        {{range .Results}}
            <article class="result-{{.Type}}">
                <h2><a href="{{.URL}}">{{.Title}}</a></h2>
                {{with .Summary}}<p>{{.}}</p>{{end}}
            </article>
        {{else}}
            <p>{{tl localizer "Nothing found for “%s”." .Query}}</p>
        {{end}}
    {{end}}
</main>
```

A search form in the layout points to `{{searchURL}}`, the search screen in the current language:

```gotemplate
<form action="{{searchURL}}" method="get" role="search">
    <input type="search" name="q" aria-label="{{tl localizer "Search"}}">
</form>
```

- The layout's `.Title` is the query, and the language switch leads to the same search in the other languages.
- A site without `search.gohtml` answers "not found" at `/search`.
- A published page with the slug `search` directly below the home page takes the address instead of the search screen.

Each visitor may run 30 searches a minute on the screen (`frontend.DefaultSearchThrottle`). A search beyond that does not run: the visitor gets the search screen with status 429, without results and with `.Throttled` set, so the template can say why, as above. htmx 4 swaps that answer into a live search like any other; see [HTMX](htmx.md#fragments). The screen without a query and the `search` function do not count. Visitors are told apart by IP address, so list a reverse proxy in `Config.TrustedProxies` (see [Visitor forms](visitor-forms.md#knowing-the-visitor)). The counts are kept in memory: a restart clears them, and each server counts its own. `app.Frontend.SetSearchThrottle(limit, window)` sets another limit; a limit of 0 turns it off.

## How matching works

A result must contain every word of the query, in its title or its text. Case and accents are ignored in every script, so "ecole" finds "École". Words also match inside longer words: "pot" finds "pottery".

A word in the title counts 10 and a word in the text 1; a query of several words that appears whole in the title gets 5 more. Title matches therefore come first. Results with the same score are sorted by title.

There is no special syntax: quotes and operators are plain characters. Only the first 10 different words of a query count.

The search runs in Go over the published content, so it behaves the same on SQLite, PostgreSQL and MySQL. It reads all published content for every search, which suits sites up to a few thousand items. The plain text of each published page revision is kept in memory, so a page's Markdown is turned into searchable text once, not for every search.

## What is searched

- **Pages**: the title, description and content of every published page in the language, including the own text of pages that hold a module.
- **Modules**: the publishing, events, gallery, lists, people, directory and documents modules add their published content. Their READMEs list the fields they search.

A module's content is only found while its instance is placed on a page, because a result needs an address. A module that fails is logged and left out, so it cannot break the search. Your own module joins with `Parts.Search` and `frontend.ParseQuery`; see [Writing a module](writing-a-module.md).

The same search works in any template, in the current language:

```gotemplate
{{range search "workshop" 3}}
    <a href="{{.URL}}">{{.Title}}</a>
{{end}}
```

## sitemap.xml

`/sitemap.xml` lists every published page in every enabled language, each with links to the same page in all the languages it has. A page in more than one language also links to its address in the default language as `x-default`, for visitors whose language the site does not have; a page without a translation in the default language has none. Below a module page it adds the module's own addresses, such as each post and category of a news instance. The publishing, events, gallery, people and directory modules add their items, and the documents module its category pages. Feeds, such as a news page's `feed.atom` and `feed.rss`, are not in it. On a site whose default language is Dutch:

```text
<url>
  <loc>https://example.com/nl/over-ons</loc>
  <xhtml:link rel="alternate" hreflang="nl" href="https://example.com/nl/over-ons"></xhtml:link>
  <xhtml:link rel="alternate" hreflang="en" href="https://example.com/en/about"></xhtml:link>
  <xhtml:link rel="alternate" hreflang="x-default" href="https://example.com/nl/over-ons"></xhtml:link>
</url>
<url>
  <loc>https://example.com/nl/nieuws/open-atelier</loc>
</url>
```

The addresses start with `Config.BaseURL`. Without it, each request's own address is used: `https` when the request came over TLS, and its `Host` header. `X-Forwarded-Proto` and `X-Forwarded-Host`, or else the `proto=` and `host=` of `Forwarded`, count only from proxies listed in `Config.TrustedProxies`. Set `BaseURL` in production; see [Configuration](configuration.md).

The sitemap is built again as soon as what visitors see changes on the same server, so a page or item published there, in the admin or from Go, is in the next sitemap at once, also while the [output cache](deployment.md#the-output-cache) keeps the sitemap like a page. Otherwise it is built at most once every 10 seconds. A change made by another server shows up once this server reads the published pages again (`Config.CacheTTL`, 30 seconds by default), in a sitemap at most 10 seconds later, plus the output cache's time, 1 minute by default. It lists at most 50,000 addresses, the most a sitemap file may hold; beyond that, the last ones are left out and a warning is logged. A module that fails is logged and left out, so it cannot break the sitemap.

## robots.txt

`/robots.txt` allows everything and points to the sitemap:

```text
User-agent: *
Allow: /
Sitemap: https://example.com/sitemap.xml
```

It does not name the admin, so an admin path of your own stays private (see [Admin path](configuration.md#admin-path)). Admin pages are sent with `X-Robots-Tag: noindex, nofollow` instead, which keeps them out of search results also when other sites link to them.

Both files are served by WebThings itself; a site cannot replace them with a page.

## The head region

The tags of this guide, for the title, the description, robots, the canonical and `hreflang` links, the feeds, the content API, sharing and structured data, are the head region, which the layout places in its `<head>` with `{{slot "head"}}`. A theme without `regions/head.gohtml` gets core's. On `/en/about`, a page with a description, on a site in English and Dutch with `Config.BaseURL` set to `https://example.com`, core writes this, here without its empty lines and with the JSON-LD shortened:

```html
<title>About · My site</title><template data-partial-head="head"></template>
<meta name="description" content="Who we are">
<link rel="canonical" href="https://example.com/en/about">
<link rel="alternate" hreflang="en" href="https://example.com/en/about">
<link rel="alternate" hreflang="nl" href="https://example.com/nl/over-ons">
<link rel="alternate" hreflang="x-default" href="https://example.com/en/about">
<meta property="og:title" content="About">
<meta property="og:description" content="Who we are">
<meta property="og:type" content="website">
<meta property="og:url" content="https://example.com/en/about">
<meta property="og:site_name" content="My site">
<meta property="og:locale" content="en">
<meta name="twitter:card" content="summary">
<script type="application/ld+json">…</script>
<template data-partial-head-end="head"></template>
```

A tag without a value is left out, such as the description of a page without one; the favicon, the apple-touch-icon, `og:image`, the feed links and the link to the content API follow when the site has them. `og:type` is `article` on a screen whose first structured data item is an `Article`, such as a post, and `website` elsewhere.

A theme adds tags of its own in `regions/head.gohtml`, after core's tags; a head region that does not call them replaces them, and gets the same View as the layout to write its own:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/frontend.View */}}
{{template "webthings/head-tags.gohtml" .}}
<meta name="theme-color" content="#336699">
```

A layout without `{{slot "head"}}` gets nothing from core. An HTMX navigation replaces the region's tags with those of the new page, so the description, alternates, feed links and structured data of the page left do not stay behind. A Back or Forward that htmx restores from its history is the exception (see [HTMX](htmx.md#the-head-region)). `task templates -- site` writes a head region that calls core's tags (see [Themes](themes.md#the-head-region)).

## Titles and descriptions

The layout and its regions get a title, a description and an image for the current screen:

| Screen | `.Title` | `.Description` | `.Image` |
| --- | --- | --- | --- |
| Page | the page's title | the page's **Summary** | empty |
| Module list | set by the module, usually the page's title | set by the module | set by the module |
| Module item, such as a post | the item's title | set by the module, such as a post's excerpt | set by the module, such as a featured image |
| Search | the query | empty | empty |
| Not found | "Not found", translated with the site's `tl` | empty | empty |

The site settings add texts per language: `.Site.Name` (**Site title**), `.Site.Tagline` (**Tagline**), `.Site.SEOTitle` (**SEO title**) and `.Site.SEODescription` (**SEO description**). Core's head tags use them:

- The `<title>` is the screen's title and the site's name, such as `About · My site`, or the site's name alone on a screen without a title. On the home page the SEO title, when there is one, is the whole `<title>` and the `og:title`; no other page shows it.
- `og:title` is the screen's title, or else the site's name.
- The description, also as `og:description`, is the screen's own, or else the SEO description, or else the tagline. Without any of them the page has none.

A theme whose head region replaces core's tags decides itself. For example, the SEO title on the home page and the SEO description where a screen has no description of its own:

```gotemplate
{{if and .Page .Page.IsHome .Site.SEOTitle}}
    <title>{{.Site.SEOTitle}}</title>
{{else}}
    <title>{{if .Title}}{{.Title}} · {{end}}{{.Site.Name}}</title>
{{end}}

{{with or .Description .Site.SEODescription}}<meta name="description" content="{{.}}">{{end}}
```

`.Image` is the image the module gives in `frontend.Content.Image`, such as a post's featured image, as a 1200 × 630 JPEG cut around the image's [focal point](media.md#the-focal-point): the size social networks and chat apps expect. An image too small for that gives a smaller copy with the same proportions. `.Site.SocialImageURL` is the **Social image** of the site settings, made the same way, for screens without an image of their own. Both are paths such as `/media/v/…`, whatever the storage; an address outside the media library is used as it is. Social networks need a full address. `{{absURL}}` makes one with `Config.BaseURL`, or with the request's address like `sitemap.xml`. An address with a scheme or a host, such as `https://…` or `//cdn.example.com/…`, stays as it is:

```gotemplate
{{with or .Image .Site.SocialImageURL}}<meta property="og:image" content="{{absURL .}}">{{end}}
```

Core's head tags write this `og:image`, with `<meta name="twitter:card" content="summary_large_image">`; a screen without either image gets `summary`.

The **Favicon** of the site settings gives two copies, both cut square around the focal point: `.Site.FaviconURL`, a 32 × 32 PNG for the browser's tab, and `.Site.AppleTouchIconURL`, a 180 × 180 PNG for the icon phones put on their home screen. Core's head tags link both:

```gotemplate
{{with .Site.FaviconURL}}<link rel="icon" href="{{.}}" sizes="32x32">{{end}}
{{with .Site.AppleTouchIconURL}}<link rel="apple-touch-icon" href="{{.}}">{{end}}
```

`.Site.LogoURL` is a copy of the **Logo** at most 600 pixels wide, never enlarged, in the format it was uploaded in, since WebP would blur its sharp edges: a JPEG gives a JPEG and a PNG a PNG. A PNG at most 600 pixels wide, and a GIF of any size, keep their own address. A theme's own logo, such as an SVG in its assets, is not a setting: link it with `{{asset "logo.svg"}}`. Each copy is made on its first request, not while the page renders; see [The site's images](media.md#the-sites-images) and [How the copies are made](media.md#how-the-copies-are-made).

## Canonical and alternate links

Every page has one address: other spellings, such as a trailing slash, redirect to it (see [Pages](pages.md#canonical-addresses)). `.Path` in the layout and its regions is that address with its language prefix and without query string, so a canonical link is:

```gotemplate
<link rel="canonical" href="{{absURL .Path}}">
```

`.Languages` gives the same content in each language where it is published, including the current one, for `hreflang` links. Their `URL` is a path; search engines want full addresses:

```gotemplate
{{range .Languages}}<link rel="alternate" hreflang="{{.Locale}}" href="{{absURL .URL}}">{{end}}
{{if gt (len .Languages) 1}}{{range .Languages}}{{if .Default}}<link rel="alternate" hreflang="x-default" href="{{absURL .URL}}">{{end}}{{end}}{{end}}
```

The second line names the address in the default language, the one with `.Default` set, as `x-default`, for visitors whose language the site does not have. It writes nothing when the content is in one language only, or not in the default language. Core's head tags have the canonical link and both lines. A page with status 400 or above, such as the not-found page or a module's 404, has none of them and no `og:url`: it has no address of its own to give.

On a module's screen below its page, such as a post, the links lead to the translations of the content shown when the module gives them in `frontend.Content.Alternates`, as the publishing, events, gallery, people and directory modules do for their items. A screen whose module gives none links to the module's page in the other languages, and to itself in its own language, so its `hreflang` link for the current language and its canonical link are the same address. On the screen `/en/ticker/topic/harbour` of a ticker module without alternates, on a site whose default language is English:

```html
<link rel="canonical" href="https://example.com/en/ticker/topic/harbour">
<link rel="alternate" hreflang="en" href="https://example.com/en/ticker/topic/harbour"><link rel="alternate" hreflang="nl" href="https://example.com/nl/tikker">
<link rel="alternate" hreflang="x-default" href="https://example.com/en/ticker/topic/harbour">
```

`sitemap.xml` carries the alternates of every page with full addresses. [Languages](languages.md#the-language-switch) explains which languages are listed on which screen.

## Feeds

Modules can offer Atom and RSS feeds, such as the posts of a news page at `/en/news/feed.atom` and `/en/news/feed.rss`. `{{feedLinks}}`, which core's head tags have, makes them discoverable: it writes a `<link rel="alternate">` for each feed of the page shown, with absolute addresses, and nothing on pages without feeds or on error pages (see [Template functions](template-functions.md#feedlinks)):

```gotemplate
{{feedLinks}}
```

A link visitors see takes the feed's address from `{{feedURL "atom"}}` or `{{feedURL "rss"}}` (see [Feeds](themes.md#feeds) in Themes). `sitemap.xml` never lists feeds. An entry of a feed has an ID that stays the same when its address changes: a `tag:` URI on the site's host, such as `tag:example.com,2026-10-01:news/<post ID>` for a post of the news instance created that day. The host is that of `Config.BaseURL`, or else of the request, so set `BaseURL` for IDs that do not change with the address a reader used. A module offers feeds with `Parts.Feeds`; see [Feeds](module-feeds-and-links.md#feeds) for modules.

## A link to the content API

While the [content API](api.md) is on, `{{apiLink}}` in the head region links each page to its content as JSON, for apps: a `<link rel="alternate" type="application/json">` to `/api/v1/pages/<id>?locale=<locale>` for a page, and to `/api/v1/pages/resolve?path=<address>` below a module's page, such as for a post, with an absolute address (see [Template functions](template-functions.md#apilink)):

```gotemplate
{{apiLink}}
```

It writes nothing while the API is off, on an error page, such as the not-found page or a module's 404 for a post that does not exist, and in a preview, which the API never shows. Core's head tags have it after `{{feedLinks}}`.

## Structured data

`{{structuredData}}`, the last of core's head tags, tells search engines what a page is about, as [schema.org](https://schema.org) items in JSON-LD. It writes one `<script type="application/ld+json">` with `@context` and an `@graph` of the page's items (see [Template functions](template-functions.md#structureddata)):

```gotemplate
{{structuredData}}
```

| Item | What it holds |
| --- | --- |
| `WebSite` | The site in the page's language: the **Site title** as `name`, the **SEO description**, the home page as `url`, the language as `inLanguage` and the organisation as `publisher`. When the theme has `search.gohtml`, a `SearchAction` leads to the search screen, such as `https://example.com/en/search?q={search_term_string}`. |
| `Organization` | The organisation behind the site, from the site settings: the **Organisation** as `name`, or the **Site title** when it is empty, the **Logo** at most 600 pixels wide (`.Site.LogoURL`), the **Email**, **Phone** and **Address**, and the social media profiles as `sameAs`. |
| `BreadcrumbList` | The page's place in the page tree, from the home page down to the page, and to the module's screen below it, such as a post. The home page has none, and neither do screens outside the page tree, such as the search screen. |
| `WebPage` | The page itself, at its canonical address: its title, description and image (`.Title`, `.Description` and `.Image`), its language, the site and the trail. The search screen is a `SearchResultsPage`. |
| The module's items | What a module's screen shows, such as a post's `Article`; see below. |

The items name each other by `@id`. The organisation's `@id` is the same on every page and in every language, such as `https://example.com/#organization`, so an article names it as its author and an event as its organiser. The site is always an `Organization`, never a `LocalBusiness`: the site settings hold the address as free text and no opening hours, and not every organisation with an address is a business that receives visitors. A directory entry with opening hours is a `LocalBusiness`.

The modules add an item for the content a screen shows:

| Module | Screen | Item |
| --- | --- | --- |
| [Publishing](../modules/publishing/README.md) | a post | `Article`: the title as `headline`, the excerpt as `description`, the featured image as a 1200 × 630 JPEG, the post's date as `datePublished`, when the revision shown was saved as `dateModified`, the language, the categories as `articleSection`, and the organisation as `author` and `publisher` |
| [Events](../modules/events/README.md) | an event | `Event`: the title, summary and image, `startDate` and `endDate` in the event's own timezone, such as `2026-11-07T14:00:00+01:00`, or the day alone for an all-day event, `eventStatus` `EventScheduled`, the **Location** and **Address** as a `Place`, the language, and the organisation as `organizer` |
| [Registrations](../modules/registrations/README.md) with `ForEvents` | an event of that agenda | an `Offer` in the event's `offers` while visitors can register (`InStock`) or every place is taken (`SoldOut`): the adult price, the currency, the event's page as `url` and the time registration closes as `validThrough` |
| [People](../modules/people/README.md) | a person | `Person`: the name, the role as `jobTitle`, the photo, e-mail address and phone number, the website and LinkedIn address as `sameAs`, and the organisation as `worksFor` |
| [Directory](../modules/directory/README.md) | an entry | `LocalBusiness` when the entry has opening hours, `Place` when it has a street, a city or a position, `Organization` otherwise, with its address, position and contact details |
| [Publishing](../modules/publishing/README.md), [Events](../modules/events/README.md), [People](../modules/people/README.md), [Directory](../modules/directory/README.md) and [Gallery](../modules/gallery/README.md) | a list: the posts of a news page or a category, the upcoming or past events, the people, the entries of a directory, also when searched or filtered, or of a category's page, and the albums | `ItemList`: the page's title as `name`, or the category's on a category's page, the number of items as `numberOfItems`, and a `ListItem` for each item, with its `position` and the address of its page as `url`, in the order the page shows them. A list without items has none |

The module's first item is the page's main entity: its `mainEntityOfPage` is the page's address. The other modules add no item, also on their lists. A module of your own adds items with `frontend.Content.StructuredData`, and adds to the items of other modules, as registrations does, with `Parts.StructuredData`; see [Structured data](module-feeds-and-links.md#structured-data) for modules.

Every address in it is absolute, with `Config.BaseURL` or the request's address, as `{{absURL}}` makes it. Empty values are left out, so a page without a description has no `description` and a site without a logo an `Organization` without `logo`. The not-found page, a module's screen with a status of 400 or more, and the theme's error page, `500.gohtml`, have no structured data. That holds for every failing page: a page whose template fails, and a module's handler or a visitor action that fails (`Frontend.Fail`). html/template escapes `<`, `>` and `&` in the JSON, so text such as a title with `</script>` cannot end the script.

On the post `/en/news/a-new-kiln`, with `Config.BaseURL` set to `https://example.com`, it writes this on one line; here it is laid out, with the media paths shortened:

```json
{
  "@context": "https://schema.org",
  "@graph": [
    {
      "@id": "https://example.com/en/#website",
      "@type": "WebSite",
      "description": "Ceramics studio in Amsterdam Noord",
      "inLanguage": "en",
      "name": "Atelier Noord",
      "potentialAction": {
        "@type": "SearchAction",
        "query-input": "required name=search_term_string",
        "target": {"@type": "EntryPoint", "urlTemplate": "https://example.com/en/search?q={search_term_string}"}
      },
      "publisher": {"@id": "https://example.com/#organization"},
      "url": "https://example.com/en/"
    },
    {
      "@id": "https://example.com/#organization",
      "@type": "Organization",
      "address": "NDSM-Plein 28, Amsterdam",
      "email": "hello@example.com",
      "logo": "https://example.com/media/v/…_600x0.png",
      "name": "Atelier Noord",
      "sameAs": ["https://www.instagram.com/ateliernoord"],
      "telephone": "+31 20 123 4567",
      "url": "https://example.com/en/"
    },
    {
      "@id": "https://example.com/en/news/a-new-kiln#breadcrumb",
      "@type": "BreadcrumbList",
      "itemListElement": [
        {"@type": "ListItem", "item": "https://example.com/en/", "name": "Home", "position": 1},
        {"@type": "ListItem", "item": "https://example.com/en/news", "name": "News", "position": 2},
        {"@type": "ListItem", "item": "https://example.com/en/news/a-new-kiln", "name": "A new kiln", "position": 3}
      ]
    },
    {
      "@id": "https://example.com/en/news/a-new-kiln",
      "@type": "WebPage",
      "breadcrumb": {"@id": "https://example.com/en/news/a-new-kiln#breadcrumb"},
      "description": "The kiln arrived.",
      "image": "https://example.com/media/v/…_1200x630c.jpg",
      "inLanguage": "en",
      "isPartOf": {"@id": "https://example.com/en/#website"},
      "name": "A new kiln",
      "url": "https://example.com/en/news/a-new-kiln"
    },
    {
      "@type": "Article",
      "articleSection": ["Studio"],
      "author": {"@id": "https://example.com/#organization"},
      "dateModified": "2026-10-07T09:54:26Z",
      "datePublished": "2026-10-01",
      "description": "The kiln arrived.",
      "headline": "A new kiln",
      "image": "https://example.com/media/v/…_1200x630c.jpg",
      "inLanguage": "en",
      "mainEntityOfPage": {"@id": "https://example.com/en/news/a-new-kiln"},
      "publisher": {"@id": "https://example.com/#organization"},
      "url": "https://example.com/en/news/a-new-kiln"
    }
  ]
}
```

## Keeping pages out of search engines

- Previews of revisions, a page's address with `?revision=<id>`, are never indexed or cached. Only signed-in editors see them, and they are sent with `X-Robots-Tag: noindex, nofollow` and `Cache-Control: private, no-store`, without an `ETag`. The View's `.Preview` is set there too, and core's head tags then write `<meta name="robots" content="noindex">`; a head region that replaces core's tags writes it with `{{if .Preview}}<meta name="robots" content="noindex">{{end}}`. See [Editing and publishing](editing.md#previewing-a-revision).
- The search screen is sent with `X-Robots-Tag: noindex`: its results are endless variations of the site's content.
- Admin pages are sent with `X-Robots-Tag: noindex, nofollow`; `robots.txt` does not name the admin.

There is no per-page "noindex" setting. A head region can add the meta tag for chosen paths, such as `{{if eq .Path "/en/thanks"}}<meta name="robots" content="noindex">{{end}}`.
