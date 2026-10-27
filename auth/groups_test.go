package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
)

func TestGroupsAndPermissions(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		accounts := service(t, database)

		editor, err := accounts.CreateUser(ctx, "editor@example.test", "", "editor long passphrase", auth.Editor)
		if err != nil {
			t.Fatal(err)
		}

		writers, err := accounts.SaveGroup(ctx, auth.Group{
			Name:        "Writers",
			Permissions: auth.Permissions{"news": {auth.Read, auth.Create, "unknown"}},
		})
		if err != nil {
			t.Fatal(err)
		}

		publishers, err := accounts.SaveGroup(ctx, auth.Group{
			Name:        "Publishers",
			Permissions: auth.Permissions{"news": {auth.Publish}, "pages": {auth.Read}},
		})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := accounts.SaveGroup(ctx, auth.Group{Name: "Writers"}); !errors.Is(err, auth.ErrDuplicate) {
			t.Fatalf("duplicate name: %v", err)
		}

		if err := accounts.SetUserGroups(ctx, editor.ID, []string{writers.ID, publishers.ID}); err != nil {
			t.Fatal(err)
		}

		permissions, err := accounts.Permissions(ctx, editor)
		if err != nil {
			t.Fatal(err)
		}

		// Rights add up over groups; unknown actions are not stored.
		for _, allowed := range [][2]string{{"news", auth.Read}, {"news", auth.Publish}, {"pages", auth.Read}} {
			if !permissions.Allows(allowed[0], allowed[1]) {
				t.Fatalf("missing %v in %v", allowed, permissions)
			}
		}

		if permissions.Allows("news", auth.Delete) || permissions.Allows("news", "unknown") {
			t.Fatalf("too many rights: %v", permissions)
		}

		if err := accounts.DeleteGroup(ctx, publishers.ID); err != nil {
			t.Fatal(err)
		}

		permissions, _ = accounts.Permissions(ctx, editor)
		if permissions.Allows("news", auth.Publish) {
			t.Fatal("rights of a deleted group remain")
		}

		groups, err := accounts.UserGroups(ctx, editor.ID)
		if err != nil || len(groups) != 1 || groups[0] != writers.ID {
			t.Fatalf("groups: %v %v", groups, err)
		}
	})
}
