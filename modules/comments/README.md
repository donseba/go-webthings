# Comments

Comments below posts, events or pages, and guestbooks. New messages wait for approval unless moderation is off, and staff can reply from the admin.

Every message belongs to a subject: a text the template chooses, such as a post ID, a page ID or a fixed word. Add an instance per kind of conversation; each has its own messages, settings and admin section.

## Add it to the site

```go
app.Use(
    comments.Module("guestbook", "Guestbook"),
    comments.Module("remarks", "Comments"),
)
```

The key (`guestbook`) is the instance's name in URLs and template functions and must be unique on the site. The title (`Guestbook`) is what editors see in the admin navigation and the page editor.

## Comments below content

Comments attach to anything with an ID through the template functions; the instance needs no page of its own. Below a post in `news/post.gohtml`:

```gotemplate
{{commentThread "remarks" .ID}}
```

`commentThread` renders the approved comments on that subject, oldest first with their replies, followed by the form. `{{commentForm "remarks" .ID}}` renders only the form, and `{{commentCount "remarks" .ID}}` counts the approved comments and replies, for example next to titles in a list.

Both come with plain, accessible default markup. A theme replaces them with `remarks/thread.gohtml` and `remarks/form.gohtml`, or `comments/thread.gohtml` and `comments/form.gohtml` for every instance; `task templates -- remarks` writes the defaults to start from. They use stable `wt-*` classes (`wt-comments`, `wt-comment`, `wt-comment--staff`, …), laid out by the optional stylesheet `webthings.css`, which the layout links with `{{styles "webthings.css" "site.css"}}` (see [The base stylesheet](../../docs/themes.md#the-base-stylesheet)). Their texts go through `tl`, so the site's translations translate them.

For markup of your own, `comments` returns the approved comments, `commentURL` the address the form posts to, `commentInput` the form's model for a subject, `commentResult` the outcome of the visitor's last post and `commentsClosed` whether new comments are refused:

```gotemplate
{{range comments "remarks" .ID}}
    <article>
        <strong>{{.Name}}</strong> {{date .CreatedAt}}
        {{.HTML}}

        {{range .Replies}}
            <blockquote>{{if .Staff}}Reply from the site:{{else}}{{.Name}}:{{end}} {{.HTML}}</blockquote>
        {{end}}
    </article>
{{end}}

<div id="remarks">
    {{with commentResult "remarks"}}<p class="notice">{{.}}</p>{{end}}

    {{if not (commentsClosed "remarks")}}
        {{with commentInput "remarks" .ID}}
            {{form_open .}}
                {{form_field . "subject" nil}}
                {{form_field . "name" nil}}
                {{form_field . "email" nil}}
                {{form_field . "body" nil}}
                <button>Send</button>
            {{form_close}}
        {{end}}
    {{end}}
</div>
```

`HTML` is the message escaped, with its paragraphs and line breaks. `CreatedAt` is in UTC; `{{date .CreatedAt}}` shows it in the page's language and the site's timezone.

`form_open` writes the form tag, posting to `commentURL`, with the spam trap and the page to return to: the element whose id is the instance key, here `remarks`. `form_field` renders one field of the form by its key, with its label, its limits (`required`, `maxlength`) and its help text, translated with the site's translations and in the site's form theme; `subject` is a hidden field with the subject. The fields' ids start with the key, such as `remarks-name`. See [Template functions](../../docs/template-functions.md#forms). Inputs written by hand with the same names work too.

## Guestbook page

In the admin, open a page and choose the instance under **Shows on this page**. The page renders `guestbook/list.gohtml`, or `comments/list.gohtml` for every instance without a design of its own, with a `comments.ListView`: the approved messages of the instance, newest first, 20 per page. `.Form` is the form, with `.Subject`, the instance key, as its subject: `{{form_open .Form}}` with `form_field` per field, `subject` included, returns the visitor to the element whose id is `.Key`. The pager's `.NewerURL` and `.OlderURL` are the page's address with `?page=`, such as `/en/guestbook?page=2`. Generate a starting point with:

```sh
task templates -- guestbook
```

The form component works here too:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/comments.ListView */}}
<h1>{{.Title}}</h1>

{{commentForm .Key .Subject}}

{{range .Comments}}
    <article>
        <p><strong>{{.Name}}</strong> · {{date .CreatedAt}}</p>
        {{.HTML}}
    </article>
{{end}}

