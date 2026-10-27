package directory

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"
)

// TestStore runs the schema, unique slugs, opening hours, coordinates, media
// tracking, refused category deletion and the CSV import on every configured dialect.
func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "directory", schema()); err != nil {
			t.Fatal(err)
		}

		storage, err := media.NewFilesystem(t.TempDir(), "/media")
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = storage.Close() })

		library := media.New(database, storage)
		logo := upload(t, library)
		service := newTestService(t, database)

		food, err := service.categories.Save(ctx, "guide", content.Draft[CategoryFields, CategoryText]{
			Fields:       CategoryFields{Slug: "food"},
			Translations: []content.Translation[CategoryText]{{Locale: "en", Text: CategoryText{Name: "Food"}}},
		})
		if err != nil {
			t.Fatal(err)
		}

		// Slugs are unique per instance, not across instances.
		_, err = service.categories.Save(ctx, "guide", content.Draft[CategoryFields, CategoryText]{
			Fields: CategoryFields{Slug: "food"},
		})
		if !db.IsUnique(err) || issue(err) == "" {
			t.Fatalf("duplicate category slug: %v", err)
		}

		_, err = service.categories.Save(ctx, "other", content.Draft[CategoryFields, CategoryText]{
			Fields: CategoryFields{Slug: "food"},
		})
		if err != nil {
			t.Fatalf("same slug in another instance: %v", err)
		}

		var hours OpeningHours
		hours[1] = Day{Open: "09:00", Close: "17:30"}
		hours[5] = Day{Open: "22:00", Close: "02:00"}

		bakery, err := service.entries.Save(ctx, "guide", content.Draft[EntryFields, EntryText]{
			Fields: EntryFields{
				Category:  food.ID,
				Slug:      "bakery-jansen",
				Name:      "Bakery Jansen",
				Image:     logo,
				Website:   "https://bakery.example",
				Email:     "info@bakery.example",
				Latitude:  sql.NullFloat64{Float64: 52.3731, Valid: true},
				Longitude: sql.NullFloat64{Float64: 4.8922, Valid: true},
				Hours:     hours,
			},
			Translations: []content.Translation[EntryText]{
				{Locale: "en", Text: EntryText{Summary: "Bread", Keywords: "bread, cake"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		if bakery.Fields.Hours != hours || bakery.Fields.Latitude.Float64 != 52.3731 || !bakery.Fields.Longitude.Valid {
			t.Fatalf("round trip: %+v", bakery.Fields)
		}

		plain, err := service.entries.Save(ctx, "guide", content.Draft[EntryFields, EntryText]{
			Fields: EntryFields{Category: food.ID, Slug: "plain", Name: "Plain"},
		})
		if err != nil {
			t.Fatal(err)
		}

		if plain.Fields.Hours.Known() || plain.Fields.Latitude.Valid || plain.Fields.Image != "" {
			t.Fatalf("empty fields: %+v", plain.Fields)
		}

		_, err = service.entries.Save(ctx, "guide", content.Draft[EntryFields, EntryText]{
			Fields: EntryFields{Category: food.ID, Slug: "plain", Name: "Plain again"},
		})
		if !db.IsUnique(err) {
			t.Fatalf("duplicate entry slug: %v", err)
		}

		invalid := map[error]EntryFields{
			errEmail:    {Category: food.ID, Slug: "x", Name: "X", Email: "not an address"},
			errWebsite:  {Category: food.ID, Slug: "x", Name: "X", Website: "javascript:alert(1)"},
			errLocation: {Category: food.ID, Slug: "x", Name: "X", Latitude: sql.NullFloat64{Float64: 1, Valid: true}},
			errHours:    {Category: food.ID, Slug: "x", Name: "X", Hours: OpeningHours{{Open: "25:00", Close: "26:00"}}},
			errSlug:     {Category: food.ID, Slug: "Not A Slug", Name: "X"},
			errName:     {Category: food.ID, Slug: "x"},
			// Store refuses an entry without a category like the admin does:
			// it would never be shown.
			errCategory: {Slug: "x", Name: "X"},
		}

		for want, fields := range invalid {
			_, err := service.entries.Save(ctx, "guide", content.Draft[EntryFields, EntryText]{Fields: fields})
			if !errors.Is(err, want) || issue(err) == "" {
				t.Fatalf("invalid entry %+v: want %v, got %v", fields, want, err)
			}
		}

		if err := library.Delete(ctx, logo); !errors.Is(err, media.ErrInUse) {
			t.Fatalf("delete used logo: %v", err)
		}

		// A category with entries cannot be deleted, so they cannot vanish.
		err = service.categories.Delete(ctx, "guide", food.ID)
		if !errors.Is(err, errCategoryUsed) || !strings.Contains(categoryIssue(err), "still has entries") {
			t.Fatalf("delete used category: %v", err)
		}

		kept, err := service.entries.Get(ctx, "guide", bakery.ID)
		if err != nil || kept.Fields.Category != food.ID {
			t.Fatalf("entry of used category: %+v %v", kept.Fields, err)
		}

		testImport(t, service)
	})
}

func testImport(t *testing.T, service *Service) {
	t.Helper()

	ctx := context.Background()
	csv := "\xef\xbb\xbfName;Category;Summary;Website;Keywords;City\n" +
		"Café Noir;Food & Drinks;Coffee and cake;cafe.example;coffee, cake;Amsterdam\n" +
		";Food & Drinks;No name\n" +
		"Library;;No category\n" +
		"Plain;Food & Drinks;Already there\n" +
		"Bad site;Food & Drinks;x;not a site;;\n" +
		"\n" +
		"Town hall;Services;\"Opening; by appointment\";https://town.example;;Amsterdam\n"

	result, err := service.Import(ctx, strings.NewReader(csv), ImportOptions{Locale: "en", Publish: true})
	if err != nil {
		t.Fatal(err)
	}

	if result.Created != 2 || result.Skipped != 4 || result.Categories != 2 || len(result.Problems) != 4 {
		t.Fatalf("import: %+v", result)
	}

	if result.Problems[0].Row != 3 || result.Problems[2].Reason != "An entry with this name already exists." ||
		result.Problems[3].Row != 6 {
		t.Fatalf("problems: %+v", result.Problems)
	}

	published, err := service.entries.ListPublished(ctx, "guide", "en")
	if err != nil {
		t.Fatal(err)
	}

	var cafe EntryItem

	for _, item := range published {
		if item.Fields.Slug == "cafe-noir" {
			cafe = item
		}
	}

	text, _ := cafe.Text("en")
	if cafe.Fields.Website != "https://cafe.example" || cafe.Fields.City != "Amsterdam" || text.Keywords != "coffee, cake" {
		t.Fatalf("imported entry: %+v %+v", cafe.Fields, text)
	}

	// A second run updates existing entries when asked, and finds the
	// categories it created by name.
	update := "name,category,summary\nCafé Noir,food & drinks,Coffee only\n"

	result, err = service.Import(ctx, strings.NewReader(update), ImportOptions{Locale: "en", Update: true})
	if err != nil || result.Updated != 1 || result.Categories != 0 {
		t.Fatalf("update import: %+v %v", result, err)
	}

	updated, err := service.entries.Get(ctx, "guide", cafe.ID)
	if err != nil {
		t.Fatal(err)
	}

	text, _ = updated.Text("en")
	if text.Summary != "Coffee only" || text.Keywords != "coffee, cake" || updated.Fields.City != "Amsterdam" {
		t.Fatalf("updated entry: %+v %+v", updated.Fields, text)
	}

	// Publishing an import never publishes an existing category that an
	// editor keeps hidden; the new entry is published but stays off the site
	// until the category is.
	result, err = service.Import(
		ctx,
		strings.NewReader("name,category\nCorner bakery,Food\n"),
		ImportOptions{Locale: "en", Publish: true},
	)
	if err != nil || result.Created != 1 || result.Categories != 0 {
		t.Fatalf("import into hidden category: %+v %v", result, err)
	}

	hidden, err := service.categories.ListPublished(ctx, "guide", "en")
	if err != nil {
		t.Fatal(err)
	}

	for _, category := range hidden {
		if category.Fields.Slug == "food" {
			t.Fatal("import published an existing category")
		}
	}

	if _, err := service.Import(
		ctx,
		strings.NewReader("title,body\nx,y\n"),
		ImportOptions{Locale: "en"},
	); !errors.Is(
		err,
		errHeader,
	) {
		t.Fatalf("missing columns: %v", err)
	}
}

func newTestService(t *testing.T, database *db.DB) *Service {
	t.Helper()

	entries, err := content.NewCollection(database, entrySpec())
	if err != nil {
		t.Fatal(err)
	}

	categories, err := content.NewCollection(database, deletableCategories(database, categorySpec()))
	if err != nil {
		t.Fatal(err)
	}

	return &Service{key: "guide", entries: entries, categories: categories}
}

func upload(t *testing.T, library *media.Service) string {
	t.Helper()

	var buffer bytes.Buffer

	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}

	asset, err := library.Upload(context.Background(), "logo.png", &buffer)
	if err != nil {
		t.Fatal(err)
	}

	return asset.ID
}
