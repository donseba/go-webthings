# Events

An agenda of dated events with a location, an image and Markdown text, each in its own timezone, with drafts, previews and scheduled publishing.

Use one instance per agenda. Upcoming and past events are listed separately, and other modules, such as Registrations and Comments, attach to an event by its ID.

## Add it to the site

```go
app.Use(
    events.Module("agenda", "Agenda"),
)
```

The key (`agenda`) is the instance's name in URLs and templates and must be unique on the site. The title (`Agenda`) is what editors see in the admin navigation and the page editor.

## Place it on a page

In the admin, open a page and choose the instance under **Shows on this page**. The page then serves the events below its own address; with the page at `/agenda`:

| Address | Template | Shows |
| --- | --- | --- |
| `/agenda` | `agenda/list.gohtml` | the upcoming and running events, soonest first |
| `/agenda/past` | `agenda/list.gohtml` | the past events, newest first |
| `/agenda/<slug>` | `agenda/event.gohtml` | one event |

An event is past once its end has passed, or its start when it has no end. All-day events last until the end of their last day. Both lists use the same template: `.Past` tells them apart, and `.UpcomingURL` and `.PastURL` link between them. The lists are not paged.

Templates are looked up by instance key first, then by module type, so `events/event.gohtml` serves every Events instance that has no design of its own. Generate starting points with every field of the data:

```sh
task templates -- agenda
```

A minimal list:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/events.ListView */}}
<h1>{{.Title}}</h1>

{{if .Past}}
    <a href="{{.UpcomingURL}}">Upcoming events</a>
{{else}}
    <a href="{{.PastURL}}">Past events</a>
{{end}}

{{range .Events}}
    <article>
        <h2><a href="{{.URL}}">{{.Title}}</a></h2>
        <p>
            {{with .When}}
                {{if .AllDay}}{{dateRange .Start .End "full"}}
                {{else if .SameDay}}{{date .Start "full"}} · {{timeRange .Start .End}}
                {{else}}{{date .Start "full"}}, {{time .Start}} – {{date .End "full"}}, {{time .End}}{{end}}
            {{end}}
            {{with .Location}} · {{.}}{{end}}
        </p>
        {{with .Summary}}<p>{{.}}</p>{{end}}
    </article>
{{else}}
    <p>No events planned.</p>
{{end}}
```

`event.gohtml` gets a single `events.Event`; its `Body` is the rendered Markdown, with media from the library already resolved. `Start` and `End` are in the event's own timezone (`.Timezone`), and the date functions show them in that zone, also for events in `UTC`. `End` is zero when the event has none; for all-day events it is the last day, so `{{dateRange .Start .End}}` reads "25–27 October 2026".

`.When` gives the start, end and all-day switch as a `frontend.When`, which a registration's subject gives too (`registrations.Subject.When`), so one partial shows both:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/frontend.When */}}
{{if .AllDay}}{{dateRange .Start .End "full"}}
{{else if .SameDay}}{{date .Start "full"}} · {{timeRange .Start .End}}
{{else}}{{date .Start "full"}}, {{time .Start}} – {{date .End "full"}}, {{time .End}}{{end}}
```

Saved as `partials/when.gohtml`, it is `{{template "partials/when.gohtml" .When}}` in `event.gohtml`. `.SameDay` is set when the event ends on the day it starts, or has no end. The starting points from `task templates -- agenda` show events the same way, in one line, in the list and on the event's page; there an event with times is followed by its timezone, such as "Monday 16 November 2026, 09:00 – Tuesday 17 November 2026, 18:00 (Europe/Amsterdam)".

`ImageURL` is the event's image as pages show it, at most 1600 pixels wide, with `ImageSrcset`, `ImageWidth` and `ImageHeight` for the `img` tag, and `ImageOriginalURL` is the uploaded file; all are empty for an event without one. Events in a list have the same fields:

```gotemplate
{{if .ImageURL}}<img src="{{.ImageURL}}" srcset="{{.ImageSrcset}}" sizes="100vw" width="{{.ImageWidth}}" height="{{.ImageHeight}}" alt="">{{end}}
```

