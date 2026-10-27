package publishing

import (
	"context"
	"html/template"
	"log/slog"

	"github.com/donseba/go-webthings/frontend"
)

// funcs are the template functions Publishing offers to every template.
//
// go-clue:funcmap
func funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string, limit int) []github.com/donseba/go-webthings/modules/publishing.Post
		"posts": posts,
	}
}

// posts returns up to limit of the newest published posts of an instance,
// for example {{range posts "news" 3}} on the home page. A limit of 0 or less
// returns all of them. URLs are empty while the instance is on no page.
func posts(ctx context.Context, key string, limit int) []Post {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	list, err := service.Latest(ctx, frontend.Locale(ctx), limit)
	if err != nil {
		slog.ErrorContext(ctx, "template posts", "module", key, "error", err)
	}

	return list
}
