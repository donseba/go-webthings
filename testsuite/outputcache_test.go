package testsuite

import (
	"bytes"
	"context"
	"html/template"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/modules/comments"
	"github.com/donseba/go-webthings/modules/polls"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

// cacheStatus is how the output cache answered a GET of path by s, as its
// X-Cache header says in development, with the body; the request carries
// the cookies of s and, when given, the Cookie header cookie.
func cacheStatus(t *testing.T, s *sitetest.Site, path, cookie string) (string, string) {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}

	response := s.Request(request)
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	return response.Header.Get("X-Cache"), string(body)
}

// expectCache checks that a GET of path by s is answered as want (hit, miss
// or skip) with every one of parts, and returns the body.
func expectCache(t *testing.T, s *sitetest.Site, path, cookie, want string, parts ...string) string {
	t.Helper()

	status, body := cacheStatus(t, s, path, cookie)
	if status != want {
		t.Fatalf("GET %s (cookie %q): X-Cache %q, want %q\n%s", path, cookie, status, want, body)
	}

	for _, part := range parts {
		if !strings.Contains(body, part) {
			t.Fatalf("GET %s (cookie %q): missing %q\n%s", path, cookie, part, body)
		}
	}

	return body
}

// cachedSite is the configuration of a site whose output cache is on,
// with X-Cache headers (development).
func cachedSite(ttl time.Duration) func(config *webthings.Config) {
	return func(config *webthings.Config) {
		config.Development = true
		config.OutputCacheTTL = ttl
		config.MetricsToken = "metrics-token"
	}
}

