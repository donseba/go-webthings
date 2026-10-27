package newsletter

import (
	"context"
	"html/template"

	"github.com/donseba/go-webthings/frontend"
)

// funcs are the template functions Newsletter offers to every template.
//
// go-clue:funcmap
func funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string) *github.com/donseba/go-webthings/modules/newsletter.SignupView
		"newsletter": signup,
		//go-clue:sig func(key string) string
		"newsletterURL": actionURL,
		//go-clue:sig func(key string) string
		"newsletterResult": visitorResult,
		//go-clue:sig func(key string) html/template.HTML
		"newsletterForm": newsletterForm,
	}
}

// signup returns the signup heading, introduction, form address and result of
// an instance in the page's language, for example
// {{with newsletter "news"}}<h2>{{.Title}}</h2>{{end}}; nil for unknown keys.
func signup(ctx context.Context, key string) *SignupView {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	view := service.view(ctx, frontend.Request(ctx), frontend.Locale(ctx))

	return &view
}

// actionURL is the address signup forms post to, for example
// <form method="post" action="{{newsletterURL "news"}}">, or "" for an
// unknown instance.
func actionURL(ctx context.Context, key string) string {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return ""
	}

	return service.subscribeURL()
}

// visitorResult is the result a visitor returned with: "check-mail",
// "confirmed", "unsubscribed", "invalid", "throttled" or "".
func visitorResult(ctx context.Context, key string) string {
	return result(frontend.Request(ctx), key)
}
