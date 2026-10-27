package testsuite

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

// TestTrash deletes a published post and a page in the admin, checks that
// they leave the website, the search, the sitemap and the dashboard, that
// the admin's trash refuses an empty selection, and brings them back from
// it.
func TestTrash(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	page := `<main id="content"><h1>{{.Title}}</h1>{{.Body}}</main>`

	if err := os.WriteFile(filepath.Join(dir, "page.gohtml"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir}, publishing.Module("news", "News"))

	home, err := s.App.Pages.Home(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	s.Mount("news", "News", "news")

	post := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title": {"Harbour festival"},
		"Slug":  {"harbour-festival"},
		"Date":  {"2026-10-01"},
		"Body":  {"Boats and music."},
	})
	s.Publish(post + "?locale=en")

	hours := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":  {"Opening hours"},
		"Slug":   {"hours"},
		"Parent": {home},
	})
	s.Publish(hours + "?locale=en")

	visitor := s.Visitor()
	visitor.Expect("/en/search?q=harbour", http.StatusOK, "Harbour festival")
	visitor.Expect("/en/hours", http.StatusOK, "Opening hours")

	if !strings.Contains(s.Dashboard(), "Harbour festival") {
		t.Fatal("the dashboard lacks the post")
	}

	_, body := s.PostStatus(post+"?locale=en", post+"/delete?locale=en", url.Values{})
	if !strings.Contains(body, "Moved to the trash.") {
		t.Fatalf("delete notice missing\n%s", body)
	}

	s.Post(hours+"?locale=en", hours+"/delete?locale=en", url.Values{})

	// Deleted content is gone from every public and admin list.
	visitor.Expect("/en/news/harbour-festival", http.StatusNotFound)
	visitor.Expect("/en/hours", http.StatusNotFound)
	visitor.ExpectMissing("/en/search?q=harbour", "Harbour festival")
	visitor.ExpectMissing("/sitemap.xml", "harbour-festival")
	visitor.ExpectMissing("/sitemap.xml", "/en/hours")
	s.ExpectMissing("/admin/news", "Harbour festival")

	if strings.Contains(s.Dashboard(), "Harbour festival") {
		t.Fatal("the dashboard shows a post in the trash")
	}

	s.Expect(post+"?locale=en", http.StatusNotFound)

	// The trash lists both, grouped by section; one section can be shown
	// alone.
	s.Expect("/admin/trash", http.StatusOK, "Harbour festival", "Opening hours", `value="news:`, `value="pages:`)
	s.ExpectMissing("/admin/trash?section=news", "Opening hours")

	// Without a ticked item both buttons are refused, and the trash shows
	// as it was. Its script disables them until an item is ticked and adds
	// a box that ticks every item of a section.
	s.Expect("/admin/trash", http.StatusOK, "data-trash-all", `data-trash-action name="action" value="restore"`,
		`data-trash-action name="action" value="purge"`)

	for _, action := range []string{"restore", "purge"} {
		status, body := s.PostStatus("/admin/trash", "/admin/trash", url.Values{"action": {action}})
		if status != http.StatusUnprocessableEntity || !sitetest.HasToast(body, "error", "Select at least one item.") ||
			!strings.Contains(body, "Harbour festival") || strings.Contains(body, "0 items") {
			t.Fatalf("%s without a selection: %d\n%s", action, status, body)
		}
	}

	id := strings.TrimPrefix(post, "/admin/news/")
	_, body = s.PostStatus("/admin/trash", "/admin/trash", url.Values{"action": {"restore"}, "item": {"news:" + id}})

	if !strings.Contains(body, "1 item restored.") {
		t.Fatalf("restore notice missing\n%s", body)
	}

	// A restored post is published again, as it was.
	visitor.Expect("/en/search?q=harbour", http.StatusOK, "Harbour festival")

	pageID := strings.TrimPrefix(hours, "/admin/pages/")
	_, body = s.PostStatus("/admin/trash", "/admin/trash", url.Values{"action": {"purge"}, "item": {"pages:" + pageID}})

	if !strings.Contains(body, "1 item deleted for good.") || strings.Contains(body, "Opening hours") {
		t.Fatalf("purge\n%s", body)
	}
}

// TestMediaTrash deletes files from the media library, refuses files that
// content uses, and restores and purges files in the admin's trash.
func TestMediaTrash(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t, publishing.Module("news", "News"))
	ctx := t.Context()

	photo := s.Upload("photo.png", sitetest.PNG(t, 4, 4))
	used := s.Upload("used.png", sitetest.PNG(t, 5, 4))
	spare := s.Upload("spare.png", sitetest.PNG(t, 6, 4))

	s.Submit("/admin/news/new?locale=en", url.Values{
		"Title": {"With a picture"},
		"Slug":  {"with-a-picture"},
		"Date":  {"2026-10-01"},
		"Body":  {"![A picture](media:" + used.ID + ")"},
	})

	// A file that content uses stays.
	editor := "/admin/media/" + used.ID
	status, body := s.PostStatus(editor, editor+"/delete", url.Values{})

	if status != http.StatusConflict || !strings.Contains(body, "cannot be deleted") {
		t.Fatalf("delete of a used file: %d\n%s", status, body)
	}

	// Others move to the trash: they leave the library.
	editor = "/admin/media/" + photo.ID

	if path := s.Post(editor, editor+"/delete", url.Values{}); path != "/admin/media" {
		t.Fatalf("delete ended on %s", path)
	}

	s.ExpectMissing("/admin/media", "photo.png")
	s.Expect(editor, http.StatusNotFound)
	s.Expect("/admin/trash?section=media", http.StatusOK, "photo.png", `value="media:`+photo.ID+`"`)

	// Restored, a file is back in the library.
	_, body = s.PostStatus("/admin/trash", "/admin/trash", url.Values{"action": {"restore"}, "item": {"media:" + photo.ID}})
	if !strings.Contains(body, "1 item restored.") {
		t.Fatalf("restore notice missing\n%s", body)
	}

	s.Expect("/admin/media", http.StatusOK, "photo.png")

	// Purged, it is gone for good.
	s.Post(editor, editor+"/delete", url.Values{})

	_, body = s.PostStatus("/admin/trash", "/admin/trash", url.Values{"action": {"purge"}, "item": {"media:" + photo.ID}})
	if !strings.Contains(body, "1 item deleted for good.") {
		t.Fatalf("purge notice missing\n%s", body)
	}

	if _, err := s.App.Media.Get(ctx, photo.ID); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("purged file: %v", err)
	}

	// A file in the trash that content started to use meanwhile, as another
	// editor's save racing the delete can do, stays in the trash with the
	// reason.
	editor = "/admin/media/" + spare.ID
	s.Post(editor, editor+"/delete", url.Values{})

	_, err := s.App.DB.Exec(ctx, `INSERT INTO wt_media_usage(asset_id,owner,owner_id) VALUES(?,?,?)`, spare.ID, "race", "1")
	if err != nil {
		t.Fatal(err)
	}

	status, body = s.PostStatus("/admin/trash", "/admin/trash", url.Values{"action": {"purge"}, "item": {"media:" + spare.ID}})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "This file is still used by content in the trash.") {
		t.Fatalf("purge of a used file: %d\n%s", status, body)
	}

	s.Expect("/admin/trash?section=media", http.StatusOK, "spare.png")
}
