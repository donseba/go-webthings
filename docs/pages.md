# Pages

Editors build the site's structure as a tree of pages in the admin. Each page has an address and a place in the menu, and shows either its own text or a module.

## The page tree

Every page except the home page has a parent page. The tree gives each page its address:

```text
Home                        /en/
├── About                   /en/about
│   └── Team                /en/about/team
└── News [module: news]     /en/news, and everything below it
```

Under **Pages** the admin shows this tree in the order of the menu: Home first, each page with its children indented below it. Every page shows its path, its module if it has one, and its status: **Draft**, **In review**, **Published**, **Changes not published** or **Scheduled**. A tab per language shows the tree in that language and marks pages that miss a translation there. **New page** opens an empty page below Home.

The **↑** and **↓** buttons move a page up or down among the pages with the same parent. A page can also be dragged by its handle to another place among them; a line shows where it lands, and Escape cancels. The order is not part of a revision: it changes the menu on the site at once. Pages that share a position, for example after an import, are in the order they were created, the same in every language. The buttons and handles need the right to edit pages and are hidden while the tree is searched. The buttons also work from the keyboard and without JavaScript.

A page has these fields:

| Field | Per language | What it does |
| --- | --- | --- |
| **Title** | yes | The page's name in the menu, in the layout's `.Title` and in search results. Required. |
| **Web address** | yes | The page's own part of its address, its slug, such as `team`. Made from the title when left empty. |
| **Summary** | yes | A short text: the layout's `.Description`, for the meta description, and the summary in search results. |
| **Content** | yes | The page's text, written formatted and stored as Markdown; see [Editing and publishing](editing.md#writing-text). |
| **Parent page** | no | Where the page sits in the tree. |
| **Shows on this page** | no | A module instance that serves this page and everything below it, or "Nothing, a normal page". |

The fields without a language are shared: changing the parent in one language moves the page in every language.

## Addresses

A page's address is the language prefix followed by the slugs of its parents and its own slug, such as `/nl/over-ons/team` and `/en/about/team`. Every language has its own slugs.

- A **Web address** uses lowercase letters, digits and dashes, and does not start or end with a dash. Another value is refused at the field: "Use lowercase letters, digits and dashes for the web address."
- An empty **Web address** is made from the title when the page is saved: "Opening hours" becomes `opening-hours`, and `-2`, `-3` and so on are added when a page with the same parent already uses it. A title without Latin letters or digits, such as one in Greek, needs a web address typed by hand; without one, saving is refused with "Give the page a web address of lowercase letters, digits and dashes, or a title it can be made from."
- Within one language, pages with the same parent need different web addresses. This is checked when a page is published.
- A top-level page cannot have the first segment of one of the app's own addresses as its web address: that of the [admin path](configuration.md#admin-path), `admin` by default, and `media`, `assets`, `theme`, `healthz`, `readyz` and `metrics`, and `api` while the [content API](api.md) is on. Publishing such a page is refused with "Another page at this level already uses this web address, or the address is reserved." A web address made from a title avoids them, such as `theme-2` for "Theme".
- A page is on the site in a language only when it and all its parent pages are published and have a title in that language. A page below an unpublished page is offline too.

To move a page, choose another **Parent page** and publish the page. The dropdown lists the pages in tree order, without the page itself, the pages below it and pages with a module. The page goes last among its new siblings, and its child pages move with it, so their addresses change too. The admin's tree shows the page under its new parent as soon as it is saved; the site's menu and addresses keep it in its old place until that revision is published. Until then, the page's editor shows the old and the new address in each language where it changes, and how many published items link to the old one, if any do. Once it is published, the old addresses answer "not found", unless the [redirects module](../modules/redirects/README.md) redirects them: with it, **Publish this revision** has a box, ticked by default, that redirects the old addresses of the page and of the pages below it to the new ones. A new **Web address** works the same way. See [A new address](editing.md#a-new-address).

Dragging a page in the tree to another parent does the same, without opening the page. Drop it onto a page to place it last below that page, or between the pages of another parent to place it there. The new parent is saved as a draft, and a message says to publish the page to move it on the site. The pages the dropdown leaves out take no drop, and the home page cannot be dragged.

## The home page

The first start creates the home page, titled "Home" in every language, and publishes it. It is served at `/<locale>/`, such as `/en/`, and `/` redirects to the home page of the default language.

The home page is the root of the tree. It has no web address of its own and no parent, and it cannot show a module: its **Parent page** only offers "None (top level)" and its **Shows on this page** only "Nothing, a normal page". It always stays on the site; publish a newer revision to change it. Its editor has no **Unpublish** and no **Move to trash**, and its schedule only has **Publish at**. Other attempts are refused with "The home page always stays on the website: it cannot be unpublished, scheduled to be unpublished or deleted."

