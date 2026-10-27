package directory

import (
	"testing"
	"time"

	"github.com/donseba/go-webthings/frontend"
)

// TestStructured checks the type of an entry's structured data: a
// LocalBusiness with opening hours, a Place with an address, an
// Organization otherwise.
func TestStructured(t *testing.T) {
	var hours OpeningHours

	hours[time.Monday] = Day{Open: "09:00", Close: "17:30"}
	hours[time.Saturday] = Day{Open: "10:00", Close: "14:00"}

	shop := structured(Entry{
		Name:   "Bakery",
		Email:  "info@bakery.example",
		Street: "Main street 1",
		City:   "Amsterdam",
		Hours:  hours,
	})
	if shop["@type"] != "LocalBusiness" || shop["email"] != "info@bakery.example" {
		t.Fatalf("shop: %v", shop)
	}

	days, _ := shop["openingHoursSpecification"].([]frontend.Thing)
	if len(days) != 2 || days[0]["dayOfWeek"] != "https://schema.org/Monday" || days[0]["opens"] != "09:00" ||
		days[0]["closes"] != "17:30" || days[1]["dayOfWeek"] != "https://schema.org/Saturday" {
		t.Fatalf("hours: %v", days)
	}

	address, _ := shop["address"].(frontend.Thing)
	if address["@type"] != "PostalAddress" || address["streetAddress"] != "Main street 1" ||
		address["addressLocality"] != "Amsterdam" {
		t.Fatalf("address: %v", address)
	}

	park := structured(Entry{Name: "Park", Email: "park@example.test", HasLocation: true, Latitude: 52.37, Longitude: 4.89})
	if park["@type"] != "Place" || park["email"] != nil || park["geo"].(frontend.Thing)["latitude"] != 52.37 {
		t.Fatalf("park: %v", park)
	}

	if club := structured(Entry{Name: "Club", Website: "https://club.example"}); club["@type"] != "Organization" {
		t.Fatalf("club: %v", club)
	}
}
