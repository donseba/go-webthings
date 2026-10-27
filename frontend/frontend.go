package frontend

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/pages"
	"github.com/donseba/go-webthings/render"
	"github.com/donseba/go-webthings/site"

	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/exp/localization"
	partialerrors "github.com/donseba/go-partial/ext/errors"
	"github.com/donseba/go-router"
	"github.com/donseba/go-router/middleware"
	translator "github.com/donseba/go-translator/v2"
)

// Mount is where the current request sits in the page tree.
type Mount struct {
	// Locale is the language of the request, such as "en".
	Locale string
	// Path is the page path without language prefix, for example "/blog".
	Path string
	// URL is the full path of the request, for example "/en/blog/hello".
	URL string
	// PageID is the ID of the page the request is on or below.
	PageID string
	// Module is the key of the module instance placed on that page; empty
	// for content pages.
	Module string
	// Title is the published title of the page in Locale. A newer draft of
	// the page never shows here, so module screens can use it as their title.
	Title string

	// page is the published page the request resolved to and home tells
	// whether it is the home page; module screens show it as View.Page.
	page *pages.Page
	home bool
}

type mountKey struct{}

func withMount(ctx context.Context, mount Mount) context.Context {
	return context.WithValue(ctx, mountKey{}, mount)
}

// MountFrom returns the request's position; module handlers use it for links.
func MountFrom(ctx context.Context) Mount {
	mount, _ := ctx.Value(mountKey{}).(Mount)

	return mount
}

// Frontend builds Views and renders them. Module handlers receive it to render
// their screens with the site's menu, languages and layout.
type Frontend struct {
	site  *site.Service
	pages *pages.Service
	media *media.Service
	// themes are the site's designs; renderers has one renderer per theme.
	themes    *Themes
	renderers map[string]Renderer
	// types maps module instance keys to module types, for template lookup.
	types map[string]string
	// services are the module instances' APIs, for template functions.
	services map[string]any
	// searchers are the modules' search functions by instance key.
	searchers map[string]SearchFunc
	// feeds are the modules' feeds by instance key (see ServeFeeds).
	feeds map[string]moduleFeeds
	// identify recognises signed-in editors (see Editors).
	identify func(r *http.Request) (SignedIn, bool)
	// funcs are the modules' and site's template functions, for mail and
	// Execute; mail are the modules' mail templates by instance key, and
	// mailSources their default markup (see SetMail).
	funcs       []template.FuncMap
	mail        map[string][]Template
	mailSources partial.Sources
	// translations are the site's (see SetTranslations); calendars keeps
	// the names of months and days and the date patterns of Format looked
	// up in them.
	translations *translator.Translator
	calendars    Calendars
	// components are the modules' components by instance key, and
	// componentSources their default markup (see SetComponents).
	components       map[string][]Template
	componentSources partial.Sources
	// standalone renders components, mail and Execute for a theme without
	// templates renderer, such as with a Renderer of the site's own.
	standalone *render.Renderer
	// baseURL is the site's public address for absolute links; see SetBaseURL.
	baseURL string
	// routes is the app's root router, for addresses by route name; see
	// SetRoutes.
	routes *router.Router
	// reload reads templates again for every render; see SetReload.
	reload bool
	// assets keeps the fingerprints of the themes' assets by theme name
	// (see themeManifest).
	assets sync.Map
	// sheets keeps the values stylesheet of each theme by theme name (see
	// themeSheet).
	sheets sync.Map
	// forms renders the visitor forms of templates (see SetFormTheme).
	forms siteForms
	// diagnostics is what the templates renderers report; see
	// SetDiagnostics.
	diagnostics render.Diagnostics
	// rendered keeps rendered Markdown (see renderedCache).
	rendered renderedCache
	// menus keeps the menu of each language; values the values of the
	// theme last shown (see themeValues).
	menus  menus
	values atomic.Pointer[themeValues]
	// pageTexts keeps the search texts of published pages; searches limits
	// the searches per visitor (see SetSearchThrottle).
	pageTexts pageTexts
	searches  *middleware.Limiter
	// database keeps visitors' posts with mistakes (see SetDatabase and
	// KeepForm).
	database *db.DB
	// structured add to the structured data of every page (see
	// SetStructuredData).
	structured []StructuredDataFunc
	// mistakes keeps the template mistakes already logged, and
	// mistakesLogged counts them (see mistake).
	mistakes       sync.Map
	mistakesLogged atomic.Int64
}

