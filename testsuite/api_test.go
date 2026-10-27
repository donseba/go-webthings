package testsuite

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/api"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

// updateAPI writes the golden files of the content API anew:
// go test ./testsuite -run TestContentAPI -update-api.
var updateAPI = flag.Bool("update-api", false, "write the content API's golden files")

// apiBase is the BaseURL of the sites of these tests, so the API's absolute
// addresses are the same in every run.
const apiBase = "https://example.test"

// apiResponse is an answer of the content API.
type apiResponse struct {
	status int
	header http.Header
	body   string
}

// apiGet sends a GET of path to s as a visitor, with the extra headers.
func apiGet(t *testing.T, s *sitetest.Site, path string, header ...string) apiResponse {
	t.Helper()

	return apiSend(t, s, http.MethodGet, path, header...)
}

// apiSend sends a request to s as a visitor, with header as pairs of
// names and values.
func apiSend(t *testing.T, s *sitetest.Site, method, path string, header ...string) apiResponse {
	t.Helper()

	request, err := http.NewRequest(method, path, nil)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i+1 < len(header); i += 2 {
		request.Header.Set(header[i], header[i+1])
	}

	response := s.Visitor().Request(request)
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	return apiResponse{status: response.StatusCode, header: response.Header, body: string(body)}
}

// expectAPI checks that a GET of path answers status with a JSON body, and
// returns the body decoded.
func expectAPI(t *testing.T, s *sitetest.Site, path string, status int) map[string]any {
	t.Helper()

	response := apiGet(t, s, path)
	if response.status != status {
		t.Fatalf("GET %s: status %d, want %d\n%s", path, response.status, status, response.body)
	}

	if media := response.header.Get("Content-Type"); media != "application/json; charset=utf-8" {
		t.Fatalf("GET %s: Content-Type %q\n%s", path, media, response.body)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(response.body), &decoded); err != nil {
		t.Fatalf("GET %s: %v\n%s", path, err, response.body)
	}

	return decoded
}

// expectAPIError checks that a GET of path answers the error status and
// code.
func expectAPIError(t *testing.T, s *sitetest.Site, path string, status int, code string) {
	t.Helper()

	body := expectAPI(t, s, path, status)

	failure, _ := body["error"].(map[string]any)
	if failure["code"] != code || failure["status"] != float64(status) {
		t.Fatalf("GET %s: error %v, want %d %s", path, body, status, code)
	}
}

// uuidPattern matches the IDs of content and media.
var uuidPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// variantPattern matches the signature in the address of a media variant,
// which differs per site.
var variantPattern = regexp.MustCompile(`/media/v/[A-Za-z0-9_-]+/`)

// apiTimes are the fields whose values differ per run: times relative to
// now.
var apiTimes = []string{"updated_at", "start", "end"}

// goldenAPI compares a GET of path to the golden file name, after making
// what differs per run the same: IDs become <id1>, <id2> and so on in the
// order they appear, times relative to now <time>.
func goldenAPI(t *testing.T, s *sitetest.Site, path, name string) {
	t.Helper()

	response := apiGet(t, s, path)
	if response.status != http.StatusOK {
		t.Fatalf("GET %s: status %d\n%s", path, response.status, response.body)
	}

	var decoded any
	if err := json.Unmarshal([]byte(response.body), &decoded); err != nil {
		t.Fatalf("GET %s: %v\n%s", path, err, response.body)
	}

	ids := map[string]string{}
	decoded = normalizeAPI(decoded, ids)

	var out bytes.Buffer

	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(decoded); err != nil {
		t.Fatal(err)
	}

	file := filepath.Join("testdata", "api", name+".json")

	if *updateAPI {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(file, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%v (write it with -update-api)", err)
	}

	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("GET %s differs from %s (write it anew with -update-api):\n%s", path, file, out.String())
	}
}

