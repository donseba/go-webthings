// Package documents offers files for download: minutes, reports, forms and
// policies. Each document has a date, a file from the media library or a link
// to an external address, and per language a title, a category and a
// Markdown description. Downloads are counted. Add an instance with:
//
//	app.Use(documents.Module("documents", "Documents"))
//
// Upload files in the media library first and choose them in the document
// editor. When the module is mounted on a page it serves the list at "/" and
// the list of one category at "/category/{slug}"; the category slug is made
// from the category name. The website renders:
//
//	documents/list.gohtml  ListView: documents newest first, also per year,
//	                       and the categories to filter on
//
// Document.URL is the download address /_/<key>/<id>/download. It counts the
// download and redirects to the file or the external link, so it works on
// every page, also where the module is not mounted. Templates anywhere on
// the site can list documents without Go code:
//
//	{{range documents "documents" 5}}<a href="{{.URL}}">{{.Title}}</a> {{.FileType}} {{.SizeText}}{{end}}
//	{{range documentsIn "documents" "Minutes"}}{{.Date.Format "2 January 2006"}} {{.Title}}{{end}}
//
// documentsIn matches the category name in the current language, ignoring
// case, or the category slug.
//
// The site search finds published documents by title, description and
// category once the module is on a page. A result links to the document on
// the list (of its category), at #<ID>: give each document id="{{.ID}}".
package documents

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/pages"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// Fields are the language-independent parts of a document. A document has
// either a File or a Link. Date is the document's own date, for example the
// date of a meeting, stored as midnight UTC of that day.
type Fields struct {
	File string    `db:"file_id,null"`
	Link string    `db:"link"`
	Date time.Time `db:"document_date"`
}

// Text is a document in one language. Description is Markdown.
type Text struct {
	Title       string `db:"title"`
	Description string `db:"description"`
	Category    string `db:"category"`
}

// Item is a document with its translations, as the store keeps it.
type Item = content.Item[Fields, Text]

var (
	errSource = errors.New("documents_file_or_link")
	errLink   = errors.New("documents_invalid_link")
	errDate   = errors.New("documents_invalid_date")
)

func spec() content.Spec[Fields, Text] {
	return content.Spec[Fields, Text]{
		Table: "wt_document",
		Complete: func(text Text) bool {
			return text.Title != ""
		},
		Check: check,
		Media: func(fields Fields, translations []content.Translation[Text]) []string {
			ids := []string{fields.File}

			for _, translation := range translations {
				ids = append(ids, markdown.MediaIDs(translation.Text.Description)...)
			}

			return ids
		},
	}
}

func check(fields Fields, translations []content.Translation[Text]) error {
	if (fields.File == "") == (fields.Link == "") {
		return errSource
	}

	if fields.Link != "" && !validLink(fields.Link) {
		return errLink
	}

	if fields.Date.IsZero() {
		return errDate
	}

	for _, translation := range translations {
		text := translation.Text

		if content.TooLong(200, text.Title, text.Category) || content.TooLong(256<<10, text.Description) {
			return content.ErrTooLong
		}
	}

	return nil
}

// validLink accepts absolute http(s) addresses of at most 2000 characters.
func validLink(link string) bool {
	return !content.TooLong(2000, link) && form.ValidURL("http,https", link)
}

// Service is one Documents instance, for the website's own code.
type Service struct {
	key      string
	db       *db.DB
	items    *content.Collection[Fields, Text]
	media    *media.Service
	frontend *frontend.Frontend
	pages    *pages.Service
	// adminURL builds admin addresses by route name (webthings.Env.AdminURL).
	adminURL func(name string, args ...any) string
	// actions serve the downloads (webthings.Env.Actions).
	actions *router.Router
	// public are the routes below the module's page (see handler), api
	// those of its endpoints in the content API (see apiRoutes).
	public *router.Router
	api    *router.Router
}

// Store gives write access to this instance's documents, for imports and
// seeds. Admin edits go through the same store.
func (s *Service) Store() content.Store[Fields, Text] {
	return s.items.In(s.key)
}

