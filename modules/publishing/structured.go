package publishing

import (
	"context"
	"time"

	"github.com/donseba/go-webthings/frontend"
)

// article is the structured data of a post's page (see frontend.Thing): an
// Article in locale with its social image, published on the post's date and
// modified when its revision was saved, by the site's organisation, as
// posts name no author.
func (s *Service) article(ctx context.Context, item PostItem, post Post, locale string) frontend.Thing {
	organization := frontend.Thing{"@id": frontend.OrganizationID}

	modified := item.Revision.CreatedAt
	if modified.IsZero() {
		modified = item.UpdatedAt
	}

	sections := make([]string, 0, len(post.Categories))

	for _, category := range post.Categories {
		sections = append(sections, category.Title)
	}

	return frontend.Thing{
		"@type":          "Article",
		"headline":       post.Title,
		"description":    post.Excerpt,
		"url":            post.URL,
		"image":          s.frontend.SocialImageURL(ctx, post.ImageURL),
		"datePublished":  day(post.Date),
		"dateModified":   instant(modified),
		"inLanguage":     locale,
		"articleSection": sections,
		"author":         organization,
		"publisher":      organization,
	}
}

// day is the date of t as schema.org writes dates, "" for a zero time.
func day(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.Format(time.DateOnly)
}

// instant is t as schema.org writes a date and time, "" for a zero time.
func instant(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.Format(time.RFC3339)
}
