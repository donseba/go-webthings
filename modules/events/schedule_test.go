package events

import (
	"errors"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin"

	form "github.com/donseba/go-form/v2"
)

// TestScheduleTransitions checks how the editor's wall-clock times are read
// in the event's timezone where its clocks change.
func TestScheduleTransitions(t *testing.T) {
	cases := []struct {
		name   string
		zone   string
		start  string
		allDay bool
		want   time.Time
	}{
		// The clocks skip 02:00–03:00: moved forward by the change.
		{"gap", "America/New_York", "2026-03-08T02:30", false, time.Date(2026, 3, 8, 7, 30, 0, 0, time.UTC)},
		{"gap", "Europe/Amsterdam", "2026-03-29T02:30", false, time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC)},
		// The clocks go back: the first of the two times, in summer time.
		{"overlap", "America/New_York", "2026-11-01T01:30", false, time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)},
		{"overlap", "Europe/Amsterdam", "2026-10-25T02:30", false, time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)},
		// Santiago skips midnight: the day starts at 01:00.
		{"all-day", "America/Santiago", "2026-09-06T15:00", true, time.Date(2026, 9, 6, 4, 0, 0, 0, time.UTC)},
		// Seconds are dropped.
		{"seconds", "UTC", "2026-07-01T12:00:30", false, time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)},
	}

	for _, c := range cases {
		fields, err := schedule(eventInput{Start: wall(t, c.start), AllDay: c.allDay, Timezone: admin.TimezoneChoices(c.zone)})
		if err != nil {
			t.Fatalf("%s %s: %v", c.name, c.zone, err)
		}

		if !fields.Start.Equal(c.want) {
			t.Errorf("%s %s: %s, want %s", c.name, c.zone, fields.Start, c.want)
		}
	}

	// An all-day event on the day Santiago skips midnight lasts until the
	// next midnight, 23 hours later.
	santiago := admin.TimezoneChoices("America/Santiago")

	fields, err := schedule(eventInput{Start: wall(t, "2026-09-06T00:00"), AllDay: true, Timezone: santiago})
	if err != nil {
		t.Fatal(err)
	}

	if want := time.Date(2026, 9, 7, 3, 0, 0, 0, time.UTC); !fields.until().Equal(want) {
		t.Errorf("until %s, want %s", fields.until(), want)
	}

	// Bind refuses a malformed start at its field; one without a start is
	// not scheduled.
	if _, err := schedule(eventInput{Timezone: admin.TimezoneChoices("UTC")}); !errors.Is(err, errTime) {
		t.Errorf("without start: %v", err)
	}

	// The location of the editor's times is ignored: they are wall-clock
	// times of the event's timezone.
	amsterdam, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}

	fields, err = schedule(eventInput{
		Start:    time.Date(2026, 7, 1, 12, 0, 0, 0, amsterdam),
		Timezone: admin.TimezoneChoices("UTC"),
	})
	if err != nil || !fields.Start.Equal(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("start in another location: %s %v", fields.Start, err)
	}
}

// wall is a date-time as the editor's datetime-local input sends it, as
// go-form reads it for a wall-clock field.
func wall(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := form.ParseDateTimeLocal(value, nil)
	if err != nil {
		t.Fatal(err)
	}

	return parsed
}

// editorLayout is the layout of the editor's date-times.
const editorLayout = "2006-01-02T15:04"

// TestNextHour checks that a new event starts at the next full hour of its
// timezone's clocks, also where the offset is not whole hours.
func TestNextHour(t *testing.T) {
	cases := []struct {
		zone string
		now  time.Time
		want string
	}{
		// 10:20 UTC is 15:50 in Kolkata (UTC+5:30).
		{"Asia/Kolkata", time.Date(2026, 10, 6, 10, 20, 0, 0, time.UTC), "2026-10-06T16:00"},
		// 18:40 UTC is 00:10 the next day.
		{"Asia/Kolkata", time.Date(2026, 10, 6, 18, 40, 0, 0, time.UTC), "2026-10-07T01:00"},
		// 17:50 UTC is 23:35 in Kathmandu (UTC+5:45): the next day starts.
		{"Asia/Kathmandu", time.Date(2026, 10, 6, 17, 50, 0, 0, time.UTC), "2026-10-07T00:00"},
		{"Europe/Amsterdam", time.Date(2026, 10, 6, 10, 20, 0, 0, time.UTC), "2026-10-06T13:00"},
	}

	for _, tc := range cases {
		location, err := time.LoadLocation(tc.zone)
		if err != nil {
			t.Fatal(err)
		}

		got := nextHour(tc.now, location)
		if got.Format(editorLayout) != tc.want || got.Location() != location {
			t.Fatalf("%s at %s: %s, want %s", tc.zone, tc.now, got.Format(editorLayout), tc.want)
		}
	}
}
