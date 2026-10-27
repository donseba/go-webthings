# Documents

Files for download, such as minutes, reports, forms and policies, listed by category and year with a download counter.

A document is a file from the media library or a link to another site. Use one instance per collection, for example `downloads` for public documents and `minutes` for meeting minutes, each with its own admin section and templates.

## Add it to the site

```go
app.Use(
    documents.Module("downloads", "Downloads"),
)
```

The key (`downloads`) is the instance's name in URLs and templates and must be unique on the site. The title (`Downloads`) is what editors see in the admin navigation and the page editor. The module has no options.

## Place it on a page

In the admin, open a page and choose the instance under **Shows on this page**. The page then lists the documents; with the page at `/downloads`:

| Address | Template | Shows |
| --- | --- | --- |
| `/downloads` | `downloads/list.gohtml` | all published documents, newest first |
| `/downloads/category/<slug>` | `downloads/list.gohtml` | the documents in one category |

On a category address `.Category` is set and `.Title` is the category name instead of the page title. The slug is made from the category name: "Annual reports" becomes `annual-reports`, with accents dropped and at most 80 characters. A category name without Latin letters or digits gets no address. Since category names are typed per language, a category address only exists in the languages that use that name.

Templates are looked up by instance key first, then by module type, so `documents/list.gohtml` serves every Documents instance that has no design of its own. Generate a starting point with every field of the data:

```sh
task templates -- downloads
```

A minimal list, per year:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/documents.ListView */}}
<h1>{{.Title}}</h1>

<nav>
    <a href="{{.AllURL}}">All</a>
    {{range .Categories}}
        <a href="{{.URL}}"{{if .Active}} aria-current="page"{{end}}>{{.Name}} ({{.Count}})</a>
    {{end}}
</nav>

{{range .Years}}
    <h2>{{.Year}}</h2>
    {{range .Documents}}
        <article id="{{.ID}}">
            <a href="{{.URL}}">{{.Title}}</a>
            {{if .External}}external link{{else}}{{.FileType}}, {{.SizeText}}{{end}}
            <time datetime="{{.Date.Format "2006-01-02"}}">{{date .Date}}</time>
            {{.Description}}
        </article>
    {{end}}
{{end}}
```

`.Documents` holds the same documents as one list, for a design without years. `.FileType` is the extension in capitals (`PDF`) and `.SizeText` the size for people (`1.2 MB`); both are empty for an external link. `.Description` is the rendered Markdown and `.Downloads` the count so far. `.Date` is a calendar day, so `{{date .Date}}` shows the same day in every site timezone.

Give each document `id="{{.ID}}"`: site search results link to the document on its list as `#<ID>`.

A document appears in each language it has a title in.

## Show documents anywhere

`documents` returns up to a number of the newest published documents in the current language, for a home page or a sidebar; 0 or less returns all of them. `documentsIn` returns the documents of one category, newest first, by the category's name in the current language (ignoring case) or by its slug.

```gotemplate
{{range documents "downloads" 5}}
    <a href="{{.URL}}">{{.Title}}</a> {{.FileType}} {{.SizeText}}
{{end}}

{{range documentsIn "downloads" "Minutes"}}
    <a href="{{.URL}}">{{.Title}}</a>
{{end}}

<a href="{{moduleURL "downloads"}}">All downloads</a>
```

The download links work on every page, also when the instance is not placed on a page; `.CategoryURL` is then empty.

## Visitors

