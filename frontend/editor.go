package frontend

import (
	"context"
	"net/http"
	"net/url"

	"github.com/donseba/go-router"
)

// Editors who are signed in to the admin see the public site like visitors,
// plus a link to edit what they look at: View.Editor, for example
//
//	{{with .Editor}}<a href="{{.EditURL}}">Edit</a>{{end}}
//
// The admin's session cookie reaches the public site; the frontend checks it
// only when it is present, so visitors cost nothing. Pages for editors are
// marked "Cache-Control: private, no-store", so no cache keeps or shares
// them; responses to visitors are unchanged.

// SignedIn is an editor, signed in to the admin, viewing the public site.
type SignedIn struct {
	UserID string
	Name   string
	// CanEdit reports whether the editor may change what an admin URL shows,
	// such as the editor of a page; CanView whether they may see it, which
	// allows previews (see PreviewParam).
	CanEdit func(adminURL string) bool
	CanView func(adminURL string) bool
	// AdminURL is the address of a named admin route, such as
	// "admin.pages.edit" with "id", as admin.Admin.TryURL builds it: links
	// into the admin never assume its path. It returns an error for an
	// unknown route or parameter.
	AdminURL router.PathFunc
}

// adminURL is the address of a named admin route for the signed-in editor
// viewing the site (SignedIn.AdminURL); "" for visitors and for a route the
// admin does not have, such as the section of a module without one. It is
// the template function adminURL, with arguments as the admin's route
// function takes them:
//
//	{{with adminURL "admin.dashboard"}}<a href="{{.}}">Admin</a>{{end}}
//	{{with adminURL "admin.news.edit" "id" .ID}}<a href="{{.}}">Edit</a>{{end}}
//
// Visitors get "": the admin's address (Config.AdminPath) stays off public
// pages, as it stays out of robots.txt, and pages for editors are never
// cached (see Editors), so a cached page never shows it either. The admin
// checks permissions itself, so a link an editor may not open answers 403.
func adminURL(ctx context.Context, name string, args ...any) string {
	editor, ok := Editor(ctx)
	if !ok || editor.AdminURL == nil {
		return ""
	}

	address, err := editor.AdminURL(name, args...)
	if err != nil {
		return ""
	}

	return address
}

// EditLink is View.Editor: the signed-in editor and the admin screen of the
// current page or module item.
type EditLink struct {
	Name    string
	EditURL string
}

type editorKey struct{}

// Editor returns the signed-in editor viewing the site; ok is false for
// visitors. Middleware can use it, for example to leave editors out of
// statistics.
func Editor(ctx context.Context) (SignedIn, bool) {
	editor, ok := ctx.Value(editorKey{}).(SignedIn)

	return editor, ok
}

// SetEditors sets how the frontend recognises a signed-in editor from a
// request; App connects it to the admin's sessions.
func (f *Frontend) SetEditors(identify func(r *http.Request) (SignedIn, bool)) {
	f.identify = identify
}

// Editors wraps the public site: it recognises signed-in editors, so Editor
// and View.Editor work below it, and keeps their pages out of caches.
func (f *Frontend) Editors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.identify == nil {
			next.ServeHTTP(w, r)

			return
		}

		editor, ok := f.identify(r)
		if !ok {
			next.ServeHTTP(w, r)

			return
		}

		w.Header().Set("Cache-Control", "private, no-store")
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), editorKey{}, editor)))
	})
}

// editLink is View.Editor for an admin screen: nil for visitors and for
// editors who may not edit there.
func editLink(ctx context.Context, adminURL, locale string) *EditLink {
	editor, ok := Editor(ctx)
	if !ok || adminURL == "" || editor.CanEdit == nil || !editor.CanEdit(adminURL) {
		return nil
	}

	return &EditLink{Name: editor.Name, EditURL: withLocale(adminURL, locale)}
}

// withLocale opens an admin screen in the language of the page.
func withLocale(adminURL, locale string) string {
	parsed, err := url.Parse(adminURL)
	if err != nil || locale == "" {
		return adminURL
	}

	query := parsed.Query()
	if !query.Has("locale") {
		query.Set("locale", locale)
		parsed.RawQuery = query.Encode()
	}

	return parsed.String()
}