// New creates a frontend with one renderer, for example another template
// engine. Themed gives every theme its own templates. funcs are the
// modules' and site's template functions: the renderer itself does not get
// them (see Renderer and TemplateFuncs), but components, mail and Execute
// use them, and the site's tl among them translates labels such as those of
// share links.
func New(
	settings *site.Service,
	tree *pages.Service,
	library *media.Service,
	renderer Renderer,
	funcs ...template.FuncMap,
) *Frontend {
	f := &Frontend{
		site:             settings,
		pages:            tree,
		media:            library,
		renderers:        map[string]Renderer{DefaultTheme: renderer},
		funcs:            funcs,
		componentSources: componentSources(nil),
		searches:         middleware.NewLimiter(DefaultSearchThrottle, time.Minute),
	}

	f.loadParts()

	return f
}

// Themed creates a frontend that renders each theme with Go templates from
// its files (see Templates and Themes).
func Themed(
	settings *site.Service,
	tree *pages.Service,
	library *media.Service,
	themes *Themes,
	funcs ...template.FuncMap,
) *Frontend {
	f := &Frontend{
		site:      settings,
		pages:     tree,
		media:     library,
		themes:    themes,
		renderers: map[string]Renderer{},
		funcs:     funcs,
		// Core's components work without SetComponents.
		componentSources: componentSources(nil),
		searches:         middleware.NewLimiter(DefaultSearchThrottle, time.Minute),
	}

	f.loadRenderers()
	f.loadParts()

	return f
}

// loadRenderers gives every theme its templates renderer.
func (f *Frontend) loadRenderers() {
	f.renderers = map[string]Renderer{}

	for _, theme := range f.themes.List() {
		f.renderers[theme.Name] = newTemplates(
			withCoreTemplates(theme.templateLayers()...),
			f.reload,
			f.diagnostics,
			f.tr(),
			f.funcs...)
	}

	if len(f.renderers) == 0 {
		f.renderers[DefaultTheme] = Missing()
	}
}

// loadParts makes the standalone renderer of components, mail and Execute
// for a theme without templates renderer (see parts).
func (f *Frontend) loadParts() {
	f.standalone = render.New(nil, "", templateFuncs(f.tr(), f.funcs)...)
	f.standalone.SetReload(f.reload)
	f.standalone.SetDiagnostics(withErrorTexts(f.diagnostics, f.tr()))
}

// parts returns the renderer of parts of the pages of theme, such as
// components: the theme's templates renderer, so parts get the same
// functions, stages and diagnostics as its pages.
func (f *Frontend) parts(theme string) *render.Renderer {
	if templates, ok := f.renderers[theme].(templates); ok {
		return templates.renderer
	}

	return f.standalone
}

// SetReload makes the frontend read the themes' templates again for every
// render when reload is true, so edits to template files show without a
// restart; for development (webthings.Config.ReloadTemplates). Otherwise
// every template, the layout, content templates, partials and components,
// is parsed once. Call it before serving.
func (f *Frontend) SetReload(reload bool) {
	if f.reload == reload {
		return
	}

	f.reload = reload
	f.loadParts()

	if f.themes != nil {
		f.loadRenderers()
	}
}

// SetDiagnostics sets what the themes' template renderers report, for
// example render metrics, and whether they show errors in detail
// (development); see render.Diagnostics. A Renderer of the site's own is
// left alone. Call it before serving.
func (f *Frontend) SetDiagnostics(diagnostics render.Diagnostics) {
	f.diagnostics = diagnostics
	f.loadParts()

	if f.themes != nil {
		f.loadRenderers()
	}
}

// Themes returns the site's themes; nil when it uses a single renderer.
func (f *Frontend) Themes() *Themes {
	return f.themes
}

