package frontend

import (
	"context"
	"html/template"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"text/template/parse"

	"github.com/donseba/go-importmap/assets"
	partial "github.com/donseba/go-partial"
)

// Core's head region, for a theme without regions/head.gohtml whose layout
// places {{slot "head"}}, and the tags it writes, which a theme's own head
// region can call; see Templates.

// coreHead is core's template of the tags in <head>, which every template
// of a theme can call (see coreIncludes). go-partial also names a template
// by its file's base name, so it is not head.gohtml, which would be the
// theme's regions/head.gohtml that calls it.
const coreHead = "webthings/head-tags.gohtml"

// coreHeadRegion is core's head region, used when the theme has no
// regions/head.gohtml and its layout places {{slot "head"}}. It is not in
// coreIncludes, so it is not parsed with every template.
const coreHeadRegion = "webthings/regions/head-region.gohtml"

// coreIncludes are core's templates the content, regions and fragments of
// a theme can call, such as coreHead.
const coreIncludes = "webthings/*.gohtml"

// coreTemplates are core's templates below a theme's. go-partial reads
// them below the theme's files (see partial.Overlay); they are made once,
// as its template cache tells file systems apart by identity.
var coreTemplates = partial.Sources{
	coreHeadRegion: headRegionSource,
	coreHead:       headSource,
}

// withCoreTemplates are layers, a theme's own templates and the default
// theme's, with core's templates below them, as one file system whose
// layers go-partial's FirstExistingIn still tries one after the other.
func withCoreTemplates(layers ...fs.FS) fs.FS {
	var flat []fs.FS

	// A layer that is an overlay itself, such as templates given as one,
	// gives its own layers, as it did before core's were added.
	for _, layer := range layers {
		flat = append(flat, partial.Layers(layer)...)
	}

	return partial.Overlay(append(flat, coreTemplates)...)
}

// headRegionSource is core's head region.
const headRegionSource = `{{/* @dot github.com/donseba/go-webthings/frontend.View */}}
{{- template "` + coreHead + `" .}}
`

// headSource is webthings/head-tags.gohtml, the tags of core's head region.
const headSource = `{{/* @dot github.com/donseba/go-webthings/frontend.View */}}
{{- /*
    The tags of the page in <head>, of core's head region. A theme's own
    regions/head.gohtml adds to them with {{template "webthings/head-tags.gohtml" .}}.
    An HTMX navigation replaces them with those of the page arrived at, and
    sets the document's title from the <title>.
*/ -}}
{{- $found := lt .Status 400 -}}
{{- $seoTitle := "" -}}
{{- if and $found (eq .Path .Site.HomeURL) -}}
    {{- $seoTitle = .Site.SEOTitle -}}
{{- end -}}
{{- $description := or .Description .Site.SEODescription .Site.Tagline -}}
{{- $type := "website" -}}
{{- with .StructuredData -}}
    {{- if eq (printf "%v" (index (index . 0) "@type")) "Article" -}}
        {{- $type = "article" -}}
    {{- end -}}
{{- end -}}
{{- if $seoTitle}}
<title>{{$seoTitle}}</title>
{{- else}}
<title>{{if .Title}}{{.Title}} · {{end}}{{.Site.Name}}</title>
{{- end}}
{{- with $description}}
<meta name="description" content="{{.}}">
{{- end}}
{{- if .Preview}}
<meta name="robots" content="noindex">
{{- end}}
{{- if $found}}
<link rel="canonical" href="{{absURL .Path}}">
    {{- range .Languages}}
<link rel="alternate" hreflang="{{.Locale}}" href="{{absURL .URL}}">
    {{- end}}
    {{- /* With more than one, the default language's address is for visitors of other languages too. */}}
    {{- if gt (len .Languages) 1}}
        {{- range .Languages}}
            {{- if .Default}}
<link rel="alternate" hreflang="x-default" href="{{absURL .URL}}">
            {{- end}}
        {{- end}}
    {{- end}}
{{- end}}
<meta property="og:title" content="{{or $seoTitle .Title .Site.Name}}">
{{- with $description}}
<meta property="og:description" content="{{.}}">
{{- end}}
<meta property="og:type" content="{{$type}}">
{{- if $found}}
<meta property="og:url" content="{{absURL .Path}}">
{{- end}}
{{- with .Site.Name}}
<meta property="og:site_name" content="{{.}}">
{{- end}}
<meta property="og:locale" content="{{.Locale}}">
{{- with or .Image .Site.SocialImageURL}}
<meta property="og:image" content="{{absURL .}}">
<meta name="twitter:card" content="summary_large_image">
{{- else}}
<meta name="twitter:card" content="summary">
{{- end}}
{{- with .Site.FaviconURL}}
<link rel="icon" href="{{.}}" sizes="32x32">
{{- end}}
{{- with .Site.AppleTouchIconURL}}
<link rel="apple-touch-icon" href="{{.}}">
{{- end}}
{{/* The Atom and RSS feeds of the page, such as a news page's posts. */ -}}
{{feedLinks}}
{{/* The page in the content API, for apps, while the API is on. */ -}}
{{apiLink}}
{{/* What the page is about for search engines, schema.org's JSON-LD. */ -}}
{{structuredData}}
`

// headPlacement tells whether core's head region is used: when the theme
// has no regions/head.gohtml and its layout places {{slot "head"}}. The
// answer is kept while templates are parsed once and kept, and asked again
// for every render while they reload.
type headPlacement struct {
	files  fs.FS
	reload bool
	once   func() bool
}

