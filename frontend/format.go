package frontend

import (
	"context"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/donseba/go-partial/exp/localization"
	"github.com/donseba/go-translator/v2/extract"

	"golang.org/x/text/currency"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// Dates, times and amounts are shown in the language of the page:
//
//	{{date .Start}}                    25 October 2026 (style "long")
//	{{date .Start "full"}}             Sunday 25 October 2026, zondag 25 oktober 2026
//	{{date .Start "short"}}            25/10/2026, 25-10-2026
//	{{date .Start "EEE d MMM"}}        Sun 25 Oct: any other style is a pattern
//	{{time .Start}}                    14:00 (2:00 PM in en-US)
//	{{dateRange .Start .End "long"}}   25–27 October 2026
//	{{timeRange .Start .End}}          14:00–17:00
//	{{weekday .Weekday}}               Monday, maandag ("short": Mon, ma)
//	{{money 4500 "EUR"}}               €45.00, € 45,00
//
// Styles are short, medium, long and full. Patterns use the letters of
// Unicode date patterns (CLDR): d dd, M MM MMM MMMM, y yy, E EEEE, H HH, h
// hh, mm, a; text in single quotes is copied. English (day first, as in
// Britain; "en-US" puts the month first and uses a 12-hour clock) and Dutch
// are built in. Names and patterns come from the site's translations (see
// Translations), by context (msgctxt) and English msgid: "January" …
// "December" in context "month", "Jan" … "Dec" in "month-short", "Sunday" …
// "Saturday" in "day", "Sun" … "Sat" in "day-short", "AM" and "PM" in
// "time", and the patterns of the styles "short", "medium", "long", "full"
// and "time" in "date" (see CalendarTexts). Your translations win, in every
// language: an entry replaces the built-in name or pattern, also in English
// and Dutch. What a catalogue does not translate is the built-in one, or
// English for a language without built-in ones (see BuiltInCalendar).
//
// Times stored in UTC, such as when a registration was made, are shown in
// the site's timezone (site settings). Times already in another zone keep
// it, such as an event's start in the event's own timezone. Calendar dates
// and wall-clock times that belong to no zone, such as a post's or a
// document's date or an all-day event, are passed through Civil so they are
// shown as they are.

// Floating is the location of civil times: calendar dates and wall-clock
// times that are not an instant in the site's timezone. It has no offset,
// like UTC, but Format never moves a time in Floating to another zone, so a
// date stored as midnight UTC stays on its day west of UTC.
var Floating = time.FixedZone("floating", 0)

// Civil returns the wall clock of t (its date and time of day in t's own
// location) in Floating, so Format shows it unchanged. Modules return dates
// without a zone (stored as midnight UTC) and times in a zone chosen on
// purpose, such as an event in "UTC", through Civil. A zero time stays zero.
// Store a civil time with t.UTC(), which keeps the same date and time.
func Civil(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}

	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), Floating)
}

// Format formats dates, times and amounts of money in one language. Module
// code that writes e-mail uses it; Frontend.Format gives one with the site's
// timezone and translations, and FormatDate and FormatMoney are shortcuts.
type Format struct {
	// Locale is a language code such as "nl", "en", "en-US" or "de_DE".
	Locale string
	// Zone is where times in UTC (or the server's local zone) are shown; nil
	// shows them as they are. Times in Floating (see Civil) and in any other
	// zone are always shown as they are.
	Zone *time.Location
	// Lookup returns the translation of a msgid in a context (msgctxt) in
	// Locale, and whether there is one, such as a translator's LookupC; it
	// supplies the names and patterns of months and days, which replace
	// the built-in ones (see CalendarTexts). Nil uses the built-in ones,
	// English for a language without.
	Lookup func(context, msgid string) (string, bool)
	// Calendars, when set, keeps the names and patterns Lookup gives per
	// language, so they are looked up once rather than for every date.
	// Formats that share it must have a Lookup with the same translations.
	Calendars *Calendars
}

// Calendars keeps the names and patterns of months and days that Formats
// look up in translations, per language (see Format.Calendars). Its zero
// value is ready to use; Forget drops what it keeps, for translations that
// changed.
type Calendars struct {
	kept sync.Map
}