A document's `.URL` is its download address, `/_/downloads/<id>/download`. A visit adds one to the count and redirects to the file in the media library or to the external link. HEAD requests, such as those of link checkers, are not counted. Unknown, unpublished and deleted documents get the site's 404 page, [404.gohtml](../../docs/templates.md#404gohtml). The redirect and that page are sent with `Cache-Control: no-store` and `X-Robots-Tag: noindex`. A download that fails for another reason, such as an unreachable database, gets the site's error page, [500.gohtml](../../docs/templates.md#500gohtml).

Every request counts; there is no limit per visitor. Links straight to the media file work too, but are not counted.

## Content API

While the site's [content API](../../docs/api.md) is on (`Config.API`), `/api/v1/<key>/documents` and `/api/v1/<key>/categories` serve the published documents and their categories as JSON, in the language of `?locale=`, or the site's default language without it. With the instance `downloads`:

| Endpoint | Route name | Answers with |
| --- | --- | --- |
| `/api/v1/downloads/documents` | `api.downloads.documents` | The published documents, newest first, as a [list](../../docs/api.md#lists) |
| `/api/v1/downloads/documents?category=<slug>` | | The documents of one category; a category without published documents answers 404 `not_found` |
| `/api/v1/downloads/documents/{id}` | `api.downloads.document` | One document |
| `/api/v1/downloads/categories` | `api.downloads.categories` | The categories of the published documents, in alphabetical order, as a list |

Documents have no page of their own, so the list gives each document whole: its `id`, `title` and `description_html`, its `category`, `null` without one, its `date` at midnight UTC, `download_url`, `external`, `file`, `url`, `api_url` and `updated_at`. `download_url` is the document's download address on the site, which counts the download as the site's links do. `external` is true for a link to another site, which has no `file`; `file` has the `filename`, `mime_type`, `file_type`, such as `PDF`, and `size` in bytes. `url` is the document on the list of its category, or on the whole list, such as `https://example.com/en/downloads#<id>`, and `null` while the instance is on no page. A category has its `slug`, `name`, `document_count`, its list as `url` and its documents in the API as `documents_url`. The API has no download counts, which change with every download.

`/documents/{id}` sends no `Last-Modified`, and neither do the lists: a document's category holds `document_count`, which another document changes. The ETag answers `If-None-Match`; see [Caching](../../docs/api.md#caching).

## Editing

Each instance has its own section in the admin (`/admin/downloads`; `/admin` is the default [admin path](../../docs/configuration.md#admin-path)), which shows how often each document was downloaded.

- A document has a date and either a file or an external `http(s)` link, not both. Upload the file in the media library first, then choose it under **File**. The date, file and link are shared by all languages.
- Per language a document has a title, a category and a Markdown description.
- Categories are free text: documents with the same category name belong together, and names that differ only in case or punctuation are one category. Type the name in each language to translate it.
- There are no revisions: saving changes the document at once, also when it is published. **Publish** and **Unpublish** show or hide it; publishing needs a title in the site's default language.
- The site sorts documents by date. The order in the admin (**Move up**, **Move down**) only decides between documents of the same date.
- Saving a document keeps its download count. A file that a document uses cannot be deleted from the media library.
- **Move to trash** moves a document to the [trash](../../docs/editing.md#the-trash), where it can be restored, with its download count, or deleted for good. Until it is deleted for good, its file stays in use.

Editors' rights are set per instance in the groups' permission matrix.

Once the instance is on a page, published documents appear in the site search, found by title, category and description, and the category addresses appear in `sitemap.xml`.

Links to addresses on the site in the descriptions and links of published documents count for [Links to a page](../../docs/editing.md#links-to-a-page): the editor of a page lists the documents that link to it, and **Pages › Broken links** lists their links that lead nowhere.

## From Go

`documents.Of(app, "downloads")` returns the instance once the app has started:

```go
downloads := documents.Of(app, "downloads")

latest, err := downloads.Latest(ctx, "en", 5)
minutes, err := downloads.InCategory(ctx, "en", "Minutes")
```

While the instance is on no page in that language, both still return the documents with their download address as `URL`, and an empty `CategoryURL`.

`Download(ctx, id)` counts a download and returns the address to send the visitor to, for a download route of your own.

`Store()` gives write access for imports and seed data, through the same rules the admin uses. New documents are saved unpublished; the date is a day at midnight UTC:

```go
store := downloads.Store()

item, err := store.Save(ctx, content.Draft[documents.Fields, documents.Text]{
    Fields: documents.Fields{
        Link: "https://example.com/annual-report-2025.pdf",
        Date: time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
    },
    Translations: []content.Translation[documents.Text]{
        {Locale: "en", Text: documents.Text{Title: "Annual report 2025", Category: "Reports"}},
    },
})
if err != nil {
    return err
}

err = store.Publish(ctx, item.ID, "", "en")
```

For a file, upload it with `app.Media.Upload(ctx, "report.pdf", reader)` and set `Fields.File` to the asset's ID.

## Related modules

- [Publishing](../publishing/README.md): a post can link to a document's download address, so those downloads are counted too.
- [Lists](../lists/README.md) keeps short ordered content that is not a file, such as an FAQ.