// TestOutputCache checks what the output cache keeps and what it leaves
// alone, and that every kind of change in this process shows at once:
// publishing a page, saving the settings, a new alt text in the media
// library, a save in a module's admin section and a visitor's comment.
func TestOutputCache(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"page.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.Page */}}` +
			`<main id="content"><h1>{{.Title}}</h1>{{.Body}}{{commentThread "remarks" "home"}}</main>`,
	})

	s := sitetest.StartWith(t, sitetest.Options{
		Templates:      dir,
		StartingPoints: []string{"news"},
		Configure:      cachedSite(time.Hour),
	}, publishing.Module("news", "News"), comments.Module("remarks", "Comments"))

	ctx := context.Background()

	if err := s.App.Service("remarks").(*comments.Service).SaveSettings(ctx, comments.Settings{}); err != nil {
		t.Fatal(err)
	}

	photo := s.Upload("photo.png", pngBytes(t))

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	about := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title": {"About"}, "Slug": {"about"}, "Parent": {home}, "Body": {"![](media:" + photo.ID + ")"},
	})
	s.Publish(about + "?locale=en")
	s.Mount("news", "News", "news")

	visitor := s.Visitor()

	// The second visit is answered from the cache, also with cookies that
	// change nothing, and with the same page.
	first := expectCache(t, visitor, "/en/about", "", "miss", "<h1>About</h1>", "<title>About · Test site</title>")
	second := expectCache(t, visitor, "/en/about", "", "hit")
	expectCache(t, visitor, "/en/about", "_ga=GA1.1.2; theme=dark", "hit")
	expectCache(t, visitor, "/en/about", "site_flash=1", "hit")

	if first != second {
		t.Fatalf("cached page differs:\n%s\n%s", first, second)
	}

	// Editors, visitors with a kept post, previews and searches with a
	// query get a page of their own.
	expectCache(t, s, "/en/about", "", "skip")
	expectCache(t, visitor, "/en/about", frontend.KeptFormCookie+"remarks=abc", "skip")
	expectCache(t, visitor, "/en/about?"+frontend.PreviewParam+"=1", "", "skip")
	expectCache(t, visitor, "/en/search?q=about", "", "skip")
	expectCache(t, visitor, "/en/search", "", "miss")

	// The admin, media, assets and actions are left alone.
	for _, path := range []string{"/admin/login", "/_/webthings.css", "/media/missing.png"} {
		if status, _ := cacheStatus(t, visitor, path, ""); status != "skip" {
			t.Fatalf("GET %s: X-Cache %q", path, status)
		}
	}

	// A page that is not found is not kept.
	expectCache(t, visitor, "/en/missing", "", "miss")
	expectCache(t, visitor, "/en/missing", "", "miss")

	// Publishing a page.
	s.Post(about+"?locale=en", about+"?locale=en", url.Values{
		"Title": {"About us"}, "Slug": {"about"}, "Parent": {home}, "Body": {"![](media:" + photo.ID + ")"},
	})
	expectCache(t, visitor, "/en/about", "", "hit", "<h1>About</h1>")
	s.Publish(about + "?locale=en")
	expectCache(t, visitor, "/en/about", "", "miss", "<h1>About us</h1>")
	expectCache(t, visitor, "/en/about", "", "hit", "<h1>About us</h1>")

	// Saving the settings.
	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for i := range settings.Translations {
		settings.Translations[i].Title = "Renamed site"
	}

	if err := s.App.Site.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}

	expectCache(t, visitor, "/en/about", "", "miss", "<title>About us · Renamed site</title>")

	// A new alt text in the media library.
	err = s.App.Media.SaveTranslation(ctx, photo.ID, media.Translation{Locale: "en", AltText: "The harbour"})
	if err != nil {
		t.Fatal(err)
	}

	expectCache(t, visitor, "/en/about", "", "miss", `alt="The harbour"`)

	// A save in a module's admin section.
	expectCache(t, visitor, "/en/news", "", "miss")
	expectCache(t, visitor, "/en/news", "", "hit")

	post := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title": {"Harbour festival"}, "Slug": {"harbour-festival"}, "Date": {"2026-10-01"}, "Body": {"Boats."},
	})
	s.Publish(post + "?locale=en")
	expectCache(t, visitor, "/en/news", "", "miss", "Harbour festival")

	// A visitor's comment. Every change invalidates every page, also
	// those of other modules. Posts that change nothing keep the pages:
	// spam (frontend.IsSpam) and a comment with mistakes (KeepForm). The
	// visitor who posts keeps a post with mistakes, and gets a page of
	// their own, so another reads the pages.
	reader := s.Visitor()

	expectCache(t, reader, "/en/about", "", "miss", "No comments yet.")
	expectCache(t, reader, "/en/about", "", "hit", "No comments yet.")

	unchanged := map[string]url.Values{
		"spam": {
			"subject": {"home"}, "name": {"Bot"}, "body": {"Cheap boats"}, frontend.Honeypot: {"https://spam.example"},
			"back": {"/en/about#remarks"},
		},
		"invalid": {
			"subject": {"home"}, "name": {"Ada"}, "body": {""}, "back": {"/en/about#remarks"},
		},
	}

	for name, values := range unchanged {
		status, _, _ := visitor.Send("/_/remarks/post", values)
		if status != http.StatusOK {
			t.Fatalf("%s comment: %d", name, status)
		}

		body := expectCache(t, reader, "/en/about", "", "hit", "No comments yet.")
		if strings.Contains(body, "Cheap boats") {
			t.Fatalf("%s comment is shown:\n%s", name, body)
		}
	}

	status, _, _ := visitor.Send("/_/remarks/post", url.Values{
		"subject": {"home"}, "name": {"Ada"}, "body": {"Lovely"}, "back": {"/en/about#remarks"},
	})
	if status != http.StatusOK {
		t.Fatalf("comment: %d", status)
	}

	expectCache(t, reader, "/en/about", "", "miss", "Lovely")

	// The metrics count what the cache did.
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer metrics-token")

	recorder := httptest.NewRecorder()
	s.App.ServeHTTP(recorder, request)

	for _, metric := range []string{
		"webthings_output_cache_hits_total ",
		"webthings_output_cache_misses_total ",
		"webthings_output_cache_skips_total ",
		"webthings_output_cache_entries ",
		"webthings_output_cache_bytes ",
	} {
		if !strings.Contains(recorder.Body.String(), "\n"+metric) || strings.Contains(recorder.Body.String(), "\n"+metric+"0\n") {
			t.Fatalf("metric %s:\n%s", metric, recorder.Body.String())
		}
	}
}

