package stats

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/pages"
	"github.com/donseba/go-webthings/site"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

const (
	// flushInterval is how long page views wait in memory before they are
	// written, so a visitor never waits for the database.
	flushInterval = 30 * time.Second
	// maxSeen bounds the memory used to recognise returning visitors within a
	// day. Beyond it, new combinations count as views but not as visitors.
	maxSeen = 500_000
	// maxPath is the longest path that is counted; longer ones are not pages.
	maxPath = 500
)

// Dimensions of wt_stats_dimension.
const (
	referrerDimension = "referrer"
	deviceDimension   = "device"
	localeDimension   = "locale"
)

// Service is one Stats instance, for the website's own code.
type Service struct {
	key  string
	db   *db.DB
	site *site.Service
	// pages finds the page of a module instance for PopularIn.
	pages *pages.Service
	now   func() time.Time
	query queries

	mu       sync.Mutex
	location *time.Location
	// day is the day salt and seen belong to. A new day gets a new salt, so
	// visitors cannot be followed from one day to the next.
	day     string
	salt    [32]byte
	seen    map[seenKey]struct{}
	pending *buffer
	timer   *time.Timer
	pruned  string
}

// hit is one counted page view.
type hit struct {
	at       time.Time
	path     string
	title    string
	referrer string
	device   string
	locale   string
	// address and agent identify the visitor for today only; they are hashed
	// with the day's salt and never stored.
	address string
	agent   string
}

type seenKey struct {
	visitor   [16]byte
	dimension string
	name      string
}

type count struct {
	views    int
	visitors int
}

func (c *count) add(other count) {
	c.views += other.views
	c.visitors += other.visitors
}

type pageKey struct {
	day  string
	path string
}

type dimensionKey struct {
	day       string
	dimension string
	name      string
}

// buffer holds counts that are not written yet.
type buffer struct {
	days       map[string]*count
	pages      map[pageKey]*count
	titles     map[pageKey]string
	dimensions map[dimensionKey]*count
}

func newBuffer() *buffer {
	return &buffer{
		days:       map[string]*count{},
		pages:      map[pageKey]*count{},
		titles:     map[pageKey]string{},
		dimensions: map[dimensionKey]*count{},
	}
}

func (b *buffer) empty() bool {
	return len(b.days) == 0 && len(b.pages) == 0 && len(b.dimensions) == 0
}

// merge adds other to b, for counts whose write failed.
func (b *buffer) merge(other *buffer) {
	for day, c := range other.days {
		addTo(b.days, day, *c)
	}

	for key, c := range other.pages {
		addTo(b.pages, key, *c)
	}

	for key, title := range other.titles {
		if b.titles[key] == "" {
			b.titles[key] = title
		}
	}

	for key, c := range other.dimensions {
		addTo(b.dimensions, key, *c)
	}
}

func addTo[K comparable](counts map[K]*count, key K, c count) {
	if counts[key] == nil {
		counts[key] = &count{}
	}

	counts[key].add(c)
}

func newService(key string, database *db.DB, settings *site.Service) *Service {
	return &Service{
		key:      key,
		db:       database,
		site:     settings,
		now:      time.Now,
		query:    newQueries(database.Dialect()),
		location: time.UTC,
		seen:     map[seenKey]struct{}{},
		pending:  newBuffer(),
	}
}

// record counts a page view in memory; Flush writes it.
func (s *Service) record(h hit) {
	s.mu.Lock()
	defer s.mu.Unlock()

	day := h.at.In(s.location).Format(time.DateOnly)
	if day != s.day {
		s.rotate(day)
	}

	visitor := s.visitor(h.address, h.agent)
	views := count{views: 1}

	addTo(s.pending.days, day, s.unique(visitor, "", "", views))

	page := pageKey{day: day, path: h.path}
	addTo(s.pending.pages, page, s.unique(visitor, "page", h.path, views))

	if h.title != "" {
		s.pending.titles[page] = h.title
	}

	dimensions := []struct{ dimension, name string }{
		{referrerDimension, h.referrer},
		{deviceDimension, h.device},
		{localeDimension, h.locale},
	}

	for _, d := range dimensions {
		if d.name == "" {
			continue
		}

		key := dimensionKey{day: day, dimension: d.dimension, name: d.name}
		addTo(s.pending.dimensions, key, s.unique(visitor, d.dimension, d.name, views))
	}

	if s.timer == nil {
		s.timer = time.AfterFunc(flushInterval, s.flushLater)
	}
}

