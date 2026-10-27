package banners

import (
	"context"
	"html/template"
	"log/slog"

	"github.com/donseba/go-webthings/frontend"
)

// funcs are the template functions Banners offers to every template.
//
// go-clue:funcmap
func funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string) []github.com/donseba/go-webthings/modules/banners.Banner
		"banners": active,
		//go-clue:sig func(key string) *github.com/donseba/go-webthings/modules/banners.Banner
		"banner": first,
	}
}

// active returns the active banners of an instance in their order, for
// example {{range banners "hero"}} on the home page.
func active(ctx context.Context, key string) []Banner {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	list, err := service.Active(ctx, frontend.Locale(ctx))
	if err != nil {
		slog.ErrorContext(ctx, "template banners", "module", key, "error", err)
	}

	return list
}

// first returns the first active banner of an instance, or nil, for example
// {{with banner "notice"}} in the layout.
func first(ctx context.Context, key string) *Banner {
	list := active(ctx, key)
	if len(list) == 0 {
		return nil
	}

	return &list[0]
}
