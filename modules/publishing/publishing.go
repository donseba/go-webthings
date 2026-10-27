// Package publishing is a news or blog module: dated posts with revisions,
// categories, a featured image and Markdown content. Add one instance per
// stream, for example:
//
//	app.Use(publishing.Module("news", "News"), publishing.Module("blog", "Blog"))
//
// The website renders publishing/list.gohtml and publishing/post.gohtml; see
// ListView and Post for their data.
//
// Below its page it serves Atom and RSS feeds of the newest posts, at
// feed.atom and feed.rss (routes feed.atom and feed.rss), and of each
// category, at category/<slug>/feed.atom and .rss (routes
// category-feed.atom and category-feed.rss); the layout links to them with
// {{feedLinks}}. They list as many posts as the site setting says
// (site.Settings.FeedLimit, 20 by default), with their text as content.
package publishing

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"slices"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/pages"

	"github.com/donseba/go-router"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// PostFields are the language-independent parts of a post.
type PostFields struct {
	Date       time.Time   `db:"date"`
	Image      string      `db:"image_id,null"`
	Categories content.IDs `db:"category_ids"`
}

// PostText is a post in one language. Body is Markdown.
type PostText struct {
	Slug    string `db:"slug"`
	Title   string `db:"title"`
	Excerpt string `db:"excerpt"`
	Body    string `db:"body"`
}

// CategoryText is a category's name and slug in one language; the
// category's posts are listed at /category/<slug> below the instance's page.
type CategoryText struct {
	Slug  string `db:"slug"`
	Title string `db:"title"`
}

type (
	// PostItem is a post with its translations, as the store keeps it.
	PostItem = content.Item[PostFields, PostText]
	// CategoryItem is a category with its translations; categories have
	// no fields of their own.
	CategoryItem = content.Item[struct{}, CategoryText]
)

func postSpec() content.Spec[PostFields, PostText] {
	return content.Spec[PostFields, PostText]{
		Table:      "wt_publishing_post",
		SlugColumn: "slug",
		Complete: func(text PostText) bool {
			return text.Title != "" && text.Slug != ""
		},
		Check: func(_ PostFields, translations []content.Translation[PostText]) error {
			for _, translation := range translations {
				text := translation.Text
				if content.TooLong(200, text.Title) || content.TooLong(2000, text.Excerpt) ||
					content.TooLong(256<<10, text.Body) {
					return content.ErrTooLong
				}
			}

			return nil
		},
		Media: func(fields PostFields, translations []content.Translation[PostText]) []string {
			ids := []string{fields.Image}

			for _, translation := range translations {
				ids = append(ids, markdown.MediaIDs(translation.Text.Body)...)
			}

			return ids
		},
	}
}

func categorySpec() content.Spec[struct{}, CategoryText] {
	return content.Spec[struct{}, CategoryText]{
		Table:      "wt_publishing_category",
		SlugColumn: "slug",
		Complete: func(text CategoryText) bool {
			return text.Title != "" && text.Slug != ""
		},
	}
}

// Service is one Publishing instance, for the website's own code (for
// example "latest news" on the home page).
type Service struct {
	key        string
	posts      *content.Revisions[PostFields, PostText]
	categories *content.Collection[struct{}, CategoryText]
	frontend   *frontend.Frontend
	pages      *pages.Service
	// adminURL builds admin addresses by route name (webthings.Env.AdminURL).
	adminURL func(name string, args ...any) string
	// public are the routes below the module's page (see handler), api
	// those of its endpoints in the content API (see apiRoutes).
	public *router.Router
	api    *router.Router
}

// Store gives write access to this instance's posts, for imports and seeds.
// Admin edits go through the same store.
func (s *Service) Store() content.Store[PostFields, PostText] {
	return s.posts.In(s.key)
}

// Categories gives write access to this instance's categories.
func (s *Service) Categories() content.Store[struct{}, CategoryText] {
	return s.categories.In(s.key)
}

