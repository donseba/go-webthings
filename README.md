# Go WebThings

WebThings is the backend of a website: database, admin, accounts and
permissions, pages, media, languages, themes and content modules (news,
events, galleries, comments, newsletters, sign-ups and more). The website
itself only brings its templates and assets.

Start a site from [go-webthings-clone](https://github.com/donseba/go-webthings-clone); see
[go-webthings-demo](https://github.com/donseba/go-webthings-demo) for a complete, designed example.

```go
app := webthings.New(webthings.Config{
    Name:      "My site",
    Locales:   locales.Config{Default: "nl", Enabled: []string{"nl", "en"}},
    Templates: os.DirFS("templates"),
    Mail:      mail.SMTP{Host: "smtp.example.com", From: "Site <site@example.com>"},
})

app.Use(
    publishing.Module("news", "News"),
    publishing.Module("blog", "Blog"),
    events.Module("agenda", "Agenda"),
    comments.Module("remarks", "Comments"),
    redirects.Module(),
    stats.Module(),
)

if err := app.ListenAndServe(ctx, ":8080"); err != nil {
    log.Fatal(err)
}
```

The admin is at `/admin/`, or wherever `Config.AdminPath` puts it, such as
`/toolbox`. Editors build the page tree there and place module instances on
pages; WebThings serves everything below such a page. Code links into the
admin by route name, never by its path (`Env.AdminURL`, `App.AdminURL`).
`robots.txt` allows everything and names the sitemap, not the admin: admin
pages send `X-Robots-Tag: noindex, nofollow` instead.

Once the app has started, every response is sent with
`X-Content-Type-Options: nosniff` and
`Referrer-Policy: strict-origin-when-cross-origin`: pages, visitor actions,
media, assets, `sitemap.xml`, `robots.txt` and the admin, and also
redirects to clean paths and `/healthz`, `/readyz` and `/metrics`. Which
sites may show the pages and actions in a frame is a site setting (Site
settings → Security, `site.Settings.Framing`), which only administrators
change; other editors see it read-only. `same-origin`, the default, sends
`X-Frame-Options: SAMEORIGIN` and `Content-Security-Policy: frame-ancestors
'self'`; `none` sends `DENY` and `frame-ancestors 'none'`; `any` sends
neither. The admin always sends `X-Frame-Options: DENY` and
`frame-ancestors 'none'`. WebThings can also send a strict
`Content-Security-Policy` itself, without `'unsafe-inline'` or
`'unsafe-eval'`, for the site and the admin: off (the default), report-only
or enforce, on the same card or with `Config.ContentSecurityPolicy`, which
the card then shows, disabled. It allows the import maps and the style of
go-partial's error page by their hashes, never by nonces, so the output
cache keeps each page with its own policy, and browsers report what it
blocks to `/_/csp-report`, which the site logs. The admin has no inline
styles or scripts, and neither do the starting points and the modules'
default templates, visitor forms and poll results included. A policy that a
handler around the app set keeps its other directives in every mode: core
merges `frame-ancestors` into it, and with enforce the site's directives
replace only those the handler also names, so they cannot weaken the site's
policy. See
[A Content-Security-Policy of the site's own](docs/deployment.md#a-content-security-policy-of-the-sites-own).

## Templates

The layout gets the page context, every other template only its own data:

| Template | Data |
| --- | --- |
| `layout.gohtml` | `frontend.View`: site, menu, languages, title |
| `page.gohtml` | `frontend.Page` (`IsHome`, `Path` such as `/about/team`, `Slug`, `ParentID`) |
| `404.gohtml` | `frontend.NotFound` |
| `500.gohtml` | go-partial's `errors.Data`, shown on its own when a template or a module fails, with the `frontend.View` for `@model`; `{{.Title}}`, the heading, and `{{.Message}}` are already in the page's language, and `{{styles}}` writes the theme's versioned links with their integrity, also when a visitor action fails (optional: without it, go-partial's plain error page, with its heading, message and `lang` in the page's language, and nothing of what failed) |
| `search.gohtml` | `frontend.SearchView`, at `/<locale>/search?q=` (optional) |
| `<key>/<name>.gohtml` | the module's view, for example `news/post.gohtml` gets a `publishing.Post` |

Module templates are found by instance key first (`news/post.gohtml`), then by
module type (`publishing/post.gohtml`), so instances can share a design or
each have their own. `App.WriteTemplates(dir, key)` writes starting points with
the right `@dot` and every field; the clone runs it as `task templates -- <key>`.
Templates in `partials/` can be used everywhere.

A layout that places `{{slot "head"}}` gets core's tags in `<head>`: the
title, the description, robots for a preview, the canonical and `hreflang`
links with `x-default`, the `og:` tags and `twitter:card`, the icons,
`{{feedLinks}}`, `{{apiLink}}` and `{{structuredData}}`. A theme's
`regions/head.gohtml` adds tags after
`{{template "webthings/head-tags.gohtml" .}}`, or replaces core's tags when
it does not call it. `{{styles "webthings.css" "site.css"}}` links
go-partial's `partial-errors.css`, the files given, from the theme or else
from core, and the theme's values stylesheet last, each with its version
and integrity:

```gotemplate
<head>
    <meta charset="utf-8">
    {{slot "head"}}
    {{styles "webthings.css" "site.css"}}
</head>
```

`Start` checks every template of every theme with the site's functions and
logs each problem as `template problem`, with the file, the line and a
hint: the module a function comes from ("add it to app.Use or remove the
call") or the closest name. With `Config.Development` any problem stops the
start; otherwise only one that fails every page of the active theme. A call
that finds nothing, such as `{{posts "nieuws" 3}}`, an asset file the theme
does not have or an unknown `{{theme}}` setting, is logged once with what
there is, and fails its page in development. See
[Templates](docs/templates.md#checking-the-templates-at-start).

Any template can show any module's content:

```gotemplate
{{range posts "news" 3}} … {{end}}
{{range events "agenda" 5}} … {{end}}
{{with album "work" "carousel"}}{{range .Images}} … {{end}}{{end}}
{{range entries "faq"}} … {{end}}
{{range testimonials "reviews" 3}} … {{end}}
<a href="{{moduleURL "news"}}">All news</a>
```

The site's social media profiles (Site settings → Social media profiles) are
`{{range .Site.Profiles}}` in the layout and `{{range profiles}}` anywhere;
`share` makes share links for a page (empty for the current one, or a path
or URL), labelled through the site's translations:

```gotemplate
{{range profiles}}<a href="{{.URL}}" rel="me" class="icon-{{.Network}}">{{.Label}}</a>{{end}}
{{range share "" .Title}}<a href="{{.URL}}" class="share-{{.Network}}">{{.Label}}</a>{{end}}
{{shareBar "" .Title}}
```

Template functions whose first parameter is a `context.Context` get the
current request; that is how modules know the language. Sites can add their own
the same way through `Config.Funcs`. Of go-partial's interaction helpers,
templates get the ones that write attributes (`submitAttrs`, `liveAttrs`,
`clientAttrs`, `noBoost`, …); its generic names, such as `poll`, are left to
modules. Another template engine can be used by implementing
`frontend.Renderer`.

## Components

The interactive modules come with components: parts of a page with plain,
accessible default markup that any template includes with one function.

```gotemplate
{{commentThread "remarks" .ID}}       comments with their replies, then the form
{{commentForm "remarks" .ID}}         only the form
{{pollBox "survey" "colour"}}         a poll by slug or ID ("" for the newest): vote form and results
{{newsletterForm "letters"}}          a signup box
{{registrationForm "signup" .ID}}     places, prices and the sign-up form of an event
{{shareBar "" .Title}}                share links (core)
```

A theme replaces a component like a module screen, with
`<key>/<name>.gohtml` or `<type>/<name>.gohtml` (`remarks/thread.gohtml`,
`polls/box.gohtml`, `newsletter/form.gohtml`, `registrations/form.gohtml`;
core's are in `site/`, such as `site/share.gohtml`), and gets the module's
default otherwise. Each name is looked for in the active theme before
`default/`, so a theme's `polls/box.gohtml` wins over a `survey/box.gohtml`
in `default/`. `App.WriteTemplates(dir, key)` writes the defaults to copy
and change. Modules declare components in `Module.Components` and render
them with `frontend.Component`.

The defaults use stable BEM-style classes (`wt-comments`, `wt-comment__body`,
`wt-poll__result`, `wt-newsletter`, `wt-registration__facts`,
`wt-share__link`, shared `wt-form`, `wt-form__fieldset`, `wt-form__options`,
`wt-notice`, `wt-button`) and `tl` for their texts, so the site's
translations apply.
One optional stylesheet lays them out (spacing, forms, the `<meter>` bars of
poll results) with custom properties a theme sets:

```gotemplate
{{styles "webthings.css" "site.css"}}
```

```css
/* assets/site.css */
:root { --wt-accent: #b5562f; --wt-radius: 0; --wt-gap: 1.25rem; }
```

Core serves `webthings.css` at `/_/webthings.css`; a theme with its own
`assets/webthings.css` serves that instead. Its rules have no specificity
(`:where`), so any rule of the theme wins. `styles` writes the
stylesheet links with the files' Subresource Integrity; see
[Themes](#themes).

## Images

Module views give each image as pages show it, never the uploaded original:
`<X>URL` is a WebP copy at most 1600 pixels wide (`media.ImageWidth`) and
never wider than the original, `<X>Srcset` lists smaller copies, and
`<X>Width` and `<X>Height` are the size of `<X>URL`. `<X>OriginalURL` is the
uploaded file, for downloads. A post's featured image is `ImageURL`,
`ImageSrcset` and so on, a person's photo `PhotoURL`. Only JPEG and PNG files
are resized; other images, such as a GIF, keep their own address and an
empty `<X>Srcset`.

```gotemplate
{{if .ImageURL}}<img src="{{.ImageURL}}" srcset="{{.ImageSrcset}}" sizes="100vw" width="{{.ImageWidth}}" height="{{.ImageHeight}}" alt="">{{end}}
```

`{{image .PhotoURL "800x600"}}` is a resized copy of an image: it fits inside
800 × 600 and is never made larger. `"800"` and `"x600"` bound one side,
`"800x600 crop"` cuts the exact size around the image's focal point, and a format can follow
(`"800 jpg"`, `"400x400 crop png"`); the default is WebP. The function takes the
addresses module views give, the original's address or an asset ID, and
returns the original for files that are not images.

```gotemplate
<img src="{{image .ImageURL "800"}}" srcset="{{srcset .ImageURL 400 800 1600}}" sizes="100vw" alt="">
```

The logo of the site settings, `.Site.LogoURL`, is at most 600 pixels wide
and never enlarged, in the format it was uploaded in rather than WebP: a PNG
stays a PNG and a JPEG a JPEG. A PNG no wider than that, and a GIF, keep
their own address. See [The site's images](docs/media.md#the-sites-images).

Copies are served at `/media/v/<signature>/<id>_<size>.<format>`, kept in
the media storage (also S3) and deleted with the image. The copies pages
show by default, `<X>URL` and the smaller ones of `<X>Srcset`, are made in
the background right after the upload, so the first visitor does not wait
for them; other sizes, and the copies of uploads that find the queue of
1024 images full, as in a large import, are made on their first request.
The URLs are signed with a key in the site settings, so only sizes the
templates ask for (at most 4000 pixels) can be made, and they may be cached
forever. Resizing uses Catmull-Rom and follows the photo's EXIF orientation;
WebP is lossy
(quality 80), encoded by [gen2brain/webp](https://github.com/gen2brain/webp):
libwebp translated to Go, so no cgo. A system libwebp is used when installed,
loaded through purego, which makes Linux binaries dynamically linked: build
with `CGO_ENABLED=0 go build -tags nodynamic` for a static binary, as Alpine
and `scratch` images need (a library cannot set build tags for the sites that
use it).

## Search

`/<locale>/search?q=` searches the published pages and module content of that
language and renders `search.gohtml` (written by `App.WriteTemplates(dir, "site")`).
`{{range search "query" 5}}` does the same in any template and `{{searchURL}}`
is the screen's address for a search form. A published page with the path
`/search` takes precedence over the screen; without `search.gohtml` the screen
answers 404. The screen allows each visitor 30 searches a minute and answers
more with 429 (`Frontend.SetSearchThrottle`), rendering `search.gohtml` with
`.Throttled` set and no results, so the template shows a message; the
`search` function is not limited.

Matching ignores case and accents in every script and requires every word;
words in a title rank above words in the text. It runs in Go over the
published content, so it behaves the same on every database, which suits
sites up to a few thousand items. Modules join through `Parts.Search`, using
`frontend.ParseQuery` to score and summarise like the core. The bundled
publishing, events, gallery, lists, people, directory and documents modules
do; a module only shows up in results when it is placed on a page.

## Feeds

Publishing serves Atom and RSS feeds of its newest posts below its page,
also per category, and Events of its upcoming and running events:
`/en/news/feed.atom` and `/en/news/feed.rss`,
`/en/news/category/<slug>/feed.atom` and `/en/agenda/feed.atom`.
`{{feedLinks}}` in the `<head>` writes a `<link rel="alternate">` for each
feed of the page shown, Atom and RSS, with absolute addresses, so browsers
and feed readers find them; pages without feeds get none. Core's head
tags have it, which a layout places in `<head>` with `{{slot "head"}}`, as
the starting points (`App.WriteTemplates(dir, "site")`), the demo and the
clone do. A feed lists at
most 20 items unless Site settings → General → Items in feeds says
otherwise, or the instance's own size, `publishing.WithFeedSize(n)` or
`events.WithFeedSize(n)`, and its title carries the site's name, "News from
My site", translated with the site's translations. `{{feedURL "atom"}}`
gives the address of the page's feed for a link visitors see. Feeds are
cached and refreshed like pages, with an ETag and a `Last-Modified` that
never goes back, also when a post is taken off the site, so a reader that
sends only `If-Modified-Since` gets 304 while a feed is unchanged. Each
process keeps these dates itself. Feeds are not in `sitemap.xml`. Pages send
no `Last-Modified`. Entry IDs are `tag:` URIs on the host of
`Config.BaseURL`, so set it in production. Any module offers feeds with
`Parts.Feeds`; package `feed` writes them as Atom 1.0 and RSS 2.0 from one
model. See [Feeds](docs/module-feeds-and-links.md#feeds).

## Structured data

`{{structuredData}}` in the `<head>` writes the page's structured data for
search engines: one `<script type="application/ld+json">` with schema.org
items for the site, its organisation, the page and its trail, and the
content a module shows, such as a post's `Article`, an event's `Event` with
the `Offer` of its registration, a `Person`, a directory entry's
`LocalBusiness` or the `ItemList` of a list page. Every address in it is
absolute. Core's head tags have it, so the
starting points, the demo and the clone write it; see
[Search and SEO](docs/search-and-seo.md#structured-data). A module
describes its content with `frontend.Content.StructuredData` and adds to
other modules' pages with `Parts.StructuredData`; see
[Structured data](docs/module-feeds-and-links.md#structured-data).

## Content API

`Config.API` turns on a read-only JSON API at `/api/v1/` for apps and other
frontends: the site with its languages and menu, the site search, the page
tree, a page by its address or ID, and the modules' content, such as
`/api/v1/news/posts`, `/api/v1/agenda/events` and the albums, lists, people,
testimonials, directory entries and documents. It shows only published
content, never drafts or visitors' data, in one language per request
(`?locale=`), with absolute addresses, Markdown as HTML, such as in
`body_html`, and lists paged with `limit` and `offset`. Answers go through
the output cache with an ETag, and a single page, post or event also with a
`Last-Modified`; each client may make 300 requests a minute unless
`api.Config.RateLimit` says otherwise, and other sites' pages may read it
only from `AllowedOrigins` (CORS). `{{apiLink}}` in the `<head>`
links each page to its answer in the API. A module adds endpoints with
`Parts.API`. See [Content API](docs/api.md).

## Editing from the site

Editors who are signed in to the admin see a link to the admin screen of the
page or module item they look at, when they may edit it:

```gotemplate
{{with .Editor}}<a href="{{.EditURL}}">Edit</a>{{end}}
{{with adminURL "admin.dashboard"}}<a href="{{.}}">Admin</a>{{end}}
{{with adminURL "admin.news.edit" "id" .ID}}<a href="{{.}}">Edit</a>{{end}}
```

`adminURL` is the address of a named admin route for signed-in editors and
`""` for visitors, so the admin's address stays off public pages.
Module screens set the target with `frontend.Content.EditURL`; middleware can
leave editors out with `frontend.Editor(ctx)`. The session cookie (HttpOnly,
SameSite=Lax, Secure unless `Config.Insecure`) reaches the public site for
this; it is checked only when present, and pages for editors are sent with
`Cache-Control: private, no-store`. Pages for visitors get the site's caching
from Site settings → Performance: `no-cache` with an ETag by default.

## Themes

Each folder in `templates/` is a theme; editors pick one in the admin under
Theme, after a preview. A theme only holds what differs from `default/`:
templates and assets it lacks come from there, so a colour variant can be a
single stylesheet. A theme's own module templates come first, by key or by
type: its `publishing/post.gohtml` wins over `news/post.gohtml` in
`default/`. A site with `layout.gohtml` directly in `templates/` has one
theme.

```text
templates/default/layout.gohtml
templates/default/assets/site.css     {{styles "site.css"}} or {{assetStyle "site.css"}}
templates/default/assets/site.js      {{assetScript "site.js" "type" "module"}}
templates/default/theme.json          title, description, settings
templates/night/assets/variant.css
templates/night/theme.json
```

`{{asset "site.css"}}` is the address of a file in the theme's `assets/`,
`/theme/default/site.css?v=…`, versioned by its content so browsers may
keep it. `{{assetStyle "site.css"}}` and `{{assetScript "site.js" "type"
"module"}}` write the stylesheet link and the script tag at that address
with the file's Subresource Integrity, so browsers refuse a file whose
content is not what the page expects; more attributes follow as name and
value pairs. While templates reload (`Config.ReloadTemplates`) the theme's
files may change under a page, so their tags carry no integrity then; core's
`webthings.css` keeps it. A file that neither the theme nor core has is
logged once with the closest names there are.

`Config.Assets` or `Config.ImportMap` serve more files at `/assets/`, with
versioned addresses, and `{{importmap}}` writes the import map for the
layout's head. Core adds go-partial's browser clients to it, such as
`partial-interactions`, so the site needs no copy of them, and the map
carries their integrity as it does that of the site's own files, also in an
import map the site builds itself. A client file among the import map's own
files, such as a `partial-flash.js` of the site, is used instead, with its
own integrity; see [Loading htmx](docs/htmx.md#loading-htmx).

`theme.json` declares settings editors change in the admin (colour, text, a
choice or a number), read as `{{.Theme.Values.accent}}` in the layout or `{{theme "accent"}}`
anywhere:

```json
{
  "title": "Atelier",
  "settings": [
    {"name": "accent", "label": {"en": "Accent colour", "nl": "Accentkleur"}, "type": "color", "default": "#b5562f", "css": "--accent"},
    {"name": "headings", "label": "Headings", "type": "select", "options": ["serif", "sans"], "default": "serif"}
  ]
}
```

A `color`, `select` or `number` setting with `"css"` becomes a custom property of the
theme's values stylesheet, `/theme/<theme>/values.css`, here
`:root { --accent: #b5562f; }`. `{{styles}}` links it after the theme's
own stylesheets, as `{{themeStyle}}` does on its own, so a colour reaches
the page without a `style` attribute.

## Translations

`frontend.Translations(dir)` loads the site's catalogues, `<dir>/<locale>.po`,
into a go-translator `*translator.Translator`, which the site sets as
`Config.Translations`:

```go
translations, err := frontend.Translations("translations")
if err != nil {
    log.Fatal(err)
}

app := webthings.New(webthings.Config{
    Templates:    os.DirFS("templates"),
    Translations: translations,
})
```

Templates then translate their texts with go-translator's functions: `tl`,
such as `{{tl localizer "Read more"}}`, `tn` for plurals and `ctl`/`ctn`
with a context. Core translates with it its own texts, such as the 404
page's title, the heading and message of `500.gohtml` or of the plain error
page that answers without it, such as "Internal Server Error", the labels
and messages of visitor forms, and the names
of months and days and the date patterns (see
[Dates, times and money](#dates-times-and-money)). A text without a translation shows as written. A language without a catalogue of its own
uses one of the same language, never another: `nl_BE` uses `nl.po`, and
`nl` uses `nl_NL.po`. go-form's own messages, such as its validation errors,
that the site's catalogue does not translate show in go-form's catalogue of
the page's language; nothing is merged into the site's catalogues.

`App.Translations(dir, args)` is the site's `translations` command: it
writes every text the site translates to `<dir>/translations.pot`, and with
`check` lists the texts each `.po` file misses. `App.TranslationCatalog()`
gives those texts as a catalog of go-translator's `extract` package, which
`translations.Run` takes. Besides the templates' own texts, the POT file
holds those of the default templates that not every theme replaces,
referenced as `module:<type>/<name>.gohtml`,
`module:<type>/mail/<name>.gohtml` and `core:site/<name>.gohtml`, the texts
core translates in Go (`core:texts`), the names and patterns of the
calendar (`core:calendar`) and, when a template renders forms with go-form's
functions, the texts of the modules' visitor forms (`module:<type>/forms`);
see
[Languages](docs/languages.md#translating-the-templates-texts).

The admin brings its own catalogues. `<StateDir>/translations/<locale>.po`,
when present, overrides their entries, and an empty entry keeps the built-in
text; the admin writes nothing there.

## Dates, times and money

Templates show dates and amounts in the page's language:

```gotemplate
{{date .Start "full"}}             Sunday 25 October 2026 · zondag 25 oktober 2026
{{date .Date}}                     25 October 2026 (styles: short, medium, long, full)
{{date .Start "EEE d MMM"}}        Sun 25 Oct (any other style is a CLDR pattern)
{{time .Start}}                    14:00 (2:00 PM in en-US)
{{dateRange .Start .End "long"}}   25–27 October 2026
{{timeRange .Start .End}}          14:00–17:00
{{money .PriceCents "EUR"}}        €45.00 · € 45,00 · 45,00 €
```

Times stored in UTC (a creation time) are shown in the site's timezone; times
already in a zone keep it, such as an event in its own timezone, and calendar
dates (a post's or a document's date) show the same day in every timezone.
English (day first; `en-US` month first and 12-hour) and Dutch are built in.
The site's translations (`Config.Translations`) give the names and
patterns of other languages, and an entry there replaces a built-in one in
every language, English and Dutch included; each msgid has a context (msgctxt):
`January` … `December` in `month`, `Jan` … `Dec` in `month-short`,
`Sunday` … `Saturday` in `day`, `Sun` … `Sat` in `day-short`, `AM` and `PM`
in `time`, and `short`, `medium`, `long`, `full` and `time` in `date`,
translated as a pattern such as `d. MMMM y` (`frontend.CalendarTexts` lists
them; the translations command adds them to the POT file, and its check asks
for them only in languages without built-in ones, so English and Dutch
catalogues need not contain them). Money uses the
currency's decimals and the language's separators. In Go,
`frontend.FormatDate(locale, t, style)`,
`frontend.FormatMoney(locale, cents, currency)` and
`Frontend.Format(ctx, locale)` (site timezone and translations) do the same.
`frontend.When` holds a start, an end and whether it lasts all day, for one
partial that shows when something takes place: `events.Event` and
`registrations.Subject` give theirs with `.When`.

## Mail and storage

Modules send e-mail through `Config.Mail`: `mail.SMTP` is built in, other
providers implement `mail.Sender`; without one, messages are written to the
log. `mail.Message.Headers` adds header fields such as `List-Unsubscribe`
(checked against header injection). A sender that implements
`mail.Describer` says how it sends, without secrets, in the E-mail step of
the admin's setup guide, which also sends a test e-mail. Uploads are kept in `StateDir/media`
unless `Config.Storage` names another `media.Storage`, such as an S3 bucket.
When the storage gives a file no address of its own (`Storage.URL` returns
`""`, as the default one does), the site serves it at `/media`, with the
route `media.asset`. Resized copies are always served there, with the route
`media.variant`, whatever the storage, because a copy that is not made yet
is made on its first request.

### Mail templates

Modules write their e-mail from templates the site can change and
translate. A module declares them like its page templates, with the text it
sends by default as `Example`:

```go
webthings.Module{
    Mail: []frontend.Template{{Name: "confirmation", Data: Confirmation{}, Example: confirmationMail}},
}

message, err := env.Frontend.Mail(ctx, locale, env.Key, "confirmation", data)
message.To = []string{address}
err = env.Mail.Send(ctx, message)
```

A theme replaces the default with `mail/<key>/<name>.gohtml` or
`mail/<type>/<name>.gohtml`, looked for in the active theme before
`default/`, as components are; `App.WriteTemplates(dir, key)` writes the
defaults there as a starting point. A mail template defines `subject` and
`text` (not HTML-escaped) and optionally `html`; every template function
works in the mail's language, including `tl`, `date` and `money`, and
`mail/partials/*.gohtml` can be shared:

```gotemplate
{{define "subject"}}{{tl localizer "Registration confirmed"}}: {{.Title}}{{end}}
{{define "text"}}
{{tl localizer "Hello"}} {{.Name}},
{{date .Start "full"}} · {{time .Start}}
{{end}}
```

Mail for staff uses `env.AdminLanguage` (`"en"` unless
`Config.AdminLanguage` says otherwise). `Frontend.SendMail(ctx, env.Mail,
frontend.Letter{Locale, Key, Name, Data, To, ReplyTo})` writes and sends a
mail in one step, without the request's cancellation, and logs a failure.
`Frontend.Execute(ctx, name, locale, data)` renders any template of the
active theme on its own, without the layout.

## Modules working together

`env.Service(key)` returns another instance's API once the app runs (resolve
it in handlers, not in `Setup`), for example
`env.Service("agenda").(*events.Service).Events(ctx, locale)` or
`env.Service("news").(*publishing.Service).Draft(ctx, publishing.DraftInput{…})`.
`env.Secret(ctx, name)` is a random 32-byte key of the instance, the same in
every process of the site, for example to HMAC visitors' addresses.

## Visitor forms

Visitors post to a module's action router, `env.Actions()`, mounted at
`/_/<key>/`: its routes become part of the site's routes, such as
`POST /_/remarks/post`, named with the key in front (`remarks.post`, for
`Frontend.Route`), and a wrong method gets 405. The module builds the
addresses of its actions from that router by route name, such as
`env.Actions().RoutePath("post")`, also without a request, for example for
links in e-mail; `Env{}.Actions()` gives the same addresses in tests and tools.
`frontend.Back(w, r, key, result)` returns the visitor to the form's page
with `?<key>=<result>`, and `frontend.BackWith` adds more query values, such
as a code. A post without that page, such as one over the body limit, goes
back to the `Referer` when it is on this site, and to `/` otherwise.
`frontend.Limits(key, frontend.ActionLimits{Body, Posts, Window})` is
middleware for the action router (`frontend.Actions` wraps any handler) that
limits the request body and the posts per visitor, and returns a visitor over
the limit with `?<key>=throttled` (`frontend.ResultThrottled`). `Body` can
only lower the site's limit: 1 MiB, or `Parts.ActionBodyLimit`.
`frontend.Result(r, key, allowed...)` reads a result for the page, and
`frontend.BackPath(r)` and `Frontend.BackLocale(r)` give the form's page and
its language.

An action reads the post into a struct with go-form's tags with
`env.Frontend.BindForm(r, &input)`, which trims text, checks the tags and
returns the field errors, also for a value it cannot read, with messages in
the language of the page the form was on. A module lists
these structs in `Module.Forms`, so their texts are found for translation,
and gives its views a form of one, `frontend.VisitorForm(ctx, model, action,
key)`, which a theme renders with `{{form_open .Form}}…{{form_close}}`.
`form_open` adds the honeypot and the page to return to: the element whose id
is the key, so the visitor sees the result next to the form. The fields' ids
start with the key, such as `remarks-name`, so forms of several instances can
share a page. `{{form_field .Form "name" nil}}` renders a field by the key in
its `name` tag, with its label and limits, translated, in the site's go-form
theme (`Config.FormTheme`, by default the classes of `webthings.css`). See
[Template functions](docs/template-functions.md#forms) and
[Visitor forms](docs/visitor-forms.md).

An action that calls `env.Frontend.KeepForm(w, r, key, input, errs)` before
`frontend.Back` returns the visitor to a form that shows their values again,
with the messages at their fields, with and without HTMX and with no change
to the template. The post waits for the form's page in the database (the
table `wt_kept_form`) for at most 5 minutes; the visitor gets a cookie for
that page, `wt_form_<key>` (`frontend.KeptFormCookie`), that holds only a
random token. The first view of the page takes the post, so it shows once.
A kept post never holds passwords, files or the spam trap. The comments, newsletter,
polls, registrations and submissions modules do this, and still return
`?<key>=invalid`. See
[A post with mistakes](docs/visitor-forms.md#a-post-with-mistakes).

A visitor's post that fails for another reason, such as an unreachable
database, is answered by `Frontend.Fail`: the theme's `500.gohtml` with
status 500, also for an HTMX post, which go-partial's interactions client
shows as the whole page with `reloadOnError`. The page renders as every page
does, with the theme's asset versions and integrity, and the log names the
action's module; see [500.gohtml](docs/templates.md#500gohtml).

Visitors are known by `frontend.ClientIP(r)`. Behind a reverse proxy, list it
in `Config.TrustedProxies` (addresses or CIDR ranges): only then are
`X-Forwarded-For`, `X-Real-IP` and `Forwarded` believed, read from the right
so a visitor cannot pose as another address. `env.VisitorID(ctx, r,
parts...)` tells visitors apart without storing their address: an HMAC of the
parts and the address with the instance's `visitors` secret.

## Scheduled publishing

Pages, posts and events can be published and unpublished later: the
publication box in the admin plans the selected revision for a date and time
in the site's timezone (stored in UTC), shows the plan and can cancel it. The
app checks every `Config.ScheduleInterval` (30 seconds) and publishes with
the same effects as the publish button; `App.RunSchedules` runs it at once.
Publishing or unpublishing by hand cancels the plan.
Several servers on one database can all run it: every change is claimed with
a conditional update, so one of them makes it. A change that fails, for
example because a translation is incomplete, is dropped and logged. Modules
with revisioned content join through `Parts.Scheduled`.

## Links to pages

The admin knows which published content links to which page: the text of
the published pages and what modules report with `Parts.Links`, such as the
text of a post or the address of a banner's button. The editor of a
published page lists that content and asks before the page is unpublished or
moved to the trash, and Pages → Broken links lists the links that lead to an
address without a page. With the redirects module, the editor also offers to
redirect the addresses a page leaves, when it is unpublished and when a new
slug or parent changes them, and the report offers a redirect per address.
The editors of posts, events, albums, people, directory entries and the
categories of publishing and directory do the same for the item's own
address, and a module adds this to its editor with `editor.Linked`. `App.Links` gives the same links from Go. See
[Links to a page](docs/editing.md#links-to-a-page).

## Caching

Site settings, theme values and the published routes are kept in memory, so
a page view needs no database queries for them. Changes in the same process
apply at once; changes made by another process on the same database show up
within `Config.CacheTTL` (30 seconds; negative disables the cache).

The output cache keeps the pages visitors get in memory, compressed, and
sends them again without rendering them: for a minute in at most 32 MB by
default, set (or turned off) in Site settings → Performance.
`Config.OutputCacheTTL` overrides the time, and a negative one turns the
cache off; without it, `Development` and `ReloadTemplates` turn the cache
off. The card then shows the time that applies, disabled. It keeps answers
to GET requests with status 200 for the site's pages, `sitemap.xml` and
`robots.txt`, by address and by the request headers the answer varies on,
so the whole page and HTMX answers of one address are kept side by side
(up to four lists of such headers per address, go-router's
`MaxVarySets`). Never kept are pages for signed-in editors (the admin's
session cookie), for visitors with a kept post (a `wt_form_<key>` cookie),
previews, searches with a query, and pages sent as private or no-store or
that set a cookie. Changes in the same
process show at once: publishing, settings, theme values, the media
library, posts to the admin sections of modules and to the trash, visitors'
posts and scheduled publication. A visitor's post that stores nothing, such
as spam, one with mistakes, one over the throttle or one a module marks with
`frontend.NothingChanged(r)`, keeps the pages. Changes made from Go show at
once too: the bundled modules' stores and services tell the app once a write
that visitors see is committed, such as an import with `Store()` or a
comment added with `comments.Service.Add`. Writing newsletter subscribers,
from Go or by a visitor's signup or unsubscribe, keeps the pages, as no page
shows them. The site's own code calls `app.Changed()` after other changes,
such as data its pages show from elsewhere, and a module tells the cache
about changes of its own with `Env.Changed()`. Changes by other processes,
and what changes with time alone, such as an event that has passed, show
within the cache's time.
The modules' feeds are kept and refreshed as the pages are.
`sitemap.xml` follows the same changes, so a page published in this process
is in the next sitemap; changes by other processes reach it within 10
seconds plus the cache's time. `Env.PersonalCookie(name)` names a
cookie that changes what pages show to the visitor who has it, such as the
polls' `wt_poll`: the cache keeps a page apart for each value of it, and
other cookies count for nothing, so a cookie of the site that changes its
pages must be named this way. The metrics count its hits, misses, skips
and evictions, and the pages and bytes it holds
(`webthings_output_cache_*`), at `/metrics` once `Config.MetricsToken` is
set, for requests with `Authorization: Bearer <token>`; in `Development`,
with `OutputCacheTTL` set, every answer says in `X-Cache` whether the cache
answered (`hit`, `miss` or `skip`). See
[Caching and compression](docs/deployment.md#caching-and-compression) and,
for modules, [The output cache](docs/module-caching-and-testing.md#the-output-cache).

## Permissions

Administrators may do everything, including managing accounts and groups
and downloading a backup of the site under Backup, which no group right
opens.
Editors get the rights of their groups: a matrix of sections (Pages, Media,
Site settings and every module instance) by actions (read, create, edit,
publish, delete, review, export), edited under Groups in the admin.

The first administrator comes from `Config.AdminEmail` and
`Config.AdminPassword`, or from the one-time setup link a start without
accounts logs; while no account exists, the sign-in page logs it again, at
most once a minute. `App.InviteAdministrator` creates an administrator and
returns a link, valid for an hour, where its owner chooses the password.
Administrators then get a setup guide, Getting started: the site's
languages and title, its theme, e-mail, first pages and security, with a
card on the dashboard until they finish it. See
[Accounts and permissions](docs/permissions.md#the-first-administrator) and
[Set up the site](docs/getting-started.md#set-up-the-site).

## Content and modules

- `content`: the shared engine for module content. `Revisions` keeps drafts,
  publication, history and restore (Pages, Publishing, Events); `Collection`
  keeps ordered items with a published flag (the other modules).
- `admin/editor`: the generic admin for content stores; a module supplies a form
  struct and two mapping functions.
- `modules/*`: the bundled modules (below). They use only public packages,
  exactly like a module from another repository would (a webshop, for
  instance).

Content modules keep content of their own and are usually placed on a page;
modules that attach to something add to other content; site features work
for the whole site and need one instance, so their key is optional
(`redirects.Module()`, or `redirects.Module(redirects.Named("old", "Old
addresses"))`).

| Module | Kind | What it does |
| --- | --- | --- |
| `publishing` | content | News, blogs: posts with categories, revisions, scheduling and feeds |
| `events` | content | Agenda with dates, locations, revisions and a feed |
| `gallery` | content | Albums of images |
| `lists` | content | Ordered entries such as an FAQ, services or courses |
| `testimonials` | content | Quotes from customers |
| `people` | content | Team, staff or board |
| `documents` | content | Downloads by category and year, with a counter |
| `directory` | content | Organisations and places with opening hours, search and CSV import |
| `banners` | content | Hero slides and scheduled notices |
| `forms` | content | Forms built in the admin, with an inbox and notification mail |
| `polls` | content | Questions with options, one vote per visitor; `pollBox` component |
| `comments` | attaches to any content | Comments on posts, events or pages, and guestbooks; moderation and staff replies; `commentThread` component |
| `registrations` | attaches to events (or any subject) | Sign-ups: capacity, prices, confirmation mail, lookup and cancel by code; `registrationForm` component |
| `submissions` | attaches to publishing | Visitors propose news; editors accept it as a draft post |
| `newsletter` | site feature | Double opt-in signup, mailings, CSV export, provider sync; `newsletterForm` component |
| `redirects` | site feature | Old URLs to new ones, with hits and a list of missed paths; key optional |
| `stats` | site feature | Cookieless page views and "most read" lists; key optional |
| `mailjet` ([contrib](https://github.com/donseba/go-webthings-contrib)) | integration | Syncs a newsletter's subscribers with a Mailjet contact list |
| `s3` ([contrib](https://github.com/donseba/go-webthings-contrib)) | integration | Keeps uploaded media in an S3 bucket |

Social media profiles and share links are part of core (see Templates).
Integrations with outside services are in
[go-webthings-contrib](https://github.com/donseba/go-webthings-contrib).

### Rules

`testsuite/architecture_test.go` checks that:

- core never imports a module (tests may);
- modules use core's public API, never `internal/` packages;
- modules add no dependencies: only the standard library, core, other modules
  and the libraries core already uses. Anything else belongs in contrib;
- nothing imports go-webthings-contrib;
- `sitetest` and `db/dbtest` are used in tests only.

A module is a `webthings.Module`: a type, an instance key, its schema files,
its templates, components, template functions and visitor forms, and a
`Setup` that returns admin sections, a public handler, feeds and a service.
The public handler is usually a `Frontend.Router()`, whose routes are relative
to the page the instance is on (`/{$}` is the page itself). The site serves
it at the page's address for each request, so handlers see the whole path,
and `routes.For(r)` builds the addresses of its named routes there
(`RoutePathArgs`); the page itself has no trailing slash, such as
`/en/guestbook`. Outside a request, `pages.Links(routes, base)` builds them
below the page's address (`pages.Service.MountURL`) with `routes.At(base)`,
or gives `pages.NoLinks` while the instance is on no page (`base` is `""`),
so its items are still listed, without links. A request without a route,
any path or method, gets the site's 404 page. Every module's `Of(app, key)` gives site code the
instance's API. Modules whose content editors write offer `Store()` for imports
and seeds; comments and newsletter subscribers are imported with
`comments.Service.Add` and `newsletter.Service.Import`.

## Databases

SQLite (default), Postgres and MySQL. Each package keeps its schema in
`schema/<datetime>_<name>.<flavour>.sql`, one file per flavour and version.
Before the first release only the `_init` files exist: change them and recreate
the development database. IDs are UUIDv7 (native `uuid` on Postgres).

## Development

```sh
task fmt        # gofumpt, grouped imports, line length, blank lines after blocks
task lint       # static checks and whitespace rules
task test       # SQLite
task db:up      # Postgres and MySQL in Docker (task db:down removes them)
task test:all   # all three databases
task race       # the tests with the race detector
task css        # rebuild admin/assets/admin.css after changing admin classes
task vendor     # vendor the admin's JavaScript packages (admin/importmap.json)
task check      # fmt:check, lint, vet, css:check and race, without changing files
```

Until go-partial, go-router, go-form, go-importmap and go-translator release
the changes core uses, core builds only in a Go workspace with their
development branches: `feature/exp-ext` of go-partial and
`feature/webthings-integration` of the other four. Check them out next to
go-webthings and list them in a `go.work`, which git ignores, or name a
workspace file elsewhere in `GOWORK`. The demo, the clone and contrib build in
the same workspace. go-translator's next release is
`github.com/donseba/go-translator/v2`, a version that does not exist yet, so
the workspace replaces it with the checkout rather than using it:

```text
go 1.27.1

use (
	.
	../go-webthings-clone
	../go-webthings-demo
	../go-webthings-contrib
	../go-router
	../go-form
	../go-importmap
	../go-partial
)

replace github.com/donseba/go-translator/v2 => ../go-translator
```

The tools are pinned and kept in `bin/`: `task tools` installs golangci-lint
(fmt, fmt:check and lint run it first) and `task tools:tailwind` downloads
the Tailwind CSS 4.3.2 standalone compiler (css and css:check run it first;
`TAILWIND_BIN` names another Tailwind 4 compiler). Tailwind reads only the
sources listed in `admin/assets/source.css`: the admin's templates, Go code
and scripts, the admin packages of pages, media and site, and `modules/`,
plus the utilities the `admin` package docs promise to modules outside core.
A new place with admin classes needs an `@source` line there. `task check`
runs the suite once, with the race detector. `task editor` rebuilds the
admin's Markdown editor bundle, which needs Node.js, and records it in the
lock file below.

The admin's JavaScript packages are listed in `admin/importmap.json`: htmx,
and the Markdown editor's ProseMirror bundle as a local package that the
admin builds itself (`admin/assets/editor/prosemirror.js`, see
[tools/editor](tools/editor/README.md)). `task vendor` downloads htmx with
go-importmap's `importmap` command into `admin/assets/vendor/`, with its
licence and `importmap.lock.json`, which records the integrity of each file,
the editor bundle and its licences included; `task vendor:check` checks the
files against it without network (`importmap verify` with `-root
admin/assets`, as the bundle is outside the vendor folder). They are
embedded with the other admin assets, and the admin does not start when a
file differs from the lock. Admin assets, go-partial's browser clients among
them, are served with versioned addresses and strong ETags. The admin's
import map, and the script and stylesheet tags its templates write with
`importmap_script` and `importmap_style`, carry the files' integrity;
`source.css`, the Tailwind source of `admin.css`, is not in the map.

`sitetest` starts a whole site in a test and drives the admin like a browser.
Its sites have the output cache off, so a test sees a change on the next
page; `sitetest.Options{OutputCache: true}` keeps the site's setting, and
`Configure` can set `Config.OutputCacheTTL`. `catalogtest.Complete`, of
`sitetest/catalogtest`, checks a section's form texts in the admin's
catalogues of every language but `en_US` (`catalogtest.Source`), the
language they are written in.
`db/dbtest` runs a test on every configured database. The tests of the whole
app are in `testsuite/` (package `testsuite`, with their data in
`testsuite/testdata`).

The user guides are in [docs](docs/README.md), also published at
https://docs.gowebthings.com/go-webthings; the bundled modules are documented
at https://modules.gowebthings.com.
