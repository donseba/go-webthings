# Deployment

A go-webthings site runs as one binary with its templates, its translations and a folder for its state. This guide covers building it, running it behind a reverse proxy and the settings that matter in production.

## Build the binary

```sh
task build
```

runs `CGO_ENABLED=0 go build -tags nodynamic -o bin/website .` and writes the binary to `bin/website`. On Linux it is fully static, so it also runs on Alpine and in a `scratch` image. The demo's `task build` does the same and writes `bin/demo`.

go-webthings is pure Go, including its SQLite driver, so no build needs cgo or a C compiler. Cross-compiling works too, for example on a Mac for a Linux server:

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags nodynamic -o bin/website .
```

Keep `-tags nodynamic` in your own build scripts and Dockerfiles. Without it, the Linux binary loads the system's C library when it starts, because the WebP package can use a `libwebp` installed on the system. It then runs on glibc systems such as Debian and Ubuntu, but not on Alpine or `scratch`. With the tag, it always uses its built-in WebP code.

Editors choose the site's timezone under Site settings, which needs the time zone database. Most Linux servers have it (the `tzdata` package). For a minimal system or container, embed it in the binary:

```sh
CGO_ENABLED=0 go build -tags nodynamic,timetzdata -o bin/website .
```

## What the server needs

The starter reads its templates and translations from folders relative to the working directory, and keeps its state in `WEBTHINGS_STATE_DIR`:

```text
/srv/website/
  bin/website        the binary
  templates/         the themes
  translations/      the site's .po files
/var/lib/website/    the state folder, writable by the site
  site.sqlite        the database, when the site uses SQLite
  media/             uploads and resized images, unless Storage is set
  translations/      optional: catalogues that change the admin's texts
```

When the state folder does not exist, the site creates it, accessible to its own user only. The admin, its assets and texts, the schema files and the modules' default templates are inside the binary. The admin only reads `translations/`, when you add it; see [Languages](languages.md#the-admin-language).

Templates can be embedded too, since `Config.Templates` is any `fs.FS`:

```go
//go:embed templates
var files embed.FS

templates, err := fs.Sub(files, "templates")
if err != nil {
    log.Fatal(err)
}

