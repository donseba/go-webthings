// Package directory is a categorised directory: organisations, shops,
// venues or a neighbourhood guide. Categories have a translated name and
// summary and are ordered by hand. Entries belong to one category and have a
// name, a slug, a translated summary and Markdown description, search
// keywords, a logo or image, contact details, an address with an optional
// map position and weekly opening hours. Add an instance with:
//
//	app.Use(directory.Module("guide", "Neighbourhood guide"))
//
// Names and slugs are not translated: an organisation has the same name and
// URL in every language. Categories and entries show in a language once they
// are saved in it, and an entry only shows while its category is published. A
// category with entries cannot be deleted.
//
// # Pages
//
// Below the page the module is mounted on it serves:
//
//	/                      directory/list.gohtml      ListView
//	/?q=bakery             the same, searched
//	/?category=food        the same, one category
//	/<category>            directory/category.gohtml  CategoryView
//	/<category>/<entry>    directory/entry.gohtml     Entry
//
// The search matches entries whose name, summary or keywords contain every
// word of q, ignoring case and accents like the site search. A search form is
// a plain GET form:
//
//	<form action="{{.URL}}"><input type="search" name="q" value="{{.Query}}"></form>
//
// sitemap.xml lists the page the module is mounted on (like every page) and
// the address of every published category and entry; searches and filtered
// lists are left out. The site search finds entries by name, summary, keywords
// and description, and signed-in editors get a link to the entry or category
// in the admin.
//
// # Template functions
//
// Every template can show the directory without mounting it:
//
//	{{range directoryCategories "guide"}}<a href="{{.URL}}">{{.Name}} ({{.Count}})</a>{{end}}
//	{{range directoryEntries "guide" "food" 5}}<a href="{{.URL}}">{{.Name}}</a>{{end}}
//	{{with directoryEntry "guide" "bakery-jansen"}}{{.Name}}{{if .OpenNow}} · open{{end}}{{end}}
//
// directoryEntries takes a category slug ("" for all categories) and a limit
// (0 or less for no limit). URLs point to the page the module is mounted on;
// while the module is on no page in the current language they are empty.
//
// # Opening hours
//
// Entry.Hours is the weekly schedule and Entry.OpenNow tells whether the entry
// is open while the page renders, in the site's timezone:
//
//	{{if .Hours.Known}}{{range .Hours.Week}}{{.Weekday}}: {{if .Closed}}closed{{else}}{{.Open}}–{{.Close}}{{end}}{{end}}{{end}}
//
// # Import
//
// The admin imports entries from a CSV file whose first row names the
// columns: name, category, summary, website, email, phone, street, postcode,
// city and keywords; name and category are required, others may be left out.
// Commas, semicolons and tabs separate the values. Missing categories are
// created. Text columns are stored in the site's default language.
//
// "Publish new entries and categories" publishes what the import creates. It
// never publishes an existing category: an editor may keep a category hidden
// on purpose, so entries imported into an unpublished category stay off the
// site until the category is published.
package directory

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/pages"
	"github.com/donseba/go-webthings/site"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// CategoryFields are the language-independent parts of a category.
type CategoryFields struct {
	Slug string `db:"slug"`
}

// CategoryText is a category in one language.
type CategoryText struct {
	Name    string `db:"name"`
	Summary string `db:"summary"`
}

// EntryFields are the language-independent parts of an entry. Website is a
// full http(s) URL. Latitude and Longitude are both set or both empty.
type EntryFields struct {
	Category  string          `db:"category_id,null"`
	Slug      string          `db:"slug"`
	Name      string          `db:"name"`
	Image     string          `db:"image_id,null"`
	Website   string          `db:"website"`
	Email     string          `db:"email"`
	Phone     string          `db:"phone"`
	Street    string          `db:"street"`
	Postcode  string          `db:"postcode"`
	City      string          `db:"city"`
	Latitude  sql.NullFloat64 `db:"latitude"`
	Longitude sql.NullFloat64 `db:"longitude"`
	Hours     OpeningHours    `db:"opening_hours"`
}

// EntryText is an entry in one language. Description is Markdown; Keywords
// are comma-separated words the search also matches.
type EntryText struct {
	Summary     string `db:"summary"`
	Description string `db:"description"`
	Keywords    string `db:"keywords"`
	HoursNote   string `db:"hours_note"`
}

type (
	// CategoryItem is a category with its translations, as the store keeps
	// it.
	CategoryItem = content.Item[CategoryFields, CategoryText]
	// EntryItem is an entry with its translations, as the store keeps it.
	EntryItem = content.Item[EntryFields, EntryText]
)

