package mediaadmin_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/media/mediaadmin"
	"github.com/donseba/go-webthings/sitetest"
)

// pngOf is a small PNG; shade makes each one a different file.
func pngOf(t *testing.T, shade int) *bytes.Reader {
	t.Helper()

	picture := image.NewRGBA(image.Rect(0, 0, 4, 3))
	picture.Set(0, 0, color.RGBA{R: uint8(shade), G: uint8(shade >> 8), A: 255})

	var buffer bytes.Buffer

	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}

	return bytes.NewReader(buffer.Bytes())
}

func pickerPage(t *testing.T, s *sitetest.Site, query string) mediaadmin.PickerPage {
	t.Helper()

	response, body := s.Response("/admin/media/picker?" + query)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("picker?%s: status %d", query, response.StatusCode)
	}

	if !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("picker?%s: content type %q", query, response.Header.Get("Content-Type"))
	}

	var page mediaadmin.PickerPage

	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatal(err)
	}

	return page
}

// TestPicker checks the endpoint of the Markdown editor's image picker:
// images only, newest first, by page, searchable by file name, and found by
// ID for images the Markdown refers to.
func TestPicker(t *testing.T) {
	s := sitetest.Start(t)
	ctx := t.Context()

	if page := pickerPage(t, s, "page=1"); len(page.Items) != 0 || page.Next {
		t.Fatalf("empty library: %+v", page)
	}

	var ids []string

	for number := range 25 {
		asset, err := s.App.Media.Upload(ctx, fmt.Sprintf("photo-%02d.png", number), pngOf(t, number+1))
		if err != nil {
			t.Fatal(err)
		}

		ids = append(ids, asset.ID)
	}

	text, err := s.App.Media.Upload(ctx, "notes.txt", strings.NewReader("Plain text, not an image."))
	if err != nil {
		t.Fatal(err)
	}

	first := pickerPage(t, s, "page=1")
	if len(first.Items) != 24 || !first.Next {
		t.Fatalf("first page: %d items, next %v", len(first.Items), first.Next)
	}

	item := first.Items[0]
	if item.Filename == "" || !strings.HasPrefix(item.URL, "/media/") ||
		!strings.HasPrefix(item.Thumb, "/media/v/") || !strings.HasPrefix(item.Preview, "/media/v/") {
		t.Fatalf("item: %+v", item)
	}

	second := pickerPage(t, s, "page=2")
	if len(second.Items) != 1 || second.Next {
		t.Fatalf("second page: %d items, next %v", len(second.Items), second.Next)
	}

	for _, listed := range append(first.Items, second.Items...) {
		if listed.ID == text.ID {
			t.Fatal("the picker lists a file that is not an image")
		}
	}

	found := pickerPage(t, s, "q="+url.QueryEscape("PHOTO-07"))
	if len(found.Items) != 1 || found.Items[0].ID != ids[7] {
		t.Fatalf("search: %+v", found.Items)
	}

	// The search ignores accents and case, like the library's list.
	terrace, err := s.App.Media.Upload(ctx, "Café-terrasse.png", pngOf(t, 100))
	if err != nil {
		t.Fatal(err)
	}

	for _, search := range []string{"cafe terrasse", "CAFÉ", "terrasse café"} {
		found := pickerPage(t, s, "q="+url.QueryEscape(search))
		if len(found.Items) != 1 || found.Items[0].ID != terrace.ID {
			t.Errorf("search %q: %+v", search, found.Items)
		}
	}

	query := url.Values{"id": {ids[3], "00000000-0000-0000-0000-000000000000", text.ID, strings.ToUpper(ids[1])}}

	resolved := pickerPage(t, s, query.Encode())
	if len(resolved.Items) != 2 || resolved.Items[0].ID != ids[3] || resolved.Items[1].ID != ids[1] {
		t.Fatalf("by ID: %+v", resolved.Items)
	}
}

// TestPickerPermission checks that the picker needs the media library's
// read permission and a session.
func TestPickerPermission(t *testing.T) {
	s := sitetest.Start(t)

	group := s.Submit("/admin/groups", url.Values{"Name": {"Writers"}})
	s.Post(group, group, url.Values{"name": {"Writers"}, "allow": {"pages:read", "pages:edit"}})

	account := s.Submit("/admin/accounts", url.Values{
		"Email":    {"writer@example.test"},
		"Password": {"writer long passphrase"},
		"Role":     {"editor"},
	})
	s.Post(account, account+"/groups", url.Values{"group": {group[strings.LastIndex(group, "/")+1:]}})

	writer := s.As("writer@example.test", "writer long passphrase")

	if status, _ := writer.Get("/admin/media/picker"); status != http.StatusForbidden {
		t.Fatalf("editor without media rights: want 403, got %d", status)
	}

	response, _ := s.Visitor().Response("/admin/media/picker")
	if strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		t.Fatal("the picker answers visitors that are not signed in")
	}
}

// TestSearchTranslations checks that the library's list and the editor's
// picker find images by their titles and alternative texts in any language,
// ignoring case and accents, and not only by file name.
func TestSearchTranslations(t *testing.T) {
	s := sitetest.Start(t)
	ctx := t.Context()

	beach, err := s.App.Media.Upload(ctx, "IMG_0001.png", pngOf(t, 1))
	if err != nil {
		t.Fatal(err)
	}

	other, err := s.App.Media.Upload(ctx, "IMG_0002.png", pngOf(t, 2))
	if err != nil {
		t.Fatal(err)
	}

	translations := []media.Translation{
		{Locale: "en", Title: "Summer", AltText: "Children on a sunny beach"},
		{Locale: "nl", Title: "Zomer", AltText: "Kinderen op een zonnig strand bij het café"},
	}

	for _, translation := range translations {
		if err := s.App.Media.SaveTranslation(ctx, beach.ID, translation); err != nil {
			t.Fatal(err)
		}
	}

	for _, search := range []string{"SUNNY beach", "zomer", "strand", "cafe", "Café"} {
		found := pickerPage(t, s, "q="+url.QueryEscape(search))
		if len(found.Items) != 1 || found.Items[0].ID != beach.ID {
			t.Errorf("picker search %q: %+v", search, found.Items)
		}

		library := "/admin/media?q=" + url.QueryEscape(search)
		s.Expect(library, http.StatusOK, "/admin/media/"+beach.ID)
		s.ExpectMissing(library, "/admin/media/"+other.ID)
	}

	if found := pickerPage(t, s, "q=mountain"); len(found.Items) != 0 {
		t.Fatalf("search without a match: %+v", found.Items)
	}
}
