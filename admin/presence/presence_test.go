package presence_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin/presence"

	"github.com/donseba/go-partial/exp/sse"
)

// clock is a settable time for the tracker.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

func newTracker(t *testing.T) (*presence.Tracker, *clock) {
	t.Helper()

	c := &clock{now: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)}
	tracker := presence.New(presence.Options{TTL: 45 * time.Second, Sweep: time.Hour, Now: c.Now})
	t.Cleanup(tracker.Close)

	return tracker, c
}

func names(viewers []presence.Viewer) []string {
	var list []string

	for _, viewer := range viewers {
		list = append(list, viewer.Name)
	}

	return list
}

// changed reports whether the subscription heard of a change, and empties it.
func changed(subscription *sse.Subscription) bool {
	select {
	case message := <-subscription.Messages():
		return message.Event == presence.Event
	default:
		return false
	}
}

func TestViewersOncePerUserWithoutAsker(t *testing.T) {
	ctx := context.Background()
	tracker, _ := newTracker(t)

	visits := []presence.Visit{
		{Tab: "t1", User: "ann", Name: "Ann Smith", Page: "/admin/pages/1"},
		// Ann's second tab on the same page shows her once.
		{Tab: "t2", User: "ann", Name: "Ann Smith", Page: "/admin/pages/1"},
		{Tab: "t3", User: "bob", Name: "bob", Page: "/admin/pages/1"},
		{Tab: "t4", User: "cid", Name: "Cid", Page: "/admin/pages/1"},
		{Tab: "t5", User: "dan", Name: "Dan", Page: "/admin/pages/2"},
	}

	for _, visit := range visits {
		if err := tracker.Visit(ctx, visit); err != nil {
			t.Fatal(err)
		}
	}

	viewers, err := tracker.Viewers(ctx, "/admin/pages/1", "cid")
	if err != nil {
		t.Fatal(err)
	}

	if got := names(viewers); !slices.Equal(got, []string{"Ann Smith", "bob"}) {
		t.Fatalf("viewers = %v", got)
	}

	if viewers[0].Initials != "AS" || viewers[1].Initials != "B" {
		t.Fatalf("initials = %q, %q", viewers[0].Initials, viewers[1].Initials)
	}
}

func TestNavigationMovesTheTab(t *testing.T) {
	ctx := context.Background()
	tracker, _ := newTracker(t)

	first, err := tracker.Subscribe("bob", "/admin/pages/1")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	second, err := tracker.Subscribe("bob", "/admin/pages/2")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	ann := presence.Visit{Tab: "t1", User: "ann", Name: "Ann", Page: "/admin/pages/1"}
	if err := tracker.Visit(ctx, ann); err != nil {
		t.Fatal(err)
	}

	if !changed(first) || changed(second) {
		t.Fatal("only the page Ann opened hears of her")
	}

	// Reporting the same page again changes nothing.
	if err := tracker.Visit(ctx, ann); err != nil {
		t.Fatal(err)
	}

	if changed(first) {
		t.Fatal("a repeated report was announced")
	}

	ann.Page = "/admin/pages/2"
	if err := tracker.Visit(ctx, ann); err != nil {
		t.Fatal(err)
	}

	if !changed(first) || !changed(second) {
		t.Fatal("both pages hear that Ann moved")
	}

	if viewers, _ := tracker.Viewers(ctx, "/admin/pages/1", ""); len(viewers) != 0 {
		t.Fatalf("Ann stayed on the page she left: %v", names(viewers))
	}

	// A closed tab leaves at once; another user cannot close it.
	if err := tracker.Leave(ctx, "bob", "t1"); err != nil {
		t.Fatal(err)
	}

	if viewers, _ := tracker.Viewers(ctx, "/admin/pages/2", ""); len(viewers) != 1 {
		t.Fatal("another user closed Ann's tab")
	}

	if err := tracker.Leave(ctx, "ann", "t1"); err != nil {
		t.Fatal(err)
	}

	if !changed(second) {
		t.Fatal("the page did not hear that Ann left")
	}

	if viewers, _ := tracker.Viewers(ctx, "/admin/pages/2", ""); len(viewers) != 0 {
		t.Fatal("Ann's closed tab is still there")
	}
}

