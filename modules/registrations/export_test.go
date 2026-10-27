package registrations_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/modules/registrations"
	"github.com/donseba/go-webthings/sitetest"
)

// TestExportNeedsRight checks that the CSV export of registrations,
// personal data, needs the Export right.
func TestExportNeedsRight(t *testing.T) {
	s := sitetest.Start(t, registrations.Module("tickets", "Tickets"))
	ctx := context.Background()

	user, err := s.App.Auth.CreateUser(ctx, "editor@example.test", "", "editor long passphrase", auth.Editor)
	if err != nil {
		t.Fatal(err)
	}

	group, err := s.App.Auth.SaveGroup(ctx, auth.Group{
		Name:        "Writers",
		Permissions: auth.Permissions{"tickets": {auth.Read, auth.Edit}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.App.Auth.SetUserGroups(ctx, user.ID, []string{group.ID}); err != nil {
		t.Fatal(err)
	}

	editor := s.As("editor@example.test", "editor long passphrase")

	// The right is checked before the subject is looked up.
	if status, _ := editor.Get("/admin/tickets/any/export.csv"); status != http.StatusForbidden {
		t.Fatalf("export without the right: %d", status)
	}

	group.Permissions["tickets"] = append(group.Permissions["tickets"], auth.Export)
	if _, err := s.App.Auth.SaveGroup(ctx, group); err != nil {
		t.Fatal(err)
	}

	if status, _ := editor.Get("/admin/tickets/any/export.csv"); status == http.StatusForbidden {
		t.Fatal("export refused with the right")
	}
}
