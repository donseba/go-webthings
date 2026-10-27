# Visitor forms

Visitors post comments, votes, sign-ups and messages through forms in the site's templates. This guide explains how those forms reach modules, how they are protected, and how a site adds a form of its own.

## How a visitor form works

A module that takes posts from visitors receives them below `/_/<key>/`, its visitor actions. The address does not depend on the page, so any template can show a module's form: a comment form below a post, a newsletter signup in the footer, a poll on the home page.

A post goes through the same steps everywhere:

1. The visitor sends the form to the module, for example `/_/newsletter/subscribe`.
2. The module checks the spam trap and the input, and stores what it should.
3. It redirects (303 See Other) to the page the form was on, with the result in the query: `/en/news/hello?newsletter=check-mail#newsletter`.
4. The page shows the result. Reloading it does not send the form again.

The modules' components render complete forms, such as `{{commentThread "comments" .ID}}` or `{{newsletterForm "newsletter"}}` (see [Templates](templates.md)). With your own markup, the module's data gives the form as `.Form`: `{{form_open .Form}}` … `{{form_close}}` writes the form with the address it posts to, and `{{form_field}}` renders a field as the module checks it (see [The fields](#the-fields)). The module's README lists the fields it reads and the results it returns. A signup form for the newsletter module:

```gotemplate
{{with newsletter "newsletter"}}
    <div id="{{.Key}}">
        {{if eq .Result "check-mail"}}<p>{{tl localizer "Please check your inbox and confirm."}}</p>{{end}}
        {{if eq .Result "invalid"}}<p>{{tl localizer "Please enter a valid e-mail address."}}</p>{{end}}

        {{form_open .Form}}
            {{form_field .Form "locale" nil}}
            {{form_field .Form "email" nil}}
            <button type="submit">{{tl localizer "Subscribe"}}</button>
        {{form_close}}
    </div>
{{end}}
```

The form posts to `/_/newsletter/subscribe`. The `locale` field is hidden: it sends the page's language, the language of the confirmation mail.

## The hidden fields

`{{form_open .Form}}` writes two hidden fields after the form tag:

- a spam trap: go-form's honeypot, a text input named `website` (`frontend.Honeypot`), hidden from screen readers and skipped by the keyboard. Its class, `form-honeypot`, moves it off screen. People leave it empty; many bots fill it in.
- `back`: the path of the page the form is on, with the key of the module instance as anchor, such as `/en/news/hello#newsletter`, so the module knows where to return.

The anchor returns the visitor to the element whose `id` is the key, so a form further down the page stays in view, also without JavaScript. Give the form, or the element around it, that `id`, as `<div id="{{.Key}}">` above. The key also goes before the ids of the form's fields: the e-mail field of the instance `newsletter` has the id `newsletter-email`, so the forms of two instances on one page keep ids of their own.

A template that writes the form tag itself puts `{{form_hidden .Form}}` inside it, which writes the same two fields and nothing else:

```gotemplate
{{with newsletter "newsletter"}}
    <form method="post" action="{{.Action}}" class="signup">
        {{form_hidden .Form}}
        <input type="email" name="email" required>
        <button type="submit">{{tl localizer "Subscribe"}}</button>
    </form>
{{end}}
```

On the home page, `form_hidden` writes:

```html
<input type="text" name="website" value="" tabindex="-1" autocomplete="off" aria-hidden="true" class="form-honeypot">
<input type="hidden" name="back" value="/en/#newsletter">
```

The trap has no `style` attribute: `webthings.css` hides the class `form-honeypot`, so the trap stays hidden under a Content-Security-Policy without `'unsafe-inline'` (see [A Content-Security-Policy of the site's own](deployment.md#a-content-security-policy-of-the-sites-own)). A theme without `webthings.css` hides that class in its own stylesheet. A site with a go-form theme of its own (`Config.FormTheme`) sets the theme's `Honeypot` to a class its stylesheet hides, such as go-form's `templates.HoneypotClass`, `form-honeypot`; without it, the trap gets `form-honeypot` and an inline `style` that hides it. go-form's Bootstrap theme gives the trap `visually-hidden`, its Tailwind themes `sr-only`. See [Configuration](configuration.md#templates).

A module checks the trap with `frontend.IsSpam(r)`, which is go-form's `form.IsSpam` for that field: a value in the post or in the address counts. The bundled modules answer bots that fill it in as they would answer a person, but store nothing, so the bots learn nothing. When `IsSpam` reports true, it also tells the site's output cache that the post changed nothing.

`frontend.BackPath(r)` is the page's path from `back`, or from the `Referer` header as with `frontend.Back` below, without query or fragment, or `""` when it is not a path on the site. `env.Frontend.BackLocale(r)` is that page's language: its first path segment when that is an enabled language, and the site's default language otherwise. The comments module stores a message with both. The bundled modules write their confirmations to visitors in that language.

## The fields

A module reads a post into a Go struct whose go-form tags say what each field takes. The bundled modules' structs are `comments.PostInput`, `submissions.ProposalInput`, `registrations.SignupInput` and `CancelInput`, `newsletter.SubscribeInput` and `polls.VoteInput`. Where a theme writes the form itself, the module's data offers it as `.Form`: the newsletter signup (`{{newsletter}}`), a registration's sign-up (`{{registration}}`) and the cancel form on the registration's page, the submissions form, a poll, and the comments module's list and form. `{{commentInput "remarks" .ID}}` gives the comment form of any post, and `{{submitInput "tips"}}` the submissions form of the instance `tips`. A poll's form returns to its `.Anchor`, `<key>-<ID>`, rather than to the key, since a page can show several polls. The registrations module's lookup page offers its form as `.Form` too, of `registrations.LookupInput`: it is sent with `GET` to the page itself, with the code as `?code=`, so it has no hidden fields, and its field's id is `registration-code`.

`{{form_field .Form "email" nil}}` renders one field: its label, its help text and the input with the limits the module checks, such as `required` and `maxlength`, and hints such as `autocomplete`. The browser then checks what the module checks, and a limit changes in one place. `form_field` takes the field's name in the post, such as `email`, not the name of the Go field: `"Email"` fails the template with an error that lists the names the form has, `form: no field "Email" (fields: email, name, locale)`. Its last argument holds errors to show at the field. A visitor form passes `nil`: after a post with mistakes the form brings the visitor's values and the messages itself (see [A post with mistakes](#a-post-with-mistakes)). A field written by hand with the same name works as well, for example when its limits depend on the data.

The fields render with go-form's markup and the classes of `webthings.css`, such as `wt-form__field`, `wt-form__label` and `wt-form__input`. Radio and checkbox groups, such as a poll's options or go-form's `multicheckbox`, render as a `fieldset` with their label as the `legend`: the fieldset has the field's classes and `wt-form__fieldset`, which removes the browser's border and padding, and the legend has the label's. A site with CSS of its own registers a go-form theme with its classes and names it in `Config.FormTheme`; see [Themes](themes.md#forms). Such a theme adds classes of its own to the fieldset and the legend with go-form's `ThemeClasses.Fieldset` and `Legend`, or its CSS resets `fieldset` and styles `legend` like `label`. Labels and help texts are translated with the site's `.po` files, and go-form's own texts, such as the "(required)" that screen readers announce, with go-form's catalogue; see [Languages](languages.md#translating-the-templates-texts). [Template functions](template-functions.md#forms) lists `form_field` and go-form's other functions: `form_open`, `form_hidden`, `form_errors`, `form_buttons`, `form_close` and `form_render`. `form_render` writes the whole form, with the same hidden fields as `form_open`.

## Results on the page

`frontend.Back(w, r, key, result)` sends the visitor back to the `back` path with `?<key>=<result>` added. A `#fragment` is kept. Only paths on the site itself are followed. Without such a `back` field, as when a post over the body limit could not be read, the visitor returns to the address in the `Referer` header when that is on the site, and to `/` otherwise.

`frontend.BackWith` adds more query values, for example a code the page shows:

```go
frontend.BackWith(w, r, "signup", "registered", url.Values{"code": {code}})
```

Modules give the result to their templates, usually as `.Result`, or through a function such as `{{newsletterResult "newsletter"}}`. They only pass on results they know, so a made-up value in the address shows nothing. `frontend.Result(r, key, allowed...)` reads a result that way: it returns `""` for anything not in `allowed`.

## A post with mistakes

A post that fails its checks returns to its page with a result such as `invalid`. Called before `Back`, `env.Frontend.KeepForm(w, r, key, model, errs)` also keeps the post for that page: the form that `frontend.VisitorForm` makes there for the same key and action then shows the visitor's values again, with go-form's messages at the fields that need a correction. `model` is the input the post was read into, a go-form `form.Schema`, or the form `VisitorForm` made, and `errs` are the errors to show, such as those of `BindForm`. A post refused for another reason passes `nil` and shows its values alone. The handler in [A form of your own](#a-form-of-your-own) keeps its post this way, and so do the comments, newsletter, registrations (sign-up and cancel) and submissions modules. They still return their result, such as `?newsletter=invalid`, for the notice above the form. A kept post changed nothing on the site, and `KeepForm` says so (`frontend.NothingChanged`), so the site's output cache keeps its pages; see [Caching and testing a module](module-caching-and-testing.md#the-output-cache).

Templates need no change: `form_field` and `form_render` with `nil` as their errors show the kept post. This works without JavaScript and with HTMX, which follows the redirect and swaps in the form as the page renders it. A form in a fragment that loads after the page, placed with `deferredSlot`, does not show the kept post: the request for the page takes it before the fragment's own request comes. Place such a form with `slot` or `requiredSlot`.

The post waits on the server, in core's table `wt_kept_form`, for five minutes at most. The visitor gets a cookie for the form's page only, `wt_form_<key>` (`frontend.KeptFormCookie` followed by the key), that holds a random token and nothing else. The table keeps the token's SHA-256, not the token, so its rows do not give away the posts that wait. The cookie is `HttpOnly` and `SameSite=Lax`, `Secure` when the request came over HTTPS, and lasts five minutes. The kept post holds only the values of the form's own fields, never passwords, files or the spam trap, and at most 128 KiB in all; a larger post keeps its messages and leaves out its longest values. The first `GET` of the page with the cookie takes the post, which removes it, and clears the cookie, so the post shows once; a prefetch leaves it for the visit itself. A token that is unknown, older than five minutes, or kept for another form or page shows nothing and changes nothing about the answer. A page view that takes a kept post is sent with `Cache-Control: private, no-store`, and the site's output cache neither answers nor keeps a request with such a cookie (see [Caching and testing a module](module-caching-and-testing.md#the-output-cache)), so no cache keeps the visitor's values. One visitor, known by a hash of their address, has at most ten posts kept at a time; a newer post removes the oldest. Posts nobody came back for are removed when other posts are kept, and every hour. When the form was on no page of the site, nothing is kept and the visitor returns with the result alone.

When several forms on a page share the key and the action, such as a comment form below each post, only the one whose hidden fields hold the posted values, here the post's ID in `subject`, shows the post.

## With HTMX

A form can also post with HTMX and update only its own part of the page. Put the form in a fragment of the theme named after the module instance, such as `fragments/remarks.gohtml` with the `id` `remarks`, and aim it at the fragment. `form_open` takes more attributes after the model, as name and value pairs or as `submitAttrs` writes them:

```gotemplate
{{with commentInput "remarks" .ID}}
    {{form_open . "class" "comment-form" (submitAttrs runtime (commentURL "remarks") "target" "#remarks" "swap" "outerHTML")}}
        {{form_field . "subject" nil}}
        {{form_field . "name" nil}}
        {{form_field . "body" nil}}
        <button type="submit">{{tl localizer "Post comment"}}</button>
    {{form_close}}
{{end}}
```

A `class` adds to the form theme's class, here `wt-form comment-form`. The `subject` field is hidden and holds the post's ID. `submitAttrs` writes `hx-post`, `hx-target` and `hx-swap`; see [Template functions](template-functions.md#submitattrs). The module redirects as above, the browser follows with the same target, and the page answers with the fragment alone, now with the result. Without JavaScript the same form posts and returns the usual way, to the same element, `#remarks`. A poll is not a fragment of its own, so its form aims at its `.Anchor` and picks that element out of the page with `hx-select`. See [HTMX](htmx.md#forms-with-htmx).

## Throttling

`frontend.Limits` puts a module's limits around its visitor actions, always in the same order: first the size of the request body, then how often each visitor may post. It is middleware for the routes of the module's action router, `env.Actions()`:

```go
actions := env.Actions()

actions.Use(frontend.Limits(env.Key, frontend.ActionLimits{
    Body:   64 << 10,
    Posts:  5,
    Window: 10 * time.Minute,
}))
actions.Post("/post", post).As("post")
```

`actions.Use` comes before the routes and limits all of them. To limit one route only, add the limits to that route: `actions.Post("/subscribe", subscribe).Use(frontend.Limits(…))`. The newsletter module limits only its signups this way, since one-click unsubscribes come from a few mail providers' servers. `frontend.Actions(key, handler, limits)` puts the same limits around any other handler.

`Body` is the largest request body in bytes. It can only lower the site's limit for actions (see [A form of your own](#a-form-of-your-own)), which applies first: a larger `Body` has no effect, and zero keeps the site's limit. Each visitor may post `Posts` times per `Window`; zero posts or a zero `Window` means no throttling, and `GET`, `HEAD`, `OPTIONS` and `TRACE` requests are not counted. A visitor over the limit returns to the form with the result `throttled` (`frontend.ResultThrottled`), so the page can say so. A `Throttled` handler answers them another way instead. The counts are kept in memory: a restart clears them, and several servers each count their own posts.

`frontend.Throttle(limit, window, handler)` throttles without the body limit, and answers `429 Too Many Requests` over the limit, or with the handler of `frontend.OnLimit`. A limit or window of zero does not throttle. The throttling is go-router's `middleware.RateLimit`.

The bundled modules use these limits per visitor:

| Module | Posts |
| --- | --- |
| [comments](../modules/comments/README.md) | 5 per 10 minutes |
| [forms](../modules/forms/README.md) | 10 per 10 minutes |
| [newsletter](../modules/newsletter/README.md) | 20 signups per hour |
| [polls](../modules/polls/README.md) | 20 votes per minute |
| [registrations](../modules/registrations/README.md) | 10 per 10 minutes |
| [submissions](../modules/submissions/README.md) | 5 per hour |

## Cross-site protection

Posts to `/_/` from another site are refused with `403 Forbidden` (Go's `http.CrossOriginProtection`). Browsers tell where a post comes from in the `Sec-Fetch-Site` and `Origin` headers, so this needs no token or cookie: a form never expires, and pages with forms can be cached. A form on another subdomain of your domain counts as another site.

The protection stops other websites from posting in a visitor's name. Scripts that post directly send no such headers and pass; the spam trap and throttling deal with them.

## Knowing the visitor

`frontend.ClientIP(r)` is the visitor's IP address. Throttling and the stats use it.

`env.VisitorID(ctx, r, parts...)` tells visitors apart without storing their address: an HMAC-SHA256 of the parts and the address, keyed with the instance's `visitors` secret, as 64 hexadecimal digits. It differs per module instance, and nobody can find the address from it by trying them all. With the date as a part, `time.Now().UTC().Format(time.DateOnly)`, it changes every day. The polls use it, next to a cookie, for one vote per visitor, and the comments keep a daily one with each message.

Behind a reverse proxy, every request comes from the proxy. List it in `Config.TrustedProxies`, as IP addresses or CIDR ranges:

```go
app := webthings.New(webthings.Config{
    TrustedProxies: []string{"127.0.0.1", "10.0.0.0/8"},
})
```

Only requests from these addresses have their `X-Forwarded-For`, `X-Real-IP` and `Forwarded` headers believed. `X-Forwarded-For` is read from the right, skipping trusted proxies; the first other address is the visitor, so a visitor cannot pose as another address by sending the header themselves. `X-Real-IP` is used when there is no `X-Forwarded-For`, and the `for=` entries of `Forwarded` (RFC 7239) when there is neither, read like `X-Forwarded-For`. The visitor's address also replaces `r.RemoteAddr`, with port 0, such as `198.51.100.7:0`. The `X-Forwarded-Proto` and `X-Forwarded-Host` headers of these proxies also give the site's address, or else the `proto=` and `host=` of their `Forwarded`, for absolute links when `Config.BaseURL` is empty.

Without the setting the headers are ignored, since anyone can send them. Behind a proxy that means all visitors share one address, and the throttles count them together. See [Deployment](deployment.md).

## A form of your own

A site adds its own form with a small module that registers a visitor action on its action router, `env.Actions()`, as the bundled modules do. This one takes "call me back" requests at `/_/callback/request`:

```go
// CallbackInput is the form of a "call me back" request: a phone number on
// one line, at most 50 characters.
type CallbackInput struct {
    Phone string `form:"input,tel" name:"phone" label:"Phone" required:"true" maxLength:"50" line:"true" autocomplete:"tel"`
}

func callback() webthings.Module {
    // actions is the module's action router, from Setup.
    var actions *router.Router

    return webthings.Module{
        Type: "callback",
        Key:  "callback",
        // The form's label, for task translations.
        Forms: []any{CallbackInput{}},
        Funcs: template.FuncMap{
            // {{callbackForm}} is the form: it posts to the action named
            // request and returns the visitor to the element with the id callback.
            "callbackForm": func(ctx context.Context) form.RenderModel {
                return frontend.VisitorForm(ctx, CallbackInput{}, actions.RoutePath("request"), "callback")
            },
            // {{callbackResult}} is the result in the page's address, such as "sent";
            // "" for a result the module does not know.
            "callbackResult": func(ctx context.Context) string {
                if r := frontend.Request(ctx); r != nil {
                    return frontend.Result(r, "callback", "sent", "invalid", frontend.ResultThrottled)
                }

                return ""
            },
        },
        Setup: func(env webthings.Env) (webthings.Parts, error) {
            actions = env.Actions()

            // At most 16 KiB per post, and 5 posts per visitor per 10 minutes.
            actions.Use(frontend.Limits("callback", frontend.ActionLimits{
                Body:   16 << 10,
                Posts:  5,
                Window: 10 * time.Minute,
            }))
            actions.Post("/request", func(w http.ResponseWriter, r *http.Request) {
                if frontend.IsSpam(r) {
                    frontend.Back(w, r, "callback", "sent")

                    return
                }

                var in CallbackInput

                errs, err := env.Frontend.BindForm(r, &in)
                if err != nil || len(errs) > 0 {
                    env.Frontend.KeepForm(w, r, "callback", in, errs)
                    frontend.Back(w, r, "callback", "invalid")

                    return
                }

                // Store in.Phone, or mail it to the office (see mail.md).

                frontend.Back(w, r, "callback", "sent")
            }).As("request")

            return webthings.Parts{}, nil
        },
    }
}
```

`router` is [go-router](https://github.com/donseba/go-router) and `form` is go-form (`github.com/donseba/go-form/v2`). Add the module with `app.Use(callback())`, and show the form in any template:

```gotemplate
{{$form := callbackForm}}
{{$result := callbackResult}}
<div id="callback">
    {{if eq $result "sent"}}<p>{{tl localizer "Thank you. We will call you soon."}}</p>{{end}}
    {{if eq $result "invalid"}}<p>{{tl localizer "Please enter your phone number."}}</p>{{end}}
    {{if eq $result "throttled"}}<p>{{tl localizer "Please try again later."}}</p>{{end}}

    {{form_open $form}}
        {{form_field $form "phone" nil}}
        <button type="submit">{{tl localizer "Call me back"}}</button>
    {{form_close}}
</div>
```

`env.Actions()` is a go-router mounted at `/_/<key>/`, so its routes are relative to that and become part of the site's routes. A handler sees the whole path, such as `/_/callback/request`, and `r.PathValue` gives the route's wildcards. A path without a route gets a plain 404, and a path that only has routes for other methods, such as a `GET` of `/_/callback/request`, gets 405. Route names are shared by the whole site, and a module may run more than once, so core puts the key in front: the route named `request` is `callback.request` on the site. The module asks its own router by the short name: `actions.RoutePath("request")` is `/_/callback/request`, and `actions.RoutePathArgs` fills in wildcards and adds a query. This needs no request, so it also gives the links in e-mail (see [E-mail](mail.md#mail-from-your-own-module)).

`frontend.VisitorForm(ctx, model, action, key)` makes the form of `model` for the templates: it posts to `action`, carries the spam trap, and has the page being rendered with `#<key>` as its `back` field, as in [The hidden fields](#the-hidden-fields). The field of the example has the id `callback-phone`.

Core adds the cross-site protection to every action, and limits the request body to 1 MiB (`webthings.DefaultActionBodyLimit`); `Parts.ActionBodyLimit` sets another limit, for example for uploads. The example sets a lower limit of its own in `ActionLimits`, as the bundled modules do; a higher `Body` there has no effect, so a module that takes more raises `Parts.ActionBodyLimit` instead.

`env.Frontend.BindForm(r, &in)` reads the post into `CallbackInput` and checks it against the tags, as the bundled modules do: here a phone number that is not empty, at most 50 characters and one line, without tabs, line breaks or other control characters. It trims the spaces around text and reads line breaks as `\n`, as browsers count them for `maxlength`. A value that cannot be read as its field's type, such as `abc` for a number, is an error of that field with the rule `invalid`. Every error names the rule it broke (`form.FieldValidationError.Rule`), for a module that answers with a result per rule. `err` is only set when the post cannot be read at all. The messages are in the language of the page the form was on (`BackLocale`), translated with the site's translations. `env.Frontend.KeepForm` then keeps a post with mistakes for that page, so the form shows the number and its message again there; see [A post with mistakes](#a-post-with-mistakes). The same tags give the theme's field its `required`, `maxlength` and `autocomplete`, and `Forms` lets `task translations` find its label when a template uses `form_open`, `form_field` or `form_render`. [Writing a module](writing-a-module.md) covers storage and the rest of a module, and [The admin of a module](module-admin.md) its admin sections.

An e-mail address that mail will go to gets `validate:"mail"` (`mail.Rule`) next to `form:"input,email"`, as the bundled modules' e-mail fields do: it refuses an address that mail cannot reach, such as one without a dot; see [E-mail](mail.md#addresses-in-forms). A `form:"input,datetime-local"` field is read and shown in the site's timezone, as in the admin, so a visitor enters the site's local time; see [The admin of a module](module-admin.md#screens-of-your-own).

When something else fails, such as storing the post, the handler answers with `env.Frontend.Fail(w, r, err)`, as the bundled modules do. It logs the error with the module's key and answers with status 500 and the theme's `500.gohtml`, in the language of the form's page and with the theme's asset versions, as its pages render. Without a `500.gohtml`, go-partial's plain error page answers, with its heading and message translated and the page's language as its `lang`; see [Templates](templates.md#500gohtml). An HTMX post gets the same page, which the client shows as the whole page with `reloadOnError`; see [HTMX](htmx.md#forms-with-htmx).

## Forms built by editors

The [forms module](../modules/forms/README.md) lets editors build contact forms and other forms in the admin, with an inbox and a notification mail. Its posting differs from the visitor actions above:

- The form is shown on the page the instance is placed on, and posts to that same page (`.Action`), not to `/_/`.
- With errors, the page shows the form again at once, with the visitor's answers and an error per field (status 422). There is no redirect, so it needs no `KeepForm`.
- After a valid post the visitor returns to the page with `?sent=1`.
- `.Form` is the form as the editor built it, which a theme renders with `{{form_render .Form .Errors}}`, or with `form_open` and `form_field` for each name in `.Fields`. Both write the spam trap; there is no `back` field, because the form returns to its own page.
- Over the limit, the form is shown again with `.Throttled` set and the answers kept (status 429).
- An `email` field takes one address mail can be sent to (`mail.Rule`), as the e-mail fields of the other forms do, since a reply to the notification goes to it. The module sets the rule with go-form's `Spec.Validate`.

Cross-site protection, the spam trap and throttling work the same way, so the form also needs no token and never expires.

## Next steps

- [E-mail](mail.md) covers the confirmations and notifications that forms send.
- [Using modules](modules.md) lists the modules with visitor forms.
- [Writing a module](writing-a-module.md) explains `Parts` and the module's `Setup`.
