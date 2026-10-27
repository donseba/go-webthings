package newsletter

import (
	"context"
	"html/template"

	"github.com/donseba/go-webthings/frontend"
)

// components are the parts of a page Newsletter offers to every template.
func components() []frontend.Template {
	return []frontend.Template{
		{Name: "form", Data: SignupView{}, Example: formComponent},
	}
}

// newsletterForm renders the form component, for example {{newsletterForm
// "news"}} in the footer.
func newsletterForm(ctx context.Context, key string) (template.HTML, error) {
	view := signup(ctx, key)
	if view == nil {
		return "", nil
	}

	return frontend.Component(ctx, key, "form", *view)
}

// formComponent is the default newsletter/form.gohtml.
const formComponent = `{{/* @dot github.com/donseba/go-webthings/modules/newsletter.SignupView */}}
{{/* A signup box for any page, with the result of the visitor's signup: {{newsletterForm "news"}}. */}}
<section class="wt-newsletter" id="{{.Key}}" aria-labelledby="{{.Key}}-signup-title">
    <h2 class="wt-newsletter__title" id="{{.Key}}-signup-title">{{.Title}}</h2>

    {{with .Result}}
        <p class="wt-notice" role="status">
            {{if eq . "check-mail"}}{{tl localizer "Almost done: check your inbox and confirm your subscription."}}
            {{else if eq . "confirmed"}}{{tl localizer "Your subscription is confirmed."}}
            {{else if eq . "unsubscribed"}}{{tl localizer "You are unsubscribed."}}
            {{else if eq . "throttled"}}{{tl localizer "Too many attempts. Please try again later."}}
            {{else}}{{tl localizer "That did not work. Please check your e-mail address or link."}}{{end}}
        </p>
    {{end}}

    {{with .Intro}}<div class="wt-newsletter__intro">{{.}}</div>{{end}}

    {{/* The form returns the visitor to the element with the id {{.Key}}. */}}
    {{form_open .Form}}
        {{form_field .Form "locale" nil}}
        {{form_field .Form "email" nil}}

        <button class="wt-button" type="submit">{{tl localizer "Subscribe"}}</button>
    {{form_close}}

    <p class="wt-newsletter__note wt-meta">{{tl localizer "You receive an e-mail to confirm your address. Every newsletter has a link to unsubscribe."}}</p>
</section>
`
