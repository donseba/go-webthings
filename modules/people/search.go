package people

import (
	"context"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/markdown"
)

// search finds published people in locale for the site search: by name, and
// by role, group and biography. People of an instance that is not on a page
// have no address and are left out.
func (s *Service) search(ctx context.Context, locale, query string, limit int) ([]frontend.SearchResult, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil || base == "" {
		return nil, err
	}

	items, err := s.items.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	parsed := frontend.ParseQuery(query)
	paths := s.public.At(base).RoutePathArgs

	var results []frontend.SearchResult

	for _, item := range items {
		text, _ := item.Text(locale)
		bio := markdown.Text(text.Bio)

		score := parsed.Score(item.Fields.Name, text.Role, text.Group, bio)
		if score == 0 {
			continue
		}

		// Published people have a slug.
		url, _ := paths("person", "slug", item.Fields.Slug)

		results = append(results, frontend.SearchResult{
			Title:   item.Fields.Name,
			Summary: frontend.Summary(parsed, 200, text.Role, bio),
			URL:     url,
			Score:   score,
		})
	}

	return frontend.Rank(results, limit), nil
}
