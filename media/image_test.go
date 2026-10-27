package media_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"

	"github.com/gen2brain/webp"
)

func TestImage(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		storage, err := media.NewFilesystem(t.TempDir(), "")
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = storage.Close() })

		library := media.New(database, storage)
		handler := serve(library)

		large, err := library.Upload(ctx, "large.png", bytes.NewReader(photo(t, 2000, 1000)))
		if err != nil {
			t.Fatal(err)
		}

		// A large image is shown as a WebP copy of the asked width, with a
		// srcset up to it and the upload as the original.
		shown := library.Image(ctx, large.ID, media.ImageWidth)
		original := library.URL(ctx, large.ID)

		if !strings.HasSuffix(shown.URL, large.ID+"_1600x0.webp") || shown.OriginalURL != original {
			t.Fatalf("image: %+v", shown)
		}

		if shown.Width != 1600 || shown.Height != 800 {
			t.Fatalf("image size: %d × %d", shown.Width, shown.Height)
		}

		for _, width := range []string{"480w", "800w", "1200w", "1600w"} {
			if !strings.Contains(shown.Srcset, width) {
				t.Fatalf("srcset without %s: %s", width, shown.Srcset)
			}
		}

		if strings.Contains(shown.Srcset, original) || strings.Count(shown.Srcset, ", ") != 3 {
			t.Fatalf("srcset: %s", shown.Srcset)
		}

		response := get(handler, shown.URL)

		config, err := webp.DecodeConfig(response.Body)
		if response.Code != http.StatusOK || err != nil || config.Width != 1600 || config.Height != 800 {
			t.Fatalf("variant: %d %+v %v", response.Code, config, err)
		}

		// A small image is never made larger: its own width is the largest.
		small, err := library.Upload(ctx, "small.png", bytes.NewReader(photo(t, 600, 300)))
		if err != nil {
			t.Fatal(err)
		}

		shown = library.Image(ctx, small.ID, media.LargeImageWidth)
		if !strings.HasSuffix(shown.URL, small.ID+"_600x0.webp") || shown.Width != 600 ||
			!strings.HasSuffix(shown.Srcset, "_600x0.webp 600w") || strings.Count(shown.Srcset, ", ") != 1 {
			t.Fatalf("small image: %+v", shown)
		}

		// The template functions take the variant URL module views give for
		// the asset itself.
		crop, err := library.VariantURL(ctx, shown.URL, media.Variant{Width: 100, Height: 100, Crop: true})
		if err != nil || !strings.HasSuffix(crop, small.ID+"_100x100c.webp") {
			t.Fatalf("variant of a variant URL: %s %v", crop, err)
		}

		if !library.Resizable(ctx, shown.URL) {
			t.Fatal("variant URL is not the asset")
		}

		// A GIF may be animated: it keeps its own address.
		animation, err := library.Upload(ctx, "animation.gif", bytes.NewReader(still(t)))
		if err != nil {
			t.Fatal(err)
		}

		shown = library.Image(ctx, animation.ID, media.ImageWidth)
		original = library.URL(ctx, animation.ID)

		if shown.URL != original || shown.OriginalURL != original || shown.Srcset != "" || shown.Width != 8 {
			t.Fatalf("GIF: %+v", shown)
		}

		// Files outside the library, such as a theme's SVG logo, are shown
		// as they are; unknown assets give nothing.
		logo := "/assets/default/logo.svg"
		if shown := library.Image(ctx, logo, 600); shown != (media.Image{URL: logo, OriginalURL: logo}) {
			t.Fatalf("SVG: %+v", shown)
		}

		if shown := library.Image(ctx, "00000000-0000-0000-0000-000000000000", 600); shown != (media.Image{}) {
			t.Fatalf("unknown asset: %+v", shown)
		}

		if shown := library.Image(ctx, "", 600); shown != (media.Image{}) {
			t.Fatalf("no image: %+v", shown)
		}
	})
}

