package admin_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"

	"github.com/donseba/go-partial/exp/sse"
)

// stream is an open presence stream, read like a browser's EventSource.
type stream struct {
	t       *testing.T
	status  int
	patches chan sse.Patch
	cancel  context.CancelFunc
}

// openStream opens the presence stream of a tab on page.
func (b browser) openStream(tab, page string) *stream {
	b.t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	query := url.Values{"tab": {tab}, "page": {page}}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, b.server.URL+"/admin/presence?"+query.Encode(), nil)
	if err != nil {
		b.t.Fatal(err)
	}

	request.Header.Set("Accept", "text/event-stream")

	response, err := b.client.Do(request)
	if err != nil {
		b.t.Fatal(err)
	}

	s := &stream{t: b.t, status: response.StatusCode, cancel: cancel, patches: make(chan sse.Patch, 16)}

	b.t.Cleanup(func() {
		cancel()

		_ = response.Body.Close()
	})

	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()

		return s
	}

	reader := bufio.NewReader(response.Body)

	go func() {
		defer close(s.patches)

		event := ""

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}

			line = strings.TrimRight(line, "\n")

			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: ") && event == string(sse.EventPatch):
				var patch sse.Patch
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &patch); err != nil {
					return
				}

				s.patches <- patch
			case line == "":
				event = ""
			}
		}
	}()

	return s
}

// next waits for the stream's next patch of #admin-presence.
func (s *stream) next() string {
	s.t.Helper()

	select {
	case patch, ok := <-s.patches:
		if !ok {
			s.t.Fatal("the stream ended")
		}

		if patch.Target != "#admin-presence" {
			s.t.Fatalf("patch of %q", patch.Target)
		}

		return string(patch.HTML)
	case <-time.After(5 * time.Second):
		s.t.Fatal("no patch within 5 seconds")
	}

	return ""
}

// report sends a tab's report as go-partial's exp/sse client does for the
// header's stream (sse.ReadBeacon): the tab's id, the reason and the
// stream's parameters, the page and the tab.
func (b browser) report(tab, page string, reason sse.BeaconReason) int {
	b.t.Helper()

	status, _ := b.post("/admin/presence", beacon(tab, page, reason))

	return status
}

// beacon is the form of a report of the exp/sse client.
func beacon(tab, page string, reason sse.BeaconReason) url.Values {
	return url.Values{
		"clientId": {tab},
		"reason":   {string(reason)},
		"params":   {url.Values{"page": {page}, "tab": {tab}}.Encode()},
	}
}

// presenceAdmin is an admin with a news section that editors may read and a
// secret one they may not; the owner has no name, the editor has.
func presenceAdmin(t *testing.T) (owner, editor browser) {
	t.Helper()

	accounts := newAccounts(t)

	if _, err := accounts.CreateUser(
		context.Background(), "eddie@example.test", "Eddie Editor", "test-only long passphrase", auth.Editor,
	); err != nil {
		t.Fatal(err)
	}

	authorize := func(_ context.Context, user auth.User, permission admin.Permission) bool {
		return user.IsAdmin() || permission.Section != "secret"
	}

	options := admin.Options{Language: "en", Authorize: authorize}
	owner = startWith(t, accounts, options, plainSection("news", "News", ""), plainSection("secret", "Secret", ""))
	owner.signIn()

	// A second browser on the same server.
	editor = owner
	editor.client = newClient(owner.server)
	editor.signInAs("eddie@example.test", "test-only long passphrase")

	return owner, editor
}

