package admin

import (
	"testing"
	"time"
)

// TestDayOffset checks that Today, Yesterday and Tomorrow follow the dates
// in the site's timezone, also on a day whose midnight does not exist:
// America/Santiago goes from 23:59:59 on 5 September 2026 to 01:00 on 6
// September.
func TestDayOffset(t *testing.T) {
	santiago, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Skip(err)
	}

	at := func(day, hour, minute int) time.Time {
		return time.Date(2026, time.September, day, hour, minute, 0, 0, santiago)
	}

	for _, test := range []struct {
		now, at time.Time
		want    int
	}{
		{at(6, 12, 0), at(6, 1, 0), 0},
		{at(6, 12, 0), at(5, 23, 30), -1},
		{at(6, 12, 0), at(5, 0, 30), -1},
		{at(6, 12, 0), at(4, 23, 30), -2},
		{at(6, 12, 0), at(7, 0, 0), 1},
		{at(5, 23, 30), at(6, 1, 30), 1},
		{at(7, 0, 30), at(6, 1, 0), -1},
		{at(7, 0, 30), at(5, 23, 0), -2},
	} {
		if got := dayOffset(test.now, test.at, santiago); got != test.want {
			t.Errorf("from %v, %v is %d days away, want %d", test.now, test.at, got, test.want)
		}
	}
}
