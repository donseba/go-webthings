package testsuite

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/feed"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/site"
	"github.com/donseba/go-webthings/sitetest"
)

// dutchFeeds translates the title of feeds into Dutch.
const dutchFeeds = `msgid ""
msgstr ""
"Language: nl\n"
"Content-Type: text/plain; charset=UTF-8\n"

msgid "%[1]s from %[2]s"
msgstr "%[1]s van %[2]s"
`

// feedLayout is a layout with the links to the page's feeds in its head.
const feedLayout = `<!doctype html>
<html lang="{{.Locale}}">
<head><title>{{.Title}}</title>
{{feedLinks}}
</head>
<body>{{content}}</body>
</html>`

// atomDocument is what the tests read of an Atom feed.
type atomDocument struct {
	Lang    string     `xml:"http://www.w3.org/XML/1998/namespace lang,attr"`
	ID      string     `xml:"id"`
	Title   string     `xml:"title"`
	Updated string     `xml:"updated"`
	Author  string     `xml:"author>name"`
	Links   []atomLink `xml:"link"`
	Entries []struct {
		ID         string     `xml:"id"`
		Title      string     `xml:"title"`
		Links      []atomLink `xml:"link"`
		Summary    string     `xml:"summary"`
		Content    string     `xml:"content"`
		Published  string     `xml:"published"`
		Updated    string     `xml:"updated"`
		Categories []struct {
			Term string `xml:"term,attr"`
		} `xml:"category"`
	} `xml:"entry"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
	Href string `xml:"href,attr"`
}

// linkOf is the link with rel, or none.
func linkOf(links []atomLink, rel string) atomLink {
	for _, candidate := range links {
		if candidate.Rel == rel {
			return candidate
		}
	}

	return atomLink{}
}

// rssDocument is what the tests read of an RSS feed.
type rssDocument struct {
	Version string `xml:"version,attr"`
	Channel struct {
		Title string `xml:"title"`
		// Links are the channel's link and its atom:link, which has no
		// text.
		Links    []string `xml:"link"`
		Language string   `xml:"language"`
		Items    []struct {
			Title     string `xml:"title"`
			Link      string `xml:"link"`
			GUID      string `xml:"guid"`
			Content   string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
			Enclosure struct {
				URL  string `xml:"url,attr"`
				Type string `xml:"type,attr"`
			} `xml:"enclosure"`
		} `xml:"item"`
	} `xml:"channel"`
}

// fetch GETs path with the browser of s and the given request headers as
// name and value pairs, and returns the response with its body.
func fetch(t *testing.T, s *sitetest.Site, path string, headers ...string) (*http.Response, []byte) {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}

	response := s.Request(request)
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	return response, body
}

// readFeed GETs a feed, checks its status and Content-Type and decodes it
// into document, which also checks that it is well-formed XML.
func readFeed(t *testing.T, s *sitetest.Site, path string, format feed.Format, document any) *http.Response {
	t.Helper()

	response, body := fetch(t, s, path)
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != format.ContentType() {
		t.Fatalf("GET %s: %d %v\n%s", path, response.StatusCode, response.Header, body)
	}

	if err := xml.Unmarshal(body, document); err != nil {
		t.Fatalf("GET %s: %v\n%s", path, err, body)
	}

	return response
}

// TestPublishingFeed checks a Publishing instance's feeds end to end: none
// while it is on no page, then valid Atom and RSS of the published posts
// in each language with absolute addresses, the translated title, the
// post's text, categories and image, a feed per category, the links to
// them in the head of the module's pages, the caching of pages (ETag,
// Last-Modified, the output cache) and a new feed after publishing, and
// none of them in sitemap.xml.
func TestPublishingFeed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"templates/layout.gohtml": feedLayout,
		"translations/nl.po":      dutchFeeds,
	})

	translations, err := frontend.Translations(filepath.Join(dir, "translations"))
	if err != nil {
		t.Fatal(err)
	}

	s := sitetest.StartWith(t, sitetest.Options{
		Templates:      filepath.Join(dir, "templates"),
		StartingPoints: []string{"news"},
		Configure: func(config *webthings.Config) {
			cachedSite(time.Hour)(config)
			config.Translations = translations
		},
	}, publishing.Module("news", "News"))

	ctx := context.Background()
	visitor := s.Visitor()

	// An instance on no page has no feed.
	if response, _ := fetch(t, visitor, "/en/news/feed.atom"); response.StatusCode != http.StatusNotFound {
		t.Fatalf("feed of an instance on no page: %d", response.StatusCode)
	}

	s.MountIn("news",
		sitetest.Translation{Locale: "en", Title: "News", Slug: "news"},
		sitetest.Translation{Locale: "nl", Title: "Nieuws", Slug: "nieuws"},
	)

	news := publishing.Of(s.App, "news")
	photo := s.Upload("photo.jpg", sitetest.JPEG(t, 1600, 900))

	category, err := news.Categories().Save(ctx, content.Draft[struct{}, publishing.CategoryText]{
		Translations: []content.Translation[publishing.CategoryText]{
			{Locale: "en", Text: publishing.CategoryText{Title: "Ceramics", Slug: "ceramics"}},
			{Locale: "nl", Text: publishing.CategoryText{Title: "Keramiek", Slug: "keramiek"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := news.Categories().Publish(ctx, category.ID, "", "en"); err != nil {
		t.Fatal(err)
	}

	post, err := news.Store().Save(ctx, content.Draft[publishing.PostFields, publishing.PostText]{
		Fields: publishing.PostFields{
			Date:       time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			Image:      photo.ID,
			Categories: content.IDs{category.ID},
		},
		Translations: []content.Translation[publishing.PostText]{
			{Locale: "en", Text: publishing.PostText{
				Title:   "Open studio",
				Slug:    "open-studio",
				Excerpt: "Come by on Saturday.",
				Body:    "Clay & glaze.\n\n![](media:" + photo.ID + ")\n\n[About us](/en/about)",
			}},
			{Locale: "nl", Text: publishing.PostText{Title: "Open atelier", Slug: "open-atelier", Body: "Klei en glazuur."}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := news.Store().Publish(ctx, post.ID, post.Revision.ID, "en"); err != nil {
		t.Fatal(err)
	}

	response, _ := visitor.Response("/en/")
	origin := "http://" + response.Request.URL.Host

	// Atom, in English.
	var atom atomDocument

	first := readFeed(t, visitor, "/en/news/feed.atom", feed.Atom, &atom)

	if first.Header.Get("X-Cache") != "miss" || first.Header.Get("ETag") == "" ||
		first.Header.Get("Last-Modified") == "" || first.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("headers of the feed: %v", first.Header)
	}

	if atom.Lang != "en" || atom.Title != "News from Test site" || atom.Author != "Test site" ||
		atom.ID != origin+"/en/news/feed.atom" || linkOf(atom.Links, "self").Href != origin+"/en/news/feed.atom" ||
		linkOf(atom.Links, "alternate").Href != origin+"/en/news" || len(atom.Entries) != 1 {
		t.Fatalf("feed: %+v", atom)
	}

	entry := atom.Entries[0]

	terms := make([]string, 0, len(entry.Categories))
	for _, category := range entry.Categories {
		terms = append(terms, category.Term)
	}

	if !strings.HasPrefix(entry.ID, "tag:127.0.0.1,") || !strings.HasSuffix(entry.ID, ":news/"+post.ID) ||
		entry.Title != "Open studio" || linkOf(entry.Links, "alternate").Href != origin+"/en/news/open-studio" ||
		entry.Summary != "Come by on Saturday." || entry.Published != "2026-10-01T00:00:00Z" ||
		strings.Join(terms, ",") != "Ceramics" {
		t.Fatalf("entry: %+v", entry)
	}

	// The post's text with absolute addresses, its image as e-mail shows
	// it.
	enclosure := linkOf(entry.Links, "enclosure")

	for _, want := range []string{
		`<p>Clay &amp; glaze.</p>`,
		`<img src="` + origin + `/media/v/`,
		`href="` + origin + `/en/about"`,
	} {
		if !strings.Contains(entry.Content, want) {
			t.Fatalf("content misses %s:\n%s", want, entry.Content)
		}
	}

	if !strings.HasPrefix(enclosure.Href, origin+"/media/") || enclosure.Type != "image/jpeg" {
		t.Fatalf("enclosure: %+v", enclosure)
	}

	// The output cache keeps the feed; its ETag gets 304 Not Modified.
	again, _ := fetch(t, visitor, "/en/news/feed.atom")
	if again.Header.Get("X-Cache") != "hit" || again.Header.Get("ETag") != first.Header.Get("ETag") {
		t.Fatalf("second request: %v", again.Header)
	}

	unchanged, _ := fetch(t, visitor, "/en/news/feed.atom", "If-None-Match", first.Header.Get("ETag"))
	if unchanged.StatusCode != http.StatusNotModified {
		t.Fatalf("If-None-Match: %d", unchanged.StatusCode)
	}

	// RSS, in English.
	var rss rssDocument

	readFeed(t, visitor, "/en/news/feed.rss", feed.RSS, &rss)

	if rss.Version != "2.0" || rss.Channel.Title != "News from Test site" || rss.Channel.Links[0] != origin+"/en/news" ||
		rss.Channel.Language != "en" || len(rss.Channel.Items) != 1 {
		t.Fatalf("rss: %+v", rss)
	}

	item := rss.Channel.Items[0]
	if item.GUID != entry.ID || item.Link != origin+"/en/news/open-studio" ||
		!strings.Contains(item.Content, `href="`+origin+`/en/about"`) || item.Enclosure.URL != enclosure.Href {
		t.Fatalf("rss item: %+v", item)
	}

	// Dutch, with the Dutch title.
	var dutch atomDocument

	readFeed(t, visitor, "/nl/nieuws/feed.atom", feed.Atom, &dutch)

	if dutch.Lang != "nl" || dutch.Title != "Nieuws van Test site" || len(dutch.Entries) != 1 ||
		dutch.Entries[0].Title != "Open atelier" || dutch.Entries[0].Summary != "Klei en glazuur." ||
		linkOf(dutch.Entries[0].Links, "alternate").Href != origin+"/nl/nieuws/open-atelier" {
		t.Fatalf("dutch feed: %+v", dutch)
	}

	// A feed per category; one that does not exist is not found.
	var ceramics atomDocument

	readFeed(t, visitor, "/en/news/category/ceramics/feed.atom", feed.Atom, &ceramics)

	if ceramics.Title != "Ceramics from Test site" || len(ceramics.Entries) != 1 ||
		linkOf(ceramics.Links, "alternate").Href != origin+"/en/news/category/ceramics" ||
		ceramics.ID != origin+"/en/news/category/ceramics/feed.atom" {
		t.Fatalf("category feed: %+v", ceramics)
	}

	if response, _ := fetch(t, visitor, "/en/news/category/missing/feed.atom"); response.StatusCode != http.StatusNotFound {
		t.Fatalf("feed of a missing category: %d", response.StatusCode)
	}

	// The module's pages link to their feeds, the category's page to its
	// own as well; other pages to none.
	main := `<link rel="alternate" type="application/atom+xml" title="News from Test site" href="` + origin + `/en/news/feed.atom">` +
		"\n" + `<link rel="alternate" type="application/rss+xml" title="News from Test site" href="` + origin + `/en/news/feed.rss">`
	categoryLink := `<link rel="alternate" type="application/atom+xml" title="Ceramics from Test site" href="` +
		origin + `/en/news/category/ceramics/feed.atom">`

	visitor.Expect("/en/news", http.StatusOK, main)
	visitor.Expect("/en/news/open-studio", http.StatusOK, main)
	visitor.Expect("/en/news/category/ceramics", http.StatusOK, main, categoryLink)
	visitor.Expect("/nl/nieuws", http.StatusOK, `title="Nieuws van Test site" href="`+origin+`/nl/nieuws/feed.atom"`)
	visitor.ExpectMissing("/en/news", "/category/ceramics/feed.atom")
	visitor.ExpectMissing("/en/news/open-studio", "/category/ceramics/feed.atom")
	visitor.ExpectMissing("/en/", "application/atom+xml")

	// An error page links to no feed, such as a category that does not
	// exist, whose feed does not exist either.
	for _, missing := range []string{"/en/news/category/missing", "/en/news/missing"} {
		visitor.Expect(missing, http.StatusNotFound, "</head>")
		visitor.ExpectMissing(missing, "application/atom+xml")
	}

	// Publishing in the admin shows in the feed at once.
	added := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title": {"Harbour festival"}, "Slug": {"harbour-festival"}, "Date": {"2026-10-05"}, "Body": {"Boats."},
	})
	s.Publish(added + "?locale=en")

	var updated atomDocument

	fresh := readFeed(t, visitor, "/en/news/feed.atom", feed.Atom, &updated)
	if fresh.Header.Get("X-Cache") != "miss" || fresh.Header.Get("ETag") == first.Header.Get("ETag") ||
		len(updated.Entries) != 2 || updated.Entries[0].Title != "Harbour festival" ||
		updated.Entries[0].Summary != "Boats." {
		t.Fatalf("after publishing: %v %+v", fresh.Header, updated)
	}

	// Feeds are not pages to index.
	_, sitemap := visitor.Get("/sitemap.xml")
	if !strings.Contains(sitemap, "/en/news/open-studio") || strings.Contains(sitemap, "feed.atom") ||
		strings.Contains(sitemap, "feed.rss") {
		t.Fatalf("sitemap:\n%s", sitemap)
	}
}

