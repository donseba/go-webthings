# E-mail

Modules send e-mail through one sender that the site configures: confirmations to visitors and notifications to staff. The texts come from templates that a theme can replace and translate.

## Choose a sender

`Config.Mail` is the sender. Without one, messages are written to the log instead of sent, which suits development:

```text
INFO mail not sent (no sender configured) to=anna@example.com subject="Please confirm your subscription" text=…
```

### SMTP

`mail.SMTP` sends through a mail server:

```go
app := webthings.New(webthings.Config{
    Mail: mail.SMTP{
        Host:     "smtp.example.com",
        Port:     587,
        Username: os.Getenv("SMTP_USERNAME"),
        Password: os.Getenv("SMTP_PASSWORD"),
        From:     "Atelier Noord <website@example.com>",
    },
})
```

- `Port` defaults to 587. STARTTLS is used when the server offers it, as on ports 587 and 25. Implicit TLS on port 465 is not supported.
- With a `Username`, the sender signs in with PLAIN authentication. Go only sends the password over an encrypted connection, or to `localhost`.
- `From` is an address, optionally with a name.
- `Timeout` bounds one message, from connecting to the end of the conversation; it defaults to 30 seconds (`mail.DefaultSMTPTimeout`). Sending also stops when its context ends.

### Log and Memory

`mail.Log{}` is what the site uses without a sender. Give it a `Logger` to write elsewhere than the default `slog` logger.

`mail.Memory` keeps the messages, for tests:

```go
outbox := &mail.Memory{}

app := webthings.New(webthings.Config{Mail: outbox})

// … sign up for the newsletter …

for _, message := range outbox.Sent() {
    t.Log(message.To, message.Subject)
}
```

A test site started with `sitetest` collects its mail in `Site.Mail`, a `mail.Memory`. `Site.Mails(n)` waits until the site has sent `n` messages, for mail sent in the background.

### Other providers

Any type with this method can send:

```go
type Sender interface {
    Send(ctx context.Context, message mail.Message) error
}
```

A `mail.Message` has `To`, `ReplyTo`, `Subject`, `Text` (always set) and `HTML` (optional). `Headers` holds extra header fields, such as `List-Unsubscribe` on newsletters; pass them on. `mail.Compose(from, message)` writes a message as RFC 5322 bytes, for providers that take raw mail, and refuses header names it writes itself and values with line breaks. See [mail.go](../mail/mail.go).

A sender can also say how it sends, for whoever checks the site's set-up, by implementing `mail.Describer`:

```go
type Describer interface {
    Describe() string
}
```

The setup guide's E-mail step shows the description after "The site sends e-mail with". `mail.SMTP` describes itself with its server, port and `From` address, such as `SMTP mail.example.com:587, from Site <noreply@example.com>`, without the username and password. `mail.Log` and `mail.Memory` implement it too, but for `mail.Log`, and without a sender, the step says "No sender configured: e-mail is written to the server log, not sent." Never put a secret, such as a password or an API key, in a description. A sender without the method shows as "The site sends e-mail with the sender its code sets." See [Mail and storage](deployment.md#mail-and-storage).

The `mailjet` package of go-webthings-contrib (`github.com/donseba/go-webthings-contrib/mailjet`) sends through Mailjet's API. Its sender's `From` must be an address Mailjet has validated.

## What sends e-mail

Apart from the admin's password reset link, its messages to reviewers and the test e-mail of the setup guide's E-mail step, core sends no e-mail of its own. These modules do:

| Module | Mail | To | Language |
| --- | --- | --- | --- |
| [comments](../modules/comments/README.md) | `notify` | the notification address in its settings | admin |
| [forms](../modules/forms/README.md) | `notify` | the form's notification address | admin |
| [newsletter](../modules/newsletter/README.md) | `confirmation` | a new subscriber | the subscriber's |
| [registrations](../modules/registrations/README.md) | `confirmation`, `cancellation` | the visitor who signed up | the visitor's |
| [submissions](../modules/submissions/README.md) | `notify` | the notification address | admin |
| submissions | `received`, `accepted`, `rejected` | the sender, when the settings ask for it | the sender's |

Notifications have the visitor as `Reply-To` when they gave an address, so replying answers them. Registration mail has the e-mail address of **Site settings** as `Reply-To`.

Newsletter mailings are not templates: they are the editor's message, the mailing footer and a personal unsubscribe link, sent in the background. The message and the footer are Markdown. The HTML part shows them formatted, with the link at the end of the footer's last paragraph, so a footer ending in "Unsubscribe:" reads on into it; the text part holds the Markdown as written, with the media library's files as links.

