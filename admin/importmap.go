package admin

import (
	"fmt"
	"io/fs"
	"net/http"

	"github.com/donseba/go-importmap"
	"github.com/donseba/go-partial/clients"
)

// The admin's JavaScript packages, listed in importmap.json, are vendored
// into assets/packages with their licences and a lock file of their
// integrity, and embedded with the other assets (task vendor). The lock
// also records the Markdown editor's ProseMirror bundle, which the admin
// builds itself (task editor), as a local package. The folder is not
// named vendor: a Go module leaves out the folders below a vendor folder,
// so a site that requires go-webthings would miss the packages.
//
//go:generate go run github.com/donseba/go-importmap/cmd/importmap vendor -config importmap.json -out assets/packages

// vendorLock is the lock file of the vendored packages, below assets/.
const vendorLock = "packages/" + importmap.LockFile

// adminAssets are the shared assets: the import map of the vendored
// packages, which also fingerprints every file below assets/.
type adminAssets struct {
	importMap *importmap.ImportMap
}

// loadAssets fingerprints the assets and reads the import map of the
// vendored packages. FromFS checks every locked file against the lock, the
// editor bundle included, so a file that differs from what was vendored or
// built fails here, at start.
func loadAssets(files fs.FS) (adminAssets, error) {
	importMap, err := importmap.FromFS(files, vendorLock)
	if err != nil {
		return adminAssets{}, fmt.Errorf("admin assets: %w", err)
	}

	return adminAssets{importMap: importMap}, nil
}

// mapAssets gives the assets the addresses of the admin's routes, once they
// exist: every asset at "admin.asset" with its version, go-partial's
// browser clients among them (see clients.FS). The layout's {{importmap}}
// then maps the names admin.js imports, htmx, partial-sse, partial-flash and
// partial-interactions, and preloads htmx, which admin.js needs first.
//
// Scripts also address assets relative to their own URL, such as editor.js
// importing "./prosemirror.js" or admin.js resolving "./icons.svg", so the
// map points the plain URL of each asset at its versioned one, which the
// browser may cache like the files the templates link to: every asset but
// the vendored packages, which are imported by name, and source.css, the
// Tailwind source of admin.css (go-importmap leaves out the lock and the
// licences). The assets are embedded and never change while the program
// runs, so the map, and the tags of importmap_script and importmap_style,
// carry their Subresource Integrity too.
func (a *Admin) mapAssets() {
	address := func(file string) string {
		return a.URL("admin.asset", "file", file)
	}

	a.assets.importMap.WithURLFunc(address)

	for _, client := range clients.All() {
		a.assets.importMap.WithFile(client.Import, client.File)
	}

	a.assets.importMap.
		ModulePreload("htmx").
		WithVersionedPaths("!packages/**", "!source.css").
		WithRenderOptions(importmap.WithIntegrity())

	a.importMapHash = a.assets.importMap.ScriptHash()
}

// serveAsset answers "admin.asset". A request for a file's current version
// may be cached for a year; any other one revalidates with the file's ETag.
// The assets are not secret, so they are served without a session.
func (a *Admin) serveAsset(w http.ResponseWriter, r *http.Request) {
	a.assets.importMap.ServeFile(w, r, r.PathValue("file"))
}

// ImportMapHash is the Content-Security-Policy hash of the import map the
// admin's layout writes ({{importmap}}), "'sha256-…'", for the script-src
// of a policy without 'unsafe-inline'. The assets are embedded, so it
// never changes while the program runs.
func (a *Admin) ImportMapHash() string {
	return a.importMapHash
}