// TestFeedLastModified checks that a feed's Last-Modified never goes back,
// so a reader that sends only If-Modified-Since is answered right, with the
// output cache and without it: once the newest of two posts is taken off
// the site, the date of the feed before gets the new feed, and the date of
// the new feed gets 304 Not Modified.
func TestFeedLastModified(t *testing.T) {
	t.Parallel()

	for name, configure := range map[string]func(*webthings.Config){
		"output cache": cachedSite(time.Hour),
		"no output cache": func(config *webthings.Config) {
			config.OutputCacheTTL = -1
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := sitetest.StartWith(t, sitetest.Options{
				StartingPoints: []string{"news"},
				Configure:      configure,
			}, publishing.Module("news", "News"))

			s.MountIn("news", sitetest.Translation{Locale: "en", Title: "News", Slug: "news"})

			ctx := context.Background()
			news := publishing.Of(s.App, "news")
			visitor := s.Visitor()

			var newest string

			for day, title := range []string{"Open studio", "Harbour festival"} {
				text := publishing.PostText{Title: title, Slug: strings.ToLower(strings.ReplaceAll(title, " ", "-"))}

				post, err := news.Store().Save(ctx, content.Draft[publishing.PostFields, publishing.PostText]{
					Fields:       publishing.PostFields{Date: time.Date(2026, 10, day+1, 0, 0, 0, 0, time.UTC)},
					Translations: []content.Translation[publishing.PostText]{{Locale: "en", Text: text}},
				})
				if err != nil {
					t.Fatal(err)
				}

				if err := news.Store().Publish(ctx, post.ID, post.Revision.ID, "en"); err != nil {
					t.Fatal(err)
				}

				newest = post.ID
			}

			var both atomDocument

			before := readFeed(t, visitor, "/en/news/feed.atom", feed.Atom, &both)
			old := before.Header.Get("Last-Modified")

			if len(both.Entries) != 2 || both.Entries[0].Title != "Harbour festival" || old == "" {
				t.Fatalf("feed before: %v %+v", before.Header, both)
			}

			// HTTP dates count seconds: the change comes a second later, so
			// the new feed has a date of its own.
			time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))

			if err := news.Store().Unpublish(ctx, newest); err != nil {
				t.Fatal(err)
			}

			after, body := fetch(t, visitor, "/en/news/feed.atom", "If-Modified-Since", old)

			var one atomDocument

			if err := xml.Unmarshal(body, &one); err != nil {
				t.Fatalf("feed after: %v\n%s", err, body)
			}

			oldDate, _ := http.ParseTime(old)
			newDate, err := http.ParseTime(after.Header.Get("Last-Modified"))

			if after.StatusCode != http.StatusOK || len(one.Entries) != 1 || one.Entries[0].Title != "Open studio" ||
				err != nil || !newDate.After(oldDate) {
				t.Fatalf("feed after: %d %v %+v", after.StatusCode, after.Header, one)
			}

			now := time.Now().UTC().Format(http.TimeFormat)

			unchanged, body := fetch(t, visitor, "/en/news/feed.atom", "If-Modified-Since", now)
			if unchanged.StatusCode != http.StatusNotModified || len(body) != 0 {
				t.Fatalf("unchanged feed: %d %v", unchanged.StatusCode, unchanged.Header)
			}
		})
	}
}

