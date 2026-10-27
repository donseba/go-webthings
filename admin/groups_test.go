package admin_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
)

var (
	matrixGroup   = regexp.MustCompile(`data-matrix-group-toggle hidden aria-label="[^"]*">\s*<span>([^<]+)</span>`)
	matrixSection = regexp.MustCompile(`data-matrix-row-toggle hidden aria-label="[^"]*">\s*<span>([^<]+)</span>`)
)

// TestRightsMatrix checks the matrix follows the navigation's groups and
// that every saved right brings read along, as the page's select boxes do.
func TestRightsMatrix(t *testing.T) {
	t.Parallel()

	accounts := newAccounts(t)
	b := startWith(t, accounts, admin.Options{Language: "en"},
		plainSection("news", "News", ""),
		plainSection("visits", "Visits", "Visitors"),
		plainSection("agenda", "Agenda", "Calendar"),
		plainSection("blog", "Blog", "Calendar"),
	)
	b.signIn()

	ctx := context.Background()

	group, err := accounts.SaveGroup(ctx, auth.Group{Name: "Editors"})
	if err != nil {
		t.Fatal(err)
	}

	status, body := b.get("/admin/groups/" + group.ID)
	if status != http.StatusOK {
		t.Fatalf("matrix: %d", status)
	}

	var groups, sections []string

	for _, match := range matrixGroup.FindAllStringSubmatch(body, -1) {
		groups = append(groups, match[1])
	}

	for _, match := range matrixSection.FindAllStringSubmatch(body, -1) {
		sections = append(sections, match[1])
	}

	if !slices.Equal(groups, []string{"Calendar", "Visitors"}) {
		t.Fatalf("matrix groups: %v", groups)
	}

	if !slices.Equal(sections, []string{"News", "Agenda", "Blog", "Visits"}) {
		t.Fatalf("matrix sections: %v", sections)
	}

	if !strings.Contains(body, `data-matrix-all hidden aria-label="Select all"`) ||
		!strings.Contains(body, `aria-label="Edit for all sections"`) {
		t.Fatal("select-all boxes missing")
	}

	status, _ = b.post("/admin/groups/"+group.ID, url.Values{
		"name":  {"Editors"},
		"allow": {"news:edit", "news:publish", "agenda:read"},
	})
	if status != http.StatusOK {
		t.Fatalf("save: %d", status)
	}

	saved, err := accounts.Group(ctx, group.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{auth.Read, auth.Edit, auth.Publish} {
		if !saved.Permissions.Allows("news", want) {
			t.Fatalf("news misses %s: %v", want, saved.Permissions)
		}
	}

	if saved.Permissions.Allows("news", auth.Delete) || !saved.Permissions.Allows("agenda", auth.Read) ||
		len(saved.Permissions["agenda"]) != 1 || len(saved.Permissions["blog"]) != 0 {
		t.Fatalf("saved rights: %v", saved.Permissions)
	}
}