// TestPresence follows two users through the stream and the reports: who
// joins, the header rendered with the page, language tabs being one page,
// leaving, and that the owner's e-mail address never shows.
func TestPresence(t *testing.T) {
	t.Parallel()

	owner, editor := presenceAdmin(t)

	watch := editor.openStream("eddie-tab-1", "/admin/news/1?locale=nl")
	if watch.status != http.StatusOK {
		t.Fatalf("stream: %d", watch.status)
	}

	// At first nobody else is there.
	first := watch.next()
	if !strings.Contains(first, `<div id="admin-presence" class="presence" hidden>`) {
		t.Fatalf("first patch: %s", first)
	}

	// The owner opens the same item in another language.
	status := owner.report("owner-tab-1", "/admin/news/1?locale=en", sse.BeaconBeat)
	if status != http.StatusNoContent {
		t.Fatalf("report: %d", status)
	}

	joined := watch.next()
	if !strings.Contains(joined, `title="Another user is also on this page"`) || strings.Contains(joined, " hidden>") {
		t.Fatalf("joined patch: %s", joined)
	}

	if strings.Contains(joined, "owner@example.test") {
		t.Fatal("the stream shows an e-mail address")
	}

	// A second tab of the owner shows the owner once.
	owner.report("owner-tab-2", "/admin/news/1", sse.BeaconBeat)

	if again := watch.next(); strings.Count(again, `class="presence-chip presence-hue-`) != 1 {
		t.Fatalf("two tabs show twice: %s", again)
	}

	// The owner's page shows Eddie in the header, with his initials.
	_, page := owner.get("/admin/news")
	if strings.Contains(page, "Eddie") {
		t.Error("the list page shows who is on an item")
	}

	owner.report("owner-tab-3", "/admin/news", sse.BeaconBeat)

	eddie := editor.openStream("eddie-tab-2", "/admin/news")
	defer eddie.cancel()

	eddie.next()

	_, page = owner.get("/admin/news?q=x")
	if !strings.Contains(page, `title="Eddie Editor is also on this page"`) || !strings.Contains(page, ">EE</span>") {
		t.Fatalf("the header lacks Eddie:\n%s", page)
	}

	// The header names the page's stream, to which go-partial's client adds
	// the tab's id, and the address of its reports.
	if !strings.Contains(page, `data-partial-stream="/admin/presence?page=%2Fadmin%2Fnews" data-partial-stream-client-id="tab"`+
		` data-partial-stream-hidden="30s" data-partial-stream-beat="15s" data-partial-stream-beacon="/admin/presence"`) {
		t.Fatalf("the header lacks the page's stream:\n%s", page)
	}

	// Leaving takes the owner's tabs away: one tab closes, the other
	// navigates to a page without presence, which a beat would report too.
	owner.report("owner-tab-1", "/admin/news/1?locale=en", sse.BeaconPagehide)
	watch.next()
	owner.report("owner-tab-2", "/admin/news/1", sse.BeaconRemoved)

	if left := watch.next(); !strings.Contains(left, " hidden>") {
		t.Fatalf("the owner did not leave: %s", left)
	}

	// The dashboard has no presence: its stream is refused for good.
	if s := owner.openStream("owner-tab-4", "/admin/"); s.status != http.StatusNoContent {
		t.Fatalf("dashboard stream: %d", s.status)
	}

	_, dashboard := owner.get("/admin/")
	if !strings.Contains(dashboard, `id="admin-presence"`) || strings.Contains(dashboard, "data-partial-stream") {
		t.Error("the dashboard reports a page")
	}
}

