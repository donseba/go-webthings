package testsuite

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/people"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/modules/registrations"
	"github.com/donseba/go-webthings/site"
	"github.com/donseba/go-webthings/sitetest"
)

// ldOpen starts the structured data of a page.
const ldOpen = `<script type="application/ld+json">`

// structuredGraph is the @graph of the one JSON-LD script in body, by
// "@type"; ok is false when body has none. It fails on a second script,
// or JSON that does not parse.
func structuredGraph(t *testing.T, body string) (map[string]map[string]any, bool) {
	t.Helper()

	_, rest, ok := strings.Cut(body, ldOpen)
	if !ok {
		return nil, false
	}

	script, _, ok := strings.Cut(rest, "</script>")
	if !ok || strings.Contains(rest, ldOpen) {
		t.Fatalf("structured data: %s", body)
	}

	var data struct {
		Context string           `json:"@context"`
		Graph   []map[string]any `json:"@graph"`
	}

	if err := json.Unmarshal([]byte(script), &data); err != nil || data.Context != "https://schema.org" {
		t.Fatalf("structured data %v: %s", err, script)
	}

	graph := map[string]map[string]any{}

	for _, item := range data.Graph {
		kind, _ := item["@type"].(string)
		if _, double := graph[kind]; double {
			t.Fatalf("two %s items: %s", kind, script)
		}

		graph[kind] = item
	}

	return graph, true
}

// expectAbsolute fails when an address in value, in the properties that
// hold them, does not start with origin.
func expectAbsolute(t *testing.T, value any, key, origin string) {
	t.Helper()

	switch v := value.(type) {
	case map[string]any:
		for property, inner := range v {
			expectAbsolute(t, inner, property, origin)
		}
	case []any:
		for _, inner := range v {
			expectAbsolute(t, inner, key, origin)
		}
	case string:
		addresses := []string{"@id", "url", "image", "logo", "item", "urlTemplate"}
		if slices.Contains(addresses, key) && !strings.HasPrefix(v, origin) {
			t.Fatalf("%s %q is not absolute", key, v)
		}
	}
}

// field is the value at the path of properties in item, such as
// "breadcrumb", "@id".
func field(item map[string]any, path ...string) any {
	var value any = item

	for _, key := range path {
		inner, ok := value.(map[string]any)
		if !ok {
			return nil
		}

		value = inner[key]
	}

	return value
}

