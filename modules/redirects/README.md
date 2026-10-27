# Redirects

Sends visitors from old addresses to new ones, counts the hits of every redirect and lists the paths visitors asked for that do not exist.

It is a site feature: one instance serves the whole site, for example after moving an existing website to WebThings. It has no templates or template functions.

## Add it to the site

```go
app.Use(redirects.Module())
```

The instance gets the key `redirects` and the title Redirects, translated in the admin; its section is at `/admin/redirects` (`/admin` is the default [admin path](../../docs/configuration.md#admin-path)). `Named` chooses another key and title:

```go
app.Use(redirects.Module(redirects.Named("old", "Old addresses")))
```

## In the admin

The list shows every redirect with its old path, new address, status, hits and last hit, in the site's timezone. **Redirects again** marks a redirect whose new address redirects once more.

- **New redirect** asks for the old path, the new address and the kind of redirect: 301 Moved permanently (the default), 302 Found (temporary), 307 Temporary redirect or 308 Permanent redirect. The new address is a path on this site, such as `/en/news`, or a full URL. A redirect that would lead back to its own path, directly or through other redirects, is refused. The edit screen warns when the new address redirects again, and when other redirects now take two steps to arrive.
- **Move to trash**, on a redirect's edit screen, moves it to the [trash](../../docs/editing.md#the-trash): it stops redirecting at once and its old path is free for a new redirect. Restoring it is refused when another redirect uses that path by then, or when it would lead back to its own path.
- **Test a path** shows where a path leads, step by step, or that it belongs to the website itself.
- **Missed paths** lists the paths that ended in "not found", most requested first. **Add redirect** opens a new redirect with the old path filled in; a path leaves the list once it has a redirect. **Clear list** empties it.
- **Import** takes CSV lines pasted into a text box: the old path, the new address and, optionally, the status (301 when empty).

```text
from,to,status
/about-us.html,/en/about,301
/blog/2019/summer-party,/en/news/summer-party
https://old.example.com/contact.php,/en/contact,302
```

A first line whose first field is `from` is skipped. The fields are separated by commas, semicolons or tabs, whichever the first line uses most, and a byte order mark at the start, as spreadsheet programs write, is ignored. A line whose old path already has a redirect replaces it. The import saves every line or, when one is wrong, none: the screen then lists the lines to correct. One import takes at most 5000 lines, and the box holds at most 262,144 characters: split a long list of long addresses over several imports.

Editors' rights are set in the groups' permission matrix, like those of any module instance.

## Redirects from the editors

With this module, the admin also offers redirects where a page or an item, such as a post, leaves its address; see [Links to a page](../../docs/editing.md#links-to-a-page):

- **Unpublish and redirect**, in the editor of a page that published content links to, takes the page offline and redirects its addresses, those of the pages below it and the linked addresses below those to a page the editor chooses. In the editor of a post, event, album, person, directory entry or category of Publishing or Directory that published content links to, it does the same with the item's address.
- **Redirect the old addresses to the new ones** is a box with **Publish this revision** when the revision gives a page a new **Web address** or another parent page, or a post or an event a new **Web address**. It is ticked by default, and redirects the old addresses of the page and of the pages below it, or of the item, to their new ones.
- Saving a published album, person, directory entry or category of Publishing or Directory with a new **Web address** redirects its old address to the new one.
- **Create redirect**, in the report under **Pages › Broken links**, redirects an address that links lead to and where the site has no page.

These are 301 redirects, listed here with the others. A redirect from the same old path is replaced. A redirect from the new address, which would hide the page there, such as one left from moving the page away earlier, is moved to the trash, so a page can get its old web address back. Making them needs the create right in this module's row of the permission matrix; without it, the report says "You need the right to create redirects."

## How it works

The module wraps the public site. A GET or HEAD request whose path has a redirect is redirected before the site handles it, and before the site's output cache, so a redirect also wins over a page at the same address, at once, also while that page is in the cache. The query string is kept when the new address has none: `/old?utm_source=mail` goes to `/en/new?utm_source=mail`.

Paths are compared after normalising: the host, query and fragment are dropped, a leading slash is added, and trailing slashes and dot segments are removed. Editors can therefore paste full old URLs: `https://old.example.com/a/?b=1` becomes `/a`. Case counts, so `/About` and `/about` are different paths.

Never redirected are the home page `/` and the paths WebThings serves itself: the admin, visitor actions (`/_/`), `/media/`, `/assets/` and `/theme/`, with everything below them, `/sitemap.xml`, `/robots.txt`, the endpoints for operations, `/healthz`, `/readyz` and `/metrics`, and, while `Config.API` turns it on, the content API (`/api/v1`). A redirect from one of these paths is refused.

Missed paths are GET requests that the site answered with 404. A 404 sent with `X-Robots-Tag: noindex` or `none`, such as a mistyped registration code, is not counted. The module keeps the 1000 most requested; the admin shows the first 100.

Hits and missed paths are counted in memory and written to the database in batches: at most every 30 seconds, whenever the admin lists them, and when the app closes, so a redirect never waits for the database. The redirects themselves are kept in memory too: a change applies at once in the process that made it, and other processes on the same database pick it up within a minute.

## From Go

`redirects.Of(app, "redirects")` returns the instance once the app has started, for example to add the redirects of a migration in code:

```go
moved := redirects.Of(app, "redirects")

_, err := moved.Save(ctx, redirects.Redirect{
    From:   "/about-us.html",
    To:     "/en/about",
    Status: 301,
})
```

`Import` reads the same CSV as the admin from any `io.Reader`, such as a file, and refuses one over 1 MiB or 5000 lines whole. `Resolve` and `Trace` tell where a path leads, by the saved redirects: a path that is never redirected has none, as `Save` and `Import` refuse it. `Missed` returns the missed paths. `Delete`, `Undelete` and `Purge` move a redirect to the trash, bring it back or remove it for good.

`Redirect(ctx, address, target)` saves a 301 from `address` to `target`, as the editors do: it replaces the redirect from the same path and moves a redirect from `target` to the trash. It refuses what `Save` refuses, such as a reserved path or a redirect that leads back to its own path. `Redirected(ctx, address)` returns where the redirect of a path sends visitors, and false when it has none. With `moved` as above:

```go
if err := moved.Redirect(ctx, "/en/about", "/en/about-us"); err != nil {
    return err
}

// "/en/about-us", true
target, ok := moved.Redirected(ctx, "/en/about")
```

Together they are the module's `links.Redirector`, which it gives the app as `Parts.Redirects`; see [Links to pages](../../docs/module-feeds-and-links.md#links-to-pages).

## Related modules

- [Stats](../stats/README.md) counts the pages visitors do find.
