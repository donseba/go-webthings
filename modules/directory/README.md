# Directory

A guide to organisations, shops, venues or services by category, with contact details, an address, opening hours, a search and CSV import.

An entry has the same name and address in every language; its summary, description, keywords and hours note are translated. Use one instance per guide.

## Add it to the site

```go
app.Use(
    directory.Module("directory", "Directory"),
)
```

The key (`directory`) is the instance's name in URLs and templates and must be unique on the site. The title (`Directory`) is what editors see in the admin navigation and the page editor. The module has no options.

## Place it on a page

In the admin, open a page and choose the instance under **Shows on this page**. With the page at `/directory`:

| Address | Template | Shows |
| --- | --- | --- |
| `/directory` | `directory/list.gohtml` | all entries, grouped by category |
| `/directory?q=bakery` | `directory/list.gohtml` | the entries that match a search |
| `/directory?category=<slug>` | `directory/list.gohtml` | the entries of one category |
| `/directory/<category>` | `directory/category.gohtml` | one category with its entries |
| `/directory/<category>/<entry>` | `directory/entry.gohtml` | one entry |

Entries are sorted by name; groups and categories follow the order set in the admin. The search on the list needs every word in the entry's name, summary or keywords, ignoring case and accents, like the site search. An entry that moves to another category, or whose category gets a new web address, keeps working at its old address, which redirects.

Templates are looked up by instance key first, then by module type, so a second instance `directory.Module("shops", "Shops")` uses `shops/list.gohtml` when it exists and `directory/list.gohtml` otherwise. Generate starting points with every field of the data:

```sh
task templates -- directory
```

A minimal list with a search form:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/directory.ListView */}}
<h1>{{.Title}}</h1>

<form action="{{.URL}}">
    <input type="search" name="q" value="{{.Query}}">
    <button>Search</button>
</form>

{{range .Categories}}
    <a href="{{.URL}}">{{.Name}} ({{.Count}})</a>
{{end}}

{{range .Groups}}
    <h2>{{.Category.Name}}</h2>
    {{range .Entries}}
        <p><a href="{{.URL}}">{{.Name}}</a>{{if .OpenNow}} · open now{{end}}<br>{{.Summary}}</p>
    {{end}}
{{end}}
```

`entry.gohtml` gets a single `directory.Entry`. `.Description` is the rendered Markdown, `.ImageURL` the logo or image as pages show it, at most 1600 pixels wide, and `.HasLocation` tells whether `.Latitude` and `.Longitude` are known, for a map. `.ImageSrcset`, `.ImageWidth` and `.ImageHeight` complete the `img` tag, and `.ImageOriginalURL` is the uploaded file:

```gotemplate
{{if .ImageURL}}<img src="{{.ImageURL}}" srcset="{{.ImageSrcset}}" sizes="20rem" width="{{.ImageWidth}}" height="{{.ImageHeight}}" alt="">{{end}}
```

Opening hours:

```gotemplate
{{if .Hours.Known}}
    <table>
        {{range .Hours.Week}}
            <tr><th>{{weekday .Weekday}}</th><td>{{if .Closed}}closed{{else}}{{.Open}}–{{.Close}}{{end}}</td></tr>
        {{end}}
    </table>
    <p>{{.HoursNote}}</p>
{{end}}
```

`.Hours.Week` runs from Monday to Sunday. `.Hours.Known` is false when no day has times, so such entries show no hours rather than "closed" every day. `.OpenNow` is worked out when the page renders, in the site's timezone.

## Show entries anywhere

```gotemplate
{{range directoryCategories "directory"}}
    <a href="{{.URL}}">{{.Name}} ({{.Count}})</a>
{{end}}

{{range directoryEntries "directory" "food" 5}}
    <a href="{{.URL}}">{{.Name}}</a>
{{end}}

