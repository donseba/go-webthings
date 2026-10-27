package documents

import (
	"context"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/markdown"
)

// search finds published documents in locale for the site search: by title,
// and by description and category. Documents have no page of their own:
// results link to the document on the list of its category, or on the whole
// list, at #<ID>. Documents of an instance that is not on a page are left
// out.
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
		description := markdown.Text(text.Description)

		score := parsed.Score(text.Title, text.Category, description)
		if score == 0 {
			continue
		}

		list := base
		if slug := content.Slug(text.Category); slug != "" {
			list, _ = paths("category", "slug", slug)
		}

		results = append(results, frontend.SearchResult{
			Title:   text.Title,
			Summary: frontend.Summary(parsed, 200, description, text.Category),
			URL:     list + "#" + item.ID,
			Score:   score,
		})
	}

	return frontend.Rank(results, limit), nil
}
