# People

The people behind an organisation, such as staff, a team, a board or volunteers: a name, photo, contact details and biography, with a page per person.

Use one instance per set of people. Within an instance, a translated group such as "Board" or "Staff" sorts people into sections, so one instance usually covers the whole organisation.

## Add it to the site

```go
app.Use(
    people.Module("team", "Team"),
)
```

The key (`team`) is the instance's name in URLs and templates and must be unique on the site. The title (`Team`) is what editors see in the admin navigation and the page editor.

## Place it on a page

In the admin, open a page and choose the instance under **Shows on this page**. The page then serves the people below its own address; with the page at `/team`:

| Address | Template | Shows |
| --- | --- | --- |
| `/team` | `team/list.gohtml` | the published people, in the order set in the admin |
| `/team/<slug>` | `team/person.gohtml` | one person |

Templates are looked up by instance key first, then by module type, so `people/person.gohtml` serves every People instance that has no design of its own. Generate starting points with every field of the data:

```sh
task templates -- team
```

The list has everyone twice: in `.People` as one list, and in `.Groups` by group. Groups come in the order of their first person; people without a group form a group without a name. A list by group:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/people.ListView */}}
<h1>{{.Title}}</h1>

{{range .Groups}}
    <section>
        {{with .Name}}<h2>{{.}}</h2>{{end}}
        {{range .People}}
            <article>
                {{with .PhotoURL}}<img src="{{image . "300x300 crop"}}" width="300" height="300" alt="">{{end}}
                <h3><a href="{{.URL}}">{{.Name}}</a></h3>
                {{with .Role}}<p>{{.}}</p>{{end}}
            </article>
        {{end}}
    </section>
{{end}}
```

`person.gohtml` gets a single `people.Person`; its `Bio` is the rendered Markdown. `PhotoURL` is the photo as pages show it, at most 1600 pixels wide, with `PhotoSrcset`, `PhotoWidth` and `PhotoHeight` for the `img` tag, and `PhotoOriginalURL` is the uploaded file; `image` makes other sizes from it, such as the square crop above. For a phone link use `PhoneURL`, a ready `tel:` link: `html/template` blocks `tel:` in a plain string.

```gotemplate
{{if .PhotoURL}}<img src="{{.PhotoURL}}" srcset="{{.PhotoSrcset}}" sizes="15rem" width="{{.PhotoWidth}}" height="{{.PhotoHeight}}" alt="{{.Name}}">{{end}}
{{with .Email}}<a href="mailto:{{.}}">{{.}}</a>{{end}}
{{with .PhoneURL}}<a href="{{.}}">{{$.Phone}}</a>{{end}}
{{with .LinkedIn}}<a href="{{.}}">LinkedIn</a>{{end}}
```

A person appears in each language they have been saved in. Their slug is the same in every language. Person pages are only reachable while the instance is placed on a page.

## Show people anywhere

`people` returns everyone in their order, `peopleIn` the people of one group (by its name in the current language, ignoring case) and `person` one person by slug, or nothing:

```gotemplate
{{range peopleIn "team" "Board"}}
    {{.Name}}, {{.Role}}
{{end}}

{{with person "team" "jane-doe"}}
    <a href="{{.URL}}">{{.Name}}</a>, {{.Role}}
{{end}}

