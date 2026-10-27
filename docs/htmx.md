# HTMX

With HTMX, a theme swaps only the part of a page that changes: a link replaces the content while the menu and the title follow, and a form updates only its own box. Every address still answers with a whole page, so the site keeps working without JavaScript.

## How it fits together

The public site answers HTMX requests with the htmx 4 protocol. A theme offers HTMX three kinds of parts:

| Part | Lives in | Its dot | HTMX gets it |
| --- | --- | --- | --- |
| The content | `<main id="content">` around each content template | The content template's data | For a request aimed at `#content`, such as a boosted link |
| A region | `regions/<name>.gohtml`, placed by the layout with `{{slot "<name>"}}` | The page, a [frontend.View](../frontend/view.go) | Out of band, with every response that swaps the content |
| A fragment | `fragments/<name>.gohtml`, placed with `{{requiredSlot "<name>"}}`, `{{slot "<name>"}}` or `{{deferredSlot "<name>"}}` | The page's content, `View.Content` | Alone, for a request aimed at `#<name>`; on the search screen with the regions out of band |

[frontend.Templates](../frontend/templates.go) reads both folders, and [render](../render/fragments.go) decides what a request gets. A theme other than `default/` finds the regions and fragments it lacks in `default/`, and replaces one with a file of the same name. The folders are read once and kept, or for every request with `Config.ReloadTemplates`. The [demo](https://github.com/donseba/go-webthings-demo) uses everything in this guide; the starter's editorial theme uses its navigation, regions, fragments and forms.

A theme writes the `hx-*` attributes itself, or has go-partial write them, as the demo does: `{{noBoost runtime}}` for a link that loads a whole page, `{{submitAttrs runtime …}}` for a form that swaps in its answer, `{{liveAttrs runtime …}}` for a field that asks while the visitor types, and `{{deferredSlot "<name>" "reveal"}}` for a fragment that loads once it scrolls into view (see [Template functions](template-functions.md#htmx-attributes)). Theme templates have go-partial's attribute functions: those whose names end in `Attrs`, `noBoost` and `confirmAttr`. Its functions that write an element of their own, such as `poll` and `reveal`, are not there.

For go-clue to know `slot`, `requiredSlot`, `deferredSlot`, `hasSlot` and the attribute functions such as `noBoost`, list `github.com/donseba/go-partial/exp/slots` and `github.com/donseba/go-partial/exp/interactions` in the `providers` of `.go-clue/config.json`, and `github.com/donseba/go-partial/exp/slots.FuncMap` and `github.com/donseba/go-partial/exp/interactions.AttrsFuncMap` in its `functionMaps`; `github.com/donseba/go-importmap.FuncMap` adds the `importmap` functions (see [Templates](templates.md#the-dot-line-and-editor-support)).

## Loading htmx

Any way of loading htmx 4 works, such as a copy in the theme's assets with `{{assetScript "htmx.min.js" "defer" ""}}`, which writes its `<script>` tag with the file's integrity (see [Template functions](template-functions.md#assetscript-and-assetstyle)).

The demo and the starter vendor it with [go-importmap](https://github.com/donseba/go-importmap) instead: its `importmap` command downloads front-end packages into the site's folder, with their licences and a lock file, and the site embeds them and serves them with an import map, so a module script can `import "htmx"` and the site needs no network at run time. go-webthings depends on go-importmap, so `go mod tidy` adds it to the site's `go.mod`. `importmap.json` lists the packages:

```json
{
  "provider": "cdnjs",
  "packages": [
    {
      "name": "htmx",
      "version": "4.0.0",
      "require": [{"file": "htmx.esm.min.js", "as": "htmx"}],
      "license": "https://cdn.jsdelivr.net/npm/htmx.org@4.0.0/LICENSE"
    }
  ]
}
```

`importmap vendor` downloads them into `assets/vendor/`: `htmx/htmx.esm.min.js`, `htmx/LICENSE` and `importmap.lock.json`, which lists each file with its source and SHA-384 integrity. The demo and the starter run it with `go generate` (their `task vendor`) and embed the folder:

```go
//go:generate go run github.com/donseba/go-importmap/cmd/importmap vendor -config importmap.json -out assets/vendor

//go:embed assets
var assetFiles embed.FS

// importMap is the import map of the vendored packages.
func importMap() (*importmap.ImportMap, error) {
    files, err := fs.Sub(assetFiles, "assets")
    if err != nil {
        return nil, err
    }

    return importmap.FromFS(files, "vendor/"+importmap.LockFile)
}
```

The site needs no copy of go-partial's browser clients. When the site starts, core adds them to its import map under the names scripts import them by, at `/assets/` with a version: `partial-interactions`, which `clientAttrs` needs for `"reloadOnError"` and `"submitConfirm"`, `partial-flash` and `partial-sse`. A name the site maps itself keeps the site's mapping. With `Config.Assets` the clients are among the site's files, and a file of the same name in that folder replaces core's. An import map of the site's own in `Config.ImportMap`, such as this one, gets core's copies, unless a client's file is among its own files, such as a `partial-flash.js` in the folder given to `importmap.FromFS`: that file is used instead. Either way the clients carry their integrity, like the site's other files. A site with neither has no import map.

Commit `assets/vendor/`, so building the site needs no network either. Run `importmap vendor` again after changing `importmap.json`. `FromFS` checks every vendored file against the lock and returns an error when one does not match, so the demo does not start with a changed file; `go run github.com/donseba/go-importmap/cmd/importmap verify -out assets/vendor` checks the same without starting the site, for CI.

`Config.ImportMap` takes the import map:

```go
imports, err := importMap()
if err != nil {
    return err
}

app := webthings.New(webthings.Config{
    Templates: os.DirFS("templates"),
    ImportMap: imports,
})
```

The site serves the embedded files at `/assets/`, with a version from each file's content in its address and an ETag, like a theme's assets (see [Themes](themes.md#assets)), and templates get `{{importmap}}`, `{{importmap_nonce}}`, `{{importmap_url}}`, `{{importmap_script}}` and `{{importmap_style}}` (see [Template functions](template-functions.md#importmap)). `Config.Assets` is served at `/assets/` too: a site that sets both does not start. In the layout's `<head>` the import map comes before the theme's script, and `site.js` starts with `import "htmx";`:

```gotemplate
{{importmap}}
{{assetScript "site.js" "type" "module"}}
```

`{{importmap}}` writes the import map with each file's versioned address and its integrity, the vendored files' and go-partial's clients', so the browser refuses a file whose content is not what the page expects:

```html
<script type="importmap">
{
  "imports": {
    "htmx": "/assets/vendor/htmx/htmx.esm.min.js?v=9481657c08f4d363",
    "partial-flash": "/assets/partial-flash.js?v=60e5df53e40a18e9",
    "partial-interactions": "/assets/partial-interactions.js?v=d8a76b4a43a6496e",
    "partial-sse": "/assets/partial-sse.js?v=a0f0d65652166f47"
  },
  "integrity": {
    "/assets/partial-flash.js?v=60e5df53e40a18e9": "sha384-YOXfU+QKGOm1S7l6qDXMeljvUGkE517dLGoJyACRoi2wEyVWHbaw/qnZUZ2z4JUk",
    "/assets/partial-interactions.js?v=d8a76b4a43a6496e": "sha384-2KdrSkOmSW69IuWFe06CoM5FD1wB6V6JJbV/XyUCaRBGsQsGkCMQpKcrngHBXwYm",
    "/assets/partial-sse.js?v=a0f0d65652166f47": "sha384-oPDWVlIWb0djQk9kQhJlRDZoj/pHABnjUmz7vwjcGt5axvSWK8TUsJxfZ/pRAsTY",
    "/assets/vendor/htmx/htmx.esm.min.js?v=9481657c08f4d363": "sha384-lIFlfAj002Phzxvrea858KMM3/dXelBFF5rq+Oh5lChznUWuVI0mabAEUnsRa/2W"
  }
}
</script>
```

## Navigation that swaps only the content

Every content template wraps its content in `<main id="content">`, as the starting points do. Boosting the body makes links and forms swap only that element; htmx 4 passes an attribute on to the elements inside only with `:inherited`:

```gotemplate
<body hx-boost:inherited="target:#content swap:'outerHTML showTarget:body'">
```

go-partial's `clientAttrs` writes the same attribute, as the demo's layout does. It quotes the swap, so its scroll modifier stays part of it:

```gotemplate
<body{{clientAttrs runtime "inherit" "true" "boost" "on" "target" "#content" "swap" "outerHTML showTarget:body"}}>
```

A request aimed at `#content` gets the content template without the layout, followed by every region out of band, in the order of their file names, such as `editor`, `head`, `languages` and then `menu`. The order is the same on every request, so the same page gives the same bytes and the same `ETag`. The [render](../render/render.go) package sends the regions of a `View`'s `Regions` first, in the order listed, and then those of its `RegionFiles` by file name; core's head region, for a theme without `regions/head.gohtml`, is in `Regions`, so it comes first. A request that needs the whole document, such as one with `hx-select` or htmx's history restore, gets the whole page. Give links that must load a whole page `{{noBoost runtime}}`, which writes `hx-boost="false"`: the language links, since the layout's `lang` changes with them, and links to the admin:

```gotemplate
{{with adminURL "admin.dashboard"}}<a href="{{.}}"{{noBoost runtime}}>Admin</a>{{end}}
```

Responses vary by `HX-Request`, `HX-Boosted`, `HX-Target`, `HX-Request-Type` and `HX-History-Restore-Request`, and by go-partial's own `X-Select` and `X-Action`, so a cache keeps whole pages and parts apart; a navigation that gets the menu region out of band also varies by `HX-Current-URL` (see [The menu](#the-menu)). While compression is on (**Site settings › Performance**, on by default), they also vary by `Accept-Encoding`, so a cache keeps the gzipped and the plain version apart. The site's own output cache does the same: it keeps the whole page and the HTMX navigation of one address side by side, and a navigation from another page, with another `HX-Current-URL`, as an answer of its own (see [The output cache](deployment.md#the-output-cache) and [The output cache](module-caching-and-testing.md#the-output-cache) for modules).

When a template fails, an HTMX request gets status 500, as a whole page does: the theme's `500.gohtml` alone, or go-partial's plain error page without one, in the page's language, or with `Config.Development` the "Template render error" box alone; the page and the box are never cached and come without the layout or regions (see [Templates](templates.md#when-something-goes-wrong)). htmx 4 swaps it into the target, unless the theme stops swaps for server errors as the demo does (see [Forms with HTMX](#forms-with-htmx)).

## Regions

A region is a part of the layout that changes from page to page, such as the menu with the current page marked. Its dot is the page, like the layout's, and its outer element carries the region's name as `id` and `{{oobAttr}}`:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/frontend.View */}}
<nav class="menu" id="menu" {{oobAttr}}>
    {{range .Menu}}
        <a id="{{.ElementID}}" href="{{.URL}}" {{if .Active}}aria-current="page"{{end}}>{{.Title}}</a>
    {{end}}
</nav>
```

The layout places it with `{{slot "menu"}}`, and `{{hasSlot "menu"}}` reports whether the theme has it. On a whole page `{{oobAttr}}` writes nothing; along with the content of a navigation it writes `hx-swap-oob="true"`, and HTMX swaps the element with the same ID.

Write the outer element on every page, empty when there is nothing to show: HTMX can only swap an element that is already on the page. The demo's `regions/editor.gohtml` is always a `<div id="editor" {{oobAttr}}>`, with the edit link inside only for editors.

### The menu

Each item of `.Menu` has an `ElementID`, `menu-` and the page's ID. When the items of `regions/menu.gohtml` carry it as their `id`, as above, a navigation gets only the items whose `Active` changes, each with `hx-swap-oob="true"`, instead of the whole menu. Going from `/en/about/team` to `/en/work`, with `regions/title.gohtml`, a region that holds only the `<title>` (see [The head region](#the-head-region)), which comes after the menu by its file name:

```html
<main id="content">…</main><a hx-swap-oob="true" id="menu-01a1…" href="/en/about" >About</a><a hx-swap-oob="true" id="menu-01a1…" href="/en/work" aria-current="page">Work</a><title>Work · My site</title>
```

The items of the pages below, `.Children`, change too, such as Team here; a region that shows them with their `ElementID` gets them along, and an item the region does not show is left out.

The items that change follow from the page the visitor comes from, which htmx sends as `HX-Current-URL`, so such a response varies by `HX-Current-URL`. That address counts as this site's also behind a trusted proxy that names the visitor's host in `X-Forwarded-Host` (`Config.TrustedProxies`, go-router's `middleware.SameOrigin`). The whole menu goes, as on any other request, when the items have no `ElementID`, when the request has no `HX-Current-URL`, and when that address is of another site, of another language or of no page, such as one the not-found page answered. Pages added, moved or renamed show in the menu with the next whole page, so a region with these IDs shows nothing else that changes from page to page.

The menu is chosen this way, and the response varies by `HX-Current-URL`, only when the menu region goes out of band: for a navigation, or a fragment of the search screen (see [Fragments](#fragments)), to a theme with `regions/menu.gohtml`, not for a fragment alone or a request for the whole document (`HX-Request-Type: full`).

### The head region

The tags of the page in `<head>` change from page to page too: the `<title>`, the description, the canonical and `hreflang` links, the feed links, the link to the content API, the structured data and the tags for sharing. A region named `head` holds them, and the layout places it in its `<head>` with `{{slot "head"}}`. A theme without `regions/head.gohtml` gets core's, with all of these tags (see [The head region](themes.md#the-head-region) in Themes). A theme's own `regions/head.gohtml` adds to core's tags by calling them first:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/frontend.View */}}
{{template "webthings/head-tags.gohtml" .}}
<meta name="theme-color" content="#336699">
```

This region needs no `id` and no `{{oobAttr}}`. `<head>` has no element that could wrap it, and the number of its tags changes from page to page, such as the feed links a news page has and a content page does not. On a whole page the region's tags sit between two empty `<template>` markers, with the `<title>` just before them:

```html
<title>News · My site</title><template data-partial-head="head"></template>
<link rel="canonical" href="https://example.com/en/news">
…
<template data-partial-head-end="head"></template>
```

Along with the content of a navigation, the region sends its `<title>` and two out-of-band swaps: one deletes the tags between the markers, the other adds the tags of the new page before the end marker. The tags of the page left, such as its feed links, alternates or description, are removed rather than kept, and none is doubled. So is its `<script type="application/ld+json">` from `{{structuredData}}`: after a navigation from a post to the home page, `<head>` holds one such script, without the post's `Article`. htmx sets the document's title from the `<title>`. This works with htmx 4 without the head-support extension. Static tags, such as `<meta charset>`, stylesheets and scripts, stay in the layout outside the region and are never touched. The whole region goes with every navigation and adds nothing to the response's `Vary` header. go-partial's `exp/head` does the work (see [Templates](templates.md#the-layout)).

A Back or Forward in the browser is the exception. htmx 4, which the demo, the starter's editorial theme and the admin load, then asks for the page again, with the header `HX-History-Restore-Request`, and the site answers with the whole page, its head region included. htmx takes only the `<title>` from that answer and leaves the rest of `<head>` as it was, so the head keeps the region's tags of the page left, such as its canonical link and feed links, until the next navigation or a reload. The content and the title are those of the page returned to.

A layout without `{{slot "head"}}` gets no head region from core: it writes these tags itself, and a navigation leaves them as they are. A region with only the `<title>`, such as `regions/title.gohtml` placed with `{{slot "title"}}`, still works: htmx 4 sets the document's title from a `<title>` in a response and leaves the element out of the swap.

## Fragments

A fragment is a part of a page that HTMX can ask for on its own, such as search results that follow the visitor's typing. The demo's `fragments/results.gohtml` is a `<div id="results">` with the results of its `frontend.SearchView`; `search.gohtml` places it with `{{requiredSlot "results"}}`, below a search field that asks for it:

```gotemplate
<input type="search" name="q" value="{{.Query}}" aria-label="Search"
    {{liveAttrs runtime .URL "target" "#results" "swap" "outerHTML" "replaceURL" "true"}}>
```

`liveAttrs` writes `hx-get` with the address, `hx-target`, `hx-swap`, `hx-trigger="input changed delay:300ms, search"` and, for `"replaceURL" "true"`, `hx-replace-url="true"`, so the address bar shows the query.

A request for a fragment of `search.gohtml`, such as the one aimed at `#results`, gets the fragment without the layout, followed by the regions out of band, as a navigation does. The search screen's title is the query and its language links keep it, so the tab's title, the head region's tags and the language links follow what the visitor types. A request for a fragment of any other page gets only the fragment, without the layout or the regions. Core sets `render.View.FragmentRegions` for the search screen only. On a whole page the fragment renders where it is placed. Its dot is the page's content, `View.Content`, also when the layout places it, so a fragment in the layout names the page with `@model` (see [Templates](templates.md#the-page-anywhere-model)). Its outer element carries the fragment's name as `id`. `{{slot "results"}}` places it as well. With a name the theme has no fragment for, `{{requiredSlot}}` fails the render with `slot "results" is not set`, and `{{slot}}` writes nothing.

Each request of the search field is a search, and a visitor may run 30 a minute. Beyond that the search does not run: the screen answers with status 429 and `SearchView.Throttled` set, without results, and htmx 4 swaps that answer in like any other, so a fragment that shows a message for `.Throttled` keeps the results box in place (see [Templates](templates.md#searchgohtml) and [Search and SEO](search-and-seo.md#the-search-screen)):

```gotemplate
{{/* @dot github.com/donseba/go-webthings/frontend.SearchView */}}
<div id="results">
    {{if .Throttled}}
        <p role="status">Too many searches in a short time. Wait a moment and try again.</p>
    {{else}}
        {{range .Results}}<a href="{{.URL}}">{{.Title}}</a>{{else}}<p>Nothing found.</p>{{end}}
    {{end}}
</div>
```

### Alone or as a navigation

A fragment answers alone, or on the search screen with the regions, only on a page that has what it declares: data of its `@dot` type, and a value for every `@model` among the page and its content. A fragment that declares neither answers on every page. On any other page the request is a navigation: it gets the page's content with `HX-Retarget: #content` and `HX-Reswap: outerHTML`, and for a GET `HX-Push-Url`, so HTMX replaces the content and the address bar follows. A request aimed at an element that is neither the content, a region nor a fragment gets the same. Each answer has the page's status, such as 422 for a page that shows a form's errors, also a fragment or region alone.

The demo's sign-up box of a workshop, `fragments/signup.gohtml`, named after the registrations instance `signup`, declares the workshop and posts with `hx-target="#signup"`; `form_open` adds the attributes of `submitAttrs` to the form tag:

```gotemplate
{{/* @model Event github.com/donseba/go-webthings/modules/events.Event */}}
{{with registration "signup" Event.ID}}
    <section class="booking" id="signup">
        {{form_open .Form (submitAttrs runtime .Action "target" "#signup" "swap" "outerHTML")}}
            …
        {{form_close}}
    </section>
{{end}}
```

When the workshop is full or a field is wrong, the [registrations](../modules/registrations/README.md) module returns to the workshop, which answers with the box alone and its message; for a wrong field, the form shows the visitor's values again with the messages at their fields (see [A post with mistakes](visitor-forms.md#a-post-with-mistakes)). A successful sign-up goes to the visitor's booking page instead. That page has no workshop, so it answers as a navigation: the visitor lands on the booking page and the address bar follows.

### A fragment that loads later

A part that takes long to compute can follow the page. The demo's journal places its fragment `fragments/popular.gohtml` with `deferredSlot`, which writes an empty element that asks for the fragment once it scrolls into view:

```gotemplate
<aside class="sidebar">
    {{deferredSlot "popular" "reveal"}}
</aside>
```

On a whole page this writes `<div id="popular" hx-get="/en/journal" hx-swap="outerHTML" hx-target="this" hx-trigger="revealed" aria-busy="true"></div>`, with the page's address. Without `"reveal"`, `{{deferredSlot "popular"}}` asks with `hx-trigger="load"`, as soon as the page has loaded. The fragment declares no `@dot` or `@model`, so every page answers the request with the fragment alone, and only that request counts the most read pages. A name the theme has no fragment for writes nothing. `revealAttrs` with `"swap" "outerHTML"` writes the same request attributes on an element of the template's own (see [Template functions](template-functions.md#revealattrs)).

A visitor form in such a fragment does not show a post with mistakes again: the request for the page takes the kept post before the fragment's own request comes. Place a form with `{{slot}}` or `{{requiredSlot}}` instead (see [A post with mistakes](visitor-forms.md#a-post-with-mistakes)).

## Forms with HTMX

Visitor forms post to the module's actions at `/_/<key>/…`, which answer with a 303 back to the page the form is on, the result in the query (see [Visitor forms](visitor-forms.md)). A module's form returns the visitor to the element whose id is the instance key, such as `#remarks`, and its fields' ids start with that key, such as `remarks-name`. With HTMX, the browser follows the redirect with the same `HX-Target`, so a form aimed at its fragment gets the fragment back with the result. The comments under a post, in a fragment named after the instance key, `fragments/remarks.gohtml`:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/publishing.Post */}}
<section id="remarks">
    {{range comments "remarks" .ID}}
        <p><strong>{{.Name}}</strong> {{.HTML}}</p>
    {{end}}

    {{if eq (commentResult "remarks") "sent"}}<p role="status">Thank you for your comment.</p>{{end}}

    {{with commentInput "remarks" .ID}}
        {{form_open . (submitAttrs runtime (commentURL "remarks") "target" "#remarks" "swap" "outerHTML")}}
            {{form_field . "subject" nil}}
            {{form_field . "name" nil}}
            {{form_field . "body" nil}}
            <button type="submit">Send</button>
        {{form_close}}
    {{end}}
</section>
```

`commentInput` is the form of the comments on the post. `form_open` writes the form tag with the spam trap and the page to return to, and adds what `submitAttrs` writes: `hx-post` with the address, `hx-target` and `hx-swap`. Posting replaces the comments, now with the new one and the message, and the address stays. Without JavaScript the form posts the usual way and returns to `#remarks`. [Comments](../modules/comments/README.md) lists the fields and results.

A poll has no fixed ID, since a page can show several: its `Anchor`, `<key>-<ID>`, is the id of its element and the place its form returns to. The form picks its own poll out of the page with `hx-select`, which makes htmx ask for the whole page:

```gotemplate
{{form_open .Form (submitAttrs runtime .Action "target" (printf "#%s" .Anchor) "select" (printf "#%s" .Anchor) "swap" "outerHTML")}}
```

A form in a boosted body without an `hx-target` of its own is a navigation: it swaps in the content of the page it returns to. The [forms](../modules/forms/README.md) module's forms post to their own page and work this way, also when they show errors with status 422: htmx 4 swaps every response except 204 and 304. To keep a server error from replacing the page, the demo's layout sets `hx-status:5xx:inherited="swap:none push:false"` on its body, with `"keepOnServerError" "true"` among the `clientAttrs` above. With `"reloadOnError" "true"` as well, a link that gets a 5xx answer, or none, loads as a whole page, and so does a GET form; a POST is never sent twice: the server's error page replaces the page. A visitor action that fails, such as a comment that cannot be saved, answers an HTMX post as it answers a plain one, with status 500 and the whole error page: the theme's `500.gohtml`, or go-partial's plain error page without one, in the language of the form's page (`Frontend.Fail`; see [Templates](templates.md#500gohtml)). `reloadOnError` needs go-partial's browser client `partial-interactions.js`, which core adds to the site's import map (see [Loading htmx](#loading-htmx)); the demo loads it with `import "partial-interactions";` in `site.js`. The starter's editorial theme sets `keepOnServerError` and `reloadOnError` on its body too, and loads the client the same way.

## Under a Content-Security-Policy

htmx 4 works under a Content-Security-Policy without `'unsafe-inline'` and `'unsafe-eval'`, such as the one the site sends itself when an administrator turns it on under **Site settings › Security** (see [A Content-Security-Policy of the site's own](deployment.md#a-content-security-policy-of-the-sites-own)), as long as the theme leaves out what htmx runs as JavaScript, which only `'unsafe-eval'` allows:

- `hx-on*` attributes, such as `hx-on:click`;
- trigger filters, such as `hx-trigger="click[ctrlKey]"`;
- `js:` and `javascript:` values, such as in `hx-vals`, `hx-headers` and `hx-confirm`. Plain JSON in `hx-vals` and `hx-headers`, and a plain question in `hx-confirm`, are fine.

The starting points, the demo and the starter use none of them, and go-partial writes none of them itself. htmx 4 adds the styles of its indicators as a constructable stylesheet rather than a `<style>` element, which such a policy does not block, and go-partial's browser clients are files the site serves at `/assets/`, which `'self'` covers. Three things need care:

- The import map is an inline script. The site's own policy allows it by its hash on every page; a policy sent from elsewhere, such as the proxy, needs that hash too (see [importmap](template-functions.md#importmap)).
- Content that htmx swaps into the page runs under the page's policy, not under that of the response. A `style` attribute in a fragment or region is blocked, and an inline `<style>` or `<script>` in it applies only when the page's policy has its hash. Style swapped content with classes, and keep inline scripts out of fragments. The box that answers an HTMX request whose template fails in development has go-partial's classes only, and `partial-errors.css`, which `{{styles}}` links first, styles them (see [The error box](themes.md#the-error-box)).
- With `reloadOnError`, a failed POST writes the server's error page into the page, under the page's policy. A theme's `500.gohtml` without inline style shows as it is. go-partial's plain error page, which a theme without one shows, styles itself with a `<style>` element. The site's own policy has the hash of that element, go-partial's `errors.StyleHash()` (`ext/errors`), on every page, so the error page is styled; under another policy it is styled only when the page's `style-src` has that hash.

go-partial's README covers its own output under such a policy in [Content-Security-Policy](https://github.com/donseba/go-partial#content-security-policy); see also [Under a Content-Security-Policy](themes.md#under-a-content-security-policy) in Themes.

## A complete example

A news site with the menu, the tags in `<head>` and comments under each post has `regions/menu.gohtml` from [Regions](#regions), core's head region, `fragments/remarks.gohtml` from [Forms with HTMX](#forms-with-htmx), `import "htmx";` in `assets/site.js`, and this layout:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/frontend.View */}}
<!doctype html>
<html lang="{{.Locale}}">
<head>
    <meta charset="utf-8">
    {{slot "head"}}
    {{styles "site.css"}}
    {{importmap}}
    {{assetScript "site.js" "type" "module"}}
</head>
<body hx-boost:inherited="target:#content swap:'outerHTML showTarget:body'">
    <header>
        <a href="{{.Site.HomeURL}}">{{.Site.Name}}</a>
        {{slot "menu"}}
    </header>

    {{content}}
</body>
</html>
```

`news/post.gohtml` places the comments; `page.gohtml` and the other content templates also wrap their content in `<main id="content">`:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/publishing.Post */}}
<main id="content">
    <h1>{{.Title}}</h1>
    {{.Body}}

    {{requiredSlot "remarks"}}
</main>
```

What the browser asks and gets:

```text
GET  /en/news/hello                 the whole page
GET  /en/news/hello                 HX-Target: main#content
                                    HX-Current-URL: https://example.com/en/
     → <main id="content">, then the <title>, the head region's two swaps and, with hx-swap-oob, the menu items that change
POST /_/remarks/post                HX-Target: section#remarks
     → 303 See Other to /en/news/hello?remarks=sent#remarks
GET  /en/news/hello?remarks=sent    HX-Target: section#remarks
     → <section id="remarks"> alone, with the new comment
```
