# Accounts and permissions

Everyone who works in the admin signs in with an account of their own. Administrators may do everything; editors get the rights of their groups, section by section.

## Administrators and editors

Every account has one of two roles:

| Role | May |
| --- | --- |
| Administrator | do everything, including managing accounts and groups and downloading backups |
| Editor | do what their groups allow; nothing else |

An editor without groups sees only the dashboard and their own account under System. The dashboard shows only the sections an editor may read, and "New …" buttons only where they may create. Give editors rights through groups, and keep the administrator role for the people who manage the site.

The admin's header shows who else has the same page open; see [Who else is on this page](editing.md#who-else-is-on-this-page). It does so only on pages of sections the viewer may read, and only for others who may read them too. It shows names, never e-mail addresses.

## The first administrator

On an empty database, the first start creates an administrator from `Config.AdminEmail` and `Config.AdminPassword`:

```go
app := webthings.New(webthings.Config{
    AdminEmail:    os.Getenv("WEBTHINGS_ADMIN_EMAIL"),
    AdminPassword: os.Getenv("WEBTHINGS_ADMIN_PASSWORD"),
})
```

Once any account exists, both are ignored, so changing them later changes nothing. The password needs at least 12 characters; a shorter one stops the start with an error. The starter reads both from the environment and `task run` sets local defaults, as [Getting started](getting-started.md) describes.

