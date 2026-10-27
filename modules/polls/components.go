package polls

import (
	"context"
	"errors"
	"html/template"
	"log/slog"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
)

// components are the parts of a page Polls offers to every template.
func components() []frontend.Template {
	return []frontend.Template{
		{Name: "box", Data: Poll{}, Example: boxComponent},
	}
}

// pollBox renders the box component of one published poll, by slug or ID,
// or of the newest one when slugOrID is empty, for example {{pollBox
// "survey" "favourite-colour"}}. Without such a poll it renders nothing.
func pollBox(ctx context.Context, key, slugOrID string) (template.HTML, error) {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return "", nil
	}

	r := frontend.Request(ctx)
	locale := frontend.Locale(ctx)

	if slugOrID == "" {
		newest, err := service.Latest(ctx, r, locale, 1)
		if err != nil {
			slog.ErrorContext(ctx, "template pollBox", "module", key, "error", err)
		}

		if len(newest) == 0 {
			return "", nil
		}

		return frontend.Component(ctx, key, "box", newest[0])
	}

	poll, err := service.Find(ctx, r, locale, slugOrID)
	if err != nil {
		if !errors.Is(err, content.ErrNotFound) {
			slog.ErrorContext(ctx, "template pollBox", "module", key, "error", err)
		}

		return "", nil
	}

	return frontend.Component(ctx, key, "box", poll)
}

// boxComponent is the default polls/box.gohtml.
const boxComponent = `{{/* @dot github.com/donseba/go-webthings/modules/polls.Poll */}}
{{/* One poll: the vote form, then the results when they may be shown: {{pollBox "survey" "slug"}}. */}}
<section class="wt-poll" id="{{.Anchor}}" aria-labelledby="{{.Anchor}}-question">
    <h3 class="wt-poll__question" id="{{.Anchor}}-question">{{.Question}}</h3>

    {{with .Result}}
        <p class="wt-notice" role="status">
            {{if eq . "voted"}}{{tl localizer "Thank you for voting."}}
            {{else if eq . "already"}}{{tl localizer "You already voted on this question."}}
            {{else if eq . "closed"}}{{tl localizer "This poll is closed."}}
            {{else if eq . "throttled"}}{{tl localizer "Too many votes. Please try again later."}}
            {{else}}{{tl localizer "Please choose one of the options."}}{{end}}
        </p>
    {{end}}

    {{if .CanVote}}
        {{/* The form returns the visitor to the element with the id {{.Anchor}}. */}}
        {{form_open .Form}}
            {{form_field .Form "poll" nil}}
            {{/* The options as radio buttons, labelled with the question. */}}
            {{form_field .Form "option" nil}}

            <button class="wt-button" type="submit">{{tl localizer "Vote"}}</button>
        {{form_close}}
    {{end}}

    {{if .ShowResults}}
        <ul class="wt-poll__results">
            {{range .Options}}
                <li class="wt-poll__result{{if .Chosen}} wt-poll__result--chosen{{end}}">
                    <span class="wt-poll__label">{{.Label}}{{if .Chosen}} <span class="wt-meta">({{tl localizer "your choice"}})</span>{{end}}</span>
                    <span class="wt-poll__percent">{{.Percent}}%</span>
                    <meter class="wt-poll__bar" min="0" max="100" value="{{.Percent}}" aria-hidden="true">{{.Percent}}%</meter>
                </li>
            {{end}}
        </ul>

        <p class="wt-poll__total wt-meta">{{tl localizer "Votes: %d" .Total}}</p>
    {{end}}

    {{if .Closed}}
        <p class="wt-meta">{{tl localizer "Voting has closed."}}</p>
    {{else if not .ClosesAt.IsZero}}
        <p class="wt-meta">{{tl localizer "Voting closes on %s." (date .ClosesAt)}}</p>
    {{end}}
</section>
`
