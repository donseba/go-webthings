package admin

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/donseba/go-importmap/assets"
)

// stylesheetPrefix is the pattern of the section stylesheets below the
// admin's path ("admin.stylesheet").
const stylesheetPrefix = assetsPath + "sections/"

// sectionStylesheet is a section's CSS: its file among the section's Files,
// fingerprinted alone, once at start-up.
type sectionStylesheet struct {
	manifest *assets.Manifest
	file     string
}

// loadStylesheets fingerprints the stylesheets the sections name, so a
// missing file fails at start-up instead of on every page. Their links carry
// the version of their content and its Subresource Integrity.
func (a *Admin) loadStylesheets() error {
	a.stylesheets = map[string]sectionStylesheet{}

	for _, section := range a.sections {
		if section.Stylesheet == "" {
			continue
		}

		if section.Files == nil {
			return fmt.Errorf("admin section %q: a stylesheet needs Files", section.ID)
		}

		file := strings.TrimPrefix(section.Stylesheet, "/")

		// Only the stylesheet: Files may hold a module's templates and more.
		manifest, err := assets.Fingerprint(section.Files, assets.Only(file))
		if err != nil {
			return fmt.Errorf("admin section %q stylesheet: %w", section.ID, err)
		}

		link := a.URL("admin.stylesheet", "file", section.ID+".css")
		manifest.WithURLFunc(func(string) string {
			return link
		})

		tag, err := manifest.Stylesheet(file)
		if err != nil {
			return fmt.Errorf("admin section %q stylesheet: %w", section.ID, err)
		}

		a.stylesheets[section.ID] = sectionStylesheet{manifest: manifest, file: file}
		a.stylesheetLinks = append(a.stylesheetLinks, tag)
	}

	return nil
}

// serveStylesheet answers /assets/sections/<section>.css. Stylesheets are not
// secret: like the shared admin assets they are served without a session,
// and their current version may be cached for a year.
func (a *Admin) serveStylesheet(w http.ResponseWriter, r *http.Request) {
	name, ok := strings.CutSuffix(r.PathValue("file"), ".css")
	if !ok {
		http.NotFound(w, r)

		return
	}

	stylesheet, ok := a.stylesheets[name]
	if !ok {
		http.NotFound(w, r)

		return
	}

	stylesheet.manifest.ServeFile(w, r, stylesheet.file)
}