// renderer returns the renderer of a theme, falling back to the first one.
func (f *Frontend) renderer(theme string) Renderer {
	if renderer, ok := f.renderers[theme]; ok {
		return renderer
	}

	if fallback, ok := f.themes.Get(theme); ok {
		return f.renderers[fallback.Name]
	}

	return f.renderers[DefaultTheme]
}

// Has reports whether a theme has a template, for example "news/post".
func (f *Frontend) Has(theme, name string) bool {
	return f.renderer(theme).Has(name)
}

// SetModules tells the frontend the type and API of every module instance:
// types let "news/post" fall back to "publishing/post", services are what
// template functions use (see Service).
func (f *Frontend) SetModules(types map[string]string, services map[string]any) {
	f.types = types
	f.services = services
}

// Template returns the template for a module screen in a theme: <key>/<name>
// when the theme has it, otherwise <type>/<name>. A theme's own templates
// come before those it has from the default theme (see Themes), both names
// (go-partial's FirstExistingIn).
func (f *Frontend) Template(theme, key, name string) string {
	own := key + "/" + name

	renderer := f.renderer(theme)
	if templates, ok := renderer.(templates); ok {
		file, err := templates.renderer.FirstExistingIn(nil, f.candidates("", key, name)...)
		if err == nil {
			return strings.TrimSuffix(file, ".gohtml")
		}
	} else if renderer.Has(own) {
		return own
	}

	if moduleType, ok := f.types[key]; ok {
		return moduleType + "/" + name
	}

	return own
}

// Content is what a module handler renders.
type Content struct {
	// Template is the module's template name, for example "post".
	Template string
	// Module is the instance key. Render uses the module of the current page
	// when empty.
	Module string
	// Title and Description are the page's title and description, for the
	// layout's <title> and meta tags.
	Title       string
	Description string
	// Image is the image shown when the page is shared (View.Image).
	Image string
	// Data is what the template gets as the content, such as a post.
	Data any
	// Alternates maps locales to this content's path below the mount, for the
	// language switcher, for example {"en": "/hello", "nl": "/hallo"}. Nil
	// links to the mounted page in each language.
	Alternates map[string]string
	// EditURL is the admin screen of the content, for the edit link signed-in
	// editors see (View.Editor), built from a route name (see
	// webthings.Env.AdminURL), such as the editor of a news item. Empty links
	// to the module's admin section, its route "index".
	EditURL string
	// Status is the response status; zero is 200 OK.
	Status int
	// Revision is set when the content is a revision an editor previews
	// (see PreviewRevision and RevisionOf); Render marks the page as a
	// preview.
	Revision *RevisionPreview
	// StructuredData are the content's schema.org items, such as a post's
	// Article, which {{structuredData}} writes with the site's own (see
	// Thing); the first is the page's main entity.
	StructuredData []Thing
}

// Render shows module content in the site layout.
func (f *Frontend) Render(w http.ResponseWriter, r *http.Request, content Content) {
	mount := MountFrom(r.Context())

	view, err := f.view(r, mount.Locale, mount.PageID, content.Alternates)
	if err != nil {
		f.Fail(w, r, err)

		return
	}

	if content.Module == "" {
		content.Module = mount.Module
	}

	view.Title = content.Title
	view.Description = content.Description
	view.Image = f.SocialImageURL(r.Context(), content.Image)
	view.Module = content.Module
	view.Content = content.Data
	view.StructuredData = content.StructuredData

	if mount.page != nil {
		view.Page = f.pageView(r.Context(), *mount.page, mount.Locale, mount.Path, mount.home)
	}

	edit := content.EditURL
	if edit == "" && content.Module != "" {
		// The section's own page is its route "index" (admin.RouteName).
		edit = adminURL(r.Context(), "admin."+content.Module+".index")
	}

	view.Editor = editLink(r.Context(), edit, mount.Locale)

	// The module's content, or the page it is on, may be a preview.
	if revision := cmp.Or(content.Revision, mountPreview(r.Context())); revision != nil {
		previewing(w, &view, revision)
	}

	f.write(w, r, f.Template(view.Theme.Name, content.Module, content.Template), view, content.Status)
}

