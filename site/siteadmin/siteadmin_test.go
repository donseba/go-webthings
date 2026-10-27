package siteadmin_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/site"
	"github.com/donseba/go-webthings/sitetest"
)

// editorWith creates an editor whose group allows actions in the site
// section and returns a browser signed in as that editor.
func editorWith(t *testing.T, s *sitetest.Site, email string, actions ...string) *sitetest.Site {
	t.Helper()
	ctx := context.Background()

	user, err := s.App.Auth.CreateUser(ctx, email, "", "editor long passphrase", auth.Editor)
	if err != nil {
		t.Fatal(err)
	}

	group, err := s.App.Auth.SaveGroup(ctx, auth.Group{Name: email, Permissions: auth.Permissions{"site": actions}})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.App.Auth.SetUserGroups(ctx, user.ID, []string{group.ID}); err != nil {
		t.Fatal(err)
	}

	return s.As(email, "editor long passphrase")
}

// saves reports whether body has a form posting to action, with or without
// a query, whose button is the plain "Save" every settings card has.
func saves(body, action string) bool {
	for _, part := range strings.Split(body, `<form action="`)[1:] {
		form, _, _ := strings.Cut(part, "</form>")
		if !strings.HasPrefix(form, action+`"`) && !strings.HasPrefix(form, action+"?") {
			continue
		}

		if strings.Contains(form, ">Save</button>") {
			return true
		}
	}

	return false
}

// TestSettingsReadOnly checks that an editor who may only view the site
// settings sees them read-only.
func TestSettingsReadOnly(t *testing.T) {
	s := sitetest.Start(t)
	reader := editorWith(t, s, "reader@example.test", auth.Read)

	_, body := reader.Get("/admin/site")
	if strings.Contains(body, ">Save</button>") ||
		!strings.Contains(body, "You can view these settings but not change them.") {
		t.Fatalf("a reader does not get the settings read-only:\n%s", body)
	}

	if _, body := s.Get("/admin/site"); !saves(body, "/admin/site") || !saves(body, "/admin/site/text") {
		t.Fatal("the administrator gets the settings read-only")
	}
}

// TestReviewToggleIsAdminOnly checks that only administrators switch the
// review workflow: editors who may edit the settings see it read-only and
// are refused when they post it.
func TestReviewToggleIsAdminOnly(t *testing.T) {
	s := sitetest.Start(t)
	ctx := context.Background()
	editor := editorWith(t, s, "editor@example.test", auth.Read, auth.Edit)

	_, body := editor.Get("/admin/site")
	if !strings.Contains(body, "Only administrators can switch the review workflow on or off.") {
		t.Fatalf("the review form is not read-only for editors:\n%s", body)
	}

	status, body := editor.PostStatus("/admin/site", "/admin/site/review", url.Values{"Sections": {"pages"}})
	if status != http.StatusForbidden || !strings.Contains(body, "You are not allowed to do this.") {
		t.Fatalf("an editor switched review on: %d", status)
	}

	if settings, _ := s.App.Site.Settings(ctx); len(settings.Review) != 0 {
		t.Fatalf("review: %v", settings.Review)
	}

	s.Post("/admin/site", "/admin/site/review", url.Values{"Sections": {"pages"}})

	if settings, _ := s.App.Site.Settings(ctx); !slices.Equal(settings.Review, []string{"pages"}) {
		t.Fatalf("the administrator's review: %v", settings.Review)
	}
}

// TestSecurityIsAdminOnly checks that only administrators change the
// Security card: editors who may edit the settings see it read-only and are
// refused when they post it.
func TestSecurityIsAdminOnly(t *testing.T) {
	s := sitetest.Start(t)
	ctx := context.Background()
	editor := editorWith(t, s, "editor@example.test", auth.Read, auth.Edit)

	_, body := editor.Get("/admin/site")
	if !strings.Contains(body, "Only administrators can change the security settings.") ||
		saves(body, "/admin/site/security") {
		t.Fatalf("the security form is not read-only for editors:\n%s", body)
	}

	if !saves(body, "/admin/site") {
		t.Fatal("the editor gets the other settings read-only")
	}

	status, body := editor.PostStatus("/admin/site", "/admin/site/security", url.Values{"Framing": {site.FramingAny}})
	if status != http.StatusForbidden || !strings.Contains(body, "You are not allowed to do this.") {
		t.Fatalf("an editor changed the security settings: %d", status)
	}

	if settings, _ := s.App.Site.Settings(ctx); settings.FramingPolicy() != site.FramingSameOrigin {
		t.Fatalf("framing: %q", settings.Framing)
	}

	_, body = s.Get("/admin/site")
	if !saves(body, "/admin/site/security") ||
		strings.Contains(body, "Only administrators can change the security settings.") {
		t.Fatal("the administrator gets the security form read-only")
	}

	s.Post("/admin/site", "/admin/site/security", url.Values{"Framing": {site.FramingAny}})

	if settings, _ := s.App.Site.Settings(ctx); settings.FramingPolicy() != site.FramingAny {
		t.Fatalf("the administrator's framing: %q", settings.Framing)
	}
}

