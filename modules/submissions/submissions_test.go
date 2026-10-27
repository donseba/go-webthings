package submissions_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/modules/submissions"
	"github.com/donseba/go-webthings/sitetest"
)

func TestSubmissions(t *testing.T) {
	translations, err := frontend.Translations("testdata/translations")
	if err != nil {
		t.Fatal(err)
	}

	options := sitetest.Options{
		Templates:      "testdata/templates",
		StartingPoints: []string{"news"},
		Configure: func(config *webthings.Config) {
			config.Translations = translations
		},
	}

	s := sitetest.StartWith(t, options,
		publishing.Module("news", "News"),
		submissions.Module("tips", "News tips", "news"),
	)

	settings := "/admin/tips/settings"

	code, body := s.PostStatus(settings, settings, url.Values{"Notify": {"not an address"}})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid notification address: %d\n%s", code, body)
	}

	s.Post(settings, settings, url.Values{
		"Notify":       {"editors@example.test"},
		"ConfirmMail":  {"on"},
		"DecisionMail": {"on"},
	})
	s.Expect(settings, http.StatusOK, `value="editors@example.test"`, `name="ConfirmMail"`)

	s.Mount("tips", "Tips", "tips")
	s.Mount("news", "News", "news")
	s.Expect("/en/tips", http.StatusOK,
		"<h1>Tips</h1>",
		`action="/_/tips/submit"`,
		`name="website"`,
		`name="back" value="/en/tips#tips"`,
		`id="tips-title"`,
		`maxlength="200"`,
	)

	proposal := url.Values{
		"name":  {"Jane"},
		"email": {"jane@example.test"},
		"title": {"Street party"},
		"text":  {"Everyone is *welcome* on Saturday."},
		"back":  {"/en/tips"},
	}

	if landed := propose(t, s, proposal, sitetest.PNG(t, 4, 3)); landed != "/en/tips?tips=sent" {
		t.Fatalf("proposal: %s", landed)
	}

	s.Expect("/en/tips?tips=sent", http.StatusOK, `<p id="result">sent</p>`, `<p id="func-result">sent</p>`)

	confirmation, notification := sent(t, s, 2)[0], sent(t, s, 2)[1]
	if confirmation.To[0] != "jane@example.test" || confirmation.Subject != "We received your proposal: Street party" ||
		!strings.HasPrefix(confirmation.Text, "Hello Jane,\n") {
		t.Fatalf("confirmation: %+v", confirmation)
	}

	if notification.To[0] != "editors@example.test" || notification.ReplyTo != "jane@example.test" ||
		notification.Subject != "New proposal: Street party" ||
		!strings.Contains(notification.Text, "Everyone is *welcome* on Saturday.") ||
		!strings.Contains(notification.Text, "/media/") ||
		!strings.Contains(notification.Text, "/admin/tips/") {
		t.Fatalf("notification: %+v", notification)
	}

	// The image is in the media library.
	assets, err := s.App.Media.List(context.Background())
	if err != nil || len(assets) != 1 || !assets[0].IsImage() {
		t.Fatalf("media: %+v %v", assets, err)
	}

	// Missing fields and files that are not images are refused; spam is
	// thanked; none of them is stored or mailed.
	incomplete := clone(proposal)
	incomplete.Del("title")

	response := postProposal(t, s, incomplete, nil)
	if landed := response.Header().Get("Location"); landed != "/en/tips?tips=invalid" {
		t.Fatalf("incomplete proposal: %d %s", response.Code, landed)
	}

	s.Expect("/en/tips?tips=invalid", http.StatusOK, `<p id="result">invalid</p>`)

	// The form shows the proposal again, with the message at its field.
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("incomplete proposal kept in %v", cookies)
	}

	request := httptest.NewRequest(http.MethodGet, "/en/tips?tips=invalid", nil)
	request.AddCookie(cookies[0])

	form := httptest.NewRecorder()
	s.App.ServeHTTP(form, request)

	for _, part := range []string{`value="Jane"`, `value="jane@example.test"`, "Everyone is *welcome* on Saturday.</textarea>", "This field is required."} {
		if !strings.Contains(form.Body.String(), part) {
			t.Fatalf("the form misses %q:\n%s", part, form.Body.String())
		}
	}

	// A text of almost 20000 characters comes back whole, without
	// JavaScript and with HTMX.
	long := clone(incomplete)
	long.Set("text", strings.Repeat("A long tip. ", 1665))
	long.Set("back", "/en/tips#tips")

	status, page, landed := s.Visitor().Send("/_/tips/submit", long)
	if status != http.StatusOK || landed != "/en/tips?tips=invalid" || !strings.Contains(page, long.Get("text")+"</textarea>") {
		t.Fatalf("long proposal: %d %s", status, landed)
	}

	status, page, _ = s.Visitor().SendHTMX("/_/tips/submit", long, "tips")
	if status != http.StatusOK || strings.Contains(page, "<body") || !strings.Contains(page, long.Get("text")+"</textarea>") ||
		!strings.Contains(page, "This field is required.") {
		t.Fatalf("long proposal with HTMX: %d", status)
	}

	if landed := propose(t, s, proposal, []byte("just some text")); landed != "/en/tips?tips=invalid" {
		t.Fatalf("text as image: %s", landed)
	}

	spam := clone(proposal)
	spam.Set("website", "https://spam.example")
	spam.Set("title", "Buy now")

	if landed := propose(t, s, spam, nil); landed != "/en/tips?tips=sent" {
		t.Fatalf("spam: %s", landed)
	}

	if status := s.CrossSite("/_/tips/submit", proposal); status != http.StatusForbidden {
		t.Fatalf("cross-site post: %d", status)
	}

	service := submissions.Of(s.App, "tips")

	if count, err := service.Count(context.Background(), ""); err != nil || count != 1 {
		t.Fatalf("stored proposals: %d %v", count, err)
	}

	sent(t, s, 2)

	// A Dutch proposal keeps its language; this one will be rejected.
	dutch := clone(proposal)
	dutch.Set("title", "Rommelmarkt")
	dutch.Set("back", "/nl/tips")

	if landed := propose(t, s, dutch, nil); landed != "/nl/tips?tips=sent" {
		t.Fatalf("dutch proposal: %s", landed)
	}

	// Each visitor can send only a few proposals per hour.
	if landed := propose(t, s, proposal, nil); landed != "/en/tips?tips=throttled" {
		t.Fatalf("throttle: %s", landed)
	}

	s.Expect("/en/tips?tips=throttled", http.StatusOK, `<p id="result">throttled</p>`)

	// The confirmation is in the proposal's language, the notification in
	// the admin's.
	if mails := sent(t, s, 4); mails[2].Subject != "We hebben je voorstel ontvangen: Rommelmarkt" ||
		!strings.HasPrefix(mails[2].Text, "Hallo Jane,\n") || mails[3].Subject != "New proposal: Rommelmarkt" {
		t.Fatalf("dutch mails: %+v", mails[2:])
	}

	// New proposals are counted in the list and next to the section.
	s.Expect("/admin/tips", http.StatusOK, "Street party", "Rommelmarkt", `<span class="badge">2</span>`,
		`<span class="nav-badge">2</span>`)
	s.ExpectMissing("/admin/tips", "Buy now")

	list, _, err := service.List(context.Background(), submissions.StatusNew, 0, 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}

	market, party := list[0], list[1]
	if market.Locale != "nl" || party.Locale != "en" {
		t.Fatalf("locales: %s %s", market.Locale, party.Locale)
	}

	detail := "/admin/tips/" + party.ID
	s.Expect(detail, http.StatusOK, "Street party", "jane@example.test", "Everyone is *welcome* on Saturday.", `src="/media/`)

	// Accepting creates a draft post in the news admin that is not public yet.
	s.Post(detail, detail+"/accept", url.Values{})

	accepted, err := service.Get(context.Background(), party.ID)
	if err != nil || accepted.Status != submissions.StatusAccepted || accepted.PostID == "" {
		t.Fatalf("accepted: %+v %v", accepted, err)
	}

	editor := "/admin/news/" + accepted.PostID + "?locale=en"
	s.Expect(detail, http.StatusOK, `href="`+strings.ReplaceAll(editor, "&", "&amp;")+`"`, "admin@example.test")
	s.Expect("/admin/news", http.StatusOK, "Street party")
	s.Expect(editor, http.StatusOK, `value="street-party"`, "Everyone is *welcome* on Saturday.")
	s.ExpectMissing("/en/news", "Street party")
	s.Expect("/en/news/street-party", http.StatusNotFound)

	if mails := sent(t, s, 5); mails[4].To[0] != "jane@example.test" ||
		mails[4].Subject != "Your proposal was accepted: Street party" {
		t.Fatalf("accepted mail: %+v", mails[4])
	}

	// Accepting twice is refused without a second post.
	code, _ = s.PostStatus(detail, detail+"/accept", url.Values{})
	if code != http.StatusConflict {
		t.Fatalf("accept twice: %d", code)
	}

	s.Publish(editor)
	s.Expect("/en/news/street-party", http.StatusOK, "<em>welcome</em>")

	// Rejecting tells the submitter; deleting removes the proposal.
	rejected := "/admin/tips/" + market.ID
	s.Post(rejected, rejected+"/reject", url.Values{})
	s.Expect("/admin/tips?status=rejected", http.StatusOK, "Rommelmarkt")
	s.ExpectMissing("/admin/tips", "Rommelmarkt")

	if mails := sent(t, s, 6); mails[5].Subject != "About your proposal: Rommelmarkt" {
		t.Fatalf("rejected mail: %+v", mails[5])
	}

	// Deleting moves the proposal to the instance's part of the trash.
	s.Expect("/admin/tips", http.StatusOK, `href="/admin/trash?section=tips"`)

	code, body = s.PostStatus(rejected, rejected+"/delete", url.Values{})
	if code != http.StatusOK || !sitetest.HasToast(body, "success", "Moved to the trash.") {
		t.Fatalf("delete: %d\n%s", code, body)
	}

	s.ExpectMissing("/admin/tips?status=all", "Rommelmarkt")
	s.Expect("/admin/tips?status=all", http.StatusOK, "Street party")
	s.ExpectMissing("/admin/tips", `class="nav-badge"`)
	s.Expect(rejected, http.StatusNotFound)
	s.Expect("/admin/trash?section=tips", http.StatusOK, "Rommelmarkt (Jane)", `value="tips:`+market.ID+`"`)

	s.Post("/admin/trash?section=tips", "/admin/trash?section=tips", url.Values{
		"action": {"restore"},
		"item":   {"tips:" + market.ID},
	})
	s.Expect("/admin/tips?status=rejected", http.StatusOK, "Rommelmarkt")

	// Deleting the accepted proposal for good leaves its post alone.
	s.Post(rejected, rejected+"/delete", url.Values{})
	s.Post(detail, detail+"/delete", url.Values{})

	code, body = s.PostStatus("/admin/trash", "/admin/trash", url.Values{
		"action": {"purge"},
		"item":   {"tips:" + market.ID, "tips:" + party.ID},
	})
	if code != http.StatusOK || !sitetest.HasToast(body, "success", "2 items deleted for good.") {
		t.Fatalf("purge: %d\n%s", code, body)
	}

	s.ExpectMissing("/admin/trash?section=tips", "Rommelmarkt")
	s.Expect("/en/news/street-party", http.StatusOK, "<em>welcome</em>")
	s.Expect("/admin/news", http.StatusOK, "Street party")
}