// TestStructuredData checks the JSON-LD the site starting point writes in
// <head> with {{structuredData}}: the site's WebSite and Organization, the
// trail and the WebPage of every page, the Article of a post in its
// language, an Event in its timezone with the offer of its registration
// and a Person; that every address is absolute, text cannot close the
// script, it follows an HTMX navigation and a missing page has none.
func TestStructuredData(t *testing.T) {
	t.Parallel()

	s := sitetest.StartWith(t, sitetest.Options{
		StartingPoints: []string{"site", "news", "agenda", "signup", "team"},
	},
		publishing.Module("news", "News"),
		events.Module("agenda", "Agenda"),
		registrations.Module("signup", "Sign-ups", registrations.ForEvents("agenda")),
		people.Module("team", "Team"),
	)
	ctx := context.Background()

	logo := s.Upload("logo.png", sitetest.PNG(t, 1000, 400))
	photo := s.Upload("photo.png", sitetest.PNG(t, 2400, 1600))

	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	settings.Organisation = "Clay Studio"
	settings.Email = "hello@example.test"
	settings.Phone = "+31 20 123 4567"
	settings.Address = "Main street 1, Amsterdam"
	settings.LogoMediaID = logo.ID
	settings.Profiles = []site.Profile{{Network: "instagram", URL: "https://instagram.com/claystudio"}}

	if err := s.App.Site.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}

	s.MountIn("news",
		sitetest.Translation{Locale: "en", Title: "News", Slug: "news"},
		sitetest.Translation{Locale: "nl", Title: "Nieuws", Slug: "nieuws"},
	)

	post := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title":   {"Kiln </script><b>hot</b>"},
		"Slug":    {"hello-world"},
		"Date":    {"2026-10-01"},
		"Excerpt": {"First firing"},
		"Image":   {photo.ID},
	})
	s.Publish(post + "?locale=en")
	s.Submit(post+"?locale=nl", url.Values{
		"Title": {"Hallo wereld"},
		"Slug":  {"hallo-wereld"},
		"Date":  {"2026-10-01"},
		"Image": {photo.ID},
	})
	s.Publish(post + "?locale=nl")

	visitor := s.Visitor()
	response, page := visitor.Response("/en/news/hello-world")
	origin := "http://" + response.Request.URL.Host

	graph, ok := structuredGraph(t, page)
	if !ok {
		t.Fatalf("no structured data: %s", page)
	}

	for _, kind := range []string{"WebSite", "Organization", "BreadcrumbList", "WebPage", "Article"} {
		if graph[kind] == nil {
			t.Fatalf("no %s in %v", kind, graph)
		}
	}

	for _, item := range graph {
		expectAbsolute(t, item, "", origin)
	}

	// The post's title is text in the JSON: no tag of it reaches the page.
	if strings.Contains(page, "<b>hot</b>") || strings.Count(page, "</script>") != strings.Count(page, "<script") {
		t.Fatalf("title escaped badly: %s", page)
	}

	website := graph["WebSite"]
	if website["name"] != "Test site" || website["inLanguage"] != "en" ||
		field(website, "potentialAction", "target", "urlTemplate") != origin+"/en/search?q={search_term_string}" ||
		field(website, "publisher", "@id") != origin+"/#organization" {
		t.Fatalf("website: %v", website)
	}

	organization := graph["Organization"]
	if organization["@id"] != origin+"/#organization" || organization["name"] != "Clay Studio" ||
		organization["email"] != "hello@example.test" || organization["telephone"] != "+31 20 123 4567" ||
		organization["address"] != "Main street 1, Amsterdam" ||
		!strings.HasPrefix(organization["logo"].(string), origin+"/") ||
		!slices.Equal(organization["sameAs"].([]any), []any{"https://instagram.com/claystudio"}) {
		t.Fatalf("organization: %v", organization)
	}

	// The 600 pixel logo of the site.
	logoPath := strings.TrimPrefix(organization["logo"].(string), origin)
	if format, width, _ := imageAt(t, s, logoPath); format != "png" || width != 600 {
		t.Fatalf("logo: %s %d", format, width)
	}

	canonical := origin + "/en/news/hello-world"

	trail, _ := graph["BreadcrumbList"]["itemListElement"].([]any)
	if len(trail) != 3 {
		t.Fatalf("trail: %v", graph["BreadcrumbList"])
	}

	for i, want := range []struct{ name, item string }{
		{"Home", origin + "/en/"},
		{"News", origin + "/en/news"},
		{"Kiln </script><b>hot</b>", canonical},
	} {
		step := trail[i].(map[string]any)
		if step["@type"] != "ListItem" || step["position"] != float64(i+1) ||
			step["name"] != want.name || step["item"] != want.item {
			t.Fatalf("trail %d: %v", i, step)
		}
	}

	webPage := graph["WebPage"]
	if webPage["@id"] != canonical || webPage["inLanguage"] != "en" ||
		field(webPage, "breadcrumb", "@id") != canonical+"#breadcrumb" ||
		field(webPage, "isPartOf", "@id") != origin+"/en/#website" {
		t.Fatalf("web page: %v", webPage)
	}

	article := graph["Article"]
	if article["headline"] != "Kiln </script><b>hot</b>" || article["description"] != "First firing" ||
		article["inLanguage"] != "en" || article["url"] != canonical || article["datePublished"] != "2026-10-01" ||
		field(article, "mainEntityOfPage", "@id") != canonical || field(article, "author", "@id") != origin+"/#organization" {
		t.Fatalf("article: %v", article)
	}

	if _, err := time.Parse(time.RFC3339, article["dateModified"].(string)); err != nil {
		t.Fatalf("modified: %v", article)
	}

	// The image of an article is the social image, 1200 × 630.
	imagePath := strings.TrimPrefix(article["image"].(string), origin)
	if format, width, height := imageAt(t, s, imagePath); format != "jpeg" || width != 1200 || height != 630 {
		t.Fatalf("article image: %s %d × %d", format, width, height)
	}

	// The post's page names its own translation, not the news page, and
	// itself as canonical; the Dutch one names the English post.
	for _, tag := range []string{
		`<link rel="canonical" href="` + canonical + `">`,
		`<link rel="alternate" hreflang="nl" href="` + origin + `/nl/nieuws/hallo-wereld">`,
		`<link rel="alternate" hreflang="en" href="` + canonical + `">`,
		`<link rel="alternate" hreflang="x-default" href="` + canonical + `">`,
	} {
		if !strings.Contains(page, tag) {
			t.Fatalf("no %s in %s", tag, page)
		}
	}

	_, dutch := visitor.Get("/nl/nieuws/hallo-wereld")
	dutchGraph, _ := structuredGraph(t, dutch)

	if dutchGraph["Article"]["inLanguage"] != "nl" || dutchGraph["WebPage"]["inLanguage"] != "nl" ||
		!strings.Contains(dutch, `<link rel="canonical" href="`+origin+`/nl/nieuws/hallo-wereld">`) ||
		!strings.Contains(dutch, `<link rel="alternate" hreflang="en" href="`+canonical+`">`) ||
		!strings.Contains(dutch, `<link rel="alternate" hreflang="x-default" href="`+canonical+`">`) {
		t.Fatalf("dutch post: %v\n%s", dutchGraph, dutch)
	}

	// An HTMX navigation brings the structured data of the page arrived at.
	_, body := navigate(t, visitor, canonical, "/en/")

	_, tags, ok := headTags(body)
	if !ok {
		t.Fatalf("no head region: %s", body)
	}

	// The home page has no trail, and no image without one.
	home, ok := structuredGraph(t, tags)
	if !ok || home["Article"] != nil || home["BreadcrumbList"] != nil || field(home["WebPage"], "@id") != origin+"/en/" ||
		home["WebPage"]["image"] != nil {
		t.Fatalf("home: %v", home)
	}

	// The news page ends its trail.
	_, news := visitor.Get("/en/news")
	newsGraph, _ := structuredGraph(t, news)

	if trail, _ := newsGraph["BreadcrumbList"]["itemListElement"].([]any); len(trail) != 2 {
		t.Fatalf("news trail: %v", newsGraph["BreadcrumbList"])
	}

	// The news page lists its posts, by their absolute addresses.
	list := newsGraph["ItemList"]

	var first map[string]any
	if elements, _ := list["itemListElement"].([]any); len(elements) == 1 {
		first, _ = elements[0].(map[string]any)
	}

	if list["name"] != "News" || list["numberOfItems"] != float64(1) || first["url"] != canonical ||
		first["position"] != float64(1) || field(list, "mainEntityOfPage", "@id") != origin+"/en/news" {
		t.Fatalf("news list: %v", list)
	}

	// The sitemap names the default language's address as x-default too.
	_, sitemap := visitor.Get("/sitemap.xml")
	if !strings.Contains(sitemap, `hreflang="x-default" href="`+origin+`/en/news"`) {
		t.Fatalf("sitemap: %s", sitemap)
	}

	_, body = navigate(t, visitor, origin+"/en/", "/en/news/hello-world")
	_, tags, _ = headTags(body)

	if back, _ := structuredGraph(t, tags); back["Article"] == nil {
		t.Fatalf("back to the post: %s", body)
	}

	// A page that is not found has none.
	if _, missing := visitor.Get("/en/news/nope"); strings.Contains(missing, ldOpen) {
		t.Fatalf("not found: %s", missing)
	}

	expectEvent(t, s, origin)
	expectPerson(t, s, origin, photo.ID)
}

