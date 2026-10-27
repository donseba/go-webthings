package editor_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/locales"
)

// adminContext is the context of a request to an admin at path with
// section, so section hooks build the addresses of its routes.
func adminContext(t *testing.T, path string, section admin.Section) context.Context {
	t.Helper()

	accounts, err := auth.New(dbtest.SQLite(t))
	if err != nil {
		t.Fatal(err)
	}

	backoffice, err := admin.New(accounts, admin.Options{Path: path, CatalogueDir: t.TempDir()}, section)
	if err != nil {
		t.Fatal(err)
	}

	return backoffice.Context(context.Background())
}

// plainStore hides the scheduling of store, as a collection has none.
type plainStore struct {
	content.Store[person, text]
}

// TestDashboardHooks checks what an editor section gives the dashboard: its
// latest changes newest first, its planned publications and the "new item"
// action, with links to the admin wherever it is.
func TestDashboardHooks(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)

	languages, err := locales.New(locales.Config{Default: "nl", Enabled: []string{"nl", "en"}})
	if err != nil {
		t.Fatal(err)
	}

	old := ada()
	old.ID = "old"
	old.UpdatedAt = now.Add(-48 * time.Hour)
	old.Revision = content.Revision{ID: "r1", Number: 1, CreatedAt: now.Add(-72 * time.Hour)}

	// A new revision is newer than the item's last update.
	edited := ada()
	edited.ID = "edited"
	edited.Revision = content.Revision{ID: "r2", Number: 2, Author: "user-1", CreatedAt: now.Add(-time.Hour)}
	edited.UpdatedAt = now.Add(-5 * time.Hour)
	edited.Published = true
	edited.PublishedNumber = 1

	planned := ada()
	planned.ID = "planned"
	planned.UpdatedAt = now.Add(-3 * time.Hour)
	planned.Revision.CreatedAt = now.Add(-4 * time.Hour)
	planned.Schedule = content.Schedule{
		RevisionID:  "r1",
		PublishAt:   now.Add(24 * time.Hour),
		UnpublishAt: now.Add(72 * time.Hour),
	}

	people := newStore(old, edited, planned)
	config := editor.Config[person, text, personInput]{
		ID:       "people",
		Title:    "People",
		NewLabel: "New person",
		Store:    people,
		Locales:  languages,
		Label: func(value text) string {
			return value.Role
		},
	}

	section := editor.New(config)
	if section.NewLabel != "New person" || section.NewRoute != "new" {
		t.Fatalf("new action = %q %q", section.NewLabel, section.NewRoute)
	}

	ctx := adminContext(t, "/toolbox", section)

	recent, err := section.Recent(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(recent) != 2 || recent[0].URL != "/toolbox/people/edited?locale=nl" ||
		recent[1].URL != "/toolbox/people/planned?locale=nl" {
		t.Fatalf("recent = %+v", recent)
	}

	// The store reads no more than the dashboard shows.
	if !slices.Equal(people.recentLimits, []int{2}) {
		t.Fatalf("recent limits = %v", people.recentLimits)
	}

	first := recent[0]
	if !first.UpdatedAt.Equal(now.Add(-time.Hour)) || first.Author != "user-1" || first.Status != "Changes not published" {
		t.Fatalf("recent item = %+v", first)
	}

	// The item has no Dutch text, so it is named in English.
	if first.Title != "Analyst" {
		t.Fatalf("title = %q", first.Title)
	}

	scheduled, err := section.Scheduled(ctx, 8)
	if err != nil {
		t.Fatal(err)
	}

	if none, err := section.Scheduled(ctx, 0); err != nil || len(none) != 0 {
		t.Fatalf("a limit of 0 lists %d planned items (%v)", len(none), err)
	}

	if len(scheduled) != 1 || scheduled[0].URL != "/toolbox/people/planned?locale=nl" ||
		!scheduled[0].PublishAt.Equal(
			planned.Schedule.PublishAt,
		) || !scheduled[0].UnpublishAt.Equal(planned.Schedule.UnpublishAt) {
		t.Fatalf("scheduled = %+v", scheduled)
	}

	// A single item has no "new" action, and a store that cannot plan
	// publication nothing scheduled.
	config.Single = true
	config.Store = plainStore{newStore(old)}

	section = editor.New(config)
	if section.NewRoute != "" || section.Scheduled != nil || section.Recent == nil {
		t.Fatalf("single plain section: new %q, scheduled %v", section.NewRoute, section.Scheduled != nil)
	}
}

// Planned lists the items with a planned publication change, as
// content.Planner does, the earliest first.
func (s *store) Planned(context.Context) ([]item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var planned []item

	for _, it := range s.items {
		if !it.Schedule.IsZero() {
			planned = append(planned, it)
		}
	}

	slices.SortFunc(planned, func(x, y item) int {
		return x.Schedule.PublishAt.Compare(y.Schedule.PublishAt)
	})

	return planned, nil
}
