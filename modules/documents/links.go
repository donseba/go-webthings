package documents

import (
	"context"
	"net/url"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/links"
	"github.com/donseba/go-webthings/pages"
)

// linkSources lists the published documents in locale with the links in
// their descriptions and their links, for the site's link check
// (webthings.Parts.Links). A document is shown on the instance's page, or
// on its category's list below it, which is its address; without a page
// its links lead from the home page.
func (s *Service) linkSources(ctx context.Context, locale string) ([]links.Source, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	items, err := s.items.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	paths := pages.Links(s.public, base)
	sources := make([]links.Source, 0, len(items))

	for _, item := range items {
		text, _ := item.Text(locale)

		address := base
		if slug := content.Slug(text.Category); slug != "" && base != "" {
			address, _ = paths("category", "slug", slug)
		}

		sources = append(sources, links.Source{
			ID:        item.ID,
			Title:     text.Title,
			URL:       address,
			EditURL:   s.adminURL(admin.RouteName(s.key, "edit"), "id", item.ID, url.Values{"locale": {locale}}),
			Markdown:  []string{text.Description},
			Addresses: []string{item.Fields.Link},
		})
	}

	return sources, nil
}
