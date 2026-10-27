package events

import (
	"context"
	"embed"
	"errors"
	"net/http"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
)

//go:embed translations
var translations embed.FS

// defaultTimezone is the timezone of new events when the site has none.
const defaultTimezone = "UTC"

// Module adds an Events instance. key is used in URLs of the admin and must be
// unique on the site; title is shown in the admin and the page editor.
func Module(key, title string, opts ...Option) webthings.Module {
	var config options

	for _, option := range opts {
		option(&config)
	}

	return webthings.Module{
		Type:   "events",
		Key:    key,
		Title:  title,
		Schema: schema(),
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			return setup(env, config)
		},
		Funcs: funcs(),
		Templates: []frontend.Template{
			{Name: "list", Data: ListView{}, Example: listExample},
			{Name: "event", Data: Event{}, Example: eventExample},
		},
	}
}

func setup(env webthings.Env, config options) (webthings.Parts, error) {
	eventsSpec := eventSpec()
	eventsSpec.Changed = env.Changed

	events, err := content.NewRevisions(env.DB, eventsSpec)
	if err != nil {
		return webthings.Parts{}, err
	}

	service := &Service{
		key:      env.Key,
		events:   events,
		frontend: env.Frontend,
		pages:    env.Pages,
		adminURL: env.AdminURL,
	}

	service.public = service.handler()
	service.api = service.apiRoutes()

	return webthings.Parts{
		Admin:     []admin.Section{eventsSection(env, service)},
		Public:    service.public,
		API:       service.api,
		Service:   service,
		Sitemap:   service.sitemap,
		Feeds:     service.feeds(config.feedSize),
		Search:    service.search,
		Links:     service.linkSources,
		Scheduled: []content.Scheduler{events.Scheduler(env.Key)},
	}, nil
}

// eventInput is one language of an event in the editor. Start and End are
// wall-clock times in Timezone, which schedule turns into instants: they
// are read and shown as the editor types them (wallclock:"true"), not in
// the site's timezone as the admin's other date-times, in which a time of
// the event's own timezone may not exist. End is zero when the event has
// none.
type eventInput struct {
	Title    string                    `form:"input,text"           label:"Title"       required:"true" maxLength:"200"`
	Slug     string                    `form:"input,text"           label:"Web address"                 maxLength:"190"    pattern:"^(?:[a-z0-9]+(?:-[a-z0-9]+)*)?$" patternMsg:"Use lowercase letters, digits and dashes for the web address." description:"The last part of the address, such as open-studio in /en/news/open-studio. Made from the title when left empty."`
	Start    time.Time                 `form:"input,datetime-local" label:"Start"       required:"true"                                                                                                                                                                                                                                                                                                                                                                      wallclock:"true"`
	End      time.Time                 `form:"input,datetime-local" label:"End"                                                                                                                                                                                                                                                                                                                                                                                              wallclock:"true"`
	AllDay   bool                      `form:"checkbox"             label:"All day"`
	Timezone form.SortedSelect[string] `form:"dropdown"             label:"Timezone"                                                                                                                                                            description:"Start and end are times in this timezone. When its clocks go forward, a time they skip moves forward by the change (02:30 becomes 03:30); when they go back, a time that occurs twice is the first of the two."`
	Summary  string                    `form:"textarea"             label:"Summary"                     maxLength:"2000"                                                                                                                        description:"A short text for lists, search results and search engines."`
	Body     string                    `form:"textarea"             label:"Content"                     maxLength:"262144"                                                                                                                                                                                                                                                                                                                                                                    class:"markdown"`
	Location string                    `form:"input,text"           label:"Location"                    maxLength:"200"`
	Address  string                    `form:"textarea"             label:"Address"                     maxLength:"1000"`
	Image    form.SortedSelect[string] `form:"dropdown"             label:"Image"                                                                                                                                                                                                                                                                                                                                                                                                                              placeholder:"—"`
}

