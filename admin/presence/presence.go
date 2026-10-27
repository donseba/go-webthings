// Package presence keeps track of which admin page every signed-in user is
// looking at, so the admin can show "Ann is also on this page". It locks
// nothing: two editors can still change the same item.
//
// Browser tabs report the page they show (Tracker.Visit) when they open it
// and every few seconds after; a tab that stops reporting is forgotten after
// Options.TTL, and a tab that closes says so (Tracker.Leave). Streams of a
// page (Tracker.Subscribe) hear when its viewers change.
//
// The Tracker keeps its streams in memory, so it serves one server. Visits
// are kept in a Store, by default in memory as well; a shared Store, and
// notifications between servers, are for later.
package presence

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/donseba/go-partial/exp/sse"
)

// Visit is one browser tab of a signed-in user on an admin page.
type Visit struct {
	// Tab identifies the browser tab; the browser chooses it.
	Tab string
	// User is the user's ID and Name what others see: the user's name, never
	// an e-mail address. An empty name shows as a general one.
	User string
	Name string
	// Page is the normalised admin path, the same for every language tab
	// of an editor, such as the editor of a page.
	Page string
	// Seen is when the tab last reported.
	Seen time.Time
}

// Store keeps the visits. Memory keeps them in this process; a store shared
// by several servers can implement the same.
type Store interface {
	// Touch records the visit, replacing the earlier one of the same user
	// and tab, and returns the page that tab was on before: empty for a new
	// tab.
	Touch(ctx context.Context, visit Visit) (previous string, err error)
	// Leave forgets the tab of user and returns the page it was on; empty
	// when the store did not know it.
	Leave(ctx context.Context, user, tab string) (page string, err error)
	// Visits lists the visits of page.
	Visits(ctx context.Context, page string) ([]Visit, error)
	// Expire forgets the visits last seen before before and returns the
	// pages they were on.
	Expire(ctx context.Context, before time.Time) (pages []string, err error)
}

// Viewer is a user looking at a page, once however many tabs they have open
// on it.
type Viewer struct {
	ID   string
	Name string
	// Initials are one or two letters of the name for a small avatar; "?"
	// without name.
	Initials string
}

// Options configure a Tracker.
type Options struct {
	// Store keeps the visits; nil keeps them in memory.
	Store Store
	// TTL is how long a tab counts as present after it last reported; zero
	// means 45 seconds. Tabs report more often than that.
	TTL time.Duration
	// Sweep is how often forgotten tabs are cleared and their pages told;
	// zero means 10 seconds.
	Sweep time.Duration
	// MaxStreams caps the open streams of the server, MaxStreamsPerUser
	// those of one user; zero means 500 and 8.
	MaxStreams        int
	MaxStreamsPerUser int
	// Now is the clock, for tests; nil uses time.Now.
	Now func() time.Time
}

// Event is the name of the messages streams receive when the viewers of
// their page changed.
const Event sse.EventName = "presence"

// Tracker records visits and tells the streams of a page when its viewers
// change.
type Tracker struct {
	store   Store
	broker  *sse.Broker
	ttl     time.Duration
	sweep   time.Duration
	now     func() time.Time
	mu      sync.Mutex
	started bool
	stop    chan struct{}
	stopped bool
}

// New returns a tracker.
func New(options Options) *Tracker {
	if options.Store == nil {
		options.Store = NewMemory()
	}

	if options.TTL <= 0 {
		options.TTL = 45 * time.Second
	}

	if options.Sweep <= 0 {
		options.Sweep = 10 * time.Second
	}

	if options.MaxStreams <= 0 {
		options.MaxStreams = 500
	}

	if options.MaxStreamsPerUser <= 0 {
		options.MaxStreamsPerUser = 8
	}

	if options.Now == nil {
		options.Now = time.Now
	}

	// Streams only hear that something changed, so one queued message is
	// enough: they read the current viewers when they handle it.
	broker := sse.NewBroker(sse.BrokerOptions{
		MaxSubscribers: options.MaxStreams,
		MaxPerKey:      options.MaxStreamsPerUser,
		Buffer:         1,
	})

	return &Tracker{
		store:  options.Store,
		broker: broker,
		ttl:    options.TTL,
		sweep:  options.Sweep,
		now:    options.Now,
		stop:   make(chan struct{}),
	}
}

