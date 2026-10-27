package lists

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/donseba/go-webthings/api"
	"github.com/donseba/go-webthings/content"

	"github.com/donseba/go-router"
)

// APIEntry is a published entry in the content API (see package api), at
// /api/v1/<key>/entries/{id} and in the list of /api/v1/<key>/entries.
// Entries have no page of their own: the list's page shows them whole, so
// the list has their text too.
type APIEntry struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	BodyHTML string     `json:"body_html"`
	Image    *api.Image `json:"image"`
	// Link is the entry's link as a visitor follows it from the list's
	// page: a path of the site made absolute, an anchor on the list's
	// page, other addresses (https:, mailto:, tel:) as they are; null
	// without a link.
	Link *string `json:"link"`
	// URL is the entry on the list's page (#<id>), null while the list is
	// on no page; APIURL the entry in the API.
	URL       *string   `json:"url"`
	APIURL    string    `json:"api_url"`
	UpdatedAt time.Time `json:"updated_at"`
}

// apiRoutes are the instance's endpoints in the content API, below
// /api/v1/<key>: the published entries in the editors' order and an entry
// by its ID.
func (s *Service) apiRoutes() *router.Router {
	routes := api.NewRouter()

	routes.Get("/entries", s.apiEntries).As("entries")
	routes.Get("/entries/{id}", s.apiEntry).As("entry")

	return routes
}

// apiEntries answers /entries: a page (api.Paginate) of the published
// entries.
func (s *Service) apiEntries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := api.Locale(r)

	items, err := s.entries.ListPublished(ctx, s.key, locale)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	page, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	entries := make([]APIEntry, 0, len(items))

	for _, item := range items {
		entries = append(entries, s.apiItem(r, item, page))
	}

	api.Write(w, r, api.Paginate(r, entries))
}

// apiEntry answers /entries/{id}: a published entry in the request's
// language.
func (s *Service) apiEntry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := api.Locale(r)

	item, err := s.entries.Published(ctx, s.key, r.PathValue("id"), locale)
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

	text, _ := item.Text(locale)

	api.Modified(w, api.ItemDates(r, s.key, item, text.Body, item.Fields.Image)...)
	api.Write(w, r, s.apiItem(r, item, page))
}

// apiItem is a published entry in the request's language; page is the
// address of the list's page in that language, empty for none.
func (s *Service) apiItem(r *http.Request, item Item, page string) APIEntry {
	locale := api.Locale(r)
	text, _ := item.Text(locale)
	self, _ := s.api.RoutePathArgs("entry", "id", item.ID)

	entry := APIEntry{
		ID:        item.ID,
		Title:     text.Title,
		BodyHTML:  api.HTML(r, text.Body),
		Image:     api.ImageOf(r, item.Fields.Image),
		Link:      apiLink(r, item.Fields.Link, page),
		APIURL:    api.URL(r, self+"?"+url.Values{"locale": {locale}}.Encode()),
		UpdatedAt: item.UpdatedAt.UTC(),
	}

	if page != "" {
		entry.URL = api.Optional(api.URL(r, page+"#"+item.ID))
	}

	return entry
}

// apiLink is an entry's link as a visitor follows it from the list's page
// (see APIEntry.Link); page is that page's address, empty for none.
func apiLink(r *http.Request, link, page string) *string {
	switch {
	case link == "":
		return nil
	case strings.HasPrefix(link, "#") && page != "":
		return api.Optional(api.URL(r, page+link))
	case strings.HasPrefix(link, "/") && !strings.HasPrefix(link, "//"):
		return api.Optional(api.URL(r, link))
	}

	return &link
}
