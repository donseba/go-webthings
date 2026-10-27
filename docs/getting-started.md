# Getting started

Start a site from the starter repository, run it locally, and find your way around the admin and the templates.

## What you need

- Go 1.27 or newer.
- [Task](https://taskfile.dev) to run the starter's commands. Each task is a single `go` command (`go run`, `go generate`, `go build` or `go test`), so plain `go` works too.
- Nothing else: no C compiler, and no database server. The site uses SQLite in a folder of its own until you configure PostgreSQL or MySQL.

## Copy the starter

The starter is a complete website: `main.go` with the configuration and the modules, and a `templates/` folder with two themes. `default/` is plain HTML without JavaScript that uses every field of its data; `editorial/` is a finished design with HTMX navigation. Editors choose one under **Theme** in the admin.

```sh
git clone https://github.com/donseba/go-webthings-clone my-site
cd my-site
```

Change the module path in `go.mod` to your own. Until go-webthings has a tagged release, the starter's `go.mod` builds against a checkout next to it, so clone that too:

```sh
git clone https://github.com/donseba/go-webthings ../go-webthings
```

For now go-webthings itself builds only against development branches of go-partial, go-router, go-form, go-importmap and go-translator, in a Go workspace; its [README](../README.md#development) names them. Run the starter in that workspace too.

The folder holds:

```text
main.go         configuration and the list of modules
templates/      one folder per theme
  default/        the plain theme: layout, pages and one folder per module
  default/assets/ CSS, JavaScript and images, served at /theme/
  editorial/      a second, complete theme with HTMX navigation
importmap.json  the themes' JavaScript packages, vendored in assets/vendor/
translations/   the website's own texts per language (en.po, nl.po, ...)
var/            database and uploads, created on first start
```

## Run it

```sh
task run
```

Open http://127.0.0.1:8092/ for the site and http://127.0.0.1:8092/admin/ for the admin. `/admin` is the default admin path; [`Config.AdminPath`](configuration.md#admin-path) can move it. The starter's admin is in Dutch; this guide uses its English names, which `WEBTHINGS_ADMIN_LANGUAGE=en task run` shows.

The first start creates the database in `var/`, a published home page, and an administrator from `WEBTHINGS_ADMIN_EMAIL` and `WEBTHINGS_ADMIN_PASSWORD`. `task run` sets local defaults for both, `admin@example.test` and `change-me-please`; use your own for a real site. Once any account exists, these variables are ignored. Started without them, for example with `WEBTHINGS_INSECURE_HTTP=true go run .`, the site logs a one-time link, `http://127.0.0.1:8092/admin/setup?token=…`, where you create the first administrator yourself.

Lost the setup link? Open `/admin/login`: while no account exists, the sign-in page logs the link again, at level WARN and at most once a minute. Search the log for "no account exists yet". Or let the starter create an administrator:

```sh
go run . admin you@example.com
```

It prints a link, valid for one hour, where the account's owner chooses the password, so no password goes into the environment. The command also adds an administrator to a site that has accounts; an address that has an account is refused. It uses `App.InviteAdministrator`; see [Accounts and permissions](permissions.md#the-first-administrator).

`task run` also sets `WEBTHINGS_INSECURE_HTTP=true`, so the admin's session cookie works over plain HTTP. Never set it in production.

### While you work on the templates

`task run` serves the site as in production: templates are read once, and visitors get pages from the [output cache](deployment.md#the-output-cache). While you change the templates, run this instead:

```sh
task dev
```

It serves on the same address with `WEBTHINGS_DEVELOPMENT=true`, which sets `Config.Development` and `Config.ReloadTemplates`. Templates are read again from `templates/` on every request, so an edit shows on the next request without a restart. A failing page shows the error with its template and line. A template with a problem, such as a function the site does not have, stops the start with all such problems listed (see [Checking the templates at start](templates.md#checking-the-templates-at-start)). The output cache is off. The site logs `development mode (WEBTHINGS_DEVELOPMENT): …` when it starts. Never set it in production, because visitors would see the details of errors. Without it, a broken template gives visitors the theme's `500.gohtml` in their language, without the error, which goes to the log; see [Development mode](deployment.md#development-mode).

## Set up the site

After you create the first administrator with the setup link, the admin opens **Getting started**, in Dutch **Aan de slag**: a guide through what a new site needs. Its steps:

1. **Site**: the languages, the timezone, and the site's title and tagline in each language.
2. **Theme**: the theme, its colours and settings, and the logo.
3. **E-mail**: how the site sends e-mail, and a test e-mail to you; see [Deployment](deployment.md#mail-and-storage).
4. **Pages**: a published page for each module and drafts of About us, Contact and Privacy; see [Pages from the setup guide](editing.md#pages-from-the-setup-guide).
5. **Security**: the Security card of **Site settings**, with the Content-Security-Policy; see [Security settings](editing.md#security-settings).
6. **Finish**: what is done and what was skipped, with next steps.

The Site, Theme and Security steps show the cards of **Site settings** and **Theme**, and every step saves at once. **Skip for now** leaves a step for later. The guide remembers which steps are done or skipped, also after a restart, and opens again at the first open step. Until you finish it, the dashboard shows "Finish setting up your site" with the steps done, such as "2 of 5 steps done", and **Site settings** links to it with **Continue setup**. Once it is finished, **Open the setup guide** on **Site settings** brings it back. Only administrators see the guide; editors never do.

## Look around the admin

Sign in at `/admin/`. The navigation has a section for every part of the site:

- **Pages** is the page tree. Every page has a title and a **Web address** per language, the last part of its address, made from the title when left empty; a page can show its own text or a module.
- **Media** is the library of images and files that pages and modules use.
- **Site settings** hold the site's title and tagline per language, the languages, the timezone, the logo and contact details, and the social media profiles. **Theme** chooses the design and its settings.
- **Accounts** and **Groups** decide who may edit what.
- Every module instance in `main.go` has a section of its own, such as **News** or **Agenda**.

To put a module on the site, create a page, choose the instance under **Shows on this page** and publish the page. The module then serves its list and detail pages below that page's address.

## Change the modules

The modules are listed in `siteModules` in `main.go`, which `main` passes to `app.Use` and `main_test.go` starts too:

```go
func siteModules() []webthings.Module {
    return []webthings.Module{
        publishing.Module("news", "News").Group("Content"),
        publishing.Module("blog", "Blog").Group("Content"),
        events.Module("agenda", "Agenda").Group("Agenda"),
        // …
    }
}
```

The first argument is the instance key, the second the title editors see. `Group` places the instance's admin section under a heading of the admin navigation. Add, remove or rename instances and restart. See [Using modules](modules.md), and https://modules.gowebthings.com for what each module offers.

## Change the design

Everything visitors see comes from the active theme. This section describes `templates/default/`, the theme a new site starts with: plain HTML that uses every field of its data, without JavaScript; build your design around it and style it in `templates/default/assets/site.css`. `templates/editorial/` has the same pages with a finished design, regions and fragments for HTMX, and its own settings; the starter's [README](https://github.com/donseba/go-webthings-clone#the-editorial-theme) describes it, and [Themes](themes.md) covers themes.

- `layout.gohtml` wraps every page and gets the site, the menu and the languages.
- The layout places the tags of the page in `<head>` with `{{slot "head"}}`: WebThings writes the title, the description, the canonical and alternate links, the sharing tags, the icons, the feed links and the structured data. A `regions/head.gohtml` of your own adds tags to them (see [Themes](themes.md#the-head-region)).
- `page.gohtml` shows a page, `404.gohtml` the "not found" page, and `500.gohtml` the page visitors get when a template or a module fails, on its own without the layout (see [500.gohtml](templates.md#500gohtml)).
- The layout loads its stylesheets with `{{styles "webthings.css" "site.css"}}`, which links go-partial's `partial-errors.css`, then these files, then the theme's values stylesheet, each with the file's Subresource Integrity (see [Themes](themes.md#assets)); `{{assetScript "site.js" "type" "module"}}` does the same for a script in `assets/`.
- Module templates live in a folder named after the instance key, such as `news/list.gohtml` and `news/post.gohtml`.

After adding a module, write starting points for its templates:

```sh
task templates -- agenda
```

`task templates -- site` writes the layout with its head region and its stylesheet `assets/site.css`, and the page, 404 and search templates. Existing files are never overwritten, and a template that is still missing is reported in the log when the site starts, with the `task templates -- <key>` that writes it. The start also checks every template of every theme, and logs each problem as `template problem` with the file and the line; with `task dev` the site does not start until they are fixed (see [Checking the templates at start](templates.md#checking-the-templates-at-start)). [Templates](templates.md) explains which template gets which data, and [Theme checklist](theme-checklist.md) lists what a theme needs.

## Build it

```sh
task build
```

writes a single static binary to `bin/website`. It needs the `templates/` and `translations/` folders in the folder it runs in, and a writable state folder for the database and uploads (`var/`, or `WEBTHINGS_STATE_DIR`). [Deployment](deployment.md) covers running it in production, and [Databases](databases.md) switching to PostgreSQL or MySQL. The site can also send a strict Content-Security-Policy, which administrators turn on under **Site settings › Security** (see [A Content-Security-Policy of the site's own](deployment.md#a-content-security-policy-of-the-sites-own)).

## Next steps

- [Configuration](configuration.md) lists every setting of `webthings.Config`.
- [Pages](pages.md) explains the page tree and how modules are placed on it.
- [Templates](templates.md) and [Themes](themes.md) cover the design.
- [Languages](languages.md) covers multilingual sites and translating your template texts.
