package publishing

import (
	"net/http"
	"slices"

	"github.com/donseba/go-webthings/feed"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/markdown"
)

// summaryLength is about how many characters of a post's text summarise a
// post without an excerpt in its feed.
const summaryLength = 300

// Option changes how an instance works.
type Option func(*options)

type options struct {
	feedSize int
}

// WithFeedSize makes the instance's feeds list the size newest posts, the
// main feed and those of the categories alike, rather than as many as the
// site setting says (site.Settings.FeedLimit); at most site.MaxFeedSize.
func WithFeedSize(size int) Option {
	return func(o *options) {
		o.feedSize = size
	}
}

// feeds are the instance's feeds below its page: its newest posts at
// feed.atom and feed.rss, and those of a category below the category's
// list, linked from that list, at category/<slug>/feed.atom and .rss. size
// is the instance's feed size, zero for the site's (see WithFeedSize).
func (s *Service) feeds(size int) []frontend.Feed {
	return []frontend.Feed{
		{Limit: size, Build: s.feed},
		{Name: "category-feed", Path: "/category/{slug}/feed", Page: "category", Limit: size, Build: s.feed},
	}
}

// feed is the newest published posts in the page's language, of the
// category the request names, if any: their excerpt, or the start of their
// text, as summary, their text as content, with absolute addresses and
// images as e-mail shows them, their categories and featured image. A post
// is published at its date and updated when its published revision was
// saved; its ID is a tag: URI of the instance and the post's ID, so it
// stays when the post's slug or page changes.
func (s *Service) feed(r *http.Request, limit int) (feed.Feed, error) {
	ctx := r.Context()
	locale := frontend.MountFrom(ctx).Locale
	here := s.public.For(r).RoutePathArgs

	var (
		built    feed.Feed
		category string
	)

	if slug := r.PathValue("slug"); slug != "" {
		// A category that does not exist is a 404.
		item, err := s.categories.PublishedBySlug(ctx, s.key, locale, slug)
		if err != nil {
			return feed.Feed{}, err
		}

		text, _ := item.Text(locale)
		category = item.ID
		built.Title = text.Title
		built.Link, _ = here("category", "slug", slug)
	}

	items, err := s.posts.ListPublished(ctx, s.key, locale)
	if err != nil {
		return feed.Feed{}, err
	}

	slices.SortStableFunc(items, newestFirst)

	categories, err := s.categoryMap(ctx, locale, here)
	if err != nil {
		return feed.Feed{}, err
	}

	for _, item := range items {
		if len(built.Entries) == limit {
			break
		}

		if category != "" && !item.Fields.Categories.Has(category) {
			continue
		}

		built.Entries = append(built.Entries, s.entry(r, item, locale, categories))
	}

	return built, nil
}

// entry is a published post as its feed lists it.
func (s *Service) entry(r *http.Request, item PostItem, locale string, categories map[string]Category) feed.Entry {
	ctx := r.Context()
	text, _ := item.Text(locale)

	// Published posts have a slug.
	link, _ := s.public.For(r).RoutePathArgs("post", "slug", text.Slug)

	summary := text.Excerpt
	if summary == "" {
		summary = frontend.Summary(frontend.ParseQuery(""), summaryLength, markdown.Text(text.Body))
	}

	entry := feed.Entry{
		ID:        s.frontend.TagURI(r, item.CreatedAt, s.key+"/"+item.ID),
		Title:     text.Title,
		Link:      link,
		Summary:   summary,
		Content:   string(s.frontend.AbsoluteMarkdown(r, text.Body)),
		Published: item.Fields.Date,
		Updated:   item.Fields.Date,
		Image:     feed.Enclosure{URL: s.frontend.MailImage(ctx, item.Fields.Image).URL},
	}

	// A revision published later than the post's date updates it.
	if item.Revision.CreatedAt.After(entry.Updated) {
		entry.Updated = item.Revision.CreatedAt
	}

	for _, id := range item.Fields.Categories {
		if found, ok := categories[id]; ok {
			entry.Categories = append(entry.Categories, found.Title)
		}
	}

	return entry
}

// newestFirst orders posts by their date, the newest first.
func newestFirst(a, b PostItem) int {
	return b.Fields.Date.Compare(a.Fields.Date)
}