// pngBytes is a PNG image.
func pngBytes(t *testing.T) []byte {
	t.Helper()

	var buffer bytes.Buffer

	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 40, 30))); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

// changing is a module whose template function shows a text the test
// changes without a request, as content that changes with time does.
func changing(text *atomic.Value, env *webthings.Env) webthings.Module {
	return webthings.Module{
		Type: "changing",
		Key:  "changing",
		Funcs: template.FuncMap{
			"changingText": func() string {
				return text.Load().(string)
			},
		},
		Setup: func(e webthings.Env) (webthings.Parts, error) {
			*env = e

			return webthings.Parts{}, nil
		},
	}
}

// TestOutputCacheTime checks that a kept page shows what changed without a
// save once its time has passed, and at once after Env.Changed.
func TestOutputCacheTime(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"page.gohtml": `<main id="content"><p class="text">{{changingText}}</p></main>`,
	})

	var (
		text atomic.Value
		env  webthings.Env
	)

	text.Store("first")

	s := sitetest.StartWith(t, sitetest.Options{
		Templates: dir,
		Configure: cachedSite(300 * time.Millisecond),
	}, changing(&text, &env))

	visitor := s.Visitor()

	expectCache(t, visitor, "/en/", "", "miss", `<p class="text">first</p>`)

	text.Store("second")
	expectCache(t, visitor, "/en/", "", "hit", `<p class="text">first</p>`)

	time.Sleep(400 * time.Millisecond)
	expectCache(t, visitor, "/en/", "", "miss", `<p class="text">second</p>`)

	text.Store("third")
	env.Changed()
	expectCache(t, visitor, "/en/", "", "miss", `<p class="text">third</p>`)

	// The site's own code tells the app with App.Changed.
	text.Store("fourth")
	expectCache(t, visitor, "/en/", "", "hit", `<p class="text">third</p>`)
	s.App.Changed()
	expectCache(t, visitor, "/en/", "", "miss", `<p class="text">fourth</p>`)
}

// TestOutputCacheSitemap checks that the sitemap the output cache keeps
// follows the published pages: a page published in this process is in the
// next sitemap, and the cache keeps no older sitemap for the new version.
func TestOutputCacheSitemap(t *testing.T) {
	t.Parallel()

	s := sitetest.StartWith(t, sitetest.Options{Configure: cachedSite(time.Hour)})

	home, err := s.App.Pages.Home(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	visitor := s.Visitor()

	before := expectCache(t, visitor, "/sitemap.xml", "", "miss")
	expectCache(t, visitor, "/sitemap.xml", "", "hit")

	if strings.Contains(before, "/en/hours") {
		t.Fatalf("sitemap lists a page that is not there yet:\n%s", before)
	}

	hours := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title": {"Opening hours"}, "Slug": {"hours"}, "Parent": {home},
	})
	s.Publish(hours + "?locale=en")

	expectCache(t, visitor, "/sitemap.xml", "", "miss", "/en/hours</loc>")
	expectCache(t, visitor, "/sitemap.xml", "", "hit", "/en/hours</loc>")
}

