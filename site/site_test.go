package site_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/site"
)

func TestInitAndSave(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "site", site.Schema()); err != nil {
			t.Fatal(err)
		}

		settings := site.New(database)

		if err := settings.Init(ctx, "My site", locales.Config{Default: "nl", Enabled: []string{"nl", "en"}}); err != nil {
			t.Fatal(err)
		}

		current, err := settings.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if current.DefaultLocale != "nl" || len(current.Locales) != 2 || current.Text("en").Title != "My site" {
			t.Fatalf("initial settings: %+v", current)
		}

		current.Translations = current.WithText(site.Translation{Locale: "en", Title: "My website", Tagline: "Hello"})
		current.Timezone = "Europe/Amsterdam"

		if err := settings.Save(ctx, current); err != nil {
			t.Fatal(err)
		}

		// Init does not overwrite saved settings.
		if err := settings.Init(ctx, "Other", locales.Config{Default: "en", Enabled: []string{"en"}}); err != nil {
			t.Fatal(err)
		}

		config, err := settings.Configuration(ctx)
		if err != nil || config.Default != "nl" {
			t.Fatalf("configuration: %+v %v", config, err)
		}

		saved, _ := settings.Settings(ctx)
		if saved.Text("en").Tagline != "Hello" || saved.Timezone != "Europe/Amsterdam" {
			t.Fatalf("saved: %+v", saved)
		}

		// Feeds list 20 items unless the settings say otherwise, at most
		// 100.
		if saved.FeedLimit() != site.DefaultFeedSize {
			t.Fatalf("feed limit %d", saved.FeedLimit())
		}

		saved.FeedSize = site.MaxFeedSize + 1
		if err := settings.Save(ctx, saved); !errors.Is(err, site.ErrInvalid) {
			t.Fatalf("feed size over the maximum: %v", err)
		}

		saved.FeedSize = 50
		if err := settings.Save(ctx, saved); err != nil {
			t.Fatal(err)
		}

		saved, _ = settings.Settings(ctx)
		if saved.FeedLimit() != 50 {
			t.Fatalf("saved feed limit %d", saved.FeedLimit())
		}

		saved.LogoMediaID = db.NewID()
		if err := settings.Save(ctx, saved); !errors.Is(err, media.ErrNotFound) {
			t.Fatalf("want missing media, got %v", err)
		}

		saved.LogoMediaID = ""
		saved.Locales = []string{"en"}

		if err := settings.Save(ctx, saved); !errors.Is(err, site.ErrInvalid) {
			t.Fatalf("default locale must stay enabled: %v", err)
		}
	})
}

// TestReservedLocales checks that a reserved code, such as the first
// segment of the admin's path, cannot be enabled as a language, whatever
// its case, on first start or later.
func TestReservedLocales(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "site", site.Schema()); err != nil {
			t.Fatal(err)
		}

		settings := site.New(database)
		settings.ReserveLocales("NL")

		err := settings.Init(ctx, "My site", locales.Config{Default: "en", Enabled: []string{"en", "nl"}})
		if !errors.Is(err, site.ErrReservedLocale) {
			t.Fatalf("first start with a reserved language: %v", err)
		}

		if err := settings.Init(ctx, "My site", locales.Config{Default: "en", Enabled: []string{"en"}}); err != nil {
			t.Fatal(err)
		}

		current, err := settings.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}

		current.Locales = append(current.Locales, "nl")
		if err := settings.Save(ctx, current); !errors.Is(err, site.ErrReservedLocale) {
			t.Fatalf("enabling a reserved language: %v", err)
		}

		if code, reserved := settings.ReservedLocale([]string{"en", "nl"}); !reserved || code != "nl" {
			t.Fatalf("reserved locale: %q %v", code, reserved)
		}
	})
}

func TestThemeValues(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		for owner, files := range map[string]fs.FS{"media": media.Schema(), "site": site.Schema()} {
			if err := database.ApplySchema(ctx, owner, files); err != nil {
				t.Fatal(err)
			}
		}

		settings := site.New(database)

		if err := settings.Init(ctx, "My site", locales.Config{Default: "en", Enabled: []string{"en"}}); err != nil {
			t.Fatal(err)
		}

		if err := settings.SaveThemeValues(ctx, "dark", map[string]string{"accent": "#000000", "empty": ""}); err != nil {
			t.Fatal(err)
		}

		if err := settings.SetTheme(ctx, "dark"); err != nil {
			t.Fatal(err)
		}

		current, err := settings.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}

		// Theme values are neither a language nor lost when settings are saved.
		if current.Theme != "dark" || len(current.Translations) != 1 {
			t.Fatalf("settings: %+v", current)
		}

		values, err := settings.ThemeValues(ctx, "dark")
		if err != nil {
			t.Fatal(err)
		}

		if len(values) != 1 || values["accent"] != "#000000" {
			t.Fatalf("values: %v", values)
		}
	})
}

