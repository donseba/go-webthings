package events

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-router"
)

// Info is an event as other modules see it, published or not, for example a
// registrations module that lists events to sign up for and names them in
// e-mail. Get it with Service.Event and Service.Events, through
// webthings.Env.Service.
type Info struct {
	ID string
	// Title is in the requested language, or in the first language the
	// event has when it is not translated.
	Title string
	// Start and End are in Timezone, for "UTC" as civil times (see
	// frontend.Civil); End is zero when the event has none and the last day
	// for all-day events.
	Start    time.Time
	End      time.Time
	AllDay   bool
	Timezone string
	// Published is true when visitors can see the event in the requested
	// language; URL is then its address, when the agenda is on a page.
	Published bool
	URL       string
	// Past is true once the event is over.
	Past bool
}

// Event returns one event in locale: as published when visitors can see it
// in locale, otherwise its latest revision.
func (s *Service) Event(ctx context.Context, id, locale string) (Info, error) {
	item, err := s.events.Published(ctx, s.key, id, locale)

	published := err == nil
	if errors.Is(err, content.ErrNotFound) {
		item, err = s.events.Latest(ctx, s.key, id)
	}

	if err != nil {
		return Info{}, err
	}

	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return Info{}, err
	}

	return info(item, locale, pages.Links(s.public, base), published), nil
}

// Events returns every event of the instance in locale, latest start first,
// with the latest revision of each, published or not.
func (s *Service) Events(ctx context.Context, locale string) ([]Info, error) {
	items, err := s.events.ListLatest(ctx, s.key)
	if err != nil {
		return nil, err
	}

	visible, err := s.events.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	public := map[string]EventItem{}
	for _, item := range visible {
		public[item.ID] = item
	}

	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	paths := pages.Links(s.public, base)

	list := make([]Info, 0, len(items))

	for _, item := range items {
		if published, ok := public[item.ID]; ok {
			// The URL is the published slug, which the latest draft may
			// have changed.
			current := info(item, locale, paths, true)
			current.URL = info(published, locale, paths, true).URL
			list = append(list, current)

			continue
		}

		list = append(list, info(item, locale, paths, false))
	}

	slices.SortStableFunc(list, func(a, b Info) int {
		return b.Start.Compare(a.Start)
	})

	return list, nil
}

func info(item EventItem, locale string, paths router.PathFunc, published bool) Info {
	text, ok := item.Text(locale)
	if !ok && len(item.Translations) > 0 {
		text = item.Translations[0].Text
	}

	fields := item.Fields

	event := Info{
		ID:        item.ID,
		Title:     text.Title,
		Start:     fields.local(fields.Start),
		AllDay:    fields.AllDay,
		Timezone:  fields.Timezone,
		Published: published,
		Past:      fields.until().Before(time.Now()),
	}

	if fields.End != nil {
		event.End = fields.local(*fields.End)
	}

	if published {
		event.URL, _ = paths("event", "slug", text.Slug)
	}

	return event
}
