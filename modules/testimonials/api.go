package testimonials

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/donseba/go-webthings/api"
	"github.com/donseba/go-webthings/content"

	"github.com/donseba/go-router"
)

// APITestimonial is a published testimonial in the content API (see
// package api), at /api/v1/<key>/testimonials/{id} and in the list of
// /api/v1/<key>/testimonials. Quote is plain text, as the editors wrote it.
type APITestimonial struct {
	ID           string     `json:"id"`
	Quote        string     `json:"quote"`
	Author       string     `json:"author"`
	Role         string     `json:"role"`
	Organisation string     `json:"organisation"`
	Photo        *api.Image `json:"photo"`
	// URL is the page that lists the testimonials, null while the instance
	// is on no page; testimonials have no page of their own. APIURL is the
	// testimonial in the API.
	URL       *string   `json:"url"`
	APIURL    string    `json:"api_url"`
	UpdatedAt time.Time `json:"updated_at"`
}

// apiRoutes are the instance's endpoints in the content API, below
// /api/v1/<key>: the published testimonials in the editors' order and a
// testimonial by its ID.
func (s *Service) apiRoutes() *router.Router {
	routes := api.NewRouter()

	routes.Get("/testimonials", s.apiTestimonials).As("testimonials")
	routes.Get("/testimonials/{id}", s.apiTestimonial).As("testimonial")

	return routes
}

// apiTestimonials answers /testimonials: a page (api.Paginate) of the
// published testimonials.
func (s *Service) apiTestimonials(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := api.Locale(r)

	items, err := s.items.ListPublished(ctx, s.key, locale)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	page, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	testimonials := make([]APITestimonial, 0, len(items))

	for _, item := range items {
		testimonials = append(testimonials, s.apiItem(r, item, page))
	}

	api.Write(w, r, api.Paginate(r, testimonials))
}

// apiTestimonial answers /testimonials/{id}: a published testimonial in
// the request's language.
func (s *Service) apiTestimonial(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := api.Locale(r)

	item, err := s.items.Published(ctx, s.key, r.PathValue("id"), locale)
	if errors.Is(err, content.ErrNotFound) {
		api.NotFound(w, r)

		return
	}

	if err != nil {
		api.Fail(w, r, err)

		return
	}

	page, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	api.Modified(w, api.ItemDates(r, s.key, item, "", item.Fields.Photo)...)
	api.Write(w, r, s.apiItem(r, item, page))
}

// apiItem is a published testimonial in the request's language; page is
// the address of the instance's page in that language, empty for none.
func (s *Service) apiItem(r *http.Request, item Item, page string) APITestimonial {
	locale := api.Locale(r)
	text, _ := item.Text(locale)
	self, _ := s.api.RoutePathArgs("testimonial", "id", item.ID)

	return APITestimonial{
		ID:           item.ID,
		Quote:        text.Quote,
		Author:       item.Fields.Author,
		Role:         text.Role,
		Organisation: text.Organisation,
		Photo:        api.ImageOf(r, item.Fields.Photo),
		URL:          api.Optional(api.URL(r, page)),
		APIURL:       api.URL(r, self+"?"+url.Values{"locale": {locale}}.Encode()),
		UpdatedAt:    item.UpdatedAt.UTC(),
	}
}
