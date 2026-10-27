package editor

import (
	"context"
	"slices"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/content"
)

// recent lists the latest changed items for the dashboard, named and linked
// in the site's default language.
func (e Editor[F, T, I]) recent(ctx context.Context, limit int) ([]admin.RecentItem, error) {
	config, err := e.config.Locales.Configuration(ctx)
	if err != nil {
		return nil, err
	}

	// The store reads only the newest items, however many there are.
	items, err := e.config.Store.Recent(ctx, limit)
	if err != nil {
		return nil, err
	}

	locale := config.Default

	slices.SortStableFunc(items, func(x, y content.Item[F, T]) int {
		return changed(y).Compare(changed(x))
	})

	recent := make([]admin.RecentItem, 0, len(items))

	for _, item := range items {
		recent = append(recent, admin.RecentItem{
			Title:     e.dashboardLabel(item, locale),
			URL:       e.itemURL(ctx, item.ID, locale),
			UpdatedAt: changed(item),
			Status:    Status(item, e.config.Store.Revisioned()),
			Author:    item.Revision.Author,
		})
	}

	return recent, nil
}

// planned lists at most limit items with a planned publication change, the
// earliest first. Only stores that can plan publication (content.Planner)
// have any; they read just those items.
func (e Editor[F, T, I]) planned(ctx context.Context, limit int) ([]admin.ScheduledItem, error) {
	planner, ok := e.config.Store.(content.Planner[F, T])
	if !ok || limit <= 0 {
		return nil, nil
	}

	config, err := e.config.Locales.Configuration(ctx)
	if err != nil {
		return nil, err
	}

	items, err := planner.Planned(ctx)
	if err != nil {
		return nil, err
	}

	var planned []admin.ScheduledItem

	for _, item := range items {
		if len(planned) == limit {
			break
		}

		if item.Schedule.IsZero() {
			continue
		}

		planned = append(planned, admin.ScheduledItem{
			Title:       e.dashboardLabel(item, config.Default),
			URL:         e.itemURL(ctx, item.ID, config.Default),
			PublishAt:   item.Schedule.PublishAt,
			UnpublishAt: item.Schedule.UnpublishAt,
		})
	}

	return planned, nil
}

// dashboardLabel names an item in locale, or else in the first language it
// has a name in; empty when it has none.
func (e Editor[F, T, I]) dashboardLabel(item content.Item[F, T], locale string) string {
	if label := e.label(item, locale); label != "" {
		return label
	}

	for _, translation := range item.Translations {
		if label := e.label(item, translation.Locale); label != "" {
			return label
		}
	}

	return ""
}

// changed is when the item last changed: a new revision, a publication or
// an edit.
func changed[F, T any](item content.Item[F, T]) time.Time {
	if item.Revision.CreatedAt.After(item.UpdatedAt) {
		return item.Revision.CreatedAt
	}

	return item.UpdatedAt
}