// normalizeAPI replaces what differs per run in a decoded value.
func normalizeAPI(value any, ids map[string]string) any {
	switch typed := value.(type) {
	case map[string]any:
		// In the order of the golden file, so IDs are numbered alike.
		for _, key := range slices.Sorted(maps.Keys(typed)) {
			field := typed[key]
			if slices.Contains(apiTimes, key) && field != nil {
				typed[key] = "<time>"

				continue
			}

			typed[key] = normalizeAPI(field, ids)
		}

		return typed
	case []any:
		for i, item := range typed {
			typed[i] = normalizeAPI(item, ids)
		}

		return typed
	case string:
		text := uuidPattern.ReplaceAllStringFunc(typed, func(id string) string {
			if _, ok := ids[id]; !ok {
				ids[id] = fmt.Sprintf("<id%d>", len(ids)+1)
			}

			return ids[id]
		})

		return variantPattern.ReplaceAllString(text, "/media/v/<sig>/")
	default:
		return value
	}
}

// greeter is a module of another repository that serves an endpoint of its
// own in the content API.
func greeter() webthings.Module {
	return webthings.Module{
		Type:  "greeter",
		Key:   "greeter",
		Title: "Greeter",
		Setup: func(webthings.Env) (webthings.Parts, error) {
			routes := api.NewRouter()

			routes.Get("/hello", func(w http.ResponseWriter, r *http.Request) {
				api.Write(w, r, map[string]string{
					"locale": api.Locale(r),
					"self":   api.URL(r, routes.RoutePath("hello")),
					"html":   api.HTML(r, "See [the news](/en/news)."),
				})
			}).As("hello")

			return webthings.Parts{API: routes}, nil
		},
	}
}

// contentSite is a site with the content API on, as configure changes it,
// with pages, a news instance and an agenda with published content and
// drafts in English and Dutch. It returns the IDs of the content by name.
func contentSite(t *testing.T, configure func(*webthings.Config)) (*sitetest.Site, map[string]string) {
	t.Helper()

	s := sitetest.StartWith(t, sitetest.Options{
		StartingPoints: []string{"news", "agenda"},
		Configure: func(config *webthings.Config) {
			config.BaseURL = apiBase
			config.API = api.Config{Enabled: true}

			if configure != nil {
				configure(config)
			}
		},
	}, publishing.Module("news", "News"), events.Module("agenda", "Agenda"), greeter())

	ids := map[string]string{}
	photo := s.Upload("harbour.png", sitetest.PNG(t, 1600, 900))

	for locale, alt := range map[string]string{"en": "Boats in the harbour", "nl": "Boten in de haven"} {
		err := s.App.Media.SaveTranslation(t.Context(), photo.ID, media.Translation{Locale: locale, AltText: alt})
		if err != nil {
			t.Fatal(err)
		}
	}

	home, err := s.App.Pages.Home(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	about := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":       {"About"},
		"Slug":        {"about"},
		"Description": {"Who we are"},
		"Parent":      {home},
		"Body":        {"Read the [news](/en/news).\n\n![The harbour](media:" + photo.ID + ")"},
	})
	s.Submit(about+"?locale=nl", url.Values{
		"Title":  {"Over ons"},
		"Slug":   {"over-ons"},
		"Parent": {home},
		"Body":   {"Lees het *nieuws*."},
	})
	s.Publish(about + "?locale=en")
	ids["about"] = path.Base(about)

	team := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title": {"Team"}, "Slug": {"team"}, "Parent": {ids["about"]}, "Body": {"The *team*."},
	})
	s.Publish(team + "?locale=en")

	// A page that is never published.
	s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"Secret"}, "Slug": {"secret"}, "Parent": {home}})

	s.MountIn("news", sitetest.Translation{Locale: "en", Title: "News", Slug: "news"},
		sitetest.Translation{Locale: "nl", Title: "Nieuws", Slug: "nieuws"})
	s.Mount("agenda", "Agenda", "agenda")

	category := s.Submit("/admin/news-categories/new?locale=en", url.Values{"Title": {"Updates"}, "Slug": {"updates"}})
	s.Submit(category+"?locale=nl", url.Values{"Title": {"Berichten"}, "Slug": {"berichten"}})
	s.Publish(category + "?locale=en")
	ids["category"] = path.Base(category)

	post := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title":      {"Hello harbour"},
		"Slug":       {"hello-harbour"},
		"Date":       {"2026-10-01"},
		"Excerpt":    {"Boats arrived"},
		"Body":       {"The *boats* are in. ![](media:" + photo.ID + ")"},
		"Image":      {photo.ID},
		"Categories": {ids["category"]},
	})
	s.Submit(post+"?locale=nl", url.Values{
		"Title":      {"Hallo haven"},
		"Slug":       {"hallo-haven"},
		"Date":       {"2026-10-01"},
		"Body":       {"De boten zijn er."},
		"Categories": {ids["category"]},
	})
	s.Publish(post + "?locale=en")
	ids["post"] = path.Base(post)

	draft := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title": {"Unpublished harbour"}, "Slug": {"unpublished"}, "Date": {"2026-10-02"}, "Body": {"Not yet."},
	})
	ids["draft"] = path.Base(draft)

	day := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)

	fair := s.Submit("/admin/agenda/new?locale=en", url.Values{
		"Title":    {"Harbour fair"},
		"Slug":     {"harbour-fair"},
		"Start":    {day + "T14:30"},
		"End":      {day + "T16:00"},
		"Timezone": {"Europe/Amsterdam"},
		"Summary":  {"Games and food"},
		"Body":     {"Bring *friends*."},
		"Location": {"Town hall"},
	})
	s.Publish(fair + "?locale=en")
	ids["event"] = path.Base(fair)

	planned := s.Submit("/admin/agenda/new?locale=en", url.Values{
		"Title": {"Planned fair"}, "Slug": {"planned-fair"}, "Start": {day + "T10:00"}, "Timezone": {"UTC"},
	})
	ids["planned"] = path.Base(planned)

	return s, ids
}

