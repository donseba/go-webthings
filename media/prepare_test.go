package media_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"
)

// TestPrepare checks that uploaded images get the variants pages show in
// the background, and that Prepare stops when its context ends.
func TestPrepare(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		storage, err := media.NewFilesystem(t.TempDir(), "")
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = storage.Close() })

		library := media.New(database, storage)
		serve(library)

		// Uploads do not wait for Prepare: they queue until it runs.
		large, err := library.Upload(ctx, "large.png", bytes.NewReader(photo(t, 1700, 100)))
		if err != nil {
			t.Fatal(err)
		}

		small, err := library.Upload(ctx, "small.png", bytes.NewReader(photo(t, 600, 40)))
		if err != nil {
			t.Fatal(err)
		}

		animation, err := library.Upload(ctx, "animation.gif", bytes.NewReader(still(t)))
		if err != nil {
			t.Fatal(err)
		}

		if exists(ctx, storage, large.ID+"_1600x0.webp") {
			t.Fatal("variants made before Prepare runs")
		}

		running, stop := context.WithCancel(ctx)
		stopped := make(chan struct{})

		go func() {
			library.Prepare(running)
			close(stopped)
		}()

		waiting, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if err := library.WaitPrepared(waiting); err != nil {
			t.Fatal(err)
		}

		// The copy pages show and its srcset are stored, never larger than
		// the image; the upload itself stays.
		for _, key := range []string{
			large.ID + "_1600x0.webp", large.ID + "_1200x0.webp", large.ID + "_800x0.webp", large.ID + "_480x0.webp",
			small.ID + "_600x0.webp", small.ID + "_480x0.webp", large.StorageKey, small.StorageKey,
		} {
			if !exists(ctx, storage, key) {
				t.Fatalf("%s is not stored", key)
			}
		}

		var keys []string

		err = database.Select(ctx, &keys, `SELECT storage_key FROM wt_media_variant WHERE asset_id=?`, animation.ID)
		if err != nil || len(keys) != 0 {
			t.Fatalf("variants of a GIF: %v %v", keys, err)
		}

		// The variants are those pages ask for.
		shown := library.Image(ctx, large.ID, media.ImageWidth)

		err = database.Select(ctx, &keys, `SELECT storage_key FROM wt_media_variant WHERE asset_id=?`, large.ID)
		if err != nil || len(keys) != 4 || !strings.HasSuffix(shown.URL, large.ID+"_1600x0.webp") {
			t.Fatalf("variants: %v %v", keys, err)
		}

		// An upload while it runs is prepared as well.
		later, err := library.Upload(ctx, "later.png", bytes.NewReader(photo(t, 900, 40)))
		if err != nil {
			t.Fatal(err)
		}

		if err := library.WaitPrepared(waiting); err != nil {
			t.Fatal(err)
		}

		if !exists(ctx, storage, later.ID+"_900x0.webp") {
			t.Fatal("an upload while Prepare runs is not prepared")
		}

		stop()

		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			t.Fatal("Prepare does not stop")
		}

		// Without Prepare, uploads still go through, and waiting stops with
		// its context.
		if _, err := library.Upload(ctx, "after.png", bytes.NewReader(photo(t, 100, 50))); err != nil {
			t.Fatal(err)
		}

		expired, cancelExpired := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancelExpired()

		if err := library.WaitPrepared(expired); err == nil {
			t.Fatal("waiting for an image nobody prepares")
		}
	})
}

// exists reports whether storage has a file under key.
func exists(ctx context.Context, storage media.Storage, key string) bool {
	file, err := storage.Open(ctx, key)
	if err != nil {
		return false
	}

	_, _ = io.Copy(io.Discard, file)
	_ = file.Close()

	return true
}
