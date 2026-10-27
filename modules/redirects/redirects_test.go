package redirects_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/modules/redirects"
	"github.com/donseba/go-webthings/sitetest"
)

func TestRedirects(t *testing.T) {
	s := sitetest.Start(t, redirects.Module())
	ctx := context.Background()
	service := redirects.Of(s.App, "redirects")

	editor := s.Submit("/admin/redirects/new", url.Values{
		"From":   {"/old-page/"},
		"To":     {"/en/"},
		"Status": {"301"},
	})

	// Old URLs answer with the redirect, keeping the query; HEAD as well.
	expectRedirect(t, s, http.MethodGet, "/old-page?utm_source=mail", http.StatusMovedPermanently, "/en/?utm_source=mail")
	expectRedirect(t, s, http.MethodHead, "/old-page/", http.StatusMovedPermanently, "/en/")

	// Other methods and other paths reach the site as usual.
	if code := serve(s, http.MethodPost, "/old-page").Code; code == http.StatusMovedPermanently {
		t.Fatalf("POST redirected: %d", code)
	}

	if code := serve(s, http.MethodGet, "/admin/login").Code; code != http.StatusOK {
		t.Fatalf("admin: %d", code)
	}

	// The overview writes the counted hits and shows them.
	s.Expect("/admin/redirects", http.StatusOK, "/old-page", "/en/", "<td class=\"p-3 numeric\">2</td>")

	list, err := service.List(ctx)
	if err != nil || len(list) != 1 || list[0].Hits != 2 || list[0].LastHit == nil {
		t.Fatalf("hits: %+v %v", list, err)
	}

	// Loops and the site's own paths are refused.
	status, body := s.PostStatus("/admin/redirects/new", "/admin/redirects/new", url.Values{
		"From": {"/loop"}, "To": {"/loop/?again=1"}, "Status": {"301"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "This redirect would lead back to its own path.") {
		t.Fatalf("loop: %d\n%s", status, body)
	}

	status, body = s.PostStatus("/admin/redirects/new", "/admin/redirects/new", url.Values{
		"From": {"/admin/pages"}, "To": {"/en/"}, "Status": {"301"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "belongs to the website itself") {
		t.Fatalf("admin path: %d\n%s", status, body)
	}

	// Core serves theme files before any redirect could.
	status, body = s.PostStatus("/admin/redirects/new", "/admin/redirects/new", url.Values{
		"From": {"/theme/site.css"}, "To": {"/en/"}, "Status": {"301"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "belongs to the website itself") {
		t.Fatalf("theme path: %d\n%s", status, body)
	}

	// A redirect to an old path works but warns about the chain, on both ends.
	older := s.Submit("/admin/redirects/new", url.Values{
		"From": {"/older"}, "To": {"/old-page"}, "Status": {"302"},
	})
	s.Expect(older, http.StatusOK, "The new address redirects again, to /en/.")
	s.Expect(editor, http.StatusOK, "Other redirects lead to this path and now take two steps: /older.")
	expectRedirect(t, s, http.MethodGet, "/older", http.StatusFound, "/old-page")
	s.Expect(older, http.StatusOK, `<option value="302" selected`)

	// The test box follows the chain.
	s.Expect("/admin/redirects?test=https://old.example/older/", http.StatusOK,
		`<div id="test-result"`, "/older</a> → /old-page", "/old-page</a> → /en/",
	)
	s.Expect("/admin/redirects?test=/nothing", http.StatusOK, "No redirect for this path.")

	// The search narrows the list on the old path and the new address.
	s.Expect("/admin/redirects?q=OLDER", http.StatusOK, `href="/admin/redirects/`, ">/older</a>")
	s.ExpectMissing("/admin/redirects?q=OLDER", ">/old-page</a>")
	s.Expect("/admin/redirects?q=zzz", http.StatusOK, "No results for “zzz”")

	// Pages that are not found are listed as missed paths, except the site's own.
	s.Expect("/en/nothing-here", http.StatusNotFound)
	s.Expect("/en/nothing-here/", http.StatusNotFound)
	s.Expect("/media/nothing.png", http.StatusNotFound)
	s.Expect("/admin/redirects/missed", http.StatusOK,
		"/en/nothing-here", "/admin/redirects/new?from=%2Fen%2Fnothing-here",
	)
	s.ExpectMissing("/admin/redirects/missed", "nothing.png")
	s.ExpectMissing("/admin/redirects/missed?q=elsewhere", "/en/nothing-here")

	// A wrong CSV line refuses the whole import.
	status, body = s.PostStatus("/admin/redirects/import", "/admin/redirects/import", url.Values{
		"CSV": {"from,to,status\n/en/nothing-here,/en/,302\n/broken\n"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Line 3: Use two or three columns") ||
		!sitetest.HasToast(body, "error", "Nothing was imported. Correct these lines and try again.") {
		t.Fatalf("import errors: %d\n%s", status, body)
	}

	status, body = s.PostStatus("/admin/redirects/import", "/admin/redirects/import", url.Values{
		"CSV": {"from,to,status\n/en/nothing-here,/en/,302\n/blog/1,https://blog.example/1\n"},
	})
	if status != http.StatusOK || !strings.Contains(body, "Redirects imported: 2") {
		t.Fatalf("import: %d\n%s", status, body)
	}

	expectRedirect(t, s, http.MethodGet, "/en/nothing-here", http.StatusFound, "/en/")
	expectRedirect(t, s, http.MethodGet, "/blog/1/", http.StatusMovedPermanently, "https://blog.example/1")
	s.ExpectMissing("/admin/redirects/missed", "/en/nothing-here")

	// Clearing the missed paths asks first and says so.
	s.Expect("/en/gone", http.StatusNotFound)
	s.Expect("/admin/redirects/missed", http.StatusOK, `hx-confirm="Clear the list of missed paths?"`)

	status, body = s.PostStatus("/admin/redirects/missed", "/admin/redirects/missed/clear", url.Values{})
	if status != http.StatusOK || !strings.Contains(body, "The list was cleared.") || strings.Contains(body, "/en/gone") {
		t.Fatalf("clear missed paths: %d\n%s", status, body)
	}

	// Deleting a redirect asks first, moves it to the trash and lets the old
	// path through again.
	s.Expect(editor, http.StatusOK, `hx-confirm="Move this item to the trash?"`)
	s.Expect("/admin/redirects", http.StatusOK, `href="/admin/trash?section=redirects"`)

	status, body = s.PostStatus(editor, editor+"/delete", url.Values{})
	if status != http.StatusOK || !strings.Contains(body, "Moved to the trash.") {
		t.Fatalf("delete: %d\n%s", status, body)
	}

	if code := serve(s, http.MethodGet, "/old-page").Code; code != http.StatusNotFound {
		t.Fatalf("deleted redirect: %d", code)
	}

	if status, _ := s.PostStatus("/admin/redirects", editor+"/delete", url.Values{}); status != http.StatusNotFound {
		t.Fatalf("delete twice: %d", status)
	}

	s.Expect(editor, http.StatusNotFound)
	s.ExpectMissing("/admin/redirects", ">/old-page</a>")

	// The trash shows it with where it led; restoring it redirects again.
	id := editor[strings.LastIndex(editor, "/")+1:]

	s.Expect("/admin/trash?section=redirects", http.StatusOK, "/old-page → /en/", `value="redirects:`+id+`"`)

	status, body = s.PostStatus("/admin/trash?section=redirects", "/admin/trash?section=redirects", url.Values{
		"action": {"restore"},
		"item":   {"redirects:" + id},
	})
	if status != http.StatusOK || !strings.Contains(body, "1 item restored.") {
		t.Fatalf("restore: %d\n%s", status, body)
	}

	expectRedirect(t, s, http.MethodGet, "/old-page", http.StatusMovedPermanently, "/en/")
}

// TestTimezone checks that the admin shows times in the site's timezone,
// not the server's.
func TestTimezone(t *testing.T) {
	s := sitetest.Start(t, redirects.Module())
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

	zone, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		t.Fatal(err)
	}

	// The admin is English: "6 Oct 2026 11:49".
	shown := func() string {
		now := time.Now().In(zone)

		return frontend.FormatDate("en", now, "medium") + " " + now.Format("15:04")
	}

	before := shown()

	s.Submit("/admin/redirects/new", url.Values{"From": {"/old"}, "To": {"/en/"}, "Status": {"301"}})
	expectRedirect(t, s, http.MethodGet, "/old", http.StatusMovedPermanently, "/en/")
	s.Expect("/en/gone", http.StatusNotFound)

	after := shown()

	for _, screen := range []string{"/admin/redirects", "/admin/redirects/missed"} {
		_, body := s.Get(screen)
		if !strings.Contains(body, before) && !strings.Contains(body, after) {
			t.Fatalf("%s does not show %s:\n%s", screen, after, body)
		}
	}
}

// serve sends a request straight to the app, without following redirects.
func serve(s *sitetest.Site, method, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	s.App.ServeHTTP(recorder, httptest.NewRequest(method, target, nil))

	return recorder
}

func expectRedirect(t *testing.T, s *sitetest.Site, method, target string, status int, location string) {
	t.Helper()

	response := serve(s, method, target)
	if response.Code != status || response.Header().Get("Location") != location {
		t.Fatalf(
			"%s %s: want %d to %s, got %d to %s",
			method,
			target,
			status,
			location,
			response.Code,
			response.Header().Get("Location"),
		)
	}
}

// TestNamed checks the default key and title and how Named changes them.
func TestNamed(t *testing.T) {
	module := redirects.Module()
	if module.Key != "redirects" || module.Title != "Redirects" {
		t.Fatalf("default: %q %q", module.Key, module.Title)
	}

	module = redirects.Module(redirects.Named("old", "Old addresses"))
	if module.Key != "old" || module.Title != "Old addresses" {
		t.Fatalf("named: %q %q", module.Key, module.Title)
	}
}
