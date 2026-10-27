package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/internal/cache"
)

func TestValue(t *testing.T) {
	ctx := context.Background()
	loads := 0
	load := func(context.Context) (int, error) {
		loads++

		return loads, nil
	}

	// A fake clock decides expiry, so a slow run cannot expire a value
	// between two reads and the test needs no sleep.
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

	var value cache.Value[int]

	value.SetClock(func() time.Time {
		return now
	})
	value.SetTTL(50 * time.Millisecond)

	first, _ := value.Get(ctx, load)
	second, _ := value.Get(ctx, load)

	if first != 1 || second != 1 {
		t.Fatalf("cached value not reused: %d %d", first, second)
	}

	// A write in this process applies at once.
	value.Invalidate()

	if got, _ := value.Get(ctx, load); got != 2 {
		t.Fatalf("after invalidate: %d", got)
	}

	// A write elsewhere shows up when the time to live has passed.
	now = now.Add(49 * time.Millisecond)

	if got, _ := value.Get(ctx, load); got != 2 {
		t.Fatalf("expired before the ttl: %d", got)
	}

	now = now.Add(2 * time.Millisecond)

	if got, _ := value.Get(ctx, load); got != 3 {
		t.Fatalf("after ttl: %d", got)
	}

	// A value loaded while a write invalidated the cache is not kept.
	value.Invalidate()

	stale := func(context.Context) (int, error) {
		value.Invalidate()

		return -1, nil
	}

	if got, _ := value.Get(ctx, stale); got != -1 {
		t.Fatalf("stale load: %d", got)
	}

	if got, _ := value.Get(ctx, load); got != 4 {
		t.Fatalf("stale value kept: %d", got)
	}

	// Zero or less disables caching.
	value.SetTTL(0)

	a, _ := value.Get(ctx, load)
	b, _ := value.Get(ctx, load)

	if a == b {
		t.Fatal("disabled cache reused a value")
	}
}

func TestValueInTransaction(t *testing.T) {
	database := dbtest.SQLite(t)
	loads := 0
	load := func(context.Context) (int, error) {
		loads++

		return loads, nil
	}

	var value cache.Value[int]

	if got, _ := value.Get(context.Background(), load); got != 1 {
		t.Fatalf("first load: %d", got)
	}

	// Inside a transaction the cached value may be older than the
	// transaction's own changes, and a value loaded there may never be
	// committed: it loads every time and keeps nothing.
	err := database.Tx(context.Background(), func(ctx context.Context) error {
		got, err := value.Get(ctx, load)
		if got != 2 {
			t.Errorf("cached value read in a transaction: %d", got)
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	if got, _ := value.Get(context.Background(), load); got != 1 {
		t.Fatalf("value from a transaction kept: %d", got)
	}
}