// MediaURL is the public URL of a media asset, or "". Images on pages use
// Image instead, which does not send the uploaded original.
func (f *Frontend) MediaURL(ctx context.Context, id string) string {
	if id == "" {
		return ""
	}

	return f.media.URL(ctx, id)
}

// Image is how a page shows an image asset by default: a copy at most
// media.ImageWidth wide in media.DefaultVariantFormat, with a srcset of
// smaller ones, its size and the original for downloads (see
// media.Service.Image). Module views give their images with it, as
// ImageURL, ImageSrcset, ImageWidth, ImageHeight and ImageOriginalURL
// fields (<X>URL and so on for an image named otherwise, such as Photo);
// an empty id gives an empty Image.
func (f *Frontend) Image(ctx context.Context, id string) media.Image {
	if id == "" {
		return media.Image{}
	}

	return f.media.Image(ctx, id, media.ImageWidth)
}

// MailImage is how e-mail shows an image asset: a copy at most
// media.MailImageWidth wide, never enlarged, as a JPEG, or as a PNG when
// the upload is a PNG with transparency (media.MailFormat); a GIF keeps its
// own address. Its URL is a path on the site when the library serves the
// file: mail needs it made absolute. An empty id gives an empty Image.
func (f *Frontend) MailImage(ctx context.Context, id string) media.Image {
	if id == "" {
		return media.Image{}
	}

	return f.media.ImageAs(ctx, id, media.MailImageWidth, media.MailFormat)
}

// AltText is the alt text editors wrote in the media library for the
// image asset id in locale, or "", such as for an image in e-mail.
func (f *Frontend) AltText(ctx context.Context, id, locale string) string {
	if id == "" {
		return ""
	}

	return f.media.AltText(ctx, id, locale)
}

// The site's own images are made to measure: the logo is shown small, in
// the format it was uploaded in (lossy WebP would blur its sharp edges);
// the favicon and the icon phones put on their home screen are square PNGs;
// the image shown when a page is shared is the 1200 × 630 JPEG social
// networks and chat apps expect, cut around the focal point.
const logoWidth = 600

var (
	faviconIcon = media.Variant{Width: 32, Height: 32, Crop: true, Format: "png"}
	touchIcon   = media.Variant{Width: 180, Height: 180, Crop: true, Format: "png"}
	socialImage = media.Variant{Width: 1200, Height: 630, Crop: true, Format: "jpg"}
)

// logoURL is the address of the logo, at most logoWidth wide in its own
// format (see Site.LogoURL); "" without one.
func (f *Frontend) logoURL(ctx context.Context, id string) string {
	if id == "" {
		return ""
	}

	return f.media.ImageAs(ctx, id, logoWidth, media.OriginalFormat).URL
}

// variantURL is the address of a variant of an image asset; ref is an asset
// ID or an address, such as View.Image. Other files and addresses outside
// the library keep their own; "" gives "".
func (f *Frontend) variantURL(ctx context.Context, ref string, variant media.Variant) string {
	if ref == "" {
		return ""
	}

	link, err := f.media.VariantURL(ctx, ref, variant)
	if err != nil {
		return original(ctx, f, ref)
	}

	return link
}

// Markdown renders editor content with media resolved, in the language of
// the page being rendered or of the request's page (see MountFrom). JPEG and
// PNG images are shown as resized copies, as Image gives them; an image without a description in the Markdown
// gets its alt text from the media library.
func (f *Frontend) Markdown(ctx context.Context, source string) template.HTML {
	locale := Locale(ctx)
	if locale == "" {
		locale = MountFrom(ctx).Locale
	}

	return f.markdown(ctx, locale, source)
}

// footer renders the site's footer text, which is Markdown; most sites
// have none.
func (f *Frontend) footer(ctx context.Context, locale, source string) template.HTML {
	if source == "" {
		return ""
	}

	return f.markdown(ctx, locale, source)
}

// renderMarkdown renders source in locale with media resolved; markdown
// keeps what it renders.
func (f *Frontend) renderMarkdown(ctx context.Context, locale, source string) template.HTML {
	return markdown.Render(source, f.MarkdownOptions(ctx, locale, "", false))
}

