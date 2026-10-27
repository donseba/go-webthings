package redirects

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/donseba/go-router/middleware"
)

const (
	// refreshEvery reloads the table now and then, so changes made by another
	// process serving the same database are picked up.
	refreshEvery = time.Minute
	// flushEvery is how long hits are counted in memory before they are
	// written; the admin writes them at once when it shows them.
	flushEvery = 30 * time.Second
	// maxPending limits the distinct missed paths counted in memory between
	// two writes, so a scan of random URLs cannot grow it without bound.
	maxPending = 1000
	// maxMissed is how many missed paths are kept; the least requested go first.
	maxMissed = 1000
)

// table is the in-memory copy of an instance's redirects.
type table struct {
	byPath map[string]Redirect
	loaded time.Time
}

// current returns the redirects, loading them when they are missing or old.
// While one request reloads, others keep using the previous copy.
func (s *Service) current(ctx context.Context) *table {
	loaded := s.table.Load()
	if loaded != nil && time.Since(loaded.loaded) < refreshEvery {
		return loaded
	}

	if !s.reloading.TryLock() {
		if loaded != nil {
			return loaded
		}

		s.reloading.Lock()
	}

	defer s.reloading.Unlock()

	if fresh := s.table.Load(); fresh != loaded && fresh != nil {
		return fresh
	}

	// Other requests wait for this load, so a visitor who leaves must not
	// cancel it.
	fresh, err := s.load(context.WithoutCancel(ctx))
	if err != nil {
		slog.ErrorContext(ctx, "load redirects", "module", s.key, "error", err)

		if loaded != nil {
			return loaded
		}

		return &table{}
	}

	return fresh
}

// reload reads the redirects again after a change.
func (s *Service) reload(ctx context.Context) {
	s.reloading.Lock()
	defer s.reloading.Unlock()

	if _, err := s.load(ctx); err != nil {
		slog.ErrorContext(ctx, "reload redirects", "module", s.key, "error", err)
		s.table.Store(nil)
	}
}

func (s *Service) load(ctx context.Context) (*table, error) {
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	loaded := &table{byPath: make(map[string]Redirect, len(list)), loaded: time.Now()}

	for _, redirect := range list {
		loaded.byPath[redirect.From] = redirect
	}

	s.table.Store(loaded)

	return loaded, nil
}

// Resolve returns the redirect for a request path, if there is one. No
// redirect is saved from a reserved path (see Save), and requests to the
// paths the app serves itself never reach the site's middleware.
func (s *Service) Resolve(ctx context.Context, requestPath string) (Redirect, bool) {
	redirect, ok := s.current(ctx).byPath[cleanPath(requestPath)]

	return redirect, ok
}

// Trace follows a path through the redirects, at most a few steps, and
// returns each redirect on the way.
func (s *Service) Trace(ctx context.Context, requestPath string) []Redirect {
	var steps []Redirect

	seen := map[string]bool{}
	current := requestPath

	for range 5 {
		redirect, ok := s.Resolve(ctx, current)
		if !ok || seen[redirect.ID] {
			break
		}

		seen[redirect.ID] = true
		steps = append(steps, redirect)

		current = s.local(redirect.To)
		if current == "" {
			break
		}
	}

	return steps
}

// middleware redirects matching GET and HEAD requests and counts requests
// that end in "not found".
func (s *Service) middleware(next http.Handler) http.Handler {
	// A GET that ends in "not found" is a missed path, unless the answer
	// asks search engines not to index it.
	missed := middleware.Observe(func(r *http.Request, info middleware.ResponseInfo) {
		if info.Status == http.StatusNotFound && !noindex(info.Header) {
			s.miss(r.URL.Path)
		}
	})(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)

			return
		}

		if redirect, ok := s.Resolve(r.Context(), r.URL.Path); ok {
			s.hit(redirect.ID)
			http.Redirect(w, r, target(redirect.To, r.URL.RawQuery), redirect.Status)

			return
		}

		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)

			return
		}

		missed.ServeHTTP(w, r)
	})
}

// noindex reports whether a response asks search engines not to index it
// (X-Robots-Tag: noindex or none, also for one crawler as in "googlebot:
// noindex"). Such an answer belongs to a private or temporary address, such
// as a mistyped registration code, which no redirect should point to and no
// list of missed paths should show.
func noindex(header http.Header) bool {
	for _, value := range header.Values("X-Robots-Tag") {
		for directive := range strings.FieldsFuncSeq(strings.ToLower(value), isSeparator) {
			if directive == "noindex" || directive == "none" {
				return true
			}
		}
	}

	return false
}

// isSeparator splits X-Robots-Tag values into directives and crawler names.
func isSeparator(r rune) bool {
	return r == ',' || r == ':' || r == ' ' || r == '\t'
}

// target adds the request's query to an address without one, so campaign
// and search parameters survive the redirect.
func target(to, query string) string {
	if query == "" {
		return to
	}

	parsed, err := url.Parse(to)
	if err != nil || parsed.RawQuery != "" {
		return to
	}

	parsed.RawQuery = query

	return parsed.String()
}