// rotate starts a new day: a fresh salt and nobody seen yet. Callers hold mu.
func (s *Service) rotate(day string) {
	s.day = day
	s.seen = map[seenKey]struct{}{}

	_, _ = rand.Read(s.salt[:])
}

// visitor is an anonymous identifier that is only valid today. Callers hold mu.
func (s *Service) visitor(address, agent string) [16]byte {
	hash := sha256.New()
	hash.Write(s.salt[:])
	hash.Write([]byte(address))
	hash.Write([]byte{0})
	hash.Write([]byte(agent))

	var id [16]byte

	copy(id[:], hash.Sum(nil))

	return id
}

// unique adds a visitor to c when the visitor has not been counted for this
// dimension today. Callers hold mu.
func (s *Service) unique(visitor [16]byte, dimension, name string, c count) count {
	key := seenKey{visitor: visitor, dimension: dimension, name: name}
	if _, ok := s.seen[key]; ok || len(s.seen) >= maxSeen {
		return c
	}

	s.seen[key] = struct{}{}
	c.visitors++

	return c
}

func (s *Service) flushLater() {
	if err := s.Flush(context.Background()); err != nil {
		slog.Error("flush statistics", "module", s.key, "error", err)
	}
}

// close writes the remaining views when the app closes and stops the timer,
// so nothing runs after the database is closed.
func (s *Service) close(ctx context.Context) error {
	err := s.Flush(ctx)

	s.mu.Lock()

	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}

	s.mu.Unlock()

	return err
}

// Flush writes the page views counted in memory. It runs by itself every 30
// seconds after a page view and when the app closes; call it to read
// up-to-date numbers, for example in tests.
func (s *Service) Flush(ctx context.Context) error {
	s.refreshLocation(ctx)

	s.mu.Lock()

	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}

	pending := s.pending
	s.pending = newBuffer()
	today := s.now().In(s.location).Format(time.DateOnly)
	prune := s.pruned != today
	s.mu.Unlock()

	if !pending.empty() {
		if err := s.write(ctx, pending); err != nil {
			s.mu.Lock()
			s.pending.merge(pending)

			if s.timer == nil {
				s.timer = time.AfterFunc(flushInterval, s.flushLater)
			}

			s.mu.Unlock()

			return err
		}
	}

	if !prune {
		return nil
	}

	if err := s.Prune(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	s.pruned = today
	s.mu.Unlock()

	return nil
}

// refreshLocation follows the site's timezone setting.
func (s *Service) refreshLocation(ctx context.Context) {
	if s.site == nil {
		return
	}

	settings, err := s.site.Settings(ctx)
	if err != nil {
		return
	}

	s.mu.Lock()
	s.location = settings.Location()
	s.mu.Unlock()
}

