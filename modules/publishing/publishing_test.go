package publishing_test

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

func TestPublishing(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, publishing.Module("news", "News"))

	category := s.Submit("/admin/news-categories/new?locale=en", url.Values{
		"Title": {"Updates"},
		"Slug":  {"updates"},
	})
	s.Publish(category + "?locale=en")

	categoryID := category[len("/admin/news-categories/"):]

	post := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title":      {"Hello world"},
		"Slug":       {"hello-world"},
		"Date":       {"2026-10-01"},
		"Excerpt":    {"First post"},
		"Body":       {"Some *news*"},
		"Categories": {categoryID},
	})

	// The categories are a group of checkboxes: a fieldset whose legend
	// names it, without a label that points to no element.
	s.Expect(post+"?locale=en", http.StatusOK, `<fieldset class="field">`, "<legend", `id="Categories_label">`,
		`<label class="field-label" for="Categories_0" id="Categories_0_label">Updates</label>`)
	s.ExpectMissing(post+"?locale=en", `for="Categories"`)

	s.Mount("news", "News", "news")
	s.Expect("/en/news", http.StatusOK, "<h1>News</h1>")
	s.ExpectMissing("/en/news", "hello-world")

	s.Publish(post + "?locale=en")

	s.Expect("/en/news", http.StatusOK, `href="/en/news/hello-world">Hello world`, `href="/en/news/category/updates">Updates`)
	s.Expect("/en/news/hello-world", http.StatusOK, "<em>news</em>", `hreflang="en"`)
	s.Expect("/en/news/category/updates", http.StatusOK, "<h1>Updates</h1>", "hello-world")
	s.Expect("/en/news/nope", http.StatusNotFound)
	s.Expect("/nl/news", http.StatusNotFound)

	// A Dutch translation of the post appears once the Dutch page exists too.
	s.Submit(post+"?locale=nl", url.Values{
		"Title":      {"Hallo wereld"},
		"Slug":       {"hallo-wereld"},
		"Date":       {"2026-10-01"},
		"Categories": {categoryID},
	})
	s.Publish(post + "?locale=nl")
	s.Expect("/en/news/hello-world", http.StatusOK, "Hello world")

	// The news page has no Dutch translation yet, so Dutch posts are not public.
	s.ExpectMissing("/sitemap.xml", "hallo-wereld")
	s.Expect("/sitemap.xml", http.StatusOK, "/en/news/hello-world</loc>", "/en/news/category/updates</loc>")
	s.Expect("/robots.txt", http.StatusOK, "Allow: /", "/sitemap.xml")

	// An unpublished category has no page, so posts stop linking to it.
	s.Post(category+"?locale=en", category+"/unpublish", url.Values{})
	s.Expect("/en/news/category/updates", http.StatusNotFound)
	s.ExpectMissing("/en/news", "/category/updates")
	s.Post(category+"?locale=en", category+"/publish", url.Values{})
	s.Expect("/en/news", http.StatusOK, `href="/en/news/category/updates"`)

	posts, err := publishing.Of(s.App, "news").Latest(context.Background(), "en", 5)
	if err != nil || len(posts) != 1 || posts[0].URL != "/en/news/hello-world" {
		t.Fatalf("latest: %+v %v", posts, err)
	}

	// Other modules propose drafts, which editors publish.
	id, err := publishing.Of(s.App, "news").Draft(context.Background(), publishing.DraftInput{
		Locale: "en",
		Title:  "Café opens again!",
		Body:   "From a *visitor*",
		Note:   "Ada <ada@example.test>",
	})
	if err != nil {
		t.Fatal(err)
	}

	draft, err := publishing.Of(s.App, "news").Store().Load(context.Background(), id, "")
	if err != nil || draft.Published || draft.Revision.Note != "Ada <ada@example.test>" {
		t.Fatalf("draft: %+v %v", draft, err)
	}

	if text, ok := draft.Text("en"); !ok || text.Slug != "cafe-opens-again" || draft.Fields.Date.IsZero() {
		t.Fatalf("draft text: %+v %+v", text, draft.Fields)
	}

	s.ExpectMissing("/en/news", "cafe-opens-again")
	s.Publish("/admin/news/" + id + "?locale=en")
	s.Expect("/en/news/cafe-opens-again", http.StatusOK, "<em>visitor</em>")

	if _, err := publishing.Of(s.App, "news").Draft(context.Background(), publishing.DraftInput{Locale: "en"}); err == nil {
		t.Fatal("draft without title accepted")
	}
}

