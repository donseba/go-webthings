package testsuite

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

// TestPermissionMatrix checks that editors get exactly the rights of their
// groups, and that only administrators manage accounts and groups.
func TestPermissionMatrix(t *testing.T) {
	t.Parallel()

	admin := sitetest.Start(t, publishing.Module("news", "News"))

	group := admin.Submit("/admin/groups", url.Values{"Name": {"Writers"}})
	admin.Post(group, group, url.Values{
		"name":  {"Writers"},
		"allow": {"news:read", "news:create", "news:edit", "pages:read"},
	})

	account := admin.Submit("/admin/accounts", url.Values{
		"Email":    {"writer@example.test"},
		"Password": {"writer long passphrase"},
		"Role":     {"editor"},
	})
	groupID := group[strings.LastIndex(group, "/")+1:]
	admin.Post(account, account+"/groups", url.Values{"group": {groupID}})

	writer := admin.As("writer@example.test", "writer long passphrase")

	// Navigation shows only what the writer may read.
	writer.Expect("/admin/", http.StatusOK, `href="/admin/news"`, `href="/admin/pages"`)
	writer.ExpectMissing("/admin/", `href="/admin/media"`)
	writer.ExpectMissing("/admin/", `href="/admin/accounts"`)

	for _, path := range []string{"/admin/media", "/admin/site", "/admin/accounts", "/admin/groups", "/admin/pages/new"} {
		if status, _ := writer.Get(path); status != http.StatusForbidden {
			t.Fatalf("%s: want 403, got %d", path, status)
		}
	}

	// Writing is allowed, publishing is not.
	post := writer.Submit("/admin/news/new?locale=en", url.Values{
		"Title": {"Draft"},
		"Slug":  {"draft"},
		"Date":  {"2026-10-05"},
	})
	writer.ExpectMissing(post+"?locale=en", "/publish")
	writer.Expect("/admin/news-categories", http.StatusOK)

	_, editor := admin.Get(post + "?locale=en")
	if !strings.Contains(editor, "/publish") {
		t.Fatal("administrator has no publish button")
	}

	// Granting publish takes effect on the next request.
	admin.Post(group, group, url.Values{
		"name":  {"Writers"},
		"allow": {"news:read", "news:create", "news:edit", "news:publish", "pages:read"},
	})
	writer.Publish(post + "?locale=en")

	// The last administrator cannot become an editor.
	users, err := admin.App.Auth.Users(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	for _, user := range users {
		if user.Email != "admin@example.test" {
			continue
		}

		status, _ := admin.PostStatus(
			"/admin/accounts/"+user.ID,
			"/admin/accounts/"+user.ID+"/role",
			url.Values{"Role": {"editor"}},
		)
		if status != http.StatusConflict {
			t.Fatalf("last administrator demoted: %d", status)
		}
	}

	// Deleting the group takes its rights away on the next request; the
	// account stays.
	_, body := admin.PostStatus(group, group+"/delete", url.Values{})
	if !sitetest.HasToast(body, "success", "Group deleted.") || strings.Contains(body, "Writers") {
		t.Fatalf("delete group:\n%s", body)
	}

	if status, _ := writer.Get("/admin/news"); status != http.StatusForbidden {
		t.Fatalf("rights of a deleted group: %d", status)
	}

	admin.Expect(account, http.StatusOK, "writer@example.test")

	// A deleted group is gone, also when deleted again.
	admin.Post("/admin/groups", group+"/delete", url.Values{})

	if status, _ := admin.Get(group); status != http.StatusNotFound {
		t.Fatalf("deleted group: %d", status)
	}
}
