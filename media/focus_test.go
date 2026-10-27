package media_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"
)

func TestFocalPointWindow(t *testing.T) {
	cases := []struct {
		name   string
		bounds image.Rectangle
		size   image.Point
		focus  media.FocalPoint
		want   image.Point
	}{
		{"centre", image.Rect(0, 0, 400, 200), image.Pt(200, 200), media.Centre, image.Pt(100, 0)},
		{"left of centre", image.Rect(0, 0, 400, 200), image.Pt(200, 200), media.FocalPoint{X: 0.3, Y: 0.5}, image.Pt(20, 0)},
		{"left edge", image.Rect(0, 0, 400, 200), image.Pt(200, 200), media.FocalPoint{X: 0, Y: 0.5}, image.Pt(0, 0)},
		{"right edge", image.Rect(0, 0, 400, 200), image.Pt(200, 200), media.FocalPoint{X: 1, Y: 0.5}, image.Pt(200, 0)},
		{"top left corner", image.Rect(0, 0, 200, 1000), image.Pt(200, 200), media.FocalPoint{X: 0, Y: 0}, image.Pt(0, 0)},
		{"bottom right corner", image.Rect(0, 0, 200, 1000), image.Pt(200, 200), media.FocalPoint{X: 1, Y: 1}, image.Pt(0, 800)},
		{
			"near the bottom",
			image.Rect(0, 0, 200, 1000),
			image.Pt(200, 200),
			media.FocalPoint{X: 0.02, Y: 0.98},
			image.Pt(0, 800),
		},
		{
			"middle of a tall image",
			image.Rect(0, 0, 200, 1000),
			image.Pt(200, 200),
			media.FocalPoint{X: 0.5, Y: 0.6},
			image.Pt(0, 500),
		},
		{"panorama", image.Rect(0, 0, 10000, 100), image.Pt(100, 100), media.FocalPoint{X: 0.25, Y: 0.5}, image.Pt(2450, 0)},
		{"panorama end", image.Rect(0, 0, 10000, 100), image.Pt(100, 100), media.FocalPoint{X: 0.999, Y: 0}, image.Pt(9900, 0)},
		{"strip", image.Rect(0, 0, 1, 3000), image.Pt(1, 1), media.FocalPoint{X: 1, Y: 0.5}, image.Pt(0, 1500)},
		{"whole image", image.Rect(0, 0, 300, 200), image.Pt(300, 200), media.FocalPoint{X: 0.9, Y: 0.1}, image.Pt(0, 0)},
		{"offset bounds", image.Rect(10, 20, 410, 220), image.Pt(200, 200), media.FocalPoint{X: 1, Y: 1}, image.Pt(210, 20)},
		{"outside the image", image.Rect(0, 0, 400, 200), image.Pt(200, 200), media.FocalPoint{X: -1, Y: 2}, image.Pt(0, 0)},
	}

	for _, c := range cases {
		window := c.focus.Window(c.bounds, c.size)
		if window.Min != c.want || window.Size() != c.size {
			t.Errorf("%s: got %v, want %v of size %v", c.name, window, c.want, c.size)
		}
	}
}

