package events

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/donseba/go-webthings/feed"
	"github.com/donseba/go-webthings/frontend"
)

// Option changes how an instance works.
type Option func(*options)

type options struct {
	feedSize int
}

// WithFeedSize makes the instance's feed list the size soonest events,
// rather than as many as the site setting says (site.Settings.FeedLimit);
// at most site.MaxFeedSize.
func WithFeedSize(size int) Option {
	return func(o *options) {
		o.feedSize = size
	}
}

// feeds are the instance's feeds below its page: the upcoming events at
// feed.atom and feed.rss. size is the instance's feed size, zero for the
// site's (see WithFeedSize).
func (s *Service) feeds(size int) []frontend.Feed {
	return []frontend.Feed{{Limit: size, Build: s.feed}}
}

// feed is the upcoming and running events in the page's language, soonest
// first. An entry's summary says when and where the event takes place, in
// the page's language, before the event's own summary; its content is the
// event's text, with absolute addresses and images as e-mail shows them.
// An event is published when it was created and updated when its published
// revision was saved; its ID is a tag: URI of the instance and the event's
// ID, so it stays when the event's slug or page changes.
func (s *Service) feed(r *http.Request, limit int) (feed.Feed, error) {
	ctx := r.Context()
	locale := frontend.MountFrom(ctx).Locale

	items, err := s.events.ListPublished(ctx, s.key, locale)
	if err != nil {
		return feed.Feed{}, err
	}

	now := time.Now()

	items = slices.DeleteFunc(items, func(item EventItem) bool {
		return item.Fields.until().Before(now)
	})

	slices.SortStableFunc(items, func(a, b EventItem) int {
		return a.Fields.Start.Compare(b.Fields.Start)
	})

	var built feed.Feed

	for _, item := range items[:min(limit, len(items))] {
		built.Entries = append(built.Entries, s.entry(r, item, locale))
	}

	return built, nil
}

// entry is a published event as its feed lists it.
func (s *Service) entry(r *http.Request, item EventItem, locale string) feed.Entry {
	ctx := r.Context()
	text, _ := item.Text(locale)

	// Published events have a slug.
	link, _ := s.public.For(r).RoutePathArgs("event", "slug", text.Slug)

	return feed.Entry{
		ID:        s.frontend.TagURI(r, item.CreatedAt, s.key+"/"+item.ID),
		Title:     text.Title,
		Link:      link,
		Summary:   s.summary(r, item, locale),
		Content:   string(s.frontend.AbsoluteMarkdown(r, text.Body)),
		Published: item.CreatedAt,
		Updated:   item.Revision.CreatedAt,
		Image:     feed.Enclosure{URL: s.frontend.MailImage(ctx, item.Fields.Image).URL},
	}
}

// summary says when and where an event takes place, such as "25 October
// 2026 · 14:00–17:00 · De Werf", in locale, then the event's summary.
func (s *Service) summary(r *http.Request, item EventItem, locale string) string {
	text, _ := item.Text(locale)
	fields := item.Fields
	format := s.frontend.Format(r.Context(), locale)

	var end time.Time
	if fields.End != nil {
		end = fields.local(*fields.End)
	}

	start := fields.local(fields.Start)
	parts := []string{format.DateRange(start, end, "long")}

	if !fields.AllDay {
		parts = append(parts, format.TimeRange(start, end))
	}

	if text.Location != "" {
		parts = append(parts, text.Location)
	}

	when := strings.Join(parts, " · ")
	if text.Summary == "" {
		return when
	}

	return when + "\n\n" + text.Summary
}
