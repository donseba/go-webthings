# The admin of a module

This guide continues the links module of [Writing a module](writing-a-module.md) in the admin: the section in which editors write the links, screens of a module's own, deleting and the trash, and previews and review for content with revisions.

## The admin section

Package `admin/editor` is the generic admin for content stores: an overview with language tabs and a search field, an editor per language, publication and, for ordered stores, arrow buttons to move items. Its save button reads **Save** for a `content.Collection` and **Save draft** for a `content.Revisions`, whose saves are drafts; **Unpublish** asks "Take this off the website?" and **Move to trash** "Move this item to the trash?". The module supplies a [go-form](https://github.com/donseba/go-form) struct for one language and two mapping functions:

```go
type linkInput struct {
    Title       string `form:"input,text" label:"Title"       required:"true" maxLength:"200"`
    URL         string `form:"input,text" label:"Address"     required:"true" maxLength:"2000" placeholder:"https://" trim:"true"`
    Description string `form:"textarea"   label:"Description"                 maxLength:"1000"`
}

func section(env webthings.Env, service *Service) admin.Section {
    return editor.New(editor.Config[Fields, Text, linkInput]{
        ID:       env.Key,
        Title:    env.Title,
        Icon:     "link",
        NewLabel: "New link",
        Store:    service.items.In(env.Key),
        Locales:  env.Locales,
        Files:    files,
        Label: func(text Text) string {
            return text.Title
        },
        Form: func(_ context.Context, item Item, locale string) (linkInput, error) {
            text, _ := item.Text(locale)

            return linkInput{Title: text.Title, URL: item.Fields.URL, Description: text.Description}, nil
        },
        Apply: func(_ context.Context, in linkInput, draft *content.Draft[Fields, Text], locale string) error {
            draft.Fields = Fields{URL: in.URL}
            draft.Translations = content.SetText(draft.Translations, locale, Text{
                Title:       in.Title,
                Description: in.Description,
            })

            return nil
        },
        Issue: func(err error) string {
            if errors.Is(err, errURL) {
                return "Enter an address starting with https://."
            }

            return ""
        },
    })
}
```

`Form` fills the form for one language; the item is empty for a new link. `Apply` writes a submitted form into the draft, and `Issue` turns the module's errors into a message for the editor. go-form checks the tags first: `required:"true"` counts a value of only spaces as empty, and `minLength` does not count spaces around the text. An address of only spaces therefore gets go-form's "This field is required.", not the message from `Issue`. `trim:"true"` removes the white space around the address as the form is read, before `Apply` sees it. `Path` is the section's address below the admin; empty, as here, is `/` followed by `ID`, such as `/partners`. The admin itself is at `/admin` unless `Config.AdminPath` moves it; see [Configuration](configuration.md#admin-path). Other fields add buttons on the overview (`Links`, each to a `URL` or to an admin route by name in `Route`), notes per item (`Notes`), a preview on the website (`PreviewURL`; see [Previews and review](#previews-and-review)), names in the revision history (`Names: env.Auth.Names`), a count in the navigation and under **Needs attention** on the dashboard (`Badge`), an overview of the module's own instead of the list (`Overview`; `editor.Status(item, revisioned)` names an item's state there, such as "Draft" or "Published", for the template to translate), items that stay on the website by their nature, such as the home page (`Permanent`; see [Deleting and the trash](#deleting-and-the-trash)), screens of the module's own (`Routes`; see [Screens of your own](#screens-of-your-own)), notices and other changes on the edit screen of an item (`Edit`; see [Links to pages](module-feeds-and-links.md#links-to-pages)), a follow-up after an existing item is saved (`Saved`; see the same section) and deleted records of the module's own in the trash (`Trash`; see [Deleting and the trash](#deleting-and-the-trash)). `editor.MediaChoices` makes a dropdown of the media library's files by name, or of its images only. Its field, a `form.SortedSelect[string]`, needs a placeholder, such as `placeholder:"—"`, to offer no file; see [`../modules/lists/module.go`](../modules/lists/module.go).

Editor sections also feed the admin's dashboard and trash on their own: their most recently edited items, their planned publication changes, their revisions waiting for review, a "New …" quick action and their deleted items. An `admin.Section` of your own can do the same with `NewLabel` and `NewRoute`, `Recent`, `Scheduled`, `Review` and `Trash`. `NewRoute` is the name of the section's route for a new item, such as `new`, which editor sections set; route names are explained below. A name the section did not register shows no quick action. `NewPath`, a path below the admin like `Path`, such as `/partners/new`, applies only while `NewRoute` is empty. `Admin.QuickActions(r)` returns the sections' "New …" actions that the user may take, in the order of the navigation, each an `admin.QuickAction` with a `Label`, the `Section` and a `URL`, for a screen of your own that suggests what to add first, as the Pages step of the setup guide does. `Recent`, `Scheduled` and `Review` are each a `func(ctx, limit)`, returning `[]admin.RecentItem`, `[]admin.ScheduledItem` and `[]admin.ReviewItem`, whose `URL` links to the item; `admin.RouteURL(ctx, name, args...)` builds it from a route name. They run on every dashboard view, so each reads at most `limit` items and leaves deleted items out: `Recent` the newest first, `Scheduled` the earliest planned change first and `Review` the oldest request first. A content store's `Recent(ctx, limit)` does this for `Recent`. `Group` lists a section under a heading in the navigation; `webthings.Module.Group` sets it for all of a module's sections and wins over the section's own, and without either the section is listed under `admin.GroupModules`. See [Using modules](modules.md#the-admin-navigation).

`Callout`, a `func(ctx) (admin.Callout, bool)`, shows a card above the dashboard's panels that asks the user to finish something, such as the Site settings' "Finish setting up your site" with "2 of 5 steps done". An `admin.Callout` has a `Title` and a `Text`, shown as written, so translate them first, a button to `URL` with the text `Action`, a translation key such as "Continue setup", and a progress bar of `Done` of `Total`, none while `Total` is zero. The dashboard asks it for the users who may view the section, with a context that carries the signed-in user (`auth.CurrentUser`), on every view, so keep it cheap; false shows no card. The Site settings and Theme sections of core are built with `siteadmin.New(settings, options)` and `siteadmin.NewTheme(settings, options)`, which take one `siteadmin.Options` for both: `Media`, `Fixed`, `Frontend`, `Public`, `Pages`, `Modules`, `Mail` and `BaseURL`, what the settings, the theme cards and the setup guide need. The app builds both at start.

The dashboard shows at once and loads its panels after it. Each panel asks all sections at the same time, for at most eight items, and waits five seconds at most. A section that fails, panics or has not answered by then is named in the panel as one that could not be loaded; the other sections still show. `Badge` runs on every admin page, for each section in the navigation. The badges are asked for at the same time too, and one that takes longer than a second shows no count, so keep it to one cheap query. All of these get a context that ends at the timeout: pass it on to the database, so a slow query stops. Editor sections list planned changes only from stores that implement `content.Planner`, as those of a `content.Revisions` do; a store of your own that wraps one needs `Planned` too, or the dashboard shows none of its plans. [Editing](editing.md#the-dashboard) shows the dashboard as editors see it.

Every page of a section, also a screen of your own, shows in its header who else is looking at it, to the users who may view the section. The admin does this on its own. A page is its path without the query, so the language tabs of an editor are one page. A page must be the address of one of the section's routes, and forms for new items, routes named `new` or with a name ending in `-new`, such as `mailing-new`, show no one. See [Editing](editing.md#who-else-is-on-this-page).

Every admin route needs an action in its section: `admin.View`, `admin.Create`, `admin.Edit`, `admin.Publish`, `admin.Delete`, `admin.Review` or `admin.Export`. The editor asks View to look, Create for new items, Edit to save, move and submit for review, Publish to publish and plan, Delete to move items to the trash, and Review to approve and reject. Each section is a row in the groups' permission matrix, unless its `Permission` names another section whose row it shares. Only sections with `Review` offer the Review right. `admin.Export` is for downloads of personal data, such as subscribers as CSV: only sections that register a route with it offer the Export right, and the matrix shows the Export column only when one does. A user without the right gets status 403. An `admin.Section` of your own registers handlers in `Routes` with `routes.Get(pattern, action, handler)` and `routes.Post`, at patterns below the section's path, such as `/{id}`. Handlers see the full path, and `r.PathValue("id")` reads a parameter. `.Use(middleware...)` after a route adds middleware inside its permission check: with `admin.AdministratorsOnly`, every user who is not an administrator gets status 403, for changes only administrators may make, such as the site's security settings. A page that shows such settings to other users shows them read-only itself. Handlers show pages with `routes.Admin().Render`; see [`../modules/submissions/module.go`](../modules/submissions/module.go) and [Accounts and permissions](permissions.md).

Links into the admin are built from route names, never from paths, as the admin may be anywhere. `.As(name)` after `Get` or `Post` names a route `admin.<section ID>.<name>`, which `Section.RouteName(name)` and `admin.RouteName(section, name)` return. By convention a section's own page is `index`, the form for a new item `new` and an item's page `edit` or `show`; a post to the address of a named page needs no name of its own. A route or a name registered twice makes `admin.New` fail, so the app does not start. `routes.URL(name, args...)` builds the address of one of the section's routes, with name and value pairs for its parameters and `url.Values` for a query. `Admin.URL` does the same for any route by its full name, such as the admin's own `admin.dashboard` and `admin.trash`, and so does `{{route name args...}}` in admin templates, next to go-router's other functions, such as `hasRoute` and `routeOr`. Editor sections name their routes too, such as `index`, `new` and `edit`: `routes.URL("edit", "id", id)` is the editor of one link, so screens a module adds to an editor section choose other names. Outside the admin, such as in a public handler or an e-mail to staff, `env.AdminURL` does the same once the app has started. They panic on an unknown name or a missing parameter, a mistake in the code, and the request gets status 500; `Admin.TryURL` returns an error instead.

Labels and messages are written in English and translated into the admin's language (`Config.AdminLanguage`) from `translations/<locale>.po` in the section's `Files`, such as `translations/nl_NL.po` with `msgid "New link"` and `msgstr "Nieuwe link"`. Copy the header from a bundled module, such as [`../modules/lists/translations/nl_NL.po`](../modules/lists/translations/nl_NL.po). go-form's own messages, such as "This field is required.", come with go-form in English and Dutch, so a module's catalogues leave them out; a field's own message from a tag, such as `urlMsg`, is translated like a label. A site can change any text: a catalogue on disk, such as `<StateDir>/translations/nl_NL.po`, wins over the embedded ones, and an entry left empty there keeps the embedded text. That folder is optional, and the admin writes nothing to it: it does not record the texts it looks up while it serves, so keep a module's texts in the module's own catalogues. The admin finds a catalogue for its language as go-translator's language fallback does, message by message: `nl` uses `nl.po` and, for what that lacks, `nl_NL.po`. See [Languages](languages.md#the-admin-language). A test with `catalogtest.Complete` checks that the catalogues miss none of the texts of the section's forms; see [Testing](module-caching-and-testing.md#testing).

## Screens of your own

A screen of your own looks and works like the editor's when it uses the admin's shared parts. [`../modules/submissions/admin.go`](../modules/submissions/admin.go) is a list built this way.

Every list in the admin has a search field. Every admin page offers it as the go-partial model `Search`, an `admin.SearchBox` that is computed only when a template uses it. Declare it at the top of the screen's template with `{{/* @model Search github.com/donseba/go-webthings/admin.SearchBox */}}`, put `{{template "admin-search" Search}}` in the actions of the screen's header, and `{{template "admin-empty" Search}}` where the list is empty; it says that nothing matched, or that there is nothing yet. On the overview of a section with a `NewLabel`, the empty list also offers that action, such as "New link", to users who may create.

`admin.ListPage` reads the page of a list that a request asks for, with its `page` and `q` parameters. Without a search, `Load(ctx, limit, offset)` reads that page from the database: at most `limit` rows from `offset`, in the order of SQL's `LIMIT` and `OFFSET`, and whether more follow. With a search, it reads at most `admin.SearchLimit()` rows (5000), keeps those whose `Fields` match and pages the matches. A row matches when every word occurs in one of its fields, ignoring case and accents. A list that is read whole sets `All` instead of `Load`. `Read` returns the page's `Rows` and its `Pager`, which `{{template "admin-pager" .Pager}}` shows below the rows. `Size` is the number of rows per page, `admin.DefaultPageSize` (50) when zero.

This handler lists the link suggestions of [Visitor actions](module-visitor-actions.md#visitor-actions), had the module stored them. `screens` holds the service, `routes.Admin()` and the section's `routes`, which registered the list with `routes.Get("/suggestions", admin.View, list.suggestions).As("suggestions")`:

```go
func (s screens) suggestions(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    list, err := admin.ListPage[Suggestion]{
        Path: s.routes.URL("suggestions"),
        Load: s.service.Suggestions,
        Fields: func(suggestion Suggestion) []string {
            return []string{suggestion.URL, suggestion.Name}
        },
    }.Read(r)
    if err != nil {
        s.admin.Fail(w, r, err)

        return
    }

    view := SuggestionsView{Pager: list.Pager}

    for _, suggestion := range list.Rows {
        view.Rows = append(view.Rows, SuggestionRow{
            URL:  suggestion.URL,
            Name: suggestion.Name,
            Sent: admin.DateTime(ctx, suggestion.CreatedAt),
        })
    }

    s.admin.Render(w, r, admin.Page{
        Title:    "Suggestions",
        Template: "templates/suggestions.gohtml",
        Files:    files,
        Data:     view,
    })
}
```

The template sits in `files` next to the translations (`//go:embed translations templates`). `Page.Title` is translated. A title that editors typed, such as the name of the item on the screen, goes in `Page.Label` instead, which is shown as written. Editor sections search on their own, in each item's label and note.

Admin pages get the site's Content-Security-Policy when it is on, with the hash of the admin's import map, `Admin.ImportMapHash()`; see [A Content-Security-Policy of the site's own](deployment.md#a-content-security-policy-of-the-sites-own). Under **Enforce** browsers block a `style` attribute, a `<style>` element, an inline script and an event handler attribute, such as `onclick` or `hx-on`, so a screen's template has none. Style its markup with the classes of the admin's stylesheet, listed in [`../admin/doc.go`](../admin/doc.go), and with a stylesheet of the section's own: `Section.Stylesheet` names a CSS file in the section's `Files`. A bar of a chart is `<span class="bar bar-40"></span>`, with a class from `bar-0` to `bar-100` for its height in percent, and a share bar is `<progress class="meter" max="100" value="40">`. The admin's own parts work the same way. The chip of an `admin.PresenceViewer` has the class `presence-hue-<Hue>`, with `Hue` from 0 to 11. A crop preview of the media library, a `mediaadmin.CropPreview`, has its focal point in `Class`, a class of `focal-points.css` such as `fp-35-60`. `frontend.FocalPointsStylesheet()` gives that `focal-points.css` to a part of the site that serves it with its own files, as the admin does for these previews.

Filter tabs above a list, such as its statuses, come from `admin.FilterTabs(r, label, path, name, current, tabs...)` and show with `{{template "admin-tabs" .Tabs}}`. Each `admin.Tab` sets the parameter `name` to its `Value`, and `Count` with `Counted` adds a badge. The links of the tabs and the pager keep the request's other parameters, such as the search. Content in several languages gets language tabs: `admin.ContentLocale(r, config)` reads the language from the `locale` parameter, with `config` from `env.Locales.Configuration(ctx)`, and `admin.LocaleTabs(config.Enabled, locale, path, nil)` with `{{template "locale-tabs.gohtml" .Tabs}}` shows them, each named in its own language, with the code only where two languages share a name. A list built in another way uses the parts: `admin.SearchOf(r)` reads the search, its `Matches` checks one row, `admin.Filter` and `admin.Paginate` filter and page a slice, and `admin.NewPager` makes the pager.

A page with one form, such as the settings of an instance, is an `admin.FormScreen[In]`. `Show` fills the form with `Load`. `Post` validates a post, stores it with `Save` and returns to the page with "Changes saved.". Register both at one address, such as `routes.Get("/settings", admin.View, settings.Show).As("settings")` and `routes.Post("/settings", admin.Edit, settings.Post)`: the form posts to the address it is shown at, so it needs no `Action`. `Save` refuses a single field with `admin.FieldError(field, message)`, which shows at that field with status 422; translate the message first. [`../modules/comments/admin.go`](../modules/comments/admin.go) edits its settings this way.

A part of a screen that HTMX asks for on its own, such as a list a form updates, is a fragment. List it in `admin.Page.Fragments` as a `render.Fragment` with the ID of its element, its template and its data, and place it with `{{slot "ID"}}` or `{{requiredSlot "ID"}}`; for an ID the page has no fragment of, `requiredSlot` fails the render and `slot` shows nothing. A request aimed at that ID gets the fragment alone. `Dot` in place of `Data`, such as go-partial's `partial.DotFuncOf(load)`, computes the fragment's data only when it renders, and `Page.Dot` the page's, so a request for the fragment alone does not pay for the rest of the page. A `Deferred` fragment loads after the page: the page shows a placeholder that asks for the fragment as soon as the page loads, and never computes the fragment's data. `Placeholder` names a template of the page's `Files` for that placeholder and defers the fragment too. Its dot is go-partial's `slots.PlaceholderData`, and its outer element must carry `id="{{.ID}}"` and `{{.Attrs}}`, which make it ask for the fragment. The fragment's `PlaceholderData` is the placeholder's `.Data`, so one placeholder template serves several fragments. The dashboard's panels load this way, with their heading as `.Data` and "Loading" as the placeholder. See [HTMX](htmx.md#a-fragment-that-loads-later).

Ordered lists use arrow buttons. `admin.Moves(upURL, downURL)` returns the pair for one row and `{{template "admin-moves" .Moves}}` shows it; an empty URL shows that arrow disabled, at the first or the last place. Register each URL as a post route with `admin.Edit`, as the editor does. The editor hides the arrows while searching, since moving needs the whole list. [`../modules/gallery/admin.go`](../modules/gallery/admin.go) orders an album's images this way.

Notices show as toasts in the corner of the screen. After a post, `Admin.Redirect` shows one on the next page, chosen by the name of the post's route, the part after its last dot or dash: "Published." after a route named `publish` or `revision-publish`, "Unpublished." after `unpublish`, "Moved to the trash." after `delete`, "Revision restored as a new draft." after `restore`, "Publication scheduled." after `schedule`, "Schedule cancelled." after `unschedule`, and "Changes saved." after other posts. A post without a name of its own has the name of the page at its address. `Admin.RedirectNotice` shows a success message of your own, written in English, which it translates before it redirects. Other toasts are messages of go-partial's `flash` package (`github.com/donseba/go-partial/exp/flash`). `Admin.RedirectFlash(w, r, target, message)` shows one of any level after a post, such as `flash.Warn(text)`. A handler that renders a page itself adds them to the request before `Render`: `admin.WithError` an error, for a problem with the whole post, such as a file that cannot be read, and `flash.Add` one of any level. Their text is shown as given, so translate it first:

```go
r = admin.WithError(r, admin.T(r.Context(), "The file could not be read."))
```

`flash.Success` and `flash.Info` hide after five seconds; `flash.Warn` and `flash.Error` stay until the editor closes them.

A handler that refuses a request without showing a page answers with `Admin.Forbidden` (403, "You are not allowed to do this."), or with `Admin.BadRequest` (400, "This request could not be read.") for a post that no form of the screen sends. `Admin.Fail` logs an unexpected error and answers 500 with "The request failed. Please try again.". The texts are in the admin's language. An HTMX request gets the text as an error toast and leaves the screen as it is; any other request gets the text alone.

Templates translate statuses and other labels with `{{tl localizer .Status}}`. Fixed choices in a form, such as a dropdown of statuses, get `translate:"true"` on their field: go-form then translates the option labels, written in English, from the module's catalogue, with the label as the `msgid`. Labels from a `values:` tag are translated without it; choices made of the site's own data, such as categories, stay as they are. `required:"true"` works on these fields too: an empty or unknown choice counts as missing. Dates and money follow the admin's language and the site's timezone: `admin.DateTime(ctx, t)` writes "5 Oct 2026 14:05", `admin.Date(ctx, t)` "5 Oct 2026", and `admin.Format(ctx)` formats the rest, such as `admin.Format(ctx).Money(1250, "EUR")`. `admin.LanguageName("nl")` names a language in its own language ("Nederlands"), as the language tabs do, and `admin.LanguageLabel("nl")` adds the code ("Nederlands (nl)"), as the languages of the site settings do; and `admin.TimezoneChoices(selected)` is a dropdown of time zones with their offset from UTC.

A screen of your own with a form reads its post with `Admin.Bind(r, &input)`, which maps the post onto the form's struct and validates it, as the editor and `FormScreen` do. It returns the field errors, translated, for the form to show at their fields; answer them with status 422. A value it cannot read as its field's type, such as "abc" for a number, a date in another format or a choice the form does not offer, is not mapped: it comes back as a field error, in place of what validation says about that field. `Admin.MapForm(r, &input)` maps the same way without validating. Both return an error only for a post they cannot map at all, which no form of the screen sends:

```go
func (s screens) saveNote(w http.ResponseWriter, r *http.Request) {
    var input noteInput

    errs, err := s.admin.Bind(r, &input)
    if err != nil {
        s.admin.BadRequest(w, r)

        return
    }

    if len(errs) > 0 {
        s.noteForm(w, r, input, errs, http.StatusUnprocessableEntity)

        return
    }

    // Store the note, then return with s.admin.Redirect.
}
```

`noteForm` renders the screen as its GET route does, with the post and its errors in the page's data. Its template shows them with `{{form_render_localized localizer .Form .Errors}}`, where `.Form` is `Admin.Form(r, input, action, submit)`. [`../modules/comments/admin.go`](../modules/comments/admin.go) answers comments this way.

A time that editors enter in a `form:"input,datetime-local"` field has no timezone. The admin reads and shows it in the site's timezone, `Admin.Location(r)`, in a `time.Time` field, or a `*time.Time` that stays nil when the field is empty. The editor reads its form this way, and so do `Admin.Bind` and `Admin.MapForm`; go-form's own `form.MapForm` reads the time in UTC. The admin's localizer is a go-form `form.TimeLocalizer` of the same timezone, so the field shows a stored time as the editor entered it. Convert the time with `UTC()` before storing it, as [`../modules/polls/module.go`](../modules/polls/module.go) does with a poll's closing time. A time of another timezone than the site's, such as an event's start in the event's own timezone, gets `wallclock:"true"`: the field is read and shown as typed, as a time in UTC, and go-form's `form.WallTime(t, location)` turns it into the instant at which the clocks in `location` show that time, as [`../modules/events/module.go`](../modules/events/module.go) does. Visitor forms read and show date-times in the site's timezone too (`BindForm`). Date, time and month inputs hold no instant: go-form reads them in UTC and shows them without converting, so a date does not move a day.

An import takes its file in a go-form file field, a `*multipart.FileHeader` with `form:"input,file"`. Its `accept` and `maxSize` tags refuse another type or a larger file at the field, and `Section.MaxBodyBytes` raises the admin's limit of 512 KiB per request. A larger request gets status 413 with "Upload request too large.", as a toast or as text like the refusals above; see [`../modules/directory/import.go`](../modules/directory/import.go). `admin.ReadCSV` reads a file saved by a spreadsheet program: it skips a byte order mark and splits on the comma, semicolon or tab that the first line has most of. For an export, `admin.WriteCSV(w, name, header, rows)` answers with a CSV download that is never cached. It puts an apostrophe before every value that a spreadsheet would run as a formula (`admin.CSVCell`). Register the export's route with `admin.Export`.

## Deleting and the trash

Deleting moves an item to the trash. `Store.Delete` sets its `deleted_at` and `deleted_by`, and the item leaves the website, every list and the dashboard. It keeps its place, its publication and the media it uses, so `Undelete` brings it back as it was; revisioned content loses its planned publication. `Trashed` lists the deleted items, the most recently deleted first, and `Purge` removes one for good. `Collection` and `Revisions` have the same methods, with the instance as an extra argument. [Editing](editing.md#the-trash) shows the trash as editors see it.

The content engine fills the audit columns of the schema in [Schema files](writing-a-module.md#schema-files). `created_by`, `updated_by` and `deleted_by` hold the signed-in user, or `NULL` for visitors and the scheduler. Tables for records of your own need the same columns and fill them with `db.Stamp(ctx)`, which returns the time and the signed-in user; see [Databases](databases.md#audit-columns).

`Spec.Deletable` can refuse a delete with an error, for example while live items of another type still use the item. The editor shows the error through `Issue`. [`../modules/directory/directory.go`](../modules/directory/directory.go) refuses to delete a category that still has entries.

Permanent items stay on the website. Their editor offers no unpublish, no planned unpublication and no delete, and refuses them with status 422 when they are posted anyway; planning a publication stays allowed. Some items are permanent by their nature: `editor.Config.Permanent` names them, such as the home page, and `Issue` can explain `editor.ErrPermanent` for them. Administrators mark other items permanent in their editor, with **Make permanent** and **Remove permanent mark**; the mark of an item that is permanent by its nature cannot be removed. Editor sections do all this on their own. See [Editing](editing.md#permanent-items).

The app keeps the marks with `admin.NewPermanence`, in core's table `wt_permanent_items`, by section ID and item ID. A screen of your own that unpublishes or deletes items of an editor section checks them first. `Admin.Permanence()` (`e.Admin().Permanence()` in the editor's `Routes`) returns nil when the admin keeps no marks (`admin.Options.Permanence`); otherwise its `Permanent(ctx, section, id)` reports whether an item is marked, with the editor's `ID` as the section. Check `Config.Permanent` too, and refuse the change when either holds. In the editor's `Routes`, `e.RefusePermanent(w, r)` does both for the item whose ID is the route's `{id}`: for a permanent item it shows the editor with the reason, with status 422, and returns true. It also returns true when it answered an error itself, such as 404 for an unknown item, so the handler returns when it does. The content stores check neither, so imports and seeds can still unpublish and delete permanent items.

The admin's trash lists the deleted items of every section with a `Trash`. Editor sections have one. Records of your own, outside a content store, get one from `admin.RecordTrash`, and `db.SoftTable` writes the statements of their table: `Delete` moves a row to the trash, `Undelete` brings it back, `Purge` removes it for good and `Trashed` reads the trash. Had the module stored the link suggestions in a table `site_link_suggestion` of its own, with the audit columns, and kept the database in the service as `s.db`:

```go
// suggestions is the instance's part of site_link_suggestion.
func (s *Service) suggestions() db.SoftTable {
    return db.SoftTable{DB: s.db, Table: "site_link_suggestion", Scope: "instance=?", Args: []any{s.key}}
}

// trash lists the deleted suggestions in the admin's trash.
func (s *Service) trash() admin.Trash {
    return admin.RecordTrash(admin.TrashRecords[Suggestion]{
        Load: func(ctx context.Context, limit, offset int) ([]Suggestion, int, error) {
            var trashed []Suggestion

            columns := "id, url, name, deleted_at, deleted_by"
            total, err := s.suggestions().Trashed(ctx, &trashed, columns, limit, offset)

            return trashed, total, err
        },
        Fields: func(suggestion Suggestion) []string {
            return []string{suggestion.URL, suggestion.Name}
        },
        Item: func(suggestion Suggestion) admin.TrashItem {
            return admin.TrashItem{
                ID:        suggestion.ID,
                Title:     suggestion.URL,
                DeletedAt: *suggestion.DeletedAt,
                DeletedBy: suggestion.DeletedBy.String(),
            }
        },
        Undelete: func(ctx context.Context, id string) error {
            return s.suggestions().Undelete(ctx, id)
        },
        Purge: func(ctx context.Context, id string) error {
            return s.suggestions().Purge(ctx, id)
        },
    })
}
```

`Suggestion` reads `deleted_at` into a `*time.Time` and `deleted_by` into a `db.Ref`. `Load(ctx, limit, offset)`, with the arguments in a list's order, returns one page of the trash, the most recently deleted first, and how many records it holds in all; `SoftTable.Trashed` reads both. A search of the trash reads at most `admin.SearchLimit()` records and matches their `Fields`. A delete button posts to a route with `admin.Delete` that calls `s.suggestions().Delete(ctx, id)`. `Scope` limits every statement to the instance's rows. Each returns `db.ErrNotFound` for a row that is not where it should be, which the trash explains to the editor (`admin.NotInTrash`). `Undelete(ctx, id, checks...)` runs checks first, in one transaction with the change, to keep a record in the trash that may not come back. `Purge(ctx, id, steps...)` runs steps first, for what goes with the record, such as the media it uses. `TrashRecords.Reason` explains the module's own errors, in English: the editor sees the reason at that record, and the other records of the selection go ahead. A trash that needs more implements `admin.Trash` itself, with `admin.EachTrashItem` for its `Restore` and `Purge`.

Set the trash as `Trash` on an `admin.Section` of your own, or as `editor.Config.Trash` to list the records next to the deleted items of the editor's store. `admin.JoinTrash` combines several. `Admin.TrashURL(section)`, such as `routes.Admin().TrashURL(env.Key)`, links to the section's part of the trash, for a link on its overview. See [`../modules/submissions/trash.go`](../modules/submissions/trash.go) and [`../modules/forms/trash.go`](../modules/forms/trash.go). Seeing a section's trash needs `admin.View`; restoring and deleting for good need `admin.Delete`. In screens of your own, `Admin.Form` asks "Move this item to the trash?" before a form is sent to a route named `delete` or with a name ending in `-delete`, such as `mailing-delete`, and "Take this off the website?" before one is sent to a route named `unpublish` or with a name ending in `-unpublish`, as the toasts of `Admin.Redirect` follow the route name; `Admin.Confirm` sets another question, for example for a delete that cannot be undone.

A unique value, such as a slug or an e-mail address, must ignore deleted rows. Otherwise an item in the trash keeps other items from using it. Use a partial index on SQLite and PostgreSQL and a generated column on MySQL; see [Databases](databases.md#audit-columns). A content store's `Undelete` fails with an error that wraps `content.ErrDuplicateSlug`, for `errors.Is`, when another item took the item's slug or unique value meanwhile; the item then stays in the trash. For records of your own, `SoftTable.Undelete` then fails with an error that `db.IsUnique` recognises, unless a check refuses first with an error of the module's own. `content.UniqueSlug(slug, taken)` gives a new item a free slug, such as `team-2`.

## Previews and review

Both are for content in a `content.Revisions`; the links module, a collection, has neither.

Editors preview a revision at its own address on the website, with `?revision=<id>` (`frontend.PreviewParam`) added. The editor's preview button goes to the address that `editor.Config.PreviewURL` returns. `ModulePreviewURL` makes it below the instance's page, or returns `""` while the instance is on no page:

```go
PreviewURL: func(ctx context.Context, item Item, locale string) string {
    text, _ := item.Text(locale)

    return env.Frontend.ModulePreviewURL(ctx, env.Key, locale, "/"+text.Slug, item.Revision.ID)
},
```

The public handler then shows that revision instead of the published item. `frontend.PreviewRevision` returns the revision a request asks for when a signed-in editor who may view the admin section at the given address, such as its `index`, sends it, and `""` otherwise. `PreviewBySlug` returns that revision when it belongs to the content at the slug, also when it was never published, and what `PublishedBySlug` returns otherwise. `frontend.RevisionOf` as `Content.Revision` marks the page as a preview: the layout's `.Preview` and `.Revision` are set, for a banner, and the page stays out of caches and search engines. A handler for posts at `/{slug}`, kept in a `content.Revisions` as `s.posts`, with `env.AdminURL` kept as `s.adminURL`:

```go
func (s *Service) postPage(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    mount := frontend.MountFrom(ctx)
    preview := frontend.PreviewRevision(r, s.adminURL(admin.RouteName(s.key, "index")))

    item, err := s.posts.PreviewBySlug(ctx, s.key, mount.Locale, r.PathValue("slug"), preview)
    if err != nil {
        s.frontend.Fail(w, r, err)

        return
    }

    text, _ := item.Text(mount.Locale)

    s.frontend.Render(w, r, frontend.Content{
        Template: "post",
        Title:    text.Title,
        Data:     text,
        EditURL:  s.adminURL(admin.RouteName(s.key, "edit"), "id", item.ID),
        Revision: frontend.RevisionOf(r, item, preview),
    })
}
```

[`../modules/publishing/module.go`](../modules/publishing/module.go) previews posts this way.

A site can require a review before publishing, per instance in **Site settings › Review**; it is off by default. See [Editing](editing.md#review-before-publishing). Editor sections of a `content.Revisions` store get the workflow on their own: the buttons to submit, approve and reject, the Review right in the permission matrix, an e-mail to the reviewers when the site sends mail, and the revisions waiting for review under **Needs attention** on the dashboard, through `Section.Review`. The setting names the instance key, so the editor section's `ID` must be `env.Key`. While review is on, the store refuses, with `content.ErrNotApproved`, to publish or schedule a revision that is not approved, and the editor tells why. Only the admin and the scheduler apply the setting (`content.WithReview`), so imports and seeds publish as before. A store of your own that wraps a `content.Revisions` gets the workflow by implementing `content.Reviewing`, for example with `Revisions.Reviews(key)`.
