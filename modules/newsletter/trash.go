package newsletter

import (
	"errors"

	"github.com/donseba/go-webthings/admin"
)

// trash is the instance's part of the admin's trash: deleted subscribers,
// named by their address, and deleted mailings, named by their subject.
func (s *Service) trash() admin.Trash {
	subscribers := admin.RecordTrash(admin.TrashRecords[Subscriber]{
		Load: s.TrashedSubscribers,
		Fields: func(subscriber Subscriber) []string {
			return []string{subscriber.Email, subscriber.Name}
		},
		Item: func(subscriber Subscriber) admin.TrashItem {
			title := subscriber.Email
			if subscriber.Name != "" {
				title = subscriber.Name + " <" + subscriber.Email + ">"
			}

			return admin.TrashItem{
				ID:        subscriber.ID,
				Title:     title,
				Kind:      "Subscriber",
				DeletedAt: *subscriber.DeletedAt,
				DeletedBy: subscriber.DeletedBy.String(),
			}
		},
		Undelete: s.UndeleteSubscriber,
		Purge:    s.PurgeSubscriber,
		Reason: func(err error) string {
			if errors.Is(err, errEmailTaken) {
				return "This subscriber cannot be restored: another subscriber uses the e-mail address now."
			}

			return ""
		},
	})

	mailings := admin.RecordTrash(admin.TrashRecords[Mailing]{
		Load: s.TrashedMailings,
		Item: func(mailing Mailing) admin.TrashItem {
			return admin.TrashItem{
				ID:        mailing.ID,
				Title:     mailing.Subject,
				Kind:      "Mailing",
				DeletedAt: *mailing.DeletedAt,
				DeletedBy: mailing.DeletedBy.String(),
			}
		},
		Undelete: s.UndeleteMailing,
		Purge:    s.PurgeMailing,
	})

	return admin.JoinTrash(subscribers, mailings)
}
