package mediaadmin_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/sitetest"
)

// TestUploadHelp checks that the library names the accepted files and the
// size limit of media.Upload.
func TestUploadHelp(t *testing.T) {
	s := sitetest.Start(t)

	s.Expect("/admin/media", http.StatusOK,
		"Images (JPEG, PNG or GIF), PDF, plain text, CSV, ZIP, Word, Excel, PowerPoint and OpenDocument files",
		"up to 10 MB",
		"WebP and SVG are not accepted.",
	)
}

// TestUpload checks the upload form: a file is stored and opened, a missing,
// unaccepted or too large file is refused at the field, and a file the
// library refuses for its content shows the reason as a toast.
func TestUpload(t *testing.T) {
	s := sitetest.Start(t)

	s.Expect("/admin/media", http.StatusOK, `enctype="multipart/form-data"`, `type="file"`, `name="file"`, `accept=".jpg,`)

	status, body := s.PostFiles("/admin/media", "/admin/media", nil, sitetest.File{
		Field: "file", Name: "photo.png", Data: sitetest.PNG(t, 4, 3),
	})
	if status != http.StatusOK || !strings.Contains(body, "photo.png") || !strings.Contains(body, `name="AltText"`) {
		t.Fatalf("upload: %d\n%s", status, body)
	}

	refusals := map[string]sitetest.File{
		"Choose a file to upload.": {},
		"This file type is not supported.": {
			Field: "file", Name: "virus.exe", Data: []byte("MZ"),
		},
		"This file is too large.": {
			Field: "file", Name: "big.txt", Data: bytes.Repeat([]byte("x"), 10<<20+1),
		},
	}

	for message, file := range refusals {
		var files []sitetest.File
		if file.Field != "" {
			files = append(files, file)
		}

		status, body := s.PostFiles("/admin/media", "/admin/media", nil, files...)
		if status != http.StatusUnprocessableEntity || !strings.Contains(body, message) ||
			!strings.Contains(body, `aria-invalid="true"`) {
			t.Fatalf("%s: %d\n%s", message, status, body)
		}

		if sitetest.HasToast(body, "error", message) {
			t.Fatalf("%s: the refusal is a toast instead of at the field", message)
		}
	}

	// The library checks the content: SVG is refused whatever its name.
	unsupported := "This file type is not supported. Upload JPEG, PNG or GIF images, PDF, plain text, CSV, ZIP, Word, " +
		"Excel, PowerPoint or OpenDocument files; WebP and SVG are not accepted."

	status, body = s.PostFiles("/admin/media", "/admin/media", nil, sitetest.File{
		Field: "file", Name: "logo.txt", Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`),
	})
	if status != http.StatusUnprocessableEntity || !sitetest.HasToast(body, "error", unsupported) {
		t.Fatalf("svg: %d\n%s", status, body)
	}
}

// upload adds a width × height PNG to the site's media library.
func upload(t *testing.T, s *sitetest.Site, width, height int) media.Asset {
	t.Helper()

	var buffer bytes.Buffer

	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}

	asset, err := s.App.Media.Upload(context.Background(), "photo.png", &buffer)
	if err != nil {
		t.Fatal(err)
	}

	return asset
}

// TestFocalPointEditor checks that an image's screen shows its focal point
// with crop previews and a form that works without JavaScript, which saves
// the point and refuses points outside the image. Nothing has a style
// attribute: the previews have classes, and the script places the marker
// from the number fields.
func TestFocalPointEditor(t *testing.T) {
	s := sitetest.Start(t)
	asset := upload(t, s, 400, 200)
	page := "/admin/media/" + asset.ID + "?locale=en"

	s.Expect(page, http.StatusOK,
		`data-focal-editor data-width="400" data-height="200"`,
		`data-x="50" data-y="50"`,
		`<div class="focal-crop focal-crop-16-9">`,
		`<img class="fp-50-50" src="`,
		`<link rel="stylesheet" href="/admin/assets/focal-points.css?v=`,
		`<script src="/admin/assets/focalpoint.js?v=`,
		`name="FocalX"`, `name="FocalY"`,
		"From the left (%)",
		"Save focal point",
	)

	status, body := s.PostStatus(page, "/admin/media/"+asset.ID+"/focus?locale=en", url.Values{
		"FocalX": {"10"},
		"FocalY": {"80.5"},
	})
	if status != http.StatusOK || !strings.Contains(body, `data-x="10" data-y="80.5"`) || strings.Contains(body, "style=") {
		t.Fatalf("save focal point: %d\n%s", status, body)
	}

	stored, err := s.App.Media.Get(context.Background(), asset.ID)
	if err != nil || stored.FocalPoint() != (media.FocalPoint{X: 0.1, Y: 0.805}) {
		t.Fatalf("stored focal point: %v %v", stored.FocalPoint(), err)
	}

	// The square preview shows the left of the wide image.
	s.Expect(page, http.StatusOK, `<img class="fp-0-50" src="`)

	status, body = s.PostStatus(page, "/admin/media/"+asset.ID+"/focus?locale=en", url.Values{
		"FocalX": {"150"},
		"FocalY": {"50"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, `data-x="10" data-y="80.5"`) {
		t.Fatalf("focal point outside the image: %d\n%s", status, body)
	}

	if stored, _ := s.App.Media.Get(context.Background(), asset.ID); stored.FocalX != 0.1 {
		t.Fatalf("a refused point was stored: %v", stored.FocalPoint())
	}

	// Other files have no focal point.
	document, err := s.App.Media.Upload(context.Background(), "notes.txt", strings.NewReader("plain text"))
	if err != nil {
		t.Fatal(err)
	}

	s.ExpectMissing("/admin/media/"+document.ID, "data-focal-editor")

	status, _ = s.PostStatus("/admin/media/"+document.ID, "/admin/media/"+document.ID+"/focus", url.Values{
		"FocalX": {"10"},
		"FocalY": {"10"},
	})
	if status != http.StatusNotFound {
		t.Fatalf("focal point of a document: %d", status)
	}

	s.Expect("/admin/assets/focalpoint.js", http.StatusOK, "data-focal-marker")
}

// TestFocalPointEditorInDutch checks the editor's Dutch texts.
func TestFocalPointEditorInDutch(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{
		Configure: func(config *webthings.Config) {
			config.AdminLanguage = "nl"
		},
	})
	asset := upload(t, s, 300, 300)

	s.Expect("/admin/media/"+asset.ID, http.StatusOK,
		`<h2 class="text-xl font-semibold mb-2">Focuspunt</h2>`,
		"Zet het focuspunt op wat in de afbeelding het belangrijkst is",
		`data-value-text="{x} vanaf links, {y} vanaf boven"`,
		"Vanaf links (%)",
		"Vanaf boven (%)",
		"Focuspunt opslaan",
	)
}

// TestReadOnlyLibrary checks that an editor who may only view media gets
// no upload form and the texts of a file read-only.
func TestReadOnlyLibrary(t *testing.T) {
	s := sitetest.Start(t)
	asset := upload(t, s, 40, 30)

	group := s.Submit("/admin/groups", url.Values{"Name": {"Readers"}})
	s.Post(group, group, url.Values{"name": {"Readers"}, "allow": {"media:read"}})

	account := s.Submit("/admin/accounts", url.Values{
		"Email":    {"reader@example.test"},
		"Password": {"reader long passphrase"},
		"Role":     {"editor"},
	})
	s.Post(account, account+"/groups", url.Values{"group": {group[strings.LastIndex(group, "/")+1:]}})

	reader := s.As("reader@example.test", "reader long passphrase")

	_, body := reader.Get("/admin/media")
	if strings.Contains(body, `enctype="multipart/form-data"`) || !strings.Contains(body, "photo.png") {
		t.Fatalf("a reader gets the upload form:\n%s", body)
	}

	disabled := regexp.MustCompile(`<input[^>]*name="Title"[^>]*\sdisabled\s`)

	_, body = reader.Get("/admin/media/" + asset.ID)
	if !disabled.MatchString(body) || strings.Contains(body, ">Save</button>") || strings.Contains(body, "/focus") {
		t.Fatalf("a reader can change the file:\n%s", body)
	}

	if _, body := s.Get("/admin/media"); !strings.Contains(body, `enctype="multipart/form-data"`) {
		t.Fatal("the administrator gets no upload form")
	}
}
