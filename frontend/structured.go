package frontend

import (
	"cmp"
	"context"
	"html/template"
	"strings"
	"time"

	"github.com/donseba/go-webthings/pages"

	partialerrors "github.com/donseba/go-partial/ext/errors"
)

// Structured data tells search engines what a page is about, as schema.org
// items in JSON-LD. A theme writes it with {{structuredData}} in
// regions/head.gohtml, one <script type="application/ld+json"> holding an
// @graph of:
//
//	WebSite        the site in the page's language, with a SearchAction when
//	               the theme has search.gohtml
//	Organization   the organisation behind the site (Site settings): its
//	               name, logo, e-mail, phone, address and social profiles
//	BreadcrumbList the page's place in the page tree, from the home page
//	WebPage        the page itself, SearchResultsPage on the search page
//
// and the items of the module whose content the page shows, such as a
// news post's Article, an agenda's Event or the ItemList of a list page
// (Content.StructuredData), the first of which is the page's main entity. Other modules add to them with
// a StructuredDataFunc (webthings.Parts.StructuredData), such as the offers
// of an event visitors register for.
//
// Every address in it is absolute. The WebSite and the WebPage name the
// page's language (inLanguage), as modules' items do where schema.org has
// it. The WebSite and the Organization need the site's name, a trail a
// page below the home page; pages that are not found or fail have no
// structured data, and a page without any item no script.

// Thing is one schema.org item of a page's structured data, such as an
// Article: its "@type" and properties, as encoding/json writes them: text,
// numbers, booleans, time.Time, nested Things and lists. Empty values, such
// as "", nil, a zero time.Time and empty lists, are left out, so a module
// can name every property without checking for content first. Addresses
// in the properties that hold them (see urlProperties) may be paths, such
// as a post's URL or an image of the media library: they are made
// absolute. Dates are best given as text, in the content's own timezone,
// such as "2026-10-07" or "2026-10-07T19:30:00+02:00".
type Thing map[string]any

// StructuredDataFunc adds to the structured data of a page, or changes it,
// for a module that adds to the pages of others, such as a registrations
// module that gives an event its offers. items are the page's items from
// its module (View.StructuredData, see Content.StructuredData) and those
// earlier functions returned; it returns them with its own, and gets the
// page shown, View.Content and View.Module telling what it is.
type StructuredDataFunc func(ctx context.Context, view View, items []Thing) []Thing

// OrganizationID is the "@id" of the site's Organization item, for items
// that name it, such as the author or publisher of an article:
// Thing{"@id": frontend.OrganizationID}. Like other addresses, it is made
// absolute.
const OrganizationID = "/#organization"

// ItemList is the structured data of a page that lists items with pages of
// their own, such as a news list or an agenda (Content.StructuredData): an
// ItemList named name of their addresses, in the order the page shows
// them. Items without an address are left out; without any, ItemList is
// nil, which adds nothing.
func ItemList(name string, urls ...string) Thing {
	elements := make([]Thing, 0, len(urls))

	for _, url := range urls {
		if url == "" {
			continue
		}

		elements = append(elements, Thing{"@type": "ListItem", "position": len(elements) + 1, "url": url})
	}

	if len(elements) == 0 {
		return nil
	}

	return Thing{
		"@type":           "ItemList",
		"name":            name,
		"numberOfItems":   len(elements),
		"itemListElement": elements,
	}
}

// urlProperties are the properties whose values are addresses, made
// absolute in the items of modules.
var urlProperties = map[string]bool{
	"@id":          true,
	"url":          true,
	"image":        true,
	"logo":         true,
	"item":         true,
	"sameAs":       true,
	"contentUrl":   true,
	"thumbnailUrl": true,
	"urlTemplate":  true,
}

// ldScript writes structured data: html/template writes the value as JSON
// in a script of type application/ld+json, with <, > and & escaped, so no
// text in it can close the script.
var ldScript = template.Must(template.New("ld").Parse(`<script type="application/ld+json">{{.}}</script>`))

// SetStructuredData sets the functions that add to the structured data of
// every page, in the order of the modules (see StructuredDataFunc).
func (f *Frontend) SetStructuredData(funcs []StructuredDataFunc) {
	f.structured = funcs
}

// SocialImageURL is the image shown when a page is shared, as View.Image
// has it: a 1200 × 630 JPEG of the image ref, an asset ID or a media URL,
// cut around its focal point (see Site.SocialImageURL). Modules give it
// as the image of their structured data, such as an article's. Other
// files and addresses keep their own; "" gives "".
func (f *Frontend) SocialImageURL(ctx context.Context, ref string) string {
	return f.variantURL(ctx, ref, socialImage)
}

