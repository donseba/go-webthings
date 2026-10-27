package gallery_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/modules/gallery"
	"github.com/donseba/go-webthings/sitetest"
)

func TestGallery(t *testing.T) {
	s := sitetest.StartWith(
		t,
		sitetest.Options{Templates: "testdata/templates"},
		gallery.Module("photos", "Photos"),
		gallery.Module("unlisted", "Unlisted"),
	)
	ctx := context.Background()

	// Different sizes give different files.
	first := s.Upload("first.png", sitetest.PNG(t, 3, 2))
	second := s.Upload("second.png", sitetest.PNG(t, 4, 2))

	err := s.App.Media.SaveTranslation(ctx, second.ID, media.Translation{
		Locale:  "en",
		Title:   "Sunset",
		AltText: "Sun over the sea",
	})
	if err != nil {
		t.Fatal(err)
	}

	album := s.Submit("/admin/photos/new?locale=en", url.Values{
		"Title":       {"Holiday"},
		"Slug":        {"holiday"},
		"Description": {"A *sunny* week"},
		"Images":      {first.ID, second.ID},
	})
	s.Publish(album + "?locale=en")

	// Without a web address, the album gets one made from its title.
	secret := s.Submit("/admin/photos/new?locale=en", url.Values{
		"Title": {"Secret"},
	})
	s.Expect(secret+"?locale=en", http.StatusOK, `value="secret"`)

	// Pages show variants; the uploads are only offered for download.
	firstURL := s.App.Media.Image(ctx, first.ID, media.ImageWidth).URL
	secondURL := s.App.Media.Image(ctx, second.ID, media.ImageWidth).URL
	secondLarge := s.App.Media.Image(ctx, second.ID, media.LargeImageWidth).URL
	secondOriginal := s.App.Media.URL(ctx, second.ID)

	if !strings.HasSuffix(firstURL, first.ID+"_3x0.webp") || !strings.HasSuffix(secondLarge, second.ID+"_4x0.webp") {
		t.Fatalf("variants: %s, %s", firstURL, secondLarge)
	}

	s.Mount("photos", "Photos", "photos")
	s.Expect("/en/photos", http.StatusOK, "<h1>Photos</h1>", `href="/en/photos/holiday"`, `src="`+firstURL+`"`)
	s.ExpectMissing("/en/photos", "secret")
	s.Expect("/en/photos/holiday", http.StatusOK,
		"<em>sunny</em>",
		`hreflang="en"`,
		`src="`+firstURL+`"`,
		`src="`+secondURL+`" srcset="`+secondURL+` 4w" width="4" height="2" data-large="`+secondLarge+`"`,
		`alt="Sun over the sea" title="Sunset"><a href="`+secondOriginal+`" download>`,
	)
	s.ExpectMissing("/en/photos/holiday", `src="`+secondOriginal+`"`)
	s.Expect("/en/photos/secret", http.StatusNotFound)
	s.Expect("/nl/photos", http.StatusNotFound)

	if err := s.App.Media.Delete(ctx, second.ID); !errors.Is(err, media.ErrInUse) {
		t.Fatalf("delete used image: %v", err)
	}

	// The media file page refuses with a toast.
	asset := "/admin/media/" + second.ID

	code, page := s.PostStatus(asset, asset+"/delete", url.Values{})
	if code != http.StatusConflict ||
		!sitetest.HasToast(page, "error", "This file is used by content, perhaps in the trash, and cannot be deleted.") {
		t.Fatalf("delete used image in the admin: %d\n%s", code, page)
	}

	albums, err := gallery.Of(s.App, "photos").Albums(ctx, "en")
	if err != nil || len(albums) != 1 || albums[0].URL != "/en/photos/holiday" || albums[0].ImageCount != 2 {
		t.Fatalf("albums: %+v %v", albums, err)
	}

	s.Expect("/sitemap.xml", http.StatusOK, "/en/photos/holiday</loc>")
	s.ExpectMissing("/sitemap.xml", "/en/photos/secret")

	// The list title is the published page's, not a newer draft's.
	s.DraftPage("Photos", "Photos (draft)", "photos")
	s.Expect("/en/photos", http.StatusOK, "<h1>Photos</h1>")
	s.ExpectMissing("/en/photos", "Photos (draft)")

	// Images are put in order on the image order screen of the album.
	albumID := strings.TrimPrefix(album, "/admin/photos/")
	images := "/admin/photos/" + albumID + "/images?locale=en"

	s.Expect("/admin/photos?locale=en", http.StatusOK, `href="/admin/photos/images"`)
	s.Expect("/admin/photos/images?locale=en", http.StatusOK, `<a href="`+images+`">Holiday</a>`)
	s.Expect(images, http.StatusOK, "first.png", "Sunset (second.png)", "/images/"+second.ID+"/up?locale=en")
	s.ExpectMissing(images, "/images/"+first.ID+"/up")

	s.Post(images, "/admin/photos/"+albumID+"/images/"+second.ID+"/up?locale=en", url.Values{})

	_, body := s.Get("/en/photos/holiday")
	if strings.Index(body, secondURL) > strings.Index(body, firstURL) {
		t.Fatalf("second image is not first:\n%s", body)
	}

	// Saving the album keeps the order.
	s.Submit(album+"?locale=en", url.Values{
		"Title":  {"Holiday"},
		"Slug":   {"holiday"},
		"Images": {first.ID, second.ID},
	})

	_, body = s.Get("/en/photos/holiday")
	if strings.Index(body, secondURL) > strings.Index(body, firstURL) {
		t.Fatalf("saving changed the order:\n%s", body)
	}

	// A gallery on no page has albums without addresses.
	other := s.Submit("/admin/unlisted/new?locale=en", url.Values{
		"Title":  {"Elsewhere"},
		"Slug":   {"elsewhere"},
		"Images": {first.ID},
	})
	s.Publish(other + "?locale=en")

	unlisted, err := gallery.Of(s.App, "unlisted").Albums(ctx, "en")
	if err != nil || len(unlisted) != 1 || unlisted[0].URL != "" {
		t.Fatalf("unmounted albums: %+v %v", unlisted, err)
	}

	one, err := gallery.Of(s.App, "unlisted").Album(ctx, "en", "elsewhere")
	if err != nil || one.URL != "" || len(one.Images) != 1 {
		t.Fatalf("unmounted album: %+v %v", one, err)
	}
}
