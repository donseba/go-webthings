package newsletter_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/modules/newsletter"
	"github.com/donseba/go-webthings/sitetest"
)

// TestExportNeedsRight checks that exporting subscribers, personal data,
// needs the Export right: without it the button is gone and the export
// refused.
func TestExportNeedsRight(t *testing.T) {
	s := sitetest.Start(t, newsletter.Module("news", "Newsletter"))
	ctx := context.Background()

	user, err := s.App.Auth.CreateUser(ctx, "editor@example.test", "", "editor long passphrase", auth.Editor)
	if err != nil {
		t.Fatal(err)
	}

	group, err := s.App.Auth.SaveGroup(ctx, auth.Group{
		Name:        "Writers",
		Permissions: auth.Permissions{"news": {auth.Read, auth.Create, auth.Edit, auth.Publish, auth.Delete}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.App.Auth.SetUserGroups(ctx, user.ID, []string{group.ID}); err != nil {
		t.Fatal(err)
	}

	editor := s.As("editor@example.test", "editor long passphrase")

	if _, body := editor.Get("/admin/news"); strings.Contains(body, "/admin/news/export") {
		t.Fatal("the export is offered without the right")
	}

	if status, _ := editor.Get("/admin/news/export"); status != http.StatusForbidden {
		t.Fatalf("export without the right: %d", status)
	}

	group.Permissions["news"] = append(group.Permissions["news"], auth.Export)
	if _, err := s.App.Auth.SaveGroup(ctx, group); err != nil {
		t.Fatal(err)
	}

	if _, body := editor.Get("/admin/news"); !strings.Contains(body, "/admin/news/export") {
		t.Fatal("the export is not offered with the right")
	}

	if status, _ := editor.Get("/admin/news/export"); status != http.StatusOK {
		t.Fatalf("export with the right: %d", status)
	}
}
