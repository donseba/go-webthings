package media

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"

	"github.com/donseba/go-router"
)

// The addresses media makes without fmt, a new HMAC or the router for each
// one are checked against the way it made them before: byte for byte the
// same.

// referenceKey is Variant.key as it was made with fmt.
func referenceKey(v Variant, assetID string) string {
	crop := ""

	if v.Crop {
		crop = "c"

		if focus := v.focus(); !focus.isCentre() {
			crop += fmt.Sprintf("-%d-%d", thousandths(focus.X), thousandths(focus.Y))
		}
	}

	return fmt.Sprintf("%s_%dx%d%s.%s", assetID, v.Width, v.Height, crop, v.Format)
}

// referenceSignature is the signature of key made with a new HMAC.
func referenceSignature(signingKey []byte, key string) string {
	mac := hmac.New(sha256.New, signingKey)
	mac.Write([]byte(key))

	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:12])
}

// referenceAssetID is assetID as it was made with regular expressions only.
func referenceAssetID(ref string) (string, bool) {
	pattern := regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

	ref = strings.TrimSpace(ref)

	if end := strings.IndexAny(ref, "?#"); end >= 0 {
		ref = ref[:end]
	}

	name := ref[strings.LastIndexByte(ref, '/')+1:]
	if match := variantKey.FindStringSubmatch(name); match != nil {
		return match[1], true
	}

	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		name = name[:dot]
	}

	if !pattern.MatchString(name) {
		return "", false
	}

	return strings.ToLower(name), true
}

func TestVariantKeyAsBefore(t *testing.T) {
	id := "0193a5b2-7c4d-7e8f-9a0b-1c2d3e4f5a6b"

	for _, variant := range []Variant{
		{Width: 800, Format: "webp"},
		{Height: 600, Format: "jpg"},
		{Width: 4000, Height: 4000, Format: "png"},
		{Width: 120, Height: 120, Crop: true, Format: "png"},
		{Width: 1200, Height: 630, Crop: true, Format: "jpg", Focus: &FocalPoint{X: 0.25, Y: 0.8}},
		{Width: 32, Height: 32, Crop: true, Format: "png", Focus: &FocalPoint{X: 0.5, Y: 0.5}},
		{Width: 32, Height: 32, Crop: true, Format: "png", Focus: &FocalPoint{X: 0.0004, Y: 1}},
		{Width: 1, Height: 1, Crop: true, Format: "webp", Focus: &FocalPoint{X: 0.9996, Y: 0}},
	} {
		if got, want := variant.key(id), referenceKey(variant, id); got != want {
			t.Fatalf("key %q, want %q", got, want)
		}
	}
}

func TestSignatureAsBefore(t *testing.T) {
	signingKey := []byte("a key of the site")
	signer := newSigner(signingKey)

	var group sync.WaitGroup

	for worker := range 8 {
		group.Go(func() {
			for i := range 200 {
				key := fmt.Sprintf("0193a5b2-7c4d-7e8f-9a0b-1c2d3e4f5a6b_%dx%d.webp", worker, i)
				if got, want := signer.sign(key), referenceSignature(signingKey, key); got != want {
					t.Errorf("signature of %s: %q, want %q", key, got, want)

					return
				}
			}
		})
	}

	group.Wait()
}

func TestAssetIDAsBefore(t *testing.T) {
	id := "0193a5b2-7c4d-7e8f-9a0b-1c2d3e4f5a6b"

	for _, ref := range []string{
		id,
		strings.ToUpper(id),
		" " + id + " ",
		"/media/" + id + ".png",
		"/media/" + id + ".png?x=1#top",
		"https://cdn.example.com/files/" + strings.ToUpper(id) + ".jpg",
		"/media/v/sig/" + id + "_800x0.webp",
		"/media/v/sig/" + id + "_120x120c-250-800.png",
		"/media/v/sig/" + id + "_800x0.gif",
		"/media/v/sig/" + id + "_garbage.webp",
		id[:35],
		id + "0",
		strings.Replace(id, "-", "_", 1),
		strings.Replace(id, "a", "g", 1),
		"",
		"logo.svg",
		"/theme/default/logo.svg",
	} {
		gotID, gotOK := assetID(ref)
		wantID, wantOK := referenceAssetID(ref)

		if gotID != wantID || gotOK != wantOK {
			t.Fatalf("assetID(%q) = %q %v, want %q %v", ref, gotID, gotOK, wantID, wantOK)
		}
	}
}

