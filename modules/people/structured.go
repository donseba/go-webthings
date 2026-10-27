package people

import (
	"github.com/donseba/go-webthings/frontend"
)

// structured is the structured data of a person's page (see
// frontend.Thing): a Person who works for the site's organisation, with
// their photo, contact details and own website and LinkedIn profile.
func structured(person Person) frontend.Thing {
	return frontend.Thing{
		"@type":     "Person",
		"name":      person.Name,
		"jobTitle":  person.Role,
		"url":       person.URL,
		"image":     person.PhotoURL,
		"email":     person.Email,
		"telephone": person.Phone,
		"sameAs":    []string{person.Website, person.LinkedIn},
		"worksFor":  frontend.Thing{"@id": frontend.OrganizationID},
	}
}
