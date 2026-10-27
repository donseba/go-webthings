// Package events is an agenda module: dated events with revisions, a timezone,
// an optional end, all-day events, a location, an image and Markdown content.
// Add one instance per agenda, for example:
//
//	app.Use(events.Module("agenda", "Agenda"))
//
// Below its page the module serves the upcoming events at "/", past events at
// "/past" and one event at "/{slug}", and Atom and RSS feeds of the upcoming
// events at "/feed.atom" and "/feed.rss" (routes feed.atom and feed.rss),
// which the layout links to with {{feedLinks}}. The website renders these
// templates:
//
//	events/list.gohtml   ListView: upcoming or past events
//	events/event.gohtml  Event: one event
//
// Times in Event are in the event's own timezone (events in "UTC" as civil
// times, see frontend.Civil, so they are not moved to the site's timezone);
// the template functions show them in the page's language, for example
// {{dateRange .Start .End "full"}} · {{timeRange .Start .End}}. Other modules
// read events with Service.Event and Service.Events.
package events

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"slices"
	"sync"
	"time"

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

// EventFields are the language-independent parts of an event. Start and End
// are stored in UTC. All-day events start at midnight in Timezone and End is
// midnight of their last day.
type EventFields struct {
	Start    time.Time  `db:"starts_at"`
	End      *time.Time `db:"ends_at"`
	AllDay   bool       `db:"all_day"`
	Timezone string     `db:"timezone"`
	Image    string     `db:"image_id,null"`
}

// EventText is an event in one language. Body is Markdown.
type EventText struct {
	Slug     string `db:"slug"`
	Title    string `db:"title"`
	Summary  string `db:"summary"`
	Body     string `db:"body"`
	Location string `db:"location_name"`
	Address  string `db:"location_address"`
}

// EventItem is an event with its translations, as the store keeps it.
type EventItem = content.Item[EventFields, EventText]

var (
	errTimezone = errors.New("events_invalid_timezone")
	errTime     = errors.New("events_invalid_time")
	errEnd      = errors.New("events_end_before_start")
	errPastSlug = errors.New("events_slug_reserved")
)

func eventSpec() content.Spec[EventFields, EventText] {
	return content.Spec[EventFields, EventText]{
		Table:      "wt_events_event",
		SlugColumn: "slug",
		Complete: func(text EventText) bool {
			return text.Title != "" && text.Slug != ""
		},
		Check: check,
		Media: func(fields EventFields, translations []content.Translation[EventText]) []string {
			ids := []string{fields.Image}

			for _, translation := range translations {
				ids = append(ids, markdown.MediaIDs(translation.Text.Body)...)
			}

			return ids
		},
	}
}

func check(fields EventFields, translations []content.Translation[EventText]) error {
	if _, err := loadZone(fields.Timezone); err != nil {
		return err
	}

	if fields.Start.IsZero() {
		return errTime
	}

	if fields.End != nil && !fields.End.After(fields.Start) {
		return errEnd
	}

	for _, translation := range translations {
		text := translation.Text

		if content.TooLong(200, text.Title, text.Location) || content.TooLong(2000, text.Summary) {
			return content.ErrTooLong
		}

		if content.TooLong(1000, text.Address) || content.TooLong(256<<10, text.Body) {
			return content.ErrTooLong
		}

		// "/past" lists the past events.
		if text.Slug == "past" {
			return errPastSlug
		}
	}

	return nil
}

// zones keeps the timezones loadZone parsed, by name: every event shown
// needs its timezone, and parsing reads the timezone database.
var zones sync.Map

// loadZone loads an IANA timezone such as "Europe/Amsterdam".
func loadZone(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, errTimezone
	}

	if location, ok := zones.Load(name); ok {
		return location.(*time.Location), nil
	}

	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, errTimezone
	}

	zones.Store(name, location)

	return location, nil
}

// location is the event's timezone, or UTC when it does not load.
func (f EventFields) location() *time.Location {
	location, err := loadZone(f.Timezone)
	if err != nil {
		return time.UTC
	}

	return location
}

// local shows an instant in the event's timezone. Times in UTC are civil times
// (frontend.Civil) with the same instant, because the template functions move
// times in time.UTC to the site's timezone: an event in "UTC" would otherwise
// show at another hour, and an all-day event on another day.
func (f EventFields) local(t time.Time) time.Time {
	location := f.location()
	if location == time.UTC {
		return frontend.Civil(t.UTC())
	}

	return t.In(location)
}

