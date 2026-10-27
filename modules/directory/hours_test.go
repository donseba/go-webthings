package directory

import (
	"testing"
	"time"

	"github.com/donseba/go-webthings/content"
)

func TestOpenNow(t *testing.T) {
	var hours OpeningHours

	hours[time.Monday] = Day{Open: "09:00", Close: "17:30"}
	hours[time.Friday] = Day{Open: "22:00", Close: "02:00"}
	hours[time.Saturday] = Day{Open: "00:00", Close: "00:00"}
	hours[time.Sunday] = Day{Open: "12:00", Close: "24:00"}

	// 5 October 2026 is a Monday.
	at := func(day, hour, minute int) time.Time {
		return time.Date(2026, time.October, day, hour, minute, 0, 0, time.UTC)
	}

	cases := []struct {
		at   time.Time
		open bool
	}{
		{at(5, 8, 59), false},
		{at(5, 9, 0), true},
		{at(5, 17, 29), true},
		{at(5, 17, 30), false},
		{at(6, 12, 0), false},
		{at(9, 21, 59), false},
		{at(9, 23, 0), true},
		// Friday night runs into Saturday, which is open all day anyway.
		{at(10, 1, 0), true},
		{at(10, 23, 59), true},
		{at(11, 11, 0), false},
		{at(11, 23, 59), true},
		// Sunday closes at midnight, so Monday morning is closed.
		{at(12, 0, 30), false},
	}

	for _, c := range cases {
		if got := hours.OpenNow(c.at); got != c.open {
			t.Errorf("%s: want open %v, got %v", c.at.Format("Mon 15:04"), c.open, got)
		}
	}

	var overnight OpeningHours

	overnight[time.Tuesday] = Day{Open: "20:00", Close: "03:00"}

	if !overnight.OpenNow(at(7, 2, 59)) || overnight.OpenNow(at(7, 3, 0)) {
		t.Error("Tuesday night should run until 03:00 on Wednesday")
	}

	week := hours.Week()
	if week[0].Weekday != time.Monday || week[6].Weekday != time.Sunday || !week[1].Closed() {
		t.Fatalf("week: %+v", week)
	}

	if !hours.valid() || (OpeningHours{{Open: "09:00"}}).valid() || (OpeningHours{{Open: "24:00", Close: "01:00"}}).valid() {
		t.Fatal("validation")
	}
}

func TestClockTime(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"9:00":     "09:00",
		"09:30:00": "09:30",
		"17.45":    "17:45",
		"24:00":    "24:00",
	}

	for in, want := range cases {
		got, ok := clockTime(in)
		if !ok || got != want {
			t.Errorf("clockTime(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}

	if _, ok := clockTime("noon"); ok {
		t.Error("noon is not a time")
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Bakkerij Jansen & Zn.": "bakkerij-jansen-zn",
		"  Café Noir ":          "cafe-noir",
		"Straße 12":             "strasse-12",
		"!!!":                   "",
	}

	for in, want := range cases {
		if got := content.Slug(in); got != want {
			t.Errorf("content.Slug(%q) = %q, want %q", in, got, want)
		}
	}
}
