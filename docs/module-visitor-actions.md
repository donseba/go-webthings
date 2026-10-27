# Visitor actions and forms of a module

Visitor actions are the forms visitors post to and the links they follow. This guide adds a form for link suggestions to the links module of [Writing a module](writing-a-module.md): the action that receives it, the form a theme shows, how the post is checked and kept, and how the form's texts reach the site's translations. [Visitor forms](visitor-forms.md) explains these forms as a site sees them.

## Visitor actions

`env.Actions()` returns the go-router of the instance's visitor actions: the forms visitors post to and the links they follow, from any page. The site serves it at `/_/<key>/`, so its routes are relative to that; posts from other websites are refused, and so are bodies over 1 MiB unless `Parts.ActionBodyLimit` allows more. Every call returns the same router, so the service can keep it, here as `s.actions`. `frontend.Limits` adds the module's own limits as middleware, always in the same order: first the size of the body, then the posts per visitor. A visitor over the limit returns to the form with `?<key>=throttled` (`frontend.ResultThrottled`), or gets `ActionLimits.Throttled`. A `Posts` or `Window` of zero does not throttle, and a `Body` of zero keeps the site's limit. Actions for link suggestions at `/_/partners/suggest`, with bodies up to 64 KiB and at most 5 posts per visitor in 10 minutes:

```go
// SuggestionInput is the form a visitor posts to /_/<key>/suggest.
type SuggestionInput struct {
    URL string `form:"input,url" name:"url" label:"Address" required:"true" maxLength:"2000" url:"https" line:"true"`
}

// registerActions registers the instance's actions on its action router.
func (s *Service) registerActions() {
    s.actions.Use(frontend.Limits(s.key, frontend.ActionLimits{
        Body:   64 << 10,
        Posts:  5,
        Window: 10 * time.Minute,
    }))
    s.actions.Post("/suggest", s.suggest).As("suggest")
}

func (s *Service) suggest(w http.ResponseWriter, r *http.Request) {
    // Bots that fill in the honeypot get the answer a person would get.
    if frontend.IsSpam(r) {
        frontend.Back(w, r, s.key, "sent")

        return
    }

    var in SuggestionInput

    errs, err := s.frontend.BindForm(r, &in)
    if err != nil || len(errs) > 0 {
        s.frontend.KeepForm(w, r, s.key, in, errs)
        frontend.Back(w, r, s.key, "invalid")

        return
    }

    slog.InfoContext(r.Context(), "link suggested", "module", s.key, "url", in.URL)
    frontend.Back(w, r, s.key, "sent")
}
```

The `Service` keeps the router in a field `actions *router.Router`: `Setup` sets it with `actions: env.Actions()` and calls `service.registerActions()` before it returns.

`Body` can only lower the site's limit; for a larger body, such as an upload, raise `Parts.ActionBodyLimit`. `Use` comes before the routes and limits all of them; `s.actions.Post(pattern, handler).Use(...)` limits one route, as [`../modules/newsletter/module.go`](../modules/newsletter/module.go) limits its signups only. The routes become part of the site's routes: handlers see the whole path, such as `/_/partners/suggest`, and `r.PathValue` reads a parameter. The site puts the key in front of an action route's name, so a module that runs more than once names its routes alike: the route named `suggest` is `<key>.suggest` for the site, such as `partners.suggest`, and `suggest` for the action router itself. `s.actions.RoutePath("suggest")` returns its address, `/_/partners/suggest`, and `s.actions.RoutePathArgs(name, args...)` fills in parameters. Both work without a request, such as for a link in an e-mail, and `env.Frontend.Route("partners.suggest")` returns the same. An `Env` made without an app, as in a test, gives the same addresses, with a new router on every call: `webthings.Env{Key: "partners"}.Actions()`. A request without a route gets a plain 404, or 405 for a path that has routes for other methods. `frontend.Actions(key, handler, limits)` adds the same limits to a single handler.

