package directory

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Day is the opening time of one weekday as "15:04" wall-clock times. A day
// without times is closed. A Close at or before Open means the entry closes
// after midnight; "24:00" closes at midnight and "00:00"–"00:00" is open all
// day.
type Day struct {
	Open  string `json:"open,omitempty"`
	Close string `json:"close,omitempty"`
}

// Closed reports whether the entry is closed all day.
func (d Day) Closed() bool {
	return d.Open == ""
}

// span returns the opening and closing minute of the day.
func (d Day) span() (int, int, bool) {
	open, ok := minutes(d.Open)
	if !ok {
		return 0, 0, false
	}

	closing, ok := minutes(d.Close)
	if !ok {
		return 0, 0, false
	}

	return open, closing, true
}

// WeekDay is one row of a weekly schedule, for templates.
type WeekDay struct {
	Weekday time.Weekday
	Day
}

// OpeningHours is a weekly schedule indexed by time.Weekday, so
// hours[time.Monday] is Monday. It is stored as JSON in one column.
type OpeningHours [7]Day

// Known reports whether the schedule has any opening time. Entries without
// one show no opening hours at all rather than "closed" every day.
func (h OpeningHours) Known() bool {
	for _, day := range h {
		if !day.Closed() {
			return true
		}
	}

	return false
}

// Week returns the days from Monday to Sunday, for example
// {{range .Hours.Week}}{{.Weekday}} {{.Open}}–{{.Close}}{{end}}.
func (h OpeningHours) Week() []WeekDay {
	week := make([]WeekDay, 0, len(h))

	for i := range h {
		weekday := time.Weekday((i + 1) % 7)
		week = append(week, WeekDay{Weekday: weekday, Day: h[weekday]})
	}

	return week
}

// OpenNow reports whether the entry is open at t. t must be in the timezone
// the hours are written in; the module's own views use the site's timezone.
func (h OpeningHours) OpenNow(t time.Time) bool {
	minute := t.Hour()*60 + t.Minute()

	open, closing, ok := h[t.Weekday()].span()
	if ok && minute >= open && (closing <= open || minute < closing) {
		return true
	}

	// Yesterday's hours may run past midnight into today.
	open, closing, ok = h[(t.Weekday()+6)%7].span()

	return ok && closing <= open && minute < closing
}

// valid reports whether every day is closed or has two valid times.
func (h OpeningHours) valid() bool {
	for _, day := range h {
		if day.Open == "" && day.Close == "" {
			continue
		}

		if _, _, ok := day.span(); !ok || day.Open == "24:00" {
			return false
		}
	}

	return true
}

// Value stores the schedule as JSON, or "" when no day has times.
func (h OpeningHours) Value() (driver.Value, error) {
	if !h.Known() {
		return "", nil
	}

	encoded, err := json.Marshal([7]Day(h))
	if err != nil {
		return nil, err
	}

	return string(encoded), nil
}

// Scan implements sql.Scanner, reading the JSON column back; an empty or
// NULL column is no opening hours.
func (h *OpeningHours) Scan(value any) error {
	var text string

	switch v := value.(type) {
	case nil:
	case string:
		text = v
	case []byte:
		text = string(v)
	default:
		return fmt.Errorf("cannot scan %T into directory.OpeningHours", value)
	}

	*h = OpeningHours{}

	if strings.TrimSpace(text) == "" {
		return nil
	}

	var days [7]Day

	if err := json.Unmarshal([]byte(text), &days); err != nil {
		return err
	}

	*h = days

	return nil
}

// minutes parses a canonical "15:04" time, or "24:00", into minutes since
// midnight.
func minutes(clock string) (int, bool) {
	hour, minute, found := strings.Cut(clock, ":")
	if !found || len(hour) != 2 || len(minute) != 2 {
		return 0, false
	}

	h, err := strconv.Atoi(hour)
	if err != nil {
		return 0, false
	}

	m, err := strconv.Atoi(minute)
	if err != nil {
		return 0, false
	}

	if h == 24 && m == 0 {
		return 24 * 60, true
	}

	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}

	return h*60 + m, true
}

// clockTime turns what an editor typed ("9:00", "09:00:00", "24:00") into
// the canonical "15:04" form; ok is false for anything else.
func clockTime(value string) (string, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, ".", ":"))
	if value == "" {
		return "", true
	}

	if value == "24:00" {
		return value, true
	}

	for _, layout := range []string{"15:04", "15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.Format("15:04"), true
		}
	}

	return "", false
}
