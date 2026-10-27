# Writing a module

A module adds content or a feature to a site through the same public API the bundled modules use. This guide builds a small links module step by step and points to the bundled modules for more.

## What a module is

A module is a function that returns a `webthings.Module`, which the site passes to `app.Use`:

| Field | |
| --- | --- |
| `Type` | The module's name. Its instances share the type's tables, and templates fall back from `<key>/` to `<type>/`. |
| `Key`, `Title` | The instance, as the site names it; see [Using modules](modules.md). |
| `Schema` | The schema files as an `fs.FS`, or nil. |
| `Setup` | `func(webthings.Env) (webthings.Parts, error)`, required. It runs once per instance when the app starts. |
| `Templates` | The screens the public handler renders. The site writes them. |
| `Funcs` | Template functions for every template. |
| `Components` | Parts of a page with default markup, rendered by the module's template functions. |
| `Mail` | E-mail templates with a default text; see [E-mail](mail.md). |
| `Forms` | The inputs of the module's visitor forms, structs with go-form's tags, such as `SuggestionInput{}`. Themes render them with `form_open` and `form_field`, translated; see [Visitor actions](module-visitor-actions.md#visitor-actions). |

Templates, components and mail are `frontend.Template` values: a `Name` (the file name without `.gohtml`), `Data` (a value of the data type) and an optional `Example`, which is the default of a component or mail and the starting point `App.WriteTemplates` writes for a screen.

`Setup` gets a `webthings.Env` with the shared services and returns `webthings.Parts`, what the instance adds to the site. Both are listed at the end of this guide.

## The example: a links module

The links module keeps a list of links to other websites, such as partners. Editors write them in the admin, a page can show them, and any template can list them. It is a package in the site's own repository, with the module path `example.com/mysite`, and the site adds it like any other module:

```go
err = app.Use(links.Module("partners", "Partners"))
```

## Schema files

Schema files sit in one folder, named `<YYYYMMDDHHMMSS>[_description].<flavour>.sql`, such as `schema/20261005120000_init.sqlite.sql`. The datetime is the version, and every version needs a file for each flavour: `sqlite`, `postgres` and `mysql`. At start the app runs the current database's files that are newer than the version it recorded for the module type, each in a transaction. Statements end with `;` at the end of a line; lines starting with `--` are left out.

```text
CREATE TABLE site_link (
    id         TEXT PRIMARY KEY,
    instance   TEXT NOT NULL,
    published  BOOLEAN NOT NULL DEFAULT FALSE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    created_by TEXT,
    updated_at TIMESTAMP NOT NULL,
    updated_by TEXT,
    deleted_at TIMESTAMP,
    deleted_by TEXT,
    url        TEXT NOT NULL DEFAULT ''
);

CREATE INDEX site_link_instance ON site_link (instance, position);

CREATE TABLE site_link_translation (
    item_id     TEXT NOT NULL REFERENCES site_link (id) ON DELETE CASCADE,
    locale      TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
```

The `created_*`, `updated_*` and `deleted_*` columns are the audit columns that the content engine fills; see [Deleting and the trash](module-admin.md#deleting-and-the-trash). The PostgreSQL and MySQL files differ in types, as in [`../modules/testimonials/schema`](../modules/testimonials/schema): IDs, also in the `*_by` columns, are `UUID` and `VARCHAR(36)`, times `TIMESTAMPTZ` and `DATETIME(6)`, and MySQL declares indexes and foreign keys inside `CREATE TABLE ... ENGINE=InnoDB`. Core and the bundled modules use tables starting with `wt_`, so give yours another prefix. Before the site's first release, change the `_init` files and recreate the development database; after a release, add newer files instead. A file whose statements change after a database applied it stops the start with `db.ErrSchemaChanged`; see [Databases](databases.md#schema-files).

The module embeds the folder with `//go:embed schema/*.sql` and passes it as `Schema`; below, `schema()` returns `fs.Sub(schemaFiles, "schema")`, as [Databases](databases.md) shows.

## Content

Package `content` stores multilingual content for modules. A module describes it with two structs, whose `db` tags name the columns, and a `content.Spec`:

```go
// Fields are the parts of a link that every language shares.
type Fields struct {
    URL string `db:"url"`
}

// Text is a link in one language.
type Text struct {
    Title       string `db:"title"`
    Description string `db:"description"`
}

type Item = content.Item[Fields, Text]

var errURL = errors.New("links_invalid_url")

func spec() content.Spec[Fields, Text] {
    return content.Spec[Fields, Text]{
        Table: "site_link",
        Complete: func(text Text) bool {
            return text.Title != ""
        },
        Check: func(fields Fields, translations []content.Translation[Text]) error {
            parsed, err := url.Parse(fields.URL)
            if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
                return errURL
            }

            if content.TooLong(2000, fields.URL) {
                return content.ErrTooLong
            }

            for _, translation := range translations {
                if content.TooLong(200, translation.Text.Title) || content.TooLong(1000, translation.Text.Description) {
                    return content.ErrTooLong
                }
            }

            return nil
        },
    }
}
```

`Complete` decides whether a translation can be shown; publishing needs a complete translation in the site's default language. `Check` validates every save, also from imports and seeds, which skip the form's `maxLength`. `content.TooLong(limit, values...)` counts characters, not bytes, as the form does; for `content.ErrTooLong` the editor says "The text is too long." on its own. `SlugColumn` names the slug of content with pages of its own, and `Media` lists the media it uses, which the library then refuses to delete, also while the item is in the trash. `Deletable` can refuse to delete an item; see [Deleting and the trash](module-admin.md#deleting-and-the-trash). `Changed` is called after every committed write that may change what visitors see: publishing, unpublishing, moving an item to the trash and back and, for a `Collection`, saving an item and moving it in the order. Saving or restoring a draft of a `Revisions` and purging an item from the trash do not call it. `Setup` sets it to `env.Changed`, below, so the output cache shows such a write at once, also one made from Go; see [The output cache](module-caching-and-testing.md#the-output-cache). A `db:"photo_id,null"` tag stores an empty string as `NULL`, for optional references.

| | `content.Collection` | `content.Revisions` |
| --- | --- | --- |
| Keeps | one row per item, with a published flag and an order | immutable revisions: drafts, preview, history, restore, scheduled publishing and an optional review |
| Tables for `Table: "x"` | `x`, `x_translation` (as above) | `x`, `x_revision`, `x_translation` (see [`../content/revisions.go`](../content/revisions.go)) |
| Used by | most bundled modules | Pages, Publishing and Events |

Both offer `ListPublished`, `Published` and `PublishedBySlug` for the website, and `In(key)` returns the `content.Store` of one instance, for the admin, imports and seeds. `Revisions.Outline(ctx, instance, texts...)` lists the newest revision of every item with only the named texts, such as `"slug"` and `"title"`, for a tree or a menu that does not need long texts such as bodies.

## The service

The service is the instance's API, for the public handler, the template functions and the site's own code. Like the bundled modules it can also offer `Store()`, returning `s.items.In(s.key)`, for imports and seeds.

```go
// Service is one instance: the links of one list.
type Service struct {
    key      string
    items    *content.Collection[Fields, Text]
    frontend *frontend.Frontend
}

// Link is a published link as templates see it.
type Link struct {
    ID          string
    Title       string
    Description string
    URL         string
}

// ListView is the data of links/list.gohtml.
type ListView struct {
    Title string
    Links []Link
}

// Of returns the instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
    service, _ := app.Service(key).(*Service)

    return service
}

// Links returns the published links with a title in locale, in their order.
func (s *Service) Links(ctx context.Context, locale string) ([]Link, error) {
    items, err := s.items.ListPublished(ctx, s.key, locale)
    if err != nil {
        return nil, err
    }

    links := make([]Link, 0, len(items))

    for _, item := range items {
        text, _ := item.Text(locale)
        links = append(links, Link{ID: item.ID, Title: text.Title, Description: text.Description, URL: item.Fields.URL})
    }

    return links, nil
}
```

## Module and Setup

```go
//go:embed translations
var files embed.FS

// Module adds a list of links. key names the instance; title is what
// editors see.
func Module(key, title string) webthings.Module {
    return webthings.Module{
        Type:      "links",
        Key:       key,
        Title:     title,
        Schema:    schema(),
        Setup:     setup,
        Funcs:     funcs(),
        Templates: []frontend.Template{{Name: "list", Data: ListView{}}},
    }
}

func setup(env webthings.Env) (webthings.Parts, error) {
    itemsSpec := spec()
    itemsSpec.Changed = env.Changed

    items, err := content.NewCollection(env.DB, itemsSpec)
    if err != nil {
        return webthings.Parts{}, err
    }

    service := &Service{key: env.Key, items: items, frontend: env.Frontend}

    return webthings.Parts{
        Admin:   []admin.Section{section(env, service)},
        Public:  service.handler(),
        Feeds:   service.feeds(),
        Service: service,
    }, nil
}
```

`section` is built in [The admin of a module](module-admin.md#the-admin-section) and `service.feeds()` in [Feeds](module-feeds-and-links.md#feeds).

## The public handler

`Parts.Public` serves the instance at and below the page it is placed on:

```go
func (s *Service) handler() http.Handler {
    routes := s.frontend.Router()

    routes.Get("/{$}", s.listPage)

    return routes
}

func (s *Service) listPage(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    mount := frontend.MountFrom(ctx)

    links, err := s.Links(ctx, mount.Locale)
    if err != nil {
        s.frontend.Fail(w, r, err)

        return
    }

    s.frontend.Render(w, r, frontend.Content{
        Template: "list",
        Title:    mount.Title,
        Data:     ListView{Title: mount.Title, Links: links},
    })
}
```

`frontend.MountFrom` tells where the request is: its language, the page's path and its published title. `Render` shows the screen in the site's layout, from `partners/list.gohtml` or `links/list.gohtml`. Without either the screen answers with an error, and the app logs `missing template` for each theme without it when it starts, with the command that writes it, `task templates -- partners` in a site made from the starter; that command calls `App.WriteTemplates`, which writes a starting point with every field of `ListView`. `frontend.Content` also takes a `Description`, an `Image` shown when the page is shared, `Alternates` (the content's path in other languages) and an `EditURL` for the edit link editors see, from `env.AdminURL`; without it the link goes to the section's `index`. `Fail` answers an error: content that does not exist (`content.ErrNotFound`) gets the 404 page, and any other error is logged with the module and the path and answered with status 500 by the theme's `500.gohtml`, rendered as the theme's pages are, with its partials, its asset versions, `@model View` and the page's language, and with the status text, "Internal Server Error", translated with the site's translations as `.Title`. Without a `500.gohtml`, go-partial's plain error page answers, with that heading and a message that tells nothing of the error, both translated, and the page's language as its `lang`; see [Templates](templates.md#500gohtml). An HTMX request gets the same page. With `Config.Development` the page shows the error itself; otherwise no error page names the error or the template that failed, also when `500.gohtml` fails itself.

A view that shows an image of the media library, such as a photo, gets it from `s.frontend.Image(ctx, id)`, a `media.Image` as pages show it: `URL` is a WebP copy at most `media.ImageWidth` (1600) pixels wide and never larger than the upload, `Srcset` lists copies up to that width for a `srcset` attribute, `Width` and `Height` are the size of `URL`, and `OriginalURL` is the uploaded file, for downloads. The view gives each as a field of its own, `<X>URL`, `<X>Srcset`, `<X>Width`, `<X>Height` and `<X>OriginalURL`, as the testimonials' `PhotoURL` and the others in [`../modules/testimonials/testimonials.go`](../modules/testimonials/testimonials.go); the original is never the default. A GIF and an address outside the library keep their own URL, and an empty ID gives an empty `Image`. `MediaURL` is the address of the uploaded file itself, for a download such as a PDF; see [Media and images](media.md).

A view of something that takes place at a time, such as an event, gives that time with a method `When() frontend.When`, as `events.Event` and `registrations.Subject` do: `Start` and `End` in the timezone they are shown in, `End` zero without an end, and `AllDay`. `SameDay` reports whether it ends on the day it starts. One partial of the theme, with `{{/* @dot github.com/donseba/go-webthings/frontend.When */}}`, then shows the time of any of them.

`Router` returns a [go-router](https://github.com/donseba/go-router) of the instance's own. The site serves it at the page's address for each request, so its routes are relative to the page, whatever the page's address: `/{$}` is the page itself and `/{slug}` an item below it. Handlers see the whole path, such as `/en/partners/go`, and `r.PathValue("slug")` reads a parameter. Every request without a route, any path or method, gets the site's 404 page, which `NotFound` renders; a route that answers 404 itself keeps its own answer. A module therefore registers no `/` route. Middleware comes before the routes: `routes.Use` wraps the routes and the 404 page, so a check of posts also sees posts to a path without a route, and `routes.UseAll` wraps every answer before a route is matched, for example headers that keep the answers out of caches; see [`../modules/forms/public.go`](../modules/forms/public.go). Route names are the module's own. In a handler, `routes.For(r)` builds their addresses at the page the request is on: with `routes.Get("/{slug}", s.linkPage).As("link")`, `routes.For(r).RoutePathArgs("link", "slug", "go")` returns `/en/partners/go`, and a route `/{$}` named `list` is the page itself, `/en/partners`, without a trailing slash. Any other `http.Handler` as `Parts.Public` sees the whole path too and finds its page with `frontend.MountFrom`, whose `Locale` and `Path`, such as `en` and `/partners`, make the page's address, `/en/partners`. Such a handler builds the links below its page itself, where a `Frontend.Router` builds them from route names.

Content with pages of its own sets `SlugColumn`, registers a named route such as `routes.Get("/{slug}", s.linkPage).As("link")`, finds the item with `PublishedBySlug` and `r.PathValue("slug")`, links to it with `routes.For(r)` and lists its paths in `Parts.Sitemap`. Its form labels the slug **Web address**, as the bundled modules do, and `editor.Slug(in.Slug, in.Title)` in `Apply` makes an empty one from the title, as for posts and events; a title without letters `content.Slug` can use gives `""`. The router gives those without a request, below the page: `routes.RoutePathArgs("link", "slug", "go")` is `/go`. A service method that lists the items for another page or the search has no request either. Had the service kept the router, a go-router `*router.Router`, as `s.public` and `env.Pages` as `s.pages`, `s.public.At(base)` builds the addresses that `For(r)` gives, below `base`, the instance's page in a language from `MountURL`. While the instance is on no page, `MountURL` is `""` and `pages.NoLinks` stands in: it returns `""` for every route, so the items are still listed, without links. `pages.Links(routes, base)` chooses between the two. It returns go-router's `router.PathFunc`, to which `NoLinks` and any `RoutePathArgs` method value can be assigned, also those of `For(r)` and `At(base)`:

```go
// paths builds the addresses of the instance's routes in locale outside a
// request: below its page, or none while it is on no page.
func (s *Service) paths(ctx context.Context, locale string) (router.PathFunc, error) {
    base, err := s.pages.MountURL(ctx, s.key, locale)
    if err != nil {
        return nil, err
    }

    return pages.Links(s.public, base), nil
}
```

`paths("link", "slug", "go")` is then `/en/partners/go`. See [`../modules/gallery/module.go`](../modules/gallery/module.go) and the `Albums` method in [`../modules/gallery/gallery.go`](../modules/gallery/gallery.go).

## Template functions

A template function whose first parameter is a `context.Context` gets the context of the request. `frontend.ServiceOf[*Service](ctx, key)` returns an instance's service while a page renders, and `frontend.Locale` the page's language:

```go
func funcs() template.FuncMap {
    return template.FuncMap{
        "links": list,
    }
}

// list returns the published links of an instance in the page's language,
// for example {{range links "partners"}} in a footer.
func list(ctx context.Context, key string) []Link {
    service, ok := frontend.ServiceOf[*Service](ctx, key)
    if !ok {
        return nil
    }

    links, err := service.Links(ctx, frontend.Locale(ctx))
    if err != nil {
        slog.ErrorContext(ctx, "template links", "module", key, "error", err)
    }

    return links
}
```

Use `ServiceOf` rather than `frontend.Service(ctx, key).(*Service)`: a key the site has no instance of this module for is a mistake in a template that would show nothing, so `ServiceOf` logs it once at WARN as `template mistake`, with the function's name in the template and the keys there are, such as `links: no links instance "partner" (known: partners)`, and with `Config.Development` the page fails with that message. Outside a render it reports false.

Instances of one type share the functions, so the key is their first argument. When two module types offer the same name, the app does not start; the same goes for a name go-partial or core's renderer uses, such as `url`, `locale`, `slot`, `requiredSlot`, `deferredSlot`, `targetHeader`, `targetValue` or `targetIs`, or one starting with `_` (`render.ErrReserved`; `render.Reserved(name)` checks one).

## Components

A component is a part of a page with default markup. Declare it in `Module.Components` with its default as `Example`, under a name none of the module's screens has, and render it from a template function with `frontend.Component`:

```go
const boxExample = `<aside class="links">
    <h2>{{tl localizer "Links"}}</h2>
    <ul>{{range .}}<li><a href="{{.URL}}" rel="external">{{.Title}}</a></li>{{end}}</ul>
</aside>
`

// box renders the box component, for example {{linkBox "partners"}}.
func box(ctx context.Context, key string) (template.HTML, error) {
    links := list(ctx, key)
    if len(links) == 0 {
        return "", nil
    }

    return frontend.Component(ctx, key, "box", links)
}
```

With `Components: []frontend.Template{{Name: "box", Data: []Link{}, Example: boxExample}}` and `"linkBox": box` in `funcs`, a theme can replace the markup with `partners/box.gohtml` or `links/box.gohtml`. Write default texts with `tl localizer`, so the site's translations apply.

Write a module's default templates and components without `style` attributes, `<style>` elements, inline scripts and event handler attributes, as the bundled modules do: the site's Content-Security-Policy blocks them under **Enforce**. Give the markup classes that `webthings.css` or the theme styles, and show a share as a `<meter>`, as the polls module does. See [A Content-Security-Policy of the site's own](deployment.md#a-content-security-policy-of-the-sites-own).

## Topic guides

These guides continue the links module:

- [The admin of a module](module-admin.md) builds the admin section that `Setup` returns, and covers screens of a module's own, deleting and the trash, and previews and review.
- [Visitor actions and forms of a module](module-visitor-actions.md) adds a form for link suggestions and covers how a module receives, checks and keeps visitors' posts.
- [Feeds, structured data, API and links of a module](module-feeds-and-links.md) adds the links module's feed, its endpoints in the content API and the links in its content to the site's pages, and covers structured data for search engines.
- [Caching and testing a module](module-caching-and-testing.md) explains how the output cache keeps a module's pages and learns of its changes, and how to test a module.

## Parts

| Field | Adds |
| --- | --- |
| `Admin` | Admin sections, usually one at `/<key>` below the admin, `/admin/<key>` by default. |
| `Public` | The handler at and below the instance's page, usually a `Frontend.Router`. Nil means it cannot be placed on a page. |
| `Service` | The API that `App.Service`, `Env.Service` and `frontend.Service` return. |
| `Sitemap` | The paths below the page per language, for `sitemap.xml`. |
| `Feeds` | Atom and RSS feeds below the instance's page, as routes of `Public`, which is then a `Frontend.Router`; see [Feeds](module-feeds-and-links.md#feeds). |
| `ActionBodyLimit` | The largest request body the routes of `Env.Actions` accept in bytes; zero is 1 MiB (`webthings.DefaultActionBodyLimit`). |
| `Middleware` | A wrapper around the public site, for example to redirect old addresses. It runs before the output cache and compression, so it sees every request for the site's pages, also those the cache answers, and a response it reads may be gzip-compressed (`Content-Encoding`). Editors are recognised before it, so `frontend.Editor(r.Context())` tells them from visitors. `App.CorePaths()` lists the addresses core serves itself, such as the admin's (`Config.AdminPath`), `/_` and `/media`; requests below most of them never reach it. `env.IsCorePath(path)` checks one. Both come from the app's own routes, so the list is complete, and they work from `webthings.New` on. |
| `Close` | A function run when the app closes, before the database, for example to write buffered data. |
| `Search` | Results for the site search; [`../modules/lists/search.go`](../modules/lists/search.go) scores them with `frontend.ParseQuery`. See [Search and SEO](search-and-seo.md). |
| `Scheduled` | Planned publication of revisioned content, a `[]content.Scheduler`: usually `[]content.Scheduler{posts.Scheduler(env.Key)}` for a `content.Revisions`. |
| `Links` | The links in the module's published content, a `links.Func`, so the admin knows which content links to a page; see [Links to pages](module-feeds-and-links.md#links-to-pages). |
| `Redirects` | A `links.Redirector` when the module redirects old addresses: the admin then offers to redirect the addresses a page or an item leaves. The first module that sets it is used. |
| `API` | The module's endpoints in the [content API](api.md), a go-router of `api.NewRouter()`, mounted at `/api/v1/<key>/` while `Config.API` turns the API on; see [The content API](module-feeds-and-links.md#the-content-api). |
| `StructuredData` | A `frontend.StructuredDataFunc` that adds to the structured data of other modules' pages, or changes it; a module's own pages give theirs in `frontend.Content.StructuredData`. See [Structured data](module-feeds-and-links.md#structured-data). |

## Env

| Field | |
| --- | --- |
| `Key`, `Title` | The instance. |
| `DB` | The database, with `Get`, `Select`, `Exec`, `Tx` and `?` placeholders on every database. |
| `Locales`, `Site` | The site's languages and settings. |
| `Auth`, `Media`, `Pages` | Accounts, the media library and the page tree. `Pages.MountURL(ctx, key, locale)` is the address of the instance's page, such as `/en/partners`, or `""` while it is on no page; `pages.Links(routes, base)` builds the addresses of the public router's routes below it, with `routes.At(base)`, or none with `pages.NoLinks` while it is `""`; see [The public handler](#the-public-handler). |
| `Frontend` | `Render`, `NotFound`, `Fail`, `Router`, `Route`, `Image`, `MailImage`, `AltText`, `MediaURL`, `Markdown`, `AbsoluteMarkdown`, `MarkdownOptions`, `TagURI`, `ModulePreviewURL`, `SocialImageURL`, `Mail`, `SendMail`, `BackLocale`, `BindForm`, `KeepForm` and `ValidateSchema`. `Markdown` keeps what it rendered, so a handler can render a body on every request. `AbsoluteMarkdown` and `TagURI` are for feeds, and `MarkdownOptions` for Markdown rendered in your own code, such as for e-mail; see [Feeds](module-feeds-and-links.md#feeds). `SocialImageURL` is for structured data; see [Structured data](module-feeds-and-links.md#structured-data). |
| `Mail` | The site's `mail.Sender`. |
| `BaseURL`, `AbsoluteURL(r, path)` | Absolute links, for e-mail. |
| `AdminLanguage` | The admin's language, for mail to staff. |
| `Service(key)` | Another instance's service once the app has started, nil before: resolve it in handlers, not in `Setup`. |
| `Secret(ctx, name)` | A random 32-byte key of the instance, the same in every process, for example to hash visitors' addresses. |
| `VisitorID(ctx, r, parts...)` | A visitor's address hashed with the instance's secret, as 64 hexadecimal digits; the parts, such as the date, narrow it. |
| `AdminURL(name, args...)` | The address of a named admin route once the app has started, `""` before; see [The admin section](module-admin.md#the-admin-section). |
| `Actions()` | The go-router of the instance's visitor actions at `/_/<key>/`; see [Visitor actions](module-visitor-actions.md#visitor-actions). |
| `IsCorePath(path)` | Whether core serves `path` itself, such as the admin's pages; see `Middleware` in [Parts](#parts). |
| `Links()` | The site's link check, a `*links.Check`, for the editor of items with an address of their own (`editor.Linked`); see [Links to pages](module-feeds-and-links.md#links-to-pages). Nil on an `Env` made without an app. |
| `Changed()` | Tells the output cache that what visitors see of the module changed. Stores whose `content.Spec` has `Changed` set to it call it themselves. Call it after the commit, such as with `db.AfterWrite`; `App.Changed()` is the same for the site's own code. See [The output cache](module-caching-and-testing.md#the-output-cache). |
| `PersonalCookie(name)` | Names a cookie of the module that changes what a page shows to the visitor who has it, so the output cache keeps the page apart per value; call it in `Setup`. |

## Rules for modules

The bundled modules are built like a module from any other repository. [`../testsuite/architecture_test.go`](../testsuite/architecture_test.go) checks that:

- core never imports a module;
- modules use core's public packages, never `internal/`;
- modules add no dependencies beyond the standard library, core, other modules and the libraries core already uses, such as go-form; anything else belongs in [go-webthings-contrib](https://github.com/donseba/go-webthings-contrib);
- nothing imports go-webthings-contrib;
- `sitetest`, with `sitetest/catalogtest`, and `db/dbtest` are used in tests only.

A module in your own repository cannot import `internal/` packages either, and may depend on what it needs.

## Fuller examples

- [Testimonials](../modules/testimonials/README.md) and [Lists](../modules/lists/README.md): collections like the links module, with an image; Lists also joins the site search.
- [Gallery](../modules/gallery/README.md) and [People](../modules/people/README.md): items with pages of their own and a sitemap.
- [Publishing](../modules/publishing/README.md) and [Events](../modules/events/README.md): revisions, previews, scheduled publishing, feeds, structured data and endpoints in the content API.
- [Comments](../modules/comments/README.md), [Polls](../modules/polls/README.md) and [Registrations](../modules/registrations/README.md): visitor actions, components and mail; Registrations also sets `Parts.StructuredData`.
- [Submissions](../modules/submissions/README.md): records in a table of their own, with a list screen, filter tabs, a settings screen and the trash.
- [Redirects](../modules/redirects/README.md) and [Stats](../modules/stats/README.md): middleware and a `Close` function; Redirects also sets `Parts.Redirects`.
