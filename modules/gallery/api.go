package gallery

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/donseba/go-webthings/api"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-router"
)

// APIAlbum is a published album in the content API (see package api), in
// the list of /api/v1/<key>/albums and, with its images, at
// /api/v1/<key>/albums/{id} (APIAlbumImages).
type APIAlbum struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
	// DescriptionHTML is the album's description as HTML (api.HTML); only
	// the album's own endpoint has it.
	DescriptionHTML string `json:"description_html,omitempty"`
	// Cover is the cover image, or the first image when the editors chose
	// none; null for an album without images.
	Cover      *api.Image `json:"cover"`
	ImageCount int        `json:"image_count"`
	// URL is the album's page, null while the gallery is on no page; APIURL
	// the album in the API.
	URL       *string   `json:"url"`
	APIURL    string    `json:"api_url"`
	UpdatedAt time.Time `json:"updated_at"`
}

// APIAlbumImages is a published album with its images in their order, at
// /api/v1/<key>/albums/{id}.
type APIAlbumImages struct {
	APIAlbum

	Images []APIImage `json:"images"`
}

// APIImage is an image of an album: its media library ID, its title in the
// request's language and the image as pages show it (api.Image).
type APIImage struct {
	ID    string `json:"id"`
	Title string `json:"title"`

	api.Image
}

// apiRoutes are the instance's endpoints in the content API, below
// /api/v1/<key>: the published albums in the editors' order, which
// ?slug=<slug> filters, and an album with its images by its ID.
func (s *Service) apiRoutes() *router.Router {
	routes := api.NewRouter()

	routes.Get("/albums", s.apiAlbums).As("albums")
	routes.Get("/albums/{id}", s.apiAlbum).As("album")

	return routes
}

// apiAlbums answers /albums: a page (api.Paginate) of the published albums.
func (s *Service) apiAlbums(w http.ResponseWriter, r *http.Request) {
	locale := api.Locale(r)

	items, err := s.albums.ListPublished(r.Context(), s.key, locale)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	paths, err := s.apiPaths(r)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	slug := r.URL.Query().Get("slug")
	albums := []APIAlbum{}

	for _, item := range items {
		text, _ := item.Text(locale)

		if slug != "" && text.Slug != slug {
			continue
		}

		albums = append(albums, s.apiItem(r, item, paths, false))
	}

	api.Write(w, r, api.Paginate(r, albums))
}

// apiAlbum answers /albums/{id}: a published album in the request's
// language with its images.
func (s *Service) apiAlbum(w http.ResponseWriter, r *http.Request) {
	item, err := s.albums.Published(r.Context(), s.key, r.PathValue("id"), api.Locale(r))
	if errors.Is(err, content.ErrNotFound) {
		api.NotFound(w, r)

		return
	}

	if err != nil {
		api.Fail(w, r, err)

		return
	}

	paths, err := s.apiPaths(r)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	images, err := s.apiImages(r, item)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	text, _ := item.Text(api.Locale(r))
	shown := append([]string{item.Fields.Cover}, item.Fields.Images...)

	api.Modified(w, api.ItemDates(r, s.key, item, text.Description, shown...)...)
	api.Write(w, r, APIAlbumImages{APIAlbum: s.apiItem(r, item, paths, true), Images: images})
}

// apiPaths are the addresses of the instance's routes on its page in the
// request's language.
func (s *Service) apiPaths(r *http.Request) (router.PathFunc, error) {
	base, err := s.pages.MountURL(r.Context(), s.key, api.Locale(r))
	if err != nil {
		return nil, err
	}

	return pages.Links(s.public, base), nil
}

// apiItem is a published album in the request's language; body adds its
// description.
func (s *Service) apiItem(r *http.Request, item AlbumItem, paths router.PathFunc, body bool) APIAlbum {
	locale := api.Locale(r)
	text, _ := item.Text(locale)

	cover := item.Fields.Cover
	if cover == "" && len(item.Fields.Images) > 0 {
		cover = item.Fields.Images[0]
	}

	// Published albums have a slug.
	address, _ := paths("album", "slug", text.Slug)
	self, _ := s.api.RoutePathArgs("album", "id", item.ID)

	album := APIAlbum{
		ID:         item.ID,
		Slug:       text.Slug,
		Title:      text.Title,
		Cover:      api.ImageOf(r, cover),
		ImageCount: len(item.Fields.Images),
		URL:        api.Optional(api.URL(r, address)),
		APIURL:     api.URL(r, self+"?"+url.Values{"locale": {locale}}.Encode()),
		UpdatedAt:  item.UpdatedAt.UTC(),
	}

	if body {
		album.DescriptionHTML = api.HTML(r, text.Description)
	}

	return album
}

// apiImages are an album's images in their order, skipping missing ones.
func (s *Service) apiImages(r *http.Request, item AlbumItem) ([]APIImage, error) {
	ctx := r.Context()
	images := []APIImage{}

	for _, id := range item.Fields.Images {
		asset, err := s.media.Get(ctx, id)
		if errors.Is(err, media.ErrNotFound) {
			continue
		}

		if err != nil {
			return nil, err
		}

		shown := api.ImageOf(r, asset.ID)
		if shown == nil {
			continue
		}

		images = append(images, APIImage{
			ID:    asset.ID,
			Title: asset.Text(api.Locale(r)).Title,
			Image: *shown,
		})
	}

	return images, nil
}
