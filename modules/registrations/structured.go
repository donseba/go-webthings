package registrations

import (
	"context"
	"log/slog"
	"maps"
	"time"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/modules/events"
)

// offers gives the event on a page of the linked events instance, the one
// with key, its offer in the page's structured data (see
// frontend.StructuredDataFunc): the adult price, in stock while visitors
// can register and sold out when every place is taken. Events without
// registration, or whose registration closed, get none.
func (s *Service) offers(key string) frontend.StructuredDataFunc {
	return func(ctx context.Context, view frontend.View, items []frontend.Thing) []frontend.Thing {
		event, ok := view.Content.(events.Event)
		if !ok || view.Module != key || len(items) == 0 || items[0]["@type"] != "Event" {
			return items
		}

		availability, err := s.Availability(ctx, event.ID, view.Locale)
		if err != nil {
			slog.WarnContext(ctx, "registrations offers", "event", event.ID, "error", err)

			return items
		}

		stock := ""

		switch {
		case availability.Open:
			stock = "https://schema.org/InStock"
		case availability.Configured && availability.Full:
			stock = "https://schema.org/SoldOut"
		default:
			return items
		}

		offer := frontend.Thing{
			"@type":         "Offer",
			"url":           event.URL,
			"price":         availability.Prices.Adult.String(),
			"priceCurrency": availability.Currency,
			"availability":  stock,
		}

		if !availability.ClosesAt.IsZero() {
			offer["validThrough"] = availability.ClosesAt.Format(time.RFC3339)
		}

		// The items are the events module's: the event is changed in a copy.
		changed := append([]frontend.Thing{}, items...)
		changed[0] = maps.Clone(items[0])
		changed[0]["offers"] = offer

		return changed
	}
}