// in the Config:
Templates: templates,
```

`frontend.Translations` reads a folder on disk, so `translations/` stays in the working directory.

When the site sends mail over TLS or keeps media in S3 over HTTPS, the server also needs the usual CA certificates.

## Settings for production

- `WEBTHINGS_LISTEN`: the address to serve on. Keep `127.0.0.1:8092` behind a proxy on the same machine; use `:8092` in a container.
- `BaseURL`: the public address, such as `https://example.com`. It makes the addresses in `sitemap.xml`, `robots.txt`, share links, feeds and e-mail, and the IDs of feed entries, independent of the request's `Host` header. The admin's password reset needs it, and the start logs a warning without it.
- `TrustedProxies`: the address of the reverse proxy, so visitors are known by their own address and links use the scheme and host the proxy reports.
- `Insecure`: never set it in production. The admin's session cookie must stay HTTPS-only.
- `Development`: never set it in production either. It shows anyone the details of a failing template; see [Development mode](#development-mode). In the starter, leave `WEBTHINGS_DEVELOPMENT` unset. Without it, the start logs the problems of the templates at WARN, as `template problem`, and fails only when every page of the active theme would fail; read the log after a deploy (see [Checking the templates at start](templates.md#checking-the-templates-at-start)).
- `Mail`: without it, e-mail is only written to the log and the admin offers no password reset. The setup guide's E-mail step shows how the site sends and sends a test e-mail; see [Mail and storage](#mail-and-storage).
- `WEBTHINGS_ADMIN_EMAIL` and `WEBTHINGS_ADMIN_PASSWORD`: used on the first start only. Remove them from the environment afterwards. Without them, the first start logs a one-time link, `<AdminPath>/setup?token=…` on `BaseURL` or else on the address the site listens on, where you create the first administrator. `<AdminPath>` is `/admin` unless [`Config.AdminPath`](configuration.md#admin-path) says otherwise. Every start makes a new link until an account exists. While no account exists, opening `<AdminPath>/login` logs the link again, at level WARN and at most once a minute; search the log for "no account exists yet". To keep passwords out of the environment altogether, the starter's `admin` command, such as `bin/website admin you@example.com` with the site's environment, creates an administrator with `App.InviteAdministrator` and prints a link, valid for one hour, where its owner chooses the password. `InviteAdministrator` returns the link on `BaseURL`, or only its path without one, which the starter puts on the address it listens on. See [Accounts and permissions](permissions.md#the-first-administrator).

The starter reads these from its environment, for example:

```sh
WEBTHINGS_BASE_URL=https://example.com
WEBTHINGS_TRUSTED_PROXIES=127.0.0.1
WEBTHINGS_SMTP_HOST=smtp.example.com
WEBTHINGS_SMTP_USERNAME=website
WEBTHINGS_SMTP_PASSWORD=secret
WEBTHINGS_SMTP_FROM='Example <hello@example.com>'
```

[Configuration](configuration.md) describes every field.

## Behind a reverse proxy

go-webthings serves plain HTTP. Put a reverse proxy in front for HTTPS; the browser talks HTTPS to the proxy, so the secure session cookie works without `Insecure`.

The proxy should:

- pass the original `Host` header, which the site uses for addresses when `BaseURL` is empty and to reject form posts from other sites;
- add the visitor's address in `X-Forwarded-For` and the scheme in `X-Forwarded-Proto`, and be listed in `TrustedProxies`. Without that, every visitor has the proxy's address: limits on visitors' form posts and searches then apply to all visitors together, and statistics cannot tell visitors apart. With `BaseURL` empty, links then also start with `http://`. A standard `Forwarded` header (RFC 7239) works too: its `for=` names the visitor when there is no `X-Forwarded-For` or `X-Real-IP`, and its `proto=` and `host=` give the scheme and host when there is no `X-Forwarded-Proto` or `X-Forwarded-Host`;
- accept request bodies of at least 10 MB, the largest upload the media library takes;
- pass the admin's event stream on as it comes. While an admin page is visible, the browser keeps a stream of server-sent events open at `<AdminPath>/presence`, such as `/admin/presence`, for [who else is on the page](editing.md#who-else-is-on-this-page), with a heartbeat every 15 seconds. The site sends it with `X-Accel-Buffering: no`, which turns off nginx's buffering for it unless `proxy_ignore_headers` lists that header, and Caddy passes event streams on at once. A proxy timeout longer than 15 seconds, such as nginx's default of 60, keeps the stream open.

Caddy does all of this by default, including the certificate:

```text
example.com {
    reverse_proxy 127.0.0.1:8092
}
```

With nginx:

```text
server {
    listen 443 ssl;
    server_name example.com;
    # ssl_certificate and ssl_certificate_key

    client_max_body_size 12m;

    location / {
        proxy_pass http://127.0.0.1:8092;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

In both cases `TrustedProxies` is `[]string{"127.0.0.1"}`. Neither needs to add security headers: the site sends them itself; see [Security headers](#security-headers).

`ListenAndServe` limits a request to 30 seconds for reading, including an upload, and 60 seconds for the answer; the admin's event stream has no such limit. A proxy that buffers request bodies, as nginx does by default, keeps slow uploads within that.

The app is also an `http.Handler`. To serve HTTPS without a proxy, call `Start`, use the app as the `Handler` of your own `http.Server` and call `Close` once the server has shut down. Give `Shutdown` a deadline, as `ListenAndServe` does: the admin's event streams stay open until `Close`, so `Shutdown` would otherwise wait for them.

To serve on a listener you opened yourself, such as one systemd hands over with socket activation, call `app.Serve(ctx, listener)`. It works like `ListenAndServe`, shutdown included, and logs the address it really listens on.

## Security headers

The site sends its own security headers, so a proxy need not add them. Once the app has started, every response carries these two: those of the site and the admin, the redirects the router gives itself, such as from `//en/` to `/en/`, and the answers of `/healthz`, `/readyz` and `/metrics`:

- `X-Content-Type-Options: nosniff`, so browsers never take a file, such as an upload, for another type than it has;
- `Referrer-Policy: strict-origin-when-cross-origin`, so other sites learn only the site's address when visitors follow a link there.

Whether other sites may show the site's pages in a frame, such as an iframe, is the setting **Other sites may embed pages** on the Security card under Site settings, `site.Settings.Framing`. Only administrators can change the card; editors who may read Site settings see it read-only, and a post of theirs to it is refused with 403. Pages, module pages, the 404 page and the answers of visitor actions at `/_/<key>/` get its headers:

| Setting | `Framing` | Headers |
| --- | --- | --- |
| **No, only this site**, the default | `same-origin` | `X-Frame-Options: SAMEORIGIN` and `Content-Security-Policy: frame-ancestors 'self'` |
| **Yes, any site** | `any` | none |
| **No, not even this site** | `none` | `X-Frame-Options: DENY` and `Content-Security-Policy: frame-ancestors 'none'` |

Older browsers follow `X-Frame-Options`, newer ones the `frame-ancestors` of the policy. With the default, other sites cannot lay a page over their own to trick visitors into clicking. The setting applies without a restart; other servers on the same database follow within `CacheTTL`. When the settings cannot be read, pages get the headers of `same-origin`. `Settings.FramingPolicy()` returns the value in effect, `same-origin` for an empty `Framing`; saving any other value fails with `site.ErrInvalid`.

Files at `/media/`, `/assets/` and `/theme/`, `sitemap.xml` and `robots.txt` get no framing headers. Admin pages always send `X-Frame-Options: DENY` and `Content-Security-Policy: frame-ancestors 'none'`, whatever the setting. Uploads that `Config.Storage` serves from an address of its own, such as an S3 bucket's, do not pass the site, so their headers are that service's.

### A Content-Security-Policy of the site's own

Core can send a strict `Content-Security-Policy` itself. It lets browsers run only the scripts and styles the site serves, so most code injected into a page does not run. It is off until an administrator changes **Content-Security-Policy** on the Security card under Site settings, `site.Settings.ContentSecurity`, or the site sets `Config.ContentSecurityPolicy`, which wins over the setting (see [Configuration](configuration.md#production)). The card is for administrators only, as above, and the setting applies without a restart. While `Config.ContentSecurityPolicy` sets the mode, the card shows that mode with the control turned off and the note "Set by the server configuration."

| Setting | Value | Headers |
| --- | --- | --- |
| **Off**, the default | `off` | only the `frame-ancestors` of the framing setting, as above |
| **Report only, block nothing** | `report-only` | the policy in `Content-Security-Policy-Report-Only`, and the `frame-ancestors` of the framing setting in `Content-Security-Policy` |
| **Enforce** | `enforce` | the policy in `Content-Security-Policy`, with the `frame-ancestors` of the framing setting |

With report-only, browsers block nothing and report what the policy would block. They ignore `frame-ancestors` in a reported policy, so that directive stays enforced. Pages, module pages, the 404 page, the answers of visitor actions at `/_/<key>/` and the admin get the policy; other responses, such as files at `/media/`, `/assets/` and `/theme/`, `sitemap.xml`, `robots.txt`, the content API and `/healthz`, get none. When the settings cannot be read, the policy is off, unless `Config.ContentSecurityPolicy` sets it.

The policy is go-router's `middleware.StrictCSP`, with no `'unsafe-inline'` and no `'unsafe-eval'`, which core completes for each page. The demo's home page with **Enforce** and the framing setting at its default gets:

```text
Content-Security-Policy: default-src 'self'; script-src 'self' 'sha256-ikhAzHpIhdk6wxu+v3TtAkGM69wDavllgwgrXyKsTBc='; style-src 'self' 'sha256-5WNh79OqlbaM4rZ/DlkOnop5aYED/JCWp9ChTZ5ZOSQ='; img-src 'self' data:; object-src 'none'; base-uri 'self'; form-action 'self'; report-uri /_/csp-report; frame-ancestors 'self'
```

- `script-src` has the hash of the page's import map, which `{{importmap}}` writes inline (go-importmap's `ScriptHash()`). The hash changes whenever a file in the map does; with `ReloadTemplates` it is that of the map the page writes.
- `style-src` has the hash of the `<style>` element of go-partial's built-in error page (`errors.StyleHash()` of `ext/errors`). A theme without `500.gohtml` shows that page, development mode shows it with the error, and the interactions client writes it into a page with `reloadOnError`. With `Config.Development`, `style-src` also has the hash of the `{{debug}}` box (`debug.StyleHash()`).
- When the media storage serves its files from an address of its own, an absolute `http` or `https` address such as an S3 bucket's public URL, that origin is added to `img-src` and `media-src`, such as `img-src 'self' data: https://media.example.com`; see [Storage](media.md#storage).
- The sources of `Config.ContentSecuritySources`, of **Outside sources** on the Security card and of the theme's `"csp"` in `theme.json` (see [Themes](themes.md)) are added to their directives. A directive the policy does not have starts from `default-src`, so `frame-src https://www.youtube-nocookie.com` becomes `frame-src 'self' https://www.youtube-nocookie.com`.
- `report-uri` is where browsers report what the policy blocks; see below.
- `frame-ancestors` comes from the framing setting. With **Yes, any site** the policy has none.

The policy allows inline code by its hash, never by a nonce, so the output cache keeps a page with its policy, and a page it sends again carries the hashes it was rendered with. After a save of the card, the cache renders its pages again, with the new policy.

**Outside sources** takes one directive per line, followed by its sources, such as `frame-src https://www.youtube-nocookie.com https://player.vimeo.com` for embedded videos. `site.CheckCSPSources` checks each line, and also `Config.ContentSecuritySources` at `Start` and a theme's `"csp"`:

- The directive is one of `connect-src`, `font-src`, `form-action`, `frame-src`, `img-src`, `manifest-src`, `media-src`, `script-src`, `style-src` and `worker-src`.
- A source is an address with the scheme `http`, `https`, `ws` or `wss`, such as `https://*.example.com/embed/`; `'self'`; one of the schemes `https:`, `wss:`, `data:` and `blob:`, but not in `script-src`; or a hash such as `'sha256-…'`, in `script-src` and `style-src` only.

Anything else is refused, such as `'unsafe-inline'`, `'unsafe-eval'`, `*`, a nonce, `'strict-dynamic'` or a line without sources. The card then shows "Each line needs a directive, such as frame-src, and addresses, such as https://www.youtube-nocookie.com. Inline code, eval and every site (*) cannot be allowed." at the field, with status 422, and saves nothing. `Start` fails with an error that wraps `site.ErrCSPSource`, also for a theme's `"csp"`. It also fails, with an error of its own, for a `Config.ContentSecurityPolicy` that is not empty, `off`, `report-only` or `enforce`. In Go, `Settings.ContentSecurityPolicy()` returns the setting in effect, `site.CSPOff`, `site.CSPReportOnly` or `site.CSPEnforce`, and `off` for an empty one; saving another value fails with `site.ErrInvalid`. `Settings.ContentSecuritySources` holds the outside sources as text, which `site.ParseCSPSources` reads by directive.

Browsers report what the policy blocks, or would block with report-only, to `/_/csp-report` (the route `csp.report`), in the format of `report-uri` or of the Reporting API. Each violation is logged at level Warn as `content security policy violation`, with the disposition, the directive, the blocked address, the page and the file the violation is in, these three without their query, the line, the column and the first 80 characters of the sample. Nothing is stored. Each visitor may send 30 reports a minute and gets 429 for more, at most 60 are logged a minute in all, and reports about browser extensions are left out. To switch the policy on, choose **Report only, block nothing**, use the site and the admin, read the log, change what it names, then choose **Enforce**.

With report-only or enforce, the admin gets the site's policy with the hash of its own import map (`Admin.ImportMapHash()`) instead of the site's, and `frame-ancestors 'none'`, which stays enforced with report-only. Its pages have no `style` attribute, `<style>` element, inline script or event handler attribute, so the admin works under the policy. A theme's preview in the admin is a page of the site: its policy has the hashes of both import maps and the theme's sources. With the policy off, the preview sends only the admin's `frame-ancestors 'none'`, like every admin page.

What the policy means for the site's templates:

- The pages of the starting points (`App.WriteTemplates(dir, "site")`) and the modules' default templates have no `<style>` element, no `style` attribute, no inline script and no event handler attribute. The spam trap of visitor forms has the class `form-honeypot` instead of a `style` attribute, and `webthings.css` hides it; see [The hidden fields](visitor-forms.md#the-hidden-fields). The bars of a poll's results are `<meter>` elements.
- `{{structuredData}}` writes a `<script type="application/ld+json">`. Browsers do not run such a data block, so `script-src` does not block it.
- A colour or select setting with `"css"` in `theme.json` reaches the page as a custom property of `/theme/<theme>/values.css`, linked by `{{styles}}` or `{{themeStyle}}`, and a focal point as a class from `{{focalClass}}`, whose rule is in `/_/focal-points.css`, linked by `{{styles … "focal-points.css"}}` or `{{assetStyle "focal-points.css"}}`: neither is blocked. A theme's own `style` attributes, `<style>` elements, inline scripts and event handler attributes, such as a `style` attribute with `{{objectPosition}}`, are blocked with **Enforce** and reported with report-only; see [Themes](themes.md). A setting with fixed options, written as a data attribute that the theme's stylesheet reads, is not.
- go-partial's error box, which takes the place of a part of a page that failed, such as the answer to a failing HTMX request in development mode, has classes and no inline style. go-partial's `partial-errors.css` styles it, which core serves at `/_/partial-errors.css`; `{{styles}}` in the layout links it first, and a layout without `{{styles}}` links it with `{{assetStyle "partial-errors.css"}}`.

A site may also send a policy of its own from the proxy in front of it, as a header of its own. A browser enforces every policy it gets, so a page must pass both. A handler around the app may set the header before the app answers instead. With the setting off, core merges the framing setting's `frame-ancestors` into that policy on pages, the 404 page and the answers of visitor actions, directive by directive: the policy keeps its other directives, and a `frame-ancestors` of its own gives way to the setting's, unless the setting is **Yes, any site**. The admin merges `frame-ancestors 'none'` into it the same way. With enforce, the handler's policy stays as well: the site's policy replaces only the directives it names, so others, such as `upgrade-insecure-requests`, stay, and a directive both name, such as a `script-src` with `'unsafe-inline'`, becomes the site's strict one, so it cannot weaken it; `frame-ancestors` follows the framing setting. With report-only, the handler's policy stays enforced, with the framing setting's `frame-ancestors`, and the policy reported is the handler's with the site's directives applied, without `frame-ancestors`. Any further policy, a second header, stays as it is. This holds for pages, the 404 page, the answers of visitor actions and the admin. Other responses, such as files at `/assets/` and `/theme/`, keep the handler's policy as it is. The output cache keeps pages with their headers, so a page it keeps can be sent with the policy it was rendered with: keep the handler's policy the same while the site runs.

## Two-step sign-in

Ask everyone who works in the admin, administrators above all, to turn on [two-step sign-in](permissions.md#two-step-sign-in) on **Your account**: a stolen or guessed password alone then opens no account. It needs no setting. The codes depend on the time, so keep the server's clock right, for example with NTP: a code works for its own 30 seconds and the 30 seconds before and after, and a server whose clock is further off refuses the codes. Phones set their clocks themselves.

The secrets of the authenticator apps are stored encrypted with a key of the site, and the database holds both, so a copy of the database needs the same care as the site (see [Backups](#backups)). Restoring the database restores the key with it. Without the key, such as after the site's settings were lost, a code from the app fails with "The request failed. Please try again." and an error in the log: those accounts then sign in with a recovery code and set up two-step sign-in again, or an administrator turns it off for them.

## Caching and compression

The Performance card under Site settings in the admin decides how pages reach visitors. Its settings apply without a restart; other servers on the same database follow within `CacheTTL`. Its output cache, which keeps pages in each server's memory, has a section of its own: [The output cache](#the-output-cache).

**Compress responses** is on by default. Browsers that accept gzip get compressed responses of 1 KiB or more, on the whole site and in the admin: pages, stylesheets, scripts, JSON, XML and SVG. Other images, video, WOFF fonts and event streams are sent as they are, and so are `HEAD` requests, requests for a range and responses that are encoded already. Responses that could be compressed say `Vary: Accept-Encoding`, so caches keep both versions apart. Turn the setting off when the proxy compresses instead.

**Browsers and caches keep pages for** sets how long pages for visitors may be kept without asking the site. The default is no time: pages are sent with `Cache-Control: no-cache`, so browsers and caches ask every time. With a time, pages are sent with `Cache-Control: public, max-age=…`, and **Then show the kept page while refreshing** adds `stale-while-revalidate=…`. Changes then show after at most that time. An address with a query, such as a search or a form's result, is always sent with `no-cache`.

Every page for visitors also gets a weak `ETag`, a hash of the page, the same whether the page is sent compressed or not. A browser or cache that asks again with `If-None-Match` gets `304 Not Modified` without the page when it has not changed, so asking every time costs little. This applies to complete HTML pages and to the Atom and RSS feeds of modules with status 200, for `GET` and `HEAD`; a page whose module sets a `Cache-Control` of its own keeps that one. Left as they are:

- pages for signed-in editors, which are sent with `Cache-Control: private, no-store`;
- responses that are private or `no-store`, set a cookie, have an `ETag` of their own or are streamed;
- other statuses, such as the 404 page;
- files at `/media/`, `/theme/` and `/assets/`, which keep their own headers.

Feeds, such as a news page's `feed.atom` and `feed.rss`, get the same `Cache-Control` as pages, and also say when they last changed, in `Last-Modified`. That time never goes back: it is when the server first sent the feed as it is now, or the time the feed gives itself, such as that of its newest entry, when that is later, so taking a post off the site moves it forward too. A reader that asks with `If-Modified-Since` alone gets `304 Not Modified` while the feed has not changed, and the whole feed after a change; one that sends the `ETag` in `If-None-Match` is answered by the `ETag`. This holds for feeds the output cache keeps and for those it leaves alone. Each server keeps these times in its own memory, so after a restart, or from another server, a reader that asks by the time gets the whole feed once more. A time the server's clock has not reached yet is not sent, such as that of a feed that changed twice within one second. Pages send no `Last-Modified`, so they are checked by their `ETag` alone.

Pages also say `Vary` for the HTMX request headers, since an HTMX request may get only part of a page. A CDN that keeps pages should pass requests with the admin's session cookie, `wt_admin`, to the site, so editors see the latest pages and their edit links. It should pass requests with a `wt_form_<key>` cookie too: that cookie brings a visitor's post with mistakes back to the form's page, which the site then sends with `Cache-Control: private, no-store` (see [Visitor forms](visitor-forms.md#a-post-with-mistakes)).

Each server also keeps some work in memory, without a setting:

- Markdown rendered to HTML, up to 32 MB in all; when full, the cache starts over. Text that shows media is kept per language, and for a minute at most.
- What it read of each media file, such as its size, focal point and alt texts, for a minute.
- `sitemap.xml`, for 10 seconds, or until what visitors see changes on the same server.

Changes made on the same server show at once; see [The output cache](#the-output-cache). For other servers, see [Several servers on one database](#several-servers-on-one-database).

The site search allows each visitor 30 searches a minute (`frontend.DefaultSearchThrottle`). More get the search page with status `429 Too Many Requests`, without results and with `SearchView.Throttled` set, so the template can say why; see [Search and SEO](search-and-seo.md#the-search-screen). Only searches with a query count. To change the limit, call `app.Frontend.SetSearchThrottle(limit, window)` after `Start` and before serving; a limit of zero turns it off.

## The output cache

Each server keeps the pages it sends visitors in memory and sends them again without rendering them. It is on by default. Two settings of the Performance card set it:

- **The site keeps pages in memory for**: 1 minute by default, or 10 seconds, 5 or 15 minutes, or **Off**.
- **Memory for kept pages**: 32 MB by default, or 8, 64, 128 or 256 MB. When it is full, the pages not asked for the longest are dropped first.

`Config.OutputCacheTTL` overrides the time, and `Config.Development` and `Config.ReloadTemplates` turn the cache off unless it is set; see [Configuration](configuration.md#production).

The cache keeps the answers to `GET` requests for pages, module pages and their feeds, `sitemap.xml` and `robots.txt` that have status 200, with their headers and their `ETag`, and answers `GET` and `HEAD` requests with them. It keeps a page apart for every address, query included, and for the request headers the page varies on, such as those of HTMX and `Accept-Encoding`. A whole page and an HTMX navigation of the same address vary on different headers, so they are kept side by side, up to four such kinds per address; beyond that, the kind stored least recently is dropped. When many visitors ask at once for a page that is not kept, it is rendered once for each kind, and the others wait for it. Cookies count for nothing, such as a site's analytics, except one a module names with `Env.PersonalCookie` because its pages show something per visitor by it, such as the polls module's `wt_poll`: the cache then keeps a page apart for each of its values. Only two kinds of cookie keep a request from the cache, those below; any other cookie of the site that changes what its pages show must be named with `Env.PersonalCookie` (see [Caching and testing a module](module-caching-and-testing.md#the-output-cache)), or visitors get the page kept for someone else. Left out are:

- requests with the admin's session cookie, `wt_admin`, so signed-in editors always get a fresh page;
- requests with a `wt_form_<key>` cookie, which brings a visitor's post with mistakes back to its form (see [Visitor forms](visitor-forms.md#a-post-with-mistakes));
- previews of revisions, with `?revision=`;
- searches with a query;
- other statuses, such as the 404 page and redirects;
- answers that are private or `no-store`, that set a cookie, or whose body is over 1 MB;
- the admin, `/media/`, `/assets/`, `/theme/` and the visitor actions at `/_/`, which have their own caching or none.

Changes made on the same server show at once: changes to the published pages, saved site settings and theme values, changes in the media library, the publications the scheduler carries out, every post to a module's admin section or to the trash, and every visitor's post to an action at `/_/` or to a page. A post answered with an error status changes nothing, and neither does a visitor's post that says it stored nothing, such as spam, a post with mistakes or one over the throttle.

Changes made from Go on the same server show at once too. The pages, the site settings and the media library count their own changes, and the bundled modules' stores tell the app once the write is committed: when an item is published, unpublished, moved to the trash or back, and when an item of a collection, such as a banner, is saved or moved. The services of the comments, polls and registrations modules tell it too. Writes of newsletter subscribers, submissions and redirects do not: no page shows subscribers or submissions, and redirects are answered before the cache. Modules tell the cache about other changes themselves; see [Caching and testing a module](module-caching-and-testing.md#the-output-cache). For other changes, such as data the site's pages show from elsewhere, the site's own code calls `app.Changed()` after the commit.

`sitemap.xml` follows the same changes, so a page published on this server is in the next sitemap at once. Changes made by another server, and what changes with time alone, such as an event that has passed, show once a kept page is older than the cache's time. In `sitemap.xml`, a change made by another server can take up to 10 seconds plus the cache's time.

The cache does not change what browsers and caches are told: a kept page has the same `Cache-Control` and `ETag` as a new one, and a kept feed the same `Last-Modified`. It answers `If-None-Match` and `If-Modified-Since` with `304 Not Modified` itself, from a page it keeps that is still current. A CDN that keeps pages for **Browsers and caches keep pages for** may get a page from the output cache, so a change made by another server can take both times to reach visitors.

The modules' middleware, such as that of the stats and redirects modules, runs before the cache and sees every request for a page, also those the cache answers.

The metrics count the cache's hits, misses, skips and evictions and show what it holds; see [Metrics and tracing](#metrics-and-tracing). With `Config.Development` and `OutputCacheTTL`, responses say in `X-Cache` how the cache answered: `hit`, `miss` or `skip`.

## The content API

With `Config.API` on, the site also serves its published content as JSON at `/api/v1/`; see [Content API](api.md). Its requests pass, in this order, CORS and the rate limit, the output cache, compression and the ETag:

- The [output cache](#the-output-cache) keeps the API's answers as it keeps pages, and the same changes have them made again. Searches with a query are not kept. The API reads no cookies, so the requests of signed-in editors are answered from the cache too.
- Answers with status 200 have a weak `ETag`, and a client that sends it in `If-None-Match` gets `304 Not Modified` while the answer has not changed. Their `Cache-Control` is that of pages, **Browsers and caches keep pages for**, so a CDN may keep them as long as pages. Searches with a query and errors are sent with `no-cache`. A single page, at `/api/v1/pages/{id}` or `/api/v1/pages/resolve`, and a module's single item, such as a post or an event, also have a `Last-Modified`, the latest date of what they show, and `If-Modified-Since` gets `304` while none of it is newer. Lists, the site and searches have none, as an item can leave a list without a newer date; see [Content API](api.md).
- **Compress responses** applies to the API as to pages.
- The rate limit, 300 requests a minute per client unless `Config.API.RateLimit` says otherwise, comes before the output cache, so answers from the cache count too. It knows clients by their address: behind a proxy, list the proxy in `TrustedProxies`, or every client has the proxy's address and they share one limit. A request over the limit gets 429 with `Retry-After`, the window in seconds. Each server counts in its own memory, and a CDN that answers from its own cache keeps those requests from the count.
- CORS also comes before the output cache, so an answer from the cache gets the headers for the origin of each request. With `Config.API.AllowedOrigins`, every answer says `Vary: Origin`, also one to a request without an `Origin`, so a CDN keeps the answers for each origin apart, and an answer without CORS headers apart from those for the allowed origins. Browsers keep the answer to a preflight request for ten minutes.
- Like every response, the API's answers carry `X-Content-Type-Options: nosniff` and `Referrer-Policy`, but no framing headers; see [Security headers](#security-headers).

The metrics and the access log count the API's requests under the route class `api`; see [Metrics and tracing](#metrics-and-tracing).

## Health checks

Two addresses are answered before the site, its middleware and sessions, for load balancers and orchestrators:

- `/healthz` answers 200 `ok` while the process answers requests, also before the app has started. Use it to check that the process is alive.
- `/readyz` answers 200 `ready` when the app has started, the database answers, every schema version this start applied is still in the database, and the media storage can be reached. Otherwise it answers 503 `not ready`, and logs the reason as a warning. It waits at most 3 seconds. Use it to decide whether the server gets traffic.

The built-in storage checks that its folder can be read; other storage is checked when it implements `media.Pinger`, as contrib's `s3` does by checking its bucket. Both answer `GET` and `HEAD` only; other methods go on to the site, which answers with its 404 page. Both answers are sent with `Cache-Control: no-store`, and neither shows up in the access log or the metrics. `App.Ready(ctx)` runs the same check from your own code.

In Kubernetes, with `WEBTHINGS_LISTEN=:8092`:

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8092
readinessProbe:
  httpGet:
    path: /readyz
    port: 8092
```

## Metrics and tracing

`App.Metrics()` writes the app's metrics in the Prometheus text format:

- `webthings_http_requests_total` by route class, method and status code, and `webthings_http_request_duration_seconds` by route class. The classes are `admin`, `action`, `asset`, `media`, `seo`, `operations`, `api`, for the [content API](#the-content-api), and `public`. The health and metrics addresses themselves are left out, so `operations` only counts the router's redirects to them, such as from `//healthz`.
- `webthings_http_requests_in_flight` and `webthings_start_time_seconds`.
- `webthings_render_total` and `webthings_render_duration_seconds` by render kind, for the templates of the themes, e-mail included, and the admin. An e-mail's subject and text count as kind `text`.
- The database's connection pool, such as `webthings_db_open_connections` and `webthings_db_wait_count_total`, and `webthings_db_statements_total`.
- The [output cache](#the-output-cache): `webthings_output_cache_hits_total`, `webthings_output_cache_misses_total`, `webthings_output_cache_skips_total` for the `GET` and `HEAD` requests it left alone, such as editors' and the admin's, and `webthings_output_cache_evictions_total` for pages it dropped to stay within its memory or its four kinds per address; `webthings_output_cache_entries` and `webthings_output_cache_bytes` are what it holds now.
- `go_goroutines`.

With `Config.MetricsToken` set, the site serves them at `/metrics` to requests with `Authorization: Bearer <token>`; other requests get 401. Without a token, `/metrics` answers a plain 404. Its answers are sent with `Cache-Control: no-store`. To keep the metrics off the public address, serve them on an address only the monitoring reaches:

```go
if err := app.Start(ctx); err != nil {
    log.Fatal(err)
}

// The metrics on an address only the monitoring reaches.
go func() {
    internal := http.NewServeMux()
    internal.Handle("GET /metrics", app.Metrics())

    log.Println(http.ListenAndServe("127.0.0.1:9100", internal))
}()

if err := app.ListenAndServe(ctx, ":8092"); err != nil {
    log.Fatal(err)
}
```

Each process counts its own requests, so scrape every server. The counts leave out the health and metrics addresses. They include the answers the router gives itself, such as the redirect of `//en/` to `/en/`.

`Config.Telemetry` plugs in tracing and other instrumentation. `Middleware` wraps the same requests as the metrics, inside the request ID and outside the access log. `RenderStages` run in every render of the themes and the admin, and `RenderEvents` receive go-partial's diagnostic events, for example go-partial's `exp/otel` sink. Core brings no tracing library of its own: integrations with OpenTelemetry belong in go-webthings-contrib, with the other outside dependencies.

## Logs

Without `Config.LogFormat`, go-webthings logs with Go's default logger, `slog.Default()`, as the site set it up; the starter leaves it alone, so the log is text on standard error. `LogFormat` `text` or `json` makes the start log to standard error in that format, from `LogLevel` up, Info by default.

`Config.AccessLog` adds a line `request` for every request but those to the health and metrics addresses, at level Info, with these fields: `method`, `path` without the query (which may hold tokens), `status`, `bytes` as sent (after compression), `duration_ms`, `class` (as in the metrics) and `request_id`. Like the metrics, it includes the answers the router gives itself, such as redirects to a clean path. With `LogFormat`, a `LogLevel` above Info leaves these lines out.

Every request has an ID. A proxy may send one in `X-Request-ID`: it is kept when it has at most 64 letters, digits, `-`, `_`, `.` or `:`, and replaced by a new one otherwise. nginx sends its own with `proxy_set_header X-Request-ID $request_id;`. Every response names the ID in `X-Request-ID`, also before the app has started, so a visitor's report can be found in the log. With `LogFormat`, every line logged with a request's context carries it as `request_id`. [Configuration](configuration.md#logs-metrics-and-development) shows how a logger of the site's own gets it too.

A panic in a handler is logged as `panic recovered`, at level Error, with the fields `panic`, `method`, `path`, `request_id` and `stack`. The visitor gets a plain 500, or a broken connection when the answer had already started, and the site keeps serving. The request still shows up in the access log, the metrics and `Telemetry.Middleware`, with its 500 or the status already sent. Before the app has started, a panic is logged the same way, with its `request_id`.

## Development mode

`Config.Development` shows a failing template in detail, on the site and in the admin: the template and the line, with status 500. It also makes `{{debug .}}` show the value it gets. It is for your own machine only, because it shows anyone the inside of the site. The starter and the demo turn it on, with `ReloadTemplates`, only when `WEBTHINGS_DEVELOPMENT` is `true`, as `task dev` sets it, and then log `development mode (WEBTHINGS_DEVELOPMENT): …` at start.

Without it, a failing template, or a module's handler or visitor action that fails, is logged as `render page` with its details, such as the path and the module, and the visitor gets the theme's `500.gohtml` with status 500, without the details: in the page's language, with the theme's partials, its asset versions and integrity, and the page's `View` for `@model`, also after a visitor action that fails. A theme without one gives go-partial's plain error page with status 500: the heading "Internal Server Error" and the message "Something went wrong. Please try again.", both translated into the page's language with the site's translations, and that language in `lang`. A theme's `500.gohtml` gets the translated heading as `.Title`; see [Templates](templates.md#500gohtml). The plain page never names the failed template or the error, also when the theme's `500.gohtml` or the admin's error page fails itself. See [Templates](templates.md#when-something-goes-wrong). The admin answers with an error page of its own, in the admin's language and with that language in `lang`, and a request for part of an admin page, such as a dashboard list, with the error message "The request failed. Please try again.", both with status 500. The admin's handlers answer other unexpected errors with the same sentence: as an error message on the page for the admin's HTMX requests, as plain text for any other request. `{{debug .}}` then shows nothing, so a template may keep it. `ReloadTemplates` and `Insecure` are for development only as well. `Development` and `ReloadTemplates` turn the [output cache](#the-output-cache) off, unless `Config.OutputCacheTTL` is set.

The start checks every template of every theme. In development it fails with every problem it finds. Without development each problem is a warning, `template problem`, with the file, the line and a hint, and the start fails only for one every page of the active theme would fail with: its layout, regions or partials do not parse or call a function the site does not have, such as one of a module removed from `app.Use`. A template call that would show nothing, such as an instance key, an asset file or a theme setting the site does not have, is logged once as `template mistake`, and the page renders without it. See [Checking the templates at start](templates.md#checking-the-templates-at-start).

## Several servers on one database

Several processes can serve one site from one PostgreSQL or MySQL database:

- Changes made on one server show up on the others within `CacheTTL`, 30 seconds by default. A shorter time costs more queries; a negative one turns the cache off.
- Changes to media, such as an alt text or a focal point, can take up to two minutes. Each server keeps what it read of a media file for a minute, and Markdown it rendered with media for a minute as well. `CacheTTL` does not change these times.
- Each server keeps its own `sitemap.xml` for 10 seconds, so a change made on another server can take that much longer to show in it.
- Each server has its own [output cache](#the-output-cache). A page it keeps shows a change made on another server only once it is older than the cache's time, 1 minute by default, so add that time to the ones above.
- Each server dates the feeds itself (`Last-Modified`; see [Caching and compression](#caching-and-compression)), so a reader that asks with `If-Modified-Since` alone gets the whole feed once from every server. While a server has not seen a change made on another one yet, it may give the older feed a later time than the newer feed has there, and such a reader then keeps the older feed until the feed changes again. Readers that send the `ETag` are not affected.
- Uploads must be in storage that every server reaches, such as S3. With the default storage, each server has its own `media/` folder.
- Sign-in sessions are kept in the database, so editors stay signed in on every server and after a restart. So are the sign-ins that wait for a code of [two-step sign-in](#two-step-sign-in) and the last code each account used, so the code may reach another server than the password did, and a code works once on all of them together.
- The admin refuses form posts from other websites by the headers browsers send, without tokens. Any server can take any admin request, so no sticky sessions are needed, and open forms keep working after a restart.
- Every server may run the scheduler: each planned change is made by one of them.
- Every server runs the hourly check of the trash (see [Emptying the trash](#emptying-the-trash)): each item is deleted by one of them.
- Throttles on visitors' forms and on the site search count per server, and so does the rate limit of the [content API](#the-content-api).
- Who else is on an admin page is kept in each server's memory. Editors see each other only when their browsers reach the same server, so with several servers the header's chips can miss people.

## Scheduled publishing

Editors can plan a page, post or event to be published or unpublished later. The app checks every `ScheduleInterval`, 30 seconds by default, and publishes with the same effects as the publish button. A negative interval stops the background scheduler; then call `app.RunSchedules(ctx)` from your own code.

## Emptying the trash

**Site settings › Trash** can have items deleted for good once they have been in the trash for a number of days; see [The trash](editing.md#the-trash). The app checks when it starts and then every hour, together with the removal of expired sessions, and deletes those items in every section the way **Delete selected for good** does. For every section it deleted items in, it logs `deleted expired items from the trash` at level Info, with `section` and `count`. A check that fails logs `delete expired items from the trash` at level Error, with `error`; a section that fails does not stop the others. While the setting is empty or 0, the default, the check deletes nothing. `app.PurgeTrash(ctx)` runs the check from your own code and returns how many items it deleted.

## Backups

Administrators download a backup under **Backup**, in the **System** group of the admin navigation, with **Download backup**. The zip holds the uploaded files below `media/`, read from `Config.Storage` when it is set, and `manifest.json`; on SQLite it holds the database too, as `site.sqlite`. On PostgreSQL and MySQL it leaves the database out: back that up with `pg_dump` or `mysqldump` at the same time. [Backups](databases.md#backups) describes what the zip holds and the manual way.

The zip is sent while it is written, so a large one is not held in memory, and the server's write timeout of 60 seconds does not cut it short. On SQLite the site first copies the database to the server's temporary folder (`TMPDIR`), which needs room for that copy, and removes the copy afterwards. Each download is logged at level Info as `backup downloaded`, with `user`, `email`, `database` and `files`; a download that fails while the zip is on its way is logged at level Error as `write backup`, and the zip it leaves does not open.

The download is made by hand. For backups on a schedule, run the commands of [By hand](databases.md#by-hand) from cron or a systemd timer.

To restore a downloaded zip on SQLite, stop the site, put `site.sqlite` and `media/` from the zip in the state folder in place of the old ones, and start it. With the unit of [Running with systemd](#running-with-systemd):

```sh
sudo systemctl stop website
cd /var/lib/website
sudo mv site.sqlite site.sqlite.before-restore
sudo mv media media.before-restore
sudo -u website unzip /tmp/my-site-backup-20261027-1432.zip site.sqlite 'media/*'
sudo systemctl start website
```

Delete `site.sqlite-wal` and `site.sqlite-shm` in the state folder too, if they exist, and keep the old copies until the site runs as it should. On PostgreSQL and MySQL, restore the database from its own backup and put the files of `media/` back the same way. With `Config.Storage` set, put the files in that storage instead, under the same names. See [Restoring](databases.md#restoring).

## Mail and storage

`mail.SMTP` is built in. It uses port 587 unless `Port` is set, and STARTTLS when the server offers it; port 465 with implicit TLS is not supported. Sending a message stops when its context ends or after `Timeout`, 30 seconds by default. See [E-mail](mail.md).

The **E-mail** step of the setup guide (see [Set up the site](getting-started.md#set-up-the-site)) shows administrators how the site sends. For `mail.SMTP` it says "The site sends e-mail with SMTP smtp.example.com:587, from …", with the server, its port and the `From` address, never the username or password. Without a sender it says "No sender configured: e-mail is written to the server log, not sent." When `BaseURL` is empty, it warns that links in e-mail then use the address each visitor used, and that outside local development the admin offers no password reset. **Send a test e-mail to …** sends a short message to the signed-in administrator and waits for it, at most 30 seconds. The message afterwards says whether it went, with the sender's error when it did not; a test that went finishes the step. Each administrator may send one test e-mail every 30 seconds; another is refused with "Wait half a minute before you send another test e-mail."

A sender of your own describes itself by implementing `mail.Describer`, a method `Describe() string`, as `mail.SMTP`, `mail.Log` and `mail.Memory` do. Leave secrets, such as passwords and API keys, out of the description. A sender without it shows as "The site sends e-mail with the sender its code sets." See [E-mail](mail.md#other-providers).

Uploads are kept in `StateDir/media` and served at `/media/`. `Config.Storage` puts them elsewhere; resized copies of images are still served at `/media/`, whatever the storage. See [Media and images](media.md).

Integrations with outside services live in the separate repository github.com/donseba/go-webthings-contrib:

- `mailjet`: Mailjet as the mail sender, and a list sync for the newsletter module.
- `s3`: media storage on S3-compatible stores, such as AWS S3, DigitalOcean Spaces and MinIO.

## Shutting down

The starter cancels its context on `SIGINT` and `SIGTERM`. `ListenAndServe` (or `Serve`) then stops accepting connections, gives running requests up to 5 seconds, and closes the app: it stops the scheduler and the cleanup of expired sessions and of the trash, lets modules finish for up to 10 seconds, and closes the storage and the database. Modules write what they keep in memory, such as page views and redirect hits; a newsletter mailing stops after the current message and continues at the next start. Closing the app also ends the admin's open event streams.

Give the process about 15 seconds after `SIGTERM` before it is killed.

## Sitemap and robots.txt

`/sitemap.xml` lists every published page in every language, with links to the other languages, and the pages that modules add below their page, such as news posts. A module whose list fails is left out and logged. The sitemap holds at most 50,000 addresses, the most a sitemap file may have; beyond that, the last ones are left out and a warning is logged. Each server builds it again at once when what visitors see changes on that server, and otherwise at most once every 10 seconds, and keeps it in its [output cache](#the-output-cache) like a page.

`/robots.txt` allows every crawler everywhere and points to the sitemap. It does not name the admin, so a [path of your own](configuration.md#admin-path) is not published there. Admin pages answer with `X-Robots-Tag: noindex, nofollow` instead. Both use `BaseURL`, or the request's address without it. See [Search and SEO](search-and-seo.md).

## Running with systemd

```text
[Unit]
Description=example.com
After=network-online.target
Wants=network-online.target

[Service]
User=website
WorkingDirectory=/srv/website
ExecStart=/srv/website/bin/website
StateDirectory=website
Environment=WEBTHINGS_STATE_DIR=/var/lib/website
EnvironmentFile=/etc/website.env
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

`StateDirectory` creates `/var/lib/website` for the `website` user. `/etc/website.env` holds the secrets, such as `WEBTHINGS_DB_DSN` and the mail password, and should be readable by root only. systemd stops the service with `SIGTERM`.

```sh
sudo systemctl enable --now website
journalctl -u website -f
```

The log goes to standard error: the address it serves on, the setup link while no account exists (again at level WARN when the sign-in page is opened, at most once a minute), templates that are still missing, template problems and mistakes, a warning when `BaseURL` is empty, and errors. [Logs](#logs) describes the format and the access log.

## In a container

A container image needs the same pieces:

- a build stage with Go that builds as `task build` does, with `CGO_ENABLED=0` and `-tags nodynamic`, so the runtime image can be Alpine or `scratch` as well as Debian slim;
- until go-webthings has a release, its checkout in the build context, because the starter's `go.mod` points to `../go-webthings`;
- the binary, `templates/` and `translations/` in the working directory;
- a volume for the state folder;
- CA certificates, and time zone data unless the binary embeds it;
- `WEBTHINGS_LISTEN=:8092`, so the site is reachable from outside the container;
- a stop timeout of about 15 seconds.