// TestFocalPointAlwaysVisible checks for many images, crops and focal points
// that the crop stays inside the image and shows the focal point, centred
// when the image allows it.
func TestFocalPointAlwaysVisible(t *testing.T) {
	sizes := []image.Point{{4000, 3000}, {3000, 4000}, {10000, 50}, {50, 10000}, {1, 1}, {2, 7}}
	boxes := []image.Point{{1, 1}, {16, 9}, {9, 16}, {800, 100}, {4, 3}}
	fractions := []float64{0, 0.001, 0.1, 0.25, 0.5, 0.75, 0.9, 0.999, 1}

	for _, size := range sizes {
		bounds := image.Rectangle{Max: size}

		for _, box := range boxes {
			_, part := media.Variant{Width: box.X, Height: box.Y, Crop: true}.Fit(size.X, size.Y)

			for _, x := range fractions {
				for _, y := range fractions {
					window := media.FocalPoint{X: x, Y: y}.Window(bounds, part)
					focal := image.Pt(min(int(x*float64(size.X)), size.X-1), min(int(y*float64(size.Y)), size.Y-1))

					if !window.In(bounds) || !focal.In(window) {
						t.Fatalf("%v crop %v of %v around (%v, %v): window %v misses %v", box, part, size, x, y, window, focal)
					}

					// Unless the window touches an edge, the focal point is in its middle.
					middle := window.Min.X + part.X/2
					touches := window.Min.X == 0 || window.Max.X == size.X

					if !touches && abs(middle-int(x*float64(size.X))) > 1 {
						t.Fatalf("%v crop %v of %v around x %v: window %v is not centred", box, part, size, x, window)
					}
				}
			}
		}
	}
}

func abs(value int) int {
	if value < 0 {
		return -value
	}

	return value
}

// TestObjectPosition checks the object-position of a crop by the browser,
// and its class of focal-points.css, rounded to steps of 5%.
func TestObjectPosition(t *testing.T) {
	cases := []struct {
		name          string
		focus         media.FocalPoint
		width, height int
		box           image.Point
		want          string
		class         string
	}{
		{"no box", media.FocalPoint{X: 0.25, Y: 0.75}, 400, 200, image.Point{}, "25% 75%", "fp-25-75"},
		{"no size", media.FocalPoint{X: 0.333, Y: 1}, 0, 0, image.Pt(1, 1), "33.3% 100%", "fp-35-100"},
		{"centre", media.Centre, 400, 200, image.Pt(1, 1), "50% 50%", "fp-50-50"},
		{"left of centre", media.FocalPoint{X: 0.3, Y: 0.5}, 400, 200, image.Pt(1, 1), "10% 50%", "fp-10-50"},
		{"left edge", media.FocalPoint{X: 0.1, Y: 0.9}, 400, 200, image.Pt(1, 1), "0% 50%", "fp-0-50"},
		{"right edge", media.FocalPoint{X: 1, Y: 0}, 400, 200, image.Pt(1, 1), "100% 50%", "fp-100-50"},
		{"wide box on a tall image", media.FocalPoint{X: 0, Y: 0.25}, 200, 400, image.Pt(16, 9), "50% 15.3%", "fp-50-15"},
		{"half a step rounds up", media.FocalPoint{X: 0.125, Y: 0.875}, 400, 200, image.Point{}, "12.5% 87.5%", "fp-15-90"},
		{"just under half a step", media.FocalPoint{X: 0.124, Y: 0.976}, 400, 200, image.Point{}, "12.4% 97.6%", "fp-10-100"},
	}

	for _, c := range cases {
		if got := c.focus.ObjectPosition(c.width, c.height, c.box); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}

		if got := c.focus.PositionClass(c.width, c.height, c.box); got != c.class {
			t.Errorf("%s: class %q, want %q", c.name, got, c.class)
		}
	}
}

// split is a PNG of width × height: red on the left of x, blue from x on.
func split(t *testing.T, width, height, x int) image.Image {
	t.Helper()

	picture := image.NewRGBA(image.Rect(0, 0, width, height))

	for py := range height {
		for px := range width {
			fill := color.RGBA{R: 255, A: 255}
			if px >= x {
				fill = color.RGBA{B: 255, A: 255}
			}

			picture.Set(px, py, fill)
		}
	}

	return picture
}

// colours reports whether every pixel of a decoded PNG is more red than
// blue, and whether every one is more blue than red.
func colours(t *testing.T, data []byte) (red, blue bool) {
	t.Helper()

	picture, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	red, blue = true, true
	bounds := picture.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, _, b, _ := picture.At(x, y).RGBA()
			red = red && r > b
			blue = blue && b > r
		}
	}

	return red, blue
}