// TestSnapshot checks that a request reads one snapshot of the settings
// throughout, also when they are saved meanwhile, and that a save makes a
// new snapshot rather than changing the one requests read.
func TestSnapshot(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		for owner, files := range map[string]fs.FS{"media": media.Schema(), "site": site.Schema()} {
			if err := database.ApplySchema(ctx, owner, files); err != nil {
				t.Fatal(err)
			}
		}

		settings := site.New(database)

		if err := settings.Init(ctx, "My site", locales.Config{Default: "en", Enabled: []string{"en"}}); err != nil {
			t.Fatal(err)
		}

		if err := settings.SaveThemeValues(ctx, "dark", map[string]string{"accent": "#000000"}); err != nil {
			t.Fatal(err)
		}

		request := settings.WithSnapshot(ctx)

		before, err := settings.Snapshot(request)
		if err != nil {
			t.Fatal(err)
		}

		// Without copies: the cached snapshot itself.
		if current, _ := settings.Snapshot(ctx); current != before {
			t.Fatal("snapshot is not the cached one")
		}

		changed, err := settings.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}

		changed.Email = "new@example.com"

		if err := settings.Save(ctx, changed); err != nil {
			t.Fatal(err)
		}

		if err := settings.SaveThemeValues(ctx, "dark", map[string]string{"accent": "#ffffff"}); err != nil {
			t.Fatal(err)
		}

		// The request still reads what it started with; the snapshot it
		// has did not change.
		during, err := settings.Snapshot(request)
		if err != nil || during != before || during.Settings().Email != "" || during.ThemeValues("dark")["accent"] != "#000000" {
			t.Fatalf("during the request: %+v %v", during, err)
		}

		// A new request, and Settings, read the saved settings.
		after, err := settings.Snapshot(settings.WithSnapshot(ctx))
		if err != nil || after == before || after.Settings().Email != "new@example.com" ||
			after.ThemeValues("dark")["accent"] != "#ffffff" {
			t.Fatalf("after the save: %+v %v", after, err)
		}

		if current, err := settings.Settings(request); err != nil || current.Email != "new@example.com" {
			t.Fatalf("settings: %+v %v", current, err)
		}
	})
}

// TestCache checks that settings are read from memory, that saves apply at
// once and that changes by another process show up after the time to live.
func TestCache(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		for owner, files := range map[string]fs.FS{"media": media.Schema(), "site": site.Schema()} {
			if err := database.ApplySchema(ctx, owner, files); err != nil {
				t.Fatal(err)
			}
		}

		// A fake clock decides when cached settings expire, so the test
		// needs no sleep and a slow run cannot expire them early.
		now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

		settings := site.New(database)
		site.SetClock(settings, func() time.Time {
			return now
		})
		settings.SetCacheTTL(200 * time.Millisecond)

		if err := settings.Init(ctx, "My site", locales.Config{Default: "en", Enabled: []string{"en"}}); err != nil {
			t.Fatal(err)
		}

		current, err := settings.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}

		// Repeated reads need no query.
		before := database.Statements()

		for range 5 {
			if _, err := settings.Settings(ctx); err != nil {
				t.Fatal(err)
			}

			if _, err := settings.ThemeValues(ctx, "default"); err != nil {
				t.Fatal(err)
			}
		}

		if queries := database.Statements() - before; queries != 0 {
			t.Fatalf("cached reads ran %d queries", queries)
		}

		// Callers cannot change the cached copy.
		current.Locales[0] = "xx"

		if again, _ := settings.Settings(ctx); again.Locales[0] != "en" {
			t.Fatalf("cache changed through a copy: %v", again.Locales)
		}

		// Another process changes the title behind this service's back.
		_, err = database.Exec(ctx, `UPDATE wt_setting SET value=? WHERE scope=? AND name=?`, "Elsewhere", "en", "title")
		if err != nil {
			t.Fatal(err)
		}

		if cached, _ := settings.Settings(ctx); cached.Text("en").Title != "My site" {
			t.Fatalf("change seen before the time to live: %q", cached.Text("en").Title)
		}

		now = now.Add(250 * time.Millisecond)

		fresh, err := settings.Settings(ctx)
		if err != nil || fresh.Text("en").Title != "Elsewhere" {
			t.Fatalf("change not seen after the time to live: %q %v", fresh.Text("en").Title, err)
		}

		// A save in this process applies at once.
		fresh.Translations = fresh.WithText(site.Translation{Locale: "en", Title: "Saved"})
		if err := settings.Save(ctx, fresh); err != nil {
			t.Fatal(err)
		}

		if saved, _ := settings.Settings(ctx); saved.Text("en").Title != "Saved" {
			t.Fatalf("save not applied: %q", saved.Text("en").Title)
		}

		if err := settings.SaveThemeValues(ctx, "default", map[string]string{"accent": "#123456"}); err != nil {
			t.Fatal(err)
		}

		if values, _ := settings.ThemeValues(ctx, "default"); values["accent"] != "#123456" {
			t.Fatalf("theme values not applied: %v", values)
		}
	})
}