// TestContentAPI checks every endpoint of the content API in English and
// Dutch against golden files, and that drafts never show.
func TestContentAPI(t *testing.T) {
	t.Parallel()

	s, ids := contentSite(t, nil)

	for _, check := range []struct{ path, golden string }{
		{"/api/v1/site", "site.en"},
		{"/api/v1/site?locale=nl", "site.nl"},
		{"/api/v1/site/search?q=harbour", "search.en"},
		{"/api/v1/site/search?q=haven&locale=nl", "search.nl"},
		{"/api/v1/pages", "pages.en"},
		{"/api/v1/pages?locale=nl", "pages.nl"},
		{"/api/v1/pages/resolve?path=/en/about", "resolve.en"},
		{"/api/v1/pages/resolve?path=/nl/over-ons", "resolve.nl"},
		{"/api/v1/pages/resolve?path=/en/news/hello-harbour", "resolve.module"},
		{"/api/v1/pages/" + ids["about"] + "?locale=nl", "page.nl"},
		{"/api/v1/news/posts", "posts.en"},
		{"/api/v1/news/posts?locale=nl", "posts.nl"},
		{"/api/v1/news/posts/" + ids["post"], "post.en"},
		{"/api/v1/news/posts/" + ids["post"] + "?locale=nl", "post.nl"},
		{"/api/v1/news/categories?locale=nl", "categories.nl"},
		{"/api/v1/agenda/events", "events.en"},
		{"/api/v1/agenda/events/" + ids["event"], "event.en"},
		{"/api/v1/greeter/hello?locale=nl", "greeter.nl"},
	} {
		goldenAPI(t, s, check.path, check.golden)
	}

	// Drafts and pages that were never published do not show.
	expectAPIError(t, s, "/api/v1/pages/resolve?path=/en/secret", http.StatusNotFound, api.CodeNotFound)
	expectAPIError(t, s, "/api/v1/news/posts/"+ids["draft"], http.StatusNotFound, api.CodeNotFound)
	expectAPIError(t, s, "/api/v1/agenda/events/"+ids["planned"], http.StatusNotFound, api.CodeNotFound)

	for _, list := range []string{"/api/v1/news/posts", "/api/v1/agenda/events", "/api/v1/pages", "/api/v1/site/search?q=unpublished"} {
		if response := apiGet(t, s, list); strings.Contains(response.body, "Unpublished") ||
			strings.Contains(response.body, "Planned") || strings.Contains(response.body, "Secret") {
			t.Fatalf("GET %s shows a draft:\n%s", list, response.body)
		}
	}

	// The Team page is not translated into Dutch.
	expectAPIError(t, s, "/api/v1/pages/resolve?path=/nl/over-ons/team", http.StatusNotFound, api.CodeNotFound)

	// A language the site does not offer, content that is not there and
	// methods other than GET are not found, as JSON.
	expectAPIError(t, s, "/api/v1/site?locale=fr", http.StatusNotFound, api.CodeUnknownLocale)
	expectAPIError(t, s, "/api/v1/pages/resolve?path=/fr/about", http.StatusNotFound, api.CodeNotFound)
	expectAPIError(t, s, "/api/v1/pages/nope", http.StatusNotFound, api.CodeNotFound)
	expectAPIError(t, s, "/api/v1/nope", http.StatusNotFound, api.CodeNotFound)
	expectAPIError(t, s, "/api/v1/news/nope", http.StatusNotFound, api.CodeNotFound)
	expectAPIError(t, s, "/api/v1/news/posts?category=nope", http.StatusNotFound, api.CodeNotFound)

	if response := apiSend(t, s, http.MethodPost, "/api/v1/site"); response.status != http.StatusNotFound ||
		!strings.Contains(response.body, `"code":"not_found"`) {
		t.Fatalf("POST /api/v1/site: %d\n%s", response.status, response.body)
	}

	// The API answers JSON that browsers must not take for another type.
	response := apiGet(t, s, "/api/v1/site")
	if response.header.Get("X-Content-Type-Options") != "nosniff" || response.header.Get("ETag") == "" {
		t.Fatalf("GET /api/v1/site: headers %v", response.header)
	}

	// Pages of a list: limit and offset, with the address of the next page.
	page := expectAPI(t, s, "/api/v1/pages/resolve?path=/en/about", http.StatusOK)
	if page["title"] != "About" {
		t.Fatalf("resolve: %v", page)
	}

	list := expectAPI(t, s, "/api/v1/site/search?q=harbour&limit=1", http.StatusOK)
	if list["limit"] != float64(1) || len(list["items"].([]any)) != 1 || list["next"] == nil {
		t.Fatalf("search page: %v", list)
	}

	if next := list["next"].(string); !strings.HasPrefix(next, apiBase+"/api/v1/site/search?") ||
		!strings.Contains(next, "offset=1") {
		t.Fatalf("next page: %s", next)
	}

	clamped := expectAPI(t, s, "/api/v1/news/posts?limit=1000&offset=-3", http.StatusOK)
	if clamped["limit"] != float64(api.MaxLimit) || clamped["offset"] != float64(0) {
		t.Fatalf("clamped page: %v", clamped)
	}
}

