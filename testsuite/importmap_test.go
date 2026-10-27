package testsuite

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/sitetest"

	"github.com/donseba/go-importmap"
	"github.com/donseba/go-importmap/assets"
)

// siteImportMap is the import map of a vendored package, lib, and the site's
// own script app/main.js.
func siteImportMap(t *testing.T) *importmap.ImportMap {
	t.Helper()

	lib := []byte(`export default 1;`)
	lock := fmt.Sprintf(`{"lockfileVersion": 1, "packages": [{"name": "lib", "version": "1.0.0", "files": [
		{"path": "lib/lib.js", "as": "lib", "source": "https://cdn.example.test/lib.js", "integrity": %q}
	]}]}`, assets.Integrity(lib))

	im, err := importmap.FromFS(fstest.MapFS{
		"vendor/importmap.lock.json": {Data: []byte(lock)},
		"vendor/lib/lib.js":          {Data: lib},
		"app/main.js":                {Data: []byte(`import lib from "lib";`)},
	}, "vendor/"+importmap.LockFile)
	if err != nil {
		t.Fatal(err)
	}

	return im
}

// TestConfigImportMap checks Config.ImportMap: templates get the import map
// and versioned URLs of its files, which /assets/ serves with strong ETags.
func TestConfigImportMap(t *testing.T) {
	t.Parallel()

	s := sitetest.StartWith(t, sitetest.Options{
		Files: fstest.MapFS{
			"layout.gohtml": {Data: []byte(`<head>{{importmap}}` +
				`<script type="module" src="{{importmap_url "app/main.js"}}"></script></head><body>{{content}}</body>`)},
			"page.gohtml": {Data: []byte(`<main>{{.Title}}</main>`)},
		},
		Configure: func(config *webthings.Config) {
			config.ImportMap = siteImportMap(t)
		},
	})

	_, home := s.Response("/en/")

	imported := regexp.MustCompile(`"lib": "(/assets/vendor/lib/lib\.js\?v=[0-9a-f]{16})"`).FindStringSubmatch(home)
	if imported == nil || !regexp.MustCompile(`src="/assets/app/main\.js\?v=[0-9a-f]{16}"`).MatchString(home) {
		t.Fatalf("the page lacks the import map or the script's versioned URL:\n%s", home)
	}

	// The files are embedded, so the import map carries their integrity.
	integrity := fmt.Sprintf("%q: %q", imported[1], assets.Integrity([]byte(`export default 1;`)))
	if !strings.Contains(home, integrity) {
		t.Fatalf("the import map lacks %s:\n%s", integrity, home)
	}

	current := request(t, s.App, http.MethodGet, imported[1])
	if current.Code != http.StatusOK || current.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		current.Body.String() != `export default 1;` {
		t.Fatalf("current version: %d %v %s", current.Code, current.Header(), current.Body)
	}

	etag := current.Header().Get("ETag")

	revalidated := request(t, s.App, http.MethodGet, "/assets/vendor/lib/lib.js", "If-None-Match", etag)
	if revalidated.Code != http.StatusNotModified {
		t.Fatalf("revalidation: %d", revalidated.Code)
	}

	if missing := request(t, s.App, http.MethodGet, "/assets/missing.js"); missing.Code != http.StatusNotFound {
		t.Fatalf("missing file: %d", missing.Code)
	}
}

// TestAssetsAndImportMap checks that a site cannot serve both Assets and
// ImportMap at /assets/.
func TestAssetsAndImportMap(t *testing.T) {
	t.Parallel()

	app := webthings.New(webthings.Config{
		StateDir:  t.TempDir(),
		Assets:    fstest.MapFS{},
		ImportMap: siteImportMap(t),
	})

	err := app.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ImportMap") {
		_ = app.Close()

		t.Fatalf("both are accepted: %v", err)
	}
}

