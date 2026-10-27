package redirects

import (
	"errors"

	"github.com/donseba/go-webthings/admin"
)

// trash is the instance's part of the admin's trash: its deleted redirects,
// known to editors by their old path and where it led.
func (s *Service) trash() admin.Trash {
	return admin.RecordTrash(admin.TrashRecords[Redirect]{
		Load: s.Trashed,
		Fields: func(redirect Redirect) []string {
			return []string{redirect.From, redirect.To}
		},
		Item: func(redirect Redirect) admin.TrashItem {
			return admin.TrashItem{
				ID:        redirect.ID,
				Title:     redirect.From + " → " + redirect.To,
				DeletedAt: *redirect.DeletedAt,
				DeletedBy: redirect.DeletedBy.String(),
			}
		},
		Undelete: s.Undelete,
		Purge:    s.Purge,
		Reason:   reason,
	})
}

// reason explains why a redirect could not be restored.
func reason(err error) string {
	switch {
	case errors.Is(err, errTaken):
		return "This redirect cannot be restored: another redirect uses its old path now."
	case errors.Is(err, errLoop):
		return "This redirect cannot be restored: it would lead back to its own path."
	}

	return ""
}
