package site_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/site"
)

// TestProfiles stores social media profiles with the settings: completed,
// in the order of site.Networks, and validated.
func TestProfiles(t *testing.T) {
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

		current, err := settings.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}

		current.Profiles = []site.Profile{
			{Network: "mastodon", URL: "https://mastodon.social/@me"},
			{Network: "instagram", URL: " instagram.com/me "},
			{Network: "github", URL: ""},
		}

		if err := settings.Save(ctx, current); err != nil {
			t.Fatal(err)
		}

		saved, err := settings.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}

		want := []site.Profile{
			{Network: "instagram", URL: "https://instagram.com/me"},
			{Network: "mastodon", URL: "https://mastodon.social/@me"},
		}

		if len(saved.Profiles) != len(want) || saved.Profiles[0] != want[0] || saved.Profiles[1] != want[1] {
			t.Fatalf("profiles: %+v", saved.Profiles)
		}

		if saved.Profile("mastodon") != "https://mastodon.social/@me" || saved.Profile("github") != "" {
			t.Fatalf("profile lookup: %+v", saved.Profiles)
		}

		// Saving other settings keeps the profiles; an empty list removes them.
		saved.Phone = "0123"
		if err := settings.Save(ctx, saved); err != nil {
			t.Fatal(err)
		}

		if again, _ := settings.Settings(ctx); len(again.Profiles) != 2 {
			t.Fatalf("profiles lost: %+v", again.Profiles)
		}

		invalid := [][]site.Profile{
			{{Network: "myspace", URL: "https://myspace.com/me"}},
			{{Network: "x", URL: "javascript:alert(1)"}},
			{{Network: "x", URL: "https://user:pass@x.com/"}},
			{{Network: "x", URL: "https://x.com/a"}, {Network: "x", URL: "https://x.com/b"}},
		}

		for _, profiles := range invalid {
			saved.Profiles = profiles
			if err := settings.Save(ctx, saved); !errors.Is(err, site.ErrProfile) {
				t.Fatalf("%+v: want ErrProfile, got %v", profiles, err)
			}
		}

		saved.Profiles = nil
		if err := settings.Save(ctx, saved); err != nil {
			t.Fatal(err)
		}

		if again, _ := settings.Settings(ctx); len(again.Profiles) != 0 {
			t.Fatalf("profiles not removed: %+v", again.Profiles)
		}

		if site.NetworkName("linkedin") != "LinkedIn" || site.NetworkName("email") != "E-mail" ||
			site.NetworkName("new") != "new" {
			t.Fatal("network names")
		}
	})
}
