package directory

import (
	"context"
	"net/http"
	"strings"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/frontend"

	"github.com/donseba/go-router"
)

// maxQuery limits the search words read from ?q=.
const maxQuery = 200

// handler serves the module below its page: the list, category pages and
// entry pages. Their addresses come from the route names: in a handler for
// the request's page (router.For), elsewhere below the page.
func (s *Service) handler() *router.Router {
	// Paths without a route get the site's 404 page.
	routes := s.frontend.Router()

	routes.Get("/{$}", s.listPage).As("list")
	routes.Get("/{category}", s.categoryPage).As("category")
	routes.Get("/{category}/{entry}", s.entryPage).As("entry")

	return routes
}

// mounted loads the directory with URLs below the current mount.
func (s *Service) mounted(r *http.Request) (directory, error) {
	mount := frontend.MountFrom(r.Context())

	return s.load(r.Context(), mount.Locale, s.public.For(r).RoutePathArgs)
}

func (s *Service) listPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mount := frontend.MountFrom(ctx)

	loaded, err := s.mounted(r)
	if err != nil {
		s.frontend.Fail(w, r, err)

		return
	}

	query := Query{
		Search:   strings.TrimSpace(r.URL.Query().Get("q")),
		Category: r.URL.Query().Get("category"),
	}

	if len(query.Search) > maxQuery {
		query.Search = query.Search[:maxQuery]
	}

	view := ListView{
		Title: mount.Title,
		URL:   s.public.For(r).RoutePath("list"),
		Query: query.Search,
	}

	if query.Category != "" {
		category, ok := loaded.category(query.Category)
		if !ok {
			s.frontend.NotFound(w, r)

			return
		}

		category.Active = true
		view.Category = &category
	}

	for _, category := range loaded.categories {
		if category.Count == 0 {
			continue
		}

		category.Active = view.Category != nil && view.Category.ID == category.ID
		view.Categories = append(view.Categories, category)
	}

	view.Groups, view.Count = group(loaded.categories, loaded.find(query))

	var entries []Entry

	for _, group := range view.Groups {
		entries = append(entries, group.Entries...)
	}

	s.frontend.Render(w, r, frontend.Content{
		Template:       "list",
		Title:          view.Title,
		Data:           view,
		StructuredData: []frontend.Thing{itemList(view.Title, entries)},
	})
}

// itemList is the structured data of a page that lists entries: an
// ItemList of their pages, in the order shown.
func itemList(name string, entries []Entry) frontend.Thing {
	urls := make([]string, 0, len(entries))

	for _, entry := range entries {
		urls = append(urls, entry.URL)
	}

	return frontend.ItemList(name, urls...)
}

// group puts entries under their categories, in category order, and counts
// them.
func group(categories []Category, entries []Entry) ([]Group, int) {
	var groups []Group

	for _, category := range categories {
		current := Group{Category: category}

		for _, entry := range entries {
			if entry.Category.ID == category.ID {
				current.Entries = append(current.Entries, entry)
			}
		}

		if len(current.Entries) > 0 {
			groups = append(groups, current)
		}
	}

	return groups, len(entries)
}

func (s *Service) categoryPage(w http.ResponseWriter, r *http.Request) {
	loaded, err := s.mounted(r)
	if err != nil {
		s.frontend.Fail(w, r, err)

		return
	}

	category, ok := loaded.category(r.PathValue("category"))
	if !ok {
		s.frontend.NotFound(w, r)

		return
	}

	view := CategoryView{
		Category: category,
		Entries:  loaded.find(Query{Category: category.Slug}),
		ListURL:  s.public.For(r).RoutePath("list"),
	}

	s.frontend.Render(w, r, frontend.Content{
		Template:       "category",
		Title:          category.Name,
		Description:    category.Summary,
		Data:           view,
		Alternates:     s.categoryAlternates(r.Context(), category.ID),
		EditURL:        s.adminURL(admin.RouteName(s.key+"-categories", "edit"), "id", category.ID),
		StructuredData: []frontend.Thing{itemList(category.Name, view.Entries)},
	})
}

func (s *Service) entryPage(w http.ResponseWriter, r *http.Request) {
	loaded, err := s.mounted(r)
	if err != nil {
		s.frontend.Fail(w, r, err)

		return
	}

	for _, entry := range loaded.entries {
		if entry.Slug != r.PathValue("entry") {
			continue
		}

		// An entry that moved to another category keeps working at its old
		// address.
		if entry.Category.Slug != r.PathValue("category") {
			http.Redirect(w, r, entry.URL, http.StatusMovedPermanently)

			return
		}

		s.frontend.Render(w, r, frontend.Content{
			Template:    "entry",
			Title:       entry.Name,
			Description: entry.Summary,
			Image:       entry.ImageURL,
			Data:        entry,
			Alternates:  s.entryAlternates(r.Context(), entry),
			EditURL:     s.adminURL(admin.RouteName(s.key, "edit"), "id", entry.ID),
			// The entry, the page's main entity.
			StructuredData: []frontend.Thing{structured(entry)},
		})

		return
	}

	s.frontend.NotFound(w, r)
}

// categoryAlternates links the category in every language it is shown in.
// Slugs are the same in every language.
func (s *Service) categoryAlternates(ctx context.Context, id string) map[string]string {
	item, err := s.categories.Get(ctx, s.key, id)
	if err != nil {
		return nil
	}

	alternates := map[string]string{}

	for _, translation := range item.Translations {
		if categorySpec().Complete(translation.Text) {
			alternates[translation.Locale], _ = s.public.RoutePathArgs("category", "category", item.Fields.Slug)
		}
	}

	return alternates
}

// entryAlternates links the entry in every language both it and its category
// are shown in.
func (s *Service) entryAlternates(ctx context.Context, entry Entry) map[string]string {
	item, err := s.entries.Get(ctx, s.key, entry.ID)
	if err != nil {
		return nil
	}

	categories := s.categoryAlternates(ctx, entry.Category.ID)
	alternates := map[string]string{}

	for _, translation := range item.Translations {
		if _, ok := categories[translation.Locale]; ok {
			alternates[translation.Locale], _ = s.public.RoutePathArgs(
				"entry",
				"category",
				entry.Category.Slug,
				"entry",
				entry.Slug,
			)
		}
	}

	return alternates
}

// sitemap lists the published categories and entries below the module page.
func (s *Service) sitemap(ctx context.Context, locale string) ([]string, error) {
	// The unmounted routes give the paths below the module page.
	loaded, err := s.load(ctx, locale, s.public.RoutePathArgs)
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(loaded.categories)+len(loaded.entries))

	for _, category := range loaded.categories {
		paths = append(paths, category.URL)
	}

	for _, entry := range loaded.entries {
		paths = append(paths, entry.URL)
	}

	return paths, nil
}
