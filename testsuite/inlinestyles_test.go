package testsuite

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/modules/comments"
	"github.com/donseba/go-webthings/modules/directory"
	"github.com/donseba/go-webthings/modules/documents"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/gallery"
	"github.com/donseba/go-webthings/modules/lists"
	"github.com/donseba/go-webthings/modules/newsletter"
	"github.com/donseba/go-webthings/modules/people"
	"github.com/donseba/go-webthings/modules/polls"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/modules/registrations"
	"github.com/donseba/go-webthings/modules/testimonials"
	"github.com/donseba/go-webthings/sitetest"
)

// inlineStyle finds what a Content-Security-Policy without 'unsafe-inline'
// in style-src blocks: a style attribute or a <style> element.
var inlineStyle = regexp.MustCompile(`(?i)\sstyle=|<style[\s>]`)

// TestStartingPointsWithoutInlineStyles renders every kind of page of the
// starting points, of the site and of the bundled modules with their
// default components, with content, and checks that none has a style
// attribute or a <style> element.
func TestStartingPointsWithoutInlineStyles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// A page with the components of the modules and core's share bar.
	writeFiles(t, dir, map[string]string{
		"probe/screen.gohtml": `<main id="content"><h1>Components</h1>
{{commentThread "remarks" "home"}}
{{pollBox "survey" ""}}
{{newsletterForm "letters"}}
{{registrationForm "signup" "open-day"}}
{{shareBar "" "Components"}}
</main>`,
	})

	var asset string

	s := sitetest.StartWith(t, sitetest.Options{
		Templates: dir,
		StartingPoints: []string{
			"site", "news", "agenda", "photos", "faq", "team", "quotes", "guide", "docs",
			"remarks", "survey", "letters", "signup",
		},
	},
		publishing.Module("news", "News"),
		events.Module("agenda", "Agenda"),
		gallery.Module("photos", "Photos"),
		lists.Module("faq", "FAQ"),
		people.Module("team", "Team"),
		testimonials.Module("quotes", "Quotes"),
		directory.Module("guide", "Guide"),
		documents.Module("docs", "Documents"),
		comments.Module("remarks", "Comments"),
		polls.Module("survey", "Polls"),
		newsletter.Module("letters", "Newsletter"),
		registrations.Module("signup", "Sign-ups"),
		screens(&asset),
	)

	seedEveryModule(t, s)

	// A vote and a comment, so the results and the thread show.
	visitor := s.Visitor()

	poll := pollID(t, s)
	if status, _, _ := visitor.Send("/_/survey/vote", url.Values{
		"poll": {poll}, "option": {"coffee"}, "back": {"/en/probe"},
	}); status != http.StatusOK {
		t.Fatalf("vote: %d", status)
	}

	if status, _, _ := visitor.Send("/_/remarks/post", url.Values{
		"subject": {"home"}, "name": {"Ada"}, "body": {"Lovely place"}, "back": {"/en/probe"},
	}); status != http.StatusOK {
		t.Fatalf("comment: %d", status)
	}

	checked := crawlWithoutInlineStyles(t, visitor, "/en/", "/en/probe", "/en/search?q=hello", "/en/nope")

	// Every kind of page was there.
	for _, want := range []string{
		"/en/", "/en/probe", "/en/search?q=hello", "/en/nope",
		"/en/news", "/en/news/hello", "/en/news/category/updates", "/en/agenda", "/en/agenda/fair",
		"/en/photos", "/en/faq", "/en/team", "/en/quotes", "/en/guide", "/en/documents",
	} {
		if !checked[want] {
			t.Errorf("%s was not checked; checked %v", want, checked)
		}
	}

	if body := pageBody(t, visitor, "/en/probe"); !strings.Contains(body, `<meter class="wt-poll__bar"`) ||
		!strings.Contains(body, "Lovely place") {
		t.Fatalf("the components show no results:\n%s", body)
	}
}

