package content

import (
	"reflect"
	"strings"

	"github.com/donseba/go-webthings/db"
)

// column is one `db`-tagged field of a fields or translation struct.
type column struct {
	name  string
	index []int
	kind  reflect.Kind
	// null stores "" as NULL and reads NULL as "", for optional references:
	// `db:"parent_id,null"`.
	null bool
}

func columnsOf(t reflect.Type) []column {
	if t.Kind() != reflect.Struct {
		return nil
	}

	var out []column

	for _, field := range reflect.VisibleFields(t) {
		name, options, _ := strings.Cut(field.Tag.Get("db"), ",")
		if !field.IsExported() || name == "" || name == "-" {
			continue
		}

		out = append(out, column{
			name:  name,
			index: field.Index,
			kind:  field.Type.Kind(),
			null:  options == "null" && field.Type.Kind() == reflect.String,
		})
	}

	return out
}

func columnNames(columns []column, alias string) []string {
	names := make([]string, len(columns))

	for i, column := range columns {
		names[i] = alias + column.name
	}

	return names
}

// values returns the field values of v in column order, for INSERT arguments.
func values(v any, columns []column) []any {
	value := reflect.ValueOf(v)
	out := make([]any, len(columns))

	for i, column := range columns {
		out[i] = value.FieldByIndex(column.index).Interface()

		// A db.Ref stores the empty string as NULL.
		if column.null {
			out[i] = db.Ref(value.FieldByIndex(column.index).String())
		}
	}

	return out
}

// pointers returns scan destinations for the fields of the struct v points to.
func pointers(v any, columns []column) []any {
	value := reflect.ValueOf(v).Elem()
	out := make([]any, len(columns))

	for i, column := range columns {
		target := value.FieldByIndex(column.index).Addr().Interface()

		// A db.Ref reads NULL as the empty string.
		if column.null {
			target = (*db.Ref)(target.(*string))
		}

		out[i] = target
	}

	return out
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
