package testsuite

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/modules/banners"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

var (
	sidebarLink  = regexp.MustCompile(`class="nav-link" href="([^"]+)"`)
	sidebarGroup = regexp.MustCompile(`data-nav-group="([^"]+)"`)
)

// sidebar returns the links and group keys of the admin navigation of page.
func sidebar(t *testing.T, s *sitetest.Site, page string) (links, groups []string) {
	t.Helper()

	_, body := s.Get(page)

	start := strings.Index(body, `<aside id="admin-sidebar"`)
	end := strings.Index(body, `</aside>`)

	if start < 0 || end < start {
		t.Fatalf("%s has no sidebar", page)
	}

	for _, match := range sidebarLink.FindAllStringSubmatch(body[start:end], -1) {
		links = append(links, match[1])
	}

	for _, match := range sidebarGroup.FindAllStringSubmatch(body[start:end], -1) {
		groups = append(groups, match[1])
	}

	return links, groups
}

// TestAdminNavigationGroups checks that a module's group applies to all its
// sections, that modules without one are listed under Modules and that the
// site's settings join the admin's own sections under System, last.
func TestAdminNavigationGroups(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t,
		publishing.Module("news", "News").Group("Content"),
		events.Module("agenda", "Agenda"),
		banners.Module("notice", "Notices").Group("Website"),
		publishing.Module("blog", "Blog").Group("Content"),
	)

	links, groups := sidebar(t, s, "/admin/")

	want := []string{
		"/admin/", "/admin/pages", "/admin/media",
		"/admin/blog", "/admin/news",
		"/admin/agenda",
		"/admin/notice",
		"/admin/accounts", "/admin/groups", "/admin/site", "/admin/theme", "/admin/trash", "/admin/account",
	}
	if !slices.Equal(links, want) {
		t.Fatalf("links = %v\nwant    %v", links, want)
	}

	if want := []string{"Content", "Modules", "Website", "System"}; !slices.Equal(groups, want) {
		t.Fatalf("groups = %v, want %v", groups, want)
	}

	// The categories of News are hidden but in its group, which stays open
	// on their page.
	s.Expect("/admin/news-categories", http.StatusOK, `data-nav-group="Content" data-active`)
	s.Expect("/admin/site", http.StatusOK, `data-nav-group="System" data-active`)
}

// TestAdminDashboard checks the dashboard of an administrator and of an
// editor who may only read News.
func TestAdminDashboard(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t, publishing.Module("news", "News").Group("Content"))

	home, err := s.App.Pages.Home(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	page := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About us"}, "Slug": {"about"}, "Parent": {home}})
	post := s.Submit(
		"/admin/news/new?locale=en",
		url.Values{"Title": {"Opening day"}, "Slug": {"opening"}, "Date": {"2026-10-05"}},
	)

	tomorrow := time.Now().Add(26 * time.Hour).UTC().Format("2006-01-02T15:04")
	s.Post(post+"?locale=en", action(t, s, post+"?locale=en", schedulePattern), url.Values{"PublishAt": {tomorrow}})

	body := s.Dashboard()

	for _, part := range []string{
		`<a href="` + page + `?locale=en">About us</a>`,
		`<a href="` + post + `?locale=en">Opening day</a>`,
		"Pages · by admin@example.test",
		`href="/admin/pages/new"`,
		`href="/admin/news/new"`,
		"News · Goes online",
	} {
		if !strings.Contains(body, part) {
			t.Errorf("dashboard lacks %q", part)
		}
	}

	// The post was changed last, so it comes first.
	if strings.Index(body, ">Opening day</a>") > strings.Index(body, ">About us</a>") {
		t.Error("recent items are not newest first")
	}

	group := s.Submit("/admin/groups", url.Values{"Name": {"Readers"}})
	s.Post(group, group, url.Values{"name": {"Readers"}, "allow": {"news:read"}})

	account := s.Submit("/admin/accounts", url.Values{
		"Email":    {"reader@example.test"},
		"Password": {"reader long passphrase"},
		"Role":     {"editor"},
	})
	s.Post(account, account+"/groups", url.Values{"group": {group[strings.LastIndex(group, "/")+1:]}})

	reader := s.As("reader@example.test", "reader long passphrase")
	body = reader.Dashboard()

	if !strings.Contains(body, ">Opening day</a>") || !strings.Contains(body, "News · Goes online") {
		t.Error("the reader misses the news on the dashboard")
	}

	for _, part := range []string{">About us</a>", `href="/admin/pages/new"`, `href="/admin/news/new"`} {
		if strings.Contains(body, part) {
			t.Errorf("the reader's dashboard shows %q", part)
		}
	}
}
