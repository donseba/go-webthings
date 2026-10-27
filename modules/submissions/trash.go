package submissions

import (
	"github.com/donseba/go-webthings/admin"
)

// trash is an instance's part of the admin's trash: deleted proposals, named
// by their title and submitter, as editors know them from the list.
func (s *Service) trash() admin.Trash {
	return admin.RecordTrash(admin.TrashRecords[trashed]{
		Load: s.trashed,
		Fields: func(proposal trashed) []string {
			return []string{proposal.Title, proposal.Name, proposal.Email, proposal.Body}
		},
		Item: func(proposal trashed) admin.TrashItem {
			return admin.TrashItem{
				ID:        proposal.ID,
				Title:     proposal.Title + " (" + proposal.Name + ")",
				DeletedAt: *proposal.DeletedAt,
				DeletedBy: proposal.DeletedBy.String(),
			}
		},
		Undelete: s.Undelete,
		Purge:    s.Purge,
	})
}