func newHeadPlacement(files fs.FS, reload bool) *headPlacement {
	placement := &headPlacement{files: files, reload: reload}
	placement.once = sync.OnceValue(placement.check)

	return placement
}

// core reports whether the page gets core's head region.
func (p *headPlacement) core() bool {
	if p == nil || p.files == nil {
		return false
	}

	if p.reload {
		return p.check()
	}

	return p.once()
}

// check reports whether the theme has no head region of its own and its
// layout places one.
func (p *headPlacement) check() bool {
	if _, err := fs.Stat(p.files, "regions/"+headRegion+".gohtml"); err == nil {
		return false
	}

	layout, err := fs.ReadFile(p.files, "layout.gohtml")
	if err != nil {
		return false
	}

	return placesSlot(string(layout), headRegion)
}

// placesSlot reports whether the template source places the slot name
// with {{slot "name"}} or {{requiredSlot "name"}}, in any of its
// templates. A source that does not parse places none: its render fails
// anyway.
func placesSlot(source, name string) bool {
	tree := parse.New("layout")
	tree.Mode = parse.SkipFuncCheck

	set := map[string]*parse.Tree{}
	if _, err := tree.Parse(source, "", "", set); err != nil {
		return false
	}

	for _, defined := range set {
		if defined.Root != nil && nodePlacesSlot(defined.Root, name) {
			return true
		}
	}

	return false
}

// nodePlacesSlot reports whether node, or a node in it, calls slot or
// requiredSlot with name.
func nodePlacesSlot(node parse.Node, name string) bool {
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return false
		}

		return slices.ContainsFunc(n.Nodes, func(child parse.Node) bool {
			return nodePlacesSlot(child, name)
		})
	case *parse.ActionNode:
		return nodePlacesSlot(n.Pipe, name)
	case *parse.PipeNode:
		if n == nil {
			return false
		}

		return slices.ContainsFunc(n.Cmds, func(cmd *parse.CommandNode) bool {
			return nodePlacesSlot(cmd, name)
		})
	case *parse.CommandNode:
		return commandPlacesSlot(n, name)
	case *parse.IfNode:
		return branchPlacesSlot(&n.BranchNode, name)
	case *parse.RangeNode:
		return branchPlacesSlot(&n.BranchNode, name)
	case *parse.WithNode:
		return branchPlacesSlot(&n.BranchNode, name)
	case *parse.TemplateNode:
		return nodePlacesSlot(n.Pipe, name)
	default:
		return false
	}
}

// branchPlacesSlot is nodePlacesSlot for an if, range or with.
func branchPlacesSlot(branch *parse.BranchNode, name string) bool {
	if nodePlacesSlot(branch.Pipe, name) || nodePlacesSlot(branch.List, name) {
		return true
	}

	return branch.ElseList != nil && nodePlacesSlot(branch.ElseList, name)
}

// commandPlacesSlot is nodePlacesSlot for one command: a call of slot or
// requiredSlot with name, or one in its arguments, such as in parentheses.
func commandPlacesSlot(cmd *parse.CommandNode, name string) bool {
	if len(cmd.Args) >= 2 {
		function, isIdent := cmd.Args[0].(*parse.IdentifierNode)
		argument, isString := cmd.Args[1].(*parse.StringNode)

		if isIdent && isString && argument.Text == name &&
			(function.Ident == "slot" || function.Ident == "requiredSlot") {
			return true
		}
	}

	return slices.ContainsFunc(cmd.Args, func(arg parse.Node) bool {
		pipe, ok := arg.(*parse.PipeNode)

		return ok && nodePlacesSlot(pipe, name)
	})
}

// errorStylesheet is go-partial's stylesheet of the box that takes the
// place of a part of a page that failed, which core serves (see
// CoreAssets).
const errorStylesheet = "partial-errors.css"

// styles is {{styles "webthings.css" "site.css"}}: the stylesheet links of
// a theme's <head>, in the order they must have. First
// partial-errors.css, the box that takes the place of a part of the page
// that failed; then files, in the order given, each linked as
// {{assetStyle}} links it: a file of the theme's assets/ folder, or one
// core offers when the theme has none of that name, such as webthings.css,
// the layout of the module components, or focal-points.css, the classes of
// focalClass; last the theme's values stylesheet, as {{themeStyle}} links
// it, whose custom properties replace the fallbacks of the stylesheets
// before it. Every link has its version and Subresource Integrity, and
// none is a <style> element, so the page works under a
// Content-Security-Policy without 'unsafe-inline'. A file given twice, or
// partial-errors.css given, is linked once; a file neither the theme nor
// core has is a mistake, as for assetStyle.
func styles(ctx context.Context, files ...string) (template.HTML, error) {
	var b strings.Builder

	linked := map[string]bool{}

	for _, file := range append([]string{errorStylesheet}, files...) {
		name := strings.TrimPrefix(file, "/")
		if linked[name] {
			continue
		}

		linked[name] = true

		address, integrity, err := assetIntegrity(ctx, "styles", name)
		if err != nil {
			return "", err
		}

		tag, err := assets.StylesheetTag(address, integrity)
		if err != nil {
			return "", err
		}

		b.WriteString(string(tag))
		b.WriteString("\n")
	}

	values, err := themeStyle(ctx)
	if err != nil {
		return "", err
	}

	b.WriteString(string(values))

	//nolint:gosec // the tags are go-importmap's, with escaped attributes.
	return template.HTML(strings.TrimSuffix(b.String(), "\n")), nil
}
