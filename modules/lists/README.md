# Lists

Short ordered lists such as an FAQ, services, features or steps: entries with a title, Markdown text, an optional image and an optional link.

One instance is one list, so a site with an FAQ and a list of services adds two instances. Entries have no pages of their own: a list is shown whole, on the page it is placed on or anywhere else through a template function.

## Add it to the site

```go
app.Use(
    lists.Module("faq", "FAQ"),
    lists.Module("services", "Services"),
)
```

The key (`faq`) is the list's name in URLs and templates and must be unique on the site. The title (`FAQ`) is what editors see in the admin navigation and the page editor.

## Place it on a page

In the admin, open a page and choose the list under **Shows on this page**. With the page at `/faq`:

| Address | Template | Shows |
| --- | --- | --- |
| `/faq` | `faq/list.gohtml` | the published entries, in the order set in the admin |

Other addresses below the page get the site's 404 page. Templates are looked up by instance key first, then by module type, so `lists/list.gohtml` serves every list that has no design of its own; `.Key` tells the lists apart inside a shared template. Generate a starting point with every field of the data:

```sh
task templates -- faq
```

A minimal FAQ:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/lists.ListView */}}
<h1>{{.Title}}</h1>

{{range .Entries}}
    <article id="{{.ID}}">
        {{with .ImageURL}}<img src="{{image . "120x120 crop"}}" width="120" height="120" alt="">{{end}}
        <h2>{{.Title}}</h2>
        {{.Body}}
        {{with .Link}}<a href="{{.}}">Read more</a>{{end}}
    </article>
{{end}}
```

`Title` is the published title of the page the list is placed on. `Body` is the rendered Markdown, with media from the library already resolved. `Link` is a `template.URL`, so `tel:` links work in `href`. `ImageURL` is the image as pages show it, at most 1600 pixels wide, with `ImageSrcset`, `ImageWidth` and `ImageHeight` for an `img` tag of that size and `ImageOriginalURL` for the uploaded file; given `ImageURL`, `image` makes the square copy above from the uploaded image (see [Media and images](../../docs/media.md#image-sizes-in-templates)). Keep `id="{{.ID}}"` on each entry, as the generated starting point does: site search results link to `#<id>` on the list's page, and the ID never changes.

An entry appears in each language it has a title for.

## Show a list anywhere

The `entries` template function returns the published entries of a list in the current language, in their order. The list does not need to be placed on a page for this.

```gotemplate
<section>
    <h2>Frequently asked</h2>
    {{range entries "faq"}}
        <h3>{{.Title}}</h3>
        {{.Body}}
    {{end}}
    <a href="{{moduleURL "faq"}}">All questions</a>
</section>
```

## Content API

While the site's [content API](../../docs/api.md) is on (`Config.API`), `/api/v1/<key>/entries` serves the published entries as JSON, in the language of `?locale=`, or the site's default language without it. With the list `faq`:

| Endpoint | Route name | Answers with |
| --- | --- | --- |
| `/api/v1/faq/entries` | `api.faq.entries` | The published entries, in the editors' order, as a [list](../../docs/api.md#lists) |
| `/api/v1/faq/entries/{id}` | `api.faq.entry` | One entry |

Entries have no page of their own, so the list gives each entry whole: its `id`, `title` and `body_html`, its `image` as an [image object](../../docs/api.md#responses), `null` without one, its `link`, `url`, `api_url` and `updated_at`. `link` is the entry's link as a visitor follows it from the list's page: a path of the site made absolute, an anchor on the list's page, and other addresses, such as `mailto:` links, as they are; `null` without a link. `url` is the entry on the list's page, such as `https://example.com/en/faq#<id>`, and `null` while the list is on no page.

`/entries/{id}` sends a `Last-Modified`: the latest change of the entry, of the page the instance is on and of the entry's media, its image and those in its text. An entry with an image that was deleted gets none, and so does every entry while the instance is on no page; the lists get none either; see [Caching](../../docs/api.md#caching).

## Editing

Each list has its own section in the admin (`/admin/faq`; `/admin` is the default [admin path](../../docs/configuration.md#admin-path)) with its entries.

- An entry has an optional image from the media library and an optional link, shared by all languages, and per language a title and Markdown text.
- A link is a path on the site (`/contact`), an anchor (`#opening-hours`), or a web, `mailto:` or `tel:` address.
- Entries have no drafts or revisions: saving changes the entry at once, also when it is published. New entries start unpublished at the end of the list. **Publish** and **Unpublish** show or hide an entry; publishing needs a title in the site's default language.
- The move buttons on the overview set the order of the entries on the site.
- **Move to trash** moves an entry to the [trash](../../docs/editing.md#the-trash), where it can be restored or deleted for good.

Editors' rights are set per list in the groups' permission matrix.

Published entries appear in the site search by title and text once the list is placed on a page.

Links to addresses on the site in the text and links of published entries count for [Links to a page](../../docs/editing.md#links-to-a-page): the editor of a page lists the entries that link to it, and **Pages › Broken links** lists their links that lead nowhere.

## From Go

`lists.Of(app, "faq")` returns the list once the app has started:

```go
faq := lists.Of(app, "faq")

entries, err := faq.Entries(ctx, "en")
```

`Store()` gives write access for imports and seed data, through the same rules the admin uses:

```go
store := faq.Store()

entry, err := store.Save(ctx, content.Draft[lists.Fields, lists.Text]{
    Fields: lists.Fields{
        Link: "/contact",
    },
    Translations: []content.Translation[lists.Text]{
        {
            Locale: "en",
            Text: lists.Text{
                Title: "Do you deliver?",
                Body:  "Within the city, yes. [Ask us](/contact) about other places.",
            },
        },
    },
})
if err != nil {
    return err
}

err = store.Publish(ctx, entry.ID, "", "en")
```

## Related modules

- [People](../people/README.md) suits a team, with a page and contact details per person.
- [Testimonials](../testimonials/README.md) keeps quotes from customers.
- [Gallery](../gallery/README.md) shows albums of images.
