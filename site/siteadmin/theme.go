package siteadmin

import (
	"cmp"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/site"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-form/v2/types"
	"github.com/donseba/go-router"
)

// ThemeView is the theme section: the themes and the settings of one.
type ThemeView struct {
	Themes []ThemeChoice
	// Screenshots is set when a theme has a picture (ThemeChoice.Screenshot):
	// the others then show a placeholder of the same size, so the cards
	// line up.
	Screenshots bool
	// Selected is the theme whose settings are shown.
	Selected ThemeChoice
	// Form is the form of the selected theme's settings (a form.Schema made
	// from its theme.json), read-only without CanEdit; nil for a theme
	// without settings.
	Form   *form.RenderModel
	Errors form.FieldErrors
	// CanEdit offers choosing a theme and saving its settings; others see
	// the settings without their button.
	CanEdit bool
}

// ThemeChoice is one theme the site can use, with links to choose and to
// preview it.
type ThemeChoice struct {
	Name        string
	Title       string
	Description string
	Active      bool
	URL         string
	PreviewURL  string
	// Screenshot is the address of the picture of the theme its
	// theme.json names (frontend.Theme.Screenshot); empty without one.
	Screenshot string
	// Activate is the "Use this theme" button; nil for the active theme
	// and without the right to edit.
	Activate *form.RenderModel
}

// activateInput is the form that makes a theme the active one.
type activateInput struct {
	Theme string `form:"input,hidden" name:"theme"`
}

type themeSection struct {
	admin    *admin.Admin
	settings *site.Service
	frontend *frontend.Frontend
	themes   *frontend.Themes
	public   func() http.Handler
	// guide is the setup guide, which shows the cards too (see guide.go).
	guide *guide
}

// NewTheme is the admin section to choose the site's theme and edit its
// settings, with options.Frontend's themes and options.Public for
// previews.
func NewTheme(settings *site.Service, options Options) admin.Section {
	return admin.Section{
		ID:         "theme",
		Permission: "site",
		Title:      "Theme",
		Icon:       "palette",
		Files:      files,
		Hidden:     len(options.Frontend.Themes().List()) == 0,
		Routes: func(routes *admin.Routes) {
			s := newGuide(routes.Admin(), settings, options).theme

			routes.Get("", admin.View, s.show).As("index")
			routes.Post("/activate", admin.Edit, s.activate).As("activate")
			routes.Post("/values", admin.Edit, s.saveValues).As("values")
			routes.Get("/preview/{theme}", admin.View, s.preview).As("preview")
		},
	}
}

func (s themeSection) show(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, r.URL.Query().Get("theme"), nil, nil, http.StatusOK)
}

// errNoTheme is a theme to show that the site does not have.
var errNoTheme = errors.New("no_such_theme")

// render shows the themes and the settings of selected: the stored values,
// or posted ones with errs; in the setup guide, its Theme step (see
// guide.step).
func (s themeSection) render(
	w http.ResponseWriter,
	r *http.Request,
	selected string,
	posted url.Values,
	errs form.FieldErrors,
	status int,
) {
	view, err := s.view(r, selected, posted, errs)
	if errors.Is(err, errNoTheme) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	if step := s.guide.step(r); step != "" {
		s.guide.render(w, r, step, guideParts{theme: &view, status: status})

		return
	}

	s.admin.Render(w, r, admin.Page{
		Title:    "Theme",
		Template: "templates/theme.gohtml",
		Includes: []string{themeCards},
		Files:    files,
		Data:     view,
		Status:   status,
	})
}

