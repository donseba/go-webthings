package frontend

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/donseba/go-webthings/site"

	form "github.com/donseba/go-form/v2"
	partial "github.com/donseba/go-partial"
)

// DefaultTheme is the theme other themes fall back to.
const DefaultTheme = "default"

// Themes are the designs of a site, one folder each in the templates:
//
//	templates/default/layout.gohtml
//	templates/default/page.gohtml
//	templates/default/assets/site.css     served at /theme/site.css
//	templates/default/theme.json          optional title and settings
//	templates/dark/layout.gohtml          only what differs from default
//	templates/dark/assets/site.css
//
// A theme finds the templates and assets it does not have in "default", so a
// variant can be just a stylesheet. A site with layout.gohtml directly in its
// templates folder has one theme, "default", made of the whole folder.
//
// theme.json names the theme and declares settings that editors change in the
// admin, such as colours; templates read them as {{.Theme.Values.accent}} in
// the layout or {{theme "accent"}} anywhere:
//
//	{
//	  "title": "Atelier",
//	  "description": "Warm colours, serif headings",
//	  "settings": [
//	    {"name": "accent", "label": "Accent colour", "type": "color", "default": "#c2410c", "css": "--accent"},
//	    {"name": "headings", "label": "Headings", "type": "select", "options": ["serif", "sans"], "default": "serif"},
//	    {"name": "measure", "label": "Text width", "type": "number", "min": 30, "max": 48, "step": 1, "unit": "rem", "default": "38", "css": "--measure"},
//	    {"name": "notice", "label": {"en": "Notice", "nl": "Mededeling"}, "type": "text"}
//	  ]
//	}
//
// A number setting has a min and a max, and optionally a step (from min)
// and a unit; its value is the number alone, such as "38", and a template
// adds the unit itself. A colour, select or number setting with "css"
// becomes a custom property of the theme's values stylesheet,
// /theme/<theme>/values.css, which {{themeStyle}} links: :root { --accent:
// #c2410c; --measure: 38rem; }, a number with its unit. It needs no style
// attribute, so it works under a Content-Security-Policy without
// 'unsafe-inline' (see Frontend.ThemeStyles). A text setting never goes
// into CSS.
type Themes struct {
	themes []Theme
}

// Theme is one design.
type Theme struct {
	// Name is the folder name.
	Name        string
	Title       string
	Description string
	Settings    []ThemeSetting
	// CSP are the sources, by directive, that the site's
	// Content-Security-Policy allows for pages in the theme, besides the
	// site's own, such as a font host: theme.json's "csp", as
	// {"font-src": ["https://fonts.example.com"]}, checked with
	// site.CheckCSPSources.
	CSP map[string][]string
	// Screenshot is a picture of the theme that the admin shows with it,
	// theme.json's "screenshot": the path of an image in the theme's
	// assets/ folder, such as "screenshot.png"; empty without one. See
	// checkScreenshot.
	Screenshot string
	// Files are the theme's templates on top of the default theme's.
	Files fs.FS
	// layers are the theme's own folder and the default theme's: the
	// layers of Files, in which a module's templates are looked up one
	// after the other (see Frontend.Template).
	layers []fs.FS
	// dirs are the folders of layers in the site's templates, such as
	// "editorial" and "default", or "" for a single theme at the root;
	// see Frontend.CheckTemplates.
	dirs []string
}

// templateLayers are the layers of the theme's templates, from the theme's
// own to the default theme's; Files alone when the theme was not loaded by
// LoadThemes.
func (t Theme) templateLayers() []fs.FS {
	if len(t.layers) > 0 {
		return slices.Clone(t.layers)
	}

	if t.Files != nil {
		return []fs.FS{t.Files}
	}

	return nil
}

// ThemeSetting is a value editors choose for a theme.
type ThemeSetting struct {
	Name string `json:"name"`
	// Label is shown in the admin, one text or one per admin language.
	Label Label `json:"label"`
	// Type is color, text, select or number.
	Type    string   `json:"type"`
	Options []string `json:"options"`
	// Default is the value without one saved; for a number, the number
	// alone, such as "38".
	Default string `json:"default"`
	// Min and Max bound a number, both included; a number needs both.
	Min *float64 `json:"min"`
	Max *float64 `json:"max"`
	// Step is the difference between a number's values, counted from Min,
	// such as 0.5; zero allows any number between Min and Max.
	Step float64 `json:"step"`
	// Unit is the CSS unit of a number, one of cssUnits, such as "rem",
	// "px" or "%", which the values stylesheet writes after it; empty for
	// a number without one, such as a line height.
	Unit string `json:"unit"`
	// CSS is the custom property, such as "--accent", that the theme's
	// values stylesheet sets to the value of a color, select or number
	// setting (see Themes); a select's options must then be plain CSS
	// values (see cssOption).
	CSS string `json:"css"`
}

// Label is a text with optional translations, written in theme.json as a
// string or as an object of language codes.
type Label map[string]string

