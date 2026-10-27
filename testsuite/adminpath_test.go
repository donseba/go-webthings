package testsuite

import (
	"bufio"
	"context"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/modules/banners"
	"github.com/donseba/go-webthings/modules/comments"
	"github.com/donseba/go-webthings/modules/directory"
	"github.com/donseba/go-webthings/modules/documents"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/forms"
	"github.com/donseba/go-webthings/modules/gallery"
	"github.com/donseba/go-webthings/modules/lists"
	"github.com/donseba/go-webthings/modules/newsletter"
	"github.com/donseba/go-webthings/modules/people"
	"github.com/donseba/go-webthings/modules/polls"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/modules/redirects"
	"github.com/donseba/go-webthings/modules/registrations"
	"github.com/donseba/go-webthings/modules/stats"
	"github.com/donseba/go-webthings/modules/submissions"
	"github.com/donseba/go-webthings/modules/testimonials"
	"github.com/donseba/go-webthings/sitetest"
)

// defaultAdmin is an address of the admin at its default path: /admin
// followed by a slash, a quote, a query or the end, but not /admin.css or
// /admin-page (the admin's files and classes keep their names).
var defaultAdmin = regexp.MustCompile(`/admin(?:[/"'?#&<\s]|$)`)

// adminLink is an address in an href, src or action attribute.
var adminLink = regexp.MustCompile(`(?:href|src|action)="([^"]+)"`)

// allModules are the bundled modules, each with its admin.
func allModules() []webthings.Module {
	return []webthings.Module{
		publishing.Module("news", "News"),
		submissions.Module("proposals", "Proposals", "news"),
		comments.Module("guestbook", "Guestbook"),
		forms.Module("contact", "Contact"),
		gallery.Module("photos", "Photos"),
		polls.Module("poll", "Poll"),
		directory.Module("shops", "Shops"),
		redirects.Module(),
		newsletter.Module("letter", "Letter"),
		stats.Module(),
		registrations.Module("signup", "Sign-up"),
		lists.Module("faq", "FAQ"),
		testimonials.Module("quotes", "Quotes"),
		banners.Module("notice", "Notice"),
		people.Module("team", "Team"),
		documents.Module("files", "Files"),
		events.Module("agenda", "Agenda"),
	}
}

// TestAdminPath runs the whole admin, with every module, at /toolbox: signing
// in, editing and saving, the trash, the presence stream, the edit link on
// the site and every screen the admin links to. No response names /admin,
// in its body or in a redirect, and /admin is the site's again.
func TestAdminPath(t *testing.T) {
	t.Parallel()

	s := sitetest.StartWith(t, sitetest.Options{
		StartingPoints: []string{"news"},
		Configure: func(config *webthings.Config) {
			config.AdminPath = "/toolbox"
		},
	}, allModules()...)

	if s.App.AdminPath() != "/toolbox" || s.App.AdminURL("admin.dashboard") != "/toolbox/" {
		t.Fatalf("admin at %q, dashboard %q", s.App.AdminPath(), s.App.AdminURL("admin.dashboard"))
	}

	// The bare path leads to the dashboard.
	if response, _ := s.Response("/toolbox"); response.Request.URL.Path != "/toolbox/" {
		t.Fatalf("GET /toolbox ends at %s", response.Request.URL.Path)
	}

	s.Mount("news", "News", "news")

	// Edit and save a post, all by route name.
	post := s.Submit(s.App.AdminURL("admin.news.new", url.Values{"locale": {"en"}}), url.Values{
		"Title": {"Harbour festival"},
		"Slug":  {"harbour-festival"},
		"Date":  {"2026-10-01"},
		"Body":  {"Boats and music."},
	})

	id := strings.TrimPrefix(post, "/toolbox/news/")
	if post != s.App.AdminURL("admin.news.edit", "id", id) {
		t.Fatalf("saved post at %s", post)
	}

	s.Publish(post + "?locale=en")
	s.Expect("/en/news/harbour-festival", http.StatusOK, "Boats and music.")

	// The site's edit link opens the admin where it is.
	link, _ := editLink(t, s, "/en/news/harbour-festival")
	if !strings.HasPrefix(link, post+"?") {
		t.Fatalf("edit link %q", link)
	}

	followPresence(t, s, post)

	// Every screen the admin links to answers, also those of modules, and
	// none names /admin.
	crawlAdmin(t, s)

	// /admin is the site's again: a missing page, which the redirects
	// module notes after the crawl, as it would any other.
	if status, _ := s.Get("/admin/"); status != http.StatusNotFound {
		t.Fatalf("GET /admin/: %d", status)
	}

	// Deleting moves the post to the trash, which lists it.
	s.Post(post+"?locale=en", s.App.AdminURL("admin.news.delete", "id", id), url.Values{})
	s.Expect(s.App.AdminURL("admin.trash", url.Values{"section": {"news"}}), http.StatusOK, "Harbour festival")

	// robots.txt does not name the admin, and its pages are not indexed.
	_, robots := s.Get("/robots.txt")
	if strings.Contains(robots, "toolbox") || defaultAdmin.MatchString(robots) {
		t.Fatalf("robots.txt names the admin:\n%s", robots)
	}

	// It names the sitemap by its route.
	if !strings.Contains(robots, "\nSitemap: http") || !strings.HasSuffix(robots, "/sitemap.xml\n") {
		t.Fatalf("robots.txt does not name the sitemap:\n%s", robots)
	}

	response, _ := s.Response("/toolbox/")
	if response.Header.Get("X-Robots-Tag") != "noindex, nofollow" {
		t.Fatalf("X-Robots-Tag %q", response.Header.Get("X-Robots-Tag"))
	}

	// Signing out leads to the sign-in page at the admin's path.
	out, _ := s.PostStatus(s.App.AdminURL("admin.dashboard"), s.App.AdminURL("admin.logout"), url.Values{})
	if out != http.StatusOK {
		t.Fatalf("sign out: %d", out)
	}

	response, body := s.Response("/toolbox/news")
	if response.Request.URL.Path != "/toolbox/login" || defaultAdmin.MatchString(body) {
		t.Fatalf("signed out, /toolbox/news ends at %s", response.Request.URL.Path)
	}
}

