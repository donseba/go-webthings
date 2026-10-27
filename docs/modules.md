# Using modules

Modules add content and features to the site, such as news, an agenda, comments or a newsletter. You choose them in `main.go`, each as one or more named instances.

## Add instances

`app.Use` adds module instances before the app starts:

```go
err = app.Use(
    publishing.Module("news", "News"),
    publishing.Module("blog", "Blog"),
    events.Module("agenda", "Agenda"),
    comments.Module("remarks", "Comments"),
)
if err != nil {
    log.Fatal(err)
}
```

The first argument is the instance key, the second the title editors see in the admin navigation and the page editor. The key:

- starts with a lowercase letter, followed by lowercase letters, digits and dashes, at most 64 characters in all;
- is unique on the site;
- is not a name core uses itself: `pages`, `media`, `site`, `theme`, `mail`, `partials` or `webthings`, the folder of core's own templates, nor one of the admin's own sections and pages: `accounts`, `groups`, `account`, `assets`, `login`, `logout`, `setup`, `forgot-password`, `reset-password`, `trash` or `presence`;
- differs from the admin sections that other instances name after their key. Publishing `news` also adds the section `news-categories`, so another instance with that key stops `app.Start` with `admin section "news-categories" exists twice`. Directory adds `<key>-categories` too, and Newsletter `<key>-texts`.

An invalid or repeated key stops the app with an error.

The key is the instance's name everywhere: in the admin (`/admin/news` with the default [admin path](configuration.md#admin-path)), in the addresses visitors' forms post to (`/_/remarks/...`) and the names of those routes (`remarks.post`), in template folders (`news/post.gohtml`) and as the first argument of template functions (`{{range posts "news" 3}}`). It is also stored with the instance's content, its page and the groups' permissions. A renamed key starts as an empty instance; the content of the old key stays in the database until you use that key again.

### The admin navigation

`Group` lists an instance's admin sections under a heading in the admin's navigation, including the sections it adds next to its main one, such as `news-categories`:

```go
err = app.Use(
    publishing.Module("news", "News").Group("Website"),
    events.Module("agenda", "Agenda").Group("Agenda"),
    comments.Module("remarks", "Comments").Group("Visitors"),
)
```

Instances without a group are listed under **Modules**. The admin translates the group names Agenda, Content, Visitors, Website, Communication and Shop; other names are shown as written.

The navigation starts with Dashboard, Pages and Media, then lists the groups by name, each with its sections by title, and ends with **System**: Accounts, Groups, Site settings, Theme, Trash and Your account. Editors can fold a group away and the browser remembers it; the group of the current section always stays open, and a folded group shows the total of its sections' counts.

### Several instances of one module

Add a module as often as you need it, each time with its own key. Every instance has its own content, admin section and row in the permission matrix. Templates are found by key first, then by module type: `news/post.gohtml`, then `publishing/post.gohtml`. Instances can share one design or each have their own, and the starter's `task templates -- <key>` writes starting points for an instance; see [Templates](templates.md).

## Place an instance on a page

In the admin, open a page, choose the instance under **Shows on this page** and publish the page. The instance then serves the page's address and everything below it: with `news` on the page `/news`, `/en/news` lists the posts and `/en/news/<slug>` shows one.

- Only instances with public screens are offered under **Shows on this page**.
- An instance is placed on one page at most, and a page with a module has no child pages.
- An address below the page that the instance has no screen for, or a request with another method, gets the site's 404 page.
- The instance is shown in every language the page is published in.
- A page whose instance was removed from `main.go` answers with an error until it is unpublished or gets another module.

`{{moduleURL "news"}}` is the address of that page in the current language, or empty when the instance is on no page. Many modules also work without a page of their own, through their template functions. Their items are then still listed, but without links to pages that do not exist: `.URL` of `{{range posts "news" 3}}` is empty. The site search finds an instance's content only while it is on a page. [Pages](pages.md) explains the page tree.

