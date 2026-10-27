// Package media stores shared assets (images and documents) with translated
// titles and alt texts. Content references assets by ID; an asset that is still
// referenced (see wt_media_usage) cannot be deleted. Images can be shown in
// other sizes and formats through variants (see Variant).
package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"  // Register GIF for dimension checks and variants.
	_ "image/jpeg" // Register JPEG for dimension checks and variants.
	_ "image/png"  // Register PNG for dimension checks and variants.
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/locales"

	"github.com/donseba/go-router"

	"golang.org/x/sync/singleflight"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// Schema holds this package's schema files per dialect (see db.ApplySchema).
// It also creates wt_media_usage, which content stores write to.
func Schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}

var (
	// ErrNotFound is an asset that is not in the library, or not in the
	// trash where that is asked.
	ErrNotFound = errors.New("media_not_found")
	// ErrInvalid is an upload without a usable file name, an empty file, an
	// image that cannot be read or has too many pixels, or a storage
	// address that is no URL.
	ErrInvalid = errors.New("invalid_media")
	// ErrTooLarge is an upload over the size limit (see MaxUploadSize).
	ErrTooLarge = errors.New("media_too_large")
	// ErrUnsupportedType is an upload whose content is no type the library
	// accepts, whatever its file name says.
	ErrUnsupportedType = errors.New("unsupported_media_type")
	// ErrInUse refuses to delete an asset that content still uses, so pages
	// never show a missing image.
	ErrInUse = errors.New("media_in_use")
	// ErrUnsafeKey is a storage key that could reach outside the media
	// directory, such as one with a slash or "..".
	ErrUnsafeKey = errors.New("unsafe_media_key")
)

// Allowed upload types, detected from the content, and their file extensions.
var extensions = map[string]string{
	"image/jpeg":                "jpg",
	"image/png":                 "png",
	"image/gif":                 "gif",
	"application/pdf":           "pdf",
	"text/plain; charset=utf-8": "txt",
}

// containers are types the content cannot tell apart, by file extension:
// office documents are zip files and CSV is plain text.
var containers = map[string]map[string]string{
	"application/zip": {
		"zip":  "application/zip",
		"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"odt":  "application/vnd.oasis.opendocument.text",
		"ods":  "application/vnd.oasis.opendocument.spreadsheet",
		"odp":  "application/vnd.oasis.opendocument.presentation",
	},
	"text/plain; charset=utf-8": {
		"csv": "text/csv; charset=utf-8",
	},
}

// detect returns the type of an upload and the extension to store it with.
// SVG is refused also without an XML declaration, when it would otherwise
// read as plain text.
func detect(filename string, data []byte) (string, string, bool) {
	mimeType := http.DetectContentType(data)
	extension := strings.ToLower(strings.TrimPrefix(path.Ext(filename), "."))

	if mimeType == "text/plain; charset=utf-8" && svgMarkup(data) {
		return "image/svg+xml", "svg", false
	}

	if refined, ok := containers[mimeType][extension]; ok {
		return refined, extension, true
	}

	extension, ok := extensions[mimeType]

	return mimeType, extension, ok
}

// sniffLength is how much of an upload http.DetectContentType reads.
const sniffLength = 512

// svgMarkup reports whether text starts as markup with an <svg element in
// its first bytes, such as an SVG image without an XML declaration, which
// the content sniffer reads as plain text.
func svgMarkup(data []byte) bool {
	head := data[:min(len(data), sniffLength)]
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	head = bytes.TrimLeft(head, " \t\r\n\f")

	return bytes.HasPrefix(head, []byte("<")) && bytes.Contains(bytes.ToLower(head), []byte("<svg"))
}

const (
	maxSize   = 10 << 20
	maxPixels = 40_000_000
)

// Translation is an asset's text in one language: the title shown with
// it, and the alternative text read to visitors who cannot see an image.
type Translation struct {
	Locale  string `db:"locale"`
	Title   string `db:"title"`
	AltText string `db:"alt_text"`
}

