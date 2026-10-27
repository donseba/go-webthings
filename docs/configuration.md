# Configuration

A site configures go-webthings with one Config value in its main.go. This guide lists every field, the starter's environment variables and the methods of the app.

## An example

```go
app := webthings.New(webthings.Config{
    Name:           "Atelier",
    StateDir:       "/var/lib/atelier",
    Driver:         "pgx",
    DSN:            os.Getenv("WEBTHINGS_DB_DSN"),
    Locales:        locales.Config{Default: "nl", Enabled: []string{"nl", "en"}},
    AdminLanguage:  "nl",
    AdminEmail:     os.Getenv("WEBTHINGS_ADMIN_EMAIL"),
    AdminPassword:  os.Getenv("WEBTHINGS_ADMIN_PASSWORD"),
    Templates:      os.DirFS("templates"),
    Translations:   translations,
    BaseURL:        "https://atelier.example",
    TrustedProxies: []string{"127.0.0.1"},
    Mail: mail.SMTP{
        Host:     "smtp.example.com",
        Username: os.Getenv("SMTP_USER"),
        Password: os.Getenv("SMTP_PASSWORD"),
        From:     "Atelier <hello@atelier.example>",
    },
})
```

Every field is optional. The fields are defined in [webthings.go](../webthings.go).

## All fields

| Field | Default | What it does |
| --- | --- | --- |
| `Name` | `WebThings` | The admin's title, and the site title in every language on first start |
| `StateDir` | `var` | Folder for the SQLite database, uploads and catalogues that change the admin's texts |
| `Driver` | `sqlite` | Database driver: `sqlite` (`sqlite3` works too), `pgx` or `mysql` |
| `DSN` | SQLite file in `StateDir` | How to reach the database |
| `Locales` | English only | Content languages on first start |
| `AdminLanguage` | English | Language of the admin, such as `en` or `nl` |
| `AdminPath` | `/admin` | Where the admin is served, such as `/toolbox` |
| `AdminEmail`, `AdminPassword` | none | The first administrator, on an empty database |
| `Insecure` | `false` | Allows the admin's session cookie over plain HTTP |
| `Authorize` | built-in rules | Replaces the admin's permission rules |
| `Templates` | none | The public site's templates, one folder per theme |
| `Translations` | none: texts as written | The site's own translations, from `frontend.Translations` |
| `Funcs` | none | Extra template functions |
| `Renderer` | none | Another template engine instead of `Templates` |
| `ReloadTemplates` | `false` | Reads the templates again for every request, for development |
| `Assets` | none | Files served at `/assets/`, with versioned addresses and go-partial's browser clients |
| `ImportMap` | none | An import map whose files are served at `/assets/`, with versioned addresses and go-partial's browser clients |
| `FormTheme` | `webthings` | The go-form theme that visitor forms in templates render with |
| `BaseURL` | address of each request | The public address, for absolute links |
| `TrustedProxies` | none | Reverse proxies whose forwarding headers are believed |
| `Mail` | log only | Sends e-mail from the modules and the admin |
| `Storage` | `StateDir/media` | Keeps uploaded media |
| `CacheTTL` | 30 seconds | How long settings and routes are kept in memory |
| `OutputCacheTTL` | the site setting, 1 minute | How long pages for visitors are kept in memory; overrides the site setting |
| `ScheduleInterval` | 30 seconds | How often scheduled publishing runs |
| `Development` | `false` | Shows failing templates in detail and stops the start for every template problem, for development |
| `LogFormat` | none: the site's own logger | `text` or `json`: logs to standard error, with request IDs |
| `LogLevel` | Info | The lowest level logged with `LogFormat` |
| `AccessLog` | `false` | Logs a line for every request |
| `MetricsToken` | none: no `/metrics` | Serves the metrics at `/metrics` to requests with this token |
| `Telemetry` | none | Tracing and other instrumentation |
| `ContentSecurityPolicy` | the site setting, off | The Content-Security-Policy: `off`, `report-only` or `enforce`; overrides the site setting |
| `ContentSecuritySources` | none | Sources the policy allows besides the site's own, by directive |
| `API` | off | The content API: the published content as JSON at `/api/v1/` |