// MarkdownOptions are the options to render editor content with
// (markdown.Render, markdown.PlainMedia) as the site does: links to media
// give the file, and an image without a description in the Markdown gets
// its alt text in locale from the media library. base is
// markdown.Options.Base, such as "https://example.com/en/" for readers away
// from the site, or "" for the site's own pages. Images are shown as a page
// shows them (Image), a resized copy with a srcset; with mail, as e-mail
// shows them (MailImage), one copy every reader can show, such as for a
// feed or a mailing.
func (f *Frontend) MarkdownOptions(ctx context.Context, locale, base string, mail bool) markdown.Options {
	return markdown.Options{
		Base: base,
		Media: func(id string) string {
			return f.media.URL(ctx, id)
		},
		Images: func(id, alt string) (markdown.Image, bool) {
			return f.markdownImage(ctx, locale, id, alt, mail)
		},
	}
}

// markdownImage is how content shows an image asset, on a page or, with
// mail, in e-mail (see MarkdownOptions); ok is false for unknown assets.
func (f *Frontend) markdownImage(ctx context.Context, locale, id, alt string, mail bool) (markdown.Image, bool) {
	var image markdown.Image

	if alt == "" && locale != "" {
		image.Alt = f.media.AltText(ctx, id, locale)
	}

	if mail {
		shown := f.MailImage(ctx, id)
		image.URL = shown.URL
		image.Width = shown.Width
		image.Height = shown.Height

		return image, image.URL != ""
	}

	shown := f.media.Image(ctx, id, media.ImageWidth)
	image.URL = shown.URL
	image.Srcset = shown.Srcset
	image.Sizes = "100vw"
	image.Width = shown.Width
	image.Height = shown.Height

	return image, image.URL != ""
}

// NotFound renders 404.gohtml when the site has one, with the title "Not
// found" in the page's language (the site's translations, see
// Translations); otherwise a plain 404.
func (f *Frontend) NotFound(w http.ResponseWriter, r *http.Request) {
	mount := MountFrom(r.Context())

	view, err := f.view(r, mount.Locale, "", nil)
	if err != nil {
		slog.ErrorContext(r.Context(), "render not found", "path", r.URL.Path, "error", err)
		http.NotFound(w, r)

		return
	}

	view.Title = f.tr().Tl(localization.New(view.Locale), notFoundTitle)

	view.Content = NotFound{HomeURL: homeURL(view.Locale)}

	renderer := f.renderer(view.Theme.Name)
	if !renderer.Has("404") {
		http.NotFound(w, r)

		return
	}

	view.Status = http.StatusNotFound
	view = forRenderer(renderer, view)
	r = withRendering(r, f, view)

	if err := renderer.Render(withStatus(w, renderer, view.Status), r, "404", view); err != nil {
		slog.ErrorContext(r.Context(), "render not found", "path", r.URL.Path, "template", "404.gohtml", "error", err)

		// In development the renderer showed the error already.
		if !errors.Is(err, render.ErrShown) {
			http.NotFound(w, r)
		}
	}
}

func (f *Frontend) page(w http.ResponseWriter, r *http.Request, page pages.Page, route pages.Route) {
	view, err := f.view(r, route.Locale, page.ID, nil)
	if err != nil {
		f.Fail(w, r, err)

		return
	}

	text, _ := page.Text(route.Locale)
	view.Title = text.Title
	view.Description = text.Description
	view.Page = f.pageView(r.Context(), page, route.Locale, route.Path, route.Depth == 0)
	view.Content = *view.Page
	view.Editor = editLink(r.Context(), adminURL(r.Context(), pagesEditor, "id", page.ID), route.Locale)

	f.write(w, r, "page", view, http.StatusOK)
}

// pageView is a page as templates see it, in locale; path is where it
// shows: its published path, or for a preview the path of the previewed
// revision (pages.Service.PreviewPath).
func (f *Frontend) pageView(ctx context.Context, page pages.Page, locale, path string, home bool) *Page {
	text, _ := page.Text(locale)

	view := &Page{
		ID:          page.ID,
		ParentID:    page.Fields.ParentID,
		Slug:        text.Slug,
		Path:        path,
		Title:       text.Title,
		Description: text.Description,
		Body:        f.markdown(ctx, locale, text.Body),
		IsHome:      home,
	}

	if path != "" {
		view.URL = link(locale, path, "")
	}

	return view
}