// Visit records that a tab shows page now. The streams of the page, and of
// the page the tab showed before, hear of it when the tab is new there.
func (t *Tracker) Visit(ctx context.Context, visit Visit) error {
	visit.Seen = t.now()

	previous, err := t.store.Touch(ctx, visit)
	if err != nil {
		return err
	}

	t.startSweeping()

	if previous == visit.Page {
		return nil
	}

	if previous != "" {
		t.changed(previous)
	}

	t.changed(visit.Page)

	return nil
}

// Leave forgets a tab, for example one that closed.
func (t *Tracker) Leave(ctx context.Context, user, tab string) error {
	page, err := t.store.Leave(ctx, user, tab)
	if err != nil {
		return err
	}

	if page != "" {
		t.changed(page)
	}

	return nil
}

// Viewers lists the users on page, by name, each once, leaving out except
// (the user asking) and tabs that stopped reporting.
func (t *Tracker) Viewers(ctx context.Context, page, except string) ([]Viewer, error) {
	visits, err := t.store.Visits(ctx, page)
	if err != nil {
		return nil, err
	}

	since := t.now().Add(-t.ttl)
	seen := map[string]bool{}

	var viewers []Viewer

	for _, visit := range visits {
		if visit.User == except || seen[visit.User] || visit.Seen.Before(since) {
			continue
		}

		seen[visit.User] = true

		viewers = append(viewers, Viewer{
			ID:       visit.User,
			Name:     visit.Name,
			Initials: Initials(visit.Name),
		})
	}

	slices.SortFunc(viewers, func(x, y Viewer) int {
		if c := cmp.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)); c != 0 {
			return c
		}

		return cmp.Compare(x.ID, y.ID)
	})

	return viewers, nil
}

// Subscribe opens a stream's subscription to the changes of page. It fails
// with sse.ErrTooManySubscribers when the user or the server has too many
// streams open. Close the subscription when the stream ends.
func (t *Tracker) Subscribe(user, page string) (*sse.Subscription, error) {
	subscription, err := t.broker.Subscribe(user, page)
	if err != nil {
		return nil, err
	}

	t.startSweeping()

	return subscription, nil
}

// Sweep forgets the tabs that stopped reporting and tells their pages.
func (t *Tracker) Sweep(ctx context.Context) error {
	pages, err := t.store.Expire(ctx, t.now().Add(-t.ttl))
	if err != nil {
		return err
	}

	for _, page := range pages {
		t.changed(page)
	}

	return nil
}

// Close ends every stream and stops sweeping.
func (t *Tracker) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.stopped {
		t.stopped = true
		close(t.stop)
	}

	t.broker.Close()
}

// startSweeping starts the sweeper with the first visit or stream, so a
// site where nobody signs in runs none.
func (t *Tracker) startSweeping() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.started || t.stopped {
		return
	}

	t.started = true

	go t.sweepEvery()
}

func (t *Tracker) sweepEvery() {
	ticker := time.NewTicker(t.sweep)
	defer ticker.Stop()

	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			// A failing store is tried again at the next tick.
			_ = t.Sweep(context.Background())
		}
	}
}

func (t *Tracker) changed(page string) {
	t.broker.Publish(page, sse.Message{Event: Event})
}

// Initials are the first letters of the first and last word of name, in
// upper case; "?" when name has no letters.
func Initials(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	if len(words) == 0 {
		return "?"
	}

	initials := []rune{firstRune(words[0])}

	if len(words) > 1 {
		initials = append(initials, firstRune(words[len(words)-1]))
	}

	return strings.ToUpper(string(initials))
}

func firstRune(word string) rune {
	for _, r := range word {
		return r
	}

	return '?'
}
