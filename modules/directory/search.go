package directory

import (
	"context"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/markdown"
)

// search finds published entries in locale for the site search: by name, and
// by summary, keywords and description. Like on the website, only entries in
// a published category count. A directory that is not on a page has no
// addresses and is left out.
func (s *Service) search(ctx context.Context, locale, query string, limit int) ([]frontend.SearchResult, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil || base == "" {
		return nil, err
	}

	categories, err := s.categories.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	slugs := make(map[string]string, len(categories))

	for _, category := range categories {
		slugs[category.ID] = category.Fields.Slug
	}

	entries, err := s.entries.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	parsed := frontend.ParseQuery(query)
	paths := s.public.At(base).RoutePathArgs

	var results []frontend.SearchResult

	for _, item := range entries {
		category, ok := slugs[item.Fields.Category]
		if !ok {
			continue
		}

		text, _ := item.Text(locale)
		description := markdown.Text(text.Description)

		score := parsed.Score(item.Fields.Name, text.Summary, text.Keywords, description)
		if score == 0 {
			continue
		}

		// Published entries have a slug.
		url, _ := paths("entry", "category", category, "entry", item.Fields.Slug)

		results = append(results, frontend.SearchResult{
			Title:   item.Fields.Name,
			Summary: frontend.Summary(parsed, 200, text.Summary, description),
			URL:     url,
			Score:   score,
		})
	}

	return frontend.Rank(results, limit), nil
}
