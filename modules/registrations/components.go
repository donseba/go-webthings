package registrations

import (
	"context"
	"html/template"

	"github.com/donseba/go-webthings/frontend"
)

// FormView is the data of the form component: whether and how visitors can
// register for a subject, with the result of the visitor's last post.
type FormView struct {
	// Key is the instance key.
	Key string
	Availability
	// Result is "registered", "full", "closed", "invalid", "cancelled",
	// "throttled" or "".
	Result string
	// Code is the visitor's registration code after registering here, when
	// the instance is placed on no page of its own.
	Code string
	// LookupURL is the page where visitors look up their registration, or ""
	// when the instance is placed on no page.
	LookupURL string
}

// components are the parts of a page Registrations offers to every template.
func components() []frontend.Template {
	return []frontend.Template{
		{Name: "form", Data: FormView{}, Example: formComponent},
	}
}

// registrationForm renders the form component of a subject, for example
// {{registrationForm "signup" .ID}} on an event page. Subjects without
// registration settings render nothing.
func registrationForm(ctx context.Context, key, subjectID string) (template.HTML, error) {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return "", nil
	}

	view := FormView{
		Key:          key,
		Availability: availability(ctx, key, subjectID),
		Result:       result(ctx, key),
		Code:         code(ctx, key),
	}

	// Without a page in this language there is no lookup to link to.
	view.LookupURL, _ = service.pages.MountURL(ctx, key, frontend.Locale(ctx))

	return frontend.Component(ctx, key, "form", view)
}

// formComponent is the default registrations/form.gohtml.
const formComponent = `{{/* @dot github.com/donseba/go-webthings/modules/registrations.FormView */}}
{{/* The sign-up box of a subject: prices, places, the result of the last post and the form: {{registrationForm "signup" .ID}}. */}}
{{if .Configured}}
<section class="wt-registration" id="{{.Key}}" aria-labelledby="{{.Key}}-registration-title">
    <h2 class="wt-registration__title" id="{{.Key}}-registration-title">{{tl localizer "Sign up"}}</h2>

    {{with .Result}}
        <p class="wt-notice" role="status">
            {{if eq . "registered"}}{{tl localizer "Thank you, you are registered. We sent you a confirmation."}}
                {{with $.Code}}{{tl localizer "Your registration code is %s." .}}{{end}}
            {{else if eq . "full"}}{{tl localizer "Sorry, there are not enough places left."}}
            {{else if eq . "closed"}}{{tl localizer "Registration is closed."}}
            {{else if eq . "cancelled"}}{{tl localizer "Your registration is cancelled."}}
            {{else if eq . "throttled"}}{{tl localizer "Too many attempts. Please try again later."}}
            {{else}}{{tl localizer "Please check your name, e-mail address and the number of places."}}{{end}}
        </p>
    {{end}}

    <dl class="wt-registration__facts">
        <dt>{{tl localizer "Price"}}</dt>
        <dd>
            {{if .Prices.Free}}{{tl localizer "Free"}}{{else}}{{money .Prices.Adult .Currency}} {{tl localizer "per person"}}{{end}}
            {{if and .AllowChildren .Prices.Child}}<br>{{money .Prices.Child .Currency}} {{tl localizer "per child"}}{{end}}
        </dd>

        {{if .Capacity}}
            <dt>{{tl localizer "Places left"}}</dt>
            <dd>{{if .Full}}{{tl localizer "None"}}{{else}}{{tl localizer "%d of %d" .Left .Capacity}}{{end}}</dd>
        {{end}}

        {{if not .ClosesAt.IsZero}}
            <dt>{{tl localizer "Sign up before"}}</dt>
            <dd>{{date .ClosesAt}}, {{time .ClosesAt}}</dd>
        {{end}}
    </dl>

    {{with .Notes}}<div class="wt-registration__notes">{{.}}</div>{{end}}

    {{if .Open}}
        {{/* The form returns the visitor to the element with the id {{.Key}}. */}}
        {{form_open .Form}}
            {{form_field .Form "subject" nil}}
            {{form_field .Form "name" nil}}
            {{form_field .Form "email" nil}}

            <div class="wt-form__row">
                {{form_field .Form "adults" nil}}
                {{if .AllowChildren}}{{form_field .Form "children" nil}}{{end}}
            </div>

            {{form_field .Form "notes" nil}}

            <button class="wt-button" type="submit">{{tl localizer "Sign up"}}</button>
        {{form_close}}
    {{else if .Full}}
        <p class="wt-notice">{{tl localizer "There are no places left."}}</p>
    {{else}}
        <p class="wt-notice">{{tl localizer "Registration is closed."}}</p>
    {{end}}

    {{with .LookupURL}}
        <p class="wt-meta">{{tl localizer "Already registered?"}} <a href="{{.}}">{{tl localizer "Look up your registration"}}</a></p>
    {{end}}
</section>
{{end}}
`
