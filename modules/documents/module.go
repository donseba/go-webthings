package documents

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
	"github.com/donseba/go-router/middleware"
)

//go:embed translations
var files embed.FS

// Module adds a Documents instance. key is used in URLs of the admin and must
// be unique on the site; title is shown in the admin and the page editor.
func Module(key, title string) webthings.Module {
	return webthings.Module{
		Type:   "documents",
		Key:    key,
		Title:  title,
		Schema: schema(),
		Setup:  setup,
		Funcs:  funcs(),
		Templates: []frontend.Template{
			{Name: "list", Data: ListView{}},
		},
	}
}

func setup(env webthings.Env) (webthings.Parts, error) {
	itemsSpec := spec()
	itemsSpec.Changed = env.Changed

	items, err := content.NewCollection(env.DB, itemsSpec)
	if err != nil {
		return webthings.Parts{}, err
	}

	service := &Service{
		key:      env.Key,
		db:       env.DB,
		items:    items,
		media:    env.Media,
		frontend: env.Frontend,
		pages:    env.Pages,
		adminURL: env.AdminURL,
		actions:  env.Actions(),
	}

	service.registerActions()
	service.public = service.handler()
	service.api = service.apiRoutes()

	return webthings.Parts{
		Admin:   []admin.Section{section(env, service)},
		Public:  service.public,
		API:     service.api,
		Service: service,
		Sitemap: service.sitemap,
		Search:  service.search,
		Links:   service.linkSources,
	}, nil
}

// documentInput is one language of a document in the editor. The file, link
// and date are shared by all languages.
type documentInput struct {
	Title       string                    `form:"input,text" label:"Title"         required:"true" maxLength:"200"    trim:"true"`
	Category    string                    `form:"input,text" label:"Category"                      maxLength:"200"    trim:"true"`
	Date        time.Time                 `form:"input,date" label:"Date"          required:"true"`
	File        form.SortedSelect[string] `form:"dropdown"   label:"File"                                                         placeholder:"—"`
	Link        string                    `form:"input,url"  label:"External link"                 maxLength:"2000"   trim:"true"                 url:"http,https" urlMsg:"Links must start with http:// or https://."`
	Description string                    `form:"textarea"   label:"Description"                   maxLength:"262144"                                                                                                  class:"markdown"`
}

func section(env webthings.Env, service *Service) admin.Section {
	return editor.New(editor.Config[Fields, Text, documentInput]{
		ID:       env.Key,
		Title:    env.Title,
		Icon:     "file",
		NewLabel: "New document",
		Store:    service.items.In(env.Key),
		Locales:  env.Locales,
		Files:    files,
		Label: func(text Text) string {
			return text.Title
		},
		Notes: service.downloadNotes,
		Form: func(ctx context.Context, item Item, locale string) (documentInput, error) {
			text, _ := item.Text(locale)
			fields := item.Fields

			file, err := editor.MediaChoices(ctx, env.Media, false, fields.File)
			if err != nil {
				return documentInput{}, err
			}

			// The stored midnight UTC is the date itself.
			date := fields.Date.UTC()
			if fields.Date.IsZero() {
				date = time.Now()
			}

			return documentInput{
				Title:       text.Title,
				Category:    text.Category,
				Date:        date,
				File:        file,
				Link:        fields.Link,
				Description: text.Description,
			}, nil
		},
		Apply: func(_ context.Context, in documentInput, draft *content.Draft[Fields, Text], locale string) error {
			date := in.Date.UTC()

			draft.Fields = Fields{
				File: in.File.Get(),
				Link: in.Link,
				Date: time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC),
			}

			draft.Translations = content.SetText(draft.Translations, locale, Text{
				Title:       in.Title,
				Description: in.Description,
				Category:    in.Category,
			})

			return nil
		},
		Issue: issue,
	})
}

