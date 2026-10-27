package admin

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/donseba/go-webthings/render"

	partial "github.com/donseba/go-partial"
)

// The dashboard shows at once, with its quick actions, and its panels load
// after it, each on its own request: a panel is a deferred fragment of the
// page (render.Fragment.Deferred) whose placeholder HTMX loads as soon as it
// shows.
// Every panel asks all sections at the same time, with capped queries, and
// waits for them no longer than panelTimeout, so a slow section never holds
// up the page or the other panels.

// RecentItem is an item a section changed lately, for the dashboard's
// "Recently edited" (see Section.Recent).
type RecentItem struct {
	Title string
	// URL is the item's editor, built from its route name.
	URL       string
	UpdatedAt time.Time
	// Status is a translation key such as "Draft" or "Published"; empty
	// shows none.
	Status string
	// Author is the ID of the user who made the change; empty when unknown.
	Author string
}

// ScheduledItem is an item whose publication is planned, for the
// dashboard's "Scheduled" (see Section.Scheduled).
type ScheduledItem struct {
	Title string
	URL   string
	// PublishAt puts the item online, UnpublishAt takes it offline; zero
	// when not planned.
	PublishAt   time.Time
	UnpublishAt time.Time
}

// DashboardView is the dashboard's data, limited to what the user may see.
type DashboardView struct {
	// Actions are the quick actions shown as buttons; More holds the rest,
	// by navigation group, when there are many.
	Actions []QuickAction
	More    []ActionGroup
	// WebsiteURL is the root of the website, which leads to the home page in
	// its default language.
	WebsiteURL string
	// Callouts are the sections' cards above the panels (see
	// Section.Callout).
	Callouts []Callout
}

// PanelState is a dashboard panel's heading and what went wrong for it.
type PanelState struct {
	Heading PanelHeading
	// Failed names the sections that failed or did not answer in time.
	Failed []string
}

// PanelHeading is the heading of a dashboard panel, which its placeholder
// shows too while the panel loads.
type PanelHeading struct {
	// ID is the panel's element ID; the heading's is ID + "-title".
	ID string
	// Icon names the heading's icon in icons.svg.
	Icon string
	// Title is a translation key.
	Title string
}

// The headings of the dashboard's panels.
var (
	recentHeading    = PanelHeading{ID: recentPanel, Icon: "write", Title: "Recently edited"}
	attentionHeading = PanelHeading{ID: attentionPanel, Icon: "inbox", Title: "Needs attention"}
	scheduledHeading = PanelHeading{ID: scheduledPanel, Icon: "clock", Title: "Scheduled"}
)

// FailedList names the sections that could not be loaded, for the panel.
func (p PanelState) FailedList() string {
	return strings.Join(p.Failed, ", ")
}

// RecentPanel is "Recently edited": the latest changes of all sections.
type RecentPanel struct {
	PanelState

	Items []RecentEntry
}

// ScheduledPanel is "Scheduled": the planned publication changes.
type ScheduledPanel struct {
	PanelState

	Items []ScheduledEntry
}

// AttentionPanel is "Needs attention": revisions waiting for the user's
// review, and the sections with a badge count, such as new comments.
type AttentionPanel struct {
	PanelState

	Reviews  []ReviewEntry
	Sections []NavigationLink
}

// QuickAction creates an item in a section.
type QuickAction struct {
	Label   string
	Section string
	URL     string
}

// ActionGroup are the quick actions of one navigation group.
type ActionGroup struct {
	Name    string
	Actions []QuickAction
}

// RecentEntry is a recently changed item, with its section and the times
// in the site's timezone.
type RecentEntry struct {
	Title   string
	URL     string
	Section string
	Icon    string
	Status  string
	// Author is the name of who made the change; empty when unknown.
	Author string
	// When is the time as shown, such as "Today 14:05"; Time is the same as
	// RFC 3339, for the datetime attribute.
	When string
	Time string

	at     time.Time
	author string
}

// ScheduledEntry is one planned publication change.
type ScheduledEntry struct {
	Title   string
	URL     string
	Section string
	Icon    string
	// Change is "Goes online" or "Goes offline", translated.
	Change string
	// Offline is set for an unpublication.
	Offline bool
	When    string
	Time    string
	at      time.Time
}