// structuredData is {{structuredData}}: the page's structured data as one
// <script type="application/ld+json">, or nothing (see Thing). A theme's
// error page (500.gohtml) gets nothing, also when it answers a page whose
// template failed: what the page is about is not what it shows.
func structuredData(ctx context.Context) (template.HTML, error) {
	if _, failed := partialerrors.FromContext(ctx); failed {
		return "", nil
	}

	current, ok := renderingFrom(ctx)
	if !ok || current.request == nil {
		return "", nil
	}

	graph := current.frontend.structuredGraph(ctx, current)
	if len(graph) == 0 {
		return "", nil
	}

	var b strings.Builder

	err := ldScript.Execute(&b, Thing{"@context": "https://schema.org", "@graph": graph})
	if err != nil {
		return "", err
	}

	//nolint:gosec // html/template escaped the JSON for the script.
	return template.HTML(b.String()), nil
}

// ldPage is what the structured data of one page is made from.
type ldPage struct {
	frontend *Frontend
	view     View
	parts    *layoutParts
	// abs makes an address absolute; canonical is the page's own.
	abs       func(string) string
	canonical string
}

// structuredGraph is the @graph of the page being rendered: the site's
// items, the page's and its module's, without empty values.
func (f *Frontend) structuredGraph(ctx context.Context, current rendering) []Thing {
	view := current.view
	if view.layout == nil || view.Status >= 400 {
		return nil
	}

	page := ldPage{
		frontend: f,
		view:     view,
		parts:    view.layout,
		abs: func(link string) string {
			// An empty address is none, not the page itself.
			if link == "" {
				return ""
			}

			return absolute(link, f.baseURL, current.request, current.path)
		},
	}
	page.canonical = page.abs(view.Path)

	organization := page.organization(ctx)
	website := page.website(organization)
	breadcrumb := page.breadcrumb()
	items := page.moduleItems(ctx)

	graph := []Thing{website, organization, breadcrumb, page.webPage(website, breadcrumb, items)}
	graph = append(graph, items...)

	out := make([]Thing, 0, len(graph))

	for _, item := range graph {
		if item = compact(item); item != nil {
			out = append(out, item)
		}
	}

	return out
}

// reference is {"@id": id} of item, or nil without item.
func reference(item Thing) Thing {
	if item == nil {
		return nil
	}

	return Thing{"@id": item["@id"]}
}

// organization is the organisation behind the site, or nil without a name.
func (p ldPage) organization(ctx context.Context) Thing {
	settings := p.parts.settings
	text := settings.Text(p.parts.locale)

	name := cmp.Or(settings.Organisation, text.Title)
	if name == "" {
		return nil
	}

	sameAs := make([]string, 0, len(settings.Profiles))

	for _, profile := range settings.Profiles {
		if profile.URL != "" {
			sameAs = append(sameAs, profile.URL)
		}
	}

	return Thing{
		"@type":     "Organization",
		"@id":       p.abs(OrganizationID),
		"name":      name,
		"url":       p.abs(homeURL(p.parts.locale)),
		"logo":      p.abs(p.frontend.logoURL(ctx, settings.LogoMediaID)),
		"email":     settings.Email,
		"telephone": settings.Phone,
		"address":   settings.Address,
		"sameAs":    sameAs,
	}
}

// website is the site in the page's language, with a SearchAction when
// the theme has a search page; nil without a name.
func (p ldPage) website(organization Thing) Thing {
	locale := p.parts.locale
	text := p.parts.settings.Text(locale)

	if text.Title == "" {
		return nil
	}

	home := p.abs(homeURL(locale))
	website := Thing{
		"@type":       "WebSite",
		"@id":         home + "#website",
		"url":         home,
		"name":        text.Title,
		"description": text.SEODescription,
		"inLanguage":  locale,
		"publisher":   reference(organization),
	}

	if p.frontend.renderer(p.view.Theme.Name).Has("search") {
		website["potentialAction"] = Thing{
			"@type": "SearchAction",
			"target": Thing{
				"@type":       "EntryPoint",
				"urlTemplate": p.abs(link(locale, SearchPath, "")) + "?q={search_term_string}",
			},
			"query-input": "required name=search_term_string",
		}
	}

	return website
}

// breadcrumb is the page's trail through the page tree, from the home page
// to the page and, below a module's page, the module's screen; nil for the
// home page and pages outside the tree.
func (p ldPage) breadcrumb() Thing {
	locale := p.parts.locale
	table := p.parts.table

	route, _, ok := table.Lookup(locale, p.parts.current)
	if p.parts.pageID == "" || !ok || route.PageID != p.parts.pageID {
		return nil
	}

	trail := []pages.Route{route}

	for step := route; step.ParentID != "" && len(trail) <= maxDepth; {
		path, ok := table.Alternates(step.ParentID)[locale]
		if !ok {
			return nil
		}

		step, _, ok = table.Lookup(locale, path)
		if !ok {
			return nil
		}

		trail = append([]pages.Route{step}, trail...)
	}

	elements := make([]Thing, 0, len(trail)+1)

	for _, step := range trail {
		elements = append(elements, Thing{
			"@type": "ListItem",
			"name":  step.Title,
			"item":  p.abs(link(locale, step.Path, "")),
		})
	}

	// A module's screen below its page, such as a post, ends the trail.
	page := strings.TrimSuffix(p.abs(link(locale, route.Path, "")), "/")
	if strings.TrimSuffix(p.canonical, "/") != page && p.view.Title != "" {
		elements = append(elements, Thing{"@type": "ListItem", "name": p.view.Title, "item": p.canonical})
	}

	if len(elements) < 2 {
		return nil
	}

	for i := range elements {
		elements[i]["position"] = i + 1
	}

	return Thing{
		"@type":           "BreadcrumbList",
		"@id":             p.canonical + "#breadcrumb",
		"itemListElement": elements,
	}
}

