// Package site holds the website's identity and settings: languages, timezone,
// branding, contact details, social media profiles and per-language title,
// tagline, SEO and footer.
//
// Settings are stored as key/value pairs in wt_setting. The scope is "" for
// site-wide values and a language code for translated text, so new settings
// need no schema change. They are saved directly, not revisioned.
//
// Every public request reads the settings, often several times, so the
// Service keeps them in memory: a save in this process replaces them at once,
// a save by another process shows up within the cache's time to live (see
// SetCacheTTL).
package site

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/mail"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/internal/cache"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/media"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// Schema holds this package's schema files per dialect (see db.ApplySchema).
func Schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}

// ErrInvalid is a settings value the site cannot work with, such as an
// unknown time zone, a language list without the default language or a
// malformed e-mail address.
var ErrInvalid = errors.New("invalid_site_settings")

// ErrReservedLocale is a language whose code is the first segment of an
// address the site serves itself, such as the admin's at /en: its pages,
// at /<code>/..., could not be reached (see Service.ReserveLocales).
var ErrReservedLocale = errors.New("reserved_locale")

// Settings are the site-wide values. Each `setting` tag names a stored key.
type Settings struct {
	DefaultLocale string   `setting:"default_locale"`
	Locales       []string `setting:"locales"`
	Timezone      string   `setting:"timezone"`
	// Theme is the active theme's folder name; empty uses the first theme.
	Theme string `setting:"theme"`

	LogoMediaID        string `setting:"logo_media_id"`
	FaviconMediaID     string `setting:"favicon_media_id"`
	SocialImageMediaID string `setting:"social_image_media_id"`

	Organisation string `setting:"organisation"`
	Email        string `setting:"email"`
	Phone        string `setting:"phone"`
	Address      string `setting:"address"`

	// FeedSize is how many of the newest items each feed of the site lists,
	// such as the posts of a news page; zero is DefaultFeedSize. See
	// FeedLimit.
	FeedSize int `setting:"feed_size"`

	// Profiles are the site's social media profiles, in the order of
	// Networks. Each is stored as its own setting, profile_<network>.
	Profiles []Profile

	// CompressionOff, PageMaxAge and PageStale tune how the site is served;
	// see Compress and PageCache. Responses are compressed unless
	// CompressionOff is set; the cache times are seconds.
	CompressionOff bool `setting:"compression_off"`
	PageMaxAge     int  `setting:"page_max_age"`
	PageStale      int  `setting:"page_stale"`

	// OutputCacheOff, OutputCacheTTL and OutputCacheMB tune the output
	// cache, which keeps public pages for visitors in memory; see
	// OutputCache. It is on unless OutputCacheOff is set; zero times and
	// sizes are the defaults.
	OutputCacheOff bool `setting:"output_cache_off"`
	OutputCacheTTL int  `setting:"output_cache_ttl"`
	OutputCacheMB  int  `setting:"output_cache_mb"`

	// Framing is which sites may show the site's pages in a frame:
	// FramingSameOrigin, FramingAny or FramingNone; empty is
	// FramingSameOrigin. See FramingPolicy.
	Framing string `setting:"framing"`

	// ContentSecurity is whether the site sends a Content-Security-Policy:
	// CSPOff, CSPReportOnly or CSPEnforce; empty is CSPOff. See
	// ContentSecurityPolicy.
	ContentSecurity string `setting:"content_security"`
	// ContentSecuritySources are sources the policy allows besides the
	// site's own, one directive per line, such as "frame-src
	// https://www.youtube-nocookie.com"; see ParseCSPSources.
	ContentSecuritySources string `setting:"content_security_sources"`

	// Review lists the admin sections, by permission ID, whose revisions
	// need a reviewer's approval before they are published; see Reviewed.
	// Empty, the default, turns the review workflow off everywhere.
	Review []string `setting:"review"`

	// TrashDays is after how many days in the admin's trash items are
	// deleted for good, at most MaxTrashDays; zero, the default, keeps
	// them until someone deletes them. See TrashCutoff.
	TrashDays int `setting:"trash_days"`

	// Setup lists the steps of the admin's setup guide an administrator
	// finished, by ID, such as "site", or skipped, as "skip:" followed by
	// the ID; "done" once the guide is finished. Empty, the default, is a
	// site whose setup has not started.
	Setup []string `setting:"setup"`

	Translations []Translation

	// location is Timezone, parsed once when the settings are loaded; see
	// Location.
	location *time.Location
}

