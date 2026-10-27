// Package frontend serves the public website. WebThings resolves the URL
// (language prefix, page tree, module mounts) and builds one View per request;
// the website's templates only render it.
//
// The layout gets the whole View: site, menu, languages and title. A content
// template gets only its own typed data (View.Content), so it stays small and
// its go-clue @dot contract can be checked:
//
//	layout.gohtml               frontend.View, with {{content}} for the page
//	page.gohtml                 frontend.Page, a content page
//	<key>/<name>.gohtml         a module screen, for example news/post.gohtml
//	404.gohtml                  frontend.NotFound (optional)
//	search.gohtml               frontend.SearchView, at /<locale>/search (optional)
//
// Module screens are looked up by the instance key first ("news/post"), then
// by the module type ("publishing/post"), so instances of one module can share
// templates or each have their own. Modules declare their templates (see
// Template); App.WriteTemplates writes starting points for them.
//
// Modules also offer components, parts of a page any template includes with
// a function such as {{commentThread "remarks" .ID}}; a theme replaces one
// with <key>/<name>.gohtml or <type>/<name>.gohtml, core's with
// site/<name>.gohtml, and otherwise the module's default renders (see
// Component).
//
// The templates of a site are grouped in themes, one folder each (see Themes);
// the paths above are relative to the theme.
//
// Every template gets the page language from the {{locale}} function and the
// localizer the translation functions take from {{localizer}}, as in
// {{tl localizer "Read more"}}.
// Any other template engine can be used by implementing Renderer, which
// receives the whole View.
//
// Requests from HTMX receive only the content, without the layout, with the
// theme's regions out of band (see Templates). One that targets a fragment of
// the theme gets only that fragment. One that targets another element gets
// the content, with HX-Retarget and HX-Reswap headers so HTMX swaps the
// content element; with hx-select (HX-Request-Type "full") the whole page
// comes back. Of the theme's menu region, a navigation gets only the items
// whose state changes from the page the visitor comes from
// (HX-Current-URL), when the items carry their ElementID (see MenuItem).
// The theme's head region, regions/head.gohtml, replaces the page's tags
// in <head>, such as the canonical, alternate and feed links, with those
// of the page arrived at (see Templates).
//
// The layout and content templates also have go-partial's helpers. Of those,
// url, request, urlIs, urlStarts and urlContains see the page's whole path,
// for example /en/news/hello on a news post; content is the content
// template in the layout. slot and hasSlot place the theme's regions in the
// layout, and oob and oobAttr tell a region it goes out of band. The others,
// partial, runtime, ctx, basePath, joinPath and urlPath, belong to
// go-partial's own page structure; basePath is "". Template functions of the
// site and of modules cannot take any of these names, nor locale, localizer,
// requiredSlot and deferredSlot: Start refuses them (see render.Reserved).
package frontend

import (
	"html/template"
	"net/http"
)

// Renderer turns a View into HTML. name is the template name without extension,
// for example "page" or "news/post". Has reports whether a template exists.
//
// A Renderer other than the built-in Templates (webthings.Config.Renderer)
// renders the layout around the content itself: View.Content is the
// content template's data, the rest of View the layout's. It answers "page",
// "404" (optional, see Has), "search" (optional) and module screens such as
// "news/post", and HTMX requests as it sees fit. Render's request carries
// the page being rendered, so TemplateFuncs(r.Context()) gives every
// template function (core, modules' and site's, with localizer and locale
// of the page) ready to call; modules' functions such as {{posts "news" 3}} and
// components such as {{commentThread "remarks" .ID}} need them. When Render
// returns an error it must not have written anything, so the frontend can
// answer with its error page (see Frontend.Fail) and log it; the one
// exception is an error that wraps render.ErrShown, returned after the
// renderer answered with an error page itself, such as a theme's
// 500.gohtml.
type Renderer interface {
	Render(w http.ResponseWriter, r *http.Request, name string, view View) error
	Has(name string) bool
}

// Template is a template a module renders, declared so sites can be checked
// and starting points can be generated.
type Template struct {
	// Name is the file name without extension, for example "post".
	Name string
	// Data is a value of the template's data type, for example Post{}.
	Data any
	// Example is a hand-written starting point. Empty generates one from Data.
	Example string
}

