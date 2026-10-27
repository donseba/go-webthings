package forms

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"time"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/mail"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-form/v2/types"
	"github.com/donseba/go-router"
	"github.com/donseba/go-router/middleware"
)

// submitText is the text of the form's button, which the site's
// translations translate.
const submitText = "Send"

const (
	bodyLimit = 64 << 10
	// Each visitor may send the form postLimit times per postWindow. Posts
	// with errors count too, so the limit leaves room for a few corrections.
	postLimit  = 10
	postWindow = 10 * time.Minute
)

// FormView is the data of forms/form.gohtml.
type FormView struct {
	Title string
	Intro template.HTML
	// Action is the URL the form posts to: the page itself.
	Action string
	// Form is the form's model: its fields as the editor defined them, in
	// the page's language, with the visitor's answers after a post with
	// errors, for go-form's template functions, such as {{form_render
	// .Form .Errors}}, or {{form_open .Form}} with {{form_field .Form name
	// .Errors}} per name of Fields. form_open adds the spam trap
	// (frontend.Honeypot), and the fields' ids start with the instance key.
	Form form.RenderModel
	// Fields are the names of the form's fields, in order, for form_field.
	Fields []string
	// Errors are the problems of a post per field, with messages in the
	// page's language, which form_field and form_render show at their
	// fields; empty otherwise.
	Errors form.FieldErrors
	// Invalid is set when a submission has errors; see Errors.
	Invalid bool
	// Throttled is set when the visitor sent the form too often in a short
	// time. Nothing was stored; the form is shown with their answers, to send
	// again later.
	Throttled bool
	// Sent is set after a successful submission; show Success instead of the form.
	Sent    bool
	Success template.HTML
}

// handler serves the form on the page the module is placed on, and receives
// it there too: unlike the visitor actions under /_/ (webthings.Env.Actions), a
// post with errors can show the form again with the visitor's answers, which
// a redirect back would lose. Like those actions, it rejects cross-site posts
// (http.CrossOriginProtection), has a honeypot (frontend.Honeypot) and is
// throttled per visitor (frontend.Throttle). None of this keeps state
// between requests, so a form stays valid however long it is open.
func (s *Service) handler() *router.Router {
	// Paths without a route get the site's 404 page.
	routes := s.frontend.Router()

	// No answer about a form may be kept by a cache, also not the 404 of a
	// path without a route, so this wraps every answer of the router.
	routes.UseAll(middleware.NoStore)
	routes.Use(http.NewCrossOriginProtection().Handler)
	routes.Use(frontend.Limits(s.key, frontend.ActionLimits{
		Body:      bodyLimit,
		Posts:     postLimit,
		Window:    postWindow,
		Throttled: http.HandlerFunc(s.throttled),
	}))

	routes.Get("/{$}", s.show).As("form")
	routes.Post("/{$}", s.submit).As("submit")

	return routes
}

func (s *Service) show(w http.ResponseWriter, r *http.Request) {
	item, ok := s.published(w, r)
	if !ok {
		return
	}

	view := s.view(r, item, nil, nil)
	view.Sent = r.URL.Query().Has("sent")

	s.render(w, r, view, http.StatusOK)
}

func (s *Service) submit(w http.ResponseWriter, r *http.Request) {
	item, ok := s.published(w, r)
	if !ok {
		return
	}

	// The form route has no parameters, so it always has a path.
	sent, _ := s.public.For(r).RoutePathArgs("form", url.Values{"sent": {"1"}})

	// Bots fill in the invisible trap; they are told it worked.
	if frontend.IsSpam(r) {
		http.Redirect(w, r, sent, http.StatusSeeOther)

		return
	}

	locale := frontend.MountFrom(r.Context()).Locale
	schema := formSchema(item, locale)
	values := form.MapValues(r, schema)

	if errs := s.frontend.ValidateSchema(r, schema, values); len(errs) > 0 {
		view := s.view(r, item, values, errs)
		view.Invalid = true

		s.render(w, r, view, http.StatusUnprocessableEntity)

		return
	}

	answers := answersOf(item, locale, values)

	id, err := s.save(r.Context(), locale, answers)
	if err != nil {
		s.frontend.Fail(w, r, fmt.Errorf("save form submission: %w", err))

		return
	}

	s.notify(r, item, locale, id, answers)

	http.Redirect(w, r, sent, http.StatusSeeOther)
}

