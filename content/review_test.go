package content_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
)

// newsReviewed is a review policy that has the workflow on for "news" only.
func newsReviewed(_ context.Context, instance string) bool {
	return instance == "news"
}

func TestReviewWorkflow(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		setup(t, database)
		store := articles(t, database)

		editor := db.NewID()
		reviewer := db.NewID()
		plain := context.Background()
		reviewed := content.WithReview(plain, newsReviewed)
		asEditor := db.WithActor(reviewed, editor)
		asReviewer := db.WithActor(reviewed, reviewer)

		// Without a policy nothing changes: drafts publish as before.
		free := article(t, store, "free", "Free")
		if err := store.Publish(plain, "news", free.ID, free.Revision.ID, "en"); err != nil {
			t.Fatal(err)
		}

		// Nor does it for instances the policy leaves out.
		if content.ReviewRequired(reviewed, "other") || !content.ReviewRequired(reviewed, "news") {
			t.Fatal("policy per instance")
		}

		// With the workflow on, a draft cannot be published or scheduled.
		item := article(t, store, "hello", "Hello")

		if err := store.Publish(asEditor, "news", item.ID, item.Revision.ID, "en"); !errors.Is(err, content.ErrNotApproved) {
			t.Fatalf("publish a draft: %v", err)
		}

		plan := content.Schedule{RevisionID: item.Revision.ID, PublishAt: time.Now().Add(time.Hour)}
		if err := store.Schedule(asEditor, "news", item.ID, plan, "en"); !errors.Is(err, content.ErrNotApproved) {
			t.Fatalf("schedule a draft: %v", err)
		}

		// Only submitted revisions can be decided on.
		if err := store.Approve(asReviewer, "news", item.ID, item.Revision.ID, ""); !errors.Is(err, content.ErrReviewState) {
			t.Fatalf("approve a draft: %v", err)
		}

		if err := store.Submit(asEditor, "news", item.ID, item.Revision.ID); err != nil {
			t.Fatal(err)
		}

		if err := store.Submit(asEditor, "news", item.ID, item.Revision.ID); !errors.Is(err, content.ErrReviewState) {
			t.Fatalf("submit twice: %v", err)
		}

		loaded, err := store.Latest(plain, "news", item.ID)
		if err != nil {
			t.Fatal(err)
		}

		review := loaded.Revision.Review
		if review.State != content.ReviewPending || review.SubmittedBy != editor || review.SubmittedAt.IsZero() {
			t.Fatalf("submitted: %+v", review)
		}

		waiting, err := store.InReview(plain, "news", 10)
		if err != nil || len(waiting) != 1 || waiting[0].Revision.ID != item.Revision.ID {
			t.Fatalf("in review: %v %v", waiting, err)
		}

		// A rejection needs a note, and a revision of another item is not
		// found.
		if err := store.Reject(asReviewer, "news", item.ID, item.Revision.ID, " "); !errors.Is(err, content.ErrReviewState) {
			t.Fatalf("reject without note: %v", err)
		}

		if err := store.Approve(asReviewer, "news", free.ID, item.Revision.ID, ""); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("approve a revision of another item: %v", err)
		}

		if err := store.Reject(asReviewer, "news", item.ID, item.Revision.ID, "Too short"); err != nil {
			t.Fatal(err)
		}

		history, err := store.History(plain, "news", item.ID)
		if err != nil {
			t.Fatal(err)
		}

		review = history[0].Review
		if review.State != content.ReviewRejected || review.ReviewedBy != reviewer || review.Note != "Too short" {
			t.Fatalf("rejected: %+v", review)
		}

		// A rejected revision can be submitted again; submitting a newer
		// revision sends the older one back to draft.
		if err := store.Submit(asEditor, "news", item.ID, item.Revision.ID); err != nil {
			t.Fatal(err)
		}

		newer, err := store.Save(asEditor, "news", content.Draft[articleFields, articleText]{
			ID: item.ID,
			Translations: []content.Translation[articleText]{
				{Locale: "en", Text: articleText{Slug: "hello", Title: "Hello again"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		if newer.Revision.Review.State != content.ReviewDraft {
			t.Fatalf("new revision: %+v", newer.Revision.Review)
		}

		if err := store.Submit(asEditor, "news", item.ID, newer.Revision.ID); err != nil {
			t.Fatal(err)
		}

		old, err := store.Revision(plain, "news", item.ID, item.Revision.ID)
		if err != nil || old.Revision.Review.State != content.ReviewDraft {
			t.Fatalf("older submission: %+v %v", old.Revision.Review, err)
		}

		// An approved revision publishes.
		if err := store.Approve(asReviewer, "news", item.ID, newer.Revision.ID, "Fine"); err != nil {
			t.Fatal(err)
		}

		if err := store.Publish(asReviewer, "news", item.ID, newer.Revision.ID, "en"); err != nil {
			t.Fatal(err)
		}

		// The published revision cannot be submitted again.
		if err := store.Submit(asEditor, "news", item.ID, newer.Revision.ID); !errors.Is(err, content.ErrReviewState) {
			t.Fatalf("submit the published revision: %v", err)
		}

		waiting, err = store.InReview(plain, "news", 10)
		if err != nil || len(waiting) != 0 {
			t.Fatalf("in review after approval: %v %v", waiting, err)
		}
	})
}

// TestReviewSchedule checks that the scheduler respects the workflow, also
// for plans made before it was turned on.
func TestReviewSchedule(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		setup(t, database)
		store := articles(t, database)

		plain := context.Background()
		reviewed := content.WithReview(plain, newsReviewed)
		past := time.Now().Add(-time.Minute)

		approved := article(t, store, "approved", "Approved")
		draft := article(t, store, "draft", "Draft")

		if err := store.Submit(reviewed, "news", approved.ID, approved.Revision.ID); err != nil {
			t.Fatal(err)
		}

		if err := store.Approve(reviewed, "news", approved.ID, approved.Revision.ID, ""); err != nil {
			t.Fatal(err)
		}

		if err := store.Schedule(
			reviewed,
			"news",
			approved.ID,
			content.Schedule{RevisionID: approved.Revision.ID, PublishAt: past},
			"en",
		); err != nil {
			t.Fatal(err)
		}

		// Planned while the workflow was off.
		if err := store.Schedule(
			plain,
			"news",
			draft.ID,
			content.Schedule{RevisionID: draft.Revision.ID, PublishAt: past},
			"en",
		); err != nil {
			t.Fatal(err)
		}

		changes, err := store.Scheduler("news").RunDue(reviewed, time.Now(), "en")
		if changes != 1 || !errors.Is(err, content.ErrNotApproved) {
			t.Fatalf("run: %d changes, %v", changes, err)
		}

		for id, want := range map[string]bool{approved.ID: true, draft.ID: false} {
			item, err := store.Latest(plain, "news", id)
			if err != nil || item.Published != want || !item.Schedule.IsZero() {
				t.Fatalf("%s: published %v, schedule %+v, %v", id, item.Published, item.Schedule, err)
			}
		}
	})
}