{{with directoryEntry "directory" "bakery-jansen"}}
    <p>{{.Name}}, {{.Street}}, {{.City}}{{if .OpenNow}} · open now{{end}}</p>
{{end}}
```

`directoryEntries` takes a category slug (`""` for all categories) and a limit (`0` or less for all). `directoryCategories` returns every published category, also those without entries. Links point to the page the instance is placed on; while it is on no page in the current language, `.URL` of entries and categories is empty.

## Content API

While the site's [content API](../../docs/api.md) is on (`Config.API`), `/api/v1/<key>/entries` and `/api/v1/<key>/categories` serve the published entries and categories as JSON, in the language of `?locale=`, or the site's default language without it. With the instance `directory`:

| Endpoint | Route name | Answers with |
| --- | --- | --- |
| `/api/v1/directory/entries` | `api.directory.entries` | The published entries of the published categories, by name, as a [list](../../docs/api.md#lists) |
| `/api/v1/directory/entries?category=<slug>` | | The entries of one category; a category that does not exist or is not published answers 404 `not_found` |
| `/api/v1/directory/entries?slug=<slug>` | | The entry with that slug |
| `/api/v1/directory/entries/{id}` | `api.directory.entry` | One entry, with its description |
| `/api/v1/directory/categories` | `api.directory.categories` | The published categories, in the editors' order, as a list |

An entry has its `id`, `slug` and `name`, its `summary`, `keywords` and `hours_note` in the language of the answer, its `category` and its `image` as an [image object](../../docs/api.md#responses). The contact details, address, position and opening hours are those the entry's page shows, each `null` when not given:

| Field | |
| --- | --- |
| `website`, `email`, `phone` | As the editors wrote them |
| `address` | `street`, `postcode` and `city` |
| `location` | `latitude` and `longitude` |
| `opening_hours` | The days from Monday to Sunday, each with `day`, such as `monday`, and `open` and `close`, such as `"08:00"`, both `null` on a closed day |

`url` is the entry's page, `null` while the instance is on no page; `api_url` and `updated_at` follow. `/entries/{id}` adds `description_html`, when the entry has a description. The API has no `open_now`, which changes with the time of day; work it out from `opening_hours`. A category has its `id`, `slug`, `name` and `summary`, `entry_count`, the number of its published entries, its page as `url` and its entries in the API as `entries_url`.

`/entries/{id}` sends no `Last-Modified`, and neither do the lists: an entry's category holds `entry_count`, which another entry changes. The ETag answers `If-None-Match`; see [Caching](../../docs/api.md#caching).

## Editing

The admin section (`/admin/directory`; `/admin` is the default [admin path](../../docs/configuration.md#admin-path)) lists the entries by name, with their category, and links to **Categories** and **Import CSV**.

- An entry has a name, a web address, a category, a logo or image, a website, e-mail, phone, an address with an optional latitude and longitude, and weekly opening hours. Per language it has a summary, a Markdown description, search keywords (separated by commas) and a note on the hours, such as "closed on public holidays".
- Latitude and longitude are number fields in decimal degrees, such as `52.3731` and `4.8922`, with a decimal point; a comma is not accepted. Fill in both or neither; the latitude lies between -90 and 90 and the longitude between -180 and 180, or the field gets an error.
- An empty **Web address**, of an entry or a category, is made from the name and kept when the name changes later: lowercase letters and digits joined by dashes, accents dropped, at most 80 characters, so "Café Noord" becomes `cafe-noord`. A name without Latin letters or digits needs a web address typed by hand. Web addresses are unique per instance. The website must be a full address starting with `http://` or `https://`; anything else gets an error at its field.
- Opening hours are an opening and a closing time per day; a day without times is closed. A closing time at or before the opening time runs past midnight (22:00–02:00), `24:00` closes at midnight and 00:00–00:00 is open all day.
- Categories have a web address, and per language a name and summary. They are ordered by hand. A category that still has entries cannot be deleted. Deleted entries keep their category, so it can only be deleted for good after them.
- Entries and categories are published one by one, without revisions. An entry only shows while its category is published. Entries show in each language they were saved in, categories in each language they have a name in.
- **Move to trash** moves an entry or a category to the [trash](../../docs/editing.md#the-trash), where it can be restored or deleted for good. Its web address is free again meanwhile; restoring it is refused when another item has taken it.

Editors' rights are set per instance in the groups' permission matrix; categories and the import share the rights of their instance.

Once the instance is on a page, published entries appear in the site search, found by name, summary, keywords and description, and every category and entry address appears in `sitemap.xml`.

An entry's page describes the entry to search engines in its structured data, which core's head tags write with `{{structuredData}}`. The entry is a schema.org `LocalBusiness` when it has opening hours, with them as `openingHoursSpecification`; a `Place` when it has a street, a postcode, a city or a latitude and longitude, without the e-mail address, which a `Place` does not have; and an `Organization` otherwise. Each has the name, the summary as `description`, the page as `url`, the image, the phone number, the e-mail address, the website as `sameAs`, the address as a `PostalAddress` and the latitude and longitude as `geo`, as far as the entry has them. The list and a category's page are an `ItemList` of the entries they show, in their order. See [Search and SEO](../../docs/search-and-seo.md#structured-data).

Links to addresses on the site in the descriptions and websites of published entries count for [Links to a page](../../docs/editing.md#links-to-a-page): the editor of a page lists the entries that link to it, and **Pages › Broken links** lists their links that lead nowhere.

The editor of a published entry lists the published content that links to the entry and asks before it is unpublished or moved to the trash. With the [redirects module](../redirects/README.md), editors who may create redirects also get **Unpublish and redirect**, and saving a published entry with a new **Web address** redirects its old address to the new one; see [Links to an item](../../docs/editing.md#links-to-an-item). The editor of a published category does the same for the category's page, such as `/directory/food`, and counts the links to its entries' addresses, such as `/directory/food/bakery`, apart from those: "1 published item links to an address below this item.", since unpublishing the category takes its entries offline too; a category that still has entries cannot be moved to the trash. While only such links exist, it asks "Published items link to addresses below this item. Unpublish it anyway?". **Unpublish and redirect** redirects the entries' addresses too, to the same page. When the category gets a new **Web address**, only its own address gets a redirect; its entries' old addresses lead to their new ones without one, as for an entry that moved to another category.

## Import entries from CSV

**Import CSV** takes an uploaded file or pasted text. The file must be a CSV, TSV or plain-text file (`.csv`, `.tsv`, `.txt`); another kind gets an error at the field. The first row names the columns, in any order: `name` and `category` are required, `summary`, `website`, `email`, `phone`, `street`, `postcode`, `city` and `keywords` are optional. Values are separated by commas, semicolons or tabs, whichever the first row uses most.

```text
name;category;summary;website;city;keywords
Bakery Jansen;Food;Bread and cakes;bakery-jansen.example;Amsterdam;bread, cake
Town Hall;Services;Passports and permits;;Amsterdam;passport
```

- A row matches an existing entry by the web address made from its name. Existing entries are skipped, unless **Update entries that already exist** is checked; columns missing from the file then keep their values.
- The category is found by its name in the default language, ignoring case, or by its web address. Missing categories are created.
- A website without a scheme gets `https://`: `bakery-jansen.example` becomes `https://bakery-jansen.example`.
- The summary, keywords and new category names are stored in the site's default language. Descriptions, images, coordinates and opening hours are not imported.
- New entries and categories stay unpublished unless **Publish new entries and categories** is checked. Existing categories keep their state, so entries imported into an unpublished category stay hidden until it is published.

The result counts what was created, updated and skipped, and lists each skipped row with the reason. The import needs the create right; updating also needs the edit right and publishing the publish right.

## From Go

`directory.Of(app, "directory")` returns the instance once the app has started:

```go
guide := directory.Of(app, "directory")

bakeries, err := guide.Entries(ctx, "en", directory.Query{Search: "bread", Category: "food"})
```

`Entry(ctx, locale, slug)` returns one entry and `ListCategories(ctx, locale)` the categories. While the instance is on no page in that language, they still return the published entries and categories, with an empty `URL`. `Store()` and `Categories()` give write access for seed data, through the same rules the admin uses, so an entry without a category is refused. `Import` runs the CSV import on any `io.Reader`:

```go
options := directory.ImportOptions{Locale: "en", Update: true, Publish: true}

result, err := guide.Import(ctx, file, options)
if err != nil {
    return err
}

log.Printf("%d created, %d updated, %d skipped", result.Created, result.Updated, result.Skipped)
```

## Related modules

- [People](../people/README.md) presents the people of an organisation rather than organisations.
- [Events](../events/README.md) keeps an agenda with locations.
- [Lists](../lists/README.md) keeps short ordered lists, such as services, without addresses or hours.
