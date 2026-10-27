package people

import (
	"context"
	"errors"
	"html/template"
	"log/slog"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
)

// funcs are the template functions People offers to every template.
//
// go-clue:funcmap
func funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string) []github.com/donseba/go-webthings/modules/people.Person
		"people": all,
		//go-clue:sig func(key string, group string) []github.com/donseba/go-webthings/modules/people.Person
		"peopleIn": inGroup,
		//go-clue:sig func(key string, slug string) *github.com/donseba/go-webthings/modules/people.Person
		"person": bySlug,
	}
}

// all returns the published people of an instance in their order, for
// example {{range people "team"}}.
func all(ctx context.Context, key string) []Person {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	list, err := service.List(ctx, frontend.Locale(ctx))
	if err != nil {
		slog.ErrorContext(ctx, "template people", "module", key, "error", err)
	}

	return list
}

// inGroup returns the published people of one group, for example
// {{range peopleIn "team" "Board"}}.
func inGroup(ctx context.Context, key, group string) []Person {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	list, err := service.InGroup(ctx, frontend.Locale(ctx), group)
	if err != nil {
		slog.ErrorContext(ctx, "template peopleIn", "module", key, "error", err)
	}

	return list
}

// bySlug returns one published person, or nil, for example
// {{with person "team" "jane-doe"}}{{.Name}}{{end}}.
func bySlug(ctx context.Context, key, slug string) *Person {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	person, err := service.BySlug(ctx, frontend.Locale(ctx), slug)
	if err != nil {
		if !errors.Is(err, content.ErrNotFound) {
			slog.ErrorContext(ctx, "template person", "module", key, "error", err)
		}

		return nil
	}

	return &person
}