func TestPreviewBySlug(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		setup(t, database)
		store := articles(t, database)

		published := article(t, store, "live", "Live")
		if err := store.Publish(ctx, "news", published.ID, published.Revision.ID, "en"); err != nil {
			t.Fatal(err)
		}

		draft, err := store.Save(ctx, "news", content.Draft[articleFields, articleText]{
			ID: published.ID,
			Translations: []content.Translation[articleText]{
				{Locale: "en", Text: articleText{Slug: "live", Title: "Live, edited"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		unpublished := article(t, store, "new", "New")

		for _, check := range []struct {
			name, slug, revision, want string
			missing                    bool
		}{
			{name: "no preview", slug: "live", want: published.Revision.ID},
			{name: "draft of the published item", slug: "live", revision: draft.Revision.ID, want: draft.Revision.ID},
			{name: "revision of another item", slug: "live", revision: unpublished.Revision.ID, want: published.Revision.ID},
			{name: "unknown revision", slug: "live", revision: db.NewID(), want: published.Revision.ID},
			{name: "never published", slug: "new", revision: unpublished.Revision.ID, want: unpublished.Revision.ID},
			{name: "never published, no preview", slug: "new", missing: true},
			{name: "never published, wrong address", slug: "other", revision: unpublished.Revision.ID, missing: true},
		} {
			item, err := store.PreviewBySlug(ctx, "news", "en", check.slug, check.revision)

			switch {
			case check.missing && !errors.Is(err, content.ErrNotFound):
				t.Fatalf("%s: want not found, got %v %v", check.name, item.Revision.ID, err)
			case !check.missing && (err != nil || item.Revision.ID != check.want):
				t.Fatalf("%s: got %v %v", check.name, item.Revision.ID, err)
			}
		}
	})
}
