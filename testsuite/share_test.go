package testsuite

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/sitetest"
)

// TestProfilesAndShareLinks edits the site's social media profiles in the
// admin and shows them, with share links, in the page's language.
func TestProfilesAndShareLinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"templates/layout.gohtml": `<body>
<footer>{{range .Site.Profiles}}<a href="{{.URL}}" rel="me" class="{{.Network}}">{{.Label}}</a>{{end}}</footer>
{{content}}</body>`,
		"templates/page.gohtml": `<main id="content"><h1>{{.Title}}</h1>
<ul class="profiles">{{range profiles}}<li class="{{.Network}}">{{.Label}}</li>{{end}}</ul>
<ul class="share">{{range share "" .Title}}<li><a href="{{.URL}}" class="{{.Network}}">{{.Label}}</a></li>{{end}}</ul>
<p class="mail">{{range share "https://example.test/a b?x=1&y=2" "Tom & Jerry"}}{{if eq .Network "email"}}{{.URL}}{{end}}{{end}}</p>
{{shareBar "/en/about" "About"}}
</main>`,
		"translations/nl.po": `msgid ""
msgstr ""
"Language: nl\n"
"Content-Type: text/plain; charset=UTF-8\n"

msgid "E-mail"
msgstr "Per e-mail"

msgid "Facebook"
msgstr "Deel op Facebook"

msgid "Share"
msgstr "Delen"
`,
	})

	translations, err := frontend.Translations(filepath.Join(dir, "translations"))
	if err != nil {
		t.Fatal(err)
	}

	s := sitetest.StartWith(t, sitetest.Options{
		Templates: filepath.Join(dir, "templates"),
		Configure: func(config *webthings.Config) {
			config.Translations = translations
		},
	})

	status, body := s.PostStatus("/admin/site?locale=en", "/admin/site/profiles?locale=en", url.Values{
		"Instagram": {"instagram.com/example"},
		"X":         {"javascript:alert(1)"},
	})
	if status != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "Enter a web address, such as https://example.com/name.") {
		t.Fatalf("invalid address: %d\n%s", status, body)
	}

	s.Post("/admin/site?locale=en", "/admin/site/profiles?locale=en", url.Values{
		"Mastodon":  {"https://mastodon.social/@example"},
		"Instagram": {" instagram.com/example "},
	})

	// The form shows the completed addresses.
	s.Expect("/admin/site?locale=en", http.StatusOK, `value="https://instagram.com/example"`, "Social media profiles")

	// Profiles keep the order of the networks, in the layout and anywhere.
	s.Expect("/en/", http.StatusOK,
		`<footer><a href="https://instagram.com/example" rel="me" class="instagram">Instagram</a>`+
			`<a href="https://mastodon.social/@example" rel="me" class="mastodon">Mastodon</a></footer>`,
		`<li class="instagram">Instagram</li><li class="mastodon">Mastodon</li>`,
	)

	// Share links of the current page are absolute and escaped.
	s.Expect("/en/?ignored=1", http.StatusOK,
		`<a href="https://www.facebook.com/sharer/sharer.php?u=http%3A%2F%2F127.0.0.1%3A`,
		`%2Fen%2F" class="facebook">Facebook</a>`,
		`class="whatsapp">WhatsApp</a>`,
		`mailto:?subject=Tom%20%26%20Jerry&amp;body=https%3A%2F%2Fexample.test%2Fa%20b%3Fx%3D1%26y%3D2`,
	)
	s.ExpectMissing("/en/?ignored=1", "ignored%3D1")
	s.Expect("/en/", http.StatusOK, `class="email">E-mail</a>`)

	// shareBar renders the site/share component for a path.
	s.Expect(
		"/en/",
		http.StatusOK,
		`<nav class="wt-share" aria-label="Share">`,
		`<a class="wt-share__link wt-share__link--linkedin" href="https://www.linkedin.com/sharing/share-offsite/?url=http%3A%2F%2F127.0.0.1%3A`,
		`%2Fen%2Fabout" target="_blank" rel="noopener noreferrer">LinkedIn</a>`,
	)

	// Network names follow the site's translations in the page's language.
	s.Expect("/nl/", http.StatusOK,
		`class="facebook">Deel op Facebook</a>`,
		`class="email">Per e-mail</a>`,
		`class="x">X</a>`,
		`<nav class="wt-share" aria-label="Delen">`,
	)
}
