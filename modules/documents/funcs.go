package documents

import (
	"context"
	"html/template"
	"log/slog"

	"github.com/donseba/go-webthings/frontend"
)

// funcs are the template functions Documents offers to every template.
//
// go-clue:funcmap
func funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string, limit int) []github.com/donseba/go-webthings/modules/documents.Document
		"documents": latest,
		//go-clue:sig func(key string, category string) []github.com/donseba/go-webthings/modules/documents.Document
		"documentsIn": inCategory,
	}
}

// latest returns up to limit of the newest published documents of an
// instance, for example {{range documents "documents" 5}} on the home page. A
// limit of 0 or less returns all of them.
func latest(ctx context.Context, key string, limit int) []Document {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	list, err := service.Latest(ctx, frontend.Locale(ctx), limit)
	if err != nil {
		slog.ErrorContext(ctx, "template documents", "module", key, "error", err)
	}

	return list
}

// inCategory returns the published documents of one category, newest first,
// for example {{range documentsIn "documents" "Minutes"}}.
func inCategory(ctx context.Context, key, category string) []Document {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return nil
	}

	list, err := service.InCategory(ctx, frontend.Locale(ctx), category)
	if err != nil {
		slog.ErrorContext(ctx, "template documentsIn", "module", key, "error", err)
	}

	return list
}
