# Editing and publishing

Editors write formatted text that is stored as Markdown, keep drafts next to the published version and publish now or at a planned time. When they are signed in, the site itself shows them a link to edit what they look at.

## Writing text

Text fields such as a page's **Content** or a post's **Content** show the text formatted, with a toolbar above it, and store it as [Markdown](https://commonmark.org/help/). The toolbar has paragraph, headings 2 to 4, bold, italic, code, link, bulleted and numbered lists, quote, code block, horizontal line, an image from the media library, undo and redo. Pointing at a button shows its name and shortcut.

- **Link** asks for an address, such as `https://example.com`, `/en/contact` or `mailto:info@example.com`. An empty address removes the link. Without selected text, the address becomes the link's text.
- **Image from the media library** opens the library's images, with a search by file name, title and alternative text that ignores case and accents. Choose one, add an alternative text if you like, and press **Insert**, or double-click the image. The image gets a paragraph of its own and is stored as `![alt](media:<id>)`. The button is only there for editors who may read **Media**.
- Pasted text keeps only what the toolbar offers. Fonts, colours and layout from Word or web pages are left out, and so are images pasted from elsewhere; HTML never gets into the text.

| Keys | Mac | Does |
| --- | --- | --- |
| Ctrl+B, Ctrl+I, Ctrl+` | Cmd+B, Cmd+I, Cmd+` | bold, italic, code |
| Ctrl+K | Cmd+K | link |
| Ctrl+Z | Cmd+Z | undo |
| Ctrl+Shift+Z or Ctrl+Y | Cmd+Shift+Z or Cmd+Y | redo |
| Ctrl+Shift+0 | Control+Shift+0 | paragraph |
| Ctrl+Shift+2, 3 or 4 | Control+Shift+2, 3 or 4 | heading 2, 3 or 4; again for a paragraph |
| Ctrl+Shift+7, Ctrl+Shift+8 | Control+Shift+7, Control+Shift+8 | numbered list, bulleted list |
| Ctrl+[, Ctrl+] | Cmd+[, Cmd+] | move a list item out, in |
| Shift+Enter | Shift+Enter | line break within a paragraph |

Typed at the start of a line, some Markdown becomes formatting at once: `## `, `### ` or `#### ` a heading, `- ` or `* ` a bulleted list, `1. ` a numbered list, `> ` a quote, ```` ``` ```` a code block and `---` a horizontal line. Backspace right after it undoes it. Other Markdown typed in the formatted text, such as `**bold**`, stays plain text.

### The Markdown view

The **Markdown** button at the end of the toolbar shows the text as Markdown, to write it by hand; pressing it again shows the formatted text. The browser remembers the choice for the next text. Without JavaScript the field is a plain box for Markdown.

The site understands Markdown with the GitHub additions: tables, strikethrough, task lists, and web addresses that become links by themselves. The formatted view cannot show tables and task lists (`- [ ] item`): a text with one opens in the Markdown view, with a note, and stays there.

```text
## Opening hours

We are open from **Tuesday to Saturday**. See the [workshops](/en/workshops).

| Day      | Hours       |
| -------- | ----------- |
| Tuesday  | 10:00–17:00 |
| Saturday | 10:00–16:00 |
```

- Raw HTML is left out, never rendered, so editors cannot break the page or add scripts.
- Headings get an `id` made of their words, so `[hours](#opening-hours)` links to the heading above.
- Templates get the text as HTML, such as `{{.Body}}` in `page.gohtml`.

The rendering is in the [`markdown`](../markdown/markdown.go) package; modules render their own Markdown with `Frontend.Markdown`. The site keeps rendered texts in memory, up to 32 MB, so a text is not rendered again for every visit. A text with media is rendered again when the media library changes, and after a minute at most, so changes made on other servers show too.

### How the text is saved

A text that is not changed is saved exactly as it was, and so is text changed in the Markdown view. Once a text is changed in the formatted view, the editor writes all of it in one style:

- `*italic*` and `**bold**`, also where the text had `_italic_` and `__bold__`;
- numbered lists as `1.`, `2.`, `3.`, counting up from the first number, also where the text had `1)`;
- headings with `#`, code blocks between ```` ``` ```` and horizontal lines as `---`;
- characters that would mean Markdown, such as a `*` typed as text, with a `\` before them.

## Images and files in the text

The **Image from the media library** button places an image. For other files, and for the Markdown view, every file in **Media** has a **Use in content** field with a ready-made reference:

```text
![Bowls drying on the shelf](media:0192f3a4-5b6c-7d8e-9f01-23456789abcd)
[price-list.pdf](media:0192f3a4-5b6c-7d8e-9f01-23456789abce)
```

An image is written with its alt text in the language of the media screen's tab; any other file becomes a link. Paste the reference in the Markdown view: in the formatted view it stays plain text. The reference is turned into the file's address when the page is shown, so the text never holds the address itself.

- Saving refuses a reference to a file that no longer exists or is in the trash: "A referenced media file no longer exists."
- A file that content refers to, also in an older revision or from content in the trash, cannot be deleted from the library.
- JPEG and PNG images in the text are shown as resized WebP copies, at most 1600 pixels wide, with copies in other widths for small and high-density screens. [Media and images](media.md#images-and-files-in-text) has the details.
- An image written without alt text, such as `![](media:…)`, gets the media library's alternative text in the page's language.

## Drafts and revisions

Pages and the content of the [publishing](../modules/publishing/README.md) and [events](../modules/events/README.md) modules are revisioned. Saving never changes the live site: it creates a new revision, a draft. The editor's button reads **Save draft**, and the admin says "Saved as a draft. Publish it to show it on the website." The published revision stays online until an editor publishes another one.

A revision holds every language of the item. Each language has its own tab in the editor; saving a tab creates a new revision with that language changed and the others as they were.

The overview shows each item's status:

| Status | Meaning |
| --- | --- |
| **Draft** | No revision is published. |
| **Published** | The newest revision is the published one. |
| **Changes not published** | A newer draft exists next to the published revision. |
| **Scheduled** | A publication is planned. |
| **In review** | The newest revision waits for a reviewer; see [Review before publishing](#review-before-publishing). |

## The publication box

The editor of an item has a column on the right, beside the form: the **Publication** box, the box of a [permanent item](#permanent-items) and **Move to trash**. The column stays in view while the form scrolls, and so does the form's save button. On a narrow screen the column comes below the form.

For revisioned content, the **Publication** box shows the selected, the latest and the published revision, and offers:

- **Publish this revision** makes the selected revision public. It needs a complete translation in the site's default language: "Complete the default language before publishing." Where the site asks for a review, it also needs a reviewer's approval; see [Review before publishing](#review-before-publishing).
- **Unpublish** takes the item offline, after the question "Take this off the website?". Its revisions are kept. For a page that published content links to, the admin asks another question; see [Links to a page](#links-to-a-page).
- **Preview this revision** opens it on the website in a new tab; see [Previewing a revision](#previewing-a-revision).
- **Schedule this revision** plans publishing and unpublishing; see below. For a permanent item, such as the home page, it plans publishing only; see [Permanent items](#permanent-items).
- **Revision history** lists every revision with its time, author and note, and its review state where the site asks for a review. The times are in the site's timezone, shown next to the heading. Open one to see it in the editor, preview it or publish it as it is. **Restore as new draft** copies it into a new revision on top, without publishing it.

Who may do what is set per section in the groups' permission matrix: an editor with the right to edit but not to publish can write drafts, and someone with the right to publish puts them online. Where the site asks for a review, a reviewer approves them first. An editor with only the read right sees the editor read-only, with the note "You can view this item but not change it." See [Accounts and permissions](permissions.md).

### Previewing a revision

A preview shows a revision on the website itself, in the site's design, at its own address with `?revision=<id>`, such as `/en/news/open-studio?revision=…`. It opens in the language of the editor's tab and works for content that was never published too.

- Only editors who are signed in and may read the section see it. For everyone else, and for a revision of other content than the address shows, `?revision=` does nothing: the address shows the published page or "not found", so it never tells that a revision exists.
- A page shows at its published address, or where the revision would put it: below its parent page's published address, with the revision's **Web address**. A post or an event shows at its published address or at the one with the revision's **Web address**.
- A revision without an address yet, for example below a page that is not published in that language, cannot be previewed. The admin then stays on the editor: "This revision has no address on the website yet, so it cannot be previewed there."

The layout gets `.Preview` and `.Revision`, with the revision's `Number`, whether it is the `Published` one and `PublishedURL`, the same address without the preview. A banner tells editors what they see:

```gotemplate
{{with .Revision}}
    <p class="preview-bar" role="status">
        {{tl localizer "Preview of revision %d" .Number}}{{if not .Published}} ({{tl localizer "not published"}}){{end}}.
        <a href="{{.PublishedURL}}">{{tl localizer "Show the published version"}}</a>
    </p>
{{end}}
```

Previews are sent with `X-Robots-Tag: noindex, nofollow` and `Cache-Control: private, no-store`, without an `ETag`, so search engines leave them out and no cache keeps them.

## A simple published flag

The other content modules, such as gallery, lists, people and documents, keep one version of each item with a published flag:

- **Publish** and **Unpublish** switch the flag. Publishing needs a complete translation in the default language. **Unpublish** asks "Take this off the website?" first.
- **Save** changes the item at once, also on the live site when it is published, and the admin says "Changes saved.".
- New items start unpublished, at the end of the list. The **↑** and **↓** buttons in the overview set the order; they are hidden while the list is searched.

There is no history, preview or schedule for these items. The **Editing** section of each module's README says how its content is published.

## Scheduled publishing

Revisioned content can be published and taken offline at a planned time. Open the revision to publish, unfold **Schedule this revision** in the publication box and fill in one or both fields:

- **Publish at** publishes the selected revision.
- **Unpublish at** takes the item offline. It must come after **Publish at**.

The times are in the site's timezone (**Site settings › Timezone**), shown next to the fields, and stored in UTC. The box then shows the plan, and **Cancel schedule** removes it. A time in the past is carried out on the next check. A time that cannot be read, or an **Unpublish at** that does not come after **Publish at**, is refused with "Choose when to publish or unpublish; unpublishing must come after publishing."

Publishing or unpublishing by hand cancels the plan: the editor decided now, and the plan would undo it. The admin asks for confirmation first, "Publishing now cancels the schedule. Continue?" or "Unpublishing now cancels the schedule. Continue?", and says afterwards that the schedule was cancelled.

The app checks for due changes every `Config.ScheduleInterval`, 30 seconds by default:

```go
app := webthings.New(webthings.Config{
    ScheduleInterval: time.Minute,
})
```

- A planned change has the same effects as the publish button; for a page, its address and the menu are updated too.
- A change that can no longer be made, for example because the default language's translation is incomplete, is dropped and logged.
- Several servers on one database can all run the check: each change is made by one of them.
- A negative interval stops the background check; `App.RunSchedules(ctx)` runs it once, for example in tests or from a job of your own.
- Permanent items, such as the home page, can be scheduled for publication, not for unpublication.

Modules with revisioned content take part through `Parts.Scheduled`; the plan is a `content.Schedule` (see [`content/schedule.go`](../content/schedule.go)). The [banners module](../modules/banners/README.md) has a window of its own instead: **Show from** and **Show until**.

## Feeds

The pages of the publishing and events modules, such as news and an agenda, offer Atom and RSS feeds, which visitors follow in a feed reader: of the published posts, and of the published events that have not ended (see [Publishing](../modules/publishing/README.md) and [Events](../modules/events/README.md)). Publishing a post or event, by hand or at its planned time, puts it in the feeds of its page, and unpublishing it takes it out. **Items in feeds**, on the **General** card of **Site settings**, sets how many items each feed lists: 10, 20, 50 or 100, 20 by default. The site's code can give the feeds of one instance a size of their own instead (`WithFeedSize`), which the setting does not change.

## Review before publishing

A site can ask for a reviewer's approval before revisioned content goes online. The review is off by default, and while it is off nothing changes.

### Switching it on

The **Review** card in **Site settings** has a box for every section with revisions: **Pages** and each instance of the publishing and events modules. In a ticked section, only revisions that a reviewer approved are published. Only administrators can change the card; editors who may read **Site settings** see it read-only.

Reviewers need the **Review** right in the section's row of a group; see [Accounts and permissions](permissions.md#groups-and-the-matrix).

### The states

| State | Meaning |
| --- | --- |
| **Draft** | Nobody asked for a review. Every new revision starts here, so a changed text needs a new approval. |
| **In review** | An editor submitted the revision for review. |
| **Approved** | A reviewer approved it; it can be published. |
| **Rejected** | A reviewer sent it back, with a note why. |

The publication box shows the selected revision's state, who submitted and reviewed it and when, and the reviewer's note. The revision history shows the state of each revision.

### Who does what

- An editor with the edit right presses **Submit for review** on a draft or rejected revision. The published revision cannot be submitted.
- A newer submission sends an older revision of the item that is still in review back to draft: the newest one is the one to review.
- A reviewer opens the revision and, under **Review this revision**, presses **Approve** or **Reject**. Both take a note of at most 500 characters; a rejection needs one: "Write why the revision is rejected."
- A reviewer who may also publish can tick **Publish this revision now** to approve and publish in one step.
- Someone with both the edit and the review right can approve a revision they submitted. Administrators have every right.

### Publishing needs an approval

- **Publish this revision** shows only for an approved revision. Publishing any other revision is refused, also for editors with the publish right: "This revision needs a reviewer's approval before it can be published."
- Scheduling a publication needs an approved revision too. Unpublishing needs no approval.
- The scheduler checks again when the time comes. A plan made before the review was switched on, for a revision that is not approved, is dropped and logged.
- When the site settings cannot be read, publishing waits for an approval rather than skipping it.

### The dashboard and e-mail

- **Needs attention** on the dashboard lists at most eight revisions that wait for your review, marked **Waiting for review**, the longest waiting first, with their section and who submitted them. It shows only sections where you have the review right.
- The page tree and the overviews of posts and events show **In review** for an item whose newest revision waits for a reviewer.
- When an editor submits a revision, everyone else with the review right in that section gets a short plain-text e-mail in the admin's language, with a link to the revision. It is only sent when the admin can send mail, as for the password reset link (see [Accounts and permissions](permissions.md#passwords)).

## Security settings

The **Security** card in **Site settings** decides how the site's pages protect visitors. Only administrators can change it; editors who may read **Site settings** see it read-only.

- **Other sites may embed pages** is whether other sites may show the site's pages in a frame: **No, only this site**, the default, **Yes, any site** or **No, not even this site**.
- **Content-Security-Policy** is **Off**, the default, **Report only, block nothing** or **Enforce**. With a policy, browsers run only the scripts and styles the site serves itself, which blocks most code injected into a page. With **Report only, block nothing**, browsers block nothing and report what the policy would block, and the site writes it to its log. Check the log, then choose **Enforce**. A theme with styles or scripts written into its pages needs changes first.
- **Outside sources** are other sites the pages may load from, one directive per line followed by addresses, such as `frame-src https://www.youtube-nocookie.com` for embedded videos. The directives are `img-src`, `media-src`, `font-src`, `style-src`, `script-src`, `connect-src`, `frame-src`, `worker-src`, `manifest-src` and `form-action`. The site's own files and its media storage are always allowed.

A line without one of these directives or without addresses, or with a source such as `'unsafe-inline'`, `'unsafe-eval'` or `*`, is refused at the field with "Each line needs a directive, such as frame-src, and addresses, such as https://www.youtube-nocookie.com. Inline code, eval and every site (*) cannot be allowed.", and nothing is saved. The settings apply without a restart. See [Security headers](deployment.md#security-headers) and [A Content-Security-Policy of the site's own](deployment.md#a-content-security-policy-of-the-sites-own).

The server configuration can decide a setting instead of the card (see [Configuration](configuration.md#production)). With `Config.ContentSecurityPolicy` set, **Content-Security-Policy** shows that policy, disabled, with the note "Set by the server configuration." The **Performance** card does the same for **The site keeps pages in memory for** when `Config.OutputCacheTTL` is set, or when `Config.Development` or `Config.ReloadTemplates` turns the cache off: it shows the time that applies, or **Off**. Saving such a card keeps the stored setting of that field and saves the other fields.

## The trash

**Move to trash** moves an item to the trash. The admin asks "Move this item to the trash?" first and says "Moved to the trash." afterwards.

- The item leaves the website at once: its address, the menu, the site search and the sitemap. It also leaves the admin's lists, counts and dashboard.
- Its web address, and any other value that must be unique, is free again for another item.
- A planned publication change is cancelled. The item keeps its revisions, its publication and the media it uses.

**Trash**, under **System** in the admin navigation, lists the deleted items per section, with when and by whom each was deleted. The search field searches them, and a section's tab shows that section alone, 50 items per page. The **Trash** button on every overview, the page tree and **Media** included, opens the trash at that section.

Tick the items, or the box in the head of a section's table to tick all of that section's items on the page, and choose:

- **Restore selected** brings them back as they were. A published item is on the website again.
- **Delete selected for good** removes them with their revisions, after the question "Delete the selected items for good? This cannot be undone."

Both buttons stay disabled until an item is ticked. Without JavaScript they stay enabled and the boxes in the heads are not shown; a post without a ticked item is then refused with "Select at least one item.", and the trash stays as it was.

Restoring refuses an item that cannot come back and says why; the other selected items are still restored:

- another item uses its web address now: "This item cannot be restored: another item uses its web address now.", for a page "Another page at this level already uses this web address, or the address is reserved.";
- a page whose parent page is in the trash: restore the parent first;
- a page whose module is placed on another page now.

Content in the trash still uses its media files, so the library cannot delete those files until that content is deleted for good. The trash is never emptied by itself: items stay until someone deletes them for good.

Seeing a section's trash needs the read right, restoring and deleting for good the delete right; see [Accounts and permissions](permissions.md#the-trash). Accounts and groups do not go to the trash.

## Permanent items

A permanent item always stays on the website. Its editor says so in a box of its own, and has no **Unpublish** and no **Move to trash**; its schedule only has **Publish at**. Unpublishing, planning an unpublication and deleting are refused with the reason, also from a screen opened before the item became permanent. Saving, publishing a newer revision and planning a publication still work.

- Administrators mark an item in its editor with **Make permanent**, and take the mark away with **Remove permanent mark**. Both ask for confirmation first. The administrator also needs the edit right in the section, which they have unless `Config.Authorize` says otherwise.
- An item with a planned unpublication cannot be marked: "Cancel the planned unpublication first: a permanent item stays on the website."
- Some items are permanent by their nature, such as the [home page](pages.md#the-home-page). Their mark cannot be removed. A module names its own with `Permanent` in the `editor.Config` of its section.

## Links to a page

The admin knows which published content links to which page or item: the text of the published pages, and the published content of the modules that report their links, such as posts, events and the button of a banner (see [Using modules](modules.md#links-in-module-content)). A link counts when it leads to an address on the site: a path such as `/en/about`, a relative link, which counts from the address of the text it is in, as a browser follows it (`team` in the text of `/en/about` leads to `/en/team`, `about/team` to `/en/about/team`), or a full address on the site's own host (`Config.BaseURL`). Links to other websites, e-mail and phone links and media files do not count, and neither do drafts.

The admin looks for the links when an editor or the report below needs them, never for visitors. It keeps what it found until something visitors see changes on the same server, and for `Config.CacheTTL` at most, 30 seconds by default, so changes made on other servers show within that time.

### Before a page goes offline

The editor of a published page lists the published content that links to it or to an address below it, such as a page below it or a post on its module page: "2 published items link to this page.", with the title of each, linked to its editor, and its section and language, such as "News · en". Content below the page itself is left out, as it goes with the page. While such links exist, **Unpublish** asks "Published items link to this page. Unpublish it anyway?" instead of "Take this off the website?", and **Move to trash** asks "Published items link to this page. Move it to the trash anyway?".

With the [redirects module](../modules/redirects/README.md), editors who may create redirects also get **Unpublish and redirect**. Choose a page under **Redirect to**, the parent page unless you choose another; the page itself and the pages below it are not offered. The page then goes offline, and its addresses redirect to the chosen page. So do the addresses of the published pages below it and the linked addresses below those, such as a post on a module page. Each address redirects to the chosen page in its own language, or in the site's default language when the chosen page has none in that language. The message afterwards says how many old addresses redirect now.

### A new address

A revision with a new **Web address** or another parent page gives the page, and the pages below it, new addresses once it is published. The editor of a published page shows the change before that, per language, such as `/en/about → /en/about-us`, or "not on the website" where the page would not be on the site in that language. When content links to the old address, it says how many published items do: "2 published items link to the old address."

With the redirects module, **Publish this revision** comes with a box, **Redirect the old addresses to the new ones**, ticked by default. The old addresses of the page, of the pages below it and the linked addresses below those then redirect to their new addresses, so links and search engines that use an old address still arrive. Without the module, or without the right to create redirects, publishing asks first while content links to the old address: "Published items link to the old address, and their links will lead nowhere. Publish anyway?"

### Links to an item

Posts, events, albums, people, directory entries and the categories of the publishing and directory modules have an address of their own, such as `/en/news/hello` or `/en/news/category/tips`. The editor of such an item, once it is published, lists the published content that links to its address in the same way: "1 published item links to this item.", with the title, section and language of each. While such links exist, **Unpublish** asks "Published items link to this item. Unpublish it anyway?", and **Move to trash** asks "Published items link to this item. Move it to the trash anyway?". With the redirects module, editors who may create redirects also get **Unpublish and redirect**. Under **Redirect to**, the page the item is shown on is chosen unless you choose another. The item then goes offline, and its address in each language redirects to the chosen page in that language, or in the site's default language when the chosen page has none in that language.

A revision of a post or an event with a new **Web address** gives the item a new address once it is published. The editor shows the change before that, per language, such as `/en/news/hello → /en/news/hello-again`, and how many published items link to the old address. With the redirects module, **Publish this revision** comes with the box **Redirect the old addresses to the new ones**, ticked by default, as for a page. Without the module, or without the right to create redirects, publishing asks first while content links to the old address.

Albums, people, directory entries and categories have no revisions: saving a published one with a new **Web address** changes its address at once. For editors who may create redirects, the old address then redirects to the new one on its own, and the message says so: "Changes saved. 1 old address redirects now." While content links to the item, its editor says so before you save: "Saving it with another address redirects the old address to the new one." Without the redirects module, or without the right, the old address leads nowhere.

The entries of a directory category have addresses below it, such as `/en/directory/food/bakery`. The category's editor counts the links to them apart, "1 published item links to an address below this item.", and asks before the category goes offline: "Published items link to addresses below this item. Unpublish it anyway?" **Unpublish and redirect** redirects the entries' addresses as well as the category's. Only the category's own address gets a redirect when its **Web address** changes; its entries' old addresses redirect to the new ones without one, as for an entry that moved to another category. The categories of the documents module have no editor of their own, so they have none of this.

### Broken links

**Broken links**, above the page tree under **Pages**, lists the addresses on the site that links in published pages and module content lead to, where the website has no page. Each address shows the content that links there, linked to its editor, and the reason:

- "The address does not start with a language of the website.";
- "The page with this address is not published.", with a link to the page's editor;
- "The page with this address is in the trash.", with a link to the trash;
- "No page has this address."

A link to an address that a redirect leads from, to a page or to another website, is not broken. An address below a module page always counts as found, since the module answers there, so a link to a post that was taken off the site is not listed.

Fix the link in its content, publish the page again, or redirect the address. With the redirects module, choose a page under **Redirect to** next to the address and press **Create redirect**: the address then redirects to that page in the address's language, or in the site's default language when the page has none in that language. Without the module, the report says "With the Redirects module, you can redirect such an address to another page here."; with it, users without the right to create redirects read "You need the right to create redirects."

Seeing the report needs the read right in **Pages**. Redirects made here and in the editor of a page or an item need the create right in the redirects module's row; see [Accounts and permissions](permissions.md#links-and-redirects).

## The edit link on the site

Editors who are signed in to the admin see the public site like visitors do, plus a link to the admin screen of what they look at. The layout gets it as `.Editor`:

```gotemplate
{{with .Editor}}
    <a class="edit-link" href="{{.EditURL}}">{{tl localizer "Edit"}}</a>
{{end}}
```

`.Editor` is a `frontend.EditLink` with the editor's `Name` and the `EditURL`. It is set only when the editor may edit there:

- on a page, it opens that page in the admin;
- on a module item, such as a post, it opens that item;
- on other module screens, it opens the module's section.

The admin screen opens in the language of the page. Visitors, editors without the right to edit and the search and "not found" screens get no link.

Other links into the admin, such as one to the dashboard in the footer, use the template function `adminURL`. It gives signed-in editors the address and visitors `""`, so the admin's address stays off public pages:

```gotemplate
{{with adminURL "admin.dashboard"}}<a href="{{.}}">{{tl localizer "Admin"}}</a>{{end}}
```

See [Template functions](template-functions.md#adminurl).

The admin's session cookie reaches the public site for this. It is only checked when it is present, so visitors cost nothing. Pages for signed-in editors are sent with `Cache-Control: private, no-store`, so no cache keeps them or shows them to visitors, and the site renders them anew for every request. For visitors, the site keeps the pages it sent in memory for a while, as **Site settings › Performance** allows: 1 minute by default. A change made on the same server shows to them at once all the same, in the admin and when content is published at its planned time. A change made by another server of the site, and what changes with time alone, such as an event that has passed, can take that minute; see [The output cache](deployment.md#the-output-cache). Visitors' browsers and caches keep pages as **Site settings › Performance** allows too: by default they ask the site every time, so a published change shows at once; with a time set there, it can take that long, plus the refresh time when one is set. Middleware can tell editors apart with `frontend.Editor(ctx)`; the [stats module](../modules/stats/README.md) uses it to leave editors out of its counts. See [`frontend/editor.go`](../frontend/editor.go).

## The dashboard

The admin opens on a dashboard of what is going on. The page shows at once, with its buttons; its three lists load right after it, each on its own:

- **Needs attention** lists the revisions that wait for your review (see [Review before publishing](#review-before-publishing)) and the sections with a count, such as comments to approve.
- **Recently edited** shows the items changed last, with who changed them and when, in the site's timezone.
- **Scheduled** lists the planned publication changes, the soonest first, each marked **Goes online** or **Goes offline**.
- Buttons create something new, such as a post or an event; with many sections they are collected in a menu.
- **View website** opens the site.

A time on the day before, of or after today reads "Yesterday 09:30", "Today 14:05" or "Tomorrow 08:00", by the dates in the site's timezone, also on a day its clocks skip midnight; other times show their date, such as "5 Oct 2026 14:05".

Each list shows at most eight entries. A list waits at most five seconds for the sections. A section that fails or takes longer is left out, and the list names it below the others, for example "Could not load: News, Agenda". Reload the page to try again.

Each editor sees only the sections they may read, and the buttons only where they may create.

The breadcrumbs in the header of every admin page start at **Dashboard**, followed by the section and the page, such as Dashboard › News › Open studio; on the dashboard they show **Dashboard** alone. Pages for people who are not signed in, such as the sign-in page, have none.

Until the setup guide is finished, administrators see a card above the lists: "Finish setting up your site", with the steps done, such as "2 of 5 steps done", and **Continue setup**. Once every step is done or skipped, it reads "Everything is ready — finish setting up" and "All steps done", with **Finish**. Editors never see the card or the guide; see [Set up the site](getting-started.md#set-up-the-site).

## Pages from the setup guide

The **Pages** step of the setup guide makes a site's first pages. It has a row for every module instance that can be on a page, and for three ordinary pages: **About us**, **Contact** and **Privacy**. Each row has a box, **Create page**, and a title in each language, filled in with the row's title; the boxes of the modules are ticked. **Create the ticked pages** makes each ticked page below Home:

- a module's page is published at once, so visitors can reach the module;
- an ordinary page is a draft, for you to write first.

A row that has a page already shows it, such as "On page News", and has no box, so posting the step again creates nothing twice. An ordinary page counts as there when a page below Home has its title in any language. A title that makes no address is refused at its row: "A page with this address exists already, or the title makes no address. Choose another title." Once there are pages, the step offers **Write the first content**, the dashboard's buttons that create something new.

## Who else is on this page

The admin's header shows a round chip with the initials of every other person who has the same page open, in the same colour on every page. Pointing at a chip shows "Anna is also on this page". At most five chips show; more people show as one chip such as **+3**, which names them when you point at it. You never see yourself, also not with several tabs open.

- The chips show names only, never e-mail addresses. An account without a name shows as "Another user", with **?** as its initials.
- Every page of a section you may read has the chips: its overview, an item's editor and its other screens. The dashboard, **Your account**, **Trash** and the forms for a new item have none.
- All language tabs of an item are one page, and so are its older revisions: everyone in the item's editor counts.
- People appear as soon as they open the page and disappear when they close it or go to another one. Someone who signs out disappears at once. A tab that ends without closing, for example on a laptop that goes to sleep, disappears within about a minute, and so does someone whose account is disabled.
- A tab in the background still counts as present. After 30 seconds it stops following who comes and goes, and catches up when you return to it.

The chips lock nothing. Two people can still change the same item at the same time, and the later save wins for everything on its screen, such as the text of its language tab; the other languages stay as they were. With revisions, the earlier save stays in the **Revision history** and can be restored as a new draft. Content with a simple published flag keeps only the later save.

## Counts and notes in the admin

Some sections show a count next to their name in the navigation when something waits for an editor: comments to approve in the [comments module](../modules/comments/README.md), new entries in the [forms module](../modules/forms/README.md) and proposed news in the [submissions module](../modules/submissions/README.md). The sections are asked at the same time, and a count that takes longer than a second is left out on that page, so a slow section never holds up the admin.

Overviews can show a muted note after an item's title, such as a download count in the [documents module](../modules/documents/README.md), and "Missing translation" for items that are not complete in the language of the tab.

## Searching and messages

Lists in the admin, such as pages, media, accounts and each module's items, have a search field at the top. It searches while you type and finds the items that contain every word, ignoring case and accents. **×** or Escape clears it. An empty overview says "Nothing here yet." and, for editors who may create, offers the section's "New …" action where it has one, such as **New post**; a search without results says so instead.

Long lists are split into pages, usually of 50 items, with **Previous** and **Next** below them; the media library shows 48 files per page. A search starts again at the first page. Lists that are read from the database a page at a time, such as comments, form submissions and newsletter subscribers, search the first 5000 items of the tab only.

Messages after an action, such as "Changes saved.", appear at the bottom right, or across the bottom on a phone; only the sign-in and password reset pages show theirs on the page. A value the admin refuses, such as an e-mail address that is already in use or a password that is too short, is shown at its field. So is a value it cannot read, such as letters in a number field or a choice that is not in the list, and the form is not saved. A required field with only spaces counts as empty, with "This field is required.", and spaces around a value do not count toward a minimum length. Required choices, such as the default language, are marked like other required fields. A group of checkboxes, such as **Languages** in the site settings or a post's **Categories**, is a fieldset with the field's name as its legend, so screen readers announce the name with each box. Confirmations disappear after five seconds, but not while the pointer or the focus is on them; warnings and errors stay until they are closed.

A request the admin cannot carry out shows an error message the same way, and the page stays as it is: "The request failed. Please try again." after an error on the server (status 500), "You are not allowed to do this." without the right (403), "This request could not be read." for a post that no admin form sends (400), and "Upload request too large." for a post larger than the admin accepts (413). Without JavaScript, the browser shows the sentence alone.

Date and time fields, such as **Publish at**, show and read times in the site's timezone, as visitor forms on the site do. An event's **Start** and **End** are read as typed, in the event's own **Timezone**, and a new event starts at the next full hour there, also in a timezone that is a half or a quarter of an hour off, such as Asia/Kolkata. A start or end that cannot be read is refused at its field. Number fields take a decimal point, not a comma, such as a directory entry's **Latitude** and **Longitude**, which must lie between −90 and 90 and between −180 and 180.