A screen or component shows the form as a go-form model, as the bundled modules' views do in a field `Form` of type `form.RenderModel`. `frontend.VisitorForm(ctx, SuggestionInput{}, s.actions.RoutePath("suggest"), s.key)` makes one that posts to the action, with the honeypot (`frontend.Honeypot`) and a hidden field `back`: the page being shown, followed by `#` and the key. A theme renders it with `{{form_open .Form}}`, a field by its `name` with `{{form_field .Form "url" nil}}`, with its label and its limits, such as `maxlength`, in the page's language, and `{{form_close}}`. A template that writes the form tag itself adds the hidden fields with `{{form_hidden .Form}}`. See [Template functions](template-functions.md#forms). The key also goes before the ids of the fields, so the forms of two instances on one page keep ids of their own: the `url` field of `partners` has the id `partners-url`.

`Back` returns the visitor to the form's page with `?partners=sent`, at the element whose id is the key, so a theme puts the form and its result in an element with that id. `BackWith` adds more values to the query. When the post names no page on the site, as one over the body limit, which cannot be read, both return to the `Referer` when that is an address on the host the post was sent to, and to the home page otherwise. A template function reads the result with `frontend.Result(frontend.Request(ctx), key, allowed...)`, which returns only one of the allowed results, so a made-up address shows none. In the handler, `frontend.BackPath(r)` is the path of the form's page, found the same way, and `env.Frontend.BackLocale(r)` its language, for example to store a message in the visitor's language.

Check every value a visitor sends. `env.Frontend.BindForm`, here `s.frontend.BindForm`, maps the post onto the input's struct and checks the struct's go-form tags: `required`, `maxLength` in characters, `url` and the others, `line:"true"` for a single line, such as a name or a subject: valid UTF-8 without tabs, line breaks or other control characters, and `validate:"mail"` for an e-mail address that mail will go to (`mail.Rule`; see [E-mail](mail.md#addresses-in-forms)). It trims the text and reads line breaks as `\n`, as browsers count them for `maxlength`, and reads a `datetime-local` field in the site's timezone, in which the form shows it. A value it cannot read as its field's type, such as "abc" for a number, comes back as an error with the rule `invalid`; the other errors carry the rule they broke (`form.FieldValidationError.Rule`), for a result per rule. `err` is set only when the post cannot be mapped at all. The messages are in the language of the page the form was on (`BackLocale`). It is go-form's `Bind`, so an input that implements `form.Overrider`, with limits that change at runtime, such as the places left, is checked against those. A form that is not a struct but a go-form `form.Schema`, such as one editors put together, is checked with `env.Frontend.ValidateSchema(r, schema, values)`, with the messages in the language of the page being served; [`../modules/forms/public.go`](../modules/forms/public.go) receives its form on its page this way, to show it again with the errors.

`s.frontend.KeepForm(w, r, s.key, in, errs)` before `Back` keeps a post with mistakes for the form's page. There the form that `VisitorForm` makes for the same key and action shows the visitor's values again, with the messages at their fields, without a change to the templates. The post waits on the server for five minutes at most, with only a token in a cookie for that page, and shows once; see [Visitor forms](visitor-forms.md#a-post-with-mistakes). Show such a form with the page or in a fragment placed with `slot` or `requiredSlot`, not `deferredSlot`, whose later request comes after the page took the post. The bundled comments, newsletter, registrations and submissions modules keep their posts this way; [`../modules/comments/actions.go`](../modules/comments/actions.go) also keeps a post its service refuses, with `nil` errors, so it shows the values alone.

A post that fails for another reason, such as a database that cannot store it, answers with `s.frontend.Fail(w, r, err)`, as the bundled modules' actions do: the error is logged with the module's key and the path, and the visitor gets the theme's `500.gohtml` in the language of the form's page, rendered as the theme's pages are, with its asset versions, as with `Fail` in [The public handler](writing-a-module.md#the-public-handler). An HTMX post gets that page too, which the client shows as the whole page with `reloadOnError`; see [HTMX](htmx.md#forms-with-htmx).

List the inputs in `Module.Forms`, such as `Forms: []any{SuggestionInput{}}`: when a theme or a module's default template renders forms with `form_open`, `form_field` or `form_render`, the starter's `task translations` adds their labels, descriptions, placeholders, option labels, submit texts and messages to `translations/translations.pot`, referenced as `module:<type>/forms`. The texts of the module's default templates, components and mail come with them, as `module:<type>/<name>.gohtml` and `module:<type>/mail/<name>.gohtml`, unless every theme replaces the file. `App.TranslationCatalog()` returns all these texts as a go-translator `*extract.Catalog`, also in a test, without `Start`; see [Languages](languages.md#keeping-the-files-up-to-date).

`env.VisitorID(ctx, r, parts...)` identifies a visitor without storing their address, for example per day with the date as a part. `env.Frontend.SendMail(ctx, env.Mail, frontend.Letter{...})` writes a mail template and sends it, also when the visitor leaves the page, and logs a failure; send it after the post is stored. See [Visitor forms](visitor-forms.md) and [E-mail](mail.md).
