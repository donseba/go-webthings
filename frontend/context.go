package frontend

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"math"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/donseba/go-webthings/media"

	form "github.com/donseba/go-form/v2"
)

type renderKey struct{}

// rendering is what template functions can reach while a page renders.
type rendering struct {
	frontend *Frontend
	locale   string
	request  *http.Request
	// path is the page's full path, including the language prefix.
	path  string
	theme ThemeView
	// view is the page; components name it and its content with @model.
	view View
}

func withRendering(r *http.Request, f *Frontend, view View) *http.Request {
	current := rendering{frontend: f, locale: view.Locale, request: r, path: view.Path, theme: view.Theme, view: view}

	return r.WithContext(context.WithValue(r.Context(), renderKey{}, current))
}

// Request is the request of the page being rendered, for template functions
// that read its query, for example ?comment=sent after a visitor's post.
func Request(ctx context.Context) *http.Request {
	current, _ := renderingFrom(ctx)

	return current.request
}

func renderingFrom(ctx context.Context) (rendering, bool) {
	current, ok := ctx.Value(renderKey{}).(rendering)

	return current, ok && current.frontend != nil
}

// Locale is the language of the page being rendered, for template functions.
func Locale(ctx context.Context) string {
	current, _ := renderingFrom(ctx)

	return current.locale
}

// Service returns the API of a module instance while a page renders, for
// template functions, for example Service(ctx, "news").(*publishing.Service).
func Service(ctx context.Context, key string) any {
	current, ok := renderingFrom(ctx)
	if !ok {
		return nil
	}

	return current.frontend.services[key]
}

// Funcs are the core template functions. Module functions come with their
// modules; all of them can be used in every template.
//
// go-clue:funcmap
func Funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string) string
		"moduleURL": moduleURL,
		//go-clue:sig func(model any, attrs ...any) (html/template.HTML, error)
		"form_open": formOpen,
		//go-clue:sig func(model any) (html/template.HTML, error)
		"form_hidden": formHidden,
		//go-clue:sig func(model any, name string, errs github.com/donseba/go-form/v2.FieldErrors) (html/template.HTML, error)
		"form_field": formField,
		//go-clue:sig func(errs github.com/donseba/go-form/v2.FieldErrors, name string) []string
		"form_errors": form.FieldErrors.Messages,
		//go-clue:sig func(model any) (html/template.HTML, error)
		"form_buttons": formButtons,
		//go-clue:sig func() (html/template.HTML, error)
		"form_close": formClose,
		//go-clue:sig func(model any, errs github.com/donseba/go-form/v2.FieldErrors) (html/template.HTML, error)
		"form_render": formRender,
		//go-clue:sig func(name string) (string, error)
		"theme": themeValue,
		//go-clue:sig func(file string) (string, error)
		"asset": asset,
		//go-clue:sig func(file string, attrs ...string) (html/template.HTML, error)
		"assetScript": assetScript,
		//go-clue:sig func(file string, attrs ...string) (html/template.HTML, error)
		"assetStyle": assetStyle,
		//go-clue:sig func(attrs ...string) (html/template.HTML, error)
		"themeStyle": themeStyle,
		//go-clue:sig func(files ...string) (html/template.HTML, error)
		"styles": styles,
		//go-clue:sig func(ref string, spec string) string
		"image": imageURL,
		//go-clue:sig func(ref string, widths ...int) string
		"srcset": srcset,
		//go-clue:sig func(ref string) string
		"alt": altText,
		//go-clue:sig func(ref string, box ...string) string
		"objectPosition": objectPosition,
		//go-clue:sig func(ref string, box ...string) string
		"focalClass": focalClass,
		//go-clue:sig func(link string) string
		"absURL": absURL,
		//go-clue:sig func(query string, limit int) []github.com/donseba/go-webthings/frontend.SearchResult
		"search": search,
		//go-clue:sig func() string
		"searchURL": searchURL,
		//go-clue:sig func(t time.Time, style ...string) string
		"date": dateFunc,
		//go-clue:sig func(t time.Time) string
		"time": timeFunc,
		//go-clue:sig func(start time.Time, end time.Time, style ...string) string
		"dateRange": dateRangeFunc,
		//go-clue:sig func(start time.Time, end time.Time) string
		"timeRange": timeRangeFunc,
		//go-clue:sig func(day time.Weekday, style ...string) string
		"weekday": weekdayFunc,
		//go-clue:sig func(amount any, currency string) (string, error)
		"money": moneyFunc,
		//go-clue:sig func(page string, title string) []github.com/donseba/go-webthings/frontend.ShareLink
		"share": share,
		//go-clue:sig func(page string, title string) (html/template.HTML, error)
		"shareBar": shareBar,
		//go-clue:sig func() []github.com/donseba/go-webthings/frontend.Profile
		"profiles": profilesFunc,
		//go-clue:sig func(name string, args ...any) string
		"adminURL": adminURL,
		//go-clue:sig func() html/template.HTML
		"feedLinks": feedLinks,
		//go-clue:sig func(format string, name ...string) string
		"feedURL": feedURL,
		//go-clue:sig func() html/template.HTML
		"apiLink": apiLink,
		//go-clue:sig func() (html/template.HTML, error)
		"structuredData": structuredData,
	}
}

