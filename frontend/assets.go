package frontend

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing/fstest"

	"github.com/donseba/go-importmap/assets"
	partial "github.com/donseba/go-partial"
	partialerrors "github.com/donseba/go-partial/ext/errors"
)

// themeManifest returns the fingerprints of theme's assets/ folder, which
// give its files versioned URLs and strong ETags (see asset and Assets), or
// nil when it has none. Each theme is fingerprinted once. While templates
// reload (development), the manifest looks at the folder again on every
// use, by the names, sizes and modification times of its files
// (go-importmap's assets.Reload), and fingerprints it again when anything
// in it changed, so edited files get new versions without a restart. It
// does so for every theme, also one whose assets are embedded: those never
// change, so looking finds nothing new and only costs the listing, in
// development alone.
func (f *Frontend) themeManifest(theme Theme) *assets.Manifest {
	cached, ok := f.assets.Load(theme.Name)
	if ok {
		return cached.(*assets.Manifest)
	}

	files := theme.Assets()
	if files == nil {
		return nil
	}

	var options []assets.Option

	if f.reload {
		options = append(options, assets.Reload())
	}

	manifest, err := assets.Fingerprint(files, options...)
	if err != nil {
		slog.Warn("theme assets unavailable", "theme", theme.Name, "error", err)

		return nil
	}

	manifest.WithURLFunc(func(file string) string {
		return f.Route("theme", "file", theme.Name+"/"+file)
	})

	// Two requests may fingerprint the theme at the same time; both keep
	// the manifest stored first.
	cached, _ = f.assets.LoadOrStore(theme.Name, manifest)

	return cached.(*assets.Manifest)
}

// asset is the URL of a file in the theme's assets/ folder, for example
// {{asset "site.css"}} is /theme/default/site.css?v=3f2a…. The version is a
// hash of the file's content, so browsers may cache it until the file
// changes. Files core offers to themes, such as webthings.css (see
// Component), come from core when the theme does not have its own. A file
// neither has is a mistake (see Frontend.mistake), logged with the closest
// files there are; in development the render fails with it.
func asset(ctx context.Context, file string) (string, error) {
	_, address, err := themeAsset(ctx, "asset", file)

	return address, err
}

// assetScript is {{assetScript "site.js" "type" "module"}}: a script tag for
// a file of the theme's assets/ folder at asset's URL, with its Subresource
// Integrity, so browsers refuse a file whose content is not what the page
// expects. attrs are more attributes as name and value pairs (go-importmap's
// assets.ScriptTag). While the files reload (development) they change under
// pages that may be cached, so the tag has no integrity then, nor for a
// file the theme does not have.
func assetScript(ctx context.Context, file string, attrs ...string) (template.HTML, error) {
	address, integrity, err := assetIntegrity(ctx, "assetScript", file)
	if err != nil {
		return "", err
	}

	return assets.ScriptTag(address, integrity, attrs...)
}

// assetStyle is {{assetStyle "site.css"}}: a stylesheet link for a file of
// the theme's assets/ folder, with its Subresource Integrity, as
// assetScript (go-importmap's assets.StylesheetTag).
func assetStyle(ctx context.Context, file string, attrs ...string) (template.HTML, error) {
	address, integrity, err := assetIntegrity(ctx, "assetStyle", file)
	if err != nil {
		return "", err
	}

	return assets.StylesheetTag(address, integrity, attrs...)
}

// assetIntegrity is the URL of file (see asset) and its Subresource
// Integrity, or "" when its manifest reloads or has no such file
// (go-importmap's Manifest.TagIntegrity), for the template function fn.
func assetIntegrity(ctx context.Context, fn, file string) (address, integrity string, err error) {
	manifest, address, err := themeAsset(ctx, fn, file)
	if err != nil || manifest == nil {
		return address, "", err
	}

	return address, manifest.TagIntegrity(strings.TrimPrefix(file, "/")), nil
}

