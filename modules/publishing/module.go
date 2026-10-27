package publishing

import (
	"context"
	"embed"
	"net/http"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
)

//go:embed translations
var translations embed.FS

// Module adds a Publishing instance. key is used in URLs of the admin and must
// be unique on the site; title is shown in the admin and the page editor.
func Module(key, title string, opts ...Option) webthings.Module {
	var config options

	for _, option := range opts {
		option(&config)
	}

	return webthings.Module{
		Type:   "publishing",
		Key:    key,
		Title:  title,
		Schema: schema(),
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			return setup(env, config)
		},
		Funcs: funcs(),
		Templates: []frontend.Template{
			{Name: "list", Data: ListView{}},
			{Name: "post", Data: Post{}},
		},
	}
}

func setup(env webthings.Env, config options) (webthings.Parts, error) {
	postsSpec := postSpec()
	postsSpec.Changed = env.Changed

	posts, err := content.NewRevisions(env.DB, postsSpec)
	if err != nil {
		return webthings.Parts{}, err
	}

	categoriesSpec := categorySpec()
	categoriesSpec.Changed = env.Changed

	categories, err := content.NewCollection(env.DB, categoriesSpec)
	if err != nil {
		return webthings.Parts{}, err
	}

	service := &Service{
		key:        env.Key,
		posts:      posts,
		categories: categories,
		frontend:   env.Frontend,
		pages:      env.Pages,
		adminURL:   env.AdminURL,
	}

	service.public = service.handler()
	service.api = service.apiRoutes()

	return webthings.Parts{
		Admin: []admin.Section{
			postsSection(env, service),
			categoriesSection(env, service),
		},
		Public:    service.public,
		API:       service.api,
		Service:   service,
		Sitemap:   service.sitemap,
		Feeds:     service.feeds(config.feedSize),
		Search:    service.search,
		Links:     service.linkSources,
		Scheduled: []content.Scheduler{posts.Scheduler(env.Key)},
	}, nil
}

type postInput struct {
	Title      string                         `form:"input,text"    label:"Title"       required:"true" maxLength:"200"`
	Slug       string                         `form:"input,text"    label:"Web address"                 maxLength:"190"    pattern:"^(?:[a-z0-9]+(?:-[a-z0-9]+)*)?$" patternMsg:"Use lowercase letters, digits and dashes for the web address." description:"The last part of the address, such as open-studio in /en/news/open-studio. Made from the title when left empty."`
	Date       time.Time                      `form:"input,date"    label:"Date"        required:"true"`
	Excerpt    string                         `form:"textarea"      label:"Summary"                     maxLength:"2000"                                                                                                                        description:"A short text for lists, search results and search engines."`
	Body       string                         `form:"textarea"      label:"Content"                     maxLength:"262144"                                                                                                                                                                                                                                                    class:"markdown"`
	Image      form.SortedSelect[string]      `form:"dropdown"      label:"Image"                                                                                                                                                                                                                                                                                                              placeholder:"—"`
	Categories form.SortedMultiSelect[string] `form:"multicheckbox" label:"Categories"`
}