// Location is the site's timezone, for showing and reading dates and times.
// Loaded settings parse it once, so calling it on every request costs
// nothing; settings whose Timezone was changed since, or that were made in
// code, parse it on each call. An empty or unknown Timezone is UTC.
func (s Settings) Location() *time.Location {
	if s.location != nil && s.location.String() == s.Timezone {
		return s.location
	}

	return loadLocation(s.Timezone)
}

// loadLocation parses a timezone name; empty or unknown is UTC.
func loadLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}

	return location
}

// Translation is the text of one language; its scope is the locale.
type Translation struct {
	Locale         string
	Title          string `setting:"title"`
	Tagline        string `setting:"tagline"`
	SEOTitle       string `setting:"seo_title"`
	SEODescription string `setting:"seo_description"`
	// Footer is Markdown.
	Footer string `setting:"footer"`
}

// Text returns the translation for locale, falling back to an empty one.
func (s Settings) Text(locale string) Translation {
	for _, translation := range s.Translations {
		if translation.Locale == locale {
			return translation
		}
	}

	return Translation{Locale: locale}
}

// WithText returns the translations with locale replaced.
func (s Settings) WithText(translation Translation) []Translation {
	out := []Translation{translation}

	for _, existing := range s.Translations {
		if existing.Locale != translation.Locale {
			out = append(out, existing)
		}
	}

	return out
}

// LocaleConfig returns the content languages of the settings in the form
// the locales package checks and services use.
func (s Settings) LocaleConfig() locales.Config {
	return locales.Config{Default: s.DefaultLocale, Enabled: s.Locales}
}

// clone copies the slices, so callers cannot change the cached settings.
func (s Settings) clone() Settings {
	s.Locales = slices.Clone(s.Locales)
	s.Review = slices.Clone(s.Review)
	s.Setup = slices.Clone(s.Setup)
	s.Profiles = slices.Clone(s.Profiles)
	s.Translations = slices.Clone(s.Translations)

	return s
}

// Service reads and saves the Site settings, theme values and secrets. It
// is safe for concurrent use.
type Service struct {
	db    *db.DB
	cache cache.Value[*Snapshot]
	// secrets holds the keys Secret read, by name: a key never changes.
	secrets sync.Map
	// reserved are the codes no language may have (see ReserveLocales).
	reserved []string
}

// Snapshot is everything in wt_setting as one load read it: the settings
// and the values of each theme. The Service keeps one and gives it to every
// reader until a save, or the end of the cache's time to live, makes a new
// one; a Snapshot never changes. What its methods return is its own, to
// read and never to change: Service.Settings gives a copy to change.
type Snapshot struct {
	settings Settings
	// themes holds the values of each theme by theme name.
	themes map[string]map[string]string
}

// Settings are the snapshot's settings. Their lists (Locales, Profiles,
// Review and Translations) are the snapshot's own: read them, never change
// them.
func (s *Snapshot) Settings() Settings {
	return s.settings
}

// ThemeValues are the stored values of a theme, nil when it has none; the
// map is the snapshot's own, to read and never to change.
func (s *Snapshot) ThemeValues(theme string) map[string]string {
	return s.themes[theme]
}

type snapshotKey struct{}

// WithSnapshot returns ctx with the current Snapshot, for a request: every
// Snapshot call with ctx, or a context made from it, gives that one, so the
// settings are the same throughout the request, also when an editor saves
// meanwhile. When the settings cannot be read, ctx is returned as it is,
// and Snapshot reads them itself. App does this for every request to the
// site, the admin and the modules' actions.
func (s *Service) WithSnapshot(ctx context.Context) context.Context {
	snapshot, err := s.cache.Get(ctx, s.load)
	if err != nil {
		return ctx
	}

	return context.WithValue(ctx, snapshotKey{}, snapshot)
}

// Snapshot returns the settings and theme values to read: those of the
// request ctx belongs to (see WithSnapshot), otherwise the current ones. It
// copies nothing; see Snapshot for what readers may not do. Code that
// changes the settings it reads, such as a settings form, uses Settings
// and ThemeValues instead, which give copies of the current ones.
func (s *Service) Snapshot(ctx context.Context) (*Snapshot, error) {
	if snapshot, ok := ctx.Value(snapshotKey{}).(*Snapshot); ok {
		return snapshot, nil
	}

	return s.cache.Get(ctx, s.load)
}

// Generation counts the saves of settings and theme values in this
// process. It only goes up, so a cache of what is made from them, such as
// the app's output cache, keeps it with what it made and makes it again
// once it moved on. Saves by other processes show when the cache's time to
// live (see SetCacheTTL) has passed, without changing it.
func (s *Service) Generation() uint64 {
	return s.cache.Generation()
}

