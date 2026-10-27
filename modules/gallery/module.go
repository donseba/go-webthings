package gallery

import (
	"context"
	"embed"
	"errors"
	"net/http"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
)

//go:embed templates translations
var files embed.FS

// Module adds a Gallery instance. key is used in URLs of the admin and must be
// unique on the site; title is shown in the admin and the page editor.
func Module(key, title string) webthings.Module {
	return webthings.Module{
		Type:   "gallery",
		Key:    key,
		Title:  title,
		Schema: schema(),
		Setup:  setup,
		Funcs:  funcs(),
		Templates: []frontend.Template{
			{Name: "list", Data: ListView{}},
			{Name: "album", Data: Album{}},
		},
	}
}

func setup(env webthings.Env) (webthings.Parts, error) {
	albumsSpec := albumSpec()
	albumsSpec.Changed = env.Changed

	albums, err := content.NewCollection(env.DB, albumsSpec)
	if err != nil {
		return webthings.Parts{}, err
	}

	service := &Service{
		key:      env.Key,
		albums:   albums,
		media:    env.Media,
		frontend: env.Frontend,
		pages:    env.Pages,
		adminURL: env.AdminURL,
	}

	service.public = service.handler()
	service.api = service.apiRoutes()

	return webthings.Parts{
		Admin:   []admin.Section{albumsSection(env, service)},
		Public:  service.public,
		API:     service.api,
		Service: service,
		Sitemap: service.sitemap,
		Search:  service.search,
		Links:   service.linkSources,
	}, nil
}

type albumInput struct {
	Title       string                         `form:"input,text"    label:"Title"       required:"true" maxLength:"200"`
	Slug        string                         `form:"input,text"    label:"Web address"                 maxLength:"190"   pattern:"^(?:[a-z0-9]+(?:-[a-z0-9]+)*)?$" patternMsg:"Use lowercase letters, digits and dashes for the web address." description:"The last part of the address, such as open-studio in /en/news/open-studio. Made from the title when left empty."`
	Description string                         `form:"textarea"      label:"Description"                 maxLength:"65536"                                                                                                                                                                                                                                                    class:"markdown"`
	Cover       form.SortedSelect[string]      `form:"dropdown"      label:"Cover image"                                                                                                                                                                                                                                                                                                       placeholder:"—"`
	Images      form.SortedMultiSelect[string] `form:"multicheckbox" label:"Images"`
}

func albumsSection(env webthings.Env, service *Service) admin.Section {
	linked := editor.Linked[AlbumFields, AlbumText, albumInput]{Check: env.Links(), Address: service.address}

	return editor.New(editor.Config[AlbumFields, AlbumText, albumInput]{
		ID:       env.Key,
		Title:    env.Title,
		Icon:     "folder",
		NewLabel: "New album",
		Links:    []editor.Link{{Label: "Image order", Route: admin.RouteName(env.Key, "order")}},
		Store:    service.albums.In(env.Key),
		Locales:  env.Locales,
		Files:    files,
		Edit:     linked.Edit,
		Saved:    linked.Saved,
		Label: func(text AlbumText) string {
			return text.Title
		},
		Form: func(ctx context.Context, item AlbumItem, locale string) (albumInput, error) {
			text, _ := item.Text(locale)

			cover, err := editor.MediaChoices(ctx, env.Media, true, item.Fields.Cover)
			if err != nil {
				return albumInput{}, err
			}

			images, err := imageChoices(ctx, service, item.Fields.Images)
			if err != nil {
				return albumInput{}, err
			}

			return albumInput{
				Title:       text.Title,
				Slug:        text.Slug,
				Description: text.Description,
				Cover:       cover,
				Images:      images,
			}, nil
		},
		Apply: func(_ context.Context, in albumInput, draft *content.Draft[AlbumFields, AlbumText], locale string) error {
			draft.Fields = AlbumFields{
				Cover:  in.Cover.Get(),
				Images: ordered(draft.Fields.Images, in.Images.Get()),
			}

			draft.Translations = content.SetText(draft.Translations, locale, AlbumText{
				Slug:        editor.Slug(in.Slug, in.Title),
				Title:       in.Title,
				Description: in.Description,
			})

			return nil
		},
		Issue: func(err error) string {
			if errors.Is(err, errTooManyImages) {
				return "The album has too many images."
			}

			return ""
		},
		Routes: func(routes *admin.Routes, edit editor.Editor[AlbumFields, AlbumText, albumInput]) {
			order{service: service, admin: routes.Admin(), editor: edit, routes: routes, section: env.Title}.register()
			linked.Routes(routes, edit)
		},
	})
}

