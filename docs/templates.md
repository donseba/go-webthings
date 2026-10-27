# Templates

The public site is rendered from Go templates: a layout around one content template per screen. This guide covers which template gets which data, how module templates are found, and how to write starting points for them.

## Where templates live

`Config.Templates` is the folder with the site's templates, usually `os.DirFS("templates")`. Each theme is a folder in it (see [Themes](themes.md)); the paths in this guide are relative to a theme folder such as `templates/default/`.

```text
layout.gohtml            wraps every screen; gets frontend.View
page.gohtml              a content page; gets frontend.Page
404.gohtml               the "not found" page; gets frontend.NotFound (optional)
500.gohtml               the page for a failing template (optional)
search.gohtml            the search screen; gets frontend.SearchView (optional)
news/list.gohtml         a module screen, by instance key ...
publishing/post.gohtml   ... or by module type
partials/card.gohtml     snippets every template can use
regions/head.gohtml      the page's tags in <head>; gets frontend.View
regions/menu.gohtml      a part of the layout; gets frontend.View (for HTMX)
fragments/results.gohtml a part of a page; gets the page's content (for HTMX)
mail/                    e-mail templates
assets/site.css          CSS, JavaScript and images, served at /theme/
```

The layout places a region with `{{slot "menu"}}`, which HTMX navigation updates along with the content, and a template places a fragment with `{{requiredSlot "results"}}`, `{{slot "results"}}` or `{{deferredSlot "results"}}`, which HTMX can ask for on its own. See [HTMX](htmx.md).