// ticker is a module with feeds, as any module offers them: a list on its
// page, a page per topic, the newest items of the instance and those of a
// topic; the topic "missing" is not found. limits records the limit each
// Build was given.
func ticker(key string, limits chan<- int) webthings.Module {
	return webthings.Module{
		Type: "ticker",
		Key:  key,
		Templates: []frontend.Template{
			{Name: "list", Data: "", Example: `<main id="content"><h1>{{.}}</h1></main>`},
		},
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			routes := env.Frontend.Router()

			list := func(w http.ResponseWriter, r *http.Request) {
				if r.PathValue("topic") == "missing" {
					env.Frontend.NotFound(w, r)

					return
				}

				title := frontend.MountFrom(r.Context()).Title
				if topic := r.PathValue("topic"); topic != "" {
					title = "Topic " + topic
				}

				env.Frontend.Render(w, r, frontend.Content{Template: "list", Title: title, Data: title})
			}

			routes.Get("/{$}", list).As("list")
			routes.Get("/topic/{topic}", list).As("topic")
			routes.Get("/item/{n}", list).As("item")

			build := func(r *http.Request, limit int) (feed.Feed, error) {
				limits <- limit

				topic := r.PathValue("topic")
				if topic == "missing" {
					return feed.Feed{}, fmt.Errorf("topic %s: %w", topic, db.ErrNotFound)
				}

				var built feed.Feed

				if topic != "" {
					built.Title = "Topic " + topic
				}

				// More items than the site's feeds list: the app leaves out
				// the rest. Links and images are paths on the site.
				for n := range 30 {
					item, _ := routes.For(r).RoutePathArgs("item", "n", n)

					built.Entries = append(built.Entries, feed.Entry{
						ID:        fmt.Sprintf("tag:example.com,2026-01-01:%s/%d", key, n),
						Title:     fmt.Sprintf("Item %d", n),
						Link:      item,
						Published: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(n) * time.Hour),
						Image:     feed.Enclosure{URL: "/theme/default/item.png"},
					})
				}

				return built, nil
			}

			return webthings.Parts{
				Public: routes,
				Feeds: []frontend.Feed{
					{Build: build},
					{Name: "topic-feed", Path: "/topic/{topic}/feed", Page: "topic", Build: build},
				},
			}, nil
		},
	}
}

