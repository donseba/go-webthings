package polls_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/modules/polls"
	"github.com/donseba/go-webthings/sitetest"
)

func TestPolls(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, polls.Module("survey", "Polls"))
	ctx := context.Background()

	editor := s.Submit("/admin/survey/new?locale=en", url.Values{
		"Question": {"Favourite colour?"},
		"Slug":     {"colour"},
		"Options":  {"Red\nGreen\n\nBlue\n"},
	})

	s.Submit(editor+"?locale=nl", url.Values{
		"Question": {"Favoriete kleur?"},
		"Slug":     {"kleur"},
		"Options":  {"Rood\nGroen\nBlauw"},
	})

	// Drafts are not shown.
	s.ExpectMissing("/en/", "Favourite colour?")

	s.Publish(editor + "?locale=en")
	s.Expect("/en/", http.StatusOK,
		"<h2>Favourite colour?</h2>",
		`name="option"`,
		`<fieldset`,
		`<legend`,
		`>Green</label>`,
		`name="website"`,
		`action="/_/survey/vote"`,
		`<p class="latest">Favourite colour?</p>`,
		`<p class="action">/_/survey/vote</p>`,
	)
	s.ExpectMissing("/en/", "total")
	s.Expect("/nl/", http.StatusOK, `<p class="latest">Favoriete kleur?</p>`)

	service := polls.Of(s.App, "survey")

	poll, err := service.Find(ctx, nil, "en", "colour")
	if err != nil || len(poll.Options) != 3 || poll.Options[2].Label != "Blue" {
		t.Fatalf("find: %+v %v", poll, err)
	}

	red, green, blue := poll.Options[0].ID, poll.Options[1].ID, poll.Options[2].ID

	// The vote form returns to the poll's element, survey-<ID>.
	if poll.Anchor != "survey-"+poll.ID {
		t.Fatalf("anchor: %q", poll.Anchor)
	}

	s.Expect("/en/", http.StatusOK,
		`<section id="survey-`+poll.ID+`">`,
		`name="back" value="/en/#survey-`+poll.ID+`"`,
		`id="survey-`+poll.ID+`-poll"`,
		`value="`+poll.ID+`"`,
	)

	expectVote(t, s, poll.ID, red, "voted")
	s.Expect(landing(poll.ID, "voted"), http.StatusOK, "result=voted", "you voted", "Red: 100% (1) chosen", "total 1")
	s.ExpectMissing("/en/", `name="option"`)

	// A result belongs to the poll that was voted on, and only known results
	// are shown.
	s.Expect("/en/?survey=voted", http.StatusOK, "<p>result=</p>")
	s.Expect("/en/?survey=voted&survey_poll="+db.NewID(), http.StatusOK, "<p>result=</p>")
	s.Expect("/en/?survey=made-up&survey_poll="+poll.ID, http.StatusOK, "<p>result=</p>")

	// One vote per visitor.
	expectVote(t, s, poll.ID, green, "already")
	s.Expect("/en/", http.StatusOK, "Red: 100% (1) chosen", "total 1")

	// The spam trap pretends to accept the vote but counts nothing.
	_, _, landed := s.Send("/_/survey/vote", url.Values{
		"poll": {poll.ID}, "option": {green}, "website": {"https://spam.example"}, "back": {"/en/"},
	})
	if landed != landing(poll.ID, "voted") {
		t.Fatalf("spam: landed on %s", landed)
	}

	s.Expect("/en/", http.StatusOK, "total 1")

	if status := s.CrossSite("/_/survey/vote", url.Values{"poll": {poll.ID}, "option": {red}}); status != http.StatusForbidden {
		t.Fatalf("cross-site vote: %d", status)
	}

	// A second browser on the same address first meets the address's vote,
	// then votes with a cookie of its own.
	other := s.As("admin@example.test", "test-only long passphrase")
	other.Expect("/en/", http.StatusOK, `name="option"`)
	expectVote(t, other, poll.ID, green, "already")
	expectVote(t, other, poll.ID, green, "voted")
	expectVote(t, other, poll.ID, green, "already")
	other.Expect("/en/", http.StatusOK, "Red: 50% (1)", "Green: 50% (1) chosen", "total 2")

	// Scripts without cookies get one vote per address.
	if location := bot(t, s, poll.ID, blue); location != landing(poll.ID, "voted") {
		t.Fatalf("bot vote: %s", location)
	}

	if location := bot(t, s, poll.ID, blue); location != landing(poll.ID, "already") {
		t.Fatalf("second bot vote: %s", location)
	}

	expectVote(t, s, poll.ID, "not-an-option", "invalid")
	expectVote(t, s, "not-a-poll", red, "closed")

	// A vote without an option, or with one the poll does not have, shows
	// again with the message at the options (frontend.KeepForm).
	newcomer := s.Visitor()

	for option, message := range map[string]string{"": "This field is required.", "not-an-option": "is not allowed."} {
		_, body, landed := newcomer.Send("/_/survey/vote", url.Values{"poll": {poll.ID}, "option": {option}, "back": {"/en/"}})
		if landed != landing(poll.ID, "invalid") || !strings.Contains(body, message) || !strings.Contains(body, `type="radio"`) {
			t.Fatalf("vote %q: %s\n%s", option, landed, body)
		}
	}

	s.Expect("/en/", http.StatusOK, "Red: 33% (1)", "Blue: 33% (1)", "total 3")

	// The mounted page lists all polls with their results.
	s.Mount("survey", "Polls", "polls")
	s.Expect("/en/polls", http.StatusOK, "<h1>Polls</h1>", "<h2>Favourite colour?</h2>", "Green: 1")

	// The title is the published page's, not a newer draft's.
	s.DraftPage("Polls", "Polls (draft)", "polls")
	s.Expect("/en/polls", http.StatusOK, "<h1>Polls</h1>")
	s.ExpectMissing("/en/polls", "Polls (draft)")

	// Options with votes cannot be removed.
	status, body := s.PostStatus(editor+"?locale=en", editor+"?locale=en", url.Values{
		"Question": {"Favourite colour?"},
		"Slug":     {"colour"},
		"Options":  {"Red\nGreen"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Options with votes cannot be removed.") {
		t.Fatalf("remove voted option: %d\n%s", status, body)
	}

	// Closed polls show their results and take no more votes.
	s.Submit(editor+"?locale=en", url.Values{
		"Question": {"Favourite colour?"},
		"Slug":     {"colour"},
		"Options":  {"Red\nGreen\nBlue\nPurple"},
		"ClosesAt": {"2020-01-01T12:00"},
	})

	other.Expect("/en/", http.StatusOK, "poll closed", "Purple: 0% (0)", "total 3")
	expectVote(t, fresh(s), poll.ID, red, "closed")

	// A new option shows its English label in the Dutch editor until it is translated.
	s.Expect(editor+"?locale=nl", http.StatusOK, "Blauw\nPurple")

	// The overview counts the votes and marks closed polls; signed-in editors
	// get a link to it.
	s.Expect("/admin/survey?locale=en", http.StatusOK, "Favourite colour?</a>", "· 3 votes · Closed</span>")
	s.Expect("/en/polls", http.StatusOK, `id="edit" href="/admin/survey?locale=en"`)

	// The admin shows the results and resets them after asking.
	s.Expect("/admin/survey/results", http.StatusOK, "Favourite colour?", "Closed", "Green", "Reset votes",
		`action="`+editor+`/reset?locale=en"`, `hx-confirm="Reset the votes of this poll? This cannot be undone."`)

	status, body = s.PostStatus("/admin/survey/results", editor+"/reset?locale=en", url.Values{})
	if status != http.StatusOK || !strings.Contains(body, "The votes were reset.") {
		t.Fatalf("reset: %d\n%s", status, body)
	}

	s.ExpectMissing("/admin/survey/results", "Reset votes")

	poll, err = service.Find(ctx, nil, "en", poll.ID)
	if err != nil || poll.Total != 0 || len(poll.Options) != 4 {
		t.Fatalf("after reset: %+v %v", poll, err)
	}

	// Too many votes in a minute return to the page with "throttled".
	for range 25 {
		_, _, landed := s.Send("/_/survey/vote", url.Values{"poll": {poll.ID}, "option": {red}, "back": {"/en/"}})
		if landed == landing(poll.ID, "throttled") {
			return
		}
	}

	t.Fatal("votes were not throttled")
}

func TestOverviewInDutch(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{
		Templates: "testdata/templates",
		Configure: func(config *webthings.Config) {
			config.AdminLanguage = "nl"
		},
	}, polls.Module("survey", "Polls"))

	editor := s.Submit("/admin/survey/new?locale=en", url.Values{
		"Question": {"Favourite colour?"},
		"Options":  {"Red\nGreen"},
	})
	s.Publish(editor + "?locale=en")

	// A poll without votes has no note.
	s.ExpectMissing("/admin/survey?locale=en", `<span class="muted">`)

	poll, err := polls.Of(s.App, "survey").Find(context.Background(), nil, "en", editor[strings.LastIndex(editor, "/")+1:])
	if err != nil {
		t.Fatal(err)
	}

	expectVote(t, s, poll.ID, poll.Options[0].ID, "voted")
	s.Expect("/admin/survey?locale=en", http.StatusOK, "· 1 stem</span>")

	bot(t, s, poll.ID, poll.Options[1].ID)
	s.Expect("/admin/survey?locale=en", http.StatusOK, "· 2 stemmen</span>")
}

