package webthings

import (
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/translations"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-translator/v2/extract"
)

// Translations is the "translations" command of a site's main, with dir its
// translations folder (where the .po files are):
//
//	go run . translations              writes dir/translations.pot
//	go run . translations check        lists the texts each .po file misses
//	go run . translations check --csv  the same as CSV
//
// The check returns translations.ErrMissing when a language misses texts, so
// the command fails. It reads TranslationCatalog and needs no Start.
func (a *App) Translations(dir string, args []string) error {
	catalog, err := a.TranslationCatalog()
	if err != nil {
		return err
	}

	return translations.Run(catalog, dir, args, os.Stdout, os.Stderr)
}

// TranslationCatalog is every text the site translates, found with
// go-translator's extract package. First come the texts of every template
// in Config.Templates. Then, in the order of their references, come those
// of the default templates of the modules (Module.Templates,
// Module.Components and Module.Mail) and of the core components that not
// every theme replaces, referenced as module:<type>/<name>.gohtml,
// module:<type>/mail/<name>.gohtml and core:site/<name>.gohtml, and the
// texts Go code translates: core's, such as the 404 page's title "Not
// found" and the networks' names of share links and profiles
// (frontend.CoreTexts), as core:texts, the names of months and days and the
// date patterns, with their contexts (frontend.CalendarTexts), as
// core:calendar, which a catalogue may translate to replace the built-in
// ones and must for a language without built-in ones, and the texts of the
// modules' visitor forms (Module.Forms, go-form's Texts) as
// module:<type>/forms. It needs no Start.
func (a *App) TranslationCatalog() (*extract.Catalog, error) {
	sources, err := extract.Files(a.config.Templates)
	if err != nil {
		return nil, err
	}

	themes, err := a.themeFiles()
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}

	var defaults []defaultTexts

	add := func(texts defaultTexts, candidates ...string) {
		if seen[texts.name] || replacedEverywhere(themes, candidates) {
			return
		}

		seen[texts.name] = true

		defaults = append(defaults, texts)
	}

	for _, module := range a.modules {
		for _, template := range module.Templates {
			add(
				defaultTexts{
					name:     "module:" + module.Type + "/" + template.Name + ".gohtml",
					template: frontend.Scaffold(template),
				},
				module.Key+"/"+template.Name+".gohtml",
				module.Type+"/"+template.Name+".gohtml",
			)
		}

		for _, component := range module.Components {
			add(
				defaultTexts{
					name:     "module:" + module.Type + "/" + component.Name + ".gohtml",
					template: frontend.Scaffold(component),
				},
				module.Key+"/"+component.Name+".gohtml",
				module.Type+"/"+component.Name+".gohtml",
			)
		}

		for _, template := range module.Mail {
			add(
				defaultTexts{
					name:     "module:" + module.Type + "/mail/" + template.Name + ".gohtml",
					template: frontend.MailScaffold(template),
				},
				"mail/"+module.Key+"/"+template.Name+".gohtml",
				"mail/"+module.Type+"/"+template.Name+".gohtml",
			)
		}
	}

	for _, component := range frontend.CoreComponents() {
		add(
			defaultTexts{name: "core:site/" + component.Name + ".gohtml", template: frontend.Scaffold(component)},
			"site/"+component.Name+".gohtml",
		)
	}

	add(defaultTexts{name: "core:texts", messages: extract.Texts("core:texts", frontend.CoreTexts()...)})
	add(defaultTexts{name: frontend.CalendarReference, messages: calendarTexts(frontend.CalendarReference)})

	// The texts of the modules' visitor forms are only needed when a
	// template renders forms with go-form's functions, a theme's or a
	// default one.
	if rendersForms(sources) || slices.ContainsFunc(defaults, defaultTexts.rendersForms) {
		for _, module := range a.modules {
			if texts := form.Texts(module.Forms...); len(texts) > 0 {
				name := "module:" + module.Type + "/forms"

				add(defaultTexts{name: name, messages: extract.Texts(name, texts...)})
			}
		}
	}

	slices.SortFunc(defaults, func(a, b defaultTexts) int {
		return strings.Compare(a.name, b.name)
	})

	catalog, err := extract.Extract(sources...)
	if err != nil {
		return nil, err
	}

	for _, texts := range defaults {
		if err := texts.addTo(catalog); err != nil {
			return nil, err
		}
	}

	return catalog, nil
}

// defaultTexts are texts the site translates that are not in its own
// templates: a default template of a module or of core, or texts Go code
// translates, named by their reference in the catalog.
type defaultTexts struct {
	name string
	// template is the default template.
	template string
	// messages are the texts Go code translates, without a template.
	messages []extract.Message
}

// addTo adds the texts to catalog: the template's, extracted, or the texts
// Go code translates.
func (d defaultTexts) addTo(catalog *extract.Catalog) error {
	if d.messages != nil {
		catalog.Add(d.messages...)

		return nil
	}

	found, err := extract.Extract(extract.Source{Name: d.name, Text: d.template})
	if err != nil {
		return err
	}

	catalog.Add(found.Messages...)
	catalog.Warnings = append(catalog.Warnings, found.Warnings...)
	catalog.Sources += found.Sources

	return nil
}

// calendarTexts are the names of months and days and the date patterns
// core looks up in the translations (frontend.CalendarTexts), referenced
// by reference.
func calendarTexts(reference string) []extract.Message {
	texts := frontend.CalendarTexts()

	for i := range texts {
		texts[i].References = []string{reference}
	}

	return texts
}

// rendersForms reports whether the default template renders forms with
// go-form's functions.
func (d defaultTexts) rendersForms() bool {
	return rendersForms([]extract.Source{{Name: d.name, Text: d.template}})
}

// themeFiles are the templates of each theme, with the default theme's
// files beneath the others'.
func (a *App) themeFiles() ([]fs.FS, error) {
	if a.config.Templates == nil {
		return nil, nil
	}

	themes, err := frontend.LoadThemes(a.config.Templates)
	if err != nil {
		return nil, err
	}

	var files []fs.FS

	for _, theme := range themes.List() {
		files = append(files, theme.Files)
	}

	return files, nil
}

// replacedEverywhere reports whether every theme has one of the candidate
// files, so a module's default template is never used. Without themes the
// defaults are used.
func replacedEverywhere(themes []fs.FS, candidates []string) bool {
	if len(themes) == 0 {
		return false
	}

	for _, theme := range themes {
		replaced := false

		for _, candidate := range candidates {
			if _, err := fs.Stat(theme, candidate); err == nil {
				replaced = true

				break
			}
		}

		if !replaced {
			return false
		}
	}

	return true
}

// rendersForms reports whether a template renders forms with go-form's
// functions (form_open, form_field, form_render).
func rendersForms(sources []extract.Source) bool {
	for _, source := range sources {
		if strings.Contains(source.Text, "form_open") || strings.Contains(source.Text, "form_field") ||
			strings.Contains(source.Text, "form_render") {
			return true
		}
	}

	return false
}