// ReviewEntry is a revision waiting for review (see Section.Review).
type ReviewEntry struct {
	Title   string
	URL     string
	Section string
	Icon    string
	// Author is the name of who asked for the review; empty when unknown.
	Author string
	// When is since when it waits, as shown; Time is the same as RFC 3339.
	When string
	Time string

	at     time.Time
	author string
}

const (
	// recentLimit, scheduledLimit and reviewLimit keep the dashboard short;
	// sections are asked for no more.
	recentLimit    = 8
	scheduledLimit = 8
	reviewLimit    = 8
	// directActions are shown as buttons; with more, only the actions of the
	// sections at the top of the navigation are, and the others go in a menu.
	directActions = 4
	// The panels' element IDs, by which they are loaded, and the
	// placeholder they show until then.
	recentPanel      = "dashboard-recent"
	attentionPanel   = "dashboard-attention"
	scheduledPanel   = "dashboard-scheduled"
	panelPlaceholder = "templates/dashboard-placeholder.gohtml"
)

// panelTimeout is how long a panel waits for its sections, badgeTimeout
// how long a page waits for the navigation's badges.
var (
	panelTimeout = 5 * time.Second
	badgeTimeout = time.Second
)

// dashboard shows the dashboard: its panels load after the page, each as a
// deferred fragment of its own, whose data is computed only when it renders,
// as is the page's: a request for a panel counts neither the navigation nor
// the other panels.
func (a *Admin) dashboard(w http.ResponseWriter, r *http.Request) {
	a.Render(w, r, Page{
		Title:    "Dashboard",
		Template: "templates/dashboard.gohtml",
		Includes: []string{"templates/dashboard-panels.gohtml"},
		Dot: partial.DotFuncOf(func(context.Context) (DashboardView, error) {
			view := DashboardView{WebsiteURL: "/"}

			// The quick actions need the sections, not their badges, which
			// the shell counts.
			groups := a.navigation(r, false)
			view.Actions, view.More = a.quickActions(r, groups)
			view.Callouts = a.callouts(r)

			return view, nil
		}),
		Fragments: []render.Fragment{
			{
				ID:              recentPanel,
				Template:        "templates/dashboard-recent.gohtml",
				Placeholder:     panelPlaceholder,
				PlaceholderData: recentHeading,
				Dot: partial.DotFuncOf(func(context.Context) (RecentPanel, error) {
					return a.recentPanel(r), nil
				}),
			},
			{
				ID:              attentionPanel,
				Template:        "templates/dashboard-attention.gohtml",
				Placeholder:     panelPlaceholder,
				PlaceholderData: attentionHeading,
				Dot: partial.DotFuncOf(func(context.Context) (AttentionPanel, error) {
					return a.attentionPanel(r), nil
				}),
			},
			{
				ID:              scheduledPanel,
				Template:        "templates/dashboard-scheduled.gohtml",
				Placeholder:     panelPlaceholder,
				PlaceholderData: scheduledHeading,
				Dot: partial.DotFuncOf(func(context.Context) (ScheduledPanel, error) {
					return a.scheduledPanel(r), nil
				}),
			},
		},
	})
}

// dashboardLink is a section shown on the dashboard, with its navigation
// title and icon.
type dashboardLink struct {
	section *Section
	title   string
	icon    string
}

// dashboardLinks are the sections in the navigation the user may view: the
// dashboard shows nothing of the others.
func (a *Admin) dashboardLinks(r *http.Request, has func(Section) bool) []dashboardLink {
	var links []dashboardLink

	for i := range a.sections {
		section := &a.sections[i]

		if section.Hidden || !has(*section) || !a.mayView(r, *section) {
			continue
		}

		icon := section.Icon
		if icon == "" {
			icon = "box"
		}

		links = append(links, dashboardLink{section: section, title: a.T(r, section.Title), icon: icon})
	}

	return links
}

// answer is what one section gave a panel.
type answer[T any] struct {
	link  dashboardLink
	value T
	err   error
	done  bool
}

