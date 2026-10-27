package lists

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
)

// files are the module's admin translations.
//
//go:embed translations
var files embed.FS

// Module adds a list. key is used in URLs of the admin and must be unique on
// the site; title is shown in the admin and the page editor.
func Module(key, title string) webthings.Module {
	return webthings.Module{
		Type:   "lists",
		Key:    key,
		Title:  title,
		Schema: schema(),
		Setup:  setup,
		Funcs:  funcs(),
		Templates: []frontend.Template{
			{Name: "list", Data: ListView{}, Example: listExample},
		},
	}
}

func setup(env webthings.Env) (webthings.Parts, error) {
	entriesSpec := spec()
	entriesSpec.Changed = env.Changed

	entries, err := content.NewCollection(env.DB, entriesSpec)
	if err != nil {
		return webthings.Parts{}, err
	}

	service := &Service{
		key:      env.Key,
		entries:  entries,
		frontend: env.Frontend,
		pages:    env.Pages,
		adminURL: env.AdminURL,
	}

	service.api = service.apiRoutes()

	return webthings.Parts{
		Admin:   []admin.Section{section(env, service)},
		Public:  service.handler(),
		API:     service.api,
		Service: service,
		Search:  service.search,
		Links:   service.linkSources,
	}, nil
}

type entryInput struct {
	Title string                    `form:"input,text" label:"Title" required:"true" maxLength:"200"`
	Body  string                    `form:"textarea"   label:"Text"                  maxLength:"65536" class:"markdown"`
	Image form.SortedSelect[string] `form:"dropdown"   label:"Image"                                                    placeholder:"—"`
	Link  string                    `form:"input,text" label:"Link"                  maxLength:"2000"                   placeholder:"https://" trim:"true" url:"link" urlMsg:"Enter a link starting with https://, mailto: or tel:, or a path starting with /."`
}

func section(env webthings.Env, service *Service) admin.Section {
	return editor.New(editor.Config[Fields, Text, entryInput]{
		ID:       env.Key,
		Title:    env.Title,
		Icon:     "list",
		NewLabel: "New entry",
		Store:    service.entries.In(env.Key),
		Locales:  env.Locales,
		Files:    files,
		Label: func(text Text) string {
			return text.Title
		},
		Form: func(ctx context.Context, item Item, locale string) (entryInput, error) {
			text, _ := item.Text(locale)

			image, err := editor.MediaChoices(ctx, env.Media, true, item.Fields.Image)
			if err != nil {
				return entryInput{}, err
			}

			return entryInput{
				Title: text.Title,
				Body:  text.Body,
				Image: image,
				Link:  item.Fields.Link,
			}, nil
		},
		Apply: func(_ context.Context, in entryInput, draft *content.Draft[Fields, Text], locale string) error {
			draft.Fields = Fields{
				Image: in.Image.Get(),
				Link:  in.Link,
			}

			draft.Translations = content.SetText(draft.Translations, locale, Text{
				Title: in.Title,
				Body:  in.Body,
			})

			return nil
		},
		Issue: func(err error) string {
			if errors.Is(err, errInvalidLink) {
				return "Enter a link starting with https://, mailto: or tel:, or a path starting with /."
			}

			return ""
		},
	})
}

// handler serves the list on the page it is mounted on.
func (s *Service) handler() http.Handler {
	// Paths without a route get the site's 404 page.
	routes := s.frontend.Router()

	routes.Get("/{$}", s.listPage).As("list")

	return routes
}

func (s *Service) listPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mount := frontend.MountFrom(ctx)
	view := ListView{Key: s.key, Title: mount.Title}

	entries, err := s.Entries(ctx, mount.Locale)
	if err != nil {
		s.frontend.Fail(w, r, err)

		return
	}

	view.Entries = entries

	s.frontend.Render(w, r, frontend.Content{
		Template: "list",
		Title:    view.Title,
		Data:     view,
	})
}
