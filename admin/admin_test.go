package admin_test

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/db/dbtest"
)

type browser struct {
	t      *testing.T
	client *http.Client
	server *httptest.Server
}

func (b browser) get(path string) (int, string) {
	b.t.Helper()

	response, err := b.client.Get(b.server.URL + path)
	if err != nil {
		b.t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)

	return response.StatusCode, string(body)
}

// post submits a form as a browser does from a page of the admin.
func (b browser) post(path string, values url.Values) (int, string) {
	b.t.Helper()

	return b.postFrom(b.server.URL, "same-origin", path, values)
}

// postFrom submits a form from a page at origin; site is the Sec-Fetch-Site
// header browsers send, empty for a client that sends neither header.
func (b browser) postFrom(origin, site, path string, values url.Values) (int, string) {
	b.t.Helper()

	request, err := http.NewRequest(http.MethodPost, b.server.URL+path, strings.NewReader(values.Encode()))
	if err != nil {
		b.t.Fatal(err)
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if site != "" {
		request.Header.Set("Origin", origin)
		request.Header.Set("Sec-Fetch-Site", site)
	}

	response, err := b.client.Do(request)
	if err != nil {
		b.t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)

	return response.StatusCode, string(body)
}

func setup(t *testing.T, authorize func(context.Context, auth.User, admin.Permission) bool) browser {
	t.Helper()

	return start(t, admin.Options{Language: "nl", Authorize: authorize})
}

// start runs an admin with sections behind a test server; the owner account
// exists but is not signed in.
func start(t *testing.T, options admin.Options, sections ...admin.Section) browser {
	t.Helper()

	return startWith(t, newAccounts(t), options, sections...)
}

// startWith is start with the given accounts.
func startWith(t *testing.T, accounts *auth.Service, options admin.Options, sections ...admin.Section) browser {
	t.Helper()

	options.Title = "Test site"

	if options.CatalogueDir == "" {
		options.CatalogueDir = t.TempDir()
	}

	backoffice, err := admin.New(accounts, options, sections...)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/admin/", backoffice)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return browser{t: t, client: newClient(server), server: server}
}

// newClient is a browser without cookies for server. It uses the server's
// own transport: closing a test server also closes the idle connections of
// http.DefaultTransport, which breaks a request of a parallel test.
func newClient(server *httptest.Server) *http.Client {
	jar, _ := cookiejar.New(nil)

	return &http.Client{Jar: jar, Transport: server.Client().Transport}
}

// newAccounts is an auth service on SQLite with one administrator, the
// owner (see accountsIn).
func newAccounts(t *testing.T) *auth.Service {
	t.Helper()

	return accountsIn(t, dbtest.SQLite(t))
}

func TestSignInAndOut(t *testing.T) {
	t.Parallel()

	b := setup(t, nil)

	// Unauthenticated pages redirect to the sign-in form.
	status, body := b.get("/admin/")
	if status != http.StatusOK || !strings.Contains(body, "Inloggen") {
		t.Fatalf("expected Dutch sign-in page, got %d", status)
	}

	status, _ = b.post("/admin/login", url.Values{
		"Email":    {"owner@example.test"},
		"Password": {"wrong password!"},
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("wrong password: %d", status)
	}

	status, body = b.post("/admin/login", url.Values{
		"Email":    {"owner@example.test"},
		"Password": {"test-only long passphrase"},
	})
	if status != http.StatusOK || !strings.Contains(body, "Recent bewerkt") {
		t.Fatalf("sign in did not reach the dashboard: %d", status)
	}

	status, body = b.get("/admin/accounts")
	if status != http.StatusOK || !strings.Contains(body, "owner@example.test") {
		t.Fatalf("accounts: %d", status)
	}

	status, _ = b.post("/admin/accounts", url.Values{
		"Email":    {"editor@example.test"},
		"Password": {"another long passphrase"},
	})
	if status != http.StatusOK {
		t.Fatalf("create account: %d", status)
	}

	if _, body := b.get("/admin/accounts"); !strings.Contains(body, "editor@example.test") {
		t.Fatal("new account not listed")
	}

	status, body = b.post("/admin/logout", url.Values{})
	if status != http.StatusOK || !strings.Contains(body, "Inloggen") {
		t.Fatalf("sign out: %d", status)
	}
}

func TestPermissionHook(t *testing.T) {
	t.Parallel()

	b := setup(t, func(_ context.Context, _ auth.User, permission admin.Permission) bool {
		return permission.Section != "accounts" || permission.Action == admin.View
	})

	b.post("/admin/login", url.Values{
		"Email":    {"owner@example.test"},
		"Password": {"test-only long passphrase"},
	})

	if status, _ := b.get("/admin/accounts"); status != http.StatusOK {
		t.Fatalf("view denied: %d", status)
	}

	status, _ := b.post("/admin/accounts", url.Values{
		"Email":    {"editor@example.test"},
		"Password": {"another long passphrase"},
	})
	if status != http.StatusForbidden {
		t.Fatalf("edit allowed: %d", status)
	}
}

// hxGet fetches path the way an HTMX navigation of the content area does.
func (b browser) hxGet(path string) string {
	b.t.Helper()

	request, err := http.NewRequest(http.MethodGet, b.server.URL+path, nil)
	if err != nil {
		b.t.Fatal(err)
	}

	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Target", "main#content")

	response, err := b.client.Do(request)
	if err != nil {
		b.t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)

	return string(body)
}
