# Introduction

go-webthings is a Go library that runs the backend of a website: the database, the admin, accounts and permissions, pages, media, languages, themes and modules. You bring the templates and choose the modules.

## What it is

Your website is a small Go program. Its `main.go` creates an app with a configuration, adds the modules it wants and starts serving. go-webthings does the rest: it creates the database tables, runs the admin at `/admin/` (or a [path of your own](configuration.md#admin-path)) and renders the public site with your templates.

```go
app := webthings.New(webthings.Config{
    Name:      "My site",
    Locales:   locales.Config{Default: "en", Enabled: []string{"en", "nl"}},
    Templates: os.DirFS("templates"),
})

err := app.Use(
    publishing.Module("news", "News"),
    events.Module("agenda", "Agenda"),
)
if err != nil {
    log.Fatal(err)
}

if err := app.ListenAndServe(ctx, ":8080"); err != nil {
    log.Fatal(err)
}
```

The result is a single binary. It runs on SQLite without any other service, or on PostgreSQL or MySQL.

## What it is not

- It is not a hosted service or an application to install. You build your own site with it, usually from the starter repository; see [Getting started](getting-started.md).
- It has no design of its own. The public site is your templates; the starter's `default` theme is plain HTML to build on, next to a finished one, `editorial`.
- It is not a headless CMS: pages are rendered on the server with Go templates, or with another engine through a [renderer](renderer.md). `Config.API` adds a read-only JSON API of the published content, for apps; see [Content API](api.md).
- It is not an HTML editor. Editors write formatted text, which is stored as Markdown; raw HTML in that text is left out, never rendered.

## What the site brings

| Part | Where | What it is |
| --- | --- | --- |
| Configuration | `main.go` | `webthings.Config`: name, database, languages, mail, storage; see [Configuration](configuration.md) |
| Modules | `main.go` | the `app.Use(...)` list, one entry per instance such as `news` or `agenda` |
| Templates | `templates/<theme>/` | the layout, page, 404 and search templates, and a folder per module |
| Assets | `templates/<theme>/assets/` | CSS, JavaScript and images, served at `/theme/` |
| Translations | `translations/` | the site's own texts per language, in `.po` files |

A site can also add its own Go code: template functions, a seed for an empty database, or a module of its own.

## What go-webthings runs

- The database and its tables, created and updated at start.
- The admin, with sign-in, password reset, accounts and groups.
- The page tree, the routes in every language, and scheduled publishing.
- The media library, with resized copies of images, made after the upload or on their first request.
- Site settings, themes, `sitemap.xml`, `robots.txt` and the site search.
- E-mail for the modules, through the sender you configure.
- Every module's admin section, public pages, Atom and RSS feeds, template functions and components.

## The main parts

- **Pages** form the page tree. Every page has a title, slug, description and Markdown text per language. A page can also carry a module instance, which then serves everything below it. See [Pages](pages.md).
- **Media** is the library of images and files that pages and modules use. See [Media and images](media.md).
- **Site settings** hold the title and tagline per language, the languages, the timezone, the logo, contact details and social media profiles. The layout template gets them; see [Templates](templates.md) and [Languages](languages.md).
- **Themes** are the folders in `templates/`. Editors pick the active one in the admin. See [Themes](themes.md).
- **Accounts and groups** decide who may do what. Administrators may do everything; editors get the rights of their groups. See [Accounts and permissions](permissions.md).
- **The admin** at `/admin/` has a section for each of these and for every module instance. See [Editing and publishing](editing.md).
- **The frontend** turns a request into a page: it finds the page, builds the data and renders your templates. See [Templates](templates.md).
- **Modules** add content and features, such as news, an agenda, comments or a newsletter. Each instance has a key and its own admin section. See [Using modules](modules.md).
- **Components** are parts of a page that modules offer to every template, such as `{{commentThread "comments" .ID}}` or `{{newsletterForm "newsletter"}}`. Core offers one as well, `{{shareBar "" .Title}}`. They come with plain default markup that a theme can replace.

## How a request is served

Every request passes one router. Around it runs what every request gets: the visitor's address, a request ID, the security headers, the metrics, the access log when `Config.AccessLog` is set, and the recovery from panics, which answers 500 and keeps the site running. The metrics and the access log leave out the health and metrics addresses; see [Deployment](deployment.md#logs).

go-webthings' own addresses are routes of that router, and `app.CorePaths()` lists them (see `routes` in [webthings.go](../webthings.go)). The admin's address is `Config.AdminPath`, `/admin` by default; the others are fixed:

```text
/admin/          the admin, at Config.AdminPath
/media/          uploaded files and resized images
/theme/          the themes' assets
/assets/         Config.Assets or Config.ImportMap, when set
/_/              visitors' form posts to modules, core's stylesheets such as webthings.css, and the CSP reports
/sitemap.xml     every published page in every language
/robots.txt      allows every crawler and points to the sitemap
/healthz         answers 200 while the process runs
/readyz          answers 200 when the site can serve, 503 otherwise
/metrics         the metrics, only with Config.MetricsToken
/api/v1/         the content API, only with Config.API
```

A plain 404, not the site's 404 page, answers an address below `/_/` that is no module's action, an address below `/assets/` without `Config.Assets` or `Config.ImportMap`, and `/metrics` without `Config.MetricsToken`.

A module's form posts go to `/_/<key>/`, its visitor actions, past the protection against posts from other sites and a limit on the size of the post; see [Visitor forms](visitor-forms.md). The forms of bundled modules such as comments and the newsletter send a visitor whose post has mistakes back to the form, which shows the values again with a message at each field to correct; see [A post with mistakes](visitor-forms.md#a-post-with-mistakes).

Once the app has started, every answer carries the security headers `X-Content-Type-Options: nosniff` and `Referrer-Policy: strict-origin-when-cross-origin`: the answers of these routes and of the public site, the router's own redirects, and also `/healthz`, `/readyz` and `/metrics`. Pages, the 404 page and the answers of visitor actions also say which sites may show them in a frame, as **Site settings › Security** decides, a card only administrators can change; admin pages never may. See [Deployment](deployment.md#security-headers).

Every other request goes to the public site, through the middleware of modules such as redirects and stats, and then the output cache, which sends visitors a page it keeps in memory without rendering it again (see [Deployment](deployment.md#the-output-cache)). Pages live in the database and can move, so the site finds the page itself:

1. `/` redirects to the default language, for example `/en/`.
2. The first path segment must be an enabled language. Anything else gets the 404 page.
3. The rest of the path is looked up in that language's published page tree. Addresses are canonical: `/en` becomes `/en/`, and a trailing slash elsewhere is removed.
4. A page without a module is rendered with `page.gohtml` inside `layout.gohtml`. It answers GET and HEAD; another method gets 405.
5. A page with a module is served by that module's own router, at the page's address. Its routes are relative to the page, so the module works wherever editors place it, and its handlers see the whole path. A path or method it has no route for gets the 404 page.
6. Nothing found renders `404.gohtml`.

Before the app has started, only `/healthz`, `/readyz` and `/metrics` answer as usual; every other address gets 503.

With the news module on the page `/news`:

```text
/en/                  the home page
/en/about/team        the page "team" below "about"
/en/news              the news module's route "/{$}": its list
/en/news/opening      the news module's route "/{slug}": a post
/en/news/feed.atom    the news module's Atom feed; feed.rss is the same in RSS
/en/search?q=clay     the search screen, unless a page has the path /search
```

Rendering uses the theme that is active in the site settings. Templates and assets that a theme lacks come from `templates/default/`.

## Built on

go-webthings uses a few libraries by the same author:

- [go-router](https://github.com/donseba/go-router) is the router every request passes, with named routes, and brings the middleware around it: the visitor's address, request IDs, security headers, compression, `ETag`s, the output cache, throttles and the recovery from panics.
- [go-partial](https://github.com/donseba/go-partial) renders templates inside a layout, and only the content for HTMX requests.
- [go-form](https://github.com/donseba/go-form) builds the admin's forms and the fields of visitors' forms from Go structs, reads and validates their posts, and brings the spam trap of visitors' forms. Posts from other websites are refused by Go's own cross-origin protection.
- [go-importmap](https://github.com/donseba/go-importmap) serves the admin's files, the themes' assets, `Config.Assets` and `Config.ImportMap` with versioned addresses. The admin's htmx is vendored with it and checked at start.
- [go-translator](https://github.com/donseba/go-translator) reads the `.po` catalogues for the admin and for your templates' `{{tl localizer "..."}}` texts.
- [go-clue](https://github.com/donseba/go-clue) gives editors completion and checks in Go templates. The `@dot` line at the top of a generated template names its data type for it, and an `@model` line names the page or its content in any template, partials included (see [Templates](templates.md#the-page-anywhere-model)).

The main other dependencies are the three database drivers, sqlx, goldmark for Markdown and gen2brain/webp for WebP images. None of them needs cgo. The admin's text editor is built on [ProseMirror](https://prosemirror.net), which comes with the admin as one JavaScript file, so it needs no build step and no CDN.

## Next steps

- [Getting started](getting-started.md) runs the starter site.
- [Configuration](configuration.md) lists every setting.
- [Pages](pages.md) and [Templates](templates.md) explain how the site is built.
- [Using modules](modules.md) shows how to add and place modules.
