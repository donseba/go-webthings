package frontend

import (
	"strings"
	"testing"
	"time"
)

// TestRenderedCache checks when kept HTML is used: always for sources
// without media, and for sources with media only with the media version
// it was rendered with and before it expires; and that the cache stays
// within its size.
func TestRenderedCache(t *testing.T) {
	var cache renderedCache

	now := time.Now()
	plain := renderedKey{source: "plain"}
	withMedia := renderedKey{locale: "en", source: "![](media:x)"}

	cache.store(plain, rendered{html: "<p>plain</p>"})
	cache.store(withMedia, rendered{html: "<img>", version: 3, expires: now.Add(time.Minute)})

	if html, ok := cache.load(plain, 9, now.Add(time.Hour)); !ok || html != "<p>plain</p>" {
		t.Fatalf("plain: %q %v", html, ok)
	}

	if html, ok := cache.load(withMedia, 3, now); !ok || html != "<img>" {
		t.Fatalf("media: %q %v", html, ok)
	}

	if _, ok := cache.load(withMedia, 4, now); ok {
		t.Fatal("kept HTML used after a media change")
	}

	if _, ok := cache.load(withMedia, 3, now.Add(time.Minute)); ok {
		t.Fatal("kept HTML used after it expired")
	}

	if _, ok := cache.load(renderedKey{locale: "nl", source: withMedia.source}, 3, now); ok {
		t.Fatal("kept HTML used in another language")
	}

	// Storing a key again replaces it without counting it twice.
	size := cache.size
	cache.store(plain, rendered{html: "<p>plain</p>"})

	if cache.size != size {
		t.Fatalf("size after storing again: %d, want %d", cache.size, size)
	}

	// Sources too large to keep are left out; a full cache starts over.
	cache.store(renderedKey{source: strings.Repeat("x", maxRenderedSource)}, rendered{html: "x"})

	if len(cache.entries) != 2 {
		t.Fatalf("entries after a large source: %d", len(cache.entries))
	}

	big := strings.Repeat("y", maxRenderedSource/2)
	for i := range 2 * maxRenderedBytes / len(big) {
		cache.store(renderedKey{source: big + string(rune('a'+i%26)) + strings.Repeat("z", i)}, rendered{html: "y"})

		if cache.size > maxRenderedBytes {
			t.Fatalf("cache size %d over %d", cache.size, maxRenderedBytes)
		}
	}
}