// imageChoices offers all library images as checkboxes by name, with
// selected checked.
func imageChoices(ctx context.Context, service *Service, selected []string) (form.SortedMultiSelect[string], error) {
	assets, err := service.media.List(ctx)
	if err != nil {
		return form.SortedMultiSelect[string]{}, err
	}

	choices := map[string]string{}

	for _, asset := range assets {
		if asset.IsImage() {
			choices[asset.ID] = asset.Filename
		}
	}

	for _, id := range selected {
		if choices[id] == "" {
			choices[id] = id
		}
	}

	return form.NewSortedMultiSelect(choices).SortByLabel().Selected(selected...), nil
}

// handler serves the module below its page: the album list and the
// albums. Their addresses come from the route names: in a handler for the
// request's page (router.For), elsewhere below the page.
func (s *Service) handler() *router.Router {
	// Paths without a route get the site's 404 page.
	routes := s.frontend.Router()

	routes.Get("/{$}", s.listPage).As("list")
	routes.Get("/{slug}", s.albumPage).As("album")

	return routes
}

func (s *Service) listPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mount := frontend.MountFrom(ctx)

	albums, err := s.list(ctx, mount.Locale, s.public.For(r).RoutePathArgs)
	if err != nil {
		s.frontend.Fail(w, r, err)

		return
	}

	view := ListView{Title: frontend.MountFrom(ctx).Title, Albums: albums}
	urls := make([]string, 0, len(albums))

	for _, album := range albums {
		urls = append(urls, album.URL)
	}

	s.frontend.Render(w, r, frontend.Content{
		Template:       "list",
		Title:          view.Title,
		Data:           view,
		StructuredData: []frontend.Thing{frontend.ItemList(view.Title, urls...)},
	})
}

func (s *Service) albumPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mount := frontend.MountFrom(ctx)

	item, err := s.albums.PublishedBySlug(ctx, s.key, mount.Locale, r.PathValue("slug"))
	if err != nil {
		s.frontend.NotFound(w, r)

		return
	}

	album := s.album(ctx, item, mount.Locale, s.public.For(r).RoutePathArgs)

	album.Images, err = s.images(ctx, item, mount.Locale)
	if err != nil {
		s.frontend.Fail(w, r, err)

		return
	}

	alternates := map[string]string{}

	for _, translation := range item.Translations {
		if albumSpec().Complete(translation.Text) {
			alternates[translation.Locale], _ = s.public.RoutePathArgs("album", "slug", translation.Text.Slug)
		}
	}

	s.frontend.Render(w, r, frontend.Content{
		Template:   "album",
		Title:      album.Title,
		Image:      album.CoverURL,
		Data:       album,
		Alternates: alternates,
		EditURL:    s.adminURL(admin.RouteName(s.key, "edit"), "id", item.ID),
	})
}

// sitemap lists the published albums in locale below the gallery's page.
func (s *Service) sitemap(ctx context.Context, locale string) ([]string, error) {
	items, err := s.albums.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(items))

	// The unmounted routes give the paths below the module page.
	for _, item := range items {
		text, _ := item.Text(locale)
		path, _ := s.public.RoutePathArgs("album", "slug", text.Slug)
		paths = append(paths, path)
	}

	return paths, nil
}
