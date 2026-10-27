package newsletter_test

import (
	"bytes"
	"context"
	"image"
	_ "image/jpeg" // Decode the mail's photos.
	"image/png"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/modules/newsletter"
	"github.com/donseba/go-webthings/sitetest"
)

// TestMailingImages checks that mailings show images as copies the size of
// a mail, at absolute addresses: JPEG, or PNG for a PNG with transparency,
// never enlarged, while links to media give the upload.
func TestMailingImages(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{}, newsletter.Module("news", "Newsletter"))

	_, err := newsletter.Of(s.App, "news").Import(context.Background(), []newsletter.Contact{{Email: "ann@example.test"}})
	if err != nil {
		t.Fatal(err)
	}

	photo := s.Upload("photo.jpg", sitetest.JPEG(t, 3000, 1500))
	chart := s.Upload("chart.png", opaquePNG(t, 2000, 1000))
	logo := s.Upload("logo.png", sitetest.PNG(t, 1600, 400))
	small := s.Upload("small.jpg", sitetest.JPEG(t, 500, 250))

	// The library's alt text shows where the Markdown has none.
	alt := media.Translation{Locale: "en", AltText: "A small one"}
	if err := s.App.Media.SaveTranslation(context.Background(), small.ID, alt); err != nil {
		t.Fatal(err)
	}

	body := "![Photo](media:" + photo.ID + ")\n\n![Chart](media:" + chart.ID + ")\n\n" +
		"![Logo](media:" + logo.ID + ")\n\n![](media:" + small.ID + ")\n\n[The photo](media:" + photo.ID + ")"

	mailing := s.Submit("/admin/news/mailings/new", url.Values{"Subject": {"Pictures"}, "Body": {body}})

	sent := len(s.Mail.Sent())
	s.Post(mailing, mailing+"/send", url.Values{})
	finished(t, s, mailing)

	messages := s.Mail.Sent()[sent:]
	if len(messages) != 1 {
		t.Fatalf("messages: %+v", messages)
	}

	html := messages[0].HTML
	img := regexp.MustCompile(`<img src="([^"]+)" alt="([^"]*)" width="(\d+)" height="(\d+)"`)
	sources := img.FindAllStringSubmatch(html, -1)

	if len(sources) != 4 {
		t.Fatalf("images in\n%s", html)
	}

	for index, want := range []struct {
		suffix, format, alt string
		width, height       int
	}{
		{photo.ID + "_1200x0.jpg", "jpeg", "Photo", 1200, 600},
		{chart.ID + "_1200x0.jpg", "jpeg", "Chart", 1200, 600},
		{logo.ID + "_1200x0.png", "png", "Logo", 1200, 300},
		{small.ID + "_500x0.jpg", "jpeg", "A small one", 500, 250},
	} {
		address := sources[index][1]
		if !strings.HasPrefix(address, "http://") || !strings.HasSuffix(address, want.suffix) {
			t.Fatalf("image %d: %s, want an absolute …%s", index, address, want.suffix)
		}

		// The img has the alt text and the size of the copy, so a mail
		// program does not show it larger.
		size := strconv.Itoa(want.width) + "×" + strconv.Itoa(want.height)
		if sources[index][2] != want.alt || sources[index][3]+"×"+sources[index][4] != size {
			t.Fatalf("image %d: %q", index, sources[index][0])
		}

		format, width, height := served(t, s, address)
		if format != want.format || width != want.width || height != want.height {
			t.Fatalf("%s is a %d × %d %s", address, width, height, format)
		}

		// The text part links to the same copy, with the alt text.
		if !strings.Contains(messages[0].Text, "["+want.alt+"]("+address+")") {
			t.Fatalf("image %d in the text part:\n%s", index, messages[0].Text)
		}
	}

	if strings.Contains(messages[0].Text, "media:") {
		t.Fatalf("media references in the text part:\n%s", messages[0].Text)
	}

	// A link to a file gives the upload itself.
	original := s.App.Media.URL(context.Background(), photo.ID)
	if !strings.Contains(html, `<a href="http://`) || !strings.Contains(html, original+`">The photo</a>`) {
		t.Fatalf("media link in\n%s", html)
	}
}

// TestMailingLinks checks that the links of a mailing lead to the site in
// both parts of the mail: paths on the site and relative links, which lead
// from the home page of the subscriber's language, become absolute, while
// fragments and links to other sites stay as they are.
func TestMailingLinks(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{}, newsletter.Module("news", "Newsletter"))

	_, err := newsletter.Of(s.App, "news").
		Import(context.Background(), []newsletter.Contact{{Email: "ann@example.test", Locale: "en"}})
	if err != nil {
		t.Fatal(err)
	}

	body := "Read [about us](/en/about) and [the team](team).\n\n[Top](#top) [Shop](https://shop.example/)"
	mailing := s.Submit("/admin/news/mailings/new", url.Values{"Subject": {"Links"}, "Body": {body}})

	sent := len(s.Mail.Sent())
	s.Post(mailing, mailing+"/send", url.Values{})
	finished(t, s, mailing)

	messages := s.Mail.Sent()[sent:]
	if len(messages) != 1 {
		t.Fatalf("messages: %+v", messages)
	}

	origin := regexp.MustCompile(`https?://[^/\s]+/_/news/unsubscribe`).FindString(messages[0].Text)
	origin = strings.TrimSuffix(origin, "/_/news/unsubscribe")

	if origin == "" {
		t.Fatalf("no unsubscribe link in\n%s", messages[0].Text)
	}

	for _, want := range []string{
		`<a href="` + origin + `/en/about">about us</a>`,
		`<a href="` + origin + `/en/team">the team</a>`,
		`<a href="#top">Top</a>`,
		`<a href="https://shop.example/">Shop</a>`,
	} {
		if !strings.Contains(messages[0].HTML, want) {
			t.Fatalf("missing %s in\n%s", want, messages[0].HTML)
		}
	}

	want := "Read [about us](" + origin + "/en/about) and [the team](" + origin + "/en/team).\n\n" +
		"[Top](#top) [Shop](https://shop.example/)"

	if !strings.HasPrefix(messages[0].Text, want) {
		t.Fatalf("text part:\n%s\nwant it to start with\n%s", messages[0].Text, want)
	}
}

// opaquePNG is a white PNG of width × height pixels without transparency.
func opaquePNG(t *testing.T, width, height int) []byte {
	t.Helper()

	picture := image.NewGray(image.Rect(0, 0, width, height))
	for index := range picture.Pix {
		picture.Pix[index] = 255
	}

	var buffer bytes.Buffer

	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

// served fetches the image at an absolute address on the site and returns
// its format and size.
func served(t *testing.T, s *sitetest.Site, address string) (string, int, int) {
	t.Helper()

	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}

	response, body := s.Response(parsed.RequestURI())
	if response.StatusCode != http.StatusOK {
		t.Fatalf("%s: %d", address, response.StatusCode)
	}

	config, format, err := image.DecodeConfig(strings.NewReader(body))
	if err != nil {
		t.Fatalf("%s: %v", address, err)
	}

	return format, config.Width, config.Height
}