// Of returns the Documents instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// downloadURL is the download address of the document with id: the action
// named download.
func (s *Service) downloadURL(id string) string {
	// The routes are the instance's own, so the name is known.
	path, _ := s.actions.RoutePathArgs("download", "id", id)

	return path
}

// Document is a published document as templates see it.
type Document struct {
	ID          string
	Title       string
	Description template.HTML
	Category    string
	// CategoryURL lists the document's category, empty without a category or
	// when the module is not mounted.
	CategoryURL string
	// Date is a calendar date (see frontend.Civil): {{date .Date}} shows the
	// same day in every site timezone.
	Date time.Time
	// URL counts a download and redirects to the file or the link.
	URL string
	// External is true for a link instead of a file; the file details below
	// are then empty.
	External bool
	Filename string
	MIMEType string
	// FileType is the file's extension in capitals, for example "PDF".
	FileType string
	Size     int64
	// SizeText is Size for people, for example "1.2 MB".
	SizeText  string
	Downloads int
}

// Category is a category with published documents in one language.
type Category struct {
	Name   string
	Slug   string
	URL    string
	Count  int
	Active bool
}

// Year holds the documents of one year, newest first.
type Year struct {
	Year      int
	Documents []Document
}

// ListView is the data of documents/list.gohtml.
type ListView struct {
	// Title is the title of the page the module is mounted on, or the
	// category name when the list is filtered.
	Title string
	// Documents are newest first, filtered by Category when it is set.
	Documents []Document
	// Years hold the same documents per year, newest first.
	Years []Year
	// Categories are all categories with documents, in alphabetical order.
	Categories []Category
	// Category is the active filter, or nil.
	Category *Category
	// AllURL lists all documents.
	AllURL string
}

// Latest returns up to limit published documents in locale, newest first. A
// limit of 0 or less returns all of them.
func (s *Service) Latest(ctx context.Context, locale string, limit int) ([]Document, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	documents, err := s.list(ctx, locale, pages.Links(s.public, base))
	if err != nil {
		return nil, err
	}

	if limit > 0 && limit < len(documents) {
		documents = documents[:limit]
	}

	return documents, nil
}

// InCategory returns the published documents in locale whose category name
// is category, ignoring case, or whose category slug is category.
func (s *Service) InCategory(ctx context.Context, locale, category string) ([]Document, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	documents, err := s.list(ctx, locale, pages.Links(s.public, base))
	if err != nil {
		return nil, err
	}

	return filter(documents, category), nil
}

// Download counts a download of a published document and returns where it
// is: the file's URL or the external link. It returns content.ErrNotFound
// for unknown and unpublished documents.
func (s *Service) Download(ctx context.Context, id string) (string, error) {
	target, err := s.target(ctx, id)
	if err != nil {
		return "", err
	}

	err = db.Affected(s.db.Exec(ctx,
		`UPDATE wt_document SET downloads=downloads+1 WHERE id=? AND instance=? AND published=? AND deleted_at IS NULL`,
		id, s.key, true,
	))
	if err != nil {
		return "", err
	}

	return target, nil
}

// target is where a published document's download leads.
func (s *Service) target(ctx context.Context, id string) (string, error) {
	item, err := s.items.Get(ctx, s.key, id)
	if err != nil {
		return "", err
	}

	if !item.Published {
		return "", content.ErrNotFound
	}

	if item.Fields.Link != "" {
		return item.Fields.Link, nil
	}

	target := s.frontend.MediaURL(ctx, item.Fields.File)
	if target == "" {
		return "", content.ErrNotFound
	}

	return target, nil
}

// list returns the published documents in locale, newest first, and in
// list order for the same date; category URLs are by route name from paths.
func (s *Service) list(ctx context.Context, locale string, paths router.PathFunc) ([]Document, error) {
	items, err := s.items.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	slices.SortStableFunc(items, func(a, b Item) int {
		return b.Fields.Date.Compare(a.Fields.Date)
	})

	counts, err := s.downloads(ctx)
	if err != nil {
		return nil, err
	}

	assets := map[string]media.Asset{}
	documents := make([]Document, 0, len(items))

	for _, item := range items {
		document, err := s.document(ctx, item, locale, paths, assets)
		if err != nil {
			return nil, err
		}

		document.Downloads = counts[item.ID]
		documents = append(documents, document)
	}

	return documents, nil
}