func eventsSection(env webthings.Env, service *Service) admin.Section {
	linked := editor.Linked[EventFields, EventText, eventInput]{Check: env.Links(), Address: service.address}

	return editor.New(editor.Config[EventFields, EventText, eventInput]{
		ID:       env.Key,
		Title:    env.Title,
		Icon:     "calendar",
		NewLabel: "New event",
		Store:    service.events.In(env.Key),
		Locales:  env.Locales,
		Names:    env.Auth.Names,
		Files:    translations,
		Edit:     linked.Edit,
		Routes:   linked.Routes,
		Saved:    linked.Saved,
		Label: func(text EventText) string {
			return text.Title
		},
		Form: func(ctx context.Context, item EventItem, locale string) (eventInput, error) {
			return eventForm(ctx, env, item, locale)
		},
		Apply: func(_ context.Context, in eventInput, draft *content.Draft[EventFields, EventText], locale string) error {
			fields, err := schedule(in)
			if err != nil {
				return err
			}

			fields.Image = in.Image.Get()
			draft.Fields = fields

			draft.Translations = content.SetText(draft.Translations, locale, EventText{
				Slug:     editor.Slug(in.Slug, in.Title),
				Title:    in.Title,
				Summary:  in.Summary,
				Body:     in.Body,
				Location: in.Location,
				Address:  in.Address,
			})

			return nil
		},
		PreviewURL: func(ctx context.Context, item EventItem, locale string) string {
			text, _ := item.Text(locale)

			path, err := service.public.RoutePathArgs("event", "slug", text.Slug)
			if err != nil {
				return ""
			}

			return env.Frontend.ModulePreviewURL(ctx, env.Key, locale, path, item.Revision.ID)
		},
		Issue: issue,
	})
}

func eventForm(ctx context.Context, env webthings.Env, item EventItem, locale string) (eventInput, error) {
	text, _ := item.Text(locale)
	fields := item.Fields

	image, err := editor.MediaChoices(ctx, env.Media, true, fields.Image)
	if err != nil {
		return eventInput{}, err
	}

	if fields.Timezone == "" {
		fields.Timezone = siteTimezone(ctx, env)
	}

	location := fields.location()

	// New events start at the next full hour.
	start := fields.Start.In(location)
	if fields.Start.IsZero() {
		start = nextHour(time.Now(), location)
	}

	var end time.Time

	if fields.End != nil {
		end = fields.End.In(location)
	}

	return eventInput{
		Title:    text.Title,
		Slug:     text.Slug,
		Start:    start,
		End:      end,
		AllDay:   fields.AllDay,
		Timezone: admin.TimezoneChoices(fields.Timezone),
		Summary:  text.Summary,
		Body:     text.Body,
		Location: text.Location,
		Address:  text.Address,
		Image:    image,
	}, nil
}

// siteTimezone is the timezone from the site settings, the default for new events.
func siteTimezone(ctx context.Context, env webthings.Env) string {
	settings, err := env.Site.Settings(ctx)
	if err != nil || settings.Timezone == "" {
		return defaultTimezone
	}

	return settings.Timezone
}

// schedule turns the wall-clock times of the editor into UTC instants.
func schedule(in eventInput) (EventFields, error) {
	zone := in.Timezone.Get()

	location, err := loadZone(zone)
	if err != nil {
		return EventFields{}, err
	}

	// Bind refuses a missing or malformed start at its field.
	if in.Start.IsZero() {
		return EventFields{}, errTime
	}

	fields := EventFields{
		Start:    inZone(in.Start, location, in.AllDay),
		AllDay:   in.AllDay,
		Timezone: zone,
	}

	if in.End.IsZero() {
		return fields, nil
	}

	end := inZone(in.End, location, in.AllDay)

	// An all-day event that ends on its first day is a one-day event.
	if in.AllDay && end.Equal(fields.Start) {
		return fields, nil
	}

	fields.End = &end

	return fields, nil
}

// nextHour is the next full hour after now on the clocks in location, also
// in a timezone whose offset is not whole hours, such as Asia/Kolkata:
// time.Truncate counts hours in UTC.
func nextHour(now time.Time, location *time.Location) time.Time {
	local := now.In(location)
	wall := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0, time.UTC)

	return form.WallTime(wall.Add(time.Hour), location)
}

// inZone is wall, a date-time of the editor, to the minute as a wall-clock
// time in location (form.WallTime; the location of wall is ignored), in
// UTC; with dateOnly at midnight. A time the clocks skip moves forward by
// the change and a time they show twice is the first of the two.
func inZone(wall time.Time, location *time.Location, dateOnly bool) time.Time {
	hour, minute := wall.Hour(), wall.Minute()
	if dateOnly {
		hour, minute = 0, 0
	}

	wall = time.Date(wall.Year(), wall.Month(), wall.Day(), hour, minute, 0, 0, time.UTC)

	return form.WallTime(wall, location).UTC()
}