// TemplateFuncs are every template function with ctx bound, as templates
// call them: the core functions, the modules' and the site's own, and
// localizer and locale for the language. ctx is the context of the request a
// Renderer renders, or of Frontend.Execute; elsewhere it gives nil. A
// Renderer for another template engine uses it to offer the functions; see
// Renderer.
func TemplateFuncs(ctx context.Context) template.FuncMap {
	current, ok := renderingFrom(ctx)
	if !ok {
		return nil
	}

	return current.frontend.executeFuncs(ctx, current.locale)
}

// formatFor is the formatting of the page being rendered: its language, the
// site's timezone and translations.
func formatFor(ctx context.Context) Format {
	current, ok := renderingFrom(ctx)
	if !ok {
		return Format{}
	}

	return current.frontend.Format(ctx, current.locale)
}

// dateFunc is {{date .Start "long"}}; see Format.Date.
func dateFunc(ctx context.Context, t time.Time, style ...string) string {
	return formatFor(ctx).Date(t, strings.Join(style, " "))
}

// timeFunc is {{time .Start}}; see Format.Time.
func timeFunc(ctx context.Context, t time.Time) string {
	return formatFor(ctx).Time(t)
}

// dateRangeFunc is {{dateRange .Start .End "long"}}; see Format.DateRange.
func dateRangeFunc(ctx context.Context, start, end time.Time, style ...string) string {
	return formatFor(ctx).DateRange(start, end, strings.Join(style, " "))
}

// timeRangeFunc is {{timeRange .Start .End}}; see Format.TimeRange.
func timeRangeFunc(ctx context.Context, start, end time.Time) string {
	return formatFor(ctx).TimeRange(start, end)
}

// weekdayFunc is {{weekday .Weekday}} or {{weekday .Weekday "short"}}; see
// Format.Weekday.
func weekdayFunc(ctx context.Context, day time.Weekday, style ...string) string {
	return formatFor(ctx).Weekday(day, strings.Join(style, " "))
}

// moneyFunc is {{money .PriceCents "EUR"}}: an amount in minor units of any
// integer type; see Format.Money.
func moneyFunc(ctx context.Context, amount any, code string) (string, error) {
	value := reflect.ValueOf(amount)

	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return formatFor(ctx).Money(value.Int(), code), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return formatFor(ctx).Money(int64(min(value.Uint(), math.MaxInt64)), code), nil
	default:
		return "", fmt.Errorf("money: amount must be an integer in minor units (cents), not %T", amount)
	}
}

// imageURL is a resized copy of an image, for example {{image .PhotoURL
// "800x600"}}: ref is a media URL, such as the image URLs module views
// give (variants of the asset, see Frontend.Image), or an asset ID, spec a
// size with optional "crop" and format (see media.ParseVariant). Other
// files and unknown specs give the original URL, so a page never loses its
// image.
func imageURL(ctx context.Context, ref, spec string) string {
	current, ok := renderingFrom(ctx)
	if !ok || ref == "" {
		return ref
	}

	variant, err := media.ParseVariant(spec)
	if err != nil {
		slog.WarnContext(ctx, "template image", "spec", spec, "error", err)

		return original(ctx, current.frontend, ref)
	}

	link, err := current.frontend.media.VariantURL(ctx, ref, variant)
	if err != nil {
		return original(ctx, current.frontend, ref)
	}

	return link
}

// srcset lists resized copies of an image for a srcset attribute, for
// example <img src="{{image .PhotoURL "800"}}" srcset="{{srcset .PhotoURL 400
// 800 1200}}" sizes="100vw">. The copies are WebP.
func srcset(ctx context.Context, ref string, widths ...int) string {
	current, ok := renderingFrom(ctx)
	if !ok || ref == "" {
		return ""
	}

	set, err := current.frontend.media.Srcset(ctx, ref, media.DefaultVariantFormat, widths...)
	if err != nil {
		return ""
	}

	return set
}