In a mailing, images from the media library are not the uploads. Each is a copy at most 1200 pixels wide (`media.MailImageWidth`) and never larger than the upload, as a JPEG, or as a PNG for a PNG with transparency, since not every mail program shows WebP. Other images, such as a GIF, keep their own address. The HTML gives each image its size as `width` and `height`, so a mail program does not show it larger, and an image without a description in the Markdown gets the library's alt text in the mailing's language, or in the subscriber's for a mailing to every language. The text part shows such an image as a link, `[alt](address)`, and a link to a file of the library with the file's address.

A message is sent while the request that causes it is handled, such as the visitor's post. A visitor who leaves early does not stop it; the sender's timeout still bounds it. A message that cannot be sent is logged; what the visitor sent is stored either way.

The admin sends its password reset link itself, through `Config.Mail` and in the background: plain text in the admin's language, without a template. See [Accounts and permissions](permissions.md#passwords). Where the site asks for a review, it mails the reviewers the same way when a revision waits for them, under the same conditions as the reset link; see [Editing and publishing](editing.md#review-before-publishing).

## Mail templates

A module declares its mail like its page templates, with English default texts. A theme replaces one with a file of its own, looked up in this order:

1. `mail/<key>/<name>.gohtml`, for one instance, such as `mail/remarks/notify.gohtml`;
2. `mail/<type>/<name>.gohtml`, for every instance of the module, such as `mail/comments/notify.gohtml`;
3. the module's default.

Like other templates, a theme without the file uses the one in `templates/default/`. `task templates -- <key>` writes the module's defaults into `templates/default/mail/<key>/` as a starting point, next to its page templates.

A mail template defines its parts:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/comments.NotifyView */}}
{{define "subject"}}{{tl localizer "New message from"}} {{.Name}}{{end}}

{{define "text"}}
{{.Name}}{{with .Email}} <{{.}}>{{end}} {{tl localizer "wrote"}}:

{{.Body}}

{{tl localizer "Review:"}} {{.ReviewURL}}
{{template "mail/partials/signature.gohtml" .}}
{{end}}

{{define "html"}}
<p><strong>{{.Name}}</strong> {{tl localizer "wrote"}}:</p>
<blockquote>{{.HTML}}</blockquote>
<p><a href="{{.ReviewURL}}">{{tl localizer "Review"}}</a></p>
{{end}}
```

- `subject` is required; its spaces and line breaks become single spaces.
- `text` is required: the plain-text message, trimmed.
- `html` is optional. When present, the message has both parts.
- Each part renders through go-partial as a part of the theme's pages, with the same functions and render stages. `subject` and `text` are plain text (go-partial's `RenderText`, with Go's `text/template`), so nothing in them is HTML-escaped; `html` is escaped as on the pages.
- Templates in the theme's `mail/partials/` can be used in every mail template, by their path. The example expects `templates/default/mail/partials/signature.gohtml`.

Every template function works, in the language of the message: `tl` with the site's translations, `date`, `time`, `money` and the modules' functions. `moduleURL`, `image` and `asset` give addresses without the domain, so use the absolute links that the mail's data gives, such as `.ReviewURL` above.

Mail templates are read once and kept, like the theme's pages: a mail template that is changed or added while the site runs is used after a restart, or at once with `Config.ReloadTemplates`.

The default texts use `tl`, so the site's translations translate them without a template of your own. `task translations` collects their texts with those of your templates. See [Languages](languages.md).

## Language

Mail to visitors is written in their language: the language of the page they used, or the one stored with their subscription or proposal.

Mail to staff, such as notifications, is written in `Config.AdminLanguage`, the language of the admin: English (`en`) unless the site sets another. Its texts are translated with the site's translation for that language, `translations/<code>.po`.

## Links in e-mail

Links in e-mail must be absolute. Modules make them absolute with `Config.BaseURL`:

```go
app := webthings.New(webthings.Config{
    BaseURL: "https://www.example.com",
})
```

Without a `BaseURL`, links use the address of the request that caused the mail: its `Host` header, and `https` when the connection is TLS. Behind a proxy listed in `Config.TrustedProxies`, the proxy's `X-Forwarded-Proto` and `X-Forwarded-Host` headers name the address instead, or else the `proto=` and `host=` of its `Forwarded` header; from other clients they are ignored. Set `BaseURL` in production, so every link points to the site's real address. Newsletter mailings use the site's address at the moment the editor sends them, for their links and images: every address of the site in a mailing is absolute, in the HTML and the text part, and relative links lead from the home page of the mailing's language, or of the subscriber's for a mailing to every language.

The admin's password reset is the exception: without a `BaseURL` it is not offered, since a link made from the `Host` header could point to another site. `Config.Insecure` allows it for local development.

## Mail from your own module

A site's own module declares its mail in `Module.Mail` and sends it with `Frontend.SendMail`:

```go
type RequestMail struct {
    Phone string
}