func TestSecret(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		for owner, files := range map[string]fs.FS{"media": media.Schema(), "site": site.Schema()} {
			if err := database.ApplySchema(ctx, owner, files); err != nil {
				t.Fatal(err)
			}
		}

		settings := site.New(database)

		if err := settings.Init(ctx, "My site", locales.Config{Default: "en", Enabled: []string{"en"}}); err != nil {
			t.Fatal(err)
		}

		key, err := settings.Secret(ctx, "test")
		if err != nil || len(key) != 32 {
			t.Fatalf("secret: %x %v", key, err)
		}

		// Another process gets the same key, and saving settings keeps it.
		current, _ := settings.Settings(ctx)
		if err := settings.Save(ctx, current); err != nil {
			t.Fatal(err)
		}

		again, err := site.New(database).Secret(ctx, "test")
		if err != nil || !bytes.Equal(key, again) {
			t.Fatalf("secret changed: %x %x %v", key, again, err)
		}

		// Secrets are not a language.
		if current, _ := settings.Settings(ctx); len(current.Translations) != 1 {
			t.Fatalf("secret shown as a translation: %+v", current.Translations)
		}
	})
}

// TestStoredValues checks how settings are stored: booleans and numbers are
// typed, an empty list reads back empty, keys this version does not know
// survive a save, and the timezone is parsed once.
func TestStoredValues(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "site", site.Schema()); err != nil {
			t.Fatal(err)
		}

		settings := site.New(database)
		settings.SetCacheTTL(0)

		if err := settings.Init(ctx, "My site", locales.Config{Default: "en", Enabled: []string{"en"}}); err != nil {
			t.Fatal(err)
		}

		// Values written by a newer version, site-wide and per language.
		for _, scope := range []string{"", "en"} {
			_, err := database.Exec(ctx,
				`INSERT INTO wt_setting(scope,name,value,updated_at,updated_by) VALUES(?,?,?,?,?)`,
				scope, "future_setting", "kept", time.Now().UTC(), db.Actor(ctx),
			)
			if err != nil {
				t.Fatal(err)
			}
		}

		current, err := settings.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if current.Review != nil || !current.Compress() || current.Location() != time.UTC {
			t.Fatalf("defaults: review %q, compress %v, location %v", current.Review, current.Compress(), current.Location())
		}

		current.CompressionOff = true
		current.PageMaxAge = 300
		current.Timezone = "Europe/Amsterdam"

		if err := settings.Save(ctx, current); err != nil {
			t.Fatal(err)
		}

		saved, err := settings.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}

		maxAge, stale := saved.PageCache()
		if saved.Compress() || maxAge != 300 || stale != 0 || saved.Location().String() != "Europe/Amsterdam" {
			t.Fatalf("saved: %+v", saved)
		}

		var kept int

		err = database.Get(ctx, &kept, `SELECT COUNT(*) FROM wt_setting WHERE name='future_setting' AND value='kept'`)
		if err != nil || kept != 2 {
			t.Fatalf("unknown keys after a save: %d %v", kept, err)
		}

		saved.PageStale = -1
		if err := settings.Save(ctx, saved); !errors.Is(err, site.ErrInvalid) {
			t.Fatalf("negative cache time: %v", err)
		}
	})
}

func TestLocation(t *testing.T) {
	if location := (site.Settings{}).Location(); location != time.UTC {
		t.Fatalf("no timezone: %v", location)
	}

	if location := (site.Settings{Timezone: "Nowhere/Unknown"}).Location(); location != time.UTC {
		t.Fatalf("unknown timezone: %v", location)
	}

	if location := (site.Settings{Timezone: "Europe/Amsterdam"}).Location(); location.String() != "Europe/Amsterdam" {
		t.Fatalf("timezone: %v", location)
	}
}