func TestTabsThatStopReportingExpire(t *testing.T) {
	ctx := context.Background()
	tracker, c := newTracker(t)

	subscription, err := tracker.Subscribe("bob", "/admin/pages/1")
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	if err := tracker.Visit(ctx, presence.Visit{Tab: "t1", User: "ann", Name: "Ann", Page: "/admin/pages/1"}); err != nil {
		t.Fatal(err)
	}

	changed(subscription)

	// Reports keep Ann present.
	c.Add(30 * time.Second)

	if err := tracker.Visit(ctx, presence.Visit{Tab: "t1", User: "ann", Name: "Ann", Page: "/admin/pages/1"}); err != nil {
		t.Fatal(err)
	}

	c.Add(30 * time.Second)

	if viewers, _ := tracker.Viewers(ctx, "/admin/pages/1", ""); len(viewers) != 1 {
		t.Fatal("Ann expired although she reported")
	}

	// Without reports, she is gone after the TTL, before any sweep.
	c.Add(20 * time.Second)

	if viewers, _ := tracker.Viewers(ctx, "/admin/pages/1", ""); len(viewers) != 0 {
		t.Fatal("a tab that stopped reporting is still shown")
	}

	if err := tracker.Sweep(ctx); err != nil {
		t.Fatal(err)
	}

	if !changed(subscription) {
		t.Fatal("the sweep did not tell the page")
	}
}

func TestStreamLimits(t *testing.T) {
	tracker := presence.New(presence.Options{MaxStreams: 3, MaxStreamsPerUser: 2})
	t.Cleanup(tracker.Close)

	for range 2 {
		if _, err := tracker.Subscribe("ann", "/admin/pages/1"); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := tracker.Subscribe("ann", "/admin/pages/1"); !errors.Is(err, sse.ErrTooManySubscribers) {
		t.Fatalf("a third stream of one user: %v", err)
	}

	if _, err := tracker.Subscribe("bob", "/admin/pages/1"); err != nil {
		t.Fatal(err)
	}

	if _, err := tracker.Subscribe("cid", "/admin/pages/1"); !errors.Is(err, sse.ErrTooManySubscribers) {
		t.Fatalf("a stream over the server's limit: %v", err)
	}
}

func TestCloseEndsStreams(t *testing.T) {
	tracker := presence.New(presence.Options{})

	subscription, err := tracker.Subscribe("ann", "/admin/pages/1")
	if err != nil {
		t.Fatal(err)
	}

	tracker.Close()
	tracker.Close()

	if _, ok := <-subscription.Messages(); ok {
		t.Fatal("the stream stayed open")
	}
}

func TestMemoryExpire(t *testing.T) {
	ctx := context.Background()
	store := presence.NewMemory()
	now := time.Now()

	visits := []presence.Visit{
		{Tab: "t1", User: "ann", Page: "/admin/a", Seen: now.Add(-time.Minute)},
		{Tab: "t2", User: "bob", Page: "/admin/b", Seen: now},
		{Tab: "t3", User: "cid", Page: "/admin/a", Seen: now},
	}

	for _, visit := range visits {
		if _, err := store.Touch(ctx, visit); err != nil {
			t.Fatal(err)
		}
	}

	pages, err := store.Expire(ctx, now.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(pages, []string{"/admin/a"}) {
		t.Fatalf("expired pages = %v", pages)
	}

	left, _ := store.Visits(ctx, "/admin/a")
	if len(left) != 1 || left[0].User != "cid" {
		t.Fatalf("visits left = %v", left)
	}
}

func TestInitials(t *testing.T) {
	for name, want := range map[string]string{
		"Ann Smith":            "AS",
		"jan van der berg":     "JB",
		"Émile":                "É",
		"":                     "?",
		"  --  ":               "?",
		"Ann-Marie O'Connor":   "AC",
		"李 小龙":                 "李小",
		"r2d2":                 "R",
		"Mary  Jane   Watson ": "MW",
	} {
		if got := presence.Initials(name); got != want {
			t.Errorf("Initials(%q) = %q, want %q", name, got, want)
		}
	}
}
