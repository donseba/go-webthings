package siteadmin_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/site/siteadmin"
	"github.com/donseba/go-webthings/sitetest"
)

// downloadBackup presses the Backup screen's button as s and returns the
// response with its body.
func downloadBackup(t *testing.T, s *sitetest.Site) (*http.Response, []byte) {
	t.Helper()

	request, err := http.NewRequest(http.MethodPost, s.App.AdminURL("admin.backup.download"), nil)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Sec-Fetch-Site", "same-origin")

	response := s.Request(request)

	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	return response, body
}

// backupName is the file name of a backup, with the time it was made.
var backupName = regexp.MustCompile(`^test-site-backup-(\d{8}-\d{4})\.zip$`)

// TestBackup checks the download of the Backup screen on every database: a
// zip, saved under the site's name and the time in the site's timezone,
// with every uploaded file below media/, also those in the trash but not
// the resized copies of images, and a manifest. On SQLite it holds the
// database as site.sqlite, which opens with db.Open and has the site's
// rows. On PostgreSQL and MySQL it holds no database, and the screen says
// to back the database up with its own tools.
func TestBackup(t *testing.T) {
	dbtest.EachDSN(t, func(t *testing.T, driver, dsn string) {
		s := sitetest.StartWith(t, sitetest.Options{Configure: func(config *webthings.Config) {
			config.Driver = driver
			config.DSN = dsn
		}})
		ctx := t.Context()

		// Kathmandu is 5:45 ahead of UTC: the file name shows which zone it
		// uses.
		settings, err := s.App.Site.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}

		settings.Timezone = "Asia/Kathmandu"

		if err := s.App.Site.Save(ctx, settings); err != nil {
			t.Fatal(err)
		}

		photoData := sitetest.PNG(t, 40, 30)
		notesData := []byte("Minutes of the meeting.")
		photo := s.Upload("photo.png", photoData)
		notes := s.Upload("notes.txt", notesData)
		old := s.Upload("old.png", sitetest.PNG(t, 20, 10))

		s.Post("/admin/media/"+old.ID, "/admin/media/"+old.ID+"/delete", url.Values{})

		// The site makes resized copies of new images, which stay out of the
		// backup.
		if err := s.App.Media.WaitPrepared(ctx); err != nil {
			t.Fatal(err)
		}

		var variants int

		if err := s.App.DB.Get(ctx, &variants, `SELECT COUNT(*) FROM wt_media_variant`); err != nil || variants == 0 {
			t.Fatalf("want resized copies, got %d: %v", variants, err)
		}

		sqlite := s.App.DB.Dialect() == db.SQLite

		// The screen says what the download holds; on PostgreSQL and MySQL,
		// how to back up the database.
		_, screen := s.Get("/admin/backup")

		for _, part := range []string{
			`href="/admin/backup"`,
			`action="/admin/backup/download"`,
			">Download backup</button>",
			"manifest.json",
			"password hashes",
		} {
			if !strings.Contains(screen, part) {
				t.Fatalf("the screen misses %q:\n%s", part, screen)
			}
		}

		tool := "pg_dump"
		if s.App.DB.Dialect() == db.MySQL {
			tool = "mysqldump"
		}

		switch {
		case sqlite && (strings.Contains(screen, "pg_dump") || !strings.Contains(screen, "site.sqlite")):
			t.Fatalf("the SQLite screen:\n%s", screen)
		case !sqlite && (!strings.Contains(screen, tool) || strings.Contains(screen, "site.sqlite") ||
			!strings.Contains(screen, "https://docs.gowebthings.com/go-webthings/databases#backups")):
			t.Fatalf("the screen does not say to back up the database with %s:\n%s", tool, screen)
		}

		// The copy of the database is made in the temporary folder and
		// removed afterwards.
		temporary := t.TempDir()
		t.Setenv("TMPDIR", temporary)

		before := time.Now()
		response, body := downloadBackup(t, s)

		if response.StatusCode != http.StatusOK {
			t.Fatalf("download: %d\n%s", response.StatusCode, body)
		}

		if left, err := os.ReadDir(temporary); err != nil || len(left) != 0 {
			t.Fatalf("the temporary folder holds %v: %v", left, err)
		}

		if kind := response.Header.Get("Content-Type"); kind != "application/zip" {
			t.Fatalf("Content-Type %q", kind)
		}

		if cache := response.Header.Get("Cache-Control"); cache != "no-store" {
			t.Fatalf("Cache-Control %q", cache)
		}

		disposition, params, err := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
		match := backupName.FindStringSubmatch(params["filename"])

		if err != nil || disposition != "attachment" || match == nil {
			t.Fatalf("Content-Disposition %q: %v", response.Header.Get("Content-Disposition"), err)
		}

		kathmandu, err := time.LoadLocation("Asia/Kathmandu")
		if err != nil {
			t.Fatal(err)
		}

		named, err := time.ParseInLocation("20060102-1504", match[1], kathmandu)
		if err != nil || named.Before(before.Add(-time.Minute)) || named.After(time.Now()) {
			t.Fatalf("file name %q is not the time in the site's timezone", params["filename"])
		}

		archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatalf("the zip does not open: %v", err)
		}

		files := map[string][]byte{}
		methods := map[string]uint16{}

		for _, file := range archive.File {
			files[file.Name] = readZipFile(t, file)
			methods[file.Name] = file.Method
		}

		// Photos are compressed already and go in as they are; text is
		// compressed.
		if methods["media/"+photo.StorageKey] != zip.Store || methods["media/"+notes.StorageKey] != zip.Deflate ||
			methods["manifest.json"] != zip.Deflate {
			t.Fatalf("compression: %v", methods)
		}

		names := slices.Sorted(maps.Keys(files))
		want := []string{"manifest.json", "media/" + notes.StorageKey, "media/" + old.StorageKey, "media/" + photo.StorageKey}

		if sqlite {
			want = append(want, "site.sqlite")
		}

		slices.Sort(want)

		if !slices.Equal(names, want) {
			t.Fatalf("the zip holds %q, want %q", names, want)
		}

		if !bytes.Equal(files["media/"+photo.StorageKey], photoData) ||
			!bytes.Equal(files["media/"+notes.StorageKey], notesData) {
			t.Fatal("the media files differ from the uploads")
		}

		var manifest siteadmin.BackupManifest

		if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
			t.Fatalf("manifest: %v\n%s", err, files["manifest.json"])
		}

		versions, err := s.App.DB.SchemaVersions(ctx)
		if err != nil {
			t.Fatal(err)
		}

		switch {
		case manifest.Site != "Test site",
			manifest.Dialect != s.App.DB.Dialect(),
			manifest.DatabaseIncluded != sqlite,
			manifest.MediaFiles != 3,
			manifest.CreatedAt.Location() != time.UTC,
			manifest.CreatedAt.Before(before.Add(-time.Second)),
			manifest.CreatedAt.After(time.Now()),
			!maps.Equal(manifest.SchemaVersions, versions),
			manifest.SchemaVersions["media"] == 0:
			t.Fatalf("manifest:\n%s", files["manifest.json"])
		}

		if sqlite {
			checkBackupDatabase(t, files["site.sqlite"], photo.ID, photo.StorageKey)
		}
	})
}

