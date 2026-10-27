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

func englishArticle(slug, title string) []content.Translation[articleText] {
	return []content.Translation[articleText]{
		{Locale: "en", Text: articleText{Slug: slug, Title: title}},
	}
}

func TestRevisionsAuditAndTrash(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		setup(t, database)
		store := articles(t, database)
		alice, bob := db.NewID(), db.NewID()
		asAlice := db.WithActor(context.Background(), alice)
		asBob := db.WithActor(context.Background(), bob)

		first, err := store.Save(asAlice, "news", content.Draft[articleFields, articleText]{
			Translations: englishArticle("hello", "Hello"),
		})
		if err != nil {
			t.Fatal(err)
		}

		if first.CreatedBy != alice || first.UpdatedBy != alice || first.DeletedAt != nil {
			t.Fatalf("audit of a new item: %+v", first)
		}

		second, err := store.Save(asBob, "news", content.Draft[articleFields, articleText]{
			ID:           first.ID,
			Translations: englishArticle("hello", "Hello again"),
		})
		if err != nil {
			t.Fatal(err)
		}

		if second.CreatedBy != alice || second.UpdatedBy != bob {
			t.Fatalf("audit of a changed item: %+v", second)
		}

		if err := store.Publish(asAlice, "news", first.ID, second.Revision.ID, "en"); err != nil {
			t.Fatal(err)
		}

		if err := store.Delete(asBob, "news", first.ID); err != nil {
			t.Fatal(err)
		}

		// A deleted item is gone from every read.
		if _, err := store.Latest(asBob, "news", first.ID); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("latest of a deleted item: %v", err)
		}

		if _, err := store.PublishedBySlug(asBob, "news", "en", "hello"); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("deleted item public: %v", err)
		}

		if _, err := store.History(asBob, "news", first.ID); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("history of a deleted item: %v", err)
		}

		for name, list := range map[string]func() ([]content.Item[articleFields, articleText], error){
			"latest": func() ([]content.Item[articleFields, articleText], error) { return store.ListLatest(asBob, "news") },
			"published": func() ([]content.Item[articleFields, articleText], error) {
				return store.ListPublished(asBob, "news", "en")
			},
			"recent": func() ([]content.Item[articleFields, articleText], error) { return store.Recent(asBob, "news", 10) },
		} {
			items, err := list()
			if err != nil || len(items) != 0 {
				t.Fatalf("%s lists a deleted item: %+v %v", name, items, err)
			}
		}

		if err := store.Publish(asBob, "news", first.ID, second.Revision.ID, "en"); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("publish of a deleted item: %v", err)
		}

		if _, err := store.Save(
			asBob,
			"news",
			content.Draft[articleFields, articleText]{ID: first.ID},
		); !errors.Is(
			err,
			content.ErrNotFound,
		) {
			t.Fatalf("save of a deleted item: %v", err)
		}

		trashed, err := store.Trashed(asBob, "news")
		if err != nil || len(trashed) != 1 {
			t.Fatalf("trash: %+v %v", trashed, err)
		}

		if trashed[0].DeletedBy != bob || trashed[0].DeletedAt == nil || trashed[0].Revision.Number != 2 {
			t.Fatalf("trashed item: %+v", trashed[0])
		}

		// The slug of a deleted item is free; bringing the old item back
		// then conflicts, and it stays in the trash.
		other, err := store.Save(asAlice, "news", content.Draft[articleFields, articleText]{
			Translations: englishArticle("hello", "Another hello"),
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := store.Publish(asAlice, "news", other.ID, other.Revision.ID, "en"); err != nil {
			t.Fatalf("slug of a deleted item blocks: %v", err)
		}

		if err := store.Undelete(asAlice, "news", first.ID); !errors.Is(err, content.ErrDuplicateSlug) {
			t.Fatalf("want duplicate slug on undelete, got %v", err)
		}

		if trashed, _ := store.Trashed(asAlice, "news"); len(trashed) != 1 {
			t.Fatalf("conflicting undelete left the trash: %+v", trashed)
		}

		if err := store.Unpublish(asAlice, "news", other.ID); err != nil {
			t.Fatal(err)
		}

		if err := store.Undelete(asAlice, "news", first.ID); err != nil {
			t.Fatal(err)
		}

		restored, err := store.PublishedBySlug(asAlice, "news", "en", "hello")
		if err != nil || restored.ID != first.ID || restored.UpdatedBy != alice || restored.DeletedAt != nil {
			t.Fatalf("undeleted item: %+v %v", restored, err)
		}

		// Purge only takes items from the trash.
		if err := store.Purge(asAlice, "news", other.ID); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("purge of a live item: %v", err)
		}

		if err := store.Delete(asAlice, "news", other.ID); err != nil {
			t.Fatal(err)
		}

		if err := store.Purge(asAlice, "news", other.ID); err != nil {
			t.Fatal(err)
		}

		if trashed, _ := store.Trashed(asAlice, "news"); len(trashed) != 0 {
			t.Fatalf("purged item in the trash: %+v", trashed)
		}

		if err := store.Undelete(asAlice, "news", other.ID); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("undelete of a purged item: %v", err)
		}
	})
}

