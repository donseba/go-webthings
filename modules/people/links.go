package people

import (
	"context"
	"net/url"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/links"
	"github.com/donseba/go-webthings/pages"
)

// linkSources lists the published people in locale with the links in their
// biographies and their websites, for the site's link check
// (webthings.Parts.Links). A person's address is below the instance's
// page; people of an instance on no page have none, and their links lead
// from the home page.
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

		// Published people have a slug.
		address, _ := paths("person", "slug", item.Fields.Slug)

		sources = append(sources, links.Source{
			ID:        item.ID,
			Title:     item.Fields.Name,
			URL:       address,
			EditURL:   s.adminURL(admin.RouteName(s.key, "edit"), "id", item.ID, url.Values{"locale": {locale}}),
			Markdown:  []string{text.Bio},
			Addresses: []string{item.Fields.Website},
		})
	}

	return sources, nil
}

// address is the address of a person on the site in locale, from their
// slug, for the warnings about links to them in their editor
// (editor.Linked); empty for an instance on no page.
func (s *Service) address(ctx context.Context, item Item, locale string) (string, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return "", err
	}

	// A person without a slug has no address.
	address, _ := pages.Links(s.public, base)("person", "slug", item.Fields.Slug)

	return address, nil
}