{{with .NewerURL}}<a href="{{.}}">Newer</a>{{end}}
{{with .OlderURL}}<a href="{{.}}">Older</a>{{end}}
```

The page lists the messages of every subject of the instance, so keep a guestbook apart from the comments below posts.

## Visitors

The form posts to `/_/<key>/post` (`{{commentURL "remarks"}}`) with the fields of `comments.PostInput`:

| Field | |
| --- | --- |
| `subject` | required, one line, at most 200 characters |
| `name` | required, one line, at most 100 characters |
| `email` | optional; never shown on the website |
| `body` | required, plain text, at most 5,000 characters |
| `parent` | optional: the ID of the comment to answer; a reply to a reply joins the same thread |

`form_open` adds the spam trap and the page to return to, the element whose id is the instance key. The visitor comes back with `?<key>=<result>`, which `commentResult` and the form component read:

- `sent`: the message is shown right away;
- `pending`: it waits for approval;
- `invalid`: a field is missing, too long or not on one line;
- `closed`: the instance was closed for new messages when the visitor posted; the form component says so even after it opens again;
- `throttled`: the visitor posted more than 5 times in 10 minutes.

With `invalid`, the form shows what the visitor wrote again, with a message in the page's language at each field to correct. The components and `form_field` do this without a change to the template; inputs written by hand do not. Of several comment forms on one page, only the one for the subject the visitor wrote on shows it. See [A post with mistakes](../../docs/visitor-forms.md#a-post-with-mistakes). A post that cannot be saved, such as when the database cannot be reached, gets the site's error page, [500.gohtml](../../docs/templates.md#500gohtml), also with HTMX.

Bots that fill in the spam trap get the answer a person would get; nothing is stored. A post that stores nothing, spam, `invalid`, `closed` or `throttled`, leaves the pages in the site's output cache as they are; see [Modules and the output cache](../../docs/modules.md#modules-and-the-output-cache). The message's language is the language of the page it was written on. Visitors' IP addresses are never stored: a hash that changes daily lets moderators spot a flood from one sender.

## Moderation

The instance's admin section (`/admin/remarks`; `/admin` is the default [admin path](../../docs/configuration.md#admin-path)) lists the messages newest first, grouped by subject, with tabs for pending, approved and all messages. Each message shows the name, the e-mail address, the language, the page it was written on and a short sender code. The badge in the navigation counts the messages that are not shown.

- **Approve** shows a message, **Hide** takes it off the website again, after the question "Take this off the website?". Both need the publish right.
- **Reply** adds a staff reply, shown at once and marked as `Staff`. It is signed with the site title in the comment's language (or the organisation, or the editor's name) and approves the comment it answers. It needs the create right.
- **Move to trash** moves a message and its replies to the [trash](../../docs/editing.md#the-trash), where they can be restored or deleted for good; restoring the message brings its replies back with it. It needs the delete right. A reply deleted on its own cannot be restored while the message it answers is in the trash.

Under **Settings**, editors with the edit right switch **Approve messages before they are shown** (on by default), close the instance with **Closed for new messages**, and set a **Notification address**.

## E-mail

With a notification address, every new message from a visitor is mailed there in the admin's language: the name and address, the message, the page it was written on, whether it waits for approval, and a link to the admin. Replying to the mail answers the visitor when they gave an address.

The mail is the template `notify`, with a `comments.NotifyView`. A theme replaces it with `mail/remarks/notify.gohtml`, or `mail/comments/notify.gohtml` for every instance; `task templates -- remarks` writes the default there.

## From Go

`comments.Of(app, "remarks")` returns the instance once the app has started:

```go
remarks := comments.Of(app, "remarks")

list, err := remarks.List(ctx, postID)
count, err := remarks.Count(ctx, postID)
```

`Add` stores a message after the same checks as the form, for example in an import; `Reply`, `Approve` and `Delete` do what the admin does, and `Undelete` and `Purge` bring a deleted message back or remove it for good. What they change shows on the site at once, also on pages in the site's output cache; see [Modules and the output cache](../../docs/modules.md#modules-and-the-output-cache). `CreatedAt` keeps the original date; zero means now. A reply passes the ID of the comment `Add` returned as `ParentID`:

```go
comment, err := remarks.Add(ctx, comments.Entry{
    Subject:   postID,
    Name:      "Anna",
    Body:      "Great to see the playground open again.",
    Locale:    "en",
    Approved:  true,
    CreatedAt: written,
})

reply, err := remarks.Add(ctx, comments.Entry{
    Subject:  postID,
    ParentID: comment.ID,
    Name:     "Ben",
    Body:     "The swings are new too.",
    Locale:   "en",
    Approved: true,
})
```

## Related modules

- [Publishing](../publishing/README.md) and [Events](../events/README.md) give comments a subject: a post's or event's `ID`.
- [Registrations](../registrations/README.md) attaches sign-ups to events in the same way.
- [Forms](../forms/README.md) receives messages for the site's staff only, with an inbox.
