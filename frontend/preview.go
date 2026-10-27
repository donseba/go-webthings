package frontend

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/pages"
)

// Editors preview a revision on the website itself: its public address with
// ?revision=<id> (PreviewParam), for example "/en/news/hello?revision=…",
// shows that revision instead of the published one, also for content that
// was never published. Only signed-in editors who may read the content's
// admin section see it; for visitors and everyone else the parameter does
// nothing, so a preview never reveals that a revision exists. A revision of
// other content than the address shows is ignored the same way.
//
// A preview is marked as such: View.Preview and View.Revision are set, the
// page carries "X-Robots-Tag: noindex, nofollow", and, as every page for signed-in
// editors, "Cache-Control: private, no-store" (see Editors).

// PreviewParam is the query parameter that names the revision to preview.
const PreviewParam = "revision"

// The routes of the admin section of pages: its overview, whose rights
// decide who may preview pages, and the editor of a page.
const (
	pagesAdmin  = "admin.pages.index"
	pagesEditor = "admin.pages.edit"
)

// RevisionPreview is View.Revision: the revision an editor previews.
type RevisionPreview struct {
	// Number is the revision's number in the item's history.
	Number int
	// Published is set when the previewed revision is the published one;
	// usually it is not.
	Published bool
	// PublishedURL is the same address without the preview.
	PublishedURL string
}

// PreviewRevision returns the revision a request asks to preview (see
// PreviewParam) when it comes from a signed-in editor who may read the
// admin section of adminURL, such as the section's index (see
// webthings.Env.AdminURL); "" otherwise. Module
// handlers look the revision up for the content at the address (see
// content.Revisions.PreviewBySlug) and pass what they show to RevisionOf.
func PreviewRevision(r *http.Request, adminURL string) string {
	revision := strings.TrimSpace(r.URL.Query().Get(PreviewParam))
	if revision == "" || len(revision) > 64 {
		return ""
	}

	editor, ok := Editor(r.Context())
	if !ok || editor.CanView == nil || !editor.CanView(adminURL) {
		return ""
	}

	return revision
}

// RevisionOf is Content.Revision for an item a module shows: set when item
// holds the revision the request previews (see PreviewRevision), nil
// otherwise.
func RevisionOf[F, T any](r *http.Request, item content.Item[F, T], previewID string) *RevisionPreview {
	if previewID == "" || item.Revision.ID != previewID {
		return nil
	}

	return &RevisionPreview{
		Number:       item.Revision.Number,
		Published:    item.IsPublishedRevision(),
		PublishedURL: withoutPreview(r.URL),
	}
}

// withoutPreview is the address of a request without PreviewParam.
func withoutPreview(address *url.URL) string {
	query := address.Query()
	query.Del(PreviewParam)

	out := address.Path
	if encoded := query.Encode(); encoded != "" {
		out += "?" + encoded
	}

	return out
}

// previewing marks a view as a preview of revision.
func previewing(w http.ResponseWriter, view *View, revision *RevisionPreview) {
	view.Preview = true
	view.Revision = revision

	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
}

// pagePreview returns the page revision a request previews at path, when
// the editor may read pages and the revision is of the page at path (see
// pages.Service.Preview).
func (f *Frontend) pagePreview(r *http.Request, locale, path string) (pages.Page, bool) {
	revision := PreviewRevision(r, adminURL(r.Context(), pagesAdmin))
	if revision == "" {
		return pages.Page{}, false
	}

	page, err := f.pages.Preview(r.Context(), locale, path, revision)

	return page, err == nil
}

// previewPage renders a page revision at path.
func (f *Frontend) previewPage(w http.ResponseWriter, r *http.Request, page pages.Page, locale, path string) {
	view, err := f.view(r, locale, page.ID, nil)
	if err != nil {
		f.Fail(w, r, err)

		return
	}

	text, _ := page.Text(locale)
	view.Title = text.Title
	view.Description = text.Description
	view.Page = f.pageView(r.Context(), page, locale, path, page.Fields.ParentID == "")
	view.Content = *view.Page
	view.Editor = editLink(r.Context(), adminURL(r.Context(), pagesEditor, "id", page.ID), locale)

	previewing(w, &view, RevisionOf(r, page, page.Revision.ID))
	f.write(w, r, "page", view, http.StatusOK)
}

// previewMount is the module page an editor previews: the page as the
// revision has it, at path, in place of the published one.
func previewMount(page pages.Page, locale, path string) pages.Resolution {
	text, _ := page.Text(locale)

	depth := 1
	if page.Fields.ParentID == "" {
		depth = 0
	}

	return pages.Resolution{
		Page: page,
		Route: pages.Route{
			Locale:     locale,
			Path:       path,
			PageID:     page.ID,
			RevisionID: page.Revision.ID,
			ParentID:   page.Fields.ParentID,
			Module:     page.Fields.Module,
			Depth:      depth,
			Title:      text.Title,
		},
		Rest: "/",
	}
}

type mountPreviewKey struct{}

// withMountPreview remembers that the module page of a request is a
// preview, so Render marks the module's screen as one.
func withMountPreview(ctx context.Context, revision *RevisionPreview) context.Context {
	return context.WithValue(ctx, mountPreviewKey{}, revision)
}

func mountPreview(ctx context.Context) *RevisionPreview {
	revision, _ := ctx.Value(mountPreviewKey{}).(*RevisionPreview)

	return revision
}

// PagePreviewURL is the address at which an editor previews a page
// revision in locale (see PreviewParam); "" when it has none, because its
// parent page is not published.
func (f *Frontend) PagePreviewURL(ctx context.Context, page pages.Page, locale string) string {
	path, err := f.pages.PreviewPath(ctx, page, locale)
	if err != nil {
		return ""
	}

	return previewURL(link(locale, path, ""), page.Revision.ID)
}

// ModulePreviewURL is the address at which an editor previews a revision
// of module content, at relative below the page of the module instance
// key, for example "/hello"; "" while the instance is on no page in locale.
func (f *Frontend) ModulePreviewURL(ctx context.Context, key, locale, relative, revisionID string) string {
	mount, err := f.pages.MountPath(ctx, key, locale)
	if err != nil {
		return ""
	}

	return previewURL(link(locale, mount, relative), revisionID)
}

func previewURL(address, revisionID string) string {
	return address + "?" + url.Values{PreviewParam: {revisionID}}.Encode()
}
