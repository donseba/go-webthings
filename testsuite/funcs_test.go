package testsuite

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/gallery"
	"github.com/donseba/go-webthings/modules/lists"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

// TestTemplateFunctions shows a frontend adding content anywhere: the home
// page and the blog list both show the latest news, through a shared partial,
// without Go code in the site.
func TestTemplateFunctions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"layout.gohtml": `<body>{{content}}</body>`,
		"page.gohtml": `<main id="content"><h1>{{.Title}}</h1>
{{range posts "news" 2}}{{template "partials/teaser.gohtml" .}}{{end}}
<a class="all" href="{{moduleURL "news"}}">all news</a></main>`,
		"partials/teaser.gohtml": `<a class="teaser" href="{{.URL}}">{{.Title}}</a>`,
		"publishing/list.gohtml": `<main id="content"><h1>{{.Title}}</h1>
{{range .Posts}}<a class="post" href="{{.URL}}">{{.Title}}</a>{{end}}
<aside>{{range posts "news" 1}}{{template "partials/teaser.gohtml" .}}{{end}}</aside></main>`,
		"publishing/post.gohtml": `<main id="content">{{.Title}}</main>`,
	})

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir, Prepare: func(*webthings.App) error { return nil }},
		publishing.Module("news", "News"),
		publishing.Module("blog", "Blog"),
	)

	s.Mount("news", "News", "news")
	s.Mount("blog", "Blog", "blog")

	for _, title := range []string{"First", "Second", "Third"} {
		post := s.Submit("/admin/news/new?locale=en", url.Values{
			"Title": {title},
			"Slug":  {"post-" + strings.ToLower(title)},
			"Date":  {"2026-10-0" + map[string]string{"First": "1", "Second": "2", "Third": "3"}[title]},
		})
		s.Publish(post + "?locale=en")
	}

	// The home page shows the two newest news posts and links to the news page.
	s.Expect("/en/", http.StatusOK,
		`<a class="teaser" href="/en/news/post-third">Third</a>`,
		`<a class="teaser" href="/en/news/post-second">Second</a>`,
		`<a class="all" href="/en/news">`,
	)
	s.ExpectMissing("/en/", "post-first")

	// The blog list has its own posts (none yet) and the latest news on the side.
	s.Expect("/en/blog", http.StatusOK, `<aside><a class="teaser" href="/en/news/post-third">Third</a></aside>`)
}

// TestModuleTemplateFunctions uses the template functions of gallery,
// events and lists, and core's srcset and weekday, on a page that shows
// none of those modules.
func TestModuleTemplateFunctions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"page.gohtml": `<main id="content"><h1>{{.Title}}</h1>
{{range albums "photos"}}<p class="albums">{{.Title}} {{.ImageCount}} {{.URL}}</p>{{end}}
{{with album "photos" "holiday"}}{{range .Images}}<img alt="{{.Alt}}" srcset="{{srcset .URL 100 200}}">{{end}}{{end}}
{{with album "photos" "missing"}}<p class="missing">{{.Title}}</p>{{end}}
{{range events "agenda" 1}}<p class="event">{{.Title}} on {{weekday .Start.Weekday}}</p>{{end}}
{{range entries "faq"}}<p class="entry">{{.Title}}</p>{{end}}</main>`,
	})

	s := sitetest.StartWith(t,
		sitetest.Options{Templates: dir, StartingPoints: []string{"photos", "agenda", "faq"}},
		gallery.Module("photos", "Photos"),
		events.Module("agenda", "Agenda"),
		lists.Module("faq", "FAQ"),
	)

	s.Mount("photos", "Photos", "photos")

	photo := s.Upload("beach.png", sitetest.PNG(t, 400, 200))
	album := s.Submit("/admin/photos/new?locale=en", url.Values{
		"Title":  {"Holiday"},
		"Slug":   {"holiday"},
		"Images": {photo.ID},
	})
	s.Publish(album + "?locale=en")
	s.Submit("/admin/photos/new?locale=en", url.Values{"Title": {"Unpublished"}, "Slug": {"unpublished"}})

	// Two upcoming events: the function returns the nearest only.
	soon := time.Now().AddDate(0, 0, 10)

	for i, title := range []string{"Market", "Concert"} {
		day := soon.AddDate(0, 0, i).Format(time.DateOnly)
		event := s.Submit("/admin/agenda/new?locale=en", url.Values{
			"Title":    {title},
			"Slug":     {strings.ToLower(title)},
			"Start":    {day + "T10:00"},
			"Timezone": {"UTC"},
		})
		s.Publish(event + "?locale=en")
	}

	entry := s.Submit("/admin/faq/new?locale=en", url.Values{"Title": {"Opening hours?"}, "Body": {"Nine to five."}})
	s.Publish(entry + "?locale=en")
	s.Submit("/admin/faq/new?locale=en", url.Values{"Title": {"Draft question"}, "Body": {"Not yet."}})

	_, body := s.Get("/en/")

	for _, want := range []string{
		`<p class="albums">Holiday 1 /en/photos/holiday</p>`,
		` 100w, `,
		` 200w"`,
		`<p class="event">Market on ` + soon.Weekday().String() + `</p>`,
		`<p class="entry">Opening hours?</p>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("home lacks %q\n%s", want, body)
		}
	}

	for _, unwanted := range []string{"Unpublished", `class="missing"`, "Concert", "Draft question"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("home shows %q\n%s", unwanted, body)
		}
	}
}
