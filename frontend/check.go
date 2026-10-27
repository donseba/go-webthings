package frontend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// TemplateProblem is a problem of a theme's template that CheckTemplates
// finds: one its renders fail with.
type TemplateProblem struct {
	// Themes are the themes that render the file, such as "default" and
	// "dark" for a file of the default theme that dark does not replace.
	Themes []string
	// Path is the file in the site's templates, in the folder of the theme
	// it comes from, such as "default/layout.gohtml"; the file alone for a
	// single theme at the root of the templates.
	Path string
	// Line and Column are where the problem is in the file, counted from 1;
	// Column is 0 when the parser does not say.
	Line   int
	Column int
	// Func is the function the template calls that the site does not
	// have, such as "newsletterForm"; empty for other problems.
	Func string
	// Template is the template a {{template}} names that no file defines;
	// empty for other problems.
	Template string
	// Suggestion is the function or template whose name is closest to Func
	// or Template; empty when none is close.
	Suggestion string
	// Message says what is wrong, without the position, such as
	// `function "newsletterForm" not defined`.
	Message string
	// EveryPage reports whether every page of the Themes fails with it: a
	// template that does not parse or calls a function the site does not
	// have, in layout.gohtml, regions/ or partials/, which every page
	// parses.
	EveryPage bool
}

// Position is the problem's file, line and, when known, column, such as
// "default/layout.gohtml:69:9".
func (p TemplateProblem) Position() string {
	position := p.Path + ":" + strconv.Itoa(p.Line)
	if p.Column > 0 {
		position += ":" + strconv.Itoa(p.Column)
	}

	return position
}

// Error is the position and the message, with the suggestion when there is
// one.
func (p TemplateProblem) Error() string {
	message := p.Position() + ": " + p.Message
	if p.Suggestion != "" {
		message += fmt.Sprintf("; did you mean %q?", p.Suggestion)
	}

	return message
}

// CheckTemplates parses every template of every theme, *.gohtml in any
// folder, with the functions and render stages the theme's pages render
// with, and reports every problem a render would fail with: a template
// that does not parse, a function the site does not have, such as one of
// a module that is not in app.Use, and a {{template}} that names a
// template no file of the theme defines (go-partial's
// Partial.CheckTemplates). A file of the default theme that other themes
// use is reported once, with all of them. A site with a Renderer of its
// own has nothing to check. The error is that of reading the files.
func (f *Frontend) CheckTemplates() ([]TemplateProblem, error) {
	var problems []TemplateProblem

	for _, theme := range f.themes.List() {
		renderer, ok := f.renderers[theme.Name].(templates)
		if !ok {
			continue
		}

		checked, err := renderer.renderer.CheckTemplates()
		if err != nil {
			return nil, fmt.Errorf("theme %s: %w", theme.Name, err)
		}

		for _, problem := range checked {
			dir := ""
			if problem.Layer < len(theme.dirs) {
				dir = theme.dirs[problem.Layer]
			}

			found := TemplateProblem{
				Path:       path.Join(dir, problem.File),
				Line:       problem.Line,
				Column:     problem.Column,
				Func:       problem.Func,
				Template:   problem.Template,
				Suggestion: problem.Suggestion,
				Message:    problem.Message,
				EveryPage:  problem.Template == "" && everyPage(problem.File),
			}

			problems = addProblem(problems, found, theme.Name)
		}
	}

	return problems, nil
}

// addProblem adds problem of theme to problems, or theme to the same
// problem found in another theme.
func addProblem(problems []TemplateProblem, problem TemplateProblem, theme string) []TemplateProblem {
	for i := range problems {
		if problems[i].Error() == problem.Error() {
			problems[i].Themes = append(problems[i].Themes, theme)

			return problems
		}
	}

	problem.Themes = []string{theme}

	return append(problems, problem)
}

// everyPage reports whether every page parses the theme's file: the
// layout, a region or a partial.
func everyPage(file string) bool {
	if file == "layout.gohtml" {
		return true
	}

	for _, pattern := range []string{regions, partials} {
		if matched, _ := path.Match(pattern, file); matched {
			return true
		}
	}

	return false
}

// maxMistakes is how many different template mistakes are logged; more,
// such as for asset names from content, are not.
const maxMistakes = 1000

