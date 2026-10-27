package frontend

import (
	"context"
	"html/template"
	"strings"
	"sync"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/markdown"
)

// renderedCache keeps Markdown rendered to HTML. Page bodies and module
// texts are rendered on every request although published revisions never
// change, so the same source is rendered again and again.
//
// The source itself is the key, so a new revision or a draft in preview is
// simply another entry and nothing goes out of date when content changes.
// Only media change what a source renders to: their URLs, sizes, focal
// points and alt texts (the latter per locale). A source that refers to
// media is therefore kept with its locale and the media library's version
// (see media.Service.Version), which every change in this process moves on,
// and for at most renderedTTL, the time after which the library itself reads
// changes by other processes again. The cache holds at most maxRenderedBytes
// of sources and HTML and starts over when full, like the media library's.
type renderedCache struct {
	mu      sync.Mutex
	entries map[renderedKey]rendered
	size    int
}

type renderedKey struct {
	// locale is empty for sources without media, which render the same in
	// every language.
	locale string
	source string
}

type rendered struct {
	html template.HTML
	// version is the media library's version the HTML was rendered with and
	// expires when it is read again; both are zero for sources without
	// media.
	version uint64
	expires time.Time
}

const (
	// maxRenderedBytes bounds the memory of the cache; maxRenderedSource
	// leaves out sources too large to be worth keeping.
	maxRenderedBytes  = 32 << 20
	maxRenderedSource = 1 << 20
	renderedTTL       = time.Minute
)

// load returns the HTML of key rendered with media version, when known.
func (c *renderedCache) load(key renderedKey, version uint64, now time.Time) (template.HTML, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		return "", false
	}

	if !entry.expires.IsZero() && (entry.version != version || !now.Before(entry.expires)) {
		return "", false
	}

	return entry.html, true
}

// store keeps the HTML of key.
func (c *renderedCache) store(key renderedKey, entry rendered) {
	size := len(key.source) + len(entry.html)
	if size > maxRenderedSource {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if previous, ok := c.entries[key]; ok {
		c.size -= len(key.source) + len(previous.html)
	}

	if c.entries == nil || c.size+size > maxRenderedBytes {
		c.entries = map[renderedKey]rendered{}
		c.size = 0
	}

	c.entries[key] = entry
	c.size += size
}

// markdown renders source in locale with media resolved, from the cache
// when it was rendered before (see renderedCache).
func (f *Frontend) markdown(ctx context.Context, locale, source string) template.HTML {
	if source == "" {
		return ""
	}

	// Inside a transaction media may hold changes that are not committed
	// yet, which other requests must not see.
	if db.InTx(ctx) {
		return f.renderMarkdown(ctx, locale, source)
	}

	key := renderedKey{source: source}
	entry := rendered{}
	now := time.Now()

	if strings.Contains(source, markdown.MediaPrefix) {
		key.locale = locale
		entry.version = f.media.Version()
		entry.expires = now.Add(renderedTTL)
	}

	if html, ok := f.rendered.load(key, entry.version, now); ok {
		return html
	}

	entry.html = f.renderMarkdown(ctx, locale, source)
	f.rendered.store(key, entry)

	return entry.html
}
