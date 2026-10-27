# Themes

A theme is one design of the site: a folder of templates and assets. Editors choose the active theme in the admin, preview it and change its settings, such as colours. [Theme checklist](theme-checklist.md) lists what a theme needs.

## Theme folders

Every folder in `templates/` that has a `layout.gohtml` or a `theme.json` is a theme. `default/` is the base: another theme only holds what differs from it, and finds the templates, partials, regions, fragments, components, e-mail templates and assets it lacks in `default/`. A colour variant can be a single stylesheet:

```text
templates/
  default/
    layout.gohtml
    page.gohtml
    news/post.gohtml
    partials/post-card.gohtml
    regions/head.gohtml
    regions/menu.gohtml
    fragments/comments.gohtml
    assets/site.css
    assets/variant.css
    theme.json
  night/
    assets/variant.css      replaces default's variant.css
    theme.json              the night theme's title and settings
```

Folder names are lowercase letters, digits, `-` and `_`. Other folders are ignored.

A site with `layout.gohtml` directly in `templates/` has a single theme, `default`, made of the whole folder; its subfolders are then not themes.

For a module screen, a component and an e-mail template, a theme's own files come before those of `default/`, and within each folder the instance-key file wins over the type file: with `default/news/post.gohtml` and `night/publishing/post.gohtml`, the night theme shows news posts with `night/publishing/post.gohtml`, and with `default/remarks/form.gohtml` and `night/comments/form.gohtml`, the remarks form with `night/comments/form.gohtml`.

The starter has a second, complete theme next to `default/`: `editorial/`, with its own layout, regions, fragments and module screens, so it needs nothing from `default/`. Its module screens are in the type's folders: `editorial/publishing/list.gohtml` and `editorial/publishing/post.gohtml` show the posts of both `news` and `blog`, where `default/` has `news/` and `blog/`, and `events/` and `registrations/` serve `agenda` and `signup` the same way.

Themes are read when the site starts. Restart it after adding a theme or changing a `theme.json`, also with `Config.ReloadTemplates`.

## theme.json

`theme.json` is optional. It gives the theme a title and description for the admin, and declares settings that editors change there:

```json
{
  "title": "Atelier",
  "description": "Warm paper tones, clay accents and serif headings.",
  "settings": [
    {"name": "accent", "label": {"en": "Accent colour", "nl": "Accentkleur"}, "type": "color", "default": "#b5562f", "css": "--accent"},
    {"name": "headings", "label": "Headings", "type": "select", "options": ["serif", "sans"], "default": "serif"},
    {"name": "notice", "label": "Notice", "type": "text"},
    {"name": "measure", "label": "Text width", "type": "number", "min": 32, "max": 52, "step": 1, "unit": "rem", "default": "42", "css": "--measure"}
  ]
}
```