// TestModuleFeeds checks the feeds of a module of any kind: served below
// its page at named routes, with what the module leaves empty filled in
// and its paths made absolute, at most as many entries as the site setting
// says, a 404 for what the module does not find, the links to them on its
// pages, and none for an instance on no page.
func TestModuleFeeds(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"layout.gohtml": feedLayout})

	limits := make(chan int, 100)

	s := sitetest.StartWith(t, sitetest.Options{
		Templates:      dir,
		StartingPoints: []string{"ticker"},
	}, ticker("ticker", limits), ticker("idle", limits))

	s.Mount("ticker", "Ticker", "ticker")

	visitor := s.Visitor()
	response, _ := visitor.Response("/en/")
	origin := "http://" + response.Request.URL.Host

	// What the module leaves empty is filled in; 20 items, the site's
	// default.
	var atom atomDocument

	readFeed(t, visitor, "/en/ticker/feed.atom", feed.Atom, &atom)

	if limit := <-limits; limit != 20 {
		t.Fatalf("limit %d", limit)
	}

	if atom.ID != origin+"/en/ticker/feed.atom" || atom.Title != "Ticker from Test site" || atom.Lang != "en" ||
		linkOf(atom.Links, "alternate").Href != origin+"/en/ticker" || len(atom.Entries) != 20 ||
		atom.Updated != "2026-01-01T19:00:00Z" {
		t.Fatalf("feed: %+v", atom)
	}

	if got := linkOf(atom.Entries[3].Links, "alternate").Href; got != origin+"/en/ticker/item/3" {
		t.Fatalf("entry link %s", got)
	}

	if got := linkOf(
		atom.Entries[3].Links,
		"enclosure",
	); got.Href != origin+"/theme/default/item.png" ||
		got.Type != "image/png" {
		t.Fatalf("enclosure %+v", got)
	}

	// The site setting changes how many.
	settings, err := s.App.Site.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	settings.FeedSize = 10

	if err := s.App.Site.Save(context.Background(), settings); err != nil {
		t.Fatal(err)
	}

	var rss rssDocument

	readFeed(t, visitor, "/en/ticker/feed.rss", feed.RSS, &rss)

	if limit := <-limits; limit != 10 || len(rss.Channel.Items) != 10 {
		t.Fatalf("limit %d, %d items", limit, len(rss.Channel.Items))
	}

	// A feed of a route with wildcards, linked from that route's pages.
	var topic atomDocument

	readFeed(t, visitor, "/en/ticker/topic/harbour/feed.atom", feed.Atom, &topic)
	<-limits

	if topic.Title != "Topic harbour from Test site" || topic.ID != origin+"/en/ticker/topic/harbour/feed.atom" {
		t.Fatalf("topic feed: %+v", topic)
	}

	visitor.Expect("/en/ticker/topic/harbour", http.StatusOK,
		`<link rel="alternate" type="application/atom+xml" title="Ticker from Test site" href="`+origin+`/en/ticker/feed.atom">`,
		`<link rel="alternate" type="application/rss+xml" title="Topic harbour from Test site" href="`+
			origin+`/en/ticker/topic/harbour/feed.rss">`,
	)
	visitor.ExpectMissing("/en/ticker/item/1", "/topic/")
	visitor.Expect("/en/ticker/item/1", http.StatusOK, origin+"/en/ticker/feed.rss")

	// What the module does not find is a 404, as is the feed of an
	// instance on no page, which no page links to.
	if response, _ := fetch(t, visitor, "/en/ticker/topic/missing/feed.atom"); response.StatusCode != http.StatusNotFound {
		t.Fatalf("missing topic: %d", response.StatusCode)
	}

	<-limits

	if response, _ := fetch(t, visitor, "/en/idle/feed.atom"); response.StatusCode != http.StatusNotFound {
		t.Fatalf("feed of an instance on no page: %d", response.StatusCode)
	}

	visitor.ExpectMissing("/en/", "/idle/")

	select {
	case limit := <-limits:
		t.Fatalf("the idle instance built a feed (limit %d)", limit)
	default:
	}
}

