package directory

import (
	"context"
	"errors"
	"net/url"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/links"
	"github.com/donseba/go-webthings/pages"
)

// linkSources lists the published entries in locale with the links in
// their descriptions and their websites, for the site's link check
// (webthings.Parts.Links). An entry's address is below the instance's
// page, in its category; entries of an instance on no page have none, and
// their links lead from the home page.
func (s *Service) linkSources(ctx context.Context, locale string) ([]links.Source, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
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

	paths := pages.Links(s.public, base)
	sources := make([]links.Source, 0, len(entries))

	for _, item := range entries {
		text, _ := item.Text(locale)

		var address string

		// Published entries have a slug; one in no published category
		// has no address.
		if category, ok := slugs[item.Fields.Category]; ok {
			address, _ = paths("entry", "category", category, "entry", item.Fields.Slug)
		}

		sources = append(sources, links.Source{
			ID:        item.ID,
			Title:     item.Fields.Name,
			URL:       address,
			EditURL:   s.adminURL(admin.RouteName(s.key, "edit"), "id", item.ID, url.Values{"locale": {locale}}),
			Markdown:  []string{text.Description},
			Addresses: []string{item.Fields.Website},
		})
	}

	return sources, nil
}

// address is the address of an entry on the site in locale, in its
// category, for the warnings about links to it in its editor
// (editor.Linked); empty for an instance on no page and for an entry whose
// category is not published in locale.
func (s *Service) address(ctx context.Context, item EntryItem, locale string) (string, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return "", err
	}

	category, err := s.categories.Published(ctx, s.key, item.Fields.Category, locale)
	if errors.Is(err, content.ErrNotFound) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	// An entry without a slug has no address.
	address, _ := pages.Links(s.public, base)("entry", "category", category.Fields.Slug, "entry", item.Fields.Slug)

	return address, nil
}

// categoryAddress is the address of a category on the site in locale, from
// its slug, for the warnings about links to it in its editor
// (editor.Linked); empty for an instance on no page. The entries of the
// category have addresses below it, which entryAddresses gives.
func (s *Service) categoryAddress(ctx context.Context, item CategoryItem, locale string) (string, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return "", err
	}

	// A category without a slug has no address.
	address, _ := pages.Links(s.public, base)("category", "category", item.Fields.Slug)

	return address, nil
}

// entryAddresses are the addresses of the published entries of a category
// on the site in locale, below the category's own (editor.Linked.Below):
// they go offline when the category is unpublished, while a new slug of
// the category keeps them working, as entryPage redirects them. None for
// an instance on no page.
func (s *Service) entryAddresses(ctx context.Context, item CategoryItem, locale string) ([]string, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	entries, err := s.entries.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	paths := pages.Links(s.public, base)

	var addresses []string

	for _, entry := range entries {
		if entry.Fields.Category != item.ID {
			continue
		}

		address, _ := paths("entry", "category", item.Fields.Slug, "entry", entry.Fields.Slug)
		if address != "" {
			addresses = append(addresses, address)
		}
	}

	return addresses, nil
}
