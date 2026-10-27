package forms

import (
	"github.com/donseba/go-webthings/admin"
)

// trash adds the deleted submissions to the trash of the form's section,
// next to a deleted form definition.
func (s *Service) trash() admin.Trash {
	return admin.RecordTrash(admin.TrashRecords[Submission]{
		Load: s.TrashedSubmissions,
		Fields: func(submission Submission) []string {
			fields := []string{summary(submission.Answers)}
			for _, answer := range submission.Answers {
				fields = append(fields, answer.Value)
			}

			return fields
		},
		Item: func(submission Submission) admin.TrashItem {
			return admin.TrashItem{
				ID:        submission.ID,
				Title:     summary(submission.Answers),
				Kind:      "Submission",
				DeletedAt: *submission.DeletedAt,
				DeletedBy: submission.DeletedBy.String(),
			}
		},
		Undelete: s.UndeleteSubmission,
		Purge:    s.PurgeSubmission,
	})
}