// TestContentAPIOff checks that the API is off by default: /api/v1/ is then
// an address of the site, which has no page there.
func TestContentAPIOff(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t, publishing.Module("news", "News"))

	response := apiGet(t, s, "/api/v1/site")
	if response.status != http.StatusNotFound || strings.Contains(response.header.Get("Content-Type"), "json") {
		t.Fatalf(
			"GET /api/v1/site without the API: %d %s\n%s",
			response.status,
			response.header.Get("Content-Type"),
			response.body,
		)
	}

	if slices.Contains(s.App.CorePaths(), api.Path) || s.App.IsCorePath("/api/v1/site") {
		t.Fatalf("core paths without the API: %v", s.App.CorePaths())
	}
}

// TestContentAPIRoutes checks that the API's endpoints, also those of the
// modules, are routes of the app by name, and its address a core path.
func TestContentAPIRoutes(t *testing.T) {
	t.Parallel()

	s, ids := contentSite(t, nil)

	if !slices.Contains(s.App.CorePaths(), api.Path) || !s.App.IsCorePath("/api/v1/news/posts") {
		t.Fatalf("core paths with the API: %v", s.App.CorePaths())
	}

	for name, want := range map[string]string{
		"api.site":          "/api/v1/site",
		"api.search":        "/api/v1/site/search",
		"api.pages":         "/api/v1/pages",
		"api.resolve":       "/api/v1/pages/resolve",
		"api.greeter.hello": "/api/v1/greeter/hello",
		"api.news.posts":    "/api/v1/news/posts",
		"api.agenda.events": "/api/v1/agenda/events",
	} {
		if got := s.App.Frontend.Route(name); got != want {
			t.Errorf("route %s: %q, want %q", name, got, want)
		}
	}

	if got := s.App.Frontend.Route("api.news.post", "id", ids["post"]); got != "/api/v1/news/posts/"+ids["post"] {
		t.Errorf("route api.news.post: %q", got)
	}
}

