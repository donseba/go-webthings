package content

import (
	"database/sql/driver"
	"fmt"
	"slices"
	"strings"
)

// IDs is a small list of references stored in one text column, for example a
// post's categories. Use a separate table when the list can grow large.
type IDs []string

// Value implements driver.Valuer: the IDs are stored comma separated.
func (ids IDs) Value() (driver.Value, error) {
	return strings.Join(ids, ","), nil
}

// Scan implements sql.Scanner, reading the comma separated column back; an
// empty or NULL column is no IDs.
func (ids *IDs) Scan(value any) error {
	var text string

	switch v := value.(type) {
	case nil:
	case string:
		text = v
	case []byte:
		text = string(v)
	default:
		return fmt.Errorf("cannot scan %T into content.IDs", value)
	}

	*ids = nil

	for id := range strings.SplitSeq(text, ",") {
		if id != "" {
			*ids = append(*ids, id)
		}
	}

	return nil
}

// Has reports whether id is in the list.
func (ids IDs) Has(id string) bool {
	return slices.Contains(ids, id)
}
