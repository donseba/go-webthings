package registrations_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/redirects"
	"github.com/donseba/go-webthings/modules/registrations"
	"github.com/donseba/go-webthings/modules/stats"
	"github.com/donseba/go-webthings/sitetest"
)

var landedPattern = regexp.MustCompile(`^/en/registration/([A-Z2-9]{4}-[A-Z2-9]{4})\?signup=registered$`)

func TestRegistrations(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"},
		events.Module("agenda", "Agenda"),
		registrations.Module("signup", "Sign-ups", registrations.ForEvents("agenda")),
	)

	day := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)

	editor := s.Submit("/admin/agenda/new?locale=en", url.Values{
		"Title":    {"Summer fair"},
		"Slug":     {"summer-fair"},
		"Start":    {day + "T14:30"},
		"Timezone": {"Europe/Amsterdam"},
	})
	s.Publish(editor + "?locale=en")

	event := editor[strings.LastIndex(editor, "/")+1:]
	admin := "/admin/signup/" + event
	page := "/en/agenda/summer-fair"

	s.Mount("agenda", "Agenda", "agenda")
	s.Mount("signup", "My registration", "registration")

	amsterdam, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}

	start, err := time.ParseInLocation("2006-01-02T15:04", day+"T14:30", amsterdam)
	if err != nil {
		t.Fatal(err)
	}

	// Events can be picked in the admin; registration is closed until opened.
	// Dates are shown in the admin language.
	adminDate := frontend.FormatDate("en", start, "medium") + " 14:30"
	s.Expect("/admin/signup", http.StatusOK, `href="`+admin+`">Summer fair`, adminDate, "Closed")
	s.Expect(page, http.StatusOK, "No registration.")

	// What registrants read is written per language, before or after the
	// other settings. The notes are Markdown, written in the editor.
	s.Expect(admin+"/settings?locale=en", http.StatusOK, `name="Notes"`, `markdown"`)
	s.Post(admin+"/settings", admin+"/settings/texts?locale=en", url.Values{
		"PaymentLink": {"https://pay.example/fair"},
		"Notes":       {"Bring a *cup*"},
	})
	s.Expect(page, http.StatusOK, "Registration is closed.")

	// A price ParseCents cannot read is refused at its field.
	status, body := s.PostStatus(
		admin+"/settings",
		admin+"/settings",
		url.Values{"AdultPrice": {"12.505"}, "ChildPrice": {"five"}},
	)
	if status != http.StatusUnprocessableEntity || strings.Count(body, "Enter an amount such as 12.50.") != 2 {
		t.Fatalf("invalid prices: %d\n%s", status, body)
	}

	landed := s.Submit(admin+"/settings", url.Values{
		"Open":          {"on"},
		"Capacity":      {"3"},
		"AdultPrice":    {"12,50"},
		"ChildPrice":    {"5"},
		"AllowChildren": {"on"},
	})
	if landed != admin {
		t.Fatalf("settings saved to %s", landed)
	}

	s.Post(admin+"/settings?locale=nl", admin+"/settings/texts?locale=nl", url.Values{"Notes": {"Neem een beker mee"}})
	s.Expect(admin+"/settings?locale=nl", http.StatusOK, "Neem een beker mee")
	s.Expect(admin+"/settings", http.StatusOK, "https://pay.example/fair")

	s.Expect(
		page,
		http.StatusOK,
		`action="/_/signup/register"`,
		"adult €12.50 child €5.00",
		"3 places left",
		"Bring a <em>cup</em>",
		// The form limits the adults and the children to the places left.
		`id="signup-adults"`,
		`max="3"`,
		`name="back" value="`+page+`#signup"`,
	)

	register := func(values url.Values) string {
		t.Helper()

		values.Set("subject", event)
		values.Set("back", page)

		status, body, landed := s.Send("/_/signup/register", values)
		if status != http.StatusOK {
			t.Fatalf("register: %d %s", status, body)
		}

		return landed
	}

	landed = register(url.Values{
		"name":     {"Ada Lovelace"},
		"email":    {"ada@example.com"},
		"adults":   {"1"},
		"children": {"1"},
		"notes":    {"Vegetarian"},
	})

	match := landedPattern.FindStringSubmatch(landed)
	if match == nil {
		t.Fatalf("registered, landed on %s", landed)
	}

	code := match[1]

	s.Expect(landed, http.StatusOK, "result: registered", code+" Ada Lovelace adults 1 children 1 total €17.50 pending",
		"https://pay.example/fair", // The test site's timezone is UTC, so the date is today in UTC.
		"registered "+frontend.FormatDate("en", time.Now().UTC(), "long"),
		`<div class="info"><p>Bring a <em>cup</em></p>`,
		// The cancel form, with the code, returns to the instance's element.
		`<section id="signup">`, `name="back" value="/en/registration/`+code+`#signup"`, `value="`+code+`"`)

	sent := s.Mail.Sent()
	if len(sent) != 1 || sent[0].To[0] != "ada@example.com" || sent[0].Subject != "Registration "+code+": Summer fair" {
		t.Fatalf("confirmation: %+v", sent)
	}

	when := frontend.FormatDate("en", start, "full") + " · 14:30"

	for _, part := range []string{
		"Hello Ada Lovelace,", "Summer fair", when, code, "Total: €17.50", "https://pay.example/fair", "Bring a *cup*",
		"http://", "/en/registration/" + code,
	} {
		if !strings.Contains(sent[0].Text, part) {
			t.Fatalf("confirmation misses %q:\n%s", part, sent[0].Text)
		}
	}

	// Codes are found as people type them.
	loose := url.QueryEscape(strings.ToLower(strings.ReplaceAll(code, "-", " ")))
	s.Expect("/en/registration?code="+loose, http.StatusOK, "Ada Lovelace")
	s.Expect("/en/registration/AAAA-AAAA", http.StatusNotFound, "No registration has the code AAAA-AAAA.")

	// The lookup form asks the page itself, also from a code's page.
	s.Expect("/en/registration", http.StatusOK, `action="/en/registration"`)
	s.Expect("/en/registration/AAAA-AAAA", http.StatusNotFound, `action="/en/registration"`)

	// It is go-form's form of LookupInput, sent with method get, with the
	// code looked up.
	s.Expect(
		"/en/registration/AAAA-AAAA",
		http.StatusNotFound,
		`method="GET"`,
		`id="registration-code"`,
		`name="code"`,
		`value="AAAA-AAAA"`,
		`placeholder="K7Q4-9M2X"`,
		`autocomplete="off"`,
		`maxlength="20"`,
		"Registration code",
	)
	s.Expect(page, http.StatusOK, "1 places left")

	// Not enough places, missing fields and closed subjects return to the form.
	if landed := register(
		url.Values{"name": {"Bob"}, "email": {"bob@example.com"}, "adults": {"2"}},
	); landed != page+"?signup=full" {
		t.Fatalf("over capacity: %s", landed)
	}

	// A sign-up with mistakes returns to the form with the visitor's values
	// and the message at its field, once.
	status, body, landed = s.Send("/_/signup/register", url.Values{
		"subject": {event},
		"name":    {"Bob"},
		"adults":  {"2"},
		"back":    {page + "#signup"},
	})
	if status != http.StatusOK || landed != page+"?signup=invalid" {
		t.Fatalf("without e-mail: %d %s", status, landed)
	}

	for _, part := range []string{"result: invalid", `value="Bob"`, `value="2"`, "This field is required."} {
		if !strings.Contains(body, part) {
			t.Fatalf("the form misses %q:\n%s", part, body)
		}
	}

	s.ExpectMissing(landed, `value="Bob"`)

	// With HTMX the part of the page HTMX swaps in shows it.
	status, body, _ = s.Visitor().SendHTMX("/_/signup/register", url.Values{
		"subject": {event},
		"name":    {"Bob"},
		"adults":  {"2"},
		"back":    {page + "#signup"},
	}, "signup")
	if status != http.StatusOK || strings.Contains(body, "<body") ||
		!strings.Contains(body, `value="Bob"`) || !strings.Contains(body, "This field is required.") {
		t.Fatalf("HTMX sign-up without e-mail: %d\n%s", status, body)
	}

	s.Expect(page+"?signup=full", http.StatusOK, "result: full")

	// Bots are thanked, nothing is stored or sent.
	spam := register(url.Values{"name": {"Bot"}, "email": {"bot@example.com"}, "website": {"http://spam.example"}})
	if spam != page+"?signup=registered" || len(s.Mail.Sent()) != 1 {
		t.Fatalf("spam: %s, %d mails", spam, len(s.Mail.Sent()))
	}

	if status := s.CrossSite(
		"/_/signup/register",
		url.Values{"subject": {event}, "name": {"Mallory"}},
	); status != http.StatusForbidden {
		t.Fatalf("cross-site post: %d", status)
	}

	service := registrations.Of(s.App, "signup")

	ada, err := service.ByCode(context.Background(), code)
	if err != nil {
		t.Fatal(err)
	}

	// Editors follow registrations, mark payments and export them.
	s.Expect("/admin/signup", http.StatusOK, ">1<", "2 / 3")
	s.Expect(admin, http.StatusOK, code, "Ada Lovelace", "ada@example.com", "€17.50", "Pending", "Export CSV",
		"https://pay.example/fair")

	s.Submit(admin+"/"+ada.ID, url.Values{"Payment": {"paid"}, "AdminNotes": {"Paid at the door"}})
	s.Expect(admin+"/"+ada.ID, http.StatusOK, "Paid at the door", "Vegetarian")

	status, csv := s.Get(admin + "/export.csv")
	if status != http.StatusOK ||
		!strings.Contains(csv, code+",Ada Lovelace,ada@example.com,1,1,17.50,paid,Vegetarian,Paid at the door,en,") {
		t.Fatalf("export: %d\n%s", status, csv)
	}

	// Visitors cancel with their e-mail address as confirmation.
	cancel := func(email string) string {
		t.Helper()

		_, _, landed := s.Send(
			"/_/signup/cancel",
			url.Values{"code": {code}, "email": {email}, "back": {"/en/registration/" + code}},
		)

		return landed
	}

	// Without an address the cancel form says so at its field.
	_, body, _ = s.Send("/_/signup/cancel", url.Values{"code": {code}, "back": {"/en/registration/" + code + "#signup"}})
	if !strings.Contains(body, "result: invalid") || !strings.Contains(body, "This field is required.") {
		t.Fatalf("cancel without an address:\n%s", body)
	}

	status, body, _ = s.Visitor().SendHTMX(
		"/_/signup/cancel",
		url.Values{"code": {code}, "back": {"/en/registration/" + code + "#signup"}},
		"signup",
	)
	if status != http.StatusOK || strings.Contains(body, "<body") || !strings.Contains(body, "This field is required.") {
		t.Fatalf("HTMX cancel without an address: %d\n%s", status, body)
	}

	if landed := cancel("eve@example.com"); landed != "/en/registration/"+code+"?signup=invalid" {
		t.Fatalf("cancel with another address: %s", landed)
	}

	if landed := cancel("ADA@example.com"); landed != "/en/registration/"+code+"?signup=cancelled" {
		t.Fatalf("cancel: %s", landed)
	}

	s.Expect("/en/registration/"+code+"?signup=cancelled", http.StatusOK, "result: cancelled", "cancelled", "paid")
	s.ExpectMissing("/en/registration/"+code, "/_/signup/cancel")
	s.Expect(page, http.StatusOK, "3 places left")

	sent = s.Mail.Sent()
	if len(sent) != 2 || sent[1].Subject != "Registration cancelled "+code+": Summer fair" ||
		!strings.Contains(sent[1].Text, "we will contact you about a refund") {
		t.Fatalf("cancellation mail: %+v", sent)
	}

	// A full event stops taking registrations.
	register(url.Values{"name": {"Bob"}, "email": {"bob@example.com"}, "adults": {"3"}})
	s.Expect(page, http.StatusOK, "This event is full.")

	if landed := register(url.Values{"name": {"Cy"}, "email": {"cy@example.com"}}); landed != page+"?signup=full" {
		t.Fatalf("full event: %s", landed)
	}

	list, err := service.Registrations(context.Background(), event)
	if err != nil || len(list) != 2 {
		t.Fatalf("registrations: %+v %v", list, err)
	}

	bob := admin + "/" + list[1].ID

	s.Post(bob, bob+"/cancel", url.Values{})
	s.Expect(page, http.StatusOK, "3 places left")

	// Deleting asks first and moves the registration to the trash: it leaves
	// the list, the counts, the export and the lookup by code.
	s.Expect(bob, http.StatusOK, `hx-confirm="Move this item to the trash?"`)

	status, body = s.PostStatus(bob, bob+"/delete", url.Values{})
	if status != http.StatusOK || !strings.Contains(body, "Moved to the trash.") {
		t.Fatalf("delete: %d\n%s", status, body)
	}

	s.Expect(bob, http.StatusNotFound)
	s.ExpectMissing(admin, "bob@example.com")
	s.ExpectMissing(admin+"/export.csv", "bob@example.com")
	s.Expect("/en/registration/"+list[1].Code, http.StatusNotFound)
	s.Expect("/admin/signup", http.StatusOK, `href="/admin/trash?section=signup"`)

	// The trash names who registered for what; restoring brings it back.
	s.Expect("/admin/trash?section=signup", http.StatusOK, "Bob · Summer fair", `value="signup:`+list[1].ID+`"`)
	s.ExpectMissing("/admin/trash?section=signup&q=ada", "Bob · Summer fair")

	status, body = s.PostStatus("/admin/trash?section=signup", "/admin/trash?section=signup", url.Values{
		"action": {"restore"},
		"item":   {"signup:" + list[1].ID},
	})
	if status != http.StatusOK || !strings.Contains(body, "1 item restored.") {
		t.Fatalf("restore: %d\n%s", status, body)
	}

	s.Expect(bob, http.StatusOK, "bob@example.com")
	s.Expect(page, http.StatusOK, "3 places left")
}

