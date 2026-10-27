// Package polls asks visitors a question with a few options and shows the
// results. Every visitor has one vote per poll. Add an instance with:
//
//	app.Use(polls.Module("survey", "Polls"))
//
// Editors write polls in the admin: a question, one option per line, an
// optional closing time and whether visitors see the results before they
// vote. Results per poll, and a button to reset them, are under Results.
//
// # Templates
//
// Polls can be shown on any page with template functions; they need no page
// of their own:
//
//	{{poll "survey" "favourite-colour"}}  *Poll by slug or ID, or nil
//	{{polls "survey" 3}}                  []Poll: the first published polls, newest first; 0 for all
//	{{pollURL "survey"}}                  the address votes are posted to
//	{{pollBox "survey" "colour"}}         the box component, rendered (below)
//
// When the module is mounted on a page, that page renders polls/list.gohtml
// (or <key>/list.gohtml) with a ListView: all published polls with their
// results. App.WriteTemplates(dir, key) writes a starting point that shows
// each poll with the box component.
//
// # Component
//
// The box component (see webthings.Module.Components) shows one poll with
// plain, accessible default markup: the vote form, the results with bars
// and the result of the visitor's vote.
//
//	{{pollBox "survey" "favourite-colour"}}   the poll with that slug or ID
//	{{pollBox "survey" ""}}                   the newest poll, for example on the home page
//
// It gets a Poll and renders nothing when there is no such poll. A theme
// replaces it with <key>/box.gohtml, or polls/box.gohtml for every instance;
// App.WriteTemplates(dir, key) writes the default there to start from. The
// default uses the classes wt-poll, wt-poll__question, wt-poll__results,
// wt-poll__result, wt-poll__result--chosen, wt-poll__label,
// wt-poll__percent, wt-poll__bar (a <meter> of the percentage, without a
// style attribute) and wt-poll__total, and the shared wt-notice, wt-button
// and wt-meta, styled by the optional {{asset "webthings.css"}}; go-form renders the options with the site's form theme
// (webthings.Config.FormTheme), by default with wt-form, wt-form__field,
// wt-form__options and wt-form__option. Its texts go through tl, so the
// site's translations translate them.
//
// Or write the markup yourself; a complete poll:
//
//	{{with poll "survey" "favourite-colour"}}
//	  <section id="{{.Anchor}}">
//	    <h2>{{.Question}}</h2>
//	    {{if eq .Result "voted"}}<p>Thanks for voting.</p>{{end}}
//	    {{if .CanVote}}
//	      {{form_open .Form}}
//	        {{form_field .Form "poll" nil}}
//	        {{form_field .Form "option" nil}}
//	        <button>Vote</button>
//	      {{form_close}}
//	    {{end}}
//	    {{if .ShowResults}}
//	      {{range .Options}}<p>{{.Label}}: {{.Percent}}% ({{.Votes}}){{if .Chosen}} ✓{{end}}</p>{{end}}
//	      <p>{{.Total}} votes</p>
//	    {{end}}
//	  </section>
//	{{end}}
//
// # Voting
//
// The form posts to /_/<key>/vote with the fields poll (the poll ID) and
// option (an option ID), VoteInput; form_open adds the spam trap and the way
// back, to the element with the id Poll.Anchor. The visitor returns to the
// page with ?<key>=<result>&<key>_poll=<poll ID>; the result is Poll.Result
// of that poll only, so other polls on the page show none:
//
//	voted      the vote was counted
//	already    this visitor already voted on this poll
//	closed     the poll is closed or no longer published
//	invalid    no option, or an option of another poll, was chosen
//	throttled  the visitor sent more than 20 votes in a minute; nothing was counted
//
// A visitor is recognised by the wt_poll cookie, which the first vote sets.
// A visitor without it is recognised by their IP address (frontend.ClientIP,
// so list a reverse proxy in Config.TrustedProxies), so scripts that drop
// cookies get one vote per address; when someone else on that address
// already voted, the visitor gets a cookie of their own and can vote again.
// The address is keyed with a secret of the instance (webthings.Env.Secret)
// and only the SHA-256 of the visitor's id is stored, so votes cannot be
// traced back to addresses.
package polls

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"embed"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"regexp"
	"slices"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/pages"
	"github.com/donseba/go-webthings/site"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-form/v2/types"
	"github.com/donseba/go-router"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// Lines is a list of strings stored as a JSON array in one column.