// still is an 8 × 4 GIF.
func still(t *testing.T) []byte {
	t.Helper()

	picture := image.NewPaletted(image.Rect(0, 0, 8, 4), color.Palette{color.White, color.Black})

	var buffer bytes.Buffer

	if err := gif.Encode(&buffer, picture, nil); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

// TestImageAs checks the formats that depend on the image: the logo keeps
// the format of its upload, mail gets JPEG unless the PNG is transparent.
func TestImageAs(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		storage, err := media.NewFilesystem(t.TempDir(), "")
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = storage.Close() })

		library := media.New(database, storage)
		handler := serve(library)

		upload := func(name string, data []byte) string {
			t.Helper()

			asset, err := library.Upload(ctx, name, bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}

			return asset.ID
		}

		opaque := upload("opaque.png", photo(t, 1600, 800))
		alpha := upload("alpha.png", encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 1600, 800))))
		palette := upload("palette.png", encodePNG(t, image.NewPaletted(
			image.Rect(0, 0, 1600, 800), color.Palette{color.Transparent, color.Black},
		)))
		solid := upload("solid.png", encodePNG(t, image.NewPaletted(
			image.Rect(0, 0, 1600, 800), color.Palette{color.White, color.Black},
		)))
		small := upload("small.png", photo(t, 400, 200))
		photograph := upload("photo.jpg", encodeJPEG(t, 2000, 1000))
		animation := upload("animation.gif", still(t))

		for _, want := range []struct {
			id, format, suffix, decoded string
			width                       int
		}{
			// A logo keeps its format, at most the asked width.
			{opaque, media.OriginalFormat, opaque + "_600x0.png", "png", 600},
			{photograph, media.OriginalFormat, photograph + "_600x0.jpg", "jpeg", 600},
			// A PNG that needs no resizing is the upload itself.
			{small, media.OriginalFormat, small + ".png", "png", 400},
			{animation, media.OriginalFormat, animation + ".gif", "gif", 8},
			// Mail gets JPEG, or PNG for a PNG with transparency.
			{opaque, media.MailFormat, opaque + "_600x0.jpg", "jpeg", 600},
			{solid, media.MailFormat, solid + "_600x0.jpg", "jpeg", 600},
			{alpha, media.MailFormat, alpha + "_600x0.png", "png", 600},
			{palette, media.MailFormat, palette + "_600x0.png", "png", 600},
			{photograph, media.MailFormat, photograph + "_600x0.jpg", "jpeg", 600},
			// Other variant formats are taken as they are.
			{photograph, "png", photograph + "_600x0.png", "png", 600},
		} {
			shown := library.ImageAs(ctx, want.id, 600, want.format)
			if !strings.HasSuffix(shown.URL, want.suffix) || shown.Width != want.width {
				t.Fatalf("%s as %s: %+v, want …%s", want.id, want.format, shown, want.suffix)
			}

			// The srcset of a copy is in its format; an upload has none.
			copied := strings.Contains(want.suffix, "_")
			if copied != strings.Contains(shown.Srcset, want.suffix+" 600w") {
				t.Fatalf("%s as %s: srcset %s", want.id, want.format, shown.Srcset)
			}

			response := get(handler, shown.URL)

			config, format, err := image.DecodeConfig(response.Body)
			if response.Code != http.StatusOK || err != nil || format != want.decoded || config.Width != want.width {
				t.Fatalf("%s as %s: %d %s %+v %v", want.id, want.format, response.Code, format, config, err)
			}
		}
	})
}

// encodePNG encodes picture as a PNG.
func encodePNG(t *testing.T, picture image.Image) []byte {
	t.Helper()

	var buffer bytes.Buffer

	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

// encodeJPEG is a grey width × height JPEG.
func encodeJPEG(t *testing.T, width, height int) []byte {
	t.Helper()

	var buffer bytes.Buffer

	if err := jpeg.Encode(&buffer, image.NewGray(image.Rect(0, 0, width, height)), nil); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}