// UnmarshalJSON reads a plain string as the text for every language (key
// ""), and an object as a text per language code.
func (l *Label) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*l = Label{"": text}

		return nil
	}

	var texts map[string]string
	if err := json.Unmarshal(data, &texts); err != nil {
		return errors.New("label must be a text or an object of language codes")
	}

	*l = texts

	return nil
}

// In returns the label in language, the untranslated one, or English.
func (l Label) In(language string) string {
	for _, key := range []string{language, "", "en"} {
		if text, ok := l[key]; ok {
			return text
		}
	}

	for _, text := range l {
		return text
	}

	return ""
}

// ThemeView is the active theme as templates see it.
type ThemeView struct {
	Name string
	// Values are the theme's settings, with defaults for unset ones.
	Values map[string]string
}

var (
	themeName   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	colourValue = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	// cssProperty is the name of a custom property a setting sets.
	cssProperty = regexp.MustCompile(`^--[a-z][a-z0-9-]{0,62}$`)
	// cssOption is an option of a select whose value goes into CSS: words,
	// numbers, lengths and colours such as "serif", "1.25rem" or "#fff",
	// without anything that ends a declaration or a rule (; { }), quotes,
	// escapes, comments or functions such as url().
	cssOption = regexp.MustCompile(`^[A-Za-z0-9#.,% -]{1,100}$`)
	// cssUnits are the units of a number setting: lengths, percentages,
	// times and angles, or none.
	cssUnits = []string{"", "px", "rem", "em", "ch", "ex", "%", "vw", "vh", "vmin", "vmax", "s", "ms", "deg", "turn"}
)

// LoadThemes reads the themes in files. Every folder with a layout.gohtml or
// a theme.json is a theme; layout.gohtml at the root makes the root the only
// theme. Files without any themes give an empty list.
func LoadThemes(files fs.FS) (*Themes, error) {
	if _, err := fs.Stat(files, "layout.gohtml"); err == nil {
		theme, err := loadTheme(files, []fs.FS{files}, DefaultTheme)
		if err != nil {
			return nil, err
		}

		return &Themes{themes: []Theme{theme}}, nil
	}

	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, err
	}

	var (
		themes []Theme
		base   fs.FS
	)

	if folder, err := fs.Sub(files, DefaultTheme); err == nil && isTheme(folder) {
		base = folder
	}

	for _, entry := range entries {
		if !entry.IsDir() || !themeName.MatchString(entry.Name()) {
			continue
		}

		own, err := fs.Sub(files, entry.Name())
		if err != nil || !isTheme(own) {
			continue
		}

		layers := []fs.FS{own}
		dirs := []string{entry.Name()}

		if base != nil && entry.Name() != DefaultTheme {
			layers = append(layers, base)
			dirs = append(dirs, DefaultTheme)
		}

		theme, err := loadTheme(own, layers, entry.Name())
		if err != nil {
			return nil, err
		}

		theme.dirs = dirs

		themes = append(themes, theme)
	}

	// The default theme comes first, the others by name.
	slices.SortStableFunc(themes, func(a, b Theme) int {
		switch {
		case a.Name == DefaultTheme:
			return -1
		case b.Name == DefaultTheme:
			return 1
		default:
			return strings.Compare(a.Name, b.Name)
		}
	})

	return &Themes{themes: themes}, nil
}

func isTheme(folder fs.FS) bool {
	for _, name := range []string{"layout.gohtml", "theme.json"} {
		if _, err := fs.Stat(folder, name); err == nil {
			return true
		}
	}

	return false
}

// loadTheme reads theme.json from the theme's own folder; settings are not
// inherited from the default theme. layers are the theme's own folder and
// the default theme's, if any.
func loadTheme(own fs.FS, layers []fs.FS, name string) (Theme, error) {
	theme := Theme{Name: name, Title: name, Files: own, layers: layers}
	if len(layers) > 1 {
		theme.Files = partial.Overlay(layers...)
	}

	data, err := fs.ReadFile(own, "theme.json")
	if errors.Is(err, fs.ErrNotExist) {
		return theme, nil
	}

	if err != nil {
		return Theme{}, err
	}

	var declared struct {
		Title       string              `json:"title"`
		Description string              `json:"description"`
		Settings    []ThemeSetting      `json:"settings"`
		CSP         map[string][]string `json:"csp"`
		Screenshot  string              `json:"screenshot"`
	}

	if err := json.Unmarshal(data, &declared); err != nil {
		return Theme{}, fmt.Errorf("theme %s: theme.json: %w", name, err)
	}

	if declared.Title != "" {
		theme.Title = declared.Title
	}

	theme.Description = declared.Description

	if err := checkScreenshot(theme, declared.Screenshot); err != nil {
		return Theme{}, fmt.Errorf("theme %s: screenshot %q: %w", name, declared.Screenshot, err)
	}

	theme.Screenshot = declared.Screenshot
	theme.Settings = declared.Settings

	for directive, sources := range declared.CSP {
		if err := site.CheckCSPSources(directive, sources); err != nil {
			return Theme{}, fmt.Errorf("theme %s: csp %s %v: %w", name, directive, sources, err)
		}
	}

	theme.CSP = declared.CSP

	properties := map[string]bool{}

	for _, setting := range theme.Settings {
		if err := setting.check(); err != nil {
			return Theme{}, fmt.Errorf("theme %s: setting %q: %w", name, setting.Name, err)
		}

		if setting.CSS != "" && properties[setting.CSS] {
			return Theme{}, fmt.Errorf("theme %s: setting %q: css %s is set by another setting", name, setting.Name, setting.CSS)
		}

		properties[setting.CSS] = true
	}

	return theme, nil
}

