package polls

import (
	"context"
	"errors"
	"html/template"
	"log/slog"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
)

// funcs are the template functions Polls offers to every template.
//
// go-clue:funcmap
func funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string, slugOrID string) *github.com/donseba/go-webthings/modules/polls.Poll
		"poll": find,
		//go-clue:sig func(key string, limit int) []github.com/donseba/go-webthings/modules/polls.Poll
		"polls": latest,
		//go-clue:sig func(key string) string
		"pollURL": actionURL,
		//go-clue:sig func(key string, slugOrID string) html/template.HTML
		"pollBox": pollBox,
	}
}

// find returns one published poll for the current visitor, for example
// {{with poll "survey" "favourite-colour"}}, or nil.
func find(ctx context.Context, key, slugOrID string) *Poll {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	poll, err := service.Find(ctx, frontend.Request(ctx), frontend.Locale(ctx), slugOrID)
	if err != nil {
		if !errors.Is(err, content.ErrNotFound) {
			slog.ErrorContext(ctx, "template poll", "module", key, "error", err)
		}

		return nil
	}

	return &poll
}

// latest returns up to limit published polls, newest first unless editors
// reordered them, for example {{range polls "survey" 1}} on the home page; a
// limit of 0 or less returns all of them.
func latest(ctx context.Context, key string, limit int) []Poll {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	list, err := service.Latest(ctx, frontend.Request(ctx), frontend.Locale(ctx), limit)
	if err != nil {
		slog.ErrorContext(ctx, "template polls", "module", key, "error", err)
	}

	return list
}

// actionURL is the address vote forms post to, for example
// <form method="post" action="{{pollURL "survey"}}">, or "" for an unknown
// instance.
func actionURL(ctx context.Context, key string) string {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return ""
	}

	return service.voteURL()
}