// view is the Theme screen with the settings of selected, or of the
// active theme when selected is empty. In the setup guide the forms post
// back to the guide's step, and the links stay in it.
func (s themeSection) view(r *http.Request, selected string, posted url.Values, errs form.FieldErrors) (ThemeView, error) {
	ctx := r.Context()

	current, err := s.settings.Settings(ctx)
	if err != nil {
		return ThemeView{}, err
	}

	active, _ := s.themes.Get(current.Theme)

	if selected == "" {
		selected = active.Name
	}

	theme, ok := s.themes.Get(selected)
	if !ok {
		return ThemeView{}, errNoTheme
	}

	values := posted
	if values == nil {
		stored, err := s.settings.ThemeValues(ctx, theme.Name)
		if err != nil {
			return ThemeView{}, err
		}

		values = shownValues(theme, stored)
	}

	// In the guide, the forms come back to its step, and the links to the
	// themes' settings stay in it.
	step := s.guide.step(r)
	query := url.Values{}
	settingsURL := func(name string) string {
		return s.admin.URL("admin.theme.index", url.Values{"theme": {name}})
	}

	if step != "" {
		query.Set(guideParam, step)
		settingsURL = func(name string) string {
			return s.admin.URL("admin.site.guide-step", "step", step, url.Values{"theme": {name}})
		}
	}

	view := ThemeView{
		Errors:  errs,
		CanEdit: s.admin.Can(r, admin.Permission{Section: "site", Action: admin.Edit}),
	}

	for _, candidate := range s.themes.List() {
		choice := ThemeChoice{
			Name:        candidate.Name,
			Title:       candidate.Title,
			Description: candidate.Description,
			Active:      candidate.Name == active.Name,
			URL:         settingsURL(candidate.Name),
			PreviewURL:  s.admin.URL("admin.theme.preview", "theme", candidate.Name),
			Screenshot:  s.frontend.ThemeScreenshot(candidate),
		}

		if choice.Screenshot != "" {
			view.Screenshots = true
		}

		if view.CanEdit && !choice.Active {
			model := s.admin.Form(
				r,
				activateInput{Theme: candidate.Name},
				s.admin.URL("admin.theme.activate", query),
				"Use this theme",
			)
			model = s.admin.Confirm(r, model, "Use this theme on the website? Visitors see it right away.")
			choice.Activate = &model
		}

		if candidate.Name == theme.Name {
			view.Selected = choice
		}

		view.Themes = append(view.Themes, choice)
	}

	if len(theme.Settings) > 0 {
		values.Set("theme", theme.Name)

		model := s.admin.Form(r, s.schema(theme).Bind(values), s.admin.URL("admin.theme.values", query), "Save")
		model.Info.ReadOnly = !view.CanEdit
		view.Form = &model
	}

	return view, nil
}

func (s themeSection) activate(w http.ResponseWriter, r *http.Request) {
	var in activateInput

	if errs, err := s.admin.MapForm(r, &in); err != nil || len(errs) > 0 {
		s.admin.BadRequest(w, r)

		return
	}

	theme, ok := s.themes.Get(in.Theme)
	if !ok || theme.Name != in.Theme {
		s.admin.BadRequest(w, r)

		return
	}

	if err := s.settings.SetTheme(r.Context(), theme.Name); err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	// The guide's Theme step goes on with the settings of the theme.
	s.guide.done(w, r, s.admin.URL("admin.theme.index"), s.guide.stay(r, stepTheme, nil))
}

func (s themeSection) saveValues(w http.ResponseWriter, r *http.Request) {
	name := r.PostFormValue("theme")

	theme, ok := s.themes.Get(name)
	if !ok || theme.Name != name {
		s.admin.BadRequest(w, r)

		return
	}

	schema := s.schema(theme)
	posted := form.MapValues(r, schema)
	errs := s.admin.ValidateSchema(r, schema, posted)
	values := map[string]string{}

	// The theme's own rules check what the form's rules let through.
	for _, setting := range theme.Settings {
		name := settingField + setting.Name

		if len(errs.Messages(name)) > 0 {
			continue
		}

		value, err := setting.Clean(posted.Get(name))
		if err != nil {
			errs = append(errs, admin.FieldError(name, s.admin.T(r, err.Error()))...)

			continue
		}

		// A value equal to the default is not stored, so later changes to the
		// theme's default apply.
		if value != setting.Default {
			values[setting.Name] = value
		}
	}

	if len(errs) > 0 {
		s.render(w, r, theme.Name, posted, errs, http.StatusUnprocessableEntity)

		return
	}

	if err := s.settings.SaveThemeValues(r.Context(), theme.Name, values); err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	// The guide's Theme step goes on with the logo.
	query := url.Values{"theme": {theme.Name}}
	s.guide.done(w, r, s.admin.URL("admin.theme.index", query), s.guide.stay(r, stepTheme, query))
}

