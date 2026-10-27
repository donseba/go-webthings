package publishing

import (
	"context"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/markdown"
)

// search finds published posts in locale for the site search: by title,
// excerpt and text. Posts of an instance that is not on a page have no
// address and are left out.
func (s *Service) search(ctx context.Context, locale, query string, limit int) ([]frontend.SearchResult, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil || base == "" {
		return nil, err
	}

	items, err := s.posts.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	parsed := frontend.ParseQuery(query)
	paths := s.public.At(base).RoutePathArgs

	var results []frontend.SearchResult

	for _, item := range items {
		text, _ := item.Text(locale)
		body := markdown.Text(text.Body)

		score := parsed.Score(text.Title, text.Excerpt, body)
		if score == 0 {
			continue
		}

		// Published posts have a slug.
		url, _ := paths("post", "slug", text.Slug)

		results = append(results, frontend.SearchResult{
			Title:   text.Title,
			Summary: frontend.Summary(parsed, 200, text.Excerpt, body),
			URL:     url,
			Score:   score,
		})
	}

	return frontend.Rank(results, limit), nil
}