// Asset is one file in the media library, such as a photo or a PDF, with
// its texts per language. Content refers to it by ID.
type Asset struct {
	ID         string `db:"id"`
	StorageKey string `db:"storage_key"`
	Filename   string `db:"filename"`
	MIMEType   string `db:"mime_type"`
	SizeBytes  int64  `db:"size_bytes"`
	// Width and Height are the size of an image as shown: upright, following
	// its EXIF orientation.
	Width  int `db:"width"`
	Height int `db:"height"`
	// FocalX and FocalY are the image's focal point (see FocalPoint and
	// SetFocalPoint).
	FocalX    float64   `db:"focal_x"`
	FocalY    float64   `db:"focal_y"`
	Checksum  string    `db:"checksum"`
	CreatedAt time.Time `db:"created_at"`
	CreatedBy db.Ref    `db:"created_by"`
	UpdatedAt time.Time `db:"updated_at"`
	UpdatedBy db.Ref    `db:"updated_by"`
	// DeletedAt is when the asset went to the trash, by DeletedBy; nil for
	// assets in the library. Only Trashed returns deleted assets.
	DeletedAt    *time.Time    `db:"deleted_at"`
	DeletedBy    db.Ref        `db:"deleted_by"`
	Translations []Translation `db:"-"`
}

// IsImage reports whether the asset is an image, which can be shown in
// other sizes (see Variant) and needs an alternative text.
func (a Asset) IsImage() bool {
	return strings.HasPrefix(a.MIMEType, "image/")
}

// FocalPoint is the point of the image that crops keep in view.
func (a Asset) FocalPoint() FocalPoint {
	return FocalPoint{X: a.FocalX, Y: a.FocalY}
}

// Text returns the translation for locale; empty when none was written.
func (a Asset) Text(locale string) Translation {
	for _, translation := range a.Translations {
		if translation.Locale == locale {
			return translation
		}
	}

	return Translation{Locale: locale}
}

// Storage keeps the bytes of the files; the database keeps everything else.
// Filesystem is the default; a site can bring another, such as an object
// store, with Config.Storage.
type Storage interface {
	// Put stores size bytes from reader under key, a name the Service
	// chose. It fails rather than overwrite a file.
	Put(ctx context.Context, key string, reader io.Reader, size int64) error
	// Open returns the file stored under key.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes the file under key; a file that is gone already is no
	// error.
	Delete(ctx context.Context, key string) error
	// URL is the address the file is served at, or "" when the site serves
	// it with the library's routes (see Service.RegisterRoutes), as it does
	// with Filesystem.
	URL(key string) string
}

// Pinger is a Storage that can tell whether it can be reached, for example
// an object store over the network. The app's readiness check (/readyz)
// asks storage that implements it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Service is the media library: it stores uploads in Storage and their
// details in the database, serves them with variants, and keeps assets that
// content uses from being deleted. It is safe for concurrent use.
type Service struct {
	db      *db.DB
	storage Storage
	// routes serve the library (see RegisterRoutes); the addresses of
	// variants and of files Storage leaves to the site are theirs.
	routes *router.Router
	// signer signs variant URLs (see SetSigningKey).
	signer atomic.Pointer[signer]
	// assets caches what URLs and variants need of an asset; cached counts
	// its entries.
	assets sync.Map
	cached atomic.Int64
	// version counts the changes to assets in this process; see Version.
	version atomic.Uint64
	// flights joins concurrent requests for one new variant; slots limits how
	// many variants are made at once.
	flights singleflight.Group
	slots   chan struct{}
	// transparency caches whether PNG assets can be transparent (see
	// MailFormat); transparencies counts its entries.
	transparency   sync.Map
	transparencies atomic.Int64
	// preparing holds the IDs of uploaded images whose variants Prepare
	// makes; pending counts those not done yet and prepared is closed when
	// none are (see WaitPrepared).
	preparing chan string
	pendingMu sync.Mutex
	pending   int
	prepared  chan struct{}
}

// New returns the media library on database, whose schema (Schema) must be
// applied, with the files in storage. It signs variant URLs with a random
// key until SetSigningKey sets a lasting one.
func New(database *db.DB, storage Storage) *Service {
	key := make([]byte, 32)
	_, _ = rand.Read(key)

	s := &Service{
		db:        database,
		storage:   storage,
		slots:     make(chan struct{}, max(1, runtime.GOMAXPROCS(0)/2)),
		preparing: make(chan string, prepareQueue),
	}

	s.SetSigningKey(key)

	return s
}