// downloadNotes shows in the overview how often each document was
// downloaded, read in one query for the instance. The overview still works
// without them when the query fails.
func (s *Service) downloadNotes(ctx context.Context, items []Item, _ string) map[string]string {
	counts, err := s.downloads(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "document downloads", "module", s.key, "error", err)

		return nil
	}

	notes := make(map[string]string, len(items))

	for _, item := range items {
		if count := counts[item.ID]; count > 0 {
			notes[item.ID] = admin.TN(ctx, "%d download", "%d downloads", count, count)
		}
	}

	return notes
}

func issue(err error) string {
	switch {
	case errors.Is(err, errSource):
		return "Choose a file or enter an external link, not both."
	case errors.Is(err, errLink):
		return "Links must start with http:// or https://."
	case errors.Is(err, errDate):
		return "The date is not valid."
	}

	return ""
}

// handler serves the list of documents and the list per category below the
// page the module is mounted on. Their addresses come from the route names:
// in a handler for the request's page (router.For), elsewhere below the
// page.
func (s *Service) handler() *router.Router {
	// Paths without a route get the site's 404 page.
	routes := s.frontend.Router()

	routes.Get("/{$}", s.listPage).As("list")
	routes.Get("/category/{slug}", s.listPage).As("category")

	return routes
}

func (s *Service) listPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mount := frontend.MountFrom(ctx)
	here := s.public.For(r)
	slug := r.PathValue("slug")
	view := ListView{Title: mount.Title, AllURL: here.RoutePath("list")}

	all, err := s.list(ctx, mount.Locale, here.RoutePathArgs)
	if err != nil {
		s.frontend.Fail(w, r, err)

		return
	}

	view.Categories = categories(all, here.RoutePathArgs, slug)
	view.Documents = all

	for i := range view.Categories {
		if view.Categories[i].Active {
			view.Category = &view.Categories[i]
		}
	}

	if slug != "" {
		// Category names are free text per language, so a category page
		// exists only in languages that use it.
		if view.Category == nil {
			s.frontend.NotFound(w, r)

			return
		}

		view.Title = view.Category.Name
		view.Documents = filter(all, slug)
	}

	view.Years = years(view.Documents)

	var alternates map[string]string

	if slug != "" {
		path, _ := s.public.RoutePathArgs("category", "slug", slug)
		alternates = map[string]string{mount.Locale: path}
	}

	s.frontend.Render(w, r, frontend.Content{
		Template:   "list",
		Title:      view.Title,
		Data:       view,
		Alternates: alternates,
	})
}

// registerActions registers the download address, which works on every
// page, on the instance's action router (webthings.Env.Actions).
func (s *Service) registerActions() {
	routes := s.actions

	// The download address redirects to the file's current URL, which may
	// change: no cache keeps the answer, and no search engine indexes it.
	routes.Get("/{id}/download", s.download).As("download").Use(
		middleware.NoStore,
		middleware.SecurityHeaders(middleware.SecurityHeadersOptions{XRobotsTag: "noindex"}),
	)
}

// download counts the download and redirects to the file or link. HEAD
// requests, such as link checkers, are not counted.
func (s *Service) download(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	var (
		target string
		err    error
	)

	if r.Method == http.MethodHead {
		target, err = s.target(ctx, id)
	} else {
		target, err = s.Download(ctx, id)
	}

	// An unknown or unpublished document gets the theme's not-found page.
	if errors.Is(err, content.ErrNotFound) {
		s.frontend.NotFound(w, r)

		return
	}

	// The theme's error page answers, which logs err.
	if err != nil {
		s.frontend.Fail(w, r, fmt.Errorf("download document %s: %w", id, err))

		return
	}

	http.Redirect(w, r, target, http.StatusFound)
}

// sitemap lists the category pages below the module page.
func (s *Service) sitemap(ctx context.Context, locale string) ([]string, error) {
	// The unmounted routes give the paths below the module page.
	documents, err := s.list(ctx, locale, s.public.RoutePathArgs)
	if err != nil {
		return nil, err
	}

	var paths []string

	for _, category := range categories(documents, s.public.RoutePathArgs, "") {
		paths = append(paths, category.URL)
	}

	return paths, nil
}
