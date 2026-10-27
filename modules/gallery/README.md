# Gallery

Photo albums: a title, a Markdown description and a cover per album, with images picked from the media library.

Use one instance per gallery, for example `photos` for event pictures and `work` for a portfolio. Each has its own albums, admin section and templates. Captions and alt text are not kept in the album: they are the images' own title and alt text in the media library, per language.

## Add it to the site

```go
app.Use(
    gallery.Module("photos", "Photos"),
)
```

The key (`photos`) is the instance's name in URLs and templates and must be unique on the site. The title (`Photos`) is what editors see in the admin navigation and the page editor.

## Place it on a page

In the admin, open a page and choose the instance under **Shows on this page**. The page then serves the albums below its own address; with the page at `/photos`:

| Address | Template | Shows |
| --- | --- | --- |
| `/photos` | `photos/list.gohtml` | the published albums, in the order set in the admin |
| `/photos/<slug>` | `photos/album.gohtml` | one album with its images |

Templates are looked up by instance key first, then by module type, so `gallery/album.gohtml` serves every Gallery instance that has no design of its own. Generate starting points with every field of the data:

```sh
task templates -- photos
```

The list gets a `gallery.ListView`. Its albums have a `CoverURL` (the cover, or the first image when no cover is set) and an `ImageCount`, but no `Images`; those are only loaded on the album page. A minimal album page:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/gallery.Album */}}
<h1>{{.Title}}</h1>
{{.Description}}

{{range .Images}}
    <figure>
        <a href="{{.LargeURL}}">
            <img src="{{.URL}}" srcset="{{.Srcset}}" sizes="(max-width: 40rem) 100vw, 50vw" alt="{{.Alt}}" width="{{.Width}}" height="{{.Height}}" loading="lazy">
        </a>
        {{with .Title}}<figcaption>{{.}}</figcaption>{{end}}
    </figure>
{{end}}
```

`URL` is the image as pages show it, at most 1600 pixels wide, `Srcset` lists smaller copies, and `Width` and `Height` are the size of `URL`. `LargeURL` is a copy at most 2400 pixels wide, for the image on its own, such as in a lightbox, and `OriginalURL` is the uploaded file, for downloads. An album's cover comes the same way, as `CoverURL`, `CoverSrcset`, `CoverWidth`, `CoverHeight` and `CoverOriginalURL`, without a large copy. `image` and `srcset` make other sizes from these addresses, such as a square thumbnail: `{{image .URL "400x400 crop"}}`.

An album appears in each language it has a title and slug for. Albums are only reachable while the instance is placed on a page.

## Show albums anywhere

`albums` returns the published albums in the current language, without their images. `album` returns one album with its images, or nothing, for a carousel or a strip of photos on another page. Its slug may be the album's slug in any language.

```gotemplate
{{range albums "photos"}}
    <a href="{{.URL}}"><img src="{{image .CoverURL "300x200 crop"}}" width="300" height="200" alt=""> {{.Title}}</a>
{{end}}

{{with album "photos" "summer-fair"}}
    {{range .Images}}<img src="{{image .URL "800x500 crop"}}" width="800" height="500" alt="{{.Alt}}">{{end}}
{{end}}
```

Links point to the page the instance is placed on. An instance that only feeds carousels does not need a page; `.URL` is then empty.

## Content API

While the site's [content API](../../docs/api.md) is on (`Config.API`), `/api/v1/<key>/albums` serves the published albums as JSON, in the language of `?locale=`, or the site's default language without it. With the instance `photos`:

| Endpoint | Route name | Answers with |
| --- | --- | --- |
| `/api/v1/photos/albums` | `api.photos.albums` | The published albums, in the editors' order, as a [list](../../docs/api.md#lists) |
| `/api/v1/photos/albums?slug=<slug>` | | The album with that slug |
| `/api/v1/photos/albums/{id}` | `api.photos.album` | One album, with its description and images |

An album has its `id`, `slug` and `title`, its `cover`, the cover image or else the first image, as an [image object](../../docs/api.md#responses), `null` for an album without images, its `image_count`, its page as `url`, `null` while the instance is on no page, `api_url` and `updated_at`. `/albums/{id}` adds `description_html`, when the album has a description, and `images`: the album's images in their order, each an image object with its `id` and `title` in the media library.

`/albums/{id}` sends a `Last-Modified`: the latest change of the album, of the page the instance is on and of the album's media, its cover, its images and those in its description. An album with an image that was deleted gets none, and so does every album while the instance is on no page; the lists get none either; see [Caching](../../docs/api.md#caching).

## Editing

Each instance has its own section in the admin (`/admin/photos`; `/admin` is the default [admin path](../../docs/configuration.md#admin-path)) with the list of albums.

- An album has an optional cover image and a set of images, ticked in a list of every image in the media library, by file name. Upload new photos to the library first. Per language it has a title, a **Web address**, made from the title when left empty, and a Markdown description.
- Images keep their order in the album; newly ticked images are added at the end. **Image order** on the overview moves an album's images up and down. An album holds at most 500 images.
- Captions and alt text are edited in the media library. An image used by an album cannot be deleted from the library, also while the album is in the trash.
- Albums have no drafts or revisions: saving changes the album at once, also when it is published. New albums start unpublished at the end of the list. **Publish** and **Unpublish** show or hide an album; publishing needs a title and a web address in the site's default language.
- The move buttons on the overview set the order of the albums on the site.
- **Move to trash** moves an album to the [trash](../../docs/editing.md#the-trash), where it can be restored or deleted for good. Its web addresses are free again meanwhile; restoring is refused when another album has taken one.

Editors' rights are set per instance in the groups' permission matrix.

Published albums appear in the site search by title and description, and are listed in `sitemap.xml`.

Links to addresses on the site in the descriptions of published albums count for [Links to a page](../../docs/editing.md#links-to-a-page): the editor of a page lists the albums that link to it, and **Pages › Broken links** lists their links that lead nowhere.

The editor of a published album lists the published content that links to the album and asks before it is unpublished or moved to the trash. With the [redirects module](../redirects/README.md), editors who may create redirects also get **Unpublish and redirect**, and saving a published album with a new **Web address** redirects its old address to the new one; see [Links to an item](../../docs/editing.md#links-to-an-item).

## From Go

`gallery.Of(app, "photos")` returns the instance once the app has started:

```go
photos := gallery.Of(app, "photos")

all, err := photos.Albums(ctx, "en")

fair, err := photos.Album(ctx, "en", "summer-fair")
```

While the instance is on no page in that language, both still return the albums, with an empty `URL`.

`Store()` gives write access for imports and seed data, through the same rules the admin uses. Images come from the media library:

```go
asset, err := app.Media.Upload(ctx, "harbour.jpg", file)
if err != nil {
    return err
}

store := photos.Store()

album, err := store.Save(ctx, content.Draft[gallery.AlbumFields, gallery.AlbumText]{
    Fields: gallery.AlbumFields{
        Images: content.IDs{asset.ID},
    },
    Translations: []content.Translation[gallery.AlbumText]{
        {
            Locale: "en",
            Text: gallery.AlbumText{
                Slug:  "harbour",
                Title: "The harbour",
            },
        },
    },
})
if err != nil {
    return err
}

err = store.Publish(ctx, album.ID, "", "en")
```

## Related modules

- [Lists](../lists/README.md) keeps short entries with one image each, such as services or features.
- [Banners](../banners/README.md) shows hero slides and notices.
- [Events](../events/README.md) and [Publishing](../publishing/README.md) have one image per event or post; `album` adds a whole album to any template.
