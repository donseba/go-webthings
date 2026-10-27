// Package cache keeps one value read from the database in memory for a short
// time. Writes in this process invalidate it at once; writes by other
// processes show up when the time to live has passed.
//
// Reading is lock free: a hit costs two atomic loads. A value loaded while a
// write invalidated the cache is never kept, because every entry remembers the
// generation it was loaded in.
package cache

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/donseba/go-webthings/db"
)

// DefaultTTL is how long a value is reused when nothing else is configured.
const DefaultTTL = 30 * time.Second

// Value caches the result of a load function. The zero value caches for
// DefaultTTL; it is safe for concurrent use.
type Value[T any] struct {
	// ttl is the time to live in nanoseconds; 0 means DefaultTTL, negative
	// disables the cache.
	ttl        atomic.Int64
	generation atomic.Uint64
	current    atomic.Pointer[entry[T]]
	// now is the clock; nil means time.Now. Tests replace it to let values
	// expire without waiting.
	now func() time.Time
}

type entry[T any] struct {
	value      T
	generation uint64
	expires    time.Time
}

// SetTTL changes how long values are kept; zero or less disables caching.
func (v *Value[T]) SetTTL(ttl time.Duration) {
	if ttl <= 0 {
		ttl = -1
	}

	v.ttl.Store(int64(ttl))
	v.Invalidate()
}

// SetClock replaces the clock that decides when values expire, so tests can
// move time forward instead of sleeping. Call it before the value is used.
func (v *Value[T]) SetClock(now func() time.Time) {
	v.now = now
}

func (v *Value[T]) clock() time.Time {
	if v.now == nil {
		return time.Now()
	}

	return v.now()
}

func (v *Value[T]) duration() time.Duration {
	ttl := time.Duration(v.ttl.Load())
	if ttl == 0 {
		return DefaultTTL
	}

	return ttl
}

// Get returns the cached value, or loads and keeps a new one. Inside a
// transaction it always loads and keeps nothing: the transaction must see
// its own changes, which are not committed and so not in the cache, and
// others must not see them before the commit.
func (v *Value[T]) Get(ctx context.Context, load func(context.Context) (T, error)) (T, error) {
	if db.InTx(ctx) {
		return load(ctx)
	}

	generation := v.generation.Load()

	if current := v.current.Load(); current != nil && current.generation == generation && v.clock().Before(current.expires) {
		return current.value, nil
	}

	value, err := load(ctx)
	if err != nil || v.duration() < 0 {
		return value, err
	}

	v.current.Store(&entry[T]{value: value, generation: generation, expires: v.clock().Add(v.duration())})

	return value, nil
}

// Generation counts the invalidations (see Invalidate): it changes with
// every write in this process, and only goes up, so what is made from the
// value, such as a rendered page, can be kept with the generation it was
// made at and made again once it moved on. Values loaded again when the
// time to live has passed do not change it.
func (v *Value[T]) Generation() uint64 {
	return v.generation.Load()
}

// Invalidate drops the cached value; call it after every committed write.
func (v *Value[T]) Invalidate() {
	v.generation.Add(1)
	v.current.Store(nil)
}
