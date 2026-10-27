package frontend

import (
	"net/http"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-router/middleware"
)

// DefaultSearchThrottle is how many searches one visitor may run per minute
// on the search screen (see SetSearchThrottle).
const DefaultSearchThrottle = 30

// SetSearchThrottle allows each visitor (by ClientIP) at most limit
// searches per window on the search screen; more get the search page with
// SearchView.Throttled and status 429 Too Many Requests. A search reads all published content, so a visitor sending
// queries in a loop would keep the site busy. Visiting the screen without
// a query is not counted, nor is the {{search}} template function, whose
// queries the site's templates choose. Searches are counted in memory, per
// process. Zero or less turns the limit off; the default is
// DefaultSearchThrottle searches per minute.
func (f *Frontend) SetSearchThrottle(limit int, window time.Duration) {
	f.searches = middleware.NewLimiter(limit, window)
}

// searchAllowed counts a search of the visitor behind r and reports whether
// it is within the limit (see SetSearchThrottle).
func (f *Frontend) searchAllowed(r *http.Request) bool {
	return f.searches.Allow(ClientIP(r), time.Now())
}

// pageTexts keeps what the search needs of published page revisions: the
// plain text of the body and the folded texts that Query.Score compares.
// Revisions never change, so an entry stays right as long as the revision
// is published; turning Markdown into text and folding it were most of the
// work of every search. The cache holds at most maxPageTextBytes and starts
// over when full.
type pageTexts struct {
	mu      sync.Mutex
	entries map[pageTextKey]pageText
	size    int
}

type pageTextKey struct {
	revision string
	locale   string
}

type pageText struct {
	// body is the body as plain text, for summaries.
	body string
	// title and text are the title and the description and body, folded
	// (see fold).
	title string
	text  string
}

const maxPageTextBytes = 16 << 20

// get returns the search texts of a published page revision in locale.
func (c *pageTexts) get(page pages.Page, locale string) (pages.Text, pageText) {
	text, _ := page.Text(locale)
	key := pageTextKey{revision: page.Revision.ID, locale: locale}

	c.mu.Lock()
	entry, ok := c.entries[key]
	c.mu.Unlock()

	if ok {
		return text, entry
	}

	body := markdown.Text(text.Body)
	entry = pageText{
		body:  body,
		title: foldString(text.Title),
		text:  foldString(text.Description + " " + body),
	}

	size := len(entry.body) + len(entry.title) + len(entry.text)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries == nil || c.size+size > maxPageTextBytes {
		c.entries = map[pageTextKey]pageText{}
		c.size = 0
	}

	if _, ok := c.entries[key]; !ok {
		c.entries[key] = entry
		c.size += size
	}

	return text, entry
}

// foldString is fold without the offsets, for comparing.
func foldString(text string) string {
	var b []byte

	for _, r := range text {
		if r < utf8.RuneSelf {
			b = append(b, byte(unicode.ToLower(r)))

			continue
		}

		b = append(b, foldRune(r)...)
	}

	return string(b)
}
