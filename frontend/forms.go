package frontend

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/donseba/go-webthings/mail"

	form "github.com/donseba/go-form/v2"
	formtemplates "github.com/donseba/go-form/v2/templates"
	"github.com/donseba/go-partial/exp/localization"
)

// Visitor forms are go-form's too. A module maps and validates a visitor's
// post onto a struct with go-form's tags with BindForm, and offers that
// struct to themes as a form (VisitorForm), so a theme renders it with
// go-form's functions, which take the limits from the tags, translated with
// the site's translations:
//
//	{{form_open .Form}}
//	    {{form_field .Form "name" nil}}
//	    <button type="submit">{{tl localizer "Send"}}</button>
//	{{form_close}}
//
// The fields render with go-form's markup and the classes of a go-form theme:
// FormTheme, the classes of webthings.css, unless the site names one of its
// own (SetFormTheme, webthings.Config.FormTheme).

// FormTheme is the go-form theme visitor forms render with unless the site
// sets another: go-form's markup with the classes of webthings.css.
const FormTheme = "webthings"

// formClasses are webthings.css's classes for go-form's markup.
var formClasses = formtemplates.ThemeClasses{
	Form:         formtemplates.StyleOption{Class: "wt-form"},
	Wrapper:      formtemplates.StyleOption{Class: "wt-form__field"},
	Fieldset:     formtemplates.StyleOption{Class: "wt-form__fieldset"},
	Label:        formtemplates.StyleOption{Class: "wt-form__label"},
	Description:  formtemplates.StyleOption{Class: "wt-form__help"},
	Input:        formtemplates.StyleOption{Class: "wt-form__input"},
	Select:       formtemplates.StyleOption{Class: "wt-form__input"},
	Textarea:     formtemplates.StyleOption{Class: "wt-form__input"},
	File:         formtemplates.StyleOption{Class: "wt-form__input"},
	Error:        formtemplates.StyleOption{Class: "wt-form__error"},
	RadioGroup:   formtemplates.StyleOption{Class: "wt-form__options"},
	RadioWrapper: formtemplates.StyleOption{Class: "wt-form__option"},
	// Checkbox groups lay out like radio groups; a single checkbox is one
	// option.
	Multicheckbox:   formtemplates.StyleOption{Class: "wt-form__options"},
	CheckboxWrapper: formtemplates.StyleOption{Class: "wt-form__option"},
	Button:          formtemplates.StyleOption{Class: "wt-button"},
	FormButtons:     formtemplates.StyleOption{Class: "wt-form__row"},
	// The spam trap, hidden by webthings.css rather than an inline style.
	Honeypot: formtemplates.StyleOption{Class: formtemplates.HoneypotClass},
}

// siteForms renders the visitor forms of the site's templates: go-form
// with the site's translations and form theme, made when a template first
// renders a form.
type siteForms struct {
	once  sync.Once
	theme string
	form  *form.Form
}

// SetFormTheme names the go-form theme the site's visitor forms render
// with (form_field and the other form functions), such as one the site
// registered with go-form's formtemplates.RegisterClassTheme with the classes
// of its CSS; empty is FormTheme. Call it before serving.
func (f *Frontend) SetFormTheme(name string) {
	f.forms.theme = name
}

// siteForm is the site's form, made at first use, translated with the
// site's translations, with the validation of e-mail addresses
// validate:"mail" (mail.Rule).
func (f *Frontend) siteForm() *form.Form {
	f.forms.once.Do(func() {
		theme := f.forms.theme
		if theme == "" {
			theme = FormTheme

			registerFormTheme()
		}

		forms := form.NewTranslatedForm(f.translateForm)
		forms.SetTheme(theme)
		forms.RegisterLocalizedValidation(mail.Rule, mail.FormRule)

		f.forms.form = forms
	})

	return f.forms.form
}

// translateForm translates go-form's texts, the labels of a form's tags and
// go-form's own messages, with the site's translations; without them they
// show as written, go-form's own messages in go-form's catalogue of the
// page's language.
func (f *Frontend) translateForm(loc form.Localizer, key string, args ...any) string {
	return f.tr().Tl(loc, key, args...)
}

// registerFormTheme registers FormTheme with go-form.
func registerFormTheme() {
	if _, err := formtemplates.RegisterClassTheme(FormTheme, formClasses); err != nil {
		slog.Error("visitor form theme", "error", err)
	}
}

// defaultForm renders forms outside a page, such as in a test of a
// template function: go-form's texts as written, with FormTheme.
var defaultForm = sync.OnceValue(func() *form.Form {
	registerFormTheme()

	forms := form.NewForm()
	forms.SetTheme(FormTheme)

	return forms
})

// pageForm is the form and the localizer of the page being rendered, with
// the site's timezone (see formLocalizer).
func pageForm(ctx context.Context) (*form.Form, form.Localizer) {
	current, ok := renderingFrom(ctx)
	if !ok {
		return defaultForm(), localization.New("")
	}

	return current.frontend.siteForm(), current.frontend.formLocalizer(ctx, current.locale)
}

