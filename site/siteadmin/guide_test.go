package siteadmin_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/site"
	"github.com/donseba/go-webthings/sitetest"
)

// guideRoutes are the setup guide's routes, by name, with an address and
// the method of each.
var guideRoutes = []struct {
	name   string
	args   []any
	method string
	path   string
}{
	{"admin.site.guide", nil, http.MethodGet, "/admin/site/guide"},
	{"admin.site.guide-step", []any{"step", "site"}, http.MethodGet, "/admin/site/guide/site"},
	{"admin.site.guide-skip", []any{"step", "mail"}, http.MethodPost, "/admin/site/guide/mail/skip"},
	{"admin.site.guide-pages", nil, http.MethodPost, "/admin/site/guide/pages"},
	{"admin.site.guide-mail", nil, http.MethodPost, "/admin/site/guide/mail"},
	{"admin.site.guide-finish", nil, http.MethodPost, "/admin/site/guide/finish"},
	{"admin.site.guide-reopen", nil, http.MethodPost, "/admin/site/guide/reopen"},
}

// TestGuideRoutes checks the names and addresses of the guide's routes.
func TestGuideRoutes(t *testing.T) {
	s := sitetest.Start(t)

	for _, route := range guideRoutes {
		if got := s.App.AdminURL(route.name, route.args...); got != route.path {
			t.Errorf("%s is %q, want %q", route.name, got, route.path)
		}
	}
}

// TestGuideIsForAdministrators checks that editors, also those who may
// edit the settings, never get the guide: its routes refuse them, the
// dashboard shows them no card, and ?guide= on a card's post is ignored.
func TestGuideIsForAdministrators(t *testing.T) {
	s := sitetest.Start(t)
	editor := editorWith(t, s, "editor@example.test", auth.Read, auth.Edit)

	for _, route := range guideRoutes {
		var status int

		if route.method == http.MethodGet {
			status, _ = editor.Get(route.path)
		} else {
			status, _ = editor.PostStatus("/admin/site", route.path, url.Values{})
		}

		if status != http.StatusForbidden {
			t.Errorf("%s %s for an editor: %d", route.method, route.path, status)
		}
	}

	if body := editor.Dashboard(); strings.Contains(body, "Finish setting up your site") {
		t.Fatal("an editor sees the guide's card")
	}

	if body := s.Dashboard(); !strings.Contains(body, "Finish setting up your site") ||
		!strings.Contains(body, "0 of 5 steps done") || !strings.Contains(body, `href="/admin/site/guide"`) {
		t.Fatalf("the administrator does not see the guide's card:\n%s", body)
	}

	// The editor's save stays on Site settings, outside the guide.
	page := editor.Post("/admin/site", "/admin/site/text?guide=site&locale=en", url.Values{"Title": {"Editor's title"}})
	if page != "/admin/site" {
		t.Fatalf("the editor's save ended on %s", page)
	}

	if _, body := editor.Get("/admin/site"); strings.Contains(body, "Continue setup") {
		t.Fatal("Site settings offers an editor the guide")
	}
}

