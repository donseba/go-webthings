package webthings

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/donseba/go-importmap"
	"github.com/donseba/go-importmap/assets"
	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/clients"
)

// assetsImportMap turns Config.Assets into an import map of those files
// and go-partial's browser clients, so they are served like Config.ImportMap's: with versioned URLs
// ({{importmap_url "file"}}), strong ETags and a year's caching at their
// current version. A plain /assets/<file> link still works; it revalidates
// with the ETag. While templates reload (development) the files are looked
// at again on every use, so edits get new versions without a restart.
// Assets is cleared, so a second Start finds only the import map.
func (a *App) assetsImportMap() error {
	if a.config.Assets == nil {
		return nil
	}

	if a.config.ImportMap != nil {
		return errors.New("both Assets and ImportMap are served at /assets/: set one of them")
	}

	var options []assets.Option

	if a.config.ReloadTemplates {
		options = append(options, assets.Reload())
	}

	// go-partial's browser clients are among the files, under the site's
	// own of the same name (see mapClients).
	manifest, err := assets.Fingerprint(partial.Overlay(a.config.Assets, clients.FS()), options...)
	if err != nil {
		return fmt.Errorf("assets: %w", err)
	}

	a.config.ImportMap = importmap.New().WithAssets(manifest)
	a.config.Assets = nil

	return nil
}

// assetFiles serves the files of Config.ImportMap at the route "assets",
// /assets/{file...}, which New registers, and gives the import map that
// route's addresses: a request for a file's current version may be cached
// for a year, any other one revalidates with the file's strong ETag.
// Without an import map there are no assets: every address answers 404.
//
// Files that do not change while the program runs, such as embedded ones,
// also get their Subresource Integrity in the import map and on module
// preloads and stylesheets, so browsers refuse a file whose content is not
// what the page expects. A manifest that reloads (development) changes
// under pages that may be cached, so it gets none
// (importmap.WithStableIntegrity).
func (a *App) assetFiles() http.Handler {
	im := a.config.ImportMap
	if im == nil {
		return http.NotFoundHandler()
	}

	address := func(file string) string {
		path, err := a.root.RoutePathArgs("assets", "file", file)
		if err != nil {
			slog.Error("import map address", "file", file, "error", err)

			return ""
		}

		return path
	}

	im.WithURLFunc(address)
	im.WithRenderOptions(importmap.WithStableIntegrity())

	clientFiles := mapClients(im, address)

	// The route's file wildcard names the file (see routes). A client the
	// site's files do not have comes from core's copy.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file := r.PathValue("file")

		if manifest := im.Manifest(); (manifest == nil || !manifest.Has(file)) && clientFiles.Has(file) {
			clientFiles.ServeFile(w, r, file)

			return
		}

		im.ServeFile(w, r, file)
	})
}

// mapClients maps go-partial's browser clients in the site's import map by
// the names scripts import them by, "partial-flash", "partial-sse" and
// "partial-interactions" (go-partial's clients.All), so every site's
// scripts can import them; a name the site maps itself is left alone. A
// client among the files of the import map's own manifest, such as a
// partial-flash.js of a site-built Config.ImportMap, or of Config.Assets,
// which have the clients among their files (see assetsImportMap), is the
// site's file, mapped like its other files, with its integrity. Any other
// client comes from core's copy at the same route, returned for the
// handler to serve, with its versioned URL and its integrity in the map
// (go-importmap's WithImport).
func mapClients(im *importmap.ImportMap, address func(file string) string) *assets.Manifest {
	clientFiles := assets.MustFingerprint(clients.FS()).WithURLFunc(address)
	manifest := im.Manifest()

	for _, client := range clients.All() {
		if _, taken := im.Structure.Imports[client.Import]; taken {
			continue
		}

		if manifest != nil && manifest.Has(client.File) {
			im.WithFile(client.Import, client.File)

			continue
		}

		im.WithImport(client.Import, clientFiles.URL(client.File), clientFiles.Integrity(client.File))
	}

	return clientFiles
}
