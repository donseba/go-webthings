package testsuite

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/pages"
)

// benchPages is how many published pages the benchmark site has.
const benchPages = 200

// benchSections and benchChildren shape the nested benchmark site: a menu
// of benchSections sections with benchChildren pages each, as sites are
// usually built.
const (
	benchSections = 8
	benchChildren = 25
)

// benchSite starts an app on SQLite with benchPages published pages below
// Home, each with a body of a few kilobytes that shows three images, and
// returns it with the path of one of the pages. The output cache keeps
// pages for cache; zero is off (see benchApp).
func benchSite(b *testing.B, cache time.Duration) (*webthings.App, string) {
	b.Helper()

	app := benchApp(b, "../sitetest/templates", cache)
	images := benchImages(b, app)
	home := benchHome(b, app)

	for i := range benchPages {
		benchPage(b, app, home, fmt.Sprintf("Page %d", i), images)
	}

	return app, "/en/page-7"
}

// benchNestedSite is benchSite with its pages in benchSections sections of
// benchChildren pages, in the theme of templates. It returns the app with
// the paths of two pages in different sections.
func benchNestedSite(b *testing.B, templates string, cache time.Duration) (app *webthings.App, first, second string) {
	b.Helper()

	app = benchApp(b, templates, cache)
	images := benchImages(b, app)
	home := benchHome(b, app)

	for section := range benchSections {
		parent := benchPage(b, app, home, fmt.Sprintf("Section %d", section), images)

		for child := range benchChildren {
			benchPage(b, app, parent, fmt.Sprintf("Page %d", section*benchChildren+child), images)
		}
	}

	return app, "/en/section-2/page-57", "/en/section-5/page-130"
}

// benchApp starts an app on SQLite with the theme of templates, and waits
// until the images it is given later are prepared (see benchImages). The
// output cache keeps pages for cache; with zero it is off, so the
// benchmarks measure renders.
func benchApp(b *testing.B, templates string, cache time.Duration) *webthings.App {
	b.Helper()

	if cache == 0 {
		cache = -1
	}

	app := webthings.New(webthings.Config{
		Mail:           &mail.Memory{},
		Name:           "Bench site",
		StateDir:       b.TempDir(),
		Locales:        locales.Config{Default: "en", Enabled: []string{"en", "nl"}},
		Insecure:       true,
		Templates:      os.DirFS(templates),
		OutputCacheTTL: cache,
	})

	if err := app.Start(context.Background()); err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() { _ = app.Close() })

	return app
}

// benchImages uploads three images and waits for the variants the library
// makes of them in the background (media.Service.Prepare), so their
// encoding does not run while the benchmark measures.
func benchImages(b *testing.B, app *webthings.App) []string {
	b.Helper()

	ctx := context.Background()

	var images []string

	for i := range 3 {
		var buffer bytes.Buffer

		if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 1600+i, 900))); err != nil {
			b.Fatal(err)
		}

		asset, err := app.Media.Upload(ctx, fmt.Sprintf("photo-%d.png", i), &buffer)
		if err != nil {
			b.Fatal(err)
		}

		images = append(images, asset.ID)
	}

	if err := app.Media.WaitPrepared(ctx); err != nil {
		b.Fatal(err)
	}

	return images
}

// benchHome is the ID of the home page.
func benchHome(b *testing.B, app *webthings.App) string {
	b.Helper()

	home, err := app.Pages.Home(context.Background())
	if err != nil {
		b.Fatal(err)
	}

	return home
}

// benchPage publishes a page titled title below parent, with a body of a
// few kilobytes that shows the three images, and returns its ID.
func benchPage(b *testing.B, app *webthings.App, parent, title string, images []string) string {
	b.Helper()

	ctx := context.Background()
	paragraph := strings.Repeat("Lorem ipsum dolor sit amet, consectetur **adipiscing** elit, sed do eiusmod. ", 6)

	var body strings.Builder

	for j := range 8 {
		fmt.Fprintf(&body, "## Section %d\n\n%s\n\n", j, paragraph)

		if j%3 == 0 {
			fmt.Fprintf(&body, "![](media:%s)\n\n", images[j/3])
		}
	}

	body.WriteString("A [link](https://example.com) and `code`, number of " + title + ".\n")

	page, err := app.Pages.Save(ctx, pages.Draft{
		Fields: pages.Fields{ParentID: parent},
		Translations: []content.Translation[pages.Text]{
			{Locale: "en", Text: pages.Text{Title: title, Body: body.String()}},
			{Locale: "nl", Text: pages.Text{Title: title + " (nl)", Body: body.String()}},
		},
	})
	if err != nil {
		b.Fatal(err)
	}

	if err := app.Pages.Publish(ctx, page.ID, page.Revision.ID); err != nil {
		b.Fatal(err)
	}

	return page.ID
}

// get serves path with the app and fails the benchmark on another status
// than 200 OK.
func get(b *testing.B, app *webthings.App, path string) {
	b.Helper()

	serve(b, app, httptest.NewRequest(http.MethodGet, path, nil), http.StatusOK)
}

// serve serves request with the app and fails the benchmark on another
// status than status. It returns the body.
func serve(b *testing.B, app *webthings.App, request *http.Request, status int) string {
	b.Helper()

	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != status {
		b.Fatalf("%s %s: %d\n%s", request.Method, request.URL, recorder.Code, recorder.Body.String())
	}

	return recorder.Body.String()
}