Without them, a start without accounts logs a one-time link to the setup screen, `/admin/setup?token=…`, on `Config.BaseURL` or else on the address the site listens on. `/admin` is the default admin path; [`Config.AdminPath`](configuration.md#admin-path) can move the admin, and this guide's addresses with it. Every start makes a new link. That form creates the first administrator, signs them in and opens the setup guide (see [Set up the site](getting-started.md#set-up-the-site)). Without the link's token, and once any account exists, the address answers 404. The sign-in page says to use the link from the server's log: "search the log for “no account exists yet”". While no account exists, opening the sign-in page also logs the link again, at level WARN and at most once a minute, so it is at the end of the log. You can also create the account in Go, after `app.Start`:

```go
_, err := app.Auth.CreateUser(ctx, "anna@example.com", "Anna", password, auth.Admin)
```

To keep the password out of the code and the environment, `app.InviteAdministrator(ctx, email)` creates an administrator with a password nobody knows and returns a link to the admin's password reset page, where the account's owner chooses one. The link works for an hour (`auth.ResetLifetime`). It is on `Config.BaseURL`, or without one the path below the site's address, such as `/admin/reset-password?token=…`. It works once the app has started, also on a site with accounts; before `Start` it returns `webthings.ErrNotStarted`, for an address that is no address `auth.ErrEmail`, and for one that has an account `auth.ErrDuplicate`. Give the link only to the owner of the address: it opens the account. The starter's `go run . admin you@example.com` prints such a link.

## Accounts

**Accounts** in the admin is for administrators only. It lists every account with badges for its role and status, and the search field finds one by e-mail address or name. Next to the list is the form for a new account: an e-mail address, an optional name, a password of at least 12 characters and a role.

Choosing an account shows a card with its name, role, status and when it was created; **New account** at the top brings back the form. Below the card, an administrator can:

- change its e-mail address and name;
- change its role;
- choose its groups;
- disable it, or enable it again. A disabled account cannot sign in, and disabling it ends its sessions;
- see its sessions that have not expired, with when each was created, expires and was last used, and revoke one;
- see whether its two-step sign-in is on, and turn it off for someone who lost both their phone and their recovery codes (see [Two-step sign-in](#two-step-sign-in));
- delete it, with its sessions and group memberships. This cannot be undone: accounts do not go to the trash. Nobody can delete their own account.

At least one active administrator must remain: the last one cannot be disabled, made an editor or deleted.

### Passwords

A password is set when the account is created. After that, only the account's owner changes it:

- **Your account**, in the admin navigation of every account, changes your own password. It asks for the current one first, and wrong guesses count towards the sign-in limit (see [Sessions](#sessions)). Your other sessions are signed out.
- **Forgot your password?** on the sign-in page mails a link to choose a new password. The link works once, within an hour; an account gets at most three links per hour, and the page answers the same for addresses without an account. Choosing the new password signs the account out everywhere.

The admin offers the reset only with `Config.Mail` and `Config.BaseURL`, so the link is sent and points to the site (see [E-mail](mail.md#links-in-e-mail)). With `Config.Insecure`, for local development, it is always offered; without a sender the link is written to the log.

### Two-step sign-in

Every account can add a second step to signing in: after the password, the admin asks for a code from an authenticator app on a phone, such as Google Authenticator, Microsoft Authenticator, 1Password or Aegis. It is off until the account's owner turns it on on **Your account**, and only the owner can turn it on.

On the card **Two-step sign-in**, **Turn on two-step sign-in** shows a QR code and the same key as text, in groups of four characters, for an app that cannot scan. Scan the code or type the key into the app, enter the code the app then shows under **Code from the app**, and choose **Turn on**. Until that code is right nothing changes: the password alone still signs in, and **Cancel** drops the setup. The QR code is only shown to the signed-in account itself, and only during the setup; once two-step sign-in is on, the key is never shown again.

Turning it on shows 10 recovery codes, such as `4f7k2-9xq3m`, under **Your recovery codes**, with "Keep these codes somewhere safe, such as a password manager." They are shown only this once. Each code signs in once instead of a code from the app, for example after losing the phone; capitals, spaces and the dash do not matter. The card says how many are left. **Make new recovery codes** asks for a code, from the app or a recovery code, and replaces all of them with 10 new ones, which are shown once as well; the old ones stop working.

With two-step sign-in on, signing in takes two pages. After a correct password, the second page asks "Enter the code from your authenticator app." and also takes a recovery code ("Lost your phone? Enter one of your recovery codes instead."). The password alone signs nobody in:

- The browser has 5 minutes for the code (`auth.PendingLifetime`). After that, and after a password change or reset, it starts again at the password, with "The sign-in expired. Enter your password again."
- A code from the app works during its 30 seconds and during the 30 seconds before and after, to allow for a phone's clock that is a little off. It works once: a code that was used, or an older one, is refused with "This code is not correct."
- Wrong codes count towards the same limit as wrong passwords: 10 failures for an address within 15 minutes block it, whether passwords or codes (see [Sessions](#sessions)). A correct password does not reset the count; only a completed sign-in does.
- **Forgot your password?** does not skip the code. Choosing a new password signs the account out everywhere, and signing in with the new password asks for the code as before.

To turn two-step sign-in off, enter the current password and a code, from the app or a recovery code, on the card and choose **Turn off two-step sign-in**. Wrong passwords and codes count towards the sign-in limit here too. The recovery codes are deleted with it.

For someone who lost both their phone and their recovery codes, an administrator turns it off under **Accounts**. The account's card shows **Two-step sign-in**, **On** or **Off**, and with it on, **Turn off two-step sign-in** asks "Turn off two-step sign-in for this account? It then signs in with its password alone, until its owner turns it on again." first. It is offered for other accounts only: administrators turn off their own on **Your account**, with a code, like everyone. Each time is logged at level Info as `two-step sign-in turned off by an administrator`, with the account (`user`, `email`) and the administrator (`administrator`, `administrator_email`). Editors cannot do this: **Accounts** is for administrators only.

The secret that the app shares with the site is stored encrypted with AES-256-GCM, bound to its account, with a random key of the site named `two_step_sign_in`. The key is made on first use and kept in the database with the site's settings (`wt_setting`), the same for every server of the site. Recovery codes are stored only as SHA-256 digests. Neither the secrets nor any code is ever logged. As the key lives in the same database, the encryption keeps the secrets out of a copy of the account tables alone, not out of a copy of the whole database: keep backups as safe as the site itself (see [Backups](databases.md#backups)).

Two-step sign-in needs the site's keys, which `webthings.App` gives the accounts service at `Start` (`auth.Service.UseSecrets`). An admin built in Go around an `auth.Service` without them does not offer it.

## Groups and the matrix

**Groups** is for administrators only. A group has a name and a matrix of rights: a row for every section of the admin, and a column for every action.

The rows are **Pages**, **Media**, **Site settings** and a section for every module instance in `main.go`, such as **News** or **Agenda**, under the same headings as in the admin navigation. The columns are the actions:

| Action | In Pages and module content |
| --- | --- |
| Read | see the list, open items and preview them on the site |
| Create | add a new item |
| Edit | save changes, restore an older revision, change the order, submit a revision for review |
| Publish | publish, unpublish and schedule |
| Delete | move an item to the trash, restore it or delete it for good |
| Review | approve or reject a revision submitted for review |
| Export | download personal data as CSV, where a section offers it |

The **Review** column has a box only in sections with revisions: **Pages** and each instance of the publishing and events modules. The right matters only where **Site settings › Review** asks for a review. There, publishing and scheduling need an approved revision, also with the publish right, and a reviewer who may publish can approve and publish in one step. See [Review before publishing](editing.md#review-before-publishing).

The **Export** column shows only when a section offers an export, and has a box only in those sections: each instance of the newsletter module (its subscribers) and of the registrations module (the registrations of a subject). An export holds personal data, so read is not enough: only editors with the export right see **Export CSV**. No group has it until an administrator ticks it.

In **Media**, read opens the library, create uploads, edit changes titles and alternative texts, and delete moves files to the trash, restores them or deletes them for good. **Site settings** uses read and edit, and its rights also cover **Theme**. Only administrators change its **Review** and **Security** cards, which decide who may publish without a review, which sites may show the pages in a frame, and the site's Content-Security-Policy with its outside sources (see [Security settings](editing.md#security-settings)); others see them read-only, and a save of theirs is refused with 403. Modules with screens of their own use the same actions; their READMEs say what needs which, for example archiving a form submission needs edit, and accepting a proposed news item needs publish.

Boxes in the headings select a whole column (one action in every section), a row (every action of a section), a heading's sections or, at the top left, the whole matrix. A box shows a dash when only part of what it covers is selected. The column headings stay in view while you scroll.

Every screen opens with read, so any other right in a row grants read as well: checking a box checks read in its row, clearing read clears the row, and saving adds read to every row with another right. Editors without publish do not see the publish buttons; they save drafts that someone else publishes.

With read alone, an editor sees an item's editor, a media file's texts, **Site settings** and the theme's settings read-only, with a note that they can view but not change them: their fields are disabled and there is no save button. Buttons that need another right are hidden, such as the upload form in **Media** without create, and **Use this theme** without edit in **Site settings**. A screen or a post without the right is refused with status 403 and "You are not allowed to do this.": an error message at the bottom right for a request from the admin's pages, the sentence alone for any other; see [Searching and messages](editing.md#searching-and-messages).

Add editors to a group on their account's page. An editor in several groups gets all of their rights together. Changes to a group apply from the editor's next request.

A typical setup for a news site:

| Group | Rights |
| --- | --- |
| Writers | News: read, create, edit · Media: read, create · Pages: read |
| Editors-in-chief | News: everything · Pages: read, create, edit, publish · Media: everything |

With News ticked under **Site settings › Review**, the writers submit their posts and the editors-in-chief, who have the review right in News, approve and publish them.

### Sections without a row

Some sections are not in the matrix:

- **Accounts**, **Groups** and **Backup** are for administrators only. A module can mark its own admin sections the same way with `Section.AdminOnly`.
- **Backup** downloads the database, with the password hashes of the accounts, and every uploaded file (see [Backups](databases.md#backups)). No group right opens it, also not a group with every box ticked: an editor does not see it in the navigation, and the screen and its download are refused with 403.
- A section that shares another section's rights has no row of its own. **Theme** uses **Site settings**, and a news instance's categories use the rights of the news instance. A module sets this with `Section.Permission`, the ID of the section whose rights apply. See [The admin of a module](module-admin.md#the-admin-section).

### The trash

Deleting moves an item to the trash, under **System** in the admin navigation. The trash follows the rows of the matrix:

- Read in a section shows that section's deleted items, with who deleted them. Editors without read in any section with a trash do not see **Trash**.
- Delete in a section restores its items and deletes them for good.

Edit in **Site settings** decides, on its **Trash** card, after how many days the trash deletes items for good by itself, in every section.

Accounts and groups do not go to the trash: deleting one cannot be undone. See [The trash](editing.md#the-trash).

### Links and redirects

The editor of a published page lists the content that links to it to everyone who may read **Pages**, and the editor of a published post, event, album, person or directory entry to everyone who may read its section. **Pages › Broken links** needs the read right in **Pages**. The redirects that the editors and the report offer, with the [redirects module](../modules/redirects/README.md), need the create right in the module's row, **Redirects**:

- **Unpublish and redirect** also needs publish in **Pages**, or in the item's section;
- the box **Redirect the old addresses to the new ones** comes with **Publish this revision**, so it needs publish in **Pages** or in the section as well;
- saving an album, a person or a directory entry with a new **Web address** redirects its old address for users who may save it, with edit in its section;
- **Create redirect** in the report needs nothing more.

Without the create right, editors see none of them, and the report tells them "You need the right to create redirects." A post of theirs to **Unpublish and redirect** or **Create redirect** is refused with 403, and publishing a revision or saving an item with a new **Web address** makes no redirects for them. See [Links to a page](editing.md#links-to-a-page).

## Replacing the rule

`Config.Authorize` replaces the built-in rule with your own; without it, the built-in rule applies:

```go
app := webthings.New(webthings.Config{
    Authorize: func(ctx context.Context, user auth.User, permission admin.Permission) bool {
        if user.IsAdmin() {
            return true
        }

        // Editors may do everything except delete, in every section.
        return permission.Action != admin.Delete
    },
})
```

`permission.Section` is the section's ID (`pages`, `media`, `site` or a module key), and `permission.Action` one of `read`, `create`, `edit`, `publish`, `delete`, `review` and `export` (`admin.View`, `admin.Create`, `admin.Edit`, `admin.Publish`, `admin.Delete`, `admin.Review`, `admin.Export`).

The function is asked for every check, also for administrators and their **Accounts** and **Groups** screens, so let it allow administrators. The groups are then not used. It can keep those screens from administrators, but it never opens them to editors: sections for administrators only stay so, whatever it returns, and so do the **Review** and **Security** cards of **Site settings** and the permanent mark of an item. It also decides who sees the edit link and previews on the site, and who gets the e-mail about a revision to review.

The function must be safe for concurrent use: requests ask it at the same time, and so does work in the background, such as finding the reviewers to e-mail.

## Sessions

Sign in at `/admin/login` with the e-mail address and password. A session lasts 12 hours from signing in, and **Sign out** ends it at once. Signing in again in the same browser ends that browser's previous session.

After 10 failed attempts for one e-mail address within 15 minutes, signing in with that address is blocked until those 15 minutes have passed. Wrong codes of [two-step sign-in](#two-step-sign-in) count as failed attempts too. The message does not tell whether the account exists.

The session is kept in a cookie named `wt_admin`: HttpOnly, SameSite=Lax and, unless `Config.Insecure` is set, Secure, so it is only sent over HTTPS. Signing in over plain HTTP needs `Insecure`; the starter sets it for local development through `WEBTHINGS_INSECURE_HTTP`. Never set it in production.

Passwords are stored as Argon2id hashes, and sessions, reset links, pending sign-ins and recovery codes only as SHA-256 digests. Expired sessions, pending sign-ins and reset links, and failed sign-ins whose 15 minutes are over, are removed when the app starts and every hour after.

While a sign-in waits for its code, the browser keeps it in a second cookie, `wt_admin_pending`: HttpOnly, SameSite=Strict, Secure like `wt_admin`, and gone after 5 minutes. The site keeps only a digest of it, bound to the account; it is no session, and the code page is the only one that accepts it.

Form posts to the admin from other websites are refused with `403 Forbidden` and "This request came from another website and was refused." (Go's `http.CrossOriginProtection`). Browsers say where a post comes from, so the admin's forms need no token: they keep working after a restart and on every server of the site.

## The admin's language

`Config.AdminLanguage` is the language of the admin, the same for every account:

```go
app := webthings.New(webthings.Config{
    AdminLanguage: "nl",
})
```

English (`en`, the default) and Dutch (`nl`) are built in; texts without a translation show in English. A catalogue in `StateDir/translations/`, such as `nl.po`, changes the admin's texts or adds a language; see [Languages](languages.md#the-admin-language). Mail to staff, such as the notification of a new comment, is written in the same language (see [E-mail](mail.md#language)).

The admin's language is not the language of the content: editors write every site language in the tabs of each page and item. See [Languages](languages.md).

## Edit links on the site

Editors who are signed in to the admin see the public site as visitors do, plus a link to the admin screen of what they look at. Put it in the layout:

```gotemplate
{{with .Editor}}<a class="edit-link" href="{{.EditURL}}">{{tl localizer "Edit"}}</a>{{end}}
```

`.Editor` is set only for a signed-in account that may edit what the page shows: the edit right in the section of that page, post or other item. The link opens the screen in the page's language. `.Editor.Name` is the account's name, or its address without one.

The session cookie reaches the public site for this. It is only checked when it is present, so visitors cost nothing, and pages for editors are sent with `Cache-Control: private, no-store`, so caches never keep or share them and editors always see the latest version. Visitors may get a kept page for as long as **Site settings › Performance** allows. Middleware can recognise editors with `frontend.Editor(ctx)`, for example to leave them out of statistics. See [Editing and publishing](editing.md).

## Next steps

- [Editing and publishing](editing.md) covers drafts, revisions and scheduling.
- [Configuration](configuration.md) lists every setting of `webthings.Config`.
- [Deployment](deployment.md) covers HTTPS and running the site in production.