// assetInfo is what URLs, variants and rendered content need of an asset.
// Only the focal point and the alt texts change, so entries are read again
// after infoTTL: a change by another process shows up within that time.
type assetInfo struct {
	key      string
	mimeType string
	width    int
	height   int
	focus    FocalPoint
	// alts are the alt texts by locale; locales without one are missing.
	alts    map[string]string
	expires time.Time
	// urls keeps the addresses made of this version of the asset.
	urls *assetURLs
}

const (
	// maxCached bounds the asset cache; it starts over when full.
	maxCached = 10000
	infoTTL   = time.Minute
)

// info returns the storage key, type, size, focal point and alt texts of an
// asset, from memory when it was read recently.
func (s *Service) info(ctx context.Context, id string) (assetInfo, error) {
	if cached, ok := s.assets.Load(id); ok && time.Now().Before(cached.(assetInfo).expires) {
		return cached.(assetInfo), nil
	}

	// One query reads the asset with the alt texts of all its translations:
	// pages ask for both of every image they show.
	var rows []struct {
		Key      string         `db:"storage_key"`
		MIMEType string         `db:"mime_type"`
		Width    int            `db:"width"`
		Height   int            `db:"height"`
		FocalX   float64        `db:"focal_x"`
		FocalY   float64        `db:"focal_y"`
		Locale   sql.NullString `db:"locale"`
		Alt      sql.NullString `db:"alt_text"`
	}

	err := s.db.Select(ctx, &rows, `
		SELECT a.storage_key, a.mime_type, a.width, a.height, a.focal_x, a.focal_y, t.locale, t.alt_text
		FROM wt_media_asset a
		LEFT JOIN wt_media_translation t ON t.asset_id=a.id
		WHERE a.id=? AND a.deleted_at IS NULL`,
		id,
	)
	if err != nil {
		return assetInfo{}, notFound(err)
	}

	if len(rows) == 0 {
		return assetInfo{}, ErrNotFound
	}

	row := rows[0]
	info := assetInfo{
		key:      row.Key,
		mimeType: row.MIMEType,
		width:    row.Width,
		height:   row.Height,
		focus:    FocalPoint{X: row.FocalX, Y: row.FocalY},
		expires:  time.Now().Add(infoTTL),
		urls:     &assetURLs{},
	}

	for _, row := range rows {
		if row.Alt.String == "" {
			continue
		}

		if info.alts == nil {
			info.alts = map[string]string{}
		}

		info.alts[row.Locale.String] = row.Alt.String
	}

	// Inside a transaction the row may hold changes that are not committed
	// yet, which other requests must not see.
	if !db.InTx(ctx) {
		if s.cached.Add(1) > maxCached {
			s.assets.Clear()
			s.cached.Store(1)
		}

		s.assets.Store(id, info)
	}

	return info, nil
}

// MaxUploadSize is the largest accepted file in bytes.
func (s *Service) MaxUploadSize() int64 {
	return maxSize
}

