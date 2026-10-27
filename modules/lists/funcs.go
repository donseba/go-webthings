package lists

import (
	"context"
	"html/template"
	"log/slog"

	"github.com/donseba/go-webthings/frontend"
)

// funcs are the template functions Lists offers to every template.
//
// go-clue:funcmap
func funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string) []github.com/donseba/go-webthings/modules/lists.Entry
		"entries": entries,
	}
}

// entries returns the published entries of a list, for example
// {{range entries "faq"}} on any page.
func entries(ctx context.Context, key string) []Entry {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	list, err := service.Entries(ctx, frontend.Locale(ctx))
	if err != nil {
		slog.ErrorContext(ctx, "template entries", "module", key, "error", err)
	}

	return list
}
