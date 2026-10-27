package presence

import (
	"context"
	"sync"
	"time"
)

// Memory is a Store in this process's memory, for a site on one server.
type Memory struct {
	mu     sync.Mutex
	visits map[tabKey]Visit
	// pages holds the tabs of every page, so Visits reads only those.
	pages map[string]map[tabKey]struct{}
}

// tabKey is a tab of a user: tabs are chosen by browsers, so one user can
// never move or close another user's tab.
type tabKey struct {
	user string
	tab  string
}

// NewMemory returns an empty memory store.
func NewMemory() *Memory {
	return &Memory{
		visits: map[tabKey]Visit{},
		pages:  map[string]map[tabKey]struct{}{},
	}
}

// Touch implements Store.
func (m *Memory) Touch(_ context.Context, visit Visit) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := tabKey{user: visit.User, tab: visit.Tab}
	previous := m.visits[key].Page

	if previous != visit.Page {
		m.remove(key, previous)
	}

	m.visits[key] = visit

	tabs := m.pages[visit.Page]
	if tabs == nil {
		tabs = map[tabKey]struct{}{}
		m.pages[visit.Page] = tabs
	}

	tabs[key] = struct{}{}

	return previous, nil
}

// Leave implements Store.
func (m *Memory) Leave(_ context.Context, user, tab string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := tabKey{user: user, tab: tab}

	visit, ok := m.visits[key]
	if !ok {
		return "", nil
	}

	delete(m.visits, key)
	m.remove(key, visit.Page)

	return visit.Page, nil
}

// Visits implements Store.
func (m *Memory) Visits(_ context.Context, page string) ([]Visit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	visits := make([]Visit, 0, len(m.pages[page]))

	for key := range m.pages[page] {
		visits = append(visits, m.visits[key])
	}

	return visits, nil
}

// Expire implements Store.
func (m *Memory) Expire(_ context.Context, before time.Time) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	changed := map[string]bool{}

	for key, visit := range m.visits {
		if !visit.Seen.Before(before) {
			continue
		}

		delete(m.visits, key)
		m.remove(key, visit.Page)

		changed[visit.Page] = true
	}

	pages := make([]string, 0, len(changed))

	for page := range changed {
		pages = append(pages, page)
	}

	return pages, nil
}

// remove takes a tab off page; the caller holds the lock.
func (m *Memory) remove(key tabKey, page string) {
	tabs := m.pages[page]
	if tabs == nil {
		return
	}

	delete(tabs, key)

	if len(tabs) == 0 {
		delete(m.pages, page)
	}
}
