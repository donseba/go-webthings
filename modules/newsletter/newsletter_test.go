package newsletter_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/modules/newsletter"
	"github.com/donseba/go-webthings/sitetest"
)

// provider records the calls of the provider sync.
type provider struct {
	mu    sync.Mutex
	calls []string
}

func (p *provider) Subscribe(_ context.Context, email, _, locale string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls = append(p.calls, "subscribe "+email+" "+locale)

	return nil
}

func (p *provider) Unsubscribe(_ context.Context, email, _, locale string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls = append(p.calls, "unsubscribe "+email+" "+locale)

	return nil
}

var (
	confirmPattern     = regexp.MustCompile(`https?://[^/\s]+(/_/news/confirm\?token=[0-9a-f]{64})`)
	unsubscribePattern = regexp.MustCompile(`https?://[^/\s]+(/_/news/unsubscribe\?token=[0-9a-f]{64})`)
)

func TestNewsletter(t *testing.T) {
	translations, err := frontend.Translations("testdata/translations")
	if err != nil {
		t.Fatal(err)
	}

	recorder := &provider{}
	s := sitetest.StartWith(t,
		sitetest.Options{
			Templates: "testdata/templates",
			Configure: func(config *webthings.Config) {
				config.Translations = translations
			},
		},
		newsletter.Module("news", "Newsletter", newsletter.WithProvider(recorder)),
	)

	// Until texts are published the built-in defaults are used.
	s.Expect("/en/", http.StatusOK, `action="/_/news/subscribe"`, `name="website"`, "<h2>Newsletter</h2>")

	texts := s.Submit("/admin/news-texts/new?locale=en", url.Values{
		"Title":  {"Our news"},
		"Intro":  {"Monthly *news*."},
		"Footer": {"Unsubscribe *here*:"},
	})
	s.Submit(texts+"?locale=nl", url.Values{
		"Title":  {"Ons nieuws"},
		"Footer": {"Afmelden:"},
	})
	s.Publish(texts + "?locale=en")

	s.Expect("/en/", http.StatusOK, "<h2>Our news</h2>", "<em>news</em>")
	s.Expect("/nl/", http.StatusOK, "<h2>Ons nieuws</h2>")

	s.Mount("news", "Newsletter", "newsletter")
	s.Expect(
		"/en/newsletter",
		http.StatusOK,
		"<h1>Our news</h1>",
		`action="/_/news/subscribe"`,
		`name="back" value="/en/newsletter#news"`,
		`id="news-email"`,
		`name="locale"`,
		`value="en"`,
	)

	// Alice signs up on the newsletter page and confirms with the link in her mail.
	signup(t, s, url.Values{"email": {" Alice@Example.test "}, "name": {"Alice"}, "back": {"/en/newsletter"}},
		"/en/newsletter?news=check-mail")

	confirmation := lastMail(t, s, "alice@example.test")
	if confirmation.Subject != "Please confirm your subscription" ||
		!strings.HasPrefix(confirmation.Text, "Hello Alice,\n\nThank you for subscribing") {
		t.Fatalf("confirmation mail: %+v", confirmation)
	}

	s.Expect(link(t, confirmPattern, confirmation.Text), http.StatusOK, "<h1>Our news</h1>", "result: confirmed")

	// Bob signs up on the Dutch home page and gets the Dutch mail; the module
	// is not on a Dutch page, so his link leads to the Dutch home page.
	signup(t, s, url.Values{"email": {"bob@example.test"}, "back": {"/nl/"}}, "/nl/?news=check-mail")

	confirmation = lastMail(t, s, "bob@example.test")
	if confirmation.Subject != "Bevestig je aanmelding" || !strings.HasPrefix(confirmation.Text, "Thank you") {
		t.Fatalf("Dutch confirmation mail: %+v", confirmation)
	}

	s.Expect(link(t, confirmPattern, confirmation.Text), http.StatusOK, "<h2>Ons nieuws</h2>", "result: confirmed")

	// The form's locale field chooses the mail's language over the page's.
	signup(t, s, url.Values{"email": {"carol@example.test"}, "locale": {"nl"}, "back": {"/en/"}}, "/en/?news=check-mail")

	if confirmation = lastMail(t, s, "carol@example.test"); confirmation.Subject != "Bevestig je aanmelding" {
		t.Fatalf("confirmation mail in the posted language: %+v", confirmation)
	}

	// Known addresses get the same answer and no new mail; invalid ones are reported.
	mails := len(s.Mail.Sent())

	signup(t, s, url.Values{"email": {"alice@example.test"}, "back": {"/en/"}}, "/en/?news=check-mail")
	signup(t, s, url.Values{"email": {"not an address"}, "back": {"/en/"}}, "/en/?news=invalid")

	// Bots that fill the honeypot are thanked but not stored.
	signup(t, s, url.Values{"email": {"spam@example.test"}, "website": {"x"}, "back": {"/en/"}}, "/en/?news=check-mail")

	if len(s.Mail.Sent()) != mails {
		t.Fatalf("unexpected mail: %+v", s.Mail.Sent()[mails:])
	}

	if status := s.CrossSite("/_/news/subscribe", url.Values{"email": {"mallory@example.test"}}); status != http.StatusForbidden {
		t.Fatalf("cross-site signup: %d", status)
	}

	s.Expect("/_/news/confirm?token=unknown", http.StatusOK, "result: invalid")

	// The admin lists subscribers by status and exports them.
	s.Expect("/admin/news", http.StatusOK, "alice@example.test", "bob@example.test", "carol@example.test",
		`All <span class="badge">3</span>`, `Confirmed <span class="badge">2</span>`, `Pending <span class="badge">1</span>`)
	s.ExpectMissing("/admin/news", "spam@example.test")
	s.ExpectMissing("/admin/news", "mallory@example.test")
	s.Expect("/admin/news?status=pending", http.StatusOK, "carol@example.test")
	s.ExpectMissing("/admin/news?status=pending", "alice@example.test")

	code, csv := s.Get("/admin/news/export?status=confirmed")
	if code != http.StatusOK || !strings.HasPrefix(csv, "email,name,locale,status,created_at,confirmed_at\n") ||
		!strings.Contains(csv, "alice@example.test,Alice,en,confirmed,") || strings.Contains(csv, "carol") {
		t.Fatalf("export: %d\n%s", code, csv)
	}

	// A mailing goes to every confirmed subscriber, one message each, with a
	// personal unsubscribe link and the footer in their language.
	mailing := s.Submit("/admin/news/mailings/new", url.Values{
		"Subject":  {"October news"},
		"Language": {""},
		"Body":     {"Hello **readers**."},
	})
	s.Expect(mailing, http.StatusOK, "<strong>readers</strong>", "Send to subscribers")

	mails = len(s.Mail.Sent())
	s.Post(mailing, mailing+"/send", url.Values{})
	finished(t, s, mailing)

	sent := s.Mail.Sent()[mails:]
	if len(sent) != 2 {
		t.Fatalf("mailing: want 2 messages, got %+v", sent)
	}

	// The footer is Markdown: as written in the text, rendered in the HTML,
	// where the link continues its last paragraph.
	footers := map[string]string{"alice@example.test": "Unsubscribe *here*:", "bob@example.test": "Afmelden:"}
	htmlFooters := map[string]string{
		"alice@example.test": `<p>Unsubscribe <em>here</em>: <a href="`,
		"bob@example.test":   `<p>Afmelden: <a href="`,
	}
	unsubscribe := map[string]string{}

	for _, message := range sent {
		if len(message.To) != 1 || message.Subject != "October news" {
			t.Fatalf("mailing message: %+v", message)
		}

		to := message.To[0]
		if !strings.Contains(message.Text, "Hello **readers**.") || !strings.Contains(message.Text, footers[to]) ||
			!strings.Contains(message.HTML, "<strong>readers</strong>") {
			t.Fatalf("mailing to %s: %+v", to, message)
		}

		unsubscribe[to] = link(t, unsubscribePattern, message.Text)

		if !strings.Contains(message.HTML, unsubscribe[to]) || !strings.Contains(message.HTML, htmlFooters[to]) {
			t.Fatalf("mailing HTML to %s has no unsubscribe link:\n%s", to, message.HTML)
		}

		// Mail programs offer the same link (RFC 2369), with one-click
		// unsubscribe (RFC 8058).
		header := message.Headers["List-Unsubscribe"]
		if !strings.HasPrefix(header, "<http") || !strings.HasSuffix(header, unsubscribe[to]+">") ||
			message.Headers["List-Unsubscribe-Post"] != "List-Unsubscribe=One-Click" {
			t.Fatalf("mailing headers to %s: %+v", to, message.Headers)
		}
	}

	if unsubscribe["alice@example.test"] == unsubscribe["bob@example.test"] {
		t.Fatal("subscribers share an unsubscribe link")
	}

	s.Expect(mailing, http.StatusOK, "Recipients: 2")
	s.ExpectMissing(mailing, "Send to subscribers")

	// A sent mailing cannot go out twice.
	s.Post(mailing, mailing+"/send", url.Values{})
	finished(t, s, mailing)

	if len(s.Mail.Sent()) != mails+2 {
		t.Fatalf("mailing sent twice: %+v", s.Mail.Sent()[mails:])
	}

	// A mailing for one language reaches only that language's subscribers.
	dutch := s.Submit("/admin/news/mailings/new", url.Values{
		"Subject":  {"Nieuws"},
		"Language": {"nl"},
		"Body":     {"Hallo."},
	})
	s.Post(dutch, dutch+"/send", url.Values{})
	finished(t, s, dutch)

	if last := s.Mail.Sent()[len(s.Mail.Sent())-1]; len(s.Mail.Sent()) != mails+3 || last.To[0] != "bob@example.test" {
		t.Fatalf("Dutch mailing: %+v", s.Mail.Sent()[mails:])
	}

	s.Expect("/admin/news/mailings", http.StatusOK, "October news", "Nieuws")

	// A mailing that was not sent can be changed and deleted.
	draft := s.Submit("/admin/news/mailings/new", url.Values{"Subject": {"Draft"}, "Body": {"First words."}})
	s.Post(draft, draft+"/save", url.Values{"Subject": {"Draft, changed"}, "Language": {""}, "Body": {"Second words."}})
	s.Expect(draft, http.StatusOK, "Draft, changed", "Second words.")

	status, _ := s.PostStatus(draft, draft+"/save", url.Values{"Subject": {" "}, "Body": {"x"}})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("mailing without subject: %d", status)
	}

	// A subject needs at least five characters, not counting spaces around it.
	short, refusal := s.PostStatus(draft, draft+"/save", url.Values{"Subject": {"  Hi  "}, "Body": {"x"}})
	if short != http.StatusUnprocessableEntity || !strings.Contains(refusal, "at least 5 characters") {
		t.Fatalf("mailing with a short subject: %d\n%s", short, refusal)
	}

	// A sent mailing stays as it went out.
	path := s.Post(mailing, mailing+"/save", url.Values{"Subject": {"Rewritten"}, "Body": {"x"}})
	if path != "/admin/news/mailings" {
		t.Fatalf("saving a sent mailing ended on %s", path)
	}

	s.ExpectMissing("/admin/news/mailings", "Rewritten")

	if path := s.Post(draft, draft+"/delete", url.Values{}); path != "/admin/news/mailings" {
		t.Fatalf("delete ended on %s", path)
	}

	s.ExpectMissing("/admin/news/mailings", "Draft, changed")
	s.Expect(draft, http.StatusNotFound)

	// Alice leaves with the link in her mailing.
	s.Expect(unsubscribe["alice@example.test"], http.StatusOK, "<h1>Our news</h1>", "result: unsubscribed")
	s.Expect("/admin/news?status=unsubscribed", http.StatusOK, "alice@example.test")
	s.ExpectMissing("/admin/news?status=confirmed", "alice@example.test")

	// Deleting moves a subscriber to the trash: off the list, the counts and
	// the export.
	pending, _, err := newsletter.Of(s.App, "news").Subscribers(context.Background(), newsletter.Pending, 0, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending: %+v %v", pending, err)
	}

	s.Expect("/admin/news", http.StatusOK, `href="/admin/trash?section=news"`, "Move this item to the trash?")

	code, page := s.PostStatus("/admin/news", "/admin/news/subscribers/"+pending[0].ID+"/delete", url.Values{})
	if code != http.StatusOK || !strings.Contains(page, "Moved to the trash.") {
		t.Fatalf("delete subscriber: %d\n%s", code, page)
	}

	s.ExpectMissing("/admin/news", "carol@example.test")
	s.Expect("/admin/news", http.StatusOK, `Pending <span class="badge">0</span>`, `All <span class="badge">2</span>`)

	if code, csv := s.Get("/admin/news/export"); code != http.StatusOK || strings.Contains(csv, "carol") {
		t.Fatalf("export after delete: %d\n%s", code, csv)
	}

	s.Expect("/admin/trash?section=news", http.StatusOK, "carol@example.test", "Subscriber")

	// Bob leaves with one click in his mail program: a POST to the link.
	status, body, _ := s.Send(unsubscribe["bob@example.test"], url.Values{"List-Unsubscribe": {"One-Click"}})
	if status != http.StatusOK || body != "unsubscribed\n" {
		t.Fatalf("one-click unsubscribe: %d %s", status, body)
	}

	s.Expect("/admin/news?status=unsubscribed", http.StatusOK, "bob@example.test")

	if status, _, _ := s.Send("/_/news/unsubscribe?token=unknown", url.Values{}); status != http.StatusNotFound {
		t.Fatalf("one-click with an unknown token: %d", status)
	}

	// Visitors who sign up too often are sent back.
	for range 20 {
		s.Send("/_/news/subscribe", url.Values{"email": {"dave@example.test"}, "back": {"/en/"}})
	}

	signup(t, s, url.Values{"email": {"dave@example.test"}, "back": {"/en/"}}, "/en/?news=throttled")

	want := []string{
		"subscribe alice@example.test en",
		"subscribe bob@example.test nl",
		"unsubscribe alice@example.test en",
		"unsubscribe bob@example.test nl",
	}
	if !slices.Equal(recorder.calls, want) {
		t.Fatalf("provider: want %q, got %q", want, recorder.calls)
	}
}