// TestContentAPICORS checks the CORS headers: none without AllowedOrigins;
// with them, for the allowed sites only, also for answers of the output
// cache, which keeps one answer for every origin.
func TestContentAPICORS(t *testing.T) {
	t.Parallel()

	const (
		allowed = "https://app.example.test"
		other   = "https://other.example.test"
	)

	closed, _ := contentSite(t, nil)

	response := apiGet(t, closed, "/api/v1/site", "Origin", allowed)
	if response.status != http.StatusOK || response.header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("without AllowedOrigins: %d %v", response.status, response.header)
	}

	open, _ := contentSite(t, func(config *webthings.Config) {
		cachedSite(time.Hour)(config)
		config.API.AllowedOrigins = []string{allowed}
	})

	// The first answer is kept with the allowed origin's headers, which the
	// cache leaves to each request.
	for _, check := range []struct{ origin, cache, allow string }{
		{allowed, "miss", allowed},
		{"", "hit", ""},
		{allowed, "hit", allowed},
	} {
		response := apiGet(t, open, "/api/v1/news/posts", "Origin", check.origin)
		vary := strings.Join(response.header.Values("Vary"), ", ")

		if response.status != http.StatusOK || response.header.Get("X-Cache") != check.cache ||
			response.header.Get("Access-Control-Allow-Origin") != check.allow || !strings.Contains(vary, "Origin") {
			t.Fatalf("GET with Origin %q: %d %v", check.origin, response.status, response.header)
		}
	}

	// An answer first asked for without an Origin says Vary: Origin too.
	if response := apiGet(
		t,
		open,
		"/api/v1/site",
	); response.status != http.StatusOK ||
		!slices.Contains(response.header.Values("Vary"), "Origin") {
		t.Fatalf("GET without Origin: %d %v", response.status, response.header)
	}

	if response := apiGet(t, open, "/api/v1/site", "Origin", other); response.status != http.StatusForbidden {
		t.Fatalf("GET from another origin: %d %v", response.status, response.header)
	}

	preflight := apiSend(t, open, http.MethodOptions, "/api/v1/site",
		"Origin", allowed, "Access-Control-Request-Method", http.MethodGet)
	if preflight.status != http.StatusNoContent || preflight.header.Get("Access-Control-Allow-Origin") != allowed {
		t.Fatalf("preflight: %d %v", preflight.status, preflight.header)
	}
}

// TestContentAPIRateLimit checks that a client over the rate limit is
// answered 429 rate_limited, also for answers the output cache keeps, and
// that errors say so in the request's language.
func TestContentAPIRateLimit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"nl.po": `msgid ""
msgstr ""
"Language: nl\n"
"Content-Type: text/plain; charset=UTF-8\n"

msgid "Not Found"
msgstr "Niet gevonden"

msgid "Too Many Requests"
msgstr "Te veel verzoeken"
`,
	})

	translations, err := frontend.Translations(dir)
	if err != nil {
		t.Fatal(err)
	}

	s, _ := contentSite(t, func(config *webthings.Config) {
		cachedSite(time.Hour)(config)
		config.Translations = translations
		config.API.RateLimit = 4
	})

	missing := apiGet(t, s, "/api/v1/pages/nope?locale=nl")
	if !strings.Contains(missing.body, `"message":"Niet gevonden"`) {
		t.Fatalf("not found in Dutch: %s", missing.body)
	}

	for range 3 {
		expectAPI(t, s, "/api/v1/site?locale=nl", http.StatusOK)
	}

	expectAPIError(t, s, "/api/v1/site?locale=nl", http.StatusTooManyRequests, api.CodeRateLimited)

	response := apiGet(t, s, "/api/v1/pages?locale=nl")
	if response.header.Get("Retry-After") != "60" || !strings.Contains(response.body, `"message":"Te veel verzoeken"`) {
		t.Fatalf("over the limit: %v\n%s", response.header, response.body)
	}

	if english := apiGet(t, s, "/api/v1/pages"); !strings.Contains(english.body, `"message":"Too Many Requests"`) {
		t.Fatalf("over the limit in English: %s", english.body)
	}
}

