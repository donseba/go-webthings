package db

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// Values such as a list of form fields can live in one text column as
// JSON. Their type implements driver.Valuer and sql.Scanner with JSONValue
// and ScanJSON, so it keeps its own name and methods everywhere else:
//
//	type Fields []Field
//
//	// Value stores the fields as JSON.
//	func (f Fields) Value() (driver.Value, error) {
//		return db.JSONValue(f)
//	}
//
//	// Scan reads fields stored by Value.
//	func (f *Fields) Scan(src any) error {
//		return db.ScanJSON(src, f)
//	}
//
// The column is a text column (TEXT, or MEDIUMTEXT on MySQL for long
// values), which every dialect reads back as it was written.

// JSONValue encodes value as JSON text, for the Value method of a type
// stored as JSON.
func JSONValue(value any) (driver.Value, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode JSON column: %w", err)
	}

	return string(data), nil
}

// ScanJSON decodes the JSON text of a column into target, a pointer, for
// the Scan method of a type stored as JSON. NULL and empty text leave
// target as it is, so they read as the zero value.
func ScanJSON(src, target any) error {
	var data []byte

	switch value := src.(type) {
	case nil:
		return nil
	case string:
		data = []byte(value)
	case []byte:
		data = value
	default:
		return fmt.Errorf("scan %T as JSON", src)
	}

	if len(data) == 0 {
		return nil
	}

	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode JSON column: %w", err)
	}

	return nil
}
