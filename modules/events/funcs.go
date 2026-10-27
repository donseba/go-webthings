package events

import (
	"context"
	"html/template"
	"log/slog"

	"github.com/donseba/go-webthings/frontend"
)

// funcs are the template functions Events offers to every template.
//
// go-clue:funcmap
func funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string, limit int) []github.com/donseba/go-webthings/modules/events.Event
		"events": upcoming,
	}
}

// upcoming returns the next events of an instance, for example
// {{range events "agenda" 5}} on the home page; a limit of 0 or less returns
// all of them.
func upcoming(ctx context.Context, key string, limit int) []Event {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	list, err := service.Upcoming(ctx, frontend.Locale(ctx), limit)
	if err != nil {
		slog.ErrorContext(ctx, "template events", "module", key, "error", err)
	}

	return list
}
