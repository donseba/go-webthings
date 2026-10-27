package comments

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/donseba/go-webthings/admin"
)

// excerpt is how many characters of a message the trash shows after the name.
const excerpt = 60

// trash is an instance's part of the admin's trash: deleted messages, each
// with the replies that went along with it.
func (s *Service) trash() admin.Trash {
	return admin.RecordTrash(admin.TrashRecords[trashed]{
		Load: s.trashed,
		Fields: func(message trashed) []string {
			return []string{message.Name, message.Email, message.Body, message.Path, message.Subject}
		},
		Item: func(message trashed) admin.TrashItem {
			item := admin.TrashItem{
				ID:        message.ID,
				Title:     title(message),
				DeletedAt: *message.DeletedAt,
				DeletedBy: message.DeletedBy.String(),
			}

			// Replies cannot come back while the message they answer is in
			// the trash, so the trash tells them apart.
			if message.ParentID != "" {
				item.Kind = "Reply to a message"
			}

			return item
		},
		Undelete: s.Undelete,
		Purge:    s.Purge,
		Reason: func(err error) string {
			if errors.Is(err, ErrParentDeleted) {
				return "This reply cannot be restored while the message it answers is in the trash. Restore that message instead."
			}

			return ""
		},
	})
}

// title names a message by its writer and the start of its text, the way the
// moderators saw it in the list.
func title(message trashed) string {
	body := strings.Join(strings.Fields(message.Body), " ")
	if utf8.RuneCountInString(body) > excerpt {
		body = strings.TrimSpace(string([]rune(body)[:excerpt])) + "…"
	}

	return message.Name + ": " + body
}
