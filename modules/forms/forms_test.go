package forms_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/modules/forms"
	"github.com/donseba/go-webthings/sitetest"
)

const definition = `email* | email | Your email | you@example.com
topic | select | Topic
- sales | Sales
- support | Support
message* | textarea | Message | | Tell us everything.
`

func TestForms(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, forms.Module("contact", "Contact"))

	editor := s.Submit("/admin/contact/new?locale=en", url.Values{
		"Title":   {"Contact us"},
		"Intro":   {"We *reply* fast."},
		"Fields":  {definition},
		"Success": {"Thanks for your message."},
	})

	s.Mount("contact", "Contact", "contact")
	s.Expect("/en/contact", http.StatusNotFound)

	s.Publish(editor + "?locale=en")

	// The form posts to the page itself, without a trailing slash, so the
	// post is not redirected.
	s.Expect("/en/contact", http.StatusOK,
		`action="/en/contact"`,
		"<h1>Contact us</h1>",
		"<em>reply</em>",
		`id="contact-email"`,
		`placeholder="you@example.com"`,
		`maxlength="254"`,
		`>Support</option>`,
		`name="website"`,
	)

	// Missing and invalid values come back with go-form's messages at their
	// fields and the input kept.
	code, body := send(t, s, url.Values{"email": {"not an email"}, "topic": {"other"}})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid post: %d\n%s", code, body)
	}

	for _, part := range []string{
		`id="contact-email_error_0" role="alert">Enter a valid email address.`,
		`id="contact-topic_error_0"`,
		`id="contact-message_error_0"`,
		`value="not an email"`,
		`action="/en/contact"`,
	} {
		if !strings.Contains(body, part) {
			t.Fatalf("invalid post: missing %q\n%s", part, body)
		}
	}

	// HTMX gets the same answer, the content it swaps in: the form is
	// shown again in place, so it needs no redirect.
	request := httptest.NewRequest(http.MethodPost, "/en/contact", strings.NewReader("email=not+an+email"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Request-Type", "partial")
	request.Header.Set("HX-Target", "main#content")

	response := httptest.NewRecorder()
	s.App.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(response.Body.String(), "Enter a valid email address.") ||
		!strings.Contains(response.Body.String(), `value="not an email"`) ||
		strings.Contains(response.Body.String(), "<body") {
		t.Fatalf("invalid HTMX post: %d\n%s", response.Code, response.Body.String())
	}

	code, body, path := s.Send("/en/contact", url.Values{
		"email":   {"jane@example.test"},
		"topic":   {"support"},
		"message": {"Hello there"},
	})
	if code != http.StatusOK || path != "/en/contact?sent=1" || !strings.Contains(body, "Thanks for your message.") {
		t.Fatalf("valid post: %d %s\n%s", code, path, body)
	}

	// The spam trap pretends to accept the post but stores nothing.
	_, _, path = s.Send("/en/contact", url.Values{
		"email":   {"bot@example.test"},
		"message": {"Buy now"},
		"website": {"https://spam.example"},
	})
	if path != "/en/contact?sent=1" {
		t.Fatalf("spam post: %s", path)
	}

	// Posts from other websites are refused; no token is needed otherwise,
	// so a form open for hours or across a restart still works.
	if code := s.CrossSite(
		"/en/contact",
		url.Values{"email": {"forged@example.test"}, "message": {"x"}},
	); code != http.StatusForbidden {
		t.Fatalf("cross-site post: %d", code)
	}

	s.Expect("/admin/contact/inbox", http.StatusOK, "jane@example.test · support · Hello there")
	s.ExpectMissing("/admin/contact/inbox", "bot@example.test")
	s.ExpectMissing("/admin/contact/inbox", "forged@example.test")

	service := forms.Of(s.App, "contact")

	list, _, err := service.SubmissionsByStatus(context.Background(), "", 0, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("submissions: %+v %v", list, err)
	}

	detail := "/admin/contact/inbox/" + list[0].ID
	s.Expect(detail, http.StatusOK, "Your email", "jane@example.test", "Hello there")

	// Deleting moves the submission to the trash of the form's section.
	s.Expect("/admin/contact/inbox", http.StatusOK, `href="/admin/trash?section=contact"`)
	s.Expect(detail, http.StatusOK, "Move this item to the trash?")

	code, body = s.PostStatus(detail, detail+"/delete", url.Values{})
	if code != http.StatusOK || !strings.Contains(body, "Moved to the trash.") {
		t.Fatalf("delete: %d\n%s", code, body)
	}

	s.ExpectMissing("/admin/contact/inbox", "jane@example.test")
	s.ExpectMissing("/admin/contact/inbox?status=all", "jane@example.test")
	s.Expect(detail, http.StatusNotFound)
	s.Expect("/admin/trash?section=contact", http.StatusOK, "jane@example.test · support · Hello there", "Submission")

	if count, err := service.Count(context.Background(), ""); err != nil || count != 0 {
		t.Fatalf("count after delete: %d %v", count, err)
	}
}

// send posts values to the public form as a visitor's browser does and
// returns the status and body of the answer, without following a redirect.
func send(t *testing.T, s *sitetest.Site, values url.Values) (int, string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/en/contact", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Sec-Fetch-Site", "same-origin")

	response := httptest.NewRecorder()
	s.App.ServeHTTP(response, request)

	return response.Code, response.Body.String()
}