// objectPosition is {{objectPosition .PhotoURL "4:3"}}: a CSS
// object-position value for an image the browser crops with object-fit:
// cover, so the crop keeps the focal point editors placed in the media
// library in view, for example
//
//	<img src="{{image .PhotoURL "1200"}}" style="object-fit: cover; object-position: {{objectPosition .PhotoURL "4:3"}}">
//
// With the box's proportions, as "4:3" or "800x600", the browser shows
// exactly what {{image .PhotoURL "800x600 crop"}} shows; without them the
// value suits boxes of any shape. ref is a media URL or asset ID, as for
// image; other images give the centre. A style attribute needs
// 'unsafe-inline' in a Content-Security-Policy; focalClass does not.
func objectPosition(ctx context.Context, ref string, box ...string) string {
	current, ok := renderingFrom(ctx)
	if !ok || ref == "" {
		return "50% 50%"
	}

	return current.frontend.media.ObjectPosition(ctx, ref, strings.Join(box, " "))
}

// focalClass is {{focalClass .PhotoURL "4:3"}}: objectPosition as a class
// of focal-points.css, the stylesheet core serves to themes, with both
// percentages rounded to a multiple of 5, such as "fp-35-60", for example
//
//	{{assetStyle "focal-points.css"}}
//	<img src="{{image .PhotoURL "1200"}}" class="cover {{focalClass .PhotoURL "4:3"}}">
//
// where the theme's .cover sets object-fit: cover. It needs no style
// attribute, so it works under a Content-Security-Policy without
// 'unsafe-inline'. ref and box are as for objectPosition; other images give
// the centre, "fp-50-50".
func focalClass(ctx context.Context, ref string, box ...string) string {
	current, ok := renderingFrom(ctx)
	if !ok || ref == "" {
		return "fp-50-50"
	}

	return current.frontend.media.PositionClass(ctx, ref, strings.Join(box, " "))
}

// altText is {{alt .PhotoURL}}: the alt text editors wrote for an image in
// the media library, in the page's language, or "". ref is a media URL or
// asset ID, as for image.
func altText(ctx context.Context, ref string) string {
	current, ok := renderingFrom(ctx)
	if !ok || ref == "" {
		return ""
	}

	return current.frontend.media.AltText(ctx, ref, current.locale)
}

// absURL is {{absURL .URL}}: a link made absolute with the site's address
// (webthings.Config.BaseURL, or the address of the request), for canonical
// and hreflang links or og:image. A path is relative to the page's and
// keeps its query and fragment: "" is the page itself without its query,
// "#top" the page with #top, "?page=2" the page with that query. An
// absolute URL, also one without a scheme such as "//cdn.example.com/a.png",
// stays as it is. Without a request or BaseURL, for example in e-mail, a
// link stays as it is.
func absURL(ctx context.Context, link string) string {
	current, ok := renderingFrom(ctx)
	if !ok {
		return link
	}

	return absolute(link, current.frontend.baseURL, current.request, current.path)
}

// original is the URL of ref itself: ref when it is a URL, the asset's URL
// when it is an ID.
func original(ctx context.Context, f *Frontend, ref string) string {
	if strings.Contains(ref, "/") {
		return ref
	}

	return f.MediaURL(ctx, ref)
}

// themeValue is a setting of the active theme, for example {{theme "accent"}}.
// A name the theme has no setting of is a mistake (see Frontend.mistake),
// logged with the settings there are; in development the render fails
// with it.
func themeValue(ctx context.Context, name string) (string, error) {
	current, ok := renderingFrom(ctx)
	if !ok {
		return "", nil
	}

	value, ok := current.theme.Values[name]
	if ok {
		return value, nil
	}

	known := make([]string, 0, len(current.theme.Values))
	for setting := range current.theme.Values {
		known = append(known, setting)
	}

	slices.Sort(known)

	list := "none"
	if len(known) > 0 {
		list = strings.Join(known, ", ")
	}

	message := fmt.Sprintf("theme: no setting %q in theme %s (known: %s)", name, current.theme.Name, list)

	return "", current.frontend.mistake(ctx, message)
}

// moduleURL is the address of the page a module instance is placed on, in the
// current language, or "" when it is not on a page.
func moduleURL(ctx context.Context, key string) string {
	current, ok := renderingFrom(ctx)
	if !ok {
		return ""
	}

	address, err := current.frontend.pages.MountURL(ctx, key, current.locale)
	if err != nil {
		return ""
	}

	return address
}