// view fills the parts every template shares. The site, the menu and the
// languages, which only the layout and its regions show, are filled when
// a template needs them (see View.filled), so a request for the content or
// a fragment alone does without.
func (f *Frontend) view(r *http.Request, locale, pageID string, alternates map[string]string) (View, error) {
	ctx := r.Context()

	snapshot, err := f.site.Snapshot(ctx)
	if err != nil {
		return View{}, err
	}

	table, err := f.pages.Table(ctx)
	if err != nil {
		return View{}, err
	}

	mount := MountFrom(ctx)

	path := r.URL.Path
	if mount.URL != "" {
		path = mount.URL
	}

	view := View{
		Locale: locale,
		Path:   path,
		layout: &layoutParts{
			frontend:   f,
			ctx:        ctx,
			settings:   snapshot.Settings(),
			table:      table,
			locale:     locale,
			pageID:     pageID,
			alternates: alternates,
			current:    mount.Path,
		},
	}

	view.Theme = f.theme(ctx, snapshot)

	return view, nil
}

// layoutParts are the parts of a View that only the layout and its
// regions show, made when a template needs them: once per request, as the
// renderer shares one lazy value among them (see templates.Render).
type layoutParts struct {
	frontend *Frontend
	ctx      context.Context
	settings site.Settings
	table    *pages.Table
	locale   string
	// pageID and alternates are those of the page shown, for the
	// languages; current is its path, for the menu.
	pageID     string
	alternates map[string]string
	current    string
}

// filled is the view with its Site, Menu and Languages made.
func (v View) filled() View {
	parts := v.layout
	if parts == nil || v.ready {
		return v
	}

	v.Site = parts.site()
	v.Menu = parts.frontend.menus.tree(parts.table, parts.locale).marked(parts.current)
	v.Languages = languages(parts.settings, parts.table, parts.locale, parts.pageID, parts.alternates, v.Path)
	v.ready = true

	return v
}

// site is the site's identity in the language of the parts.
func (p *layoutParts) site() Site {
	f := p.frontend
	text := p.settings.Text(p.locale)

	return Site{
		Name:              text.Title,
		Tagline:           text.Tagline,
		SEOTitle:          text.SEOTitle,
		SEODescription:    text.SEODescription,
		Footer:            f.footer(p.ctx, p.locale, text.Footer),
		LogoURL:           f.logoURL(p.ctx, p.settings.LogoMediaID),
		FaviconURL:        f.variantURL(p.ctx, p.settings.FaviconMediaID, faviconIcon),
		AppleTouchIconURL: f.variantURL(p.ctx, p.settings.FaviconMediaID, touchIcon),
		SocialImageURL:    f.SocialImageURL(p.ctx, p.settings.SocialImageMediaID),
		Organisation:      p.settings.Organisation,
		Email:             p.settings.Email,
		Phone:             p.settings.Phone,
		Address:           p.settings.Address,
		HomeURL:           homeURL(p.locale),
		Profiles:          f.profiles(p.settings, p.locale),
	}
}

// homeURL is the address of the home page in locale.
func homeURL(locale string) string {
	return "/" + locale + "/"
}

type themeKey struct{}

// WithTheme renders the request in another theme than the active one, for
// previews in the admin.
func WithTheme(ctx context.Context, theme string) context.Context {
	return context.WithValue(ctx, themeKey{}, theme)
}

// theme is the theme a request renders in, with its values.
func (f *Frontend) theme(ctx context.Context, snapshot *site.Snapshot) ThemeView {
	name := snapshot.Settings().Theme
	if override, ok := ctx.Value(themeKey{}).(string); ok {
		name = override
	}

	theme, ok := f.themes.Get(name)
	if !ok {
		return ThemeView{Name: DefaultTheme, Values: map[string]string{}}
	}

	return ThemeView{Name: theme.Name, Values: f.themeValues(snapshot, theme)}
}

