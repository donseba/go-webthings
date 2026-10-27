# Another template engine

go-webthings renders the public site with Go templates, but another template engine can take over by implementing `frontend.Renderer`. This guide covers what a renderer receives, what it must do and what the site gives up.

## The Renderer interface

A renderer turns a page's data into HTML. The interface is in [frontend/view.go](../frontend/view.go):

```go
type Renderer interface {
    Render(w http.ResponseWriter, r *http.Request, name string, view View) error
    Has(name string) bool
}
```

The built-in Go templates are a renderer too: `frontend.Templates` in [frontend/templates.go](../frontend/templates.go), one per theme.

## Config.Renderer

Set `Config.Renderer` instead of `Config.Templates`. When both are set, the renderer is used.

```go
app := webthings.New(webthings.Config{
    Name:     "My site",
    Renderer: engine,
    Assets:   os.DirFS("static"),
})
```

## What a renderer receives

`Render` is called once per page with:

- `name`, the template to render, without extension.
- `view`, the whole [frontend.View](../frontend/view.go): what `layout.gohtml` gets, with the content template's data in `view.Content`. The renderer draws both the layout and the content.
- `r`, the request. Its context carries the page being rendered, so `frontend.Locale(ctx)`, `frontend.Request(ctx)` and `frontend.Service(ctx, key)` work, and so do the template functions (see [Template functions](#template-functions)).

| `name` | `view.Content` |
| --- | --- |
| `page` | `frontend.Page` |
| `404` | `frontend.NotFound` |
| `search` | `frontend.SearchView` |
| `<key>/<name>`, such as `news/post` | The module's view, such as `publishing.Post` |
| `<type>/<name>`, such as `publishing/post` | The same, when `Has` reports no `<key>/<name>` |

[Templates](templates.md) describes the View and each kind of content.

## What a renderer must do

`Has` reports whether the site has a template. go-webthings asks it before every render and acts on the answer:

| `Has` is false for | Then |
| --- | --- |
| A module's `<key>/<name>` | `<type>/<name>` is rendered instead |
| The screen to render | The visitor gets status 500 with `missing_template`, and the log names the template |
| `404` | Visitors get a plain-text "404 page not found" |
| `search` | `/<locale>/search` answers 404 |

When the site starts, `Has` also decides which module screens are reported as missing (see [Templates](templates.md#missing-templates)).

`Render` writes the whole page and sets the `Content-Type`. Leave the status to go-webthings: the not-found page is sent with 404, and a module screen that sets a status with that status, whatever success status the renderer sets. `view.Status` is that status, such as 404 for `404`; zero is 200 OK. An error status from the renderer, 400 or higher, is kept. When `Render` returns an error, go-webthings logs it and answers with status 500 and go-partial's plain error page (`Frontend.Fail`): its heading, "Internal Server Error", and its sentence, "Something went wrong. Please try again.", are in the page's language, with the site's translations, and so is its `lang`; for `404` it sends a plain not-found page instead. Render into a buffer first and write only when it worked, so an error leaves no half page.

With `Config.Development` the plain page shows the error in place of the sentence, in English; outside development it never shows the error. The detailed error page, with the template and the line, belongs to the built-in Go templates, and so does a theme's `500.gohtml` (see [Templates](templates.md#when-something-goes-wrong)). A renderer that answers an error itself, with a detailed page while you develop or with an error page of its own, writes that page and returns an error that wraps `render.ErrShown`; go-webthings then logs the error and writes nothing more.

Escaping is the renderer's job. Fields of type `template.HTML`, such as a page's `Body`, are HTML already; everything else is text.

## Template functions

`frontend.TemplateFuncs(r.Context())` from [frontend/context.go](../frontend/context.go) returns every template function: core's, the modules', go-translator's `tl`, `tn`, `ctl` and `ctn` with the site's translations from `Config.Translations`, `importmap`, `importmap_nonce`, `importmap_url`, `importmap_script` and `importmap_style` when `Config.ImportMap` or `Config.Assets` is set, your own from `Config.Funcs`, and `localizer` and `locale` for the page's language. go-partial's HTMX attribute functions, such as `submitAttrs`, belong to the built-in Go templates and are not among them. Functions that take a `context.Context` first come with the request's context filled in, as templates call them:

```go
funcs := frontend.TemplateFuncs(r.Context())

date := funcs["date"].(func(time.Time, ...string) string)
posts := funcs["posts"].(func(string, int) []publishing.Post)
```

An engine that takes a map of Go functions can get the map as it is. Outside `Render`, `TemplateFuncs` returns nil.

`funcs["feedLinks"]`, a `func() template.HTML`, writes the links to the feeds of the page for its head (see [Template functions](template-functions.md#feedlinks)). An engine that writes its own tags gets the same links from `app.Frontend.FeedLinks(r, view.Title)`, each a `frontend.FeedLink` with `Type`, `Title` and `URL`, Atom first. As with `feedLinks`, it is empty on pages without feeds and on error pages, where `view.Status` is 400 or higher, such as a category that does not exist; `app.Frontend.FeedURL(r, "atom")` is `""` there too:

```go
for _, link := range app.Frontend.FeedLinks(r, view.Title) {
    fmt.Fprintf(w, `<link rel="alternate" type="%s" title="%s" href="%s">`,
        html.EscapeString(link.Type), html.EscapeString(link.Title), html.EscapeString(link.URL))
}
```

Set `Config.Translations` to the translator `frontend.Translations` returns, and `Config.Funcs`, as you would for Go templates: the same translations and functions also serve components, e-mail, the labels of share links and profiles, and the names of months and days.

## What the site gives up

Themes, components and e-mail are built on Go templates. With a renderer:

- The site has no themes. The admin's Theme section is hidden, `view.Theme` is always `default` without values, and `theme` gives `""`, logged once as a template mistake (see [Calls that find nothing](templates.md#calls-that-find-nothing)).
- `/theme/` serves no files, and `asset`, `assetScript` and `assetStyle` give `/theme/default/…` addresses that answer 404, logged once as a template mistake; only core's files come from core: `webthings.css`, `focal-points.css` and `partial-errors.css` at `/_/`. With `Config.Development` such a call returns the mistake as its error. Serve CSS, JavaScript and images at `/assets/` with `Config.Assets` or `Config.ImportMap`; both give each file a versioned address (`importmap_url`) and its tag (`importmap_style`, `importmap_script`), and the import map for the head (`importmap`), which maps go-partial's browser clients too; see [Configuration](configuration.md) and [Themes](themes.md#assets).
- Components render the modules' defaults, which a theme cannot replace. They have every template function and the site's translations.
- E-mail uses the modules' default templates, translated with the site's translations.
- `Frontend.Execute` has no templates to render.
- `Config.Development` shows only the text of the error `Render` returns, on go-partial's plain error page, and there is no `debug` function (see [Templates](templates.md#while-you-develop)).
- `App.WriteTemplates` still writes Go templates. They remain useful as a list of each template's data and fields.
- Start checks no templates of the renderer (see [Checking the templates at start](templates.md#checking-the-templates-at-start)), and core's head region is for Go templates: the renderer writes the tags of `<head>` itself, with `feedLinks`, `apiLink` and `structuredData` from `frontend.TemplateFuncs`.
- `App.Translations` reads Go templates only: the modules' defaults, and `Config.Templates` when it is set.
- HTMX requests get whatever the renderer writes; the built-in renderer sends the content template with the theme's regions, among them the tags of the head region, the theme's `regions/head.gohtml` or core's, which replace those of the page left in `<head>`, or a fragment alone (see [HTMX](htmx.md)).

## A skeleton

This renderer draws every template with a Go function, as code generated by another engine would. It compiles against the current interface:

```go
package main

import (
    "bytes"
    "context"
    "fmt"
    "html"
    "io"
    "log"
    "net/http"
    "os"
    "time"

    webthings "github.com/donseba/go-webthings"
    "github.com/donseba/go-webthings/frontend"
    "github.com/donseba/go-webthings/modules/publishing"
)

// Screen writes the HTML of one template.
type Screen func(w io.Writer, r *http.Request, view frontend.View) error

// Screens renders every template with a Go function. The keys are the
// template names: "page", "404", "search" and "<key>/<name>".
type Screens map[string]Screen

// Has reports whether the site has a template.
func (s Screens) Has(name string) bool {
    _, ok := s[name]

    return ok
}

// Render writes the whole page: the layout around the template.
func (s Screens) Render(w http.ResponseWriter, r *http.Request, name string, view frontend.View) error {
    screen, ok := s[name]
    if !ok {
        return fmt.Errorf("no template %q", name)
    }

    // Render into a buffer, so an error leaves no half page.
    var body bytes.Buffer

    if err := screen(&body, r, view); err != nil {
        return err
    }

    w.Header().Set("Content-Type", "text/html; charset=utf-8")

    _, err := fmt.Fprintf(w, "<!doctype html>\n<html lang=\"%s\">\n<title>%s</title>\n%s</html>\n",
        html.EscapeString(view.Locale), html.EscapeString(view.Site.Name), body.Bytes())

    return err
}

func page(w io.Writer, _ *http.Request, view frontend.View) error {
    page, ok := view.Content.(frontend.Page)
    if !ok {
        return fmt.Errorf("page: unexpected data %T", view.Content)
    }

    // Body is HTML already; everything else is escaped.
    _, err := fmt.Fprintf(w, "<main><h1>%s</h1>%s</main>", html.EscapeString(page.Title), page.Body)

    return err
}

func notFound(w io.Writer, _ *http.Request, view frontend.View) error {
    data, _ := view.Content.(frontend.NotFound)

    _, err := fmt.Fprintf(w, "<main><p>Not found. <a href=\"%s\">Home</a></p></main>", html.EscapeString(data.HomeURL))

    return err
}

func newsPost(w io.Writer, r *http.Request, view frontend.View) error {
    post, ok := view.Content.(publishing.Post)
    if !ok {
        return fmt.Errorf("news/post: unexpected data %T", view.Content)
    }

    funcs := frontend.TemplateFuncs(r.Context())
    date := funcs["date"].(func(time.Time, ...string) string)

    _, err := fmt.Fprintf(w, "<article><h1>%s</h1><p>%s</p>%s</article>",
        html.EscapeString(post.Title), html.EscapeString(date(post.Date, "long")), post.Body)

    return err
}

func main() {
    app := webthings.New(webthings.Config{
        Name: "My site",
        Renderer: Screens{
            "page":      page,
            "404":       notFound,
            "news/post": newsPost,
        },
        Assets: os.DirFS("static"),
    })

    if err := app.Use(publishing.Module("news", "News")); err != nil {
        log.Fatal(err)
    }

    if err := app.ListenAndServe(context.Background(), ":8080"); err != nil {
        log.Fatal(err)
    }
}
```

At start this site reports `news/list` as missing, since the screens have no `news/list`; add a screen for every template the modules render.
