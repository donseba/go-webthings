package forms

import (
	"context"
	"net/url"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/links"
)

// linkSources lists the published form in locale with the links in its
// introduction and success message, for the site's link check
// (webthings.Parts.Links). Its address is the page it is on; a form on no
// page is not on the website.
func (s *Service) linkSources(ctx context.Context, locale string) ([]links.Source, error) {
	address, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil || address == "" {
		return nil, err
	}

	items, err := s.forms.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	sources := make([]links.Source, 0, len(items))

	for _, item := range items {
		text, _ := item.Text(locale)

		sources = append(sources, links.Source{
			ID:       item.ID,
			Title:    text.Title,
			URL:      address,
			EditURL:  s.adminURL(admin.RouteName(s.key, "edit"), "id", item.ID, url.Values{"locale": {locale}}),
			Markdown: []string{text.Intro, text.Success},
		})
	}

	return sources, nil
}
