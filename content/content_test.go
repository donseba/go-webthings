package content_test

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/markdown"
)

//go:embed testdata/*.sql
var testdata embed.FS

type (
	articleFields struct {
		Pinned bool `db:"pinned"`
	}
	articleText struct {
		Slug  string `db:"slug"`
		Title string `db:"title"`
		Body  string `db:"body"`
	}
	quoteFields struct {
		Author string `db:"author"`
	}
	quoteText struct {
		Slug string `db:"slug"`
		Text string `db:"text"`
	}
)

func setup(t *testing.T, database *db.DB) {
	t.Helper()

	files, err := fs.Sub(testdata, "testdata")
	if err != nil {
		t.Fatal(err)
	}

	if err := database.ApplySchema(context.Background(), "content_test", files); err != nil {
		t.Fatal(err)
	}
}

func articles(t *testing.T, database *db.DB) *content.Revisions[articleFields, articleText] {
	t.Helper()

	store, err := content.NewRevisions(database, content.Spec[articleFields, articleText]{
		Table:      "t_article",
		SlugColumn: "slug",
		Complete: func(text articleText) bool {
			return text.Title != "" && text.Slug != ""
		},
		Media: func(_ articleFields, translations []content.Translation[articleText]) []string {
			var ids []string

			for _, translation := range translations {
				ids = append(ids, markdown.MediaIDs(translation.Text.Body)...)
			}

			return ids
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	return store
}

func TestRevisionsLifecycle(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		setup(t, database)
		store := articles(t, database)

		first, err := store.Save(ctx, "news", content.Draft[articleFields, articleText]{
			Translations: []content.Translation[articleText]{
				{Locale: "en", Text: articleText{Slug: "hello", Title: "Hello"}},
			},
			Note: "first",
		})
		if err != nil {
			t.Fatal(err)
		}

		if first.Revision.Number != 1 || first.Published {
			t.Fatalf("unexpected first revision: %+v", first)
		}

		if err := store.Publish(ctx, "news", first.ID, first.Revision.ID, "en"); err != nil {
			t.Fatal(err)
		}

		// A Dutch draft is incomplete and stays invisible.
		second, err := store.Save(ctx, "news", content.Draft[articleFields, articleText]{
			ID:           first.ID,
			Fields:       articleFields{Pinned: true},
			Translations: first.WithText("nl", articleText{Title: "Hallo"}),
		})
		if err != nil {
			t.Fatal(err)
		}

		if second.Revision.Number != 2 || !second.Published || second.PublishedNumber != 1 || second.IsPublishedRevision() {
			t.Fatalf("unexpected second revision: %+v", second)
		}

		live, err := store.PublishedBySlug(ctx, "news", "en", "hello")
		if err != nil || live.Revision.Number != 1 || live.Fields.Pinned {
			t.Fatalf("public view changed by draft: %+v %v", live, err)
		}

		if _, err := store.Published(ctx, "news", first.ID, "nl"); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("incomplete translation visible: %v", err)
		}

		if _, err := store.PublishedBySlug(ctx, "blog", "en", "hello"); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("instance scope leaked: %v", err)
		}

		history, err := store.History(ctx, "news", first.ID)
		if err != nil || len(history) != 2 || history[0].Number != 2 {
			t.Fatalf("history: %+v %v", history, err)
		}

		restored, err := store.Restore(ctx, "news", first.ID, first.Revision.ID, "")
		if err != nil || restored.Revision.Number != 3 || restored.Fields.Pinned {
			t.Fatalf("restore: %+v %v", restored, err)
		}

		// Another article cannot publish the same slug.
		other, err := store.Save(ctx, "news", content.Draft[articleFields, articleText]{
			Translations: []content.Translation[articleText]{
				{Locale: "en", Text: articleText{Slug: "hello", Title: "Again"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := store.Publish(ctx, "news", other.ID, other.Revision.ID, "en"); !errors.Is(err, content.ErrDuplicateSlug) {
			t.Fatalf("want duplicate slug, got %v", err)
		}

		if err := store.Publish(ctx, "news", other.ID, other.Revision.ID, "nl"); !errors.Is(err, content.ErrIncomplete) {
			t.Fatalf("want incomplete, got %v", err)
		}

		if err := store.Unpublish(ctx, "news", first.ID); err != nil {
			t.Fatal(err)
		}

		published, err := store.ListPublished(ctx, "news", "en")
		if err != nil || len(published) != 0 {
			t.Fatalf("unpublished content listed: %+v %v", published, err)
		}

		latest, err := store.ListLatest(ctx, "news")
		if err != nil || len(latest) != 2 {
			t.Fatalf("latest: %+v %v", latest, err)
		}

		if err := store.Delete(ctx, "news", first.ID); err != nil {
			t.Fatal(err)
		}

		if _, err := store.Latest(ctx, "news", first.ID); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("deleted content found: %v", err)
		}

		// IDs from URLs may be anything; on Postgres uuid columns too.
		if _, err := store.Latest(ctx, "news", "not-a-uuid"); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("invalid id: %v", err)
		}

		if err := store.Publish(ctx, "news", "not-a-uuid", "nope", "en"); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("invalid id publish: %v", err)
		}
	})
}

func TestRevisionsValidationAndMedia(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		setup(t, database)
		store := articles(t, database)

		invalid := []content.Draft[articleFields, articleText]{
			{Translations: []content.Translation[articleText]{{Locale: "not a locale"}}},
			{Translations: []content.Translation[articleText]{{Locale: "en"}, {Locale: "en"}}},
			{Translations: []content.Translation[articleText]{{Locale: "en", Text: articleText{Slug: "Bad Slug"}}}},
		}

		for _, draft := range invalid {
			if _, err := store.Save(ctx, "news", draft); !errors.Is(err, content.ErrInvalid) {
				t.Fatalf("want invalid for %+v, got %v", draft, err)
			}
		}

		missing := db.NewID()
		draft := content.Draft[articleFields, articleText]{
			Translations: []content.Translation[articleText]{
				{Locale: "en", Text: articleText{Slug: "pic", Title: "Pic", Body: "![x](media:" + missing + ")"}},
			},
		}

		if _, err := store.Save(ctx, "news", draft); !errors.Is(err, content.ErrMediaMissing) {
			t.Fatalf("want missing media, got %v", err)
		}

		if _, err := database.Exec(ctx, `INSERT INTO wt_media_asset(id) VALUES(?)`, missing); err != nil {
			t.Fatal(err)
		}

		saved, err := store.Save(ctx, "news", draft)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := database.Exec(ctx, `DELETE FROM wt_media_asset WHERE id=?`, missing); !db.IsForeignKey(err) {
			t.Fatalf("used media deleted: %v", err)
		}

		if err := store.Delete(ctx, "news", saved.ID); err != nil {
			t.Fatal(err)
		}

		// An item in the trash keeps its media, so it can come back.
		if _, err := database.Exec(ctx, `DELETE FROM wt_media_asset WHERE id=?`, missing); !db.IsForeignKey(err) {
			t.Fatalf("media of a deleted item deleted: %v", err)
		}

		if err := store.Purge(ctx, "news", saved.ID); err != nil {
			t.Fatal(err)
		}

		if _, err := database.Exec(ctx, `DELETE FROM wt_media_asset WHERE id=?`, missing); err != nil {
			t.Fatalf("unused media not deletable: %v", err)
		}
	})
}

