package site_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/site"
)

// TestSecretCached checks that a secret is created once, kept by every process of
// the site, and cannot be changed through the slice Secret returns.
func TestSecretCached(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "site", site.Schema()); err != nil {
			t.Fatal(err)
		}

		first := site.New(database)

		key, err := first.Secret(ctx, "visitors")
		if err != nil || len(key) != 32 {
			t.Fatalf("secret: %x %v", key, err)
		}

		original := bytes.Clone(key)
		key[0]++

		again, _ := first.Secret(ctx, "visitors")
		other, _ := site.New(database).Secret(ctx, "visitors")
		different, _ := first.Secret(ctx, "variants")

		if !bytes.Equal(again, original) || !bytes.Equal(other, original) || bytes.Equal(different, original) {
			t.Fatalf("secrets: %x %x %x %x", original, again, other, different)
		}
	})
}