// expectEvent checks an event's Event: its times in its own timezone,
// its place and, once visitors can register, its offer.
func expectEvent(t *testing.T, s *sitetest.Site, origin string) {
	t.Helper()

	day := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)

	editor := s.Submit("/admin/agenda/new?locale=en", url.Values{
		"Title":    {"Summer fair"},
		"Slug":     {"summer-fair"},
		"Start":    {day + "T14:30"},
		"End":      {day + "T16:00"},
		"Timezone": {"America/New_York"},
		"Summary":  {"Games and food"},
		"Location": {"Town hall"},
		"Address":  {"Main street 1"},
	})
	s.Publish(editor + "?locale=en")
	s.Mount("agenda", "Agenda", "agenda")

	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}

	start, err := time.ParseInLocation("2006-01-02T15:04", day+"T14:30", newYork)
	if err != nil {
		t.Fatal(err)
	}

	visitor := s.Visitor()
	_, page := visitor.Get("/en/agenda/summer-fair")
	graph, _ := structuredGraph(t, page)
	event := graph["Event"]

	if event["name"] != "Summer fair" || event["startDate"] != start.Format(time.RFC3339) ||
		event["endDate"] != start.Add(90*time.Minute).Format(time.RFC3339) ||
		event["eventStatus"] != "https://schema.org/EventScheduled" || event["inLanguage"] != "en" ||
		field(event, "location", "name") != "Town hall" || field(event, "location", "address") != "Main street 1" ||
		event["image"] != nil || event["offers"] != nil {
		t.Fatalf("event: %v", event)
	}

	// Open registration gives the event its offer.
	id := editor[strings.LastIndex(editor, "/")+1:]
	s.Submit("/admin/signup/"+id+"/settings", url.Values{"Open": {"on"}, "Capacity": {"3"}, "AdultPrice": {"12,50"}})

	_, page = visitor.Get("/en/agenda/summer-fair")
	graph, _ = structuredGraph(t, page)
	offer, _ := graph["Event"]["offers"].(map[string]any)

	if offer["@type"] != "Offer" || offer["price"] != "12.50" || offer["priceCurrency"] != "EUR" ||
		offer["availability"] != "https://schema.org/InStock" || offer["url"] != origin+"/en/agenda/summer-fair" {
		t.Fatalf("offer: %v", graph["Event"])
	}
}

