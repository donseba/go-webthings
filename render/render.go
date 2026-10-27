// Package render wraps go-partial: a layout with a content slot and optional
// out-of-band regions, served as a full page or as an HTMX fragment.
package render

import (
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"

	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/connector"
	"github.com/donseba/go-partial/exp/head"
	"github.com/donseba/go-partial/exp/localization"
	"github.com/donseba/go-partial/exp/slots"
	"github.com/donseba/go-partial/exp/target"
	"github.com/donseba/go-partial/ext/debug"
	"github.com/donseba/go-partial/ext/logger"
)

// Renderer renders views in one layout. Templates are parsed once and kept,
// the layout as well as the content templates, unless SetReload asks to read
// them again for every render.
type Renderer struct {
	// root holds what every render shares: the file system, functions,
	// stages and diagnostics. Pages and parts render under it, without
	// changing it.
	root *partial.Partial
	// layout is the layout template, if the renderer has one.
	layout []string
	// failure are the stages of a partial that answers an error outside a
	// render (see FailureStages).
	failure []partial.RenderStage
}

// View is one response: a content template inside the layout.
type View struct {
	// Template is the content template path.
	Template string
	// Includes are the templates the content, regions and fragments can
	// call, as fs.Glob patterns, such as "partials/*.gohtml" (go-partial's
	// SetChildIncludes on the page); the layout has none. The error
	// templates of Diagnostics get them too.
	Includes []string
	// Files overrides the renderer's file system for the content template.
	Files fs.FS
	// Data is the content template's dot; Shell is the layout's dot.
	Data  any
	Shell any
	// Dot and ShellDot, when set, replace Data and Shell with a go-partial
	// Dot, such as one computed only when the content or the layout renders
	// (partial.DotFuncOf), so a request for a fragment or region alone,
	// which leaves them out, costs nothing of theirs. A Dot given to several
	// parts of the page, such as the layout and its regions, is computed
	// once per response. An error fails the render.
	Dot      partial.Dot
	ShellDot partial.Dot
	// Models are the values templates can name with @model, matched by
	// type: the layout, the content and every partial, as go-partial's
	// SetModel does. A partial.ModelFuncOf is computed only when a template
	// calls it, once per response.
	Models []any
	// ContentID is the element ID HTMX swaps; "content" when empty.
	ContentID string
	// Status is the response status, also of a response for a region or a
	// fragment alone; zero is 200 OK. An error page keeps its own.
	Status int
	// Regions are layout parts updated alongside content, such as a header.
	// An HTMX response sends them out of band in this order, then those of
	// RegionFiles in the order of their file names, the same on every
	// render, so its bytes and ETag are too.
	Regions []Region
	// Fragments are parts of the page HTMX can ask for on their own by ID;
	// see Fragment.
	Fragments []Fragment
	// FragmentRegions sends the regions out of band with a fragment asked
	// for on its own, as with the content, rather than the fragment alone:
	// for a page whose fragments come from a new address, such as the
	// results of a live search that replaces the address while the visitor
	// types, so the head region's title and links and a language switch
	// follow it.
	FragmentRegions bool
	// RegionFiles and FragmentFiles add a region, with the layout's dot, or
	// a fragment, with the content's dot, for every template the fs.Glob
	// pattern finds in the renderer's file system, such as
	// "regions/*.gohtml": named after the file without its extension, such
	// as "menu" for regions/menu.gohtml (go-partial's slots.Glob). With the
	// template cache the files are found once; without (see SetReload), at
	// every render, so new files show.
	RegionFiles   string
	FragmentFiles string
	// RegionElements, by region name, are the IDs of the elements of a
	// region an HTMX request gets out of band instead of the whole region,
	// such as the menu items that change with a navigation (go-partial's
	// SetOOBElements): IDs that are not in the region's output are left
	// out, and when none of them is, the whole region goes. A full page is
	// not affected.
	RegionElements map[string][]string
	// RegionElementsFunc, by region name, chooses those IDs per request,
	// only when the region actually goes out of band (go-partial's
	// SetOOBElementsFunc), and names the request headers the choice
	// depends on, which the response's Vary header then lists. It replaces
	// RegionElements of the same region.
	RegionElementsFunc map[string]func(*http.Request) partial.OOBElements
	// HeadRegion names the region, of Regions or RegionFiles, that the
	// layout places in <head>, such as "head" for regions/head.gohtml
	// with the canonical, alternate and feed links of the page (go-partial's
	// exp/head): on a full page its elements sit between two empty
	// <template> markers, and out of band they replace the elements
	// between those markers, so the head keeps no element of the page
	// left. Its <title> goes on its own, which htmx sets as the document's
	// title.
	HeadRegion string
}

