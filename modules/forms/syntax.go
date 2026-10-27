package forms

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
)

// The editor describes fields as text, one field per line:
//
//	name* | type | label | placeholder | help
//
// A * after the name makes the field required; placeholder and help are
// optional. A select field lists its options on the lines below it:
//
//	topic | select | Topic
//	- sales | Sales
//	- support | Support
//
// Names and types are shared by all languages; labels belong to the language
// being edited.

// definitionError is a problem with a form definition. Its text is the
// message for the editor, translated by the admin.
type definitionError string

func (e definitionError) Error() string {
	return string(e)
}

const (
	errLine        definitionError = "Each field needs a line with a name, a type and a label: name | type | label."
	errName        definitionError = "Field names use lowercase letters, digits and underscores, and start with a letter."
	errDuplicate   definitionError = "Each field name can be used only once."
	errType        definitionError = "Unknown field type. Use text, email, textarea, select, checkbox, number or date."
	errOption      definitionError = "Option lines (starting with -) belong below a select field."
	errNoOptions   definitionError = "A select field needs at least one option."
	errOptionValue definitionError = "Each option of a field needs a different value."
	errNoFields    definitionError = "Add at least one field."
	errTooMany     definitionError = "A form can have at most 50 fields with 100 options each."
	errSecondForm  definitionError = "This module holds one form. Edit the existing form instead."
	errNotify      definitionError = "Enter one valid e-mail address for notifications, or leave it empty."
)

const (
	maxFields  = 50
	maxOptions = 100
)

var (
	namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	fieldTypes  = []string{"text", "email", "textarea", "select", "checkbox", "number", "date"}
)

// exampleFields is the starting point for a new form.
const exampleFields = `name* | text | Name
email* | email | Email
message* | textarea | Message
`

// parseFields reads the editor text into fields and their labels.
func parseFields(source string) (FieldList, Labels, error) {
	var fields FieldList

	labels := Labels{}

	for line := range strings.Lines(source) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if option, ok := strings.CutPrefix(line, "-"); ok {
			if err := addOption(fields, labels, option); err != nil {
				return nil, nil, err
			}

			continue
		}

		field, label, err := parseField(line)
		if err != nil {
			return nil, nil, err
		}

		fields = append(fields, field)
		labels[field.Name] = label
	}

	return fields, labels, validateFields(fields)
}

func parseField(line string) (Field, Label, error) {
	columns := strings.Split(line, "|")
	if len(columns) < 3 || len(columns) > 5 {
		return Field{}, Label{}, errLine
	}

	for i := range columns {
		columns[i] = strings.TrimSpace(columns[i])
	}

	// Placeholder and help are optional.
	columns = append(columns, "", "")

	name, required := strings.CutSuffix(columns[0], "*")
	field := Field{
		Name:     strings.TrimSpace(name),
		Type:     strings.ToLower(columns[1]),
		Required: required,
	}

	if columns[2] == "" {
		return Field{}, Label{}, errLine
	}

	label := Label{
		Label:       columns[2],
		Placeholder: columns[3],
		Help:        columns[4],
	}

	return field, label, nil
}

// addOption adds "value | label" to the last field, which must be a select.
func addOption(fields FieldList, labels Labels, line string) error {
	if len(fields) == 0 || fields[len(fields)-1].Type != "select" {
		return errOption
	}

	field := &fields[len(fields)-1]
	value, text, _ := strings.Cut(line, "|")
	value = strings.TrimSpace(value)
	text = strings.TrimSpace(text)

	if strings.Contains(text, "|") {
		return errLine
	}

	if text == "" {
		text = value
	}

	label := labels[field.Name]
	if label.Options == nil {
		label.Options = map[string]string{}
	}

	label.Options[value] = text
	labels[field.Name] = label
	field.Options = append(field.Options, value)

	return nil
}

// formatFields writes fields with their labels in one language as editor text.
func formatFields(fields FieldList, labels Labels) string {
	var out strings.Builder

	for _, field := range fields {
		label := labels[field.Name]

		name := field.Name
		if field.Required {
			name += "*"
		}

		columns := []string{name, field.Type, label.Label, label.Placeholder, label.Help}
		for len(columns) > 3 && columns[len(columns)-1] == "" {
			columns = columns[:len(columns)-1]
		}

		out.WriteString(strings.TrimSpace(strings.Join(columns, " | ")) + "\n")

		for _, option := range field.Options {
			out.WriteString(strings.TrimSpace("- "+option+" | "+label.Options[option]) + "\n")
		}
	}

	return out.String()
}

func validateFields(fields FieldList) error {
	if len(fields) == 0 {
		return errNoFields
	}

	if len(fields) > maxFields {
		return errTooMany
	}

	names := map[string]bool{}

	for _, field := range fields {
		if !namePattern.MatchString(field.Name) || field.Name == frontend.Honeypot {
			return errName
		}

		if names[field.Name] {
			return errDuplicate
		}

		names[field.Name] = true

		if !slices.Contains(fieldTypes, field.Type) {
			return errType
		}

		if err := validateOptions(field); err != nil {
			return err
		}
	}

	return nil
}

func validateOptions(field Field) error {
	switch {
	case field.Type != "select" && len(field.Options) > 0:
		return errOption
	case field.Type == "select" && len(field.Options) == 0:
		return errNoOptions
	case len(field.Options) > maxOptions:
		return errTooMany
	}

	values := map[string]bool{}

	for _, value := range field.Options {
		if value == "" || values[value] || utf8.RuneCountInString(value) > 100 {
			return errOptionValue
		}

		values[value] = true
	}

	return nil
}

func validateText(text Text) error {
	long := content.TooLong(200, text.Title) || content.TooLong(5000, text.Intro, text.Success)

	for _, label := range text.Labels {
		long = long ||
			content.TooLong(200, label.Label, label.Placeholder) ||
			content.TooLong(1000, label.Help)

		for _, option := range label.Options {
			long = long || content.TooLong(200, option)
		}
	}

	if long {
		return content.ErrTooLong
	}

	return nil
}

// ParseFields reads fields in the editor's syntax, for defining a form in code
// (for example in a seed). The labels belong to one language.
func ParseFields(source string) (FieldList, Labels, error) {
	return parseFields(source)
}
