package publishing

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/donseba/go-webthings/content"
)

// ErrDraft is returned for a draft without a language or title.
var ErrDraft = errors.New("publishing_invalid_draft")

// DraftInput is a post another module proposes, for example an accepted
// visitor submission. Editors review and publish it in the admin.
type DraftInput struct {
	// Locale is the language of the text, for example "nl".
	Locale string
	Title  string
	// Slug is derived from the title when empty (see content.Slug).
	Slug    string
	Excerpt string
	// Body is Markdown.
	Body string
	// ImageID is a media asset for the featured image.
	ImageID string
	// Date is the post's date; zero is today.
	Date time.Time
	// Author is the ID of the account the revision is credited to, empty for
	// none. Note describes the revision in the history, for example who sent
	// the text in.
	Author string
	Note   string
}

// Draft saves a new, unpublished post with the instance's own rules and
// returns its ID.
func (s *Service) Draft(ctx context.Context, input DraftInput) (string, error) {
	title := strings.TrimSpace(input.Title)
	if input.Locale == "" || title == "" {
		return "", ErrDraft
	}

	slug := content.Slug(input.Slug)
	if slug == "" {
		slug = content.Slug(title)
	}

	if slug == "" {
		slug = "post"
	}

	date := input.Date
	if date.IsZero() {
		date = time.Now()
	}

	item, err := s.posts.Save(ctx, s.key, content.Draft[PostFields, PostText]{
		Fields: PostFields{
			Date:  date.UTC(),
			Image: input.ImageID,
		},
		Translations: []content.Translation[PostText]{{
			Locale: input.Locale,
			Text: PostText{
				Slug:    slug,
				Title:   title,
				Excerpt: input.Excerpt,
				Body:    input.Body,
			},
		}},
		Author: input.Author,
		Note:   input.Note,
	})
	if err != nil {
		return "", err
	}

	return item.ID, nil
}