## Site and admin

`Name` is the admin's title. On first start it also becomes the site title in every language; after that, editors change the title under Site settings.

`AdminLanguage` sets the language of the admin. English (`en`, the default) and Dutch (`nl`) are built in; see [Languages](languages.md). Modules also use it for e-mail to staff, such as a notification about a new form entry.

`Authorize` replaces the built-in rules, in which administrators may do everything and editors what their groups allow; without it, those rules apply. When set, it decides every check, also for administrators. It is asked about the Accounts and Groups screens too, so it can keep them from administrators, but it never opens them to editors. It must be safe for concurrent use: requests and work in the background ask it at the same time.

```go
Authorize: func(ctx context.Context, user auth.User, permission admin.Permission) bool {
    // Editors may look at everything but change nothing.
    return user.IsAdmin() || permission.Action == admin.View
},
```

`permission.Section` is `pages`, `media`, `site`, `accounts`, `groups` or a module's key; `permission.Action` is one of `read`, `create`, `edit`, `publish`, `delete`, `review` and `export`. See [Accounts and permissions](permissions.md).

### Admin path

`AdminPath` is where the admin is served: `/admin` by default, or a path of your own, such as `/toolbox`.

```go
AdminPath: "/toolbox",
```

The path starts with a slash and does not end with one. It has letters, digits, `-`, `.`, `_` and `~` only, in one segment or more, such as `/staff/admin`, and no segment `.` or `..`. It cannot be one of the app's other addresses, or above or below one, such as `/media`, `/_`, `/healthz` or `/assets/admin`. Its first segment cannot be the code of an enabled language, such as `/en` or `/en/admin`: the admin would take that language's pages. Otherwise `Start` stops with an error. Later, the site settings refuse to enable a language with that code; see [Languages](languages.md#adding-or-removing-a-language).

Every address of the admin is below the path: the sign-in page at `/toolbox/login`, the setup link at `/toolbox/setup?token=…`, the event stream at `/toolbox/presence` and a module's section at `/toolbox/<key>`. The bare path leads to the dashboard. Nothing else depends on the path: the admin, core and the modules link to admin pages by route name. Go code builds those links with `app.AdminURL` or, in a module, `env.AdminURL`. `app.AdminPath()` returns the path, for tests and tools.

`robots.txt` does not name the admin, so a path of your own is not published there. Admin pages answer with `X-Robots-Tag: noindex, nofollow` instead, which keeps them out of search results also when other sites link to them. The session cookie, `wt_admin`, is set for the whole site, so editors get their edit links on public pages. The cookie that carries the admin's messages across a redirect, `wt_admin_flash`, is set for the admin's path only.

## First start only

Some fields only fill an empty database:

- `Locales` sets the content languages and the default language, for example `locales.Config{Default: "nl", Enabled: []string{"nl", "en"}}`. The default must be one of the enabled languages. Later changes are made in the admin under Site settings; see [Languages](languages.md).
- `AdminEmail` and `AdminPassword` create the first administrator while there are no accounts at all. The password needs at least 12 characters, or the start fails. Once any account exists, both are ignored. Without them, the start logs a one-time link below the [admin path](#admin-path), such as `/admin/setup?token=…`, where you create the first administrator. Without that token, and once any account exists, the address answers 404. See [Accounts and permissions](permissions.md#the-first-administrator).
- `Name` becomes the site title in every language, as above.

The first start also creates and publishes a home page, and sets the timezone to UTC.

## Database and files

`StateDir` is created at start if it does not exist. It holds `site.sqlite` when the site uses SQLite without a DSN and `media/` when `Storage` is not set. A `translations/` folder there, when you add one, holds catalogues that change the admin's texts; the admin reads it and never writes to it. See [Languages](languages.md#the-admin-language).

`Driver` and `DSN` choose the database. An empty `DSN` with `sqlite` or `sqlite3` uses `StateDir/site.sqlite`. See [Databases](databases.md) for PostgreSQL and MySQL.

`Storage` keeps uploaded files somewhere else than `StateDir/media`, for example an S3 bucket. It is a `media.Storage`. Without it, the app keeps them with `media.NewFilesystem` in `StateDir/media` and serves them itself at `/media/`; see [Media and images](media.md#storage).

## Templates

`Templates` is a file system with one folder per theme, such as `templates/default/`, or a single theme with `layout.gohtml` at its root. Without `Templates` or `Renderer`, every page shows a short text that asks for templates.

```go
Templates: os.DirFS("templates"),
```

`Translations` are the site's own translations of the templates' texts, read from `.po` files with `frontend.Translations`. The starter loads them like this:

```go
translations, err := frontend.Translations("translations")
if err != nil {
    log.Fatal(err)
}

// in the Config:
Translations: translations,
```

Templates get `tl`, `tn`, `ctl` and `ctn` from them. Core translates its own texts with them too: the title of the 404 page, the heading and the message of the error page, the names of social networks, go-form's texts in visitor forms, and the names of months and days and the date patterns, which replace the built-in ones, also those of English and Dutch. Without `Translations`, every text shows as written. `app.Frontend.SetTranslations` does the same after `Start`, before the site serves. See [Languages](languages.md#translating-the-templates-texts).

`Funcs` adds template functions to every template in `Templates`:

```go
Funcs: []template.FuncMap{{
    "upper": strings.ToUpper,
}},
```

A site function with the same name as a module's function replaces it in every template, so give the site's functions names of their own unless you mean to replace one. A name go-partial uses, such as `url`, `content`, `locale`, `localizer`, `slot`, `hasSlot`, `requiredSlot`, `deferredSlot`, `targetHeader`, `targetValue` or `targetIs`, or any name starting with `_`, stops the start with `render.ErrReserved`; see [Templates](templates.md#your-own-functions).

`Renderer` replaces `Templates` with another template engine; when both are set, `Renderer` is used. See [Another template engine](renderer.md).

`ReloadTemplates` reads the templates again for every request, so edits show without a restart. It also turns the output cache off, unless `OutputCacheTTL` is set; see [Production](#production). Use it during development only; otherwise every template is read once.

`Assets` is an extra file system served at `/assets/`, like the files of an import map below. `Start` gives each file a version from its content. `{{importmap_url "site.css"}}` is the file's versioned address, such as `/assets/site.css?v=9b2ca0fe143be1a4`, which browsers may keep for a year; a plain `/assets/site.css` is checked with the file's `ETag` on every request. Core adds go-partial's browser clients among the files, so `{{importmap}}` maps `partial-flash`, `partial-sse` and `partial-interactions`, with their integrity; a file of that name in `Assets` replaces core's. With `ReloadTemplates`, a changed file gets a new version without a restart, and no file carries integrity, in the import map or in the tags of `importmap_script` and `importmap_style`, since it may change under a page. Folders are not listed. A theme's own files belong in its `assets/` folder instead, served at `/theme/`; see [Themes](themes.md).

`ImportMap` is an import map of [go-importmap](https://github.com/donseba/go-importmap), such as one of vendored packages loaded with `importmap.FromFS`. Its files are served at `/assets/` with versioned addresses: a request for a file's current version may be kept for a year, any other is checked with the file's `ETag`. Templates get `{{importmap}}`, the import map for the layout's `<head>`, and `{{importmap_url "file"}}`, a file's versioned address; functions of the site's own with these names replace them. Core adds go-partial's browser clients to the map, `partial-flash`, `partial-sse` and `partial-interactions`, unless the map has such a name already; a client file among the map's own files is used instead, and core's copies are served at `/assets/` too. The import map, and the tags of `importmap_script` and `importmap_style`, carry the integrity of every file, the clients' included, except for a map made with go-importmap's `assets.Reload`, whose files may change while the site runs; the files of `importmap.FromFS` keep it, also with `ReloadTemplates`. See [HTMX](htmx.md#loading-htmx). Set `Assets` or `ImportMap`, not both: with both, `Start` stops with an error.

`FormTheme` names the go-form theme that visitor forms in templates render with: `{{form_field .Form "name" nil}}` and go-form's other functions; see [Visitor forms](visitor-forms.md#the-fields) and [Themes](themes.md#forms). Empty uses `frontend.FormTheme`, `webthings`: go-form's markup with the classes of `webthings.css`. A site with CSS of its own registers a theme with its classes, with go-form's `templates` package, before the site serves:

```go
_, err := templates.RegisterClassTheme("atelier", templates.ThemeClasses{
    Wrapper:     templates.StyleOption{Class: "field"},
    Description: templates.StyleOption{Class: "help"},
    Error:       templates.StyleOption{Class: "error"},
    Honeypot:    templates.StyleOption{Class: templates.HoneypotClass},
})
if err != nil {
    log.Fatal(err)
}

// in the Config:
FormTheme: "atelier",
```

`Honeypot` gives the spam trap a class, here `form-honeypot`, which `webthings.css` hides; a theme without `webthings.css` hides it in its own stylesheet. A theme without `Honeypot` gives the trap `form-honeypot` and an inline `style` that hides it. A Content-Security-Policy without `'unsafe-inline'` blocks that style, so then only a stylesheet that hides `form-honeypot` keeps the trap out of sight (see [Visitor forms](visitor-forms.md#the-hidden-fields)).

`app.Frontend.SetFormTheme(name)` does the same after `Start`, before the site serves.

## Production

`BaseURL` is the public address, such as `https://example.com`. It is used in `sitemap.xml`, `robots.txt`, share links, `{{absURL}}`, feeds and the IDs of their entries, and links in e-mail. Without it, each request's own address is used: its `Host` header and whether it came over TLS, or what a trusted proxy reports. The start then logs a warning, unless `Insecure` is set. The admin offers its password reset only with both `BaseURL` and `Mail` (or with `Insecure`), because a link made from the `Host` header could point to another site.

`TrustedProxies` lists the reverse proxies in front of the site, as IP addresses or CIDR ranges such as `127.0.0.1` or `10.0.0.0/8`. Only requests from these addresses may name the visitor's address in `X-Forwarded-For`, `X-Real-IP` or `Forwarded`, and the site's address in `X-Forwarded-Proto` and `X-Forwarded-Host`, or else in the `proto=` and `host=` of `Forwarded`. Behind such a proxy, `r.RemoteAddr` holds the visitor's address with port 0, such as `198.51.100.7:0`, so handlers that read it see the visitor too. An entry that is not an address or range stops the start with an error.

`Insecure` sends the admin's session cookie without the `Secure` flag, so signing in works over plain HTTP. It also offers the password reset without `Mail` and `BaseURL`, with the link written to the log. It is for local development only; never set it in production.

`Mail` sends the modules' e-mail, the admin's password reset links and the setup guide's test e-mail. `mail.SMTP` is built in; without a sender, messages are written to the log. A sender that implements `mail.Describer` says in the setup guide how it sends. See [E-mail](mail.md).

`CacheTTL` is how long site settings, theme values and the published routes are kept in memory. Changes made in the same process apply at once; changes made by another process on the same database show up within this time. Zero means 30 seconds and a negative value turns the cache off. The admin keeps what it found of the [links to pages](editing.md#links-to-a-page) for this time at most too.

`OutputCacheTTL` overrides how long the output cache keeps pages for visitors in memory, so they are sent again without being rendered. The cache is on by default: editors set its time and memory under Site settings → Performance, 1 minute and 32 MB unless they change them. A positive `OutputCacheTTL` keeps pages that long, whatever the setting; a negative one turns the cache off. Zero follows the setting, except that `Development` and `ReloadTemplates` turn the cache off. When the configuration decides the time in one of these ways, Site settings → Performance shows the time that applies for **The site keeps pages in memory for**, or **Off**, disabled, with the note "Set by the server configuration."; saving the card keeps the stored time and saves its other fields. Changes made on this server show at once: in the admin, at planned publication, and from Go through the core services and the bundled modules. For other changes, such as data the site's pages show from elsewhere, call `app.Changed()` after the commit. Changes made by another server, and what changes with time alone, show within the cache's time. [Deployment](deployment.md#the-output-cache) says what the cache keeps, such as a whole page and an HTMX navigation of the same address side by side, and when it renders a page again; [Caching and testing a module](module-caching-and-testing.md#the-output-cache) says what modules do for it.

```go
OutputCacheTTL: 5 * time.Minute,
```

`ScheduleInterval` is how often planned publications are carried out. Zero means 30 seconds; a negative value stops the background scheduler, for example in tests that call `RunSchedules` themselves.

`ContentSecurityPolicy` overrides the site setting **Content-Security-Policy** under Site settings → Security: `off`, `report-only` or `enforce` (`site.CSPOff`, `site.CSPReportOnly`, `site.CSPEnforce`). Empty follows the setting, which is off until an administrator changes it; any other value stops the start with an error. With a policy set, the card shows it, disabled, with the note "Set by the server configuration.", and saving the card keeps the stored setting. `ContentSecuritySources` adds sources to the policy by directive, besides the site's own, such as a host of fonts. They come on top of the setting's **Outside sources** and the theme's `"csp"` in `theme.json`. The start checks them with `site.CheckCSPSources` and stops with an error for a source it refuses, such as `'unsafe-inline'` or `*`. See [A Content-Security-Policy of the site's own](deployment.md#a-content-security-policy-of-the-sites-own).

```go
ContentSecurityPolicy:  site.CSPEnforce,
ContentSecuritySources: map[string][]string{"font-src": {"https://fonts.example.com"}},
```

[Deployment](deployment.md) explains these settings in context.

## The content API

`API` turns on the content API, which serves the published content as JSON at `/api/v1/`, for apps and other frontends; see [Content API](api.md). It is an `api.Config` of package `api`:

```go
API: api.Config{
    Enabled:        true,
    AllowedOrigins: []string{"https://app.example.com"},
},
```

`Enabled` serves the API; it is off by default. `AllowedOrigins` lists the sites whose pages may read the API in the browser (CORS): an origin such as `https://app.example.com`, `*` for every site or `*.example.com` for the subdomains of `example.com`. Without it, the API sends no CORS headers; with it, a request whose `Origin` is not allowed is answered with 403. `RateLimit` is how many requests one client, known by its address, may make per `RateWindow`: zero means 300, a negative value turns the limit off, and a zero `RateWindow` means a minute. More get 429 with `Retry-After`. Behind a reverse proxy, the limit needs `TrustedProxies`, as above.

While the API is on, `/api/v1` is one of `CorePaths()`: no language can have the code `api` and no top-level page the slug `api`. [Deployment](deployment.md#the-content-api) says how it is cached and limited behind a proxy.

## Logs, metrics and development

`Development` shows a failing template in detail, on the site and in the admin: the template and the line, with status 500. It also makes `{{debug .}}` show the value it gets. Use it on your own machine only. Without it, a failing template is logged and the visitor gets the theme's `500.gohtml` with status 500, in the page's language, with the theme's partials and the page's `View` for `@model`. A theme without one gives go-partial's plain error page, with status 500, the heading "Internal Server Error" and the message "Something went wrong. Please try again.", both translated into the page's language with `Translations`, and that language in `lang`. It never names the failed template or the error. The admin shows an error page of its own, in the admin's language, or the message "The request failed. Please try again." for part of a page. `{{debug .}}` then shows nothing. A site's own `Renderer` is not affected. `Development` also turns the output cache off, unless `OutputCacheTTL` is set; responses then say in `X-Cache` how the cache answered: `hit`, `miss` or `skip`. See [Deployment](deployment.md#development-mode).

`Start` checks every template of every theme with the site's functions and logs each problem at WARN as `template problem`, with the file, the line and a hint. With `Development` it fails with all of them at once (`ErrTemplates`), and a template call that would show nothing, such as `{{posts "nieuws" 3}}` for an instance key the site does not have, an asset file the theme does not have or an unknown `{{theme}}` setting, fails its page. Without it, `Start` fails only when every page of the active theme would fail: its layout, regions or partials do not parse or call a function the site does not have, such as one of a module removed from `app.Use`; such calls are logged once at WARN as `template mistake` and show nothing. See [Checking the templates at start](templates.md#checking-the-templates-at-start).

`LogFormat`, `text` or `json`, makes the start log to standard error in that format and makes that logger slog's default. Any other value stops the start with an error. `LogLevel` is the lowest level logged, a `slog.Level`; the zero value is Info. Without `LogFormat`, go-webthings logs with `slog.Default()` as the site set it up, and `LogLevel` does nothing.

`AccessLog` logs a line for every request but those to the health and metrics addresses below, at level Info: method, path without the query, status, bytes, duration, route class and request ID. See [Deployment](deployment.md#logs).

Every request has an ID: the `X-Request-ID` header a proxy sent, when it has at most 64 letters, digits, `-`, `_`, `.` or `:`, or else a new one. Every response names it in `X-Request-ID`, also before `Start`, and `webthings.RequestID(ctx)` returns it. With `LogFormat`, every line logged with a request's context, such as `slog.ErrorContext(r.Context(), …)`, carries it as `request_id`. A site with a logger of its own leaves `LogFormat` empty and wraps its handler with `webthings.LogHandler`:

```go
handler := slog.NewJSONHandler(os.Stdout, nil)
slog.SetDefault(slog.New(webthings.LogHandler(handler)))
```

A panic in a handler, such as a module's, does not stop the site. The request gets a plain 500, and `slog.Default()` logs `panic recovered` at level Error, with the panic, method, path, request ID and stack. When the answer had already started, the connection is cut instead, so the visitor does not get half a page that looks complete. The access log, the metrics and `Telemetry.Middleware` still see the request, with its 500 or the status already sent. Before `Start`, a panic is recovered and logged with its request ID too.

Three addresses are answered before the site, its middleware and sessions, to `GET` and `HEAD` requests; other methods go on to the site, which answers with its 404 page. `/healthz` answers 200 while the process runs. `/readyz` answers 200 when the app can serve (see `Ready` below) and 503 otherwise. `/metrics` serves the metrics only when `MetricsToken` is set, to requests with `Authorization: Bearer <token>`; without a token it answers a plain 404. All three answers are sent with `Cache-Control: no-store`. A site can also serve `Metrics()` itself, for example on an address only its monitoring reaches. See [Deployment](deployment.md#health-checks).

`Telemetry` plugs in instrumentation such as tracing. `Middleware` wraps every request except those three, like the access log and the metrics. That includes the answers the router gives itself, such as the redirect of `//en/` to `/en/`. `RenderStages` run in every render of the themes' and the admin's templates, and `RenderEvents` receive go-partial's diagnostic events. Core brings no tracing library; see [Deployment](deployment.md#metrics-and-tracing).

Compression, how long browsers and caches keep pages, and the output cache's time and memory are not fields but site settings, under Site settings → Performance; only `OutputCacheTTL` can override one of them. See [Deployment](deployment.md#caching-and-compression). Whether other sites may show the pages in a frame is a site setting too, under Site settings → Security, which only administrators can change. That card also holds the Content-Security-Policy, which `ContentSecurityPolicy` overrides, and its outside sources (see [Production](#production)). The site sends the headers for them, and its other security headers, itself; see [Deployment](deployment.md#security-headers). How many items each Atom and RSS feed lists is a site setting as well, **Items in feeds** under Site settings → General: 10, 20, 50 or 100, 20 by default. In Go it is `site.Settings.FeedSize`; `FeedLimit()` gives the number in effect, `site.DefaultFeedSize` (20) for zero, and saving a number below zero or above `site.MaxFeedSize` (100) fails with `site.ErrInvalid`. See [Search and SEO](search-and-seo.md#feeds). After how many days items in the trash are deleted for good is a site setting too, **Delete items in the trash for good after (days)** under Site settings → Trash: empty or 0, never, by default. In Go it is `site.Settings.TrashDays`, and saving a number below zero or above `site.MaxTrashDays` (3650) fails with `site.ErrInvalid`. See [The trash](editing.md#the-trash).

## The starter's environment variables

The starter's `main.go` reads these variables. They are the starter's own choice: change them, or add more, in your copy.

| Variable | Sets | Starter default |
| --- | --- | --- |
| `WEBTHINGS_NAME` | `Name` | `My website` |
| `WEBTHINGS_STATE_DIR` | `StateDir` | `var` |
| `WEBTHINGS_DB_DRIVER` | `Driver` | `sqlite` |
| `WEBTHINGS_DB_DSN` | `DSN` | empty |
| `WEBTHINGS_ADMIN_LANGUAGE` | `AdminLanguage` | `nl` |
| `WEBTHINGS_ADMIN_EMAIL` | `AdminEmail` | empty; `task run` and `task dev` set a local default |
| `WEBTHINGS_ADMIN_PASSWORD` | `AdminPassword` | empty; `task run` and `task dev` set a local default |
| `WEBTHINGS_INSECURE_HTTP` | `Insecure`, when `true` | empty; `task run` and `task dev` set `true` |
| `WEBTHINGS_DEVELOPMENT` | `Development` and `ReloadTemplates`, when `true` | empty; `task dev` sets `true` |
| `WEBTHINGS_BASE_URL` | `BaseURL` | empty |
| `WEBTHINGS_TRUSTED_PROXIES` | `TrustedProxies`, separated by commas | empty |
| `WEBTHINGS_CONTENT_SECURITY_POLICY` | `ContentSecurityPolicy` | empty: the site setting |
| `WEBTHINGS_SMTP_HOST` | `Mail`: a `mail.SMTP` to this server | empty: e-mail goes to the log |
| `WEBTHINGS_SMTP_PORT` | its `Port` | `587` |
| `WEBTHINGS_SMTP_USERNAME`, `WEBTHINGS_SMTP_PASSWORD` | its `Username` and `Password` | empty |
| `WEBTHINGS_SMTP_FROM` | its `From` | none; required with `WEBTHINGS_SMTP_HOST` |
| `WEBTHINGS_LISTEN` | the address to serve on | `127.0.0.1:8092` |
| `WEBTHINGS_THEME` | the theme `task templates` writes into | `default` |

With `WEBTHINGS_SMTP_HOST` set, the starter stops at once when `WEBTHINGS_SMTP_FROM` is missing or `WEBTHINGS_SMTP_PORT` is not a number. It sets the languages in code (`nl` and `en`). Set `WEBTHINGS_BASE_URL`, `WEBTHINGS_TRUSTED_PROXIES` and the mail server before going live. With `WEBTHINGS_DEVELOPMENT=true` it logs `development mode (WEBTHINGS_DEVELOPMENT): …` at start; never set it in production. It reads no variables for logs or metrics; add them in your copy when you need them.

The starter also has an `admin` command: `go run . admin you@example.com` creates an administrator with `InviteAdministrator` (see below) and prints the link, valid for one hour, where its owner chooses the password. Without `BaseURL`, the link is on the address of `WEBTHINGS_LISTEN`.

## The app

`webthings.New(config)` applies the defaults and returns an `*App`; it opens nothing yet. The app's methods:

| Method | What it does |
| --- | --- |
| `Use(modules...)` | Adds module instances. Call it before `Start`. It fails on an invalid or duplicate key. |
| `Start(ctx)` | Opens the database, applies the schemas, creates the first administrator (or logs the setup link), settings and home page, sets up the modules and the admin, and starts the background work: the scheduler, and a cleanup of expired sessions, sign-in attempts and reset links, and of the items that have been in the trash longer than **Site settings › Trash** allows, at start and then every hour. |
| `ListenAndServe(ctx, addr)` | Listens on `addr` and calls `Serve`. |
| `Serve(ctx, listener)` | Calls `Start` unless that already happened, then serves on `listener` until `ctx` is cancelled, and lets running requests finish. It closes the listener and the app when it returns. Use it with a listener of your own, such as one from systemd socket activation. The "serving" log line, and without `BaseURL` the setup link, show the address it really listens on, also with `:0`. |
| `ServeHTTP(w, r)` | The app is an `http.Handler`. Before `Start` it answers 503, except at `/healthz`, `/readyz` (which reports not ready) and `/metrics` (a plain 404 without `MetricsToken`). |
| `Close()` | Stops the background work, closes the modules (last added first), the storage and the database. |
| `WriteTemplates(dir, key)` | Writes starting points for a module's templates, or for `"site"`. Needs no `Start`. An unknown key gives an error that lists the keys it accepts. |
| `TemplateKeys()` | The keys `WriteTemplates` accepts: `"site"`, then the modules with templates in the order they were added. Needs no `Start`. |
| `Translations(dir, args)` | The `translations` command: writes `translations.pot`, or checks the `.po` files. Needs no `Start`. |
| `TranslationCatalog()` | Every text `Translations` writes, as go-translator's `*extract.Catalog`: the templates' texts, core's, the calendar names and the modules' form texts. Needs no `Start`. |
| `RunSchedules(ctx)` | Carries out planned publications now and returns how many changed. |
| `PurgeTrash(ctx)` | Deletes for good, now, the items that have been in the trash longer than **Site settings › Trash** allows, and returns how many it deleted; see [Emptying the trash](deployment.md#emptying-the-trash). |
| `Changed()` | Tells the app that what visitors see changed, so the [output cache](deployment.md#the-output-cache) renders its pages again. Call it after the commit of a change the app does not see itself, such as data the site's pages show from elsewhere. Writes through the core services and the bundled modules tell it themselves. |
| `Service(key)` | The API of a started module instance, or nil. |
| `AdminURL(name, args...)` | The address of a named admin route, such as `AdminURL("admin.login")` or `AdminURL("admin.pages.edit", "id", id)`. Empty before `Start`. It panics on an unknown route or parameter. |
| `AdminPath()` | Where the admin is served, such as `/admin`, for tests and tools; see [Admin path](#admin-path). Links use `AdminURL`. Needs no `Start`. |
| `CorePaths()` | The addresses the app serves itself, next to the site's pages: the admin at its path, `/healthz`, `/readyz`, `/metrics`, `/_`, `/media`, `/assets`, `/theme`, `/sitemap.xml` and `/robots.txt`, and `/api/v1` while `API` turns the content API on, read from the app's own routes. Needs no `Start`. |
| `IsCorePath(path)` | Whether `path` is one of `CorePaths` or below one: `/theme/site.css` is, `/themes` is not. Modules ask `env.IsCorePath`. Needs no `Start`. |
| `SetupToken()` | The token of the setup link, `<AdminPath>/setup?token=…`, when the site started without accounts; empty otherwise. Every start makes a new one. |
| `InviteAdministrator(ctx, email)` | Creates an administrator with a password nobody knows and returns a link, valid for an hour, where its owner chooses one: on `BaseURL`, or the path without it, such as `/admin/reset-password?token=…`. Needs `Start` (`ErrNotStarted` before it), also on a site with accounts. See [Accounts and permissions](permissions.md#the-first-administrator). |
| `Ready(ctx)` | The check behind `/readyz`: nil when the app has started, the database answers, every schema version this start applied is still in the database and the media storage can be reached, when it can tell (`media.Pinger`). |
| `Metrics()` | An `http.Handler` that writes the app's metrics in the Prometheus text format, the same as `/metrics`. It answers 503 before `Start`. |

A module key starts with a lowercase letter, followed by lowercase letters, digits or hyphens, up to 64 characters. `accounts`, `groups`, `pages`, `media`, `site`, `login`, `logout`, `setup`, `forgot-password`, `reset-password`, `account`, `trash`, `assets`, `presence`, `theme`, `mail`, `partials` and `webthings` are reserved.

Some modules also add admin sections named after their key, such as `news-categories` for a publishing instance `news`. A key equal to such a section stops `Start` with "admin section … exists twice: choose module keys that differ …".

Module packages offer a typed `Of` for `Service`, for example `publishing.Of(app, "news")`. After `Start`, the app's fields `DB`, `Auth`, `Media`, `Site`, `Pages` and `Frontend` give the core services, for example to fill an empty site, and `Links` the links between published content (see [Pages](pages.md#links-to-pages)):

```go
if err := app.Start(ctx); err != nil {
    log.Fatal(err)
}

if err := seed(ctx, app); err != nil {
    _ = app.Close()
    log.Fatal(err)
}

// ListenAndServe sees that the app has started and only serves.
if err := app.ListenAndServe(ctx, ":8080"); err != nil {
    log.Fatal(err)
}
```

To serve with your own `http.Server`, for example with TLS, call `Start`, use the app as the handler and call `Close` when the server stops. See [Deployment](deployment.md).
