package banners

import (
	"context"
	"net/url"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/links"
)

// linkSources lists the published banners in locale with the links in
// their text and their button's address, for the site's link check
// (webthings.Parts.Links). Banners have no address of their own: templates
// show them on any page, and their links lead from the home page.
func (s *Service) linkSources(ctx context.Context, locale string) ([]links.Source, error) {
	items, err := s.items.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	sources := make([]links.Source, 0, len(items))

	for _, item := range items {
		text, _ := item.Text(locale)

		sources = append(sources, links.Source{
			ID:        item.ID,
			Title:     text.Title,
			EditURL:   s.adminURL(admin.RouteName(s.key, "edit"), "id", item.ID, url.Values{"locale": {locale}}),
			Markdown:  []string{text.Body},
			Addresses: []string{text.ButtonURL},
		})
	}

	return sources, nil
}
