package api

import (
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/pages"
)

// Site is the site in one language, at /api/v1/site.
type Site struct {
	Name        string `json:"name"`
	Tagline     string `json:"tagline"`
	Description string `json:"description"`
	// Locale is the language of the response, DefaultLocale the site's.
	Locale        string `json:"locale"`
	DefaultLocale string `json:"default_locale"`
	// URL is the home page in Locale.
	URL  string `json:"url"`
	Logo *Image `json:"logo"`
	// Languages are the languages the site offers.
	Languages []Language `json:"languages"`
	// Menu is the published page tree below the home page, as the site's
	// menu shows it.
	Menu []Node `json:"menu"`
	// Modules are the module instances with endpoints of their own.
	Modules []ModuleRef `json:"modules"`
}

// Language is a language the site offers: its home page and the site in
// that language in the API.
type Language struct {
	Locale  string `json:"locale"`
	URL     string `json:"url"`
	APIURL  string `json:"api_url"`
	Default bool   `json:"default"`
}

// Node is a published page in the page tree, with the pages below it.
type Node struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Path is the page's address without the language, "/" for the home
	// page; URL its absolute address and APIURL the page in the API.
	Path     string     `json:"path"`
	URL      string     `json:"url"`
	APIURL   string     `json:"api_url"`
	Module   *ModuleRef `json:"module"`
	Children []Node     `json:"children"`
}

// ModuleRef is a module instance: the one placed on a page, or one of the
// site's modules with an API.
type ModuleRef struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Type  string `json:"type"`
	// URL is the page the instance is placed on, null when on none.
	URL *string `json:"url"`
	// APIBase is the address its endpoints are below, such as
	// "https://example.com/api/v1/news" for /api/v1/news/posts, null
	// when it has none.
	APIBase *string `json:"api_base"`
	// Path is the address below the module's page that a page was
	// resolved for (/pages/resolve), such as "/hello" for a post: the
	// module's own content, which its endpoints give; "/" for the page
	// itself. It is left out elsewhere.
	Path string `json:"path,omitempty"`
}

