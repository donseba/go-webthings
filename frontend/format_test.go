package frontend_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/donseba/go-webthings/frontend"

	"github.com/donseba/go-partial/exp/localization"
)

func TestFormatDates(t *testing.T) {
	amsterdam, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, time.October, 25, 14, 0, 0, 0, amsterdam)
	end := time.Date(2026, time.October, 25, 17, 30, 0, 0, amsterdam)

	cases := []struct {
		locale, style, want string
	}{
		{"en", "", "25 October 2026"},
		{"en", "short", "25/10/2026"},
		{"en", "medium", "25 Oct 2026"},
		{"en", "full", "Sunday 25 October 2026"},
		{"en-US", "full", "Sunday, October 25, 2026"},
		{"en_US", "medium", "Oct 25, 2026"},
		{"nl", "full", "zondag 25 oktober 2026"},
		{"nl_NL", "short", "25-10-2026"},
		{"nl", "medium", "25 okt 2026"},
		{"nl", "EEE d MMM ''yy 'om' HH:mm", "zo 25 okt '26 om 14:00"},
		{"de", "long", "25 October 2026"},
	}

	for _, c := range cases {
		if got := frontend.FormatDate(c.locale, start, c.style); got != c.want {
			t.Errorf("%s %q: got %q, want %q", c.locale, c.style, got, c.want)
		}
	}

	nl := frontend.Format{Locale: "nl"}
	us := frontend.Format{Locale: "en-US"}

	if got := nl.Time(start); got != "14:00" {
		t.Errorf("nl time: %q", got)
	}

	if got := us.TimeRange(start, end); got != "2:00 PM – 5:30 PM" {
		t.Errorf("en-US time range: %q", got)
	}

	if got := nl.TimeRange(start, end); got != "14:00–17:30" {
		t.Errorf("nl time range: %q", got)
	}

	if got := nl.TimeRange(start, time.Time{}); got != "14:00" {
		t.Errorf("open end: %q", got)
	}

	if got := nl.Date(time.Time{}, "long"); got != "" {
		t.Errorf("zero time: %q", got)
	}

	ranges := []struct {
		format     frontend.Format
		start, end time.Time
		style      string
		want       string
	}{
		{nl, start, end, "full", "zondag 25 oktober 2026"},
		{nl, start, start.AddDate(0, 0, 2), "long", "25–27 oktober 2026"},
		{nl, start, start.AddDate(0, 0, 8), "long", "25 oktober – 2 november 2026"},
		{nl, start, start.AddDate(0, 3, 0), "medium", "25 okt 2026 – 25 jan 2027"},
		{us, start, start.AddDate(0, 0, 2), "long", "October 25–27, 2026"},
		{us, start, start.AddDate(0, 0, 8), "medium", "Oct 25 – Nov 2, 2026"},
		{nl, start, start.AddDate(0, 0, 2), "full", "zondag 25 oktober 2026 – dinsdag 27 oktober 2026"},
		{nl, start, time.Time{}, "long", "25 oktober 2026"},
	}

	for _, c := range ranges {
		if got := c.format.DateRange(c.start, c.end, c.style); got != c.want {
			t.Errorf("%s range %q: got %q, want %q", c.format.Locale, c.style, got, c.want)
		}
	}
}

func TestFormatZone(t *testing.T) {
	amsterdam, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}

	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}

	format := frontend.Format{Locale: "en", Zone: amsterdam}

	// Stored times are in UTC and shown in the site's timezone: 22:30 UTC is
	// the next day in Amsterdam.
	stored := time.Date(2026, time.October, 24, 22, 30, 0, 0, time.UTC)
	if got := format.Date(stored, "long") + " " + format.Time(stored); got != "25 October 2026 00:30" {
		t.Errorf("UTC time: %q", got)
	}

	// Times placed in a zone, like an event in its own timezone, keep it.
	placed := time.Date(2026, time.October, 25, 9, 0, 0, 0, tokyo)
	if got := format.Time(placed); got != "09:00" {
		t.Errorf("zoned time: %q", got)
	}
}

func TestFormatCivil(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}

	format := frontend.Format{Locale: "en", Zone: chicago}

	// A date stored as midnight UTC is the previous evening in Chicago as an
	// instant, but a civil date stays on its day.
	day := time.Date(2026, time.October, 25, 0, 0, 0, 0, time.UTC)
	if got := format.Date(day, "long"); got != "24 October 2026" {
		t.Errorf("instant: %q", got)
	}

	civil := frontend.Civil(day)
	if got := format.Date(civil, "long"); got != "25 October 2026" {
		t.Errorf("civil date: %q", got)
	}

	if got := format.DateRange(civil, civil.AddDate(0, 0, 2), "long"); got != "25–27 October 2026" {
		t.Errorf("civil range: %q", got)
	}

	// Civil keeps the wall clock of t in t's own zone.
	tokyo := time.FixedZone("JST", 9*60*60)

	wall := frontend.Civil(time.Date(2026, time.October, 25, 9, 30, 0, 0, tokyo))
	if got := format.Time(wall); got != "09:30" {
		t.Errorf("civil time: %q", got)
	}

	if wall.Location() != frontend.Floating {
		t.Errorf("location: %v", wall.Location())
	}

	// Stored as UTC, a civil time keeps its date and time.
	if got := civil.UTC(); !got.Equal(day) || got.Day() != 25 {
		t.Errorf("UTC: %v", got)
	}

	if !frontend.Civil(time.Time{}).IsZero() {
		t.Error("zero time")
	}
}

