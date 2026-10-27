# Forms

Contact forms, quote requests and other forms that editors build in the admin, with an inbox for the answers and an optional e-mail for each one.

Each instance holds one form. Add an instance per form: a site can have `contact` and `quote` side by side, each with its own fields, inbox and template.

## Add it to the site

```go
app.Use(
    forms.Module("contact", "Contact"),
    forms.Module("quote", "Quote request"),
)
```

The key (`contact`) is the instance's name in URLs and templates and must be unique on the site. The title (`Contact`) is what editors see in the admin navigation and the page editor.

## Build the form

In the admin, open the instance (`/admin/contact`; `/admin` is the default [admin path](../../docs/configuration.md#admin-path)) and choose **New form**. An instance has one form, so the button disappears once it exists. The form has a title, an introduction and a success message (both Markdown), its fields, and an optional **Notification e-mail**.

Fields are written as text, one per line: `name | type | label | placeholder | help`. A `*` after the name makes the field required; placeholder and help are optional. The options of a select follow on their own lines, starting with `-`:

```text
name* | text | Name
email* | email | E-mail | you@example.com
topic | select | Topic
- sales | Sales
- support | Support
message* | textarea | Message | | Tell us what you need.
terms* | checkbox | I agree to the privacy statement
```

- Types: `text`, `email`, `textarea`, `select`, `checkbox`, `number` and `date`.
- Names use lowercase letters, digits and underscores, start with a letter, and are unique. `website` is taken by the spam trap.
- A form has at most 50 fields, a select at most 100 options.
- Names, types and options are shared by all languages; labels, placeholders, help and option labels belong to the language being edited. A field without a label in the visitor's language shows its name.

Publish the form when it is ready. It is shown in each language that has a title, a success message and field labels.

While the form is on a page, links to addresses on the site in its introduction and success message count for [Links to a page](../../docs/editing.md#links-to-a-page): the editor of a page lists the form when it links there, and **Pages › Broken links** lists its links that lead nowhere.

## Place it on a page

In the admin, open a page and choose the instance under **Shows on this page**. The page shows the form at its own address and receives it there too, with `contact/form.gohtml`, or `forms/form.gohtml` for every form without a design of its own. Generate a complete working starting point, with every kind of field:

```sh
task templates -- contact
```

A minimal template:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/forms.FormView */}}
<h1>{{.Title}}</h1>

{{if .Sent}}
    {{.Success}}
{{else}}
    {{.Intro}}

    {{if .Throttled}}<p>You sent this form several times already. Please try again later.</p>{{end}}
    {{if .Invalid}}<p>Please check the marked fields.</p>{{end}}

    {{form_render .Form .Errors}}
{{end}}
```

`.Form` is the form as the editor built it, in the page's language. `form_render` renders all of it with go-form: each field with its label, placeholder, help text and limits, the visitor's answers and the errors of a post at their fields, the spam trap and a **Send** button, translated with the site's translations and in the site's form theme. The fields' ids start with the instance key, such as `contact-email`.

To lay out the fields yourself, write the form with `form_open`, `form_field` per name in `.Fields` and `form_close`:

```gotemplate
{{form_open .Form}}
    {{range .Fields}}
        {{form_field $.Form . $.Errors}}
    {{end}}
    <button type="submit">Send</button>
{{form_close}}
```

`form_field` renders a field by its name, `email` for example, so a template can also place the fields one by one. See [Template functions](../../docs/template-functions.md#forms).

## Visitors

The form posts to the page itself (`.Action`), not to a visitor action under `/_/`, but it is protected the same way.

- With errors, the page shows the form again with status 422, the visitor's answers, `.Invalid` set and `.Errors`: go-form's messages in the page's language, which `form_render` and `form_field` show at their fields.
- After a valid submission the visitor returns to the page with `?sent=1`, such as `/en/contact?sent=1`, and `.Sent` is set: show `.Success` instead of the form.
- `form_open` and `form_render` add the spam trap. Bots that fill it in are told the form was sent; nothing is stored.
- Posts from other sites are refused by cross-origin protection. The form needs no token or cookie, so it never expires, however long it stays open.
- Each visitor may send the form 10 times in 10 minutes, posts with errors included. After that the form is shown again with `.Throttled` set and their answers kept, with status 429; nothing is stored.

Values are checked by type, with go-form's validation: a `text` is one line of at most 500 characters, a `textarea` at most 10,000, an `email` one address mail can be sent to, as the site's other forms check it (`mail.Rule`, see [Addresses in forms](../../docs/mail.md#addresses-in-forms)), so `anna@localhost` is refused, a `number` a number, a `date` a `YYYY-MM-DD` date and a `select` one of its options. A checkbox is stored as `yes` or `no`.

## Inbox

**Submissions** on the form's admin page opens the inbox (`/admin/contact/inbox`), with tabs for new, archived and all submissions, dated in the site's timezone. The badge in the navigation counts the new ones.

A submission keeps the answers with the labels the visitor saw and the language they used, so later changes to the form leave it unchanged. Editors archive a handled submission or move it back to new; **Move to trash** moves it to the [trash](../../docs/editing.md#the-trash), where it can be restored or deleted for good. Archiving needs the edit right, deleting the delete right. **Trash** on the inbox lists the deleted submissions.

## E-mail

When the form has a notification address, every submission is mailed there in the admin's language. By default the subject is the form's title in the visitor's language; the text lists each answer under its label, followed by a link to the submission in the admin. Replying goes to the first address the visitor gave in an `email` field.

The mail is the template `notify`, with a `forms.NotifyView`. A theme replaces it with `mail/contact/notify.gohtml`, or `mail/forms/notify.gohtml` for every form; `task templates -- contact` writes the default there. The submission is stored first; a mail that fails is only logged.

## From Go

`forms.Of(app, "contact")` returns the instance once the app has started. It reads and handles submissions the way the inbox does:

```go
contact := forms.Of(app, "contact")

waiting, err := contact.Count(ctx, forms.StatusNew)
latest, more, err := contact.SubmissionsByStatus(ctx, forms.StatusNew, 0, 20)
```

An empty status lists or counts every submission. `Submission` reads one, and `Archive`, `Unarchive`, `DeleteSubmission`, `UndeleteSubmission` and `PurgeSubmission` take a submission ID; the last three move a submission to the trash, bring it back or remove it for good. For seed data, `ParseFields` reads fields in the editor's syntax and `Store()` saves the form:

```go
fields, labels, err := forms.ParseFields("name* | text | Name\nemail* | email | E-mail\nmessage* | textarea | Message\n")

form, err := contact.Store().Save(ctx, content.Draft[forms.Definition, forms.Text]{
    Fields: forms.Definition{Fields: fields, Notify: "info@example.com"},
    Translations: content.SetText(nil, "en", forms.Text{
        Title:   "Contact",
        Success: "Thank you. We reply within two working days.",
        Labels:  labels,
    }),
})

err = contact.Store().Publish(ctx, form.ID, "", "en")
```

## Related modules

- [Registrations](../registrations/README.md) handles sign-ups for events, with places, prices and a confirmation mail.
- [Submissions](../submissions/README.md) lets visitors send in news that editors accept as a draft post.
- [Newsletter](../newsletter/README.md) collects e-mail addresses with double opt-in.