// navigation is the request of an HTMX navigation from the page at from to
// the page at path, as a boosted link sends it.
func navigation(from, path string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Request-Type", "partial")
	request.Header.Set("HX-Target", "main#content")
	request.Header.Set("HX-Current-URL", "http://example.com"+from)

	return request
}

// benchCached runs a benchmark rendered, with the output cache off, and
// cached, served from the output cache, with the site run makes for each.
func benchCached(b *testing.B, run func(b *testing.B, cache time.Duration)) {
	b.Run("rendered", func(b *testing.B) {
		run(b, 0)
	})

	b.Run("cached", func(b *testing.B) {
		run(b, time.Hour)
	})
}

// BenchmarkPublicPage serves a published page with three images, in a
// menu of benchPages pages side by side, as a browser asks for it, with
// gzip.
func BenchmarkPublicPage(b *testing.B) {
	benchCached(b, func(b *testing.B, cache time.Duration) {
		app, path := benchSite(b, cache)

		getGzip(b, app, path)
		b.ResetTimer()

		for b.Loop() {
			getGzip(b, app, path)
		}
	})
}

// BenchmarkPublicPageNested serves a published page in a menu of
// benchSections sections of benchChildren pages, as a browser asks for it,
// with gzip.
func BenchmarkPublicPageNested(b *testing.B) {
	benchCached(b, func(b *testing.B, cache time.Duration) {
		app, path, _ := benchNestedSite(b, "../sitetest/templates", cache)

		getGzip(b, app, path)
		b.ResetTimer()

		for b.Loop() {
			getGzip(b, app, path)
		}
	})
}

// getGzip is get for a client that accepts gzip.
func getGzip(b *testing.B, app *webthings.App, path string) {
	b.Helper()

	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Accept-Encoding", "gzip")

	serve(b, app, request, http.StatusOK)
}

// BenchmarkNotFound serves the not-found page of the site with benchPages
// pages.
func BenchmarkNotFound(b *testing.B) {
	app, _ := benchSite(b, 0)
	request := httptest.NewRequest(http.MethodGet, "/en/missing", nil)

	serve(b, app, request, http.StatusNotFound)
	b.ResetTimer()

	for b.Loop() {
		serve(b, app, request, http.StatusNotFound)
	}
}

// BenchmarkHTMXContent is an HTMX navigation in a theme without regions:
// only the content goes back.
func BenchmarkHTMXContent(b *testing.B) {
	app, first, second := benchNestedSite(b, "../sitetest/templates", 0)

	serve(b, app, navigation(first, second), http.StatusOK)
	b.ResetTimer()

	for b.Loop() {
		serve(b, app, navigation(first, second), http.StatusOK)
	}
}

// BenchmarkHTMXNavigation is an HTMX navigation between pages in two
// sections of the nested site, in a theme whose regions follow the
// content: the head, the languages and the menu.
func BenchmarkHTMXNavigation(b *testing.B) {
	app, first, second := benchNestedSite(b, "testdata/navtheme", 0)

	serve(b, app, navigation(first, second), http.StatusOK)
	b.ResetTimer()

	var body string

	for b.Loop() {
		body = serve(b, app, navigation(first, second), http.StatusOK)
	}

	b.ReportMetric(float64(len(body)), "body-bytes")
}

// BenchmarkSearchPages searches the published pages.
func BenchmarkSearchPages(b *testing.B) {
	app, _ := benchSite(b, 0)
	ctx := context.Background()

	b.ResetTimer()

	for b.Loop() {
		results, err := app.Frontend.Search(ctx, "en", "adipiscing number", 10)
		if err != nil || len(results) == 0 {
			b.Fatal(err, len(results))
		}
	}
}

// BenchmarkSitemap serves /sitemap.xml.
func BenchmarkSitemap(b *testing.B) {
	app, _ := benchSite(b, 0)

	get(b, app, "/sitemap.xml")
	b.ResetTimer()

	for b.Loop() {
		get(b, app, "/sitemap.xml")
	}
}

// BenchmarkPagesSave saves a new revision of one page among benchPages.
func BenchmarkPagesSave(b *testing.B) {
	app, _ := benchSite(b, 0)
	ctx := context.Background()

	page, err := app.Pages.Latest(ctx, mustPageID(b, app))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for b.Loop() {
		_, err := app.Pages.Save(ctx, pages.Draft{ID: page.ID, Fields: page.Fields, Translations: page.Translations})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPagesTree lists the latest page tree, as the admin does.
func BenchmarkPagesTree(b *testing.B) {
	app, _ := benchSite(b, 0)
	ctx := context.Background()

	b.ResetTimer()

	for b.Loop() {
		nodes, err := app.Pages.Tree(ctx, "en")
		if err != nil || len(nodes) != benchPages+1 {
			b.Fatal(err, len(nodes))
		}
	}
}

// mustPageID is the ID of a page below Home.
func mustPageID(b *testing.B, app *webthings.App) string {
	b.Helper()

	routes, err := app.Pages.Routes(context.Background(), "en")
	if err != nil || len(routes) < 2 {
		b.Fatal(err, len(routes))
	}

	return routes[1].PageID
}
