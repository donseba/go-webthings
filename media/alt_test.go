package media_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"testing"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"
)

// TestAltTextAndResizable checks the helpers that show images in content:
// the alt text of a language, by asset ID or URL, and which images keep what
// they show in a variant.
func TestAltTextAndResizable(t *testing.T) {
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

		var animation bytes.Buffer

		frame := image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black, color.White})
		if err := gif.EncodeAll(&animation, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 10}}); err != nil {
			t.Fatal(err)
		}

		moving, err := library.Upload(ctx, "moving.gif", &animation)
		if err != nil {
			t.Fatal(err)
		}

		err = library.SaveTranslation(ctx, picture.ID, media.Translation{Locale: "nl", AltText: "Een foto"})
		if err != nil {
			t.Fatal(err)
		}

		for ref, want := range map[string]string{
			picture.ID:                   "Een foto",
			library.URL(ctx, picture.ID): "Een foto",
			moving.ID:                    "",
			"not-an-asset":               "",
			db.NewID():                   "",
		} {
			if got := library.AltText(ctx, ref, "nl"); got != want {
				t.Fatalf("alt text of %s: %q, want %q", ref, got, want)
			}
		}

		if got := library.AltText(ctx, picture.ID, "en"); got != "" {
			t.Fatalf("alt text in another language: %q", got)
		}

		if !library.Resizable(ctx, picture.ID) || library.Resizable(ctx, moving.ID) || library.Resizable(ctx, db.NewID()) {
			t.Fatal("resizable: want only the PNG")
		}
	})
}
