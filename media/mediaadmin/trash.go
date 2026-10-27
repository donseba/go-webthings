package mediaadmin

import (
	"errors"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/media"
)

// trash is the media library's part of the admin's trash.
func trash(library *media.Service) admin.Trash {
	return admin.RecordTrash(admin.TrashRecords[media.Asset]{
		Load:   library.Trashed,
		Fields: searchFields,
		Item: func(asset media.Asset) admin.TrashItem {
			return admin.TrashItem{
				ID:        asset.ID,
				Title:     asset.Filename,
				DeletedAt: *asset.DeletedAt,
				DeletedBy: asset.DeletedBy.String(),
			}
		},
		Undelete: library.Undelete,
		Purge:    library.Purge,
		Reason: func(err error) string {
			switch {
			case errors.Is(err, media.ErrNotFound):
				return admin.NotInTrash
			case errors.Is(err, media.ErrInUse):
				return "This file is still used by content in the trash. Delete that content for good first."
			}

			return ""
		},
	})
}
