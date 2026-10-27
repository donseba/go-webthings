package newsletter

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

	"github.com/donseba/go-router/middleware"
)

//go:embed templates translations
var files embed.FS

// Module adds a Newsletter instance. key is used in URLs of the admin and the
// visitor actions and must be unique on the site; title is shown in the admin
// and the page editor.
func Module(key, title string, opts ...Option) webthings.Module {
	var config options

	for _, option := range opts {
		option(&config)
	}

	return webthings.Module{
		Type:   "newsletter",
		Key:    key,
		Title:  title,
		Schema: schema(),
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			return setup(env, config)
		},
		Funcs: funcs(),
		// The visitor forms themes render with form_field.
		Forms: []any{SubscribeInput{}},
		Templates: []frontend.Template{
			{Name: "signup", Data: SignupView{}, Example: signupExample},
		},
		Mail: []frontend.Template{
			{Name: mailConfirmation, Data: ConfirmationView{}, Example: confirmationMail},
		},
		Components: components(),
	}
}

func setup(env webthings.Env, config options) (webthings.Parts, error) {
	textsSpec := textSpec()
	textsSpec.Changed = env.Changed

	texts, err := content.NewCollection(env.DB, textsSpec)
	if err != nil {
		return webthings.Parts{}, err
	}

	service := &Service{
		key:      env.Key,
		db:       env.DB,
		texts:    texts,
		frontend: env.Frontend,
		pages:    env.Pages,
		locales:  env.Locales,
		mail:     env.Mail,
		absolute: env.AbsoluteURL,
		provider: config.provider,
		actions:  env.Actions(),
	}

	service.registerActions()

	service.start()

	return webthings.Parts{
		Admin: []admin.Section{
			section(env, service),
			textsSection(env, service),
		},
		Public:  service.handler(),
		Service: service,
		Close:   service.close,
	}, nil
}

// section is the instance's admin: subscribers, export and mailings.
func section(env webthings.Env, service *Service) admin.Section {
	return admin.Section{
		ID: env.Key,
		// The admin parses the body before the handler runs, so the import's
		// limit has to be the section's.
		MaxBodyBytes: maxImportBytes,
		Title:        env.Title,
		Icon:         "megaphone",
		Files:        files,
		Trash:        service.trash(),
		Routes: func(routes *admin.Routes) {
			s := screens{
				service: service,
				admin:   routes.Admin(),
				routes:  routes,
				title:   env.Title,
			}

			routes.Get("", admin.View, s.subscribers).As("index")
			routes.Get("/export", admin.Export, s.export).As("export")
			routes.Get("/import", admin.Create, s.importForm).As("import")
			routes.Post("/import", admin.Create, s.importSubscribers)
			routes.Post("/subscribers/{subscriber}/delete", admin.Delete, s.deleteSubscriber).As("subscriber-delete")
			routes.Get("/mailings", admin.View, s.mailings).As("mailings")
			routes.Get("/mailings/new", admin.Create, s.newMailing).As("mailing-new")
			routes.Post("/mailings/new", admin.Create, s.createMailing)
			routes.Get("/mailings/{mailing}", admin.View, s.mailing).As("mailing")
			routes.Post("/mailings/{mailing}/save", admin.Edit, s.saveMailing).As("mailing-save")
			routes.Post("/mailings/{mailing}/send", admin.Publish, s.sendMailing).As("mailing-send")
			routes.Post("/mailings/{mailing}/delete", admin.Delete, s.deleteMailing).As("mailing-delete")
		},
	}
}

type textsInput struct {
	Title  string `form:"input,text" label:"Title"          required:"true" maxLength:"200"`
	Intro  string `form:"textarea"   label:"Introduction"                   maxLength:"5000" class:"markdown" description:"Shown with the signup form."`
	Footer string `form:"textarea"   label:"Mailing footer" required:"true" maxLength:"1000" class:"markdown" description:"Ends every mailing, followed by the subscriber's personal unsubscribe link." rows:"3"`
}

// textsSection edits the instance's texts per language. It is reached from
// the subscribers page, not from the navigation, and shares its rights.
func textsSection(env webthings.Env, service *Service) admin.Section {
	section := editor.New(editor.Config[struct{}, Text, textsInput]{
		ID:         env.Key + "-texts",
		Permission: env.Key,
		Title:      "Newsletter texts",
		NewLabel:   "New texts",
		Single:     true,
		Links:      []editor.Link{{Label: env.Title, Route: admin.RouteName(env.Key, "index")}},
		Store:      service.texts.In(env.Key),
		Locales:    env.Locales,
		Files:      files,
		Label: func(text Text) string {
			return text.Title
		},
		Form: func(_ context.Context, item Item, locale string) (textsInput, error) {
			text, ok := item.Text(locale)
			if item.ID == "" || !ok {
				text = defaultText
			}

			return textsInput(text), nil
		},
		Apply: func(_ context.Context, in textsInput, draft *content.Draft[struct{}, Text], locale string) error {
			draft.Translations = content.SetText(draft.Translations, locale, Text(in))

			return nil
		},
	})

	section.Hidden = true

	return section
}

// handler serves the signup page on the page the module is mounted on.
func (s *Service) handler() http.Handler {
	// Paths without a route get the site's 404 page.
	routes := s.frontend.Router()

	routes.Get("/{$}", s.signupPage).As("signup")

	return routes
}

// registerActions registers the actions that receive the signup form and
// the links in the mails on the instance's action router
// (webthings.Env.Actions). Signups are throttled per visitor; the links are
// GETs, which Throttle lets through, and one-click unsubscribes come from
// mail providers with a token.
func (s *Service) registerActions() {
	routes := s.actions

	// Only signups are throttled: one-click unsubscribes come from a few
	// mail providers' servers.
	routes.Post("/subscribe", s.subscribe).Use(frontend.Limits(s.key, frontend.ActionLimits{
		Body:   bodyLimit,
		Posts:  20,
		Window: time.Hour,
	})).As("subscribe")

	// The links in the mails carry their token, and what they answer
	// depends on it: no cache keeps it.
	routes.Get("/confirm", s.confirm).As("confirm").Use(middleware.NoStore)
	routes.Get("/unsubscribe", s.unsubscribe).As("unsubscribe").Use(middleware.NoStore)
	routes.Post("/unsubscribe", s.oneClick).Use(middleware.NoStore)
}

// signupExample is the starting point for newsletter/signup.gohtml.
const signupExample = `{{/* @dot github.com/donseba/go-webthings/modules/newsletter.SignupView */}}
{{/* Starting point generated by WebThings: the signup form and its results. */}}
<main id="content">
    <h1>{{.Title}}</h1>

    {{.Intro}}

    {{/* The form returns the visitor here, to the element with the id {{.Key}}. */}}
    <section id="{{.Key}}">
        {{if eq .Result "check-mail"}}
            <p>Thank you. Please check your inbox and confirm your subscription.</p>
        {{else if eq .Result "confirmed"}}
            <p>Your subscription is confirmed.</p>
        {{else if eq .Result "unsubscribed"}}
            <p>You are unsubscribed.</p>
        {{else if eq .Result "invalid"}}
            <p>That did not work. Please check your e-mail address or link.</p>
        {{else if eq .Result "throttled"}}
            <p>Too many attempts. Please try again later.</p>
        {{end}}

        {{form_open .Form}}
            {{form_field .Form "locale" nil}}
            {{form_field .Form "email" nil}}
            {{form_field .Form "name" nil}}

            <button type="submit">Subscribe</button>
        {{form_close}}
    </section>
</main>
`
