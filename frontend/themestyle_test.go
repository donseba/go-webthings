package frontend

import "testing"

// TestThemeStylesheetValues checks that only colours and the options of a
// select reach the values stylesheet, also when a theme's settings were
// not checked by LoadThemes.
func TestThemeStylesheetValues(t *testing.T) {
	t.Parallel()

	theme := Theme{Settings: []ThemeSetting{
		{Name: "accent", Type: "color", CSS: "--accent"},
		{Name: "ink", Type: "color", CSS: "--ink"},
		{Name: "font", Type: "select", Options: []string{"serif", "sans;}body{display:none"}, CSS: "--font"},
		{Name: "size", Type: "select", Options: []string{"small"}, CSS: "--size"},
		{Name: "notice", Type: "text", CSS: "--notice"},
		{Name: "plain", Type: "color"},
	}}

	got := string(themeStylesheet(theme, map[string]string{
		"accent": "#AABBCC",
		"ink":    "#000;}body{display:none",
		"font":   "sans;}body{display:none",
		"size":   "large",
		"notice": "red",
		"plain":  "#123456",
	}))
	if got != ":root {\n    --accent: #aabbcc;\n}\n" {
		t.Fatalf("stylesheet:\n%s", got)
	}

	if sheet := themeStylesheet(theme, map[string]string{"ink": "red"}); sheet != nil {
		t.Fatalf("a stylesheet without properties: %q", sheet)
	}
}

// TestThemeStylesheetNumbers checks that a number setting reaches the
// values stylesheet with its unit when it is in range, and is left out
// otherwise, also when a theme's settings were not checked by LoadThemes.
func TestThemeStylesheetNumbers(t *testing.T) {
	t.Parallel()

	low, high := 30.0, 48.0
	theme := Theme{Settings: []ThemeSetting{
		{Name: "measure", Type: "number", Min: &low, Max: &high, Step: 1, Unit: "rem", CSS: "--measure"},
		{Name: "lines", Type: "number", Min: &low, Max: &high, CSS: "--lines"},
		{Name: "wide", Type: "number", Min: &low, Max: &high, CSS: "--wide"},
		{Name: "odd", Type: "number", Min: &low, Max: &high, Unit: "px;}body{display:none", CSS: "--odd"},
		{Name: "open", Type: "number", CSS: "--open"},
	}}

	got := string(themeStylesheet(theme, map[string]string{
		"measure": "42",
		"lines":   "31.5",
		"wide":    "64",
		"odd":     "40",
		"open":    "40",
	}))
	if got != ":root {\n    --measure: 42rem;\n    --lines: 31.5;\n}\n" {
		t.Fatalf("stylesheet:\n%s", got)
	}

	if sheet := themeStylesheet(theme, map[string]string{"measure": "42.5"}); sheet != nil {
		t.Fatalf("a number between steps: %q", sheet)
	}
}
