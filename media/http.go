package media

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"github.com/donseba/go-router"
	"github.com/donseba/go-router/middleware"
)

// RegisterRoutes registers the routes that serve the library on routes, to
// GET and HEAD requests: the bytes of an asset by storage key, named asset,
// and image variants (see Variant), named variant. The library builds the
// addresses of variants from them, and those of assets its Storage leaves to
// the site (Storage.URL returns ""), so routes must be mounted where they
// are served, behind X-Content-Type-Options: nosniff (go-router's
// middleware.SecurityHeaders), so browsers never take an upload for another
// type than it has. webthings.App mounts them at /media. Call it once,
// before routes serves.
func (s *Service) RegisterRoutes(routes *router.Router) {
	routes.Get("/v/{signature}/{key...}", func(w http.ResponseWriter, r *http.Request) {
		serveVariant(s, w, r, r.PathValue("signature"), r.PathValue("key"))
	}).As("variant")

	routes.Get("/{key...}", func(w http.ResponseWriter, r *http.Request) {
		serveAsset(s, w, r, r.PathValue("key"))
	}).As("asset")

	s.routes = routes
}

// serveAsset serves the bytes of the asset stored under key.
func serveAsset(s *Service, w http.ResponseWriter, r *http.Request, key string) {
	asset, file, err := s.Open(r.Context(), key)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnsafeKey) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		slog.ErrorContext(r.Context(), "open media", "error", err)
		http.Error(w, "internal_error", http.StatusInternalServerError)

		return
	}

	defer func() { _ = file.Close() }()

	w.Header().Set("Content-Type", asset.MIMEType)
	w.Header().Set("ETag", `"`+asset.Checksum+`"`)
	w.Header().Set("Cache-Control", "public, max-age=3600")

	if !asset.IsImage() {
		disposition := mime.FormatMediaType("attachment", map[string]string{"filename": asset.Filename})
		w.Header().Set("Content-Disposition", disposition)
	}

	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		http.Error(w, "internal_error", http.StatusInternalServerError)

		return
	}

	http.ServeContent(w, r, asset.Filename, asset.CreatedAt, seeker)
}

// serveVariant serves /v/<signature>/<key>, making the variant when needed.
func serveVariant(s *Service, w http.ResponseWriter, r *http.Request, signature, key string) {
	data, mimeType, err := s.variant(r.Context(), signature, key)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrVariant) || errors.Is(err, ErrInvalid) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		slog.ErrorContext(r.Context(), "media variant", "key", key, "error", err)
		http.Error(w, "internal_error", http.StatusInternalServerError)

		return
	}

	// The URL names the asset, size and format and is signed: its bytes never change.
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("ETag", `"`+key+`"`)
	w.Header().Set("Cache-Control", middleware.DefaultImmutable)

	http.ServeContent(w, r, key, time.Time{}, bytes.NewReader(data))
}