func TestResizeAroundFocalPoint(t *testing.T) {
	var buffer bytes.Buffer

	if err := png.Encode(&buffer, split(t, 400, 200, 200)); err != nil {
		t.Fatal(err)
	}

	original := buffer.Bytes()

	for _, c := range []struct {
		focus     *media.FocalPoint
		red, blue bool
	}{
		{&media.FocalPoint{X: 0.05, Y: 0.5}, true, false},
		{&media.FocalPoint{X: 0.95, Y: 0.5}, false, true},
		{nil, false, false},
	} {
		out, err := media.Resize(original, media.Variant{Width: 50, Height: 50, Crop: true, Format: "png", Focus: c.focus})
		if err != nil {
			t.Fatal(err)
		}

		if red, blue := colours(t, out); red != c.red || blue != c.blue {
			t.Fatalf("focus %v: red %v, blue %v", c.focus, red, blue)
		}
	}

	// Orientation 6 turns the stored 80 × 40 photo a quarter clockwise: its
	// red left half shows on top. The focal point is placed on what editors
	// see.
	turned := exifJPEG(t, split(t, 80, 40, 40), 6)

	top, err := media.Resize(
		turned,
		media.Variant{Width: 20, Height: 20, Crop: true, Format: "png", Focus: &media.FocalPoint{X: 0.5, Y: 0}},
	)
	if err != nil {
		t.Fatal(err)
	}

	bottom, err := media.Resize(
		turned,
		media.Variant{Width: 20, Height: 20, Crop: true, Format: "png", Focus: &media.FocalPoint{X: 0.5, Y: 1}},
	)
	if err != nil {
		t.Fatal(err)
	}

	if red, _ := colours(t, top); !red {
		t.Fatal("the top of the turned photo is not red")
	}

	if _, blue := colours(t, bottom); !blue {
		t.Fatal("the bottom of the turned photo is not blue")
	}

	// A focal point belongs to crops only.
	if _, err := media.Resize(
		original,
		media.Variant{Width: 50, Focus: &media.FocalPoint{X: 1, Y: 1}},
	); !errors.Is(
		err,
		media.ErrVariant,
	) {
		t.Fatalf("focal point without crop: %v", err)
	}
}