// limitedTicker is a ticker (see ticker) whose feeds list at most limit
// entries (frontend.Feed.Limit).
func limitedTicker(key string, limits chan<- int, limit int) webthings.Module {
	module := ticker(key, limits)
	setup := module.Setup

	module.Setup = func(env webthings.Env) (webthings.Parts, error) {
		parts, err := setup(env)

		for i := range parts.Feeds {
			parts.Feeds[i].Limit = limit
		}

		return parts, err
	}

	return module
}

// TestModuleFeedLimit checks that a feed's own Limit replaces the site's
// feed size, also after the site setting changes, while the feeds of
// another instance follow the site setting.
func TestModuleFeedLimit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"layout.gohtml": feedLayout})

	limits := make(chan int, 100)

	s := sitetest.StartWith(t, sitetest.Options{
		Templates:      dir,
		StartingPoints: []string{"ticker", "other"},
	}, limitedTicker("ticker", limits, 5), ticker("other", limits))

	s.Mount("ticker", "Ticker", "ticker")
	s.Mount("other", "Other", "other")

	visitor := s.Visitor()

	for _, size := range []int{0, 50} {
		settings, err := s.App.Site.Settings(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		settings.FeedSize = size

		if err := s.App.Site.Save(context.Background(), settings); err != nil {
			t.Fatal(err)
		}

		var limited, other rssDocument

		readFeed(t, visitor, "/en/ticker/topic/harbour/feed.rss", feed.RSS, &limited)

		if limit := <-limits; limit != 5 || len(limited.Channel.Items) != 5 {
			t.Fatalf("site size %d: limit %d, %d items", size, limit, len(limited.Channel.Items))
		}

		readFeed(t, visitor, "/en/other/feed.rss", feed.RSS, &other)

		want := settings.FeedLimit()
		if limit := <-limits; limit != want || len(other.Channel.Items) != min(want, 30) {
			t.Fatalf("site size %d, other instance: limit %d, %d items", size, limit, len(other.Channel.Items))
		}
	}
}

