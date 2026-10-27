# Template functions

A reference of the functions go-webthings gives every public template: links, dates and money, images, theme assets, forms, translations, sharing, search, feeds, the content API, structured data, HTMX attributes and debugging. Module functions are listed by module with a link to their documentation.

## Where functions come from

Templates get functions from seven places. All of them work in the layout and in every content template.

| Source | Functions |
| --- | --- |
| Core, [frontend.Funcs](../frontend/context.go) | `moduleURL`, `searchURL`, `absURL`, `adminURL`, `date`, `time`, `dateRange`, `timeRange`, `weekday`, `money`, `image`, `srcset`, `focalClass`, `objectPosition`, `alt`, `theme`, `asset`, `assetScript`, `assetStyle`, `themeStyle`, `styles`, `form_open`, `form_hidden`, `form_field`, `form_errors`, `form_buttons`, `form_close`, `form_render`, `profiles`, `share`, `shareBar`, `search`, `feedLinks`, `feedURL`, `apiLink`, `structuredData` |
| [go-translator](https://github.com/donseba/go-translator), with the site's translations from [frontend.Translations](../frontend/translations.go) in `Config.Translations` | `tl`, `tn`, `ctl`, `ctn` |
| [go-partial](https://github.com/donseba/go-partial), which renders the pages | `content`, `locale`, `localizer`, `request`, `url`, `urlIs`, `urlStarts`, `urlContains`, `joinPath`, `urlPath`, `basePath`, `partial`, `runtime`, `ctx`, `oob`, `oobAttr`, `oobSwap`, `slot`, `hasSlot`, `requiredSlot`, `deferredSlot`, `targetHeader`, `targetValue`, `targetIs` |
| go-partial's `exp/interactions`, for HTMX attributes | `noBoost`, `submitAttrs`, `liveAttrs`, `revealAttrs`, `clientAttrs`, `confirmAttr`, `asyncAttrs`, `pollAttrs`, `onAttrs`, `streamAttrs`, `refreshAttrs` |
| Core's renderer, [render](../render/render.go) | `debug` |
| [go-importmap](https://github.com/donseba/go-importmap), with `Config.ImportMap` or `Config.Assets` | `importmap`, `importmap_nonce`, `importmap_url`, `importmap_script`, `importmap_style` |
| The modules in `app.Use`, and `Config.Funcs` | See [Module functions](#module-functions) and [Your own functions](templates.md#your-own-functions) |

A function in `Config.Funcs` replaces a core, module, translation or import map function with the same name, and core and module functions replace go-partial's interaction functions of the same name. A site or module function with a name from the go-partial row, or a name starting with `_`, stops the start with `render.ErrReserved`.

Go's own template functions, such as `and`, `or`, `not`, `eq`, `len`, `index`, `printf` and `slice`, work as usual; see the [text/template documentation](https://pkg.go.dev/text/template#hdr-Functions).

The signatures below are as templates call them. Many functions take a `context.Context` first in Go; they get it from the request being rendered, and templates leave it out.

## Links and addresses

### moduleURL

```go
func moduleURL(key string) string
```

The address of the page a module instance is placed on, in the current language, or `""` when it is on no page.

```gotemplate
{{with moduleURL "news"}}<a href="{{.}}">All news</a>{{end}}
```

### searchURL

```go
func searchURL() string
```

The address of the search screen in the current language, such as `/en/search`, for a search form. See [Search](#search).

```gotemplate
<form action="{{searchURL}}" method="get" role="search">
    <input type="search" name="q" aria-label="Search">
</form>
```

### absURL

```go
func absURL(link string) string
```

A link made absolute with the site's address: `Config.BaseURL`, or the address of the request. A relative path is read from the current page, and a path keeps its query and fragment: `""` is the current page without its query, `"#top"` the current page with `#top`, and `"?page=2"` the current page with that query. A link with a scheme or a host, such as `https://…`, `mailto:…` or `//cdn.example.com/…`, stays as it is. Use it for canonical and `hreflang` links and `og:image`.

```gotemplate
<link rel="canonical" href="{{absURL .Path}}">
{{with or .Image .Site.SocialImageURL}}<meta property="og:image" content="{{absURL .}}">{{end}}
```

### adminURL

```go
func adminURL(name string, args ...any) string
```

The address of a named admin route, for an editor who is signed in to the admin; `""` for visitors and for a route the admin does not have. `args` are the route's parameters as name and value pairs. The admin's own pages are named like `admin.dashboard`, and a section's like `admin.<key>.index` and `admin.<key>.edit` with `"id"`, such as `admin.pages.edit` or `admin.news.edit`. The address follows `Config.AdminPath`, and visitors never get it, so the admin's path stays off public pages. The admin checks rights itself: a link to a screen the editor may not open answers 403. For the admin screen of what the page shows, the layout's `.Editor` is simpler (see [Editing and publishing](editing.md#the-edit-link-on-the-site)).

```gotemplate
{{with adminURL "admin.dashboard"}}<a href="{{.}}">Admin</a>{{end}}
{{with adminURL "admin.news.edit" "id" .ID}}<a href="{{.}}">Edit</a>{{end}}
```

### request

```go
func request() *http.Request
```

The request being rendered. Its path is the page's whole path, also on module screens, such as `/en/news/hello`.

```gotemplate
{{if eq (request.URL.Query.Get "view") "map"}}…{{end}}
```

### url

```go
func url() *url.URL
```

The URL of the request being rendered. Its path is the page's whole path, like `.Path` in the layout, also on module screens.

```gotemplate
{{with url.Query.Get "q"}}<p>You searched for {{.}}</p>{{end}}
```

### urlIs, urlStarts and urlContains

```go
func urlIs(path string) bool
func urlStarts(prefix string) bool
func urlContains(text string) bool
```

Compare the request's path: `urlIs` ignores leading and trailing slashes, `urlStarts` and `urlContains` compare as written. Like `url`, they see the page's whole path, also on module screens.

```gotemplate
<a href="/en/contact" {{if urlIs "/en/contact"}}aria-current="page"{{end}}>Contact</a>
```

### joinPath and urlPath

```go
func joinPath(parts ...string) string
func urlPath(base string, parts ...string) template.URL
```

Join path parts with slashes and clean the result, as Go's `path.Join` does; `urlPath` returns it as a URL that `html/template` keeps as it is.

```gotemplate
<a href="{{urlPath (moduleURL "news") "archive"}}">Archive</a>
```

### basePath

```go
func basePath() string
```

go-partial's base path; always `""` on a go-webthings site.

## Dates, times and money

The date and money functions write in the page's language. Times stored in UTC, such as when a registration was made, are shown in the site's timezone; times in another zone keep it, such as an event in its own timezone, and calendar dates, such as a post's or a document's date, show the same day in every timezone. English and Dutch are built in. The site's translations replace month and day names and patterns in every language, English and Dutch included, by context (`msgctxt`) and English msgid: `January` … `December` in `month`, `Jan` … `Dec` in `month-short`, `Sunday` … `Saturday` in `day`, `Sun` … `Sat` in `day-short`, `AM` and `PM` in `time`, and the patterns of the styles `short`, `medium`, `long`, `full` and `time` in `date`. What a catalogue does not translate is the built-in name or pattern, or English in a language without built-in ones, so English and Dutch catalogues need not contain these texts (`frontend.BuiltInCalendar`). `frontend.CalendarTexts()` lists them, `task translations` adds them to the POT file, and `task translations:check` reports them missing only in languages without built-in ones (see [Languages](languages.md#dates-times-and-money)). A German `de.po`:

```text
msgctxt "month"
msgid "October"
msgstr "Oktober"

msgctxt "day"
msgid "Sunday"
msgstr "Sonntag"

msgctxt "date"
msgid "long"
msgstr "d. MMMM y"
```

With it, `{{date .Start}}` writes `25. Oktober 2026` on a German page.

### date

```go
func date(t time.Time, style ...string) string
```

The day of `t` in a style (`short`, `medium`, `long` or `full`; `long` when left out) or in a [CLDR date pattern](https://unicode.org/reports/tr35/tr35-dates.html#Date_Field_Symbol_Table). A zero time gives `""`.

```gotemplate
{{date .Date}}                25 October 2026
{{date .Date "short"}}        25/10/2026, in Dutch 25-10-2026
{{date .Date "medium"}}       25 Oct 2026
{{date .Start "full"}}        Sunday 25 October 2026, in Dutch zondag 25 oktober 2026
{{date .Start "EEE d MMM"}}   Sun 25 Oct
```

Patterns use `d dd`, `M MM MMM MMMM`, `y yy`, `E EEEE`, `H HH`, `h hh`, `mm` and `a`; text in single quotes is copied.

### time

```go
func time(t time.Time) string
```

The time of day of `t`, such as `14:00`, or `2:00 PM` in `en-US`.

```gotemplate
<time datetime="{{.Start.Format "2006-01-02T15:04"}}">{{time .Start}}</time>
```

### dateRange

```go
func dateRange(start, end time.Time, style ...string) string
```

The days from `start` to `end`, leaving out what they share in the built-in languages, for a style whose pattern the site's translations do not replace. An end on the same day, or a zero end, gives one date.

```gotemplate
{{dateRange .Start .End "long"}}   25–27 October 2026
```

### timeRange

```go
func timeRange(start, end time.Time) string
```

The times of day from `start` to `end`; a zero end gives only the start.

```gotemplate
{{date .Start "full"}} · {{timeRange .Start .End}}   Sunday 25 October 2026 · 14:00–17:00
```

### weekday

```go
func weekday(day time.Weekday, style ...string) string
```

The name of a day of the week, or with `"short"` its abbreviation: `Monday` and `Mon`, in Dutch `maandag` and `ma`.

```gotemplate
{{weekday .Start.Weekday "short"}}
```

### money

```go
func money(amount any, currency string) (string, error)
```

An amount in minor units (cents) of an ISO 4217 currency, with the language's separators and symbol position. The amount must be an integer; anything else stops the template with an error.

```gotemplate
{{money .PriceCents "EUR"}}   €45.00, in Dutch € 45,00, in German 45,00 €
```

## Images

`image` and `srcset` make resized copies of images from the media library, `focalClass` and `objectPosition` let the browser crop them around their focal point, and `alt` gives their alt text. Module views already give each image as a copy at most 1600 pixels wide, with its srcset and size, such as a post's `.ImageURL`, `.ImageSrcset`, `.ImageWidth` and `.ImageHeight`; these functions are for other sizes. [Media and images](media.md#image-sizes-in-templates) explains the sizes, formats and how the copies are kept.

### image

```go
func image(ref string, spec string) string
```

The address of a resized copy of an image. `ref` is an image's URL as module views give it, the copy such as `.ImageURL` or the original such as `.ImageOriginalURL`, the URL of any other copy, or a media ID; the copy is always made from the original. `spec` is a size, optionally followed by `crop` and a format (`webp`, the default, `jpg` or `png`). A crop is cut around the image's [focal point](media.md#the-focal-point), which editors place in the media library. Files that are not images, and unknown specs, give `ref` as it is, or the file's URL for a media ID.

```gotemplate
<img src="{{image .ImageURL "800x600"}}" alt="">        fits inside 800 × 600
<img src="{{image .ImageURL "800"}}" alt="">            800 wide
<img src="{{image .ImageURL "400x400 crop"}}" alt="">   exactly 400 × 400, around the focal point
<img src="{{image .ImageURL "1200 jpg"}}" alt="">       1200 wide, as JPEG
```

### srcset

```go
func srcset(ref string, widths ...int) string
```

WebP copies of an image in the given widths, for a `srcset` attribute. `ref` is as for `image`. Widths beyond the image's own width are left out, and the image's own width is offered instead. The copies are never cropped; see [focalClass](#focalclass). Gives `""` for files that are not images.

Module views give the srcset of their own copy, such as `.ImageSrcset`, which goes up to 1600 pixels. `srcset` offers other widths, such as larger copies for an image as wide as the screen:

```gotemplate
<img src="{{.ImageURL}}" srcset="{{srcset .ImageURL 800 1600 2400}}" sizes="100vw" alt="">
```

### focalClass

```go
func focalClass(ref string, box ...string) string
```

The class of core's `focal-points.css` that keeps an image's focal point in view when the browser crops it with `object-fit: cover`: `fp-` with the horizontal and the vertical position in percent, each rounded to a multiple of 5, such as `fp-35-60`. It is the value of [objectPosition](#objectposition) as a class, with the same `ref` and `box`: with the box's proportions, as `"3:2"` or `"720x480"`, the browser shows about the same part as `{{image . "720x480 crop"}}`. Files that are not images in the library give `fp-50-50`, the centre.

The class needs `focal-points.css` in the layout, such as with `{{styles "site.css" "focal-points.css"}}`, and `object-fit: cover` from the theme's stylesheet (see [Focal points](themes.md#focal-points)). It needs no `style` attribute, so it works under a Content-Security-Policy without `'unsafe-inline'`. Use it with `srcset`, whose copies are not cropped:

```gotemplate
<img src="{{image .ImageURL "720x480 crop"}}"
     srcset="{{srcset .ImageURL 360 720 1080}}"
     sizes="(max-width: 700px) 100vw, 360px"
     class="photo {{focalClass .ImageURL "720x480"}}"
     alt="">
```

```css
.photo { aspect-ratio: 3 / 2; object-fit: cover; }
```

For a photo of 2000 × 1000 pixels with its focal point 30% from the left and 60% from the top, this writes `class="photo fp-0-50"`: the 3:2 box shows the photo's whole height, and its left edge. Without proportions, `{{focalClass .ImageURL}}` gives `fp-30-60`.

### objectPosition

```go
func objectPosition(ref string, box ...string) string
```

A CSS `object-position` value for an image the browser crops with `object-fit: cover`, so the crop keeps the image's focal point in view. With the box's proportions, as `"4:3"` or `"800x600"`, the browser shows the same part as `{{image . "800x600 crop"}}`. Without them it gives the focal point itself, such as `25% 75%`, which stays in view in a box of any shape but is not centred. `ref` is as for `image`: a copy's URL, such as `.ImageURL`, gives the focal point of its image. Files that are not images in the library give `50% 50%`.

The value goes in a `style` attribute:

```gotemplate
<img src="{{image .ImageURL "720x480 crop"}}"
     srcset="{{srcset .ImageURL 360 720 1080}}"
     sizes="(max-width: 700px) 100vw, 360px"
     style="aspect-ratio: 3 / 2; object-fit: cover; object-position: {{objectPosition .ImageURL "720x480"}}"
     alt="">
```

A Content-Security-Policy without `'unsafe-inline'` in `style-src` blocks a `style` attribute, so `objectPosition` is for themes without such a policy; [focalClass](#focalclass) gives the same position as a class. See [A Content-Security-Policy of the site's own](deployment.md#a-content-security-policy-of-the-sites-own).

### alt

```go
func alt(ref string) string
```

The alt text editors wrote for an image in the media library, in the page's language, or `""` when there is none. `ref` is as for `image`, such as the copy a module view gives.

```gotemplate
<img src="{{.ImageURL}}" width="{{.ImageWidth}}" height="{{.ImageHeight}}" alt="{{alt .ImageURL}}">
```

## Theme and assets

### theme

```go
func theme(name string) (string, error)
```

A setting of the active theme from its `theme.json`, as editors set it in the admin, or its default. A `number` setting gives the number without its unit, such as `42`. In the layout `{{.Theme.Values.accent}}` is the same. A setting the theme does not declare gives `""` and is logged once with the ones it has, such as `theme: no setting "acent" in theme default (known: accent)`; with `Config.Development` the page fails with that message (see [Calls that find nothing](templates.md#calls-that-find-nothing)). See [Themes](themes.md#settings).

```gotemplate
<body data-headings="{{theme "headings"}}">
```

### asset

```go
func asset(file string) (string, error)
```

The address of a file in the theme's `assets/` folder, with a version from the file's content, such as `/theme/default/site.css?v=9b2ca0fe143be1a4`, so the address changes when the file does. `webthings.css`, `focal-points.css` and `partial-errors.css` come from core when the theme has no file of that name. A file that neither the theme nor core has gives its address without a version, such as `/theme/default/logo.svg`, and is logged once with the closest names there are; with `Config.Development` the page fails with that message (see [Calls that find nothing](templates.md#calls-that-find-nothing)). For a stylesheet or script, [assetStyle and assetScript](#assetscript-and-assetstyle) write the whole tag with the file's integrity. See [Themes](themes.md#assets).

```gotemplate
<link rel="icon" href="{{asset "favicon.svg"}}" type="image/svg+xml">
<img src="{{asset "img/logo.svg"}}" alt="">
```

### assetScript and assetStyle

```go
func assetScript(file string, attrs ...string) (template.HTML, error)
func assetStyle(file string, attrs ...string) (template.HTML, error)
```

`assetScript` writes a `<script>` and `assetStyle` a `<link rel="stylesheet">` for a file of the theme's `assets/` folder, at the address `asset` gives, with the file's Subresource Integrity and `crossorigin="anonymous"`, so the browser refuses a file whose content is not what the page expects. `attrs` are more attributes as name and value pairs; an empty value writes the name alone, such as `defer`. `webthings.css`, `focal-points.css` and `partial-errors.css`, go-partial's stylesheet of the box that takes the place of a part of the page that failed (see [The error box](themes.md#the-error-box)), come from core, as with `asset`.

```gotemplate
{{assetStyle "webthings.css"}}
{{assetStyle "partial-errors.css"}}
{{assetStyle "site.css"}}
{{assetStyle "print.css" "media" "print"}}
{{assetScript "site.js" "type" "module"}}
```

This writes:

```html
<link rel="stylesheet" href="/_/webthings.css?v=1490831917b655ed" integrity="sha384-FJCDGRe2Ve0xKnzX94IslGPK/7R+/RPOOqYddXKEC6t0t5sqBSjqEDR3xxlUwxAh" crossorigin="anonymous">
<link rel="stylesheet" href="/_/partial-errors.css?v=dbaec249af3f1089" integrity="sha384-267CSa8/EImeIQQLR3XAW5pnK8j7PFJDgO+m1+qHE/V5s/kmZZL2w7DZDOHMixq+" crossorigin="anonymous">
<link rel="stylesheet" href="/theme/default/site.css?v=9b2ca0fe143be1a4" integrity="sha384-myyg/hQ74aSgjBBvVME/QXAXEkT4Y9dHbVQ5C0lIyGpldvNLJV2IWc5ElXbqLi06" crossorigin="anonymous">
<link rel="stylesheet" href="/theme/default/print.css?v=3308f99567027e50" integrity="sha384-Mwj5lWcCflBkQKUBW+SXpHV5nUa7KmgCgLQbh2/9mXSvcECXSfO4988ugU0PGRw0" crossorigin="anonymous" media="print">
<script src="/theme/default/site.js?v=7e94ac7e9f75c0e9" integrity="sha384-fpSsfp91wOloMeTRBzPOrwMSDH0D6SBhTnFTWERkBorjp/6R2ze6PZCreuu3Lr6S" crossorigin="anonymous" type="module"></script>
```

With `Config.ReloadTemplates` the theme's files may change while the site runs, so their tags have no integrity, only the address; core's `webthings.css`, `focal-points.css` and `partial-errors.css` do not change and keep it. A file that neither the theme nor core has gets neither version nor integrity, and is logged once as a template mistake with the closest names there are, such as `assetStyle: no file "sitee.css" in the assets of theme default (closest: site.css, webthings.css, focal-points.css)`; with `Config.Development` the page fails with that message (see [Calls that find nothing](templates.md#calls-that-find-nothing)). A `crossorigin` in `attrs` replaces the default one. The address (`src`, or `rel` and `href`) and `integrity` cannot be set in `attrs`; trying, or an odd number of `attrs`, fails the render.

### themeStyle

```go
func themeStyle(attrs ...string) (template.HTML, error)
```

The `<link rel="stylesheet">` of the values stylesheet of the theme being rendered, `/theme/<theme>/values.css`, with its version, its Subresource Integrity and `crossorigin="anonymous"`. Core makes the stylesheet from the theme's `color`, `select` and `number` settings with `css` in `theme.json`: each sets its custom property to the setting's value, a number with its unit, such as `42rem`, so the page needs no `style` attribute for them. A theme without such settings gets nothing. A preview in the admin links the values of the theme it shows. `attrs` are more attributes, as for `assetStyle`. Put it after the theme's own stylesheets, whose fallbacks its properties replace; [styles](#styles) links it last by itself. See [The values stylesheet](themes.md#the-values-stylesheet).

```gotemplate
{{assetStyle "site.css"}}
{{themeStyle}}
```

With `{"name": "accent", "label": "Accent colour", "type": "color", "default": "#b5562f", "css": "--accent"}` as the theme's only such setting, while no editor chose another colour, `{{themeStyle}}` writes:

```html
<link rel="stylesheet" href="/theme/default/values.css?v=d00cf169d95b4358" integrity="sha384-0AzxadlbQ1ihbiw8IrDYoOEMNd5euEqG0fwxFnEecsLlTSf4ySWQZt0kpauEw/iI" crossorigin="anonymous">
```

and the stylesheet is:

```css
:root {
    --accent: #b5562f;
}
```

### styles

```go
func styles(files ...string) (template.HTML, error)
```

The stylesheet links of the layout's `<head>`, in the order they must have: go-partial's `partial-errors.css` first, which styles the box that takes the place of a part of the page that failed (see [The error box](themes.md#the-error-box)); then `files`, in the order given, each linked as [assetStyle](#assetscript-and-assetstyle) links it, from the theme's `assets/` or, when the theme has no file of that name, from core, such as `webthings.css` and `focal-points.css`; last the theme's values stylesheet, as [themeStyle](#themestyle) links it, whose custom properties replace the fallbacks of the stylesheets before it. A file given twice, or `partial-errors.css` given, is linked once. Every link has its version and Subresource Integrity, as with `assetStyle`, and none is a `<style>` element, so the page works under a Content-Security-Policy without `'unsafe-inline'`. A file that neither the theme nor core has is a template mistake, as for `assetStyle`.

```gotemplate
{{styles "webthings.css" "site.css"}}
```

With the theme's `assets/site.css` and the setting of [themeStyle](#themestyle) above, this writes:

```html
<link rel="stylesheet" href="/_/partial-errors.css?v=dbaec249af3f1089" integrity="sha384-267CSa8/EImeIQQLR3XAW5pnK8j7PFJDgO+m1+qHE/V5s/kmZZL2w7DZDOHMixq+" crossorigin="anonymous">
<link rel="stylesheet" href="/_/webthings.css?v=1490831917b655ed" integrity="sha384-FJCDGRe2Ve0xKnzX94IslGPK/7R+/RPOOqYddXKEC6t0t5sqBSjqEDR3xxlUwxAh" crossorigin="anonymous">
<link rel="stylesheet" href="/theme/default/site.css?v=16abe7e5ac5a49fd" integrity="sha384-Fqvn5axaSf3U7wTN3et5Jw2dbuNKYHeVtkP0rNIwSkeqN2zpT/WT3q2vIPKFoa/L" crossorigin="anonymous">
<link rel="stylesheet" href="/theme/default/values.css?v=d00cf169d95b4358" integrity="sha384-0AzxadlbQ1ihbiw8IrDYoOEMNd5euEqG0fwxFnEecsLlTSf4ySWQZt0kpauEw/iI" crossorigin="anonymous">
```

The layout `task templates -- site` writes uses it, and so do the starter's and the demo's.

### importmap

```go
func importmap() (template.HTML, error)
func importmap_nonce(nonce string) (template.HTML, error)
func importmap_url(file string) string
func importmap_script(file string, attrs ...string) (template.HTML, error)
func importmap_style(file string, attrs ...string) (template.HTML, error)
```

go-importmap's functions, only with `Config.ImportMap` or `Config.Assets`. `importmap` writes the import map for the layout's `<head>`: a `<script type="importmap">` that names each package's file by its versioned address, with its integrity. Core adds go-partial's browser clients to it, `partial-flash`, `partial-sse` and `partial-interactions`, with their integrity, unless the site maps such a name itself; a client's file among the site's own files, such as a `partial-flash.js`, is used instead of core's copy. `importmap_url` is the versioned address of one of its files, by the file's path in the folder given to `importmap.FromFS`, such as `/assets/vendor/htmx/htmx.esm.min.js?v=9481657c08f4d363` for `vendor/htmx/htmx.esm.min.js`. With `Config.Assets`, `importmap_url` is the versioned address of a file in that folder, such as `/assets/site.css?v=9b2ca0fe143be1a4` for `site.css`, and `importmap` maps only go-partial's clients (see [Themes](themes.md#assets)). See [HTMX](htmx.md#loading-htmx).

```gotemplate
{{importmap}}
<link rel="modulepreload" href="{{importmap_url "vendor/htmx/htmx.esm.min.js"}}">
```

`importmap_style` writes a `<link rel="stylesheet">` and `importmap_script` a `<script>` for a file at its versioned address, with `attrs` as name and value pairs, for a file the page loads itself rather than through the import map. Both carry the file's integrity and `crossorigin="anonymous"`, so the browser refuses a file whose content is not what the page expects. Files that may change while the site runs get no integrity, here or in the import map: those of `Config.Assets` with `Config.ReloadTemplates`, and those of a `Config.ImportMap` made with go-importmap's `assets.Reload`; the import map then has no integrity at all, not for go-partial's clients either. The files of `importmap.FromFS` do not change and keep it, also with `ReloadTemplates`.

```gotemplate
{{importmap_style "site.css"}}
{{importmap_script "site.js" "type" "module"}}
```

With `Config.Assets` this writes:

```html
<link rel="stylesheet" href="/assets/site.css?v=9b2ca0fe143be1a4" integrity="sha384-myyg/hQ74aSgjBBvVME/QXAXEkT4Y9dHbVQ5C0lIyGpldvNLJV2IWc5ElXbqLi06" crossorigin="anonymous">
<script src="/assets/site.js?v=beecfec8eef56dc6" integrity="sha384-vuz+yO71bcb30P4dMUNzy6/D2y+6d/n0KcOnt5clJtTBxEDoKAqGay0stFlC8Dpr" crossorigin="anonymous" type="module"></script>
```

The import map is an inline script, so a Content-Security-Policy without `'unsafe-inline'` in `script-src` blocks it unless the policy allows it by its hash or by a nonce. The site's own policy, report only or enforced under **Site settings › Security**, has the hash of the page's import map, so the site needs nothing more: `{{importmap}}` writes exactly the map that was hashed, also while the map's files reload, and a page the output cache serves again carries the hash it was made with. Do not change the map's text after it is rendered. A policy sent from elsewhere, such as the proxy or a handler around the app, needs the hash too. go-importmap's `ImportMap.ScriptHash()` gives the hash of what `{{importmap}}` writes, with its quotes, such as `'sha256-…'`. Core gives the map in `Config.ImportMap` its addresses and go-partial's clients when the site starts, so take the hash after `App.Start`. The hash changes only when the map does, so it suits pages the output cache keeps. While the map's files reload (`assets.Reload`), `RenderWithHash` gives the import map and its hash from the same moment. `importmap_nonce` writes what `importmap` writes with a `nonce` attribute, such as `<script type="importmap" nonce="…">` (go-importmap's `WithNonce`), and `importmap_script` takes `"nonce"` among its `attrs`. A nonce must differ for every response, so it suits only a site that sends its own policy with nonces, on pages that are never cached; core's policy has no nonces. See go-importmap's [Content-Security-Policy](https://github.com/donseba/go-importmap#content-security-policy) and [A Content-Security-Policy of the site's own](deployment.md#a-content-security-policy-of-the-sites-own).

## Forms

Visitor forms post to a module's action, which returns the visitor to the form's page with a result (see [Visitor forms](visitor-forms.md)). The `form_*` functions are [go-form](https://github.com/donseba/go-form)'s, for the forms modules offer: the `Form` of a module's view, such as the comments' `ListView`, is a go-form `form.RenderModel` of the struct the module's action reads, and the functions render its fields with the limits of its tags, such as `required` and `maxlength`, in the page's language. A module makes it with `frontend.VisitorForm`: the form posts to the module's action with a spam trap and the page to return to, the element whose id is the instance key, such as `/en/guestbook#remarks`. The ids of its fields start with that key, such as `remarks-name`, so the forms of two instances on one page keep ids of their own.

### form_field

```go
func form_field(model any, name string, errs form.FieldErrors) (template.HTML, error)
```

One field of a form: its label, its `input`, `select` or `textarea` with the limits of its tags, its description and its errors. `name` is the field's key in the form, the value of its `name` tag, such as `"name"` for the comments' `Name`; the Go field name fails the render with the names the form has, `form: no field "Name" (fields: subject, parent, name, email, body)`. `errs` are the errors to show at the field. Templates pass `nil` for a module's form: after a post with mistakes, the form itself brings the visitor's values and the messages for its fields (see [A post with mistakes](visitor-forms.md#a-post-with-mistakes)). Errors passed in `errs` take the place of those messages.

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/comments.ListView */}}
{{form_open .Form}}
    {{form_field .Form "subject" nil}}
    {{form_field .Form "name" nil}}
    {{form_field .Form "email" nil}}
    {{form_field .Form "body" nil}}
    <button type="submit">{{tl localizer "Send"}}</button>
{{form_close}}
```

Outside a module's view, a module function can give the model, such as `{{form_field (commentInput "remarks" .ID) "name" nil}}` for the comments on a post (see [Comments](../modules/comments/README.md)), or `{{form_open (submitInput "tips")}}` for a proposal (see [Submissions](../modules/submissions/README.md)).

### form_open, form_buttons and form_close

```go
func form_open(model any, attrs ...any) (template.HTML, error)
func form_buttons(model any) (template.HTML, error)
func form_close() (template.HTML, error)
```

The parts of a form around its fields. `form_open` writes the `<form>` tag with the model's action and method, followed by the hidden fields: the spam trap and, for a module's form, the page to return to, a field named `back`. `form_buttons` writes the submit button, and a cancel link when the model has one; `form_close` writes `</form>`. Hidden fields of the model, such as the comments' `subject`, are fields like the others: place them with `form_field`.

```gotemplate
{{form_open .Form}}
    {{form_field .Form "subject" nil}}
    {{form_field .Form "name" nil}}
    {{form_field .Form "body" nil}}
    {{form_buttons .Form}}
{{form_close}}
```

`attrs` are more attributes of the form tag: name and value pairs, or a `template.HTMLAttr` such as the attributes `submitAttrs` writes. A `class` adds to the form theme's class. `action`, `method` and `enctype` come from the model, and giving them fails the render; a pair cannot set an event handler such as `onsubmit`.

```gotemplate
{{form_open .Form "class" "comment-form" (submitAttrs runtime .Action "target" "#remarks" "swap" "outerHTML")}}
```

This writes `<form action="/_/remarks/post" method="POST" class="wt-form comment-form" hx-post="/_/remarks/post" hx-swap="outerHTML" hx-target="#remarks">`, then the hidden fields.

### form_hidden

```go
func form_hidden(model any) (template.HTML, error)
```

Only the hidden fields `form_open` writes after the tag: the spam trap and the page to return to, for a template that writes the `<form>` tag itself.

```gotemplate
<form method="post" action="{{.Action}}" class="comment-form">
    {{form_hidden .Form}}
    {{form_field .Form "subject" nil}}
    {{form_field .Form "name" nil}}
    {{form_field .Form "body" nil}}
    <button type="submit">{{tl localizer "Send"}}</button>
</form>
```

For the comments `remarks` on the home page, `form_hidden` writes:

```html
<input type="text" name="website" value="" tabindex="-1" autocomplete="off" aria-hidden="true" class="form-honeypot">
<input type="hidden" name="back" value="/en/#remarks">
```

The spam trap has the class `form-honeypot` and no `style` attribute; see [The hidden fields](visitor-forms.md#the-hidden-fields).

### form_render

```go
func form_render(model any, errs form.FieldErrors) (template.HTML, error)
```

The whole form: what `form_open` writes, the page to return to included, every field in the order of the struct, hidden ones included, the buttons and `</form>`.

```gotemplate
{{form_render .Form nil}}
```

### form_errors

```go
func form_errors(errs form.FieldErrors, name string) []string
```

The messages `errs` holds for the field `name`, for a field a template writes itself, such as the errors of [Frontend.BindForm](../frontend/forms.go) (`env.Frontend.BindForm`) in the view of a module of the site's own.

```gotemplate
{{range form_errors .Errors "email"}}<p class="wt-form__error">{{.}}</p>{{end}}
```

### Texts and the form theme

Labels, descriptions and placeholders from the tags are translated with the site's translations. go-form's own texts, such as the "(required)" that screen readers announce after the label of a required field, use the site's entry when it has one; otherwise they show in go-form's own catalogue of the page's language, such as Dutch, also without a `.po` file, and in English for a language go-form has no catalogue for. When a theme or a module's default template uses `form_open`, `form_field` or `form_render`, `task translations` also finds the texts of the modules' forms, their option labels and submit texts included (see [Languages](languages.md)).

The fields render with go-form's markup and the classes of `webthings.css`: `wt-form`, `wt-form__field`, `wt-form__label`, `wt-form__input`, `wt-form__help`, `wt-form__error`, `wt-form__fieldset` for a group of radio buttons or checkboxes, `wt-form__options` and `wt-form__option` for radio buttons and checkboxes, `wt-form__row` and `wt-button`, and `form-honeypot` for the spam trap, which `webthings.css` hides without a `style` attribute, so it stays hidden under a Content-Security-Policy without `'unsafe-inline'`. A group of radio buttons (`form:"radios"`) or checkboxes (`form:"multicheckbox"`) is a `fieldset` with `wt-form__field` and `wt-form__fieldset`, its label the `legend`, with `wt-form__label`; its radio buttons or checkboxes are in `wt-form__options`, each in a `wt-form__option`, and a single checkbox is in a `wt-form__option` too. A poll's options are such a group: `{{form_field .Form "option" nil}}` writes them, with the question as their legend (see [Polls](../modules/polls/README.md)). `Config.FormTheme` names another go-form theme, such as one with the classes of the site's own stylesheet, whose `Honeypot` names the class that hides the spam trap; see [Themes](themes.md#forms) and [Configuration](configuration.md#templates).

## Translations

`tl`, `tn`, `ctl` and `ctn` are [go-translator](https://github.com/donseba/go-translator)'s functions. The translator `frontend.Translations` returns, which the site sets as `Config.Translations`, gives them the site's translations; without it they show every text as written. Their first argument is always `localizer`, the page's language. A language without a `.po` file of its own uses the one of its base language, or of a regional variant: `nl_BE` uses `nl.po`, and `nl` uses `nl_NL.po`, never another language. [Languages](languages.md) covers the `.po` files.

### tl

```go
func tl(loc translator.Localizer, key string, args ...any) string
```

The translation of `key` in the page's language; `args` fill in `%s` and `%d` as `fmt` does. A text without a translation is shown as written.

```gotemplate
{{tl localizer "Read more"}}
{{tl localizer "Comments (%d)" .Count}}
```

### tn

```go
func tn(loc translator.Localizer, singular, plural string, n int, args ...any) string
```

The singular or plural form for `n`, as the language counts it; pass the number again in `args` to show it.

```gotemplate
{{tn localizer "%d comment" "%d comments" .Count .Count}}
```

### ctl and ctn

```go
func ctl(loc translator.Localizer, context, key string, args ...any) string
func ctn(loc translator.Localizer, context, singular, plural string, n int, args ...any) string
```

`tl` and `tn` with a context, for one English text that needs different translations.

```gotemplate
<button>{{ctl localizer "button" "Close"}}</button>
```

All four show a text without a translation as written. `tn` and `ctn` then take the singular when `n` is 1 and the plural otherwise.

### locale and localizer

```go
func locale() string
func localizer() localization.Localizer
```

`locale` is the page's language, such as `nl`; `localizer` passes it to the translation functions.

```gotemplate
<p lang="{{locale}}">{{tl localizer "Opening hours"}}</p>
```

## Social media

The site's profiles are filled in under Site settings, Social media profiles. Labels are the networks' names, translated with the site's translations: the msgid is the name, such as `Facebook` or `E-mail`. `task translations` finds these names itself.

### profiles

```go
func profiles() []frontend.Profile
```

The site's social media profiles, each with `Network` (such as `instagram`), `Label` and `URL`. The layout has them as `.Site.Profiles` too.

```gotemplate
{{range profiles}}<a href="{{.URL}}" rel="me" class="profile-{{.Network}}">{{.Label}}</a>{{end}}
```

### share

```go
func share(page, title string) []frontend.ShareLink
```

Links that share a page on Facebook, X, LinkedIn, WhatsApp, Telegram, e-mail and Bluesky, each with `Network` (such as `facebook`), `Label` and `URL`. `page` is `""` for the current page, a path such as `/en/news/hello`, or an absolute URL; paths become absolute with `Config.BaseURL` or the address of the request.

```gotemplate
{{range share "" .Title}}<a href="{{.URL}}" class="share-{{.Network}}" target="_blank" rel="noopener">{{.Label}}</a>{{end}}
```

### shareBar

```go
func shareBar(page, title string) (template.HTML, error)
```

The same links rendered with core's share component, `site/share.gohtml` (see [Themes](themes.md#components)).

```gotemplate
{{shareBar "" .Title}}
```

## Search

### search

```go
func search(query string, limit int) []frontend.SearchResult
```

Published pages and module content in the current language that contain every word of `query`, best first, each with `Title`, `Summary`, `URL`, `Module`, `Type` and `Score`. A limit of 0 or less gives at most 20 results, and never more than 100. See [Search and SEO](search-and-seo.md).

```gotemplate
{{range search "glaze" 5}}<a href="{{.URL}}">{{.Title}}</a>{{end}}
```

`searchURL` is under [Links and addresses](#searchurl).

## Feeds

### feedLinks

```go
func feedLinks() template.HTML
```

A `<link rel="alternate">` for each Atom and RSS feed of the page shown, for `<head>`, so browsers and feed readers find them. Core's head tags have it; a theme that writes its own tags puts it in `regions/head.gohtml` or the layout's `<head>`. The feeds are those of the module on the page: those it offers on every page of it, such as a news page's posts, and those it offers on the page shown, such as a category's posts on the category's page. Each feed gets an Atom link and then an RSS link, with the feed's title as readers see it, the title and the site's name, translated with the site's translations (the msgid is `%[1]s from %[2]s`), and an absolute address, with `Config.BaseURL` or the address of the request. Pages without feeds, such as content pages, get nothing, and so do error pages, such as the 404 a news page answers for a category that does not exist, and e-mail templates. See [Themes](themes.md#feeds).

```gotemplate
<head>
    {{feedLinks}}
</head>
```

On the page of the category "Ceramics" of a news page, on the site "Atelier", this writes:

```html
<link rel="alternate" type="application/atom+xml" title="News from Atelier" href="https://example.com/en/news/feed.atom">
<link rel="alternate" type="application/rss+xml" title="News from Atelier" href="https://example.com/en/news/feed.rss">
<link rel="alternate" type="application/atom+xml" title="Ceramics from Atelier" href="https://example.com/en/news/category/ceramics/feed.atom">
<link rel="alternate" type="application/rss+xml" title="Ceramics from Atelier" href="https://example.com/en/news/category/ceramics/feed.rss">
```

For a link visitors see, use [feedURL](#feedurl).

### feedURL

```go
func feedURL(format string, name ...string) string
```

The address of a feed of the page shown in `format`, `"atom"` or `"rss"`, for a link visitors see: the feed the module offers on the page shown, such as a category's posts on the category's page, or else the one it offers on every page of it, such as a news page's posts. A `name` picks the feed with that name instead, such as `"feed"` and `"category-feed"`, the names of the publishing module's feeds. The address is a path, such as `/en/news/feed.atom`. Pages without feeds, an unknown format and a name the page has no feed of give `""`, and so do e-mail templates. In Go, `Frontend.FeedURL(r, format, name...)` gives the same. On an error page, such as the 404 a module answers for a category that does not exist, `feedURL` gives `""` too. See [Themes](themes.md#feeds).

```gotemplate
{{with feedURL "atom"}}
    <a href="{{.}}" type="application/atom+xml"{{noBoost runtime}}>Atom</a>
    <a href="{{feedURL "rss"}}" type="application/rss+xml"{{noBoost runtime}}>RSS</a>
{{end}}
```

On a news page this writes:

```html
<a href="/en/news/feed.atom" type="application/atom+xml" hx-boost="false">Atom</a>
<a href="/en/news/feed.rss" type="application/rss+xml" hx-boost="false">RSS</a>
```

and on the page of its category "Ceramics":

```html
<a href="/en/news/category/ceramics/feed.atom" type="application/atom+xml" hx-boost="false">Atom</a>
<a href="/en/news/category/ceramics/feed.rss" type="application/rss+xml" hx-boost="false">RSS</a>
```

A post's page has no feed of its own, so it gets the news page's. `{{feedURL "atom" "feed"}}` gives `/en/news/feed.atom` on all three. The module's README gives the names and addresses of its feeds, for example [Publishing](../modules/publishing/README.md).

## Content API

### apiLink

```go
func apiLink() template.HTML
```

A `<link rel="alternate" type="application/json">` to the page shown in the [content API](api.md), for `<head>`, so apps find its content. Core's head tags have it; a theme that writes its own tags puts it in `regions/head.gohtml` or the layout's `<head>`. A page links to `/api/v1/pages/<id>?locale=<locale>`, and an address below a module's page, such as a post's or a category's, to `/api/v1/pages/resolve?path=<address>`. The address is absolute, with `Config.BaseURL` or the address of the request. It writes nothing while the API is off, on an error page, such as the not-found page or the 404 a module answers for a post that does not exist, and in a preview of a revision, which the API never shows. In Go, `Frontend.APILink(r)` gives the address, or `""` while the API is off and for a request on no page. See [Search and SEO](search-and-seo.md#a-link-to-the-content-api).

```gotemplate
<head>
    {{apiLink}}
</head>
```

On the page `/en/about`, with `Config.BaseURL` set to `https://example.com`, this writes:

```html
<link rel="alternate" type="application/json" href="https://example.com/api/v1/pages/01a116da-5fe0-72a3-9751-056f4b932fa7?locale=en">
```

and on the post `/en/news/open-studio`:

```html
<link rel="alternate" type="application/json" href="https://example.com/api/v1/pages/resolve?path=%2Fen%2Fnews%2Fopen-studio">
```

## Structured data

### structuredData

```go
func structuredData() (template.HTML, error)
```

The page's structured data for search engines, for `<head>`, where core's head tags write it last, or a theme's own tags in `regions/head.gohtml` or the layout's `<head>`: one `<script type="application/ld+json">` with schema.org's `@context` and an `@graph` of the site's `WebSite` and `Organization`, the page's `BreadcrumbList` and `WebPage`, and the items of the module whose content the page shows, such as a post's `Article`, an event's `Event` or the `ItemList` of a news page's posts. Every address is absolute, with `Config.BaseURL` or the address of the request, and empty values are left out. html/template escapes `<`, `>` and `&` in the JSON, so no text in it can end the script. The not-found page, module screens with a status of 400 or more and the theme's `500.gohtml` get nothing, and so do e-mail templates. `500.gohtml` gets nothing for every failing page: a page whose template fails, and a module's handler or a visitor action that fails (`Frontend.Fail`). See [Search and SEO](search-and-seo.md#structured-data).

```gotemplate
<head>
    {{structuredData}}
</head>
```

On the home page of the site "Atelier Noord", with an e-mail address and an Instagram profile in the site settings, this writes:

```html
<script type="application/ld+json">{"@context":"https://schema.org","@graph":[{"@id":"https://example.com/en/#website","@type":"WebSite","inLanguage":"en","name":"Atelier Noord","potentialAction":{"@type":"SearchAction","query-input":"required name=search_term_string","target":{"@type":"EntryPoint","urlTemplate":"https://example.com/en/search?q={search_term_string}"}},"publisher":{"@id":"https://example.com/#organization"},"url":"https://example.com/en/"},{"@id":"https://example.com/#organization","@type":"Organization","email":"hello@example.com","name":"Atelier Noord","sameAs":["https://www.instagram.com/ateliernoord"],"url":"https://example.com/en/"},{"@id":"https://example.com/en/","@type":"WebPage","inLanguage":"en","isPartOf":{"@id":"https://example.com/en/#website"},"name":"Home","url":"https://example.com/en/"}]}</script>
```

The home page has no `BreadcrumbList`, and the `SearchAction` is there because the theme has `search.gohtml`. A module adds its items with `frontend.Content.StructuredData`, and a list page its `ItemList` with `frontend.ItemList(name, urls...)`; see [Structured data](module-feeds-and-links.md#structured-data) for modules.

## Layout and composition

### content

```go
func content() template.HTML
```

The content template, rendered with its data. Used once, in `layout.gohtml`.

```gotemplate
<body>{{content}}</body>
```

### template

Go's own `template` action includes a template from `partials/` with the data you give it. The name is the path with its extension.

```gotemplate
{{range posts "news" 3}}{{template "partials/post-card.gohtml" .}}{{end}}
```

### partial and runtime

```go
func partial(runtime *partial.Runtime, path string, args ...any) template.HTML
func runtime() *partial.Runtime
```

Render a template file of the theme through go-partial, with one value or key and value pairs as its data. Prefer `template`, which go-clue understands better. Each `partial` call renders the file on its own, which costs several times more than `template`, and key and value pairs arrive as an untyped map. For a part that HTMX asks for on its own by its ID, use a fragment of the theme, placed with [requiredSlot](#slot-requiredslot-and-hasslot), instead. The file can use `@model` values like any other template (see [Templates](templates.md#the-page-anywhere-model)).

```gotemplate
{{partial runtime "partials/notice.gohtml" "Text" "Closed on Monday"}}
```

### ctx

```go
func ctx() *partial.RenderContext
```

go-partial's render context. Templates rarely need it.

### oob, oobAttr and oobSwap

```go
func oob() bool
func oobAttr(values ...string) template.HTMLAttr
func oobSwap(swap ...string) template.HTMLAttr
```

go-partial's helpers for HTMX out-of-band swaps, used in the theme's regions. When a region goes along with an HTMX navigation, `oob` is `true` and `oobAttr` writes `hx-swap-oob="true"`, or the value given; on a whole page, and in other templates, `oob` is `false` and `oobAttr` writes nothing. `oobSwap` writes the attribute in every response, with the swap given, such as `hx-swap-oob="beforeend"`, or `hx-swap-oob="true"` without one. See [HTMX](htmx.md#regions).

```gotemplate
<nav id="menu" {{oobAttr}}>…</nav>
```

### slot, requiredSlot and hasSlot

```go
func slot(name string) template.HTML
func requiredSlot(name string) (template.HTML, error)
func hasSlot(name string) bool
```

go-partial's named slots, which hold the theme's regions and fragments: `{{slot "menu"}}` places `regions/menu.gohtml` in the layout, `{{slot "head"}}` places the tags of the page in the layout's `<head>`, `regions/head.gohtml` or core's head region for a theme without one (see [The head region](htmx.md#the-head-region)), `{{slot "comments"}}` places `fragments/comments.gohtml` with the page's content as its dot, and `hasSlot "menu"` reports whether the theme has it. An HTMX request aimed at a fragment's ID gets only the fragment. A fragment of `search.gohtml`, such as the results of a live search, comes with the regions out of band, so the `<title>` of the head region and the language links follow the query (`render.View.FragmentRegions`); fragments of other pages come alone. A name without a region or fragment gives `""` and `false`. `requiredSlot` places a region or fragment like `slot`, but fails the render for a name without one, with `slot "<name>" is not set`, so a missing fragment does not go unnoticed. See [Regions](htmx.md#regions) and [Fragments](htmx.md#fragments) in HTMX.

```gotemplate
<header>{{slot "menu"}}</header>

<main id="content">
    <h1>{{.Title}}</h1>
    {{requiredSlot "comments"}}
</main>
```

### deferredSlot

```go
func deferredSlot(name string, mode ...string) (template.HTML, error)
```

A fragment that loads after the page. On a whole page it writes an empty element with the fragment's ID that asks the page's own address for the fragment: as soon as the page has loaded, or with `"reveal"` once the element scrolls into view. The page itself does not render the fragment, so a part that takes long to compute does not hold the page up. A name without a fragment writes nothing, and a region's name fails the render: a region goes out of band and cannot be deferred. A visitor form in such a fragment does not show a post with mistakes again, since the request for the page takes the kept post first; place it with `slot` or `requiredSlot`. See [HTMX](htmx.md#a-fragment-that-loads-later).

```gotemplate
<aside>{{deferredSlot "popular" "reveal"}}</aside>
```

### targetHeader, targetValue and targetIs

```go
func targetHeader() string
func targetValue() string
func targetIs(ids ...string) bool
```

go-partial's helpers for the element an HTMX request is aimed at. `targetValue` is its ID, such as `comments` for `HX-Target: section#comments`, and `""` for a whole page; `targetIs` reports whether it is one of `ids`; `targetHeader` is the header's name, `HX-Target`. See [HTMX](htmx.md).

```gotemplate
{{if targetIs "comments"}}<p role="status">Updated.</p>{{end}}
```

## HTMX attributes

go-partial's `exp/interactions` writes htmx's attributes for an element of the template's own, so a theme does not spell them out. Each function takes `runtime` first, and its attributes start with a space, so it can follow the element's other attributes directly. Options come as name and value pairs: `"target"` and `"swap"` say where the answer goes and how (`innerHTML` when left out), `"select"` picks a part of the answer, `"include"`, `"pushURL"` and `"replaceURL"` become `hx-include`, `hx-push-url` and `hx-replace-url`, and a name starting with `hx-`, `data-` or `aria-` is written as it is. Without a `"target"`, the element itself is the target. Wrong arguments, such as an odd number of them, write a `data-partial-error` attribute with the error. See [HTMX](htmx.md).

### noBoost

```go
func noBoost(runtime *partial.Runtime) template.HTMLAttr
```

`hx-boost="false"`, for a link or form in a boosted body that must load a whole page, such as a link to the admin or to another language.

```gotemplate
<a href="{{.URL}}" hreflang="{{.Locale}}"{{noBoost runtime}}>{{.Locale}}</a>
```

### submitAttrs

```go
func submitAttrs(runtime *partial.Runtime, endpoint string, params ...any) template.HTMLAttr
```

The attributes of a form that posts to `endpoint` with htmx and swaps the answer into its target: `hx-post`, `hx-target` and `hx-swap`. `"method"` picks another method, such as `"put"`. Keep `method` and `action` on the form, so it works without JavaScript too.

```gotemplate
<form method="post" action="{{commentURL "remarks"}}"{{submitAttrs runtime (commentURL "remarks") "target" "#remarks" "swap" "outerHTML"}}>
```

This writes `hx-post="/_/remarks/post" hx-swap="outerHTML" hx-target="#remarks"`.

### liveAttrs

```go
func liveAttrs(runtime *partial.Runtime, endpoint string, params ...any) template.HTMLAttr
```

The attributes of a field that asks for `endpoint` while the visitor types, such as a search field: `hx-get`, `hx-target`, `hx-swap` and `hx-trigger="input changed delay:300ms, search"`. `"delay"` changes the pause, and `"include"` sends other fields along, such as `"closest form"`.

```gotemplate
<input type="search" name="q" value="{{.Query}}"{{liveAttrs runtime .URL "target" "#results" "swap" "outerHTML" "replaceURL" "true"}}>
```

### revealAttrs

```go
func revealAttrs(runtime *partial.Runtime, endpoint string, params ...any) template.HTMLAttr
```

The attributes of an element that asks for `endpoint` once it scrolls into view, and swaps the answer into itself: `hx-get`, `hx-target="this"`, `hx-swap` and `hx-trigger="revealed"`. With the page's own address and a fragment's ID, the page answers with the fragment alone; [deferredSlot](#deferredslot) writes such an element for a fragment of the theme (see [HTMX](htmx.md#a-fragment-that-loads-later)).

```gotemplate
{{/* @model View github.com/donseba/go-webthings/frontend.View */}}
<section id="popular"{{revealAttrs runtime View.Path "swap" "outerHTML"}}></section>
```

### Other interaction functions

`confirmAttr` writes `hx-confirm`, and `clientAttrs` a confirmation, boosting, target and swap for a link or form; with `"inherit" "true"` they hold for every element inside, such as the body's for the whole page, where `"disableWhileRequest"` disables elements while a request runs and `"keepOnServerError" "true"` keeps the page as it is on a 5xx answer (see [HTMX](htmx.md#navigation-that-swaps-only-the-content)). With `"reloadOnError" "true"`, a link or GET form that gets a 5xx answer, or none, loads as a whole page, and after a POST the server's error page replaces the page; this needs go-partial's browser client `partial-interactions.js` (see [HTMX](htmx.md#forms-with-htmx)). `asyncAttrs`, `pollAttrs`, `onAttrs`, `streamAttrs` and `refreshAttrs` load an element's content when the page loads, at an interval, on an event, from a stream or on a click. go-partial's functions that write an element of their own, such as `poll` and `reveal`, are not among the template functions. [go-partial](https://github.com/donseba/go-partial) documents them.

## Debugging

### debug

```go
func debug(value any) template.HTML
```

A box that shows `value`, such as a template's dot, with its fields as indented JSON, or in Go syntax when it cannot be JSON. It works only with `Config.Development`; otherwise it shows nothing, so one that is left behind does no harm. A `debug` function of your own in `Config.Funcs` replaces it, also in development. See [Templates](templates.md#while-you-develop).

```gotemplate
{{debug .}}         the template's dot
{{debug .Site}}     in the layout: the site's name, address and more
```

The box is go-partial's, styled by one `<style>` element in it. In development the site's own Content-Security-Policy (see [A Content-Security-Policy of the site's own](deployment.md#a-content-security-policy-of-the-sites-own)) allows that element by its hash, go-partial's `debug.StyleHash()` (`ext/debug`). Another policy without `'unsafe-inline'` in `style-src` blocks it unless it has that hash; the value then shows unstyled.

## Module functions

Every module in `app.Use` adds its functions to every template; their first argument is the instance key. A module's functions exist only when the site uses it. Their signatures, data and examples are in the module's documentation, also at https://modules.gowebthings.com.

| Module | Functions |
| --- | --- |
| [Banners](../modules/banners/README.md) | `banners`, `banner` |
| [Comments](../modules/comments/README.md) | `comments`, `commentCount`, `commentURL`, `commentResult`, `commentsClosed`, `commentThread`, `commentForm`, `commentInput` |
| [Directory](../modules/directory/README.md) | `directoryEntries`, `directoryEntry`, `directoryCategories` |
| [Documents](../modules/documents/README.md) | `documents`, `documentsIn` |
| [Events](../modules/events/README.md) | `events` |
| [Gallery](../modules/gallery/README.md) | `albums`, `album` |
| [Lists](../modules/lists/README.md) | `entries` |
| [Newsletter](../modules/newsletter/README.md) | `newsletter`, `newsletterURL`, `newsletterResult`, `newsletterForm` |
| [People](../modules/people/README.md) | `people`, `peopleIn`, `person` |
| [Polls](../modules/polls/README.md) | `poll`, `polls`, `pollURL`, `pollBox` |
| [Publishing](../modules/publishing/README.md) | `posts` |
| [Registrations](../modules/registrations/README.md) | `registration`, `registerURL`, `registrationResult`, `registrationCode`, `registrationForm` |
| [Stats](../modules/stats/README.md) | `pageViews`, `popular` |
| [Submissions](../modules/submissions/README.md) | `submitURL`, `submitResult`, `submitInput` |
| [Testimonials](../modules/testimonials/README.md) | `testimonials` |

[Forms](../modules/forms/README.md) and [Redirects](../modules/redirects/README.md) add no functions.

```gotemplate
{{range posts "news" 3}}<a href="{{.URL}}">{{.Title}}</a>{{end}}
{{range events "agenda" 5}}{{date .Start "full"}}: {{.Title}}{{end}}
{{commentThread "remarks" .ID}}
```

## Components and e-mail

Components render through go-partial as parts of the page, children of the template that includes them, with the page's request and language. They get every function on this page, go-partial's included, and the same render stages as the page, so `debug` works there with `Config.Development`. A component that fails fails the page. `content` fails in a component; `slot` and `requiredSlot` place the page's fragments, as in the template that includes the component.

Every part of an e-mail renders through go-partial too, the subject and text as plain text, with every function on this page and the same render stages, but without a request or a page: `request` and `url` give nothing, `urlIs` is false, `content` and `requiredSlot` fail, and `slot` and `deferredSlot` write nothing. Without the site's translations, `tl`, `tn`, `ctl` and `ctn` still work in every part and show the text as written.

An e-mail has no visitor's page: `adminURL` gives `""`, and `moduleURL`, `searchURL`, `asset` and `image` give paths without the site's address. `absURL` makes a path absolute only with `Config.BaseURL`. See [E-mail](mail.md).
