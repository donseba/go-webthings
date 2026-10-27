package media_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()

	var buffer bytes.Buffer

	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

func TestUploadServeAndDelete(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		// Without a base URL of its own, the files are served by the
		// library's routes.
		storage, err := media.NewFilesystem(t.TempDir(), "")
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = storage.Close() })

		library := media.New(database, storage)
		handler := serve(library)

		asset, err := library.Upload(ctx, "evil.exe", strings.NewReader("MZ\x90\x00binary"))
		if !errors.Is(err, media.ErrUnsupportedType) {
			t.Fatalf("want unsupported type, got %v", err)
		}

		// Office documents are zip files and CSV is text: the extension decides.
		for name, want := range map[string]string{
			"minutes.docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			"budget.ods":   "application/vnd.oasis.opendocument.spreadsheet",
			"archive.zip":  "application/zip",
			"members.csv":  "text/csv; charset=utf-8",
			"notes.txt":    "text/plain; charset=utf-8",
		} {
			data := "PK\x03\x04 zip container"
			if strings.HasSuffix(name, ".csv") || strings.HasSuffix(name, ".txt") {
				data = "name,email\nAda,ada@example.test\n"
			}

			document, err := library.Upload(ctx, name, strings.NewReader(data))
			if err != nil || document.MIMEType != want {
				t.Fatalf("%s: %v %q", name, err, document.MIMEType)
			}
		}

		// The content decides first: text named .docx stays text.
		fake, err := library.Upload(ctx, "fake.docx", strings.NewReader("plain text"))
		if err != nil || fake.MIMEType != "text/plain; charset=utf-8" || !strings.HasSuffix(fake.StorageKey, ".txt") {
			t.Fatalf("text renamed to docx: %v %+v", err, fake)
		}

		asset, err = library.Upload(ctx, "../photo.png", bytes.NewReader(pngBytes(t)))
		if err != nil {
			t.Fatal(err)
		}

		if asset.Filename != "photo.png" || asset.Width != 3 || asset.Height != 2 {
			t.Fatalf("unexpected asset: %+v", asset)
		}

		err = library.SaveTranslation(ctx, asset.ID, media.Translation{Locale: "nl", Title: "Foto", AltText: "Een foto"})
		if err != nil {
			t.Fatal(err)
		}

		loaded, err := library.Get(ctx, asset.ID)
		if err != nil || loaded.Text("nl").AltText != "Een foto" {
			t.Fatalf("translation: %+v %v", loaded, err)
		}

		url := library.URL(ctx, asset.ID)
		if url != "/media/"+asset.ID+".png" {
			t.Fatalf("asset URL: %q", url)
		}

		response := get(handler, url)
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("serve: %d %v", response.Code, response.Header())
		}

		_, err = database.Exec(ctx,
			`INSERT INTO wt_media_usage(asset_id,owner,owner_id) VALUES(?,?,?)`,
			asset.ID, "test", "row",
		)
		if err != nil {
			t.Fatal(err)
		}

		if err := library.Delete(ctx, asset.ID); !errors.Is(err, media.ErrInUse) {
			t.Fatalf("want in use, got %v", err)
		}

		if _, err := database.Exec(ctx, `DELETE FROM wt_media_usage WHERE owner=?`, "test"); err != nil {
			t.Fatal(err)
		}

		if err := library.Delete(ctx, asset.ID); err != nil {
			t.Fatal(err)
		}

		if library.URL(ctx, asset.ID) != "" {
			t.Fatal("deleted asset still has a URL")
		}

		// An asset in the trash is not served, listed or found, and cannot
		// get new uses; it comes back with Undelete.
		response = get(handler, url)
		if response.Code != http.StatusNotFound {
			t.Fatalf("asset in the trash served: %d", response.Code)
		}

		if _, err := library.Get(ctx, asset.ID); !errors.Is(err, media.ErrNotFound) {
			t.Fatalf("get of an asset in the trash: %v", err)
		}

		if err := media.Track(ctx, database, "test", "row", []string{asset.ID}); !errors.Is(err, media.ErrNotFound) {
			t.Fatalf("track of an asset in the trash: %v", err)
		}

		trashed, _, err := library.Trashed(ctx, 0, 0)
		if err != nil || len(trashed) != 1 || trashed[0].DeletedAt == nil {
			t.Fatalf("trash: %+v %v", trashed, err)
		}

		if err := library.Undelete(ctx, asset.ID); err != nil {
			t.Fatal(err)
		}

		if library.URL(ctx, asset.ID) == "" {
			t.Fatal("undeleted asset has no URL")
		}

		// Purge takes only assets in the trash, and keeps those in use.
		if err := library.Purge(ctx, asset.ID); !errors.Is(err, media.ErrNotFound) {
			t.Fatalf("purge of a live asset: %v", err)
		}

		if err := library.Delete(ctx, asset.ID); err != nil {
			t.Fatal(err)
		}

		if _, err := database.Exec(ctx,
			`INSERT INTO wt_media_usage(asset_id,owner,owner_id) VALUES(?,?,?)`,
			asset.ID, "test", "row",
		); err != nil {
			t.Fatal(err)
		}

		if err := library.Purge(ctx, asset.ID); !errors.Is(err, media.ErrInUse) {
			t.Fatalf("purge of a used asset: %v", err)
		}

		if _, err := database.Exec(ctx, `DELETE FROM wt_media_usage WHERE owner=?`, "test"); err != nil {
			t.Fatal(err)
		}

		if err := library.Purge(ctx, asset.ID); err != nil {
			t.Fatal(err)
		}

		if trashed, _, _ := library.Trashed(ctx, 0, 0); len(trashed) != 0 {
			t.Fatalf("purged asset in the trash: %+v", trashed)
		}
	})
}

