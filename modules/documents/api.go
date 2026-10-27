package documents

import (
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/donseba/go-webthings/api"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-router"
)

// APIDocument is a published document in the content API (see package
// api), at /api/v1/<key>/documents/{id} and in the list of
// /api/v1/<key>/documents. Documents have no page of their own: the list
// shows them whole, so the list has their description too.
type APIDocument struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	DescriptionHTML string `json:"description_html"`
	// Category is null for a document without a category.
	Category *APICategory `json:"category"`
	// Date is the document's own date, such as that of a meeting, at
	// midnight UTC.
	Date time.Time `json:"date"`
	// DownloadURL is the site's download address of the document, which
	// counts the download and leads to the file or the external link, as
	// on the site. External is set for a link, which has no File.
	DownloadURL string   `json:"download_url"`
	External    bool     `json:"external"`
	File        *APIFile `json:"file"`
	// URL is the document on the list of its category, or on the whole
	// list (#<id>), null while the instance is on no page; APIURL the
	// document in the API.
	URL       *string   `json:"url"`
	APIURL    string    `json:"api_url"`
	UpdatedAt time.Time `json:"updated_at"`
}

// APIFile is the file of a document from the media library.
type APIFile struct {
	Filename string `json:"filename"`
	MIMEType string `json:"mime_type"`
	// FileType is the file's extension in capitals, such as "PDF"; Size is
	// in bytes.
	FileType string `json:"file_type"`
	Size     int64  `json:"size"`
}

// APICategory is a category of published documents in one language, at
// /api/v1/<key>/categories and with each of its documents. Categories are
// the documents' category names; the slug is made from the name.
type APICategory struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	// DocumentCount is the number of published documents in the category.
	DocumentCount int `json:"document_count"`
	// URL is the category's list on the site, null while the instance is
	// on no page; DocumentsURL its documents in the API.
	URL          *string `json:"url"`
	DocumentsURL string  `json:"documents_url"`
}

// apiContent is the published documents in a request's language, newest
// first, as items and as the site shows them, their categories in
// alphabetical order and the address of the instance's page in that
// language, empty for none.
type apiContent struct {
	base       string
	items      []Item
	documents  []Document
	categories []APICategory
}

// apiRoutes are the instance's endpoints in the content API, below
// /api/v1/<key>: the published documents, newest first, which
// ?category=<slug> filters, a document by its ID and the categories.
func (s *Service) apiRoutes() *router.Router {
	routes := api.NewRouter()

	routes.Get("/documents", s.apiDocuments).As("documents")
	routes.Get("/documents/{id}", s.apiDocument).As("document")
	routes.Get("/categories", s.apiCategories).As("categories")

	return routes
}

// apiDocuments answers /documents: a page (api.Paginate) of the published
// documents.
func (s *Service) apiDocuments(w http.ResponseWriter, r *http.Request) {
	loaded, err := s.apiLoad(r)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	category := r.URL.Query().Get("category")

	// The site's list of an unknown category is not found either.
	if category != "" && loaded.category(category) == nil {
		api.NotFound(w, r)

		return
	}

	documents := []APIDocument{}

	for i, item := range loaded.items {
		document := s.apiItem(r, item, loaded.documents[i], loaded)

		if category != "" && (document.Category == nil || document.Category.Slug != category) {
			continue
		}

		documents = append(documents, document)
	}

	api.Write(w, r, api.Paginate(r, documents))
}

// apiDocument answers /documents/{id}: a published document in the
// request's language. It has no Last-Modified (see api.Modified): its
// category holds the number of published documents in it, which another
// document changes without a date this answer shows.
func (s *Service) apiDocument(w http.ResponseWriter, r *http.Request) {
	loaded, err := s.apiLoad(r)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	id := r.PathValue("id")

	for i, item := range loaded.items {
		if item.ID == id {
			api.Write(w, r, s.apiItem(r, item, loaded.documents[i], loaded))

			return
		}
	}

	api.NotFound(w, r)
}

// apiCategories answers /categories: a page of the categories of the
// published documents, in alphabetical order.
func (s *Service) apiCategories(w http.ResponseWriter, r *http.Request) {
	loaded, err := s.apiLoad(r)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	api.Write(w, r, api.Paginate(r, loaded.categories))
}

// apiLoad reads the published documents in the request's language, newest
// first as on the site, with their categories and the addresses of the
// instance's routes on its page in that language.
func (s *Service) apiLoad(r *http.Request) (apiContent, error) {
	ctx := r.Context()
	locale := api.Locale(r)

	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return apiContent{}, err
	}

	paths := pages.Links(s.public, base)

	items, err := s.items.ListPublished(ctx, s.key, locale)
	if err != nil {
		return apiContent{}, err
	}

	slices.SortStableFunc(items, func(a, b Item) int {
		return b.Fields.Date.Compare(a.Fields.Date)
	})

	loaded := apiContent{base: base, items: items, categories: []APICategory{}}
	assets := map[string]media.Asset{}

	for _, item := range items {
		document, err := s.document(ctx, item, locale, paths, assets)
		if err != nil {
			return apiContent{}, err
		}

		loaded.documents = append(loaded.documents, document)
	}

	for _, category := range categories(loaded.documents, paths, "") {
		documents := s.api.RoutePath("documents") + "?" + url.Values{"category": {category.Slug}, "locale": {locale}}.Encode()

		loaded.categories = append(loaded.categories, APICategory{
			Slug:          category.Slug,
			Name:          category.Name,
			DocumentCount: category.Count,
			URL:           api.Optional(api.URL(r, category.URL)),
			DocumentsURL:  api.URL(r, documents),
		})
	}

	return loaded, nil
}

// category is the category with slug, or nil.
func (c apiContent) category(slug string) *APICategory {
	for i := range c.categories {
		if c.categories[i].Slug == slug {
			return &c.categories[i]
		}
	}

	return nil
}

// apiItem is a published document in the request's language; document is
// how the site shows it.
func (s *Service) apiItem(r *http.Request, item Item, document Document, loaded apiContent) APIDocument {
	locale := api.Locale(r)
	text, _ := item.Text(locale)
	self, _ := s.api.RoutePathArgs("document", "id", item.ID)

	result := APIDocument{
		ID:              item.ID,
		Title:           text.Title,
		DescriptionHTML: api.HTML(r, text.Description),
		Category:        loaded.category(content.Slug(text.Category)),
		Date:            item.Fields.Date.UTC(),
		DownloadURL:     api.URL(r, document.URL),
		External:        document.External,
		APIURL:          api.URL(r, self+"?"+url.Values{"locale": {locale}}.Encode()),
		UpdatedAt:       item.UpdatedAt.UTC(),
	}

	if document.Filename != "" {
		result.File = &APIFile{
			Filename: document.Filename,
			MIMEType: document.MIMEType,
			FileType: document.FileType,
			Size:     document.Size,
		}
	}

	// As in the site search: on the list of its category, or on the whole
	// list.
	list := document.CategoryURL
	if list == "" {
		list = loaded.base
	}

	if list != "" {
		result.URL = api.Optional(api.URL(r, list+"#"+item.ID))
	}

	return result
}
