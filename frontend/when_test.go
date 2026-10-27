package frontend_test

import (
	"testing"
	"time"

	"github.com/donseba/go-webthings/frontend"
)

func TestWhen(t *testing.T) {
	amsterdam, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, 10, 7, 20, 0, 0, 0, amsterdam)

	cases := []struct {
		name          string
		when          frontend.When
		zero, sameDay bool
	}{
		{"nothing", frontend.When{}, true, true},
		{"no end", frontend.When{Start: start}, false, true},
		{"evening", frontend.When{Start: start, End: start.Add(3 * time.Hour)}, false, true},
		{"past midnight", frontend.When{Start: start, End: start.Add(5 * time.Hour)}, false, false},
		{
			"two days",
			frontend.When{Start: frontend.Civil(start), End: frontend.Civil(start.AddDate(0, 0, 1)), AllDay: true},
			false,
			false,
		},
	}

	for _, tc := range cases {
		if tc.when.IsZero() != tc.zero || tc.when.SameDay() != tc.sameDay {
			t.Fatalf("%s: zero %v, same day %v", tc.name, tc.when.IsZero(), tc.when.SameDay())
		}
	}
}