// seedEveryModule mounts the modules of TestStartingPointsWithoutInlineStyles
// and gives each published content.
func seedEveryModule(t *testing.T, s *sitetest.Site) {
	t.Helper()

	ctx := context.Background()
	ids := map[string]string{}
	photo := s.Upload("harbour.png", sitetest.PNG(t, 1600, 900))
	boat := s.Upload("boat.png", sitetest.PNG(t, 800, 600))

	for _, mount := range [][2]string{
		{"news", "news"},
		{"agenda", "agenda"},
		{"photos", "photos"},
		{"faq", "faq"},
		{"team", "team"},
		{"quotes", "quotes"},
		{"guide", "guide"},
		{"docs", "documents"},
		{"probe", "probe"},
	} {
		s.Mount(mount[0], strings.ToUpper(mount[1][:1])+mount[1][1:], mount[1])
	}

	category := s.Submit("/admin/news-categories/new?locale=en", url.Values{"Title": {"Updates"}, "Slug": {"updates"}})
	s.Publish(category + "?locale=en")

	post := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title":      {"Hello"},
		"Slug":       {"hello"},
		"Date":       {"2026-10-01"},
		"Excerpt":    {"Boats arrived"},
		"Body":       {"The *boats* are in. ![](media:" + photo.ID + ")"},
		"Image":      {photo.ID},
		"Categories": {category[strings.LastIndex(category, "/")+1:]},
	})
	s.Publish(post + "?locale=en")

	day := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)
	event := s.Submit("/admin/agenda/new?locale=en", url.Values{
		"Title":    {"Fair"},
		"Slug":     {"fair"},
		"Start":    {day + "T14:30"},
		"End":      {day + "T16:00"},
		"Timezone": {"Europe/Amsterdam"},
		"Image":    {photo.ID},
	})
	s.Publish(event + "?locale=en")

	addGallery(s, ids, photo.ID, boat.ID)
	addLists(s, ids, photo.ID)
	addPeople(s, ids, photo.ID)
	addTestimonials(s, ids, photo.ID)
	addDirectory(s, ids, photo.ID)
	addDocuments(t, s, ids)

	remarks := s.App.Service("remarks").(*comments.Service)
	if err := remarks.SaveSettings(ctx, comments.Settings{Moderated: false}); err != nil {
		t.Fatal(err)
	}

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

	signup := s.App.Service("signup").(*registrations.Service)
	if err := signup.SaveSettings(ctx, registrations.Settings{
		SubjectID: "open-day", Title: "Open day", Open: true, Capacity: 10,
	}); err != nil {
		t.Fatal(err)
	}
}

// pollID is the ID of the newest published poll.
func pollID(t *testing.T, s *sitetest.Site) string {
	t.Helper()

	survey := s.App.Service("survey").(*polls.Service)

	newest, err := survey.Latest(context.Background(), nil, "en", 1)
	if err != nil || len(newest) != 1 {
		t.Fatalf("poll: %v %+v", err, newest)
	}

	return newest[0].ID
}

// crawlWithoutInlineStyles visits the pages from start on, and the pages
// of the site they link to, and fails for every one with an inline style.
// It returns the addresses it checked.
func crawlWithoutInlineStyles(t *testing.T, visitor *sitetest.Site, start ...string) map[string]bool {
	t.Helper()

	links := regexp.MustCompile(`href="(/(?:en|nl)/[^"#]*)"`)
	checked := map[string]bool{}
	queue := start

	for len(queue) > 0 && len(checked) < 120 {
		address := queue[0]
		queue = queue[1:]

		if checked[address] {
			continue
		}

		checked[address] = true

		response, body := visitor.Response(address)
		if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/html") {
			continue
		}

		if found := inlineStyle.FindAllString(body, -1); found != nil {
			t.Errorf("%s (%d) has inline styles %q:\n%s", address, response.StatusCode, found, body)
		}

		for _, link := range links.FindAllStringSubmatch(body, -1) {
			target := strings.ReplaceAll(link[1], "&amp;", "&")
			if strings.Contains(target, "/feed.") || strings.Contains(target, "/feed/") {
				continue
			}

			queue = append(queue, target)
		}
	}

	return checked
}

// pageBody is the body of a page.
func pageBody(t *testing.T, visitor *sitetest.Site, address string) string {
	t.Helper()

	_, body := visitor.Response(address)

	return body
}
