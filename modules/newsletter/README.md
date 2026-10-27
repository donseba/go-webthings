# Newsletter

A subscriber list with double opt-in, and mailings to it from the admin. Every mailing carries a personal unsubscribe link.

A site usually has one instance. Its signup box goes in any template, such as the footer; placing the instance on a page gives it a signup page as well, where the links in the e-mails land.

## Add it to the site

```go
app.Use(newsletter.Module("newsletter", "Newsletter"))
```

The key (`newsletter`) is the instance's name in URLs and template functions and must be unique on the site. The title (`Newsletter`) is what editors see in the admin navigation and the page editor. Mail goes out through the site's `Config.Mail`.

`WithProvider` keeps a mailing service's list in step: it is told about every confirmed subscriber, and about confirmed subscribers who leave, are deleted or are restored from the trash. Errors are logged; the site's own list stays the reference. The `mailjet` package of go-webthings-contrib (`github.com/donseba/go-webthings-contrib/mailjet`) syncs a Mailjet contact list:

```go
app.Use(newsletter.Module("newsletter", "Newsletter",
    newsletter.WithProvider(mailjet.List{APIKey: key, SecretKey: secret, ListID: 123}),
))
```

Another service implements `newsletter.Provider`: `Subscribe` and `Unsubscribe`, each with the address, name and language.

## Show the signup form

```gotemplate
{{newsletterForm "newsletter"}}
```

`newsletterForm` renders a signup box: the title and introduction from the admin, the result of the visitor's signup and an e-mail field. It comes with plain, accessible default markup. A theme replaces it with `<key>/form.gohtml`, or `newsletter/form.gohtml` for every instance; `task templates -- newsletter` writes the default to start from, together with the signup page and the confirmation mail. It uses stable `wt-*` classes (`wt-newsletter`, `wt-newsletter__intro`, …) and the form classes of the site's form theme, laid out by the optional stylesheet `webthings.css`, which the layout links with `{{styles "webthings.css" "site.css"}}` (see [The base stylesheet](../../docs/themes.md#the-base-stylesheet)), and its texts go through `tl`, so the site's translations translate them.

For markup of your own, `newsletter` returns the title, introduction, form address, form and result in the page's language. `.Intro` is rendered from Markdown and brings its own paragraphs, so it does not go inside a `<p>`:

```gotemplate
{{with newsletter "newsletter"}}
    <section id="newsletter">
        <h2>{{.Title}}</h2>
        {{.Intro}}

        {{if eq .Result "check-mail"}}<p>Please check your inbox and confirm.</p>{{end}}

        {{form_open .Form}}
            {{form_field .Form "locale" nil}}
            {{form_field .Form "email" nil}}
            {{form_field .Form "name" nil}}
            <button>Subscribe</button>
        {{form_close}}
    </section>
{{end}}
```

`.Form` is the form: `form_open` writes the form tag, posting to `.Action`, with the spam trap and the page to return to, the element whose id is the instance key. `form_field` renders one of its fields by its key, with its label, limits and placeholder, translated with the site's translations and in the site's form theme; the ids start with the key, such as `newsletter-email`. `locale` is a hidden field with the page's language, the language of the confirmation mail. See [Template functions](../../docs/template-functions.md#forms). Inputs written by hand with the same names work too.

`{{newsletterURL "newsletter"}}` and `{{newsletterResult "newsletter"}}` give the form address and the result on their own.

## Signup page

In the admin, open a page and choose the instance under **Shows on this page**. The page renders `newsletter/signup.gohtml` with a `newsletter.SignupView`, the same data as the `newsletter` function; the starting point from `task templates` shows the form and every result. The box works here too:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/newsletter.SignupView */}}
<main id="content">
    {{newsletterForm .Key}}