// Page is a published page in one language.
type Page struct {
	ID          string `json:"id"`
	Locale      string `json:"locale"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Slug is the page's own part of Path, empty for the home page.
	Slug     string  `json:"slug"`
	Path     string  `json:"path"`
	URL      string  `json:"url"`
	APIURL   string  `json:"api_url"`
	ParentID *string `json:"parent_id"`
	IsHome   bool    `json:"is_home"`
	// BodyHTML is the page's text rendered from Markdown (see HTML).
	BodyHTML string `json:"body_html"`
	// UpdatedAt is when the published revision was saved.
	UpdatedAt time.Time `json:"updated_at"`
	// Alternates are the page in the other languages it is published in.
	Alternates []Alternate `json:"alternates"`
	// Breadcrumbs lead from the home page to this page, both included.
	Breadcrumbs []Crumb `json:"breadcrumbs"`
	// Module is the module instance placed on the page, null for none.
	Module *ModuleRef `json:"module"`
}

// Alternate is a page in another language.
type Alternate struct {
	Locale string `json:"locale"`
	URL    string `json:"url"`
	APIURL string `json:"api_url"`
}

// Crumb is a page on the way from the home page to a page.
type Crumb struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	APIURL string `json:"api_url"`
}

// SearchResult is a match of the site search: a page or a module's
// content.
type SearchResult struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	URL     string `json:"url"`
	// Type is "page" or the module type, such as "publishing"; Module the
	// module instance, null for a page.
	Type   string  `json:"type"`
	Module *string `json:"module"`
}

// site answers /site.
func (a *API) site(w http.ResponseWriter, r *http.Request) {
	current, _ := scopeOf(r)
	settings := current.settings
	text := settings.Text(current.locale)

	table, err := a.options.Pages.Table(r.Context())
	if err != nil {
		Fail(w, r, err)

		return
	}

	response := Site{
		Name:          text.Title,
		Tagline:       text.Tagline,
		Description:   text.SEODescription,
		Locale:        current.locale,
		DefaultLocale: settings.DefaultLocale,
		URL:           URL(r, "/"+current.locale+"/"),
		Logo:          ImageOf(r, settings.LogoMediaID),
		Languages:     []Language{},
		Menu:          []Node{},
		Modules:       []ModuleRef{},
	}

	for _, locale := range settings.Locales {
		response.Languages = append(response.Languages, Language{
			Locale:  locale,
			URL:     URL(r, "/"+locale+"/"),
			APIURL:  a.endpoint(r, locale, "site"),
			Default: locale == settings.DefaultLocale,
		})
	}

	if home, ok := a.pageTree(r, table, current.locale); ok {
		response.Menu = home.Children
	}

	for _, module := range a.options.Modules {
		if module.API {
			response.Modules = append(response.Modules, *a.moduleRef(r, module.Key, current.locale))
		}
	}

	Write(w, r, response)
}

// search answers /site/search?q=…: the site search's matches, which a page
// of List holds. Without a query there are none.
func (a *API) search(w http.ResponseWriter, r *http.Request) {
	locale := Locale(r)

	found, err := a.options.Frontend.Search(r.Context(), locale, r.URL.Query().Get("q"), frontend.MaxSearchLimit)
	if err != nil {
		Fail(w, r, err)

		return
	}

	results := make([]SearchResult, 0, len(found))

	for _, result := range found {
		results = append(results, SearchResult{
			Title:   result.Title,
			Summary: result.Summary,
			URL:     URL(r, result.URL),
			Type:    result.Type,
			Module:  Optional(result.Module),
		})
	}

	Write(w, r, Paginate(r, results))
}

// tree answers /pages: the home page with the published pages below it.
func (a *API) tree(w http.ResponseWriter, r *http.Request) {
	table, err := a.options.Pages.Table(r.Context())
	if err != nil {
		Fail(w, r, err)

		return
	}

	home, ok := a.pageTree(r, table, Locale(r))
	if !ok {
		NotFound(w, r)

		return
	}

	Write(w, r, home)
}

// resolve answers /pages/resolve?path=/en/about: the published page at an
// address of the site, in the address's language, as the site finds it:
// an address below a module's page gives that page, the address below it
// in Module.Path.
func (a *API) resolve(w http.ResponseWriter, r *http.Request) {
	current, _ := scopeOf(r)

	address := path.Clean("/" + r.URL.Query().Get("path"))
	segment, rest, _ := strings.Cut(strings.TrimPrefix(address, "/"), "/")

	locale, err := current.settings.LocaleConfig().Require(segment)
	if err != nil {
		NotFound(w, r)

		return
	}

	current.locale = locale
	r = r.WithContext(withScope(r.Context(), current))

	resolved, err := a.options.Pages.Resolve(r.Context(), locale, "/"+rest)
	if errors.Is(err, pages.ErrNotFound) {
		NotFound(w, r)

		return
	}

	if err != nil {
		Fail(w, r, err)

		return
	}

	a.writePage(w, r, resolved.Page, resolved.Route, resolved.Rest)
}

// page answers /pages/{id}: a published page in the request's language.
func (a *API) page(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := Locale(r)

	table, err := a.options.Pages.Table(ctx)
	if err != nil {
		Fail(w, r, err)

		return
	}

	route, ok := a.route(table, locale, r.PathValue("id"))
	if !ok {
		NotFound(w, r)

		return
	}

	page, err := a.options.Pages.Published(ctx, route)
	if err != nil {
		Fail(w, r, err)

		return
	}

	a.writePage(w, r, page, route, "")
}

// route is the published route of the page id in locale.
func (a *API) route(table *pages.Table, locale, id string) (pages.Route, bool) {
	address, ok := table.Alternates(id)[locale]
	if !ok {
		return pages.Route{}, false
	}

	route, rest, ok := table.Lookup(locale, address)
	if !ok || rest != "/" || route.PageID != id {
		return pages.Route{}, false
	}

	return route, true
}

// writePage answers with a published page at its route; rest is the
// address below a module's page that was resolved, or "".
func (a *API) writePage(w http.ResponseWriter, r *http.Request, page pages.Page, route pages.Route, rest string) {
	table, err := a.options.Pages.Table(r.Context())
	if err != nil {
		Fail(w, r, err)

		return
	}

	locale := route.Locale
	text, _ := page.Text(locale)

	response := Page{
		ID:          page.ID,
		Locale:      locale,
		Title:       text.Title,
		Description: text.Description,
		Slug:        text.Slug,
		Path:        route.Path,
		URL:         URL(r, link(locale, route.Path)),
		APIURL:      a.pageURL(r, page.ID, locale),
		ParentID:    Optional(page.Fields.ParentID),
		IsHome:      route.Depth == 0,
		BodyHTML:    HTML(r, text.Body),
		UpdatedAt:   page.Revision.CreatedAt.UTC(),
		Alternates:  []Alternate{},
		Breadcrumbs: a.breadcrumbs(r, table, route),
	}

	if response.IsHome {
		response.Slug = ""
	}

	current, _ := scopeOf(r)

	for _, other := range current.settings.Locales {
		address, ok := table.Alternates(page.ID)[other]
		if other == locale || !ok {
			continue
		}

		response.Alternates = append(response.Alternates, Alternate{
			Locale: other,
			URL:    URL(r, link(other, address)),
			APIURL: a.pageURL(r, page.ID, other),
		})
	}

	if route.Module != "" {
		response.Module = a.moduleRef(r, route.Module, locale)
		response.Module.Path = rest
	}

	modified, err := a.pageModified(r.Context(), table, route)
	if err != nil {
		Fail(w, r, err)

		return
	}

	Modified(w, modified, MediaModified(r, text.Body))
	Write(w, r, response)
}

// breadcrumbs lead from the home page to the page at route.
func (a *API) breadcrumbs(r *http.Request, table *pages.Table, route pages.Route) []Crumb {
	var crumbs []Crumb

	at := route

	for {
		crumb := Crumb{
			ID:     at.PageID,
			Title:  at.Title,
			URL:    URL(r, link(route.Locale, at.Path)),
			APIURL: a.pageURL(r, at.PageID, route.Locale),
		}
		crumbs = append([]Crumb{crumb}, crumbs...)

		parent, ok := a.route(table, route.Locale, at.ParentID)
		if at.ParentID == "" || !ok {
			return crumbs
		}

		at = parent
	}
}

// pageTree is the published home page in locale with the pages below it;
// false when the home page is not published in locale.
func (a *API) pageTree(r *http.Request, table *pages.Table, locale string) (Node, bool) {
	routes := table.Routes(locale)
	children := map[string][]pages.Route{}

	var (
		home  pages.Route
		found bool
	)

	for _, route := range routes {
		if route.Depth == 0 {
			home, found = route, true

			continue
		}

		children[route.ParentID] = append(children[route.ParentID], route)
	}

	if !found {
		return Node{}, false
	}

	return a.node(r, home, children), true
}

// node is the page at route with the pages below it; children are the
// routes in tree order by parent.
func (a *API) node(r *http.Request, route pages.Route, children map[string][]pages.Route) Node {
	node := Node{
		ID:       route.PageID,
		Title:    route.Title,
		Path:     route.Path,
		URL:      URL(r, link(route.Locale, route.Path)),
		APIURL:   a.pageURL(r, route.PageID, route.Locale),
		Children: []Node{},
	}

	if route.Module != "" {
		node.Module = a.moduleRef(r, route.Module, route.Locale)
	}

	for _, child := range children[route.PageID] {
		node.Children = append(node.Children, a.node(r, child, children))
	}

	return node
}

// moduleRef is the module instance key in locale.
func (a *API) moduleRef(r *http.Request, key, locale string) *ModuleRef {
	ref := &ModuleRef{Key: key}

	for _, module := range a.options.Modules {
		if module.Key != key {
			continue
		}

		ref.Title = module.Title
		ref.Type = module.Type

		if module.API {
			ref.APIBase = Optional(URL(r, Path+"/"+key))
		}
	}

	// The table is the one MountURL reads: an instance on no page in
	// locale has no address.
	address, err := a.options.Pages.MountURL(r.Context(), key, locale)
	if err == nil {
		ref.URL = Optional(URL(r, address))
	}

	return ref
}

// pageURL is the absolute address of the page id in locale in the API.
func (a *API) pageURL(r *http.Request, id, locale string) string {
	address, err := a.routes.RoutePathArgs("page", "id", id, url.Values{"locale": {locale}})
	if err != nil {
		return ""
	}

	return URL(r, address)
}

// endpoint is the absolute address of the API's endpoint name in locale.
func (a *API) endpoint(r *http.Request, locale, name string) string {
	address, err := a.routes.RoutePathArgs(name, url.Values{"locale": {locale}})
	if err != nil {
		return ""
	}

	return URL(r, address)
}

// link is the address of the page at path in locale, as the site has it:
// "/en/" for the home page and "/en/about" below it.
func link(locale, address string) string {
	if address == "/" {
		return "/" + locale + "/"
	}

	return "/" + locale + strings.TrimSuffix(address, "/")
}
