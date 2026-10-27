package directory

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/donseba/go-webthings/api"
	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-router"
)

// APIEntry is a published entry in the content API (see package api), at
// /api/v1/<key>/entries/{id} and in the list of /api/v1/<key>/entries,
// which leaves out DescriptionHTML. The contact details, address, position
// and opening hours are those the entry's page shows, also in its
// structured data; each is null when not given.
type APIEntry struct {
	ID       string   `json:"id"`
	Slug     string   `json:"slug"`
	Name     string   `json:"name"`
	Summary  string   `json:"summary"`
	Keywords []string `json:"keywords"`
	// DescriptionHTML is the entry's description as HTML (api.HTML); only
	// the entry's own endpoint has it.
	DescriptionHTML string      `json:"description_html,omitempty"`
	Category        APICategory `json:"category"`
	Image           *api.Image  `json:"image"`
	Website         *string     `json:"website"`
	Email           *string     `json:"email"`
	Phone           *string     `json:"phone"`
	Address         *APIAddress `json:"address"`
	Location        *APIPoint   `json:"location"`
	// OpeningHours are the days from Monday to Sunday, null for an entry
	// without opening hours; HoursNote is the editors' note about them.
	OpeningHours []APIDay `json:"opening_hours"`
	HoursNote    string   `json:"hours_note"`
	// URL is the entry's page, null while the directory is on no page;
	// APIURL the entry in the API.
	URL       *string   `json:"url"`
	APIURL    string    `json:"api_url"`
	UpdatedAt time.Time `json:"updated_at"`
}

// APICategory is a published category in the content API, at
// /api/v1/<key>/categories and with each of its entries.
type APICategory struct {
	ID      string `json:"id"`
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
	// EntryCount is the number of published entries in the category.
	EntryCount int `json:"entry_count"`
	// URL is the category's page, null while the directory is on no page;
	// EntriesURL its entries in the API.
	URL        *string `json:"url"`
	EntriesURL string  `json:"entries_url"`
}

// APIAddress is an entry's postal address.
type APIAddress struct {
	Street   string `json:"street"`
	Postcode string `json:"postcode"`
	City     string `json:"city"`
}

// APIPoint is an entry's position on a map.
type APIPoint struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// APIDay is one day of an entry's opening hours: the weekday in English
// lower case, such as "monday", and its opening and closing times as
// "15:04", both null on a closed day (see Day).
type APIDay struct {
	Day   string  `json:"day"`
	Open  *string `json:"open"`
	Close *string `json:"close"`
}

// apiContent is the published directory in a request's language: the
// categories in the editors' order, and the entries in them by name with
// the category of each.
type apiContent struct {
	categories []APICategory
	entries    []EntryItem
	categoryOf map[string]APICategory
	paths      router.PathFunc
}

// apiRoutes are the instance's endpoints in the content API, below
// /api/v1/<key>: the published entries by name, which ?category=<slug>
// and ?slug=<slug> filter, an entry by its ID and the published
// categories.
func (s *Service) apiRoutes() *router.Router {
	routes := api.NewRouter()

	routes.Get("/entries", s.apiEntries).As("entries")
	routes.Get("/entries/{id}", s.apiEntry).As("entry")
	routes.Get("/categories", s.apiCategories).As("categories")

	return routes
}

// apiEntries answers /entries: a page (api.Paginate) of the published
// entries.
func (s *Service) apiEntries(w http.ResponseWriter, r *http.Request) {
	loaded, err := s.apiLoad(r)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	query := r.URL.Query()
	category := query.Get("category")

	// The site's page of an unknown category is not found either.
	if category != "" && !loaded.hasCategory(category) {
		api.NotFound(w, r)

		return
	}

	slug := query.Get("slug")
	entries := []APIEntry{}

	for _, item := range loaded.entries {
		found := loaded.categoryOf[item.ID]

		if category != "" && found.Slug != category {
			continue
		}

		if slug != "" && item.Fields.Slug != slug {
			continue
		}

		entries = append(entries, s.apiItem(r, item, found, loaded.paths, false))
	}

	api.Write(w, r, api.Paginate(r, entries))
}

// apiEntry answers /entries/{id}: a published entry of a published
// category in the request's language. It has no Last-Modified (see
// api.Modified): its category holds the number of published entries in
// it, which another entry changes without a date this answer shows.
func (s *Service) apiEntry(w http.ResponseWriter, r *http.Request) {
	loaded, err := s.apiLoad(r)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	id := r.PathValue("id")

	for _, item := range loaded.entries {
		if item.ID == id {
			api.Write(w, r, s.apiItem(r, item, loaded.categoryOf[item.ID], loaded.paths, true))

			return
		}
	}

	api.NotFound(w, r)
}