func postsSection(env webthings.Env, service *Service) admin.Section {
	linked := editor.Linked[PostFields, PostText, postInput]{Check: env.Links(), Address: service.address}

	return editor.New(editor.Config[PostFields, PostText, postInput]{
		ID:       env.Key,
		Title:    env.Title,
		Icon:     "write",
		NewLabel: "New post",
		Links:    []editor.Link{{Label: "Categories", Route: admin.RouteName(env.Key+"-categories", "index")}},
		Store:    service.posts.In(env.Key),
		Locales:  env.Locales,
		Names:    env.Auth.Names,
		Files:    translations,
		Edit:     linked.Edit,
		Routes:   linked.Routes,
		Saved:    linked.Saved,
		Label: func(text PostText) string {
			return text.Title
		},
		Form: func(ctx context.Context, item PostItem, locale string) (postInput, error) {
			text, _ := item.Text(locale)

			image, err := editor.MediaChoices(ctx, env.Media, true, item.Fields.Image)
			if err != nil {
				return postInput{}, err
			}

			choices, err := categoryChoices(ctx, service, locale)
			if err != nil {
				return postInput{}, err
			}

			categories := form.NewSortedMultiSelect(choices).Selected(item.Fields.Categories...)

			date := item.Fields.Date
			if date.IsZero() {
				date = time.Now()
			}

			return postInput{
				Title:      text.Title,
				Slug:       text.Slug,
				Date:       date,
				Excerpt:    text.Excerpt,
				Body:       text.Body,
				Image:      image,
				Categories: categories,
			}, nil
		},
		Apply: func(_ context.Context, in postInput, draft *content.Draft[PostFields, PostText], locale string) error {
			draft.Fields = PostFields{
				Date:       in.Date,
				Image:      in.Image.Get(),
				Categories: in.Categories.Get(),
			}

			draft.Translations = content.SetText(draft.Translations, locale, PostText{
				Slug:    editor.Slug(in.Slug, in.Title),
				Title:   in.Title,
				Excerpt: in.Excerpt,
				Body:    in.Body,
			})

			return nil
		},
		PreviewURL: func(ctx context.Context, item PostItem, locale string) string {
			text, _ := item.Text(locale)

			path, err := service.public.RoutePathArgs("post", "slug", text.Slug)
			if err != nil {
				return ""
			}

			return env.Frontend.ModulePreviewURL(ctx, env.Key, locale, path, item.Revision.ID)
		},
	})
}

func categoryChoices(ctx context.Context, service *Service, locale string) (map[string]string, error) {
	items, err := service.categories.List(ctx, service.key)
	if err != nil {
		return nil, err
	}

	choices := map[string]string{}

	for _, item := range items {
		text, _ := item.Text(locale)

		choices[item.ID] = text.Title
		if text.Title == "" {
			choices[item.ID] = item.ID
		}
	}

	return choices, nil
}

type categoryInput struct {
	Title string `form:"input,text" label:"Title"       required:"true" maxLength:"200"`
	Slug  string `form:"input,text" label:"Web address"                 maxLength:"190" pattern:"^(?:[a-z0-9]+(?:-[a-z0-9]+)*)?$" patternMsg:"Use lowercase letters, digits and dashes for the web address." description:"The last part of the address, such as open-studio in /en/news/open-studio. Made from the title when left empty."`
}

func categoriesSection(env webthings.Env, service *Service) admin.Section {
	linked := editor.Linked[struct{}, CategoryText, categoryInput]{Check: env.Links(), Address: service.categoryAddress}

	section := editor.New(editor.Config[struct{}, CategoryText, categoryInput]{
		ID:         env.Key + "-categories",
		Permission: env.Key,
		Title:      "Categories",
		NewLabel:   "New category",
		Links:      []editor.Link{{Label: env.Title, Route: admin.RouteName(env.Key, "index")}},
		Store:      service.categories.In(env.Key),
		Locales:    env.Locales,
		Files:      translations,
		Edit:       linked.Edit,
		Routes:     linked.Routes,
		Saved:      linked.Saved,
		Label: func(text CategoryText) string {
			return text.Title
		},
		Form: func(_ context.Context, item CategoryItem, locale string) (categoryInput, error) {
			text, _ := item.Text(locale)

			return categoryInput{Title: text.Title, Slug: text.Slug}, nil
		},
		Apply: func(_ context.Context, in categoryInput, draft *content.Draft[struct{}, CategoryText], locale string) error {
			draft.Translations = content.SetText(draft.Translations, locale, CategoryText{
				Slug:  editor.Slug(in.Slug, in.Title),
				Title: in.Title,
			})

			return nil
		},
	})

	// Categories are reached from the posts overview, not from the navigation.
	section.Hidden = true

	return section
}

// handler serves the module below its page: the list, category lists and
// posts. Their addresses come from the route names: in a handler for the
// request's page (router.For), elsewhere below the page.
func (s *Service) handler() *router.Router {
	// Paths without a route get the site's 404 page.
	routes := s.frontend.Router()

	routes.Get("/{$}", s.listPage).As("list")
	routes.Get("/category/{slug}", s.listPage).As("category")
	routes.Get("/{slug}", s.postPage).As("post")

	return routes
}