// TestResultBelongsToPoll shows the result of a vote on the poll that was
// voted on, not on the other polls of the page.
func TestResultBelongsToPoll(t *testing.T) {
	s := sitetest.Start(t, polls.Module("survey", "Polls"))
	ctx := context.Background()
	service := polls.Of(s.App, "survey")

	for _, question := range []string{"Tea or coffee?", "Cats or dogs?"} {
		item, err := service.Store().Save(ctx, content.Draft[polls.Fields, polls.Text]{
			Fields: polls.Fields{Options: polls.Lines{"a", "b"}},
			Translations: []content.Translation[polls.Text]{{
				Locale: "en",
				Text:   polls.Text{Question: question, Options: polls.Lines{"A", "B"}},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := service.Store().Publish(ctx, item.ID, "", "en"); err != nil {
			t.Fatal(err)
		}
	}

	list, err := service.Latest(ctx, nil, "en", 0)
	if err != nil || len(list) != 2 {
		t.Fatalf("polls: %+v %v", list, err)
	}

	voted := list[1].ID
	request := httptest.NewRequest(http.MethodGet, landing(voted, "voted"), nil)

	list, err = service.Latest(ctx, request, "en", -1)
	if err != nil || len(list) != 2 {
		t.Fatalf("polls after vote: %+v %v", list, err)
	}

	for _, poll := range list {
		want := ""
		if poll.ID == voted {
			want = "voted"
		}

		if poll.Result != want {
			t.Fatalf("%s: result %q, want %q", poll.Question, poll.Result, want)
		}
	}
}

// expectVote votes as the browser of s and checks the result it returns with.
func expectVote(t *testing.T, s *sitetest.Site, pollID, optionID, result string) {
	t.Helper()

	status, body, landed := s.Send("/_/survey/vote", url.Values{"poll": {pollID}, "option": {optionID}, "back": {"/en/"}})
	if status != http.StatusOK || landed != landing(pollID, result) {
		t.Fatalf("vote %s: want %s, got %d %s\n%s", optionID, result, status, landed, body)
	}
}

// landing is where a vote on pollID returns with result: the result belongs
// to that poll, when pollID can be one.
func landing(pollID, result string) string {
	query := url.Values{"survey": {result}}

	if len(pollID) == 36 {
		query.Set("survey_poll", pollID)
	}

	return "/en/?" + query.Encode()
}

// bot votes from another address without keeping cookies and returns where
// it is sent.
func bot(t *testing.T, s *sitetest.Site, pollID, optionID string) string {
	t.Helper()

	values := url.Values{"poll": {pollID}, "option": {optionID}, "back": {"/en/"}}
	request := httptest.NewRequest(http.MethodPost, "/_/survey/vote", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "192.0.2.7:4321"

	response := httptest.NewRecorder()
	s.App.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("bot vote: %d\n%s", response.Code, response.Body)
	}

	return response.Header().Get("Location")
}

// fresh is another browser, without the cookie of earlier votes.
func fresh(s *sitetest.Site) *sitetest.Site {
	return s.As("admin@example.test", "test-only long passphrase")
}

// TestClosingTimeInSiteTimezone checks that editors enter and read the
// closing time in the site's timezone, and that it is stored in UTC.
func TestClosingTimeInSiteTimezone(t *testing.T) {
	s := sitetest.Start(t, polls.Module("survey", "Polls"))
	ctx := context.Background()

	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	settings.Timezone = "Europe/Amsterdam"
	if err := s.App.Site.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}

	editor := s.Submit("/admin/survey/new?locale=en", url.Values{
		"Question": {" Favourite colour? "},
		"Options":  {"Red\nGreen"},
		"ClosesAt": {"2030-07-01T12:00"},
	})

	s.Expect(editor+"?locale=en", http.StatusOK, `value="2030-07-01T12:00"`, `value="Favourite colour?"`)

	item, err := polls.Of(s.App, "survey").Store().Load(ctx, editor[strings.LastIndex(editor, "/")+1:], "")
	if err != nil {
		t.Fatal(err)
	}

	want := time.Date(2030, 7, 1, 10, 0, 0, 0, time.UTC)
	if closes := item.Fields.ClosesAt; closes == nil || !closes.Equal(want) || closes.Location() != time.UTC {
		t.Fatalf("closes at %v, want %v", closes, want)
	}

	// An empty field opens the poll again.
	s.Submit(editor+"?locale=en", url.Values{"Question": {"Favourite colour?"}, "Options": {"Red\nGreen"}, "ClosesAt": {""}})

	item, err = polls.Of(s.App, "survey").Store().Load(ctx, editor[strings.LastIndex(editor, "/")+1:], "")
	if err != nil || item.Fields.ClosesAt != nil {
		t.Fatalf("reopened: %v %v", item.Fields.ClosesAt, err)
	}
}