// TestTarget checks that accepting fails clearly when the target is no
// Publishing instance.
// TestAdminRights checks that only editors who may save the settings open
// them, and that times are shown in the site's timezone.
func TestAdminRights(t *testing.T) {
	s := sitetest.StartWith(
		t,
		sitetest.Options{Templates: "testdata/templates", StartingPoints: []string{"news"}},
		publishing.Module("news", "News"),
		submissions.Module("tips", "News tips", "news"),
	)
	ctx := context.Background()

	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// UTC+14 differs from any server's zone used to run the tests.
	settings.Timezone = "Pacific/Kiritimati"
	if err := s.App.Site.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}

	submission, err := submissions.Of(s.App, "tips").Create(ctx, submissions.Proposal{
		Name:   "Jane",
		Email:  "jane@example.test",
		Title:  "Street party",
		Body:   "On Saturday.",
		Locale: "en",
	})
	if err != nil {
		t.Fatal(err)
	}

	zone, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		t.Fatal(err)
	}

	// The admin is English: "6 Oct 2026 11:58".
	local := submission.CreatedAt.In(zone)
	created := frontend.FormatDate("en", local, "medium") + " " + local.Format("15:04")
	s.Expect("/admin/tips", http.StatusOK, created, `href="/admin/tips/settings"`)
	s.Expect("/admin/tips/"+submission.ID, http.StatusOK, created)

	group := s.Submit("/admin/groups", url.Values{"Name": {"Readers"}})
	s.Post(group, group, url.Values{"name": {"Readers"}, "allow": {"tips:read"}})

	account := s.Submit("/admin/accounts", url.Values{
		"Email":    {"reader@example.test"},
		"Password": {"reader long passphrase"},
		"Role":     {"editor"},
	})
	s.Post(account, account+"/groups", url.Values{"group": {group[strings.LastIndex(group, "/")+1:]}})

	reader := s.As("reader@example.test", "reader long passphrase")
	reader.Expect("/admin/tips", http.StatusOK, "Street party")
	reader.ExpectMissing("/admin/tips", "/admin/tips/settings")

	if status, _ := reader.Get("/admin/tips/settings"); status != http.StatusForbidden {
		t.Fatalf("settings for a reader: %d", status)
	}
}

