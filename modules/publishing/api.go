package publishing

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/donseba/go-webthings/api"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-router"
)

// APIPost is a published post in the content API (see package api), at
// /api/v1/<key>/posts/{id} and in the list of /api/v1/<key>/posts, which
// leaves out BodyHTML.
type APIPost struct {
	ID      string `json:"id"`
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Excerpt string `json:"excerpt"`
	// BodyHTML is the post's text as HTML (api.HTML); only the post's own
	// endpoint has it.
	BodyHTML string `json:"body_html,omitempty"`
	// Date is the post's date; UpdatedAt when its published revision was
	// saved.
	Date      time.Time `json:"date"`
	UpdatedAt time.Time `json:"updated_at"`
	// URL is the post's page, null while the instance is on no page;
	// APIURL the post in the API.
	URL        *string       `json:"url"`
	APIURL     string        `json:"api_url"`
	Image      *api.Image    `json:"image"`
	Categories []APICategory `json:"categories"`
}

// APICategory is a published category in the content API, at
// /api/v1/<key>/categories and with its posts.
type APICategory struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
	// URL is the category's list on the site, null while the instance is on
	// no page; PostsURL its posts in the API.
	URL      *string `json:"url"`
	PostsURL string  `json:"posts_url"`
}

// apiRoutes are the instance's endpoints in the content API, below
// /api/v1/<key>: the published posts, newest first, which ?category=<slug>
// and ?slug=<slug> filter, a post by its ID and the published categories.
func (s *Service) apiRoutes() *router.Router {
	routes := api.NewRouter()

	routes.Get("/posts", s.apiPosts).As("posts")
	routes.Get("/posts/{id}", s.apiPost).As("post")
	routes.Get("/categories", s.apiCategories).As("categories")

	return routes
}

// apiPosts answers /posts: a page (api.Paginate) of the published posts.
func (s *Service) apiPosts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := api.Locale(r)

	items, err := s.posts.ListPublished(ctx, s.key, locale)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	categories, paths, err := s.apiCategoryList(r)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	query := r.URL.Query()
	category := ""

	if slug := query.Get("category"); slug != "" {
		for _, found := range categories {
			if found.Slug == slug {
				category = found.ID
			}
		}

		// The site's list of an unknown category is not found either.
		if category == "" {
			api.NotFound(w, r)

			return
		}
	}

	slices.SortStableFunc(items, newestFirst)

	slug := query.Get("slug")
	posts := []APIPost{}

	for _, item := range items {
		text, _ := item.Text(locale)

		if category != "" && !item.Fields.Categories.Has(category) {
			continue
		}

		if slug != "" && text.Slug != slug {
			continue
		}

		posts = append(posts, s.apiItem(r, item, paths, categories, false))
	}

	api.Write(w, r, api.Paginate(r, posts))
}

// apiPost answers /posts/{id}: a published post in the request's language.
func (s *Service) apiPost(w http.ResponseWriter, r *http.Request) {
	item, err := s.posts.Published(r.Context(), s.key, r.PathValue("id"), api.Locale(r))
	if errors.Is(err, content.ErrNotFound) {
		api.NotFound(w, r)

		return
	}

	if err != nil {
		api.Fail(w, r, err)

		return
	}

	categories, paths, err := s.apiCategoryList(r)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	modified, err := s.apiModified(r, item)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	api.Modified(w, modified...)
	api.Write(w, r, s.apiItem(r, item, paths, categories, true))
}

// apiModified are the dates of what the answer of /posts/{id} shows (see
// api.Modified): those of api.ItemDates and of the categories it is in,
// zero for one that was deleted.
func (s *Service) apiModified(r *http.Request, item PostItem) ([]time.Time, error) {
	text, _ := item.Text(api.Locale(r))
	modified := api.ItemDates(r, s.key, item, text.Body, item.Fields.Image)

	if len(item.Fields.Categories) == 0 {
		return modified, nil
	}

	categories, err := s.categories.List(r.Context(), s.key)
	if err != nil {
		return nil, err
	}

	dates := map[string]time.Time{}
	for _, category := range categories {
		dates[category.ID] = category.UpdatedAt
	}

	for _, id := range item.Fields.Categories {
		modified = append(modified, dates[id])
	}

	return modified, nil
}

// apiCategories answers /categories: a page of the published categories,
// in the editors' order.
func (s *Service) apiCategories(w http.ResponseWriter, r *http.Request) {
	categories, _, err := s.apiCategoryList(r)
	if err != nil {
		api.Fail(w, r, err)

		return
	}

	api.Write(w, r, api.Paginate(r, categories))
}

// apiCategoryList is the published categories in the request's language,
// in the editors' order, and the addresses of the instance's routes on its
// page in that language.
func (s *Service) apiCategoryList(r *http.Request) ([]APICategory, router.PathFunc, error) {
	ctx := r.Context()
	locale := api.Locale(r)

	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return nil, nil, err
	}

	paths := pages.Links(s.public, base)

	items, err := s.categories.List(ctx, s.key)
	if err != nil {
		return nil, nil, err
	}

	categories := []APICategory{}

	for _, item := range items {
		text, ok := item.Text(locale)
		if !item.Published || !ok || !categorySpec().Complete(text) {
			continue
		}

		address, _ := paths("category", "slug", text.Slug)
		postsURL, _ := s.api.RoutePathArgs("posts", url.Values{"category": {text.Slug}, "locale": {locale}})

		categories = append(categories, APICategory{
			ID:       item.ID,
			Slug:     text.Slug,
			Title:    text.Title,
			URL:      api.Optional(api.URL(r, address)),
			PostsURL: api.URL(r, postsURL),
		})
	}

	return categories, paths, nil
}

// apiItem is a published post in the request's language, with those of
// categories it is in; body adds its text.
func (s *Service) apiItem(r *http.Request, item PostItem, paths router.PathFunc, categories []APICategory, body bool) APIPost {
	locale := api.Locale(r)
	text, _ := item.Text(locale)

	// Published posts have a slug.
	address, _ := paths("post", "slug", text.Slug)
	self, _ := s.api.RoutePathArgs("post", "id", item.ID)

	post := APIPost{
		ID:         item.ID,
		Slug:       text.Slug,
		Title:      text.Title,
		Excerpt:    text.Excerpt,
		Date:       item.Fields.Date.UTC(),
		UpdatedAt:  item.Revision.CreatedAt.UTC(),
		URL:        api.Optional(api.URL(r, address)),
		APIURL:     api.URL(r, self+"?"+url.Values{"locale": {locale}}.Encode()),
		Image:      api.ImageOf(r, item.Fields.Image),
		Categories: []APICategory{},
	}

	if body {
		post.BodyHTML = api.HTML(r, text.Body)
	}

	for _, category := range categories {
		if item.Fields.Categories.Has(category.ID) {
			post.Categories = append(post.Categories, category)
		}
	}

	return post
}
