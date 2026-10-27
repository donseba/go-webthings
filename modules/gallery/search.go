package gallery

import (
	"context"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/markdown"
)

// search finds published albums in locale for the site search, by title and
// description. Albums of an instance that is not on a page have no address
// and are left out.
func (s *Service) search(ctx context.Context, locale, query string, limit int) ([]frontend.SearchResult, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil || base == "" {
		return nil, err
	}

	items, err := s.albums.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	parsed := frontend.ParseQuery(query)
	paths := s.public.At(base).RoutePathArgs

	var results []frontend.SearchResult

	for _, item := range items {
		text, _ := item.Text(locale)
		description := markdown.Text(text.Description)

		score := parsed.Score(text.Title, description)
		if score == 0 {
			continue
		}

		// Published albums have a slug.
		url, _ := paths("album", "slug", text.Slug)

		results = append(results, frontend.SearchResult{
			Title:   text.Title,
			Summary: frontend.Summary(parsed, 200, description),
			URL:     url,
			Score:   score,
		})
	}

	return frontend.Rank(results, limit), nil
}
