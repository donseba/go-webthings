package directory_test

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/modules/directory"
	"github.com/donseba/go-webthings/sitetest"
)

func TestDirectory(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, directory.Module("guide", "Guide"))
	ctx := context.Background()
	logo := s.Upload("logo.png", sitetest.PNG(t, 2, 2)).ID

	food := s.Submit("/admin/guide-categories/new?locale=en", url.Values{
		"Name":    {"Food"},
		"Summary": {"Eat and drink well."},
	})
	s.Submit(food+"?locale=nl", url.Values{"Name": {"Eten"}, "Summary": {"Lekker eten."}})
	s.Publish(food + "?locale=en")

	services := s.Submit("/admin/guide-categories/new?locale=en", url.Values{"Name": {"Services"}})
	s.Publish(services + "?locale=en")

	bakeryValues := url.Values{
		"Name":              {"Bakery Jansen"},
		"Summary":           {"Fresh bread every day."},
		"Description":       {"**Since 1920**"},
		"Keywords":          {"bread, croissant"},
		"Image":             {logo},
		"Website":           {"https://bakery.example"},
		"Email":             {"info@bakery.example"},
		"Phone":             {"020 123 4567"},
		"Address.Street":    {"Main street 1"},
		"Address.Postcode":  {"1011 AB"},
		"Address.City":      {"Amsterdam"},
		"Address.Latitude":  {"52.3731"},
		"Address.Longitude": {"4.8922"},
		"Hours.Note":        {"Closed on public holidays."},
	}

	// Open all day, every day, so the entry is open whenever the test runs.
	for _, day := range []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"} {
		bakeryValues.Set("Hours."+day+"Open", "00:00")
		bakeryValues.Set("Hours."+day+"Close", "00:00")
	}

	bakery := s.Submit("/admin/guide/new?locale=en", entryValues(path.Base(food), bakeryValues))

	// The editor shows the coordinates as numbers.
	s.Expect(bakery+"?locale=en", http.StatusOK, `type="number"`, `value="52.3731"`, `value="4.8922"`, `step="any"`)

	// Coordinates beyond the poles, or no number, are refused at their fields.
	for _, latitude := range []string{"91", "north"} {
		far := url.Values{"Name": {"Far away"}, "Address.Latitude": {latitude}, "Address.Longitude": {"4.9"}}

		code, body := s.PostStatus("/admin/guide/new?locale=en", "/admin/guide/new?locale=en", entryValues(path.Base(food), far))
		if code != http.StatusUnprocessableEntity || !strings.Contains(body, `aria-invalid="true"`) {
			t.Fatalf("latitude %s: %d\n%s", latitude, code, body)
		}
	}

	// A website that is no http or https address is refused at its field.
	badSite := url.Values{"Name": {"Bad link"}, "Website": {"javascript:alert(1)"}}

	code, body := s.PostStatus("/admin/guide/new?locale=en", "/admin/guide/new?locale=en", entryValues(path.Base(food), badSite))
	if code != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "The website must be a web address starting with http:// or https://.") {
		t.Fatalf("bad website: %d\n%s", code, body)
	}

	cheese := s.Submit("/admin/guide/new?locale=en", entryValues(path.Base(food), url.Values{
		"Name":     {"Cheese Shop"},
		"Summary":  {"Dutch cheese."},
		"Keywords": {"gouda"},
	}))

	s.Publish(bakery + "?locale=en")

	// While the directory is on no page, entries and categories have no
	// address instead of one that does not exist.
	unmounted, err := directory.Of(s.App, "guide").Entries(ctx, "en", directory.Query{})
	if err != nil || len(unmounted) != 1 || unmounted[0].URL != "" || unmounted[0].Category.URL != "" {
		t.Fatalf("unmounted entries: %+v %v", unmounted, err)
	}

	categories, err := directory.Of(s.App, "guide").ListCategories(ctx, "en")
	if err != nil || len(categories) == 0 || categories[0].URL != "" {
		t.Fatalf("unmounted categories: %+v %v", categories, err)
	}

	page := s.MountIn("guide",
		sitetest.Translation{Locale: "en", Title: "Guide", Slug: "guide"},
		sitetest.Translation{Locale: "nl", Title: "Gids", Slug: "gids"},
	)

	// The admin lists entries by name, which is not part of the text, with
	// their category in the language of the tab.
	s.Expect("/admin/guide?locale=en", http.StatusOK, ">Bakery Jansen</a>", "· Food</span>", ">Cheese Shop</a>", "Import CSV")
	s.Expect("/admin/guide?locale=nl", http.StatusOK, ">Bakery Jansen</a>", "· Eten</span>")

	// Only published entries in published categories with entries are listed.
	s.Expect("/en/guide", http.StatusOK,
		"<h1>Guide</h1>",
		`<a class="category" href="/en/guide/food">Food (1)</a>`,
		`<a class="entry" href="/en/guide/food/bakery-jansen">Bakery Jansen</a>: Fresh bread every day.`,
	)
	s.ExpectMissing("/en/guide", "Cheese Shop")
	s.ExpectMissing("/en/guide", "Services")

	s.Publish(cheese + "?locale=en")
	s.Expect("/en/guide", http.StatusOK, "Food (2)", "Cheese Shop", `<p id="count">2 found</p>`)

	// The search form and the link back to the list go to the page itself,
	// without a trailing slash.
	s.Expect("/en/guide", http.StatusOK, `<form action="/en/guide">`)
	s.Expect("/en/guide/food", http.StatusOK, `<a href="/en/guide">all</a>`)

	// Search matches name, summary and keywords, ignoring case and accents
	// like the site search.
	for _, query := range []string{"BAKERY", "fresh bread", "Croissant", "bakéry", "CROISSANT"} {
		s.Expect("/en/guide?q="+url.QueryEscape(query), http.StatusOK, "Bakery Jansen", `<p id="count">1 found</p>`)
		s.ExpectMissing("/en/guide?q="+url.QueryEscape(query), "Cheese Shop")
	}

	s.Expect("/en/guide?q=gouda", http.StatusOK, "Cheese Shop", `value="gouda"`)
	s.Expect("/en/guide?q=pizza", http.StatusOK, `<p id="count">0 found</p>`)
	s.Expect("/en/guide?category=food&q=cheese", http.StatusOK, `class="category active"`, "Cheese Shop")
	s.Expect("/en/guide?category=nothing", http.StatusNotFound)

	s.Expect("/en/guide/food", http.StatusOK, "<h1>Food</h1>", "Eat and drink well.", "Bakery Jansen", "Cheese Shop")
	s.Expect("/en/guide/services", http.StatusOK, "<h1>Services</h1>")
	s.Expect("/en/guide/nothing", http.StatusNotFound)

	s.Expect("/en/guide/food/bakery-jansen", http.StatusOK,
		"<h1>Bakery Jansen</h1>",
		"<strong>Since 1920</strong>",
		`<img src="/media/`,
		"https://bakery.example info@bakery.example 020 123 4567",
		"Main street 1, 1011 AB Amsterdam",
		"52.3731,4.8922",
		"<li>Monday: 00:00–00:00</li>",
		`<p id="open">open now</p>`,
		"Closed on public holidays.",
		"[bread][croissant]",
	)
	s.Expect("/en/guide/food/cheese-shop", http.StatusOK, "<h1>Cheese Shop</h1>")
	s.ExpectMissing("/en/guide/food/cheese-shop", `class="hours"`)
	s.ExpectMissing("/en/guide/food/cheese-shop", "open now")

	// An entry found below the wrong category redirects to its address.
	s.Expect("/en/guide/services/bakery-jansen", http.StatusOK, "<h1>Bakery Jansen</h1>")
	s.Expect("/en/guide/food/nothing", http.StatusNotFound)

	// Dutch shows the Dutch category name, and only entries saved in Dutch.
	s.Expect("/nl/gids", http.StatusOK, "<h1>Gids</h1>", `<p id="count">0 found</p>`)
	s.ExpectMissing("/nl/gids", "Bakery Jansen")

	s.Submit(bakery+"?locale=nl", entryValues(path.Base(food), url.Values{
		"Name":    {"Bakery Jansen"},
		"Summary": {"Elke dag vers brood."},
	}))
	s.Expect("/nl/gids", http.StatusOK, "Eten (1)", "Bakery Jansen</a>: Elke dag vers brood.")
	s.Expect("/nl/gids/food/bakery-jansen", http.StatusOK, `hreflang="en" href="/en/guide/food/bakery-jansen"`)

	// Template functions work on any page.
	s.Expect("/en/", http.StatusOK,
		"<li>Food (2)</li>",
		`<li><a href="/en/guide/food/bakery-jansen">Bakery Jansen</a></li>`,
		`<p id="featured">Bakery Jansen</p>`,
	)
	s.ExpectMissing("/en/", `<li><a href="/en/guide/food/cheese-shop">`)
	s.Expect("/en/", http.StatusOK, `<p id="all">2</p>`)

	s.Expect("/sitemap.xml", http.StatusOK, "/en/guide/food</loc>", "/en/guide/food/bakery-jansen</loc>")

	// The site search finds entries by name, summary, keywords and
	// description.
	for _, query := range []string{"jansen", "fresh", "croissant", "1920"} {
		s.Expect("/en/search?q="+query, http.StatusOK,
			`class="result directory"><a href="/en/guide/food/bakery-jansen">Bakery Jansen</a>`)
	}

	s.Expect("/en/search?q=gouda", http.StatusOK, `<a href="/en/guide/food/cheese-shop">Cheese Shop</a><p>Dutch cheese.</p>`)
	s.Expect("/nl/search?q=brood", http.StatusOK, `<a href="/nl/gids/food/bakery-jansen">Bakery Jansen</a>`)
	s.Expect("/nl/search?q=gouda", http.StatusOK, "nothing found")

	// Signed-in editors get links to the admin; visitors do not.
	s.Expect("/en/guide/food/bakery-jansen", http.StatusOK, `id="edit" href="`+bakery+`?locale=en"`)
	s.Expect("/en/guide/food", http.StatusOK, `id="edit" href="`+food+`?locale=en"`)
	s.Expect("/en/guide", http.StatusOK, `id="edit" href="/admin/guide?locale=en"`)
	s.Visitor().ExpectMissing("/en/guide/food/bakery-jansen", `id="edit"`)

	// A category with entries cannot be deleted; an empty one can.
	status, body := s.PostStatus(food+"?locale=en", food+"/delete?locale=en", url.Values{})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "This category still has entries.") {
		t.Fatalf("delete used category: %d\n%s", status, body)
	}

	s.Expect("/en/guide/food", http.StatusOK, "Bakery Jansen")
	s.Post(services+"?locale=en", services+"/delete?locale=en", url.Values{})
	s.Expect("/en/guide/services", http.StatusNotFound)

	entries, err := directory.Of(s.App, "guide").Entries(ctx, "en", directory.Query{Search: "dutch"})
	if err != nil || len(entries) != 1 || entries[0].URL != "/en/guide/food/cheese-shop" {
		t.Fatalf("entries: %+v %v", entries, err)
	}

	// A draft of the page does not change the list's title on the website.
	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	s.Submit(
		page+"?locale=en",
		url.Values{"Title": {"Secret new name"}, "Slug": {"guide"}, "Parent": {home}, "Module": {"guide"}},
	)
	s.Expect("/en/guide", http.StatusOK, "<h1>Guide</h1>")
	s.ExpectMissing("/en/guide", "Secret new name")

	testEditorErrors(t, s, path.Base(food))
	testImport(t, s)
}

