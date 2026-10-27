package testimonials

import (
	"context"
	"html/template"
	"log/slog"

	"github.com/donseba/go-webthings/frontend"
)

// funcs are the template functions Testimonials offers to every template.
//
// go-clue:funcmap
func funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string, limit int) []github.com/donseba/go-webthings/modules/testimonials.Testimonial
		"testimonials": list,
	}
}

// list returns up to limit published testimonials in their order, for
// example {{range testimonials "reviews" 3}} on the home page. A limit of 0
// or less returns all of them.
func list(ctx context.Context, key string, limit int) []Testimonial {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	all, err := service.List(ctx, frontend.Locale(ctx))
	if err != nil {
		slog.ErrorContext(ctx, "template testimonials", "module", key, "error", err)
	}

	return limited(all, limit)
}
