package media_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"
)

// TestAltTextCache checks that alt texts, kept in memory with the asset,
// follow every change at once, and that each change moves Version on.
func TestAltTextCache(t *testing.T) {
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

		picture, err := library.Upload(ctx, "photo.png", bytes.NewReader(photo(t, 40, 20)))
		if err != nil {
			t.Fatal(err)
		}

		// Read once, so the asset is cached without an alt text.
		if got := library.AltText(ctx, picture.ID, "en"); got != "" {
			t.Fatalf("alt text before any: %q", got)
		}

		version := library.Version()

		// changed checks that the last change moved the version on.
		changed := func(what string) {
			t.Helper()

			if library.Version() == version {
				t.Fatalf("%s did not change the version", what)
			}

			version = library.Version()
		}

		err = library.SaveTranslation(ctx, picture.ID, media.Translation{Locale: "en", AltText: "A photo"})
		if err != nil {
			t.Fatal(err)
		}

		changed("saving a translation")

		if got := library.AltText(ctx, picture.ID, "en"); got != "A photo" {
			t.Fatalf("alt text after saving: %q", got)
		}

		err = library.SaveTranslation(ctx, picture.ID, media.Translation{Locale: "en", AltText: "A better photo"})
		if err != nil {
			t.Fatal(err)
		}

		changed("changing a translation")

		if got := library.AltText(ctx, picture.ID, "en"); got != "A better photo" {
			t.Fatalf("alt text after changing: %q", got)
		}

		if err := library.SetFocalPoint(ctx, picture.ID, media.FocalPoint{X: 0.2, Y: 0.3}); err != nil {
			t.Fatal(err)
		}

		changed("setting the focal point")

		if err := library.Delete(ctx, picture.ID); err != nil {
			t.Fatal(err)
		}

		changed("deleting")

		if got := library.AltText(ctx, picture.ID, "en"); got != "" {
			t.Fatalf("alt text of an asset in the trash: %q", got)
		}

		if err := library.Undelete(ctx, picture.ID); err != nil {
			t.Fatal(err)
		}

		changed("undeleting")

		if got := library.AltText(ctx, picture.ID, "en"); got != "A better photo" {
			t.Fatalf("alt text after undeleting: %q", got)
		}

		// A failed change changes nothing.
		if err := library.Undelete(ctx, db.NewID()); err == nil {
			t.Fatal("undeleted an unknown asset")
		}

		if library.Version() != version {
			t.Fatal("a failed change moved the version on")
		}
	})
}