// feedURLLayout is a layout that shows the addresses {{feedURL}} gives on
// each page.
const feedURLLayout = `<!doctype html>
<html lang="{{.Locale}}">
<head><title>{{.Title}}</title></head>
<body>{{content}}<p>feeds:[{{feedURL "atom"}}|{{feedURL "rss"}}|{{feedURL "atom" "feed"}}|{{feedURL "json"}}]</p></body>
</html>`

// TestFeedURL checks {{feedURL}}: the feed of the page shown, the one of
// the page's own route before the module's, a feed by its name, and
// nothing for an unknown format, on a page without feeds or on an error
// page, such as a topic that does not exist.
func TestFeedURL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"layout.gohtml": feedURLLayout})

	limits := make(chan int, 100)

	s := sitetest.StartWith(t, sitetest.Options{
		Templates:      dir,
		StartingPoints: []string{"ticker"},
	}, ticker("ticker", limits))

	s.Mount("ticker", "Ticker", "ticker")

	visitor := s.Visitor()

	visitor.Expect("/en/ticker", http.StatusOK,
		"feeds:[/en/ticker/feed.atom|/en/ticker/feed.rss|/en/ticker/feed.atom|]")
	visitor.Expect("/en/ticker/topic/harbour", http.StatusOK,
		"feeds:[/en/ticker/topic/harbour/feed.atom|/en/ticker/topic/harbour/feed.rss|/en/ticker/feed.atom|]")
	visitor.Expect("/en/ticker/item/3", http.StatusOK,
		"feeds:[/en/ticker/feed.atom|/en/ticker/feed.rss|/en/ticker/feed.atom|]")
	visitor.Expect("/en/", http.StatusOK, "feeds:[|||]")
	visitor.Expect("/en/ticker/topic/missing", http.StatusNotFound, "feeds:[|||]")
}