// Region is an independently rendered part of the layout.
type Region struct {
	ID       string
	Template string
	Files    fs.FS
	Data     any
	// Dot, when set, replaces Data, such as a dot computed only when the
	// region renders; see View.Dot.
	Dot partial.Dot
}

// New creates a renderer for a layout template using the HTMX 4 protocol.
// Functions whose first parameter is a context.Context receive the context of
// the render, as go-partial binds them; templates call them without it. A
// later map replaces the functions of earlier ones, also a function without
// a context replacing one with.
//
// Names of go-partial's own helpers and of the stages the renderer installs
// (see Reserved) cannot be replaced: go-partial ignores such functions, or
// its stages replace them, with a warning in the log.
//
// A renderer without a layout ("") only renders parts on their own; see
// RenderPartial.
func New(files fs.FS, layout string, funcs ...template.FuncMap) *Renderer {
	var templates []string
	if layout != "" {
		templates = append(templates, layout)
	}

	renderer := &Renderer{layout: templates}
	renderer.root = partial.NewID("renderer").
		SetFileSystem(files).
		SetConnector(connector.NewHTMX4(nil)).
		// go-partial's warnings, such as a function it ignores, go to the
		// log; errors are returned by Render instead (see SetDiagnostics).
		SetEvents(logger.Sink(nil, logger.WithLevels(partial.EventWarn))).
		UseTemplateCache(true).
		// A failing part fails the whole response: visitors never see a
		// half page, and the caller answers the error.
		SetFailurePolicy(partial.FailResponse).
		Use(stages()...).
		// {{debug .}} shows a value in development only (see
		// SetDiagnostics); elsewhere it shows nothing, so templates keep
		// working. The site's own debug function replaces it.
		SetFunc(debug.Disabled()).
		SetFunc(funcs...)

	return renderer
}

// stages are the render stages every renderer installs.
func stages() []partial.RenderStage {
	return []partial.RenderStage{localization.Stage(), slots.Stage(), target.Stage()}
}

// Use adds render stages to every render, such as exp/flash's stage:
//
//	renderer.Use(flash.Stage())
//
// Templates can call the functions the stages give them without a FuncMap.
// Call it before the first render.
func (r *Renderer) Use(stages ...partial.RenderStage) {
	r.root.Use(stages...)
}

// Funcs adds template functions to every render, such as go-partial's
// exp/interactions functions, as New's funcs do. Call it before the first
// render.
func (r *Renderer) Funcs(funcs ...template.FuncMap) {
	r.root.SetFunc(funcs...)
}

// Connector is the renderer's go-partial connector (HTMX 4), for handlers
// that inspect a request or answer one with connector.IsRequest,
// connector.CurrentURL or a connector.ResponseBuilder.
func (r *Renderer) Connector() connector.Connector {
	return r.root.Connector()
}

// Root is the partial every render of the renderer has as parent, with its
// file system, functions, stages, connector, template cache and
// diagnostics. A part that reaches the browser another way renders under
// it, such as a part of a page sent as a server-sent event with go-partial's
// exp/sse:
//
//	events := sse.NewWriter(w).SetParent(renderer.Root())
//	err := events.PatchPartial(r.Context(), r, "#viewers", partial.NewID("viewers", "viewers.gohtml").SetDot(view))
//
// Render parts under it with SetParent, which leaves it as it is; configure
// the renderer with its own methods, before the first render.
func (r *Renderer) Root() *partial.Partial {
	return r.root
}

