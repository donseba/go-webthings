package lists

import (
	"context"
	"net/url"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/links"
)

// linkSources lists the published entries in locale with the links in
// their text and their links, for the site's link check
// (webthings.Parts.Links). Entries are shown on the instance's page, which
// is their address, and wherever templates list them; without a page their
// links lead from the home page.
func (s *Service) linkSources(ctx context.Context, locale string) ([]links.Source, error) {
	address, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	items, err := s.entries.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	sources := make([]links.Source, 0, len(items))

	for _, item := range items {
		text, _ := item.Text(locale)

		sources = append(sources, links.Source{
			ID:        item.ID,
			Title:     text.Title,
			URL:       address,
			EditURL:   s.adminURL(admin.RouteName(s.key, "edit"), "id", item.ID, url.Values{"locale": {locale}}),
			Markdown:  []string{text.Body},
			Addresses: []string{item.Fields.Link},
		})
	}

	return sources, nil
}