// New returns the settings Service on database, whose schema (Schema) must
// be applied. Init stores the first settings.
func New(database *db.DB) *Service {
	return &Service{db: database}
}

// SetCacheTTL sets how long settings are kept in memory (30 seconds by
// default). Saves in this process apply at once; the time to live bounds how
// long saves by other processes take to show up. Zero or less disables the
// cache.
func (s *Service) SetCacheTTL(ttl time.Duration) {
	s.cache.SetTTL(ttl)
}

// Init stores first settings when none exist yet. Later calls do nothing.
func (s *Service) Init(ctx context.Context, name string, config locales.Config) error {
	var count int

	if err := s.db.Get(ctx, &count, `SELECT COUNT(*) FROM wt_setting`); err != nil {
		return err
	}

	if count > 0 {
		return nil
	}

	settings := Settings{DefaultLocale: config.Default, Locales: config.Enabled, Timezone: "UTC"}

	for _, locale := range config.Enabled {
		settings.Translations = append(settings.Translations, Translation{Locale: locale, Title: name})
	}

	return s.Save(ctx, settings)
}

type setting struct {
	Scope string `db:"scope"`
	Name  string `db:"name"`
	Value string `db:"value"`
}

// Settings returns the current settings with all translations.
func (s *Service) Settings(ctx context.Context) (Settings, error) {
	current, err := s.cache.Get(ctx, s.load)
	if err != nil {
		return Settings{}, err
	}

	return current.settings.clone(), nil
}

// load reads all settings and theme values in one query.
func (s *Service) load(ctx context.Context) (*Snapshot, error) {
	var rows []setting

	if err := s.db.Select(ctx, &rows, `SELECT scope, name, value FROM wt_setting ORDER BY scope`); err != nil {
		return nil, err
	}

	var settings Settings

	texts := map[string]*Translation{}
	themes := map[string]map[string]string{}

	for _, row := range rows {
		if network, ok := strings.CutPrefix(row.Name, profilePrefix); ok && row.Scope == "" {
			// Profiles of networks this version does not know are left out.
			if slices.Contains(Networks, network) {
				settings.Profiles = append(settings.Profiles, Profile{Network: network, URL: row.Value})
			}

			continue
		}

		if row.Scope == "" {
			set(&settings, row.Name, row.Value)

			continue
		}

		if theme, ok := strings.CutPrefix(row.Scope, themeScope); ok {
			if themes[theme] == nil {
				themes[theme] = map[string]string{}
			}

			themes[theme][row.Name] = row.Value

			continue
		}

		if strings.HasPrefix(row.Scope, secretScope) {
			continue
		}

		if texts[row.Scope] == nil {
			texts[row.Scope] = &Translation{Locale: row.Scope}
		}

		set(texts[row.Scope], row.Name, row.Value)
	}

	for _, text := range texts {
		settings.Translations = append(settings.Translations, *text)
	}

	slices.SortFunc(settings.Translations, func(a, b Translation) int {
		return strings.Compare(a.Locale, b.Locale)
	})

	sortProfiles(settings.Profiles)

	settings.location = loadLocation(settings.Timezone)

	return &Snapshot{settings: settings, themes: themes}, nil
}

// Configuration implements locales.Provider with the enabled content languages.
func (s *Service) Configuration(ctx context.Context) (locales.Config, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return locales.Config{}, err
	}

	return settings.LocaleConfig(), nil
}

// ReserveLocales keeps languages with the given codes, ignoring case, from
// being enabled: Save refuses them with ErrReservedLocale. The app reserves
// the first segments of its own addresses (webthings.App.CorePaths), such as
// the admin's path, /media and, with the content API, /api, whose routes
// would hide the language's pages. Call it before Init and before the site
// serves.
func (s *Service) ReserveLocales(codes ...string) {
	s.reserved = append(s.reserved, codes...)
}

// ReservedLocale returns the first of locales that is reserved (see
// ReserveLocales), and false when none is.
func (s *Service) ReservedLocale(locales []string) (string, bool) {
	for _, locale := range locales {
		for _, code := range s.reserved {
			if strings.EqualFold(locale, code) {
				return locale, true
			}
		}
	}

	return "", false
}

