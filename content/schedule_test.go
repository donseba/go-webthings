package content_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
)

func article(
	t *testing.T,
	store *content.Revisions[articleFields, articleText],
	slug, title string,
) content.Item[articleFields, articleText] {
	t.Helper()

	item, err := store.Save(context.Background(), "news", content.Draft[articleFields, articleText]{
		Translations: []content.Translation[articleText]{{Locale: "en", Text: articleText{Slug: slug, Title: title}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	return item
}

func TestSchedule(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		setup(t, database)
		store := articles(t, database)
		now := time.Now()

		due := article(t, store, "due", "Due")
		later := article(t, store, "later", "Later")

		// Plans are checked: something must be planned, unpublishing comes
		// after publishing and the revision must belong to the item and be
		// complete.
		for _, plan := range []content.Schedule{
			{},
			{RevisionID: due.Revision.ID},
			{PublishAt: now},
			{RevisionID: due.Revision.ID, PublishAt: now, UnpublishAt: now.Add(-time.Hour)},
		} {
			if err := store.Schedule(ctx, "news", due.ID, plan, "en"); !errors.Is(err, content.ErrSchedule) {
				t.Fatalf("%+v: want ErrSchedule, got %v", plan, err)
			}
		}

		err := store.Schedule(ctx, "news", due.ID, content.Schedule{RevisionID: later.Revision.ID, PublishAt: now}, "en")
		if !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("revision of another item: %v", err)
		}

		err = store.Schedule(ctx, "news", due.ID, content.Schedule{RevisionID: due.Revision.ID, PublishAt: now}, "nl")
		if !errors.Is(err, content.ErrIncomplete) {
			t.Fatalf("incomplete translation: %v", err)
		}

		plan := content.Schedule{RevisionID: due.Revision.ID, PublishAt: now.Add(-time.Minute)}
		if err := store.Schedule(ctx, "news", due.ID, plan, "en"); err != nil {
			t.Fatal(err)
		}

		plan = content.Schedule{RevisionID: later.Revision.ID, PublishAt: now.Add(time.Hour), UnpublishAt: now.Add(2 * time.Hour)}
		if err := store.Schedule(ctx, "news", later.ID, plan, "en"); err != nil {
			t.Fatal(err)
		}

		loaded, err := store.Latest(ctx, "news", later.ID)
		if err != nil || loaded.Schedule.RevisionID != later.Revision.ID ||
			!loaded.Schedule.PublishAt.Equal(now.Add(time.Hour).Truncate(time.Second)) {
			t.Fatalf("loaded schedule: %+v %v", loaded.Schedule, err)
		}

		// Two schedulers at once (two processes): the due article is published once.
		var (
			wait  sync.WaitGroup
			mu    sync.Mutex
			total int
		)

		for range 2 {
			wait.Go(func() {
				changes, err := store.RunDue(ctx, "news", now, "en", content.Publisher{})
				if err != nil {
					t.Error(err)
				}

				mu.Lock()
				total += changes
				mu.Unlock()
			})
		}

		wait.Wait()

		if total != 1 {
			t.Fatalf("want 1 change, got %d", total)
		}

		published, err := store.PublishedBySlug(ctx, "news", "en", "due")
		if err != nil || !published.Schedule.IsZero() {
			t.Fatalf("due article: %+v %v", published.Schedule, err)
		}

		if _, err := store.PublishedBySlug(ctx, "news", "en", "later"); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("future article published: %v", err)
		}

		// Later runs carry out the rest, in time order.
		changes, err := store.RunDue(ctx, "news", now.Add(90*time.Minute), "en", content.Publisher{})
		if err != nil || changes != 1 {
			t.Fatalf("publish later: %d %v", changes, err)
		}

		if _, err := store.PublishedBySlug(ctx, "news", "en", "later"); err != nil {
			t.Fatal(err)
		}

		changes, err = store.RunDue(ctx, "news", now.Add(3*time.Hour), "en", content.Publisher{})
		if err != nil || changes != 1 {
			t.Fatalf("unpublish later: %d %v", changes, err)
		}

		if _, err := store.PublishedBySlug(ctx, "news", "en", "later"); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("unpublished article visible: %v", err)
		}

		// A change that fails is dropped and reported, not retried forever.
		clash := article(t, store, "due", "Clash")

		plan = content.Schedule{RevisionID: clash.Revision.ID, PublishAt: now.Add(-time.Second)}
		if err := store.Schedule(ctx, "news", clash.ID, plan, "en"); err != nil {
			t.Fatal(err)
		}

		if _, err := store.RunDue(ctx, "news", now, "en", content.Publisher{}); !errors.Is(err, content.ErrDuplicateSlug) {
			t.Fatalf("want duplicate slug, got %v", err)
		}

		dropped, err := store.Latest(ctx, "news", clash.ID)
		if err != nil || dropped.Published || !dropped.Schedule.IsZero() {
			t.Fatalf("failed plan kept: %+v %v", dropped, err)
		}

		// Plans can be cancelled; other instances are not touched.
		plan = content.Schedule{UnpublishAt: now.Add(-time.Second)}
		if err := store.Schedule(ctx, "news", due.ID, plan, "en"); err != nil {
			t.Fatal(err)
		}

		if changes, err := store.RunDue(ctx, "blog", now, "en", content.Publisher{}); err != nil || changes != 0 {
			t.Fatalf("other instance: %d %v", changes, err)
		}

		if err := store.Unschedule(ctx, "news", due.ID); err != nil {
			t.Fatal(err)
		}

		if changes, err := store.RunDue(ctx, "news", now, "en", content.Publisher{}); err != nil || changes != 0 {
			t.Fatalf("cancelled plan ran: %d %v", changes, err)
		}

		// The store bound to an instance offers the same through Scheduling.
		scheduling, ok := store.In("news").(content.Scheduling)
		if !ok {
			t.Fatal("revisioned store cannot be scheduled")
		}

		if err := scheduling.Schedule(ctx, due.ID, content.Schedule{UnpublishAt: now}, "en"); err != nil {
			t.Fatal(err)
		}

		if changes, err := store.Scheduler("news").RunDue(ctx, now, "en"); err != nil || changes != 1 {
			t.Fatalf("scheduler: %d %v", changes, err)
		}

		if _, err := store.PublishedBySlug(ctx, "news", "en", "due"); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("scheduled unpublication: %v", err)
		}
	})
}