func (s *Service) listPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mount := frontend.MountFrom(ctx)
	view := ListView{}
	alternates := map[string]string{}

	view.Title = mount.Title

	var (
		category string
		filter   CategoryItem
	)

	if slug := r.PathValue("slug"); slug != "" {
		item, err := s.categories.PublishedBySlug(ctx, s.key, mount.Locale, slug)
		if err != nil {
			// A category that does not exist is a 404; other errors are
			// logged.
			s.frontend.Fail(w, r, err)

			return
		}

		category = item.ID
		filter = item

		for _, translation := range item.Translations {
			if categorySpec().Complete(translation.Text) {
				alternates[translation.Locale], _ = s.public.RoutePathArgs("category", "slug", translation.Text.Slug)
			}
		}
	}

	var err error

	here := s.public.For(r).RoutePathArgs

	view.Posts, view.Categories, err = s.list(ctx, mount.Locale, here, category)
	if err != nil {
		s.frontend.Fail(w, r, err)

		return
	}

	for i := range view.Categories {
		if view.Categories[i].Active {
			view.Category = &view.Categories[i]
		}
	}

	// A published category without visible posts is not among the
	// categories, but its list is still the category's.
	if category != "" && view.Category == nil {
		text, _ := filter.Text(mount.Locale)
		address, _ := here("category", "slug", text.Slug)
		view.Category = &Category{ID: category, Title: text.Title, URL: address, Active: true}
	}

	title := view.Title
	if view.Category != nil {
		title = view.Category.Title
		view.Title = title
	} else {
		alternates = nil
	}

	urls := make([]string, 0, len(view.Posts))

	for _, post := range view.Posts {
		urls = append(urls, post.URL)
	}

	s.frontend.Render(w, r, frontend.Content{
		Template:       "list",
		Title:          title,
		Data:           view,
		Alternates:     alternates,
		StructuredData: []frontend.Thing{frontend.ItemList(title, urls...)},
	})
}

func (s *Service) postPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mount := frontend.MountFrom(ctx)

	// An editor may preview a revision of the post here.
	preview := frontend.PreviewRevision(r, s.adminURL(admin.RouteName(s.key, "index")))

	// An unknown post is a 404; other errors are logged as such.
	item, err := s.posts.PreviewBySlug(ctx, s.key, mount.Locale, r.PathValue("slug"), preview)
	if err != nil {
		s.frontend.Fail(w, r, err)

		return
	}

	here := s.public.For(r).RoutePathArgs

	categories, err := s.categoryMap(ctx, mount.Locale, here)
	if err != nil {
		s.frontend.Fail(w, r, err)

		return
	}

	post := s.post(ctx, item, mount.Locale, here, categories)
	alternates := map[string]string{}

	for _, translation := range item.Translations {
		if postSpec().Complete(translation.Text) {
			alternates[translation.Locale], _ = s.public.RoutePathArgs("post", "slug", translation.Text.Slug)
		}
	}

	s.frontend.Render(w, r, frontend.Content{
		Template:    "post",
		Title:       post.Title,
		Description: post.Excerpt,
		Image:       post.ImageURL,
		Data:        post,
		Alternates:  alternates,
		EditURL:     s.adminURL(admin.RouteName(s.key, "edit"), "id", item.ID),
		Revision:    frontend.RevisionOf(r, item, preview),
		// The post's Article, the page's main entity.
		StructuredData: []frontend.Thing{s.article(ctx, item, post, mount.Locale)},
	})
}

// sitemap lists published posts and their categories below the module page.
func (s *Service) sitemap(ctx context.Context, locale string) ([]string, error) {
	// The unmounted routes give the paths below the module page.
	posts, categories, err := s.list(ctx, locale, s.public.RoutePathArgs, "")
	if err != nil {
		return nil, err
	}

	var paths []string

	for _, post := range posts {
		paths = append(paths, post.URL)
	}

	for _, category := range categories {
		paths = append(paths, category.URL)
	}

	return paths, nil
}