Publishing and Events also serve Atom and RSS feeds below their page: with `news` on `/news`, `/en/news/feed.atom` and `/en/news/feed.rss` list the newest posts and `/en/news/category/<slug>/feed.atom` and `.rss` those of a category; with `agenda` on `/agenda`, `/en/agenda/feed.atom` and `.rss` list the upcoming and running events. Core's head tags link to the feeds of the page shown with `{{feedLinks}}`, so browsers and feed readers find them; see [Themes](themes.md#feeds). Feeds list at most 20 items unless **Items in feeds** in **Site settings › General** says otherwise. The modules' READMEs describe the entries, and [Feeds](module-feeds-and-links.md#feeds) for modules explains how a module of your own offers feeds.

The pages of some modules describe their content to search engines as structured data, schema.org items in JSON-LD, which core's head tags write in the page's `<head>` with `{{structuredData}}`; see [Search and SEO](search-and-seo.md#structured-data). Core describes the site, its organisation and the page itself; these modules add their content:

| Module | Structured data |
| --- | --- |
| Publishing | A post's page is an `Article`, by the site's organisation. |
| Events | An event's page is an `Event`, with its place and its start and end in the event's timezone. |
| Registrations | Gives the `Event` of a linked events instance (`ForEvents`) an `Offer` while visitors can register or every place is taken. |
| People | A person's page is a `Person` who works for the site's organisation. |
| Directory | An entry's page is a `LocalBusiness` when it has opening hours, a `Place` when it has an address or a position, and an `Organization` otherwise. |

The list pages of Publishing, Events, Gallery, People and Directory, and a directory category's page, add an `ItemList` of the item pages they show, in their order. The other bundled modules add none; their pages have core's items only. [Structured data](module-feeds-and-links.md#structured-data) for modules explains how a module of your own adds items.

## The content API

While `Config.API` turns on the [content API](api.md), Publishing, Events, Gallery, Lists, People, Testimonials, Directory and Documents also serve their published content as JSON below `/api/v1/<key>/`: with `news`, `/api/v1/news/posts` lists the posts and `/api/v1/news/categories` the categories, and with `agenda`, `/api/v1/agenda/events` lists the upcoming and running events. [Content API](api.md#endpoints) lists the endpoints of each, and the modules' READMEs describe their fields. An instance has its endpoints also while it is on no page; their `url` fields are then `null`. Registrations, Submissions, Forms, Comments, Polls, Newsletter, Banners, Redirects and Stats have none, so the API shows no data of visitors, such as sign-ups, form entries or subscribers. `/api/v1/site` lists the instances with endpoints, and [The content API](module-feeds-and-links.md#the-content-api) for modules explains how a module of your own adds some.

## Options and site features

Some modules take options after the title, or a third argument:

```go
err = app.Use(
    registrations.Module("signup", "Sign-ups", registrations.ForEvents("agenda"), registrations.Currency("GBP")),
    newsletter.Module("letters", "Newsletter", newsletter.WithProvider(provider)),
    submissions.Module("tips", "Tips", "news"),
)
```

Redirects and Stats are site features: one instance serves the whole site, so their key is optional. Without options they are named `redirects` and `stats`, titled "Redirects" and "Statistics". `Named` chooses another key and title:

```go
err = app.Use(
    redirects.Module(),
    stats.Module(stats.Named("visits", "Visits")),
)
```

The stats key is then the first argument of its template functions: `{{pageViews "visits" ""}}`.

## Bundled modules

Each module is documented in its README, also published at https://modules.gowebthings.com.

| Module | Add with | What it does |
| --- | --- | --- |
| [Publishing](../modules/publishing/README.md) | `publishing.Module(key, title)` | News and blogs: dated posts with categories, drafts, scheduled publishing and feeds |
| [Events](../modules/events/README.md) | `events.Module(key, title)` | An agenda of events with dates, a location, drafts, scheduled publishing and a feed |
| [Gallery](../modules/gallery/README.md) | `gallery.Module(key, title)` | Photo albums with images from the media library |
| [Lists](../modules/lists/README.md) | `lists.Module(key, title)` | Short ordered lists, such as an FAQ or services |
| [Testimonials](../modules/testimonials/README.md) | `testimonials.Module(key, title)` | Quotes from customers or partners |
| [People](../modules/people/README.md) | `people.Module(key, title)` | Staff, team or board, with a page per person |
| [Documents](../modules/documents/README.md) | `documents.Module(key, title)` | Downloads by category and year, with a counter |
| [Directory](../modules/directory/README.md) | `directory.Module(key, title)` | Organisations and places with opening hours, search and CSV import |
| [Banners](../modules/banners/README.md) | `banners.Module(key, title)` | Hero slides and announcement bars, with an optional time window |
| [Forms](../modules/forms/README.md) | `forms.Module(key, title)` | A form built in the admin, with an inbox and notification mail |
| [Polls](../modules/polls/README.md) | `polls.Module(key, title)` | Questions with options and one vote per visitor |
| [Comments](../modules/comments/README.md) | `comments.Module(key, title)` | Comments below any content, and guestbooks, with moderation |
| [Registrations](../modules/registrations/README.md) | `registrations.Module(key, title, options...)` | Sign-ups for events or other subjects, with places, prices and confirmation mail |
| [Submissions](../modules/submissions/README.md) | `submissions.Module(key, title, target)` | Visitors propose news; editors accept it as a draft post |
| [Newsletter](../modules/newsletter/README.md) | `newsletter.Module(key, title, options...)` | Double opt-in signup and mailings |
| [Redirects](../modules/redirects/README.md) | `redirects.Module(options...)` | Old addresses to new ones, with hits and missed paths |
| [Stats](../modules/stats/README.md) | `stats.Module(options...)` | Page views without cookies, and "most read" lists |

## Template functions and components

Modules add template functions that every template can use, with the instance key as first argument. Functions that return content give it in the page's language:

```gotemplate
{{range posts "news" 3}}<a href="{{.URL}}">{{.Title}}</a>{{end}}
{{range entries "faq"}}<h3>{{.Title}}</h3>{{.Body}}{{end}}
```

Images come as `<X>URL`, a WebP copy at most 1600 pixels wide, never the uploaded original, with `<X>Srcset`, `<X>Width` and `<X>Height` for the `img` tag and `<X>OriginalURL` for downloads, such as a post's `ImageURL` or a person's `PhotoURL`:

```gotemplate
{{range posts "news" 3}}{{if .ImageURL}}<img src="{{.ImageURL}}" srcset="{{.ImageSrcset}}" sizes="100vw" width="{{.ImageWidth}}" height="{{.ImageHeight}}" alt="">{{end}}{{end}}
```

[Media and images](media.md#image-sizes-in-templates) explains the copies and the `image` and `srcset` functions for other sizes.

Components are template functions that render a whole part of a page with default markup, such as a comment thread with its form:

```gotemplate
{{commentThread "remarks" .ID}}
{{pollBox "survey" ""}}
{{newsletterForm "letters"}}
```

A theme replaces a component's markup with `<key>/<name>.gohtml` or `<type>/<name>.gohtml`, for example `remarks/thread.gohtml`. [Template functions](template-functions.md) covers the functions core adds, and [Visitor forms](visitor-forms.md) the forms components contain.

| Module | Functions | Components |
| --- | --- | --- |
| Publishing | `posts` | |
| Events | `events` | |
| Gallery | `albums`, `album` | |
| Lists | `entries` | |
| Testimonials | `testimonials` | |
| People | `people`, `peopleIn`, `person` | |
| Documents | `documents`, `documentsIn` | |
| Directory | `directoryEntries`, `directoryEntry`, `directoryCategories` | |
| Banners | `banners`, `banner` | |
| Polls | `polls`, `poll`, `pollURL` | `pollBox` |
| Comments | `comments`, `commentCount`, `commentURL`, `commentInput`, `commentResult`, `commentsClosed` | `commentThread`, `commentForm` |
| Registrations | `registration`, `registerURL`, `registrationResult`, `registrationCode` | `registrationForm` |
| Submissions | `submitURL`, `submitInput`, `submitResult` | |
| Newsletter | `newsletter`, `newsletterURL`, `newsletterResult` | `newsletterForm` |
| Stats | `pageViews`, `popular` | |

Forms and Redirects have no template functions. Each module's README describes its functions.

Comments, Newsletter, Polls, Registrations and Submissions also hand templates their visitor forms as `.Form` (Comments through `commentInput` and Submissions through `submitInput` too), so a template that lays out a form itself renders it with go-form's functions. `form_open` writes the form tag with the spam trap and the page to return to: the element whose id is the instance key, or a poll's `.Anchor`. `form_field` renders a field by the key of its `name` tag, with its label and limits, and the fields' ids start with the instance key, such as `letters-email`:

```gotemplate
{{with newsletter "letters"}}
    <div id="letters">
        {{form_open .Form}}
            {{form_field .Form "locale" nil}}
            {{form_field .Form "email" nil}}
            <button type="submit">Subscribe</button>
        {{form_close}}
    </div>
{{end}}
```

Hidden fields, such as the newsletter's `locale`, are rendered with `form_field` too. Forms renders its whole form with `{{form_render .Form .Errors}}`. [Template functions](template-functions.md#forms) covers the `form_*` functions.

After a post with mistakes, the visitor returns to the form with `?<key>=invalid`, and the form of Comments, Newsletter, Registrations or Submissions shows their values again, with the messages at their fields; a poll shows the vote form again with the message at its options. `form_field` and `form_render` do this with `nil` as errors, so the templates need no change; inputs written by hand do not show them. See [A post with mistakes](visitor-forms.md#a-post-with-mistakes). A post that fails for another reason, such as an unreachable database, gets the theme's [500.gohtml](templates.md#500gohtml).

The e-mail fields of the bundled modules that store an address, in visitors' forms and in the admin, are checked with `validate:"mail"` (`mail.Rule`): one address mail can be sent to, with a dot in it, so `anna@localhost` gets the message of an invalid address. The `email` fields editors build in Forms are checked with `mail.Rule` too, since a reply to the notification goes to that address.

## Reach an instance from Go

Every bundled module has `Of(app, key)`, which returns the instance's typed service once the app has started, and nil before or for an unknown key. `app.Service(key)` returns the same value untyped. To use it before serving, start the app first; `ListenAndServe` does not start it again:

```go
if err := app.Start(ctx); err != nil {
    log.Fatal(err)
}

latest, err := publishing.Of(app, "news").Latest(ctx, "en", 5)
```

While an instance is on no page, its service still returns its items, with empty URLs: Publishing's `Latest`, Gallery's `Albums` and `Album`, Directory's `Entries` and `ListCategories`, People's `List` and `BySlug`, Documents' `Latest` and `InCategory` (an empty `CategoryURL`; the download links work) and Events' `Upcoming`, `Event` and `Events`.

The visitors' forms post to routes named with the instance key in front, so `app.Frontend.Route("remarks.post")` is `/_/remarks/post`, the address of the `remarks` comment form.

Modules whose content editors write offer `Store()` for imports and seed data, with the same rules as the admin. Comments are imported with `comments.Service.Add` and newsletter subscribers with `newsletter.Service.Import`.

## Modules and the output cache

The site keeps the pages visitors get in memory for a while, a minute by default (see [Caching](../README.md#caching)), so a page with module content, or a module's feed, is not rendered again for every visitor. What editors change in a module's admin section, visitors' posts to its actions and scheduled publication show at once. A visitor's post that stores nothing, such as spam, a post with mistakes or one over the throttle, keeps the pages in the cache; the bundled modules also say so for a closed comment instance or sign-up, every vote that did not count, a proposal whose body cannot be read and a newsletter signup or unsubscribe (`frontend.NothingChanged`). Changes made from Go show at once too. The bundled modules' stores tell the cache after every committed write visitors see, such as an import or seed through `Store()`: publishing and unpublishing an item, moving it to the trash and back and, for a collection, saving an item and moving it in the order. Saving a draft of revisioned content changes nothing visitors see. The services of Comments, Polls and Registrations do the same, such as `comments.Service.Add` and `registrations.Service.SaveSettings`. Newsletter subscribers and Submissions' proposals do not, as no page shows them, and Redirects works before the cache. The site's own code calls `app.Changed()` for other changes, such as data its pages show from elsewhere.

- Polls marks the polls a visitor voted on through the `wt_poll` cookie, which it registers with `Env.PersonalCookie`: the cache keeps a page apart for each value of it, so voters see their own choice and other visitors the vote form.
- Stats and Redirects wrap the site before the cache: Stats also counts the pages the cache answers, and a new redirect works at once, also for an address whose page is in the cache.
- Pages that are a visitor's own, such as a registration looked up by its code, are sent with `Cache-Control: private, no-store`, which the cache never keeps.

A module of your own whose content is a `content.Revisions` or `content.Collection` sets `Changed` in its `content.Spec` to `env.Changed` in `Setup`; the store then tells the cache after every committed write visitors see. For its other writes it calls `env.Changed()` after the commit, such as with `db.AfterWrite(ctx, err, env.Changed)`, and `db.AfterCommit(ctx, fn)` waits for the end of a transaction; see [The output cache](module-caching-and-testing.md#the-output-cache) in Caching and testing a module.

## Links in module content

The admin knows which published content links to which page, so a page's editor can list that content before the page goes offline or changes its address, and **Pages › Broken links** can list the links that lead nowhere; see [Links to a page](editing.md#links-to-a-page). Next to the text of the pages, these bundled modules report the links in their published content, and the editors of the items with an address of their own list the content that links to them:

| Module | Links in | Editor lists the links to |
| --- | --- | --- |
| Publishing | a post's text | a post, a category |
| Events | an event's text | an event |
| Gallery | an album's description | an album |
| People | a person's biography and website | a person |
| Directory | an entry's description and website | an entry; a category, with the links to its entries apart |
| Documents | a document's description and link | |
| Lists | an entry's text and link | |
| Banners | a banner's text and button address | |
| Forms | the form's introduction and success message, while it is on a page | |

The other bundled modules report none. A relative link in such content counts from the item's own address, such as a post's, or else from the page the instance is on; content without an address, such as a banner, or of an instance on no page, counts from the home page of its language. The editor of a published post, event, album, person or directory entry, and that of a published category of Publishing or Directory, asks before the item leaves its address, as a page's editor does, and offers the same redirects; see [Links to an item](editing.md#links-to-an-item). A directory category's entries leave the website with it, so its **Unpublish and redirect** redirects their addresses too, and an entry's old address leads to its new one when the category gets a new **Web address**. A module of your own reports its links with `Parts.Links` and adds this to its editor with `editor.Linked`; see [Links to pages](module-feeds-and-links.md#links-to-pages). The [redirects module](../modules/redirects/README.md) makes the redirects that the editors and the report offer, through `Parts.Redirects`.

## Modules working together

Some modules use another instance, named by its key. Add that instance as well:

```go
err = app.Use(
    events.Module("agenda", "Agenda"),
    registrations.Module("signup", "Sign-ups", registrations.ForEvents("agenda")),
    publishing.Module("news", "News"),
    submissions.Module("tips", "Tips", "news"),
)
```

- Registrations with `ForEvents` lets editors open sign-ups per event of `agenda`; sign-ups close when the event starts unless editors set a deadline.
- Submissions saves an accepted proposal as an unpublished post in `news`, through `publishing.Service.Draft`.
- Comments and Registrations attach to any content by an ID the template passes, such as `{{commentThread "remarks" .ID}}` in a post template.

Inside a module, `env.Service(key)` returns another instance's service once the app runs; see [Writing a module](writing-a-module.md).

## Integrations

Integrations with outside services live in [go-webthings-contrib](https://github.com/donseba/go-webthings-contrib), so go-webthings itself needs no extra dependencies:

| Package | Use it for |
| --- | --- |
| `mailjet` | `mailjet.Sender` sends the site's mail (`Config.Mail`); `mailjet.List` keeps a Mailjet contact list in step with a newsletter (`newsletter.WithProvider`) |
| `s3` | `s3.New` keeps uploaded media in an S3-compatible bucket (`Config.Storage`) |

```go
err = app.Use(
    newsletter.Module("letters", "Newsletter", newsletter.WithProvider(mailjet.List{
        APIKey:    os.Getenv("MAILJET_API_KEY"),
        SecretKey: os.Getenv("MAILJET_SECRET_KEY"),
        ListID:    12345,
    })),
)
```
