# Databases

go-webthings keeps everything in one SQL database: SQLite by default, or PostgreSQL or MySQL. It creates and updates its tables itself every time the site starts.

## SQLite

Without any database settings, the site uses SQLite in its state folder:

```text
var/site.sqlite
```

The file is created on first start. The default connection string is `file:var/site.sqlite`, with `var` replaced by `Config.StateDir`. go-webthings adds the settings it relies on to every SQLite `DSN`, including one you set yourself:

```text
_foreign_keys=1&_busy_timeout=5000&_txlock=immediate&_time_format=sqlite&_timezone=UTC
```

A `DSN` that already sets one of them keeps its own value, so older connection strings keep working. Keep foreign keys on; the tables rely on them.

SQLite allows one writer at a time, so go-webthings keeps a single connection to it per process. That suits a site on one server. For several servers, use PostgreSQL or MySQL.

The SQLite driver is pure Go ([modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)), so building the site needs no cgo or C compiler; see [Deployment](deployment.md#build-the-binary). `Driver` is `sqlite`; `sqlite3` is accepted too.

## PostgreSQL

Set `Driver` to `pgx` and `DSN` to a connection URL or a key/value string:

```go
webthings.Config{
    Driver: "pgx",
    DSN:    "postgres://website:secret@127.0.0.1:5432/website?sslmode=disable",
}
```

With the starter, set the environment instead:

```sh
WEBTHINGS_DB_DRIVER=pgx \
WEBTHINGS_DB_DSN='postgres://website:secret@127.0.0.1:5432/website?sslmode=disable' \
./bin/website
```

Create the database first and let the site's user own it, so it may create tables:

```sh
createuser --pwprompt website
createdb --owner website website
```

IDs are stored in native `uuid` columns.

## MySQL

Set `Driver` to `mysql` and `DSN` to a [go-sql-driver](https://github.com/go-sql-driver/mysql) DSN:

```go
webthings.Config{
    Driver: "mysql",
    DSN:    "website:secret@tcp(127.0.0.1:3306)/website",
}
```

go-webthings adds `parseTime=true` and `clientFoundRows=true` to the DSN when they are missing; it needs both. Create the database with the `utf8mb4` character set, the default since MySQL 8.0:

```text
CREATE DATABASE website CHARACTER SET utf8mb4;
```

MySQL commits table changes immediately, even inside a transaction. If a schema file fails halfway, part of it may already be applied; fix the cause and check the tables before starting again.

go-webthings' own tests run against PostgreSQL 18 and MySQL 8.4.

## Tables and schema versions

Every core part and every module type keeps its tables in schema files. At start, go-webthings applies them in order: `auth`, `media`, `site`, `pages`, `admin` and `frontend`, then the modules in the order of `app.Use`. All tables start with `wt_`. The `frontend` part holds `wt_kept_form`, where visitors' posts with mistakes wait for their page; see [Visitor forms](visitor-forms.md#a-post-with-mistakes).

The table `wt_schema` records the version each owner has reached:

| owner | version |
| --- | --- |
| `auth` | `20261004000000` |
| `pages` | `20261004000000` |
| `module:publishing` | `20261004000000` |

Instances of one module type share its tables, so `news` and `blog` both live in the publishing tables, apart by their key. A module type's schema is applied when at least one of its instances is in `app.Use`. Removing an instance from `main.go` keeps its tables and its content.

### Schema files

Schema files sit in one folder and are named `<datetime>[_description].<flavour>.sql`:

```text
schema/20261004000000_init.sqlite.sql
schema/20261004000000_init.postgres.sql
schema/20261004000000_init.mysql.sql
schema/20261120093000_add_avatar.sqlite.sql
schema/20261120093000_add_avatar.postgres.sql
schema/20261120093000_add_avatar.mysql.sql
```

- The datetime, `YYYYMMDDHHMMSS`, is the version.
- The flavours are `sqlite`, `postgres` and `mysql`. Every version must exist in every flavour, even on a site that only uses one database; otherwise the start fails with "schema versions … differ …; add the missing files".
- Only the files of the site's own database run, oldest first, and only those newer than the version in `wt_schema`.
- Each file runs in a transaction and moves the version forward.
- A statement ends with a semicolon at the end of a line. Lines starting with `--` are skipped.

To change a table, add new files with a later datetime. Never change a file that a database has already applied: it will not run again.

A module passes its folder as `Module.Schema`, usually embedded in the binary:

```go
//go:embed schema/*.sql
var schemaFiles embed.FS

func schema() fs.FS {
    files, _ := fs.Sub(schemaFiles, "schema")

    return files
}
```

See [Writing a module](writing-a-module.md).

Until go-webthings has its first release, its own schemas only have `_init` files, and those may still change. Nothing updates an existing database when an `_init` file changes: `wt_schema` already records its version, so it does not run again, and queries then fail on missing columns, such as `created_by`, or `review_state` in the `_revision` tables, which the review workflow added. After updating go-webthings, recreate a development database: stop the site, delete `var/site.sqlite` and start again. On PostgreSQL or MySQL, drop the database and create it again.

### Audit columns

Tables of things people change record who changed them and when:

| Columns | |
| --- | --- |
| `created_at`, `created_by` | When the row was made, and by whom. |
| `updated_at`, `updated_by` | Its last change. |
| `deleted_at`, `deleted_by` | When it went to the trash, and by whom; `NULL` while it is live. |

The `*_by` columns hold a user ID, or `NULL` for visitors and the system, such as the scheduler. They have no foreign key, so the record stays when an account is deleted. Content, such as pages, media and the records of modules, has all six columns; tables whose rows cannot go to the trash, such as accounts, leave out `deleted_at` and `deleted_by`. Settings tables only have `updated_at` and `updated_by`. Logs, counters, join tables and the `_translation` tables have none. A `_revision` table has only `created_at`: a revision records its own author, and who submitted and reviewed it.

The user comes from the context. The admin puts the signed-in user there for every request (`auth.WithUser` calls `db.WithActor`), and `db.Actor(ctx)` returns it as a `db.Ref`, which is stored as `NULL` when empty. The content stores fill the columns themselves. Code that writes tables of its own takes the values from `db.Stamp(ctx)`: the current time in UTC, cut to the microsecond that every database keeps, and `db.Actor(ctx)`. Take one stamp per change and write it to every row the change touches. `db.Affected` checks a statement meant for a row that must exist: it returns `db.ErrNotFound` when no row matched, such as a member in the trash.

```go
at, by := db.Stamp(ctx)

err := db.Affected(database.Exec(ctx, `
    UPDATE site_member
    SET email=?, updated_at=?, updated_by=?
    WHERE id=? AND deleted_at IS NULL`,
    email, at, by, id,
))
```

`db.SoftTable` writes the statements of a table whose rows go to the trash, limited to a part of the table with `Scope`, such as one instance. `Delete` fills `deleted_at` and `deleted_by`, `Undelete` empties them and fills `updated_at` and `updated_by`, `Purge` removes a row in the trash for good, and `Trashed` reads one page of the trash, the most recently deleted first, with how many rows it holds. Each returns `db.ErrNotFound` for a row that is not where it should be, and joins a transaction in the context:

```go
members := db.SoftTable{DB: database, Table: "site_member", Scope: "instance=?", Args: []any{key}}

err := members.Delete(ctx, id)
```

Code outside the admin, such as an import, can set a user with `db.WithActor(ctx, userID)`; without one, the columns stay `NULL`. `db.Ref` is the type for any ID column that may be `NULL`, in structs and as an argument: it also reads `NULL` as empty.

A unique index must leave deleted rows out, or a row in the trash keeps others from using its value. SQLite and PostgreSQL use a partial index. MySQL has no partial indexes, so it indexes a generated column that is `NULL` for deleted rows; a unique index allows many `NULL`s:

```text
-- SQLite and PostgreSQL
CREATE UNIQUE INDEX site_member_email ON site_member (instance, email) WHERE deleted_at IS NULL;

-- MySQL, inside CREATE TABLE site_member (...)
    live_email VARCHAR(254) AS (CASE WHEN deleted_at IS NULL THEN email END) STORED,
    UNIQUE INDEX site_member_email (instance, live_email)
```

Restoring a row whose value a live row took meanwhile then fails with a unique error, which `db.IsUnique` recognises. The subscribers of [`../modules/newsletter/schema`](../modules/newsletter/schema) and the slugs of [`../modules/people/schema`](../modules/people/schema) work this way. `SoftTable.Undelete(ctx, id, checks...)` runs its checks first, in one transaction with the change, so a store can refuse with an error of its own instead, such as for a redirect whose old path another redirect uses now. See [The admin of a module](module-admin.md#deleting-and-the-trash) for the trash.

### JSON columns

A value such as a list of form fields, `type Fields []Field`, can live in one text column as JSON. Its type stores and reads itself with `db.JSONValue` and `db.ScanJSON`:

```go
// Value stores the fields as JSON.
func (f Fields) Value() (driver.Value, error) {
    return db.JSONValue(f)
}

// Scan reads fields stored by Value.
func (f *Fields) Scan(src any) error {
    return db.ScanJSON(src, f)
}
```

Use a `TEXT` column, or `MEDIUMTEXT` on MySQL for long values. `NULL` and empty text read as the zero value.

## Backups

Back up the database and the uploads together.

- **SQLite**: the state folder holds both. Copy the database with the `sqlite3` tool, which is safe while the site runs, and the `media/` folder:

  ```sh
  sqlite3 var/site.sqlite ".backup 'backup/site.sqlite'"
  cp -R var/media backup/
  ```

  Or stop the site and copy the whole state folder.
- **PostgreSQL**: `pg_dump website > backup/website.sql`, plus the `media/` folder.
- **MySQL**: `mysqldump --single-transaction website > backup/website.sql`, plus the `media/` folder.

With `Config.Storage` set, uploads live in that storage instead of `media/`; back them up there. Keys such as the one that signs image URLs are stored in the database, so a restored site keeps its links working.

To restore, stop the site, put the database and the uploads back, and start it.

## Switching databases

go-webthings does not copy content from one database to another. A site started on a new database is a new site: it gets a new home page, settings from the configuration and an administrator from `AdminEmail` and `AdminPassword`, or from the setup screen.

Choose the production database before editors start working. To move an existing site, copy the data yourself.

## Testing

Two packages help in tests; use them in `_test.go` files only.

[db/dbtest](../db/dbtest/dbtest.go) runs a test once per database, each with a fresh, empty database. SQLite always runs. Set `WT_TEST_POSTGRES` and `WT_TEST_MYSQL` to a server DSN to run on PostgreSQL and MySQL too; the user must be allowed to create databases, because each test creates one and drops it afterwards.

```go
func TestShop(t *testing.T) {
    dbtest.Each(t, func(t *testing.T, database *db.DB) {
        ctx := context.Background()

        if err := database.ApplySchema(ctx, "module:shop", schema()); err != nil {
            t.Fatal(err)
        }

        // Test against database.
    })
}
```

`dbtest.ForeignKeyError` returns a real foreign key error from SQLite, for testing code that turns such errors into messages.

[sitetest](../sitetest/sitetest.go) starts a complete site on SQLite in a temporary folder, signed in as an administrator, and drives it like a browser. Sent e-mail is kept in `site.Mail`; `site.Mails(n)` waits until `n` messages were sent, for mail sent in the background. `sitetest.HasToast(body, level, text)` tells whether an admin page shows a message, with level `success`, `info`, `warn` or `error`. The admin dashboard loads its panels after the page: `site.Dashboard()` returns it with the panels loaded, and `site.Fragment(path, id)` returns the part of a page with that ID, as HTMX asks for it. `site.Request(req)` sends a request of your own with the browser's cookies, such as one for a stream, and returns the response unread. `site.App.AdminURL(name, args...)` builds an admin address from a route name, wherever `Config.AdminPath` puts the admin. `site.Mount` and `site.MountIn` place a module on a published page, in one language or several, and return the page's editor; `site.DraftPage` gives a page an unpublished title and slug. `sitetest.PNG(t, width, height)` makes an image and `site.Upload(name, data)` adds it to the media library, and `site.PostFiles(formPage, action, values, files...)` sends a form with a file input, such as an import. See [Caching and testing a module](module-caching-and-testing.md#testing).

```go
func TestNews(t *testing.T) {
    site := sitetest.Start(t, publishing.Module("news", "News"))

    site.Expect(site.App.AdminURL("admin.news.index"), http.StatusOK)
    site.Expect("/en/", http.StatusOK)
}
```

`sitetest.StartWith` takes options for other templates and configuration changes, `StartingPoints` to test the templates `App.WriteTemplates` generates, `NoAccount` for a fresh site that offers the setup screen, and `OutputCache` to keep the output cache on, which sites in tests have off. Its address is `site.App.AdminURL("admin.setup", url.Values{"token": {site.App.SetupToken()}})`, and a post to it needs the same token in the `Token` field.
