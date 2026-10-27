package submissions

import (
	"context"
	"html/template"

	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
)

// funcs are the template functions Submissions offers to every template.
//
// go-clue:funcmap
func funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string) string
		"submitURL": submitURL,
		//go-clue:sig func(key string) string
		"submitResult": submitResult,
		//go-clue:sig func(key string) github.com/donseba/go-form/v2.RenderModel
		"submitInput": submitInput,
	}
}

// submitInput is the model of the proposal form, for a form in any template
// with go-form's functions, such as {{form_open (submitInput "tips")}}; the
// zero value for an unknown instance.
func submitInput(ctx context.Context, key string) form.RenderModel {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return form.RenderModel{}
	}

	return service.proposalForm(ctx)
}

// submitURL is the address a proposal form posts to, for example
// <form method="post" action="{{submitURL "tips"}}" enctype="multipart/form-data">,
// or "" for an unknown instance.
func submitURL(ctx context.Context, key string) string {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return ""
	}

	return service.submitURL()
}

// submitResult is "sent", "invalid" or "throttled" on the page the visitor
// returns to after posting a proposal, and "" otherwise, for example
// {{if eq (submitResult "tips") "sent"}}Thank you!{{end}}.
func submitResult(ctx context.Context, key string) string {
	request := frontend.Request(ctx)
	if request == nil {
		return ""
	}

	return frontend.Result(request, key, results...)
}
