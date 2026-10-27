package stats

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/donseba/go-webthings/pages"
)

// Report is the statistics of the last Days days, including today.
type Report struct {
	Days int
	// Views and Visitors are the totals. A visitor is recognised for one day
	// only, so Visitors adds up the visitors of each day.
	Views     int
	Visitors  int
	Daily     []Day
	Pages     []Page
	Referrers []Count
	Devices   []Count
	Locales   []Count
}

// Day is one day of a report.
type Day struct {
	// Date is YYYY-MM-DD.
	Date     string `db:"day"`
	Views    int    `db:"views"`
	Visitors int    `db:"visitors"`
}

// Page is a path with its number of views.
type Page struct {
	// Path is the page's address, for example /en/news/hello.
	Path string `db:"path"`
	// Title is the page's HTML title when it was last viewed, without the
	// site name.
	Title    string `db:"title"`
	Views    int    `db:"views"`
	Visitors int    `db:"visitors"`
}

// Count is the number of views by one referrer host, device class (desktop,
// mobile or tablet) or language.
type Count struct {
	Name     string `db:"name"`
	Views    int    `db:"views"`
	Visitors int    `db:"visitors"`
}

// Report sums up the last days days (1 to 366), today included. It reads
// what was flushed; call Flush first for up-to-the-second numbers.
func (s *Service) Report(ctx context.Context, days int) (Report, error) {
	days = min(max(days, 1), 366)
	first := s.firstDay(days)
	report := Report{Days: days}

	var stored []Day

	err := s.db.Select(ctx, &stored,
		`SELECT day, views, visitors FROM wt_stats_day WHERE instance=? AND day>=? ORDER BY day`,
		s.key, first,
	)
	if err != nil {
		return Report{}, err
	}

	byDate := map[string]Day{}

	for _, day := range stored {
		byDate[day.Date] = day
		report.Views += day.Views
		report.Visitors += day.Visitors
	}

	start := s.today().AddDate(0, 0, 1-days)

	for i := range days {
		date := start.AddDate(0, 0, i).Format(time.DateOnly)
		day, ok := byDate[date]

		if !ok {
			day = Day{Date: date}
		}

		report.Daily = append(report.Daily, day)
	}

	report.Pages, err = s.top(ctx, first, "", 10)
	if err != nil {
		return Report{}, err
	}

	for _, dimension := range []struct {
		name  string
		into  *[]Count
		limit int
	}{
		{referrerDimension, &report.Referrers, 10},
		{deviceDimension, &report.Devices, 3},
		{localeDimension, &report.Locales, 20},
	} {
		*dimension.into, err = s.counts(ctx, first, dimension.name, dimension.limit)
		if err != nil {
			return Report{}, err
		}
	}

	return report, nil
}

// Popular returns the limit most viewed pages in locale of the last days
// days, without the home page, for "most read" lists. A limit of 0 or less
// returns all of them.
func (s *Service) Popular(ctx context.Context, locale string, limit, days int) ([]Page, error) {
	if locale == "" {
		return nil, nil
	}

	return s.top(ctx, s.firstDay(min(max(days, 1), 366)), "/"+locale+"/", limit)
}

// PopularIn is Popular for the pages below within, without that page itself,
// for example the most read posts of a blog. within is a path without the
// language prefix, such as "/blog", or the key of a module instance, whose
// page in locale is used; an instance that is not on a page has no popular
// pages. A limit of 0 or less returns all of them.
func (s *Service) PopularIn(ctx context.Context, locale, within string, limit, days int) ([]Page, error) {
	if locale == "" {
		return nil, nil
	}

	below := within

	if !strings.HasPrefix(within, "/") {
		if s.pages == nil {
			return nil, nil
		}

		mount, err := s.pages.MountPath(ctx, within, locale)
		if errors.Is(err, pages.ErrNotFound) {
			return nil, nil
		}

		if err != nil {
			return nil, err
		}

		below = mount
	}

	prefix := "/" + locale + strings.TrimSuffix(below, "/") + "/"

	return s.top(ctx, s.firstDay(min(max(days, 1), 366)), prefix, limit)
}

// Views is the number of times path was viewed in the kept statistics,
// including views that are not flushed yet.
func (s *Service) Views(ctx context.Context, path string) (int, error) {
	var views int

	err := s.db.Get(ctx, &views,
		`SELECT COALESCE(SUM(views), 0) FROM wt_stats_page WHERE instance=? AND path=?`,
		s.key, path,
	)
	if err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for key, c := range s.pending.pages {
		if key.path == path {
			views += c.views
		}
	}

	return views, nil
}

// top lists the limit most viewed paths since first, or all of them when
// limit is 0 or less. With a prefix such as "/en/blog/" it lists the paths
// that start with it, without the page at the prefix itself. SUBSTR rather
// than LIKE compares, so "_" and "%" in paths are no wildcards. Each path has
// the title of the last day it was viewed with one.
func (s *Service) top(ctx context.Context, first, prefix string, limit int) ([]Page, error) {
	query := `
		SELECT page.path,
			COALESCE((
				SELECT latest.title
				FROM wt_stats_page latest
				WHERE latest.instance=? AND latest.path=page.path AND latest.title<>''
				ORDER BY latest.day DESC
				LIMIT 1
			), '') AS title,
			SUM(page.views) AS views,
			SUM(page.visitors) AS visitors
		FROM wt_stats_page page
		WHERE page.instance=? AND page.day>=?`
	args := []any{s.key, s.key, first}

	if prefix != "" {
		query += ` AND SUBSTR(page.path, 1, ?)=? AND page.path<>?`
		args = append(args, utf8.RuneCountInString(prefix), prefix, prefix)
	}

	query += ` GROUP BY page.path ORDER BY SUM(page.views) DESC, page.path`

	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	var pages []Page

	err := s.db.Select(ctx, &pages, query, args...)

	return pages, err
}

func (s *Service) counts(ctx context.Context, first, dimension string, limit int) ([]Count, error) {
	var counts []Count

	err := s.db.Select(ctx, &counts, `
		SELECT name, SUM(views) AS views, SUM(visitors) AS visitors
		FROM wt_stats_dimension
		WHERE instance=? AND day>=? AND dimension=?
		GROUP BY name
		ORDER BY SUM(views) DESC, name
		LIMIT ?`,
		s.key, first, dimension, limit,
	)

	return counts, err
}