// throttled shows the form again with the visitor's answers when they sent
// it too often; nothing is stored.
func (s *Service) throttled(w http.ResponseWriter, r *http.Request) {
	item, ok := s.published(w, r)
	if !ok {
		return
	}

	// A body that cannot be read leaves the answers empty.
	_ = r.ParseForm()

	view := s.view(r, item, r.PostForm, nil)
	view.Throttled = true

	s.render(w, r, view, http.StatusTooManyRequests)
}

// published returns the form if it is published in the page's language, and
// renders not found otherwise.
func (s *Service) published(w http.ResponseWriter, r *http.Request) (Item, bool) {
	items, err := s.forms.ListPublished(r.Context(), s.key, frontend.MountFrom(r.Context()).Locale)
	if err != nil {
		s.frontend.Fail(w, r, fmt.Errorf("load form: %w", err))

		return Item{}, false
	}

	if len(items) == 0 {
		s.frontend.NotFound(w, r)

		return Item{}, false
	}

	return items[0], true
}

func (s *Service) view(r *http.Request, item Item, values url.Values, errs form.FieldErrors) FormView {
	ctx := r.Context()
	locale := frontend.MountFrom(ctx).Locale
	text, _ := item.Text(locale)
	schema := formSchema(item, locale)

	view := FormView{
		Title:   text.Title,
		Intro:   s.frontend.Markdown(ctx, text.Intro),
		Success: s.frontend.Markdown(ctx, text.Success),
		Action:  s.public.For(r).RoutePath("submit"),
		Errors:  errs,
	}

	view.Form = form.WithInfo(schema.Bind(values), form.Info{
		Target:     view.Action,
		Method:     http.MethodPost,
		SubmitText: submitText,
		Honeypot:   frontend.Honeypot,
		IDPrefix:   s.key,
	})

	for _, field := range item.Fields.Fields {
		view.Fields = append(view.Fields, field.Name)
	}

	return view
}

func (s *Service) render(w http.ResponseWriter, r *http.Request, view FormView, status int) {
	s.frontend.Render(w, r, frontend.Content{
		Template: "form",
		Title:    view.Title,
		Data:     view,
		Status:   status,
	})
}

// formSchema is the form as go-form renders and checks it, in locale: per
// type of field its control and length limit, with the editor's label,
// placeholder, help and option labels; a text field holds one line. A
// field added in another language has its name as label until it has one.
func formSchema(item Item, locale string) form.Schema {
	text, _ := item.Text(locale)

	var fields []form.Spec

	for _, field := range item.Fields.Fields {
		label := text.Labels[field.Name]
		spec := form.Spec{
			Name:        field.Name,
			Label:       label.Label,
			Placeholder: label.Placeholder,
			Description: label.Help,
			Required:    field.Required,
			MaxLength:   100,
		}

		switch field.Type {
		case "text":
			spec.MaxLength = 500
			spec.SingleLine = true
		case "email":
			// An address mail can be sent to, as the site's other forms
			// check it (mail.Rule): a reply goes to it.
			spec.InputType = types.InputFieldTypeEmail
			spec.MaxLength = 254
			spec.Validate = mail.Rule
		case "textarea":
			spec.Type = types.FieldTypeTextArea
			spec.MaxLength = 10000
		case "select":
			spec.Type = types.FieldTypeDropdown

			// The empty choice comes first, as a select without one
			// would show its first option as chosen.
			if spec.Placeholder == "" {
				spec.Placeholder = "—"
			}

			for _, option := range field.Options {
				name := label.Options[option]
				if name == "" {
					name = option
				}

				spec.Options = append(spec.Options, types.FieldValue{Value: option, Name: name})
			}
		case "checkbox":
			spec.Type = types.FieldTypeCheckbox
		case "number":
			spec.InputType = types.InputFieldTypeNumber
		case "date":
			spec.InputType = types.InputFieldTypeDate
		}

		fields = append(fields, spec)
	}

	return form.Schema{Fields: fields}
}

// answersOf are the answers to store of values, the submitted values of
// the form's fields (form.MapValues of its schema) after validation, with
// their labels in locale: a checkbox is "yes" or "no".
func answersOf(item Item, locale string, values url.Values) Answers {
	text, _ := item.Text(locale)
	answers := Answers{}

	for _, field := range item.Fields.Fields {
		value := values.Get(field.Name)

		switch {
		case field.Type == "checkbox" && value != "":
			value = "yes"
		case field.Type == "checkbox":
			value = "no"
		}

		label := text.Labels[field.Name].Label
		if label == "" {
			label = field.Name
		}

		answers = append(answers, Answer{Name: field.Name, Label: label, Value: value})
	}

	return answers
}