// apiCategories answers /categories: a page of the published categories,
// in the editors' order.
func (s *Service) apiCategories(w http.ResponseWriter, r *http.Request) {
	loaded, err := s.apiLoad(r)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	api.Write(w, r, api.Paginate(r, loaded.categories))
}

// apiLoad reads the published directory in the request's language, with
// the addresses of the instance's routes on its page in that language.
func (s *Service) apiLoad(r *http.Request) (apiContent, error) {
	ctx := r.Context()
	locale := api.Locale(r)

	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return apiContent{}, err
	}

	categoryItems, err := s.categories.ListPublished(ctx, s.key, locale)
	if err != nil {
		return apiContent{}, err
	}

	entryItems, err := s.entries.ListPublished(ctx, s.key, locale)
	if err != nil {
		return apiContent{}, err
	}

	loaded := apiContent{
		categories: []APICategory{},
		categoryOf: map[string]APICategory{},
		paths:      pages.Links(s.public, base),
	}

	index := map[string]int{}

	for _, item := range categoryItems {
		index[item.ID] = len(loaded.categories)
		loaded.categories = append(loaded.categories, s.apiCategory(r, item, loaded.paths))
	}

	sortByName(entryItems)

	// Entries show only while their category is published.
	for _, item := range entryItems {
		position, ok := index[item.Fields.Category]
		if !ok {
			continue
		}

		loaded.categories[position].EntryCount++
		loaded.entries = append(loaded.entries, item)
	}

	// With the counts complete, each entry gets its category.
	for _, item := range loaded.entries {
		loaded.categoryOf[item.ID] = loaded.categories[index[item.Fields.Category]]
	}

	return loaded, nil
}

// hasCategory reports whether a published category has slug.
func (c apiContent) hasCategory(slug string) bool {
	for _, category := range c.categories {
		if category.Slug == slug {
			return true
		}
	}

	return false
}

// apiCategory is a published category in the request's language, without
// its count of entries.
func (s *Service) apiCategory(r *http.Request, item CategoryItem, paths router.PathFunc) APICategory {
	locale := api.Locale(r)
	text, _ := item.Text(locale)

	// Published categories have a slug.
	address, _ := paths("category", "category", item.Fields.Slug)
	entries := s.api.RoutePath("entries") + "?" + url.Values{"category": {item.Fields.Slug}, "locale": {locale}}.Encode()

	return APICategory{
		ID:         item.ID,
		Slug:       item.Fields.Slug,
		Name:       text.Name,
		Summary:    text.Summary,
		URL:        api.Optional(api.URL(r, address)),
		EntriesURL: api.URL(r, entries),
	}
}

// apiItem is a published entry in the request's language, in category;
// body adds its description.
func (s *Service) apiItem(r *http.Request, item EntryItem, category APICategory, paths router.PathFunc, body bool) APIEntry {
	locale := api.Locale(r)
	text, _ := item.Text(locale)
	fields := item.Fields

	// Published entries have a slug.
	address, _ := paths("entry", "category", category.Slug, "entry", fields.Slug)
	self, _ := s.api.RoutePathArgs("entry", "id", item.ID)

	entry := APIEntry{
		ID:           item.ID,
		Slug:         fields.Slug,
		Name:         fields.Name,
		Summary:      text.Summary,
		Keywords:     []string{},
		Category:     category,
		Image:        api.ImageOf(r, fields.Image),
		Website:      api.Optional(fields.Website),
		Email:        api.Optional(fields.Email),
		Phone:        api.Optional(fields.Phone),
		OpeningHours: apiHours(fields.Hours),
		HoursNote:    text.HoursNote,
		URL:          api.Optional(api.URL(r, address)),
		APIURL:       api.URL(r, self+"?"+url.Values{"locale": {locale}}.Encode()),
		UpdatedAt:    item.UpdatedAt.UTC(),
	}

	entry.Keywords = append(entry.Keywords, keywords(text.Keywords)...)

	if fields.Street != "" || fields.Postcode != "" || fields.City != "" {
		entry.Address = &APIAddress{Street: fields.Street, Postcode: fields.Postcode, City: fields.City}
	}

	if fields.Latitude.Valid && fields.Longitude.Valid {
		entry.Location = &APIPoint{Latitude: fields.Latitude.Float64, Longitude: fields.Longitude.Float64}
	}

	if body {
		entry.DescriptionHTML = api.HTML(r, text.Description)
	}

	return entry
}

// apiHours are the opening hours from Monday to Sunday, nil when the entry
// has none (see OpeningHours.Known).
func apiHours(hours OpeningHours) []APIDay {
	if !hours.Known() {
		return nil
	}

	days := make([]APIDay, 0, len(hours))

	for _, day := range hours.Week() {
		days = append(days, APIDay{
			Day:   strings.ToLower(day.Weekday.String()),
			Open:  api.Optional(day.Open),
			Close: api.Optional(day.Close),
		})
	}

	return days
}
