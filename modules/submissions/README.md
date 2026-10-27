# Submissions

Lets visitors send in news with a title, a text and an optional image. Editors review the proposals and accept one as a draft post or reject it.

Each instance feeds one Publishing instance: an accepted proposal becomes an unpublished post there, which editors finish and publish as usual. Use one instance per stream that takes tips from visitors.

## Add it to the site

```go
app.Use(
    publishing.Module("news", "News"),
    submissions.Module("tips", "Tips", "news"),
)
```

The key (`tips`) is used in the admin, the form action and the template functions, and must be unique on the site. The title (`Tips`) is what editors see in the admin and the page editor. The third argument is the key of the Publishing instance that accepted proposals go to; add that instance as well. It cannot be empty or the instance's own key.

## Place it on a page

In the admin, open a page and choose the instance under **Shows on this page**. The page shows the proposal form from `tips/form.gohtml`, or `submissions/form.gohtml` for every instance without a design of its own. Other addresses below the page get the site's 404 page. Generate a working starting point, with labels and every result message:

```sh
task templates -- tips
```

A minimal form:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/submissions.FormView */}}
<h1>{{.Title}}</h1>

<section id="{{.Key}}">
    {{if eq .Result "sent"}}
        <p>Thank you! The editors will read your proposal.</p>
    {{else}}
        {{if eq .Result "invalid"}}<p>Please fill in every field. Images are JPEG, PNG or GIF up to {{.MaxImageMB}} MB.</p>{{end}}
        {{if eq .Result "throttled"}}<p>You sent several proposals already. Please try again later.</p>{{end}}

        {{form_open .Form}}
            {{form_field .Form "name" nil}}
            {{form_field .Form "email" nil}}
            {{form_field .Form "title" nil}}
            {{form_field .Form "text" nil}}
            {{form_field .Form "image" nil}}
            <button>Send</button>
        {{form_close}}
    {{end}}
</section>
```

`.Form` is the form: `form_open` writes the form tag, posting to `.Action` as `multipart/form-data` because of the image, with the spam trap and the page to return to, the element whose id is `.Key`, the instance key. `form_field` renders one of its fields by its key, with its label and limits, such as the image's `accept`, translated with the site's translations and in the site's form theme; the ids start with the key, such as `tips-title`. See [Template functions](../../docs/template-functions.md#forms).

## Show the form anywhere

The same form works in any template, for example in a sidebar, with three template functions: `submitInput` is the form, `submitURL` the address it posts to and `submitResult` the result of the visitor's last post. The form returns the visitor to the element whose id is the instance key:

```gotemplate
<aside id="tips">
    {{if eq (submitResult "tips") "sent"}}
        <p>Thank you for your tip!</p>
    {{else}}
        {{with submitInput "tips"}}
            {{form_open .}}
                {{form_field . "name" nil}}
                {{form_field . "email" nil}}
                {{form_field . "title" nil}}
                {{form_field . "text" nil}}
                {{form_field . "image" nil}}
                <button>Send</button>
            {{form_close}}
        {{end}}
    {{end}}
</aside>
```

Inputs written by hand with the names of the fields below work too.

## Visitors

The form posts the fields of `submissions.ProposalInput` to `/_/tips/submit` as `multipart/form-data`, because of the image; a form without an image field may be sent url-encoded.

| Field | |
| --- | --- |
| `name` | one line, up to 200 characters |
| `email` | a valid e-mail address |
| `title` | one line, up to 200 characters |
| `text` | plain text or Markdown, up to 20000 characters |
| `image` | optional: JPEG, PNG or GIF up to 5 MB, sent as one of those types or with a `.jpg`, `.jpeg`, `.png` or `.gif` name, and recognised by its content |

The visitor returns to the form's page with `?tips=sent`, `?tips=invalid` when a field is missing or wrong, or `?tips=throttled` after more than five proposals in an hour from the same IP address (list a reverse proxy in `Config.TrustedProxies`). Posts that fill in the spam trap that `form_open` adds are answered with `sent` and dropped. Only a proposal that was stored makes the site's output cache render the pages again; see [Modules and the output cache](../../docs/modules.md#modules-and-the-output-cache).

With `invalid`, the form shows the visitor's name, address, title and text again, with a message in the page's language at each field to correct; the image is not kept, so the visitor chooses it again. `form_field` does this without a change to the template; inputs written by hand do not. See [A post with mistakes](../../docs/visitor-forms.md#a-post-with-mistakes). A proposal that cannot be saved, such as when the database cannot be reached, gets the site's error page, [500.gohtml](../../docs/templates.md#500gohtml), also with HTMX.

A proposal keeps the language of the page the form was on. Its image goes into the media library right away and cannot be deleted there until the proposal is deleted for good.

## In the admin

Each instance has its own section (`/admin/tips`; `/admin` is the default [admin path](../../docs/configuration.md#admin-path)) with a badge that counts the new proposals. The list shows the new proposals, newest first, with dates in the site's timezone; filters switch to the accepted, the rejected or all proposals.

A proposal shows its text, image, sender, e-mail address and language. Editors with the right to publish in the instance can:

- **Accept as draft post**: this creates an unpublished post in `news`, in the proposal's language, with its title, its text as Markdown and its image as the post's image, dated today. The post's history names the sender. **Open draft post** leads to it. A rejected proposal can still be accepted.
- **Reject** a new proposal.

**Move to trash** moves a proposal to the [trash](../../docs/editing.md#the-trash), where it can be restored or deleted for good; it needs the delete right. Deleting it for good moves its image to the media library's trash too, unless the proposal was accepted or other content uses the image.

Under **Settings**, editors who may edit the instance set the address that receives new proposals, and whether senders hear from the site. Other editors do not see the settings.

## E-mail

| Mail | Sent to | When |
| --- | --- | --- |
| `notify` | the notification address, in the admin's language | for every new proposal |
| `received` | the sender, in the proposal's language | after sending, with **Confirm receipt to submitters** on |
| `accepted`, `rejected` | the sender, in the proposal's language | when an editor decides, with **Tell submitters when their proposal is accepted or rejected** on |

The notification holds the text, a link to the image if there is one, and a link to the proposal in the admin; replying to it answers the sender. Without a notification address no one is notified, and both options for senders are off until editors switch them on. A theme replaces the texts with `mail/tips/<name>.gohtml` or `mail/submissions/<name>.gohtml`.

## From Go

`submissions.Of(app, "tips")` returns the instance once the app has started:

```go
tips := submissions.Of(app, "tips")

waiting, err := tips.Count(ctx, submissions.StatusNew)
```

`Create` stores a proposal (an image must already be in the media library), `List` and `Get` read proposals, `Accept` and `Reject` decide as the admin does, and `Delete`, `Undelete` and `Purge` move a proposal to the trash, bring it back or remove it for good. These calls send no e-mail.

## Related modules

- [Publishing](../publishing/README.md) holds the posts that accepted proposals become.
- [Forms](../forms/README.md) collects messages that do not become posts, such as a contact form.
