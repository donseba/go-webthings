package siteadmin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/site"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-partial/connector"
)

// The Backup screen gives administrators the site as one zip file, as the
// guides tell to back it up by hand: the database and the uploaded files,
// with a manifest. Only administrators open it (admin.Section.AdminOnly),
// whatever their groups allow: the database holds the password hashes of
// the accounts and the personal data of visitors.
//
// On SQLite the database is copied with VACUUM INTO, which makes a
// consistent copy while the site runs, to a temporary file, before any of
// the response is written: a failure then still answers with an error
// page. PostgreSQL and MySQL have no file to copy; the zip holds the files
// and the manifest, and the screen says to back the database up with its
// own tools. The files are read through media.Storage, so a storage such
// as S3 is backed up too. The resized copies of images are left out: the
// site makes them again on their first request.

// The names in the zip. A restore puts site.sqlite in the state folder and
// the files below media/ in the media storage, under their own names.
const (
	backupDatabase = "site.sqlite"
	backupMedia    = "media/"
	backupManifest = "manifest.json"
)

// backupDocs is the guide to backups, which explains the tools of
// PostgreSQL and MySQL.
const backupDocs = "https://docs.gowebthings.com/go-webthings/databases#backups"

// BackupView is the data of the Backup screen.
type BackupView struct {
	// Database is set when the download holds the database: on SQLite.
	Database bool
	// Server names the database the download leaves out, such as
	// PostgreSQL; Dump is the tool that backs it up, such as pg_dump, and
	// Load the one that restores it, such as psql. They are empty on
	// SQLite.
	Server string
	Dump   string
	Load   string
	// DocsURL is the guide to backups.
	DocsURL string
	// Download is the button. It posts, so prefetching a link never makes
	// a backup, and outside HTMX, so the browser saves the file.
	Download form.RenderModel
}

// BackupManifest is manifest.json in a backup: where the backup comes from
// and what it holds.
type BackupManifest struct {
	// Site is the site's title in its default language.
	Site string `json:"site"`
	// CreatedAt is when the backup was made, in UTC.
	CreatedAt time.Time `json:"created_at"`
	// Dialect is the site's database: sqlite, postgres or mysql.
	Dialect db.Dialect `json:"dialect"`
	// DatabaseIncluded is set when the zip holds the database as
	// site.sqlite, which it does on SQLite.
	DatabaseIncluded bool `json:"database_included"`
	// SchemaVersions are the schema versions of core and every module type
	// by owner, as db.SchemaVersions reads them.
	SchemaVersions map[string]int64 `json:"schema_versions"`
	// MediaFiles is the number of files below media/.
	MediaFiles int `json:"media_files"`
}

// serverTools are the tools that back up and restore the databases a
// download leaves out.
type serverTools struct {
	server string
	dump   string
	load   string
}

var backupTools = map[db.Dialect]serverTools{
	db.Postgres: {server: "PostgreSQL", dump: "pg_dump", load: "psql"},
	db.MySQL:    {server: "MySQL", dump: "mysqldump", load: "mysql"},
}

// NewBackup returns the Backup section of the admin, where administrators
// download the site as a zip file: options.DB's database on SQLite, the
// uploaded files of options.Media and a manifest.
func NewBackup(settings *site.Service, options Options) admin.Section {
	return admin.Section{
		ID:        "backup",
		Title:     "Backup",
		Icon:      "download",
		AdminOnly: true,
		Files:     files,
		Routes: func(routes *admin.Routes) {
			b := backup{
				admin:    routes.Admin(),
				settings: settings,
				db:       options.DB,
				media:    options.Media,
			}

			routes.Get("", admin.View, b.show).As("index")
			routes.Post("/download", admin.Export, b.download).As("download")
		},
	}
}

type backup struct {
	admin    *admin.Admin
	settings *site.Service
	db       *db.DB
	media    *media.Service
}

