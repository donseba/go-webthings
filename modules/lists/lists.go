// Package lists keeps short ordered lists such as FAQs, services, features
// or steps. One module instance is one list (for team members with photos,
// roles and pages of their own, use the people module):
//
//	app.Use(lists.Module("faq", "FAQ"), lists.Module("services", "Services"))
//
// Each entry has a title and a Markdown body per language, and optionally an
// image and a link. Entries are published one by one and keep the order set
// in the admin (the move buttons on the list's overview).
//
// When the list is mounted on a page, the website renders lists/list.gohtml
// with a ListView. To show a list elsewhere, for example the FAQ on the home
// page, use the entries template function ({{range entries "faq"}}) or
// Of(app, "faq").Entries(ctx, locale) from Go; both return the same entries.
//
// Every Entry has an ID. The site search links to an entry on the list's page
// at #<ID>, so give each entry id="{{.ID}}" as the generated starting point
// does.
package lists

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/pages"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// Fields are the language-independent parts of an entry.
type Fields struct {
	Image string `db:"image_id,null"`
	Link  string `db:"link_url"`
}

// Text is an entry in one language. Body is Markdown.
type Text struct {
	Title string `db:"title"`
	Body  string `db:"body"`
}

// Item is a list entry with its translations, as the store keeps it.
type Item = content.Item[Fields, Text]

var errInvalidLink = errors.New("lists_invalid_link")

func spec() content.Spec[Fields, Text] {
	return content.Spec[Fields, Text]{
		Table: "wt_lists_entry",
		Complete: func(text Text) bool {
			return text.Title != ""
		},
		Check: func(fields Fields, translations []content.Translation[Text]) error {
			if !validLink(fields.Link) {
				return errInvalidLink
			}

			for _, translation := range translations {
				if content.TooLong(200, translation.Text.Title) || content.TooLong(64<<10, translation.Text.Body) {
					return content.ErrTooLong
				}
			}

			return nil
		},
		Media: func(fields Fields, translations []content.Translation[Text]) []string {
			ids := []string{fields.Image}

			for _, translation := range translations {
				ids = append(ids, markdown.MediaIDs(translation.Text.Body)...)
			}

			return ids
		},
	}
}

// validLink accepts no link, a path on the site, an anchor, or a web, mail
// or phone address, of at most 2000 characters (go-form's "link" rule).
func validLink(link string) bool {
	if link == "" {
		return true
	}

	return !content.TooLong(2000, link) && form.ValidURL("link", link)
}

// Service is one list, for the website's own code.
type Service struct {
	key      string
	entries  *content.Collection[Fields, Text]
	frontend *frontend.Frontend
	pages    *pages.Service
	// adminURL builds admin addresses by route name (webthings.Env.AdminURL).
	adminURL func(name string, args ...any) string
	// api are the list's endpoints in the content API (see apiRoutes).
	api *router.Router
}

// Store gives write access to this instance's entries, for imports and seeds.
// Admin edits go through the same store.
func (s *Service) Store() content.Store[Fields, Text] {
	return s.entries.In(s.key)
}

// Of returns the list with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// Entry is a published entry as templates see it.
type Entry struct {
	ID    string
	Title string
	Body  template.HTML
	// ImageURL is the entry's image as pages show it, at most media.ImageWidth
	// wide (see frontend.Frontend.Image); ImageSrcset lists smaller
	// copies for a srcset attribute, ImageWidth and ImageHeight are the
	// size of ImageURL and ImageOriginalURL is the uploaded file, for
	// downloads. All are empty when the entry has no image.
	ImageURL         string
	ImageSrcset      string
	ImageOriginalURL string
	ImageWidth       int
	ImageHeight      int
	// Link is empty when not set. It is a template.URL because the store only
	// accepts paths, anchors and http(s), mailto: and tel: addresses, so
	// templates can use it in href without html/template replacing tel:
	// links with #ZgotmplZ.
	Link template.URL
}

// ListView is the data of lists/list.gohtml.
type ListView struct {
	// Key is the list's instance key, for example "faq", so one template can
	// show several lists differently.
	Key string
	// Title is the title of the page the list is mounted on.
	Title   string
	Entries []Entry
}

// Entries returns the published entries in locale, in their order.
func (s *Service) Entries(ctx context.Context, locale string) ([]Entry, error) {
	items, err := s.entries.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(items))

	for _, item := range items {
		text, _ := item.Text(locale)
		shown := s.frontend.Image(ctx, item.Fields.Image)

		entries = append(entries, Entry{
			ID:               item.ID,
			Title:            text.Title,
			Body:             s.frontend.Markdown(ctx, text.Body),
			ImageURL:         shown.URL,
			ImageSrcset:      shown.Srcset,
			ImageOriginalURL: shown.OriginalURL,
			ImageWidth:       shown.Width,
			ImageHeight:      shown.Height,
			Link:             template.URL(item.Fields.Link), //nolint:gosec // validLink checked it when it was saved.
		})
	}

	return entries, nil
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
