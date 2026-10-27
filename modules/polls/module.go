package polls

import (
	"context"
	"embed"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router/middleware"
)

//go:embed templates translations
var files embed.FS

// Module adds a Polls instance. key is used in URLs of the admin and in the
// template functions and must be unique on the site; title is shown in the
// admin and the page editor.
func Module(key, title string) webthings.Module {
	return webthings.Module{
		Type:   "polls",
		Key:    key,
		Title:  title,
		Schema: schema(),
		Setup:  setup,
		Funcs:  funcs(),
		Templates: []frontend.Template{
			{Name: "list", Data: ListView{}, Example: listExample},
		},
		Components: components(),
	}
}

func setup(env webthings.Env) (webthings.Parts, error) {
	itemsSpec := spec()
	itemsSpec.Changed = env.Changed

	items, err := content.NewCollection(env.DB, itemsSpec)
	if err != nil {
		return webthings.Parts{}, err
	}

	service := &Service{
		key:      env.Key,
		db:       env.DB,
		items:    items,
		frontend: env.Frontend,
		pages:    env.Pages,
		site:     env.Site,
		// Visitors without the cookie are known by their address.
		visitorID: env.VisitorID,
		actions:   env.Actions(),
		changed:   env.Changed,
	}

	service.registerActions()

	// The cookie marks the polls the visitor voted on, on every page.
	env.PersonalCookie(cookieName)

	return webthings.Parts{
		Admin:   []admin.Section{section(env, service)},
		Public:  service.handler(),
		Service: service,
	}, nil
}

type pollInput struct {
	Question    string     `form:"input,text"           label:"Question"                   required:"true" maxLength:"500"   trim:"true"`
	Slug        string     `form:"input,text"           label:"Short name"                                 maxLength:"190"               pattern:"^(?:[a-z0-9]+(?:-[a-z0-9]+)*)?$" patternMsg:"Use lowercase letters, digits and dashes for the short name." description:"Optional: a name for templates, such as favourite-colour."`
	Options     string     `form:"textarea"             label:"Options"                    required:"true" maxLength:"10000"                                                                                                                                 description:"One option per line, at least two. Every language lists the options in the same order; add new options at the end." rows:"6"`
	ClosesAt    *time.Time `form:"input,datetime-local" label:"Closes at"                                                                                                                                                                                    description:"Optional, in the site's timezone. Leave empty to keep the poll open."`
	ShowResults bool       `form:"checkbox"             label:"Show results before voting"`
}

func section(env webthings.Env, service *Service) admin.Section {
	return editor.New(editor.Config[Fields, Text, pollInput]{
		ID:       env.Key,
		Title:    env.Title,
		Icon:     "check",
		NewLabel: "New poll",
		Links:    []editor.Link{{Label: "Results", Route: admin.RouteName(env.Key, "results")}},
		Store:    service.Store(),
		Locales:  env.Locales,
		Files:    files,
		Label: func(text Text) string {
			return text.Question
		},
		Notes: service.notes,
		Form: func(_ context.Context, item Item, locale string) (pollInput, error) {
			return pollForm(item, locale), nil
		},
		Apply: func(ctx context.Context, in pollInput, draft *content.Draft[Fields, Text], locale string) error {
			return applyPoll(ctx, service, in, draft, locale)
		},
		Issue: issue,
		Routes: func(routes *admin.Routes, edit editor.Editor[Fields, Text, pollInput]) {
			screens := results{service: service, admin: routes.Admin(), editor: edit, routes: routes, title: env.Title}

			routes.Get("/results", admin.View, screens.list).As("results")
			routes.Post("/{id}/reset", admin.Delete, screens.reset).As("reset")
		},
	})
}