// TestContentAPICache checks that the output cache keeps the API's answers,
// for editors too, but not those of a search with a query, and that
// publishing shows at once.
func TestContentAPICache(t *testing.T) {
	t.Parallel()

	s, _ := contentSite(t, cachedSite(time.Hour))

	// Browsers may keep the answers as long as the site's pages.
	settings, err := s.App.Site.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	settings.PageMaxAge = 60
	if err := s.App.Site.Save(t.Context(), settings); err != nil {
		t.Fatal(err)
	}

	expectCache(t, s, "/api/v1/news/posts", "", "miss", "Hello harbour")
	expectCache(t, s, "/api/v1/news/posts", "", "hit", "Hello harbour")
	expectCache(t, s, "/api/v1/site/search?q=harbour", "", "skip", "Hello harbour")

	if search := apiGet(t, s, "/api/v1/site/search?q=harbour"); search.header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("search: Cache-Control %q", search.header.Get("Cache-Control"))
	}

	// An ETag answers a client that has the answer already.
	response := apiGet(t, s, "/api/v1/news/posts")
	again := apiGet(t, s, "/api/v1/news/posts", "If-None-Match", response.header.Get("ETag"))

	if again.status != http.StatusNotModified || response.header.Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("conditional GET: %d, Cache-Control %q", again.status, response.header.Get("Cache-Control"))
	}

	post := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title": {"Second harbour"}, "Slug": {"second-harbour"}, "Date": {"2026-10-05"}, "Body": {"More boats."},
	})
	s.Publish(post + "?locale=en")

	expectCache(t, s, "/api/v1/news/posts", "", "miss", "Second harbour", "Hello harbour")
}