// finnish names May differently as a month of a date and as its short
// name; the date patterns use a context of their own.
const finnish = `msgid ""
msgstr ""
"Language: fi\n"
"Content-Type: text/plain; charset=UTF-8\n"

msgid "May"
msgstr "Not a month"

msgctxt "month"
msgid "May"
msgstr "toukokuuta"

msgctxt "month-short"
msgid "May"
msgstr "touko"

msgctxt "day"
msgid "Sunday"
msgstr "sunnuntai"

msgctxt "time"
msgid "PM"
msgstr "ip."

msgctxt "date"
msgid "long"
msgstr "d. MMMM y"

msgctxt "date"
msgid "full"
msgstr "EEEE d. MMMM y"

msgctxt "date"
msgid "medium"
msgstr ""
`

func TestFormatTranslated(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "fi.po"), []byte(finnish), 0o644); err != nil {
		t.Fatal(err)
	}

	tr, err := frontend.Translations(dir)
	if err != nil {
		t.Fatal(err)
	}

	format := frontend.Format{
		Locale: "fi",
		Lookup: func(context, msgid string) (string, bool) {
			return tr.LookupC(localization.New("fi"), context, msgid)
		},
	}

	day := time.Date(2026, time.May, 24, 14, 0, 0, 0, time.UTC)

	cases := []struct {
		style string
		want  string
	}{
		{"full", "sunnuntai 24. toukokuuta 2026"},
		{"long", "24. toukokuuta 2026"},
		// The pattern without a translation is English, with the short name.
		{"medium", "24 touko 2026"},
		{"short", "24/05/2026"},
		{"EEE h a", "Sun 2 ip."},
	}

	for _, c := range cases {
		if got := format.Date(day, c.style); got != c.want {
			t.Errorf("%s: %q, want %q", c.style, got, c.want)
		}
	}
}

// TestFormatTranslationsWin checks that translated names and patterns
// replace the built-in ones also in a language with built-in ones, English
// and Dutch, that what is not translated stays built in, with its shorter
// date ranges for a pattern that is not translated, and that Calendars
// looks them up once per language.
func TestFormatTranslationsWin(t *testing.T) {
	entries := map[string]map[string]string{
		"nl": {"month\x00May": "Mei", "date\x00short": "d-M-y"},
		"en": {"day\x00Sunday": "Sun-day", "date\x00long": "MMMM d y"},
	}

	var (
		lookups   int
		calendars frontend.Calendars
	)

	format := func(locale string) frontend.Format {
		return frontend.Format{
			Locale: locale,
			Lookup: func(context, msgid string) (string, bool) {
				lookups++

				own, ok := entries[locale][context+"\x00"+msgid]

				return own, ok
			},
			Calendars: &calendars,
		}
	}

	day := time.Date(2026, time.May, 24, 14, 0, 0, 0, time.UTC)
	later := time.Date(2026, time.May, 26, 14, 0, 0, 0, time.UTC)

	cases := []struct {
		locale string
		got    string
		want   string
	}{
		{"nl", format("nl").Date(day, "long"), "24 Mei 2026"},
		{"nl", format("nl").Date(day, "short"), "24-5-2026"},
		{"nl", format("nl").Date(day, "full"), "zondag 24 Mei 2026"},
		// The long pattern is built in, and so is its shorter range.
		{"nl", format("nl").DateRange(day, later, "long"), "24–26 Mei 2026"},
		{"en", format("en").Date(day, "full"), "Sun-day 24 May 2026"},
		{"en", format("en").Date(day, "long"), "May 24 2026"},
		{"en", format("en").Time(day), "14:00"},
	}

	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: %q, want %q", c.locale, c.got, c.want)
		}
	}

	before := lookups
	format("nl").Date(day, "long")

	if lookups != before {
		t.Errorf("Calendars looked up %d texts again", lookups-before)
	}

	if !frontend.BuiltInCalendar("nl_BE") || !frontend.BuiltInCalendar("en-US") || frontend.BuiltInCalendar("de") {
		t.Error("BuiltInCalendar: want nl_BE and en-US, not de")
	}
}

func TestFormatWeekday(t *testing.T) {
	cases := []struct {
		locale string
		style  string
		want   string
	}{
		{"en", "", "Monday"},
		{"en", "short", "Mon"},
		{"nl", "", "maandag"},
		{"nl", "short", "ma"},
	}

	for _, c := range cases {
		format := frontend.Format{Locale: c.locale}
		if got := format.Weekday(time.Monday, c.style); got != c.want {
			t.Errorf("%s %q: %q, want %q", c.locale, c.style, got, c.want)
		}
	}

	german := frontend.Format{Locale: "de", Lookup: func(context, msgid string) (string, bool) {
		if context == "day" && msgid == "Monday" {
			return "Montag", true
		}

		return "", false
	}}

	if got := german.Weekday(time.Monday, ""); got != "Montag" {
		t.Errorf("translated: %q", got)
	}

	if got := german.Weekday(time.Weekday(9), ""); got != "" {
		t.Errorf("invalid day: %q", got)
	}
}

func TestFormatMoney(t *testing.T) {
	cases := []struct {
		locale string
		amount int64
		code   string
		want   string
	}{
		{"nl", 4500, "EUR", "€ 45,00"},
		{"en", 4500, "EUR", "€45.00"},
		{"en-US", 123456, "USD", "$1,234.56"},
		{"nl", 123456, "EUR", "€ 1.234,56"},
		{"de", 4500, "EUR", "45,00 €"},
		{"en", -250, "EUR", "-€2.50"},
		{"nl", -250, "EUR", "€ -2,50"},
		{"en", 1500, "JPY", "¥1,500"},
		{"en", 4500, "XYZ", "45.00 XYZ"},
	}

	for _, c := range cases {
		if got := frontend.FormatMoney(c.locale, c.amount, c.code); got != c.want {
			t.Errorf("%s %d %s: got %q, want %q", c.locale, c.amount, c.code, got, c.want)
		}
	}
}