An event appears in each language it has a title and slug for. Events are only reachable while the instance is placed on a page.

## Show events anywhere

The `events` template function returns up to a number of the next upcoming and running events in the current language, for a home page or a sidebar; 0 or less returns all of them. Links point to the page the instance is placed on; while it is on no page, `.URL` is empty.

```gotemplate
{{range events "agenda" 3}}
    <a href="{{.URL}}">{{.Title}}</a>
    {{date .Start "EEE d MMM"}}{{if not .AllDay}}, {{time .Start}}{{end}}
{{end}}

<a href="{{moduleURL "agenda"}}">Full agenda</a>
```

## Feeds

Each instance serves Atom and RSS feeds of its upcoming and running events, soonest first, in the page's language: with the page at `/agenda`, `/agenda/feed.atom` and `/agenda/feed.rss` (routes `feed.atom` and `feed.rss`). Core's head tags link to them with `{{feedLinks}}` from every page of the instance; see [Themes](../../docs/themes.md#feeds). While the instance is on no page, the feeds get the site's 404 page.

- The feeds list as many events as the site setting **Items in feeds** says (**Site settings › General**, 20 by default). An event leaves them once it is past.
- `events.Module("agenda", "Agenda", events.WithFeedSize(50))` makes the instance's feeds list 50 events instead, whatever the setting says. The size is at most 100 (`site.MaxFeedSize`) and 0 follows the setting; the app does not start with another size.
- An entry's summary says when and where the event takes place, in the page's language and the event's timezone, such as "14 November 2026 · 20:00–22:00 · De Werf", then the event's own summary after a blank line. All-day events have no times.
- Its content is the event's Markdown with absolute addresses, its images shown as in e-mail, and the event's image is an enclosure.
- It is published when the event was created and updated when its published revision was saved. Its ID is `tag:<host>,<date created>:<key>/<event ID>`, so it stays the same when the event's slug or page changes. Set `Config.BaseURL`, so the host does not depend on the address a reader used.

Feeds are cached like pages and are not in `sitemap.xml`; a past event leaves a cached feed when the cache's time has passed. [Feeds](../../docs/module-feeds-and-links.md#feeds) for modules explains what core adds to them.

## Structured data

An event's page describes the event to search engines as a schema.org `Event`. Core's head tags write it, with the site's own items, with `{{structuredData}}`; see [Search and SEO](../../docs/search-and-seo.md#structured-data). The Event of an event at `/en/agenda/harbour-fair` that visitors can register for, one item of the page's `@graph`:

```json
{
  "@type": "Event",
  "description": "Games and food",
  "endDate": "2027-06-12T17:00:00+02:00",
  "eventAttendanceMode": "https://schema.org/OfflineEventAttendanceMode",
  "eventStatus": "https://schema.org/EventScheduled",
  "inLanguage": "en",
  "location": {"@type": "Place", "address": "Quay 1, Harbourtown", "name": "Town hall"},
  "mainEntityOfPage": {"@id": "https://example.com/en/agenda/harbour-fair"},
  "name": "Harbour fair",
  "offers": {
    "@type": "Offer",
    "availability": "https://schema.org/InStock",
    "price": "12.50",
    "priceCurrency": "EUR",
    "url": "https://example.com/en/agenda/harbour-fair",
    "validThrough": "2027-06-12T12:30:00Z"
  },
  "organizer": {"@id": "https://example.com/#organization"},
  "startDate": "2027-06-12T14:30:00+02:00",
  "url": "https://example.com/en/agenda/harbour-fair"
}
```

- `name` is the event's title and `description` its summary; `image`, for an event with an image, is that image as it is shown when the page is shared, a 1200 × 630 JPEG cut around its focal point.
- `startDate` and `endDate` are in the event's timezone, such as `2027-06-12T14:30:00+02:00`, or the day alone for an all-day event, such as `2027-06-12`. An event without an end has no `endDate`.
- `eventStatus` is always `EventScheduled`: the module has no cancelled or moved events.
- `location` is a `Place` with the event's location as `name` and its address as `address`, and `eventAttendanceMode` says it takes place there; an event without either has neither.
- `inLanguage` is the page's language, and `organizer` the site's organisation.

A [Registrations](../registrations/README.md) instance linked with `registrations.ForEvents("agenda")` adds `offers`, an `Offer`, while visitors can register for the event (`InStock`) and once every place is taken (`SoldOut`), also after registration is switched off: `price` is the adult price, `priceCurrency` the instance's currency, `url` the event's page and `validThrough` when registration closes, the deadline or else the event's start. An event without registration gets none, and so does one whose registration closed with places left.

The lists of upcoming and past events are an `ItemList` of the events they show, in their order.

## Content API

While the site's [content API](../../docs/api.md) is on (`Config.API`), each instance serves its published events as JSON below `/api/v1/<key>/`, in the language of `?locale=`, or the site's default language without it. With the instance `agenda`:

| Endpoint | Route name | Answers with |
| --- | --- | --- |
| `/api/v1/agenda/events` | `api.agenda.events` | The upcoming and running events, soonest first, as a [list](../../docs/api.md#lists) |
| `/api/v1/agenda/events?past=true` | | The past events, newest first |
| `/api/v1/agenda/events?slug=<slug>` | | The event with that slug: the `module.path` that `/api/v1/pages/resolve` gives for an event's address, without its leading slash; add `past=true` for a past event |
| `/api/v1/agenda/events/{id}` | `api.agenda.event` | One event, past or not, with its text |

An event appears in each language it has a title and slug for, as on the site, and never as a draft. `GET /api/v1/agenda/events/{id}`:

```json
{
  "id": "01a115cb-b189-7cb8-9adc-82a8952a89ef",
  "slug": "harbour-fair",
  "title": "Harbour fair",
  "summary": "Games and food",
  "body_html": "\u003cp\u003eBring \u003cem\u003efriends\u003c/em\u003e.\u003c/p\u003e\n",
  "location": "Town hall",
  "address": "Quay 1, Harbourtown",
  "start": "2027-06-12T14:30:00+02:00",
  "end": "2027-06-12T17:00:00+02:00",
  "all_day": false,
  "timezone": "Europe/Amsterdam",
  "past": false,
  "url": "https://example.com/en/agenda/harbour-fair",
  "api_url": "https://example.com/api/v1/agenda/events/01a115cb-b189-7cb8-9adc-82a8952a89ef?locale=en",
  "image": null,
  "updated_at": "2026-10-07T09:57:13.225795Z"
}
```

| Field | |
| --- | --- |
| `body_html` | The event's text as HTML, with absolute addresses and images with their `srcset`; only `/events/{id}` gives it |
| `location`, `address` | The location's name and its address, as editors wrote them |
| `start`, `end` | The start and the end in the event's timezone, with its offset; `end` is `null` for an event without one. An all-day event starts and ends at midnight of its first and its last day. |
| `all_day`, `timezone` | The **All day** switch and the event's timezone, an IANA name |
| `past` | Set once the event is over, as for the list of past events on the site |
| `url` | The event's page on the site; `null` while the instance is on no page in that language |
| `api_url` | The event in the API |
| `image` | The event's image as an [image object](../../docs/api.md#responses), `null` without one |
| `updated_at` | When the published revision was saved |

The API leaves out registrations and their places: visitors' sign-ups are personal data.

`/events/{id}` sends a `Last-Modified`: the latest change of the event, of the page the instance is on and of the event's media, and, once the event is past, when it ended, since `past` changes then. The lists send none; see [Caching](../../docs/api.md#caching).

## Editing

Each instance has its own section in the admin (`/admin/agenda`; `/admin` is the default [admin path](../../docs/configuration.md#admin-path)) with the list of events.

- An event has a start, an optional end, an **All day** switch, a timezone and an optional image from the media library; per language a title, web address, summary, Markdown text, location name and address. An empty **Web address** is made from the title when the event is saved.
- Start and end are the local time in the event's timezone, an IANA name such as `Europe/Amsterdam`: the editor shows and reads them as typed in that timezone, not in the site's timezone as other dates in the admin. A value that is not a date and time gets an error at its field. When the clocks go forward, a time they skip moves forward by the change (02:30 becomes 03:30); when they go back, a time that occurs twice is the first of the two. New events get the site's timezone from the site settings and start at the next full hour on the clocks of that timezone, also in a timezone whose offset is not whole hours, such as `Asia/Kolkata`. For all-day events only the dates count; an end on the first day makes a one-day event.
- **Save draft** creates a new revision as a draft: "Saved as a draft. Publish it to show it on the website." The published version stays online until a revision is published. Editors can preview a draft at the event's own address on the website, while the instance is placed on a page, and restore an earlier revision from the history.
- The publication box plans publishing or unpublishing for a date and time in the site's timezone. Publishing by hand cancels the plan. Events that are over stay published and move to the past list.
- A site can require a review per instance in **Site settings › Review**. An event's revision then needs a reviewer's approval before it is published or planned; see [Review before publishing](../../docs/editing.md#review-before-publishing).
- Publishing needs a title and a web address in the site's default language. The web address `past` is reserved for the list of past events.
- **Move to trash** moves an event to the [trash](../../docs/editing.md#the-trash), where it can be restored with its revisions or deleted for good. It cancels a planned publication. A published event cannot be restored while another published event uses its web address.

Editors' rights are set per instance in the groups' permission matrix.

Published events, upcoming and past, appear in the site search by title, summary, text, location and address. They are also listed in `sitemap.xml`, with `/agenda/past` when there are past events.

Links to addresses on the site in the text of published events count for [Links to a page](../../docs/editing.md#links-to-a-page): the editor of a page lists the events that link to it, and **Pages › Broken links** lists their links that lead nowhere.

The editor of a published event lists the published content that links to the event and asks before it is unpublished or moved to the trash. A revision with a new **Web address** shows the old and the new address before it is published. With the [redirects module](../redirects/README.md), editors who may create redirects also get **Unpublish and redirect**, and **Publish this revision** comes with **Redirect the old addresses to the new ones**, ticked by default; see [Links to an item](../../docs/editing.md#links-to-an-item).

## From Go

`events.Of(app, "agenda")` returns the instance once the app has started:

```go
agenda := events.Of(app, "agenda")

next, err := agenda.Upcoming(ctx, "en", 3)
```

`Event(ctx, id, locale)` returns one event and `Events(ctx, locale)` all of them, published or not, as `events.Info` with `Published`, `Past` and the public `URL`. While the instance is on no page in that language, `Upcoming`, `Event` and `Events` still return the events, with an empty `URL`. They are meant for modules that refer to events, which reach the instance with `env.Service("agenda").(*events.Service)` once the app runs.

`Store()` gives write access for imports and seed data, through the same rules the admin uses. Times are stored in UTC next to the event's timezone, and publishing takes the revision to publish:

```go
amsterdam, err := time.LoadLocation("Europe/Amsterdam")
if err != nil {
    return err
}

store := agenda.Store()

item, err := store.Save(ctx, content.Draft[events.EventFields, events.EventText]{
    Fields: events.EventFields{
        Start:    time.Date(2026, 11, 14, 20, 0, 0, 0, amsterdam).UTC(),
        Timezone: "Europe/Amsterdam",
    },
    Translations: []content.Translation[events.EventText]{
        {
            Locale: "en",
            Text: events.EventText{
                Slug:  "autumn-concert",
                Title: "Autumn concert",
            },
        },
    },
})
if err != nil {
    return err
}

err = store.Publish(ctx, item.ID, item.Revision.ID, "en")
```

## Related modules

- [Registrations](../registrations/README.md) takes sign-ups for events: link an instance with `registrations.ForEvents("agenda")` and show the form with `{{registrationForm "signup" .ID}}` in `event.gohtml`.
- [Comments](../comments/README.md) adds reactions below an event with `{{commentThread "remarks" .ID}}`.
- [Publishing](../publishing/README.md) keeps news and other dated posts.
