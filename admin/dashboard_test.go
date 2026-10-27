package admin_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
)

// contentSection is a section with dashboard content: one recent item,
// a planned publication, a badge and a "new" action.
func contentSection(id, title, group string, changed time.Time, author string, badge int) admin.Section {
	section := withBadge(plainSection(id, title, group), badge)
	section.NewLabel = "New " + id
	section.NewPath = "/" + id + "/new"
	section.Recent = func(_ context.Context, limit int) ([]admin.RecentItem, error) {
		if limit <= 0 {
			return nil, nil
		}

		return []admin.RecentItem{{
			Title:     "Latest " + id,
			URL:       "/admin/" + id + "/1",
			UpdatedAt: changed,
			Status:    "Draft",
			Author:    author,
		}}, nil
	}
	section.Scheduled = func(_ context.Context, limit int) ([]admin.ScheduledItem, error) {
		if limit <= 0 {
			return nil, nil
		}

		return []admin.ScheduledItem{{
			Title:       "Planned " + id,
			URL:         "/admin/" + id + "/2",
			PublishAt:   changed.Add(72 * time.Hour),
			UnpublishAt: changed.Add(96 * time.Hour),
		}}, nil
	}

	return section
}

func TestDashboard(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	accounts := newAccounts(t)

	users, err := accounts.Users(ctx)
	if err != nil {
		t.Fatal(err)
	}

	owner := users[0]

	amsterdam, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}

	newer := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	older := time.Date(2025, 12, 30, 8, 30, 0, 0, time.UTC)

	// The editor may read news and docs, create only news, and never see
	// the secret section.
	authorize := func(_ context.Context, _ auth.User, permission admin.Permission) bool {
		switch permission.Section {
		case "secret":
			return false
		case "docs":
			return permission.Action == admin.View
		default:
			return true
		}
	}

	b := startWith(t, accounts, admin.Options{
		Language:  "en",
		Authorize: authorize,
		Timezone: func(context.Context) *time.Location {
			return amsterdam
		},
	},
		contentSection("news", "News", "", newer, owner.ID, 3),
		contentSection("docs", "Docs", "Content", older, "", 0),
		contentSection("secret", "Secret", "Content", newer.Add(time.Hour), "", 4),
	)
	b.signIn()

	_, page := b.get("/admin/")

	// The page shows at once with placeholders that load the panels.
	for _, panel := range []string{"dashboard-recent", "dashboard-attention", "dashboard-scheduled"} {
		if !strings.Contains(page, `id="`+panel+`"`) {
			t.Errorf("the page lacks the %s placeholder", panel)
		}
	}

	if strings.Contains(page, "Latest news") ||
		!strings.Contains(page, `hx-get="/admin/" hx-swap="outerHTML" hx-target="this" hx-trigger="load"`) {
		t.Error("the page queried the sections instead of loading the panels after it")
	}

	body := b.panel("dashboard-recent") + b.panel("dashboard-attention") + b.panel("dashboard-scheduled")

	if strings.Contains(body, `<aside id="admin-sidebar"`) {
		t.Error("a panel came with the whole page")
	}

	// Recently edited: newest first, with the author's name and the time in
	// the site's timezone.
	news := strings.Index(body, `<a href="/admin/news/1">Latest news</a>`)
	docs := strings.Index(body, `<a href="/admin/docs/1">Latest docs</a>`)

	if news < 0 || docs < news {
		t.Fatal("recent items missing or not newest first")
	}

	for _, part := range []string{
		"News · by " + owner.DisplayName(),
		`datetime="2026-01-02T11:00:00&#43;01:00">2 Jan 2026 11:00</time>`,
		`<span class="badge">Draft</span>`,
	} {
		if !strings.Contains(body, part) {
			t.Errorf("recent items lack %q", part)
		}
	}

	// Needs attention lists sections with a count.
	if !strings.Contains(body, `<a class="dashboard-row" href="/admin/news">`) ||
		!strings.Contains(body, `<span class="nav-badge">3</span>`) {
		t.Error("the news badge is not under Needs attention")
	}

	if strings.Contains(body, `<a class="dashboard-row" href="/admin/docs">`) {
		t.Error("a section without count needs attention")
	}

	// Scheduled lists both planned changes, soonest first.
	online := strings.Index(body, "Docs · Goes online")
	offline := strings.Index(body, "Docs · Goes offline")

	if online < 0 || offline < online || !strings.Contains(body, "News · Goes online") {
		t.Error("scheduled changes missing or out of order")
	}

	// Quick actions only where the user may create; they show with the page.
	if !strings.Contains(page, `href="/admin/news/new"`) || strings.Contains(page, `href="/admin/docs/new"`) {
		t.Error("quick actions do not follow the create permission")
	}

	if !strings.Contains(page, `href="/" target="_blank"`) {
		t.Error("no link to the website")
	}

	// Nothing of a section the user may not read.
	for _, part := range []string{"secret", "Secret"} {
		if strings.Contains(page+body, part) {
			t.Errorf("the dashboard shows %q", part)
		}
	}
}

