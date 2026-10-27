package testsuite

import (
	"context"
	"image"
	_ "image/jpeg" // Decode the social image.
	_ "image/png"  // Decode the icons.
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"

	_ "github.com/gen2brain/webp" // Decode the variants.
)

// TestImagesOnPages checks that pages show variants of images, never the
// uploads: the site's logo (a PNG stays PNG), icons and social image are
// made to measure and module views give a copy with a srcset and the
// original apart.
func TestImagesOnPages(t *testing.T) {
	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"layout.gohtml": `<html><head><title>{{.Title}}</title>
{{with or .Image .Site.SocialImageURL}}<meta property="og:image" content="{{.}}">{{end}}
<link rel="icon" href="{{.Site.FaviconURL}}"><link rel="apple-touch-icon" href="{{.Site.AppleTouchIconURL}}">
</head><body><img id="logo" src="{{.Site.LogoURL}}">{{content}}</body></html>`,
		"page.gohtml": `<main id="content"><h1>{{.Title}}</h1>{{.Body}}</main>`,
		"404.gohtml":  `<main id="content"><h1>Not found</h1></main>`,
		"news/list.gohtml": `<main id="content">{{range .Posts}}` +
			`<img src="{{.ImageURL}}" srcset="{{.ImageSrcset}}">{{end}}</main>`,
		"news/post.gohtml": `<main id="content"><img src="{{.ImageURL}}" srcset="{{.ImageSrcset}}" ` +
			`width="{{.ImageWidth}}" height="{{.ImageHeight}}"><a href="{{.ImageOriginalURL}}">Original</a></main>`,
	})

	s := sitetest.StartWith(t, sitetest.Options{Files: os.DirFS(dir)}, publishing.Module("news", "News"))
	ctx := context.Background()

	logo := s.Upload("logo.png", sitetest.PNG(t, 1000, 400))
	icon := s.Upload("icon.png", sitetest.PNG(t, 512, 512))
	share := s.Upload("share.png", sitetest.PNG(t, 2000, 1000))
	photo := s.Upload("photo.png", sitetest.PNG(t, 2400, 1600))

	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	settings.LogoMediaID = logo.ID
	settings.FaviconMediaID = icon.ID
	settings.SocialImageMediaID = share.ID

	if err := s.App.Site.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}

	_, home := s.Get("/en/")

	for _, want := range []struct {
		pattern, format string
		width, height   int
	}{
		{`id="logo" src="([^"]+)"`, "png", 600, 240},
		{`rel="icon" href="([^"]+)"`, "png", 32, 32},
		{`rel="apple-touch-icon" href="([^"]+)"`, "png", 180, 180},
		{`property="og:image" content="([^"]+)"`, "jpeg", 1200, 630},
	} {
		link := attribute(t, home, want.pattern)
		format, width, height := imageAt(t, s, link)

		if strings.HasPrefix(link, "/media/0") || format != want.format || width != want.width || height != want.height {
			t.Fatalf("%s: %s is a %d × %d %s", want.pattern, link, width, height, format)
		}
	}

	// A post shows its image as a WebP copy with a srcset; the upload is
	// only linked. Shared, the post shows its own image as the social one.
	post := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title": {"Hello"},
		"Slug":  {"hello"},
		"Date":  {"2026-10-01"},
		"Image": {photo.ID},
	})
	s.Publish(post + "?locale=en")
	s.Mount("news", "News", "news")

	shown := s.App.Media.Image(ctx, photo.ID, media.ImageWidth)
	original := s.App.Media.URL(ctx, photo.ID)

	s.Expect("/en/news/hello", http.StatusOK,
		`<img src="`+shown.URL+`" srcset="`+shown.Srcset+`" width="1600" height="1067">`,
		`<a href="`+original+`">Original</a>`,
		photo.ID+`_1200x630c.jpg">`,
	)
	s.ExpectMissing("/en/news/hello", `src="`+original+`"`)
	s.ExpectMissing("/en/news", original)

	posts, err := publishing.Of(s.App, "news").Latest(ctx, "en", 1)
	if err != nil || len(posts) != 1 {
		t.Fatalf("latest: %+v %v", posts, err)
	}

	if posts[0].ImageURL != shown.URL || posts[0].ImageSrcset != shown.Srcset || posts[0].ImageOriginalURL != original ||
		!strings.HasSuffix(posts[0].ImageURL, photo.ID+"_1600x0.webp") {
		t.Fatalf("post image: %+v", posts[0])
	}
}

// attribute is the first match of pattern's group in body.
func attribute(t *testing.T, body, pattern string) string {
	t.Helper()

	match := regexp.MustCompile(pattern).FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("no %s in\n%s", pattern, body)
	}

	return match[1]
}

// imageAt fetches the image at link and returns its format and size.
func imageAt(t *testing.T, s *sitetest.Site, link string) (string, int, int) {
	t.Helper()

	response, body := s.Response(link)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("%s: %d", link, response.StatusCode)
	}

	config, format, err := image.DecodeConfig(strings.NewReader(body))
	if err != nil {
		t.Fatalf("%s: %v", link, err)
	}

	return format, config.Width, config.Height
}

// TestImagesPrepared checks that the app makes the variants pages show of
// an upload in the background, and that Close stops it.
func TestImagesPrepared(t *testing.T) {
	s := sitetest.Start(t)
	ctx := context.Background()

	photo := s.Upload("photo.png", sitetest.PNG(t, 2000, 100))
	logo := s.Upload("logo.jpg", sitetest.JPEG(t, 1000, 400))

	waiting, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := s.App.Media.WaitPrepared(waiting); err != nil {
		t.Fatal(err)
	}

	var keys []string

	err := s.App.DB.Select(ctx, &keys, `SELECT storage_key FROM wt_media_variant WHERE asset_id=? ORDER BY storage_key`, photo.ID)
	if err != nil || strings.Join(keys, " ") != photo.ID+"_1200x0.webp "+photo.ID+"_1600x0.webp "+
		photo.ID+"_480x0.webp "+photo.ID+"_800x0.webp" {
		t.Fatalf("prepared variants: %v %v", keys, err)
	}

	// A JPEG logo stays a JPEG, as Site.LogoURL gives it.
	link := s.App.Media.ImageAs(ctx, logo.ID, 600, media.OriginalFormat).URL
	if format, width, height := imageAt(t, s, link); format != "jpeg" || width != 600 || height != 240 {
		t.Fatalf("JPEG logo: %s is a %d × %d %s", link, width, height, format)
	}

	// Close stops the background work while uploads wait for it.
	for range 5 {
		s.Upload("more.png", sitetest.PNG(t, 1700, 100))
	}

	closed := make(chan error, 1)

	go func() {
		closed <- s.App.Close()
	}()

	select {
	case <-closed:
	case <-time.After(30 * time.Second):
		t.Fatal("Close hangs")
	}
}