// TestListWithTranslations checks that the library list carries the titles
// and alt texts of every asset in every language, and that the plain list
// still leaves them out.
func TestListWithTranslations(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		storage, err := media.NewFilesystem(t.TempDir(), "/media")
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = storage.Close() })

		library := media.New(database, storage)

		empty, err := library.ListWithTranslations(ctx)
		if err != nil || len(empty) != 0 {
			t.Fatalf("empty library: %+v %v", empty, err)
		}

		photo, err := library.Upload(ctx, "photo.png", bytes.NewReader(pngBytes(t)))
		if err != nil {
			t.Fatal(err)
		}

		notes, err := library.Upload(ctx, "notes.txt", strings.NewReader("Plain text."))
		if err != nil {
			t.Fatal(err)
		}

		translations := []media.Translation{
			{Locale: "nl", Title: "Strand", AltText: "Een zonnig strand"},
			{Locale: "en", Title: "Beach", AltText: "A sunny beach"},
		}

		for _, translation := range translations {
			if err := library.SaveTranslation(ctx, photo.ID, translation); err != nil {
				t.Fatal(err)
			}
		}

		assets, err := library.ListWithTranslations(ctx)
		if err != nil || len(assets) != 2 {
			t.Fatalf("list: %+v %v", assets, err)
		}

		for _, asset := range assets {
			switch asset.ID {
			case photo.ID:
				if len(asset.Translations) != 2 ||
					asset.Text("en").AltText != "A sunny beach" ||
					asset.Text("nl").Title != "Strand" {
					t.Fatalf("photo translations: %+v", asset.Translations)
				}
			case notes.ID:
				if len(asset.Translations) != 0 {
					t.Fatalf("notes translations: %+v", asset.Translations)
				}
			default:
				t.Fatalf("unexpected asset: %+v", asset)
			}
		}

		plain, err := library.List(ctx)
		if err != nil || len(plain) != 2 || plain[0].Translations != nil || plain[1].Translations != nil {
			t.Fatalf("plain list: %+v %v", plain, err)
		}
	})
}

// TestOriginals checks that Originals names the uploaded files of the assets
// in the library and in the trash, oldest first, without the variants of
// images and without the files of assets deleted for good, and that Storage
// is where they are kept.
func TestOriginals(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := t.Context()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		storage, err := media.NewFilesystem(t.TempDir(), "")
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = storage.Close() })

		library := media.New(database, storage)

		if library.Storage() != storage {
			t.Fatal("Storage is not the library's storage")
		}

		// Variants of uploaded images are made in the background.
		background, stop := context.WithCancel(ctx)
		t.Cleanup(stop)

		go library.Prepare(background)

		var uploaded []media.Asset

		for _, name := range []string{"photo.png", "notes.txt", "old.png", "gone.txt"} {
			data := []byte("Plain text.")
			if strings.HasSuffix(name, ".png") {
				data = pngBytes(t)
			}

			asset, err := library.Upload(ctx, name, bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}

			uploaded = append(uploaded, asset)
		}

		if err := library.WaitPrepared(ctx); err != nil {
			t.Fatal(err)
		}

		var variants int

		if err := database.Get(ctx, &variants, `SELECT COUNT(*) FROM wt_media_variant`); err != nil || variants == 0 {
			t.Fatalf("want variants of the images, got %d: %v", variants, err)
		}

		// old.png goes to the trash, gone.txt is deleted for good.
		for _, asset := range uploaded[2:] {
			if err := library.Delete(ctx, asset.ID); err != nil {
				t.Fatal(err)
			}
		}

		if err := library.Purge(ctx, uploaded[3].ID); err != nil {
			t.Fatal(err)
		}

		keys, err := library.Originals(ctx)
		if err != nil {
			t.Fatal(err)
		}

		want := []string{uploaded[0].StorageKey, uploaded[1].StorageKey, uploaded[2].StorageKey}
		if !slices.Equal(keys, want) {
			t.Fatalf("originals %q, want %q", keys, want)
		}
	})
}
