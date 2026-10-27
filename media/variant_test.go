package media_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"

	"github.com/donseba/go-router"

	"github.com/gen2brain/webp"
)

func TestParseVariant(t *testing.T) {
	for spec, want := range map[string]media.Variant{
		"800x600":           {Width: 800, Height: 600, Format: "webp"},
		"800":               {Width: 800, Format: "webp"},
		"x600":              {Height: 600, Format: "webp"},
		"800x600 crop":      {Width: 800, Height: 600, Crop: true, Format: "webp"},
		"800 jpg":           {Width: 800, Format: "jpg"},
		" 400X300  PNG ":    {Width: 400, Height: 300, Format: "png"},
		"120x120 crop jpeg": {Width: 120, Height: 120, Crop: true, Format: "jpg"},
	} {
		got, err := media.ParseVariant(spec)
		if err != nil || got != want {
			t.Fatalf("%q: got %+v, %v; want %+v", spec, got, err, want)
		}
	}

	for _, spec := range []string{"", "x", "0x0", "4001", "800x600 gif", "800 crop", "-5", "800x600 big", "axb"} {
		if _, err := media.ParseVariant(spec); !errors.Is(err, media.ErrVariant) {
			t.Fatalf("%q: want ErrVariant, got %v", spec, err)
		}
	}
}

func TestVariantFit(t *testing.T) {
	cases := []struct {
		variant       media.Variant
		width, height int
		size, part    image.Point
	}{
		// Fit inside the box, keeping proportions.
		{media.Variant{Width: 800, Height: 600}, 4000, 2000, image.Pt(800, 400), image.Pt(4000, 2000)},
		{media.Variant{Width: 800, Height: 600}, 1000, 2000, image.Pt(300, 600), image.Pt(1000, 2000)},
		{media.Variant{Width: 800}, 1600, 900, image.Pt(800, 450), image.Pt(1600, 900)},
		{media.Variant{Height: 300}, 1600, 900, image.Pt(533, 300), image.Pt(1600, 900)},
		// Never larger than the image.
		{media.Variant{Width: 800, Height: 600}, 400, 200, image.Pt(400, 200), image.Pt(400, 200)},
		// Crops cut the centre to the box's proportions, then scale.
		{media.Variant{Width: 800, Height: 600, Crop: true}, 4000, 2000, image.Pt(800, 600), image.Pt(2667, 2000)},
		{media.Variant{Width: 300, Height: 300, Crop: true}, 1000, 2000, image.Pt(300, 300), image.Pt(1000, 1000)},
		// A crop larger than the image keeps the proportions at the image's size.
		{media.Variant{Width: 800, Height: 400, Crop: true}, 600, 600, image.Pt(600, 300), image.Pt(600, 300)},
	}

	for _, c := range cases {
		size, part := c.variant.Fit(c.width, c.height)
		if size != c.size || part != c.part {
			t.Fatalf("%+v of %dx%d: got %v %v, want %v %v", c.variant, c.width, c.height, size, part, c.size, c.part)
		}
	}
}

// photo is a PNG of the given size: left half red, right half blue.
func photo(t *testing.T, width, height int) []byte {
	t.Helper()

	picture := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := range height {
		for x := range width {
			fill := color.RGBA{R: 255, A: 255}
			if x >= width/2 {
				fill = color.RGBA{B: 255, A: 255}
			}

			picture.Set(x, y, fill)
		}
	}

	var buffer bytes.Buffer

	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

// withOrientation is a JPEG of width × height whose EXIF says to turn it a
// quarter clockwise (orientation 6).
func withOrientation(t *testing.T, width, height int) []byte {
	t.Helper()

	return exifJPEG(t, image.NewRGBA(image.Rect(0, 0, width, height)), 6)
}

// exifJPEG encodes picture as a JPEG whose EXIF gives orientation.
func exifJPEG(t *testing.T, picture image.Image, orientation byte) []byte {
	t.Helper()

	var encoded bytes.Buffer

	if err := jpeg.Encode(&encoded, picture, nil); err != nil {
		t.Fatal(err)
	}

	// TIFF header, one directory entry: orientation (0x0112), SHORT, 1, value.
	tiff := []byte("II*\x00\x08\x00\x00\x00\x01\x00")
	tiff = append(tiff, 0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, orientation, 0x00, 0x00, 0x00)
	tiff = append(tiff, 0, 0, 0, 0)

	segment := append([]byte("Exif\x00\x00"), tiff...)
	header := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(header[2:], uint16(len(segment)+2))

	data := append([]byte{0xFF, 0xD8}, header...)
	data = append(data, segment...)

	return append(data, encoded.Bytes()[2:]...)
}

func TestResize(t *testing.T) {
	out, err := media.Resize(photo(t, 400, 200), media.Variant{Width: 100, Height: 100})
	if err != nil {
		t.Fatal(err)
	}

	picture, err := webp.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}

	if size := picture.Bounds().Size(); size != image.Pt(100, 50) {
		t.Fatalf("size %v", size)
	}

	// The scaled image keeps its content: red on the left, blue on the right.
	if r, _, b, _ := picture.At(10, 25).RGBA(); r < b {
		t.Fatal("left is not red")
	}

	if r, _, b, _ := picture.At(90, 25).RGBA(); b < r {
		t.Fatal("right is not blue")
	}

	cropped, err := media.Resize(photo(t, 400, 200), media.Variant{Width: 50, Height: 50, Crop: true, Format: "jpg"})
	if err != nil {
		t.Fatal(err)
	}

	config, err := jpeg.DecodeConfig(bytes.NewReader(cropped))
	if err != nil || config.Width != 50 || config.Height != 50 {
		t.Fatalf("crop: %+v %v", config, err)
	}

	// EXIF orientation 6: the stored 40 × 20 photo is shown 20 × 40.
	turned, err := media.Resize(withOrientation(t, 40, 20), media.Variant{Width: 100, Format: "png"})
	if err != nil {
		t.Fatal(err)
	}

	config, err = png.DecodeConfig(bytes.NewReader(turned))
	if err != nil || config.Width != 20 || config.Height != 40 {
		t.Fatalf("orientation: %+v %v", config, err)
	}
}

