# Publishing

News, blogs and other streams of dated posts, with categories, an image, Markdown text, drafts, previews and scheduled publishing.

Use one instance per stream: a site can have `news` and `blog` side by side, each with its own posts, categories, admin section and templates.

## Add it to the site

```go
app.Use(
    publishing.Module("news", "News"),
    publishing.Module("blog", "Blog"),
)
```

The key (`news`) is the instance's name in URLs and templates and must be unique on the site. The title (`News`) is what editors see in the admin navigation and the page editor.

## Place it on a page

In the admin, open a page and choose the instance under **Shows on this page**. The page then serves the posts at and below its own address; with the page at `/news` (after the language prefix, such as `/en/news`):

| Address | Template | Shows |
| --- | --- | --- |
| `/news` | `news/list.gohtml` | the published posts, newest first |
| `/news/category/<slug>` | `news/list.gohtml` | the posts in one category |
| `/news/<slug>` | `news/post.gohtml` | one post |

Templates are looked up by instance key first, then by module type, so `publishing/post.gohtml` serves every Publishing instance that has no design of its own. Generate starting points with every field of the data:

```sh
task templates -- news
```

A minimal list:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/publishing.ListView */}}
<h1>{{.Title}}</h1>

{{range .Categories}}
    <a href="{{.URL}}"{{if .Active}} aria-current="page"{{end}}>{{.Title}}</a>
{{end}}

{{range .Posts}}
    <article>
        <h2><a href="{{.URL}}">{{.Title}}</a></h2>
        <time datetime="{{.Date.Format "2006-01-02"}}">{{date .Date}}</time>
        <p>{{.Excerpt}}</p>
    </article>
{{end}}
```

`.Excerpt` is what editors write under **Summary**. `.Date` is a calendar day, so `{{date .Date}}` shows the same day in every site timezone. On a category's page `.Category` is that category and `.Title` its title, also for a published category without visible posts, which `.Categories` leaves out.

`post.gohtml` gets a single `publishing.Post`; its `Body` is the rendered Markdown, with media from the library already resolved. `ImageURL` is the post's **Image** as pages show it, at most 1600 pixels wide, with `ImageSrcset`, `ImageWidth` and `ImageHeight` for the `img` tag, and `ImageOriginalURL` is the uploaded file; all are empty for a post without one:

```gotemplate
{{if .ImageURL}}<img src="{{.ImageURL}}" srcset="{{.ImageSrcset}}" sizes="100vw" width="{{.ImageWidth}}" height="{{.ImageHeight}}" alt="">{{end}}
```

Posts in a list have the same fields; `{{image .ImageURL "400x300 crop"}}` gives a copy cut to a fixed size around the focal point (see [Media and images](../../docs/media.md#image-sizes-in-templates)).

A post appears in each language it has a title and slug for. Posts are only reachable while the instance is placed on a page.

## Show posts anywhere

The `posts` template function returns the newest published posts in the current language, up to a limit, for a home page or a sidebar; a limit of 0 or less returns all. Links point to the page the instance is placed on; while it is on no page in the current language, `.URL` of posts and their categories is empty.

```gotemplate
{{range posts "news" 3}}
    <a href="{{.URL}}">{{.Title}}</a>
{{end}}