func (s *Service) write(ctx context.Context, pending *buffer) error {
	return s.db.Tx(ctx, func(ctx context.Context) error {
		for day, c := range pending.days {
			if _, err := s.db.Exec(ctx, s.query.day, s.key, day, c.views, c.visitors); err != nil {
				return err
			}
		}

		for key, c := range pending.pages {
			_, err := s.db.Exec(ctx, s.query.page, s.key, key.day, key.path, c.views, c.visitors, pending.titles[key])
			if err != nil {
				return err
			}
		}

		for key, c := range pending.dimensions {
			_, err := s.db.Exec(ctx, s.query.dimension, s.key, key.day, key.dimension, key.name, c.views, c.visitors)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// queries are the dialect's statements that add counts to existing rows.
type queries struct {
	day       string
	page      string
	dimension string
}

func newQueries(dialect db.Dialect) queries {
	return queries{
		day:       upsert(dialect, "wt_stats_day", []string{"instance", "day"}, nil),
		page:      upsert(dialect, "wt_stats_page", []string{"instance", "day", "path"}, []string{"title"}),
		dimension: upsert(dialect, "wt_stats_dimension", []string{"instance", "day", "dimension", "name"}, nil),
	}
}

// upsert inserts a row with keys, views, visitors and the replaced columns,
// or adds views and visitors to the row that has these keys. A replaced
// column keeps its value when the new one is empty, so views written without
// a title keep the title of earlier ones.
func upsert(dialect db.Dialect, table string, keys, replaced []string) string {
	columns := append(append(append([]string{}, keys...), "views", "visitors"), replaced...)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(columns)), ",")
	insert := "INSERT INTO " + table + " (" + strings.Join(columns, ", ") + ") VALUES (" + placeholders + ")"

	var updates []string

	if dialect == db.MySQL {
		for _, column := range []string{"views", "visitors"} {
			updates = append(updates, column+" = "+column+" + VALUES("+column+")")
		}

		for _, column := range replaced {
			updates = append(updates, column+" = IF(VALUES("+column+") <> '', VALUES("+column+"), "+column+")")
		}

		return insert + " ON DUPLICATE KEY UPDATE " + strings.Join(updates, ", ")
	}

	for _, column := range []string{"views", "visitors"} {
		updates = append(updates, column+" = "+table+"."+column+" + excluded."+column)
	}

	for _, column := range replaced {
		kept := table + "." + column
		updates = append(updates, column+" = CASE WHEN excluded."+column+" <> '' THEN excluded."+column+" ELSE "+kept+" END")
	}

	return insert + " ON CONFLICT (" + strings.Join(keys, ", ") + ") DO UPDATE SET " + strings.Join(updates, ", ")
}

// Retention is the number of days statistics are kept; 0 keeps them all.
func (s *Service) Retention(ctx context.Context) (int, error) {
	var days []int

	err := s.db.Select(ctx, &days, `SELECT retention_days FROM wt_stats_setting WHERE instance=?`, s.key)
	if err != nil || len(days) == 0 {
		return 0, err
	}

	return days[0], nil
}

// SetRetention keeps statistics for days days (0 keeps them all) and
// deletes older ones right away.
func (s *Service) SetRetention(ctx context.Context, days int) error {
	days = max(days, 0)

	err := s.db.Tx(ctx, func(ctx context.Context) error {
		if _, err := s.db.Exec(ctx, `DELETE FROM wt_stats_setting WHERE instance=?`, s.key); err != nil {
			return err
		}

		_, err := s.db.Exec(ctx,
			`INSERT INTO wt_stats_setting (instance, retention_days, updated_at, updated_by) VALUES (?,?,?,?)`,
			s.key, days, time.Now().UTC(), db.Actor(ctx),
		)

		return err
	})
	if err != nil {
		return err
	}

	return s.Prune(ctx)
}

// Prune deletes the days that are older than the retention. Flush calls it
// once a day.
func (s *Service) Prune(ctx context.Context) error {
	days, err := s.Retention(ctx)
	if err != nil || days == 0 {
		return err
	}

	oldest := s.firstDay(days)

	return s.db.Tx(ctx, func(ctx context.Context) error {
		for _, table := range []string{"wt_stats_day", "wt_stats_page", "wt_stats_dimension"} {
			if _, err := s.db.Exec(ctx, `DELETE FROM `+table+` WHERE instance=? AND day<?`, s.key, oldest); err != nil {
				return err
			}
		}

		return nil
	})
}

// today is the current date in the site's timezone, as midnight UTC: days
// are counted as dates, which a timezone that skips midnight, such as
// America/Santiago on the day its clocks go forward, cannot shift.
func (s *Service) today() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now().In(s.location)

	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// firstDay is the first of the last days days, including today.
func (s *Service) firstDay(days int) string {
	return s.today().AddDate(0, 0, 1-days).Format(time.DateOnly)
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