// TestPresenceRefusals checks the stream's and reports' guards: pages the
// user may not see, signed-out and cross-site requests, bad tab IDs and too
// many streams.
func TestPresenceRefusals(t *testing.T) {
	t.Parallel()

	owner, editor := presenceAdmin(t)

	// Eddie may not see the secret section: no stream, and his report does
	// not put him there.
	if s := editor.openStream("eddie-tab-1", "/admin/secret/1"); s.status != http.StatusNoContent {
		t.Fatalf("stream of a page the user may not see: %d", s.status)
	}

	editor.report("eddie-tab-1", "/admin/secret/1", sse.BeaconBeat)

	_, page := owner.get("/admin/secret")
	if strings.Contains(page, "Eddie") {
		t.Fatal("a user shows on a page he may not see")
	}

	if s := owner.openStream("bad tab!", "/admin/news"); s.status != http.StatusBadRequest {
		t.Fatalf("invalid tab: %d", s.status)
	}

	visitor := owner
	visitor.client = newClient(owner.server)

	if s := visitor.openStream("visitor-tab", "/admin/news"); s.status != http.StatusUnauthorized {
		t.Fatalf("signed-out stream: %d", s.status)
	}

	if status := visitor.report("visitor-tab", "/admin/news", sse.BeaconBeat); status != http.StatusUnauthorized {
		t.Fatalf("signed-out report: %d", status)
	}

	status, _ := owner.postFrom(
		"https://evil.example",
		"cross-site",
		"/admin/presence",
		beacon("owner-tab-x", "/admin/news", sse.BeaconBeat),
	)
	if status != http.StatusForbidden {
		t.Fatalf("cross-site report: %d", status)
	}

	// Eight streams per user; the ninth is refused until one closes.
	var open []*stream

	for i := range 8 {
		s := owner.openStream("owner-tab-"+string(rune('a'+i))+"xxxxxxx", "/admin/news")
		if s.status != http.StatusOK {
			t.Fatalf("stream %d: %d", i, s.status)
		}

		s.next()
		open = append(open, s)
	}

	if s := owner.openStream("owner-tab-overflow", "/admin/news"); s.status != http.StatusTooManyRequests {
		t.Fatalf("ninth stream: %d", s.status)
	}

	open[0].cancel()

	// The server notices the closed stream shortly after.
	deadline := time.Now().Add(5 * time.Second)

	for {
		s := owner.openStream("owner-tab-retry", "/admin/news")
		if s.status == http.StatusOK {
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("a closed stream kept its place: %d", s.status)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

// TestPresenceBeacon checks the reasons of the exp/sse client's reports: a
// tab whose stream closed while it is hidden stays on the page, one whose
// header was removed by a navigation leaves it, and a report that is not
// one of the client's is refused.
func TestPresenceBeacon(t *testing.T) {
	t.Parallel()

	owner, editor := presenceAdmin(t)

	watch := editor.openStream("eddie-tab-1", "/admin/news/1")
	if watch.status != http.StatusOK {
		t.Fatalf("stream: %d", watch.status)
	}

	defer watch.cancel()

	watch.next()

	if status := owner.report("owner-tab-1", "/admin/news/1", sse.BeaconBeat); status != http.StatusNoContent {
		t.Fatalf("beat: %d", status)
	}

	if joined := watch.next(); strings.Contains(joined, " hidden>") {
		t.Fatalf("the owner did not join: %s", joined)
	}

	// Hidden: the tab is still there, so Eddie's header still shows it.
	if status := owner.report("owner-tab-1", "/admin/news/1", sse.BeaconHidden); status != http.StatusNoContent {
		t.Fatalf("hidden: %d", status)
	}

	if _, page := editor.get("/admin/news/1"); !strings.Contains(page, `title="Another user is also on this page"`) {
		t.Fatalf("a hidden tab left the page:\n%s", page)
	}

	// Removed: the tab navigated to a page without presence.
	if status := owner.report("owner-tab-1", "/admin/news/1", sse.BeaconRemoved); status != http.StatusNoContent {
		t.Fatalf("removed: %d", status)
	}

	if left := watch.next(); !strings.Contains(left, " hidden>") {
		t.Fatalf("a removed tab is still shown: %s", left)
	}

	invalid := map[string]url.Values{
		"unknown reason": beacon("owner-tab-1", "/admin/news/1", "gone"),
		"no client id":   beacon("", "/admin/news/1", sse.BeaconBeat),
		"bad tab":        beacon("bad tab!", "/admin/news/1", sse.BeaconBeat),
	}

	for name, values := range invalid {
		if status, _ := owner.post("/admin/presence", values); status != http.StatusBadRequest {
			t.Errorf("%s: %d", name, status)
		}
	}
}

// TestPresenceSignOut checks that signing out with the tab, as admin.js
// does, takes that tab away at once.
func TestPresenceSignOut(t *testing.T) {
	t.Parallel()

	owner, editor := presenceAdmin(t)

	watch := editor.openStream("eddie-tab-1", "/admin/news/1")
	if watch.status != http.StatusOK {
		t.Fatalf("stream: %d", watch.status)
	}

	defer watch.cancel()

	watch.next()

	owner.report("owner-tab-1", "/admin/news/1", sse.BeaconBeat)

	if joined := watch.next(); strings.Contains(joined, " hidden>") {
		t.Fatalf("the owner did not join: %s", joined)
	}

	if status, _ := owner.post("/admin/logout", url.Values{"tab": {"owner-tab-1"}}); status >= http.StatusBadRequest {
		t.Fatalf("sign out: %d", status)
	}

	if left := watch.next(); !strings.Contains(left, " hidden>") {
		t.Fatalf("the owner is still shown after signing out: %s", left)
	}
}

// TestPresenceClient checks that the pages' import map names go-partial's
// SSE client, which applies the presence stream's patches, as a versioned
// asset served without a session.
func TestPresenceClient(t *testing.T) {
	t.Parallel()

	owner, _ := presenceAdmin(t)

	_, page := owner.get("/admin/")

	client := regexp.MustCompile(`"partial-sse": "(/admin/assets/partial-sse\.js\?v=[0-9a-f]+)"`).FindStringSubmatch(page)
	if client == nil {
		t.Fatal("the import map does not name the SSE client")
	}

	visitor := owner
	visitor.client = newClient(owner.server)

	status, script := visitor.get(client[1])
	if status != http.StatusOK || !strings.Contains(script, "goPartialSSE") {
		t.Fatalf("client script: %d %.80s", status, script)
	}
}