// askSections asks every section at the same time and waits until all
// answered or timeout passed. It returns the answers in the order of
// links, and the titles of the sections that failed or are still busy; a
// section that keeps working after the timeout is left to finish on its own.
func askSections[T any](
	r *http.Request,
	links []dashboardLink,
	timeout time.Duration,
	ask func(ctx context.Context, link dashboardLink) (T, error),
) ([]answer[T], []string) {
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	type indexed struct {
		index  int
		answer answer[T]
	}

	answers := make(chan indexed, len(links))

	for i, link := range links {
		go func() {
			// A failing module must not take the server down.
			defer func() {
				if recovered := recover(); recovered != nil {
					answers <- indexed{index: i, answer: answer[T]{link: link, err: fmt.Errorf("panic: %v", recovered)}}
				}
			}()

			value, err := ask(ctx, link)
			answers <- indexed{index: i, answer: answer[T]{link: link, value: value, err: err}}
		}()
	}

	results := make([]answer[T], len(links))

	for i, link := range links {
		results[i].link = link
	}

collect:
	for range links {
		select {
		case got := <-answers:
			got.answer.done = true
			results[got.index] = got.answer
		case <-ctx.Done():
			break collect
		}
	}

	var failed []string

	for i, result := range results {
		switch {
		case !result.done:
			slog.WarnContext(r.Context(), "admin section did not answer in time", "section", links[i].section.ID)
			failed = append(failed, links[i].title)
		case result.err != nil:
			slog.ErrorContext(r.Context(), "admin section", "section", links[i].section.ID, "error", result.err)
			failed = append(failed, links[i].title)
		}
	}

	return results, failed
}

// recentPanel lists the latest changes of all sections.
func (a *Admin) recentPanel(r *http.Request) RecentPanel {
	links := a.dashboardLinks(r, func(section Section) bool {
		return section.Recent != nil
	})

	ask := func(ctx context.Context, link dashboardLink) ([]RecentItem, error) {
		return link.section.Recent(ctx, recentLimit)
	}

	answers, failed := askSections(r, links, panelTimeout, ask)

	var recent []RecentEntry

	for _, answer := range answers {
		for _, item := range answer.value {
			recent = append(recent, a.recentEntry(r, answer.link, item))
		}
	}

	return RecentPanel{
		PanelState: PanelState{Heading: recentHeading, Failed: failed},
		Items:      a.latest(r, recent),
	}
}

// scheduledPanel lists the soonest planned publication changes.
func (a *Admin) scheduledPanel(r *http.Request) ScheduledPanel {
	links := a.dashboardLinks(r, func(section Section) bool {
		return section.Scheduled != nil
	})

	ask := func(ctx context.Context, link dashboardLink) ([]ScheduledItem, error) {
		return link.section.Scheduled(ctx, scheduledLimit)
	}

	answers, failed := askSections(r, links, panelTimeout, ask)

	var scheduled []ScheduledEntry

	for _, answer := range answers {
		for _, item := range answer.value {
			scheduled = append(scheduled, a.scheduledEntries(r, answer.link, item)...)
		}
	}

	slices.SortStableFunc(scheduled, func(x, y ScheduledEntry) int {
		return x.at.Compare(y.at)
	})

	return ScheduledPanel{
		PanelState: PanelState{Heading: scheduledHeading, Failed: failed},
		Items:      scheduled[:min(len(scheduled), scheduledLimit)],
	}
}

// attention is what a section needs the user's attention for.
type attention struct {
	badge   int
	reviews []ReviewItem
}

// attentionPanel lists the revisions waiting for the user's review, oldest
// first, and the sections with a badge count.
func (a *Admin) attentionPanel(r *http.Request) AttentionPanel {
	links := a.dashboardLinks(r, func(section Section) bool {
		return section.Badge != nil || section.Review != nil
	})

	// Permissions are checked here, once per section, before the sections
	// are asked at the same time. Authorize must be safe for concurrent use
	// all the same (see Options.Authorize).
	reviewer := map[*Section]bool{}

	for _, link := range links {
		if link.section.Review != nil {
			reviewer[link.section] = a.Can(r, Permission{Section: link.section.PermissionID(), Action: Review})
		}
	}

	ask := func(ctx context.Context, link dashboardLink) (attention, error) {
		var found attention

		if link.section.Badge != nil {
			found.badge = max(link.section.Badge(ctx), 0)
		}

		if reviewer[link.section] {
			reviews, err := link.section.Review(ctx, reviewLimit)
			if err != nil {
				return found, err
			}

			found.reviews = reviews
		}

		return found, nil
	}

	answers, failed := askSections(r, links, panelTimeout, ask)

	panel := AttentionPanel{PanelState: PanelState{Heading: attentionHeading, Failed: failed}}

	for _, answer := range answers {
		if answer.value.badge > 0 {
			panel.Sections = append(panel.Sections, NavigationLink{
				Title: answer.link.title,
				Path:  a.sectionURL(*answer.link.section),
				Icon:  answer.link.icon,
				Badge: answer.value.badge,
			})
		}

		for _, item := range answer.value.reviews {
			panel.Reviews = append(panel.Reviews, a.reviewEntry(r, answer.link, item))
		}
	}

	order := a.collator()

	slices.SortStableFunc(panel.Sections, func(x, y NavigationLink) int {
		return order.CompareString(x.Title, y.Title)
	})

	panel.Reviews = a.oldestReviews(r, panel.Reviews)

	return panel
}

