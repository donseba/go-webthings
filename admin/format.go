package admin

import (
	"context"
	"time"

	"github.com/donseba/go-webthings/frontend"

	"github.com/donseba/go-partial/exp/localization"
)

// Format formats dates, times and amounts of money in the language of the
// admin request in ctx and in the site's timezone, so admin screens show
// "5 okt 2026 14:05" and "€ 120,00" to a Dutch editor rather than
// "2026-10-05 14:05" and "120.00". Like T, it is for module code that only
// has a context. Outside an admin request it formats in English and UTC.
func Format(ctx context.Context) frontend.Format {
	a, ok := ctx.Value(messagesKey{}).(*Admin)
	if !ok {
		return frontend.Format{Locale: "en", Zone: time.UTC}
	}

	loc := localization.FromContext(ctx)

	return frontend.Format{
		Locale: a.locale.Language(),
		Zone:   a.location(ctx),
		// Month and day names and date patterns the admin catalogues
		// translate replace the built-in ones, as the website's do (see
		// frontend.CalendarTexts).
		Lookup: func(context, msgid string) (string, bool) {
			return a.tr.LookupC(loc, context, msgid)
		},
		Calendars: &a.calendars,
	}
}

// DateTime shows an instant with its date and time of day, in the admin
// language and the site's timezone: "5 Oct 2026 14:05" in English, "5 okt
// 2026 14:05" in Dutch. A zero time gives "".
func DateTime(ctx context.Context, t time.Time) string {
	if t.IsZero() {
		return ""
	}

	format := Format(ctx)

	return format.Date(t, "medium") + " " + format.Time(t)
}

// Date shows the day of an instant in the admin language and the site's
// timezone, for example "5 Oct 2026" or "5 okt 2026". A zero time gives "".
func Date(ctx context.Context, t time.Time) string {
	return Format(ctx).Date(t, "medium")
}