// Upload stores a file. The type is detected from the bytes, not the
// filename. The file is kept as it was uploaded; the variants pages show of
// an image are made in the background when Prepare runs.
func (s *Service) Upload(ctx context.Context, filename string, reader io.Reader) (Asset, error) {
	filename = path.Base(strings.ReplaceAll(filename, `\`, "/"))
	if filename == "." || filename == "/" || len(filename) > 255 || strings.ContainsAny(filename, "\x00\r\n") {
		return Asset{}, ErrInvalid
	}

	data, err := io.ReadAll(io.LimitReader(reader, maxSize+1))
	if err != nil {
		return Asset{}, err
	}

	if len(data) > maxSize {
		return Asset{}, ErrTooLarge
	}

	if len(data) == 0 {
		return Asset{}, ErrInvalid
	}

	mimeType, extension, ok := detect(filename, data)
	if !ok {
		return Asset{}, ErrUnsupportedType
	}

	now, actor := db.Stamp(ctx)
	checksum := sha256.Sum256(data)
	asset := Asset{
		ID:        db.NewID(),
		Filename:  filename,
		MIMEType:  mimeType,
		SizeBytes: int64(len(data)),
		FocalX:    Centre.X,
		FocalY:    Centre.Y,
		Checksum:  hex.EncodeToString(checksum[:]),
		CreatedAt: now,
		CreatedBy: actor,
		UpdatedAt: now,
		UpdatedBy: actor,
	}
	asset.StorageKey = asset.ID + "." + extension

	if asset.IsImage() {
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width < 1 || config.Height < 1 || config.Width*config.Height > maxPixels {
			return Asset{}, ErrInvalid
		}

		asset.Width = config.Width
		asset.Height = config.Height

		// Orientations 5 to 8 turn the photo a quarter: browsers, variants
		// and focal points see it the other way round.
		if jpegOrientation(data) >= 5 {
			asset.Width, asset.Height = asset.Height, asset.Width
		}
	}

	if err := s.storage.Put(ctx, asset.StorageKey, bytes.NewReader(data), asset.SizeBytes); err != nil {
		return Asset{}, err
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO wt_media_asset(
			id,storage_key,filename,mime_type,size_bytes,width,height,focal_x,focal_y,checksum,
			created_at,created_by,updated_at,updated_by
		)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		asset.ID, asset.StorageKey, asset.Filename, asset.MIMEType, asset.SizeBytes,
		asset.Width, asset.Height, asset.FocalX, asset.FocalY, asset.Checksum,
		asset.CreatedAt, asset.CreatedBy, asset.UpdatedAt, asset.UpdatedBy,
	)
	if err != nil {
		return Asset{}, errors.Join(err, s.storage.Delete(context.WithoutCancel(ctx), asset.StorageKey))
	}

	if asset.IsImage() {
		s.prepare(ctx, asset.ID)
	}

	return asset, nil
}

// assetColumns are the columns of Asset, so a column added to the table
// does not break reading assets.
const assetColumns = `id, storage_key, filename, mime_type, size_bytes, width, height, focal_x, focal_y,
	checksum, created_at, created_by, updated_at, updated_by, deleted_at, deleted_by`

// Get returns an asset in the library; assets in the trash are not found.
func (s *Service) Get(ctx context.Context, id string) (Asset, error) {
	var asset Asset

	err := s.db.Get(ctx, &asset, `SELECT `+assetColumns+` FROM wt_media_asset WHERE id=? AND deleted_at IS NULL`, id)
	if err != nil {
		return Asset{}, notFound(err)
	}

	assets := []Asset{asset}

	err = s.translate(ctx, assets, `
		SELECT asset_id, locale, title, alt_text
		FROM wt_media_translation
		WHERE asset_id=?
		ORDER BY locale`,
		id,
	)

	return assets[0], err
}

// List returns all assets, newest first, without translations (see
// ListWithTranslations).
func (s *Service) List(ctx context.Context) ([]Asset, error) {
	var assets []Asset

	err := s.db.Select(ctx, &assets,
		`SELECT `+assetColumns+` FROM wt_media_asset WHERE deleted_at IS NULL ORDER BY created_at DESC`,
	)

	return assets, err
}

// ListWithTranslations returns all assets, newest first, with their titles
// and alt texts in every language. It reads all translations in one query,
// so a library search can match them without a query per asset.
func (s *Service) ListWithTranslations(ctx context.Context) ([]Asset, error) {
	assets, err := s.List(ctx)
	if err != nil || len(assets) == 0 {
		return assets, err
	}

	err = s.translate(ctx, assets, `
		SELECT t.asset_id, t.locale, t.title, t.alt_text
		FROM wt_media_translation t
		JOIN wt_media_asset a ON a.id=t.asset_id
		WHERE a.deleted_at IS NULL
		ORDER BY t.asset_id, t.locale`,
	)
	if err != nil {
		return nil, err
	}

	return assets, nil
}

// translate fills in the Translations of assets from the rows of query,
// which selects asset_id, locale, title and alt_text. Rows of other assets
// are left out.
func (s *Service) translate(ctx context.Context, assets []Asset, query string, args ...any) error {
	var rows []struct {
		AssetID string `db:"asset_id"`
		Translation
	}

	if err := s.db.Select(ctx, &rows, query, args...); err != nil {
		return err
	}

	translations := make(map[string][]Translation, len(assets))

	for _, row := range rows {
		translations[row.AssetID] = append(translations[row.AssetID], row.Translation)
	}

	for index := range assets {
		assets[index].Translations = translations[assets[index].ID]
	}

	return nil
}

// notFound turns db.ErrNotFound into the library's own ErrNotFound.
func notFound(err error) error {
	if errors.Is(err, db.ErrNotFound) {
		return ErrNotFound
	}

	return err
}

// SaveTranslation sets the title and alt text for one locale.
func (s *Service) SaveTranslation(ctx context.Context, id string, translation Translation) error {
	locale, err := locales.Normalize(translation.Locale)
	if err != nil || len(translation.Title) > 200 || len(translation.AltText) > 2000 {
		return ErrInvalid
	}

	err = s.db.Tx(ctx, func(ctx context.Context) error {
		at, by := db.Stamp(ctx)

		err := db.Affected(s.db.Exec(ctx,
			`UPDATE wt_media_asset SET updated_at=?, updated_by=? WHERE id=? AND deleted_at IS NULL`,
			at, by, id,
		))
		if err != nil {
			return notFound(err)
		}

		_, err = s.db.Exec(ctx, `DELETE FROM wt_media_translation WHERE asset_id=? AND locale=?`, id, locale)
		if err != nil {
			return err
		}

		_, err = s.db.Exec(ctx,
			`INSERT INTO wt_media_translation(asset_id,locale,title,alt_text) VALUES(?,?,?,?)`,
			id, locale, strings.TrimSpace(translation.Title), strings.TrimSpace(translation.AltText),
		)

		return err
	})
	if err != nil {
		return err
	}

	// The alt text is cached with the asset.
	s.changed(id)

	return nil
}

// URL is the public address of an asset, or "" when it does not exist: the
// one its Storage gives, or else that of the library's route named asset
// (see RegisterRoutes).
func (s *Service) URL(ctx context.Context, id string) string {
	info, err := s.info(ctx, id)
	if err != nil {
		return ""
	}

	// The address is made once for each version of the asset.
	if address := info.urls.own.Load(); address != nil {
		return *address
	}

	address, err := s.ownURL(info.key)
	if err != nil {
		slog.WarnContext(ctx, "media URL", "asset", id, "error", err)

		return address
	}

	info.urls.own.Store(&address)

	return address
}

// ownURL is the address of the asset stored at key: the one Storage gives,
// or else that of the library's route named asset.
func (s *Service) ownURL(key string) (string, error) {
	if address := s.storage.URL(key); address != "" {
		return address, nil
	}

	return s.routePath("asset", "key", key)
}

// errNoRoutes is the error of an address the library's routes would give
// before RegisterRoutes was called.
var errNoRoutes = errors.New("media: the library's routes are not registered")

// routePath is the address of the library's route name with args (see
// router.Router.RoutePathArgs).
func (s *Service) routePath(name string, args ...any) (string, error) {
	if s.routes == nil {
		return "", errNoRoutes
	}

	return s.routes.RoutePathArgs(name, args...)
}

// AltText is the alt text editors wrote for an image in locale, or "" when
// there is none. ref is an asset ID or the URL of an asset, as for
// VariantURL. Alt texts are kept in memory with the asset's other details:
// a change in this process shows at once, one by another process within a
// minute.
func (s *Service) AltText(ctx context.Context, ref, locale string) string {
	id, ok := assetID(ref)
	if !ok {
		return ""
	}

	info, err := s.info(ctx, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		slog.WarnContext(ctx, "media alt text", "asset", id, "error", err)
	}

	return info.alts[locale]
}

// Version changes whenever an asset changes in this process: its focal
// point, its titles and alt texts, or whether it is in the library at all.
// Caches of what is made from assets, such as rendered content, compare it
// to know they are out of date. Changes by other processes do not change
// it; the asset details themselves are read again after a minute.
func (s *Service) Version() uint64 {
	return s.version.Load()
}

// changed forgets what is cached of an asset after a change.
func (s *Service) changed(id string) {
	s.assets.Delete(id)
	s.version.Add(1)
}

// Resizable reports whether variants of an asset keep what it shows: JPEG
// and PNG images. A GIF may be animated, which a variant would lose. ref is
// an asset ID or the URL of an asset, as for VariantURL.
func (s *Service) Resizable(ctx context.Context, ref string) bool {
	id, ok := assetID(ref)
	if !ok {
		return false
	}

	info, err := s.info(ctx, id)
	if err != nil {
		return false
	}

	return info.resizable()
}

// resizable reports whether variants keep what the asset shows (see
// Resizable).
func (i assetInfo) resizable() bool {
	return i.mimeType == "image/jpeg" || i.mimeType == "image/png"
}

// Delete moves an asset that no content references to the trash: it
// leaves the library and the website, and Undelete brings it back. Content
// in the trash still references its files, so they stay (ErrInUse).
func (s *Service) Delete(ctx context.Context, id string) error {
	err := s.db.Tx(ctx, func(ctx context.Context) error {
		var uses int

		if err := s.db.Get(ctx, &uses, `SELECT COUNT(*) FROM wt_media_usage WHERE asset_id=?`, id); err != nil {
			return err
		}

		if uses > 0 {
			return ErrInUse
		}

		if err := s.rows().Delete(ctx, id); err != nil {
			return notFound(err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Forgotten after the commit, so no request caches the asset again
	// from before it.
	s.changed(id)

	return nil
}

// rows is wt_media_asset, for moving assets to the trash and back.
func (s *Service) rows() db.SoftTable {
	return db.SoftTable{DB: s.db, Table: "wt_media_asset"}
}

// Undelete brings an asset back from the trash.
func (s *Service) Undelete(ctx context.Context, id string) error {
	if err := s.rows().Undelete(ctx, id); err != nil {
		return notFound(err)
	}

	s.changed(id)

	return nil
}

// Trashed returns assets in the trash with their translations, most
// recently deleted first: at most limit of them from offset (all for a
// limit of 0), and how many the trash holds in all.
func (s *Service) Trashed(ctx context.Context, limit, offset int) ([]Asset, int, error) {
	var assets []Asset

	total, err := s.rows().Trashed(ctx, &assets, assetColumns, limit, offset)
	if err != nil || len(assets) == 0 {
		return assets, total, err
	}

	ids := make([]string, len(assets))
	for i, asset := range assets {
		ids[i] = asset.ID
	}

	query, args, err := s.db.In(`
		SELECT asset_id, locale, title, alt_text
		FROM wt_media_translation
		WHERE asset_id IN (?)
		ORDER BY asset_id, locale`,
		ids,
	)
	if err != nil {
		return nil, 0, err
	}

	if err := s.translate(ctx, assets, query, args...); err != nil {
		return nil, 0, err
	}

	return assets, total, nil
}

// Purge removes an asset in the trash for good, with its file and variants.
// An asset that content in the trash still uses stays (ErrInUse).
func (s *Service) Purge(ctx context.Context, id string) error {
	var (
		asset    Asset
		variants []string
	)

	err := s.rows().Purge(ctx, id, func(ctx context.Context) error {
		if err := s.rows().GetTrashed(ctx, &asset, assetColumns, id); err != nil {
			return err
		}

		// The variant rows go with the asset (ON DELETE CASCADE).
		return s.db.Select(ctx, &variants, `SELECT storage_key FROM wt_media_variant WHERE asset_id=?`, id)
	})
	if db.IsForeignKey(err) {
		return ErrInUse
	}

	if err != nil {
		return notFound(err)
	}

	s.changed(id)

	if err := s.storage.Delete(ctx, asset.StorageKey); err != nil {
		// The asset is gone from the library; only an orphaned file remains.
		slog.WarnContext(ctx, "delete media file", "key", asset.StorageKey, "error", err)
	}

	s.deleteVariants(ctx, variants)

	return nil
}

// Originals returns the storage keys of the uploaded files, as Storage keeps
// them: those of the assets in the library and in the trash, oldest first.
// Variants are left out, as the library makes them again on their first
// request; so are the files of assets deleted for good. A backup of the
// files needs these alone, read from Storage.
func (s *Service) Originals(ctx context.Context) ([]string, error) {
	var keys []string

	err := s.db.Select(ctx, &keys, `SELECT storage_key FROM wt_media_asset ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}

	return keys, nil
}

// Storage returns where the library keeps its files, such as a Filesystem
// or an object store of Config.Storage, for reading the files Originals
// names.
func (s *Service) Storage() Storage {
	return s.storage
}

// Open returns an asset and its bytes by storage key. The caller closes the reader.
func (s *Service) Open(ctx context.Context, key string) (Asset, io.ReadCloser, error) {
	if !keyPattern.MatchString(key) {
		return Asset{}, nil, ErrUnsafeKey
	}

	var asset Asset

	err := s.db.Get(ctx, &asset,
		`SELECT `+assetColumns+` FROM wt_media_asset WHERE storage_key=? AND deleted_at IS NULL`,
		key,
	)
	if err != nil {
		return Asset{}, nil, notFound(err)
	}

	file, err := s.storage.Open(ctx, key)
	if errors.Is(err, fs.ErrNotExist) {
		return Asset{}, nil, ErrNotFound
	}

	return asset, file, err
}
