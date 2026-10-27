package directory

import (
	"github.com/donseba/go-webthings/frontend"
)

// structured is the structured data of an entry's page (see
// frontend.Thing): a LocalBusiness when the entry has opening hours, a
// Place when it has an address or a position, and an Organization
// otherwise.
func structured(entry Entry) frontend.Thing {
	thing := frontend.Thing{
		"@type":       "Organization",
		"name":        entry.Name,
		"description": entry.Summary,
		"url":         entry.URL,
		"image":       entry.ImageURL,
		"telephone":   entry.Phone,
		"email":       entry.Email,
		"sameAs":      []string{entry.Website},
		"address":     address(entry),
	}

	switch {
	case entry.Hours.Known():
		thing["@type"] = "LocalBusiness"
		thing["openingHoursSpecification"] = openingHours(entry.Hours)
	case entry.Street != "" || entry.Postcode != "" || entry.City != "" || entry.HasLocation:
		// A place has no e-mail address in schema.org.
		thing["@type"] = "Place"
		delete(thing, "email")
	}

	if entry.HasLocation {
		thing["geo"] = frontend.Thing{
			"@type":     "GeoCoordinates",
			"latitude":  entry.Latitude,
			"longitude": entry.Longitude,
		}
	}

	return thing
}

// address is the entry's postal address, or nil without one.
func address(entry Entry) frontend.Thing {
	if entry.Street == "" && entry.Postcode == "" && entry.City == "" {
		return nil
	}

	return frontend.Thing{
		"@type":           "PostalAddress",
		"streetAddress":   entry.Street,
		"postalCode":      entry.Postcode,
		"addressLocality": entry.City,
	}
}

// openingHours are the days the entry opens, from Monday, as schema.org's
// OpeningHoursSpecification.
func openingHours(hours OpeningHours) []frontend.Thing {
	var days []frontend.Thing

	for _, day := range hours.Week() {
		if day.Closed() {
			continue
		}

		days = append(days, frontend.Thing{
			"@type":     "OpeningHoursSpecification",
			"dayOfWeek": "https://schema.org/" + day.Weekday.String(),
			"opens":     day.Open,
			"closes":    day.Close,
		})
	}

	return days
}
