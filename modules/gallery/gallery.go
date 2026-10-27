// Package gallery shows photo albums. An album has a translated title, slug and
// Markdown description, an optional cover image and an ordered list of images
// from the media library. Captions and alt text are the images' own per-language
// title and alt text in the library. Add one instance per gallery, for example:
//
//	app.Use(gallery.Module("photos", "Photos"))
//
// Mounted on a page, the module serves two templates:
//
//   - gallery/list.gohtml at the page itself, with a ListView of the published
//     albums (without their images).
//   - gallery/album.gohtml at /{slug}, with an Album including its Images.
//
// Album.CoverURL is the cover image, or the first image when no cover is set.
// Images are shown as copies at most media.ImageWidth wide with a srcset
// (see frontend.Frontend.Image); Image.LargeURL is a copy for a lightbox and
// Image.OriginalURL the uploaded file.
// The album form chooses the images and adds new ones at the end; "Image
// order" in the admin moves them up and down.
package gallery

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"slices"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-router"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// AlbumFields are the language-independent parts of an album.
type AlbumFields struct {
	Cover  string      `db:"cover_id,null"`
	Images content.IDs `db:"image_ids"`
}

// AlbumText is an album in one language. Description is Markdown.
type AlbumText struct {
	Slug        string `db:"slug"`
	Title       string `db:"title"`
	Description string `db:"description"`
}

// AlbumItem is an album with its translations, as the store keeps it.
type AlbumItem = content.Item[AlbumFields, AlbumText]

const maxImages = 500

var errTooManyImages = errors.New("gallery_too_many_images")

func albumSpec() content.Spec[AlbumFields, AlbumText] {
	return content.Spec[AlbumFields, AlbumText]{
		Table:      "wt_gallery_album",
		SlugColumn: "slug",
		Complete: func(text AlbumText) bool {
			return text.Title != "" && text.Slug != ""
		},
		Check: func(fields AlbumFields, translations []content.Translation[AlbumText]) error {
			if len(fields.Images) > maxImages {
				return errTooManyImages
			}

			for _, translation := range translations {
				text := translation.Text
				if content.TooLong(200, text.Title) || content.TooLong(64<<10, text.Description) {
					return content.ErrTooLong
				}
			}

			return nil
		},
		Media: func(fields AlbumFields, translations []content.Translation[AlbumText]) []string {
			ids := append([]string{fields.Cover}, fields.Images...)

			for _, translation := range translations {
				ids = append(ids, markdown.MediaIDs(translation.Text.Description)...)
			}

			return ids
		},
	}
}

// Service is one Gallery instance, for the website's own code (for example
// albums on the home page).
type Service struct {
	key      string
	albums   *content.Collection[AlbumFields, AlbumText]
	media    *media.Service
	frontend *frontend.Frontend
	pages    *pages.Service
	// adminURL builds admin addresses by route name (webthings.Env.AdminURL).
	adminURL func(name string, args ...any) string
	// public are the routes below the module's page (see handler), api
	// those of its endpoints in the content API (see apiRoutes).
	public *router.Router
	api    *router.Router
}

// Store gives write access to this instance's albums, for imports and seeds.
// Admin edits go through the same store.
func (s *Service) Store() content.Store[AlbumFields, AlbumText] {
	return s.albums.In(s.key)
}

// Of returns the Gallery instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// Album is a published album as templates see it. Images is only filled on
// the album page.
type Album struct {
	ID          string
	Title       string
	Description template.HTML
	// URL is the album's page, empty when the gallery is on no page.
	URL string
	// CoverURL is the cover as pages show it, at most media.ImageWidth wide;
	// CoverSrcset lists smaller copies for a srcset attribute, CoverWidth
	// and CoverHeight are the size of CoverURL and CoverOriginalURL is the
	// uploaded file. All are empty for an album without images.
	CoverURL         string
	CoverSrcset      string
	CoverOriginalURL string
	CoverWidth       int
	CoverHeight      int
	ImageCount       int
	Images           []Image
}

// Image is one image of an album. Title and Alt come from the media library
// in the page's language.
type Image struct {
	ID string
	// URL is the image as pages show it, at most media.ImageWidth wide, and
	// Srcset smaller copies for a srcset attribute; Width and Height are the
	// size of URL.
	URL    string
	Srcset string
	Width  int
	Height int
	// LargeURL is a copy at most media.LargeImageWidth wide, for the image
	// on its own, such as in a lightbox.
	LargeURL string
	// OriginalURL is the uploaded file, for downloads.
	OriginalURL string
	Title       string
	Alt         string
}

