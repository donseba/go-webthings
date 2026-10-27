package registrations

import (
	"context"
	"errors"

	"github.com/donseba/go-webthings/admin"
)

// trashed is a registration in the trash with the title of its subject:
// editors know a registration by who registered for what.
type trashed struct {
	Registration
	Subject string
}

// trash is the instance's part of the admin's trash: its deleted
// registrations.
func (s *Service) trash() admin.Trash {
	return admin.RecordTrash(admin.TrashRecords[trashed]{
		Load: s.trashedWithSubjects,
		Fields: func(registration trashed) []string {
			return []string{registration.Name, registration.Email, registration.Code, registration.Subject}
		},
		Item: func(registration trashed) admin.TrashItem {
			return admin.TrashItem{
				ID:        registration.ID,
				Title:     registration.Name + " · " + registration.Subject,
				DeletedAt: *registration.DeletedAt,
				DeletedBy: registration.DeletedBy.String(),
			}
		},
		Undelete: s.Undelete,
		Purge:    s.Purge,
		Reason: func(err error) string {
			if errors.Is(err, ErrNoPlaces) {
				return "This registration cannot be restored: not enough places are left."
			}

			return ""
		},
	})
}

// trashedWithSubjects is Trashed with the title of each registration's
// subject in the site's default language.
func (s *Service) trashedWithSubjects(ctx context.Context, limit, offset int) ([]trashed, int, error) {
	list, total, err := s.Trashed(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	locale := s.defaultLocale(ctx)
	// Many deleted registrations share a subject; its title is looked up once.
	titles := map[string]string{}
	out := make([]trashed, len(list))

	for i, registration := range list {
		title, ok := titles[registration.SubjectID]
		if !ok {
			subject, err := s.Subject(ctx, registration.SubjectID, locale)
			if err != nil {
				return nil, 0, err
			}

			title = subject.Title
			titles[registration.SubjectID] = title
		}

		out[i] = trashed{Registration: registration, Subject: title}
	}

	return out, total, nil
}