func TestFocalPoint(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		dir := t.TempDir()

		storage, err := media.NewFilesystem(dir, "/media")
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = storage.Close() })

		library := media.New(database, storage)
		library.SetSigningKey([]byte("test key"))
		handler := serve(library)

		asset, err := library.Upload(ctx, "photo.png", bytes.NewReader(photo(t, 400, 200)))
		if err != nil {
			t.Fatal(err)
		}

		stored, err := library.Get(ctx, asset.ID)
		if err != nil || stored.FocalPoint() != media.Centre {
			t.Fatalf("new image: %v %v", stored.FocalPoint(), err)
		}

		crop, _ := media.ParseVariant("100x100 crop")
		resize, _ := media.ParseVariant("100")

		centred, _ := library.VariantURL(ctx, asset.ID, crop)
		resized, _ := library.VariantURL(ctx, asset.ID, resize)

		if !strings.HasSuffix(centred, asset.ID+"_100x100c.webp") {
			t.Fatalf("centred crop: %s", centred)
		}

		for _, link := range []string{centred, resized} {
			if response := get(handler, link); response.Code != 200 {
				t.Fatalf("%s: %d", link, response.Code)
			}
		}

		// A new focal point gives crops a new URL and deletes the old crops;
		// other variants stay.
		point := media.FocalPoint{X: 0.25, Y: 0.7504}
		if err := library.SetFocalPoint(ctx, asset.ID, point); err != nil {
			t.Fatal(err)
		}

		stored, err = library.Get(ctx, asset.ID)
		if err != nil || stored.FocalPoint() != (media.FocalPoint{X: 0.25, Y: 0.75}) {
			t.Fatalf("stored focal point: %v %v", stored.FocalPoint(), err)
		}

		if got := library.FocalPoint(ctx, library.URL(ctx, asset.ID)); got != stored.FocalPoint() {
			t.Fatalf("focal point by URL: %v", got)
		}

		focused, _ := library.VariantURL(ctx, asset.ID, crop)
		again, _ := library.VariantURL(ctx, asset.ID, resize)

		if !strings.HasSuffix(focused, asset.ID+"_100x100c-250-750.webp") || again != resized {
			t.Fatalf("after the change: crop %s, resize %s", focused, again)
		}

		if _, err := os.Stat(filepath.Join(dir, asset.ID+"_100x100c.webp")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("old crop remains: %v", err)
		}

		if _, err := os.Stat(filepath.Join(dir, asset.ID+"_100x0.webp")); err != nil {
			t.Fatalf("resize was deleted: %v", err)
		}

		var keys []string
		if err := database.Select(ctx, &keys, `SELECT storage_key FROM wt_media_variant ORDER BY storage_key`); err != nil {
			t.Fatal(err)
		}

		if len(keys) != 1 || keys[0] != asset.ID+"_100x0.webp" {
			t.Fatalf("variant rows: %v", keys)
		}

		if response := get(handler, focused); response.Code != 200 {
			t.Fatalf("focused crop: %d", response.Code)
		}

		// The same point again keeps the crops made for it.
		if err := library.SetFocalPoint(ctx, asset.ID, point); err != nil {
			t.Fatal(err)
		}

		if _, err := os.Stat(filepath.Join(dir, asset.ID+"_100x100c-250-750.webp")); err != nil {
			t.Fatalf("crop for the same point was deleted: %v", err)
		}

		// Tampered focal points are refused, also the centre spelled out.
		for _, tampered := range []string{
			strings.Replace(focused, "c-250-750", "c-900-750", 1),
			strings.Replace(focused, "c-250-750", "c-500-500", 1),
			strings.Replace(focused, "c-250-750", "c-1250-750", 1),
		} {
			if response := get(handler, tampered); response.Code != 404 {
				t.Fatalf("%s: want 404, got %d", tampered, response.Code)
			}
		}

		// CSS crops follow it too.
		if got := library.ObjectPosition(ctx, asset.ID, ""); got != "25% 75%" {
			t.Fatalf("object position: %s", got)
		}

		if got := library.ObjectPosition(ctx, asset.ID, "1:1"); got != "0% 50%" {
			t.Fatalf("object position of a square: %s", got)
		}

		if got := library.ObjectPosition(ctx, "unknown", "1:1"); got != "50% 50%" {
			t.Fatalf("object position of an unknown image: %s", got)
		}

		// Only images in the library have a focal point, inside the image.
		document, err := library.Upload(ctx, "notes.txt", strings.NewReader("plain text"))
		if err != nil {
			t.Fatal(err)
		}

		for id, want := range map[string]error{document.ID: media.ErrInvalid, db.NewID(): media.ErrNotFound} {
			if err := library.SetFocalPoint(ctx, id, media.Centre); !errors.Is(err, want) {
				t.Fatalf("%s: want %v, got %v", id, want, err)
			}
		}

		for _, outside := range []media.FocalPoint{{X: -0.1, Y: 0.5}, {X: 0.5, Y: 1.01}} {
			if err := library.SetFocalPoint(ctx, asset.ID, outside); !errors.Is(err, media.ErrInvalid) {
				t.Fatalf("%v: %v", outside, err)
			}
		}

		// Photos turned by EXIF have the size they are shown with.
		turned, err := library.Upload(ctx, "turned.jpg", bytes.NewReader(withOrientation(t, 40, 20)))
		if err != nil || turned.Width != 20 || turned.Height != 40 {
			t.Fatalf("turned photo: %dx%d %v", turned.Width, turned.Height, err)
		}
	})
}
