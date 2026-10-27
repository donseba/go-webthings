# Testimonials

Quotes from customers or partners, with the author's name, role and organisation and an optional photo.

Testimonials are kept in a list ordered by hand and published one by one. Show them on a page of their own, or a few on any page with a template function. Use one instance per list, for example `reviews` and `partners`.

## Add it to the site

```go
app.Use(
    testimonials.Module("reviews", "Reviews"),
)
```

The key (`reviews`) is the instance's name in URLs and templates and must be unique on the site. The title (`Reviews`) is what editors see in the admin navigation and the page editor. The module has no options.

## Place it on a page

In the admin, open a page and choose the instance under **Shows on this page**. With the page at `/reviews`:

| Address | Template | Shows |
| --- | --- | --- |
| `/reviews` | `reviews/list.gohtml` | all published testimonials, in their order |

Templates are looked up by instance key first, then by module type, so `testimonials/list.gohtml` serves every Testimonials instance that has no design of its own. Generate a starting point with every field of the data:

```sh
task templates -- reviews
```

A minimal list:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/testimonials.ListView */}}
<h1>{{.Title}}</h1>

{{range .Testimonials}}
    <figure>
        <blockquote>{{.Quote}}</blockquote>
        <figcaption>
            {{with .PhotoURL}}<img src="{{image . "96x96 crop"}}" width="96" height="96" alt="">{{end}}
            {{.Author}}{{with .Role}}, {{.}}{{end}}{{with .Organisation}}, {{.}}{{end}}
        </figcaption>
    </figure>
{{end}}
```

`.Title` is the published title of the page. The quote is plain text, not Markdown. `.PhotoURL` is the photo as pages show it, at most 1600 pixels wide, with `.PhotoSrcset`, `.PhotoWidth` and `.PhotoHeight` for an `img` tag of that size and `.PhotoOriginalURL` for the uploaded file; `image` makes the small square above from it. All are empty for a testimonial without a photo.

A testimonial appears in each language it has a quote in.

## Show testimonials anywhere

The `testimonials` template function returns the first published testimonials in the current language, in their order, up to a limit; a limit of 0 or less returns all. It works on every page, also when the instance is not placed on a page.

```gotemplate
{{range testimonials "reviews" 3}}
    <blockquote>{{.Quote}} — {{.Author}}</blockquote>
{{end}}

<a href="{{moduleURL "reviews"}}">All reviews</a>
```

To show a different choice on every visit, see `Random` below.

## Content API

While the site's [content API](../../docs/api.md) is on (`Config.API`), `/api/v1/<key>/testimonials` serves the published testimonials as JSON, in the language of `?locale=`, or the site's default language without it. With the instance `reviews`:

| Endpoint | Route name | Answers with |
| --- | --- | --- |
| `/api/v1/reviews/testimonials` | `api.reviews.testimonials` | The published testimonials, in the editors' order, as a [list](../../docs/api.md#lists) |
| `/api/v1/reviews/testimonials/{id}` | `api.reviews.testimonial` | One testimonial |

A testimonial has its `id`, the `quote` as plain text, the `author`, the `role` and `organisation` in the language of the answer, the `photo` as an [image object](../../docs/api.md#responses), `null` without one, and `url`, `api_url` and `updated_at`. Testimonials have no page of their own: `url` is the page the instance is placed on, `null` while it is on none.

`/testimonials/{id}` sends a `Last-Modified`: the latest change of the testimonial, of the page the instance is on and of its photo. A testimonial whose photo was deleted gets none, and so does every testimonial while the instance is on no page; the lists get none either; see [Caching](../../docs/api.md#caching).

## Editing

Each instance has its own section in the admin (`/admin/reviews`; `/admin` is the default [admin path](../../docs/configuration.md#admin-path)).

- A testimonial has the author's name, which is required, and a photo from the media library, shared by all languages. Per language it has the quote and the author's role and organisation.
- The order in the admin (**Move up**, **Move down**) is the order on the site and in `testimonials`.
- There are no revisions: saving changes the testimonial at once, also when it is published. **Publish** and **Unpublish** show or hide it; publishing needs a quote in the site's default language.
- **Move to trash** moves a testimonial to the [trash](../../docs/editing.md#the-trash), where it can be restored or deleted for good.
- A photo that a testimonial uses cannot be deleted from the media library, also while the testimonial is in the trash.

Editors' rights are set per instance in the groups' permission matrix.

Testimonials are not part of the site search. The page they are placed on is in `sitemap.xml` like any other page.

## From Go

`testimonials.Of(app, "reviews")` returns the instance once the app has started:

```go
reviews := testimonials.Of(app, "reviews")

all, err := reviews.List(ctx, "en")
picked, err := reviews.Random(ctx, "en", 3)
```

`Random` returns up to a number of published testimonials in random order; 0 or less returns all of them, shuffled. There is no template function for it; a site adds one through `Config.Funcs`:

```go
randomReviews := func(ctx context.Context, limit int) []testimonials.Testimonial {
    service, ok := frontend.ServiceOf[*testimonials.Service](ctx, "reviews")
    if !ok {
        return nil
    }

    list, _ := service.Random(ctx, frontend.Locale(ctx), limit)

    return list
}

app := webthings.New(webthings.Config{
    Funcs: []template.FuncMap{{"randomReviews": randomReviews}},
})
```

Templates then use `{{range randomReviews 2}}`.

`Store()` gives write access for imports and seed data, through the same rules the admin uses. New testimonials are saved unpublished:

```go
store := reviews.Store()

item, err := store.Save(ctx, content.Draft[testimonials.Fields, testimonials.Text]{
    Fields: testimonials.Fields{Author: "Alice"},
    Translations: []content.Translation[testimonials.Text]{
        {Locale: "en", Text: testimonials.Text{Quote: "They built our website in a week.", Organisation: "Acme"}},
    },
})
if err != nil {
    return err
}

err = store.Publish(ctx, item.ID, "", "en")
```

## Related modules

- [People](../people/README.md) presents your own team, with photos and roles.
- [Lists](../lists/README.md) keeps other short ordered content, such as services or features.
- [Banners](../banners/README.md) shows hero slides and notices next to them on the home page.