func TestThrottle(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, forms.Module("contact", "Contact"))

	editor := s.Submit("/admin/contact/new?locale=en", url.Values{
		"Title":   {"Contact us"},
		"Fields":  {definition},
		"Success": {"Thanks for your message."},
	})

	s.Mount("contact", "Contact", "contact")
	s.Publish(editor + "?locale=en")

	values := url.Values{"email": {"jane@example.test"}, "message": {"Hello there"}}

	for range 10 {
		if code, body := send(t, s, values); code != http.StatusSeeOther {
			t.Fatalf("post: %d\n%s", code, body)
		}
	}

	// The eleventh post in ten minutes is not stored; the visitor keeps the answers.
	code, body := send(t, s, url.Values{"email": {"late@example.test"}, "message": {"Hello again"}})
	if code != http.StatusTooManyRequests || !strings.Contains(body, "throttled") ||
		!strings.Contains(body, `value="late@example.test"`) {
		t.Fatalf("throttled post: %d\n%s", code, body)
	}

	count, err := forms.Of(s.App, "contact").Count(context.Background(), "")
	if err != nil || count != 10 {
		t.Fatalf("stored: %d %v", count, err)
	}

	// Reading the form is never limited.
	s.Expect("/en/contact", http.StatusOK, "<h1>Contact us</h1>")
}

func TestNotifyAndArchive(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, forms.Module("contact", "Contact"))

	values := url.Values{
		"Title":   {"Contact us"},
		"Fields":  {definition},
		"Success": {"Thanks for your message."},
		"Notify":  {"not an address"},
	}

	code, body := s.PostStatus("/admin/contact/new?locale=en", "/admin/contact/new?locale=en", values)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid notification address: %d\n%s", code, body)
	}

	values.Set("Notify", "office@example.test")
	editor := s.Submit("/admin/contact/new?locale=en", values)

	s.Mount("contact", "Contact", "contact")
	s.Publish(editor + "?locale=en")

	code, body = send(t, s, url.Values{
		"email":   {"jane@example.test"},
		"message": {"Hello there"},
	})
	if code != http.StatusSeeOther {
		t.Fatalf("post: %d\n%s", code, body)
	}

	sent := s.Mail.Sent()
	if len(sent) != 1 {
		t.Fatalf("mail: %+v", sent)
	}

	message := sent[0]
	if message.To[0] != "office@example.test" || message.ReplyTo != "jane@example.test" || message.Subject != "Contact us" {
		t.Fatalf("notification: %+v", message)
	}

	parts := []string{"Your email:\njane@example.test\n\nTopic:\n-\n\nMessage:\nHello there\n\nhttp", "/admin/contact/inbox/"}

	for _, part := range parts {
		if !strings.Contains(message.Text, part) {
			t.Fatalf("notification text: missing %q\n%s", part, message.Text)
		}
	}

	// New submissions are counted, also in the navigation; archiving moves them
	// out of the default list.
	s.Expect("/admin/contact/inbox", http.StatusOK, `<span class="badge">1</span>`, "jane@example.test")
	s.Expect("/admin/", http.StatusOK, `<span class="nav-badge">1</span>`)

	list, _, err := forms.Of(s.App, "contact").SubmissionsByStatus(context.Background(), "", 0, 10)
	if err != nil || len(list) != 1 || list[0].Status != forms.StatusNew {
		t.Fatalf("submissions: %+v %v", list, err)
	}

	detail := "/admin/contact/inbox/" + list[0].ID

	s.Post(detail, detail+"/archive", url.Values{})
	s.ExpectMissing("/admin/contact/inbox", "jane@example.test")
	s.ExpectMissing("/admin/contact/inbox", `<span class="badge">1</span>`)
	s.ExpectMissing("/admin/", `class="nav-badge"`)
	s.Expect("/admin/contact/inbox?status=archived", http.StatusOK, "jane@example.test")
	s.Expect("/admin/contact/inbox?status=all", http.StatusOK, "jane@example.test")

	s.Post(detail, detail+"/unarchive", url.Values{})
	s.Expect("/admin/contact/inbox", http.StatusOK, "jane@example.test")
	s.ExpectMissing("/admin/contact/inbox?status=archived", "jane@example.test")
}
