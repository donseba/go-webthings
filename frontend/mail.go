package frontend

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"reflect"
	"strings"

	"github.com/donseba/go-webthings/mail"

	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/exp/localization"
)

// Modules write e-mail with templates, so sites can change the text and
// translate it. A module declares its mail templates like its page
// templates (webthings.Module.Mail), with the text it sends by default as
// Example; a theme replaces one with mail/<key>/<name>.gohtml or
// mail/<type>/<name>.gohtml, and App.WriteTemplates writes the defaults as a
// starting point. A mail template defines its parts:
//
//	{{define "subject"}}{{tl localizer "Registration confirmed"}}: {{.Title}}{{end}}
//	{{define "text"}}
//	{{tl localizer "Hello"}} {{.Name}},
//	{{date .Start "full"}} {{time .Start}}
//	{{end}}
//	{{define "html"}}<p>{{tl localizer "Hello"}} {{.Name}}</p>{{end}}
//
// "subject" and "text" are required and not HTML-escaped; "html" is
// optional. All template functions work, in the language the mail is
// written in: the site's translations (tl), date, money, moduleURL. Templates
// in the theme's mail/partials/ can be used in every mail template.

// ErrNoTemplate is returned when neither the theme nor the module has a
// template.
var ErrNoTemplate = errors.New("template_not_found")

// SetMail tells the frontend the mail templates of the module instances, by
// key; their Example (or a generated text) is used when a theme has none.
func (f *Frontend) SetMail(templates map[string][]Template) {
	f.mail = templates
	f.mailSources = partial.Sources{}

	for key, declared := range templates {
		for _, declaration := range declared {
			f.mailSources["mail/"+key+"/"+declaration.Name+".gohtml"] = MailScaffold(declaration)
		}
	}
}

// Mail writes a module's e-mail from its template name in locale, for
// example Mail(ctx, "nl", "agenda", "confirmation", data): the subject, text
// and optional HTML of the message. The caller sets the recipients.
//
// Every part renders through go-partial as a part of the theme's pages, with
// its functions, render stages and diagnostics: the subject and text as
// plain text (go-partial's RenderText), the HTML part as HTML.
func (f *Frontend) Mail(ctx context.Context, locale, key, name string, data any) (mail.Message, error) {
	ctx, theme, err := f.executing(ctx, locale)
	if err != nil {
		return mail.Message{}, err
	}

	file, err := f.themeTemplate(theme, f.mailSources, f.candidates("mail/", key, name))
	if err != nil {
		return mail.Message{}, err
	}

	files := partial.Overlay(theme.Files, f.mailSources)

	renderer := f.parts(theme.Name)
	part := func(entry string) *partial.Partial {
		return partial.NewID(file, file).
			SetEntry(entry).
			SetFileSystem(files).
			SetIncludes(mailPartials).
			SetDot(data)
	}

	// Only the mail template itself declares an HTML part.
	hasHTML, err := renderer.HasEntry(part("html"))
	if err != nil {
		return mail.Message{}, fmt.Errorf("%s: %w", file, err)
	}

	var message mail.Message

	message.Subject, err = renderer.RenderText(ctx, part("subject"))
	if err != nil {
		return mail.Message{}, fmt.Errorf("%s: %w", file, err)
	}

	message.Subject = strings.Join(strings.Fields(message.Subject), " ")

	message.Text, err = renderer.RenderText(ctx, part("text"))
	if err != nil {
		return mail.Message{}, fmt.Errorf("%s: %w", file, err)
	}

	message.Text = strings.TrimSpace(message.Text) + "\n"

	if !hasHTML {
		return message, nil
	}

	html, err := renderer.RenderPartial(ctx, nil, part("html"))
	if err != nil {
		return mail.Message{}, fmt.Errorf("%s: %w", file, err)
	}

	message.HTML = strings.TrimSpace(string(html))

	return message, nil
}

// mailPartials are the templates in a theme's mail/partials/, which every
// mail template can call.
const mailPartials = "mail/partials/*.gohtml"

// Execute renders a template of the active theme on its own, without the
// layout, for example Execute(ctx, "partials/card", "nl", data), through
// go-partial as a part of the theme's pages. Templates in partials/ can be
// used, and every template function works in locale.
func (f *Frontend) Execute(ctx context.Context, name, locale string, data any) (string, error) {
	ctx, theme, err := f.executing(ctx, locale)
	if err != nil {
		return "", err
	}

	renderer := f.parts(theme.Name)
	file := name + ".gohtml"

	if theme.Files == nil || renderer == f.standalone {
		return "", fmt.Errorf("%w: %s", ErrNoTemplate, file)
	}

	if _, err := renderer.FirstExisting(file); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%w: %s", ErrNoTemplate, file)
		}

		return "", err
	}

	html, err := renderer.RenderPartial(ctx, nil, partial.NewID(file, file).SetIncludes(partials).SetDot(data))
	if err != nil {
		return "", err
	}

	return string(html), nil
}

// executing prepares ctx for template functions outside a page request: the
// active theme (or the one WithTheme chose) and locale.
func (f *Frontend) executing(ctx context.Context, locale string) (context.Context, Theme, error) {
	snapshot, err := f.site.Snapshot(ctx)
	if err != nil {
		return ctx, Theme{}, err
	}

	view := f.theme(ctx, snapshot)
	theme, _ := f.themes.Get(view.Name)

	ctx = localization.WithLocalizer(ctx, localization.New(locale))
	ctx = context.WithValue(ctx, renderKey{}, rendering{frontend: f, locale: locale, theme: view})

	return ctx, theme, nil
}

// executeFuncs are the template functions with ctx bound, for templates
// go-partial does not execute: the locale functions of the page renderer
// (go-partial's localization.Funcs), the core functions, then the modules'
// and site's own.
func (f *Frontend) executeFuncs(ctx context.Context, locale string) template.FuncMap {
	language := localization.Funcs(localization.New(locale))

	return partial.BindContext(ctx, append([]template.FuncMap{language, f.tr().FuncMap(), Funcs()}, f.funcs...)...)
}

// MailScaffold is a mail template's Example, or a starting point with every
// field of its data in the text.
func MailScaffold(t Template) string {
	if t.Example != "" {
		return t.Example
	}

	data := reflect.TypeOf(t.Data)

	var b strings.Builder

	fmt.Fprintf(&b, "{{/* @dot %s */}}\n", typeName(data))
	fmt.Fprintf(&b, "{{define \"subject\"}}%s{{end}}\n\n", t.Name)
	b.WriteString("{{define \"text\"}}\n")

	for data != nil && data.Kind() == reflect.Pointer {
		data = data.Elem()
	}

	if data != nil && data.Kind() == reflect.Struct {
		for _, field := range reflect.VisibleFields(data) {
			if !field.IsExported() || field.Anonymous {
				continue
			}

			switch {
			case field.Type == timeType:
				fmt.Fprintf(&b, "%s: {{date .%s \"long\"}} {{time .%s}}\n", field.Name, field.Name, field.Name)
			case isStruct(field.Type) || field.Type.Kind() == reflect.Slice || field.Type.Kind() == reflect.Map:
				fmt.Fprintf(&b, "{{/* %s (%s) */}}\n", field.Name, field.Type)
			default:
				fmt.Fprintf(&b, "%s: {{.%s}}\n", field.Name, field.Name)
			}
		}
	}

	b.WriteString("{{end}}\n")

	return b.String()
}
