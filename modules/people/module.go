package people

import (
	"context"
	"embed"
	"errors"
	"net/http"
	"strings"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
)

//go:embed translations
var files embed.FS

// Module adds a People instance. key is used in URLs of the admin and must be
// unique on the site; title is shown in the admin and the page editor.
func Module(key, title string) webthings.Module {
	return webthings.Module{
		Type:   "people",
		Key:    key,
		Title:  title,
		Schema: schema(),
		Setup:  setup,
		Funcs:  funcs(),
		Templates: []frontend.Template{
			{Name: "list", Data: ListView{}},
			{Name: "person", Data: Person{}},
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
		items:    items,
		frontend: env.Frontend,
		pages:    env.Pages,
		adminURL: env.AdminURL,
	}

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

// personInput is one language of a person in the editor. Name, contact
// details and links are shared by all languages.
type personInput struct {
	Name     string                    `form:"input,text"  label:"Name"        required:"true" maxLength:"200"    trim:"true"`
	Slug     string                    `form:"input,text"  label:"Web address"                 maxLength:"190"    trim:"true" pattern:"^(?:[a-z0-9]+(?:-[a-z0-9]+)*)?$"        patternMsg:"Use lowercase letters, digits and dashes for the web address." description:"The last part of the address, such as open-studio in /en/news/open-studio. Made from the name when left empty."`
	Group    string                    `form:"input,text"  label:"Group"                       maxLength:"200"    trim:"true"`
	Role     string                    `form:"input,text"  label:"Role"                        maxLength:"200"    trim:"true"`
	Bio      string                    `form:"textarea"    label:"Biography"                   maxLength:"262144"                                                                                                                                                                                                                                                                      class:"markdown"`
	Photo    form.SortedSelect[string] `form:"dropdown"    label:"Photo"                                                                                                                                                                                                                                                                                                                                placeholder:"—"`
	Email    string                    `form:"input,email" label:"Email"                       maxLength:"254"    trim:"true"                                                                                                                                                                                                                                                                                           validate:"mail"`
	Phone    string                    `form:"input,tel"   label:"Phone"                       maxLength:"50"     trim:"true" pattern:"^(?:\\+?[0-9 \\(\\)\\.\\/\\-]{3,50})?$" patternMsg:"Enter a valid phone number."`
	Website  string                    `form:"input,url"   label:"Website"                     maxLength:"500"    trim:"true"                                                                                                                                                                                                                                                                                                           url:"http,https" urlMsg:"Links must start with http:// or https://."`
	LinkedIn string                    `form:"input,url"   label:"LinkedIn"                    maxLength:"500"    trim:"true"                                                                                                                                                                                                                                                                                                           url:"http,https" urlMsg:"Links must start with http:// or https://."`
}

func section(env webthings.Env, service *Service) admin.Section {
	store := service.items.In(env.Key)
	linked := editor.Linked[Fields, Text, personInput]{Check: env.Links(), Address: service.address}

	return editor.New(editor.Config[Fields, Text, personInput]{
		ID:       env.Key,
		Title:    env.Title,
		Icon:     "users",
		NewLabel: "New person",
		Store:    store,
		Locales:  env.Locales,
		Files:    files,
		Edit:     linked.Edit,
		Routes:   linked.Routes,
		Saved:    linked.Saved,
		// The name is shared by all languages, so it is not in the text.
		ItemLabel: func(item Item, _ string) string {
			return item.Fields.Name
		},
		Notes: notes,
		Form: func(ctx context.Context, item Item, locale string) (personInput, error) {
			text, _ := item.Text(locale)
			fields := item.Fields

			photo, err := editor.MediaChoices(ctx, env.Media, true, fields.Photo)
			if err != nil {
				return personInput{}, err
			}

			return personInput{
				Name:     fields.Name,
				Slug:     fields.Slug,
				Group:    text.Group,
				Role:     text.Role,
				Bio:      text.Bio,
				Photo:    photo,
				Email:    fields.Email,
				Phone:    fields.Phone,
				Website:  fields.Website,
				LinkedIn: fields.LinkedIn,
			}, nil
		},
		Apply: func(ctx context.Context, in personInput, draft *content.Draft[Fields, Text], locale string) error {
			slug, err := uniqueSlug(ctx, store, draft.ID, in.Slug, in.Name)
			if err != nil {
				return err
			}

			draft.Fields = Fields{
				Slug:     slug,
				Name:     in.Name,
				Photo:    in.Photo.Get(),
				Email:    in.Email,
				Phone:    in.Phone,
				Website:  in.Website,
				LinkedIn: in.LinkedIn,
			}

			draft.Translations = content.SetText(draft.Translations, locale, Text{
				Group: in.Group,
				Role:  in.Role,
				Bio:   in.Bio,
			})

			return nil
		},
		Issue: issue,
	})
}

// notes show each person's role and group in the overview, for example
// "Chair · Board". They are the editors' own text, so they are not
// translated.
func notes(_ context.Context, items []Item, locale string) map[string]string {
	notes := make(map[string]string, len(items))

	for _, item := range items {
		text, _ := item.Text(locale)

		var parts []string

		for _, part := range []string{text.Role, text.Group} {
			if part != "" {
				parts = append(parts, part)
			}
		}

		notes[item.ID] = strings.Join(parts, " · ")
	}

	return notes
}

func issue(err error) string {
	switch {
	case errors.Is(err, errSlug):
		return "Use lowercase letters, digits and dashes for the web address."
	case errors.Is(err, errEmail):
		return "Enter a valid email address."
	case errors.Is(err, errPhone):
		return "Enter a valid phone number."
	case errors.Is(err, errURL):
		return "Links must start with http:// or https://."
	case db.IsUnique(err):
		return "This web address is already used."
	}

	return ""
}

// uniqueSlug returns the slug for a person. An explicit slug must be free; an
// empty one is made from the name, with a number added when it is taken.
func uniqueSlug(ctx context.Context, store content.Store[Fields, Text], id, slug, name string) (string, error) {
	items, err := store.List(ctx)
	if err != nil {
		return "", err
	}

	taken := map[string]bool{}

	for _, item := range items {
		if item.ID != id {
			taken[item.Fields.Slug] = true
		}
	}

	if slug != "" {
		if taken[slug] {
			return "", content.ErrDuplicateSlug
		}

		return slug, nil
	}

	base := content.Slug(name)
	if base == "" {
		base = "person"
	}

	return content.UniqueSlug(base, func(slug string) bool {
		return taken[slug]
	}), nil
}

// handler serves the list of people and a page per person below the page the
// module is mounted on. Their addresses come from the route names: in a
// handler for the request's page (router.For), elsewhere below the page.
func (s *Service) handler() *router.Router {
	// Paths without a route get the site's 404 page.
	routes := s.frontend.Router()

	routes.Get("/{$}", s.listPage).As("list")
	routes.Get("/{slug}", s.personPage).As("person")

	return routes
}

func (s *Service) listPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mount := frontend.MountFrom(ctx)
	view := ListView{Title: mount.Title}

	var err error

	view.People, err = s.list(ctx, mount.Locale, s.public.For(r).RoutePathArgs)
	if err != nil {
		s.frontend.Fail(w, r, err)

		return
	}

	view.Groups = groups(view.People)

	// In the order the page shows them: by group.
	var urls []string

	for _, group := range view.Groups {
		for _, person := range group.People {
			urls = append(urls, person.URL)
		}
	}

	s.frontend.Render(w, r, frontend.Content{
		Template:       "list",
		Title:          view.Title,
		Data:           view,
		StructuredData: []frontend.Thing{frontend.ItemList(view.Title, urls...)},
	})
}

func (s *Service) personPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mount := frontend.MountFrom(ctx)

	item, err := s.published(ctx, mount.Locale, r.PathValue("slug"))
	if err != nil {
		s.frontend.NotFound(w, r)

		return
	}

	person := s.person(ctx, item, mount.Locale, s.public.For(r).RoutePathArgs)
	alternates := map[string]string{}

	for _, translation := range item.Translations {
		alternates[translation.Locale], _ = s.public.RoutePathArgs("person", "slug", item.Fields.Slug)
	}

	s.frontend.Render(w, r, frontend.Content{
		Template:    "person",
		Title:       person.Name,
		Description: person.Role,
		Image:       person.PhotoURL,
		Data:        person,
		Alternates:  alternates,
		EditURL:     s.adminURL(admin.RouteName(s.key, "edit"), "id", item.ID),
		// The person, the page's main entity.
		StructuredData: []frontend.Thing{structured(person)},
	})
}

// sitemap lists the published people's pages below the module page.
func (s *Service) sitemap(ctx context.Context, locale string) ([]string, error) {
	// The unmounted routes give the paths below the module page.
	people, err := s.list(ctx, locale, s.public.RoutePathArgs)
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(people))

	for _, person := range people {
		paths = append(paths, person.URL)
	}

	return paths, nil
}