// TestEmptyCategory checks that a published category without posts lists
// as that category: its title and its own address, for its feed links,
// rather than the list of the whole instance.
func TestEmptyCategory(t *testing.T) {
	dir := t.TempDir()

	list := `<main id="content"><h1>{{.Title}}</h1>` +
		`{{with .Category}}<p class="current">{{.Title}} {{.URL}} {{.Active}}</p>{{end}}` +
		`{{range .Categories}}<a class="category" href="{{.URL}}">{{.Title}}</a>{{end}}` +
		`{{range .Posts}}<a class="post" href="{{.URL}}">{{.Title}}</a>{{end}}</main>`

	if err := os.MkdirAll(filepath.Join(dir, "publishing"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "publishing", "list.gohtml"), []byte(list), 0o644); err != nil {
		t.Fatal(err)
	}

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir}, publishing.Module("news", "News"))

	for _, title := range []string{"Updates", "Empty"} {
		category := s.Submit("/admin/news-categories/new?locale=en", url.Values{
			"Title": {title},
			"Slug":  {strings.ToLower(title)},
		})
		s.Publish(category + "?locale=en")
	}

	s.Mount("news", "News", "news")

	s.Expect("/en/news/category/empty", http.StatusOK,
		"<h1>Empty</h1>",
		`<p class="current">Empty /en/news/category/empty true</p>`,
	)
	s.ExpectMissing("/en/news/category/empty", `class="post"`)

	// Its feeds are its own, also without entries.
	s.Expect("/en/news/category/empty/feed.atom", http.StatusOK, "<title>Empty from Test site</title>")
	s.Expect("/en/news/category/nope", http.StatusNotFound)
}

// TestPostDateIsCalendarDay checks that a post's date is shown as the day
// the editor chose, also on a site west of UTC, where midnight UTC is still
// the day before.
func TestPostDateIsCalendarDay(t *testing.T) {
	dir := t.TempDir()

	templates := map[string]string{
		"list.gohtml": `<main id="content"><h1>{{.Title}}</h1>` +
			`{{range .Posts}}<p class="date">{{date .Date "full"}}</p>{{end}}</main>`,
		"post.gohtml": `<main id="content"><h1>{{.Title}}</h1><p class="date">{{date .Date "full"}}</p></main>`,
	}

	if err := os.MkdirAll(filepath.Join(dir, "publishing"), 0o755); err != nil {
		t.Fatal(err)
	}

	for name, source := range templates {
		if err := os.WriteFile(filepath.Join(dir, "publishing", name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir}, publishing.Module("news", "News"))

	settings, err := s.App.Site.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	settings.Timezone = "America/New_York"
	if err := s.App.Site.Save(context.Background(), settings); err != nil {
		t.Fatal(err)
	}

	post := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title": {"Hello world"},
		"Slug":  {"hello-world"},
		"Date":  {"2026-10-01"},
	})
	s.Publish(post + "?locale=en")
	s.Mount("news", "News", "news")

	for _, address := range []string{"/en/news", "/en/news/hello-world"} {
		s.Expect(address, http.StatusOK, `<p class="date">Thursday 1 October 2026</p>`)
	}
}