// readZipFile reads a file in a zip.
func readZipFile(t *testing.T, file *zip.File) []byte {
	t.Helper()

	reader, err := file.Open()
	if err != nil {
		t.Fatalf("%s: %v", file.Name, err)
	}

	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("%s: %v", file.Name, err)
	}

	return data
}

// checkBackupDatabase checks that data, the site.sqlite of a backup, opens
// as a database with the site's schema and the uploaded file with ID id.
func checkBackupDatabase(t *testing.T, data []byte, id, key string) {
	t.Helper()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "site.sqlite")

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	restored, err := db.Open(ctx, "sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = restored.Close() }()

	versions, err := restored.SchemaVersions(ctx)
	if err != nil || versions["auth"] == 0 || versions["media"] == 0 {
		t.Fatalf("schema versions in site.sqlite: %v %v", versions, err)
	}

	var stored string

	if err := restored.Get(ctx, &stored, `SELECT storage_key FROM wt_media_asset WHERE id=?`, id); err != nil || stored != key {
		t.Fatalf("the uploaded file in site.sqlite: %q %v", stored, err)
	}
}

// TestBackupCopyFails checks that a copy of the database that cannot be
// made answers with an error, not with a zip: it is made before the
// response starts.
func TestBackupCopyFails(t *testing.T) {
	s := sitetest.Start(t)

	// The copy goes to the temporary folder, which is not there.
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))

	response, body := downloadBackup(t, s)
	if response.StatusCode != http.StatusInternalServerError ||
		response.Header.Get("Content-Disposition") != "" ||
		response.Header.Get("Content-Type") == "application/zip" {
		t.Fatalf("a failed copy: %d %v\n%s", response.StatusCode, response.Header, body)
	}
}

// TestBackupIsAdminOnly checks that only administrators see and use the
// Backup screen: an editor with every right of every section, Backup's
// included, does not see it in the navigation and is refused the screen
// and the download.
func TestBackupIsAdminOnly(t *testing.T) {
	s := sitetest.Start(t)
	ctx := t.Context()

	user, err := s.App.Auth.CreateUser(ctx, "editor@example.test", "", "editor long passphrase", auth.Editor)
	if err != nil {
		t.Fatal(err)
	}

	everything := auth.Permissions{}

	for _, section := range []string{"pages", "media", "site", "theme", "accounts", "groups", "backup"} {
		everything[section] = auth.Actions
	}

	group, err := s.App.Auth.SaveGroup(ctx, auth.Group{Name: "Everything", Permissions: everything})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.App.Auth.SetUserGroups(ctx, user.ID, []string{group.ID}); err != nil {
		t.Fatal(err)
	}

	editor := s.As("editor@example.test", "editor long passphrase")

	editor.Expect("/admin/", http.StatusOK, `href="/admin/site"`)
	editor.ExpectMissing("/admin/", `href="/admin/backup"`)

	if status, _ := editor.Get("/admin/backup"); status != http.StatusForbidden {
		t.Fatalf("the screen for an editor: %d", status)
	}

	status, body := editor.PostStatus("/admin/", "/admin/backup/download", url.Values{})
	if status != http.StatusForbidden || !strings.Contains(body, "You are not allowed to do this.") {
		t.Fatalf("the download for an editor: %d\n%s", status, body)
	}

	if response, _ := downloadBackup(t, editor); response.StatusCode != http.StatusForbidden {
		t.Fatalf("the download for an editor: %d", response.StatusCode)
	}
}

// TestBackupInDutch checks the Dutch texts of the navigation, the screen and
// its button.
func TestBackupInDutch(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Configure: func(config *webthings.Config) {
		config.AdminLanguage = "nl"
	}})

	s.Expect("/admin/backup", http.StatusOK, ">Back-up</", ">Back-up downloaden</button>", "Systeem")
	s.ExpectMissing("/admin/backup", "Download backup")
}
