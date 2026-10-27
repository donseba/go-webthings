package frontend

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"html/template"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/donseba/go-webthings/site"

	"github.com/donseba/go-importmap/assets"
)

// themeStylesheet is the values stylesheet of theme: the custom properties
// of its colour, select and number settings with "css"
// (ThemeSetting.CSS), set to values, for example
//
//	:root {
//	    --accent: #8aa598;
//	    --measure: 38rem;
//	}
//
// It is nil when the theme has no such settings. Every value is checked
// again before it goes into the CSS, a colour as #rrggbb, a select as one
// of its options that is a plain CSS value (cssOption) and a number as one
// in its range, written with its unit; a setting without such a value is
// left out, so the theme's own stylesheet decides.
func themeStylesheet(theme Theme, values map[string]string) []byte {
	var properties strings.Builder

	for _, setting := range theme.Settings {
		if setting.CSS == "" {
			continue
		}

		value, ok := cssValue(setting, values[setting.Name])
		if !ok {
			continue
		}

		properties.WriteString("    " + setting.CSS + ": " + value + ";\n")
	}

	if properties.Len() == 0 {
		return nil
	}

	return []byte(":root {\n" + properties.String() + "}\n")
}

// cssValue is the value of a setting as it goes into CSS, and whether it
// may: a colour like #1a2b3c, in lowercase, an option of a select that is
// a plain CSS value, or a number of a number setting with its unit, such
// as 38rem.
func cssValue(setting ThemeSetting, value string) (string, bool) {
	switch setting.Type {
	case "color":
		if !colourValue.MatchString(value) {
			return "", false
		}

		return strings.ToLower(value), true
	case "select":
		if !slices.Contains(setting.Options, value) || !cssOption.MatchString(value) {
			return "", false
		}

		return value, true
	case "number":
		number, err := setting.cleanNumber(value)
		if err != nil || !slices.Contains(cssUnits, setting.Unit) {
			return "", false
		}

		return number + setting.Unit, true
	default:
		return "", false
	}
}

// themeSheet is the values stylesheet of a theme in one snapshot of the
// settings, with its version (as the theme's files have, see asset) and its
// Subresource Integrity.
type themeSheet struct {
	snapshot  *site.Snapshot
	content   []byte
	version   string
	integrity string
}

// themeSheet returns the values stylesheet of theme in snapshot, made once
// for each snapshot: saving the settings or theme values gives a new
// snapshot, so the stylesheet, and its version, follow them.
func (f *Frontend) themeSheet(snapshot *site.Snapshot, theme Theme) *themeSheet {
	if kept, ok := f.sheets.Load(theme.Name); ok && kept.(*themeSheet).snapshot == snapshot {
		return kept.(*themeSheet)
	}

	sheet := &themeSheet{
		snapshot: snapshot,
		content:  themeStylesheet(theme, theme.Values(snapshot.ThemeValues(theme.Name))),
	}

	if len(sheet.content) > 0 {
		sum := sha512.Sum384(sheet.content)
		sheet.version = hex.EncodeToString(sum[:8])
		sheet.integrity = "sha384-" + base64.StdEncoding.EncodeToString(sum[:])
	}

	f.sheets.Store(theme.Name, sheet)

	return sheet
}

// themeStyle is {{themeStyle}}: the stylesheet link of the values
// stylesheet of the theme a page renders in (see ThemeStyles), with its
// version and Subresource Integrity, or nothing when the theme has no
// settings with "css". Link it after the theme's own stylesheets, whose
// fallbacks its properties then replace. attrs are more attributes as name
// and value pairs, as for assetStyle. A theme previewed in the admin links
// its own values.
func themeStyle(ctx context.Context, attrs ...string) (template.HTML, error) {
	current, ok := renderingFrom(ctx)
	if !ok || current.frontend.site == nil {
		return "", nil
	}

	theme, ok := current.frontend.themes.Get(current.theme.Name)
	if !ok || theme.Name != current.theme.Name {
		return "", nil
	}

	snapshot, err := current.frontend.site.Snapshot(ctx)
	if err != nil {
		return "", err
	}

	sheet := current.frontend.themeSheet(snapshot, theme)
	if len(sheet.content) == 0 {
		return "", nil
	}

	address := assets.Versioned(current.frontend.Route("theme.values", "theme", theme.Name), sheet.version)

	return assets.StylesheetTag(address, sheet.integrity, attrs...)
}

// ThemeStyles serves the values stylesheets of the themes at the route
// "theme.values", /theme/<theme>/values.css, from the current settings (see
// themeStyle); a theme without settings with "css" has none, and a theme's
// own assets/values.css is not served there. The response has a strong
// ETag; a request for the current version may be cached for a year, any
// other one revalidates.
func (f *Frontend) ThemeStyles() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("theme")

		theme, ok := f.themes.Get(name)
		if !ok || theme.Name != name || f.site == nil {
			http.NotFound(w, r)

			return
		}

		snapshot, err := f.site.Snapshot(r.Context())
		if err != nil {
			http.Error(w, "internal_error", http.StatusInternalServerError)

			return
		}

		sheet := f.themeSheet(snapshot, theme)
		if len(sheet.content) == 0 {
			http.NotFound(w, r)

			return
		}

		header := w.Header()
		header.Set("Content-Type", "text/css; charset=utf-8")
		header.Set("ETag", `"`+sheet.version+`"`)

		if r.URL.Query().Get("v") == sheet.version {
			header.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			header.Set("Cache-Control", "no-cache")
		}

		http.ServeContent(w, r, "values.css", time.Time{}, bytes.NewReader(sheet.content))
	})
}