// badges are the counts of the navigation's sections, such as new
// comments, by section. The sections are asked at the same time; one that
// takes longer than badgeTimeout shows no count, so it cannot hold up every
// page of the admin.
func (a *Admin) badges(r *http.Request) map[*Section]int {
	links := a.dashboardLinks(r, func(section Section) bool {
		return section.Badge != nil
	})

	ask := func(ctx context.Context, link dashboardLink) (int, error) {
		return max(link.section.Badge(ctx), 0), nil
	}

	answers, _ := askSections(r, links, badgeTimeout, ask)

	counts := make(map[*Section]int, len(answers))

	for _, answer := range answers {
		if answer.done {
			counts[answer.link.section] = answer.value
		}
	}

	return counts
}

// latest keeps the most recently changed entries and names their authors.
func (a *Admin) latest(r *http.Request, recent []RecentEntry) []RecentEntry {
	slices.SortStableFunc(recent, func(x, y RecentEntry) int {
		return y.at.Compare(x.at)
	})

	recent = recent[:min(len(recent), recentLimit)]

	names := a.authorNames(r, len(recent), func(i int) string {
		return recent[i].author
	})

	for i := range recent {
		recent[i].Author = names[recent[i].author]
	}

	return recent
}

// oldestReviews keeps the reviews that wait longest and names who asked.
func (a *Admin) oldestReviews(r *http.Request, reviews []ReviewEntry) []ReviewEntry {
	slices.SortStableFunc(reviews, func(x, y ReviewEntry) int {
		return x.at.Compare(y.at)
	})

	reviews = reviews[:min(len(reviews), reviewLimit)]

	names := a.authorNames(r, len(reviews), func(i int) string {
		return reviews[i].author
	})

	for i := range reviews {
		reviews[i].Author = names[reviews[i].author]
	}

	return reviews
}

// authorNames are the names of the users author(i) for i below count, by
// ID; none are read when no entry has an author.
func (a *Admin) authorNames(r *http.Request, count int, author func(i int) string) map[string]string {
	needed := false

	for i := range count {
		if author(i) != "" {
			needed = true

			break
		}
	}

	if !needed {
		return nil
	}

	names, err := a.auth.Names(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "dashboard author names", "error", err)

		return nil
	}

	return names
}

func (a *Admin) recentEntry(r *http.Request, link dashboardLink, item RecentItem) RecentEntry {
	entry := RecentEntry{
		Title:   item.Title,
		URL:     item.URL,
		Section: link.title,
		Icon:    link.icon,
		at:      item.UpdatedAt,
		author:  item.Author,
	}

	if entry.Title == "" {
		entry.Title = a.T(r, "Untitled")
	}

	if item.Status != "" {
		entry.Status = a.T(r, item.Status)
	}

	entry.When, entry.Time = a.when(r, item.UpdatedAt)

	return entry
}

func (a *Admin) reviewEntry(r *http.Request, link dashboardLink, item ReviewItem) ReviewEntry {
	entry := ReviewEntry{
		Title:   item.Title,
		URL:     item.URL,
		Section: link.title,
		Icon:    link.icon,
		at:      item.SubmittedAt,
		author:  item.SubmittedBy,
	}

	if entry.Title == "" {
		entry.Title = a.T(r, "Untitled")
	}

	entry.When, entry.Time = a.when(r, item.SubmittedAt)

	return entry
}