<a href="{{moduleURL "news"}}">All news</a>
```

## Feeds

Each instance serves Atom and RSS feeds of its newest published posts below its page, in the page's language:

| Address | Routes | Lists |
| --- | --- | --- |
| `/news/feed.atom`, `/news/feed.rss` | `feed.atom`, `feed.rss` | the newest posts |
| `/news/category/<slug>/feed.atom`, `.rss` | `category-feed.atom`, `category-feed.rss` | the newest posts in one category |

Core's head tags link to them with `{{feedLinks}}`: every page of the instance to the instance's feeds, and a category's page to that category's feeds as well; see [Themes](../../docs/themes.md#feeds). A category that does not exist or is not published gets the site's 404 page, and so do the feeds while the instance is on no page.

- The feeds list as many posts as the site setting **Items in feeds** says (**Site settings › General**, 20 by default). Their title is the page's title, or the category's, with the site's name, such as "News from My site", translated with the site's translations.
- `publishing.Module("news", "News", publishing.WithFeedSize(50))` makes the instance's feeds, those of its categories included, list 50 posts instead, whatever the setting says. The size is at most 100 (`site.MaxFeedSize`) and 0 follows the setting; the app does not start with another size.
- An entry's summary is the post's excerpt, or else about the first 300 characters of its text. Its content is the post's Markdown with absolute addresses, and its images are shown as in e-mail: a JPEG at most 1200 pixels wide, or a PNG for a PNG with transparency.
- Its categories are the post's categories, and the featured image is an enclosure.
- It is published at the post's date and updated when its published revision was saved, if that was later.
- Its ID is `tag:<host>,<date created>:<key>/<post ID>`, such as `tag:example.com,2026-10-07:news/0199a3c1-…`, so it stays the same when the post's slug or page changes. The host is that of `Config.BaseURL`, or else of the request: set `BaseURL`, so the IDs do not depend on the address a reader used.

Feeds are cached like pages and are not in `sitemap.xml`. [Feeds](../../docs/module-feeds-and-links.md#feeds) for modules explains what core adds to them.

## Structured data

A post's page describes the post to search engines as a schema.org `Article`. Core's head tags write it, with the site's own items, with `{{structuredData}}`; see [Search and SEO](../../docs/search-and-seo.md#structured-data). The Article of a post at `/en/news/hello-harbour`, one item of the page's `@graph`:

```json
{
  "@type": "Article",
  "articleSection": ["Updates"],
  "author": {"@id": "https://example.com/#organization"},
  "dateModified": "2026-10-07T09:57:13Z",
  "datePublished": "2026-10-01",
  "description": "Boats arrived",
  "headline": "Hello harbour",
  "image": "https://example.com/media/v/vFm2YaBvKDUol6Ti/01a115cb-afb9-7d37-a62d-7512dda0e551_1200x630c.jpg",
  "inLanguage": "en",
  "mainEntityOfPage": {"@id": "https://example.com/en/news/hello-harbour"},
  "publisher": {"@id": "https://example.com/#organization"},
  "url": "https://example.com/en/news/hello-harbour"
}
```

- `headline` is the post's title and `description` its excerpt.
- `image` is the featured image as it is shown when the page is shared, a 1200 × 630 JPEG cut around its focal point.
- `datePublished` is the post's date, `dateModified` when the revision shown was saved.
- `inLanguage` is the page's language, and `articleSection` the titles of the post's categories.
- `author` and `publisher` are the site's organisation, as posts name no author.

Empty values, such as the description of a post without an excerpt, are left out. The list of posts and the list of a category have no Article: they are an `ItemList` of the posts they show, in their order.

## Content API

While the site's [content API](../../docs/api.md) is on (`Config.API`), each instance serves its published posts and categories as JSON below `/api/v1/<key>/`, in the language of `?locale=`, or the site's default language without it. With the instance `news`:

| Endpoint | Route name | Answers with |
| --- | --- | --- |
| `/api/v1/news/posts` | `api.news.posts` | The published posts, newest first, as a [list](../../docs/api.md#lists) |
| `/api/v1/news/posts?category=<slug>` | | The posts in one category; a category that does not exist or is not published answers 404 `not_found` |
| `/api/v1/news/posts?slug=<slug>` | | The post with that slug, such as `hello-harbour` for the `module.path` `/hello-harbour` that `/api/v1/pages/resolve` gives for a post's address |
| `/api/v1/news/posts/{id}` | `api.news.post` | One post, with its text |
| `/api/v1/news/categories` | `api.news.categories` | The published categories, in the editors' order, as a list |

A post appears in each language it has a title and slug for, as on the site, and never as a draft. `GET /api/v1/news/posts/{id}`, with the long `srcset` values shortened:

```json
{
  "id": "01a115cb-b0d3-7e8f-aab0-1307cf737b4c",
  "slug": "hello-harbour",
  "title": "Hello harbour",
  "excerpt": "Boats arrived",
  "body_html": "\u003cp\u003eThe \u003cem\u003eboats\u003c/em\u003e are in. \u003cimg src=\"https://example.com/media/v/wlN_-0Ps0_NzcXkN/01a115cb-afb9-7d37-a62d-7512dda0e551_1600x0.webp\" alt=\"Boats in the harbour\" srcset=\"…\" sizes=\"100vw\" width=\"1600\" height=\"900\"\u003e\u003c/p\u003e\n",
  "date": "2026-10-01T00:00:00Z",
  "updated_at": "2026-10-07T09:57:13.0598Z",
  "url": "https://example.com/en/news/hello-harbour",
  "api_url": "https://example.com/api/v1/news/posts/01a115cb-b0d3-7e8f-aab0-1307cf737b4c?locale=en",
  "image": {
    "url": "https://example.com/media/v/wlN_-0Ps0_NzcXkN/01a115cb-afb9-7d37-a62d-7512dda0e551_1600x0.webp",
    "srcset": "https://example.com/media/v/hJrpW0UZS6JpKfmT/01a115cb-afb9-7d37-a62d-7512dda0e551_480x0.webp 480w, …",
    "width": 1600,
    "height": 900,
    "alt": "Boats in the harbour",
    "original_url": "https://example.com/media/01a115cb-afb9-7d37-a62d-7512dda0e551.png"
  },
  "categories": [
    {
      "id": "01a115cb-b0b3-7a87-a76d-08aca70ac28e",
      "slug": "updates",
      "title": "Updates",
      "url": "https://example.com/en/news/category/updates",
      "posts_url": "https://example.com/api/v1/news/posts?category=updates\u0026locale=en"
    }
  ]
}
```

| Field | |
| --- | --- |
| `body_html` | The post's text as HTML, with absolute addresses and images with their `srcset`; only `/posts/{id}` gives it |
| `date` | The post's date, at midnight UTC |
| `updated_at` | When the published revision was saved |
| `url` | The post's page on the site; `null` while the instance is on no page in that language |
| `api_url` | The post in the API |
| `image` | The featured image as an [image object](../../docs/api.md#responses), `null` without one |
| `categories` | The published categories the post is in |

A category has its `id`, `slug` and `title`, its list on the site as `url`, `null` while the instance is on no page, and its posts in the API as `posts_url`. `/categories` lists every published category with a title and slug in that language, also one without posts.

`/posts/{id}` sends a `Last-Modified`: the latest change of the post, of its categories, of the page the instance is on and of the post's media. A post in a category that was deleted gets none, and so do the lists; see [Caching](../../docs/api.md#caching).

## Editing

Each instance has its own section in the admin (`/admin/news`; `/admin` is the default [admin path](../../docs/configuration.md#admin-path)) with the list of posts and a link to its categories.

- A post has a date, an optional image from the media library, categories, and per language a title, web address, summary and Markdown text. An empty **Web address** is made from the title when the post is saved.
- **Save draft** creates a new revision as a draft: "Saved as a draft. Publish it to show it on the website." The published version stays online until a revision is published. Editors can preview a draft at the post's own address on the website, while the instance is placed on a page, and restore an earlier revision from the history.
- The publication box plans publishing or unpublishing for a date and time in the site's timezone. Publishing by hand cancels the plan.
- A site can require a review per instance in **Site settings › Review**. A post's revision then needs a reviewer's approval before it is published or planned; see [Review before publishing](../../docs/editing.md#review-before-publishing).
- **Move to trash** moves a post or a category to the [trash](../../docs/editing.md#the-trash), where it can be restored or deleted for good. Moving a post to the trash cancels its planned publication. A published post cannot be restored while another published post uses its web address, nor a category while another category does.
- Categories are translated too, with a title and a web address per language, made from the title when left empty, and ordered by hand. They have no revisions: **Save** changes a category at once. The list only shows categories that have a visible post. Unpublished categories have no page, so they are not shown on posts or in the list.

Editors' rights are set per instance in the groups' permission matrix; categories share the rights of their instance.

Published posts appear in the site search and in `sitemap.xml`; `sitemap.xml` also lists the pages of the categories that have a published post in that language.

Links to addresses on the site in the text of published posts count for [Links to a page](../../docs/editing.md#links-to-a-page): the editor of a page lists the posts that link to it, and **Pages › Broken links** lists their links that lead nowhere.

The editor of a published post lists the published content that links to the post and asks before it is unpublished or moved to the trash. A revision with a new **Web address** shows the old and the new address before it is published. With the [redirects module](../redirects/README.md), editors who may create redirects also get **Unpublish and redirect**, and **Publish this revision** comes with **Redirect the old addresses to the new ones**, ticked by default; see [Links to an item](../../docs/editing.md#links-to-an-item). The editor of a published category does the same for the category's page, such as `/news/category/tips`: categories have no revisions, so saving one with a new **Web address** redirects its old address to the new one, for editors who may create redirects.

## From Go

`publishing.Of(app, "news")` returns the instance once the app has started:

```go
news := publishing.Of(app, "news")

latest, err := news.Latest(ctx, "en", 5)
```

While the instance is on no page in that language, `Latest` still returns the posts, with an empty `URL` for them and their categories.

`Store()` and `Categories()` give write access for imports and seed data, through the same rules the admin uses. Other modules propose posts with `Draft`, which saves an unpublished post for editors to review; [Submissions](../submissions/README.md) uses it to turn visitors' news tips into drafts:

```go
id, err := news.Draft(ctx, publishing.DraftInput{
    Locale: "en",
    Title:  "Street party on Saturday",
    Body:   "Everyone is welcome …",
    Note:   "Sent in by a visitor",
})
```

## Related modules

- [Submissions](../submissions/README.md) lets visitors send in news that editors accept as a draft post.
- [Comments](../comments/README.md) adds reactions below posts.
- [Stats](../stats/README.md) lists the most read posts.