func TestTarget(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, submissions.Module("tips", "News tips", "news"))

	created, err := submissions.Of(s.App, "tips").Create(context.Background(), submissions.Proposal{
		Name:   "Jane",
		Email:  "jane@example.test",
		Title:  "Street party",
		Body:   "Saturday",
		Locale: "en",
	})
	if err != nil {
		t.Fatal(err)
	}

	detail := "/admin/tips/" + created.ID

	code, body := s.PostStatus(detail, detail+"/accept", url.Values{})
	if code != http.StatusInternalServerError || !strings.Contains(body, "Accepted proposals go to news") {
		t.Fatalf("accept without target: %d\n%s", code, body)
	}
}

// TestFormTemplate checks that the generated starting point for
// submissions/form.gohtml renders a working form.
func TestFormTemplate(t *testing.T) {
	dir := t.TempDir()
	options := sitetest.Options{
		Templates: dir,
		Prepare: func(app *webthings.App) error {
			_, err := app.WriteTemplates(dir, "tips")

			return err
		},
	}

	s := sitetest.StartWith(t, options, publishing.Module("news", "News"), submissions.Module("tips", "News tips", "news"))

	s.Mount("tips", "Tips", "tips")
	s.Expect("/en/tips", http.StatusOK,
		"<h1>Tips</h1>",
		`action="/_/tips/submit"`,
		`enctype="multipart/form-data"`,
		`name="website"`,
		`id="tips-image"`,
		`accept="image/jpeg,image/png,image/gif,.jpg,.jpeg,.png,.gif"`,
	)
	s.Expect("/en/tips?tips=sent", http.StatusOK, "Thank you!")
	s.Expect("/en/tips?tips=throttled", http.StatusOK, "Please try again later.")
}

