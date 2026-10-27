package comments_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/modules/comments"
	"github.com/donseba/go-webthings/sitetest"
)

var publishPattern = regexp.MustCompile(`/admin/remarks/([0-9a-f-]{36})/publish`)

func TestComments(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"},
		comments.Module("remarks", "Comments"),
		comments.Module("guestbook", "Guestbook"),
	)

	home, err := s.App.Pages.Home(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	s.Submit("/admin/remarks/settings", url.Values{
		"Moderated": {"on"},
		"Notify":    {"moderator@example.test"},
	})

	s.Expect(
		"/en/",
		http.StatusOK,
		"0 comments",
		`action="/_/remarks/post"`,
		`name="website"`,
		`name="back" value="/en/#remarks"`,
		`id="remarks-name"`,
		`maxlength="100"`,
	)

	// A visitor's comment waits for approval, and the moderator gets a mail.
	landed := send(t, s, "/_/remarks/post", url.Values{
		"subject": {home},
		"name":    {"Ada"},
		"email":   {"ada@example.test"},
		"body":    {"Hello\nworld <b>bold</b>"},
		"back":    {"/en/"},
	})
	if landed != "/en/?remarks=pending" {
		t.Fatalf("post: landed on %s", landed)
	}

	s.Expect(landed, http.StatusOK, `<p class="result">pending</p>`, "0 comments")
	s.ExpectMissing("/en/", "Hello")

	sent := s.Mail.Sent()
	if len(sent) != 1 {
		t.Fatalf("mails: %+v", sent)
	}

	notification := sent[0]
	if notification.To[0] != "moderator@example.test" || notification.ReplyTo != "ada@example.test" ||
		notification.Subject != "Comments: Ada" ||
		!strings.HasPrefix(notification.Text, "A new message waits for approval.\n\nAda <ada@example.test>\n") ||
		!strings.Contains(notification.Text, "Hello\nworld") ||
		!strings.Contains(notification.Text, "/admin/remarks?filter=pending") ||
		!strings.Contains(notification.Text, "/en/\n") {
		t.Fatalf("notification: %+v", notification)
	}

	// Bots that fill in the trap get the same answer, but nothing is stored
	// or mailed.
	landed = send(t, s, "/_/remarks/post", url.Values{
		"subject": {home},
		"name":    {"Bot"},
		"body":    {"Buy now"},
		"website": {"https://spam.example"},
		"back":    {"/en/"},
	})
	if landed != "/en/?remarks=pending" || len(s.Mail.Sent()) != 1 {
		t.Fatalf("spam: landed on %s, %d mails", landed, len(s.Mail.Sent()))
	}

	landed = send(t, s, "/_/remarks/post", url.Values{"subject": {home}, "name": {"Ada"}, "back": {"/en/"}})
	if landed != "/en/?remarks=invalid" {
		t.Fatalf("invalid: landed on %s", landed)
	}

	if status := s.CrossSite(
		"/_/remarks/post",
		url.Values{"subject": {home}, "name": {"Mallory"}, "body": {"Hi"}},
	); status != http.StatusForbidden {
		t.Fatalf("cross-site post: %d", status)
	}

	// The sender is known by an HMAC of the day and the address, with a key
	// every process of the site shares.
	key, err := s.App.Site.Secret(context.Background(), "remarks/visitors")
	if err != nil {
		t.Fatal(err)
	}

	var stored struct {
		Hash      string    `db:"ip_hash"`
		CreatedAt time.Time `db:"created_at"`
	}

	err = s.App.DB.Get(context.Background(), &stored, `SELECT ip_hash, created_at FROM wt_comment WHERE name=?`, "Ada")
	if err != nil {
		t.Fatal(err)
	}

	// The day is the one the comment was posted on, also when the test
	// runs across midnight UTC.
	hash := hmac.New(sha256.New, key)
	hash.Write([]byte(stored.CreatedAt.UTC().Format(time.DateOnly) + "|127.0.0.1"))

	if stored.Hash != hex.EncodeToString(hash.Sum(nil)) {
		t.Fatalf("visitor hash %q", stored.Hash)
	}

	// The admin lists the pending comment under the title of the page it was
	// written on, with its address, and approves it; the navigation counts
	// pending comments.
	s.Expect("/admin/remarks", http.StatusOK, "Ada", "ada@example.test", `Pending <span class="badge">1</span>`,
		`break-all">Home</h2>`, `<a href="/en/">/en/</a>`, `<span class="nav-badge">1</span>`)
	s.ExpectMissing("/admin/remarks?filter=all", "Bot")
	s.ExpectMissing("/admin/remarks?filter=all", "Mallory")

	_, list := s.Get("/admin/remarks")

	match := publishPattern.FindStringSubmatch(list)
	if match == nil {
		t.Fatalf("no approve button:\n%s", list)
	}

	ada := match[1]

	s.Post("/admin/remarks", "/admin/remarks/"+ada+"/publish?filter=pending", url.Values{})
	s.Expect("/en/", http.StatusOK, "1 comments", "<strong>Ada</strong><p>Hello<br>world &lt;b&gt;bold&lt;/b&gt;</p>")
	s.ExpectMissing("/en/", "ada@example.test")
	s.Expect("/admin/remarks", http.StatusOK, `Pending <span class="badge">0</span>`, `Approved <span class="badge">1</span>`)
	s.ExpectMissing("/admin/remarks", `class="nav-badge"`)

	// Staff reply from the admin, signed with the site title.
	s.Submit("/admin/remarks/"+ada+"/reply", url.Values{"Body": {"Thanks for writing."}})
	s.Expect(
		"/en/",
		http.StatusOK,
		"2 comments",
		`<blockquote class="staff"><strong>Test site</strong><p>Thanks for writing.</p>`,
	)

	// A visitor's reply joins the thread once approved.
	landed = send(t, s, "/_/remarks/post", url.Values{
		"subject": {home},
		"parent":  {ada},
		"name":    {"Bob"},
		"body":    {"Me too"},
		"back":    {"/en/"},
	})
	if landed != "/en/?remarks=pending" {
		t.Fatalf("reply: landed on %s", landed)
	}

	s.Expect("/admin/remarks", http.StatusOK, "Bob", "Reply to Ada")

	_, list = s.Get("/admin/remarks")
	bob := publishPattern.FindStringSubmatch(list)[1]

	s.Post("/admin/remarks", "/admin/remarks/"+bob+"/publish?filter=pending", url.Values{})
	s.Expect("/en/", http.StatusOK, "3 comments", `<blockquote class="reply"><strong>Bob</strong><p>Me too</p>`)

	// Hiding the comment hides its thread.
	s.Post("/admin/remarks?filter=all", "/admin/remarks/"+ada+"/unpublish?filter=all", url.Values{})
	s.Expect("/en/", http.StatusOK, "0 comments")
	s.ExpectMissing("/en/", "Thanks for writing.")

	// A closed instance takes no new comments and templates can hide the form.
	s.Submit("/admin/remarks/settings", url.Values{"Moderated": {"on"}, "Closed": {"on"}})
	s.Expect("/en/", http.StatusOK, `<p class="closed">closed</p>`)

	landed = send(t, s, "/_/remarks/post", url.Values{"subject": {home}, "name": {"Cy"}, "body": {"Late"}, "back": {"/en/"}})
	if landed != "/en/?remarks=closed" {
		t.Fatalf("closed: landed on %s", landed)
	}

	// Five posts in ten minutes is the limit per visitor.
	landed = send(t, s, "/_/remarks/post", url.Values{"subject": {home}, "name": {"Cy"}, "body": {"Again"}, "back": {"/en/"}})
	if landed != "/en/?remarks=throttled" {
		t.Fatalf("sixth post: %s", landed)
	}

	s.Expect(landed, http.StatusOK, `<p class="result">throttled</p>`)

	// The overview links to the instance's part of the trash.
	s.Expect("/admin/remarks", http.StatusOK, `href="/admin/trash?section=remarks"`)

	// Deleting moves the comment and its replies to the trash.
	status, body := s.PostStatus("/admin/remarks?filter=all", "/admin/remarks/"+ada+"/delete?filter=all", url.Values{})
	if status != http.StatusOK || !sitetest.HasToast(body, "success", "Moved to the trash.") {
		t.Fatalf("delete: %d\n%s", status, body)
	}

	s.ExpectMissing("/admin/remarks?filter=all", "Thanks for writing.")
	s.ExpectMissing("/admin/remarks?filter=all", "Me too")
	s.Expect("/admin/trash?section=remarks", http.StatusOK, "Ada: Hello world &lt;b&gt;bold&lt;/b&gt;",
		`value="remarks:`+ada+`"`)
	s.ExpectMissing("/admin/trash?section=remarks", "Me too")

	// Restoring brings back the whole thread.
	s.Post("/admin/trash?section=remarks", "/admin/trash?section=remarks", url.Values{
		"action": {"restore"},
		"item":   {"remarks:" + ada},
	})
	s.Expect("/admin/remarks?filter=all", http.StatusOK, "Hello", "Thanks for writing.", "Me too")

	// Deleting for good empties the trash.
	s.Post("/admin/remarks?filter=all", "/admin/remarks/"+ada+"/delete?filter=all", url.Values{})

	status, body = s.PostStatus("/admin/trash", "/admin/trash", url.Values{"action": {"purge"}, "item": {"remarks:" + ada}})
	if status != http.StatusOK || !sitetest.HasToast(body, "success", "1 item deleted for good.") {
		t.Fatalf("purge: %d\n%s", status, body)
	}

	s.ExpectMissing("/admin/trash?section=remarks", "Hello world")
	s.ExpectMissing("/admin/remarks?filter=all", "Me too")

	if len(s.Mail.Sent()) != 2 {
		t.Fatalf("mails: %+v", s.Mail.Sent())
	}
}