func get(handler http.Handler, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))

	return response
}

// serve registers the library's routes on a router mounted at /media, as
// webthings.App does, and returns the site that serves them.
func serve(library *media.Service) http.Handler {
	site := router.NewSite()
	routes := router.NewSite()

	site.Mount("/media", routes, router.NamePrefix("media."))
	library.RegisterRoutes(routes)

	return site
}

func TestVariantHandler(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		dir := t.TempDir()

		storage, err := media.NewFilesystem(dir, "")
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = storage.Close() })

		library := media.New(database, storage)
		library.SetSigningKey([]byte("test key"))

		// Without its routes the library has no variant addresses.
		if _, err := library.VariantURL(ctx, "00000000-0000-0000-0000-000000000000", media.Variant{Width: 10}); err == nil {
			t.Fatal("variant URL without routes")
		}

		handler := serve(library)

		asset, err := library.Upload(ctx, "photo.png", bytes.NewReader(photo(t, 400, 200)))
		if err != nil {
			t.Fatal(err)
		}

		variant, _ := media.ParseVariant("100x100")

		// A media URL and the asset ID give the same variant.
		link, err := library.VariantURL(ctx, library.URL(ctx, asset.ID), variant)
		if err != nil {
			t.Fatal(err)
		}

		byID, _ := library.VariantURL(ctx, asset.ID, variant)
		if link != byID || !strings.HasPrefix(link, "/media/v/") || !strings.HasSuffix(link, asset.ID+"_100x100.webp") {
			t.Fatalf("variant URL %q, by ID %q", link, byID)
		}

		response := get(handler, link)
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/webp" {
			t.Fatalf("variant: %d %v", response.Code, response.Header())
		}

		if !strings.Contains(response.Header().Get("Cache-Control"), "immutable") {
			t.Fatalf("variant is not cacheable: %v", response.Header())
		}

		config, err := webp.DecodeConfig(response.Body)
		if err != nil || config.Width != 100 || config.Height != 50 {
			t.Fatalf("variant size: %+v %v", config, err)
		}

		// The variant is kept: it is served without the original.
		key := asset.ID + "_100x100.webp"
		if _, err := os.Stat(filepath.Join(dir, key)); err != nil {
			t.Fatal(err)
		}

		original := filepath.Join(dir, asset.StorageKey)

		saved, err := os.ReadFile(original)
		if err != nil {
			t.Fatal(err)
		}

		if err := os.Remove(original); err != nil {
			t.Fatal(err)
		}

		if response := get(handler, link); response.Code != http.StatusOK {
			t.Fatalf("stored variant: %d", response.Code)
		}

		if err := os.WriteFile(original, saved, 0o600); err != nil {
			t.Fatal(err)
		}

		// Tampered URLs are refused: another size with the same signature, or
		// another signature.
		for _, tampered := range []string{
			strings.Replace(link, "_100x100", "_4000x4000", 1),
			strings.Replace(link, "/v/", "/v/x", 1),
			strings.Replace(link, ".webp", ".png", 1),
		} {
			if response := get(handler, tampered); response.Code != http.StatusNotFound {
				t.Fatalf("%s: want 404, got %d", tampered, response.Code)
			}
		}

		// Documents have no variants.
		document, err := library.Upload(ctx, "notes.txt", strings.NewReader("plain text"))
		if err != nil {
			t.Fatal(err)
		}

		if _, err := library.VariantURL(ctx, document.ID, variant); !errors.Is(err, media.ErrVariant) {
			t.Fatalf("document variant: %v", err)
		}

		// srcset leaves out widths beyond the image and offers its own width.
		set, err := library.Srcset(ctx, asset.ID, "webp", 100, 300, 800, 1200)
		if err != nil {
			t.Fatal(err)
		}

		if !strings.HasSuffix(set, "_400x0.webp 400w") || strings.Count(set, ", ") != 2 {
			t.Fatalf("srcset: %s", set)
		}

		// A deleted asset's variants are not served; purging it deletes them.
		if err := library.Delete(ctx, asset.ID); err != nil {
			t.Fatal(err)
		}

		if response := get(handler, link); response.Code != http.StatusNotFound {
			t.Fatalf("variant of an asset in the trash: %d", response.Code)
		}

		if err := library.Purge(ctx, asset.ID); err != nil {
			t.Fatal(err)
		}

		if _, err := os.Stat(filepath.Join(dir, key)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("variant file remains: %v", err)
		}

		var count int
		if err := database.Get(ctx, &count, `SELECT COUNT(*) FROM wt_media_variant`); err != nil || count != 0 {
			t.Fatalf("variant rows remain: %d %v", count, err)
		}

		if response := get(handler, link); response.Code != http.StatusNotFound {
			t.Fatalf("variant of deleted asset: %d", response.Code)
		}
	})
}