// ThemeCSP are the Content-Security-Policy sources of the theme a request
// renders in (see Theme.CSP): the active theme's, or the previewed one's
// (see WithTheme); nil when the theme has none or the settings cannot be
// read. The map is the theme's own, to read and never to change.
func (f *Frontend) ThemeCSP(ctx context.Context) map[string][]string {
	snapshot, err := f.site.Snapshot(ctx)
	if err != nil {
		return nil
	}

	name := snapshot.Settings().Theme
	if override, ok := ctx.Value(themeKey{}).(string); ok {
		name = override
	}

	theme, ok := f.themes.Get(name)
	if !ok {
		return nil
	}

	return theme.CSP
}

// themeValues are the values of theme in the settings of snapshot (see
// Theme.Values), made once for each snapshot: templates only read them.
func (f *Frontend) themeValues(snapshot *site.Snapshot, theme Theme) map[string]string {
	kept := f.values.Load()
	if kept != nil && kept.snapshot == snapshot && kept.theme == theme.Name {
		return kept.values
	}

	values := theme.Values(snapshot.ThemeValues(theme.Name))
	f.values.Store(&themeValues{snapshot: snapshot, theme: theme.Name, values: values})

	return values
}

// themeValues are the values of a theme in the settings of a snapshot.
type themeValues struct {
	snapshot *site.Snapshot
	theme    string
	values   map[string]string
}

// languages links to the same page, or the same module content, in each
// enabled language where it is published. Below a module's page, a screen
// whose module gives no alternates links to the module's page in the other
// languages, and to itself, here, in its own.
func languages(
	settings site.Settings,
	table *pages.Table,
	locale, pageID string,
	alternates map[string]string,
	here string,
) []Language {
	var paths map[string]string

	if pageID != "" {
		paths = table.Alternates(pageID)
	}

	languages := make([]Language, 0, len(settings.Locales))

	for _, code := range settings.Locales {
		target, ok := paths[code]

		switch {
		case pageID == "":
			target, ok = "/", true
		case alternates != nil:
			suffix, translated := alternates[code]
			ok = ok && translated
			target = strings.TrimSuffix(target, "/") + suffix
		}

		if !ok {
			continue
		}

		address := link(code, target, "")
		if code == locale && pageID != "" && alternates == nil && below(here, address) {
			address = here
		}

		languages = append(languages, Language{
			Locale:  code,
			URL:     address,
			Active:  code == locale,
			Default: code == settings.DefaultLocale,
		})
	}

	return languages
}

// below reports whether path is an address below page, such as a module's
// screen below its page.
func below(path, page string) bool {
	path = strings.TrimSuffix(path, "/")
	page = strings.TrimSuffix(page, "/")

	return path != page && strings.HasPrefix(path, page+"/")
}

// write renders the template name with view and status. Pages for editors
// carry their edit link: Editors keeps them out of caches.
func (f *Frontend) write(w http.ResponseWriter, r *http.Request, name string, view View, status int) {
	view.Status = status
	renderer := f.renderer(view.Theme.Name)

	if !renderer.Has(name) {
		slog.ErrorContext(r.Context(), "missing template", "template", name+".gohtml", "theme", view.Theme.Name)
		http.Error(w, "missing_template", http.StatusInternalServerError)

		return
	}

	view = forRenderer(renderer, view)
	r = withRendering(r, f, view)

	// A failing template wrote nothing (see Renderer): the visitor gets
	// the theme's error page (see Fail), the log the details.
	if err := renderer.Render(withStatus(w, renderer, status), r, name, view); err != nil {
		f.Fail(w, r, fmt.Errorf("template %s: %w", name+".gohtml", err))
	}
}

// forRenderer is view as renderer gets it. Templates fills the parts of the
// layout itself when a template needs them, and chooses the menu items an
// HTMX navigation sends (see menuUpdate); another Renderer gets the view
// filled.
func forRenderer(renderer Renderer, view View) View {
	if _, ok := renderer.(templates); ok {
		return view
	}

	return view.filled()
}

