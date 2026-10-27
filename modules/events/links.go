package events

import (
	"context"
	"net/url"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/links"
	"github.com/donseba/go-webthings/pages"
)

// linkSources lists the published events in locale with the links in their
// text, for the site's link check (webthings.Parts.Links). An event's
// address is below the instance's page; events of an instance on no page
// have none, and their links lead from the home page.
func (s *Service) linkSources(ctx context.Context, locale string) ([]links.Source, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	items, err := s.events.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	paths := pages.Links(s.public, base)
	sources := make([]links.Source, 0, len(items))

	for _, item := range items {
		text, _ := item.Text(locale)

		// Published events have a slug.
		address, _ := paths("event", "slug", text.Slug)

		sources = append(sources, links.Source{
			ID:       item.ID,
			Title:    text.Title,
			URL:      address,
			EditURL:  s.adminURL(admin.RouteName(s.key, "edit"), "id", item.ID, url.Values{"locale": {locale}}),
			Markdown: []string{text.Body},
			Revision: item.Revision.ID,
		})
	}

	return sources, nil
}

// address is the address of an event, any revision of it, on the site in
// locale, from its slug there, for the warnings about links to it in its
// editor (editor.Linked); empty for an instance on no page.
func (s *Service) address(ctx context.Context, item EventItem, locale string) (string, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return "", err
	}

	text, _ := item.Text(locale)

	// An event without a slug in locale has no address there.
	address, _ := pages.Links(s.public, base)("event", "slug", text.Slug)

	return address, nil
}