// Of returns the Publishing instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// Post is a published post as templates see it.
type Post struct {
	ID      string
	Title   string
	Excerpt string
	Body    template.HTML
	// Date is a calendar date (see frontend.Civil): {{date .Date}} shows the
	// same day in every site timezone.
	Date time.Time
	// URL is the post's page. Latest and the posts template function leave
	// it empty while the instance is on no page.
	URL string
	// ImageURL is the featured image as pages show it, at most media.ImageWidth
	// wide (see frontend.Frontend.Image); ImageSrcset lists smaller
	// copies for a srcset attribute, ImageWidth and ImageHeight are the
	// size of ImageURL and ImageOriginalURL is the uploaded file, for
	// downloads. All are empty when the post has no featured image.
	ImageURL         string
	ImageSrcset      string
	ImageOriginalURL string
	ImageWidth       int
	ImageHeight      int
	Categories       []Category
}

// Category is a post category as templates see it.
type Category struct {
	ID    string
	Title string
	// URL is the category's list, empty like Post.URL.
	URL    string
	Active bool
}

// ListView is the data of publishing/list.gohtml.
type ListView struct {
	Title string
	Posts []Post
	// Categories that have at least one visible post.
	Categories []Category
	// Category is set when the list is filtered.
	Category *Category
}

// Latest returns up to limit of the newest published posts in locale, for use
// outside the module's own pages; a limit of 0 or less returns all of them.
// Links point to the page the module is mounted on. While the module is on no
// page in locale, posts have no address: Post.URL and the categories' URL are
// empty.
func (s *Service) Latest(ctx context.Context, locale string, limit int) ([]Post, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	posts, _, err := s.list(ctx, locale, pages.Links(s.public, base), "")
	if err != nil {
		return nil, err
	}

	if limit > 0 && limit < len(posts) {
		posts = posts[:limit]
	}

	return posts, nil
}

// list returns published posts in locale (filtered by category when set),
// newest first, and the categories used by them, their URLs by route name
// from paths.
func (s *Service) list(ctx context.Context, locale string, paths router.PathFunc, category string) ([]Post, []Category, error) {
	items, err := s.posts.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, nil, err
	}

	slices.SortStableFunc(items, newestFirst)

	categories, err := s.categoryMap(ctx, locale, paths)
	if err != nil {
		return nil, nil, err
	}

	var (
		posts []Post
		used  = map[string]bool{}
	)

	for _, item := range items {
		for _, id := range item.Fields.Categories {
			used[id] = true
		}

		if category != "" && !item.Fields.Categories.Has(category) {
			continue
		}

		posts = append(posts, s.post(ctx, item, locale, paths, categories))
	}

	var list []Category

	for _, id := range s.categoryOrder(ctx) {
		if found, ok := categories[id]; ok && used[id] {
			found.Active = id == category
			list = append(list, found)
		}
	}

	return posts, list, nil
}

func (s *Service) categoryMap(ctx context.Context, locale string, paths router.PathFunc) (map[string]Category, error) {
	items, err := s.categories.List(ctx, s.key)
	if err != nil {
		return nil, err
	}

	out := map[string]Category{}

	// Unpublished categories have no page, so posts do not link to them.
	for _, item := range items {
		text, ok := item.Text(locale)
		if !item.Published || !ok || !categorySpec().Complete(text) {
			continue
		}

		// Published categories have a slug.
		url, _ := paths("category", "slug", text.Slug)
		out[item.ID] = Category{ID: item.ID, Title: text.Title, URL: url}
	}

	return out, nil
}

func (s *Service) categoryOrder(ctx context.Context) []string {
	items, err := s.categories.List(ctx, s.key)
	if err != nil {
		return nil
	}

	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}

	return ids
}

func (s *Service) post(
	ctx context.Context,
	item PostItem,
	locale string,
	paths router.PathFunc,
	categories map[string]Category,
) Post {
	text, _ := item.Text(locale)

	// Published posts have a slug.
	url, _ := paths("post", "slug", text.Slug)

	shown := s.frontend.Image(ctx, item.Fields.Image)
	post := Post{
		ID:               item.ID,
		Title:            text.Title,
		Excerpt:          text.Excerpt,
		Body:             s.frontend.Markdown(ctx, text.Body),
		Date:             frontend.Civil(item.Fields.Date.UTC()),
		URL:              url,
		ImageURL:         shown.URL,
		ImageSrcset:      shown.Srcset,
		ImageOriginalURL: shown.OriginalURL,
		ImageWidth:       shown.Width,
		ImageHeight:      shown.Height,
	}

	for _, id := range item.Fields.Categories {
		if category, ok := categories[id]; ok {
			post.Categories = append(post.Categories, category)
		}
	}

	return post
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
