package people

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/donseba/go-webthings/api"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-router"
)

// APIPerson is a published person in the content API (see package api), at
// /api/v1/<key>/people/{id} and in the list of /api/v1/<key>/people, which
// leaves out BioHTML. The contact details are those the person's page
// shows, also in its structured data; each is null when not given.
type APIPerson struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Group string `json:"group"`
	Role  string `json:"role"`
	// BioHTML is the biography as HTML (api.HTML); only the person's own
	// endpoint has it.
	BioHTML  string     `json:"bio_html,omitempty"`
	Photo    *api.Image `json:"photo"`
	Email    *string    `json:"email"`
	Phone    *string    `json:"phone"`
	Website  *string    `json:"website"`
	LinkedIn *string    `json:"linkedin"`
	// URL is the person's page, null while the instance is on no page;
	// APIURL the person in the API.
	URL       *string   `json:"url"`
	APIURL    string    `json:"api_url"`
	UpdatedAt time.Time `json:"updated_at"`
}

// apiRoutes are the instance's endpoints in the content API, below
// /api/v1/<key>: the published people in the editors' order, which
// ?slug=<slug> and ?group=<group> filter, and a person by their ID.
func (s *Service) apiRoutes() *router.Router {
	routes := api.NewRouter()

	routes.Get("/people", s.apiPeople).As("people")
	routes.Get("/people/{id}", s.apiPerson).As("person")

	return routes
}

// apiPeople answers /people: a page (api.Paginate) of the published
// people, those with the slug or in the group the query names, if it does.
// Group names compare as on the site: in the request's language, ignoring
// case (see Service.InGroup).
func (s *Service) apiPeople(w http.ResponseWriter, r *http.Request) {
	locale := api.Locale(r)

	items, err := s.items.ListPublished(r.Context(), s.key, locale)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	paths, err := s.apiPaths(r)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	query := r.URL.Query()
	slug := query.Get("slug")
	group := strings.TrimSpace(query.Get("group"))
	people := []APIPerson{}

	for _, item := range items {
		if slug != "" && item.Fields.Slug != slug {
			continue
		}

		text, _ := item.Text(locale)
		if group != "" && !strings.EqualFold(strings.TrimSpace(text.Group), group) {
			continue
		}

		people = append(people, s.apiItem(r, item, paths, false))
	}

	api.Write(w, r, api.Paginate(r, people))
}

// apiPerson answers /people/{id}: a published person in the request's
// language.
func (s *Service) apiPerson(w http.ResponseWriter, r *http.Request) {
	item, err := s.items.Published(r.Context(), s.key, r.PathValue("id"), api.Locale(r))
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

	text, _ := item.Text(api.Locale(r))

	api.Modified(w, api.ItemDates(r, s.key, item, text.Bio, item.Fields.Photo)...)
	api.Write(w, r, s.apiItem(r, item, paths, true))
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

// apiItem is a published person in the request's language; body adds
// their biography.
func (s *Service) apiItem(r *http.Request, item Item, paths router.PathFunc, body bool) APIPerson {
	locale := api.Locale(r)
	text, _ := item.Text(locale)
	fields := item.Fields

	// Published people have a slug.
	address, _ := paths("person", "slug", fields.Slug)
	self, _ := s.api.RoutePathArgs("person", "id", item.ID)

	person := APIPerson{
		ID:        item.ID,
		Slug:      fields.Slug,
		Name:      fields.Name,
		Group:     text.Group,
		Role:      text.Role,
		Photo:     api.ImageOf(r, fields.Photo),
		Email:     api.Optional(fields.Email),
		Phone:     api.Optional(fields.Phone),
		Website:   api.Optional(fields.Website),
		LinkedIn:  api.Optional(fields.LinkedIn),
		URL:       api.Optional(api.URL(r, address)),
		APIURL:    api.URL(r, self+"?"+url.Values{"locale": {locale}}.Encode()),
		UpdatedAt: item.UpdatedAt.UTC(),
	}

	if body {
		person.BioHTML = api.HTML(r, text.Bio)
	}

	return person
}