// until is when the event is over: its end, or its start when it has none.
// All-day events last until the midnight after their last day.
func (f EventFields) until() time.Time {
	end := f.Start
	if f.End != nil {
		end = *f.End
	}

	if !f.AllDay {
		return end
	}

	day := end.In(f.location())
	midnight := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, time.UTC)

	// A zone may skip midnight; the day then starts when the clocks go on.
	return form.WallTime(midnight, f.location())
}

// Service is one Events instance, for the website's own code (for example
// "upcoming events" on the home page).
type Service struct {
	key      string
	events   *content.Revisions[EventFields, EventText]
	frontend *frontend.Frontend
	pages    *pages.Service
	// adminURL builds admin addresses by route name (webthings.Env.AdminURL).
	adminURL func(name string, args ...any) string
	// public are the routes below the module's page (see handler), api
	// those of its endpoints in the content API (see apiRoutes).
	public *router.Router
	api    *router.Router
}

// Store gives write access to this instance's events, for imports and seeds.
// Admin edits go through the same store.
func (s *Service) Store() content.Store[EventFields, EventText] {
	return s.events.In(s.key)
}

// Of returns the Events instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// Event is a published event as templates see it.
type Event struct {
	ID       string
	Title    string
	Summary  string
	Body     template.HTML
	Location string
	Address  string
	// Start and End are in Timezone; for "UTC" they are civil times (see
	// frontend.Civil). End is zero when the event has no end; for all-day
	// events it is the last day.
	Start    time.Time
	End      time.Time
	AllDay   bool
	Timezone string
	// URL is the event's page, empty when the agenda is on no page.
	URL string
	// ImageURL is the event's image as pages show it, at most media.ImageWidth
	// wide (see frontend.Frontend.Image); ImageSrcset lists smaller
	// copies for a srcset attribute, ImageWidth and ImageHeight are the
	// size of ImageURL and ImageOriginalURL is the uploaded file, for
	// downloads. All are empty when the event has no image.
	ImageURL         string
	ImageSrcset      string
	ImageOriginalURL string
	ImageWidth       int
	ImageHeight      int
}

// When is when the event takes place, for a template or partial that shows
// it in one line, also for a registration's Subject.
func (e Event) When() frontend.When {
	return frontend.When{Start: e.Start, End: e.End, AllDay: e.AllDay}
}

// ListView is the data of events/list.gohtml.
type ListView struct {
	Title  string
	Events []Event
	// Past is true on the list of past events, newest first. Otherwise the
	// list holds upcoming and running events, soonest first.
	Past        bool
	UpcomingURL string
	PastURL     string
}

// Upcoming returns at most limit of the next published events in locale, all
// of them when limit is 0 or less, for use outside the module's own pages.
// Links point to the page the module is mounted on; they are empty when it
// is on no page.
func (s *Service) Upcoming(ctx context.Context, locale string, limit int) ([]Event, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	events, err := s.list(ctx, locale, pages.Links(s.public, base), false)
	if err != nil {
		return nil, err
	}

	if limit > 0 && limit < len(events) {
		events = events[:limit]
	}

	return events, nil
}

// list returns published events in locale: upcoming ones soonest first, or
// past ones newest first, their URLs by route name from paths.
func (s *Service) list(ctx context.Context, locale string, paths router.PathFunc, past bool) ([]Event, error) {
	items, err := s.events.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	now := time.Now()

	items = slices.DeleteFunc(items, func(item EventItem) bool {
		return item.Fields.until().Before(now) != past
	})

	slices.SortStableFunc(items, func(a, b EventItem) int {
		if past {
			return b.Fields.Start.Compare(a.Fields.Start)
		}

		return a.Fields.Start.Compare(b.Fields.Start)
	})

	events := make([]Event, 0, len(items))

	for _, item := range items {
		events = append(events, s.event(ctx, item, locale, paths))
	}

	return events, nil
}

func (s *Service) event(ctx context.Context, item EventItem, locale string, paths router.PathFunc) Event {
	text, _ := item.Text(locale)
	fields := item.Fields
	shown := s.frontend.Image(ctx, fields.Image)
	event := Event{
		ID:               item.ID,
		Title:            text.Title,
		Summary:          text.Summary,
		Body:             s.frontend.Markdown(ctx, text.Body),
		Location:         text.Location,
		Address:          text.Address,
		Start:            fields.local(fields.Start),
		AllDay:           fields.AllDay,
		Timezone:         fields.Timezone,
		ImageURL:         shown.URL,
		ImageSrcset:      shown.Srcset,
		ImageOriginalURL: shown.OriginalURL,
		ImageWidth:       shown.Width,
		ImageHeight:      shown.Height,
	}

	// Published events have a slug.
	event.URL, _ = paths("event", "slug", text.Slug)

	if fields.End != nil {
		event.End = fields.local(*fields.End)
	}

	return event
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
