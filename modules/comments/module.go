package comments

import (
	"embed"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/frontend"
)

//go:embed templates translations
var files embed.FS

// Module adds a Comments instance. key is used in URLs of the admin and in
// the template functions and must be unique on the site; title is shown in
// the admin and the page editor.
func Module(key, title string) webthings.Module {
	return webthings.Module{
		Type:   "comments",
		Key:    key,
		Title:  title,
		Schema: schema(),
		Setup:  setup,
		Funcs:  funcs(),
		// The visitor forms themes render with form_field.
		Forms: []any{PostInput{}},
		Templates: []frontend.Template{
			{Name: "list", Data: ListView{}, Example: listExample},
		},
		Mail: []frontend.Template{
			{Name: mailNotify, Data: NotifyView{}, Example: notifyMail},
		},
		Components: components(),
	}
}

func setup(env webthings.Env) (webthings.Parts, error) {
	service := &Service{
		key:      env.Key,
		title:    env.Title,
		db:       env.DB,
		mail:     env.Mail,
		frontend: env.Frontend,
		actions:  env.Actions(),
		changed:  env.Changed,
	}

	visitors := &visitors{service: service, env: env}
	visitors.register()
	guestbook := &guestbook{service: service, frontend: env.Frontend, pages: env.Pages}
	guestbook.public = guestbook.handler()

	return webthings.Parts{
		Admin:   []admin.Section{section(env, service)},
		Public:  guestbook.public,
		Service: service,
	}, nil
}
