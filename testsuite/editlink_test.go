package testsuite

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

// probe is a module whose middleware marks requests of signed-in editors,
// as a statistics module would to leave them out.
func probe() webthings.Module {
	return webthings.Module{
		Type: "probe",
		Key:  "probe",
		Setup: func(webthings.Env) (webthings.Parts, error) {
			return webthings.Parts{
				Middleware: func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if _, ok := frontend.Editor(r.Context()); ok {
							w.Header().Set("X-Editor", "yes")
						}

						next.ServeHTTP(w, r)
					})
				},
			}, nil
		},
	}
}

// editLink returns the edit link on a public page, or "".
func editLink(t *testing.T, s *sitetest.Site, path string) (string, *http.Response) {
	t.Helper()

	response, body := s.Response(path)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d", path, response.StatusCode)
	}

	_, link, found := strings.Cut(body, `<a id="edit" href="`)
	if !found {
		return "", response
	}

	link, _, _ = strings.Cut(link, `"`)

	return strings.ReplaceAll(link, "&amp;", "&"), response
}

func TestEditLink(t *testing.T) {
	t.Parallel()

	admin := sitetest.StartWith(t, sitetest.Options{StartingPoints: []string{"news"}}, publishing.Module("news", "News"), probe())
	admin.Mount("news", "News", "news")

	home, err := admin.App.Pages.Home(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	about := admin.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About"}, "Slug": {"about"}, "Parent": {home}})
	admin.Publish(about + "?locale=en")

	post := admin.Submit("/admin/news/new?locale=en", url.Values{"Title": {"Hello"}, "Slug": {"hello"}, "Date": {"2026-10-01"}})
	admin.Publish(post + "?locale=en")

	// Visitors get no link, and their responses are not marked private: they
	// get the site's caching of public pages.
	visitor := admin.Visitor()

	for _, path := range []string{"/en/about", "/en/news", "/en/news/hello"} {
		link, response := editLink(t, visitor, path)
		if link != "" || response.Header.Get("Cache-Control") != "no-cache" || response.Header.Get("X-Editor") != "" {
			t.Fatalf("visitor on %s: link %q, headers %v", path, link, response.Header)
		}
	}

	// Administrators may edit everything: the page, the module and its posts.
	for path, want := range map[string]string{
		"/en/about":      about + "?locale=en",
		"/nl/":           "/admin/pages/" + home + "?locale=nl",
		"/en/news":       "/admin/news?locale=en",
		"/en/news/hello": post + "?locale=en",
	} {
		link, response := editLink(t, admin, path)
		if link != want {
			t.Fatalf("admin on %s: link %q, want %q", path, link, want)
		}

		if response.Header.Get("Cache-Control") != "private, no-store" || response.Header.Get("X-Editor") != "yes" {
			t.Fatalf("admin on %s: headers %v", path, response.Header)
		}
	}

	// An editor who may edit news, but not pages, sees the link on news only.
	group := admin.Submit("/admin/groups", url.Values{"Name": {"Writers"}})
	admin.Post(group, group, url.Values{"name": {"Writers"}, "allow": {"news:read", "news:edit", "pages:read"}})

	for _, email := range []string{"writer@example.test", "reader@example.test"} {
		admin.Submit("/admin/accounts", url.Values{
			"Email":    {email},
			"Password": {"long test passphrase"},
			"Role":     {"editor"},
		})
	}

	users, err := admin.App.Auth.Users(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, user := range users {
		if user.Email == "writer@example.test" {
			account := "/admin/accounts/" + user.ID
			admin.Post(account, account+"/groups", url.Values{"group": {group[strings.LastIndex(group, "/")+1:]}})
		}
	}

	writer := admin.As("writer@example.test", "long test passphrase")

	if link, _ := editLink(t, writer, "/en/news/hello"); link != post+"?locale=en" {
		t.Fatalf("writer on a post: %q", link)
	}

	link, response := editLink(t, writer, "/en/about")
	if link != "" || response.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("writer on a page: %q %v", link, response.Header)
	}

	// An editor without rights sees no links at all.
	reader := admin.As("reader@example.test", "long test passphrase")

	for _, path := range []string{"/en/about", "/en/news", "/en/news/hello"} {
		if link, _ := editLink(t, reader, path); link != "" {
			t.Fatalf("reader on %s: %q", path, link)
		}
	}

	// Signing out ends it.
	admin.Post("/admin/", "/admin/logout", url.Values{})

	if link, response := editLink(t, admin, "/en/about"); link != "" || response.Header.Get("X-Editor") != "" {
		t.Fatalf("after sign out: %q %v", link, response.Header)
	}
}