type Lines []string

// Fields are the language-independent parts of a poll. Options are the option
// IDs in their order; votes refer to them. The editor makes UUIDs; code that
// writes polls through Store may use any IDs that are unique in the poll,
// not empty and at most 36 bytes, and must keep them once there are votes.
type Fields struct {
	Options Lines `db:"option_ids"`
	// ClosesAt is when voting ends, in UTC; nil keeps the poll open.
	ClosesAt    *time.Time `db:"closes_at"`
	ShowResults bool       `db:"show_results"`
}

// Text is a poll in one language. Options are the labels of Fields.Options,
// in the same order.
type Text struct {
	Question string `db:"question"`
	Slug     string `db:"slug"`
	Options  Lines  `db:"option_labels"`
}

// Item is a stored poll with its translations.
type Item = content.Item[Fields, Text]

const (
	maxOptions = 20
	// maxLabel is the longest option label in bytes.
	maxLabel = 200
	// maxOptionID is the longest option ID in bytes, the size of the column.
	maxOptionID = 36
)

// Errors of Vote.
var (
	// ErrClosed is a poll that is not open for votes at this time.
	ErrClosed = errors.New("polls_closed")
	// ErrAlreadyVoted is a visitor who voted on the poll before.
	ErrAlreadyVoted = errors.New("polls_already_voted")
	// ErrInvalidOption is an option the poll does not have.
	ErrInvalidOption = errors.New("polls_invalid_option")
)

var (
	errOptions     = errors.New("polls_options")
	errTooMany     = errors.New("polls_too_many_options")
	errOptionVoted = errors.New("polls_option_has_votes")
	errOptionID    = errors.New("polls_invalid_option_id")
)

var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func spec() content.Spec[Fields, Text] {
	return content.Spec[Fields, Text]{
		Table:      "wt_poll",
		SlugColumn: "slug",
		Complete: func(text Text) bool {
			return text.Question != "" && len(text.Options) >= 2
		},
		Check: func(fields Fields, translations []content.Translation[Text]) error {
			if len(fields.Options) > maxOptions {
				return errTooMany
			}

			if !validOptionIDs(fields.Options) {
				return errOptionID
			}

			for _, translation := range translations {
				if err := checkText(fields, translation.Text); err != nil {
					return err
				}
			}

			return nil
		},
	}
}

// validOptionIDs reports whether every option ID is set, short enough and
// used once.
func validOptionIDs(ids Lines) bool {
	seen := make(map[string]bool, len(ids))

	for _, id := range ids {
		if id == "" || len(id) > maxOptionID || seen[id] {
			return false
		}

		seen[id] = true
	}

	return true
}

func checkText(fields Fields, text Text) error {
	if len(text.Options) > len(fields.Options) {
		return errOptions
	}

	if content.TooLong(500, text.Question) || content.TooLong(maxLabel, text.Options...) {
		return content.ErrTooLong
	}

	return nil
}

// Service is one Polls instance, for the website's own code.
type Service struct {
	key      string
	db       *db.DB
	items    *content.Collection[Fields, Text]
	frontend *frontend.Frontend
	pages    *pages.Service
	site     *site.Service
	// visitorID is webthings.Env.VisitorID, for visitors without the cookie.
	visitorID func(ctx context.Context, r *http.Request, parts ...string) (string, error)
	// actions receive the votes (webthings.Env.Actions).
	actions *router.Router
	// changed tells the app that what visitors see changed
	// (webthings.Env.Changed).
	changed func()
}