// TestManualPublishCancelsSchedule checks that publishing or unpublishing by
// hand drops a plan made earlier, which would otherwise undo the editor's
// decision.
func TestManualPublishCancelsSchedule(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		setup(t, database)
		store := articles(t, database)
		now := time.Now()

		item := article(t, store, "planned", "Planned")
		plan := content.Schedule{RevisionID: item.Revision.ID, PublishAt: now.Add(time.Hour), UnpublishAt: now.Add(2 * time.Hour)}

		if err := store.Schedule(ctx, "news", item.ID, plan, "en"); err != nil {
			t.Fatal(err)
		}

		if err := store.Publish(ctx, "news", item.ID, item.Revision.ID, "en"); err != nil {
			t.Fatal(err)
		}

		loaded, err := store.Latest(ctx, "news", item.ID)
		if err != nil || !loaded.Published || !loaded.Schedule.IsZero() {
			t.Fatalf("publish kept the plan: %+v %v", loaded.Schedule, err)
		}

		if changes, err := store.RunDue(
			ctx,
			"news",
			now.Add(3*time.Hour),
			"en",
			content.Publisher{},
		); err != nil ||
			changes != 0 {
			t.Fatalf("cancelled plan ran: %d %v", changes, err)
		}

		// A scheduled publication keeps the planned unpublication, which the
		// next run carries out.
		draft, err := store.Save(
			ctx,
			"news",
			content.Draft[articleFields, articleText]{ID: item.ID, Translations: item.Translations},
		)
		if err != nil {
			t.Fatal(err)
		}

		plan = content.Schedule{RevisionID: draft.Revision.ID, PublishAt: now.Add(time.Hour), UnpublishAt: now.Add(2 * time.Hour)}
		if err := store.Schedule(ctx, "news", item.ID, plan, "en"); err != nil {
			t.Fatal(err)
		}

		if changes, err := store.RunDue(
			ctx,
			"news",
			now.Add(90*time.Minute),
			"en",
			content.Publisher{},
		); err != nil ||
			changes != 1 {
			t.Fatalf("scheduled publication: %d %v", changes, err)
		}

		loaded, err = store.Latest(ctx, "news", item.ID)
		if err != nil || loaded.PublishedID != draft.Revision.ID || loaded.Schedule.RevisionID != "" ||
			!loaded.Schedule.UnpublishAt.Equal(plan.UnpublishAt.Truncate(time.Second)) {
			t.Fatalf("after scheduled publication: %+v %v", loaded.Schedule, err)
		}

		if changes, err := store.RunDue(
			ctx,
			"news",
			now.Add(3*time.Hour),
			"en",
			content.Publisher{},
		); err != nil ||
			changes != 1 {
			t.Fatalf("scheduled unpublication: %d %v", changes, err)
		}

		loaded, err = store.Latest(ctx, "news", item.ID)
		if err != nil || loaded.Published || !loaded.Schedule.IsZero() {
			t.Fatalf("after scheduled unpublication: %+v %v", loaded, err)
		}

		if err := store.Publish(ctx, "news", item.ID, draft.Revision.ID, "en"); err != nil {
			t.Fatal(err)
		}

		if err := store.Schedule(ctx, "news", item.ID, content.Schedule{UnpublishAt: now.Add(time.Hour)}, "en"); err != nil {
			t.Fatal(err)
		}

		if err := store.Unpublish(ctx, "news", item.ID); err != nil {
			t.Fatal(err)
		}

		loaded, err = store.Latest(ctx, "news", item.ID)
		if err != nil || loaded.Published || !loaded.Schedule.IsZero() {
			t.Fatalf("unpublish kept the plan: %+v %v", loaded.Schedule, err)
		}
	})
}
