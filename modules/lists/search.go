package lists

import (
	"context"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/markdown"
)

// search finds published entries in locale for the site search, by title and
// text. Entries have no page of their own: results link to the entry on the
// list's page (id="{{.ID}}"). A list that is not on a page is left out.
func (s *Service) search(ctx context.Context, locale, query string, limit int) ([]frontend.SearchResult, error) {
	page, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil || page == "" {
		return nil, err
	}

	items, err := s.entries.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	parsed := frontend.ParseQuery(query)
	var results []frontend.SearchResult

	for _, item := range items {
		text, _ := item.Text(locale)
		body := markdown.Text(text.Body)

		score := parsed.Score(text.Title, body)
		if score == 0 {
			continue
		}

		results = append(results, frontend.SearchResult{
			Title:   text.Title,
			Summary: frontend.Summary(parsed, 200, body),
			URL:     page + "#" + item.ID,
			Score:   score,
		})
	}

	return frontend.Rank(results, limit), nil
}