const requestMail = `{{define "subject"}}{{tl localizer "Call-back request"}}{{end}}

{{define "text"}}
{{tl localizer "Please call"}} {{.Phone}}.
{{end}}
`

func callback() webthings.Module {
    return webthings.Module{
        Type: "callback",
        Key:  "callback",
        Mail: []frontend.Template{{Name: "request", Data: RequestMail{}, Example: requestMail}},
        // Forms, Funcs and Setup …
    }
}
```

The module is the one from [Visitor forms](visitor-forms.md#a-form-of-your-own). The `Example` is the default text; a theme replaces it with `mail/callback/request.gohtml`. In the module's action handler, after `env.Frontend.BindForm` read the post into `in`:

```go
_ = env.Frontend.SendMail(r.Context(), env.Mail, frontend.Letter{
    Locale: env.AdminLanguage,
    Key:    env.Key,
    Name:   "request",
    Data:   RequestMail{Phone: in.Phone},
    To:     []string{"office@example.com"},
})
```

`Frontend.SendMail` writes the mail with its template and sends it, as the bundled modules do. A visitor who leaves early does not cancel it; the sender's timeout still bounds it. A failure is logged with the module and the mail's name, and returned for a handler that tells the visitor; most ignore it. `ReplyTo` in the `Letter` sets where answers go, such as the visitor who wrote. `env.Mail` is always set: it logs when the site has no sender.

Pass the visitor's language for mail to visitors, such as `env.Frontend.BackLocale(r)`, the language of the form's page. Pass `env.AdminLanguage` for mail to staff; it is `"en"` unless `Config.AdminLanguage` says otherwise. `env.AbsoluteURL(r, path)` makes a link absolute with `BaseURL`, or with the request's address.

A link to one of the module's own actions, such as a confirmation link, comes from its action router, `env.Actions()`, by route name. For a route named `confirm`, `actions.RoutePathArgs("confirm", url.Values{"token": {token}})` is `/_/<key>/confirm?token=…`, as the newsletter module builds the links in its mail. This needs no request, so it also works for mail that is sent outside a visitor's request, such as a newsletter mailing.

`Frontend.Mail(ctx, locale, key, name, data)` only writes the message: the subject, text and HTML. The caller then sets the recipients and sends it.

A module that mails Markdown with media, such as a message an editor wrote, resolves the media as the newsletter does in [`../modules/newsletter/sending.go`](../modules/newsletter/sending.go). It renders the HTML part with `markdown.Render` and `markdown.Options`, whose `Images` gives each image as `Frontend.MailImage(ctx, id)` returns it, with its `Width` and `Height`, and `Frontend.AltText(ctx, id, locale)` when the Markdown gives no description. `markdown.PlainMedia(source, options)` writes the text part: the Markdown with every image as a link, `[alt](address)`, and every link to a file with the file's address. `Options.Base` makes the addresses absolute in both parts. It is the absolute address that relative links lead from, such as the home page of the message's language, `https://example.com/en/`: every link and image address without a scheme is resolved from it as a browser resolves it, `/en/about` and `about` alike, and so are the paths that `MailImage` and `MediaURL` give when the library serves the file. Addresses with a scheme, such as `https:` and `mailto:`, and a `#fragment` alone stay as they are, and so does everything in code; an empty Base, or one that is not absolute, changes nothing.

## Addresses in forms

A form field for an address that mail will go to gets `validate:"mail"`, the rule `mail.Rule`, which visitor forms and the admin's forms check with `mail.FormRule`:

```go
type SignupInput struct {
    Email string `form:"input,email" name:"email" label:"E-mail address" required:"true" maxLength:"254" autocomplete:"email" validate:"mail"`
}
```

It refuses an address that `mail.Valid` refuses. `Valid` takes a single plain address with a dot in it, since mail goes to addresses on the internet, so `anna@localhost` gets go-form's message for an invalid e-mail address at its field. An empty value passes; `required:"true"` checks that. An address that an e-mail input refuses on its own gets one message, not two. The bundled modules' e-mail fields use the rule, in visitor forms and in the admin, and so do the `email` fields editors build in the [forms module](../modules/forms/README.md). A field of a go-form `form.Schema`, such as those, names the rule in `form.Spec.Validate`, as the `validate` tag does for a field of a struct, and `env.Frontend.ValidateSchema` checks it.

## Next steps

- [Visitor forms](visitor-forms.md) explains the forms that cause most mail.
- [Themes](themes.md) covers the theme folders that hold mail templates.
- [Writing a module](writing-a-module.md) covers `Module.Mail` and `Env`.