func TestPublishingUnmountedAndDrafts(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, publishing.Module("news", "News"))
	ctx := context.Background()
	news := publishing.Of(s.App, "news")

	for _, slug := range []string{"one", "two", "three"} {
		post := s.Submit("/admin/news/new?locale=en", url.Values{
			"Title": {"Post " + slug},
			"Slug":  {slug},
			"Date":  {"2026-10-01"},
		})
		s.Publish(post + "?locale=en")
	}

	// While the instance is on no page, posts have no address instead of a
	// link to a page that does not exist.
	posts, err := news.Latest(ctx, "en", 2)
	if err != nil || len(posts) != 2 || posts[0].URL != "" {
		t.Fatalf("unmounted: %+v %v", posts, err)
	}

	// A limit of 0 or less returns every post and never fails.
	for _, limit := range []int{0, -1} {
		posts, err := news.Latest(ctx, "en", limit)
		if err != nil || len(posts) != 3 {
			t.Fatalf("limit %d: %+v %v", limit, posts, err)
		}
	}

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	page := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"News"},
		"Slug":   {"news"},
		"Parent": {home},
		"Module": {"news"},
	})
	s.Publish(page + "?locale=en")

	posts, err = news.Latest(ctx, "en", 1)
	if err != nil || len(posts) != 1 || !strings.HasPrefix(posts[0].URL, "/en/news/") {
		t.Fatalf("mounted: %+v %v", posts, err)
	}

	// A draft of the page does not change the title on the website until it
	// is published.
	s.Submit(page+"?locale=en", url.Values{
		"Title":  {"Secret new name"},
		"Slug":   {"news"},
		"Parent": {home},
		"Module": {"news"},
	})
	s.Expect("/en/news", http.StatusOK, "<h1>News</h1>")
	s.ExpectMissing("/en/news", "Secret new name")
}

// TestFeedSize checks that WithFeedSize sets how many posts the feeds
// list, those of the categories too, rather than the site setting.
func TestFeedSize(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"},
		publishing.Module("news", "News", publishing.WithFeedSize(2)))

	category := s.Submit("/admin/news-categories/new?locale=en", url.Values{
		"Title": {"Updates"},
		"Slug":  {"updates"},
	})
	s.Publish(category + "?locale=en")

	for i, slug := range []string{"one", "two", "three"} {
		post := s.Submit("/admin/news/new?locale=en", url.Values{
			"Title":      {"Post " + slug},
			"Slug":       {slug},
			"Date":       {"2026-10-0" + strconv.Itoa(i+1)},
			"Categories": {category[len("/admin/news-categories/"):]},
		})
		s.Publish(post + "?locale=en")
	}

	s.Mount("news", "News", "news")

	for _, address := range []string{"/en/news/feed.rss", "/en/news/category/updates/feed.rss"} {
		_, body := s.Visitor().Response(address)

		if got := strings.Count(body, "<item>"); got != 2 || !strings.Contains(body, "Post three") ||
			strings.Contains(body, "Post one") {
			t.Fatalf("%s: %d items\n%s", address, got, body)
		}
	}
}

// TestWebAddressFromTitle checks that a post and a category saved with an
// empty web address get one made from their title, as pages do, and that
// the editor calls the field "Web address".
func TestWebAddressFromTitle(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, publishing.Module("news", "News"))

	_, form := s.Get("/admin/news/new?locale=en")
	for field, want := range map[string]string{"Slug": "Web address", "Excerpt": "Summary", "Image": "Image"} {
		label := regexp.MustCompile(`id="` + field + `_label">\s*([^<]*?)\s*<`).FindStringSubmatch(form)
		if label == nil || label[1] != want {
			t.Errorf("the %s field is labelled %v, want %q", field, label, want)
		}
	}

	if !strings.Contains(form, "The last part of the address, such as open-studio in /en/news/open-studio. "+
		"Made from the title when left empty.") {
		t.Error("the web address has no help")
	}

	category := s.Submit("/admin/news-categories/new?locale=en", url.Values{"Title": {"Latest updates"}})
	s.Publish(category + "?locale=en")

	post := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title":      {"Café opens again!"},
		"Date":       {"2026-10-01"},
		"Categories": {category[len("/admin/news-categories/"):]},
	})
	s.Publish(post + "?locale=en")

	s.Mount("news", "News", "news")
	s.Expect("/en/news/cafe-opens-again", http.StatusOK, "Café opens again!")
	s.Expect("/en/news/category/latest-updates", http.StatusOK, "<h1>Latest updates</h1>")

	// A typed web address is kept.
	s.Submit(post+"?locale=en", url.Values{"Title": {"Café opens again!"}, "Slug": {"reopened"}, "Date": {"2026-10-01"}})
	s.Publish(post + "?locale=en")
	s.Expect("/en/news/reopened", http.StatusOK, "Café opens again!")
}
