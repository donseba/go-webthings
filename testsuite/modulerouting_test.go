package testsuite

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/sitetest"
)

// routed is a module whose public handler and actions are go-routers, as
// frontend.Router and webthings.Env.Actions make them.
func routed() webthings.Module {
	return webthings.Module{
		Type:  "routed",
		Key:   "routed",
		Title: "Routed",
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			public := env.Frontend.Router()

			// Middleware comes before the routes; the site's 404 catch-all
			// does not count as one.
			public.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-Routed", "yes")
					next.ServeHTTP(w, r)
				})
			})

			public.Get("/{$}", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("list"))
			})

			// The module's own 404, such as a lookup of an unknown code.
			public.Get("/own-404", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("no such code"))
			})

			// A route by name: the router is served at the page, wherever
			// the page is, so For gives the item's address there.
			public.Get("/item/{slug}", func(w http.ResponseWriter, r *http.Request) {
				path, err := public.For(r).RoutePathArgs("item", "slug", r.PathValue("slug")+"-again")
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)

					return
				}

				_, _ = w.Write([]byte(path + " " + r.URL.Path))
			}).As("item")

			actions := env.Actions()

			actions.Use(frontend.Limits(env.Key, frontend.ActionLimits{Posts: 100}))
			actions.Post("/echo/{id}", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(r.URL.Path + " " + r.PathValue("id")))
			}).As("echo")

			// Actions have names: their addresses come from the router,
			// also on a page.
			public.Get("/links", func(w http.ResponseWriter, _ *http.Request) {
				echo, err := actions.RoutePathArgs("echo", "id", 7)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)

					return
				}

				_, _ = w.Write([]byte(echo + " " + actions.RoutePath("back")))
			})
			actions.Post("/back", func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					frontend.Back(w, r, env.Key, "invalid")

					return
				}

				frontend.Back(w, r, env.Key, "sent")
			}).As("back")

			// Every call gives the one router of the module.
			if env.Actions() != actions {
				return webthings.Parts{}, errors.New("a second action router")
			}

			return webthings.Parts{Public: public, ActionBodyLimit: 1 << 10}, nil
		},
	}
}

// TestModuleRouters checks the go-routers of a module's public handler and
// actions: paths without a route and wrong methods get the site's 404 page,
// a module's own 404 stays, routes have names below the page, and actions
// are mounted at /_/<key>/ behind the site's protections.
func TestModuleRouters(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t, routed())
	s.Mount("routed", "Routed", "routed")

	visitor := s.Visitor()

	page, body := visitor.Response("/en/routed")
	if page.StatusCode != http.StatusOK || body != "list" || page.Header.Get("X-Routed") != "yes" {
		t.Fatalf("module page: %d %q %v", page.StatusCode, body, page.Header)
	}

	// A 404 the module answers itself is not replaced by the site's.
	status, body := visitor.Get("/en/routed/own-404")
	if status != http.StatusNotFound || body != "no such code" {
		t.Fatalf("module's own 404: %d %q", status, body)
	}

	// Paths without a route get the site's 404 page.
	visitor.Expect("/en/routed/unknown/deeper", http.StatusNotFound, "<h1>not found</h1>")

	// So does a method the module has no route for, as before: not a 405.
	response := request(t, s.App, http.MethodPost, "/en/routed")
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "<h1>not found</h1>") {
		t.Fatalf("wrong method: %d\n%s", response.Code, response.Body.String())
	}

	// Handlers see the whole path.
	visitor.Expect("/en/routed/item/one", http.StatusOK, "/en/routed/item/one-again /en/routed/item/one")

	// The actions' addresses by route name.
	visitor.Expect("/en/routed/links", http.StatusOK, "/_/routed/echo/7 /_/routed/back")

	// Actions see the whole path and their wildcards.
	response = request(t, s.App, http.MethodPost, "/_/routed/echo/7")
	if response.Code != http.StatusOK || response.Body.String() != "/_/routed/echo/7 7" {
		t.Fatalf("action: %d %q", response.Code, response.Body.String())
	}

	// Without a route an action answers plainly, as a ServeMux does.
	if response := request(t, s.App, http.MethodGet, "/_/routed/echo/7"); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("action with the wrong method: %d", response.Code)
	}

	if response := request(t, s.App, http.MethodPost, "/_/routed/nothing"); response.Code != http.StatusNotFound {
		t.Fatalf("unknown action: %d", response.Code)
	}

	if status := visitor.CrossSite("/_/routed/echo/7", url.Values{}); status != http.StatusForbidden {
		t.Fatalf("cross-site action: %d", status)
	}
}

// TestOversizedPostReturnsToTheForm checks that a visitor post over the
// body limit, whose back field cannot be read, returns to the page it came
// from (the Referer) rather than the home page.
func TestOversizedPostReturnsToTheForm(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t, routed())
	s.Mount("routed", "Routed", "routed")

	visitor := s.Visitor()

	if path := visitor.Post("/en/routed", "/_/routed/back", url.Values{"back": {"/en/routed"}}); path != "/en/routed" {
		t.Fatalf("readable post: %q", path)
	}

	oversized := url.Values{"back": {"/en/routed"}, "text": {strings.Repeat("x", 4<<10)}}
	if path := visitor.Post("/en/routed", "/_/routed/back", oversized); path != "/en/routed" {
		t.Fatalf("oversized post: %q", path)
	}
}

// TestIdleRequestIDs checks that requests before Start get a request ID, so
// a panic then is logged with it.
func TestIdleRequestIDs(t *testing.T) {
	t.Parallel()

	idle := webthings.New(webthings.Config{StateDir: t.TempDir()})

	response := request(t, idle, http.MethodGet, "/en/", "X-Request-ID", "idle-1")
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("X-Request-ID") != "idle-1" {
		t.Fatalf("before start: %d %v", response.Code, response.Header())
	}
}