// downloads returns the download count per document of this instance. The
// count is not one of the Fields, so editor saves never reset it.
func (s *Service) downloads(ctx context.Context) (map[string]int, error) {
	var rows []struct {
		ID        string `db:"id"`
		Downloads int    `db:"downloads"`
	}

	err := s.db.Select(ctx, &rows, `SELECT id, downloads FROM wt_document WHERE instance=?`, s.key)
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int, len(rows))

	for _, row := range rows {
		counts[row.ID] = row.Downloads
	}

	return counts, nil
}

// document builds the template view of an item, its category URL by route
// name from paths. assets caches media lookups within one list.
func (s *Service) document(
	ctx context.Context,
	item Item,
	locale string,
	paths router.PathFunc,
	assets map[string]media.Asset,
) (Document, error) {
	text, _ := item.Text(locale)
	fields := item.Fields

	document := Document{
		ID:          item.ID,
		Title:       text.Title,
		Description: s.frontend.Markdown(ctx, text.Description),
		Category:    text.Category,
		Date:        frontend.Civil(fields.Date.UTC()),
		URL:         s.downloadURL(item.ID),
		External:    fields.Link != "",
	}

	if slug := content.Slug(text.Category); slug != "" {
		document.CategoryURL, _ = paths("category", "slug", slug)
	}

	if document.External {
		return document, nil
	}

	asset, ok := assets[fields.File]
	if !ok {
		var err error

		asset, err = s.media.Get(ctx, fields.File)
		if errors.Is(err, media.ErrNotFound) {
			return document, nil
		}

		if err != nil {
			return Document{}, err
		}

		assets[fields.File] = asset
	}

	document.Filename = asset.Filename
	document.MIMEType = asset.MIMEType
	document.FileType = strings.ToUpper(strings.TrimPrefix(path.Ext(asset.Filename), "."))
	document.Size = asset.SizeBytes
	document.SizeText = sizeText(asset.SizeBytes)

	return document, nil
}

// filter keeps the documents in category, by name (ignoring case) or slug.
func filter(documents []Document, category string) []Document {
	category = strings.TrimSpace(category)
	slug := content.Slug(category)

	var out []Document

	for _, document := range documents {
		if document.Category == "" {
			continue
		}

		if strings.EqualFold(document.Category, category) || content.Slug(document.Category) == slug {
			out = append(out, document)
		}
	}

	return out
}

// categories lists the categories of documents by slug, in alphabetical
// order, their URLs by route name from paths. Names that differ only in case
// or punctuation are one category.
func categories(documents []Document, paths router.PathFunc, active string) []Category {
	var (
		out   []Category
		index = map[string]int{}
	)

	for _, document := range documents {
		slug := content.Slug(document.Category)
		if slug == "" {
			continue
		}

		i, ok := index[slug]
		if !ok {
			i = len(out)
			index[slug] = i

			url, _ := paths("category", "slug", slug)

			out = append(out, Category{
				Name:   document.Category,
				Slug:   slug,
				URL:    url,
				Active: slug == active,
			})
		}

		out[i].Count++
	}

	slices.SortFunc(out, func(a, b Category) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})

	return out
}

// years groups documents, newest first, per year.
func years(documents []Document) []Year {
	var out []Year

	for _, document := range documents {
		year := document.Date.Year()

		if len(out) == 0 || out[len(out)-1].Year != year {
			out = append(out, Year{Year: year})
		}

		last := &out[len(out)-1]
		last.Documents = append(last.Documents, document)
	}

	return out
}

// sizeText shows a file size in bytes, kB or MB.
func sizeText(size int64) string {
	switch {
	case size < 1000:
		return fmt.Sprintf("%d B", size)
	case size < 1000*1000:
		return fmt.Sprintf("%.0f kB", float64(size)/1000)
	default:
		return fmt.Sprintf("%.1f MB", float64(size)/(1000*1000))
	}
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
