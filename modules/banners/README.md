# Banners

Hero slides, carousels and announcement bars that editors write and schedule, shown anywhere on the site through template functions.

A banner has a title, a subtitle, Markdown text, a button, an image, a tone and an optional window in which it is shown. Use one instance per place banners appear, for example `banners` for the slides on the home page and `notice` for a bar at the top of every page.

## Add it to the site

```go
app.Use(
    banners.Module("banners", "Banners"),
    banners.Module("notice", "Notices"),
)
```

The key (`banners`) is the instance's name in the admin and in the template functions and must be unique on the site. The title (`Banners`) is what editors see in the admin navigation. The module has no options.

## Show banners

Banners are not placed on a page and the instance is not offered under **Shows on this page** in the page editor. Templates show them with two functions:

- `banners` returns the active banners of an instance, in their order.
- `banner` returns the first active banner, or nothing.

A banner is active when it is published, has a title in the page's language and the current time is inside its window. The window is checked whenever a page is rendered, so a banner appears and disappears without anyone publishing it. A page the [output cache](../../docs/deployment.md#the-output-cache) keeps shows the change once it is rendered again, after a minute by default.

Slides on the home page:

```gotemplate
{{range banners "banners"}}
    <section class="slide">
        {{if .ImageURL}}<img src="{{.ImageURL}}" srcset="{{.ImageSrcset}}" sizes="100vw" width="{{.ImageWidth}}" height="{{.ImageHeight}}" alt="">{{end}}
        <h2>{{.Title}}</h2>
        <p>{{.Subtitle}}</p>
        {{.Text}}
        {{if .ButtonURL}}<a class="button" href="{{.ButtonURL}}">{{.ButtonLabel}}</a>{{end}}
    </section>
{{end}}
```

A notice bar in the layout:

```gotemplate
{{with banner "notice"}}
    <div class="notice notice-{{.Tone}}" role="status">
        <strong>{{.Title}}</strong> {{.Text}}
        {{if not .Ends.IsZero}}Until {{date .Ends}}.{{end}}
    </div>
{{end}}
```

`.Text` is the rendered Markdown, with media from the library resolved. `.ImageURL` is the image as pages show it, at most 1600 pixels wide, with `.ImageSrcset`, `.ImageWidth` and `.ImageHeight` for the `img` tag; `.ImageOriginalURL` is the uploaded file. For wider screens, `{{srcset .ImageURL 800 1600 2400}}` offers larger copies (see [Media and images](../../docs/media.md#image-sizes-in-templates)). `.ImageURL` and `.ButtonURL` are empty when the banner has none. `.Tone` is `info`, `success` or `warning` (`banners.ToneInfo`, `ToneSuccess`, `ToneWarning`), meant as a CSS class. `.Starts` and `.Ends` are the window, zero when that side is open; `date` shows them in the site's timezone.

## Editing

Each instance has its own section in the admin (`/admin/banners`; `/admin` is the default [admin path](../../docs/configuration.md#admin-path)).

- Per language a banner has a title, a subtitle, Markdown text and a button label and address, so each language can link to its own page. The address is a path on this site, such as `/en/contact`, an anchor on the page, such as `#opening-hours`, a full `http(s)` URL, or a `mailto:` or `tel:` link; other kinds, such as `javascript:`, and addresses with spaces in them are refused. Spaces around the title, subtitle, button label and address are removed.
- The image (from the media library), the tone (**Information**, **Success** or **Warning**) and the window are shared by all languages.
- **Show from** and **Show until** are entered in the site's timezone. An empty **Show from** shows the banner as soon as it is published, an empty **Show until** until it is unpublished. The overview marks banners outside their window as **Upcoming** or **Expired**.
- The order in the admin (**Move up**, **Move down**) is the order of `banners`, and decides which banner `banner` returns.
- There are no revisions: saving changes the banner at once, also when it is published. **Publish** and **Unpublish** show or hide it; publishing needs a title in the site's default language.
- **Move to trash** moves a banner to the [trash](../../docs/editing.md#the-trash), where it can be restored or deleted for good.

Editors' rights are set per instance in the groups' permission matrix.

Banners are not part of the site search or `sitemap.xml`.

Links to addresses on the site in the text and button addresses of published banners count for [Links to a page](../../docs/editing.md#links-to-a-page): the editor of a page lists the banners that link to it, and **Pages › Broken links** lists their links that lead nowhere. A banner has no address of its own, so its relative links count from the home page of its language.

## From Go

`banners.Of(app, "banners")` returns the instance once the app has started:

```go
slides := banners.Of(app, "banners")

active, err := slides.Active(ctx, "en")
```

`Store()` gives write access for imports and seed data, through the same rules the admin uses. A banner needs a tone; the window is in UTC, with `nil` for an open side. New banners are saved unpublished:

```go
ends := time.Date(2026, 12, 24, 17, 0, 0, 0, time.UTC)
store := banners.Of(app, "notice").Store()

item, err := store.Save(ctx, content.Draft[banners.Fields, banners.Text]{
    Fields: banners.Fields{Tone: banners.ToneWarning, Ends: &ends},
    Translations: []content.Translation[banners.Text]{
        {Locale: "en", Text: banners.Text{Title: "Closed on Christmas Eve from 17:00"}},
    },
})
if err != nil {
    return err
}

err = store.Publish(ctx, item.ID, "", "en")
```

## Related modules

- [Gallery](../gallery/README.md) shows the photos of an album, for example as a carousel.
- [Testimonials](../testimonials/README.md) adds quotes from customers to the same home page.
- [Lists](../lists/README.md) keeps ordered blocks such as services or features.