// Of returns the Polls instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// voteURL is the address vote forms post to: the action named vote.
func (s *Service) voteURL() string {
	return s.actions.RoutePath("vote")
}

// Store gives write access to this instance's polls, for imports and seeds.
// Admin edits go through the same store; new polls are put first.
func (s *Service) Store() content.Store[Fields, Text] {
	return newestFirst{Store: s.items.In(s.key)}
}

// newestFirst puts new polls at the top of the list, so the latest poll is
// shown first unless editors reorder the list.
type newestFirst struct {
	content.Store[Fields, Text]
}

func (s newestFirst) Save(ctx context.Context, draft content.Draft[Fields, Text]) (Item, error) {
	saved, err := s.Store.Save(ctx, draft)
	if err != nil || draft.ID != "" {
		return saved, err
	}

	if err := s.Move(ctx, saved.ID, 0); err != nil {
		return saved, err
	}

	return s.Load(ctx, saved.ID, "")
}

func (s newestFirst) Move(ctx context.Context, id string, position int) error {
	return s.Store.(content.Mover).Move(ctx, id, position)
}

// Poll is a poll as templates see it, with the results and what the current
// visitor did.
type Poll struct {
	ID       string
	Slug     string
	Question string
	Options  []Option
	// Total is the number of votes.
	Total int
	// ClosesAt is when voting ends, in the site's timezone; zero when the poll
	// stays open.
	ClosesAt time.Time
	Closed   bool
	// Voted is set when the current visitor voted; Option.Chosen marks their choice.
	Voted bool
	// CanVote is set when the poll is open and the visitor has not voted.
	CanVote bool
	// ShowResults is set when results may be shown: after voting, when the
	// poll is closed, or always when editors chose so.
	ShowResults bool
	// Action is the address the vote form posts to.
	Action string
	// Anchor is the id of the element the vote form returns the visitor
	// to, <key>-<ID>, for the element around the form and its result.
	Anchor string
	// Form is the vote form's model (VoteInput, with the poll's ID) for
	// go-form's template functions, such as {{form_open .Form}}; the ids
	// of its fields start with Anchor.
	Form form.RenderModel
	// Result is the outcome of the visitor's last vote on this poll:
	// "voted", "already", "closed", "invalid", "throttled" or empty.
	Result string
}

// Option is one answer of a poll.
type Option struct {
	ID    string
	Label string
	Votes int
	// Percent is the share of all votes, rounded.
	Percent int
	Chosen  bool
}

// ListView is the data of polls/list.gohtml.
type ListView struct {
	// Title is the title of the page the module is mounted on.
	Title string
	// Key is the instance key, for {{pollBox .Key .ID}}.
	Key   string
	Polls []Poll
}

// Latest returns up to limit published polls in locale, in their order (new
// polls first); a limit of 0 or less returns all. r is the visitor's
// request, or nil.
func (s *Service) Latest(ctx context.Context, r *http.Request, locale string, limit int) ([]Poll, error) {
	items, err := s.items.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	return s.polls(ctx, r, items, locale)
}

// Find returns a published poll by its slug in locale or by its ID, or
// content.ErrNotFound. r is the visitor's request, or nil.
func (s *Service) Find(ctx context.Context, r *http.Request, locale, slugOrID string) (Poll, error) {
	item, err := s.items.PublishedBySlug(ctx, s.key, locale, slugOrID)
	if errors.Is(err, content.ErrNotFound) && idPattern.MatchString(slugOrID) {
		item, err = s.items.Published(ctx, s.key, slugOrID, locale)
	}

	if err != nil {
		return Poll{}, err
	}

	polls, err := s.polls(ctx, r, []Item{item}, locale)
	if err != nil {
		return Poll{}, err
	}

	return polls[0], nil
}