// visitorLocalizer is the localizer of visitor forms: the page's language,
// and the site's timezone, in which go-form shows date-times
// (datetime-local inputs) as a form.TimeLocalizer, as the admin does.
type visitorLocalizer struct {
	localization.Localizer

	zone func() *time.Location
}

// Location is the site's timezone.
func (l visitorLocalizer) Location() *time.Location {
	return l.zone()
}

// formLocalizer is the localizer of visitor forms in locale for a request
// with ctx. The site's timezone is looked up when a form needs it.
func (f *Frontend) formLocalizer(ctx context.Context, locale string) visitorLocalizer {
	return visitorLocalizer{
		Localizer: localization.New(locale),
		zone: func() *time.Location {
			return f.location(ctx)
		},
	}
}

// formOpen is {{form_open .Form}}: the opening form tag of a model made with
// form.WithInfo, such as VisitorForm, with its hidden fields. attrs are more
// attributes of the tag (go-form's Open), as name/value pairs or
// template.HTMLAttr, such as go-partial's submitAttrs:
//
//	{{form_open .Form "class" "booking" (submitAttrs runtime .Action "target" "#booking")}}
func formOpen(ctx context.Context, model any, attrs ...any) (template.HTML, error) {
	forms, loc := pageForm(ctx)

	return forms.Open(loc, model, attrs...)
}

// formHidden is {{form_hidden .Form}}: only the hidden fields of a model made
// with form.WithInfo, which form_open adds after the tag (go-form's Hidden):
// the spam trap and Info.Hidden, such as the back field of a VisitorForm,
// for a template that writes the form tag itself.
func formHidden(ctx context.Context, model any) (template.HTML, error) {
	forms, loc := pageForm(ctx)

	return forms.Hidden(loc, model)
}

// formField is {{form_field .Form "name" .Errors}}: one field of a model
// with its label, description and errors, in the page's language.
func formField(ctx context.Context, model any, name string, errs form.FieldErrors) (template.HTML, error) {
	forms, loc := pageForm(ctx)

	return forms.Field(loc, model, name, errs)
}

// formButtons is {{form_buttons .Form}}: the submit button of a model made
// with form.WithInfo, and its cancel link.
func formButtons(ctx context.Context, model any) (template.HTML, error) {
	forms, loc := pageForm(ctx)

	return forms.Buttons(loc, model)
}

// formClose is {{form_close}}, the closing form tag.
func formClose(ctx context.Context) (template.HTML, error) {
	forms, _ := pageForm(ctx)

	return forms.Close()
}

// formRender is {{form_render .Form .Errors}}: the whole form of a model
// made with form.WithInfo, in the page's language.
func formRender(ctx context.Context, model any, errs form.FieldErrors) (template.HTML, error) {
	forms, loc := pageForm(ctx)

	return forms.Render(loc, model, errs)
}

// ValidateSchema checks the values of a visitor's form defined at runtime,
// a go-form Schema such as a form an editor put together, as BindForm
// checks a struct: go-form's errors, with their messages translated with
// the site's translations into the language of the page being served
// (Mount.Locale), for the page to show at their fields with form_field or
// form_render.
func (f *Frontend) ValidateSchema(r *http.Request, schema form.Schema, values url.Values) form.FieldErrors {
	loc := localization.New(MountFrom(r.Context()).Locale)

	return f.siteForm().ValidateSchemaLocalized(schema, values, loc)
}

// BindForm maps a visitor's post onto input, a pointer to a struct with
// go-form's tags, and validates it, as a module action does before it
// stores anything:
//
//	var in PostInput
//
//	errs, err := env.Frontend.BindForm(r, &in)
//	if err != nil || len(errs) > 0 {
//		env.Frontend.KeepForm(w, r, key, in, errs)
//		frontend.Back(w, r, key, "invalid")
//
//		return
//	}
//
// It is go-form's Bind: text is trimmed (MapOptions.TrimSpace), and its
// line breaks are read as "\n", as a browser counts them for maxlength,
// rather than the "\r\n" it sends (MapOptions.NormalizeNewlines). A value
// that cannot be read as its field's type, such as "abc" for a number,
// comes back as an error with the rule "invalid" in place of what
// validation says about the field; the others carry the rule they broke
// (form.FieldValidationError.Rule), for a result code per rule. Limits of
// an input that implements form.Overrider apply. The messages are in the
// language of the page the form was on (BackLocale), translated with the
// site's translations, for the form to show at its fields (KeepForm). err
// is only set when the post cannot be mapped at all. Date-times of
// datetime-local inputs are read in the site's timezone, in which visitor
// forms show them (MapOptions.Location), as the admin does.
func (f *Frontend) BindForm(r *http.Request, input any) (form.FieldErrors, error) {
	loc := f.formLocalizer(r.Context(), f.BackLocale(r))
	options := form.MapOptions{
		TrimSpace:         true,
		NormalizeNewlines: true,
		Location:          f.location(r.Context()),
	}

	return f.siteForm().Bind(r, input, options, loc)
}
