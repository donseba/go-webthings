# Registrations

Sign-ups for events or other subjects: places, prices and a deadline, a confirmation e-mail with a personal code, and a page where visitors look up or cancel their registration.

An instance is usually linked to an Events instance, so editors open registration per event. Without that link, editors add their own subjects, such as a course, by ID. The module takes no payments: it shows the editors' payment link, and editors mark registrations as paid.

## Add it to the site

```go
app.Use(
    events.Module("agenda", "Agenda"),
    registrations.Module("signup", "Sign-ups", registrations.ForEvents("agenda")),
)
```

The key (`signup`) is used in the admin, in form actions and in the template functions, and must be unique on the site. The title (`Sign-ups`) is what editors see in the admin and the page editor.

- `ForEvents("agenda")` links the instance to that Events instance. Editors pick its events in the admin, e-mail names the event and when it takes place, and registration closes when the event starts unless editors set a deadline. Add the Events instance as well.
- `Currency("GBP")` sets the currency of the prices; the default is `EUR`. It must be an ISO 4217 code, or the app does not start.

Without `ForEvents`, editors add subjects in the admin with an ID (letters, digits, `-`, `_`, `.` and `:`, up to 64 characters) and a name. Templates pass that ID wherever they would pass an event's `.ID`.

With `ForEvents`, an event's page also tells search engines about its registration. The Events module gives the page a schema.org `Event` in its structured data, which core's head tags write with `{{structuredData}}`, and this module adds an `Offer` to it as `offers`: the price per adult, such as `"12.50"`, or `"0.00"` when it is free, the currency, the event's page as `url`, `InStock` while visitors can register or `SoldOut` when every place is taken, and as `validThrough` the time registration closes, the event's start unless editors set a deadline. An event without registration settings, and registration that is switched off or past its deadline while places are left, give no offer. See [Search and SEO](../../docs/search-and-seo.md#structured-data).

## Show the sign-up form

The `registrationForm` component renders the sign-up box of one subject. On an event page (`agenda/event.gohtml` or `events/event.gohtml`):

```gotemplate
{{registrationForm "signup" .ID}}
```

It shows the prices, the places left, the deadline, the result of the visitor's last post and the form, or a notice when the subject is full or closed. When the instance is placed on a page, it also links to the lookup page. A subject without registration settings renders nothing, so the line can stay in the template of every event.

A theme changes the markup with `signup/form.gohtml` for this instance, or `registrations/form.gohtml` for every instance. Generate the default to start from, together with the other templates and e-mails of the instance:

```sh
task templates -- signup
```

The default uses `wt-registration` classes with the shared `wt-form`, `wt-notice` and `wt-button`, laid out by the optional stylesheet `webthings.css`, which the layout links with `{{styles "webthings.css" "site.css"}}` (see [The base stylesheet](../../docs/themes.md#the-base-stylesheet)). Its texts go through `tl`, so the site's translations apply.

To write the form in the template itself, `registration` returns the subject's availability in the page's language:

```gotemplate
{{with registration "signup" .ID}}
    <section id="signup">
        {{with .Notes}}<div class="notes">{{.}}</div>{{end}}
        {{if .Open}}
            {{form_open .Form}}
                {{form_field .Form "subject" nil}}
                {{form_field .Form "name" nil}}
                {{form_field .Form "email" nil}}
                {{form_field .Form "adults" nil}}
                {{if .AllowChildren}}{{form_field .Form "children" nil}}{{end}}
                {{form_field .Form "notes" nil}}
                <button>Sign up</button>
            {{form_close}}
            {{if .Capacity}}<p>{{.Left}} places left</p>{{end}}
        {{else if .Full}}<p>This event is full.</p>
        {{else if .Configured}}<p>Registration is closed.</p>{{end}}
    </section>
{{end}}
```

`Configured` is set once editors saved settings for the subject. `Open` is set while registration is switched on, the deadline has not passed and places are left. Prices are amounts in cents: `{{money .Prices.Adult .Currency}}` shows them in the page's language. `Notes` are the editors' notes for registrants, rendered from Markdown with their own paragraphs, so they go in a `<div>` rather than a `<p>`.

`.Form` is the sign-up form: `form_open` writes the form tag, posting to `.Action`, with the spam trap and the page to return to, the element whose id is the instance key. `form_field` renders one of its fields by its key, with its label and limits, translated with the site's translations and in the site's form theme; the ids start with the key, such as `signup-name`. `subject` is a hidden field with the subject's ID, adults start at 1, and while places are limited adults and children each go up to `.Left`, at most 50. See [Template functions](../../docs/template-functions.md#forms). Inputs written by hand with the field names work too.

## Visitors

The form posts to `/_/signup/register` (`.Action`, or `{{registerURL "signup"}}` anywhere) with the fields of `registrations.SignupInput`, plus the spam trap and the page to return to that `form_open` adds.

| Field | |
| --- | --- |
| `subject` | the subject ID, such as the event's `.ID` (required) |
| `name`, `email` | required; the name on one line |
| `adults` | 1 when missing or empty |
| `children` | 0 when missing; refused unless editors allow children |
| `notes` | optional remarks, up to 2000 characters |

One sign-up takes at most 50 places, and sign-ups that arrive at the same moment never exceed the capacity. The registration keeps the language of the page the form was on.

After signing up, the visitor gets a confirmation e-mail with a code such as `K7Q4-9M2X`. When the instance is placed on a page, the visitor then lands on the registration's own page with `?signup=registered`; otherwise back on the form's page with `?signup=registered&code=K7Q4-9M2X`. Other results return to the form's page:

| `?signup=` | Meaning |
| --- | --- |
| `full` | not enough places left |
| `closed` | registration is not open |
| `invalid` | a field is missing or wrong, such as a number of places that is not a number |
| `throttled` | too many posts: a visitor may send 10 sign-ups or cancellations per 10 minutes |

With `invalid` or `full`, the form shows the visitor's answers again, with a message in the page's language at each field to correct. The component and `form_field` do this without a change to the template; inputs written by hand do not. Of several sign-up forms on one page, only the one for the subject the visitor chose shows them. See [A post with mistakes](../../docs/visitor-forms.md#a-post-with-mistakes). A sign-up or cancellation that fails for another reason, such as an unreachable database, gets the site's error page, [500.gohtml](../../docs/templates.md#500gohtml).

`{{registrationResult "signup"}}` reads the result and `{{registrationCode "signup"}}` the code; the component shows both. Posts that fill in the spam trap are answered with `registered` and stored nowhere. Visitors are told apart by IP address, so list a reverse proxy in `Config.TrustedProxies`. A post that stores nothing, spam, `invalid`, `full`, `closed` or `throttled`, leaves the pages in the site's output cache as they are; see [Modules and the output cache](../../docs/modules.md#modules-and-the-output-cache).

## The registration page

Place the instance on a page to let visitors look up and cancel their registration. With the page at `/en/registration`:

| Address | Template | Shows |
| --- | --- | --- |
| `/en/registration` | `signup/lookup.gohtml` | a form asking for the code |
| `/en/registration/<code>` | `signup/registration.gohtml` | one registration with its cancel form |

`registrations/<name>.gohtml` serves every instance without a design of its own. The lookup page gets a `registrations.LookupView`. Its `.Form` is the form of `registrations.LookupInput`, sent to `.Action` with method GET as `?code=`, with the code field's id `registration-code`:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/registrations.LookupView */}}
<h1>{{.Title}}</h1>
{{if .NotFound}}<p>No registration has the code {{.Code}}.</p>{{end}}

{{form_open .Form}}
    {{form_field .Form "code" nil}}
    <button>Look up</button>
{{form_close}}
```

A code typed in lowercase, with spaces or without the dash lands on the right address. Every answer below the page, "not found" for an unknown code included, is sent with `Cache-Control: private, no-store` and `X-Robots-Tag: noindex`, so neither [Stats](../stats/README.md) nor the missed paths of [Redirects](../redirects/README.md) record a code. Cancelling needs the code and the e-mail address used to sign up, the fields of `registrations.CancelInput`:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/registrations.RegistrationView */}}
<h1>{{.Subject.Title}}</h1>
<p>{{.Code}} · {{.Name}} · {{.Adults}} adults{{if .Total}} · {{money .Total .Currency}}{{end}}</p>
{{with .PaymentLink}}<p>Please pay using: {{.}}</p>{{end}}
{{with .Info}}<div class="notes">{{.}}</div>{{end}}

<section id="{{.Key}}">
    {{if eq .Result "cancelled"}}<p>Your registration is cancelled.</p>{{end}}
    {{if eq .Result "invalid"}}<p>That e-mail address does not match this registration.</p>{{end}}
    {{if not .Cancelled}}
        {{form_open .Form}}
            {{form_field .Form "code" nil}}
            {{form_field .Form "email" nil}}
            <button>Cancel registration</button>
        {{form_close}}
    {{end}}
</section>
```

Here `.Notes` are the visitor's own remarks, as plain text, and `.Info` the editors' notes for registrants, rendered from Markdown. `.Subject.When` is when the event takes place, a `frontend.When` as an event's `.When` gives it, so the partial of the [Events](../events/README.md) README shows it too; it is zero for a subject that is no event (`.Subject.When.IsZero`). The starting point from `task templates -- signup` shows it as that partial does: an event over several days with a time as "date, time – date, time", such as "Monday 16 November 2026, 09:00 – Tuesday 17 November 2026, 18:00". `.Form` is the cancel form, with the code as a hidden field; it returns the visitor to the element whose id is `.Key`, the instance key. After `invalid`, it shows the address the visitor typed again, as the sign-up form does.

Cancelling frees the places and sends a cancellation e-mail. An unpaid registration gets the payment status cancelled; a paid one stays paid, so it can be refunded.

## In the admin

Each instance has its own section (`/admin/signup`; `/admin` is the default [admin path](../../docs/configuration.md#admin-path)). The overview lists the subjects with their status (open, closed or full), registrations and places taken. With `ForEvents` it lists every event of the agenda; without it, **New subject** adds one.

A subject's **Settings** hold:

- **Open for registration**, the number of **Places** (adults and children together; 0 is unlimited) and **Registration closes**, in the site's timezone.
- **Price per adult** and, when **Children can be registered**, **Price per child**, typed as `12.50` or `12,50`.
- Per language, the **Notes for registrants** in Markdown, shown with the form, on the registration page and in the confirmation, and a **Payment link** (a payment URL or bank details), shown while payment is pending.

The subject's page lists its registrations, cancelled ones included, with the totals and **Export CSV**. A registration with a total starts as pending. Editors change its payment status, add admin notes, cancel it or move it to the trash; cancelling in the admin sends no e-mail. **Move to trash** moves a registration to the [trash](../../docs/editing.md#the-trash) and frees its places; a restored registration keeps its code, and restoring is refused when too few places are left by then. Editors' rights are set per instance in the groups' permission matrix; **Export CSV** needs the export right (see [Groups and the matrix](../../docs/permissions.md#groups-and-the-matrix)). In the export, a value that starts with `=`, `+`, `-` or `@` gets a leading apostrophe, so a spreadsheet shows it as text instead of running it as a formula.

## E-mail

Two mail templates, sent to the visitor in the language of the page they signed up on:

- `confirmation`, after signing up: the code, the event's date and time, the places, the total, the payment link, the notes for registrants as the Markdown editors wrote (`.Info`), and a link to the registration page when the instance is on a page.
- `cancellation`, after cancelling on the site.

The confirmation shows when the event takes place as the starting point of the registration page does, so an event over several days with a time as "date, time – date, time". Replies go to the site's e-mail address from Site settings. A theme replaces the texts with `mail/signup/<name>.gohtml` or `mail/registrations/<name>.gohtml`. A mail that cannot be sent is logged; the registration stands.

## From Go

`registrations.Of(app, "signup")` returns the instance once the app has started, for example to open registration for an event from seed data:

```go
signup := registrations.Of(app, "signup")

err := signup.SaveSettings(ctx, registrations.Settings{
    SubjectID:  eventID,
    Open:       true,
    Capacity:   40,
    AdultPrice: 1250, // 12.50
    Texts:      []registrations.SubjectText{{Locale: "en", Notes: "Doors open at **19:30**."}},
})
```

`Notes` are Markdown, as editors write them in the admin.

`Availability`, `Registrations` and `Summaries` read the sign-ups, and `ByCode` finds one by its code. `Register` signs someone up with the same checks as the form, but sends no e-mail, and `Cancel` cancels a registration, freeing its places. `Delete`, `Undelete` and `Purge` move a registration to the trash, bring it back or remove it for good. What `SaveSettings`, `Register`, `Cancel`, `Delete` and `Undelete` change, such as the places left, shows on the site at once, also on pages in the site's output cache; see [Modules and the output cache](../../docs/modules.md#modules-and-the-output-cache).

## Related modules

- [Events](../events/README.md) holds the events visitors sign up for.
- [Forms](../forms/README.md) collects other questions from visitors, with an inbox.
