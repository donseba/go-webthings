package admin

import (
	"fmt"
	"sync"
	"time"

	form "github.com/donseba/go-form/v2"
)

//go:generate go run timezones_generate.go -tab /usr/share/zoneinfo/zone.tab -out zones.go

// locations are the zones loaded once, as reading them takes a file each.
var locations = sync.OnceValue(func() map[string]*time.Location {
	loaded := make(map[string]*time.Location, len(zones))

	for _, zone := range zones {
		if location, err := time.LoadLocation(zone); err == nil {
			loaded[zone] = location
		}
	}

	return loaded
})

// TimezoneChoices is a dropdown of the IANA time zones editors choose from,
// one per country region and "UTC", each with its current offset from UTC,
// such as "Europe/Amsterdam (UTC+02:00)". Zones the server cannot load (it
// has no time zone database) are left out. They are sorted by name so the zones
// of a region are together, with selected chosen. A selected zone that is
// not in the list, such as an old name like "Europe/Kiev", is added so a
// stored value stays as it is.
func TimezoneChoices(selected string) form.SortedSelect[string] {
	now := time.Now()
	choices := map[string]string{}

	for zone, location := range locations() {
		choices[zone] = zone + " (" + offset(now.In(location)) + ")"
	}

	if selected != "" && choices[selected] == "" {
		choices[selected] = selected

		if location, err := time.LoadLocation(selected); err == nil {
			choices[selected] = selected + " (" + offset(now.In(location)) + ")"
		}
	}

	return form.NewSortedSelect(choices).Selected(selected)
}

// offset writes the offset from UTC of t's zone at t, such as "UTC+05:30".
func offset(t time.Time) string {
	_, seconds := t.Zone()

	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}

	return fmt.Sprintf("UTC%s%02d:%02d", sign, seconds/3600, seconds%3600/60)
}