// propose posts a visitor's proposal as multipart form, with image when set,
// and returns where the visitor is sent, or the status when not redirected.
func propose(t *testing.T, s *sitetest.Site, values url.Values, image []byte) string {
	t.Helper()

	response := postProposal(t, s, values, image)
	if response.Code != http.StatusSeeOther {
		return strconv.Itoa(response.Code)
	}

	return response.Header().Get("Location")
}

// postProposal posts a visitor's proposal as multipart form, with image
// when set, and returns the response.
func postProposal(t *testing.T, s *sitetest.Site, values url.Values, image []byte) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer

	writer := multipart.NewWriter(&body)

	for name, list := range values {
		for _, value := range list {
			if err := writer.WriteField(name, value); err != nil {
				t.Fatal(err)
			}
		}
	}

	if image != nil {
		part, err := writer.CreateFormFile("image", "party.png")
		if err != nil {
			t.Fatal(err)
		}

		if _, err := part.Write(image); err != nil {
			t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/_/tips/submit", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())

	response := httptest.NewRecorder()
	s.App.ServeHTTP(response, request)

	return response
}

// sent returns the site's mail after checking how many were sent.
func sent(t *testing.T, s *sitetest.Site, want int) []mail.Message {
	t.Helper()

	messages := s.Mail.Sent()
	if len(messages) != want {
		t.Fatalf("want %d mails, got %d: %+v", want, len(messages), messages)
	}

	return messages
}

func clone(values url.Values) url.Values {
	copied := url.Values{}

	for name, list := range values {
		copied[name] = append([]string(nil), list...)
	}

	return copied
}