// Forget drops the names and patterns kept, so they are looked up again.
func (c *Calendars) Forget() {
	c.kept.Clear()
}

// FormatDate formats t in locale with a style (short, medium, long, full)
// or a pattern, in t's own timezone.
func FormatDate(locale string, t time.Time, style string) string {
	return Format{Locale: locale}.Date(t, style)
}

// FormatMoney formats an amount in minor units (cents) of an ISO 4217
// currency, for example FormatMoney("nl", 4500, "EUR") is "€ 45,00".
func FormatMoney(locale string, amount int64, code string) string {
	return Format{Locale: locale}.Money(amount, code)
}

// Format returns the formatting of locale with the site's timezone and the
// site's translations, as the template functions use it.
func (f *Frontend) Format(ctx context.Context, locale string) Format {
	format := Format{Locale: locale, Zone: f.location(ctx)}

	if tr := f.translations; tr != nil {
		loc := localization.New(locale)

		format.Lookup = func(context, msgid string) (string, bool) {
			return tr.LookupC(loc, context, msgid)
		}
		format.Calendars = &f.calendars
	}

	return format
}

// location is the site's timezone, or UTC without site settings or when
// they cannot be read.
func (f *Frontend) location(ctx context.Context) *time.Location {
	if f.site == nil {
		return time.UTC
	}

	snapshot, err := f.site.Snapshot(ctx)
	if err != nil {
		return time.UTC
	}

	return snapshot.Settings().Location()
}

// Date formats the day of t with a style (short, medium, long or full; ""
// is long) or a pattern. A zero time gives "".
func (f Format) Date(t time.Time, style string) string {
	if t.IsZero() {
		return ""
	}

	data := f.calendar()

	return data.format(f.in(t), data.pattern(style))
}

// Time formats the time of day of t, for example "14:00".
func (f Format) Time(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	data := f.calendar()

	return data.format(f.in(t), data.pattern("time"))
}

// DateRange formats the days from start to end, leaving out what they share
// in the built-in languages, for example "25–27 October 2026". An end on the
// same day as start, or a zero end, gives one date.
func (f Format) DateRange(start, end time.Time, style string) string {
	if start.IsZero() {
		return ""
	}

	start = f.in(start)

	if end.IsZero() {
		return f.Date(start, style)
	}

	end = f.in(end)
	data := f.calendar()

	if style == "" {
		style = "long"
	}

	sameYear := start.Year() == end.Year()
	sameMonth := sameYear && start.Month() == end.Month()

	switch shorter, ok := data.ranges[style]; {
	case sameMonth && start.Day() == end.Day():
		return f.Date(start, style)
	case ok && sameMonth:
		return data.format(start, shorter.month[0]) + "–" + data.format(end, shorter.month[1])
	case ok && sameYear:
		return data.format(start, shorter.year[0]) + " – " + data.format(end, shorter.year[1])
	default:
		return f.Date(start, style) + " – " + f.Date(end, style)
	}
}

// TimeRange formats the times of day from start to end, for example
// "14:00–17:00"; a zero end gives only the start.
func (f Format) TimeRange(start, end time.Time) string {
	if end.IsZero() {
		return f.Time(start)
	}

	return f.Time(start) + f.calendar().timeRange + f.Time(end)
}

// Weekday names a day of the week, in full or, with style "short",
// abbreviated: "Monday" and "Mon" in English, "maandag" and "ma" in Dutch.
func (f Format) Weekday(day time.Weekday, style string) string {
	if day < time.Sunday || day > time.Saturday {
		return ""
	}

	if style == "short" {
		return f.calendar().daysShort[day]
	}

	return f.calendar().days[day]
}