// SetReload makes the renderer read and parse its templates on every render
// when reload is true, so edits to template files show without a restart.
// Every partial rendered under the renderer follows it. It is meant for
// development; call it before the first render.
func (r *Renderer) SetReload(reload bool) {
	r.root.UseTemplateCache(!reload)
}

// FirstExisting returns the first of candidates that is a template in the
// renderer's file system, such as a module screen of the theme before the
// module's default; see go-partial's Partial.FirstExisting. Without the
// template cache (see SetReload) the file system is asked every time;
// with it, the answer for each candidate is remembered, also when it is
// missing. Without a match the error wraps fs.ErrNotExist.
func (r *Renderer) FirstExisting(candidates ...string) (string, error) {
	return r.root.FirstExisting(candidates...)
}

// FirstExistingIn returns the first of candidates found in layers, trying
// every candidate in a layer before the next one, such as a theme's
// templates before a module's defaults; see go-partial's
// Partial.FirstExistingIn. Without layers, those of the renderer's file
// system are used when it is a go-partial Overlay, such as a theme over the
// default theme. Answers are remembered as FirstExisting remembers them.
func (r *Renderer) FirstExistingIn(layers []fs.FS, candidates ...string) (string, error) {
	return r.root.FirstExistingIn(layers, candidates...)
}

// CheckTemplates parses every *.gohtml file of the renderer's file system
// with the functions and stages its renders have, and reports every problem
// a render would fail with: a file that does not parse, a function the
// renderer does not have, a {{template}} no file defines (go-partial's
// Partial.CheckTemplates). Of an Overlay, each problem names the layer
// that serves the file. The error is that of reading the files.
func (r *Renderer) CheckTemplates() ([]partial.TemplateProblem, error) {
	return r.root.CheckTemplates()
}

// Render writes the view. When a template fails, in the layout or in the
// content (a parse or execution error, an unknown function, a missing
// partial, a failing component), nothing is written and the error is
// returned, so the caller answers it without showing internals to visitors.
// In development (see SetDiagnostics) go-partial's detailed error page is
// written instead, with status 500 and never cached, also for an HTMX
// request, and the error returned wraps ErrShown; so does the error after
// the error templates of Diagnostics answered the request.
//
// An HTMX request receives only the content, or the region or fragment it
// targets. A request targeting another element, or a fragment this page
// cannot answer for (see Fragment), is a navigation: go-partial's target
// fallback answers it with the content, with HX-Retarget and HX-Reswap
// headers that swap it for the page's content element, and for a GET
// HX-Push-Url, so the address follows.
func (r *Renderer) Render(w http.ResponseWriter, req *http.Request, view View) error {
	root, content := r.page(view)
	root.SetTargetFallback(partial.TargetFallback{ID: content.PartialID(), PushURL: true})

	for _, region := range view.Regions {
		child := r.part(region.ID, region.Template, region.Files, region.Data, region.Dot)
		setElements(child, &view, region.ID)
		setHead(child, &view, region.ID)
		slots.Set(root, region.ID, child, slots.OOB())
	}

	if err := globSlots(root, view.RegionFiles, view.Shell, view.ShellDot, &view, slots.OOB()); err != nil {
		return err
	}

	fragmentOptions := []slots.Option{slots.CheckContracts()}
	if !view.FragmentRegions {
		fragmentOptions = append(fragmentOptions, slots.OmitOOB())
	}

	if err := globSlots(root, view.FragmentFiles, view.Data, view.Dot, nil, fragmentOptions...); err != nil {
		return err
	}

	// A fragment this page can render (see Fragment) is answered on its own,
	// without the layout's regions unless FragmentRegions; any other is left
	// to the fallback.
	for _, fragment := range view.Fragments {
		child := r.part(fragment.ID, fragment.Template, view.Files, fragment.Data, fragment.Dot)
		slots.Set(root, fragment.ID, child, fragment.options(view.Files, !view.FragmentRegions)...)
	}

	// The content and every other part inherit the status.
	if view.Status != 0 {
		root.SetStatus(view.Status)
	}

	err := partial.Write(req.Context(), w, req, root)
	if err == nil {
		return nil
	}

	// An error stage, such as the development error page, answered the
	// request: nobody writes another response.
	var written *partial.FailureResponseError
	if errors.As(err, &written) {
		return fmt.Errorf("%w: %w", ErrShown, err)
	}

	return err
}