func pollForm(item Item, locale string) pollInput {
	text, _ := item.Text(locale)
	input := pollInput{
		Question:    text.Question,
		Slug:        text.Slug,
		ClosesAt:    item.Fields.ClosesAt,
		ShowResults: item.Fields.ShowResults,
	}

	// Options not translated yet show the label of another language.
	labels := make([]string, 0, len(item.Fields.Options))
	for i := range item.Fields.Options {
		labels = append(labels, label(item, locale, i))
	}

	input.Options = strings.Join(labels, "\n")

	return input
}

// applyPoll writes one language of the editor into the draft. Options are
// matched by line: extra lines add options, missing lines remove the last
// options, which is refused while they have votes.
func applyPoll(ctx context.Context, service *Service, in pollInput, draft *content.Draft[Fields, Text], locale string) error {
	labels, err := optionLines(in.Options)
	if err != nil {
		return err
	}

	ids := slices.Clone(draft.Fields.Options)
	for len(ids) < len(labels) {
		ids = append(ids, db.NewID())
	}

	if len(labels) < len(ids) {
		votes, err := service.votesFor(ctx, draft.ID, ids[len(labels):])
		if err != nil {
			return err
		}

		if votes > 0 {
			return errOptionVoted
		}

		ids = ids[:len(labels)]
	}

	// Editors enter the closing time in the site's timezone; it is stored
	// in UTC.
	var closesAt *time.Time

	if in.ClosesAt != nil {
		closing := in.ClosesAt.UTC()
		closesAt = &closing
	}

	translations := make([]content.Translation[Text], 0, len(draft.Translations))

	for _, translation := range draft.Translations {
		if len(translation.Text.Options) > len(ids) {
			translation.Text.Options = translation.Text.Options[:len(ids)]
		}

		translations = append(translations, translation)
	}

	draft.Fields = Fields{Options: ids, ClosesAt: closesAt, ShowResults: in.ShowResults}
	draft.Translations = content.SetText(translations, locale, Text{
		Question: in.Question,
		Slug:     in.Slug,
		Options:  labels,
	})

	return nil
}

// optionLines reads the options textarea: one label per non-empty line.
func optionLines(source string) (Lines, error) {
	var labels Lines

	for line := range strings.Lines(source) {
		line = strings.TrimSpace(line)
		if line != "" {
			labels = append(labels, line)
		}
	}

	switch {
	case len(labels) < 2:
		return nil, errOptions
	case len(labels) > maxOptions:
		return nil, errTooMany
	}

	return labels, nil
}

func issue(err error) string {
	switch {
	case errors.Is(err, errOptions):
		return "A poll needs at least two options."
	case errors.Is(err, errTooMany):
		return "A poll can have at most 20 options."
	case errors.Is(err, errOptionVoted):
		return "Options with votes cannot be removed. Reset the votes first."
	case errors.Is(err, errOptionID):
		return "Option IDs must be unique, not empty and at most 36 characters."
	}

	return ""
}

// results shows the votes of every poll.
type results struct {
	service *Service
	admin   *admin.Admin
	editor  editor.Editor[Fields, Text, pollInput]
	routes  *admin.Routes
	title   string
}

// ResultsView is the data of templates/admin/results.gohtml: the votes of
// every poll.
type ResultsView struct {
	Title   string
	BackURL string
	Polls   []ResultRow
}

// ResultRow is one poll with its options and their votes.
type ResultRow struct {
	Question string
	EditURL  string
	// Status is "Draft", "Open" or "Closed".
	Status  string
	Options []Option
	Total   int
	// Reset removes the votes; nil without the Delete permission.
	Reset *form.RenderModel
}