// TestOutputCacheGoWrites checks that writes made from Go, outside a
// request, show at once: a module store's save and publication, a
// module service's write, and their move to the trash.
func TestOutputCacheGoWrites(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"page.gohtml": `<main id="content"><h1>{{.Title}}</h1>{{commentThread "remarks" "home"}}</main>`,
	})

	s := sitetest.StartWith(t, sitetest.Options{
		Templates:      dir,
		StartingPoints: []string{"news"},
		Configure:      cachedSite(time.Hour),
	}, publishing.Module("news", "News"), comments.Module("remarks", "Comments"))

	ctx := t.Context()

	s.Mount("news", "News", "news")

	visitor := s.Visitor()

	expectCache(t, visitor, "/en/news", "", "miss")
	expectCache(t, visitor, "/en/news", "", "hit")

	// A post saved and published from Go, as an import does.
	store := publishing.Of(s.App, "news").Store()

	post, err := store.Save(ctx, content.Draft[publishing.PostFields, publishing.PostText]{
		Fields: publishing.PostFields{Date: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
		Translations: []content.Translation[publishing.PostText]{
			{Locale: "en", Text: publishing.PostText{Title: "Imported news", Slug: "imported-news", Body: "From Go."}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// A saved draft changes nothing visitors see.
	expectCache(t, visitor, "/en/news", "", "hit")

	if err := store.Publish(ctx, post.ID, post.Revision.ID, "en"); err != nil {
		t.Fatal(err)
	}

	expectCache(t, visitor, "/en/news", "", "miss", "Imported news")
	expectCache(t, visitor, "/en/news", "", "hit", "Imported news")

	if err := store.Delete(ctx, post.ID); err != nil {
		t.Fatal(err)
	}

	body := expectCache(t, visitor, "/en/news", "", "miss")
	if strings.Contains(body, "Imported news") {
		t.Fatalf("a post in the trash is shown:\n%s", body)
	}

	// A module service's write: a comment added from Go.
	remarks := comments.Of(s.App, "remarks")
	if err := remarks.SaveSettings(ctx, comments.Settings{}); err != nil {
		t.Fatal(err)
	}

	expectCache(t, visitor, "/en/", "", "miss", "No comments yet.")
	expectCache(t, visitor, "/en/", "", "hit", "No comments yet.")

	_, err = remarks.Add(ctx, comments.Entry{
		Subject: "home", Name: "Ada", Body: "Imported remark", Locale: "en", Approved: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	expectCache(t, visitor, "/en/", "", "miss", "Imported remark")
}

// TestOutputCacheHTMXNavigation checks that HTMX navigations come from the
// cache by the page the visitor comes from, with the menu items that change
// for it, and that whole pages and navigations to one address are kept
// apart.
func TestOutputCacheHTMXNavigation(t *testing.T) {
	t.Parallel()

	s, ids := navigationSiteWith(t, sitetest.Options{
		Templates: navigationTheme(t, true),
		Configure: cachedSite(time.Hour),
	})
	visitor := s.Visitor()

	response, _ := visitor.Response("/en/")
	origin := "http://" + response.Request.URL.Host

	cases := []struct {
		from, cache string
		sent        int
	}{
		{"/en/about/team", "miss", 4},
		{"/en/about/team", "hit", 4},
		{"/en/work", "miss", 1},
		{"/en/work", "hit", 1},
		{"/en/about/team", "hit", 4},
	}

	var bodies []string

	for _, tc := range cases {
		response, body := navigate(t, visitor, origin+tc.from, "/en/work/tableware")
		if cache := response.Header.Get("X-Cache"); cache != tc.cache {
			t.Fatalf("from %s: X-Cache %q, want %q", tc.from, cache, tc.cache)
		}

		if !strings.Contains(strings.Join(response.Header.Values("Vary"), ","), "HX-Current-URL") {
			t.Fatalf("from %s: Vary %v", tc.from, response.Header.Values("Vary"))
		}

		if items, whole := menuItems(body, ids); whole || len(items) != tc.sent {
			t.Fatalf("from %s: items %v, whole %v: %s", tc.from, items, whole, body)
		}

		bodies = append(bodies, body)
	}

	if bodies[0] != bodies[1] || bodies[0] != bodies[4] || bodies[2] != bodies[3] {
		t.Fatal("a navigation from the cache differs from the one rendered")
	}

	// A whole page of the same address is a page of its own.
	_, page := cacheStatus(t, visitor, "/en/work/tableware", "")
	if !strings.Contains(page, "<header>") || strings.Contains(page, "hx-swap-oob") {
		t.Fatalf("whole page: %s", page)
	}

	expectCache(t, visitor, "/en/work/tableware", "", "hit", "<header>")
}

// TestOutputCacheMixedTraffic checks that a whole page and an HTMX
// navigation of the same address, whose responses vary on other request
// headers, are kept side by side: after one render each, both are served
// from the cache in turn, without evicting each other.
func TestOutputCacheMixedTraffic(t *testing.T) {
	t.Parallel()

	s, ids := navigationSiteWith(t, sitetest.Options{
		Templates: navigationTheme(t, true),
		Configure: cachedSite(time.Hour),
	})
	visitor := s.Visitor()

	response, _ := visitor.Response("/en/")
	from := "http://" + response.Request.URL.Host + "/en/about/team"

	for round, want := range []string{"miss", "hit", "hit"} {
		page := expectCache(t, visitor, "/en/work/tableware", "", want, "<header>")
		if strings.Contains(page, "hx-swap-oob") {
			t.Fatalf("round %d: the whole page has out-of-band items: %s", round, page)
		}

		response, body := navigate(t, visitor, from, "/en/work/tableware")
		if cache := response.Header.Get("X-Cache"); cache != want {
			t.Fatalf("round %d: navigation X-Cache %q, want %q", round, cache, want)
		}

		if items, whole := menuItems(body, ids); whole || len(items) != 4 {
			t.Fatalf("round %d: navigation items %v, whole %v: %s", round, items, whole, body)
		}
	}
}

// TestOutputCachePersonalCookie checks that a page that shows a visitor's
// vote is kept apart for the visitor's poll cookie, and that other visitors
// get the page without it.
func TestOutputCachePersonalCookie(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"page.gohtml": `<main id="content">{{pollBox "survey" ""}}</main>`,
	})

	s := sitetest.StartWith(t, sitetest.Options{
		Templates: dir,
		Configure: cachedSite(time.Hour),
	}, polls.Module("survey", "Polls"))

	ctx := context.Background()
	survey := s.App.Service("survey").(*polls.Service)

	poll, err := survey.Store().Save(ctx, content.Draft[polls.Fields, polls.Text]{
		Fields: polls.Fields{Options: polls.Lines{"tea", "coffee"}},
		Translations: []content.Translation[polls.Text]{{
			Locale: "en",
			Text:   polls.Text{Question: "Tea or coffee?", Options: polls.Lines{"Tea", "Coffee"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := survey.Store().Publish(ctx, poll.ID, "", "en"); err != nil {
		t.Fatal(err)
	}

	voter := s.Visitor()
	other := s.Visitor()

	expectCache(t, other, "/en/", "", "miss", `value="coffee"`)

	status, _, _ := voter.Send("/_/survey/vote", url.Values{"poll": {poll.ID}, "option": {"coffee"}, "back": {"/en/"}})
	if status != http.StatusOK {
		t.Fatalf("vote: %d", status)
	}

	chosen := `wt-poll__result--chosen`

	expectCache(t, voter, "/en/", "", "miss", chosen)
	expectCache(t, voter, "/en/", "", "hit", chosen)

	if body := expectCache(t, other, "/en/", "", "miss"); strings.Contains(body, chosen) {
		t.Fatalf("another visitor sees the vote:\n%s", body)
	}

	if body := expectCache(t, other, "/en/", "", "hit"); strings.Contains(body, chosen) {
		t.Fatalf("another visitor sees the vote from the cache:\n%s", body)
	}
}

// TestPublicPagesShared checks that no bundled module shows a visitor a
// page of their own for cookies that are not theirs to read: every module
// page, rendered without the output cache, is the same without cookies and
// with cookies of others, so the cache may keep it for every visitor.
func TestPublicPagesShared(t *testing.T) {
	t.Parallel()

	probe := webthings.New(webthings.Config{StateDir: t.TempDir()})
	if err := probe.Use(allModules()...); err != nil {
		t.Fatal(err)
	}

	keys := probe.TemplateKeys()[1:]

	s := sitetest.StartWith(t, sitetest.Options{StartingPoints: keys}, allModules()...)

	visitor := s.Visitor()

	for _, key := range keys {
		if key == "notice" {
			// Banners show on every page; they have no page of their own.
			continue
		}

		s.Mount(key, key, key)

		path := "/en/" + key

		_, plain := cacheStatus(t, visitor, path, "")
		_, other := cacheStatus(t, visitor, path, "_ga=GA1.2.3; session=abc; consent=yes")

		if plain != other {
			t.Fatalf("%s differs with other cookies:\n%s\n---\n%s", path, plain, other)
		}
	}
}