func TestSubjects(t *testing.T) {
	translations, err := frontend.Translations("testdata/translations")
	if err != nil {
		t.Fatal(err)
	}

	options := sitetest.Options{
		Templates: "testdata/templates",
		Configure: func(config *webthings.Config) {
			config.Translations = translations
		},
	}

	s := sitetest.StartWith(t, options, registrations.Module("tickets", "Tickets"))

	// A subject ID is checked at its field.
	status, body := s.PostStatus(
		"/admin/tickets/new",
		"/admin/tickets/new",
		url.Values{"Subject": {"concert 2026"}, "Title": {"Concert"}},
	)
	if status != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "Use letters, digits, dashes, dots, colons and underscores.") {
		t.Fatalf("subject with a space: %d\n%s", status, body)
	}

	landed := s.Submit("/admin/tickets/new", url.Values{"Subject": {" concert-2026 "}, "Title": {"Concert"}})
	if landed != "/admin/tickets/concert-2026/settings" {
		t.Fatalf("new subject: %s", landed)
	}

	// The deadline is entered and shown in the site's timezone (UTC here).
	s.Submit(landed, url.Values{
		"Title": {"Concert"}, "Open": {"on"}, "Capacity": {"0"}, "AdultPrice": {"45"}, "ClosesAt": {"2099-01-01T20:00"},
	})
	s.Expect(landed, http.StatusOK, `value="2099-01-01T20:00"`)

	s.Submit(landed, url.Values{"Title": {"Concert"}, "Open": {"on"}, "Capacity": {"0"}, "AdultPrice": {"45"}})
	s.Post(landed, landed+"/texts?locale=en", url.Values{"Notes": {"Doors open at eight."}})
	s.Expect("/admin/tickets", http.StatusOK, "Concert", "Open")

	register := func() (int, string) {
		status, _, back := s.Send("/_/tickets/register", url.Values{
			"subject": {"concert-2026"},
			"name":    {"Ada"},
			"email":   {"ada@example.com"},
			"back":    {"/nl/"},
		})

		return status, back
	}

	// Without a lookup page, visitors return to the form with their code.
	status, back := register()

	match := regexp.MustCompile(`^/nl/\?code=([A-Z2-9]{4}-[A-Z2-9]{4})&tickets=registered$`).FindStringSubmatch(back)
	if status != http.StatusOK || match == nil {
		t.Fatalf("register: %d %s", status, back)
	}

	s.Expect(back, http.StatusOK, "result: registered", "code: "+match[1])
	s.ExpectMissing("/nl/?tickets=registered&code=nonsense", "code:")
	s.ExpectMissing("/nl/?tickets=made-up", "result:")

	// The confirmation is in the visitor's language, with the notes of the
	// default language when the visitor's has none.
	sent := s.Mail.Sent()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "Hallo Ada,") ||
		!strings.Contains(sent[0].Text, "Bedankt voor je aanmelding voor Concert.") ||
		!strings.Contains(
			sent[0].Text,
			"Totaal: "+frontend.FormatMoney("nl", 4500, "EUR"),
		) || !strings.Contains(sent[0].Text, "Doors open at eight.") ||
		strings.Contains(sent[0].Text, "/nl/") {
		t.Fatalf("Dutch confirmation: %+v", sent)
	}

	s.Expect("/admin/tickets/concert-2026", http.StatusOK, "Ada", "nl")
	s.Expect("/admin/tickets/unknown", http.StatusNotFound)

	// Visitors who post too often are sent back.
	for range 9 {
		register()
	}

	if _, back := register(); back != "/nl/?tickets=throttled" {
		t.Fatalf("throttled: %s", back)
	}
}