// TestConfigAssets checks that Config.Assets are served like an import
// map's files: templates get their versioned URLs, cached for a year, and a
// plain link revalidates with a strong ETag.
func TestConfigAssets(t *testing.T) {
	t.Parallel()

	s := sitetest.StartWith(t, sitetest.Options{
		Files: fstest.MapFS{
			"layout.gohtml": {
				Data: []byte(`<head><script src="{{importmap_url "site.js"}}"></script></head><body>{{content}}</body>`),
			},
			"page.gohtml": {Data: []byte(`<main>{{.Title}}</main>`)},
		},
		Configure: func(config *webthings.Config) {
			config.Assets = fstest.MapFS{"site.js": {Data: []byte(`console.log(1);`)}}
		},
	})

	_, home := s.Response("/en/")

	link := regexp.MustCompile(`src="(/assets/site\.js\?v=[0-9a-f]{16})"`).FindStringSubmatch(home)
	if link == nil {
		t.Fatalf("the page lacks the script's versioned URL:\n%s", home)
	}

	current := request(t, s.App, http.MethodGet, link[1])
	if current.Code != http.StatusOK || current.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		current.Body.String() != `console.log(1);` {
		t.Fatalf("current version: %d %v %s", current.Code, current.Header(), current.Body)
	}

	plain := request(t, s.App, http.MethodGet, "/assets/site.js")
	if plain.Code != http.StatusOK || plain.Header().Get("Cache-Control") != "no-cache" || plain.Header().Get("ETag") == "" {
		t.Fatalf("plain link: %d %v", plain.Code, plain.Header())
	}

	if missing := request(t, s.App, http.MethodGet, "/assets/missing.js"); missing.Code != http.StatusNotFound {
		t.Fatalf("missing file: %d", missing.Code)
	}
}