// Save validates and replaces all settings.
func (s *Service) Save(ctx context.Context, settings Settings) error {
	settings, err := clean(settings)
	if err != nil {
		return err
	}

	if code, reserved := s.ReservedLocale(settings.Locales); reserved {
		return fmt.Errorf("%w: %s", ErrReservedLocale, code)
	}

	rows := pairs("", &settings)

	for _, profile := range settings.Profiles {
		rows = append(rows, setting{Name: profilePrefix + profile.Network, Value: profile.URL})
	}

	for i := range settings.Translations {
		rows = append(rows, pairs(settings.Translations[i].Locale, &settings.Translations[i])...)
	}

	defer s.cache.Invalidate()

	return s.db.Tx(ctx, func(ctx context.Context) error {
		if err := s.clear(ctx); err != nil {
			return err
		}

		now := time.Now().UTC()

		for _, row := range rows {
			_, err := s.db.Exec(ctx,
				`INSERT INTO wt_setting(scope,name,value,updated_at,updated_by) VALUES(?,?,?,?,?)`,
				row.Scope, row.Name, row.Value, now, db.Actor(ctx),
			)
			if err != nil {
				return err
			}
		}

		assets := []string{settings.LogoMediaID, settings.FaviconMediaID, settings.SocialImageMediaID}

		// The footer is Markdown and may show images.
		for _, translation := range settings.Translations {
			assets = append(assets, markdown.MediaIDs(translation.Footer)...)
		}

		return media.Track(ctx, s.db, "wt_setting", "site", assets)
	})
}

// clear deletes the stored settings that Save writes again: the keys of
// Settings, of the known networks' profiles and of Translation in every
// language. Other keys, such as those of newer versions, theme values and
// secrets, are kept.
func (s *Service) clear(ctx context.Context) error {
	site := names(Settings{})

	for _, network := range Networks {
		site = append(site, profilePrefix+network)
	}

	query, args, err := s.db.In(`DELETE FROM wt_setting WHERE scope = '' AND name IN (?)`, site)
	if err != nil {
		return err
	}

	if _, err := s.db.Exec(ctx, query, args...); err != nil {
		return err
	}

	query, args, err = s.db.In(
		`DELETE FROM wt_setting WHERE scope <> '' AND scope NOT LIKE ? AND scope NOT LIKE ? AND name IN (?)`,
		themeScope+"%", secretScope+"%", names(Translation{}),
	)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(ctx, query, args...)

	return err
}

// names lists the setting keys of a struct's tagged fields.
func names(target any) []string {
	kind := reflect.TypeOf(target)

	var out []string

	for i := range kind.NumField() {
		if name := kind.Field(i).Tag.Get("setting"); name != "" {
			out = append(out, name)
		}
	}

	return out
}

// themeScope prefixes the scope of a theme's values, for example "theme:default".
const themeScope = "theme:"

// secretScope is the scope of the site's secrets (see Secret).
const secretScope = "secret:"

// ThemeValues returns the stored settings of a theme.
func (s *Service) ThemeValues(ctx context.Context, theme string) (map[string]string, error) {
	current, err := s.cache.Get(ctx, s.load)
	if err != nil {
		return nil, err
	}

	values := maps.Clone(current.themes[theme])
	if values == nil {
		values = map[string]string{}
	}

	return values, nil
}