func testEditorErrors(t *testing.T, s *sitetest.Site, category string) {
	t.Helper()

	status, body := s.PostStatus("/admin/guide/new?locale=en", "/admin/guide/new?locale=en", entryValues(category, url.Values{
		"Name": {"Bakery Jansen"},
	}))
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "This web address is already used.") {
		t.Fatalf("duplicate slug: %d\n%s", status, body)
	}

	status, body = s.PostStatus("/admin/guide/new?locale=en", "/admin/guide/new?locale=en", entryValues(category, url.Values{
		"Name":             {"Night shop"},
		"Hours.MondayOpen": {"22:00"},
	}))
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Fill in both times of a day") {
		t.Fatalf("half a day: %d\n%s", status, body)
	}
}

func testImport(t *testing.T, s *sitetest.Site) {
	t.Helper()

	s.Expect("/admin/guide/import", http.StatusOK, "name, category, summary, website, email, phone")

	csv := "name;category;summary;website;keywords;city\n" +
		"Town Hall;Services;Passports and permits;town.example;passport;Amsterdam\n" +
		"Bakery Jansen;Food;Duplicate\n" +
		"Library;Culture;Books\n"

	s.Post("/admin/guide/import", "/admin/guide/import", url.Values{"csv": {csv}, "publish": {"1"}})

	_, body := s.Get("/admin/guide")
	if !strings.Contains(body, "Town Hall") || !strings.Contains(body, "Library") {
		t.Fatalf("imported entries are not in the admin:\n%s", body)
	}

	status, page := s.PostStatus("/admin/guide/import", "/admin/guide/import", url.Values{
		"csv": {"name;category\nPost office;Services\nBakery Jansen;Food\n"},
	})
	if status != http.StatusOK || !strings.Contains(page, "Entries created: 1") ||
		!strings.Contains(page, "Rows skipped: 1") || !strings.Contains(page, "An entry with this name already exists.") {
		t.Fatalf("import result: %d\n%s", status, page)
	}

	// Published entries and the category created for them show on the site;
	// the unpublished import does not.
	s.Expect("/en/guide?q=passport", http.StatusOK, "Town Hall", `<p id="count">1 found</p>`)
	s.Expect("/en/guide/culture/library", http.StatusOK, "<h1>Library</h1>")
	s.ExpectMissing("/en/guide", "Post office")

	status, _ = s.PostStatus("/admin/guide/import", "/admin/guide/import", url.Values{"csv": {"title\nx\n"}})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("import without columns: %d", status)
	}

	// A file is read instead of the text field; other files are refused.
	status, page = s.PostFiles("/admin/guide/import", "/admin/guide/import", nil, sitetest.File{
		Field: "file", Name: "guide.csv", Data: []byte("name;category\nMuseum;Culture\n"),
	})
	if status != http.StatusOK || !strings.Contains(page, "Entries created: 1") {
		t.Fatalf("import of a file: %d\n%s", status, page)
	}

	status, page = s.PostFiles("/admin/guide/import", "/admin/guide/import", nil, sitetest.File{
		Field: "file", Name: "guide.pdf", Data: []byte("%PDF-1.7"),
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(page, `aria-invalid="true"`) {
		t.Fatalf("import of a PDF: %d\n%s", status, page)
	}
}

// entryValues are the editor fields of an entry with defaults for the fields
// the test does not care about.
func entryValues(category string, values url.Values) url.Values {
	defaults := url.Values{
		"Slug":     {""},
		"Category": {category},
		"Image":    {""},
	}

	for key, value := range defaults {
		if _, ok := values[key]; !ok {
			values[key] = value
		}
	}

	return values
}