// TestPrivacy checks that registration pages, which hold the code in their
// address, ask not to be kept, and that statistics and the missed paths of
// redirects respect that.
func TestPrivacy(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"},
		registrations.Module("tickets", "Tickets"),
		stats.Module(),
		redirects.Module(),
	)
	ctx := context.Background()

	landed := s.Submit("/admin/tickets/new", url.Values{"Subject": {"concert-2026"}, "Title": {"Concert"}})
	s.Submit(landed, url.Values{"Title": {"Concert"}, "Open": {"on"}, "Capacity": {"0"}})
	s.Mount("tickets", "My registration", "registration")

	_, _, back := s.Send("/_/tickets/register", url.Values{
		"subject": {"concert-2026"},
		"name":    {"Ada"},
		"email":   {"ada@example.com"},
		"back":    {"/en/"},
	})

	match := regexp.MustCompile(`^/en/registration/([A-Z2-9]{4}-[A-Z2-9]{4})\?tickets=registered$`).FindStringSubmatch(back)
	if match == nil {
		t.Fatalf("register: %s", back)
	}

	pages := map[string]int{
		"/en/registration":                    http.StatusOK,
		"/en/registration/" + match[1]:        http.StatusOK,
		"/en/registration/AAAA-AAAA":          http.StatusNotFound,
		"/en/registration?code=%21":           http.StatusNotFound,
		"/en/registration/" + match[1] + "/x": http.StatusNotFound,
	}

	for path, status := range pages {
		response := browse(t, s, path)
		if response.Code != status ||
			response.Header().Get("Cache-Control") != "private, no-store" ||
			response.Header().Get("X-Robots-Tag") != "noindex" {
			t.Fatalf("%s: %d %v", path, response.Code, response.Header())
		}
	}

	// Other pages are counted and listed as before.
	browse(t, s, "/en/")
	browse(t, s, "/en/nothing-here")

	counter := stats.Of(s.App, "stats")
	if err := counter.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	report, err := counter.Report(ctx, 7)
	if err != nil || len(report.Pages) != 1 || report.Pages[0].Path != "/en/" {
		t.Fatalf("statistics: %+v %v", report.Pages, err)
	}

	moved := redirects.Of(s.App, "redirects")
	if err := moved.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	missed, err := moved.Missed(ctx, 10)
	if err != nil || len(missed) != 1 || missed[0].Path != "/en/nothing-here" {
		t.Fatalf("missed paths: %+v %v", missed, err)
	}
}