// maxDepth bounds the walk up the page tree.
const maxDepth = 64

// webPage is the page itself, linked to the site and its trail.
func (p ldPage) webPage(website, breadcrumb Thing, items []Thing) Thing {
	kind := "WebPage"
	if _, ok := p.view.Content.(SearchView); ok {
		kind = "SearchResultsPage"
	}

	page := Thing{
		"@type":       kind,
		"@id":         p.canonical,
		"url":         p.canonical,
		"name":        cmp.Or(p.view.Title, p.parts.settings.Text(p.parts.locale).Title),
		"description": p.view.Description,
		"inLanguage":  p.parts.locale,
		"isPartOf":    reference(website),
		"breadcrumb":  reference(breadcrumb),
		"image":       p.abs(p.view.Image),
	}

	if len(items) > 0 && items[0]["@id"] != nil {
		page["mainEntity"] = reference(items[0])
	}

	return page
}

// moduleItems are the items of the page's module and those the
// StructuredDataFuncs add, with absolute addresses; the first is the
// page's main entity.
func (p ldPage) moduleItems(ctx context.Context) []Thing {
	items := p.view.StructuredData

	for _, add := range p.frontend.structured {
		items = add(ctx, p.view, items)
	}

	out := make([]Thing, 0, len(items))

	for _, item := range items {
		if len(item) == 0 {
			continue
		}

		out = append(out, resolveMap(item, p.abs))
	}

	if len(out) > 0 && out[0]["mainEntityOfPage"] == nil {
		out[0]["mainEntityOfPage"] = Thing{"@id": p.canonical}
	}

	return out
}

// resolve is value with the addresses in the properties that hold them
// (urlProperties) made absolute; key is the property value belongs to. The
// items of modules are copied rather than changed.
func resolve(value any, key string, abs func(string) string) any {
	switch v := value.(type) {
	case Thing:
		return resolveMap(v, abs)
	case map[string]any:
		return resolveMap(v, abs)
	case []Thing:
		out := make([]any, len(v))

		for i, item := range v {
			out[i] = resolve(item, key, abs)
		}

		return out
	case []any:
		out := make([]any, len(v))

		for i, item := range v {
			out[i] = resolve(item, key, abs)
		}

		return out
	case []string:
		if !urlProperties[key] {
			return v
		}

		out := make([]string, 0, len(v))

		for _, item := range v {
			if item != "" {
				out = append(out, abs(item))
			}
		}

		return out
	case string:
		if urlProperties[key] && v != "" {
			return abs(v)
		}

		return v
	default:
		return v
	}
}

func resolveMap(item map[string]any, abs func(string) string) Thing {
	out := make(Thing, len(item))

	for key, value := range item {
		out[key] = resolve(value, key, abs)
	}

	return out
}

// compact is item without its empty values, or nil when it has no
// "@type" or nothing but its "@type" is left.
func compact(item Thing) Thing {
	if item == nil {
		return nil
	}

	out := compactMap(item)
	if out == nil || out["@type"] == nil {
		return nil
	}

	return out
}

// compactMap is item without empty values; nil when nothing but its
// "@type" would be left.
func compactMap(item map[string]any) Thing {
	out := Thing{}
	meaningful := false

	for key, value := range item {
		value = compactValue(value)
		if value == nil {
			continue
		}

		out[key] = value

		if key != "@type" && key != "@context" {
			meaningful = true
		}
	}

	if !meaningful {
		return nil
	}

	return out
}

// compactValue is value without empty parts, or nil when it is empty.
func compactValue(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}

		return v
	case time.Time:
		if v.IsZero() {
			return nil
		}

		return v
	case Thing:
		return mapOrNil(compactMap(v))
	case map[string]any:
		return mapOrNil(compactMap(v))
	case []Thing:
		list := make([]any, len(v))

		for i, item := range v {
			list[i] = item
		}

		return compactList(list)
	case []string:
		list := make([]any, len(v))

		for i, item := range v {
			list[i] = item
		}

		return compactList(list)
	case []any:
		return compactList(v)
	default:
		return v
	}
}

// mapOrNil is item as a value, or an untyped nil for a nil item.
func mapOrNil(item Thing) any {
	if item == nil {
		return nil
	}

	return item
}

// compactList is list without its empty values, nil when none is left.
func compactList(list []any) any {
	out := make([]any, 0, len(list))

	for _, value := range list {
		if item := compactValue(value); item != nil {
			out = append(out, item)
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}
