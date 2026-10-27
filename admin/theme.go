package admin

import (
	"fmt"

	"github.com/donseba/go-form/v2/templates"
)

// formTheme is the name of go-form's theme with the admin's CSS classes.
const formTheme = "webthings-admin"

// formClasses are the admin's CSS classes for go-form's markup.
var formClasses = templates.ThemeClasses{
	Wrapper:         templates.StyleOption{Class: "field"},
	Label:           templates.StyleOption{Class: "field-label"},
	Description:     templates.StyleOption{Class: "field-help"},
	Input:           templates.StyleOption{Class: "control"},
	Select:          templates.StyleOption{Class: "control"},
	Textarea:        templates.StyleOption{Class: "control min-h-40"},
	Button:          templates.StyleOption{Class: "button"},
	Error:           templates.StyleOption{Class: "field-error"},
	Required:        templates.StyleOption{Class: "muted"},
	FormButtons:     templates.StyleOption{Class: "form-actions"},
	CheckboxWrapper: templates.StyleOption{Class: "flex gap-3 items-center"},
	CheckboxLabel:   templates.StyleOption{Class: "field-label"},
	Cancel:          templates.StyleOption{Class: "button-secondary"},
	// Grouped fields (a struct field with a legend, such as an address)
	// form a fieldset-like box with a heading.
	FormGroup:  templates.StyleOption{Class: "field-group"},
	FormHeader: templates.StyleOption{Class: "field-group-header"},
	FormLegend: templates.StyleOption{Class: "field-group-legend"},
	FormBody:   templates.StyleOption{Class: "field-group-body"},
	// The spam trap, hidden by source.css rather than an inline style.
	Honeypot: templates.StyleOption{Class: templates.HoneypotClass},
}

// configureTheme registers go-form's templates with the admin's CSS classes.
// Registering again, as every Admin does, returns the same theme.
func configureTheme() error {
	if _, err := templates.RegisterClassTheme(formTheme, formClasses); err != nil {
		return fmt.Errorf("configure admin theme: %w", err)
	}

	return nil
}
