package frontend

import (
	"context"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/donseba/go-webthings/render"

	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/exp/interactions"
	"github.com/donseba/go-partial/exp/localization"
	translator "github.com/donseba/go-translator/v2"
)

// Templates renders with Go templates from files: layout.gohtml, with the
// View, wraps <name>.gohtml, with View.Content. Requests from HTMX receive only
// the content. The localizer in templates is the page language, for use with
// go-translator's `tl`.
//
// Templates in partials/ can be used in every template, for example
// {{template "partials/card.gohtml" .}}. Functions whose first parameter is a
// context.Context get the request's context; Locale and Service work there.
//
// For HTMX, a theme has two more folders. A template in regions/, such as
// regions/menu.gohtml, is a part of the layout that the layout places with
// {{slot "menu"}}: it gets the View, and goes along out of band with every
// HTMX request for the content, so a menu or a language switch follows the
// page. Of regions/menu.gohtml, whose items carry their ElementID, a
// navigation sends only the items that change (see MenuItem).
//
// regions/head.gohtml is the region of the page's tags in <head>: its
// <title>, the canonical and alternate links, {{feedLinks}}, the
// description, the tags for sharing and {{structuredData}}, the page's
// JSON-LD (see Thing). The layout places it in <head>
// with {{slot "head"}}; it needs no ID and no {{oobAttr}}. A navigation
// replaces the tags it wrote on the page left with those of the page
// arrived at, so none stays behind and none is doubled: on a whole page
// the tags sit between two empty <template> markers, and with HTMX they
// are removed and added with two out-of-band swaps (go-partial's
// exp/head). The <title> goes on its own, which HTMX sets as the
// document's title.
//
// A theme without regions/head.gohtml, also in the default theme, whose
// layout places {{slot "head"}}, gets core's head region: the title (on
// the home page the site's SEO title when there is one), the description
// (the page's, the SEO description or the tagline), robots for a preview,
// the canonical and alternate links, og:title, og:description, og:type,
// og:url, og:site_name, og:locale, og:image, twitter:card, the icons,
// {{feedLinks}}, {{apiLink}} and {{structuredData}}; a page with status
// 400 or above has no canonical, alternate or og:url. Those tags are
// webthings/head-tags.gohtml, which a theme's own regions/head.gohtml can
// call and add to:
//
//	{{template "webthings/head-tags.gohtml" .}}
//	<meta name="theme-color" content="#336699">
//
// A layout without {{slot "head"}} writes its tags itself and gets no
// head region from core.
//
// A template in fragments/, such as fragments/comments.gohtml, is a part of
// a page that HTMX can ask for on its own: a template places it with
// {{slot "comments"}}, or {{requiredSlot "comments"}}, which fails when the
// theme has no such fragment, and a request whose target is #comments gets
// only that fragment; {{deferredSlot "comments"}} places a placeholder that
// loads it after the page, {{deferredSlot "comments" "reveal"}} when it
// scrolls into view. A fragment gets View.Content, like the content
// template; its outer element has the fragment's name as ID, a region's
// also {{oobAttr}}. A fragment of search.gohtml, such as the results a
// live search asks for while the visitor types, comes with the regions
// out of band, as the content does: the search's address changes with
// every query, and the <title> of regions/head.gohtml and the language
// links follow it.
//
// A theme with a 500.gohtml shows it when a template fails, outside
// development: on its own, without the layout, with go-partial's ext/errors
// Data as its dot and the View for @model (see render.Diagnostics.ErrorPage).
// Its .Message is a safe sentence, already translated in the page's language
// with the site's translations (see CoreTexts), so {{.Message}} shows it.
//
// Every template is parsed once and kept; see Frontend.SetReload for
// development.
func Templates(files fs.FS, funcs ...template.FuncMap) Renderer {
	return newTemplates(withCoreTemplates(files), false, render.Diagnostics{}, untranslated(), funcs...)
}

// newTemplates is Templates, reading the files again for every render when
// reload is set, with diagnostics and the site's translations tr. files
// hold core's templates below the theme's (see withCoreTemplates).
func newTemplates(
	files fs.FS,
	reload bool,
	diagnostics render.Diagnostics,
	tr *translator.Translator,
	funcs ...template.FuncMap,
) templates {
	renderer := render.New(files, "layout.gohtml", templateFuncs(tr, funcs)...)
	renderer.SetReload(reload)

	t := templates{renderer: renderer, head: newHeadPlacement(files, reload)}

	if t.Has(errorPage) {
		diagnostics.ErrorPage = errorPage + ".gohtml"
	}

	renderer.SetDiagnostics(withErrorTexts(diagnostics, tr))

	return t
}