// feedEngine is a Renderer that writes the feeds of the page itself, from
// Frontend.FeedLinks and Frontend.FeedURL, as an engine with tags of its
// own does. The frontend is the started app's.
type feedEngine struct {
	frontend *atomic.Pointer[frontend.Frontend]
}

func (feedEngine) Has(string) bool {
	return true
}

func (e feedEngine) Render(w http.ResponseWriter, r *http.Request, _ string, view frontend.View) error {
	shown := e.frontend.Load()

	var links []string

	for _, link := range shown.FeedLinks(r, view.Title) {
		links = append(links, link.URL)
	}

	_, err := fmt.Fprintf(w, "<html>feeds:[%s|%s]</html>", strings.Join(links, " "), shown.FeedURL(r, "atom"))

	return err
}

// TestFeedLinksOnErrorPage checks that Frontend.FeedLinks and
// Frontend.FeedURL give a Renderer no feed on an error page, such as a
// topic that does not exist, as {{feedLinks}} and {{feedURL}} do.
func TestFeedLinksOnErrorPage(t *testing.T) {
	t.Parallel()

	shown := &atomic.Pointer[frontend.Frontend]{}
	limits := make(chan int, 100)

	s := sitetest.StartWith(t, sitetest.Options{
		StartingPoints: []string{"ticker"},
		Configure: func(config *webthings.Config) {
			config.Templates = nil
			config.Renderer = feedEngine{frontend: shown}
		},
	}, ticker("ticker", limits))
	shown.Store(s.App.Frontend)

	s.Mount("ticker", "Ticker", "ticker")

	visitor := s.Visitor()

	visitor.Expect("/en/ticker/topic/harbour", http.StatusOK,
		"/en/ticker/topic/harbour/feed.atom", "|/en/ticker/topic/harbour/feed.atom]")
	visitor.Expect("/en/ticker/topic/missing", http.StatusNotFound, "feeds:[|]")
}

// TestModuleFeedsInvalid checks that the app refuses feeds it cannot serve
// when it starts.
func TestModuleFeedsInvalid(t *testing.T) {
	t.Parallel()

	build := func(*http.Request, int) (feed.Feed, error) {
		return feed.Feed{}, nil
	}

	cases := map[string]webthings.Parts{
		"not a router": {
			Public: http.NotFoundHandler(),
			Feeds:  []frontend.Feed{{Build: build}},
		},
		"wildcards on every page": {
			Feeds: []frontend.Feed{{Path: "/topic/{topic}/feed", Build: build}},
		},
		"negative limit": {
			Feeds: []frontend.Feed{{Limit: -1, Build: build}},
		},
		"limit above the maximum": {
			Feeds: []frontend.Feed{{Limit: site.MaxFeedSize + 1, Build: build}},
		},
	}

	for name, parts := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			app := webthings.New(webthings.Config{StateDir: t.TempDir()})

			err := app.Use(webthings.Module{
				Type: "broken",
				Key:  "broken",
				Setup: func(env webthings.Env) (webthings.Parts, error) {
					if parts.Public == nil {
						parts.Public = env.Frontend.Router()
					}

					return parts, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}

			err = app.Start(context.Background())
			t.Cleanup(func() { _ = app.Close() })

			if err == nil || !strings.Contains(err.Error(), "module broken") {
				t.Fatalf("Start: %v", err)
			}
		})
	}
}