// Money formats an amount in minor units (cents) of an ISO 4217 currency
// with the language's separators and symbol position: "€45.00" in English,
// "€ 45,00" in Dutch, "45,00 €" in German.
func (f Format) Money(amount int64, code string) string {
	tag := f.tag()
	printer := message.NewPrinter(tag)

	unit, err := currency.ParseISO(code)
	if err != nil {
		digits := printer.Sprint(number.Decimal(float64(amount)/100, number.MinFractionDigits(2), number.MaxFractionDigits(2)))

		return digits + " " + strings.ToUpper(code)
	}

	scale, _ := currency.Standard.Rounding(unit)
	value := math.Abs(float64(amount)) / math.Pow10(scale)
	digits := printer.Sprint(number.Decimal(value, number.MinFractionDigits(scale), number.MaxFractionDigits(scale)))
	symbol := printer.Sprint(currency.Symbol(unit))

	sign := ""
	if amount < 0 {
		sign = "-"
	}

	base, _ := tag.Base()

	switch moneyStyle(base.String()) {
	case symbolAfter:
		return sign + digits + " " + symbol
	case symbolSpaced:
		return symbol + " " + sign + digits
	default:
		return sign + symbol + digits
	}
}

const (
	symbolBefore = iota
	symbolSpaced
	symbolAfter
)

// moneyStyle is where a language writes the currency symbol (CLDR).
func moneyStyle(language string) int {
	switch language {
	case "nl":
		return symbolSpaced
	case "bg", "ca", "cs", "da", "de", "el", "es", "et", "fi", "fr", "hr", "hu", "is", "it", "lt", "lv",
		"nb", "nn", "no", "pl", "pt", "ro", "ru", "sk", "sl", "sr", "sv", "uk":
		return symbolAfter
	default:
		return symbolBefore
	}
}

func (f Format) tag() language.Tag {
	tag, err := language.Parse(f.Locale)
	if err != nil {
		return language.English
	}

	return tag
}

// in moves times in UTC or the server's zone (stored instants) to Zone.
// Civil times in Floating keep their wall clock.
func (f Format) in(t time.Time) time.Time {
	if f.Zone == nil || t.Location() == Floating {
		return t
	}

	if location := t.Location(); location == time.UTC || location == time.Local || location.String() == "UTC" {
		return t.In(f.Zone)
	}

	return t
}

// calendar is the data of the locale: the built-in one, English for a
// language without, with the translations of Lookup in its place.
func (f Format) calendar() *calendar {
	code := calendarCode(f.Locale)

	if f.Lookup == nil {
		if data, ok := builtIn(code); ok {
			return data
		}

		return calendars["en"]
	}

	if f.Calendars == nil {
		return translated(f.Lookup, code)
	}

	if data, ok := f.Calendars.kept.Load(code); ok {
		return data.(*calendar)
	}

	data := translated(f.Lookup, code)
	f.Calendars.kept.Store(code, data)

	return data
}

// calendarCode is locale as the calendars are kept: lower case, with a
// dash, such as "en-us".
func calendarCode(locale string) string {
	return strings.ToLower(strings.ReplaceAll(locale, "_", "-"))
}

// builtIn is the built-in calendar of code (see calendarCode), or of its
// language, such as nl for nl-be.
func builtIn(code string) (*calendar, bool) {
	if data, ok := calendars[code]; ok {
		return data, true
	}

	language, _, _ := strings.Cut(code, "-")
	data, ok := calendars[language]

	return data, ok
}

// BuiltInCalendar reports whether the names of months and days and the
// date patterns of locale, such as "nl", "en_US" or "nl-BE", are built in:
// a catalogue need not translate them (see CalendarTexts), as what it does
// not translate shows the built-in ones.
func BuiltInCalendar(locale string) bool {
	_, ok := builtIn(calendarCode(locale))

	return ok
}

// calendar holds the names and patterns of one language.
type calendar struct {
	months      [12]string
	monthsShort [12]string
	// days start on Sunday, like time.Weekday.
	days      [7]string
	daysShort [7]string
	am, pm    string
	// patterns by style, and "time".
	patterns map[string]string
	// timeRange separates the times of TimeRange.
	timeRange string
	// ranges shorten DateRange by style: the patterns of start and end in
	// one month and in one year.
	ranges map[string]collapse
}

type collapse struct {
	month [2]string
	year  [2]string
}

func (c *calendar) pattern(style string) string {
	if style == "" {
		style = "long"
	}

	if pattern, ok := c.patterns[style]; ok {
		return pattern
	}

	return style
}