// withErrorTexts is diagnostics with the error page in the page's
// language, the theme's 500.gohtml or go-partial's built-in one: its
// .Message and .Title translated with the site's translations tr (Render,
// and Fail, give the request the page's localizer), and its language.
func withErrorTexts(diagnostics render.Diagnostics, tr *translator.Translator) render.Diagnostics {
	diagnostics.ErrorMessage = func(r *http.Request, message string) string {
		return tr.Tl(localization.FromContext(r.Context()), message)
	}

	diagnostics.ErrorTitle = func(r *http.Request, title string) string {
		return tr.Tl(localization.FromContext(r.Context()), title)
	}

	diagnostics.ErrorLanguage = func(r *http.Request) string {
		return Locale(r.Context())
	}

	return diagnostics
}

// errorPage is the template of a theme's page for a failing template.
const errorPage = "500"

// templateFuncs are the functions of the themes' templates: go-partial's
// exp/interactions attributes (submitAttrs, liveAttrs, noBoost, …, but not
// its generic names, such as poll, which modules may use), go-translator's
// of the site's translations tr, core's, then funcs, the modules' and the
// site's own, which replace earlier ones of the same name.
func templateFuncs(tr *translator.Translator, funcs []template.FuncMap) []template.FuncMap {
	return append([]template.FuncMap{interactions.AttrsFuncMap(), tr.FuncMap(), Funcs()}, funcs...)
}

// partials are the templates in a theme's partials/, which every template
// can call (go-partial's SetChildIncludes); regions and fragments are the
// theme's folders of regions and fragments (go-partial's slots.Glob).
const (
	partials  = "partials/*.gohtml"
	regions   = "regions/*.gohtml"
	fragments = "fragments/*.gohtml"
)

// headRegion is the theme's region of the page's tags in <head>,
// regions/head.gohtml: out of band, it replaces the tags it wrote before
// (see render.View.HeadRegion).
const headRegion = "head"

type templates struct {
	renderer *render.Renderer
	// head tells whether pages get core's head region (see headPlacement).
	head *headPlacement
}

// Has reports whether the theme has a template. go-partial remembers the
// answer while templates are parsed once and kept (see
// render.Renderer.FirstExisting).
func (t templates) Has(name string) bool {
	_, err := t.renderer.FirstExisting(name + ".gohtml")

	return err == nil
}

func (t templates) Render(w http.ResponseWriter, r *http.Request, name string, view View) error {
	r = r.WithContext(localization.WithLocalizer(r.Context(), localization.New(view.Locale)))

	page := render.View{
		Template:      name + ".gohtml",
		Status:        view.Status,
		Includes:      []string{partials, coreIncludes},
		Data:          view.Content,
		Shell:         view,
		RegionFiles:   regions,
		HeadRegion:    headRegion,
		FragmentFiles: fragments,
		// A live search asks for the results at a new address, which the
		// title and the language links follow.
		FragmentRegions: view.fragmentRegions,
		// Any template can name the View and the content with @model, for
		// example a partial that needs the site; see render.View.Models.
		Models: []any{view, view.Content},
	}

	// The site, the menu and the languages are made when the layout, a
	// region or a template that names the View needs them, once for all
	// of them: not for an HTMX request for the content or a fragment of a
	// theme without regions.
	if view.layout != nil && !view.ready {
		shell := partial.DotFuncOf(func(context.Context) (View, error) {
			return view.filled(), nil
		})

		page.Shell = nil
		page.ShellDot = shell
		page.Models = []any{shell.Model(), view.Content}
	}

	// A theme without a head region of its own, whose layout places one,
	// gets core's (see coreHead).
	if t.head.core() {
		page.Regions = []render.Region{{ID: headRegion, Template: coreHeadRegion, Data: page.Shell, Dot: page.ShellDot}}
	}

	// An HTMX navigation gets only the menu items that change, chosen when
	// the menu region goes out of band (see menuUpdate).
	if parts := view.layout; parts != nil {
		conn := t.renderer.Connector()

		page.RegionElementsFunc = map[string]func(*http.Request) partial.OOBElements{
			menuRegion: func(r *http.Request) partial.OOBElements {
				return parts.frontend.menuUpdate(r, conn, parts)
			},
		}
	}

	return t.renderer.Render(w, r, page)
}

// Missing is the renderer of a site without templates; it explains what to add.
func Missing() Renderer {
	return missing{}
}

type missing struct{}

func (missing) Has(string) bool {
	return true
}

func (missing) Render(w http.ResponseWriter, _ *http.Request, name string, view View) error {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	message := view.Title + "\n\nThis site has no templates yet. Add " + name + ".gohtml and layout.gohtml.\n"
	_, err := w.Write([]byte(message))

	return err
}
