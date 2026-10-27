package testimonials

import (
	"context"
	"embed"
	"net/http"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
)

//go:embed translations
var files embed.FS

// Module adds a Testimonials instance. key is used in URLs of the admin and
// must be unique on the site; title is shown in the admin and the page editor.
func Module(key, title string) webthings.Module {
	return webthings.Module{
		Type:   "testimonials",
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
		items:    items,
		frontend: env.Frontend,
		pages:    env.Pages,
	}

	service.api = service.apiRoutes()

	return webthings.Parts{
		Admin:   []admin.Section{section(env, service)},
		Public:  service.handler(),
		API:     service.api,
		Service: service,
	}, nil
}

type testimonialInput struct {
	Quote        string                    `form:"textarea"   label:"Quote"        required:"true" maxLength:"50000"`
	Author       string                    `form:"input,text" label:"Author name"  required:"true" maxLength:"200"   trim:"true"`
	Role         string                    `form:"input,text" label:"Author role"                  maxLength:"500"   trim:"true"`
	Organisation string                    `form:"input,text" label:"Organisation"                 maxLength:"500"   trim:"true"`
	Photo        form.SortedSelect[string] `form:"dropdown"   label:"Photo"                                                      placeholder:"—"`
}

func section(env webthings.Env, service *Service) admin.Section {
	return editor.New(editor.Config[Fields, Text, testimonialInput]{
		ID:       env.Key,
		Title:    env.Title,
		Icon:     "quote",
		NewLabel: "New testimonial",
		Store:    service.items.In(env.Key),
		Locales:  env.Locales,
		Files:    files,
		Label: func(text Text) string {
			return excerpt(text.Quote)
		},
		Form: func(ctx context.Context, item Item, locale string) (testimonialInput, error) {
			text, _ := item.Text(locale)

			photo, err := editor.MediaChoices(ctx, env.Media, true, item.Fields.Photo)
			if err != nil {
				return testimonialInput{}, err
			}

			return testimonialInput{
				Quote:        text.Quote,
				Author:       item.Fields.Author,
				Role:         text.Role,
				Organisation: text.Organisation,
				Photo:        photo,
			}, nil
		},
		Apply: func(_ context.Context, in testimonialInput, draft *content.Draft[Fields, Text], locale string) error {
			// go-form requires the author, without surrounding spaces.
			draft.Fields = Fields{
				Photo:  in.Photo.Get(),
				Author: in.Author,
			}

			draft.Translations = content.SetText(draft.Translations, locale, Text{
				Quote:        in.Quote,
				Role:         in.Role,
				Organisation: in.Organisation,
			})

			return nil
		},
	})
}

// excerpt shortens a quote for admin lists.
func excerpt(quote string) string {
	runes := []rune(quote)
	if len(runes) > 80 {
		return string(runes[:80]) + "…"
	}

	return quote
}

// handler serves the list of testimonials on the page the module is mounted on.
func (s *Service) handler() http.Handler {
	// Paths without a route get the site's 404 page.
	routes := s.frontend.Router()

	routes.Get("/{$}", s.listPage).As("list")

	return routes
}

func (s *Service) listPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mount := frontend.MountFrom(ctx)
	view := ListView{Title: mount.Title}

	var err error

	view.Testimonials, err = s.List(ctx, mount.Locale)
	if err != nil {
		s.frontend.Fail(w, r, err)

		return
	}

	s.frontend.Render(w, r, frontend.Content{
		Template: "list",
		Title:    view.Title,
		Data:     view,
	})
}