var english = calendar{
	months: [12]string{
		"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December",
	},
	monthsShort: [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
	days:        [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
	daysShort:   [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
	am:          "AM",
	pm:          "PM",
	patterns: map[string]string{
		"short":  "dd/MM/y",
		"medium": "d MMM y",
		"long":   "d MMMM y",
		"full":   "EEEE d MMMM y",
		"time":   "HH:mm",
	},
	timeRange: "–",
	ranges: map[string]collapse{
		"medium": {month: [2]string{"d", "d MMM y"}, year: [2]string{"d MMM", "d MMM y"}},
		"long":   {month: [2]string{"d", "d MMMM y"}, year: [2]string{"d MMMM", "d MMMM y"}},
	},
}

var american = calendar{
	months:      english.months,
	monthsShort: english.monthsShort,
	days:        english.days,
	daysShort:   english.daysShort,
	am:          "AM",
	pm:          "PM",
	patterns: map[string]string{
		"short":  "M/d/y",
		"medium": "MMM d, y",
		"long":   "MMMM d, y",
		"full":   "EEEE, MMMM d, y",
		"time":   "h:mm a",
	},
	timeRange: " – ",
	ranges: map[string]collapse{
		"medium": {month: [2]string{"MMM d", "d, y"}, year: [2]string{"MMM d", "MMM d, y"}},
		"long":   {month: [2]string{"MMMM d", "d, y"}, year: [2]string{"MMMM d", "MMMM d, y"}},
	},
}

var dutch = calendar{
	months: [12]string{
		"januari", "februari", "maart", "april", "mei", "juni",
		"juli", "augustus", "september", "oktober", "november", "december",
	},
	monthsShort: [12]string{"jan", "feb", "mrt", "apr", "mei", "jun", "jul", "aug", "sep", "okt", "nov", "dec"},
	days:        [7]string{"zondag", "maandag", "dinsdag", "woensdag", "donderdag", "vrijdag", "zaterdag"},
	daysShort:   [7]string{"zo", "ma", "di", "wo", "do", "vr", "za"},
	am:          "a.m.",
	pm:          "p.m.",
	patterns: map[string]string{
		"short":  "dd-MM-y",
		"medium": "d MMM y",
		"long":   "d MMMM y",
		"full":   "EEEE d MMMM y",
		"time":   "HH:mm",
	},
	timeRange: "–",
	ranges:    english.ranges,
}

var calendars = map[string]*calendar{
	"en":    &english,
	"en-us": &american,
	"nl":    &dutch,
}

// Contexts (msgctxt) of the names and patterns Format looks up; see
// CalendarTexts.
const (
	contextMonth      = "month"
	contextMonthShort = "month-short"
	contextDay        = "day"
	contextDayShort   = "day-short"
	contextTime       = "time"
	contextDate       = "date"
)

// CalendarReference is the reference of CalendarTexts in the site's texts
// (see App.TranslationCatalog).
const CalendarReference = "core:calendar"

// patternStyles are the styles of the patterns, in the order of
// CalendarTexts.
var patternStyles = []string{"short", "medium", "long", "full", "time"}

// CalendarTexts are the names and patterns Format looks up in the
// translations, with their contexts: the English names of the months
// ("month", "month-short") and days ("day", "day-short"), AM and PM
// ("time"), and the styles of the date patterns ("date": "short",
// "medium", "long", "full" and "time", translated as a pattern such as
// "dd.MM.y"). A translation replaces the built-in name or pattern; a
// language with built-in ones (BuiltInCalendar) needs none. Extraction adds
// them to the site's texts (see App.TranslationCatalog).
func CalendarTexts() []extract.Message {
	var texts []extract.Message

	add := func(context string, ids ...string) {
		for _, id := range ids {
			texts = append(texts, extract.Message{Context: context, ID: id})
		}
	}

	add(contextMonth, english.months[:]...)
	add(contextMonthShort, english.monthsShort[:]...)
	add(contextDay, english.days[:]...)
	add(contextDayShort, english.daysShort[:]...)
	add(contextTime, english.am, english.pm)
	add(contextDate, patternStyles...)

	return texts
}

// translated builds the calendar of code (see calendarCode) from the
// translations of the English names and patterns (see CalendarTexts): the
// built-in ones where they have none, or English for a language without
// built-in ones. A built-in shorter date range (see DateRange) is kept for
// a style whose pattern is not translated.
func translated(lookup func(context, msgid string) (string, bool), code string) *calendar {
	base, ok := builtIn(code)
	if !ok {
		base = &calendar{
			months:      english.months,
			monthsShort: english.monthsShort,
			days:        english.days,
			daysShort:   english.daysShort,
			am:          english.am,
			pm:          english.pm,
			patterns:    english.patterns,
			timeRange:   english.timeRange,
		}
	}

	name := func(context, msgid, builtIn string) string {
		if own, ok := lookup(context, msgid); ok {
			return own
		}

		return builtIn
	}

	data := &calendar{
		am:        name(contextTime, english.am, base.am),
		pm:        name(contextTime, english.pm, base.pm),
		patterns:  map[string]string{},
		timeRange: base.timeRange,
		ranges:    map[string]collapse{},
	}

	for i := range data.months {
		data.months[i] = name(contextMonth, english.months[i], base.months[i])
		data.monthsShort[i] = name(contextMonthShort, english.monthsShort[i], base.monthsShort[i])
	}

	for i := range data.days {
		data.days[i] = name(contextDay, english.days[i], base.days[i])
		data.daysShort[i] = name(contextDayShort, english.daysShort[i], base.daysShort[i])
	}

	for _, style := range patternStyles {
		data.patterns[style] = name(contextDate, style, base.patterns[style])

		if shorter, ok := base.ranges[style]; ok && data.patterns[style] == base.patterns[style] {
			data.ranges[style] = shorter
		}
	}

	return data
}

// format writes t with a CLDR-style pattern.
func (c *calendar) format(t time.Time, pattern string) string {
	var b strings.Builder

	runes := []rune(pattern)

	for i := 0; i < len(runes); {
		letter := runes[i]

		if letter == '\'' {
			i = c.quoted(&b, runes, i)

			continue
		}

		if !isLetter(letter) {
			b.WriteRune(letter)
			i++

			continue
		}

		count := 1
		for i+count < len(runes) && runes[i+count] == letter {
			count++
		}

		b.WriteString(c.field(t, letter, count))

		i += count
	}

	return b.String()
}

// quoted copies quoted text from runes[i] on and returns the position after
// it; two quotes are one quote.
func (c *calendar) quoted(b *strings.Builder, runes []rune, i int) int {
	if i+1 < len(runes) && runes[i+1] == '\'' {
		b.WriteRune('\'')

		return i + 2
	}

	for i++; i < len(runes); i++ {
		if runes[i] != '\'' {
			b.WriteRune(runes[i])

			continue
		}

		if i+1 < len(runes) && runes[i+1] == '\'' {
			b.WriteRune('\'')
			i++

			continue
		}

		return i + 1
	}

	return i
}

// field is one pattern field, such as "MMMM" or "HH".
func (c *calendar) field(t time.Time, letter rune, count int) string {
	switch letter {
	case 'd':
		return padded(t.Day(), count)
	case 'M', 'L':
		switch {
		case count >= 4:
			return c.months[t.Month()-1]
		case count == 3:
			return c.monthsShort[t.Month()-1]
		default:
			return padded(int(t.Month()), count)
		}
	case 'y':
		if count == 2 {
			return padded(t.Year()%100, 2)
		}

		return padded(t.Year(), count)
	case 'E', 'c':
		if count >= 4 {
			return c.days[t.Weekday()]
		}

		return c.daysShort[t.Weekday()]
	case 'H':
		return padded(t.Hour(), count)
	case 'h':
		hour := t.Hour() % 12
		if hour == 0 {
			hour = 12
		}

		return padded(hour, count)
	case 'm':
		return padded(t.Minute(), count)
	case 's':
		return padded(t.Second(), count)
	case 'a':
		if t.Hour() < 12 {
			return c.am
		}

		return c.pm
	default:
		return strings.Repeat(string(letter), count)
	}
}

func isLetter(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

func padded(value, width int) string {
	text := strconv.Itoa(value)
	if len(text) < width {
		text = strings.Repeat("0", width-len(text)) + text
	}

	return text
}