func (s results) list(w http.ResponseWriter, r *http.Request) {
	locale, _, err := s.editor.Locale(r)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	items, err := s.service.items.List(r.Context(), s.service.key)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	polls, err := s.service.polls(r.Context(), nil, items, locale)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	query := url.Values{"locale": {locale}}
	canReset := s.admin.Can(r, admin.Permission{Section: s.service.key, Action: admin.Delete})
	view := ResultsView{Title: s.title, BackURL: s.routes.URL("index", query)}
	search := admin.SearchOf(r)

	for i, poll := range polls {
		row := ResultRow{
			Question: poll.Question,
			EditURL:  s.routes.URL("edit", "id", poll.ID, query),
			Status:   "Open",
			Options:  poll.Options,
			Total:    poll.Total,
		}

		switch {
		case !items[i].Published:
			row.Status = "Draft"
		case poll.Closed:
			row.Status = "Closed"
		}

		if row.Question == "" {
			row.Question = s.admin.T(r, "Untitled")
		}

		if !search.Matches(row.Question) {
			continue
		}

		if canReset && poll.Total > 0 {
			reset := s.admin.Form(r, struct{}{}, s.routes.URL("reset", "id", poll.ID, query), "Reset votes")
			reset = s.admin.Confirm(r, reset, "Reset the votes of this poll? This cannot be undone.")
			row.Reset = &reset
		}

		view.Polls = append(view.Polls, row)
	}

	s.admin.Render(w, r, admin.Page{
		Title:    "Results",
		Template: "templates/admin/results.gohtml",
		Files:    files,
		Data:     view,
	})
}

func (s results) reset(w http.ResponseWriter, r *http.Request) {
	if err := s.service.ResetVotes(r.Context(), r.PathValue("id")); err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	query := url.Values{}
	if locale := r.URL.Query().Get("locale"); locale != "" {
		query.Set("locale", locale)
	}

	target := s.routes.URL("results", query)

	s.admin.RedirectNotice(w, r, target, "The votes were reset.")
}

// handler lists the published polls with their results on the page the
// module is mounted on.
func (s *Service) handler() http.Handler {
	// Paths without a route get the site's 404 page.
	routes := s.frontend.Router()

	// Results change with every vote and depend on the visitor.
	routes.Get("/{$}", s.listPage).As("list").Use(middleware.NoStore)

	return routes
}

func (s *Service) listPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mount := frontend.MountFrom(ctx)
	view := ListView{Key: s.key, Title: mount.Title}

	var err error

	view.Polls, err = s.Latest(ctx, r, mount.Locale, 0)
	if err != nil {
		s.frontend.Fail(w, r, err)

		return
	}

	s.frontend.Render(w, r, frontend.Content{
		Template: "list",
		Title:    view.Title,
		Data:     view,
	})
}

// notes shows in the overview how many votes each poll has and whether it is
// closed; the votes of all polls are read in one query. When that query
// fails, the overview still shows which polls are closed.
func (s *Service) notes(ctx context.Context, items []Item, _ string) map[string]string {
	ids := make([]string, 0, len(items))

	for _, item := range items {
		ids = append(ids, item.ID)
	}

	counts, err := s.counts(ctx, ids)
	if err != nil {
		slog.ErrorContext(ctx, "poll votes", "module", s.key, "error", err)
	}

	now := time.Now()
	notes := make(map[string]string, len(items))

	for _, item := range items {
		var parts []string

		if total := votes(item, counts[item.ID]); total > 0 {
			parts = append(parts, admin.TN(ctx, "%d vote", "%d votes", total, total))
		}

		if closed(item, now) {
			parts = append(parts, admin.T(ctx, "Closed"))
		}

		notes[item.ID] = strings.Join(parts, " · ")
	}

	return notes
}

// votes is the number of votes for the poll's current options, the total the
// website shows.
func votes(item Item, counts map[string]int) int {
	total := 0

	for _, id := range item.Fields.Options {
		total += counts[id]
	}

	return total
}

// listExample is the starting point for polls/list.gohtml: every published
// poll with the box component, which a theme can replace as well.
const listExample = `{{/* @dot github.com/donseba/go-webthings/modules/polls.ListView */}}
{{/* Starting point generated by WebThings: every published poll, each with the box component. */}}
<main id="content">
    <h1>{{.Title}}</h1>

    {{range .Polls}}
        {{pollBox $.Key .ID}}
    {{else}}
        <p>No polls yet.</p>
    {{end}}
</main>
`
