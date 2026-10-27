package events

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/donseba/go-webthings/api"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-router"
)

// APIEvent is a published event in the content API (see package api), at
// /api/v1/<key>/events/{id} and in the list of /api/v1/<key>/events, which
// leaves out BodyHTML.
type APIEvent struct {
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	BodyHTML string `json:"body_html,omitempty"`
	Location string `json:"location"`
	Address  string `json:"address"`
	// Start and End are in Timezone, with its offset; End is null for an
	// event without an end. For an all-day event they are the first and
	// the last day at midnight.
	Start    time.Time  `json:"start"`
	End      *time.Time `json:"end"`
	AllDay   bool       `json:"all_day"`
	Timezone string     `json:"timezone"`
	// Past is set once the event is over.
	Past bool `json:"past"`
	// URL is the event's page, null while the agenda is on no page; APIURL
	// the event in the API.
	URL       *string    `json:"url"`
	APIURL    string     `json:"api_url"`
	Image     *api.Image `json:"image"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// apiRoutes are the instance's endpoints in the content API, below
// /api/v1/<key>: the upcoming and running events, soonest first, or with
// ?past=true the past ones, newest first, which ?slug=<slug> filters, and
// an event by its ID.
func (s *Service) apiRoutes() *router.Router {
	routes := api.NewRouter()

	routes.Get("/events", s.apiEvents).As("events")
	routes.Get("/events/{id}", s.apiEvent).As("event")

	return routes
}

// apiEvents answers /events: a page (api.Paginate) of the published events.
func (s *Service) apiEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := api.Locale(r)
	query := r.URL.Query()
	past := query.Get("past") == "true"

	items, err := s.events.ListPublished(ctx, s.key, locale)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	paths, err := s.apiPaths(r)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	now := time.Now()
	slug := query.Get("slug")

	items = slices.DeleteFunc(items, func(item EventItem) bool {
		text, _ := item.Text(locale)

		return item.Fields.until().Before(now) != past || slug != "" && text.Slug != slug
	})

	slices.SortStableFunc(items, func(a, b EventItem) int {
		if past {
			return b.Fields.Start.Compare(a.Fields.Start)
		}

		return a.Fields.Start.Compare(b.Fields.Start)
	})

	events := make([]APIEvent, 0, len(items))

	for _, item := range items {
		events = append(events, s.apiItem(r, item, paths, false))
	}

	api.Write(w, r, api.Paginate(r, events))
}

// apiEvent answers /events/{id}: a published event in the request's
// language.
func (s *Service) apiEvent(w http.ResponseWriter, r *http.Request) {
	item, err := s.events.Published(r.Context(), s.key, r.PathValue("id"), api.Locale(r))
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

	api.Modified(w, apiModified(r, s.key, item)...)
	api.Write(w, r, s.apiItem(r, item, paths, true))
}

// apiModified are the dates of what the answer of /events/{id} shows (see
// api.Modified): those of api.ItemDates and, for an event that has passed,
// when it ended, the moment its past field changed.
func apiModified(r *http.Request, key string, item EventItem) []time.Time {
	text, _ := item.Text(api.Locale(r))
	modified := api.ItemDates(r, key, item, text.Body, item.Fields.Image)

	if until := item.Fields.until(); until.Before(time.Now()) {
		modified = append(modified, until)
	}

	return modified
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

// apiItem is a published event in the request's language; body adds its
// text.
func (s *Service) apiItem(r *http.Request, item EventItem, paths router.PathFunc, body bool) APIEvent {
	locale := api.Locale(r)
	text, _ := item.Text(locale)
	fields := item.Fields

	// Published events have a slug.
	address, _ := paths("event", "slug", text.Slug)
	self, _ := s.api.RoutePathArgs("event", "id", item.ID)

	event := APIEvent{
		ID:        item.ID,
		Slug:      text.Slug,
		Title:     text.Title,
		Summary:   text.Summary,
		Location:  text.Location,
		Address:   text.Address,
		Start:     fields.Start.In(fields.location()),
		AllDay:    fields.AllDay,
		Timezone:  fields.Timezone,
		Past:      fields.until().Before(time.Now()),
		URL:       api.Optional(api.URL(r, address)),
		APIURL:    api.URL(r, self+"?"+url.Values{"locale": {locale}}.Encode()),
		Image:     api.ImageOf(r, fields.Image),
		UpdatedAt: item.Revision.CreatedAt.UTC(),
	}

	if fields.End != nil {
		end := fields.End.In(fields.location())
		event.End = &end
	}

	if body {
		event.BodyHTML = api.HTML(r, text.Body)
	}

	return event
}