// TestContentAPILastModified checks the Last-Modified of a post, an event
// and a page: the latest date of what the answer shows, which answers
// If-Modified-Since with 304 also from the output cache, moves on when a
// category, an alt text or a page above changes, and is left out for an
// answer with something undated, such as a deleted category, and for
// lists.
func TestContentAPILastModified(t *testing.T) {
	t.Parallel()

	s, ids := contentSite(t, cachedSite(time.Hour))

	team, err := s.App.Pages.Resolve(t.Context(), "en", "/about/team")
	if err != nil {
		t.Fatal(err)
	}

	endpoints := map[string]string{
		"post":    "/api/v1/news/posts/" + ids["post"],
		"event":   "/api/v1/agenda/events/" + ids["event"],
		"about":   "/api/v1/pages/" + ids["about"],
		"resolve": "/api/v1/pages/resolve?path=/en/about",
		"team":    "/api/v1/pages/" + team.Page.ID,
	}

	modified := map[string]time.Time{}

	for name, address := range endpoints {
		// Twice: the output cache misses, then answers from its entry.
		for range 2 {
			response := apiGet(t, s, address)

			date, err := http.ParseTime(response.header.Get("Last-Modified"))
			if response.status != http.StatusOK || err != nil || date.After(time.Now()) {
				t.Fatalf("GET %s: %d, Last-Modified %q", address, response.status, response.header.Get("Last-Modified"))
			}

			modified[name] = date

			since := date.Format(http.TimeFormat)
			if again := apiGet(
				t,
				s,
				address,
				"If-Modified-Since",
				since,
			); again.status != http.StatusNotModified ||
				again.body != "" {
				t.Fatalf("GET %s since %s: %d", address, since, again.status)
			}

			earlier := date.Add(-time.Second).Format(http.TimeFormat)
			if again := apiGet(t, s, address, "If-Modified-Since", earlier); again.status != http.StatusOK {
				t.Fatalf("GET %s since %s: %d", address, earlier, again.status)
			}
		}
	}

	if modified["about"] != modified["resolve"] {
		t.Fatalf("about %v, resolved %v", modified["about"], modified["resolve"])
	}

	// Lists have none.
	for _, list := range []string{"/api/v1/news/posts", "/api/v1/agenda/events", "/api/v1/pages", "/api/v1/site"} {
		if response := apiGet(t, s, list); response.header.Get("Last-Modified") != "" {
			t.Fatalf("GET %s: Last-Modified %q", list, response.header.Get("Last-Modified"))
		}
	}

	// HTTP dates count seconds.
	time.Sleep(1100 * time.Millisecond)

	// expect checks which answers have a newer date than before.
	expect := func(change string, newer ...string) {
		t.Helper()

		for name, address := range endpoints {
			response := apiGet(t, s, address)

			date, err := http.ParseTime(response.header.Get("Last-Modified"))
			if err != nil {
				t.Fatalf("%s: GET %s: Last-Modified %q", change, address, response.header.Get("Last-Modified"))
			}

			if moved := date.After(modified[name]); moved != slices.Contains(newer, name) {
				t.Fatalf("%s: %s from %v to %v", change, name, modified[name], date)
			}

			since := modified[name].Format(http.TimeFormat)
			again := apiGet(t, s, address, "If-Modified-Since", since)

			if again.status == http.StatusNotModified && slices.Contains(newer, name) {
				t.Fatalf("%s: GET %s since %s: 304", change, address, since)
			}
		}
	}

	// The post's category.
	category := "/admin/news-categories/" + ids["category"]
	s.Submit(category+"?locale=en", url.Values{"Title": {"News updates"}, "Slug": {"updates"}})
	expect("category", "post")

	// The alt text of the photo in the text of the page and of the post.
	assets, err := s.App.Media.List(t.Context())
	if err != nil || len(assets) != 1 {
		t.Fatalf("media: %v %v", assets, err)
	}

	photo := assets[0].ID

	err = s.App.Media.SaveTranslation(t.Context(), photo, media.Translation{Locale: "en", AltText: "The harbour at dawn"})
	if err != nil {
		t.Fatal(err)
	}

	expect("alt text", "post", "about", "resolve")

	// A page above the team page, whose title its breadcrumbs show; the
	// dates go on from those before, so what moved before stays newer.
	home, err := s.App.Pages.Home(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	about := "/admin/pages/" + ids["about"]
	s.Submit(about+"?locale=en", url.Values{
		"Title":  {"About us"},
		"Slug":   {"about"},
		"Parent": {home},
		"Body":   {"Read the [news](/en/news).\n\n![The harbour](media:" + photo + ")"},
	})
	s.Publish(about + "?locale=en")
	expect("parent page", "post", "about", "resolve", "team")

	// A deleted category leaves the post without a date.
	if err := publishing.Of(s.App, "news").Categories().Delete(t.Context(), ids["category"]); err != nil {
		t.Fatal(err)
	}

	if response := apiGet(
		t,
		s,
		endpoints["post"],
	); response.status != http.StatusOK ||
		response.header.Get("Last-Modified") != "" {
		t.Fatalf("post with a deleted category: %d, Last-Modified %q", response.status, response.header.Get("Last-Modified"))
	}
}

// TestContentAPILink checks {{apiLink}}: a link to the page shown in the
// API, the page itself by its ID and an address below a module's page
// resolved, and none on an error page, also one of a module, or while the
// API is off.
func TestContentAPILink(t *testing.T) {
	t.Parallel()

	s, ids := contentSite(t, nil)
	visitor := s.Visitor()

	visitor.Expect("/en/about", http.StatusOK,
		`<link rel="alternate" type="application/json" href="`+apiBase+`/api/v1/pages/`+ids["about"]+`?locale=en">`)
	visitor.Expect("/nl/over-ons", http.StatusOK,
		`<link rel="alternate" type="application/json" href="`+apiBase+`/api/v1/pages/`+ids["about"]+`?locale=nl">`)
	visitor.Expect(
		"/en/news/hello-harbour",
		http.StatusOK,
		`<link rel="alternate" type="application/json" href="`+apiBase+`/api/v1/pages/resolve?path=%2Fen%2Fnews%2Fhello-harbour">`,
	)
	visitor.ExpectMissing("/en/nope", "application/json")

	// An error page of a module, which is on a page: the API does not find
	// its address either.
	for _, missing := range []string{"/en/news/nope", "/en/news/category/nope"} {
		visitor.Expect(missing, http.StatusNotFound, "</head>")
		visitor.ExpectMissing(missing, "application/json")
	}

	off := sitetest.Start(t, publishing.Module("news", "News"))
	off.Visitor().ExpectMissing("/en/", "application/json")
}