func TestRevisionsRecentAndPlanned(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		setup(t, database)
		store := articles(t, database)

		var ids []string

		// The items were changed an hour apart, in this order, so the order
		// is certain without waiting for the clock.
		changed := time.Now().UTC().Add(-time.Hour)

		for i, slug := range []string{"one", "two", "three"} {
			item, err := store.Save(ctx, "news", content.Draft[articleFields, articleText]{
				Translations: englishArticle(slug, slug),
			})
			if err != nil {
				t.Fatal(err)
			}

			ids = append(ids, item.ID)

			at := changed.Add(time.Duration(i) * time.Minute)
			if _, err := database.Exec(ctx, `UPDATE t_article SET updated_at=? WHERE id=?`, at, item.ID); err != nil {
				t.Fatal(err)
			}
		}

		// Changing the first makes it the most recent.
		if _, err := store.Save(ctx, "news", content.Draft[articleFields, articleText]{
			ID:           ids[0],
			Translations: englishArticle("one", "One"),
		}); err != nil {
			t.Fatal(err)
		}

		recent, err := store.Recent(ctx, "news", 2)
		if err != nil || len(recent) != 2 || recent[0].ID != ids[0] || recent[1].ID != ids[2] {
			t.Fatalf("recent: %+v %v", recent, err)
		}

		if recent[0].Revision.Number != 2 {
			t.Fatalf("recent shows an old revision: %+v", recent[0])
		}

		latest, err := store.Latest(ctx, "news", ids[1])
		if err != nil {
			t.Fatal(err)
		}

		plan := content.Schedule{RevisionID: latest.Revision.ID, PublishAt: time.Now().Add(time.Hour)}
		if err := store.Schedule(ctx, "news", ids[1], plan, "en"); err != nil {
			t.Fatal(err)
		}

		planned, err := store.Planned(ctx, "news")
		if err != nil || len(planned) != 1 || planned[0].ID != ids[1] {
			t.Fatalf("planned: %+v %v", planned, err)
		}

		// Deleting cancels the plan.
		if err := store.Delete(ctx, "news", ids[1]); err != nil {
			t.Fatal(err)
		}

		if err := store.Undelete(ctx, "news", ids[1]); err != nil {
			t.Fatal(err)
		}

		if planned, _ := store.Planned(ctx, "news"); len(planned) != 0 {
			t.Fatalf("plan kept through the trash: %+v", planned)
		}
	})
}

