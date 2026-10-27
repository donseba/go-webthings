package testsuite

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

// signIn adds an editor account in groupName with the rights allow, such as
// "news:read", and returns a browser signed in as that editor.
func signIn(t *testing.T, admin *sitetest.Site, email, groupName string, allow ...string) *sitetest.Site {
	t.Helper()

	group := admin.Submit("/admin/groups", url.Values{"Name": {groupName}})
	admin.Post(group, group, url.Values{"name": {groupName}, "allow": allow})

	admin.Submit("/admin/accounts", url.Values{
		"Email":    {email},
		"Password": {"long test passphrase"},
		"Role":     {"editor"},
	})

	users, err := admin.App.Auth.Users(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, user := range users {
		if user.Email == email {
			account := "/admin/accounts/" + user.ID
			admin.Post(account, account+"/groups", url.Values{"group": {group[strings.LastIndex(group, "/")+1:]}})
		}
	}

	return admin.As(email, "long test passphrase")
}

// lastID is the last part of an admin path, the item's ID.
func lastID(path string) string {
	return path[strings.LastIndex(path, "/")+1:]
}

// preview fetches path and reports the page and whether it is a preview,
// checking that a preview is never stored or indexed.
func preview(t *testing.T, s *sitetest.Site, path string) (int, string, bool) {
	t.Helper()

	response, body := s.Response(path)
	previewed := strings.Contains(body, `<p id="preview">`)

	if previewed {
		header := response.Header
		if header.Get("Cache-Control") != "private, no-store" || header.Get("ETag") != "" ||
			!strings.Contains(header.Get("X-Robots-Tag"), "noindex") {
			t.Fatalf("preview of %s may be stored or indexed: %v", path, header)
		}
	}

	return response.StatusCode, body, previewed
}

func TestPreview(t *testing.T) {
	t.Parallel()

	admin := sitetest.StartWith(
		t,
		sitetest.Options{StartingPoints: []string{"news", "agenda"}},
		publishing.Module("news", "News"),
		events.Module("agenda", "Agenda"),
	)
	ctx := context.Background()

	news := admin.Mount("news", "News", "news")
	admin.Mount("agenda", "Agenda", "agenda")

	// The news page, which shows a module, gets a new title in a draft.
	admin.DraftPage("News", "News draft", "news")

	newsDraft, err := admin.App.Pages.Latest(ctx, lastID(news))
	if err != nil {
		t.Fatal(err)
	}

	home, err := admin.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// A published page with a newer draft, and a page never published.
	about := admin.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About"}, "Slug": {"about"}, "Parent": {home}})
	admin.Publish(about + "?locale=en")
	admin.Submit(about+"?locale=en", url.Values{"Title": {"About draft"}, "Slug": {"about"}, "Parent": {home}})

	team := admin.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"Team draft"}, "Slug": {"team"}, "Parent": {home}})

	aboutDraft, err := admin.App.Pages.Latest(ctx, lastID(about))
	if err != nil {
		t.Fatal(err)
	}

	teamDraft, err := admin.App.Pages.Latest(ctx, lastID(team))
	if err != nil {
		t.Fatal(err)
	}

	// The same for a post, and a draft of another post.
	post := admin.Submit("/admin/news/new?locale=en", url.Values{"Title": {"Hello"}, "Slug": {"hello"}, "Date": {"2026-10-01"}})
	admin.Publish(post + "?locale=en")
	admin.Submit(post+"?locale=en", url.Values{"Title": {"Hello draft"}, "Slug": {"hello"}, "Date": {"2026-10-01"}})

	other := admin.Submit(
		"/admin/news/new?locale=en",
		url.Values{"Title": {"Other draft"}, "Slug": {"other"}, "Date": {"2026-10-02"}},
	)

	posts := publishing.Of(admin.App, "news").Store()

	postDraft, err := posts.Load(ctx, lastID(post), "")
	if err != nil {
		t.Fatal(err)
	}

	otherDraft, err := posts.Load(ctx, lastID(other), "")
	if err != nil {
		t.Fatal(err)
	}

	event := admin.Submit("/admin/agenda/new?locale=en", url.Values{
		"Title":    {"Tasting draft"},
		"Slug":     {"tasting"},
		"Start":    {time.Now().AddDate(0, 1, 0).Format(time.DateOnly) + "T14:30"},
		"Timezone": {"UTC"},
	})

	eventDraft, err := events.Of(admin.App, "agenda").Store().Load(ctx, lastID(event), "")
	if err != nil {
		t.Fatal(err)
	}

	previews := map[string]string{
		"/en/about?revision=" + aboutDraft.Revision.ID:          "About draft",
		"/en/team?revision=" + teamDraft.Revision.ID:            "Team draft",
		"/en/news/hello?revision=" + postDraft.Revision.ID:      "Hello draft",
		"/en/news/other?revision=" + otherDraft.Revision.ID:     "Other draft",
		"/en/agenda/tasting?revision=" + eventDraft.Revision.ID: "Tasting draft",
		"/en/news?revision=" + newsDraft.Revision.ID:            "News draft",
	}

	// Administrators see the revision, marked as a preview.
	for path, title := range previews {
		status, body, previewed := preview(t, admin, path)
		if status != http.StatusOK || !previewed || !strings.Contains(body, title) || !strings.Contains(body, "(not published)") {
			t.Fatalf("admin on %s: %d, preview %v\n%s", path, status, previewed, body)
		}
	}

	// Visitors, and editors who may not read the section, get the
	// published page, or none, as without the parameter.
	visitor := admin.Visitor()
	outsider := signIn(t, admin, "outsider@example.test", "Media", "media:read")

	for _, browser := range []*sitetest.Site{visitor, outsider} {
		for path, title := range previews {
			status, body, previewed := preview(t, browser, path)
			if previewed || strings.Contains(body, title) {
				t.Fatalf("no preview expected on %s: %d\n%s", path, status, body)
			}
		}
	}

	if status, _, _ := preview(t, visitor, "/en/team?revision="+teamDraft.Revision.ID); status != http.StatusNotFound {
		t.Fatalf("visitor on a page never published: %d", status)
	}

	// Reading a section is enough to preview its content.
	reader := signIn(t, admin, "reader@example.test", "Readers", "agenda:read")

	if _, body, previewed := preview(t, reader, "/en/agenda/tasting?revision="+eventDraft.Revision.ID); !previewed ||
		!strings.Contains(body, "Tasting draft") {
		t.Fatalf("reader of the agenda: %v\n%s", previewed, body)
	}

	// A revision of another item never shows at an item's address.
	for _, path := range []string{
		"/en/news/hello?revision=" + otherDraft.Revision.ID,
		"/en/about?revision=" + teamDraft.Revision.ID,
	} {
		_, body, previewed := preview(t, admin, path)
		if previewed || strings.Contains(body, "draft") {
			t.Fatalf("revision of another item on %s:\n%s", path, body)
		}
	}

	// The editor's preview links lead to the same addresses.
	for link, title := range map[string]string{
		post + "/revisions/" + postDraft.Revision.ID + "/preview?locale=en":   "Hello draft",
		team + "/revisions/" + teamDraft.Revision.ID + "/preview?locale=en":   "Team draft",
		about + "/revisions/" + aboutDraft.Revision.ID + "/preview?locale=en": "About draft",
	} {
		response, body := admin.Response(link)
		if !strings.HasPrefix(response.Request.URL.Path, "/en/") || !strings.Contains(body, title) {
			t.Fatalf("preview link %s ended on %s\n%s", link, response.Request.URL, body)
		}
	}

	// A page below a page that is not published has no address yet: its
	// preview link returns to the editor with a notice.
	member := admin.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"Member"},
		"Slug":   {"member"},
		"Parent": {lastID(team)},
	})

	memberDraft, err := admin.App.Pages.Latest(ctx, lastID(member))
	if err != nil {
		t.Fatal(err)
	}

	response, body := admin.Response(member + "/revisions/" + memberDraft.Revision.ID + "/preview?locale=en")
	if response.Request.URL.Path != member || !strings.Contains(body, "no address on the website yet") {
		t.Fatalf("preview without address ended on %s\n%s", response.Request.URL, body)
	}

	// The edit screen and its history link to previews.
	admin.Expect(post+"?locale=en", http.StatusOK, "/revisions/"+postDraft.Revision.ID+"/preview")
}