func (b backup) show(w http.ResponseWriter, r *http.Request) {
	view := BackupView{
		Database: b.db.Dialect() == db.SQLite,
		DocsURL:  backupDocs,
		Download: b.admin.Form(r, struct{}{}, b.admin.URL("admin.backup.download"), "Download backup"),
	}

	// HTMX would take the zip for a page to show.
	view.Download.Info.Attributes = connector.MergeAttrs(
		b.admin.Connector(),
		view.Download.Info.Attributes,
		connector.Attr{Boost: connector.BoostOff},
		connector.AttrBoost,
	)

	if tools, ok := backupTools[b.db.Dialect()]; ok {
		view.Server = tools.server
		view.Dump = tools.dump
		view.Load = tools.load
	}

	b.admin.Render(w, r, admin.Page{
		Title:    "Backup",
		Template: "templates/backup.gohtml",
		Files:    files,
		Data:     view,
		Status:   http.StatusOK,
	})
}

// download answers with the zip, written as it is sent. What can fail
// before is done first: the copy of the database, its schema versions and
// the list of files. A failure while the zip is sent can no longer change
// the response: it is logged and the download stops, which leaves a zip
// that does not open.
func (b backup) download(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	current, err := b.settings.Settings(ctx)
	if err != nil {
		b.admin.Fail(w, r, err)

		return
	}

	// The file name and the files in the zip carry the time the backup is
	// made, in the site's timezone.
	now := time.Now().In(current.Location()).Truncate(time.Second)

	database, err := b.copyDatabase(ctx)
	if err != nil {
		b.admin.Fail(w, r, err)

		return
	}

	if database != nil {
		defer discard(ctx, database)
	}

	versions, err := b.db.SchemaVersions(ctx)
	if err != nil {
		b.admin.Fail(w, r, err)

		return
	}

	keys, err := b.media.Originals(ctx)
	if err != nil {
		b.admin.Fail(w, r, err)

		return
	}

	title := current.Text(current.DefaultLocale).Title

	manifest := BackupManifest{
		Site:             title,
		CreatedAt:        now.UTC(),
		Dialect:          b.db.Dialect(),
		DatabaseIncluded: database != nil,
		SchemaVersions:   versions,
	}

	disposition := mime.FormatMediaType("attachment", map[string]string{
		"filename": backupName(title, now),
	})

	header := w.Header()
	header.Set("Content-Type", "application/zip")
	header.Set("Content-Disposition", disposition)
	header.Set("Cache-Control", "no-store")

	// A large backup takes longer than the server's write timeout allows a
	// response.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	archive := backupArchive{zip: zip.NewWriter(w), modified: now}
	user, _ := auth.CurrentUser(ctx)

	written, err := archive.write(ctx, database, b.media.Storage(), keys, manifest)
	if err != nil {
		slog.ErrorContext(ctx, "write backup", "user", user.ID, "email", user.Email, "error", err)

		// The zip has begun: break the response off, so the browser reports
		// a failed download instead of saving a zip that does not open.
		panic(http.ErrAbortHandler)
	}

	slog.InfoContext(ctx, "backup downloaded",
		"user", user.ID,
		"email", user.Email,
		"database", database != nil,
		"files", written,
	)
}

// backupName is the file name of a backup of the site with title made at
// at: the title's slug, "-backup-" and the date and time, such as
// my-site-backup-20261027-1432.zip.
func backupName(title string, at time.Time) string {
	name := content.Slug(title)
	if name == "" {
		name = "site"
	}

	return name + "-backup-" + at.Format("20060102-1504") + ".zip"
}

