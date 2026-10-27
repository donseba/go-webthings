package comments

import (
	"context"
	"html/template"
	"log/slog"

	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
)

// funcs are the template functions Comments offers to every template.
//
// go-clue:funcmap
func funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string, subject string) []github.com/donseba/go-webthings/modules/comments.Comment
		"comments": list,
		//go-clue:sig func(key string, subject string) int
		"commentCount": count,
		//go-clue:sig func(key string) string
		"commentURL": actionURL,
		//go-clue:sig func(key string) string
		"commentResult": result,
		//go-clue:sig func(key string) bool
		"commentsClosed": closed,
		//go-clue:sig func(key string, subject string) html/template.HTML
		"commentThread": commentThread,
		//go-clue:sig func(key string, subject string) html/template.HTML
		"commentForm": commentForm,
		//go-clue:sig func(key string, subject string) github.com/donseba/go-form/v2.RenderModel
		"commentInput": commentInput,
	}
}

// commentInput is the model of the comment form on subject, for a form a
// theme lays out itself with go-form's functions, such as {{form_field
// (commentInput "remarks" .ID) "name" nil}}.
func commentInput(ctx context.Context, key, subject string) form.RenderModel {
	return postForm(ctx, actionURL(ctx, key), key, subject)
}

// list returns the approved comments on subject with their replies, for
// example {{range comments "remarks" .ID}}.
func list(ctx context.Context, key, subject string) []Comment {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	comments, err := service.List(ctx, subject)
	if err != nil {
		slog.ErrorContext(ctx, "template comments", "module", key, "error", err)
	}

	return comments
}

// count returns the number of approved comments on subject, for example
// {{commentCount "remarks" .ID}} next to a post title.
func count(ctx context.Context, key, subject string) int {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return 0
	}

	total, err := service.Count(ctx, subject)
	if err != nil {
		slog.ErrorContext(ctx, "template commentCount", "module", key, "error", err)
	}

	return total
}

// actionURL is the address comment forms post to, for example
// <form method="post" action="{{commentURL "remarks"}}">, or "" for an
// unknown instance.
func actionURL(ctx context.Context, key string) string {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return ""
	}

	return service.postURL()
}

// result is the outcome of the visitor's last post on this page: "sent",
// "pending", "invalid", "closed", "throttled" or "".
func result(ctx context.Context, key string) string {
	request := frontend.Request(ctx)
	if request == nil {
		return ""
	}

	return frontend.Result(request, key, results...)
}

// closed reports whether the instance accepts no new comments, so templates
// can leave out the form.
func closed(ctx context.Context, key string) bool {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return true
	}

	settings, err := service.Settings(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "template commentsClosed", "module", key, "error", err)

		return false
	}

	return settings.Closed
}
