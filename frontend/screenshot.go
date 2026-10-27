package frontend

import (
	"errors"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// screenshotTypes are the extensions of the pictures a theme.json
// "screenshot" may name: images every browser shows.
var screenshotTypes = []string{".png", ".jpg", ".jpeg", ".webp", ".avif", ".gif"}

// checkScreenshot checks theme.json's "screenshot" (Theme.Screenshot): a
// clean path inside the theme's assets/ folder, without a leading slash or
// "..", of an image the theme has. Empty is no picture.
func checkScreenshot(theme Theme, file string) error {
	if file == "" {
		return nil
	}

	if !fs.ValidPath(file) || file == "." || path.Clean(file) != file {
		return errors.New("use a path inside the theme's assets folder, such as screenshot.png")
	}

	if !slices.Contains(screenshotTypes, strings.ToLower(path.Ext(file))) {
		return errors.New("use a PNG, JPEG, WebP, AVIF or GIF image")
	}

	assets := theme.Assets()
	if assets == nil {
		return errors.New("the theme has no assets folder")
	}

	info, err := fs.Stat(assets, file)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return errors.New("a folder is no picture")
	}

	return nil
}

// ThemeScreenshot is the address of the picture of theme its theme.json
// names (Theme.Screenshot), versioned like {{asset}}, for pages outside the
// theme, such as the admin's theme cards; "" without one.
func (f *Frontend) ThemeScreenshot(theme Theme) string {
	if theme.Screenshot == "" {
		return ""
	}

	manifest := f.themeManifest(theme)
	if manifest == nil || !manifest.Has(theme.Screenshot) {
		return ""
	}

	return manifest.URL(theme.Screenshot)
}
