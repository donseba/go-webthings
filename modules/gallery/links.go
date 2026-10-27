package gallery

import (
	"context"
	"net/url"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/links"
	"github.com/donseba/go-webthings/pages"
)

// linkSources lists the published albums in locale with the links in their
// descriptions, for the site's link check (webthings.Parts.Links). An
// album's address is below the instance's page; albums of an instance on
// no page have none, and their links lead from the home page.
func (s *Service) linkSources(ctx context.Context, locale string) ([]links.Source, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	items, err := s.albums.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	paths := pages.Links(s.public, base)
	sources := make([]links.Source, 0, len(items))

	for _, item := range items {
		text, _ := item.Text(locale)

		// Published albums have a slug.
		address, _ := paths("album", "slug", text.Slug)

		sources = append(sources, links.Source{
			ID:       item.ID,
			Title:    text.Title,
			URL:      address,
			EditURL:  s.adminURL(admin.RouteName(s.key, "edit"), "id", item.ID, url.Values{"locale": {locale}}),
			Markdown: []string{text.Description},
		})
	}

	return sources, nil
}

// address is the address of an album on the site in locale, from its slug
// there, for the warnings about links to it in its editor
// (editor.Linked); empty for an instance on no page.
func (s *Service) address(ctx context.Context, item AlbumItem, locale string) (string, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return "", err
	}

	text, _ := item.Text(locale)

	// An album without a slug in locale has no address there.
	address, _ := pages.Links(s.public, base)("album", "slug", text.Slug)

	return address, nil
}