func (s ThemeSetting) check() error {
	if !themeName.MatchString(s.Name) {
		return errors.New("name must be lowercase letters, digits, - or _")
	}

	switch s.Type {
	case "color", "text":
	case "select":
		if len(s.Options) == 0 {
			return errors.New("a select needs options")
		}
	case "number":
		if err := s.checkNumber(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown type %q (color, text, select or number)", s.Type)
	}

	if s.Default != "" {
		if _, err := s.Clean(s.Default); err != nil {
			return fmt.Errorf("default: %w", err)
		}
	}

	return s.checkCSS()
}

// checkNumber checks the bounds of a number setting: finite numbers, min
// not above max, a step that is not negative and a known unit.
func (s ThemeSetting) checkNumber() error {
	if s.Min == nil || s.Max == nil {
		return errors.New("a number needs a min and a max")
	}

	if !finite(*s.Min) || !finite(*s.Max) || *s.Min > *s.Max {
		return errors.New("min must not be above max")
	}

	if !finite(s.Step) || s.Step < 0 {
		return errors.New("step must be zero or more")
	}

	if !slices.Contains(cssUnits, s.Unit) {
		return fmt.Errorf("unknown unit %q (%s)", s.Unit, strings.Join(cssUnits[1:], ", "))
	}

	return nil
}

// finite reports whether number is neither infinite nor NaN.
func finite(number float64) bool {
	return !math.IsInf(number, 0) && !math.IsNaN(number)
}

// checkCSS checks the custom property of a setting: a valid name, for a
// colour, a number or a select whose options are all plain CSS values.
func (s ThemeSetting) checkCSS() error {
	if s.CSS == "" {
		return nil
	}

	if !cssProperty.MatchString(s.CSS) {
		return errors.New("css must be a custom property such as --accent: lowercase letters, digits and -")
	}

	switch s.Type {
	case "color", "number":
	case "select":
		for _, option := range s.Options {
			if !cssOption.MatchString(option) {
				return fmt.Errorf("option %q is no plain CSS value (letters, digits, spaces and # . , %% -)", option)
			}
		}
	default:
		return errors.New("css is for color, select and number settings")
	}

	return nil
}

// Clean validates a value for this setting; empty means the default.
func (s ThemeSetting) Clean(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	switch s.Type {
	case "color":
		if !colourValue.MatchString(value) {
			return "", errors.New("use a colour like #1a2b3c")
		}

		return strings.ToLower(value), nil
	case "select":
		if !slices.Contains(s.Options, value) {
			return "", errors.New("not one of the options")
		}
	case "number":
		return s.cleanNumber(value)
	default:
		if len(value) > 500 {
			return "", errors.New("at most 500 characters")
		}
	}

	return value, nil
}

// cleanNumber validates a number setting's value: a number between Min and
// Max on a Step, returned as Go writes it, such as "38" for "38.0".
func (s ThemeSetting) cleanNumber(value string) (string, error) {
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || !finite(number) {
		return "", errors.New("enter a number")
	}

	if s.Min == nil || s.Max == nil || number < *s.Min || number > *s.Max {
		return "", errors.New("the number is out of range")
	}

	// Steps count from min, exactly, as the theme form (go-form) and the
	// browser count them: 0.3 is on a step of 0.1.
	if s.Step > 0 && !form.OnStep(value, strconv.FormatFloat(*s.Min, 'g', -1, 64), strconv.FormatFloat(s.Step, 'g', -1, 64)) {
		return "", errors.New("the number is between two steps")
	}

	return strconv.FormatFloat(number, 'f', -1, 64), nil
}

// List returns the themes, the default theme first.
func (t *Themes) List() []Theme {
	if t == nil {
		return nil
	}

	return t.themes
}

// Get returns the named theme, or the first theme when it does not exist.
func (t *Themes) Get(name string) (Theme, bool) {
	if t == nil || len(t.themes) == 0 {
		return Theme{}, false
	}

	for _, theme := range t.themes {
		if theme.Name == name {
			return theme, true
		}
	}

	return t.themes[0], true
}

// Values are a theme's settings for templates: stored values over defaults.
func (theme Theme) Values(stored map[string]string) map[string]string {
	values := map[string]string{}

	for _, setting := range theme.Settings {
		values[setting.Name] = setting.Default

		if value, err := setting.Clean(stored[setting.Name]); err == nil && value != "" {
			values[setting.Name] = value
		}
	}

	return values
}

// Assets are the files below the theme's assets/ folder.
func (theme Theme) Assets() fs.FS {
	assets, err := fs.Sub(theme.Files, "assets")
	if err != nil {
		return nil
	}

	return assets
}