The home page is permanent by its nature, and nobody can take that away. Administrators can make other pages permanent too; see [Permanent items](editing.md#permanent-items).

## Publishing and revisions

Pages are revisioned. **Save draft** creates a new draft revision; the published revision stays online until an editor publishes another one. A revision holds every language of the page, so publishing it publishes all its translations at once.

- **Publish this revision** needs a title in the site's default language. When **Site settings › Review** is ticked for Pages, it also needs a reviewer's approval (see [Review before publishing](editing.md#review-before-publishing)).
- **Unpublish** asks "Take this off the website?" and takes the page offline, and with it every page below it. When published content links to the page, its editor lists that content and asks "Published items link to this page. Unpublish it anyway?" instead; with the redirects module it also offers **Unpublish and redirect**. See [Links to a page](editing.md#links-to-a-page).
- Editors can preview a revision on the site, plan publication for later and restore an older revision.

[Editing and publishing](editing.md) describes the publication box in detail.

### Previewing a page

**Preview this revision** opens the page on the site with `?revision=<id>`, such as `/en/about/team?revision=…`, in the language of the editor's tab. The page shows where the revision puts it: below the parent page's published address, with the revision's **Web address**. A published page also shows the revision at its current address, so a new web address or a page that was never published can be checked before publishing.

- Only signed-in editors who may read **Pages** see the preview; everyone else gets the published page or "not found".
- A page below a page that is not published has no address yet, so it cannot be previewed.
- A page with a module shows that module's screen at the page's address, with the page as the revision has it.

See [Previewing a revision](editing.md#previewing-a-revision) for the banner and the headers.

## Deleting a page

**Move to trash** asks "Move this item to the trash?" and moves a page to the trash, with its revisions. It leaves the menu and its address at once, and its web address and module are free for other pages. A page with child pages cannot be deleted: move or delete the child pages first. The home page cannot be deleted. When published content links to the page, the admin asks "Published items link to this page. Move it to the trash anyway?" instead.

A page comes back from the trash at its old place in the tree, published if it was. While its parent page is in the trash too, restore the parent first. Restoring refuses a published page whose web address another published page at its level uses now, or whose module is placed on another page now. Deleting a page for good in the trash removes its revisions too. See [The trash](editing.md#the-trash).

## Links to pages

**Broken links**, above the page tree, lists the addresses that links in published pages and module content lead to where the site has no page, with the reason and the content that links there. With the redirects module, each address has **Create redirect**. See [Broken links](editing.md#broken-links).

From Go, `App.Links`, a `links.Check`, gives what the admin uses. `Index(ctx)` returns the links in published content: `Index.Links()` all of them, per language those of the pages first, in tree order, then those of the modules, and `Index.To(addresses...)` those to one of the addresses or below it, such as to `/en/about` and `/en/about/team` for `/en/about`, without the links in content that is there itself. `Broken(ctx)` returns the links to addresses where the site has no page and no redirect leads to a page or another website. `Table(ctx)` returns the published page table the links are resolved against, such as for the pages an address can redirect to. Each `links.Link` has the content it is in (`Source`), the key of that content's module (`Module`, empty for a page), its language (`Locale`), the link as written (`Href`), the address it leads to (`Address`, such as `/en/contact`, without query and fragment), the page that answers there (`Page`) and whether one does (`Found`). An address below a module page counts as found, as the module answers it, and so does the search screen. `App.Links` reads the published pages and the modules' `Parts.Links` only when it is asked, never while a visitor's page is served, and keeps what it found as the admin does (see [Links to a page](editing.md#links-to-a-page)). The links in a published revision are kept per revision and language, since a revision never changes.

`pages.Table.Lookup(locale, path)` finds the published route of a path in a language without loading the page: the page at the path, or the module page above it, with the path below that page as `rest`, `/` for the page itself. `ok` is false when no published page answers. `App.Pages.Table(ctx)` returns the table of published routes:

```go
table, err := app.Pages.Table(ctx)
if err != nil {
    return err
}

// With the news module on /news: route.Path is "/news", route.Module
// "news" and rest "/open-studio".
route, rest, ok := table.Lookup("en", "/news/open-studio")
```

## The menu

The layout gets the published page tree in the current language as `.Menu`, a list of `frontend.MenuItem`:

| Field | Meaning |
| --- | --- |
| `ID` | The page's ID. |
| `Path` | The page's path without language prefix, such as `/about/team`. |
| `Title` | The page's title in this language. |
| `URL` | The page's address, such as `/en/about/team`. |
| `Active` | The visitor is on this page or on a page below it. |
| `Children` | The pages below it, with the same fields. |

The menu starts below the home page, so Home itself is not in it; link to it with `.Site.HomeURL`. Pages are in the order of the admin's page tree, set with **↑** and **↓** or by dragging, except a page whose move to another parent is not published yet: it stays in its old place. Module pages are in the menu like any other page.

```gotemplate
<nav>
    <a href="{{.Site.HomeURL}}">{{.Site.Name}}</a>
    {{range .Menu}}
        <a href="{{.URL}}" {{if .Active}}aria-current="page"{{end}}>{{.Title}}</a>
        {{with .Children}}
            <ul>
                {{range .}}<li><a href="{{.URL}}" {{if .Active}}aria-current="page"{{end}}>{{.Title}}</a></li>{{end}}
            </ul>
        {{end}}
    {{end}}
</nav>
```

Every published page is in the menu. To leave a page out, skip it in the layout by its `Path`, for example with `{{if ne .Path "/privacy"}}` inside `{{range .Menu}}`.

## Placing a module on a page

The **Shows on this page** field lists the module instances from `main.go` that serve pages of their own. Choose one and publish the page: the module then serves the page's address and every address below it. With the [publishing module](../modules/publishing/README.md) on a page with the web address `news`:

| Address | Shows |
| --- | --- |
| `/en/news` | the list of posts |
| `/en/news/category/<slug>` | the posts in one category |
| `/en/news/<slug>` | one post |

Each module's README lists the addresses it serves and the templates it uses.

- An instance can be placed on one page only.
- A page with a module cannot have child pages.
- Most bundled modules use the page's title as the title of their list. The page's **Summary** and **Content** are not shown on the module's screens, but the layout can show them from `.Page`.
- `{{moduleURL "news"}}` is the address of the page an instance is placed on, in the current language, or empty when it is on no page.
- When an instance is removed from `main.go`, its page stays published and answers with status 503 until the instance is back or the page is unpublished.

[Using modules](modules.md) explains instances, keys and templates.

## page.gohtml

`page.gohtml` shows a page without a module. It gets a `frontend.Page`:

| Field | Meaning |
| --- | --- |
| `ID` | The page's ID. |
| `ParentID` | The parent page's ID, empty for the home page. |
| `Slug` | The page's own part of its path in this language, such as `team`. |
| `Path` | The whole path without language prefix, such as `/about/team`; `/` for the home page. |
| `Title` | The title. |
| `Description` | The description. |
| `Body` | The content, rendered from Markdown to HTML. |
| `URL` | The page's address, such as `/en/about/team`. |
| `IsHome` | Set for the home page, which sites often design differently. |

```gotemplate
{{/* @dot github.com/donseba/go-webthings/frontend.Page */}}
<main id="content">
    {{if .IsHome}}
        <section class="hero">
            <h1>{{.Title}}</h1>
            {{with .Description}}<p class="lead">{{.}}</p>{{end}}
        </section>
        {{range posts "news" 3}}<a href="{{.URL}}">{{.Title}}</a>{{end}}
    {{else}}
        <h1>{{.Title}}</h1>
    {{end}}

    <div class="prose">{{.Body}}</div>
</main>
```

The layout gets the same page as `.Page` on content pages and their previews, and on module screens the page the module is placed on. Use `{{with .Page}}` in the layout, because the search and "not found" screens have no page there. A preview has the `Path` and `URL` of the address it is shown at, also for a page that was never published.

`task templates -- site` writes starting points for `layout.gohtml`, `page.gohtml`, `404.gohtml` and `search.gohtml`. [Templates](templates.md) explains how the layout and the content templates fit together.

## 404.gohtml

`404.gohtml` is shown, with status 404, for every address that does not exist:

- an address without a known language prefix, such as `/about` (shown in the default language);
- a path that no published page has;
- an address below a module page that the module does not know, such as a post that was unpublished.

It gets a `frontend.NotFound`, with `HomeURL` the home page in the current language. The layout's `.Title` is "Not found", translated with the site's `tl` (msgid `Not found`). `task translations` finds that text itself, although no template contains it (see [Languages](languages.md#keeping-the-files-up-to-date)). The menu is that of the current language and the language switch leads to the home page of each language.

```gotemplate
{{/* @dot github.com/donseba/go-webthings/frontend.NotFound */}}
<main id="content">
    <h1>{{tl localizer "Page not found"}}</h1>
    <p><a href="{{.HomeURL}}">{{tl localizer "Back to the home page"}}</a></p>
</main>
```

Without `404.gohtml` the site answers with a plain "404 page not found".

## Canonical addresses

Every page has one address. Other spellings redirect to it, so search engines and visitors see one version:

| Request | Answer |
| --- | --- |
| `/` | 302 to the default language's home page, such as `/en/` |
| `/en` | 301 to `/en/` |
| `/en/about/` | 301 to `/en/about` |
| `/en//about` or `/en/./about` | 307 to `/en/about`: Go's `http.ServeMux` cleans the path |
| `/EN/about` | 301 to `/en/about`, the language code as written in the site settings |

A redirect keeps the query string: `/en/about/?ref=mail` leads to `/en/about?ref=mail`. Only the home page ends with a slash. Content pages answer GET and HEAD requests only.