// TestURLsAsBefore checks the addresses of an asset and its variants
// against those the router gives, also when they are kept, and that a new
// focal point gives crops new ones.
func TestURLsAsBefore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", Schema()); err != nil {
			t.Fatal(err)
		}

		storage, err := NewFilesystem(t.TempDir(), "")
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = storage.Close() })

		signingKey := []byte("test key")

		library := New(database, storage)
		library.SetSigningKey(signingKey)

		site := router.NewSite()
		routes := router.NewSite()

		site.Mount("/media", routes, router.NamePrefix("media."))
		library.RegisterRoutes(routes)

		var picture bytes.Buffer

		if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 1600, 900))); err != nil {
			t.Fatal(err)
		}

		asset, err := library.Upload(ctx, "photo.png", &picture)
		if err != nil {
			t.Fatal(err)
		}

		own, err := routes.RoutePathArgs("asset", "key", asset.StorageKey)
		if err != nil {
			t.Fatal(err)
		}

		for range 2 {
			if got := library.URL(ctx, asset.ID); got != own {
				t.Fatalf("URL %q, want %q", got, own)
			}
		}

		check := func(variant Variant, focus FocalPoint) {
			t.Helper()

			want := variant
			if want.Crop && want.Focus == nil {
				want.Focus = &focus
			}

			key := referenceKey(want, asset.ID)

			address, err := routes.RoutePathArgs("variant", "signature", referenceSignature(signingKey, key), "key", key)
			if err != nil {
				t.Fatal(err)
			}

			// The second address is the one kept.
			for range 2 {
				got, err := library.VariantURL(ctx, asset.ID, variant)
				if err != nil || got != address {
					t.Fatalf("variant %+v: %q %v, want %q", variant, got, err, address)
				}
			}
		}

		variants := []Variant{
			{Width: 800, Format: "webp"},
			{Width: 400, Format: "jpg"},
			{Width: 32, Height: 32, Crop: true, Format: "png"},
			{Width: 1200, Height: 630, Crop: true, Format: "jpg"},
			{Width: 300, Height: 300, Crop: true, Format: "webp", Focus: &FocalPoint{X: 0.1, Y: 0.9}},
		}

		for _, variant := range variants {
			check(variant, Centre)
		}

		// A new focal point is a new version of the asset: crops get new
		// addresses.
		focus := FocalPoint{X: 0.3, Y: 0.7}
		if err := library.SetFocalPoint(ctx, asset.ID, focus); err != nil {
			t.Fatal(err)
		}

		for _, variant := range variants {
			check(variant, focus)
		}

		// The Image of a page is made of the same addresses.
		shown := library.Image(ctx, asset.ID, ImageWidth)
		if again := library.Image(ctx, asset.ID, ImageWidth); again != shown {
			t.Fatalf("image %+v, kept %+v", shown, again)
		}

		srcset, err := library.Srcset(ctx, asset.ID, DefaultVariantFormat, srcsetWidths(ImageWidth)...)
		if err != nil || shown.Srcset != srcset || !strings.HasPrefix(shown.URL, "/media/v/") || shown.OriginalURL != own {
			t.Fatalf("image %+v, srcset %q %v", shown, srcset, err)
		}
	})
}

// benchLibrary is a library with one image and its routes, for the
// benchmarks of addresses.
func benchLibrary(b *testing.B) (*Service, *router.Router, string) {
	b.Helper()

	ctx := context.Background()

	database, err := db.Open(ctx, "sqlite", "file:"+filepath.Join(b.TempDir(), "bench.sqlite"))
	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() { _ = database.Close() })

	if err := database.ApplySchema(ctx, "media", Schema()); err != nil {
		b.Fatal(err)
	}

	storage, err := NewFilesystem(b.TempDir(), "")
	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() { _ = storage.Close() })

	library := New(database, storage)
	site := router.NewSite()
	routes := router.NewSite()

	site.Mount("/media", routes, router.NamePrefix("media."))
	library.RegisterRoutes(routes)

	var picture bytes.Buffer

	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 1600, 900))); err != nil {
		b.Fatal(err)
	}

	asset, err := library.Upload(ctx, "photo.png", &picture)
	if err != nil {
		b.Fatal(err)
	}

	return library, routes, asset.ID
}

// BenchmarkVariantURL is the address of a crop the library made before.
func BenchmarkVariantURL(b *testing.B) {
	library, _, id := benchLibrary(b)
	ctx := context.Background()
	variant := Variant{Width: 1200, Height: 630, Crop: true, Format: "jpg"}

	for b.Loop() {
		if _, err := library.VariantURL(ctx, id, variant); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVariantURLMade makes the address of a crop each time, with the
// kept HMACs and the address of the route.
func BenchmarkVariantURLMade(b *testing.B) {
	library, _, id := benchLibrary(b)
	variant := Variant{Width: 1200, Height: 630, Crop: true, Format: "jpg", Focus: &FocalPoint{X: 0.3, Y: 0.7}}

	for b.Loop() {
		key := variant.key(id)

		if _, err := library.variantPath(library.sign(key), key); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVariantURLReference makes the address of a crop as it was made
// before: with fmt, a new HMAC and the router.
func BenchmarkVariantURLReference(b *testing.B) {
	_, routes, id := benchLibrary(b)
	signingKey := []byte("a key")
	variant := Variant{Width: 1200, Height: 630, Crop: true, Format: "jpg", Focus: &FocalPoint{X: 0.3, Y: 0.7}}

	for b.Loop() {
		key := referenceKey(variant, id)

		if _, err := routes.RoutePathArgs("variant", "signature", referenceSignature(signingKey, key), "key", key); err != nil {
			b.Fatal(err)
		}
	}
}