// SaveThemeValues replaces the stored settings of a theme.
func (s *Service) SaveThemeValues(ctx context.Context, theme string, values map[string]string) error {
	defer s.cache.Invalidate()

	return s.db.Tx(ctx, func(ctx context.Context) error {
		if _, err := s.db.Exec(ctx, `DELETE FROM wt_setting WHERE scope = ?`, themeScope+theme); err != nil {
			return err
		}

		now := time.Now().UTC()

		for name, value := range values {
			if value == "" {
				continue
			}

			_, err := s.db.Exec(ctx,
				`INSERT INTO wt_setting(scope,name,value,updated_at,updated_by) VALUES(?,?,?,?,?)`,
				themeScope+theme, name, value, now, db.Actor(ctx),
			)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// Secret returns a random 32-byte key that belongs to this site, creating
// it on first use, for example to sign image variant URLs. Every process of
// the site gets the same key; it is never shown in the settings.
func (s *Service) Secret(ctx context.Context, name string) ([]byte, error) {
	if key, ok := s.secrets.Load(name); ok {
		return slices.Clone(key.([]byte)), nil
	}

	key, err := s.secret(ctx, name)
	if err != nil {
		return nil, err
	}

	s.secrets.Store(name, key)

	return slices.Clone(key), nil
}

// secret reads the key name, creating it when it does not exist yet.
func (s *Service) secret(ctx context.Context, name string) ([]byte, error) {
	var value string

	err := s.db.Get(ctx, &value, `SELECT value FROM wt_setting WHERE scope=? AND name=?`, secretScope, name)
	if err == nil {
		return hex.DecodeString(value)
	}

	if !errors.Is(err, db.ErrNotFound) {
		return nil, err
	}

	key := make([]byte, 32)
	_, _ = rand.Read(key)

	_, err = s.db.Exec(ctx,
		`INSERT INTO wt_setting(scope,name,value,updated_at,updated_by) VALUES(?,?,?,?,?)`,
		secretScope, name, hex.EncodeToString(key), time.Now().UTC(), db.Actor(ctx),
	)
	if db.IsUnique(err) {
		// Another process created the key first; use that one.
		return s.secret(ctx, name)
	}

	if err != nil {
		return nil, err
	}

	return key, nil
}

// SetTheme makes theme the active theme.
func (s *Service) SetTheme(ctx context.Context, theme string) error {
	settings, err := s.Settings(ctx)
	if err != nil {
		return err
	}

	settings.Theme = theme

	return s.Save(ctx, settings)
}

// pairs lists the tagged fields of target as settings in scope.
func pairs(scope string, target any) []setting {
	value := reflect.ValueOf(target).Elem()

	var out []setting

	for i := range value.NumField() {
		name := value.Type().Field(i).Tag.Get("setting")
		if name == "" {
			continue
		}

		field := value.Field(i)

		var text string

		switch field.Kind() {
		case reflect.Slice:
			text = strings.Join(field.Interface().([]string), ",")
		case reflect.Bool:
			text = strconv.FormatBool(field.Bool())
		case reflect.Int:
			text = strconv.FormatInt(field.Int(), 10)
		default:
			text = field.String()
		}

		out = append(out, setting{Scope: scope, Name: name, Value: text})
	}

	return out
}

// set assigns a stored value to the field tagged with name. Unknown names are
// ignored, so values written by newer versions do no harm.
func set(target any, name, text string) {
	value := reflect.ValueOf(target).Elem()

	for i := range value.NumField() {
		if value.Type().Field(i).Tag.Get("setting") != name {
			continue
		}

		field := value.Field(i)

		// Unreadable booleans and numbers are their zero value, the default.
		switch field.Kind() {
		case reflect.Slice:
			// An empty list is stored as "": no items, not one empty item.
			if text == "" {
				field.SetZero()
			} else {
				field.Set(reflect.ValueOf(strings.Split(text, ",")))
			}
		case reflect.Bool:
			on, _ := strconv.ParseBool(text)
			field.SetBool(on)
		case reflect.Int:
			n, _ := strconv.Atoi(text)
			field.SetInt(int64(n))
		default:
			field.SetString(text)
		}
	}
}

func clean(settings Settings) (Settings, error) {
	config, err := locales.Validate(settings.LocaleConfig())
	if err != nil {
		return settings, ErrInvalid
	}

	settings.DefaultLocale = config.Default
	settings.Locales = config.Enabled

	if err := cleanPerformance(&settings); err != nil {
		return settings, err
	}

	if err := cleanSecurity(&settings); err != nil {
		return settings, err
	}

	if err := cleanContentSecurity(&settings); err != nil {
		return settings, err
	}

	if err := cleanTrash(&settings); err != nil {
		return settings, err
	}

	if settings.FeedSize < 0 || settings.FeedSize > MaxFeedSize {
		return settings, ErrInvalid
	}

	settings.Review = cleanReview(settings.Review)

	location, err := time.LoadLocation(settings.Timezone)
	if err != nil || settings.Timezone == "" {
		return settings, ErrInvalid
	}

	settings.location = location

	settings.Email = strings.TrimSpace(settings.Email)
	if settings.Email != "" {
		if _, err := mail.ParseAddress(settings.Email); err != nil {
			return settings, ErrInvalid
		}
	}

	settings.Profiles, err = cleanProfiles(settings.Profiles)
	if err != nil {
		return settings, err
	}

	var translations []Translation

	for _, translation := range settings.Translations {
		locale, err := locales.Normalize(translation.Locale)
		if err != nil {
			return settings, ErrInvalid
		}

		// Translations of disabled languages are kept, so re-enabling restores them.
		translation.Locale = locale
		translation.Title = strings.TrimSpace(translation.Title)
		translations = append(translations, translation)
	}

	slices.SortFunc(translations, func(a, b Translation) int {
		return strings.Compare(a.Locale, b.Locale)
	})

	settings.Translations = slices.CompactFunc(translations, func(a, b Translation) bool {
		return a.Locale == b.Locale
	})

	return settings, nil
}