func TestCollection(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		setup(t, database)

		store, err := content.NewCollection(database, content.Spec[quoteFields, quoteText]{
			Table:      "t_quote",
			SlugColumn: "slug",
			Complete: func(text quoteText) bool {
				return text.Text != ""
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		var ids []string

		for _, author := range []string{"Ada", "Grace", "Linus"} {
			item, err := store.Save(ctx, "quotes", content.Draft[quoteFields, quoteText]{
				Fields: quoteFields{Author: author},
				Translations: []content.Translation[quoteText]{
					{Locale: "en", Text: quoteText{Slug: strings.ToLower(author), Text: "Quote by " + author}},
				},
			})
			if err != nil {
				t.Fatal(err)
			}

			ids = append(ids, item.ID)
		}

		if err := store.Move(ctx, "quotes", ids[2], 0); err != nil {
			t.Fatal(err)
		}

		for _, id := range ids[:2] {
			if err := store.SetPublished(ctx, "quotes", id, true, "en"); err != nil {
				t.Fatal(err)
			}
		}

		if err := store.SetPublished(ctx, "quotes", ids[2], true, "nl"); !errors.Is(err, content.ErrIncomplete) {
			t.Fatalf("want incomplete, got %v", err)
		}

		all, err := store.List(ctx, "quotes")
		if err != nil || len(all) != 3 || all[0].Fields.Author != "Linus" {
			t.Fatalf("order: %+v %v", all, err)
		}

		published, err := store.ListPublished(ctx, "quotes", "en")
		if err != nil || len(published) != 2 {
			t.Fatalf("published: %+v %v", published, err)
		}

		updated, err := store.Save(ctx, "quotes", content.Draft[quoteFields, quoteText]{
			ID:           ids[0],
			Fields:       quoteFields{Author: "Ada Lovelace"},
			Translations: all[1].WithText("nl", quoteText{Text: "Citaat"}),
		})
		if err != nil || updated.Fields.Author != "Ada Lovelace" || len(updated.Translations) != 2 || !updated.Published {
			t.Fatalf("update: %+v %v", updated, err)
		}

		_, err = store.Save(ctx, "quotes", content.Draft[quoteFields, quoteText]{
			ID: ids[1],
			Translations: []content.Translation[quoteText]{
				{Locale: "en", Text: quoteText{Slug: "ada", Text: "Duplicate slug"}},
			},
		})
		if !errors.Is(err, content.ErrDuplicateSlug) {
			t.Fatalf("want duplicate slug, got %v", err)
		}
	})
}
