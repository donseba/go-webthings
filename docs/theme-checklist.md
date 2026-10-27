# Theme checklist

What a theme needs, in one list. Core does much of the work: it writes the tags in `<head>`, links the stylesheets in their order, and checks every template when the site starts. Each item links to the guide that explains it. The starter's `templates/default/` is a small complete theme to compare with.

## Files

| File | Needed | Without it |
| --- | --- | --- |
| `layout.gohtml` | In `default/`; another theme uses that one unless it has its own | A folder without `layout.gohtml` or `theme.json` is not a theme, and a site without any theme answers with a plain-text page that names the templates to add |
| `page.gohtml` | Yes | Every content page answers with status 500 and `missing_template` |
| `<key>/<name>.gohtml` or `<type>/<name>.gohtml` | For each screen of each module placed on a page | That screen answers with status 500; the start logs `missing template` with the `task templates -- <key>` that writes it |
| `404.gohtml` | Recommended | A plain-text "404 page not found" |
| `500.gohtml` | Recommended | go-partial's plain error page, in the page's language |
| `search.gohtml` | When the site has a search form | `/<locale>/search` answers 404 |
| `theme.json` | Optional | The folder name as the title, and no settings |
| `regions/head.gohtml` | Optional | Core's head tags |

Components and e-mail templates have defaults: copy one only to change it. A module screen with a visitor form gives the element around the form the instance key as its `id`, such as `<section id="{{.Key}}">`, so the form returns the visitor there (see [Module templates](templates.md#module-templates)). A theme other than `default/` holds only what differs from `default/` (see [Theme folders](themes.md#theme-folders)). `task templates -- site` writes the layout, its head region, `assets/site.css`, `page.gohtml`, `404.gohtml` and `search.gohtml`, and `task templates -- <key>` a module's screens (see [Starting points](templates.md#starting-points) and [When something goes wrong](templates.md#when-something-goes-wrong)).

## The layout

```gotemplate
{{/* @dot github.com/donseba/go-webthings/frontend.View */}}
<!doctype html>
<html lang="{{.Locale}}">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    {{slot "head"}}
    {{styles "webthings.css" "site.css"}}
</head>
<body>
    {{content}}
</body>
</html>
```

- `<html lang="{{.Locale}}">` gives the page's language.
- `<meta charset>` and the viewport stay in the layout: core's head tags do not write them.
- `{{slot "head"}}` places core's head tags: the title, the description, the canonical and `hreflang` links, the sharing tags, the icons, the feeds, the link to the content API and the structured data. For tags of your own, add `regions/head.gohtml` and call `{{template "webthings/head-tags.gohtml" .}}` first; a head region without that call replaces core's tags (see [The head region](themes.md#the-head-region)).
- `{{styles}}` links `partial-errors.css` first, then the files given in their order, and the values stylesheet of the theme's settings last. Leave out `webthings.css` to style the `wt-*` classes yourself, and add `focal-points.css` when the theme uses `{{focalClass}}` (see [styles](template-functions.md#styles)).
- `{{content}}` places the content template.
- Around it, the menu from `.Menu`, the language links from `.Languages`, the edit link from `.Editor` and the preview banner from `.Revision` (see [The layout](templates.md#the-layout)).

## With HTMX navigation

- Every content template, `404.gohtml`, `search.gohtml` and the module screens included, wraps its content in `<main id="content">` (see [Navigation that swaps only the content](htmx.md#navigation-that-swaps-only-the-content)).
- A region's outer element carries the region's name as `id` and `{{oobAttr}}`, and is written on every page, empty when there is nothing to show. The head region needs neither (see [Regions](htmx.md#regions)).
- The language links and the links to the admin have `{{noBoost runtime}}`, so they load a whole page.
- htmx 4 is loaded, for example with the import map (see [Loading htmx](htmx.md#loading-htmx)).

## Under a Content-Security-Policy

- No `style` attribute: `{{focalClass}}` gives an image its focal point as a class, and a setting reaches the stylesheet with `css` or as a data attribute (see [Settings](themes.md#settings)).
- No `<style>` element and no inline `<script>`. A data block, such as `<script type="application/json">`, is fine.
- No event handler attribute, such as `onclick`, and no `hx-on*` attribute, trigger filter or `js:` value (see [Under a Content-Security-Policy](htmx.md#under-a-content-security-policy)).
- Files from another site, such as fonts, are named in `theme.json` under `csp` (see [Outside sources](themes.md#outside-sources)).

Regions and fragments follow the same rules: what htmx swaps into the page runs under the page's policy. See [Under a Content-Security-Policy](themes.md#under-a-content-security-policy).

## Check it

- Run the site with `task dev`. Templates are read again on every request, the start fails with every template problem at once, and a call that finds nothing, such as `{{theme "acent"}}`, fails its page (see [While you work on the templates](getting-started.md#while-you-work-on-the-templates) and [Checking the templates at start](templates.md#checking-the-templates-at-start)).
- Restart the site after adding a theme or changing a `theme.json`, also with `task dev`.
- Open each kind of page once: the home page, a content page, every module screen, an address that does not exist and the search.
- Choose **Report only, block nothing** under **Site settings › Security**, use the site, and read the log: each `content security policy violation` names what the theme must change (see [A Content-Security-Policy of the site's own](deployment.md#a-content-security-policy-of-the-sites-own)).