// TestGuideSiteStep checks the Site step: the General card limited to the
// languages and timezone and the Texts card to the title and tagline, both
// saved by their own handlers, which keep the fields the guide leaves out
// and refuse inside the guide.
func TestGuideSiteStep(t *testing.T) {
	s := sitetest.Start(t)
	ctx := context.Background()

	current, _ := s.App.Site.Settings(ctx)
	current.Organisation = "Acme"
	current.Translations = current.WithText(site.Translation{Locale: "en", Title: "Old", SEOTitle: "Kept SEO title"})

	if err := s.App.Site.Save(ctx, current); err != nil {
		t.Fatal(err)
	}

	_, body := s.Get("/admin/site/guide/site")
	if !strings.Contains(body, "Getting started") || !strings.Contains(body, `name="Timezone"`) ||
		!strings.Contains(body, `name="Tagline"`) || strings.Contains(body, `name="Organisation"`) ||
		strings.Contains(body, `name="SEOTitle"`) || !strings.Contains(body, `action="/admin/site?guide=site&amp;locale=en"`) {
		t.Fatalf("the Site step:\n%s", body)
	}

	// A refusal stays in the guide.
	status, body := s.PostStatus("/admin/site/guide/site", "/admin/site?guide=site&locale=en", url.Values{
		"DefaultLocale": {"nl"}, "Locales": {"en"}, "Timezone": {"Europe/Amsterdam"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Getting started") ||
		!strings.Contains(body, "Tick the default language under Languages as well.") {
		t.Fatalf("a refused save: %d\n%s", status, body)
	}

	page := s.Post("/admin/site/guide/site", "/admin/site?guide=site&locale=en", url.Values{
		"DefaultLocale": {"en"}, "Locales": {"en", "nl"}, "Timezone": {"Europe/Amsterdam"},
	})
	if page != "/admin/site/guide/site" {
		t.Fatalf("the languages ended on %s", page)
	}

	current, _ = s.App.Site.Settings(ctx)
	if current.Timezone != "Europe/Amsterdam" || current.Organisation != "Acme" ||
		!slices.Equal(current.Locales, []string{"en", "nl"}) {
		t.Fatalf("the languages saved %+v", current)
	}

	status, body = s.PostStatus("/admin/site/guide/site", "/admin/site/text?guide=site&locale=en", url.Values{"Title": {""}})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Getting started") {
		t.Fatalf("a text without title: %d\n%s", status, body)
	}

	// The texts of each language follow; the last finishes the step.
	s.Post(
		"/admin/site/guide/site",
		"/admin/site/text?guide=site&locale=en",
		url.Values{"Title": {"My site"}, "Tagline": {"Hello"}},
	)

	current, _ = s.App.Site.Settings(ctx)
	if text := current.Text("en"); text.Title != "My site" || text.Tagline != "Hello" || text.SEOTitle != "Kept SEO title" {
		t.Fatalf("the English texts: %+v", text)
	}

	if slices.Contains(current.Setup, "site") {
		t.Fatal("the step finished before the last language")
	}

	page = s.Post("/admin/site/guide/site?locale=nl", "/admin/site/text?guide=site&locale=nl", url.Values{"Title": {"Mijn site"}})
	if page != "/admin/site/guide/theme" {
		t.Fatalf("the last language ended on %s", page)
	}

	if current, _ = s.App.Site.Settings(ctx); !slices.Contains(current.Setup, "site") {
		t.Fatalf("the Site step is not finished: %v", current.Setup)
	}
}

// TestGuideThemeStep checks the Theme step: the theme cards with their
// pictures, the theme's settings and the logo, saved by their own
// handlers; the logo finishes the step.
func TestGuideThemeStep(t *testing.T) {
	files := themes()
	files["dark/theme.json"] = &fstest.MapFile{Data: []byte(`{"title": "Dark", "screenshot": "shot.png"}`)}
	files["dark/assets/shot.png"] = &fstest.MapFile{Data: sitetest.PNG(t, 8, 5)}

	s := sitetest.StartWith(t, sitetest.Options{Files: files})
	ctx := context.Background()

	_, body := s.Get("/admin/site/guide/theme")
	// The theme without a picture gets a placeholder of the same size.
	if !regexp.MustCompile(`<img class="theme-screenshot" src="/theme/dark/shot\.png\?v=[^"]+"`).MatchString(body) ||
		!regexp.MustCompile(`class="theme-screenshot theme-screenshot-empty"[^>]*><span>Plain</span>`).MatchString(body) ||
		!strings.Contains(body, `action="/admin/theme/activate?guide=theme"`) || !strings.Contains(body, `name="Logo"`) ||
		strings.Contains(body, `name="Timezone"`) {
		t.Fatalf("the Theme step:\n%s", body)
	}

	if page := s.Post(
		"/admin/site/guide/theme",
		"/admin/theme/activate?guide=theme",
		url.Values{"theme": {"dark"}},
	); page != "/admin/site/guide/theme" {
		t.Fatalf("activating ended on %s", page)
	}

	if current, _ := s.App.Site.Settings(ctx); current.Theme != "dark" || slices.Contains(current.Setup, "theme") {
		t.Fatalf("activating: %q %v", current.Theme, current.Setup)
	}

	status, body := s.PostStatus("/admin/site/guide/theme?theme=default", "/admin/theme/values?guide=theme",
		url.Values{"theme": {"default"}, "value.accent": {"red"}})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Getting started") {
		t.Fatalf("a refused colour: %d\n%s", status, body)
	}

	s.Post("/admin/site/guide/theme?theme=default", "/admin/theme/values?guide=theme",
		url.Values{"theme": {"default"}, "value.accent": {"#112233"}})

	if values, _ := s.App.Site.ThemeValues(ctx, "default"); values["accent"] != "#112233" {
		t.Fatalf("values %v", values)
	}

	page := s.Post("/admin/site/guide/theme", "/admin/site?guide=theme", url.Values{"Logo": {""}})
	if page != "/admin/site/guide/mail" {
		t.Fatalf("the logo ended on %s", page)
	}

	if current, _ := s.App.Site.Settings(ctx); !slices.Contains(current.Setup, "theme") || len(current.Locales) != 2 {
		t.Fatalf("the logo: %v %v", current.Setup, current.Locales)
	}
}

// TestGuideSecurityStep checks the Security step: saved by the Security
// card's handler, which finishes the step, and the policy the server
// configuration sets shown disabled.
func TestGuideSecurityStep(t *testing.T) {
	s := sitetest.Start(t)
	ctx := context.Background()

	page := s.Post("/admin/site/guide/security", "/admin/site/security?guide=security", url.Values{
		"Framing": {site.FramingSameOrigin}, "ContentSecurity": {site.CSPReportOnly},
	})
	if page != "/admin/site/guide/site" {
		t.Fatalf("the Security step ended on %s", page)
	}

	if current, _ := s.App.Site.Settings(
		ctx,
	); current.ContentSecurity != site.CSPReportOnly ||
		!slices.Contains(current.Setup, "security") {
		t.Fatalf("security: %q %v", current.ContentSecurity, current.Setup)
	}

	status, body := s.PostStatus("/admin/site/guide/security", "/admin/site/security?guide=security", url.Values{
		"Framing": {site.FramingSameOrigin}, "Sources": {"script-src *"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Getting started") {
		t.Fatalf("refused sources: %d\n%s", status, body)
	}

	fixed := sitetest.StartWith(t, sitetest.Options{Configure: func(config *webthings.Config) {
		config.ContentSecurityPolicy = site.CSPEnforce
	}})

	disabled := regexp.MustCompile(
		`(?s)<select[^>]*name="ContentSecurity"[^>]*\sdisabled[^>]*>.*?<option value="enforce" selected`,
	)

	_, body = fixed.Get("/admin/site/guide/security")
	if !disabled.MatchString(body) || !strings.Contains(body, "Set by the server configuration.") {
		t.Fatalf("the fixed policy is not disabled:\n%s", body)
	}
}

// TestGuideProgress checks that skipping and resuming survive a restart,
// that the dashboard's card shows until the guide is finished, and that
// opening it again brings the card back.
func TestGuideProgress(t *testing.T) {
	dir := t.TempDir()
	options := sitetest.Options{Configure: func(config *webthings.Config) {
		config.StateDir = dir
	}}

	s := sitetest.StartWith(t, options)

	if _, body := s.Get("/admin/site/guide"); !strings.Contains(body, "Welcome to your new site") {
		t.Fatalf("a new guide does not welcome:\n%s", body)
	}

	if page := s.Post("/admin/site/guide/site", "/admin/site/guide/site/skip", url.Values{}); page != "/admin/site/guide/theme" {
		t.Fatalf("skipping ended on %s", page)
	}

	s.Post("/admin/site/guide/mail", "/admin/site/guide/mail/skip", url.Values{})

	// After a restart the guide resumes at the first open step.
	again := sitetest.StartWith(t, options)

	_, body := again.Get("/admin/site/guide")
	if !strings.Contains(body, `aria-current="step"`) ||
		!regexp.MustCompile(`(?s)href="/admin/site/guide/theme" aria-current="step"`).MatchString(body) {
		t.Fatalf("the guide does not resume at Theme:\n%s", body)
	}

	if !strings.Contains(again.Dashboard(), "2 of 5 steps done") {
		t.Fatal("the card does not count the skipped steps")
	}

	// A skipped step is finished when its card is saved after all.
	again.Post("/admin/site/guide/site", "/admin/site/text?guide=site&locale=nl", url.Values{"Title": {"Site"}})

	if current, _ := again.App.Site.Settings(context.Background()); !slices.Contains(current.Setup, "site") ||
		slices.Contains(current.Setup, "skip:site") {
		t.Fatalf("setup %v", current.Setup)
	}

	if page := again.Post("/admin/site/guide/done", "/admin/site/guide/finish", url.Values{}); page != "/admin/" {
		t.Fatalf("finishing ended on %s", page)
	}

	if body := again.Dashboard(); strings.Contains(body, "Finish setting up your site") {
		t.Fatal("the card shows after the guide was finished")
	}

	_, body = again.Get("/admin/site")
	if strings.Contains(body, "Continue setup") || !strings.Contains(body, `action="/admin/site/guide/reopen"`) {
		t.Fatalf("Site settings does not offer to open the guide again:\n%s", body)
	}

	again.Post("/admin/site", "/admin/site/guide/reopen", url.Values{})

	if body := again.Dashboard(); !strings.Contains(body, "Finish setting up your site") {
		t.Fatal("the card does not come back")
	}
}

// TestGuideAllStepsDone checks that a guide whose steps are all done or
// skipped, but which is not finished, asks only to finish: no "5 of 5
// steps done" with a button to continue.
func TestGuideAllStepsDone(t *testing.T) {
	s := sitetest.Start(t)

	for _, step := range []string{"site", "theme", "mail", "pages", "security"} {
		s.Post("/admin/site/guide/"+step, "/admin/site/guide/"+step+"/skip", url.Values{})
	}

	body := s.Dashboard()
	if !strings.Contains(body, "Everything is ready — finish setting up") || !strings.Contains(body, "All steps done") ||
		!strings.Contains(body, `href="/admin/site/guide/done"`) || strings.Contains(body, "5 of 5 steps done") ||
		strings.Contains(body, "Continue setup") {
		t.Fatalf("the card of a guide with every step done:\n%s", body)
	}

	_, body = s.Get("/admin/site/guide/done")
	if !strings.Contains(body, "All steps done") || strings.Contains(body, "5 of 5 steps done") {
		t.Fatalf("the guide's header:\n%s", body)
	}

	// With a step open, the count shows.
	current, _ := s.App.Site.Settings(context.Background())
	current.Setup = slices.DeleteFunc(current.Setup, func(entry string) bool {
		return entry == "skip:mail"
	})

	if err := s.App.Site.Save(context.Background(), current); err != nil {
		t.Fatal(err)
	}

	if body := s.Dashboard(); !strings.Contains(body, "4 of 5 steps done") || !strings.Contains(body, "Continue setup") {
		t.Fatalf("the card with a step open:\n%s", body)
	}
}

// TestGuidePages checks the Pages step: a published page below Home for a
// module, a draft for an ordinary page, nothing twice when the form is
// posted again, and a title that makes no address refused at its row.
func TestGuidePages(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{StartingPoints: []string{"news"}}, publishing.Module("news", "News"))
	ctx := context.Background()

	_, body := s.Get("/admin/site/guide/pages")
	if !strings.Contains(body, `name="module-news.create"`) || !strings.Contains(body, `name="page-about.title.nl"`) ||
		!strings.Contains(body, `value="News"`) {
		t.Fatalf("the Pages step:\n%s", body)
	}

	status, body := s.PostStatus("/admin/site/guide/pages", "/admin/site/guide/pages", url.Values{
		"module-news.create": {"on"}, "module-news.title.en": {"!!!"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "the title makes no address") {
		t.Fatalf("a title without address: %d\n%s", status, body)
	}

	values := url.Values{
		"module-news.create":   {"on"},
		"module-news.title.en": {"News"},
		"module-news.title.nl": {"Nieuws"},
		"page-about.create":    {"on"},
		"page-about.title.en":  {"About us"},
	}

	s.Post("/admin/site/guide/pages", "/admin/site/guide/pages", values)

	before, err := s.App.Pages.ListLatest(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(before) != 3 {
		t.Fatalf("%d pages", len(before))
	}

	for _, locale := range []string{"en", "nl"} {
		if path, err := s.App.Pages.MountPath(ctx, "news", locale); err != nil || path == "" {
			t.Fatalf("news in %s: %q %v", locale, path, err)
		}
	}

	for _, page := range before {
		if text, _ := page.Text("en"); text.Title == "About us" && page.Published {
			t.Fatal("the plain page was published")
		}
	}

	// Posting the same form again makes nothing twice.
	s.PostStatus("/admin/site/guide/pages", "/admin/site/guide/pages", values)

	if after, _ := s.App.Pages.ListLatest(ctx); len(after) != len(before) {
		t.Fatalf("posting again made %d pages", len(after)-len(before))
	}

	_, body = s.Get("/admin/site/guide/pages")
	if !strings.Contains(body, "On page News") || !strings.Contains(body, "On page About us") ||
		!strings.Contains(body, "Write the first content") {
		t.Fatalf("the Pages step after:\n%s", body)
	}

	if current, _ := s.App.Site.Settings(ctx); !slices.Contains(current.Setup, "pages") {
		t.Fatalf("setup %v", current.Setup)
	}
}

// TestGuidePagesUnderReview checks that a module page the review keeps from
// being published is refused at its row, and not made, instead of failing
// the whole step.
func TestGuidePagesUnderReview(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{StartingPoints: []string{"news"}}, publishing.Module("news", "News"))
	ctx := context.Background()

	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	settings.Review = []string{"pages"}
	if err := s.App.Site.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}

	before, err := s.App.Pages.ListLatest(ctx)
	if err != nil {
		t.Fatal(err)
	}

	status, body := s.PostStatus("/admin/site/guide/pages", "/admin/site/guide/pages", url.Values{
		"module-news.create":   {"on"},
		"module-news.title.en": {"News"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Pages need a review before they are published.") ||
		!strings.Contains(body, `name="module-news.create"`) {
		t.Fatalf("a module page under review: %d\n%s", status, body)
	}

	if after, _ := s.App.Pages.ListLatest(ctx); len(after) != len(before) {
		t.Fatalf("the refused page was kept: %d pages, %d before", len(after), len(before))
	}
}

// failing is a sender that cannot send.
type failing struct{}

func (failing) Send(context.Context, mail.Message) error {
	return errors.New("mail: connect: connection refused")
}

// TestGuideMail checks the Mail step: the sender described, the test
// e-mail at the administrator's address, one per half minute, and the
// error of a sender that fails as a toast.
func TestGuideMail(t *testing.T) {
	s := sitetest.Start(t)

	_, body := s.Get("/admin/site/guide/mail")
	if !strings.Contains(body, "The site sends e-mail with memory, for tests, not sent.") ||
		!strings.Contains(body, "The site has no public address (BaseURL)") ||
		!strings.Contains(body, ">Send a test e-mail to admin@example.test</button>") {
		t.Fatalf("the Mail step:\n%s", body)
	}

	sent := len(s.Mail.Sent())

	status, body := s.PostStatus("/admin/site/guide/mail", "/admin/site/guide/mail", url.Values{})
	if status != http.StatusOK || !sitetest.HasToast(body, "success", "Test e-mail sent to admin@example.test.") {
		t.Fatalf("the test e-mail: %d\n%s", status, body)
	}

	messages := s.Mails(sent + 1)
	if last := messages[len(messages)-1]; !slices.Equal(last.To, []string{"admin@example.test"}) ||
		!strings.Contains(last.Subject, "Test e-mail from") {
		t.Fatalf("the test e-mail: %+v", last)
	}

	if current, _ := s.App.Site.Settings(context.Background()); !slices.Contains(current.Setup, "mail") {
		t.Fatalf("setup %v", current.Setup)
	}

	status, body = s.PostStatus("/admin/site/guide/mail", "/admin/site/guide/mail", url.Values{})
	if status != http.StatusTooManyRequests ||
		!sitetest.HasToast(body, "error", "Wait half a minute before you send another test e-mail.") {
		t.Fatalf("a second test e-mail: %d\n%s", status, body)
	}

	if len(s.Mail.Sent()) != sent+1 {
		t.Fatal("the second test e-mail was sent")
	}

	broken := sitetest.StartWith(t, sitetest.Options{Configure: func(config *webthings.Config) {
		config.Mail = failing{}
	}})

	_, body = broken.Get("/admin/site/guide/mail")
	if !strings.Contains(body, "The site sends e-mail with the sender its code sets.") {
		t.Fatalf("a sender that does not describe itself:\n%s", body)
	}

	_, body = broken.PostStatus("/admin/site/guide/mail", "/admin/site/guide/mail", url.Values{})
	if !sitetest.HasToast(body, "error", "The test e-mail could not be sent: mail: connect: connection refused") {
		t.Fatalf("the failure is not shown:\n%s", body)
	}

	if current, _ := broken.App.Site.Settings(context.Background()); slices.Contains(current.Setup, "mail") {
		t.Fatal("a failed test finished the step")
	}
}

// TestGuideDutch checks the guide in Dutch, in the je-form.
func TestGuideDutch(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Configure: func(config *webthings.Config) {
		config.AdminLanguage = "nl"
	}})

	_, body := s.Get("/admin/site/guide/welcome")
	if !strings.Contains(body, "Aan de slag") || !strings.Contains(body, "Welkom op je nieuwe website") {
		t.Fatalf("the Dutch guide:\n%s", body)
	}

	if body := s.Dashboard(); !strings.Contains(body, fmt.Sprintf("%d van %d stappen klaar", 0, 5)) ||
		!strings.Contains(body, "Verder met instellen") {
		t.Fatalf("the Dutch card:\n%s", body)
	}
}