// ListView is the data of gallery/list.gohtml.
type ListView struct {
	Title  string
	Albums []Album
}

// Albums returns the published albums in locale in their admin order, for use
// outside the module's own pages. Links point to the page the module is
// mounted on; they are empty when it is on no page.
func (s *Service) Albums(ctx context.Context, locale string) ([]Album, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	return s.list(ctx, locale, pages.Links(s.public, base))
}

// Album returns one published album with its images, for example for a
// carousel on another page. The slug may be from any language; the album is
// returned in locale.
func (s *Service) Album(ctx context.Context, locale, slug string) (Album, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return Album{}, err
	}

	item, err := s.bySlug(ctx, locale, slug)
	if err != nil {
		return Album{}, err
	}

	album := s.album(ctx, item, locale, pages.Links(s.public, base))

	album.Images, err = s.images(ctx, item, locale)

	return album, err
}

// bySlug finds a published album complete in locale by its slug in any language.
func (s *Service) bySlug(ctx context.Context, locale, slug string) (AlbumItem, error) {
	items, err := s.albums.ListPublished(ctx, s.key, locale)
	if err != nil {
		return AlbumItem{}, err
	}

	for _, item := range items {
		for _, translation := range item.Translations {
			if translation.Text.Slug == slug {
				return item, nil
			}
		}
	}

	return AlbumItem{}, content.ErrNotFound
}

// list returns the published albums in locale, their URLs by route name
// from paths.
func (s *Service) list(ctx context.Context, locale string, paths router.PathFunc) ([]Album, error) {
	items, err := s.albums.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	albums := make([]Album, 0, len(items))

	for _, item := range items {
		albums = append(albums, s.album(ctx, item, locale, paths))
	}

	return albums, nil
}

// album converts an item without loading its images; its URL is by route
// name from paths.
func (s *Service) album(ctx context.Context, item AlbumItem, locale string, paths router.PathFunc) Album {
	text, _ := item.Text(locale)

	cover := item.Fields.Cover
	if cover == "" && len(item.Fields.Images) > 0 {
		cover = item.Fields.Images[0]
	}

	shown := s.frontend.Image(ctx, cover)
	album := Album{
		ID:               item.ID,
		Title:            text.Title,
		Description:      s.frontend.Markdown(ctx, text.Description),
		CoverURL:         shown.URL,
		CoverSrcset:      shown.Srcset,
		CoverOriginalURL: shown.OriginalURL,
		CoverWidth:       shown.Width,
		CoverHeight:      shown.Height,
		ImageCount:       len(item.Fields.Images),
	}

	// Published albums have a slug.
	album.URL, _ = paths("album", "slug", text.Slug)

	return album
}

// images loads an album's images in their order, skipping missing ones.
func (s *Service) images(ctx context.Context, item AlbumItem, locale string) ([]Image, error) {
	images := make([]Image, 0, len(item.Fields.Images))

	for _, id := range item.Fields.Images {
		asset, err := s.media.Get(ctx, id)
		if errors.Is(err, media.ErrNotFound) {
			continue
		}

		if err != nil {
			return nil, err
		}

		text := asset.Text(locale)
		shown := s.frontend.Image(ctx, asset.ID)
		images = append(images, Image{
			ID:          asset.ID,
			URL:         shown.URL,
			Srcset:      shown.Srcset,
			Width:       shown.Width,
			Height:      shown.Height,
			LargeURL:    s.media.Image(ctx, asset.ID, media.LargeImageWidth).URL,
			OriginalURL: shown.OriginalURL,
			Title:       text.Title,
			Alt:         text.AltText,
		})
	}

	return images, nil
}

// ordered returns the checked IDs, keeping the stored order of those already in
// the album and adding new ones at the end.
func ordered(stored, checked []string) content.IDs {
	var out content.IDs

	for _, id := range stored {
		if slices.Contains(checked, id) && !out.Has(id) {
			out = append(out, id)
		}
	}

	for _, id := range checked {
		if !out.Has(id) {
			out = append(out, id)
		}
	}

	return out
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