// expectPerson checks a person's Person.
func expectPerson(t *testing.T, s *sitetest.Site, origin, photo string) {
	t.Helper()

	person := s.Submit("/admin/team/new?locale=en", url.Values{
		"Name":     {"Jane Doe"},
		"Role":     {"Potter"},
		"Photo":    {photo},
		"LinkedIn": {"https://www.linkedin.com/in/jane"},
	})
	s.Publish(person + "?locale=en")
	s.Mount("team", "Team", "team")

	_, page := s.Visitor().Get("/en/team/jane-doe")
	graph, _ := structuredGraph(t, page)
	jane := graph["Person"]

	if jane["name"] != "Jane Doe" || jane["jobTitle"] != "Potter" || jane["url"] != origin+"/en/team/jane-doe" ||
		!strings.HasPrefix(jane["image"].(string), origin+"/") || field(jane, "worksFor", "@id") != origin+"/#organization" ||
		!slices.Equal(jane["sameAs"].([]any), []any{"https://www.linkedin.com/in/jane"}) || jane["email"] != nil {
		t.Fatalf("person: %v", jane)
	}
}

// TestLanguagesBelowModulePage checks the language links of a module's
// screen below its page whose module gives no alternates, such as a
// ticker's topic: the module's page in the other languages, and the
// screen itself in its own, so its canonical and hreflang agree.
func TestLanguagesBelowModulePage(t *testing.T) {
	t.Parallel()

	limits := make(chan int, 100)

	s := sitetest.StartWith(t, sitetest.Options{
		StartingPoints: []string{"site", "ticker"},
	}, ticker("ticker", limits))

	s.MountIn("ticker",
		sitetest.Translation{Locale: "en", Title: "Ticker", Slug: "ticker"},
		sitetest.Translation{Locale: "nl", Title: "Tikker", Slug: "tikker"},
	)

	response, page := s.Visitor().Response("/en/ticker/topic/harbour")
	origin := "http://" + response.Request.URL.Host

	for _, tag := range []string{
		`<link rel="canonical" href="` + origin + `/en/ticker/topic/harbour">`,
		`<link rel="alternate" hreflang="en" href="` + origin + `/en/ticker/topic/harbour">`,
		`<link rel="alternate" hreflang="nl" href="` + origin + `/nl/tikker">`,
	} {
		if !strings.Contains(page, tag) {
			t.Fatalf("no %s in %s", tag, page)
		}
	}

	// The trail ends with the screen below the module's page.
	graph, _ := structuredGraph(t, page)
	trail, _ := graph["BreadcrumbList"]["itemListElement"].([]any)

	if len(trail) != 3 || trail[2].(map[string]any)["item"] != origin+"/en/ticker/topic/harbour" {
		t.Fatalf("trail: %v", graph["BreadcrumbList"])
	}
}
