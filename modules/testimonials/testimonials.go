// Package testimonials shows quotes from customers or partners: a quote, the
// author's name, role and organisation, and an optional photo. Testimonials
// are kept in a manually ordered list and published one by one. Add an
// instance with:
//
//	app.Use(testimonials.Module("testimonials", "Testimonials"))
//
// When the module is mounted on a page, the website renders
// testimonials/list.gohtml with a ListView as its data. Every template can
// also show testimonials, for example on the home page, without mounting the
// module:
//
//	{{range testimonials "testimonials" 3}}<blockquote>{{.Quote}} — {{.Author}}</blockquote>{{end}}
//
// The limit counts from the top of the list; 0 or less shows all of them. From
// Go, Of(app, key).List returns the same list and Random a random choice.
package testimonials

import (
	"context"
	"embed"
	"io/fs"
	"math/rand/v2"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-router"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// Fields are the language-independent parts of a testimonial.
type Fields struct {
	Photo  string `db:"photo_id,null"`
	Author string `db:"author"`
}

// Text is a testimonial in one language. Only the quote is required.
type Text struct {
	Quote        string `db:"quote"`
	Role         string `db:"author_role"`
	Organisation string `db:"organisation"`
}

// Item is a testimonial with its translations, as the store keeps it.
type Item = content.Item[Fields, Text]

func spec() content.Spec[Fields, Text] {
	return content.Spec[Fields, Text]{
		Table: "wt_testimonial",
		Complete: func(text Text) bool {
			return text.Quote != ""
		},
		Check: func(fields Fields, translations []content.Translation[Text]) error {
			if content.TooLong(200, fields.Author) {
				return content.ErrTooLong
			}

			for _, translation := range translations {
				text := translation.Text
				if content.TooLong(50000, text.Quote) || content.TooLong(500, text.Role, text.Organisation) {
					return content.ErrTooLong
				}
			}

			return nil
		},
		Media: func(fields Fields, _ []content.Translation[Text]) []string {
			return []string{fields.Photo}
		},
	}
}

// Service is one Testimonials instance, for the website's own code.
type Service struct {
	key      string
	items    *content.Collection[Fields, Text]
	frontend *frontend.Frontend
	pages    *pages.Service
	// api are the instance's endpoints in the content API (see apiRoutes).
	api *router.Router
}

// Store gives write access to this instance's testimonials, for imports and seeds.
// Admin edits go through the same store.
func (s *Service) Store() content.Store[Fields, Text] {
	return s.items.In(s.key)
}

// Of returns the Testimonials instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// Testimonial is a published testimonial as templates see it.
type Testimonial struct {
	ID           string
	Quote        string
	Author       string
	Role         string
	Organisation string
	// PhotoURL is the photo as pages show it, at most media.ImageWidth
	// wide (see frontend.Frontend.Image); PhotoSrcset lists smaller
	// copies for a srcset attribute, PhotoWidth and PhotoHeight are the
	// size of PhotoURL and PhotoOriginalURL is the uploaded file, for
	// downloads. All are empty when the testimonial has no photo.
	PhotoURL         string
	PhotoSrcset      string
	PhotoOriginalURL string
	PhotoWidth       int
	PhotoHeight      int
}

// ListView is the data of testimonials/list.gohtml.
type ListView struct {
	// Title is the title of the page the module is mounted on.
	Title        string
	Testimonials []Testimonial
}

// List returns the published testimonials that have a quote in locale, in
// their order.
func (s *Service) List(ctx context.Context, locale string) ([]Testimonial, error) {
	items, err := s.items.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	testimonials := make([]Testimonial, 0, len(items))

	for _, item := range items {
		testimonials = append(testimonials, s.testimonial(ctx, item, locale))
	}

	return testimonials, nil
}

// Random returns up to limit published testimonials in locale, in random
// order, for example to show a few on the home page. A limit of 0 or less
// returns all of them, shuffled.
func (s *Service) Random(ctx context.Context, locale string, limit int) ([]Testimonial, error) {
	testimonials, err := s.List(ctx, locale)
	if err != nil {
		return nil, err
	}

	rand.Shuffle(len(testimonials), func(i, j int) {
		testimonials[i], testimonials[j] = testimonials[j], testimonials[i]
	})

	return limited(testimonials, limit), nil
}

// limited returns the first limit testimonials, or all of them when limit is
// 0 or less.
func limited(testimonials []Testimonial, limit int) []Testimonial {
	if limit > 0 && limit < len(testimonials) {
		return testimonials[:limit]
	}

	return testimonials
}

func (s *Service) testimonial(ctx context.Context, item Item, locale string) Testimonial {
	text, _ := item.Text(locale)

	shown := s.frontend.Image(ctx, item.Fields.Photo)

	return Testimonial{
		ID:               item.ID,
		Quote:            text.Quote,
		Author:           item.Fields.Author,
		Role:             text.Role,
		Organisation:     text.Organisation,
		PhotoURL:         shown.URL,
		PhotoSrcset:      shown.Srcset,
		PhotoOriginalURL: shown.OriginalURL,
		PhotoWidth:       shown.Width,
		PhotoHeight:      shown.Height,
	}
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