// copyDatabase copies a SQLite database to a temporary file with VACUUM
// INTO, a consistent copy while the site runs, and returns the copy open
// for reading; discard removes it. The other databases have no file to
// copy: it returns nil for them.
func (b backup) copyDatabase(ctx context.Context) (*os.File, error) {
	if b.db.Dialect() != db.SQLite {
		return nil, nil
	}

	// VACUUM INTO writes to a new or an empty file only.
	file, err := os.CreateTemp("", "webthings-backup-*.sqlite")
	if err != nil {
		return nil, fmt.Errorf("copy database: %w", err)
	}

	path := file.Name()

	if err := file.Close(); err != nil {
		return nil, errors.Join(err, os.Remove(path))
	}

	if _, err := b.db.Exec(ctx, `VACUUM INTO ?`, path); err != nil {
		return nil, errors.Join(fmt.Errorf("copy database: %w", err), os.Remove(path))
	}

	copied, err := os.Open(path)
	if err != nil {
		return nil, errors.Join(err, os.Remove(path))
	}

	return copied, nil
}

// discard closes and removes the copy of the database.
func discard(ctx context.Context, copied *os.File) {
	_ = copied.Close()

	if err := os.Remove(copied.Name()); err != nil {
		slog.ErrorContext(ctx, "remove the copy of the database", "file", copied.Name(), "error", err)
	}
}

// backupArchive writes the zip of a backup, every file with the time the
// backup was made.
type backupArchive struct {
	zip      *zip.Writer
	modified time.Time
}

// write writes the backup: the copy of the database, when there is one,
// the files of keys from storage below media/ and then the manifest, with
// the number of files written. A file that is gone from the storage, such
// as one deleted for good meanwhile, is left out. It returns that number.
func (a backupArchive) write(
	ctx context.Context,
	database *os.File,
	storage media.Storage,
	keys []string,
	manifest BackupManifest,
) (int, error) {
	if database != nil {
		if err := a.add(backupDatabase, database, zip.Deflate); err != nil {
			return 0, err
		}
	}

	for _, key := range keys {
		added, err := a.addMedia(ctx, storage, key)
		if err != nil {
			return manifest.MediaFiles, err
		}

		if added {
			manifest.MediaFiles++
		}
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return manifest.MediaFiles, err
	}

	if err := a.add(backupManifest, bytes.NewReader(data), zip.Deflate); err != nil {
		return manifest.MediaFiles, err
	}

	return manifest.MediaFiles, a.zip.Close()
}

// addMedia adds the file stored under key below media/ and reports whether
// the storage had it.
func (a backupArchive) addMedia(ctx context.Context, storage media.Storage, key string) (bool, error) {
	file, err := storage.Open(ctx, key)
	if errors.Is(err, fs.ErrNotExist) {
		slog.WarnContext(ctx, "the backup leaves out a missing media file", "key", key)

		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("open media file %s: %w", key, err)
	}

	defer func() { _ = file.Close() }()

	if err := a.add(backupMedia+key, file, mediaMethod(key)); err != nil {
		return false, err
	}

	return true, nil
}

// compressedMedia are the extensions of media files that are compressed
// already, such as photos and PDFs.
var compressedMedia = map[string]bool{
	".avif": true, ".gif": true, ".jpeg": true, ".jpg": true, ".mp3": true, ".mp4": true,
	".pdf": true, ".png": true, ".webm": true, ".webp": true, ".woff2": true, ".zip": true,
}

// mediaMethod is how the zip keeps the media file key: as it is when the
// file is compressed already, since compressing it again costs time and
// saves next to nothing, and compressed otherwise.
func mediaMethod(key string) uint16 {
	if compressedMedia[strings.ToLower(path.Ext(key))] {
		return zip.Store
	}

	return zip.Deflate
}

// add adds the file name, which holds what source reads, kept with method
// (zip.Store or zip.Deflate).
func (a backupArchive) add(name string, source io.Reader, method uint16) error {
	entry, err := a.zip.CreateHeader(&zip.FileHeader{
		Name:     name,
		Method:   method,
		Modified: a.modified,
	})
	if err != nil {
		return err
	}

	if _, err := io.Copy(entry, source); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}

	return nil
}