func TestDashboardEmpty(t *testing.T) {
	t.Parallel()

	b := start(t, admin.Options{Language: "nl"})
	b.signIn()

	_, page := b.get("/admin/")
	body := page + b.panel("dashboard-recent") + b.panel("dashboard-attention") + b.panel("dashboard-scheduled")

	for _, part := range []string{
		"Niets vraagt om je aandacht.",
		"Er is nog niets bewerkt.",
		"Er is niets gepland.",
		"Website bekijken",
	} {
		if !strings.Contains(body, part) {
			t.Errorf("empty dashboard lacks %q", part)
		}
	}

	if strings.Contains(body, `class="action-menu"`) || strings.Contains(body, "/new") {
		t.Error("quick actions without sections")
	}
}

// TestDashboardActionMenu checks that many quick actions keep the buttons of
// the sections at the top and group the others in a menu, and that times of
// today read as such.
func TestDashboardActionMenu(t *testing.T) {
	t.Parallel()

	now := time.Now()
	sections := []admin.Section{contentSection("pages", "Pages", "", now, "", 0)}

	for i := range 5 {
		sections = append(
			sections,
			contentSection(fmt.Sprintf("list%d", i), fmt.Sprintf("List %d", i), "Content", now.Add(-time.Hour), "", 0),
		)
	}

	b := start(t, admin.Options{Language: "en"}, sections...)
	b.signIn()

	_, body := b.get("/admin/")
	recent := b.panel("dashboard-recent")

	menu := strings.Index(body, `<details class="action-menu">`)
	if menu < 0 {
		t.Fatal("no action menu")
	}

	if button := strings.Index(body, `href="/admin/pages/new"`); button < 0 || button > menu {
		t.Error("the action of a section at the top is not a button")
	}

	if item := strings.Index(
		body,
		`<a href="/admin/list3/new"><span>New list3</span><span class="muted">List 3</span></a>`,
	); item < menu {
		t.Error("the action of a grouped section is not in the menu")
	}

	if !strings.Contains(body, `<p class="sidebar-label">Content</p>`) {
		t.Error("the menu lacks its group heading")
	}

	if !strings.Contains(recent, ">Today "+now.UTC().Format("15:04")+"</time>") {
		t.Error("a change of today does not say so")
	}
}

// panel loads a dashboard panel as its placeholder does: an HTMX request
// for its element.
func (b browser) panel(id string) string {
	b.t.Helper()

	request, err := http.NewRequest(http.MethodGet, b.server.URL+"/admin/", nil)
	if err != nil {
		b.t.Fatal(err)
	}

	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Target", "section#"+id)

	response, err := b.client.Do(request)
	if err != nil {
		b.t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)

	if response.StatusCode != http.StatusOK {
		b.t.Fatalf("panel %s: %d\n%s", id, response.StatusCode, body)
	}

	if !strings.HasPrefix(strings.TrimSpace(string(body)), `<section id="`+id+`"`) {
		b.t.Fatalf("panel %s is not the panel alone:\n%s", id, body)
	}

	return string(body)
}