Each theme is rendered by [frontend.Templates](../frontend/templates.go), which uses the [render](../render/render.go) package on top of [go-partial](https://github.com/donseba/go-partial). The templates are `html/template`, so output is escaped for its context. Every template gets the same functions: core's, those of the modules in `app.Use`, the translation functions of `Config.Translations`, and your own from `Config.Funcs` (see [Template functions](template-functions.md)).

The layout, content templates, partials, regions, fragments, components and e-mail templates are parsed once and kept, so restart the site after adding or changing one. While you develop, `Config.ReloadTemplates` reads them again for every request, and `Config.Development` shows template errors in the browser (see [While you develop](#while-you-develop)). `ReloadTemplates` also gives a changed file of the theme's `assets/` or of `Config.Assets` a new version, and their tags and the import map then carry no integrity (see [Themes](themes.md#assets)).

## The layout

`layout.gohtml` wraps every screen. It gets the whole page context, a [frontend.View](../frontend/view.go), and places the content template with `{{content}}`:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/frontend.View */}}
<!doctype html>
<html lang="{{.Locale}}">
<head>
    <meta charset="utf-8">
    {{slot "head"}}
    {{styles "webthings.css" "site.css"}}
</head>
<body>
    <a href="{{.Site.HomeURL}}">
        {{with .Site.LogoURL}}<img src="{{.}}" alt="">{{end}}
        {{.Site.Name}}
    </a>

    <nav>
        {{range .Menu}}
            <a href="{{.URL}}" {{if .Active}}aria-current="page"{{end}}>{{.Title}}</a>
        {{end}}
    </nav>

    {{with .Editor}}<a href="{{.EditURL}}">Edit</a>{{end}}

    {{content}}

    <footer>{{.Site.Footer}}</footer>
</body>
</html>
```

`{{slot "head"}}` places the tags of the page in `<head>`. A theme without `regions/head.gohtml` gets core's: the `<title>`, the description, robots for a preview, the canonical link, the `hreflang` links with `x-default`, the `og:` tags and `twitter:card`, the favicon and the apple-touch-icon, `{{feedLinks}}`, `{{apiLink}}` and `{{structuredData}}` (see [Search and SEO](search-and-seo.md#the-head-region)). For tags of its own, a theme adds `regions/head.gohtml`, which gets the View, like the layout, and calls core's tags first:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/frontend.View */}}
{{template "webthings/head-tags.gohtml" .}}
<meta name="theme-color" content="#336699">
```

A head region that does not call `webthings/head-tags.gohtml` replaces core's tags and writes all of them itself. A layout without `{{slot "head"}}` gets nothing from core and writes its tags itself.

On a whole page the region's tags sit between two empty `<template>` markers, with the `<title>` just before them. An HTMX navigation replaces them with those of the new page, so no tag of the page left stays behind, except after a Back or Forward that htmx restores from its history (see [The head region](htmx.md#the-head-region)). Static tags, such as the stylesheets, stay in the layout. `render.View.HeadRegion` names the region placed in `<head>`; for themes core sets it to `"head"`, and go-partial's `exp/head` does the work with `head.Region`.

`{{styles "webthings.css" "site.css"}}` links the stylesheets in the order they must have: go-partial's `partial-errors.css` first, then the files given, each from the theme's `assets/` or else from core, such as `webthings.css`, and the theme's values stylesheet last, each with its version and integrity (see [styles](template-functions.md#styles)).

| Field | What it holds |
| --- | --- |
| `.Site` | The site in the page's language: `Name`, `Tagline`, `SEOTitle`, `SEODescription`, `Footer`, `LogoURL`, `FaviconURL`, `AppleTouchIconURL`, `SocialImageURL`, `Organisation`, `Email`, `Phone`, `Address`, `HomeURL` and `Profiles` |
| `.Locale` | The page's language, such as `en` |
| `.Path` | The current path with its language prefix, such as `/en/news/hello` |
| `.Title`, `.Description` | The title and description of the page or module item |
| `.Image` | An image for sharing, when a module screen sets one, such as a post's featured image; `.Site.SocialImageURL` is the site's default. An image from the media library is made into a 1200 × 630 JPEG, as the social image is |
| `.Menu` | The published pages below the home page: `ID`, `ElementID` (the `id` for the item's element, see [The menu](htmx.md#the-menu)), `Path`, `Title`, `URL`, `Active` and `Children` |
| `.Languages` | Links to the same content in each language where it is published: `Locale`, `URL`, `Active` and `Default`, set for the site's default language, whose address the head region gives as `hreflang="x-default"` (see [Search and SEO](search-and-seo.md#canonical-and-alternate-links)) |
| `.Page` | The content page on page screens; on module screens, the page the module is placed on; `nil` elsewhere |
| `.Module` | The instance key on module screens, such as `news`; empty elsewhere |
| `.Content` | The data of the content template |
| `.Preview` | Set when a signed-in editor previews a revision on the site |
| `.Revision` | The previewed revision: its `Number`, `Published` when it is the published one, and `PublishedURL`, the address without the preview; `nil` elsewhere |
| `.Theme` | The theme's `Name` and the `Values` of its settings (see [Themes](themes.md#settings)); `{{styles}}` and `{{themeStyle}}` link those with `css` as custom properties |
| `.Editor` | The edit link for a signed-in editor who may edit what is shown (`Name`, `EditURL`); `nil` for visitors (see [Editing and publishing](editing.md)) |
| `.Status` | The page's status, such as 404 on the not-found page or a status a module screen sets; zero is 200 OK. The response has it, also an HTMX request for the content, a region or a fragment alone |

`.Site.Footer` is the editors' formatted footer text, rendered from Markdown to HTML. It brings its own paragraphs, so place it in a `<footer>` or `<div>`, not in a `<p>`.

The site's images are copies made to measure from the images of the **Site settings**: `.Site.LogoURL` is the logo at most 600 pixels wide in the format it was uploaded in, `.Site.FaviconURL` the favicon as a 32 × 32 PNG, `.Site.AppleTouchIconURL` the favicon as a 180 × 180 PNG for the home screen of phones, and `.Site.SocialImageURL` the social image as a 1200 × 630 JPEG for `og:image`. Each is empty when the settings have no image. See [The site's images](media.md#the-sites-images).

`{{feedLinks}}` writes a `<link rel="alternate">` for each Atom and RSS feed of the page shown, such as the posts of a news page, with absolute addresses, so browsers and feed readers find them. Pages without feeds get nothing. See [feedLinks](template-functions.md#feedlinks). For a link visitors see, `{{feedURL "atom"}}` and `{{feedURL "rss"}}` give the address of the page's feed, or `""` (see [feedURL](template-functions.md#feedurl)).

`{{apiLink}}` writes a `<link rel="alternate" type="application/json">` to the page shown in the [content API](api.md), for apps, with an absolute address. It writes nothing while the API is off, on the not-found page and in a preview. See [apiLink](template-functions.md#apilink).

`{{structuredData}}` writes the page's structured data for search engines, schema.org's JSON-LD in one `<script type="application/ld+json">`: the site, its organisation, the page's trail, the page, and the item of the module's content, such as a post's `Article`. The not-found page gets nothing. See [structuredData](template-functions.md#structureddata) and [Search and SEO](search-and-seo.md#structured-data).

For other links into the admin, `adminURL` gives the address of a named admin route to signed-in editors, and `""` to visitors (see [Template functions](template-functions.md#adminurl)):

```gotemplate
{{with adminURL "admin.dashboard"}}<a href="{{.}}">Admin</a>{{end}}
```

A preview is a page an editor opens from the admin to see a revision before it is published. A banner tells them so:

```gotemplate
{{with .Revision}}
    <p class="preview-bar" role="status">
        {{tl localizer "Preview of revision %d" .Number}}
        <a href="{{.PublishedURL}}">{{tl localizer "Show the published version"}}</a>
    </p>
{{end}}
```

See [Editing and publishing](editing.md#previewing-a-revision).

`partial-errors.css`, which `{{styles}}` links first, is go-partial's stylesheet of the box that takes the place of a part of the page that failed, which core serves at `/_/partial-errors.css`; a layout without `{{styles}}` links it with `{{assetStyle "partial-errors.css"}}` (see [While you develop](#while-you-develop) and [The error box](themes.md#the-error-box)).

`task templates -- site` writes a complete layout to start from, with `{{slot "head"}}` and `{{styles}}`, and a `regions/head.gohtml` that calls core's tags (see [Starting points](#starting-points)).

## Content templates

A content template gets only its own data, so it stays small and your code editor can check its fields. The layout gets everything else.

| Template | Data | Shown for |
| --- | --- | --- |
| `page.gohtml` | `frontend.Page` | Every content page |
| `404.gohtml` | `frontend.NotFound` | Addresses that do not exist, with status 404 |
| `search.gohtml` | `frontend.SearchView` | `/<locale>/search?q=` |
| `<key>/<name>.gohtml` | The module's view | Module screens, such as `news/post.gohtml` |

Content templates reach the page context through functions instead of fields, for example `{{locale}}`, `{{theme "accent"}}` and `{{moduleURL "news"}}`.

### page.gohtml

`page.gohtml` shows a page of the page tree. Its [frontend.Page](../frontend/view.go) has `ID`, `ParentID` (empty for the home page), `Slug`, `Path` (such as `/about/team`), `Title`, `Description`, `Body` (the page text as HTML), `URL` and `IsHome`:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/frontend.Page */}}
<main id="content">
    <h1>{{.Title}}</h1>
    {{with .Description}}<p class="lead">{{.}}</p>{{end}}
    {{.Body}}

    {{if .IsHome}}
        {{range posts "news" 3}}{{template "partials/post-card.gohtml" .}}{{end}}
    {{end}}
</main>
```

See [Pages](pages.md) for the page tree.

### 404.gohtml

`404.gohtml` is shown with status 404 for addresses that do not exist. [frontend.NotFound](../frontend/view.go) has one field, `HomeURL`, the home page in the current language. The layout's `.Title` is `Not found`, translated with the site's translations (the msgid is `Not found`). Without `404.gohtml`, visitors get a plain-text "404 page not found".

### search.gohtml

`search.gohtml` is the search screen at `/<locale>/search?q=`. [frontend.SearchView](../frontend/search.go) has the `Query`, the `Results` (each with `Title`, `Summary`, `URL`, `Module`, `Type` and `Score`), the screen's `URL` for the form, and `Throttled`, set when the visitor searched too often in a short time: the search did not run, `Results` is empty and the response has status 429, so the template shows a message instead. The layout's `.Title` is the query, and the links of `.Languages` keep it. A fragment of `search.gohtml`, such as results that follow the visitor's typing, comes with the regions out of band, so the title and the language links follow the query; fragments of other pages come alone (see [Fragments](htmx.md#fragments)).

```gotemplate
{{if .Throttled}}
    <p role="status">Too many searches in a short time. Wait a moment and try again.</p>
{{else if .Query}}
    {{range .Results}}<h2><a href="{{.URL}}">{{.Title}}</a></h2>{{else}}<p>Nothing found for “{{.Query}}”.</p>{{end}}
{{end}}
```
 A published page with the path `/search` takes precedence over the screen, and without `search.gohtml` the address answers 404. See [Search and SEO](search-and-seo.md).

## Module templates

A module renders its screens with templates named after the instance key: `publishing.Module("news", "News")` shows its posts with `news/list.gohtml` and `news/post.gohtml`. When a theme has no `<key>/<name>.gohtml`, the module type's `<type>/<name>.gohtml` is used, so instances can share a design or each have their own:

```text
news/post.gohtml         the news instance
publishing/post.gohtml   blog and every other publishing instance
```

A theme other than `default/` looks for both names in its own folder first, then in `default/` (see [Themes](themes.md#theme-folders)).

One shared template can still tell instances apart. In the layout `.Module` is the instance key; some module views carry it too, such as the `Key` of a list:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/lists.ListView */}}
<main id="content">
    <h1>{{.Title}}</h1>
    {{if eq .Key "faq"}}
        {{range .Entries}}<details><summary>{{.Title}}</summary>{{.Body}}</details>{{end}}
    {{else}}
        {{range .Entries}}<article><h2>{{.Title}}</h2>{{.Body}}</article>{{end}}
    {{end}}
</main>
```

The README of a module with screens lists its templates and their data, for example [Publishing](../modules/publishing/README.md) and [Events](../modules/events/README.md). Module screens have no built-in default: the theme must have them (see [Missing templates](#missing-templates)).

Module views give each image as a copy made for pages, never as the uploaded file: `<X>URL` is a WebP copy at most 1600 pixels wide, `<X>Srcset` lists it with narrower copies for a `srcset` attribute, `<X>Width` and `<X>Height` are its size, and `<X>OriginalURL` is the uploaded file, for a download link. A post has `ImageURL`, `ImageSrcset`, `ImageWidth`, `ImageHeight` and `ImageOriginalURL`, a person `PhotoURL` and the others:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/publishing.Post */}}
<main id="content">
    <h1>{{.Title}}</h1>
    {{if .ImageURL}}
        <img src="{{.ImageURL}}" srcset="{{.ImageSrcset}}" sizes="(min-width: 46rem) 42rem, 100vw"
             width="{{.ImageWidth}}" height="{{.ImageHeight}}" alt="{{alt .ImageURL}}">
    {{end}}
    {{.Body}}
</main>
```

The gallery's images also have `LargeURL`, a copy at most 2400 pixels wide for a lightbox. `{{image}}` and `{{srcset}}` make copies in other sizes from these URLs; see [Image sizes in templates](media.md#image-sizes-in-templates).

The views of modules with a visitor form, such as the comments' `ListView` and the newsletter's `SignupView`, carry the form as `.Form`. A theme renders it with `{{form_open .Form}}`, its fields with `{{form_field .Form "name" nil}}`, with their labels and limits in the page's language, and `{{form_close}}`, or the whole form with `{{form_render .Form nil}}` (see [Template functions](template-functions.md#forms)). The form returns the visitor to the element whose id is the instance key, so give the element around the form and its result that id; the ids of its fields start with the key, such as `guestbook-name`. After a post with mistakes, the forms of the comments, newsletter, registrations and submissions modules show the visitor's values again, with the messages at the fields that need a correction, without a change to the template (see [A post with mistakes](visitor-forms.md#a-post-with-mistakes)).

## Partials

Templates in `partials/` can be used in every template, the layout included:

```gotemplate
{{range posts "news" 3}}{{template "partials/post-card.gohtml" .}}{{end}}
```

The name is the path with its extension. Give a partial its own `@dot` line, like any other template:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/publishing.Post */}}
<article class="card">
    <h3><a href="{{.URL}}">{{.Title}}</a></h3>
    <span class="meta">{{date .Date "medium"}}</span>
</article>
```

Components can use `partials/` too. E-mail templates use `mail/partials/` instead.

## The @dot line and editor support

The first line of a template names the type of its data:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/events.Event */}}
```

The line changes nothing at runtime. It is a contract for [go-clue](https://docs.gowebthings.com/go-clue), which gives code editors (GoLand, VS Code, Sublime Text, Vim and Neovim) completion, checks of fields and functions, hover and go-to-definition in Go templates. The starting points come with the right line.

go-clue reads `.go-clue/config.json` in the site's folder. List the go-webthings packages whose functions the templates use, as the starter and the demo do:

```json
{
  "include": ["/"],
  "exclude": ["vendor", "var", "bin"],
  "providers": [
    "github.com/donseba/go-partial",
    "github.com/donseba/go-partial/exp/localization",
    "github.com/donseba/go-translator/v2",
    "github.com/donseba/go-webthings/frontend",
    "github.com/donseba/go-webthings/modules/publishing",
    "github.com/donseba/go-webthings/modules/events"
  ],
  "functionMaps": [
    "github.com/donseba/go-partial/exp/localization.FuncMap"
  ]
}
```

## The page anywhere: @model

`@dot` names a template's dot, which a partial or a `range` replaces. `@model` names a value a template can use however deep it is:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/publishing.Post */}}
{{/* @model View github.com/donseba/go-webthings/frontend.View */}}
<p>{{.Title}} on {{View.Site.Name}}</p>
```

Every render offers two values, matched by their type: the page's [frontend.View](../frontend/view.go), what the layout gets, and the content of the page, what the content template gets, such as a `publishing.Post`. The name is yours. A partial can declare the post it is shown with, whatever was passed to it:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/comments.Comment */}}
{{/* @model Post github.com/donseba/go-webthings/modules/publishing.Post */}}
<p>{{.Name}} on {{Post.Title}}</p>
```

`@model` works in the layout, content templates, partials and components. A value the page does not have, such as a `publishing.Post` on an events page, fails the render with an error that names it. E-mail templates and templates rendered with `Frontend.Execute` have no page; they use `@dot` only.

## Starting points

`App.WriteTemplates(dir, key)` writes starting points for templates into `dir`: the module's own example when it has one, otherwise plain HTML generated from the data, with the `@dot` line and every field. The starter runs it as a task, writing into `templates/default/`:

```sh
task templates -- agenda
task templates -- news blog
task templates -- site
```

| Key | Files written |
| --- | --- |
| `site` | `layout.gohtml` with its head region `regions/head.gohtml` and its stylesheet `assets/site.css`, `page.gohtml`, `404.gohtml`, `search.gohtml` and core's component `site/share.gohtml` |
| A module instance key | `<key>/<name>.gohtml` for each screen and component, and `mail/<key>/<name>.gohtml` for each e-mail |

Existing files are never overwritten, and the app does not need to be started. `WEBTHINGS_THEME=night task templates -- agenda` writes into `templates/night/` instead (see [Themes](themes.md)).

The layout of `site` links the theme's `assets/site.css` with `{{styles "webthings.css" "site.css"}}`, and `site` also writes that file, `frontend.StyleScaffold`: a comment saying it comes after `webthings.css` and before the theme's settings, so the settings' custom properties, such as `var(--accent)`, can be used in it. A file that is there already is never overwritten. A file the theme does not have is a call that finds nothing, which fails every page in development (see [Calls that find nothing](#calls-that-find-nothing)).

Components and e-mail have defaults, so their copies are optional: delete the ones you keep as they are, and the module's default is used. Module screens need their files.

In your own `main.go`:

```go
written, err := app.WriteTemplates("templates/default", "agenda")
if err != nil {
    log.Fatal(err)
}

for _, file := range written {
    log.Println("wrote", file)
}
```

## Missing templates

When the site starts, every module that can be placed on a page is checked against every theme. A screen that is missing as `<key>/<name>.gohtml` and as `<type>/<name>.gohtml` is logged with the command that writes it:

```text
WARN missing template theme=default template=agenda/list.gohtml or=events/list.gohtml generate="task templates -- agenda"
```

`task templates -- agenda` is the starter's task, which calls `App.WriteTemplates(dir, "agenda")` (see [Starting points](#starting-points)). The site still starts. The check does not cover `layout.gohtml`, `page.gohtml`, `404.gohtml` or `search.gohtml`.

## Checking the templates at start

When the site starts, it parses every `*.gohtml` of every theme, in any folder, with the functions its pages render with: core's, those of the modules in `app.Use`, the translation functions and `Config.Funcs` (go-partial's `Partial.CheckTemplates`). Each problem a render would fail with is logged at WARN as `template problem`, with the themes that render the file and its position: a template that does not parse, a function the site does not have, and a `{{template}}` that names a template no file of the theme defines. A function of a bundled module names that module; any other unknown name gets the closest one:

```text
WARN template problem themes=default problem="templates/default/page.gohtml:1:22: function \"absUrl\" not defined; did you mean \"absURL\"?"
WARN template problem themes=dark problem="templates/dark/page.gohtml:1:6: function \"pollBox\" not defined: it comes from the polls module; add it to app.Use or remove the call"
```

With `os.DirFS("templates")` the position starts with that folder, so your editor opens it; another file system, such as an embedded one, gives it from the theme's folder, such as `default/page.gohtml:1:22`. A file of `default/` that other themes use is logged once, with all of them.

With `Config.Development`, as `task dev` sets it, Start fails with every problem at once, with `webthings.ErrTemplates`:

```text
templates have problems (in development every problem stops the start):
  templates/default/page.gohtml:1:22: function "absUrl" not defined; did you mean "absURL"?
```

Otherwise Start fails only for a problem every page of the active theme would fail with: a template of its layout, `regions/` or `partials/` that does not parse or calls a function the site does not have, such as one of a module removed from `app.Use`. Every other problem only warns, also a `{{template}}` that names no file, which fails only where it runs:

```text
templates have problems (every page of the active theme default would fail):
  templates/default/layout.gohtml:2:5: function "newsletterForm" not defined: it comes from the newsletter module; add it to app.Use or remove the call
```

A site with a `Renderer` of its own has no templates to check.

### Calls that find nothing

Some calls run but cannot do what they ask, and would show nothing. Each is logged once at WARN as `template mistake`, with what there is:

- an instance key the site has no such module of, such as `{{posts "nieuws" 3}}`;
- a file that neither the theme's `assets/` nor core has, in `asset`, `assetStyle`, `assetScript` or `styles`;
- a setting the theme does not declare, such as `{{theme "acent"}}`.

```text
WARN template mistake problem="posts: no publishing instance \"nieuws\" (known: blog, news)"
WARN template mistake problem="assetStyle: no file \"sitee.css\" in the assets of theme default (closest: site.css, webthings.css, focal-points.css)"
WARN template mistake problem="theme: no setting \"acent\" in theme default (known: accent)"
```

Outside development the page still renders: `posts` gives no posts, `assetStyle` the file's address without a version or integrity, and `theme` gives `""`. With `Config.Development` the page fails with the message, which the error page shows.

## When something goes wrong

| Situation | What the visitor gets |
| --- | --- |
| No templates at all | A plain-text page saying which templates to add |
| The screen's template is missing | Status 500 with `missing_template`; the log names the template and theme |
| `404.gohtml` is missing | A plain-text "404 page not found" |
| `search.gohtml` is missing | Status 404 at `/<locale>/search` |
| An error in the layout or a content template | Status 500 with the theme's `500.gohtml`, or without one go-partial's plain error page: "Internal Server Error", "Something went wrong. Please try again." and the address. The heading and the sentence are translated into the page's language with the site's translations, and the page's `lang` is that language; the page never names the template or the error. The error is logged as `render page`. With `Config.Development`, a detailed error (see below) |
| A module's handler or a visitor action that fails, such as a comment that cannot be saved | The same, in the page's language or that of the page the form was on, also for an HTMX request (`Frontend.Fail`); the error is logged as `render page`. With `Config.Development`, go-partial's detailed error page with the error |
| An error in `404.gohtml` | The theme's `500.gohtml` with status 500, or a plain-text "404 page not found" without one; the error is logged as `render not found`. With `Config.Development`, a detailed error with status 500 |

Errors include a function that does not exist, a field the data does not have and a component that fails. One failing part fails the whole page, so visitors never get half a page. The log names the template and the line. A template that does not parse and a function that does not exist are found at start, before a visitor meets them (see [Checking the templates at start](#checking-the-templates-at-start)). An invalid `theme.json` stops the site at start.

### 500.gohtml

`500.gohtml` is the theme's page for a failing template, and for a module's handler or a visitor action that fails (`Frontend.Fail`), outside development. It renders on its own, without the layout, with status 500, and is never cached; an HTMX request gets it alone too. It gets every template function and the theme's `partials/`, in the page's language; `asset`, `assetStyle`, `assetScript`, `themeStyle` and `styles` give the theme's versioned addresses and integrity as on every page, also after a failing action. `@model View` names the page's [frontend.View](../frontend/view.go), what the layout gets; after a failing action it is the site in the language of the page the form was on, without a page of its own. Its dot is go-partial's [errors.Data](https://pkg.go.dev/github.com/donseba/go-partial/ext/errors#Data): its `Message` is a sentence that tells nothing of the error, "Something went wrong. Please try again.", already translated into the page's language with the site's translations, so `{{.Message}}` shows it; its `Title` is the heading, the status text "Internal Server Error", translated the same way; its `Lang` is the page's language, as `View.Locale` is; its `Error` holds the error itself, so leave that out. `task translations` adds the sentence and the heading to the POT file (see [Languages](languages.md)). The [demo](https://github.com/donseba/go-webthings-demo) has one in `templates/default/500.gohtml`. A minimal one:

```gotemplate
{{/* @dot github.com/donseba/go-partial/ext/errors.Data */}}
{{/* @model View github.com/donseba/go-webthings/frontend.View */}}
<!doctype html>
<html lang="{{View.Locale}}">
<head>
    <meta charset="utf-8">
    <title>{{View.Site.Name}}</title>
    {{styles "site.css"}}
</head>
<body>
    <h1>{{.Title}}</h1>
    <p>{{.Message}}</p>
    <p><a href="{{View.Site.HomeURL}}">{{tl localizer "Back to the home page"}}</a></p>
</body>
</html>
```

When `500.gohtml` fails itself, go-partial's plain error page answers instead, as for a theme without one, and it never names the template or the error. A theme other than `default/` uses the one in `default/` when it has none. Without any, the visitor gets go-partial's plain error page, with its heading and sentence in the page's language (see [When something goes wrong](#when-something-goes-wrong)). That page styles itself with one `<style>` element. The site's own Content-Security-Policy allows that element by its hash, go-partial's `errors.StyleHash()` (`ext/errors`), on every page (see [Under a Content-Security-Policy](themes.md#under-a-content-security-policy)); another policy without `'unsafe-inline'` in `style-src` blocks it unless it has that hash, and the page then shows unstyled. A `500.gohtml` that links the theme's stylesheets, like the one above, needs no hash. Under the site's policy, a `<style>` element or `style` attribute in `500.gohtml` itself is blocked.

### While you develop

`Config.Development` shows a failing template in the browser instead of `500.gohtml` or a plain error. The error page replaces the whole response: it says "Template render error" and names the template, the line and the error, with status 500, and is never cached. An HTMX request gets the same error alone, as a box without the layout or regions (see [HTMX](htmx.md#navigation-that-swaps-only-the-content)). The error is still logged. It works on the site and in the admin. In development Start also fails for every problem the check at start finds, and a call that finds nothing fails its page (see [Checking the templates at start](#checking-the-templates-at-start)).

The error page styles itself with the same `<style>` element as go-partial's plain error page, which the site's own policy allows by its hash (see [500.gohtml](#500gohtml)). The box has no inline style, only go-partial's classes: `go-partial-error`, and `go-partial-error__title`, `go-partial-error__message` and `go-partial-error__details` for its heading, sentence and list. go-partial's `partial-errors.css` styles them. Core serves it at `/_/partial-errors.css`, and `{{styles}}` in the layout links it first, as in the layout `task templates -- site` writes; a layout without `{{styles}}` links it with `{{assetStyle "partial-errors.css"}}` (see [The error box](themes.md#the-error-box)). Without it the box shows unstyled, as a heading, a paragraph and a list, unless the theme's stylesheet styles the classes. `errors.StylesheetFS()` holds the same rules as a file.

`Config.Development` also makes `{{debug .}}` show a value, such as the dot of a template, so you can see which fields it has:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/frontend.Page */}}
<main id="content">
    {{debug .}}
    <h1>{{.Title}}</h1>
</main>
```

Without `Config.Development`, `{{debug .}}` shows nothing, so one that is left behind does no harm. Use `Development` on your own computer only, like `ReloadTemplates`: the error page and `{{debug .}}` show internals to anyone who opens the page.

```go
app := webthings.New(webthings.Config{
    Templates:       os.DirFS("templates"),
    ReloadTemplates: true,
    Development:     true,
})
```

See [Template functions](template-functions.md#debug) for `debug`, and [Configuration](configuration.md) for every setting.

## Components

The interactive modules offer components: parts of a page with plain, accessible default markup that any template includes with one function, such as `{{commentThread "remarks" .ID}}` or `{{newsletterForm "letters"}}`. Core has one, `{{shareBar "" .Title}}`.

A theme replaces a component with `<key>/<name>.gohtml` or `<type>/<name>.gohtml`, like a module screen (see [Themes](themes.md#theme-folders) for a theme other than `default/`), and core's with `site/<name>.gohtml`; otherwise the module's default renders. A component is rendered through go-partial as a part of the page, a child of the template that includes it: in the page's language, with the theme's `partials/`, every template function and the page for `@model` (see [Template functions](template-functions.md#components-and-e-mail)). A component that fails fails the page. [Themes](themes.md#components) covers their markup and styling; each module's README lists its components, for example [Comments](../modules/comments/README.md).

## E-mail templates

Modules write their e-mail from templates too. A theme replaces a module's default with `mail/<key>/<name>.gohtml` or `mail/<type>/<name>.gohtml`, found like a module screen (see [Themes](themes.md#theme-folders)); templates in `mail/partials/` can be used in every e-mail template. A mail template defines `subject`, `text` and optionally `html`. See [E-mail](mail.md).

## Your own functions

`Config.Funcs` adds functions to every template, after core's and the modules'. A function with the name of a core or module function replaces it. The site's translations are not among them: `Config.Translations` takes the translator `frontend.Translations` returns, which gives templates `tl`, `tn`, `ctl` and `ctn` (see [Languages](languages.md)):

```go
translations, err := frontend.Translations("translations")
if err != nil {
    log.Fatal(err)
}

app := webthings.New(webthings.Config{
    Templates:    os.DirFS("templates"),
    Translations: translations,
    Funcs:        []template.FuncMap{siteFuncs()},
})
```

A function whose first parameter is a `context.Context` gets the context of the request being rendered; templates call it without that parameter. In that context, `frontend.Locale(ctx)` is the page's language, `frontend.Request(ctx)` the request and `frontend.Service(ctx, key)` the API of a module instance:

```go
func siteFuncs() template.FuncMap {
    return template.FuncMap{
        "year": func() int {
            return time.Now().Year()
        },
        // {{with featured "news"}}: the newest post with an image.
        "featured": func(ctx context.Context, key string) *publishing.Post {
            news, ok := frontend.Service(ctx, key).(*publishing.Service)
            if !ok {
                return nil
            }

            posts, err := news.Latest(ctx, frontend.Locale(ctx), 10)
            if err != nil {
                return nil
            }

            for _, post := range posts {
                if post.ImageURL != "" {
                    return &post
                }
            }

            return nil
        },
    }
}
```

In the layout:

```gotemplate
{{with featured "news"}}<a href="{{.URL}}"><img src="{{image .ImageURL "1200"}}" alt="">{{.Title}}</a>{{end}}
<p>© {{year}} {{.Site.Name}}</p>
```

Your functions also work in components and e-mail templates. To give editors their signatures and go-to-definition, mark the function that returns them for go-clue, and give each function that takes a context its signature as templates call it:

```go
// go-clue:funcmap
func siteFuncs() template.FuncMap {
    return template.FuncMap{
        //go-clue:sig func(key string) *github.com/donseba/go-webthings/modules/publishing.Post
        "featured": featured,
    }
}
```

There is no need for go-clue's `@func` in templates: it declares a function for one template, which must still be registered here.

A site or module function named like one of go-partial's, such as `url`, `content`, `locale`, `localizer`, `slot`, `hasSlot`, `requiredSlot`, `deferredSlot`, `targetHeader`, `targetValue` or `targetIs`, or with a name starting with `_`, stops the site at start with `render.ErrReserved`. Two module types that offer the same function name stop it too, with `template function "<name>" is offered by <type> and <type>`. [Template functions](template-functions.md) lists go-partial's functions.

## HTMX requests

go-partial answers HTMX requests, with the htmx 4 protocol. A request with `HX-Request: true` aimed at the element with ID `content` gets only the content template, without the layout, followed by the theme's regions out of band, in the order of their file names, the same on every request, so the same page gives the same bytes and `ETag`; the starting points wrap their content in `<main id="content">` for this. Aimed at a fragment of the theme, it gets only that fragment, when the page has what the fragment declares; on the search screen the regions follow it out of band. Without a target the request gets the whole page, and so does a request for the whole document, such as one with `hx-select`. Aimed at any other element, or at a fragment the page cannot answer for, it gets the content template with `HX-Retarget: #content` and `HX-Reswap: outerHTML`, and for a GET `HX-Push-Url`, so HTMX replaces the content element and the address follows. [HTMX](htmx.md) covers regions, fragments and forms.

## Rendering one template from Go

`app.Frontend.Execute(ctx, name, locale, data)` renders one template of the active theme without the layout, for example `Execute(ctx, "partials/card", "nl", post)`. It renders through go-partial like a part of the theme's pages, with `partials/` and every template function, in the given language. There is no request, so `request` and `url` give nothing, and no page for `@model`.