<a href="{{moduleURL "team"}}">Meet the team</a>
```

`URL` is empty while the instance is not placed on a page.

## Content API

While the site's [content API](../../docs/api.md) is on (`Config.API`), `/api/v1/<key>/people` serves the published people as JSON, in the language of `?locale=`, or the site's default language without it. With the instance `team`:

| Endpoint | Route name | Answers with |
| --- | --- | --- |
| `/api/v1/team/people` | `api.team.people` | The published people, in the editors' order, as a [list](../../docs/api.md#lists) |
| `/api/v1/team/people?slug=<slug>` | | The person with that slug |
| `/api/v1/team/people?group=<name>` | | The people in that group, by its name in the language of the answer, ignoring case and spaces around it; it can be combined with `?slug=` |
| `/api/v1/team/people/{id}` | `api.team.person` | One person, with their biography |

A person has their `id`, `slug` and `name`, the `group` and `role` in the language of the answer, the `photo` as an [image object](../../docs/api.md#responses), and the contact details the person's page shows: `email`, `phone`, `website` and `linkedin`, each `null` when not given. `url` is the person's page, `null` while the instance is on no page; `api_url` and `updated_at` follow. `/people/{id}` adds `bio_html`, when the person has a biography.

`/people/{id}` sends a `Last-Modified`: the latest change of the person, of the page the instance is on and of the person's media, the photo and those in the biography. A person with an image that was deleted gets none, and so do the lists; see [Caching](../../docs/api.md#caching).

## Editing

Each instance has its own section in the admin (`/admin/team`; `/admin` is the default [admin path](../../docs/configuration.md#admin-path)). The overview shows each person's name with their role and group.

- A person has a name, web address, photo from the media library, email address, phone number, website and LinkedIn address, shared by all languages; per language a group, a role and a Markdown biography.
- A phone number has 3 to 50 characters, digits, spaces, parentheses, dots, slashes and dashes, after an optional `+`. The website and LinkedIn addresses start with `http://` or `https://`.
- Leave the **Web address** empty to make it from the name: "José Núñez" becomes `jose-nunez`, and a second Jane Doe gets `jane-doe-2`. A web address made this way has at most 80 characters, plus the number of a second person with the same name, and a name without Latin letters or digits gets `person`. A web address typed by hand uses lowercase letters, digits and dashes and must be free in the instance.
- People have no drafts or revisions: saving changes the person at once, also when they are published. New people start unpublished at the end of the list. **Publish** and **Unpublish** show or hide a person; publishing needs them to be saved in the site's default language.
- The move buttons on the overview set the order on the site, and with it the order of the groups.
- **Move to trash** moves a person to the [trash](../../docs/editing.md#the-trash), where they can be restored or deleted for good. Their web address is free again meanwhile; restoring is refused when someone else has taken it.

Editors' rights are set per instance in the groups' permission matrix.

Published people appear in the site search by name, role, group and biography, and their pages are listed in `sitemap.xml`, both once the instance is placed on a page.

A person's page tells search engines who it is about with a schema.org `Person` in its structured data, which core's head tags write with `{{structuredData}}`: the name, the role as `jobTitle`, the page as `url`, the photo, the email address and phone number, the website and LinkedIn address as `sameAs`, and the site's organisation as `worksFor`. Empty fields are left out. The list of people is an `ItemList` of their pages, in the order shown. See [Search and SEO](../../docs/search-and-seo.md#structured-data).

Links to addresses on the site in the biographies and websites of published people count for [Links to a page](../../docs/editing.md#links-to-a-page): the editor of a page lists the people that link to it, and **Pages › Broken links** lists their links that lead nowhere.

The editor of a published person lists the published content that links to the person's page and asks before it is unpublished or moved to the trash. With the [redirects module](../redirects/README.md), editors who may create redirects also get **Unpublish and redirect**, and saving a published person with a new **Web address** redirects the old address to the new one; see [Links to an item](../../docs/editing.md#links-to-an-item).

## From Go

`people.Of(app, "team")` returns the instance once the app has started:

```go
team := people.Of(app, "team")

board, err := team.InGroup(ctx, "en", "Board")

jane, err := team.BySlug(ctx, "en", "jane-doe")
```

`List(ctx, locale)` returns every published person. While the instance is on no page in that language, these methods still return the people, with an empty `URL`. `Store()` gives write access for imports and seed data, through the same rules the admin uses. The store does not make slugs, so give each person one:

```go
store := team.Store()

person, err := store.Save(ctx, content.Draft[people.Fields, people.Text]{
    Fields: people.Fields{
        Slug:  "jane-doe",
        Name:  "Jane Doe",
        Email: "jane@example.com",
    },
    Translations: []content.Translation[people.Text]{
        {
            Locale: "en",
            Text: people.Text{
                Group: "Board",
                Role:  "Chair",
            },
        },
    },
})
if err != nil {
    return err
}

err = store.Publish(ctx, person.ID, "", "en")
```

## Related modules

- [Lists](../lists/README.md) suits short entries without pages of their own, such as services or steps.
- [Directory](../directory/README.md) lists organisations and places with opening hours.