func issue(err error) string {
	switch {
	case errors.Is(err, errTimezone):
		return "The timezone does not exist. Use a name like Europe/Amsterdam."
	case errors.Is(err, errTime):
		return "The date or time is not valid."
	case errors.Is(err, errEnd):
		return "The end must be after the start."
	case errors.Is(err, errPastSlug):
		return "The web address past is reserved for the list of past events."
	}

	return ""
}

// handler serves the module below its page: upcoming and past events and one
// event per slug. Their addresses come from the route names: in a handler
// for the request's page (router.For), elsewhere below the page.
func (s *Service) handler() *router.Router {
	// Paths without a route get the site's 404 page.
	routes := s.frontend.Router()

	routes.Get("/{$}", s.listPage(routes, false)).As("upcoming")
	routes.Get("/past", s.listPage(routes, true)).As("past")
	routes.Get("/{slug}", s.eventPage).As("event")

	return routes
}

// listPage serves the upcoming events, or with past the past ones; the two
// lists link to each other by their route names.
func (s *Service) listPage(routes *router.Router, past bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		here := routes.For(r)

		s.renderList(w, r, ListView{
			Past:        past,
			UpcomingURL: here.RoutePath("upcoming"),
			PastURL:     here.RoutePath("past"),
		})
	}
}

// renderList renders a list of events; view.Past tells which.
func (s *Service) renderList(w http.ResponseWriter, r *http.Request, view ListView) {
	ctx := r.Context()
	mount := frontend.MountFrom(ctx)

	events, err := s.list(ctx, mount.Locale, s.public.For(r).RoutePathArgs, view.Past)
	if err != nil {
		s.frontend.Fail(w, r, err)

		return
	}

	view.Title = frontend.MountFrom(ctx).Title
	view.Events = events

	var alternates map[string]string
	if view.Past {
		alternates = s.pastAlternates(ctx)
	}

	urls := make([]string, 0, len(events))

	for _, event := range events {
		urls = append(urls, event.URL)
	}

	s.frontend.Render(w, r, frontend.Content{
		Template:       "list",
		Title:          view.Title,
		Data:           view,
		Alternates:     alternates,
		StructuredData: []frontend.Thing{frontend.ItemList(view.Title, urls...)},
	})
}

// sitemap lists the published events in locale below the agenda's page, and
// the list of past events when there are any.
func (s *Service) sitemap(ctx context.Context, locale string) ([]string, error) {
	items, err := s.events.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	paths := make([]string, 0, len(items)+1)
	past := false

	// The unmounted routes give the paths below the module page.
	for _, item := range items {
		text, _ := item.Text(locale)
		path, _ := s.public.RoutePathArgs("event", "slug", text.Slug)
		paths = append(paths, path)

		if item.Fields.until().Before(now) {
			past = true
		}
	}

	if past {
		paths = append(paths, s.public.RoutePath("past"))
	}

	return paths, nil
}

// pastAlternates links the past events in every language with a mount.
func (s *Service) pastAlternates(ctx context.Context) map[string]string {
	paths, err := s.pages.Alternates(ctx, frontend.MountFrom(ctx).PageID)
	if err != nil {
		return nil
	}

	alternates := map[string]string{}

	for locale := range paths {
		alternates[locale] = s.public.RoutePath("past")
	}

	return alternates
}

func (s *Service) eventPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mount := frontend.MountFrom(ctx)

	// An editor may preview a revision of the event here.
	preview := frontend.PreviewRevision(r, s.adminURL(admin.RouteName(s.key, "index")))

	item, err := s.events.PreviewBySlug(ctx, s.key, mount.Locale, r.PathValue("slug"), preview)
	if err != nil {
		s.frontend.NotFound(w, r)

		return
	}

	event := s.event(ctx, item, mount.Locale, s.public.For(r).RoutePathArgs)
	alternates := map[string]string{}

	for _, translation := range item.Translations {
		if eventSpec().Complete(translation.Text) {
			alternates[translation.Locale], _ = s.public.RoutePathArgs("event", "slug", translation.Text.Slug)
		}
	}

	s.frontend.Render(w, r, frontend.Content{
		Template:    "event",
		Title:       event.Title,
		Description: event.Summary,
		Image:       event.ImageURL,
		Data:        event,
		Alternates:  alternates,
		EditURL:     s.adminURL(admin.RouteName(s.key, "edit"), "id", item.ID),
		Revision:    frontend.RevisionOf(r, item, preview),
		// The event, the page's main entity.
		StructuredData: []frontend.Thing{s.structured(ctx, item, event, mount.Locale)},
	})
}
