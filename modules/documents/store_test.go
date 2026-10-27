package documents

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/pages"
)

// TestStore runs the schema, the checks, the download counter and media
// tracking on every configured dialect.
func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "documents", schema()); err != nil {
			t.Fatal(err)
		}

		storage, err := media.NewFilesystem(t.TempDir(), "/media")
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = storage.Close() })

		library := media.New(database, storage)

		file, err := library.Upload(ctx, "minutes.pdf", strings.NewReader("%PDF-1.4\n% minutes\n"))
		if err != nil {
			t.Fatal(err)
		}

		items, err := content.NewCollection(database, spec())
		if err != nil {
			t.Fatal(err)
		}

		service := &Service{
			key:      "docs",
			db:       database,
			items:    items,
			media:    library,
			frontend: frontend.New(nil, nil, library, frontend.Missing()),
			// Without an app the actions are mounted where the app would.
			actions: webthings.Env{Key: "docs"}.Actions(),
		}

		service.registerActions()
		service.public = service.handler()

		store := service.Store()
		march := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)

		minutes, err := store.Save(ctx, content.Draft[Fields, Text]{
			Fields: Fields{File: file.ID, Date: march},
			Translations: []content.Translation[Text]{
				{Locale: "en", Text: Text{Title: "Minutes March", Category: "Minutes"}},
				{Locale: "nl", Text: Text{Title: "Notulen maart", Category: "Notulen"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		report, err := store.Save(ctx, content.Draft[Fields, Text]{
			Fields:       Fields{Link: "https://example.test/report.pdf", Date: march.AddDate(-1, 0, 0)},
			Translations: []content.Translation[Text]{{Locale: "en", Text: Text{Title: "Report"}}},
		})
		if err != nil {
			t.Fatal(err)
		}

		invalid := map[string]Fields{
			"neither": {Date: march},
			"both":    {File: file.ID, Link: "https://example.test", Date: march},
			"link":    {Link: "javascript:alert(1)", Date: march},
			"date":    {Link: "https://example.test"},
		}

		for name, fields := range invalid {
			if _, err := store.Save(ctx, content.Draft[Fields, Text]{Fields: fields}); err == nil {
				t.Fatalf("%s: saved %+v", name, fields)
			}
		}

		// Unpublished documents cannot be downloaded.
		if _, err := service.Download(ctx, minutes.ID); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("download draft: %v", err)
		}

		if _, err := service.Download(ctx, "not-an-id"); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("download unknown: %v", err)
		}

		for _, id := range []string{minutes.ID, report.ID} {
			if err := store.Publish(ctx, id, "", "en"); err != nil {
				t.Fatal(err)
			}
		}

		target, err := service.Download(ctx, minutes.ID)
		if err != nil || !strings.HasPrefix(target, "/media/") {
			t.Fatalf("download file: %q %v", target, err)
		}

		if _, err := service.Download(ctx, minutes.ID); err != nil {
			t.Fatal(err)
		}

		target, err = service.Download(ctx, report.ID)
		if err != nil || target != "https://example.test/report.pdf" {
			t.Fatalf("download link: %q %v", target, err)
		}

		// Saving a document again keeps its download count.
		if _, err := store.Save(ctx, content.Draft[Fields, Text]{
			ID:           minutes.ID,
			Fields:       minutes.Fields,
			Translations: minutes.Translations,
		}); err != nil {
			t.Fatal(err)
		}

		list, err := service.list(ctx, "en", service.public.At("/en/docs").RoutePathArgs)
		if err != nil {
			t.Fatal(err)
		}

		if len(list) != 2 || list[0].Title != "Minutes March" || list[0].Downloads != 2 || list[1].Downloads != 1 {
			t.Fatalf("list: %+v", list)
		}

		first := list[0]
		if first.FileType != "PDF" || first.Filename != "minutes.pdf" || first.Size != file.SizeBytes ||
			first.CategoryURL != "/en/docs/category/minutes" || first.URL != "/_/docs/"+minutes.ID+"/download" {
			t.Fatalf("document: %+v", first)
		}

		if !list[1].External || list[1].FileType != "" || list[1].CategoryURL != "" {
			t.Fatalf("external document: %+v", list[1])
		}

		// The date is a calendar date: west of UTC it stays on its day.
		chicago, err := time.LoadLocation("America/Chicago")
		if err != nil {
			t.Fatal(err)
		}

		format := frontend.Format{Locale: "en", Zone: chicago}
		if got := format.Date(first.Date, "long"); got != "10 March 2026" || first.Date.Location() != frontend.Floating {
			t.Fatalf("date: %q in %v", got, first.Date.Location())
		}

		dutch, err := service.list(ctx, "nl", pages.NoLinks)
		if err != nil || len(dutch) != 1 || dutch[0].Category != "Notulen" || dutch[0].Downloads != 2 {
			t.Fatalf("dutch: %+v %v", dutch, err)
		}

		if err := library.Delete(ctx, file.ID); !errors.Is(err, media.ErrInUse) {
			t.Fatalf("delete used file: %v", err)
		}

		if err := store.Delete(ctx, minutes.ID); err != nil {
			t.Fatal(err)
		}

		// Deleted content keeps its media until it is purged from the trash.
		if err := library.Delete(ctx, file.ID); !errors.Is(err, media.ErrInUse) {
			t.Fatalf("delete file of a deleted document: %v", err)
		}

		if err := store.Purge(ctx, minutes.ID); err != nil {
			t.Fatal(err)
		}

		if err := library.Delete(ctx, file.ID); err != nil {
			t.Fatalf("delete unused file: %v", err)
		}
	})
}

func TestHelpers(t *testing.T) {
	day := func(year int) time.Time {
		return time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	list := []Document{
		{Title: "a", Category: "Minutes", Date: day(2026)},
		{Title: "b", Category: "Reports", Date: day(2026)},
		{Title: "c", Category: "minutes", Date: day(2025)},
		{Title: "d", Date: day(2024)},
	}

	var got []string

	for _, year := range years(list) {
		got = append(got, year.Documents[0].Date.Format("2006")+":"+strconv.Itoa(len(year.Documents)))
	}

	if strings.Join(got, "|") != "2026:2|2025:1|2024:1" {
		t.Errorf("years: %q", got)
	}

	got = nil

	public := (&Service{frontend: frontend.New(nil, nil, nil, frontend.Missing())}).handler()
	paths := public.At("/en/docs").RoutePathArgs

	for _, category := range categories(list, paths, "reports") {
		got = append(got, category.Name+" "+category.URL+" "+strconv.Itoa(category.Count)+" "+strconv.FormatBool(category.Active))
	}

	if strings.Join(got, "|") != "Minutes /en/docs/category/minutes 2 false|Reports /en/docs/category/reports 1 true" {
		t.Errorf("categories: %q", got)
	}

	if len(filter(list, "MINUTES")) != 2 || len(filter(list, "reports")) != 1 || len(filter(list, "")) != 0 {
		t.Error("filter")
	}

	sizes := map[int64]string{999: "999 B", 12_345: "12 kB", 1_234_567: "1.2 MB"}

	for size, want := range sizes {
		if got := sizeText(size); got != want {
			t.Errorf("sizeText(%d) = %q, want %q", size, got, want)
		}
	}

	if got := content.Slug("Notulen & Verslagen (ALV) é"); got != "notulen-verslagen-alv-e" {
		t.Errorf("content.Slug: %q", got)
	}
}
