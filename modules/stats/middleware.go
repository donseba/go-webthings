package stats

import (
	"bytes"
	"compress/gzip"
	"context"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/donseba/go-webthings/frontend"

	"github.com/donseba/go-partial/connector"
	"github.com/donseba/go-router/middleware"
)

// htmx tells the requests htmx made apart, as the site's pages are served
// with go-partial's HTMX 4 connector.
var htmx = connector.NewHTMX4(nil)

// maxHead is how much of a page is read to find its title.
const maxHead = 16 << 10

// gzipReaders are readers of compressed pages, kept for the next page:
// each holds a decompressor of some 40 KB.
var gzipReaders sync.Pool

// plainHead is the start of a page as the recorder kept it, head, at most
// maxHead bytes: decompressed when the response is gzip-compressed (see
// the Content-Encoding in header). A compressed head is cut off, so it
// decompresses into what it holds; nil when it is no gzip.
func plainHead(header http.Header, head []byte) []byte {
	if !strings.EqualFold(header.Get("Content-Encoding"), "gzip") {
		return head
	}

	reader, _ := gzipReaders.Get().(*gzip.Reader)

	var err error

	if reader == nil {
		reader, err = gzip.NewReader(bytes.NewReader(head))
	} else {
		err = reader.Reset(bytes.NewReader(head))
	}

	if err != nil {
		return nil
	}

	defer gzipReaders.Put(reader)

	plain := make([]byte, maxHead)

	// The head ends before the compressed page does.
	n, _ := io.ReadFull(reader, plain)

	return plain[:n]
}

// middleware counts the public pages visitors see. The response is never
// delayed: the view is counted in memory after the page was written.
func (s *Service) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !countable(r) {
			next.ServeHTTP(w, r)

			return
		}

		// Editors checking the site are not visitors.
		if _, editor := frontend.Editor(r.Context()); editor {
			next.ServeHTTP(w, r)

			return
		}

		recorder := &recorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)

		if recorder.status != 0 && recorder.status != http.StatusOK {
			return
		}

		if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			return
		}

		// The page comes compressed when the visitor accepts it: the site
		// compresses, and keeps pages in its output cache, below the
		// modules' middleware.
		head := plainHead(w.Header(), recorder.head)

		if private(w.Header()) || noindexMeta(head) {
			return
		}

		language := locale(r.URL.Path)

		s.record(hit{
			at:       s.now(),
			path:     r.URL.Path,
			title:    withoutSiteName(title(head), s.siteName(r.Context(), language)),
			referrer: referrer(r),
			device:   device(r.UserAgent()),
			locale:   language,
			address:  frontend.ClientIP(r),
			agent:    r.UserAgent(),
		})
	})
}

// countable reports whether r is a person opening a page: not a file, not a
// form post, not a prefetch and not a known bot. The paths the app serves
// itself (webthings.App.CorePaths) never reach the site's middleware.
func countable(r *http.Request) bool {
	if r.Method != http.MethodGet || len(r.URL.Path) > maxPath {
		return false
	}

	if path.Ext(r.URL.Path) != "" {
		return false
	}

	// Browsers announce speculative loads; the visitor may never see them.
	for _, header := range []string{"Sec-Purpose", "Purpose", "X-Purpose", "X-Moz"} {
		if strings.Contains(strings.ToLower(r.Header.Get(header)), "prefetch") {
			return false
		}
	}

	// htmx fragments are parts of a page that was already counted; boosted
	// links are real navigation.
	if connector.IsRequest(htmx, r) && !connector.IsBoosted(htmx, r) {
		return false
	}

	return !isBot(r.UserAgent())
}

// private reports whether a response asks not to be kept: search engines may
// not index it (X-Robots-Tag: noindex or none, also for one crawler as in
// "googlebot: noindex") or only the visitor's own browser may store it
// (Cache-Control: private). Such pages show personal details, such as a
// registration looked up by its code, or are endless variations, such as
// search results; their addresses must not end up in statistics that editors
// and "most read" lists show. A bare "no-store" is not enough: forms, polls
// and comments send it to keep their answers fresh, and those pages count.
func private(header http.Header) bool {
	for _, value := range header.Values("X-Robots-Tag") {
		if hasDirective(value, "noindex", "none") {
			return true
		}
	}

	for _, value := range header.Values("Cache-Control") {
		if hasDirective(value, "private") {
			return true
		}
	}

	return false
}