// followPresence opens the presence stream of page and reads its first
// event, which names no /admin address.
func followPresence(t *testing.T, s *sitetest.Site, page string) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	query := url.Values{"tab": {"path-tab"}, "page": {page + "?locale=en"}}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.App.AdminURL("admin.presence", query), nil)
	if err != nil {
		t.Fatal(err)
	}

	response := s.Request(request)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("presence stream: %d", response.StatusCode)
	}

	read := make(chan string, 1)

	go func() {
		reader := bufio.NewReader(response.Body)

		var event strings.Builder

		for {
			line, err := reader.ReadString('\n')
			event.WriteString(line)

			if err != nil || (line == "\n" && strings.Contains(event.String(), "data:")) {
				read <- event.String()

				return
			}
		}
	}()

	select {
	case event := <-read:
		if !strings.Contains(event, "data:") || defaultAdmin.MatchString(event) {
			t.Fatalf("presence event:\n%s", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no presence event")
	}
}

// crawlAdmin opens every address below /toolbox that the admin's pages link
// to or load, starting at the dashboard, and checks each answer: no error,
// and no /admin in the body or in a redirect on the way.
func crawlAdmin(t *testing.T, s *sitetest.Site) {
	t.Helper()

	queue := []string{s.App.AdminURL("admin.dashboard")}
	seen := map[string]bool{queue[0]: true}

	for len(queue) > 0 && len(seen) < 600 {
		address := queue[0]
		queue = queue[1:]

		response, body := s.Response(address)
		if response.StatusCode >= http.StatusBadRequest {
			t.Errorf("GET %s: %d", address, response.StatusCode)

			continue
		}

		for hop := response; hop != nil; hop = hop.Request.Response {
			if location := hop.Header.Get("Location"); defaultAdmin.MatchString(location) {
				t.Errorf("GET %s redirects to %s", address, location)
			}
		}

		if match := defaultAdmin.FindStringIndex(body); match != nil {
			start := max(match[0]-80, 0)
			t.Errorf("GET %s names /admin: …%s…", address, body[start:min(match[1]+80, len(body))])
		}

		if !strings.Contains(response.Header.Get("Content-Type"), "text/html") {
			continue
		}

		for _, found := range adminLink.FindAllStringSubmatch(body, -1) {
			link := html.UnescapeString(found[1])
			// Forms are posted, not opened.
			if !strings.HasPrefix(link, "/toolbox/") || seen[link] || strings.HasPrefix(found[0], "action=") {
				continue
			}

			seen[link] = true
			queue = append(queue, link)
		}
	}

	if len(seen) < 100 {
		t.Fatalf("the crawl found only %d addresses", len(seen))
	}
}

// TestModuleRouteNames checks that every bundled module names its admin
// routes "admin.<section>.<name>", so links reach them wherever the admin is.
func TestModuleRouteNames(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t, allModules()...)

	for _, route := range []struct {
		name string
		args []any
	}{
		{name: admin.RouteName("news", "index")},
		{name: admin.RouteName("news-categories", "index")},
		{name: admin.RouteName("proposals", "index")},
		{name: admin.RouteName("proposals", "settings")},
		{name: admin.RouteName("proposals", "show"), args: []any{"id", "x"}},
		{name: admin.RouteName("proposals", "accept"), args: []any{"id", "x"}},
		{name: admin.RouteName("proposals", "reject"), args: []any{"id", "x"}},
		{name: admin.RouteName("proposals", "delete"), args: []any{"id", "x"}},
		{name: admin.RouteName("guestbook", "index")},
		{name: admin.RouteName("guestbook", "settings")},
		{name: admin.RouteName("guestbook", "publish"), args: []any{"comment", "x"}},
		{name: admin.RouteName("guestbook", "unpublish"), args: []any{"comment", "x"}},
		{name: admin.RouteName("guestbook", "delete"), args: []any{"comment", "x"}},
		{name: admin.RouteName("guestbook", "reply"), args: []any{"comment", "x"}},
		{name: admin.RouteName("contact", "inbox")},
		{name: admin.RouteName("contact", "submission"), args: []any{"submission", "x"}},
		{name: admin.RouteName("contact", "submission-archive"), args: []any{"submission", "x"}},
		{name: admin.RouteName("contact", "submission-unarchive"), args: []any{"submission", "x"}},
		{name: admin.RouteName("contact", "submission-delete"), args: []any{"submission", "x"}},
		{name: admin.RouteName("photos", "order")},
		{name: admin.RouteName("photos", "images"), args: []any{"id", "x"}},
		{name: admin.RouteName("photos", "image-up"), args: []any{"id", "x", "image", "y"}},
		{name: admin.RouteName("photos", "image-down"), args: []any{"id", "x", "image", "y"}},
		{name: admin.RouteName("poll", "results")},
		{name: admin.RouteName("poll", "reset"), args: []any{"id", "x"}},
		{name: admin.RouteName("shops", "import")},
		{name: admin.RouteName("shops-categories", "index")},
		{name: admin.RouteName("redirects", "index")},
		{name: admin.RouteName("redirects", "new")},
		{name: admin.RouteName("redirects", "import")},
		{name: admin.RouteName("redirects", "missed")},
		{name: admin.RouteName("redirects", "missed-clear")},
		{name: admin.RouteName("redirects", "edit"), args: []any{"id", "x"}},
		{name: admin.RouteName("redirects", "delete"), args: []any{"id", "x"}},
		{name: admin.RouteName("letter", "index")},
		{name: admin.RouteName("letter", "export")},
		{name: admin.RouteName("letter", "import")},
		{name: admin.RouteName("letter", "subscriber-delete"), args: []any{"subscriber", "x"}},
		{name: admin.RouteName("letter", "mailings")},
		{name: admin.RouteName("letter", "mailing-new")},
		{name: admin.RouteName("letter", "mailing"), args: []any{"mailing", "x"}},
		{name: admin.RouteName("letter", "mailing-save"), args: []any{"mailing", "x"}},
		{name: admin.RouteName("letter", "mailing-send"), args: []any{"mailing", "x"}},
		{name: admin.RouteName("letter", "mailing-delete"), args: []any{"mailing", "x"}},
		{name: admin.RouteName("letter-texts", "index")},
		{name: admin.RouteName("stats", "index")},
		{name: admin.RouteName("stats", "settings")},
		{name: admin.RouteName("signup", "index")},
		{name: admin.RouteName("signup", "new")},
		{name: admin.RouteName("signup", "subject"), args: []any{"subject", "x"}},
		{name: admin.RouteName("signup", "settings"), args: []any{"subject", "x"}},
		{name: admin.RouteName("signup", "texts"), args: []any{"subject", "x"}},
		{name: admin.RouteName("signup", "export"), args: []any{"subject", "x"}},
		{name: admin.RouteName("signup", "registration"), args: []any{"subject", "x", "id", "y"}},
		{name: admin.RouteName("signup", "cancel"), args: []any{"subject", "x", "id", "y"}},
		{name: admin.RouteName("signup", "delete"), args: []any{"subject", "x", "id", "y"}},
		{name: admin.RouteName("faq", "index")},
		{name: admin.RouteName("quotes", "index")},
		{name: admin.RouteName("notice", "index")},
		{name: admin.RouteName("team", "edit"), args: []any{"id", "x"}},
		{name: admin.RouteName("files", "index")},
		{name: admin.RouteName("agenda", "edit"), args: []any{"id", "x"}},
	} {
		address := s.App.AdminURL(route.name, route.args...)
		if !strings.HasPrefix(address, "/admin/") {
			t.Errorf("%s: %q", route.name, address)
		}
	}
}