// mistake reports a template call that cannot do what it asks and would
// otherwise show nothing, such as {{posts "nieuws" 3}} for an instance key
// the site does not have: it is logged at WARN once per message. In
// development (see SetDiagnostics) the error returned fails the render;
// otherwise it is nil.
func (f *Frontend) mistake(ctx context.Context, message string) error {
	if _, logged := f.mistakes.Load(message); !logged && f.mistakesLogged.Load() < maxMistakes {
		if _, logged := f.mistakes.LoadOrStore(message, true); !logged {
			f.mistakesLogged.Add(1)
			slog.WarnContext(ctx, "template mistake", "problem", message)
		}
	}

	if f.diagnostics.Development {
		return errors.New(message)
	}

	return nil
}

// ServiceOf returns the API of the module instance key while a page
// renders, as Service does, when it is a T, such as
// ServiceOf[*publishing.Service](ctx, "news"); a module's template
// functions use it. An instance key the site has no T of is a mistake in a
// template that would show nothing: it is logged at WARN once, with the
// keys of the instances there are, such as `posts: no publishing instance
// "nieuws" (known: blog, news)`, and in development (see SetDiagnostics)
// the render fails with it: ServiceOf panics with the error, which the
// template turns into the error of the function's call. Outside a render
// it reports false.
func ServiceOf[T any](ctx context.Context, key string) (T, bool) {
	var zero T

	current, ok := renderingFrom(ctx)
	if !ok {
		return zero, false
	}

	if service, ok := current.frontend.services[key].(T); ok {
		return service, true
	}

	if err := current.frontend.mistake(ctx, unknownInstance[T](current.frontend, key)); err != nil {
		panic(err)
	}

	return zero, false
}

// unknownInstance says that the site has no instance key of the module
// whose API is T, with the template function that asked and the keys of
// the instances there are.
func unknownInstance[T any](f *Frontend, key string) string {
	var known []string

	for name, service := range f.services {
		if _, ok := service.(T); ok {
			known = append(known, name)
		}
	}

	slices.Sort(known)

	moduleType := ""

	for _, name := range known {
		if f.types[name] != "" {
			moduleType = f.types[name]

			break
		}
	}

	if moduleType == "" {
		typ := reflect.TypeFor[T]()
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}

		moduleType = path.Base(typ.PkgPath())
	}

	list := "none"
	if len(known) > 0 {
		list = strings.Join(known, ", ")
	}

	return fmt.Sprintf("%s: no %s instance %q (known: %s)", f.callingFunc(), moduleType, key, list)
}

// callingFunc is the name of the template function that called ServiceOf:
// the name it has in the site's functions, or the name of the Go function
// that called it.
func (f *Frontend) callingFunc() string {
	names := map[uintptr]string{}

	for _, funcs := range f.funcs {
		for name, fn := range funcs {
			value := reflect.ValueOf(fn)
			if value.Kind() == reflect.Func {
				names[value.Pointer()] = name
			}
		}
	}

	pcs := make([]uintptr, 8)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(1, pcs)])
	fallback := ""

	for {
		frame, more := frames.Next()

		if name, ok := names[frame.Entry]; ok {
			return name
		}

		if fallback == "" && !strings.Contains(frame.Function, "/go-webthings/frontend.") {
			fallback = frame.Function[strings.LastIndex(frame.Function, ".")+1:]
		}

		if !more {
			break
		}
	}

	return fallback
}

// closestNames are up to limit of names, closest to name first by edit
// distance, without name itself.
func closestNames(name string, names []string, limit int) []string {
	type scored struct {
		name     string
		distance int
	}

	var candidates []scored

	for _, candidate := range names {
		if candidate == name {
			continue
		}

		candidates = append(candidates, scored{candidate, editDistance(strings.ToLower(name), strings.ToLower(candidate))})
	}

	slices.SortStableFunc(candidates, func(a, b scored) int {
		if a.distance != b.distance {
			return a.distance - b.distance
		}

		return strings.Compare(a.name, b.name)
	})

	closest := make([]string, 0, limit)
	for _, candidate := range candidates[:min(limit, len(candidates))] {
		closest = append(closest, candidate.name)
	}

	return closest
}

// editDistance is the Levenshtein distance between a and b, in bytes.
func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)

	for j := range previous {
		previous[j] = j
	}

	for i := 1; i <= len(a); i++ {
		current[0] = i

		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}

			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}

		previous, current = current, previous
	}

	return previous[len(b)]
}
