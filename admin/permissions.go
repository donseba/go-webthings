package admin

import (
	"context"
	"net/http"

	"github.com/donseba/go-webthings/auth"
)

type permissionsKey struct{}

// withPermissions returns ctx with user signed in and, when the built-in
// rules decide (no Options.Authorize) and the user is no administrator, the
// rights of the user's groups, which Can reads.
func (a *Admin) withPermissions(ctx context.Context, user auth.User) (context.Context, error) {
	ctx = auth.WithUser(ctx, user)

	if user.IsAdmin() || a.options.Authorize != nil {
		return ctx, nil
	}

	permissions, err := a.auth.Permissions(ctx, user)

	return context.WithValue(ctx, permissionsKey{}, permissions), err
}

// may reports whether the signed-in user may perform action in section:
// AdminOnly sections need the administrator role as well, and every section
// the right of its permission ID (see Can).
func (a *Admin) may(r *http.Request, section Section, action string) bool {
	user, _ := auth.CurrentUser(r.Context())
	if section.AdminOnly && !user.IsAdmin() {
		return false
	}

	return a.Can(r, Permission{Section: section.PermissionID(), Action: action})
}

// allow answers 403 to users who may not perform action in section.
func (a *Admin) allow(section Section, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !a.may(r, section, action) {
				a.Forbidden(w, r)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// AdministratorsOnly is route middleware that answers 403 to every user who
// is not an administrator, after the route's own permission check, for
// changes only administrators may make, such as the site's security
// settings or the permanent mark of an item:
//
//	routes.Post("/security", admin.Edit, s.saveSecurity).As("security").Use(admin.AdministratorsOnly)
//
// Pages that show such settings to other users show them read-only
// themselves.
func AdministratorsOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, _ := auth.CurrentUser(r.Context()); user.IsAdmin() {
			next.ServeHTTP(w, r)

			return
		}

		if a, ok := r.Context().Value(messagesKey{}).(*Admin); ok {
			a.Forbidden(w, r)

			return
		}

		http.Error(w, "forbidden", http.StatusForbidden)
	})
}
