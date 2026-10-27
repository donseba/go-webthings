package frontend_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/donseba/go-webthings/frontend"
)

// TestThemeScreenshot checks theme.json's "screenshot": a picture in the
// theme's assets folder, or a refusal at load.
func TestThemeScreenshot(t *testing.T) {
	theme := func(screenshot string) fstest.MapFS {
		return fstest.MapFS{
			"default/layout.gohtml":          {Data: []byte(`{{content}}`)},
			"default/theme.json":             {Data: []byte(`{"screenshot": "` + screenshot + `"}`)},
			"default/assets/shot.png":        {Data: []byte("png")},
			"default/assets/img/preview.jpg": {Data: []byte("jpg")},
			"default/assets/notes.txt":       {Data: []byte("text")},
			"default/secret.png":             {Data: []byte("outside the assets")},
		}
	}

	for _, good := range []string{"shot.png", "img/preview.jpg", ""} {
		themes, err := frontend.LoadThemes(theme(good))
		if err != nil {
			t.Fatalf("%q: %v", good, err)
		}

		if got, _ := themes.Get("default"); got.Screenshot != good {
			t.Fatalf("%q: screenshot %q", good, got.Screenshot)
		}
	}

	refused := map[string]string{
		"/shot.png":       "use a path inside the theme's assets folder",
		"../secret.png":   "use a path inside the theme's assets folder",
		"img/../shot.png": "use a path inside the theme's assets folder",
		"./shot.png":      "use a path inside the theme's assets folder",
		"notes.txt":       "use a PNG, JPEG, WebP, AVIF or GIF image",
		"missing.png":     "does not exist",
	}

	for bad, reason := range refused {
		_, err := frontend.LoadThemes(theme(bad))
		if err == nil || !strings.Contains(err.Error(), "screenshot") || !strings.Contains(err.Error(), reason) {
			t.Errorf("%q: want a refusal for %q, got %v", bad, reason, err)
		}
	}
}
