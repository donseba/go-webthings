package testsuite

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/sitetest"

	"github.com/donseba/go-importmap/assets"
)

// TestFocalPointOnPages checks that a focal point saved in the media
// library moves the crops templates ask for, and the CSS crops of
// objectPosition and focalClass with them, while resized copies keep their
// URL; core serves the classes of focalClass in focal-points.css.
func TestFocalPointOnPages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"templates/layout.gohtml": `<html><head>{{assetStyle "focal-points.css"}}</head><body>{{content}}</body></html>`,
		"templates/probe/screen.gohtml": `<main id="content">` +
			`<img class="crop" src="{{image . "100x100 crop"}}">` +
			`<img class="resize" src="{{image . "500"}}">` +
			`<img class="css" style="object-fit: cover; object-position: {{objectPosition . "1:1"}}">` +
			`<img class="any" style="object-position: {{objectPosition .}}">` +
			`<img class="cover {{focalClass . "1:1"}}">` +
			`<img class="cover-any {{focalClass .}}">` +
			`<img class="unknown {{focalClass ""}}">` +
			`</main>`,
	})

	var asset string

	s := sitetest.StartWith(t, sitetest.Options{Files: os.DirFS(filepath.Join(dir, "templates"))}, screens(&asset))
	s.Mount("probe", "Probe", "probe")

	photo, err := s.App.Media.Upload(context.Background(), "photo.png", bytes.NewReader(sitetest.PNG(t, 2000, 1000)))
	if err != nil {
		t.Fatal(err)
	}

	asset = photo.ID

	s.Expect("/en/probe", http.StatusOK,
		photo.ID+`_100x100c.webp">`,
		photo.ID+`_500x0.webp">`,
		`<img class="css" style="object-fit: cover; object-position: 50% 50%">`,
		`<img class="any" style="object-position: 50% 50%">`,
		`<img class="cover fp-50-50">`,
		`<img class="cover-any fp-50-50">`,
		`<img class="unknown fp-50-50">`,
	)

	page := "/admin/media/" + photo.ID
	s.Post(page, page+"/focus", url.Values{"FocalX": {"90"}, "FocalY": {"20"}})

	s.Expect("/en/probe", http.StatusOK,
		photo.ID+`_100x100c-900-200.webp">`,
		photo.ID+`_500x0.webp">`,
		`<img class="css" style="object-fit: cover; object-position: 100% 50%">`,
		`<img class="any" style="object-position: 90% 20%">`,
		`<img class="cover fp-100-50">`,
		`<img class="cover-any fp-90-20">`,
	)

	// The stylesheet of the classes, by its version and with its
	// integrity, has a rule for each.
	_, probe := s.Response("/en/probe")

	link := regexp.MustCompile(`<link rel="stylesheet" href="(/_/focal-points\.css\?v=[0-9a-f]{16})" integrity="(sha384-[^"]+)"`).
		FindStringSubmatch(probe)
	if link == nil {
		t.Fatalf("focal-points.css is not linked:\n%s", probe)
	}

	response, css := s.Response(link[1])
	if response.StatusCode != http.StatusOK || !strings.Contains(response.Header.Get("Cache-Control"), "immutable") ||
		link[2] != assets.Integrity([]byte(css)) {
		t.Fatalf("focal-points.css: %d %v", response.StatusCode, response.Header)
	}

	for _, rule := range []string{
		".fp-100-50 { object-position: 100% 50%; }",
		".fp-90-20 { object-position: 90% 20%; }",
		".fp-50-50 { object-position: 50% 50%; }",
	} {
		if !strings.Contains(css, rule) {
			t.Fatalf("no %s in focal-points.css", rule)
		}
	}
}