// page is the view's layout with its content and models.
func (r *Renderer) page(view View) (root, content *partial.Partial) {
	id := view.ContentID
	if id == "" {
		id = "content"
	}

	content = r.part(id, view.Template, view.Files, view.Data, view.Dot)
	root = setDot(partial.NewID("shell", r.layout...).SetParent(r.root), view.Shell, view.ShellDot).
		// The content, regions and fragments get the view's includes; the
		// layout's parse stays without them.
		SetChildIncludes(view.Includes...).
		SetContent(content).
		SetModel(view.Models...)

	return root, content
}

// part is the content, a region or a fragment of the view's page: its
// template, from files or, without, the renderer's file system, and its
// dot. go-partial's template cache keeps its parse, shared by every render.
func (r *Renderer) part(id, file string, files fs.FS, data any, dot partial.Dot) *partial.Partial {
	p := partial.NewID(id, file)
	if files != nil {
		p.SetFileSystem(files)
	}

	return setDot(p, data, dot)
}

// globSlots sets a slot on root for every template pattern finds, with
// data or dot, the elements of regions of view by name and its head
// region, when view is set (see View.RegionElements and View.HeadRegion),
// and opts; see View.RegionFiles.
func globSlots(
	root *partial.Partial,
	pattern string,
	data any,
	dot partial.Dot,
	view *View,
	opts ...slots.Option,
) error {
	if pattern == "" {
		return nil
	}

	setup := slots.Setup(func(name string, child *partial.Partial) {
		setDot(child, data, dot)

		if view != nil {
			setElements(child, view, name)
			setHead(child, view, name)
		}
	})

	return slots.Glob(root, pattern, append(opts, setup)...)
}

// setElements makes the region name of view go out of band as some of its
// elements only, when the view names them; see View.RegionElements and
// View.RegionElementsFunc.
func setElements(region *partial.Partial, view *View, name string) {
	if choose, ok := view.RegionElementsFunc[name]; ok && choose != nil {
		region.SetOOBElementsFunc(choose)

		return
	}

	if ids := view.RegionElements[name]; len(ids) > 0 {
		region.SetOOBElements(ids...)
	}
}

// setHead makes the region name of view the one in <head>, when the view
// names it; see View.HeadRegion.
func setHead(region *partial.Partial, view *View, name string) {
	if view.HeadRegion != "" && name == view.HeadRegion {
		head.Region(region)
	}
}

// setDot sets the dot of p: dot, when it is set, otherwise data.
func setDot(p *partial.Partial, data any, dot partial.Dot) *partial.Partial {
	if !dot.IsZero() {
		return p.SetDotSource(dot)
	}

	return p.SetDot(data)
}

// Reserved reports whether name is one the templates' own helpers use:
// go-partial's (content, url, request, …, or a name starting with "_") and
// those of the stages every renderer installs (locale, localizer, slot,
// hasSlot, requiredSlot, deferredSlot, targetHeader, targetValue and
// targetIs). Functions with these names would never be called.
func Reserved(name string) bool {
	return partial.IsReservedFuncName(name, stages()...)
}

// ErrReserved is returned for a template function named like a go-partial
// helper; see Reserved.
var ErrReserved = errors.New("template function name is a go-partial helper")
