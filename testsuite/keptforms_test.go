package testsuite

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/modules/comments"
	"github.com/donseba/go-webthings/sitetest"
)

// TestVisitorFormKeepsPost checks that a visitor's post with mistakes
// shows again in its form, with go-form's messages in the page's language,
// without JavaScript and with HTMX alike.
func TestVisitorFormKeepsPost(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"templates/layout.gohtml":         `<body>{{content}}</body>`,
		"templates/page.gohtml":           `<main id="content"><h1>{{.Title}}</h1>{{requiredSlot "talk"}}</main>`,
		"templates/fragments/talk.gohtml": `<section id="talk">{{commentForm "remarks" "home"}}</section>`,
		"translations/nl.po": `msgid ""
msgstr ""
"Language: nl\n"
"Content-Type: text/plain; charset=UTF-8\n"
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
	}, comments.Module("remarks", "Remarks"))

	post := url.Values{
		"subject": {"home"},
		"email":   {"jan@example.com"},
		"body":    {"Hallo"},
		"back":    {"/nl/#remarks"},
	}

	// Without JavaScript: the browser follows the redirect to the page.
	status, body, landed := s.Visitor().Send("/_/remarks/post", post)
	if status != http.StatusOK || landed != "/nl/?remarks=invalid" {
		t.Fatalf("post: %d %s", status, landed)
	}

	for _, part := range []string{"Dit veld is verplicht.", `value="jan@example.com"`, "Hallo</textarea>", "<main"} {
		if !strings.Contains(body, part) {
			t.Fatalf("page misses %q:\n%s", part, body)
		}
	}

	// With HTMX: the action redirects the same way, and HTMX follows with
	// the cookie to the fragment it swaps in.
	request := httptest.NewRequest(http.MethodPost, "/_/remarks/post", strings.NewReader(post.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	htmx(request, "section#talk")

	response := httptest.NewRecorder()
	s.App.ServeHTTP(response, request)

	cookies := response.Result().Cookies()
	if response.Code != http.StatusSeeOther || len(cookies) != 1 || cookies[0].Name != "wt_form_remarks" {
		t.Fatalf("HTMX post: %d %v", response.Code, cookies)
	}

	location, _, _ := strings.Cut(response.Header().Get("Location"), "#")

	request = httptest.NewRequest(http.MethodGet, location, nil)
	request.AddCookie(cookies[0])
	htmx(request, "section#talk")

	response = httptest.NewRecorder()
	s.App.ServeHTTP(response, request)

	expectBody(t, "HTMX fragment", response,
		[]string{`<section id="talk">`, "Dit veld is verplicht.", `value="jan@example.com"`, "Hallo</textarea>"},
		[]string{"<main", "<body>"},
	)

	if cleared := response.Result().Cookies(); len(cleared) != 1 || cleared[0].MaxAge >= 0 {
		t.Fatalf("HTMX: cookie not cleared: %v", cleared)
	}
}

// TestVisitorFormMailRule checks that an e-mail address mail cannot be
// sent to, without a dot in its domain, comes back at its field with
// go-form's message in the page's language (the mail rule, validate:"mail"),
// with the visitor's post kept.
func TestVisitorFormMailRule(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"layout.gohtml": `<body>{{content}}</body>`,
		"page.gohtml":   `<main id="content"><h1>{{.Title}}</h1><section id="talk">{{commentForm "remarks" "home"}}</section></main>`,
	})

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir}, comments.Module("remarks", "Remarks"))

	post := url.Values{
		"subject": {"home"},
		"name":    {"Jan"},
		"email":   {"jan@localhost"},
		"body":    {"Hallo"},
		"back":    {"/nl/#remarks"},
	}

	status, body, landed := s.Visitor().Send("/_/remarks/post", post)
	if status != http.StatusOK || landed != "/nl/?remarks=invalid" {
		t.Fatalf("post: %d %s", status, landed)
	}

	for _, part := range []string{"Vul een geldig e-mailadres in.", `value="jan@localhost"`, `value="Jan"`, "Hallo</textarea>"} {
		if !strings.Contains(body, part) {
			t.Fatalf("page misses %q:\n%s", part, body)
		}
	}

	// An address with a domain is taken.
	post.Set("email", "jan@example.com")

	if status, _, landed := s.Visitor().
		Send("/_/remarks/post", post); status != http.StatusOK ||
		strings.Contains(landed, "invalid") {
		t.Fatalf("valid post: %d %s", status, landed)
	}
}

// htmx marks request as HTMX 4 sends it, aimed at target.
func htmx(request *http.Request, target string) {
	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Request-Type", "partial")
	request.Header.Set("HX-Target", target)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
}