// Vote records visitor's choice of option on a poll. visitor identifies the
// voter; only its hash is stored. It returns content.ErrNotFound for unknown
// or unpublished polls, ErrClosed, ErrInvalidOption or ErrAlreadyVoted.
func (s *Service) Vote(ctx context.Context, pollID, optionID, visitor string) error {
	if !idPattern.MatchString(pollID) {
		return content.ErrNotFound
	}

	item, err := s.items.Get(ctx, s.key, pollID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()

	switch {
	case !item.Published:
		return content.ErrNotFound
	case closed(item, now):
		return ErrClosed
	case !slices.Contains(item.Fields.Options, optionID):
		return ErrInvalidOption
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO wt_poll_vote (poll_id, instance, option_id, visitor, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		item.ID, s.key, optionID, hash(visitor), now,
	)
	if db.IsUnique(err) {
		return ErrAlreadyVoted
	}

	return db.AfterWrite(ctx, err, s.changed)
}

// ResetVotes removes all votes of a poll.
func (s *Service) ResetVotes(ctx context.Context, pollID string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM wt_poll_vote WHERE instance=? AND poll_id=?`, s.key, pollID)

	return db.AfterWrite(ctx, err, s.changed)
}

// polls turns items into Polls with their votes and the visitor's choices.
func (s *Service) polls(ctx context.Context, r *http.Request, items []Item, locale string) ([]Poll, error) {
	ids := make([]string, 0, len(items))

	for _, item := range items {
		ids = append(ids, item.ID)
	}

	counts, err := s.counts(ctx, ids)
	if err != nil {
		return nil, err
	}

	visit := visit{}

	if r != nil {
		query := r.URL.Query()
		visit.result = frontend.Result(r, s.key, voteResults...)
		visit.poll = query.Get(s.key + pollSuffix)

		visit.choices, err = s.choices(ctx, visitorCookie(r))
		if err != nil {
			return nil, err
		}
	}

	location := s.location(ctx)
	now := time.Now().UTC()
	polls := make([]Poll, 0, len(items))

	for _, item := range items {
		polls = append(polls, s.poll(ctx, item, locale, counts[item.ID], visit, location, now))
	}

	return polls, nil
}

// visit is what the current visitor did: their choice per poll ID, and the
// result of their last vote with the poll it was on.
type visit struct {
	choices map[string]string
	result  string
	poll    string
}

func (s *Service) poll(
	ctx context.Context,
	item Item,
	locale string,
	counts map[string]int,
	visit visit,
	location *time.Location,
	now time.Time,
) Poll {
	text, _ := item.Text(locale)
	choice, voted := visit.choices[item.ID]

	poll := Poll{
		ID:       item.ID,
		Slug:     text.Slug,
		Question: text.Question,
		Closed:   closed(item, now),
		Voted:    voted,
		Action:   s.voteURL(),
		Anchor:   s.anchor(item.ID),
	}

	poll.Form = frontend.VisitorForm(ctx, VoteInput{Poll: item.ID}, poll.Action, poll.Anchor)
	poll.Form.Options = form.FieldOptions{"option": optionValues(item, locale)}
	poll.Form.Overrides = form.Overrides{"option": {"label": text.Question}}

	if visit.poll == item.ID {
		poll.Result = visit.result
	}

	if item.Fields.ClosesAt != nil {
		poll.ClosesAt = item.Fields.ClosesAt.In(location)
	}

	poll.CanVote = !poll.Closed && !poll.Voted
	poll.ShowResults = poll.Voted || poll.Closed || item.Fields.ShowResults

	for i, id := range item.Fields.Options {
		poll.Options = append(poll.Options, Option{
			ID:     id,
			Label:  label(item, locale, i),
			Votes:  counts[id],
			Chosen: voted && choice == id,
		})
		poll.Total += counts[id]
	}

	if poll.Total == 0 {
		return poll
	}

	// Integer percentages, rounded half up.
	for i, option := range poll.Options {
		poll.Options[i].Percent = (option.Votes*200 + poll.Total) / (2 * poll.Total)
	}

	return poll
}

// anchor is the id of the element around a poll's vote form and its
// result, and the key its kept vote waits under (frontend.KeepForm).
func (s *Service) anchor(pollID string) string {
	return s.key + "-" + pollID
}

// optionValues are the options of a poll for its vote form, labelled in
// locale.
func optionValues(item Item, locale string) []types.FieldValue {
	values := make([]types.FieldValue, 0, len(item.Fields.Options))

	for i, id := range item.Fields.Options {
		values = append(values, types.FieldValue{Value: id, Name: label(item, locale, i)})
	}

	return values
}

// label is the label of option i in locale. An option added in another
// language shows that language's label until it is translated.
func label(item Item, locale string, i int) string {
	if text, ok := item.Text(locale); ok && i < len(text.Options) && text.Options[i] != "" {
		return text.Options[i]
	}

	for _, translation := range item.Translations {
		if i < len(translation.Text.Options) && translation.Text.Options[i] != "" {
			return translation.Text.Options[i]
		}
	}

	return ""
}

func closed(item Item, now time.Time) bool {
	return item.Fields.ClosesAt != nil && !now.Before(*item.Fields.ClosesAt)
}

// counts returns the votes per option ID for each of the polls.
func (s *Service) counts(ctx context.Context, pollIDs []string) (map[string]map[string]int, error) {
	counts := map[string]map[string]int{}
	if len(pollIDs) == 0 {
		return counts, nil
	}

	query, args, err := s.db.In(`
		SELECT poll_id, option_id, COUNT(*) AS votes
		FROM wt_poll_vote
		WHERE instance=? AND poll_id IN (?)
		GROUP BY poll_id, option_id`,
		s.key, pollIDs,
	)
	if err != nil {
		return nil, err
	}

	var rows []struct {
		Poll   string `db:"poll_id"`
		Option string `db:"option_id"`
		Votes  int    `db:"votes"`
	}

	if err := s.db.Select(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	for _, row := range rows {
		if counts[row.Poll] == nil {
			counts[row.Poll] = map[string]int{}
		}

		counts[row.Poll][row.Option] = row.Votes
	}

	return counts, nil
}

// choices returns the option the visitor chose per poll ID.
func (s *Service) choices(ctx context.Context, visitor string) (map[string]string, error) {
	choices := map[string]string{}
	if visitor == "" {
		return choices, nil
	}

	var rows []struct {
		Poll   string `db:"poll_id"`
		Option string `db:"option_id"`
	}

	err := s.db.Select(ctx, &rows,
		`SELECT poll_id, option_id FROM wt_poll_vote WHERE instance=? AND visitor=?`,
		s.key, hash(visitor),
	)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		choices[row.Poll] = row.Option
	}

	return choices, nil
}

// votesFor counts the votes on some options of a poll.
func (s *Service) votesFor(ctx context.Context, pollID string, optionIDs []string) (int, error) {
	if pollID == "" || len(optionIDs) == 0 {
		return 0, nil
	}

	query, args, err := s.db.In(
		`SELECT COUNT(*) FROM wt_poll_vote WHERE instance=? AND poll_id=? AND option_id IN (?)`,
		s.key, pollID, optionIDs,
	)
	if err != nil {
		return 0, err
	}

	var count int

	err = s.db.Get(ctx, &count, query, args...)

	return count, err
}

// location is the site's timezone, for closing times.
func (s *Service) location(ctx context.Context) *time.Location {
	if s.site == nil {
		return time.UTC
	}

	settings, err := s.site.Settings(ctx)
	if err != nil {
		return time.UTC
	}

	return settings.Location()
}

// hash is what is stored of a visitor id.
func hash(visitor string) string {
	sum := sha256.Sum256([]byte(visitor))

	return hex.EncodeToString(sum[:])
}

// Value stores the lines as JSON, an empty list as [].
func (l Lines) Value() (driver.Value, error) {
	if l == nil {
		l = Lines{}
	}

	return db.JSONValue(l)
}

// Scan reads lines stored by Value.
func (l *Lines) Scan(src any) error {
	*l = nil

	return db.ScanJSON(src, l)
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