// counter collects hits and misses in memory; writing every request to the
// database would make redirects as slow as the slowest write.
type counter struct {
	mu       sync.Mutex
	hits     map[string]tally
	misses   map[string]tally
	since    time.Time
	flushing bool
	// writing keeps two flushes from interleaving.
	writing sync.Mutex
}

type tally struct {
	count int64
	last  time.Time
}

func (s *Service) hit(id string) {
	s.count(func(c *counter, now time.Time) {
		c.hits[id] = tally{count: c.hits[id].count + 1, last: now}
	})
}

func (s *Service) miss(requestPath string) {
	cleaned := cleanPath(requestPath)
	if len(cleaned) > maxPath || s.reserved(cleaned) {
		return
	}

	s.count(func(c *counter, now time.Time) {
		if _, ok := c.misses[cleaned]; !ok && len(c.misses) >= maxPending {
			return
		}

		c.misses[cleaned] = tally{count: c.misses[cleaned].count + 1, last: now}
	})
}

// count records a hit or miss and starts a background write when the
// counts are old enough.
func (s *Service) count(add func(c *counter, now time.Time)) {
	c := &s.counts
	now := time.Now().UTC()

	c.mu.Lock()

	if c.hits == nil {
		c.hits = map[string]tally{}
		c.misses = map[string]tally{}
	}

	if c.since.IsZero() {
		c.since = now
	}

	add(c, now)

	start := !c.flushing && now.Sub(c.since) >= flushEvery
	if start {
		c.flushing = true
	}

	c.mu.Unlock()

	if start {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := s.Flush(ctx); err != nil {
				slog.ErrorContext(ctx, "write redirect hits", "module", s.key, "error", err)
			}
		}()
	}
}

// Flush writes the hits and misses counted in memory to the database.
func (s *Service) Flush(ctx context.Context) error {
	c := &s.counts

	c.writing.Lock()
	defer c.writing.Unlock()

	c.mu.Lock()
	hits, misses := c.hits, c.misses
	c.hits, c.misses = map[string]tally{}, map[string]tally{}
	c.since = time.Time{}
	c.flushing = false
	c.mu.Unlock()

	if len(hits) == 0 && len(misses) == 0 {
		return nil
	}

	return s.db.Tx(ctx, func(ctx context.Context) error {
		// Hits come from visitors, not editors, so they leave updated_at
		// and updated_by alone.
		for id, counted := range hits {
			_, err := s.db.Exec(ctx, `
				UPDATE wt_redirect SET hits=hits+?, last_hit_at=?
				WHERE instance=? AND id=?`,
				counted.count, counted.last, s.key, id,
			)
			if err != nil {
				return err
			}
		}

		for missed, counted := range misses {
			if err := s.addMiss(ctx, missed, counted); err != nil {
				return err
			}
		}

		if len(misses) > 0 {
			return s.pruneMisses(ctx, maxMissed)
		}

		return nil
	})
}

func (s *Service) addMiss(ctx context.Context, missed string, counted tally) error {
	result, err := s.db.Exec(ctx, `
		UPDATE wt_redirect_miss SET hits=hits+?, last_hit_at=?
		WHERE instance=? AND path=?`,
		counted.count, counted.last, s.key, missed,
	)
	if err != nil {
		return err
	}

	updated, err := result.RowsAffected()
	if err != nil || updated > 0 {
		return err
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO wt_redirect_miss (instance, path, hits, last_hit_at)
		VALUES (?, ?, ?, ?)`,
		s.key, missed, counted.count, counted.last,
	)

	return err
}

// pruneMisses keeps the keep most requested paths.
func (s *Service) pruneMisses(ctx context.Context, keep int) error {
	var count int

	err := s.db.Get(ctx, &count, `SELECT COUNT(*) FROM wt_redirect_miss WHERE instance=?`, s.key)
	if err != nil || count <= keep {
		return err
	}

	var threshold Miss

	err = s.db.Get(ctx, &threshold, `
		SELECT path, hits, last_hit_at
		FROM wt_redirect_miss
		WHERE instance=?
		ORDER BY hits DESC, last_hit_at DESC
		LIMIT 1 OFFSET ?`,
		s.key, keep-1,
	)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(ctx, `
		DELETE FROM wt_redirect_miss
		WHERE instance=? AND (hits<? OR (hits=? AND last_hit_at<?))`,
		s.key, threshold.Hits, threshold.Hits, threshold.LastHit,
	)

	return err
}

// Missed returns the most requested paths that were not found and have no
// redirect yet.
func (s *Service) Missed(ctx context.Context, limit int) ([]Miss, error) {
	var list []Miss

	err := s.db.Select(ctx, &list, `
		SELECT path, hits, last_hit_at
		FROM wt_redirect_miss
		WHERE instance=?
		ORDER BY hits DESC, last_hit_at DESC, path
		LIMIT ?`,
		s.key, limit,
	)
	if err != nil {
		return nil, err
	}

	redirected := s.current(ctx).byPath
	out := list[:0]

	for _, missed := range list {
		if _, ok := redirected[missed.Path]; !ok {
			out = append(out, missed)
		}
	}

	return out, nil
}

// ClearMissed empties the list of missed paths.
func (s *Service) ClearMissed(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `DELETE FROM wt_redirect_miss WHERE instance=?`, s.key)

	return err
}
