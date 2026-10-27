package directory

import (
	"context"
	"errors"
	"html/template"
	"log/slog"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
)

// funcs are the template functions Directory offers to every template.
//
// go-clue:funcmap
func funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string, category string, limit int) []github.com/donseba/go-webthings/modules/directory.Entry
		"directoryEntries": entries,
		//go-clue:sig func(key string, slug string) *github.com/donseba/go-webthings/modules/directory.Entry
		"directoryEntry": entry,
		//go-clue:sig func(key string) []github.com/donseba/go-webthings/modules/directory.Category
		"directoryCategories": categories,
	}
}

// entries returns up to limit published entries of a category (a slug, or ""
// for all), sorted by name; a limit of 0 or less returns all of them. For
// example {{range directoryEntries "guide" "food" 5}}.
func entries(ctx context.Context, key, category string, limit int) []Entry {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	list, err := service.Entries(ctx, frontend.Locale(ctx), Query{Category: category})
	if err != nil {
		slog.ErrorContext(ctx, "template directoryEntries", "module", key, "error", err)
	}

	if limit > 0 && limit < len(list) {
		list = list[:limit]
	}

	return list
}

// entry returns one published entry by slug, or nil, for example
// {{with directoryEntry "guide" "bakery-jansen"}}{{.Name}}{{end}}.
func entry(ctx context.Context, key, slug string) *Entry {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	found, err := service.Entry(ctx, frontend.Locale(ctx), slug)
	if errors.Is(err, content.ErrNotFound) {
		return nil
	}

	if err != nil {
		slog.ErrorContext(ctx, "template directoryEntry", "module", key, "error", err)

		return nil
	}

	return &found
}

// categories returns the published categories in their order, for example
// {{range directoryCategories "guide"}}{{.Name}} ({{.Count}}){{end}}.
func categories(ctx context.Context, key string) []Category {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	list, err := service.ListCategories(ctx, frontend.Locale(ctx))
	if err != nil {
		slog.ErrorContext(ctx, "template directoryCategories", "module", key, "error", err)
	}

	return list
}
