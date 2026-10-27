package admin_test

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/donseba/go-webthings/admin"
)

// TestVersionedAssets checks that the layout links the shared assets and the
// import map's modules by versioned URLs, and that those are served with a
// strong ETag: cached for a year at their current version, revalidated
// otherwise.
func TestVersionedAssets(t *testing.T) {
	t.Parallel()

	var badge atomic.Int32

	b := start(t, admin.Options{Language: "en"}, toolsSection(&badge))
	b.signIn()

	_, page := b.get("/admin/tools")

	for _, want := range []*regexp.Regexp{
		// The layout's own tags carry their Subresource Integrity
		// (importmap_script, importmap_style).
		regexp.MustCompile(`<link rel="stylesheet" href="/admin/assets/admin\.css\?v=[0-9a-f]{16}" integrity="sha384-[^"]+" crossorigin="anonymous">`),
		regexp.MustCompile(`<script src="/admin/assets/admin\.js\?v=[0-9a-f]{16}" integrity="sha384-[^"]+" crossorigin="anonymous" type="module">`),
		regexp.MustCompile(`<script src="/admin/assets/theme\.js\?v=[0-9a-f]{16}" integrity="sha384-[^"]+" crossorigin="anonymous">`),
		regexp.MustCompile(`"htmx": "/admin/assets/packages/htmx/htmx\.esm\.min\.js\?v=[0-9a-f]{16}"`),
		// go-partial's browser clients are versioned assets too.
		regexp.MustCompile(`"partial-sse": "/admin/assets/partial-sse\.js\?v=[0-9a-f]{16}"`),
		regexp.MustCompile(`"partial-flash": "/admin/assets/partial-flash\.js\?v=[0-9a-f]{16}"`),
		regexp.MustCompile(`"partial-interactions": "/admin/assets/partial-interactions\.js\?v=[0-9a-f]{16}"`),
		regexp.MustCompile(`"/admin/assets/partial-sse\.js\?v=[0-9a-f]{16}": "sha384-[^"]+"`),
		regexp.MustCompile(`<link rel="modulepreload" href="/admin/assets/packages/htmx/htmx\.esm\.min\.js\?v=[0-9a-f]{16}" integrity="sha384-[^"]+"/>`),
		// The embedded scripts carry their Subresource Integrity.
		regexp.MustCompile(`"/admin/assets/packages/htmx/htmx\.esm\.min\.js\?v=[0-9a-f]{16}": "sha384-[^"]+"`),
		regexp.MustCompile(`"/admin/assets/editor/prosemirror\.js\?v=[0-9a-f]{16}": "sha384-[^"]+"`),
		// Modules and files the scripts address relative to themselves.
		regexp.MustCompile(`"/admin/assets/editor/prosemirror\.js": "/admin/assets/editor/prosemirror\.js\?v=[0-9a-f]{16}"`),
		regexp.MustCompile(`"/admin/assets/icons\.svg": "/admin/assets/icons\.svg\?v=[0-9a-f]{16}"`),
		regexp.MustCompile(`"/admin/assets/editor/editor\.css": "/admin/assets/editor/editor\.css\?v=[0-9a-f]{16}"`),
		regexp.MustCompile(`<use href="/admin/assets/icons\.svg\?v=[0-9a-f]{16}#`),
		regexp.MustCompile(`src="/admin/assets/editor/loader\.js\?v=[0-9a-f]{16}" integrity="sha384-[^"]+"`),
	} {
		if !want.MatchString(page) {
			t.Errorf("the page lacks %s", want)
		}
	}

	// Licences, the lock, the Tailwind source and the vendored files'
	// plain URLs stay out of the import map: no script loads them by a
	// relative path.
	for _, unwanted := range []string{
		`"/admin/assets/packages/htmx/htmx.esm.min.js": `,
		`"/admin/assets/editor/LICENSES.txt": `,
		`"/admin/assets/source.css": `,
	} {
		if strings.Contains(page, unwanted) {
			t.Errorf("the import map maps %s", unwanted)
		}
	}

	link := regexp.MustCompile(`"htmx": "(/admin/assets/packages/htmx/htmx\.esm\.min\.js\?v=[0-9a-f]+)"`).FindStringSubmatch(page)
	if link == nil {
		t.Fatal("htmx is not in the import map")
	}

	current := b.fetch(link[1], "")
	if current.StatusCode != http.StatusOK || current.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		!strings.Contains(current.Header.Get("Content-Type"), "javascript") {
		t.Fatalf("current version: %d %q %q", current.StatusCode, current.Header.Get("Cache-Control"),
			current.Header.Get("Content-Type"))
	}

	etag := current.Header.Get("ETag")
	if !regexp.MustCompile(`^"[0-9a-f]{16}"$`).MatchString(etag) {
		t.Fatalf("ETag %q", etag)
	}

	if revalidated := b.fetch(link[1], etag); revalidated.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidation: %d", revalidated.StatusCode)
	}

	// A link without its version, or with an old one, must not be cached
	// as it is.
	plain := b.fetch("/admin/assets/icons.svg?v=old", "")
	if plain.StatusCode != http.StatusOK || plain.Header.Get("Cache-Control") != "no-cache" || plain.Header.Get("ETag") == "" {
		t.Fatalf("old version: %d %q", plain.StatusCode, plain.Header.Get("Cache-Control"))
	}

	for _, missing := range []string{"/admin/assets/missing.js", "/admin/assets/editor/", "/admin/assets/htmx/htmx-4.0.0.esm.min.js"} {
		if status, _ := b.get(missing); status != http.StatusNotFound {
			t.Errorf("%s: %d", missing, status)
		}
	}
}

// TestVersionedStylesheet checks that a section's stylesheet is served by its
// versioned URL with a strong ETag.
func TestVersionedStylesheet(t *testing.T) {
	t.Parallel()

	var badge atomic.Int32

	b := start(t, admin.Options{Language: "en"}, toolsSection(&badge))
	b.signIn()

	_, page := b.get("/admin/tools")

	link := regexp.MustCompile(`href="(/admin/assets/sections/tools\.css\?v=[0-9a-f]{16})"`).FindStringSubmatch(page)
	if link == nil {
		t.Fatal("section stylesheet not linked")
	}

	current := b.fetch(link[1], "")
	if current.StatusCode != http.StatusOK || current.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("stylesheet: %d %q", current.StatusCode, current.Header.Get("Cache-Control"))
	}

	revalidated := b.fetch("/admin/assets/sections/tools.css", current.Header.Get("ETag"))
	if revalidated.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidation: %d", revalidated.StatusCode)
	}
}

// fetch requests path, with If-None-Match when etag is set, and returns the
// response with its body read.
func (b browser) fetch(path, etag string) *http.Response {
	b.t.Helper()

	request, err := http.NewRequest(http.MethodGet, b.server.URL+path, nil)
	if err != nil {
		b.t.Fatal(err)
	}

	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}

	response, err := b.client.Do(request)
	if err != nil {
		b.t.Fatal(err)
	}

	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()

	return response
}