// signup posts the signup form like a visitor and checks where it lands.
func signup(t *testing.T, s *sitetest.Site, values url.Values, want string) {
	t.Helper()

	status, body, landed := s.Send("/_/news/subscribe", values)
	if status != http.StatusOK || landed != want {
		t.Fatalf("signup %s: %d %s, want %s\n%s", values.Get("email"), status, landed, want, body)
	}
}

// lastMail returns the newest mail, which must be for to.
func lastMail(t *testing.T, s *sitetest.Site, to string) mail.Message {
	t.Helper()

	sent := s.Mail.Sent()
	if len(sent) == 0 || len(sent[len(sent)-1].To) != 1 || sent[len(sent)-1].To[0] != to {
		t.Fatalf("no mail for %s: %+v", to, sent)
	}

	return sent[len(sent)-1]
}

// link returns the path and query of the first absolute link matching pattern.
func link(t *testing.T, pattern *regexp.Regexp, text string) string {
	t.Helper()

	match := pattern.FindStringSubmatch(text)
	if match == nil {
		t.Fatalf("no link %s in:\n%s", pattern, text)
	}

	return match[1]
}

// finished waits until the mailing whose admin page is at path went out to
// everyone; mailings are sent in the background.
func finished(t *testing.T, s *sitetest.Site, path string) {
	t.Helper()

	id := path[strings.LastIndex(path, "/")+1:]
	deadline := time.Now().Add(10 * time.Second)

	for {
		mailing, err := newsletter.Of(s.App, "news").Mailing(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}

		if !mailing.Sending() {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("mailing %s did not finish: %+v", id, mailing)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func TestImportSubscribers(t *testing.T) {
	s := sitetest.Start(t, newsletter.Module("news", "Newsletter"))

	s.Expect("/admin/news", http.StatusOK, `href="/admin/news/import"`)

	csv := "email;name;locale\nann@example.test;Ann;nl\nnot-an-address;X;\n"

	// The editor has to confirm that everyone agreed to receive the newsletter.
	code, body := s.PostStatus("/admin/news/import", "/admin/news/import", url.Values{"csv": {csv}, "locale": {"en"}})

	consent := "Confirm that everyone on the list agreed to receive the newsletter."
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, consent) ||
		!strings.Contains(body, `aria-invalid="true"`) {
		t.Fatalf("import without consent: %d\n%s", code, body)
	}

	code, body = s.PostStatus("/admin/news/import", "/admin/news/import", url.Values{
		"csv":     {csv},
		"locale":  {"en"},
		"consent": {"1"},
	})
	if code != http.StatusOK || !strings.Contains(body, "Subscribers added: 1") || !strings.Contains(body, "not-an-address") {
		t.Fatalf("import: %d\n%s", code, body)
	}

	// Imported subscribers are confirmed right away, without a mail.
	s.Expect("/admin/news?status=confirmed", http.StatusOK, "ann@example.test")

	// A file is read instead of the text field.
	code, body = s.PostFiles("/admin/news/import", "/admin/news/import", url.Values{"locale": {"en"}, "consent": {"1"}},
		sitetest.File{Field: "file", Name: "list.csv", Data: []byte("email\nbob@example.test\n")})
	if code != http.StatusOK || !strings.Contains(body, "Subscribers added: 1") {
		t.Fatalf("import of a file: %d\n%s", code, body)
	}

	// A tab-separated file is read too.
	code, body = s.PostFiles("/admin/news/import", "/admin/news/import", url.Values{"locale": {"en"}, "consent": {"1"}},
		sitetest.File{Field: "file", Name: "list.tsv", Data: []byte("email\tname\ncarol@example.test\tCarol\n")})
	if code != http.StatusOK || !strings.Contains(body, "Subscribers added: 1") {
		t.Fatalf("import of a tab-separated file: %d\n%s", code, body)
	}

	if sent := s.Mail.Sent(); len(sent) != 0 {
		t.Fatalf("import sent mail: %+v", sent)
	}

	code, body = s.PostStatus("/admin/news/import", "/admin/news/import", url.Values{
		"csv":     {"name\nAnn\n"},
		"consent": {"1"},
	})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "including email") {
		t.Fatalf("import without email column: %d\n%s", code, body)
	}

	// Imports may be larger than the admin's default 512 KiB request limit.
	large := "email;note\nlarge@example.test;" + strings.Repeat("x", 600<<10) + "\n"

	code, body = s.PostStatus("/admin/news/import", "/admin/news/import", url.Values{
		"csv":     {large},
		"locale":  {"en"},
		"consent": {"1"},
	})
	if code != http.StatusOK || !strings.Contains(body, "Subscribers added: 1") {
		t.Fatalf("large import: %d\n%.500s", code, body)
	}
}

// gate is a mail sender that waits for the test before each message.
type gate struct {
	mail.Memory

	open chan struct{}
}

func (g *gate) Send(ctx context.Context, message mail.Message) error {
	select {
	case <-g.open:
	case <-ctx.Done():
		return ctx.Err()
	}

	return g.Memory.Send(ctx, message)
}

// TestSendingProgress shows a mailing's progress while it goes out in the
// background, and lets Close wait for the message being sent.
func TestSendingProgress(t *testing.T) {
	outbox := &gate{open: make(chan struct{})}

	s := sitetest.StartWith(t, sitetest.Options{
		Configure: func(config *webthings.Config) {
			config.Mail = outbox
		},
	}, newsletter.Module("news", "Newsletter"))

	service := newsletter.Of(s.App, "news")

	_, err := service.Import(context.Background(), []newsletter.Contact{
		{Email: "ann@example.test"},
		{Email: "bob@example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}

	mailing := s.Submit("/admin/news/mailings/new", url.Values{"Subject": {"News from us"}, "Body": {"Hello"}})

	status, body := s.PostStatus(mailing, mailing+"/send", url.Values{})
	if status != http.StatusOK || !strings.Contains(body, "The mailing is being sent.") {
		t.Fatalf("send: %d\n%s", status, body)
	}

	s.Expect(mailing, http.StatusOK,
		`id="mailing-progress"`,
		`hx-trigger="every 3s"`,
		`hx-select="#mailing-progress"`,
		"of 2 messages sent.",
	)
	s.Expect("/admin/news/mailings", http.StatusOK, "Sending")

	// Let the messages go, one at a time.
	outbox.open <- struct{}{}
	outbox.open <- struct{}{}

	finished(t, s, mailing)
	s.Expect(mailing, http.StatusOK, "Recipients: 2", "Finished: ")
	s.ExpectMissing(mailing, `hx-trigger="every 3s"`)

	if sent := outbox.Sent(); len(sent) != 2 {
		t.Fatalf("messages: %+v", sent)
	}
}

// TestInvalidSignupKept checks that a signup with mistakes returns to its
// form with the visitor's address and go-form's message at the field, once.
func TestInvalidSignupKept(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, newsletter.Module("news", "Newsletter"))
	visitor := s.Visitor()

	status, body, landed := visitor.Send("/_/news/subscribe", url.Values{"email": {"not an address"}, "back": {"/en/#news"}})
	if status != http.StatusOK || landed != "/en/?news=invalid" {
		t.Fatalf("signup: %d %s", status, landed)
	}

	for _, part := range []string{"result: invalid", `value="not an address"`, "Enter a valid email address.", `aria-invalid="true"`} {
		if !strings.Contains(body, part) {
			t.Fatalf("the page misses %q:\n%s", part, body)
		}
	}

	if _, again := visitor.Get(landed); strings.Contains(again, "not an address") {
		t.Fatalf("shown twice:\n%s", again)
	}

	// With HTMX the part of the page HTMX swaps in shows it.
	status, body, _ = visitor.SendHTMX(
		"/_/news/subscribe",
		url.Values{"email": {"not an address"}, "back": {"/en/#news"}},
		"news",
	)
	if status != http.StatusOK || strings.Contains(body, "<body") ||
		!strings.Contains(body, `value="not an address"`) || !strings.Contains(body, "Enter a valid email address.") {
		t.Fatalf("HTMX signup: %d\n%s", status, body)
	}

	// An address go-form accepts but mail refuses keeps the value too.
	_, body, _ = visitor.Send("/_/news/subscribe", url.Values{"email": {"jane@localhost"}, "back": {"/en/#news"}})
	if !strings.Contains(body, "result: invalid") || !strings.Contains(body, `value="jane@localhost"`) {
		t.Fatalf("refused address:\n%s", body)
	}
}
