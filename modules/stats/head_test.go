package stats

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"strings"
	"testing"
)

// TestPlainHead checks that the start of a compressed page, cut off as the
// recorder keeps it, is read as the page's start.
func TestPlainHead(t *testing.T) {
	page := "<html><head><title>About · Site</title></head><body>" + strings.Repeat("<p>text and more text</p>", 5000)

	var compressed bytes.Buffer

	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte(page))
	_ = writer.Close()

	head := compressed.Bytes()[:min(compressed.Len(), maxHead)]

	for range 2 {
		plain := plainHead(http.Header{"Content-Encoding": {"gzip"}}, head)
		if !strings.HasPrefix(string(plain), "<html><head><title>About · Site</title>") || len(plain) > maxHead {
			t.Fatalf("compressed head: %d bytes: %.80s", len(plain), plain)
		}
	}

	if plain := plainHead(http.Header{}, []byte(page[:100])); string(plain) != page[:100] {
		t.Fatalf("plain head: %s", plain)
	}

	if plain := plainHead(http.Header{"Content-Encoding": {"gzip"}}, []byte("not gzip")); plain != nil {
		t.Fatalf("broken head: %s", plain)
	}
}
