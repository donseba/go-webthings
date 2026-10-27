package stats

import (
	"context"
	"html/template"
	"log/slog"

	"github.com/donseba/go-webthings/frontend"
)

// funcs are the template functions Stats offers to every template.
//
// go-clue:funcmap
func funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string, path string) int
		"pageViews": pageViews,
		//go-clue:sig func(key string, limit int, days int, within ...string) []github.com/donseba/go-webthings/modules/stats.Page
		"popular": popular,
	}
}

// pageViews is how often path was viewed, for example
// {{pageViews "stats" .URL}}. An empty path is the page being rendered.
func pageViews(ctx context.Context, key, path string) int {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return 0
	}

	if path == "" {
		request := frontend.Request(ctx)
		if request == nil {
			return 0
		}

		path = request.URL.Path
	}

	views, err := service.Views(ctx, path)
	if err != nil {
		slog.ErrorContext(ctx, "template pageViews", "module", key, "error", err)
	}

	return views
}

// popular returns the limit most viewed pages in the current language of the
// last days days, for example {{range popular "stats" 5 30}}; a limit of 0
// or less lists all of them. An optional
// path or module instance key limits it to the pages below that page, for
// example {{range popular "stats" 5 30 "news"}}; see Service.PopularIn.
func popular(ctx context.Context, key string, limit, days int, within ...string) []Page {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	var (
		pages []Page
		err   error
	)

	switch {
	case len(within) > 0 && within[0] != "":
		pages, err = service.PopularIn(ctx, frontend.Locale(ctx), within[0], limit, days)
	default:
		pages, err = service.Popular(ctx, frontend.Locale(ctx), limit, days)
	}

	if err != nil {
		slog.ErrorContext(ctx, "template popular", "module", key, "error", err)
	}

	return pages
}