// browse requests path as a visitor's browser, which statistics count.
func browse(t *testing.T, s *sitetest.Site, path string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.RemoteAddr = "192.0.2.10:52000"
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 14.6; rv:131.0) Gecko/20100101 Firefox/131.0")

	response := httptest.NewRecorder()
	s.App.ServeHTTP(response, request)

	return response
}

// TestFormResults checks the notices of the default form component.
func TestFormResults(t *testing.T) {
	dir := t.TempDir()
	page := `<main id="content">{{registrationForm "tickets" "concert-2026"}}</main>`

	if err := os.WriteFile(filepath.Join(dir, "page.gohtml"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir}, registrations.Module("tickets", "Tickets"))

	err := registrations.Of(s.App, "tickets").SaveSettings(context.Background(), registrations.Settings{
		SubjectID: "concert-2026",
		Title:     "Concert",
		Open:      true,
	})
	if err != nil {
		t.Fatal(err)
	}

	s.Expect("/en/?tickets=cancelled", http.StatusOK, "Your registration is cancelled.")
	s.Expect("/en/?tickets=closed", http.StatusOK, "Registration is closed.")
	s.ExpectMissing("/en/?tickets=cancelled", "Please check your name")
}

// TestCurrency checks that only ISO 4217 codes are accepted.
func TestCurrency(t *testing.T) {
	for _, code := range []string{"ABC", "EURO", "€"} {
		module := registrations.Module("tickets", "Tickets", registrations.Currency(code))

		if _, err := module.Setup(webthings.Env{Key: "tickets"}); err == nil {
			t.Errorf("currency %q accepted", code)
		}
	}
}

