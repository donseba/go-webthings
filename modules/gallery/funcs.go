package gallery

import (
	"context"
	"errors"
	"html/template"
	"log/slog"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
)

// funcs are the template functions Gallery offers to every template.
//
// go-clue:funcmap
func funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string) []github.com/donseba/go-webthings/modules/gallery.Album
		"albums": albums,
		//go-clue:sig func(key string, slug string) *github.com/donseba/go-webthings/modules/gallery.Album
		"album": album,
	}
}

// albums returns the published albums of an instance, without their images.
func albums(ctx context.Context, key string) []Album {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	list, err := service.Albums(ctx, frontend.Locale(ctx))
	if err != nil {
		slog.ErrorContext(ctx, "template albums", "module", key, "error", err)
	}

	return list
}

// album returns one album with its images, or nil, for example for a
// carousel: {{with album "work" "carousel"}}{{range .Images}}...{{end}}{{end}}.
func album(ctx context.Context, key, slug string) *Album {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	found, err := service.Album(ctx, frontend.Locale(ctx), slug)
	if err != nil {
		if !errors.Is(err, content.ErrNotFound) {
			slog.ErrorContext(ctx, "template album", "module", key, "error", err)
		}

		return nil
	}

	return &found
}
