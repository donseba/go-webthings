package webthings

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"reflect"
	"slices"
	"strings"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/internal/bundled"
)

// checkTemplates checks the templates of every theme when the app starts
// (see frontend.Frontend.CheckTemplates), so a template that would fail
// its renders is known before the first request: every problem is logged
// at WARN, with the theme's folder in its position, such as
// templates/default/layout.gohtml:69:9, and a hint, such as the module
// a function comes from. In development Start fails with all of them, so
// they show at once. Otherwise Start fails only for a problem every page
// of the active theme would fail with, which would answer every visitor
// with an error: a template of its layout, regions or partials that does
// not parse or calls a function the site does not have, such as one of a
// module removed from app.Use.
func (a *App) checkTemplates(ctx context.Context) error {
	problems, err := a.Frontend.CheckTemplates()
	if err != nil {
		return fmt.Errorf("checking templates: %w", err)
	}

	if len(problems) == 0 {
		return nil
	}

	active := a.activeTheme(ctx)
	dir := templatesDir(a.config.Templates)

	var fatal []string

	for _, problem := range problems {
		message := templateProblem(dir, problem)

		slog.Warn("template problem", "themes", strings.Join(problem.Themes, ", "), "problem", message)

		if a.config.Development || (problem.EveryPage && slices.Contains(problem.Themes, active)) {
			fatal = append(fatal, message)
		}
	}

	if len(fatal) == 0 {
		return nil
	}

	reason := "in development every problem stops the start"
	if !a.config.Development {
		reason = "every page of the active theme " + active + " would fail"
	}

	return fmt.Errorf("%w (%s):\n  %s", ErrTemplates, reason, strings.Join(fatal, "\n  "))
}

// ErrTemplates is the error Start returns for templates that fail their
// renders: in development for every problem of every theme, otherwise for
// a template of the active theme's layout, regions or partials, which
// every page parses, that does not parse or calls a function the site does
// not have; see Config.Development.
var ErrTemplates = errors.New("templates have problems")

// activeTheme is the name of the theme the site shows; "" when the
// settings cannot be read.
func (a *App) activeTheme(ctx context.Context) string {
	settings, err := a.Site.Settings(ctx)
	if err != nil {
		return ""
	}

	theme, ok := a.Frontend.Themes().Get(settings.Theme)
	if !ok {
		return ""
	}

	return theme.Name
}

// templateProblem is the problem's position in the site's templates, in
// dir, and its message, with where an unknown function of a bundled module
// comes from, or else the name it is closest to.
func templateProblem(dir string, problem frontend.TemplateProblem) string {
	position := problem.Position()
	if dir != "" {
		position = path.Join(dir, position)
	}

	message := position + ": " + problem.Message

	if module := bundled.ModuleOf(problem.Func); module != "" {
		return message + ": it comes from the " + module + " module; add it to app.Use or remove the call"
	}

	if problem.Suggestion != "" {
		message += fmt.Sprintf("; did you mean %q?", problem.Suggestion)
	}

	return message
}

// templatesDir is the folder of templates made with os.DirFS, such as
// "templates", for positions a developer can open; "" for any other file
// system, such as an embedded one.
func templatesDir(files fs.FS) string {
	if files == nil {
		return ""
	}

	value := reflect.ValueOf(files)
	if value.Kind() != reflect.String || value.Type().String() != "os.dirFS" {
		return ""
	}

	return value.String()
}