// TestFixedByConfiguration checks that the cards show what the server
// configuration sets instead of the stored setting, disabled with a note,
// and that saving a card keeps the stored setting.
func TestFixedByConfiguration(t *testing.T) {
	ctx := context.Background()
	s := sitetest.StartWith(t, sitetest.Options{Configure: func(config *webthings.Config) {
		config.ContentSecurityPolicy = site.CSPEnforce
		config.OutputCacheTTL = 45 * time.Second
	}})

	disabled := `(?s)<select[^>]*name="%s"[^>]*\sdisabled[^>]*>.*?<option value="%s" selected`
	policy := regexp.MustCompile(fmt.Sprintf(disabled, "ContentSecurity", "enforce"))
	cache := regexp.MustCompile(fmt.Sprintf(disabled, "OutputCache", "45") + `[^>]*>45s</option>`)

	_, body := s.Get("/admin/site")
	if !policy.MatchString(body) || !cache.MatchString(body) ||
		strings.Count(body, "Set by the server configuration.") != 2 {
		t.Fatalf("the cards do not show the configuration:\n%s", body)
	}

	s.Post("/admin/site", "/admin/site/security", url.Values{"Framing": {site.FramingAny}, "ContentSecurity": {site.CSPOff}})
	s.Post("/admin/site", "/admin/site/performance", url.Values{"OutputCache": {"60"}, "OutputCacheMB": {"8"}})

	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if settings.ContentSecurity != "" || settings.OutputCacheTTL != 0 || settings.OutputCacheOff {
		t.Fatalf("saving changed what the configuration sets: %+v", settings)
	}

	if settings.FramingPolicy() != site.FramingAny || settings.OutputCacheMB != 8 {
		t.Fatalf("saving did not store the other settings: %+v", settings)
	}

	// The output cache is off in tests: the card says so.
	plain := sitetest.Start(t)
	off := regexp.MustCompile(fmt.Sprintf(disabled, "OutputCache", "0"))

	_, body = plain.Get("/admin/site")
	if !off.MatchString(body) || strings.Count(body, "Set by the server configuration.") != 1 {
		t.Fatalf("the output cache the configuration turns off:\n%s", body)
	}

	// Without either, the settings decide.
	free := sitetest.StartWith(t, sitetest.Options{OutputCache: true, Configure: func(config *webthings.Config) {
		config.AdminLanguage = "nl"
	}})

	_, body = free.Get("/admin/site")
	if strings.Contains(body, "Ingesteld door de serverconfiguratie.") ||
		regexp.MustCompile(`<select[^>]*name="(ContentSecurity|OutputCache)"[^>]*\sdisabled`).MatchString(body) {
		t.Fatalf("the settings are fixed without configuration:\n%s", body)
	}

	// Dutch has the note.
	dutch := sitetest.StartWith(t, sitetest.Options{Configure: func(config *webthings.Config) {
		config.AdminLanguage = "nl"
		config.ContentSecurityPolicy = site.CSPReportOnly
	}})

	_, body = dutch.Get("/admin/site")
	if strings.Count(body, "Ingesteld door de serverconfiguratie.") != 2 {
		t.Fatalf("the Dutch note is missing:\n%s", body)
	}
}

// themes are two themes, the first with settings.
func themes() fstest.MapFS {
	file := func(source string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(source)}
	}

	return fstest.MapFS{
		"default/theme.json": file(`{"title": "Plain", "settings": [
			{"name": "accent", "label": "Accent", "type": "color", "default": "#336699"}
		]}`),
		"default/layout.gohtml": file(`<html><body>{{content}}</body></html>`),
		"default/page.gohtml":   file(`<main id="content"><h1>{{.Title}}</h1></main>`),
		"dark/theme.json":       file(`{"title": "Dark"}`),
		"dark/layout.gohtml":    file(`<html><body class="dark">{{content}}</body></html>`),
	}
}