</main>
```

## Visitors

The form posts to `/_/<key>/subscribe` with the fields of `newsletter.SubscribeInput`, `email` (required) and `name` (optional, one line), and with `locale` (hidden, the page's language in `.Form`; without it, the language of the page the form is on), plus the spam trap and the way back that `form_open` adds. The visitor returns to the page with `?<key>=<result>`:

- `check-mail`: for every valid address, whether it is new or already on the list, so the form never reveals who subscribed;
- `invalid`: the address is not valid, or the name is too long or not on one line;
- `throttled`: more than 20 signups in an hour from this visitor.

With `invalid`, the form shows the visitor's address and name again, with a message in the page's language at each field to correct. The component and `form_field` do this without a change to the template; inputs written by hand do not. See [A post with mistakes](../../docs/visitor-forms.md#a-post-with-mistakes). A signup that cannot be saved, such as when the database cannot be reached, gets the site's error page, [500.gohtml](../../docs/templates.md#500gohtml).

Bots that fill in the spam trap get `check-mail`; nothing is stored.

A new address is saved as pending and receives a confirmation mail. Its link, `/_/<key>/confirm?token=…`, confirms the subscription and leads to the signup page in the subscriber's language, or to the home page when the instance is not placed on a page, with `?<key>=confirmed`, or `invalid` for a link that no longer works. Signing up again while pending sends the mail again; a confirmed address receives nothing. After unsubscribing, signing up again needs a new confirmation.

Every mailing ends with a personal link, `/_/<key>/unsubscribe?token=…`, which leads back with `?<key>=unsubscribed`. Mail programs get it in a `List-Unsubscribe` header with one-click unsubscribe. Subscribers who leave stay on the list as unsubscribed.

## Editing

The instance's admin section (`/admin/newsletter`; `/admin` is the default [admin path](../../docs/configuration.md#admin-path)) lists the subscribers with tabs for all, pending, confirmed and unsubscribed, with their language and the dates they signed up and confirmed. **Export CSV** downloads the subscribers of the current tab; a value that starts with `=`, `+`, `-` or `@` gets a leading apostrophe, so a spreadsheet shows it as text instead of running it as a formula. It needs the export right; see [Groups and the matrix](../../docs/permissions.md#groups-and-the-matrix). **Move to trash** moves a subscriber to the [trash](../../docs/editing.md#the-trash): they leave the list, the counts and the export, and their links stop working. Their address is free again, so they can sign up anew themselves. Someone who had unsubscribed is not put back by the CSV import or `Add` while their record is in the trash; only deleting it for good forgets that they left. Restoring brings them back with their status, unless the address is on the list again by then.

**Import CSV** adds people who agreed elsewhere, for example when moving a list from another service. It needs the create right.

- The first row names the columns: `email` (required), `name` and `locale`. Commas, semicolons and tabs all work as separators. Upload a CSV, TSV or plain-text file (`.csv`, `.tsv`, `.txt`) or paste the text; another kind of file gets an error at the field.
- With a `status` column, as in the export, only `confirmed` rows are added.
- The editor picks the language for rows without one, the default language unless changed, and must confirm that everyone agreed to receive the newsletter. Without that confirmation nobody is added, and the checkbox shows an error.
- Everyone is added as confirmed, without a confirmation mail; pending subscribers become confirmed. People who unsubscribed are not added again, also while their deleted record is in the trash; once it is deleted for good, the address counts as new. The result lists the addresses that are not valid.

**Texts** holds, per language, the signup box's **Title** and **Introduction** and the **Mailing footer** that ends every mailing before the unsubscribe link. The introduction and the footer are Markdown. Publish them; until then the module uses the texts of the site's default language, or plain English.

**Mailings** composes messages to the confirmed subscribers:

- A mailing has a subject, a language (**All languages** or one) and a Markdown message, with images from the media library. The subject needs at least 5 characters; spaces around it do not count. An image without alt text in the Markdown gets the one of the media library in the mailing's language, or in each subscriber's language for a mailing to all languages.
- A saved draft shows a preview and how many confirmed subscribers it would reach.
- **Send to subscribers** asks for confirmation, then sends one message to each subscriber, as text and HTML, with the footer in their language and their unsubscribe link. It needs the publish right.
- A mailing is marked as sent before the messages go out, so it cannot go out twice. Sent mailings cannot be changed.
- Messages go out in the background. The mailing's page shows the progress while it is sent, and afterwards the number of recipients, when it finished and how many messages were not delivered.
- Each subscriber gets a mailing at most once. After a restart, sending continues with the next subscriber; closing the app stops it after the current message. A message the mail server refuses is logged and counted as not delivered.
- **Move to trash** moves a mailing, a draft or a sent one, to the trash. Moving a mailing that is being sent to the trash stops it for good: subscribers who did not get it yet never will, also after a restore.

Editors' rights are set per instance in the groups' permission matrix; the texts share the rights of their instance.

## E-mail

The confirmation mail is the template `confirmation`, with a `newsletter.ConfirmationView`, in the subscriber's language. Its default is English text through `tl`, so the site's translations translate it. A theme replaces it with `mail/<key>/confirmation.gohtml`, or `mail/newsletter/confirmation.gohtml` for every instance.

Mailings are not templates: they are the editor's Markdown, the footer and the unsubscribe link. In the HTML part the link continues the footer's last paragraph, so a footer ending in "Unsubscribe:" reads on into it; after a footer that ends otherwise, such as with a list, the link gets a paragraph of its own. The text part has the footer's Markdown with the link on the next line.

Every address of the site in a mailing is absolute, in the HTML and the text part, on the site's address when the editor sends it (set `Config.BaseURL`; see [Links in e-mail](../../docs/mail.md#links-in-e-mail)). A mailing has no address of its own, so its relative links lead from the home page of its language, or of the subscriber's for a mailing to all languages: in an English mailing, `/en/about` and `about` both become `https://example.com/en/about`. Links to other sites, `mailto:` links and links in code stay as they are.

