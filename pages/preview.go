package pages

import (
	"context"
	"errors"
	"strings"

	"github.com/donseba/go-webthings/content"
)

// Preview returns the page revision an editor previews at path in locale:
// a revision of the page published at path, or of a page whose revision
// would be at path (see PreviewPath), such as a page that was never
// published or whose slug changes. A revision of any other page is
// ErrNotFound, so a preview never shows one page's text at the address of
// another; so is a revision without text in locale.
func (s *Service) Preview(ctx context.Context, locale, path, revisionID string) (Page, error) {
	page, err := s.store.Find(ctx, instance, revisionID)
	if err != nil {
		return Page{}, err
	}

	if _, ok := page.Text(locale); !ok {
		return Page{}, ErrNotFound
	}

	exact := "/" + strings.Trim(path, "/")

	resolved, err := s.Resolve(ctx, locale, exact)

	switch {
	case err == nil && resolved.Rest == "/" && resolved.Page.ID == page.ID:
		return page, nil
	case err == nil:
		// Another page is published here, or a module owns the address.
		return Page{}, ErrNotFound
	case !errors.Is(err, ErrNotFound):
		return Page{}, err
	}

	want, err := s.PreviewPath(ctx, page, locale)
	if err != nil {
		return Page{}, err
	}

	if want != exact {
		return Page{}, ErrNotFound
	}

	return page, nil
}

// PreviewPath is the path, without language prefix, at which a page
// revision shows in locale: below its parent's published path, with the
// revision's slug; "/" for the home page. ErrNotFound when the parent is
// not published in locale, or the revision has no text in it.
func (s *Service) PreviewPath(ctx context.Context, page Page, locale string) (string, error) {
	text, ok := page.Text(locale)
	if !ok {
		return "", ErrNotFound
	}

	if page.Fields.ParentID == "" {
		return "/", nil
	}

	parents, err := s.Alternates(ctx, page.Fields.ParentID)
	if err != nil {
		return "", err
	}

	parent, ok := parents[locale]
	if !ok || text.Slug == "" {
		return "", ErrNotFound
	}

	return strings.TrimSuffix(parent, "/") + "/" + text.Slug, nil
}

// Reviews is the review workflow of pages (see content.Reviewing).
// Publishing pages, by hand or scheduled, follows it through Publish.
func (s *Service) Reviews() content.Reviewing[Fields, Text] {
	return s.store.Reviews(instance)
}
