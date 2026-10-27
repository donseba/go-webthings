package events

import (
	"context"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/markdown"
)

// search finds published events in locale, past and upcoming, for the site
// search: by title, summary, text and location. Events of an instance that
// is not on a page have no address and are left out.
func (s *Service) search(ctx context.Context, locale, query string, limit int) ([]frontend.SearchResult, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil || base == "" {
		return nil, err
	}

	items, err := s.events.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	parsed := frontend.ParseQuery(query)
	paths := s.public.At(base).RoutePathArgs

	var results []frontend.SearchResult

	for _, item := range items {
		text, _ := item.Text(locale)
		body := markdown.Text(text.Body)

		score := parsed.Score(text.Title, text.Summary, body, text.Location, text.Address)
		if score == 0 {
			continue
		}

		// Published events have a slug.
		url, _ := paths("event", "slug", text.Slug)

		results = append(results, frontend.SearchResult{
			Title:   text.Title,
			Summary: frontend.Summary(parsed, 200, text.Summary, body, text.Location),
			URL:     url,
			Score:   score,
		})
	}

	return frontend.Rank(results, limit), nil
}