Images in a mailing are copies at most 1200 pixels wide (`media.MailImageWidth`), never enlarged, as JPEG, or as PNG for a PNG with transparency, because not every mail program shows WebP; a GIF keeps its own address. In the HTML part each has its alt text, `width` and `height`, so a mail program does not show it larger. In the text part an image is a link to that copy, `[alt](url)`, and a link to a file of the library is the file's address.

## From Go

`newsletter.Of(app, "newsletter")` returns the instance once the app has started:

```go
letters := newsletter.Of(app, "newsletter")

counts, err := letters.Counts(ctx)
recipients, err := letters.Recipients(ctx, "en")
```

`Subscribers` and `Subscriber` read subscribers; `Subscribers` with a limit of 0 or less returns all of them. `DeleteSubscriber` moves one to the trash, and `UndeleteSubscriber` and `PurgeSubscriber` bring it back or remove it for good. `Add` and `Import` put people on the list like the CSV import: confirmed and without a mail, so only add people who agreed. `Add` returns `ErrUnsubscribed` for someone who left the list, also while their record is in the trash; `Import` skips and counts them:

```go
subscriber, err := letters.Add(ctx, newsletter.Contact{Email: "anna@example.com", Name: "Anna", Locale: "en"})
result, err := letters.Import(ctx, contacts)
```

No page shows subscribers, so writing them, also a visitor's signup and a one-click unsubscribe, leaves the pages in the site's output cache as they are; see [Modules and the output cache](../../docs/modules.md#modules-and-the-output-cache).

`Mailings`, `Mailing` and `SaveMailing` handle drafts, which are sent from the admin. `DeleteMailing`, `UndeleteMailing` and `PurgeMailing` move a mailing to the trash, bring it back or remove it for good. `Texts()` gives write access to the texts for seed data, and what it changes shows on the site at once; `Text(ctx, locale)` returns them with the same fallbacks the website uses.

## Related modules

- [Forms](../forms/README.md) receives messages from visitors, with an inbox.
- [Registrations](../registrations/README.md) sends a confirmation mail for each sign-up to an event.