// View is everything a public template receives.
type View struct {
	Site   Site
	Locale string
	// Path is the current URL path, including the language prefix. Links
	// here, in Menu and Languages are paths; {{absURL .Path}} makes one
	// absolute, for example for a canonical link or hreflang.
	Path        string
	Title       string
	Description string
	// Image is an optional absolute or site-relative image URL for sharing;
	// Site.SocialImageURL is the site's default. An image from the media
	// library is given as Site.SocialImageURL is.
	Image string
	// Menu is the published page tree in this language, shared with
	// other requests: read it, never change it (see MenuItem).
	Menu []MenuItem
	// Languages links to this same content in the other languages.
	Languages []Language
	// Page is the current page; for module screens the page they are mounted on.
	Page *Page
	// Module is the key of the module instance on module screens, for example
	// "faq", so one template can serve several instances differently.
	Module string
	// Content is the data of the content template: a Page, a NotFound or the
	// module's view.
	Content any
	// StructuredData are the schema.org items of a module's content
	// (Content.StructuredData), which {{structuredData}} writes with those
	// of the site and the page (see Thing).
	StructuredData []Thing
	// Preview is true when an editor previews a revision instead of the
	// published content (see PreviewParam); Revision says which, for a
	// banner such as "Preview of revision 4 (not published)". Layouts
	// should also add <meta name="robots" content="noindex"> then.
	Preview  bool
	Revision *RevisionPreview
	// Theme is the active theme and the values editors chose for it.
	Theme ThemeView
	// Editor is set when a signed-in editor who may edit the current page or
	// module item views it; nil for visitors. See Editors.
	Editor *EditLink
	// Status is the response's status, such as 404 for 404.gohtml; zero
	// is 200 OK. Templates gives it to the response; for another
	// Renderer, the frontend sets it on the response writer Render gets.
	Status int

	// layout fills Site, Menu and Languages when a template needs them,
	// and ready is set once they are (see filled).
	layout *layoutParts
	ready  bool
	// fragmentRegions sends the regions with a fragment asked for on its
	// own, such as the search page's results (render.View.FragmentRegions).
	fragmentRegions bool
}

// Site is the website identity in the current language.
type Site struct {
	Name           string
	Tagline        string
	SEOTitle       string
	SEODescription string
	// Footer is the editors' footer text, rendered from Markdown.
	Footer template.HTML
	// LogoURL is the logo, an image of the media library that the editors
	// chose in the settings, at most 600 pixels wide, never enlarged, in
	// the format it was uploaded in (media.OriginalFormat): a PNG stays a
	// lossless PNG and a JPEG a JPEG. A PNG no wider, a GIF and an SVG
	// keep their own address. A theme's own logo is one of its assets
	// ({{asset "logo.svg"}}), not LogoURL.
	LogoURL string
	// FaviconURL is the favicon as a 32 × 32 PNG, for <link rel="icon">;
	// AppleTouchIconURL is the same image as a 180 × 180 PNG, for <link
	// rel="apple-touch-icon">. Both are cut square around the image's focal
	// point.
	FaviconURL        string
	AppleTouchIconURL string
	// SocialImageURL is the image shown when a page is shared on social
	// media and has no image of its own (Site settings), for og:image: a
	// 1200 × 630 JPEG cut around the image's focal point, or smaller with
	// those proportions when the image is.
	SocialImageURL string
	Organisation   string
	Email          string
	Phone          string
	Address        string
	HomeURL        string
	// Profiles are the site's social media profiles, in the order of
	// site.Networks, labelled in the page's language.
	Profiles []Profile
}

// MenuItem is one published page in the site's menu, with the pages below
// it, in the order editors gave them. The menu is made once for every
// version of the published pages and shared by the requests that show it:
// a Renderer reads it and never changes it.
type MenuItem struct {
	// ID is the page's ID, Path its path without language prefix, for
	// example "/about/team", so templates can tell pages apart.
	ID   string
	Path string
	// ElementID is the HTML id of the item's element, "menu-" and the
	// page's ID, as in <a id="{{.ElementID}}" href="{{.URL}}">. When the
	// items of the theme's menu region, regions/menu.gohtml, carry it, an
	// HTMX navigation sends only the items whose Active changes, rather
	// than the whole menu; the region must then show nothing else that
	// changes from page to page. Without it, the whole menu goes, as it
	// does with every other request. Pages added, moved or renamed show in
	// the menu with the next whole page.
	ElementID string
	Title     string
	// URL is the page's address in the page's language.
	URL string
	// Active is set for the page being shown and the pages above it, so a
	// menu can mark where the visitor is.
	Active bool
	// Children are the published pages below this one.
	Children []MenuItem
}

// Language is one language of the site in a language switch: the current
// page in that language, or the home page when there is no current page.
// Languages the page is not translated into are left out.
type Language struct {
	// Locale is the language code, such as "nl".
	Locale string
	// URL is the address of the page in this language.
	URL string
	// Active is set for the language being shown.
	Active bool
	// Default is set for the site's default language. When the page has
	// more than one language, themes also give its address as the
	// x-default alternate, for visitors whose language the site does not
	// have, as the sitemap does:
	//
	//	{{if .Default}}<link rel="alternate" hreflang="x-default" href="{{absURL .URL}}">{{end}}
	Default bool
}

// Page is a published content page.
type Page struct {
	ID string
	// ParentID is the parent page's ID, empty for the home page.
	ParentID string
	// Slug is the page's own part of its path in this language, Path the
	// whole path without language prefix, for example "team" and
	// "/about/team"; "/" for the home page. In a preview Path and URL are
	// where the previewed revision shows: below the parent's published
	// path, with the revision's slug (see pages.Service.PreviewPath), also
	// for a page that was never published.
	Slug        string
	Path        string
	Title       string
	Description string
	Body        template.HTML
	URL         string
	// IsHome is set for the home page, which sites often design differently.
	IsHome bool
}

// NotFound is the data of 404.gohtml.
type NotFound struct {
	HomeURL string
}