// TestThemeReadOnly checks that an editor who may only view the site
// settings cannot choose a theme and sees its settings read-only.
func TestThemeReadOnly(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Files: themes()})
	reader := editorWith(t, s, "reader@example.test", auth.Read)

	disabled := regexp.MustCompile(`<input[^>]*type="color"[^>]*\sdisabled\s`)

	_, body := reader.Get("/admin/theme")
	if strings.Contains(body, "/admin/theme/activate") || saves(body, "/admin/theme/values") ||
		!disabled.MatchString(body) {
		t.Fatalf("a reader can change the theme:\n%s", body)
	}

	_, body = s.Get("/admin/theme")
	if !strings.Contains(body, "/admin/theme/activate") || !saves(body, "/admin/theme/values") ||
		disabled.MatchString(body) {
		t.Fatalf("the administrator cannot change the theme:\n%s", body)
	}

	// Using another theme changes the website at once, so it asks first.
	activate := regexp.MustCompile(
		`(?s)<form action="/admin/theme/activate[^"]*"[^>]*hx-confirm="Use this theme on the website\? Visitors see it right away\."`,
	)
	if !activate.MatchString(body) {
		t.Fatalf("using a theme does not ask first:\n%s", body)
	}
}

// TestTrashCard checks the Trash card: editors who may change the site
// settings set after how many days the trash is emptied, between 0 (never,
// the default) and 3650 days; others see it read-only. The card speaks
// English and Dutch.
func TestTrashCard(t *testing.T) {
	s := sitetest.Start(t)
	ctx := context.Background()
	editor := editorWith(t, s, "editor@example.test", auth.Read, auth.Edit)
	reader := editorWith(t, s, "reader@example.test", auth.Read)

	_, body := editor.Get("/admin/site")
	if !strings.Contains(body, "Delete items in the trash for good after (days)") || !saves(body, "/admin/site/trash") {
		t.Fatalf("the editor gets no Trash card:\n%s", body)
	}

	if _, body := reader.Get("/admin/site"); saves(body, "/admin/site/trash") {
		t.Fatal("a reader can change the Trash card")
	}

	editor.Post("/admin/site", "/admin/site/trash", url.Values{"Days": {"30"}})

	if settings, _ := s.App.Site.Settings(ctx); settings.TrashDays != 30 {
		t.Fatalf("days: %d", settings.TrashDays)
	}

	if _, body := s.Get("/admin/site"); !regexp.MustCompile(`<input[^>]*name="Days"[^>]*value="30"`).MatchString(body) {
		t.Fatalf("the card does not show the days:\n%s", body)
	}

	refusals := map[string]string{
		"-1":   "Enter a value of at least 0.",
		"3651": "Enter a value of at most 3650.",
		"soon": "Enter a valid value.",
	}

	for days, message := range refusals {
		status, body := s.PostStatus("/admin/site", "/admin/site/trash", url.Values{"Days": {days}})
		if status != http.StatusUnprocessableEntity || !strings.Contains(body, `id="Days_error_0" role="alert">`+message) {
			t.Fatalf("%s days: %d\n%s", days, status, body)
		}
	}

	if settings, _ := s.App.Site.Settings(ctx); settings.TrashDays != 30 {
		t.Fatalf("a refused value changed the days: %d", settings.TrashDays)
	}

	// Empty is never, as 0 is.
	s.Post("/admin/site", "/admin/site/trash", url.Values{"Days": {""}})

	if settings, _ := s.App.Site.Settings(ctx); settings.TrashDays != 0 {
		t.Fatalf("empty days: %d", settings.TrashDays)
	}

	dutch := sitetest.StartWith(t, sitetest.Options{Configure: func(config *webthings.Config) {
		config.AdminLanguage = "nl"
	}})

	_, body = dutch.Get("/admin/site")
	if !strings.Contains(body, "Items in de prullenbak definitief verwijderen na (dagen)") ||
		!strings.Contains(body, "Hoe lang verwijderde items in de prullenbak blijven.") {
		t.Fatalf("the Dutch card:\n%s", body)
	}
}