var (
	errName     = errors.New("directory_name_missing")
	errSlug     = errors.New("directory_invalid_slug")
	errEmail    = errors.New("directory_invalid_email")
	errWebsite  = errors.New("directory_invalid_website")
	errLocation = errors.New("directory_invalid_location")
	errHours    = errors.New("directory_invalid_hours")
	errCategory = errors.New("directory_category_missing")
	// errCategoryUsed refuses deleting a category that live entries are
	// in: they would disappear from the site.
	errCategoryUsed = errors.New("directory_category_has_entries")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func categorySpec() content.Spec[CategoryFields, CategoryText] {
	return content.Spec[CategoryFields, CategoryText]{
		Table: "wt_directory_category",
		// Deletable is set by deletableCategories, which needs the database.
		Complete: func(text CategoryText) bool {
			return text.Name != ""
		},
		Check: func(fields CategoryFields, translations []content.Translation[CategoryText]) error {
			if !validSlug(fields.Slug) {
				return errSlug
			}

			for _, translation := range translations {
				if content.TooLong(200, translation.Text.Name) || content.TooLong(2000, translation.Text.Summary) {
					return content.ErrTooLong
				}
			}

			return nil
		},
	}
}

// deletableCategories refuses moving a category to the trash while live
// entries are in it. Entries in the trash keep their category too: the
// foreign key refuses purging it until they are purged.
func deletableCategories(
	database *db.DB,
	spec content.Spec[CategoryFields, CategoryText],
) content.Spec[CategoryFields, CategoryText] {
	spec.Deletable = func(ctx context.Context, id string) error {
		var count int

		err := database.Get(ctx, &count,
			`SELECT COUNT(*) FROM wt_directory_entry WHERE category_id=? AND deleted_at IS NULL`,
			id,
		)
		if err != nil {
			return err
		}

		if count > 0 {
			return errCategoryUsed
		}

		return nil
	}

	return spec
}

func entrySpec() content.Spec[EntryFields, EntryText] {
	return content.Spec[EntryFields, EntryText]{
		Table: "wt_directory_entry",
		// The name is not translated, so any saved language can be shown.
		Complete: func(EntryText) bool {
			return true
		},
		Check: checkEntry,
		Media: func(fields EntryFields, translations []content.Translation[EntryText]) []string {
			ids := []string{fields.Image}

			for _, translation := range translations {
				ids = append(ids, markdown.MediaIDs(translation.Text.Description)...)
			}

			return ids
		},
	}
}

func checkEntry(fields EntryFields, translations []content.Translation[EntryText]) error {
	if strings.TrimSpace(fields.Name) == "" {
		return errName
	}

	// An entry without a category is never shown, so it is refused for the
	// admin, imports and Store alike.
	if fields.Category == "" {
		return errCategory
	}

	if !validSlug(fields.Slug) {
		return errSlug
	}

	if content.TooLong(200, fields.Name, fields.Street) || content.TooLong(500, fields.Website) ||
		content.TooLong(254, fields.Email) {
		return content.ErrTooLong
	}

	if content.TooLong(50, fields.Phone) || content.TooLong(20, fields.Postcode) || content.TooLong(100, fields.City) {
		return content.ErrTooLong
	}

	if fields.Email != "" && !mail.Valid(fields.Email) {
		return errEmail
	}

	if fields.Website != "" && !validWebsite(fields.Website) {
		return errWebsite
	}

	if !validLocation(fields.Latitude, fields.Longitude) {
		return errLocation
	}

	if !fields.Hours.valid() {
		return errHours
	}

	for _, translation := range translations {
		text := translation.Text

		if content.TooLong(2000, text.Summary) || content.TooLong(256<<10, text.Description) {
			return content.ErrTooLong
		}

		if content.TooLong(1000, text.Keywords) || content.TooLong(500, text.HoursNote) {
			return content.ErrTooLong
		}
	}

	return nil
}

func validSlug(slug string) bool {
	return len(slug) <= 190 && slugPattern.MatchString(slug)
}

// validWebsite accepts an absolute http(s) address.
func validWebsite(website string) bool {
	return form.ValidURL("http,https", website)
}

func validLocation(latitude, longitude sql.NullFloat64) bool {
	if latitude.Valid != longitude.Valid {
		return false
	}

	if !latitude.Valid {
		return true
	}

	return latitude.Float64 >= -90 && latitude.Float64 <= 90 && longitude.Float64 >= -180 && longitude.Float64 <= 180
}

// Service is one Directory instance, for the website's own code.
type Service struct {
	key        string
	entries    *content.Collection[EntryFields, EntryText]
	categories *content.Collection[CategoryFields, CategoryText]
	frontend   *frontend.Frontend
	pages      *pages.Service
	site       *site.Service
	locales    locales.Provider
	// adminURL builds admin addresses by route name (webthings.Env.AdminURL).
	adminURL func(name string, args ...any) string
	// public are the routes below the module's page (see handler), api
	// those of its endpoints in the content API (see apiRoutes).
	public *router.Router
	api    *router.Router
}

// Store gives write access to this instance's entries, for imports and seeds.
// Admin edits go through the same store.
func (s *Service) Store() content.Store[EntryFields, EntryText] {
	return s.entries.In(s.key)
}

// Categories gives write access to this instance's categories.
func (s *Service) Categories() content.Store[CategoryFields, CategoryText] {
	return s.categories.In(s.key)
}

// Of returns the Directory instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// Category is a published category as templates see it.
type Category struct {
	ID      string
	Name    string
	Slug    string
	Summary string
	URL     string
	// Count is the number of published entries in the category.
	Count int
	// Active is set on the category the list is filtered by.
	Active bool
}

// Entry is a published entry as templates see it.
type Entry struct {
	ID          string
	Name        string
	Slug        string
	URL         string
	Category    Category
	Summary     string
	Description template.HTML
	Keywords    []string
	// ImageURL is the entry's logo or image as pages show it, at most media.ImageWidth
	// wide (see frontend.Frontend.Image); ImageSrcset lists smaller
	// copies for a srcset attribute, ImageWidth and ImageHeight are the
	// size of ImageURL and ImageOriginalURL is the uploaded file, for
	// downloads. All are empty when the entry has none.
	ImageURL         string
	ImageSrcset      string
	ImageOriginalURL string
	ImageWidth       int
	ImageHeight      int
	Website          string
	Email            string
	Phone            string
	Street           string
	Postcode         string
	City             string
	// HasLocation is set when Latitude and Longitude are known.
	HasLocation bool
	Latitude    float64
	Longitude   float64
	Hours       OpeningHours
	HoursNote   string
	// OpenNow is Hours.OpenNow at the time the page renders, in the site's
	// timezone.
	OpenNow bool
}

// Group is a category with its entries on the list page.
type Group struct {
	Category Category
	Entries  []Entry
}

// ListView is the data of directory/list.gohtml.
type ListView struct {
	// Title is the title of the page the module is mounted on.
	Title string
	// URL is the list's address, the action of a search form.
	URL string
	// Query is the search (?q=); empty shows every entry.
	Query string
	// Category is set when the list is filtered (?category=).
	Category *Category
	// Categories are all categories with entries, for a filter menu.
	Categories []Category
	// Groups hold the matching entries per category, in category order.
	Groups []Group
	// Count is the number of matching entries.
	Count int
}

// CategoryView is the data of directory/category.gohtml.
type CategoryView struct {
	Category Category
	Entries  []Entry
	// ListURL links back to the whole directory.
	ListURL string
}

// Query selects entries; the zero Query selects every published entry.
type Query struct {
	// Search holds words that must all appear in the name, summary or
	// keywords. Case and accents are ignored, as in the site search.
	Search string
	// Category is a category slug.
	Category string
}

// directory is the published content of an instance in one language.
type directory struct {
	categories []Category
	// entries are sorted by name and only in published categories.
	entries []Entry
}

// load reads the published directory in locale, its URLs by route name
// from paths.
func (s *Service) load(ctx context.Context, locale string, paths router.PathFunc) (directory, error) {
	categoryItems, err := s.categories.ListPublished(ctx, s.key, locale)
	if err != nil {
		return directory{}, err
	}

	entryItems, err := s.entries.ListPublished(ctx, s.key, locale)
	if err != nil {
		return directory{}, err
	}

	var (
		result directory
		index  = map[string]int{}
		now    = s.now(ctx)
	)

	for _, item := range categoryItems {
		text, _ := item.Text(locale)
		index[item.ID] = len(result.categories)

		category := Category{
			ID:      item.ID,
			Name:    text.Name,
			Slug:    item.Fields.Slug,
			Summary: text.Summary,
		}

		// Published categories have a slug.
		category.URL, _ = paths("category", "category", item.Fields.Slug)

		result.categories = append(result.categories, category)
	}

	sortByName(entryItems)

	for _, item := range entryItems {
		position, ok := index[item.Fields.Category]
		if !ok {
			continue
		}

		result.categories[position].Count++
		result.entries = append(result.entries, s.entry(ctx, item, locale, result.categories[position], now, paths))
	}

	return result, nil
}

func (s *Service) entry(
	ctx context.Context,
	item EntryItem,
	locale string,
	category Category,
	now time.Time,
	paths router.PathFunc,
) Entry {
	text, _ := item.Text(locale)
	fields := item.Fields

	// Published entries have a slug.
	address, _ := paths("entry", "category", category.Slug, "entry", fields.Slug)
	shown := s.frontend.Image(ctx, fields.Image)

	return Entry{
		ID:               item.ID,
		Name:             fields.Name,
		Slug:             fields.Slug,
		URL:              address,
		Category:         category,
		Summary:          text.Summary,
		Description:      s.frontend.Markdown(ctx, text.Description),
		Keywords:         keywords(text.Keywords),
		ImageURL:         shown.URL,
		ImageSrcset:      shown.Srcset,
		ImageOriginalURL: shown.OriginalURL,
		ImageWidth:       shown.Width,
		ImageHeight:      shown.Height,
		Website:          fields.Website,
		Email:            fields.Email,
		Phone:            fields.Phone,
		Street:           fields.Street,
		Postcode:         fields.Postcode,
		City:             fields.City,
		HasLocation:      fields.Latitude.Valid,
		Latitude:         fields.Latitude.Float64,
		Longitude:        fields.Longitude.Float64,
		Hours:            fields.Hours,
		HoursNote:        text.HoursNote,
		OpenNow:          fields.Hours.OpenNow(now),
	}
}

// now is the current time in the site's timezone, for opening hours.
func (s *Service) now(ctx context.Context) time.Time {
	// Without settings the time is UTC, as for an unknown timezone, never
	// the server's own timezone.
	settings, err := s.site.Settings(ctx)
	if err != nil {
		return time.Now().UTC()
	}

	return time.Now().In(settings.Location())
}

// category returns the category with slug.
func (d directory) category(slug string) (Category, bool) {
	for _, category := range d.categories {
		if category.Slug == slug {
			return category, true
		}
	}

	return Category{}, false
}

// find returns the entries in category (a slug, or "" for all) that match
// every word of search.
func (d directory) find(query Query) []Entry {
	search := frontend.ParseQuery(query.Search)

	var found []Entry

	for _, entry := range d.entries {
		if query.Category != "" && entry.Category.Slug != query.Category {
			continue
		}

		if matches(entry, search) {
			found = append(found, entry)
		}
	}

	return found
}

// matches reports whether every word of search appears in the entry's name,
// summary or keywords; an empty search matches every entry. Matching happens
// in Go rather than SQL with the site search's folding, so case and accents
// are ignored the same way for every database and every alphabet.
func matches(entry Entry, search frontend.Query) bool {
	if search.Empty() {
		return true
	}

	return search.Score(entry.Name, entry.Summary, strings.Join(entry.Keywords, "\n")) > 0
}

// Entries returns the published entries in locale that match query, sorted by
// name, for use outside the module's own pages. URLs are empty while the
// module is on no page in locale.
func (s *Service) Entries(ctx context.Context, locale string, query Query) ([]Entry, error) {
	// Outside the module's own pages the links point to the page it is on;
	// while it is on none, entries and categories get no address instead of
	// one that does not exist.
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	loaded, err := s.load(ctx, locale, pages.Links(s.public, base))
	if err != nil {
		return nil, err
	}

	return loaded.find(query), nil
}

// Entry returns the published entry with slug in locale, or content.ErrNotFound.
func (s *Service) Entry(ctx context.Context, locale, slug string) (Entry, error) {
	entries, err := s.Entries(ctx, locale, Query{})
	if err != nil {
		return Entry{}, err
	}

	for _, entry := range entries {
		if entry.Slug == slug {
			return entry, nil
		}
	}

	return Entry{}, content.ErrNotFound
}

// ListCategories returns the published categories in locale in their order,
// with the number of entries in each. URLs are empty while the module is on
// no page in locale.
func (s *Service) ListCategories(ctx context.Context, locale string) ([]Category, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	loaded, err := s.load(ctx, locale, pages.Links(s.public, base))
	if err != nil {
		return nil, err
	}

	return loaded.categories, nil
}

// keywords splits comma-separated keywords.
func keywords(list string) []string {
	var out []string

	for keyword := range strings.SplitSeq(list, ",") {
		if keyword = strings.TrimSpace(keyword); keyword != "" {
			out = append(out, keyword)
		}
	}

	return out
}

// sortByName orders entries by name, ignoring case.
func sortByName(items []EntryItem) {
	slices.SortStableFunc(items, func(a, b EntryItem) int {
		return strings.Compare(strings.ToLower(a.Fields.Name), strings.ToLower(b.Fields.Name))
	})
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
