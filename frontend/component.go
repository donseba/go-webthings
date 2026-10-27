package frontend

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"

	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/exp/localization"
)

// Components are parts of a page that any template can include, such as a
// comment thread, a poll or a newsletter signup. A module declares them like
// its page templates (webthings.Module.Components), with its default markup
// as Example, and offers a template function that renders each one with its
// data, for example {{commentThread "remarks" .ID}} or {{pollBox "survey"
// "colour"}}. Core has one: {{shareBar "" .Title}}.
//
// A theme replaces a component with <key>/<name>.gohtml, then
// <type>/<name>.gohtml, like a module screen; core components with
// site/<name>.gohtml. Without one, the default renders. App.WriteTemplates
// writes the defaults as a starting point to copy and change.
//
// The defaults are plain, accessible HTML with stable class names in BEM
// style (wt-comments, wt-comment__body, wt-poll__result) and texts through
// tl, so the site's translations apply. Their layout comes from one optional
// stylesheet, {{assetStyle "webthings.css"}}: it only arranges forms, lists and
// poll bars and takes its colours and spacing from CSS custom properties
// (--wt-accent, --wt-radius, --wt-gap, …) a theme can set. A theme with its
// own assets/webthings.css serves that file instead.
//
// Components are rendered through go-partial as parts of the theme's pages:
// with every template function and render stage, in the page's language,
// with the theme's partials/ and the page for @model; a form a component
// builds with VisitorForm brings visitors back to the page.

//go:embed assets/webthings.css assets/focal-points.css
var coreAssets embed.FS

// CoreComponents are the components of core, rendered by its template
// functions and replaced by a theme with site/<name>.gohtml.
func CoreComponents() []Template {
	return []Template{
		{Name: "share", Data: ShareBar{}, Example: shareComponent},
	}
}

// ShareBar is the data of the site/share component.
type ShareBar struct {
	// URL is the absolute address of the shared page, Title its title.
	URL   string
	Title string
	Links []ShareLink
}

// SetComponents tells the frontend the components of the module instances,
// by key; their Example is used when a theme has none.
func (f *Frontend) SetComponents(components map[string][]Template) {
	f.components = components
	f.componentSources = componentSources(components)
}

// componentSources are the default markup of the components, at the path a
// theme replaces them with: <key>/<name>.gohtml, and site/<name>.gohtml for
// core's. go-partial reads them below the theme's own files (see
// partial.Overlay); they are made once, as its template cache tells file
// systems apart by identity.
func componentSources(components map[string][]Template) partial.Sources {
	sources := partial.Sources{}

	for key, declared := range components {
		for _, component := range declared {
			sources[key+"/"+component.Name+".gohtml"] = Scaffold(component)
		}
	}

	for _, component := range CoreComponents() {
		sources["site/"+component.Name+".gohtml"] = Scaffold(component)
	}

	return sources
}

// Component renders the component name of the module instance key with
// data, for the template functions of modules, for example
// Component(ctx, "remarks", "thread", thread). Key "site" is a component of
// core. The theme's <key>/<name>.gohtml or <type>/<name>.gohtml is used when
// it has one, otherwise the module's default.
func Component(ctx context.Context, key, name string, data any) (template.HTML, error) {
	current, ok := renderingFrom(ctx)
	if !ok {
		return "", fmt.Errorf("component %s/%s: not rendering a page", key, name)
	}

	return current.frontend.component(ctx, current, key, name, data)
}

// component renders a component through go-partial, as a part of the
// theme's pages: with every template function and render stage, the theme's
// partials/, and the page and its content for @model.
func (f *Frontend) component(ctx context.Context, current rendering, key, name string, data any) (template.HTML, error) {
	theme, _ := f.themes.Get(current.theme.Name)

	file, err := f.themeTemplate(theme, f.componentSources, f.candidates("", key, name))
	if err != nil {
		return "", fmt.Errorf("component: %w", err)
	}

	renderer := f.parts(theme.Name)
	component := partial.NewID(file, file).
		SetFileSystem(partial.Overlay(theme.Files, f.componentSources)).
		SetIncludes(partials).
		SetDot(data)

	// In a template of the theme's renderer, the component is a child of the
	// partial that calls it: it has the page's functions, stages, language
	// and models, and a failing component fails the page.
	if runtime, ok := partial.RuntimeFrom(ctx); ok && renderer != f.standalone {
		return runtime.RenderPartial(component)
	}

	// Under a Renderer of the site's own, or a template go-partial does not
	// execute, the component renders on its own, in the page's language,
	// which the site's Renderer does not give go-partial's localization
	// stage.
	ctx = localization.WithLocalizer(ctx, localization.New(current.locale))

	view := current.view.filled()

	return renderer.RenderPartial(ctx, current.request, component, view, view.Content)
}

// candidates are the paths of a module's template name in a theme, below
// folder: <key>/<name>.gohtml, then <type>/<name>.gohtml.
func (f *Frontend) candidates(folder, key, name string) []string {
	candidates := []string{folder + key + "/" + name + ".gohtml"}
	if moduleType, ok := f.types[key]; ok && moduleType != key {
		candidates = append(candidates, folder+moduleType+"/"+name+".gohtml")
	}

	return candidates
}

// themeTemplate finds a module's template: the first of candidates the
// theme has, else the module's default in sources (go-partial's
// FirstExistingIn, which its template cache remembers). As for a module
// screen (see Frontend.Template), every candidate is tried in the theme's
// own folder before the default theme's: a theme's <type>/<name>.gohtml
// replaces the default theme's <key>/<name>.gohtml.
func (f *Frontend) themeTemplate(theme Theme, sources partial.Sources, candidates []string) (string, error) {
	layers := []fs.FS{sources}

	renderer := f.parts(theme.Name)
	if renderer != f.standalone {
		layers = append(theme.templateLayers(), sources)
	}

	file, err := renderer.FirstExistingIn(layers, candidates...)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%w: %s", ErrNoTemplate, candidates[0])
	}

	return file, err
}

// shareBar is {{shareBar "" .Title}}: the share links of a page rendered
// with the site/share component.
func shareBar(ctx context.Context, page, title string) (template.HTML, error) {
	links := share(ctx, page, title)

	bar := ShareBar{Title: title, Links: links}

	if current, ok := renderingFrom(ctx); ok {
		path := current.path
		if path == "" && current.request != nil {
			path = current.request.URL.Path
		}

		bar.URL = absolute(page, current.frontend.baseURL, current.request, path)
	}

	return Component(ctx, "site", "share", bar)
}

// shareComponent is the default site/share component.
const shareComponent = `{{/* @dot github.com/donseba/go-webthings/frontend.ShareBar */}}
{{if .Links}}
<nav class="wt-share" aria-label="{{tl localizer "Share"}}">
    <span class="wt-share__label">{{tl localizer "Share"}}</span>
    <ul class="wt-share__list">
        {{range .Links}}
            <li><a class="wt-share__link wt-share__link--{{.Network}}" href="{{.URL}}" target="_blank" rel="noopener noreferrer">{{.Label}}</a></li>
        {{end}}
    </ul>
</nav>
{{end}}
`
