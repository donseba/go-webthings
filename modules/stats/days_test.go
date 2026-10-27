package stats

import (
	"testing"
	"time"
)

// TestDaysWhereMidnightIsSkipped checks that the days of the reports are the
// dates in the site's timezone, also around a day whose midnight does not
// exist: America/Santiago goes from 23:59:59 on 5 September 2026 to 01:00
// on 6 September.
func TestDaysWhereMidnightIsSkipped(t *testing.T) {
	santiago, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Skip(err)
	}

	for _, test := range []struct {
		now   time.Time
		days  int
		first string
	}{
		{time.Date(2026, time.September, 6, 1, 0, 0, 0, santiago), 1, "2026-09-06"},
		{time.Date(2026, time.September, 6, 12, 0, 0, 0, santiago), 2, "2026-09-05"},
		{time.Date(2026, time.September, 7, 0, 30, 0, 0, santiago), 2, "2026-09-06"},
		{time.Date(2026, time.September, 7, 0, 30, 0, 0, santiago), 3, "2026-09-05"},
		{time.Date(2026, time.September, 5, 23, 30, 0, 0, santiago), 1, "2026-09-05"},
	} {
		service := &Service{location: santiago, now: func() time.Time { return test.now }}

		if got := service.firstDay(test.days); got != test.first {
			t.Errorf("at %v, the first of %d days is %s, want %s", test.now, test.days, got, test.first)
		}
	}
}
