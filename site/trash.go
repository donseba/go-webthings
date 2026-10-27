package site

import "time"

// MaxTrashDays is the longest TrashDays accepted: ten years.
const MaxTrashDays = 3650

// TrashCutoff is the deletion time before which items in the admin's trash
// are deleted for good at now: TrashDays days before now. ok is false when
// TrashDays is zero, the default, which keeps items in the trash until
// someone deletes them.
func (s Settings) TrashCutoff(now time.Time) (cutoff time.Time, ok bool) {
	if s.TrashDays <= 0 {
		return time.Time{}, false
	}

	return now.Add(-time.Duration(s.TrashDays) * 24 * time.Hour), true
}

// cleanTrash checks TrashDays: whole days from 0 to MaxTrashDays.
func cleanTrash(settings *Settings) error {
	if settings.TrashDays < 0 || settings.TrashDays > MaxTrashDays {
		return ErrInvalid
	}

	return nil
}