Without a title, the folder name is used. A `csp` object names the addresses of other sites that the theme's pages load from, such as a font host, for the site's Content-Security-Policy (see [Outside sources](#outside-sources)).

`"screenshot": "screenshot.png"` names a picture of the theme: a PNG, JPEG, WebP, AVIF or GIF image in the theme's `assets/` folder, by its path there, such as `screenshot.png` or `img/preview.jpg`. The admin's theme cards show it, in the **Theme** section and in the Theme step of [Getting started](getting-started.md); while one theme has a picture, the others get a placeholder of the same size with their title. A path that does not stay inside `assets/`, such as `/shot.png`, `../shot.png` or `./shot.png`, another kind of file and a file that does not exist stop the site at start with an error that names the theme, such as `theme default: screenshot "notes.txt": use a PNG, JPEG, WebP, AVIF or GIF image`.

Each setting has these fields:

| Field | Meaning |
| --- | --- |
| `name` | How templates read the setting: lowercase letters, digits, `-` and `_` |
| `label` | What editors see: one text, or an object of language codes shown in the admin's language |
| `type` | `color`, `text`, `select` or `number` |
| `options` | The choices of a `select` |
| `min`, `max` | The lowest and highest value of a `number`, both allowed; a `number` needs both |
| `step` | The difference between the values of a `number`, counted exactly from `min`, such as `0.5`; 0, or left out, allows any value between `min` and `max`. `"step": 0.1` allows 0.3, and `"min": 0.25` with `"step": 0.5` allows 0.75 but not 1 |
| `unit` | The CSS unit of a `number`, which the values stylesheet writes after it: `px`, `rem`, `em`, `ch`, `ex`, `%`, `vw`, `vh`, `vmin`, `vmax`, `s`, `ms`, `deg` or `turn`, or none, such as for a line height |
| `default` | The value until an editor chooses one; for a `number`, the number alone, such as `"42"` |
| `css` | For a `color`, `select` or `number`: the custom property, such as `--accent`, that the theme's values stylesheet sets to the value (see [Settings](#settings)) |

| Type | Admin field | Accepted values |
| --- | --- | --- |
| `color` | A colour picker | A colour like `#1a2b3c`, stored in lowercase |
| `text` | A text field | Up to 500 characters |
| `select` | A list | One of `options` |
| `number` | A number field with `min`, `max` and `step`, and the unit after the label, such as "Text width (rem)" | A number from `min` to `max` on a step, stored as the number alone, such as `42` |

A theme does not inherit settings from `default/`: each `theme.json` declares its own, even when they have the same names. A `theme.json` that is not valid JSON, or a setting with an invalid name, an unknown type, a select without options, a number without `min` and `max`, with `min` above `max`, a negative `step` or an unknown `unit`, a default that does not fit or a `css` that is not allowed (see [Settings](#settings)), or a `csp` directive or source that is not allowed (see [Outside sources](#outside-sources)), stops the site at start with an error that names the theme.

## Settings

The layout reads the active theme's settings as `{{.Theme.Values.accent}}`, and `{{theme "accent"}}` works in every template, including components and e-mail. Each declared setting has a value: the editor's choice or the default. A `number` gives the number alone, without its unit: `{{theme "measure"}}` is `42`. A stored value that no longer fits its setting, such as a colour after the setting became a `select`, or a number outside a new range, gives the default. A name the theme does not declare gives `""`, and `{{theme}}` logs it once with the names the theme has, such as `theme: no setting "acent" in theme default (known: accent)`; with `Config.Development` the page fails with that message (see [Calls that find nothing](templates.md#calls-that-find-nothing)). `.Theme.Name` is the theme's folder name.

A setting reaches the stylesheet in one of two ways, a custom property or a data attribute. Neither needs a `style` attribute or a `<style>` element, so the page works under a Content-Security-Policy without `'unsafe-inline'` for styles (see [Under a Content-Security-Policy](#under-a-content-security-policy)).

### The values stylesheet

A `color`, `select` or `number` setting with `css` becomes a custom property of the theme's values stylesheet, which core serves at `/theme/<theme>/values.css`. `{{themeStyle}}` writes its `<link>`, with a version and its integrity like `assetStyle` (see [Template functions](template-functions.md#themestyle)). It must come after the theme's own stylesheets, so its properties replace their fallbacks; `{{styles}}` links it last by itself:

```gotemplate
{{styles "site.css"}}
```

A layout without `{{styles}}` writes `{{assetStyle "site.css"}}` and then `{{themeStyle}}`.

```css
/* site.css */
:root { --accent: #b5562f; --measure: 42rem; }

a { color: var(--accent); }
main { max-width: var(--measure); }
```

With the `theme.json` above, the values stylesheet is:

```css
:root {
    --accent: #b5562f;
    --measure: 42rem;
}
```

A number goes in with its unit, so `"unit": "rem"` and the value `42` give `--measure: 42rem;`, and a number without a unit, such as a line height of `1.5`, gives `--leading: 1.5;`.

`css` is the name of a custom property: `--`, a lowercase letter, then lowercase letters, digits and `-`. Two settings of a theme cannot set the same one, and a `text` setting cannot have `css`. A select with `css` may only have options of letters, digits, spaces and `#`, `.`, `,`, `%` and `-`, such as `serif`, `1.25rem` or `#fff`, so no option can end the rule. The site does not start with anything else. Each value is checked again before it goes into the stylesheet: a colour must be like `#1a2b3c`, a select's value one of its options, and a number between `min` and `max` on a step. A stored value that does not fit gives the default, and a setting without a value, such as a colour without a default that no editor chose, is left out. A theme without such settings has no values stylesheet: `{{themeStyle}}` writes nothing and the address answers 404.

The stylesheet follows the saved values: a changed value gives it a new version, so its address changes and pages link the new one. The address with the current version is sent with `Cache-Control: public, max-age=31536000, immutable`, any other with `Cache-Control: no-cache`, and every response has an ETag. A preview in the admin links the values stylesheet of the theme it shows, with that theme's saved values. `values.css` is a reserved name below `/theme/<theme>/`: a theme's own `assets/values.css` is not served there, so a stylesheet of the theme needs another name. The layout `task templates -- site` writes links it with `{{styles}}`, after its stylesheets.

### Data attributes

A setting with a few fixed designs can be an attribute of `<html>` instead, with a rule for each option in the stylesheet:

```gotemplate
<html lang="{{.Locale}}" data-headings="{{.Theme.Values.headings}}">
```

```css
[data-headings="sans"] h1, [data-headings="sans"] h2 { font-family: system-ui, sans-serif; }
```

The demo's two themes and the starter's `default/` have a colour picker for their accent, with `"css": "--accent"`, and link it with `{{styles}}`; the demo writes its headings, `serif` or `sans`, as `data-headings`. The starter's `editorial/` keeps a choice of four accents, `vermilion`, `ink`, `forest` and `plum`, written as `data-accent`: each is a designed pair of a light and a dark shade whose contrast with the page's background passes WCAG AA in both modes, which a free colour could not promise. It writes the typeface of its text, `serif` or `sans`, as `data-text`. The width of its column of text is a `number`, `measure`, from 32 to 52 `rem` in steps of 1 and 42 by default, with `"css": "--measure"`, which its layout links with `{{styles}}`.

## Choosing a theme in the admin

The admin's **Theme** section lists the themes with their title, description and picture (see [theme.json](#themejson)), and marks the active one. It needs the same rights as Site settings, and is hidden when the site has no themes. Without the edit right there, it has no **Use this theme** and shows the settings read-only, with a note.

- **Settings** shows a theme's settings. Each theme keeps its own values, so a theme can be set up before it is used. A value equal to the default is not stored, so a new default in `theme.json` still applies. A value that does not fit its type, such as a text over 500 characters, shows its message at the field, and nothing is saved.
- **Preview** opens the home page in that theme, with its saved settings, in a new tab. `/admin/theme/preview/<theme>?path=/en/about` previews another page; `/admin` is the default admin path (see [Admin path](configuration.md#admin-path)). Links in the preview lead to the public site in the active theme.
- **Use this theme** makes it the active theme. It asks first, "Use this theme on the website? Visitors see it right away.", since the website changes at once.

Until an editor chooses one, and when the chosen theme no longer exists, the site uses the first theme: `default`, then the others by name.

## Assets

Files in a theme's `assets/` folder are served below `/theme/`:

| Address | Serves |
| --- | --- |
| `/theme/<theme>/<file>` | The file from that theme, or from `default/` when the theme lacks it |
| `/theme/<theme>/values.css` | The theme's values stylesheet, made by core from its settings (see [The values stylesheet](#the-values-stylesheet)) |
| `/theme/<file>` | The file from the active theme |

`{{asset "site.css"}}` is the address of a file in the theme being rendered, with a version from the file's content, such as `/theme/default/site.css?v=9b2ca0fe143be1a4`. The version changes when the file does, so an address with the current version is sent with `Cache-Control: public, max-age=31536000, immutable`; any other address, such as one without a version or with an old one, is sent with `Cache-Control: no-cache`. Every response has an ETag, so a browser checks a file again cheaply. Subfolders work: `{{asset "img/logo.svg"}}`. Files and folders whose name starts with a dot are not served.

The versions are worked out once per theme, so restart the site after changing an asset; until then its address keeps the old version. `Config.ReloadTemplates` looks at the folder again for every request and gives a changed file a new version, for development.

`{{assetStyle "site.css"}}` and `{{assetScript "site.js"}}` write the `<link rel="stylesheet">` or `<script>` tag of a file at that address, with the file's integrity and `crossorigin="anonymous"`, so the browser refuses a file whose content is not what the page expects; more attributes follow as name and value pairs. With `Config.ReloadTemplates` the theme's files get no integrity, since they may change while the site runs; core's `webthings.css`, `focal-points.css` and `partial-errors.css` keep it (see [Template functions](template-functions.md#assetscript-and-assetstyle)).

```gotemplate
{{assetStyle "site.css"}}
{{assetStyle "variant.css"}}
{{assetScript "site.js" "defer" ""}}
```

`{{styles "webthings.css" "site.css"}}` writes the stylesheet links of `<head>` in the order they must have: go-partial's `partial-errors.css` first (see [The error box](#the-error-box)), then the files given, each once and in that order, as `assetStyle` links them, and the theme's values stylesheet last (see [The values stylesheet](#the-values-stylesheet)). A file comes from the theme's `assets/`, or from core when the theme has none of that name, such as `webthings.css` and `focal-points.css`. None of them is a `<style>` element, so the page works under a Content-Security-Policy without `'unsafe-inline'` (see [Template functions](template-functions.md#styles)).

A file that neither the theme nor core has gives its address without a version or integrity, and `asset`, `assetStyle`, `assetScript` and `styles` log it once with the closest names there are, such as `assetStyle: no file "sitee.css" in the assets of theme default (closest: site.css, webthings.css, focal-points.css)`; with `Config.Development` the page fails with that message (see [Calls that find nothing](templates.md#calls-that-find-nothing)).

`Config.Assets` is one more folder, shared by all themes and served at `/assets/` with versions and ETags like a theme's assets: `{{importmap_url "site.css"}}` is the address of a file with its version, such as `/assets/site.css?v=9b2ca0fe143be1a4`, `{{importmap_style "site.css"}}` and `{{importmap_script "site.js"}}` write its `<link>` or `<script>` tag with its integrity (see [Template functions](template-functions.md#importmap)), and a plain `/assets/site.css` is sent with `Cache-Control: no-cache`. Core adds go-partial's browser clients to the folder, so `{{importmap}}` maps `partial-flash`, `partial-sse` and `partial-interactions` for the theme's scripts; a file of the same name in the folder replaces core's. The folder's versions are worked out when the site starts; with `Config.ReloadTemplates` a changed file gets a new version, like a theme's, and neither the tags nor the import map carry integrity (see [Configuration](configuration.md)). `Config.ImportMap` serves the files of a go-importmap import map there instead, the same way, with go-partial's clients added to its map with their integrity; a client's file among the map's own files is used instead of core's. The demo and the starter vendor htmx this way (see [HTMX](htmx.md#loading-htmx)). A site sets one of the two; with both, it does not start.

## Feeds

Modules can offer Atom and RSS feeds, such as the posts of a news page at `/en/news/feed.atom` and `/en/news/feed.rss`. `{{feedLinks}}` in `<head>` lets browsers and feed readers find them. Core's head tags have it, so a layout that places `{{slot "head"}}` links the feeds without more (see [The head region](#the-head-region)); a head region that replaces core's tags, or a layout without the slot, writes `{{feedLinks}}` itself.

`{{feedLinks}}` writes a `<link rel="alternate" type="application/atom+xml">` and a `<link rel="alternate" type="application/rss+xml">` for each feed of the page shown, with absolute addresses, and nothing on pages without feeds or on error pages, also a module's 404 (see [Template functions](template-functions.md#feedlinks)). The starter's and the demo's themes get it from core's head tags. A module offers feeds with `Parts.Feeds`; see [Feeds](module-feeds-and-links.md#feeds) for modules.

`{{feedLinks}}` is for browsers and feed readers. A link visitors see takes the feed's address from `{{feedURL "atom"}}` or `{{feedURL "rss"}}` (see [Template functions](template-functions.md#feedurl)): the feed of the page shown, such as `/en/news/category/updates/feed.atom` on a category's page, or else the module's, such as `/en/news/feed.atom`, and `""` on a page without feeds and on an error page. In a body that HTMX boosts, give the link `{{noBoost runtime}}`, so the browser opens the feed instead of swapping it into the page. The starter's editorial theme has such links below its lists of posts and events:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/publishing.ListView */}}
{{with feedURL "atom"}}
    <a href="{{.}}" type="application/atom+xml"{{noBoost runtime}}>Atom</a>
    <a href="{{feedURL "rss"}}" type="application/rss+xml"{{noBoost runtime}}>RSS</a>
{{end}}
```

## Regions and fragments

A theme that uses HTMX has two more folders:

| Folder | Holds |
| --- | --- |
| `regions/` | Parts of the layout that change from page to page, such as `regions/menu.gohtml` or `regions/head.gohtml` with the tags of the page in `<head>`. The layout places one with `{{slot "menu"}}`; it gets the page, a `frontend.View`, and follows every HTMX navigation out of band, in the order of the file names. |
| `fragments/` | Parts of a page that HTMX asks for on their own, such as `fragments/comments.gohtml`. A template places one with `{{requiredSlot "comments"}}` or `{{slot "comments"}}`, or after the page with `{{deferredSlot "comments"}}`; it gets the page's content, and a request aimed at `#comments` gets only the fragment. |

A region's outer element carries its name as `id` and `{{oobAttr}}`, except the head region's; a fragment's carries its name as `id`. Both can use the theme's `partials/`. [HTMX](htmx.md) covers them.

### The head region

The layout places the tags of the page in `<head>` with `{{slot "head"}}`. A theme without `regions/head.gohtml` gets core's head region, the tags of `webthings/head-tags.gohtml`:

- the `<title>`, the description, and `<meta name="robots" content="noindex">` in a preview;
- the canonical link, and the `hreflang` links with `x-default`;
- `og:title`, `og:description`, `og:type`, `og:url`, `og:site_name`, `og:locale`, `og:image` and `twitter:card`;
- the favicon and the apple-touch-icon;
- `{{feedLinks}}`, `{{apiLink}}` and `{{structuredData}}`.

Static tags, such as `<meta charset>`, the stylesheets and scripts, stay in the layout:

```gotemplate
<head>
    <meta charset="utf-8">
    {{slot "head"}}
    {{styles "webthings.css" "site.css"}}
</head>
```

`Language.Default` marks the site's default language among `.Languages`. With more than one language, core's tags give that language's address as `hreflang="x-default"`, for visitors whose language the site does not have (see [Search and SEO](search-and-seo.md#canonical-and-alternate-links)). A page with status 400 or above, such as the not-found page, gets no canonical link, alternates, `og:url` or structured data.

`{{structuredData}}` writes what the page is about for search engines: one `<script type="application/ld+json">` with the site, its organisation, the page's trail, the page, and the module's item for the content shown, such as a post's `Article`, an event's `Event` or the `ItemList` of a news page's posts (see [Search and SEO](search-and-seo.md#structured-data)). Like the other tags of the region, it changes with every HTMX navigation. `{{apiLink}}` writes a `<link rel="alternate" type="application/json">` to the page shown in the [content API](api.md), for apps: `/api/v1/pages/<id>?locale=<locale>` for a page, and `/api/v1/pages/resolve?path=<address>` below a module's page, such as for a post, with absolute addresses. It writes nothing while the API is off, on the not-found page and in a preview (see [Template functions](template-functions.md#apilink)).

For tags of its own, a theme adds `regions/head.gohtml` and calls core's tags in it. It gets the View, like the layout, and needs no `id` and no `{{oobAttr}}`. The head region `task templates -- site` writes (`App.WriteTemplates(dir, "site")`) is such a file, with the theme's own tags as a comment to fill in, such as:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/frontend.View */}}
{{template "webthings/head-tags.gohtml" .}}
<meta name="theme-color" content="#336699">
```

A head region that does not call `webthings/head-tags.gohtml` replaces core's tags: it writes the `<title>` and the other tags itself, with `{{feedLinks}}`, `{{apiLink}}` and `{{structuredData}}` where it wants them. A layout without `{{slot "head"}}` gets nothing from core and writes its tags in `<head>` itself.

An HTMX navigation replaces the region's tags with those of the new page, so none of the page left stays behind; a Back or Forward that htmx restores from its history is the exception (see [The head region](htmx.md#the-head-region)). A site without HTMX uses the region the same way; it renders in place on every page. The starter's and the demo's themes use core's head region. [Search and SEO](search-and-seo.md#titles-and-descriptions) covers the tags.

## Components

The interactive modules render components: parts of a page that any template includes with one function. Their default markup is plain, accessible HTML with stable class names in BEM style, and texts through `tl`, so the site's translations apply. It has no `style` attributes and no `<style>` elements, so it works under a Content-Security-Policy without `'unsafe-inline'` for styles.

| Component | Function | Default markup |
| --- | --- | --- |
| Comment thread | `{{commentThread "remarks" .ID}}` | `wt-comments`, `wt-comment`, `wt-comment__body`, `wt-comment--staff` |
| Comment form | `{{commentForm "remarks" .ID}}` | `wt-comment-form` |
| Poll | `{{pollBox "survey" "colour"}}` | `wt-poll`, `wt-poll__question`, `wt-poll__results`, `wt-poll__result`, `wt-poll__bar` |
| Newsletter signup | `{{newsletterForm "letters"}}` | `wt-newsletter`, `wt-newsletter__title`, `wt-newsletter__note` |
| Event sign-up | `{{registrationForm "signup" .ID}}` | `wt-registration`, `wt-registration__facts` |
| Share links (core) | `{{shareBar "" .Title}}` | `wt-share`, `wt-share__list`, `wt-share__link` |

Forms share `wt-form`, `wt-form__field`, `wt-form__label`, `wt-form__input` and `wt-button`, and a group of radio buttons or checkboxes is a `fieldset` with `wt-form__field` and `wt-form__fieldset`, its question a `legend` with `wt-form__label`; radio buttons and checkboxes, such as the options of a poll, are in `wt-form__options` with a `wt-form__option` for each, and a single checkbox has a `wt-form__option` too; messages use `wt-notice`, small print `wt-meta`, and `wt-visually-hidden` hides text from sight but not from screen readers. A poll's result bar is a `<meter class="wt-poll__bar">` with the percentage as its value, and the percentage is also written as text, in `wt-poll__percent`. `webthings.css` fills the bar with `--wt-accent` whatever region its value is in, so a theme's own poll markup with `low`, `high` or `optimum` on the meter gets the accent too, not the browser's red or yellow. The module READMEs describe the components, for example [Comments](../modules/comments/README.md) and [Polls](../modules/polls/README.md).

### Replacing a component

A theme replaces a component with a template of its own, found like a module screen (see [Theme folders](#theme-folders)):

| Component | Theme file |
| --- | --- |
| Comments | `<key>/thread.gohtml` and `<key>/form.gohtml`, or `comments/thread.gohtml` and `comments/form.gohtml` |
| Polls | `<key>/box.gohtml`, or `polls/box.gohtml` |
| Newsletter | `<key>/form.gohtml`, or `newsletter/form.gohtml` |
| Registrations | `<key>/form.gohtml`, or `registrations/form.gohtml` |
| Share links | `site/share.gohtml` |

Without one, the module's default renders. Start from the default: `task templates -- remarks` writes `remarks/thread.gohtml` and `remarks/form.gohtml` (with the instance's other templates), and `task templates -- site` writes `site/share.gohtml`. Move a copy to the type's folder, such as `comments/`, to use it for every instance.

A component template gets the component's data, can use the theme's `partials/` and every template function, go-partial's included (see [Template functions](template-functions.md#components-and-e-mail)). Like the other templates, components are read once per theme: restart the site after changing one, or set `Config.ReloadTemplates` while you develop (see [Templates](templates.md#where-templates-live)).

### Forms

Fields a template renders with `form_field` and the other `form_*` functions (see [Template functions](template-functions.md#forms)) have go-form's markup with the classes of `webthings.css`, from the go-form theme `frontend.FormTheme`: `wt-form`, `wt-form__field`, `wt-form__label`, `wt-form__input`, `wt-form__help`, `wt-form__error`, `wt-form__fieldset` for a group of radio buttons or checkboxes, `wt-form__options` and `wt-form__option` for radio buttons and checkboxes, `wt-form__row` and `wt-button`. A group of radio buttons (`form:"radios"`) or checkboxes (`form:"multicheckbox"`) renders as a `fieldset`, with the field's classes and `wt-form__fieldset`, and its label as the `legend`, with the label's classes; `webthings.css` takes away the browser's border and padding of the fieldset and lays out the legend like a label. The "(required)" after the label of a required field is for screen readers only: a `span` with the class `sr-only`, which `webthings.css` hides inside `wt-form__field`.

`form_open` also writes the spam trap, a text field named `website` that people do not see and many bots fill in (see [The hidden fields](visitor-forms.md#the-hidden-fields)). `frontend.FormTheme` gives it the class `form-honeypot`, which `webthings.css` hides, so the field needs no `style` attribute and the form works under a Content-Security-Policy without `'unsafe-inline'`. The admin's form theme and stylesheet do the same.

`Config.FormTheme` names another go-form theme: one of go-form's own, `bootstrap`, `tailwind`, `tailwindv4` or `plain`, or one the site registers with the classes of its own stylesheet before it starts, with go-form's `github.com/donseba/go-form/v2/templates`, as the demo does:

```go
// go-form's markup with the classes of the theme's site.css.
if _, err := templates.RegisterClassTheme("atelier", templates.ThemeClasses{
    Wrapper:     templates.StyleOption{Class: "field"},
    Description: templates.StyleOption{Class: "help"},
    Error:       templates.StyleOption{Class: "error"},
    Button:      templates.StyleOption{Class: "button"},
    // The spam trap, hidden by site.css.
    Honeypot: templates.StyleOption{Class: templates.HoneypotClass},
}); err != nil {
    log.Fatal(err)
}

app := webthings.New(webthings.Config{
    Templates: os.DirFS("templates"),
    FormTheme: "atelier",
})
```

Such a theme's stylesheet hides `.sr-only` itself. It also takes away the border and padding of the `fieldset` of a group of radio buttons or checkboxes and styles its `legend` like a `label`, as the demo's `site.css` does with `fieldset.field` and `.field legend`. `ThemeClasses.Fieldset` and `Legend` give the fieldset and the legend classes of their own, on top of those of `Wrapper` and `Label`. A name go-form does not know fails every page with a form.

`ThemeClasses.Honeypot` names the class of the spam trap, which the theme's stylesheet hides; the field then has no `style` attribute. The demo uses `templates.HoneypotClass`, `form-honeypot`, with a rule for it in its `site.css`. Without `Honeypot`, go-form gives the field the class `form-honeypot` and an inline `style` that hides it, which a Content-Security-Policy without `'unsafe-inline'` blocks; a rule for `.form-honeypot`, such as the one in `webthings.css`, still hides it then. go-form's `bootstrap` theme hides the field with the class `visually-hidden`, and `tailwind` and `tailwindv4` with `sr-only`.

## The base stylesheet

Core has one optional stylesheet that lays out the default components: spacing, forms, lists and poll bars. Include it in the layout before your own stylesheet:

```gotemplate
{{styles "webthings.css" "site.css"}}
```

Core serves it at `/_/webthings.css`, with a version, its integrity and caching like a theme's assets. A theme with its own `assets/webthings.css` gets that file from `{{styles "webthings.css"}}`, `{{assetStyle "webthings.css"}}` and `{{asset "webthings.css"}}` instead. Every rule is wrapped in `:where()`, so it has no specificity and any rule of the theme wins; only the rules that hide `sr-only` text in forms and the spam trap, `form-honeypot`, have the weight of one class. Leave it out to style the `wt-*` classes yourself.

The stylesheet takes its colours and spacing from custom properties a theme sets:

| Property | Default | Used for |
| --- | --- | --- |
| `--wt-accent` | `#2b59c3` | Buttons, badges, focus outlines, poll bars and the edge of notices |
| `--wt-accent-text` | `#ffffff` | Text on buttons and badges |
| `--wt-border` | `rgb(0 0 0 / 0.18)` | Borders of fields, boxes and share links |
| `--wt-muted` | `rgb(0 0 0 / 0.6)` | Small print and help texts |
| `--wt-surface` | `rgb(0 0 0 / 0.035)` | The background of notices |
| `--wt-bar` | `rgb(0 0 0 / 0.08)` | The track of poll bars |
| `--wt-radius` | `0.375rem` | Corner radius |
| `--wt-gap` | `1rem` | Spacing between parts |
| `--wt-field-width` | `32rem` | The maximum width of forms |
| `--wt-error` | `#b42318` | The errors of form fields |

```css
:root {
    --wt-accent: #b5562f;
    --wt-radius: 0;
    --wt-gap: 1.25rem;
}
```

A theme setting feeds them (see [Settings](#settings)). The starter's `default/` sets `--wt-accent: var(--accent)` in its `site.css`, and the values stylesheet, which `{{styles}}` links last, sets `--accent` to the colour chosen in the admin. Its editorial theme does the same through its data attribute:

```css
:root {
    --accent: #b93c0b;
    --wt-accent: var(--accent);
}

:root[data-accent="forest"] { --accent: #2f6b3a; }
```

## Focal points

Core serves a second optional stylesheet, `focal-points.css`, at `/_/focal-points.css`: one class for each position of an image's focal point in steps of 5%, from `.fp-0-0` to `.fp-100-100`, the horizontal position first, each setting `object-position`. `{{focalClass}}` gives an image the class of its focal point, so a photo the browser crops with `object-fit: cover` keeps it in view without a `style` attribute (see [Crops in the browser](media.md#crops-in-the-browser)). Include it in the layout:

```gotemplate
{{styles "site.css" "focal-points.css"}}
```

It has a version, its integrity and caching like `webthings.css`, and a theme with its own `assets/focal-points.css` gets that file instead. Each rule has the weight of one class, so a rule of the theme that sets `object-position` with more weight, such as `.card > img`, wins over it. The demo's layout includes it for the photos of its cards.

## The error box

Core serves a third optional stylesheet, go-partial's `partial-errors.css`, at `/_/partial-errors.css`. It styles the box that takes the place of a part of the page that failed, such as the answer to an HTMX request whose template fails in development (see [While you develop](templates.md#while-you-develop)). The box has go-partial's classes, `go-partial-error`, `go-partial-error__title`, `go-partial-error__message` and `go-partial-error__details`, and no inline style, so a Content-Security-Policy does not block it; without the stylesheet it shows as a plain heading, paragraph and list. `{{styles}}` links it first, so a layout with `{{styles}}` needs nothing more; a layout without it includes it with:

```gotemplate
{{assetStyle "partial-errors.css"}}
```

It has a version, its integrity and caching like `webthings.css`, and a theme with its own `assets/partial-errors.css` gets that file instead. The layout `task templates -- site` writes links it with `{{styles}}`, and so do the starter's and the demo's.

## Under a Content-Security-Policy

The site can send a Content-Security-Policy that lets browsers run only the scripts and styles the site serves itself. Administrators turn it on under **Site settings › Security**, unless `Config.ContentSecurityPolicy` sets it: the card then shows that mode, which cannot be changed there. Report only blocks nothing and logs what the policy would block, enforce blocks it (see [A Content-Security-Policy of the site's own](deployment.md#a-content-security-policy-of-the-sites-own)). The policy has no `'unsafe-inline'` and no `'unsafe-eval'`. Core's own inline code is allowed by its hash in every page's policy: the import map that `{{importmap}}` writes, the `<style>` element of go-partial's plain error page, and in development that of the `{{debug}}` box. Anything else inline is blocked, so a theme for such a site has:

- no `style` attribute, also not one with `{{objectPosition}}`: `{{focalClass}}` gives an image its focal point as a class (see [Focal points](#focal-points)), and settings reach the stylesheet without one (see [Settings](#settings));
- no `<style>` element and no inline `<script>`. A data block, such as `<script type="application/json">` or the `application/ld+json` of `{{structuredData}}`, does not run, so the policy does not block it;
- no event handler attribute, such as `onclick`, and none of what htmx runs as JavaScript: `hx-on*` attributes, trigger filters, and `js:` or `javascript:` values in `hx-vals`, `hx-headers` or `hx-confirm` (see [HTMX](htmx.md#under-a-content-security-policy)).

The starting points, the modules' default templates and the starter's and the demo's themes keep to these rules. A `<style>` or `<script>` element a theme cannot do without needs its hash in `csp` (see [Outside sources](#outside-sources)).

### Outside sources

A theme that loads files from another site, such as fonts from a font host, names those addresses in its `theme.json`, by directive:

```json
{
  "title": "Atelier",
  "csp": {
    "font-src": ["https://fonts.example.com"],
    "style-src": ["https://fonts.example.com"]
  }
}
```

The policy of the theme's pages, and of its preview in the admin, then allows them besides the site's own files while the policy is on (report only or enforce); with the policy off the preview sends only the admin's `frame-ancestors 'none'`: here `font-src 'self' https://fonts.example.com`, and `https://fonts.example.com` in `style-src`. The sources under **Site settings › Security** and in `Config.ContentSecuritySources` apply in every theme.

The directives are `connect-src`, `font-src`, `form-action`, `frame-src`, `img-src`, `manifest-src`, `media-src`, `script-src`, `style-src` and `worker-src`, in lowercase. Each source is one of:

- an address with the scheme `http`, `https`, `ws` or `wss`, such as `https://www.youtube-nocookie.com`, `https://*.example.com/embed/` or `wss://live.example.com:8443`;
- `'self'`;
- the scheme `https:`, `wss:`, `data:` or `blob:`, except in `script-src`;
- the hash of one inline script or style, such as `'sha256-…'`, in `script-src` and `style-src` only.

Anything else, such as `'unsafe-inline'`, `'unsafe-eval'`, `*`, a nonce, `'strict-dynamic'` or another directive such as `default-src`, stops the site at start:

```text
theme default: csp script-src ['unsafe-inline']: invalid_csp_source
```

`site.CheckCSPSources` does the check, the same as for the sources under **Site settings › Security**. `frontend.Theme.CSP` holds a theme's sources, and `Frontend.ThemeCSP` returns those of the theme a request renders in: the active theme, or the one the admin previews.

## Starting points for a theme

The starter's `task templates` writes into the theme named by `WEBTHINGS_THEME`, `default` when it is unset:

```sh
WEBTHINGS_THEME=night task templates -- remarks
```

A copy in a theme replaces the one in `default/` for that theme, also when the copy is a plain starting point. Delete the copies you do not change. See [Templates](templates.md#starting-points).
