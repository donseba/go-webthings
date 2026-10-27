# Media and images

Editors keep images and files in one media library, and pages and modules refer to them by ID. Templates show the images in the sizes the design needs, without editors uploading several copies.

## The media library

**Media** in the admin (`/admin/media`) lists every uploaded file, newest first, with its type, size and, for images, its dimensions, 48 files per page with **Previous** and **Next**. Its search finds files by file name, title and alternative text in every language. Upload one file at a time; the upload opens the file's own page. **Trash** shows the deleted files. The admin is at `/admin` unless `Config.AdminPath` moves it; see [Admin path](configuration.md#admin-path).

A file's page shows:

- a preview and an **Open file** link;
- for images, the **Focal point** that crops keep in view, with crop previews (see [The focal point](#the-focal-point));
- **Use in content**: the Markdown that places the file in a text, for the editor's Markdown view (see [Images and files in text](#images-and-files-in-text));
- a tab per site language with a **Title** and an **Alternative text**;
- **Move to trash**, for editors with the delete right. It moves the file to the trash: it leaves the library and the website, and can be restored. See [The trash](editing.md#the-trash).

Who may do what follows the Media row of the permission matrix: read opens the library and the text editor's image button, create uploads, edit changes titles, alternative texts and focal points, delete moves files to the trash and restores them or deletes them for good. Without create, the library has no upload form. Without edit, a file's page shows its texts read-only, with a note, and no focal point. See [Accounts and permissions](permissions.md).

Choosing an existing file in a form, such as a post's **Image**, a person's **Photo** or the site's logo under **Site settings**, needs no Media rights: the dropdown lists the library's files by name.

### Accepted files

The type is recognised from the file's content, not its name. A file is stored as `<id>.<extension>`, and its original name is kept for downloads.

| Files | Recognised by |
| --- | --- |
| JPEG, PNG and GIF images | content |
| PDF and plain UTF-8 text | content |
| ZIP, Word, Excel and PowerPoint (`.docx`, `.xlsx`, `.pptx`), OpenDocument (`.odt`, `.ods`, `.odp`) | ZIP content and the extension |
| CSV | text content and the `.csv` extension |

Other files are refused, including WebP and video, and the upload form lists the accepted types. SVG files are always refused, also without an XML declaration and whatever their name. Every file may be up to 10 MB, images up to 40 megapixels, and names up to 255 characters.

In the admin, the upload form first checks the file's extension or the type the browser names, and its size: no file, a file of another type or one over 10 MB is refused at the field. The library then checks the content, and a file it refuses, such as an SVG named `logo.txt`, gets a message with the reason.

### Titles and alternative text

Titles and alternative texts are written per language. The language tab is marked complete when an image has an alternative text, or another file a title.

The gallery module gives its templates both, in the page's language (`.Title` and `.Alt` of each image). Other modules give an image without its texts, such as a post's `.ImageURL`. `{{alt .ImageURL}}` gives the alternative text from the library in the page's language, or nothing when there is none:

```gotemplate
{{with .ImageURL}}<img src="{{.}}" alt="{{alt .}}">{{end}}
```

Write `alt=""` for a decorative image.

A changed alternative text shows at once on the server that saved it. Other servers of the site show it within a minute, and in texts within two: each keeps the library's details and its rendered texts in memory for a minute.

### The focal point

An image's focal point is the part that matters most, such as a face. Every crop keeps it as near its centre as the image allows, so a point near a corner stays near that corner. New images have it in the centre. Copies that are not cropped show the whole image and do not use it.

On the image's page, click or drag in the preview to place the marker, or select the marker and move it with the arrow keys, 1% at a time or 10% with Shift. Previews show the 1:1, 4:3, 16:9 and 3:4 crops while it moves. **Save focal point** stores it. Without JavaScript the marker does not show: type the point in **From the left (%)** and **From the top (%)**; a value that is not a number from 0 to 100 is refused at its field, and the stored point stays. Saving needs the edit right.

The point is placed on the image as browsers show it: a photo the camera stored on its side is upright. Templates crop around it with `{{image}}`, `{{focalClass}}` and `{{objectPosition}}`; see [Image sizes in templates](#image-sizes-in-templates).

### Files in use

Content records which files it uses: files placed in texts, files chosen in fields such as a post's **Image**, and the logo, favicon and social image of the site settings. A file in use cannot be deleted; the library says "This file is used by content, perhaps in the trash, and cannot be deleted."

Pages, posts and events keep their history, and every revision counts. A file that an older revision used stays in use, so that revision can still be restored. Content in the trash keeps its files in use too, so it can be restored with them. A file is free only once the page, post or other item that used it is deleted for good from the trash. The library does not list where a file is used.

## Images and files in text

Texts are stored as Markdown. In the text editor, the **Image from the media library** button lists the library's images, newest first and searchable by file name, title and alternative text in every language, ignoring case and accents (see [Editing and publishing](editing.md#writing-text)). It inserts the chosen image as `![alt](media:<id>)`, with the alternative text typed in its dialog or none. Other files are placed in the editor's Markdown view, written with `media:` and their ID, exactly as **Use in content** shows it:

```text
![Our workshop in the old harbour](media:0199b1c2-7d4e-7a51-9c3f-2e8d4b6a1f07)

Read the [annual report](media:0199b1c2-8a10-7c22-8d5e-41f0c3b9e2a6).
```

The snippet for an image uses the alternative text of the language you are editing; the snippet for another file links its name. When the page is shown, `media:<id>` becomes the file's URL. Write the reference in this exact form, so the file is recorded as in use.

JPEG and PNG images in a text are shown as module views show them (see [Image sizes in templates](#image-sizes-in-templates)): as WebP copies at most 1600 pixels wide, with a `srcset` of copies 480, 800, 1200 and 1600 pixels wide and `sizes="100vw"`. As in `{{srcset}}`, widths beyond the image's own are left out, and the image's own width is offered instead. GIFs are shown as uploaded, so an animation keeps moving. Every image gets `width` and `height` attributes with the size it is shown in, so the browser keeps its place before it loads. An image written without alternative text, such as `![](media:…)`, gets the library's alternative text in the page's language:

```html
<img src="/media/v/<signature>/<id>_1600x0.webp" alt="Our workshop in the old harbour" srcset="… 480w, … 800w, … 1200w, … 1600w" sizes="100vw" width="1600" height="1067">
```

Newsletter mails show the images of their text as JPEG or PNG copies at most 1200 pixels wide instead, which every mail program shows (see [What sends e-mail](mail.md#what-sends-e-mail)). So do the entries of the feeds of the publishing and events modules, with absolute addresses (see [Search and SEO](search-and-seo.md#feeds)).

## Image sizes in templates

Module views give each image as a copy made for pages, never as the uploaded file, which can be many megabytes. The fields are named after the image, such as a post's `ImageURL` or a person's `PhotoURL`:

| Field | What it holds |
| --- | --- |
| `<X>URL` | A WebP copy at most 1600 pixels wide, never larger than the image |
| `<X>Srcset` | `<X>URL` and the copies 480, 800 and 1200 pixels wide that are narrower, for a `srcset` attribute |
| `<X>Width`, `<X>Height` | The size of `<X>URL` in pixels, for the `width` and `height` attributes |
| `<X>OriginalURL` | The uploaded file, for a download link |

```gotemplate
{{if .ImageURL}}
    <img src="{{.ImageURL}}"
         srcset="{{.ImageSrcset}}"
         sizes="(min-width: 60rem) 50vw, 100vw"
         width="{{.ImageWidth}}" height="{{.ImageHeight}}"
         alt="{{alt .ImageURL}}">
{{end}}
```

A GIF keeps its own URL, so an animation keeps moving, and its `<X>Srcset` is empty. The gallery's images have `URL`, `Srcset`, `Width`, `Height` and `OriginalURL` in the same way, and `LargeURL`, a copy at most 2400 pixels wide for the image on its own, such as in a lightbox (see [Gallery](../modules/gallery/README.md)).

`{{image}}` makes a copy in another size. It takes a URL a module view gives, the copy or the original, or an asset ID, and a size:

```gotemplate
{{with .ImageURL}}<img src="{{image . "800x600"}}" alt="">{{end}}
```

| Size | Result |
| --- | --- |
| `"800x600"` | fits inside 800 × 600 pixels |
| `"800"` | 800 pixels wide |
| `"x600"` | 600 pixels high |
| `"800x600 crop"` | exactly 800 × 600, cut around the [focal point](#the-focal-point) |
| `"800 jpg"`, `"400x400 crop png"` | the same, as JPEG or PNG |

The default format is WebP; `jpg` (or `jpeg`) and `png` can follow the size. An image is never made larger than it is: a size beyond the original gives the original's size, and a crop larger than the image cuts the largest part with the requested proportions. A side may be at most 4000 pixels.

For files that are not images, and for sizes it cannot read, which it also reports in the log, `{{image}}` returns the URL it was given, or the file's URL for an asset ID. A URL that does not come from the media library is returned as it is.

`{{srcset}}` lists WebP copies in widths of your own for a `srcset` attribute:

```gotemplate
{{with .ImageURL}}
    <img src="{{image . "800"}}"
         srcset="{{srcset . 400 800 1600}}"
         sizes="(min-width: 60rem) 50vw, 100vw"
         alt="{{$.Title}}">
{{end}}
```

Widths beyond the image's own width are left out, and the image's own width is offered instead, so a small image is never offered as a large one. `{{srcset}}` is empty for files that are not images.

### Crops in the browser

`{{srcset}}` copies are never cropped. To show them in a box with fixed proportions, the browser crops them with `object-fit: cover`. `{{focalClass}}` with the box's proportions, as `"3:2"` or `"720x480"`, gives the image a class that places it so the browser shows about the same part as the crop copy:

```gotemplate
{{with .ImageURL}}
    <img src="{{image . "720x480 crop"}}"
         srcset="{{srcset . 360 720 1080}}"
         sizes="(max-width: 700px) 100vw, 360px"
         class="photo {{focalClass . "720x480"}}"
         alt="">
{{end}}
```

```css
.photo { aspect-ratio: 3 / 2; object-fit: cover; }
```

The classes are in `focal-points.css`, which core serves at `/_/focal-points.css` and the layout includes with `{{styles "site.css" "focal-points.css"}}`, or with `{{assetStyle "focal-points.css"}}` in a layout without `styles`, with a version and its integrity (see [styles](template-functions.md#styles)). It has one class for each position in steps of 5%, from `.fp-0-0` to `.fp-100-100`, the horizontal position first, each setting `object-position`; `{{focalClass}}` rounds both positions to the nearest step, such as `fp-35-60`. A theme with its own `assets/focal-points.css` gets that file instead (see [Focal points](themes.md#focal-points)). A class needs no `style` attribute, so it works under a Content-Security-Policy without `'unsafe-inline'`, such as the site's own (see [A Content-Security-Policy of the site's own](deployment.md#a-content-security-policy-of-the-sites-own)).

`{{objectPosition}}` gives the same position unrounded, such as `35.2% 60%`, for a `style` attribute: `style="object-position: {{objectPosition . "720x480"}}"`. Such a policy blocks that attribute, so it is for sites without one.

Without proportions, both give the focal point itself, such as `fp-25-75` or `25% 75%`: it stays in view in a box of any shape, but not centred. Files that are not images in the library give `fp-50-50` and `50% 50%`.

### The site's images

The logo, favicon and social image of the **Site settings** reach the layout as copies too (see [The layout](templates.md#the-layout)):

| Field | What it holds |
| --- | --- |
| `.Site.LogoURL` | The logo at most 600 pixels wide, never enlarged, in the format it was uploaded in: a PNG stays a lossless PNG and a JPEG a JPEG, since WebP would blur its sharp edges. A PNG no wider than 600 pixels and a GIF keep their own address |
| `.Site.FaviconURL` | The favicon as a 32 × 32 PNG, for `<link rel="icon">` |
| `.Site.AppleTouchIconURL` | The favicon as a 180 × 180 PNG, for `<link rel="apple-touch-icon">`: the icon phones put on their home screen |
| `.Site.SocialImageURL` | The social image as a 1200 × 630 JPEG, for `og:image`, or smaller with those proportions when the image is |

The two icons and the social image are cut around the [focal point](#the-focal-point). An image a module screen gives for sharing, the layout's `.Image`, such as a post's featured image, is made like the social image; an address outside the library stays as it is.

### How the copies are made

Rendering a page only writes the URLs of copies. After an upload of a JPEG or PNG, the site makes the copies pages show by default in the background, one image at a time: the WebP copy at most 1600 pixels wide, then the narrower ones of its srcset. A 3000 pixels wide photo gets the copies 1600, 1200, 800 and 480 pixels wide, a 1000 pixels wide one 1000, 800 and 480, so the first visitor of a page with a new image does not wait for them. Every other copy, such as those of `{{image}}` and `{{srcset}}`, crops, the gallery's `LargeURL`, the site's images and the copies for e-mail, is made on its first request; so are the default copies of an upload while 1024 others still wait, as in a large import. A request for a copy that is being made waits for it rather than making it again. Each copy is kept in the media storage next to the original, so later requests only read it. Its URL names the image, size and format, and for a crop the focal point:

```text
/media/v/<signature>/<id>_800x600.webp
/media/v/<signature>/<id>_400x400c.png
/media/v/<signature>/<id>_400x400c-250-750.png
```

A crop around the centre ends in `c`. Around another point, the point follows in thousandths of the width and height: here 25% from the left and 75% from the top.

The URLs are signed with a key from the site settings, so only sizes and focal points the templates ask for can be made, and every server of the site makes the same URLs. Because a URL never changes its content, copies are sent with `Cache-Control: public, max-age=31536000, immutable`. An image in the trash and its copies are not served; deleting it for good deletes its copies too.

Moving the focal point gives crops new URLs and deletes the crops made around other points. Copies that are not cropped keep their URLs. An old crop URL still gives the old crop, made again when asked for. Other servers of the site use a new point within a minute.

Resizing follows a JPEG's EXIF orientation and uses the Catmull-Rom filter. The width and height the library keeps are those of the upright image too, as browsers show it, and `{{srcset}}` uses that width. WebP copies have quality 80 and JPEG copies quality 85; JPEG has no transparency, so transparent parts become white. An animated GIF gives its first frame.

## File URLs and downloads

Module views give other files, and the uploaded originals of images (`<X>OriginalURL`), as plain URLs, such as `/media/0199b1c2-7d4e-7a51-9c3f-2e8d4b6a1f07.jpg`, and core serves them below `/media/`. Images are shown in the browser. Other files, PDFs included, are sent as downloads with their original file name. Files are cached for an hour, with an ETag, and range requests work.

Files and copies are sent with `X-Content-Type-Options: nosniff`, so a browser never takes a file for another type than it has, and `Referrer-Policy: strict-origin-when-cross-origin`, as the site's pages are. The site's framing setting does not apply to them: they have no `X-Frame-Options` or `frame-ancestors`.

For a list of downloads with a counter, by category and year, use the [documents module](../modules/documents/README.md).

In Go, `app.Media.URL(ctx, id)` returns a file's URL, or `""` for an unknown ID; module code uses `env.Frontend.MediaURL(ctx, id)`. Images that pages show come from `Image` instead (see [From Go](#from-go)).

## Storage

Uploads and their resized copies are kept in `StateDir/media` (`var/media` in the starter). Back it up together with the database; administrators can download both under **Backup** in the admin (see [Backups](databases.md#backups)). `Config.Storage` names another place:

```go
app := webthings.New(webthings.Config{
    Storage: store,
})
```

A storage implements `media.Storage` ([media.go](../media/media.go)):

```go
type Storage interface {
    Put(ctx context.Context, key string, reader io.Reader, size int64) error
    Open(ctx context.Context, key string) (io.ReadCloser, error)
    Delete(ctx context.Context, key string) error
    URL(key string) string
}
```

- Keys are file names such as `<id>.jpg` and `<id>_800x0.webp`.
- `Open` returns an error wrapping `fs.ErrNotExist` for a missing key; copies are made when it does. To serve originals at `/media/`, the reader must also be an `io.Seeker`.
- `Delete` of a missing key should succeed, as it does for the built-in storage.
- `URL` is the public address of a key, such as one on a CDN, or `""` when the site serves the file itself at `/media/`. The built-in storage, `media.NewFilesystem(dir, "")`, returns `""`. Copies are always served at `/media/v/` by the site itself, whatever the storage.
- A storage with a `Close() error` method is closed when the app closes.

`app.Media.Originals(ctx)` lists the keys of the uploaded files, those of files in the trash included and the resized copies left out, and `app.Media.Storage()` is the storage that keeps them. **Backup** in the admin reads the files this way, so its download holds them whatever the storage.

The `s3` package of go-webthings-contrib (`github.com/donseba/go-webthings-contrib/s3`) keeps the files in an S3-compatible store, such as AWS S3, DigitalOcean Spaces or MinIO. With its public URL set, pages link to the store or its CDN directly; without it, the site serves the files at `/media/` and the bucket can stay private.

When `URL` gives an absolute `http` or `https` address, such as the store's public URL, the site's Content-Security-Policy allows its origin, the scheme and host, in `img-src` and `media-src`, so pages show the files from there under the policy: `img-src 'self' data: https://media.example.com`. The origin is read once, at `Start`. A path on the site, such as that of `media.NewFilesystem(dir, "/files")`, needs nothing. See [A Content-Security-Policy of the site's own](deployment.md#a-content-security-policy-of-the-sites-own).

## From Go

Seed data and imports use the same service as the admin, once the app has started:

```go
file, err := os.Open("seed/harbour.jpg")
if err != nil {
    return err
}

defer file.Close()

asset, err := app.Media.Upload(ctx, "harbour.jpg", file)
if err != nil {
    return err
}

err = app.Media.SaveTranslation(ctx, asset.ID, media.Translation{
    Locale:  "en",
    Title:   "The old harbour",
    AltText: "Fishing boats in the old harbour at sunset",
})
```

`app.Media.Image(ctx, asset.ID, media.ImageWidth)` gives an image as module views give it: a `media.Image` with `URL`, a WebP copy at most that many pixels wide and never larger than the image, `Srcset`, `OriginalURL`, and `Width` and `Height`, the size of `URL`. A GIF and an address outside the library, such as a theme's SVG, keep their own URL; an unknown asset gives an empty `Image`. It only builds URLs: the copies are made after the upload or on their first request (see [How the copies are made](#how-the-copies-are-made)), and `app.Media.WaitPrepared(ctx)` waits until those of the images uploaded so far are stored, for tests and tools. `media.ImageWidth` (1600) is the width of module views and texts, `media.LargeImageWidth` (2400) that of the gallery's `LargeURL`, and `media.ImageWidths` (480, 800, 1200 and 1600) are the widths `Srcset` offers below `URL`. Module code uses `env.Frontend.Image(ctx, id)`, which is `Image` with `media.ImageWidth`; see [The public handler](writing-a-module.md#the-public-handler).

For e-mail, `env.Frontend.MailImage(ctx, id)` gives a copy at most `media.MailImageWidth` (1200) pixels wide, never enlarged, as a JPEG, or as a PNG for a PNG with transparency, since not every mail program shows WebP; a GIF keeps its own address. Its URL is a path on the site, which mail needs absolute. `env.Frontend.AbsoluteMarkdown(r, source)` renders a text for a feed entry this way: its images as `MailImage` gives them, with their size, and every address of the site absolute, relative links leading from the address of `r`, such as the feed's, as a feed reader resolves them (see [Feeds](module-feeds-and-links.md#feeds) for modules). `env.Frontend.MarkdownOptions(ctx, locale, base, true)` gives the options to render a text of your own this way, such as a mailing, with `base` as the address its relative links lead from. `env.Frontend.AltText(ctx, id, locale)` gives the library's alternative text in a language the code chooses, such as a mailing's; `app.Media.AltText` does the same.

Texts are rendered with `markdown.Render(source, markdown.Options{Media: …, Images: …})`. `Media` gives the URL of a file, and `Images` a `markdown.Image` for an image of the library: its `URL`, `Srcset` with `Sizes`, `Alt`, used when the Markdown gives none, and `Width` and `Height`, which become the `width` and `height` attributes; a zero size leaves them out. `Base` makes the site's own addresses absolute, for readers away from the site, such as a feed or an e-mail. It is the absolute address the text's relative links lead from, such as `https://example.com/en/`: every link or image address without a scheme is resolved from it as a browser resolves it, also one that `Media` or `Images` gives. With that `Base`, `/en/about` and `about` both become `https://example.com/en/about`, `../x` becomes `https://example.com/x`, `?page=2` becomes `https://example.com/en/?page=2`, and `//cdn.example.com/…` gets the scheme, `https://cdn.example.com/…`. Every address of an image's `srcset` is resolved the same way; its descriptors, such as `480w`, stay as they are. Addresses with a scheme, such as `https://…` and `mailto:…`, and a fragment alone, such as `#top`, stay as they are. An empty `Base`, or one that is not an absolute address, such as `/en/`, changes nothing. `markdown.AbsoluteSrcset(base, srcset)` does the same for a `srcset` outside Markdown: with the `Base` above, `/a.webp 480w, b.webp 800w` becomes `https://example.com/a.webp 480w, https://example.com/en/b.webp 800w`. `markdown.PlainMedia(source, options)` resolves the references for readers of the Markdown itself, such as the text part of an e-mail: an image becomes a link, `[alt](address)`, and a link to a file gets the file's address. With `Base`, it resolves the addresses of links, images and reference definitions the same way, except in code spans and code blocks.

`app.Media.VariantURL(ctx, asset.ID, media.Variant{Width: 800, Height: 600, Crop: true})` gives the URL of a resized copy, as `{{image}}` does. A crop is cut around the image's focal point; `Focus` names another point and needs `Crop`.

`Image`, `VariantURL`, `Srcset`, `AltText`, `Resizable`, `FocalPoint`, `ObjectPosition` and `PositionClass` take an asset ID, the URL of a file or the URL of one of its copies, so the URLs a view gives work as well as the ID. `app.Media.Resizable(ctx, ref)` reports whether copies keep what the file shows: true for JPEG and PNG, false for a GIF, which may be animated, and for other files.

The addresses come from the library's two routes, which the app mounts at `/media`: `media.asset` serves the files and `media.variant` the resized copies. `(*media.Service).RegisterRoutes(routes)` registers them on a go-router; the app does so at start. A `media.Service` of your own, made with `media.New`, needs it too: before, `VariantURL` returns an error, and `URL` gives `""` for files its storage leaves to the site.

`app.Media.SetFocalPoint(ctx, asset.ID, media.FocalPoint{X: 0.3, Y: 0.4})` sets the focal point, in fractions of the upright width and height from the top left. It returns `media.ErrInvalid` for files that are not images and for points outside the image. `app.Media.FocalPoint(ctx, asset.ID)` reads it, `app.Media.ObjectPosition(ctx, asset.ID, "4:3")` gives what `{{objectPosition}}` gives, and `app.Media.PositionClass(ctx, asset.ID, "4:3")` what `{{focalClass}}` gives. For a point and an image size of your own, `media.FocalPoint`'s `ObjectPosition(width, height, box)` and `PositionClass(width, height, box)` do the same, with the box's proportions as an `image.Point`, or a zero one for a box of any shape.

`app.Media.Version()` changes whenever a file changes in this process: its texts, its focal point, or whether it is in the library. A cache of what is made from files, such as rendered texts, compares it to know it is out of date. Changes on other servers do not change it.

## Next steps

- [Templates](templates.md) shows which data each template gets.
- [Template functions](template-functions.md) lists `image`, `srcset`, `focalClass`, `objectPosition`, `alt` and the other functions.
- [Using modules](modules.md) covers the modules that show images, such as the gallery.