// TestNoEvents checks that a link to a key without events fails clearly.
func TestNoEvents(t *testing.T) {
	s := sitetest.Start(t, registrations.Module("signup", "Sign-ups", registrations.ForEvents("agenda")))

	status, body := s.Get("/admin/signup")
	if status != http.StatusInternalServerError {
		t.Fatalf("overview without events: %d\n%s", status, body)
	}
}

// TestDutchAdmin checks that a Dutch admin labels payment statuses in Dutch
// and shows amounts and dates in Dutch.
func TestDutchAdmin(t *testing.T) {
	options := sitetest.Options{
		Templates: "testdata/templates",
		Configure: func(config *webthings.Config) {
			config.AdminLanguage = "nl"
		},
	}

	s := sitetest.StartWith(t, options, registrations.Module("tickets", "Tickets"))

	settings := s.Submit("/admin/tickets/new", url.Values{"Subject": {"concert-2026"}, "Title": {"Concert"}})
	s.Submit(settings, url.Values{"Title": {"Concert"}, "Open": {"on"}, "Capacity": {"0"}, "AdultPrice": {"120"}})

	status, _, _ := s.Send("/_/tickets/register", url.Values{
		"subject": {"concert-2026"},
		"name":    {"Ada"},
		"email":   {"ada@example.com"},
		"adults":  {"1"},
		"back":    {"/nl/"},
	})
	if status != http.StatusOK {
		t.Fatalf("register: %d", status)
	}

	money := frontend.FormatMoney("nl", 12000, "EUR")
	dutchDate := regexp.MustCompile(`\d{1,2} (jan|feb|mrt|apr|mei|jun|jul|aug|sep|okt|nov|dec) \d{4} \d{2}:\d{2}`)

	subject := "/admin/tickets/concert-2026"
	s.Expect(subject, http.StatusOK, "Openstaand", "Prijs per volwassene", money)

	_, body := s.Get(subject)

	var registration string

	for _, link := range regexp.MustCompile(`href="(`+subject+`/[^"/.]+)"`).FindAllStringSubmatch(body, -1) {
		if !strings.HasSuffix(link[1], "/settings") {
			registration = link[1]
		}
	}

	if registration == "" {
		t.Fatalf("no registration in %s", body)
	}

	if !dutchDate.MatchString(body) {
		t.Fatalf("registration date is not Dutch:\n%s", body)
	}

	s.Expect(registration, http.StatusOK, money, "Geen betaling</option>", "Openstaand</option>", "Betaald</option>",
		"Geannuleerd</option>", `value="paid"`)

	_, body = s.Get(registration)
	if !dutchDate.MatchString(body) || strings.Contains(body, "120.00") {
		t.Fatalf("registration date or total is not Dutch:\n%s", body)
	}
}