// TestDashboardSlowSections checks that a section that hangs, fails or
// panics delays only its own panel, for no longer than the panel's
// timeout, and is named there, while the other sections' items show.
func TestDashboardSlowSections(t *testing.T) {
	admin.SetPanelTimeout(t, 100*time.Millisecond)

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	now := time.Now()

	hanging := contentSection("slow", "Slow", "", now, "", 0)
	hanging.Recent = func(context.Context, int) ([]admin.RecentItem, error) {
		// A section that ignores its context.
		<-release

		return nil, nil
	}
	hanging.Badge = func(ctx context.Context) int {
		<-ctx.Done()

		return 9
	}

	failing := contentSection("broken", "Broken", "", now, "", 0)
	failing.Scheduled = func(context.Context, int) ([]admin.ScheduledItem, error) {
		return nil, errors.New("database down")
	}

	panicking := contentSection("wild", "Wild", "", now, "", 0)
	panicking.Recent = func(context.Context, int) ([]admin.RecentItem, error) {
		panic("module bug")
	}

	b := start(t, admin.Options{Language: "en"},
		contentSection("news", "News", "", now, "", 2),
		hanging, failing, panicking,
	)
	b.signIn()

	started := time.Now()
	recent := b.panel("dashboard-recent")

	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("the panel waited %s for a hanging section", elapsed)
	}

	if !strings.Contains(recent, "Latest news") || !strings.Contains(recent, "Latest broken") {
		t.Error("the sections that answered are missing")
	}

	if !strings.Contains(recent, "Could not load: Slow, Wild Reload to try again.") {
		t.Errorf("the panel does not name the sections that failed and what to do:\n%s", recent)
	}

	attention := b.panel("dashboard-attention")
	if !strings.Contains(attention, "Could not load: Slow") || !strings.Contains(attention, `<span class="nav-badge">2</span>`) {
		t.Errorf("attention panel:\n%s", attention)
	}

	scheduled := b.panel("dashboard-scheduled")
	if !strings.Contains(scheduled, "Could not load: Broken") || !strings.Contains(scheduled, "Planned news") {
		t.Errorf("scheduled panel:\n%s", scheduled)
	}

	// The page itself asks no section.
	started = time.Now()

	if status, _ := b.get("/admin/"); status != http.StatusOK || time.Since(started) > time.Second {
		t.Fatalf("the dashboard page took %s", time.Since(started))
	}
}

// TestDashboardReviews checks that revisions waiting for review are listed
// under Needs attention, oldest first and with who asked, for users who may
// review in the section only, and that sections are asked for no more than
// the dashboard shows.
func TestDashboardReviews(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	accounts := newAccounts(t)

	users, err := accounts.Users(ctx)
	if err != nil {
		t.Fatal(err)
	}

	owner := users[0]

	_, err = accounts.CreateUser(ctx, "editor@example.test", "Eddie Editor", "test-only long passphrase", auth.Editor)
	if err != nil {
		t.Fatal(err)
	}

	submitted := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

	var limits []int

	news := contentSection("news", "News", "", submitted, "", 0)
	news.Review = func(_ context.Context, limit int) ([]admin.ReviewItem, error) {
		limits = append(limits, limit)

		return []admin.ReviewItem{
			{Title: "Newer request", URL: "/admin/news/2", SubmittedBy: owner.ID, SubmittedAt: submitted.Add(time.Hour)},
			{Title: "Older request", URL: "/admin/news/1", SubmittedBy: owner.ID, SubmittedAt: submitted},
		}, nil
	}

	// Editors may read and edit news, but not review it.
	authorize := func(_ context.Context, user auth.User, permission admin.Permission) bool {
		return user.IsAdmin() || permission.Action != admin.Review
	}

	b := startWith(t, accounts, admin.Options{Language: "en", Authorize: authorize}, news)
	b.signIn()

	body := b.panel("dashboard-attention")

	older := strings.Index(body, `<a href="/admin/news/1">Older request</a>`)
	newer := strings.Index(body, `<a href="/admin/news/2">Newer request</a>`)

	if older < 0 || newer < older {
		t.Fatalf("reviews missing or not oldest first:\n%s", body)
	}

	if !strings.Contains(body, "News · Waiting for review · by "+owner.DisplayName()) {
		t.Errorf("the review lacks its section or who asked:\n%s", body)
	}

	if len(limits) != 1 || limits[0] != 8 {
		t.Errorf("review limits = %v", limits)
	}

	editor := startWith(t, accounts, admin.Options{Language: "en", Authorize: authorize}, news)
	editor.signInAs("editor@example.test", "test-only long passphrase")

	if body := editor.panel("dashboard-attention"); strings.Contains(body, "request") {
		t.Errorf("a user who may not review sees reviews:\n%s", body)
	}
}