// settingField starts the names of the settings' fields, such as
// "value.accent".
const settingField = "value."

// schema is the form of a theme's settings, as its theme.json declares them,
// labelled in the admin language, with the theme's name hidden.
func (s themeSection) schema(theme frontend.Theme) form.Schema {
	language := strings.SplitN(s.admin.Language(), "_", 2)[0]
	schema := form.Schema{Fields: []form.Spec{{Name: "theme", InputType: types.InputFieldTypeHidden}}}

	for _, setting := range theme.Settings {
		spec := form.Spec{Name: settingField + setting.Name, Label: setting.Label.In(language)}

		switch setting.Type {
		case "select":
			spec.Type = types.FieldTypeDropdown

			for _, option := range setting.Options {
				spec.Options = append(spec.Options, types.FieldValue{Value: option, Name: option})
			}
		case "color":
			spec.InputType = types.InputFieldTypeColor
			spec.Class = "w-24"
			// The colour the theme's own rule (ThemeSetting.Clean) accepts,
			// so the editor sees the refusal at the field first.
			spec.Pattern = "^(?:#[0-9a-fA-F]{6})?$"
			spec.Messages = map[string]string{"pattern": "use a colour like #1a2b3c"}
		case "number":
			numberSpec(&spec, setting)
		default:
			spec.Placeholder = setting.Default
			spec.MaxLength = 500
		}

		schema.Fields = append(schema.Fields, spec)
	}

	return schema
}

// numberSpec makes spec the number input of a number setting: its range
// and step, which the browser and the form check before the theme's own
// rule (ThemeSetting.Clean), "any" for a setting without a step, and its
// unit after the label, such as "Text width (rem)".
func numberSpec(spec *form.Spec, setting frontend.ThemeSetting) {
	spec.InputType = types.InputFieldTypeNumber
	spec.Class = "w-24"
	spec.Step = "any"

	if setting.Min != nil && setting.Max != nil {
		spec.Min = strconv.FormatFloat(*setting.Min, 'f', -1, 64)
		spec.Max = strconv.FormatFloat(*setting.Max, 'f', -1, 64)
	}

	if setting.Step > 0 {
		spec.Step = strconv.FormatFloat(setting.Step, 'f', -1, 64)
	}

	if setting.Unit != "" {
		spec.Label += " (" + setting.Unit + ")"
	}
}

// shownValues are the stored values of a theme's settings as its form
// shows them: a select, number or colour without a value shows its default
// (black for a colour without one); a text field shows the default as
// placeholder.
func shownValues(theme frontend.Theme, stored map[string]string) url.Values {
	values := url.Values{}

	for _, setting := range theme.Settings {
		value := stored[setting.Name]

		switch setting.Type {
		case "select", "number":
			value = cmp.Or(value, setting.Default)
		case "color":
			value = cmp.Or(value, setting.Default, "#000000")
		}

		values.Set(settingField+setting.Name, value)
	}

	return values
}

// preview shows the public home page, or ?path=/en/about, in a theme.
func (s themeSection) preview(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("theme")

	theme, ok := s.themes.Get(name)
	if !ok || theme.Name != name {
		http.NotFound(w, r)

		return
	}

	// Only a path on the site is previewed (router.LocalURL), with its
	// query.
	page, ok := router.LocalURL(r.URL.Query().Get("path"))
	if !ok {
		current, err := s.settings.Settings(r.Context())
		if err != nil {
			s.admin.Fail(w, r, err)

			return
		}

		page = &url.URL{Path: "/" + current.DefaultLocale + "/"}
	}

	public := r.Clone(frontend.WithTheme(r.Context(), theme.Name))
	public.URL.Path = page.Path
	public.URL.RawPath = page.RawPath
	public.URL.RawQuery = page.RawQuery
	public.RequestURI = page.RequestURI()

	// The admin's pages are no-store already (admin.pageHeaders).
	s.public().ServeHTTP(w, public)
}