// scheduledEntries are the planned changes of an item: going online,
// offline, or both.
func (a *Admin) scheduledEntries(r *http.Request, link dashboardLink, item ScheduledItem) []ScheduledEntry {
	var entries []ScheduledEntry

	title := item.Title
	if title == "" {
		title = a.T(r, "Untitled")
	}

	changes := []struct {
		at      time.Time
		change  string
		offline bool
	}{
		{item.PublishAt, "Goes online", false},
		{item.UnpublishAt, "Goes offline", true},
	}

	for _, change := range changes {
		if change.at.IsZero() {
			continue
		}

		entry := ScheduledEntry{
			Title:   title,
			URL:     item.URL,
			Section: link.title,
			Icon:    link.icon,
			Change:  a.T(r, change.change),
			Offline: change.offline,
			at:      change.at,
		}
		entry.When, entry.Time = a.when(r, change.at)
		entries = append(entries, entry)
	}

	return entries
}

// newURL is the address of a section's quick action: its NewRoute, or
// else its NewPath below the admin; "" without either, or for a NewRoute
// the section did not register.
func (a *Admin) newURL(section Section) string {
	if section.NewRoute == "" {
		if section.NewPath == "" {
			return ""
		}

		return a.base + section.NewPath
	}

	address, err := a.TryURL(section.RouteName(section.NewRoute))
	if err != nil {
		return ""
	}

	return address
}

// callouts are the cards of the sections the user may view (see
// Section.Callout), in the order of the sections.
func (a *Admin) callouts(r *http.Request) []Callout {
	var cards []Callout

	for _, section := range a.sections {
		if section.Callout == nil || !a.mayView(r, section) {
			continue
		}

		card, ok := section.Callout(r.Context())
		if ok {
			cards = append(cards, card)
		}
	}

	return cards
}

// QuickActions are the "new item" actions of the sections the user may
// create in, such as "New post", in navigation order, as the dashboard
// offers them; for screens that suggest what to write first, such as the
// setup guide.
func (a *Admin) QuickActions(r *http.Request) []QuickAction {
	buttons, menu := a.quickActions(r, a.navigation(r, false))

	for _, group := range menu {
		buttons = append(buttons, group.Actions...)
	}

	return buttons
}

// quickActions are the "new item" actions of the sections the user may
// create in, in navigation order. A few are all buttons; with more, the
// actions of the sections at the top of the navigation are buttons and the
// others are grouped in a menu. They ask no section anything, so they show
// with the page rather than in a panel.
func (a *Admin) quickActions(r *http.Request, groups []NavigationGroup) ([]QuickAction, []ActionGroup) {
	var (
		buttons []QuickAction
		menu    []ActionGroup
		count   int
	)

	for i, group := range groups {
		var actions []QuickAction

		for _, link := range group.Links {
			section := link.section
			if section == nil || section.NewLabel == "" {
				continue
			}

			address := a.newURL(*section)
			if address == "" {
				continue
			}

			if !a.Can(r, Permission{Section: section.PermissionID(), Action: Create}) {
				continue
			}

			actions = append(actions, QuickAction{
				Label:   a.T(r, section.NewLabel),
				Section: link.Title,
				URL:     address,
			})
		}

		count += len(actions)

		if i == 0 {
			buttons = actions

			continue
		}

		if len(actions) > 0 {
			menu = append(menu, ActionGroup{Name: group.Name, Actions: actions})
		}
	}

	if count > directActions {
		return buttons, menu
	}

	for _, group := range menu {
		buttons = append(buttons, group.Actions...)
	}

	return buttons, nil
}

// when shows a time in the site's timezone and the admin language: "Today
// 14:05", "Yesterday 09:30", "Tomorrow 08:00" or "5 Oct 2026 14:05", and as
// RFC 3339.
func (a *Admin) when(r *http.Request, at time.Time) (shown, machine string) {
	location := a.Location(r)
	local := at.In(location)
	machine = local.Format(time.RFC3339)
	format := Format(r.Context())
	clock := format.Time(at)

	switch dayOffset(time.Now(), at, location) {
	case 0:
		return a.T(r, "Today %s", clock), machine
	case -1:
		return a.T(r, "Yesterday %s", clock), machine
	case 1:
		return a.T(r, "Tomorrow %s", clock), machine
	default:
		return DateTime(r.Context(), at), machine
	}
}

// dayOffset is how many days the date of at comes after that of now in
// location, such as -1 for yesterday. The dates are compared at midnight
// UTC: midnight in location may not exist, such as in America/Santiago on
// the day its clocks go forward.
func dayOffset(now, at time.Time, location *time.Location) int {
	from := now.In(location)
	to := at.In(location)
	today := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	day := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)

	return int(day.Sub(today) / (24 * time.Hour))
}