// TestAdminPathRefused checks that the admin cannot take the place of
// another core address, nor have a path that is not one.
func TestAdminPathRefused(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/media", "/_", "/assets/admin", "/admin/", "toolbox", "/.", "/admin/.."} {
		app := webthings.New(webthings.Config{AdminPath: path, StateDir: t.TempDir(), Insecure: true})

		err := app.Start(context.Background())
		_ = app.Close()

		if err == nil {
			t.Errorf("admin path %q is accepted", path)
		}
	}

	// The endpoints for operations are core addresses too: the admin can
	// be neither one of them nor below one.
	for _, path := range []string{"/healthz", "/metrics", "/readyz", "/readyz/admin", "/healthz/x"} {
		app := webthings.New(webthings.Config{AdminPath: path, StateDir: t.TempDir(), Insecure: true})

		err := app.Start(context.Background())
		_ = app.Close()

		if err == nil || !strings.Contains(err.Error(), "is one of the core paths") {
			t.Errorf("admin path %q: %v", path, err)
		}
	}
}

// TestAdminPathHidesNoLanguage checks that the admin's path cannot start
// with the code of an enabled language, whose pages it would hide: Start
// refuses it, on first start and with saved settings, and the site
// settings refuse to enable such a language.
func TestAdminPathHidesNoLanguage(t *testing.T) {
	t.Parallel()

	start := func(dir, adminPath string, enabled ...string) error {
		app := webthings.New(webthings.Config{
			AdminPath: adminPath,
			StateDir:  dir,
			Insecure:  true,
			Locales:   locales.Config{Default: "en", Enabled: enabled},
		})

		defer func() { _ = app.Close() }()

		return app.Start(context.Background())
	}

	// A first start says why, not only that the language is reserved.
	if err := start(
		t.TempDir(),
		"/en",
		"en",
	); err == nil ||
		!strings.Contains(err.Error(), "admin path /en is the address of the enabled language en") {
		t.Errorf("the admin at the default language's address: %v", err)
	}

	if err := start(t.TempDir(), "/NL/office", "en", "nl"); err == nil {
		t.Error("the admin below a language's address is accepted")
	}

	// Saved settings count, not the first start's languages.
	dir := t.TempDir()
	if err := start(dir, "/admin", "en", "nl"); err != nil {
		t.Fatal(err)
	}

	if err := start(dir, "/nl", "en"); err == nil {
		t.Error("the admin at an enabled language's address is accepted on a later start")
	}

	s := sitetest.StartWith(t, sitetest.Options{
		Configure: func(config *webthings.Config) {
			config.AdminPath = "/de"
		},
	})

	status, body := s.PostStatus("/de/site", "/de/site?locale=en", url.Values{
		"DefaultLocale": {"en"},
		"Locales":       {"en", "nl", "de"},
		"Timezone":      {"Europe/Amsterdam"},
	})
	if status != http.StatusUnprocessableEntity || !sitetest.HasToast(
		body,
		"error",
		"A language cannot have the code that one of the website's own addresses starts with, such as the admin's: its pages could not be reached.",
	) {
		t.Fatalf("enabling the admin's language: %d\n%s", status, body)
	}

	if settings, _ := s.App.Site.Settings(context.Background()); slices.Contains(settings.Locales, "de") {
		t.Fatalf("languages: %v", settings.Locales)
	}
}