// Fail answers a public request that failed with err, for module handlers
// and visitor actions as for core's own pages: content that does not exist
// (db.ErrNotFound, which content and pages return) gets the site's 404
// page; any other error is logged with the module and path and answered
// with status 500 by the theme's error page, 500.gohtml, with its message
// in the page's language (see Templates), or go-partial's plain error
// markup for a theme without one (go-partial's ext/errors Respond). The
// cause ends up in the log rather than in the visitor's browser. An HTMX
// request, such as a form post, gets the page too, which the
// interactions client's reloadOnError shows as the whole page.
func (f *Frontend) Fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, pages.ErrNotFound) {
		f.NotFound(w, r)

		return
	}

	// A module's page names its module in the mount, a visitor action in
	// the action's own context.
	module := MountFrom(r.Context()).Module
	if module == "" {
		module = actionModule(r.Context())
	}

	slog.ErrorContext(r.Context(), "render page", "module", module, "path", r.URL.Path, "error", err)

	// In development the renderer showed the error already.
	if errors.Is(err, render.ErrShown) {
		return
	}

	r, page := f.failure(r)
	partialerrors.Respond(w, r, page, http.StatusInternalServerError, err)
}

// failure is what Fail answers r with: r in the language of the page, or
// of the page a visitor's form was on (see BackLocale), and the part of a
// page of its theme whose error page answers, with the theme's partials
// and the View for @model, made only when the error page names it.
func (f *Frontend) failure(r *http.Request) (*http.Request, *partial.Partial) {
	if f.site == nil {
		return r, failurePage(f.parts(DefaultTheme))
	}

	locale := MountFrom(r.Context()).Locale
	if locale == "" {
		locale = f.BackLocale(r)
	}

	if locale != "" {
		r = r.WithContext(localization.WithLocalizer(r.Context(), localization.New(locale)))
	}

	view, err := f.view(r, locale, "", nil)
	if err != nil {
		return r, failurePage(f.parts(DefaultTheme))
	}

	// A failing page carries no structured data, as other error pages.
	view.Status = http.StatusInternalServerError

	// The error page renders as every page does, also when Fail is called
	// outside a render, such as by a visitor action: with the theme's asset
	// versions and integrity, and the render context's template functions.
	r = withRendering(r, f, view)

	shell := partial.DotFuncOf(func(context.Context) (View, error) {
		return view.filled(), nil
	})

	return r, failurePage(f.parts(view.Theme.Name)).SetModel(shell.Model())
}

// failurePage is the part of a page of renderer whose error page answers
// Fail, with the theme's partials: the theme's 500.gohtml, or go-partial's
// built-in error page in the page's language (see
// render.Renderer.FailureStages).
func failurePage(renderer *render.Renderer) *partial.Partial {
	return partial.NewID("failure").
		SetParent(renderer.Root()).
		SetChildIncludes(partials).
		Use(renderer.FailureStages()...)
}

// link joins "/<locale>" + mount path + relative path.
func link(locale, mount, relative string) string {
	parts := slices.DeleteFunc([]string{locale, strings.Trim(mount, "/"), strings.Trim(relative, "/")}, func(part string) bool {
		return part == ""
	})

	out := "/" + strings.Join(parts, "/")
	if mount == "/" && relative == "" {
		out += "/"
	}

	return out
}

// withStatus is w for renderer, which writes a page with status: Templates
// gives the response View.Status itself; another Renderer writes to a
// statusWriter.
func withStatus(w http.ResponseWriter, renderer Renderer, status int) http.ResponseWriter {
	if _, ok := renderer.(templates); ok || status == 0 || status == http.StatusOK {
		return w
	}

	return &statusWriter{ResponseWriter: w, status: status}
}

// statusWriter gives a page of a site's own Renderer its status, such as
// 404 for the not-found page: it replaces the success status of the first
// header write. An error the renderer answers itself, such as a page for a
// failing template, keeps its own status.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusWriter) WriteHeader(code int) {
	if s.wrote {
		return
	}

	s.wrote = true

	if code >= http.StatusBadRequest {
		s.ResponseWriter.WriteHeader(code)

		return
	}

	s.ResponseWriter.WriteHeader(s.status)
}

func (s *statusWriter) Write(data []byte) (int, error) {
	s.WriteHeader(http.StatusOK)

	return s.ResponseWriter.Write(data)
}

// Unwrap gives http.ResponseController the writer underneath, for Flush.
func (s *statusWriter) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}
