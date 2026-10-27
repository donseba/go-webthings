package events

import (
	"context"
	"time"

	"github.com/donseba/go-webthings/frontend"
)

// structured is the structured data of an event's page (see
// frontend.Thing): an Event in locale, organised by the site's
// organisation, with its start and end in the event's timezone, its
// place and its social image. A registrations instance linked to the
// agenda adds its offers.
func (s *Service) structured(ctx context.Context, item EventItem, event Event, locale string) frontend.Thing {
	fields := item.Fields

	thing := frontend.Thing{
		"@type":       "Event",
		"name":        event.Title,
		"description": event.Summary,
		"url":         event.URL,
		"image":       s.frontend.SocialImageURL(ctx, event.ImageURL),
		"startDate":   fields.schemaTime(fields.Start),
		"eventStatus": "https://schema.org/EventScheduled",
		"inLanguage":  locale,
		"organizer":   frontend.Thing{"@id": frontend.OrganizationID},
	}

	if fields.End != nil {
		thing["endDate"] = fields.schemaTime(*fields.End)
	}

	if event.Location != "" || event.Address != "" {
		thing["location"] = frontend.Thing{
			"@type":   "Place",
			"name":    event.Location,
			"address": event.Address,
		}
		thing["eventAttendanceMode"] = "https://schema.org/OfflineEventAttendanceMode"
	}

	return thing
}

// schemaTime is t as schema.org writes it, in the event's timezone, such
// as "2026-10-07T19:30:00+02:00", or the day of an all-day event, such as
// "2026-10-07".
func (f EventFields) schemaTime(t time.Time) string {
	local := t.In(f.location())
	if f.AllDay {
		return local.Format(time.DateOnly)
	}

	return local.Format(time.RFC3339)
}