func TestCollectionAuditAndTrash(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		setup(t, database)
		alice, bob := db.NewID(), db.NewID()
		asAlice := db.WithActor(context.Background(), alice)
		asBob := db.WithActor(context.Background(), bob)

		store, err := content.NewCollection(database, content.Spec[quoteFields, quoteText]{
			Table:      "t_quote",
			SlugColumn: "slug",
			Complete: func(text quoteText) bool {
				return text.Text != ""
			},
			Deletable: func(_ context.Context, id string) error {
				if id == "keep" {
					return errors.New("in use")
				}

				return nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		quote := func(author, slug string) content.Draft[quoteFields, quoteText] {
			return content.Draft[quoteFields, quoteText]{
				Fields: quoteFields{Author: author},
				Translations: []content.Translation[quoteText]{
					{Locale: "en", Text: quoteText{Slug: slug, Text: "Quote by " + author}},
				},
			}
		}

		ada, err := store.Save(asAlice, "quotes", quote("Ada", "ada"))
		if err != nil {
			t.Fatal(err)
		}

		if err := store.SetPublished(asBob, "quotes", ada.ID, true, "en"); err != nil {
			t.Fatal(err)
		}

		ada, err = store.Get(asBob, "quotes", ada.ID)
		if err != nil || ada.CreatedBy != alice || ada.UpdatedBy != bob {
			t.Fatalf("audit: %+v %v", ada, err)
		}

		if err := store.Delete(asBob, "quotes", ada.ID); err != nil {
			t.Fatal(err)
		}

		if _, err := store.Get(asBob, "quotes", ada.ID); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("get of a deleted item: %v", err)
		}

		if _, err := store.PublishedBySlug(asBob, "quotes", "en", "ada"); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("deleted item public: %v", err)
		}

		if err := store.SetPublished(asBob, "quotes", ada.ID, false, ""); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("unpublish of a deleted item: %v", err)
		}

		trashed, err := store.Trashed(asBob, "quotes")
		if err != nil || len(trashed) != 1 || trashed[0].DeletedBy != bob || !trashed[0].Published {
			t.Fatalf("trash: %+v %v", trashed, err)
		}

		// The deleted quote's slug and unique author are free again.
		again, err := store.Save(asAlice, "quotes", quote("Ada", "ada"))
		if err != nil {
			t.Fatalf("deleted item blocks its slug or author: %v", err)
		}

		if err := store.Undelete(asAlice, "quotes", ada.ID); !errors.Is(err, content.ErrDuplicateSlug) {
			t.Fatalf("want duplicate slug, got %v", err)
		}

		// With another slug, the unique author still conflicts.
		if _, err := store.Save(asAlice, "quotes", content.Draft[quoteFields, quoteText]{
			ID:           again.ID,
			Fields:       quoteFields{Author: "Ada"},
			Translations: []content.Translation[quoteText]{{Locale: "en", Text: quoteText{Slug: "ada-2", Text: "x"}}},
		}); err != nil {
			t.Fatal(err)
		}

		if err := store.Undelete(asAlice, "quotes", ada.ID); !errors.Is(err, content.ErrDuplicateSlug) {
			t.Fatalf("want duplicate author, got %v", err)
		}

		if err := store.Delete(asAlice, "quotes", again.ID); err != nil {
			t.Fatal(err)
		}

		if err := store.Undelete(asAlice, "quotes", ada.ID); err != nil {
			t.Fatal(err)
		}

		back, err := store.PublishedBySlug(asAlice, "quotes", "en", "ada")
		if err != nil || back.ID != ada.ID || back.UpdatedBy != alice {
			t.Fatalf("undeleted: %+v %v", back, err)
		}

		recent, err := store.Recent(asAlice, "quotes", 5)
		if err != nil || len(recent) != 1 || recent[0].ID != ada.ID {
			t.Fatalf("recent: %+v %v", recent, err)
		}

		if err := store.Purge(asAlice, "quotes", again.ID); err != nil {
			t.Fatal(err)
		}

		if trashed, _ := store.Trashed(asAlice, "quotes"); len(trashed) != 0 {
			t.Fatalf("trash after purge: %+v", trashed)
		}

		if err := store.Delete(asAlice, "quotes", "keep"); err == nil || err.Error() != "in use" {
			t.Fatalf("Deletable not asked: %v", err)
		}
	})
}