// TestReloadingAssets checks that, while templates reload, edited files of
// Config.Assets and of the theme's assets/ folder get new versions without
// a restart, and that pages then carry no integrity, which a cached page
// would hold on to after an edit.
func TestReloadingAssets(t *testing.T) {
	t.Parallel()

	theme := t.TempDir()
	site := t.TempDir()

	write := func(dir, name, data string) {
		t.Helper()

		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(
		theme,
		"layout.gohtml",
		`<head>{{assetStyle "site.css"}}<script type="module" src="{{importmap_url "site.js"}}"></script>{{importmap}}</head><body>{{content}}</body>`,
	)
	write(theme, "page.gohtml", `<main>{{.Title}}</main>`)
	write(theme, "assets/site.css", `body{}`)
	write(site, "site.js", `console.log(1);`)

	s := sitetest.StartWith(t, sitetest.Options{
		Files: os.DirFS(theme),
		Configure: func(config *webthings.Config) {
			config.Assets = os.DirFS(site)
			config.ReloadTemplates = true
		},
	})

	versions := func() (string, string) {
		t.Helper()

		_, home := s.Response("/en/")

		style := regexp.MustCompile(`href="/theme/[^"]*site\.css\?v=([0-9a-f]{16})"`).FindStringSubmatch(home)
		script := regexp.MustCompile(`src="/assets/site\.js\?v=([0-9a-f]{16})"`).FindStringSubmatch(home)

		if style == nil || script == nil {
			t.Fatalf("the page lacks the versioned URLs:\n%s", home)
		}

		if strings.Contains(home, "integrity") {
			t.Fatalf("a reloading page carries integrity:\n%s", home)
		}

		return style[1], script[1]
	}

	style, script := versions()

	// Other sizes, so the change shows whatever the file system's
	// modification time resolution.
	write(theme, "assets/site.css", `body{color:red}`)
	write(site, "site.js", `console.log("changed");`)

	newStyle, newScript := versions()
	if newStyle == style || newScript == script {
		t.Fatalf("versions after an edit: style %s → %s, script %s → %s", style, newStyle, script, newScript)
	}

	changed := request(t, s.App, http.MethodGet, "/assets/site.js?v="+newScript)
	if changed.Code != http.StatusOK || changed.Body.String() != `console.log("changed");` {
		t.Fatalf("edited script: %d %s", changed.Code, changed.Body)
	}
}

// TestImportMapClients checks that core maps go-partial's browser clients
// in every site's import map, Config.Assets and Config.ImportMap alike,
// serves them at /assets/, and leaves a name the site maps itself alone.
func TestImportMapClients(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<head>{{importmap}}</head><body>{{content}}</body>`)},
		"page.gohtml":   {Data: []byte(`<main>{{.Title}}</main>`)},
	}

	client := regexp.MustCompile(`"partial-sse": "(/assets/partial-sse\.js\?v=[0-9a-f]{16})"`)

	t.Run("assets", func(t *testing.T) {
		t.Parallel()

		s := sitetest.StartWith(t, sitetest.Options{
			Files: files,
			Configure: func(config *webthings.Config) {
				config.Assets = fstest.MapFS{"site.js": {Data: []byte(`import "partial-sse";`)}}
			},
		})

		_, home := s.Response("/en/")

		for _, name := range []string{"partial-flash", "partial-interactions"} {
			if !strings.Contains(home, `"`+name+`": "/assets/`+name+`.js?v=`) {
				t.Errorf("the import map lacks %s:\n%s", name, home)
			}
		}

		address := client.FindStringSubmatch(home)
		if address == nil {
			t.Fatalf("the import map lacks partial-sse:\n%s", home)
		}

		// The clients are among the site's fixed files, so the map
		// carries their integrity.
		if !strings.Contains(home, fmt.Sprintf("%q: \"sha384-", address[1])) {
			t.Errorf("the import map lacks the client's integrity:\n%s", home)
		}

		served := request(t, s.App, http.MethodGet, address[1])
		if served.Code != http.StatusOK || served.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Fatalf("client: %d %v", served.Code, served.Header())
		}
	})

	t.Run("import map", func(t *testing.T) {
		t.Parallel()

		s := sitetest.StartWith(t, sitetest.Options{
			Files: files,
			Configure: func(config *webthings.Config) {
				config.ImportMap = siteImportMap(t).WithImport("partial-flash", "/own-flash.js", "")
			},
		})

		_, home := s.Response("/en/")

		address := client.FindStringSubmatch(home)
		if address == nil || !strings.Contains(home, `"partial-interactions": "/assets/partial-interactions.js?v=`) {
			t.Fatalf("the import map lacks the clients:\n%s", home)
		}

		if !strings.Contains(home, `"partial-flash": "/own-flash.js"`) {
			t.Errorf("the site's own partial-flash was replaced:\n%s", home)
		}

		// Core's copy of a client carries its integrity too.
		if !strings.Contains(home, fmt.Sprintf("%q: \"sha384-", address[1])) {
			t.Errorf("the import map lacks the client's integrity:\n%s", home)
		}

		served := request(t, s.App, http.MethodGet, address[1])
		if served.Code != http.StatusOK || served.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" ||
			!strings.Contains(served.Body.String(), "partial") {
			t.Fatalf("client: %d %v", served.Code, served.Header())
		}

		// The site's own files are served as before.
		if lib := request(t, s.App, http.MethodGet, "/assets/vendor/lib/lib.js"); lib.Code != http.StatusOK {
			t.Fatalf("site file: %d", lib.Code)
		}
	})

	t.Run("import map with a client", func(t *testing.T) {
		t.Parallel()

		own := []byte(`export const own = true;`)

		im, err := importmap.FromFS(fstest.MapFS{
			importmap.LockFile: {Data: []byte(`{"lockfileVersion": 1, "packages": []}`)},
			"partial-flash.js": {Data: own},
		}, importmap.LockFile)
		if err != nil {
			t.Fatal(err)
		}

		s := sitetest.StartWith(t, sitetest.Options{
			Files:     files,
			Configure: func(config *webthings.Config) { config.ImportMap = im },
		})

		_, home := s.Response("/en/")

		flash := regexp.MustCompile(`"partial-flash": "(/assets/partial-flash\.js\?v=[0-9a-f]{16})"`).FindStringSubmatch(home)
		if flash == nil {
			t.Fatalf("the import map lacks partial-flash:\n%s", home)
		}

		// The site's own file, with its own integrity, not core's copy.
		if !strings.Contains(home, fmt.Sprintf("%q: %q", flash[1], assets.Integrity(own))) {
			t.Errorf("the import map lacks the site's partial-flash with its integrity:\n%s", home)
		}

		served := request(t, s.App, http.MethodGet, flash[1])
		if served.Code != http.StatusOK || served.Body.String() != string(own) {
			t.Fatalf("partial-flash: %d %s", served.Code, served.Body)
		}
	})
}