// metaTag and metaAttribute find <meta name="robots" content="…"> in a page.
var (
	metaTag       = regexp.MustCompile(`(?is)<meta\s[^>]*>`)
	metaAttribute = regexp.MustCompile(`(?is)\b(name|content)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
)

// noindexMeta reports whether the page's head asks robots not to index it,
// with <meta name="robots" content="noindex">, as a template can do for
// pages that should stay out of sight.
func noindexMeta(head []byte) bool {
	for _, tag := range metaTag.FindAll(head, -1) {
		var name, content string

		for _, attribute := range metaAttribute.FindAllSubmatch(tag, -1) {
			value := string(attribute[2]) + string(attribute[3]) + string(attribute[4])

			if strings.EqualFold(string(attribute[1]), "name") {
				name = value
			} else {
				content = value
			}
		}

		if strings.EqualFold(strings.TrimSpace(name), "robots") && hasDirective(content, "noindex", "none") {
			return true
		}
	}

	return false
}

// hasDirective reports whether a header or meta value, a list such as
// "private, max-age=0" or "googlebot: noindex, nofollow", holds one of
// directives.
func hasDirective(value string, directives ...string) bool {
	separator := func(r rune) bool {
		return r == ',' || r == ':' || r == ' ' || r == '\t'
	}

	for field := range strings.FieldsFuncSeq(strings.ToLower(value), separator) {
		if slices.Contains(directives, field) {
			return true
		}
	}

	return false
}

// botMarkers are parts of the user agents of crawlers, link previews,
// monitors and scripts.
var botMarkers = []string{
	"bot", "crawl", "spider", "slurp", "preview", "facebookexternalhit", "whatsapp", "embedly", "headless",
	"phantomjs", "lighthouse", "pagespeed", "uptime", "monitor", "statuscake", "pingdom", "datadog", "newrelic",
	"curl", "wget", "python", "go-http-client", "httpclient", "okhttp", "java/", "libwww", "scrapy", "axios",
	"node-fetch", "feedfetcher", "validator",
}

// isBot reports user agents that are not a person's browser. Without a user
// agent it is certainly not a browser.
func isBot(agent string) bool {
	agent = strings.ToLower(strings.TrimSpace(agent))
	if agent == "" {
		return true
	}

	for _, marker := range botMarkers {
		if strings.Contains(agent, marker) {
			return true
		}
	}

	return false
}

// device classifies a browser as desktop, mobile or tablet.
func device(agent string) string {
	agent = strings.ToLower(agent)

	switch {
	case strings.Contains(agent, "ipad") || strings.Contains(agent, "tablet") ||
		strings.Contains(agent, "android") && !strings.Contains(agent, "mobile"):
		return "tablet"
	case strings.Contains(agent, "mobi") || strings.Contains(agent, "iphone") || strings.Contains(agent, "android"):
		return "mobile"
	default:
		return "desktop"
	}
}

// referrer is the host of the site the visitor came from, without "www.",
// or "" for direct visits and links within the site. The site's own host is
// the one a trusted proxy reports, so links within the site behind a proxy
// that changes the Host header are not counted as other sites.
func referrer(r *http.Request) string {
	from, err := url.Parse(r.Referer())
	if err != nil || from.Scheme != "http" && from.Scheme != "https" {
		return ""
	}

	host := strings.TrimPrefix(strings.ToLower(from.Hostname()), "www.")
	if host == "" || len(host) > 255 || host == strings.TrimPrefix(strings.ToLower(hostname(middleware.Host(r))), "www.") {
		return ""
	}

	return host
}

func hostname(host string) string {
	if name, _, err := net.SplitHostPort(host); err == nil {
		return name
	}

	return host
}

// locale is the page's language, the first segment of its path.
func locale(path string) string {
	segment, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	if len(segment) > 35 {
		return ""
	}

	return segment
}

// siteName is the site's title in locale, which layouts usually add to the
// page title; "" when unknown.
func (s *Service) siteName(ctx context.Context, locale string) string {
	if s.site == nil {
		return ""
	}

	snapshot, err := s.site.Snapshot(ctx)
	if err != nil {
		return ""
	}

	return snapshot.Settings().Text(locale).Title
}

// separators join a page title and the site name in a layout's <title>.
var separators = []string{" · ", " | ", " - ", " – ", " — ", " :: ", " / "}

// withoutSiteName removes the site name that a layout adds before or after
// the page title, as in "About · My site" or "My site | About", so lists of
// pages show the pages' own titles. A title that is only the site name, as
// on many home pages, is kept.
func withoutSiteName(title, site string) string {
	if site == "" || title == site {
		return title
	}

	for _, separator := range separators {
		if page, ok := strings.CutSuffix(title, separator+site); ok && strings.TrimSpace(page) != "" {
			return strings.TrimSpace(page)
		}

		if page, ok := strings.CutPrefix(title, site+separator); ok && strings.TrimSpace(page) != "" {
			return strings.TrimSpace(page)
		}
	}

	return title
}

// title is the text of the page's <title> element, or "".
func title(head []byte) string {
	lower := bytes.ToLower(head)

	open := bytes.Index(lower, []byte("<title"))
	if open < 0 {
		return ""
	}

	start := bytes.IndexByte(lower[open:], '>')
	if start < 0 {
		return ""
	}

	start += open + 1

	end := bytes.Index(lower[start:], []byte("</title"))
	if end < 0 {
		return ""
	}

	text := strings.Join(strings.Fields(html.UnescapeString(string(head[start:start+end]))), " ")

	return truncate(text, 300)
}

// truncate shortens s to at most limit bytes without splitting a character.
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	s = s[:limit]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}

	return s
}

// recorder notes the status of a response and keeps its first bytes, which
// hold the page's title.
type recorder struct {
	http.ResponseWriter

	status int
	head   []byte
}

func (r *recorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}

	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}

	if r.status == http.StatusOK && len(r.head) < maxHead {
		r.head = append(r.head, data[:min(len(data), maxHead-len(r.head))]...)
	}

	return r.ResponseWriter.Write(data)
}

// Flush keeps streaming responses working through the recorder.
func (r *recorder) Flush() {
	_ = http.NewResponseController(r.ResponseWriter).Flush()
}

// Unwrap lets http.ResponseController reach the original writer.
func (r *recorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