func TestGuestbook(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, comments.Module("guestbook", "Guestbook"))

	// Without moderation entries are shown right away.
	s.Submit("/admin/guestbook/settings", url.Values{})
	s.Mount("guestbook", "Guestbook", "guestbook")
	s.Expect(
		"/en/guestbook",
		http.StatusOK,
		"<h1>Guestbook</h1>",
		`<p class="empty">`,
		`id="guestbook-subject"`,
		`value="guestbook"`,
		`name="back" value="/en/guestbook#guestbook"`,
	)

	// The title is the published page's, not a newer draft's.
	s.DraftPage("Guestbook", "Guestbook (draft)", "guestbook")
	s.ExpectMissing("/en/guestbook", "Guestbook (draft)")

	landed := send(t, s, "/_/guestbook/post", url.Values{
		"subject": {"guestbook"},
		"name":    {"Ada"},
		"body":    {"Lovely site"},
		"back":    {"/en/guestbook"},
	})
	if landed != "/en/guestbook?guestbook=sent" {
		t.Fatalf("sign: landed on %s", landed)
	}

	s.Expect(landed, http.StatusOK, `<p class="result">sent</p>`, "<strong>Ada</strong><p>Lovely site</p>")

	// The admin names the guestbook's own subject by the instance title.
	s.Expect("/admin/guestbook", http.StatusOK, `break-all">Guestbook</h2>`)
	s.ExpectMissing("/admin/guestbook", `break-all">guestbook</h2>`)

	service := comments.Of(s.App, "guestbook")

	for range 20 {
		_, err := service.Add(context.Background(), comments.Entry{
			Subject:  "guestbook",
			Name:     "Imported",
			Body:     "Old entry",
			Locale:   "en",
			Approved: true,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// The newest entries come first, 20 per page. The pager links to the
	// page itself by the list's route name; the ends have no link onwards.
	s.Expect("/en/guestbook", http.StatusOK, `class="older" href="/en/guestbook?page=2"`)
	s.ExpectMissing("/en/guestbook", "Lovely site")
	s.ExpectMissing("/en/guestbook", `class="newer"`)
	s.Expect("/en/guestbook?page=2", http.StatusOK, "Lovely site", `class="newer" href="/en/guestbook?page=1"`)
	s.ExpectMissing("/en/guestbook?page=2", `class="older"`)

	if len(s.Mail.Sent()) != 0 {
		t.Fatalf("mail without a notification address: %+v", s.Mail.Sent())
	}
}

// send posts a visitor's form and returns where the visitor landed.
func send(t *testing.T, s *sitetest.Site, path string, values url.Values) string {
	t.Helper()

	status, body, landed := s.Send(path, values)
	if status != http.StatusOK {
		t.Fatalf("POST %s: %d\n%s", path, status, body)
	}

	return landed
}

// TestFormResults shows every result of a post in the form component, also
// a "closed" answer after the instance opened again, and nothing for a
// made-up result.
func TestFormResults(t *testing.T) {
	dir := t.TempDir()

	page := `<main id="content">{{commentForm "remarks" "home"}}</main>`
	if err := os.WriteFile(filepath.Join(dir, "page.gohtml"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir}, comments.Module("remarks", "Comments"))

	results := map[string]string{
		"sent":      "Thank you for your comment.",
		"pending":   "Your comment is shown after approval.",
		"invalid":   "Please fill in your name and a comment.",
		"closed":    "Comments were closed when you posted.",
		"throttled": "You posted several comments already.",
	}

	for result, text := range results {
		s.Expect("/en/?remarks="+result, http.StatusOK, `<p class="wt-notice" role="status">`, text)
	}

	s.ExpectMissing("/en/?remarks=made-up", "wt-notice")
}

// TestInvalidPostKept checks that a post with mistakes returns to its form
// with the visitor's values and the messages at their fields, once.
func TestInvalidPostKept(t *testing.T) {
	dir := t.TempDir()

	page := `<main id="content">{{commentForm "remarks" "home"}}</main>`
	if err := os.WriteFile(filepath.Join(dir, "page.gohtml"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir}, comments.Module("remarks", "Comments"))
	visitor := s.Visitor()

	post := url.Values{
		"subject": {"home"},
		"name":    {""},
		"email":   {"jane@example.com"},
		"body":    {"Hello <there>"},
		"back":    {"/en/#remarks"},
	}

	parts := []string{
		"Please fill in your name and a comment.",
		`aria-invalid="true"`,
		"This field is required.",
		`value="jane@example.com"`,
		"Hello &lt;there&gt;</textarea>",
		`name="subject" value="home"`,
	}

	status, body, landed := visitor.Send("/_/remarks/post", post)
	if status != http.StatusOK || landed != "/en/?remarks=invalid" {
		t.Fatalf("post: %d %s", status, landed)
	}

	for _, part := range parts {
		if !strings.Contains(body, part) {
			t.Fatalf("the page misses %q:\n%s", part, body)
		}
	}

	// It shows once.
	_, again := visitor.Get(landed)
	if strings.Contains(again, "jane@example.com") || strings.Contains(again, "This field is required.") {
		t.Fatalf("shown twice:\n%s", again)
	}

	// With HTMX the part of the page HTMX swaps in shows it.
	status, body, landed = visitor.SendHTMX("/_/remarks/post", post, "remarks")
	if status != http.StatusOK || landed != "/en/?remarks=invalid" || strings.Contains(body, "<body") {
		t.Fatalf("HTMX post: %d %s\n%s", status, landed, body)
	}

	for _, part := range parts {
		if !strings.Contains(body, part) {
			t.Fatalf("the HTMX answer misses %q:\n%s", part, body)
		}
	}
}
