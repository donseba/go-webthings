package testsuite

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/modules/banners"
	"github.com/donseba/go-webthings/modules/directory"
	"github.com/donseba/go-webthings/modules/documents"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/forms"
	"github.com/donseba/go-webthings/modules/gallery"
	"github.com/donseba/go-webthings/modules/lists"
	"github.com/donseba/go-webthings/modules/newsletter"
	"github.com/donseba/go-webthings/modules/people"
	"github.com/donseba/go-webthings/modules/polls"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/modules/testimonials"
	"github.com/donseba/go-webthings/sitetest"
)

var textareaPattern = regexp.MustCompile(`(?s)<textarea\s[^>]*>`)

var attributePattern = regexp.MustCompile(`(name|class)="([^"]*)"`)

// textareas lists the textareas of an admin page by name, with whether they
// are Markdown fields (class "markdown").
func textareas(t *testing.T, s *sitetest.Site, path string) map[string]bool {
	t.Helper()

	status, body := s.Get(path)
	if status != http.StatusOK {
		t.Fatalf("%s: status %d", path, status)
	}

	fields := map[string]bool{}

	for _, tag := range textareaPattern.FindAllString(body, -1) {
		var name string

		markdown := false

		for _, match := range attributePattern.FindAllStringSubmatch(tag, -1) {
			switch match[1] {
			case "name":
				name = match[2]
			case "class":
				markdown = slices.Contains(strings.Fields(match[2]), "markdown")
			}
		}

		fields[name] = markdown
	}

	return fields
}

// TestMarkdownFields checks which admin text areas are Markdown fields with
// the formatted-text editor: text the website or a mail shows formatted.
// Plain text by nature (addresses, SEO and teaser texts, lists of options,
// alt text, admin-only notes) stays a plain text area.
func TestMarkdownFields(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t,
		publishing.Module("news", "News"),
		events.Module("agenda", "Agenda"),
		newsletter.Module("letters", "Newsletter"),
		people.Module("team", "Team"),
		documents.Module("downloads", "Downloads"),
		directory.Module("stockists", "Stockists"),
		lists.Module("faq", "FAQ"),
		banners.Module("notice", "Notices"),
		gallery.Module("work", "Work"),
		forms.Module("contact", "Contact"),
		testimonials.Module("reviews", "Reviews"),
		polls.Module("poll", "Polls"),
	)

	cases := []struct {
		path     string
		markdown []string
		plain    []string
	}{
		{"/admin/pages/new", []string{"Body"}, []string{"Description"}},
		{"/admin/site", []string{"Footer"}, []string{"Address", "SEODescription"}},
		{"/admin/news/new?locale=en", []string{"Body"}, []string{"Excerpt"}},
		{"/admin/agenda/new?locale=en", []string{"Body"}, []string{"Summary", "Address"}},
		{"/admin/letters-texts/new?locale=en", []string{"Intro", "Footer"}, nil},
		{"/admin/letters/mailings/new", []string{"Body"}, nil},
		{"/admin/team/new?locale=en", []string{"Bio"}, nil},
		{"/admin/downloads/new?locale=en", []string{"Description"}, nil},
		{"/admin/stockists/new?locale=en", []string{"Description"}, []string{"Summary"}},
		{"/admin/faq/new?locale=en", []string{"Body"}, nil},
		{"/admin/notice/new?locale=en", []string{"Body"}, nil},
		{"/admin/work/new?locale=en", []string{"Description"}, nil},
		{"/admin/contact/new?locale=en", []string{"Intro", "Success"}, []string{"Fields"}},
		{"/admin/reviews/new?locale=en", nil, []string{"Quote"}},
		{"/admin/poll/new?locale=en", nil, []string{"Options"}},
	}

	for _, test := range cases {
		fields := textareas(t, s, test.path)

		for _, name := range test.markdown {
			if markdown, ok := fields[name]; !ok || !markdown {
				t.Errorf("%s: %s is not a Markdown field (%v)", test.path, name, fields)
			}
		}

		for _, name := range test.plain {
			if markdown, ok := fields[name]; !ok || markdown {
				t.Errorf("%s: %s is not a plain text area (%v)", test.path, name, fields)
			}
		}
	}
}

// TestMarkdownEditor checks that admin pages load the editor's loader with
// its texts, that the editor's files are served, and that a Markdown text
// outside content, the site footer, is shown formatted.
func TestMarkdownEditor(t *testing.T) {
	t.Parallel()

	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/markdown"})

	s.Expect("/admin/pages/new", http.StatusOK,
		`<script src="/admin/assets/editor/loader.js?v=`,
		`type="module" data-media-picker="/admin/media/picker"></script>`,
		`<script type="application/json" id="markdown-editor-labels">`,
		`"Heading 2":"Heading 2"`,
		`markdown"`,
	)

	for _, file := range []string{"loader.js", "editor.js", "prosemirror.js", "editor.css", "icons.svg", "LICENSES.txt"} {
		if status, _ := s.Get("/admin/assets/editor/" + file); status != http.StatusOK {
			t.Errorf("%s: status %d", file, status)
		}
	}

	s.Post("/admin/site", "/admin/site/text?locale=en", url.Values{
		"Title":  {"Test site"},
		"Footer": {"Made by **us**. [Contact](/en/contact)"},
	})
	s.Expect("/en/", http.StatusOK,
		`<footer><p>Made by <strong>us</strong>. <a href="/en/contact">Contact</a></p>`)
}