// themeAsset is the URL of file (see asset) and the manifest it is in: the
// theme's, or core's for a file core offers; nil when neither has it,
// which is a mistake of the template function fn (see missingAsset).
func themeAsset(ctx context.Context, fn, file string) (*assets.Manifest, string, error) {
	file = strings.TrimPrefix(file, "/")

	current, ok := renderingFrom(ctx)
	if !ok {
		// Outside a render there is no frontend to ask for the route
		// "theme" (see Assets).
		return nil, "/theme/" + file, nil
	}

	var manifest *assets.Manifest

	if theme, ok := current.frontend.themes.Get(current.theme.Name); ok {
		manifest = current.frontend.themeManifest(theme)
	}

	if manifest != nil && manifest.Has(file) {
		return manifest, manifest.URL(file), nil
	}

	if core := current.frontend.coreAsset(file); core != "" {
		return coreManifest(), core, nil
	}

	address := current.frontend.Route("theme", "file", current.theme.Name+"/"+file)

	return nil, address, current.frontend.mistake(ctx, missingAsset(fn, file, current.theme.Name, manifest))
}

// missingAsset says that the theme has no asset file and core offers none,
// with the closest files there are, such as `assetStyle: no file
// "sitee.css" in the assets of theme default (closest: site.css,
// webthings.css)`.
func missingAsset(fn, file, theme string, manifest *assets.Manifest) string {
	files := coreManifest().Files()
	if manifest != nil {
		files = append(manifest.Files(), files...)
	}

	slices.Sort(files)

	closest := "none"
	if names := closestNames(file, slices.Compact(files), 3); len(names) > 0 {
		closest = strings.Join(names, ", ")
	}

	return fmt.Sprintf("%s: no file %q in the assets of theme %s (closest: %s)", fn, file, theme, closest)
}

// Assets serves the themes' assets/ folders at the route "theme",
// /theme/{file...}: /theme/<theme>/<file> from that theme and /theme/<file>
// from the active one. Themes find files they lack in the default theme.
// Every response has a strong ETag; a request for a file's current version
// (see asset) may be cached for a year, any other one revalidates.
func (f *Frontend) Assets() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested := r.PathValue("file")
		name, file, _ := strings.Cut(requested, "/")

		theme, ok := f.themes.Get(name)
		if !ok || theme.Name != name {
			snapshot, err := f.site.Snapshot(r.Context())
			if err != nil {
				http.Error(w, "internal_error", http.StatusInternalServerError)

				return
			}

			theme, ok = f.themes.Get(snapshot.Settings().Theme)
			file = requested
		}

		if !ok {
			http.NotFound(w, r)

			return
		}

		manifest := f.themeManifest(theme)
		if manifest == nil {
			http.NotFound(w, r)

			return
		}

		manifest.ServeFile(w, r, file)
	})
}

// coreManifest fingerprints core's files for themes, with go-partial's
// partial-errors.css; they are embedded, so once.
var coreManifest = sync.OnceValue(func() *assets.Manifest {
	files, err := fs.Sub(coreAssets, "assets")
	if err != nil {
		panic(err)
	}

	return assets.MustFingerprint(partial.Overlay(files, partialerrors.StylesheetFS()))
})

// FocalPointsStylesheet returns a file system that holds focal-points.css,
// the classes of focalClass (media.FocalPoint.PositionClass), for other
// parts of the site that serve it with their own assets, such as the
// admin's crop previews.
func FocalPointsStylesheet() fs.FS {
	data, err := coreAssets.ReadFile("assets/focal-points.css")
	if err != nil {
		panic(err)
	}

	return fstest.MapFS{"focal-points.css": {Data: data, Mode: 0o444}}
}

// CoreAssets serves a file core offers themes at the route of the same
// name: webthings.css, the optional stylesheet of the default components,
// at /_/webthings.css; focal-points.css, the classes of focalClass, at
// /_/focal-points.css; and partial-errors.css, go-partial's stylesheet of
// the box that takes the place of a part of a page that failed (in
// development, or without an error fragment template), at
// /_/partial-errors.css. The box has no style attributes, so a
// Content-Security-Policy does not block it, and is plain text without
// this stylesheet. The response has a strong ETag and, at the file's
// current version, may be cached for a year.
func CoreAssets(file string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		coreManifest().ServeFile(w, r, file)
	})
}

// coreAsset is the URL of a file core serves when the theme lacks it (see
// CoreAssets), with its version, or "".
func (f *Frontend) coreAsset(file string) string {
	if !coreManifest().Has(file) {
		return ""
	}

	return assets.Versioned(f.Route(file), coreManifest().Version(file))
}
