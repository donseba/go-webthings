package admin_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-partial/connector"
	"github.com/donseba/go-partial/exp/flash"
)

func (b browser) signIn() {
	b.t.Helper()

	b.signInAs("owner@example.test", "test-only long passphrase")
}

func (b browser) signInAs(email, password string) {
	b.t.Helper()

	status, _ := b.post("/admin/login", url.Values{
		"Email":    {email},
		"Password": {password},
	})
	if status != http.StatusOK {
		b.t.Fatalf("sign in: %d", status)
	}
}

// ToolsView is the data of the test section's page.
type ToolsView struct {
	Reset     form.RenderModel
	Remove    form.RenderModel
	Plain     *form.RenderModel
	Group     form.RenderModel
	Unpublish form.RenderModel
}

type addressInput struct {
	Name    string `form:"input,text" label:"Name"`
	Address struct {
		Street string `form:"input,text" label:"Street"`
		City   string `form:"input,text" label:"City"`
	} `                               legend:"Address"`
}

type emailInput struct {
	Email string `form:"input,email" label:"E-mail"`
}

var toolsFiles = fstest.MapFS{
	"templates/tools.gohtml": {Data: []byte(`<main id="content">
<div id="reset">{{form_render_localized localizer .Reset nil}}</div>
<div id="remove">{{form_render_localized localizer .Remove nil}}</div>
<div id="plain">{{form_render_localized localizer (confirm localizer .Plain "Send it?") nil}}</div>
<div id="group">{{form_render_localized localizer .Group nil}}</div>
<div id="unpublish">{{form_render_localized localizer .Unpublish nil}}</div>
</main>`)},
	"assets/tools.css": {Data: []byte(".tools-chart { color: var(--accent); }")},
	// broken fails as it renders: struct{} has no field Missing.
	"templates/broken.gohtml": {Data: []byte(`<main id="content">{{.Missing}}</main>`)},
}

// toolsSection exercises the shell: confirmations, notices, translations and
// a badge.
func toolsSection(badge *atomic.Int32) admin.Section {
	return admin.Section{
		ID:         "tools",
		Title:      "Tools",
		Path:       "/tools",
		Files:      toolsFiles,
		Stylesheet: "assets/tools.css",
		Badge: func(context.Context) int {
			return int(badge.Load())
		},
		Routes: func(routes *admin.Routes) {
			a := routes.Admin()

			page := func(w http.ResponseWriter, r *http.Request) {
				plain := a.Form(r, struct{}{}, "/admin/tools/send", "Send")
				view := ToolsView{
					Reset:  a.Confirm(r, a.Form(r, struct{}{}, "/admin/tools/reset", "Reset"), "Reset the votes?"),
					Remove: a.Confirm(r, a.Form(r, struct{}{}, "/admin/tools/delete", "Delete"), ""),
					Plain:  &plain,
					Group:  a.Form(r, &addressInput{}, "/admin/tools/address", "Save"),
					// A route named unpublish asks first, as delete does.
					Unpublish: a.Form(r, struct{}{}, "/admin/tools/unpublish", "Unpublish"),
				}

				a.Render(w, r, admin.Page{Title: "Tools", Template: "templates/tools.gohtml", Files: toolsFiles, Data: view})
			}

			routes.Get("", admin.View, page)
			routes.Get("/failed", admin.View, func(w http.ResponseWriter, r *http.Request) {
				page(w, r.WithContext(flash.Add(r.Context(), flash.Error("The mailing failed."))))
			})
			routes.Get("/broken", admin.View, func(w http.ResponseWriter, r *http.Request) {
				a.Render(
					w,
					r,
					admin.Page{Title: "Tools", Template: "templates/broken.gohtml", Files: toolsFiles, Data: struct{}{}},
				)
			})
			routes.Get("/count", admin.View, func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, a.T(r, "%d views today", 3))
			})
			routes.Post("/check", admin.Edit, func(w http.ResponseWriter, r *http.Request) {
				var input emailInput

				if err := form.MapForm(r, &input); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)

					return
				}

				for _, problem := range a.Validate(r, &input) {
					_, message := problem.FieldError()
					_, _ = fmt.Fprintln(w, message)
				}
			})
			routes.Post("/send", admin.Edit, func(w http.ResponseWriter, r *http.Request) {
				a.RedirectNotice(w, r, "/admin/tools", "Mailing sent.")
			})
			routes.Post("/quiet", admin.Edit, func(w http.ResponseWriter, r *http.Request) {
				a.RedirectNotice(w, r, "/admin/tools", "")
			})
			// The toast comes from the route's name (see admin.RouteName).
			routes.Post("/publish", admin.Edit, func(w http.ResponseWriter, r *http.Request) {
				a.Redirect(w, r, "/admin/tools")
			}).As("publish")
			routes.Post("/unpublish", admin.Edit, func(w http.ResponseWriter, r *http.Request) {
				a.Redirect(w, r, "/admin/tools")
			}).As("unpublish")
		},
	}
}

var hxConfirm = regexp.MustCompile(`hx-confirm="([^"]*)"`)

func confirmation(t *testing.T, body, id string) string {
	t.Helper()

	start := strings.Index(body, `<div id="`+id+`">`)
	if start < 0 {
		t.Fatalf("no %s block", id)
	}

	block := body[start:]
	block = block[:strings.Index(block, "</form>")]

	if match := hxConfirm.FindStringSubmatch(block); match != nil {
		return match[1]
	}

	return ""
}

func TestConfirmations(t *testing.T) {
	t.Parallel()

	var badge atomic.Int32

	b := start(t, admin.Options{Language: "en"}, toolsSection(&badge))
	b.signIn()

	status, body := b.get("/admin/tools")
	if status != http.StatusOK {
		t.Fatalf("tools: %d %s", status, body)
	}

	if got := confirmation(t, body, "reset"); got != "Reset the votes?" {
		t.Fatalf("custom confirmation: %q", got)
	}

	if got := confirmation(t, body, "remove"); got != "" {
		t.Fatalf("removed delete confirmation still asked: %q", got)
	}

	if got := confirmation(t, body, "plain"); got != "Send it?" {
		t.Fatalf("template confirmation: %q", got)
	}

	if got := confirmation(t, body, "unpublish"); got != "Take this off the website?" {
		t.Fatalf("unpublish confirmation: %q", got)
	}
}

// TestConfirmDialog checks that admin pages ask the questions of their
// buttons in a dialog of the page, with buttons in the admin's language,
// rather than with window.confirm, which some embedded browsers never show.
func TestConfirmDialog(t *testing.T) {
	t.Parallel()

	b := setup(t, nil)
	b.signIn()

	_, body := b.get("/admin/")

	for _, want := range []string{
		`<dialog class="confirm-dialog" data-partial-confirm-dialog aria-labelledby="confirm-question">`,
		`<p id="confirm-question" data-partial-confirm-question></p>`,
		`value="cancel" autofocus>Annuleren</button>`,
		`value="confirm">Bevestigen</button>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %s", want)
		}
	}
}

// TestBreadcrumbs checks the breadcrumbs: they start at the dashboard, show
// the dashboard alone on the dashboard, and are left out of the signed-out
// pages, such as the sign-in, which have no place in the admin.
func TestBreadcrumbs(t *testing.T) {
	t.Parallel()

	var badge atomic.Int32

	b := start(t, admin.Options{Language: "nl"}, toolsSection(&badge))

	if _, body := b.get("/admin/login"); strings.Contains(body, `class="shell-location"`) {
		t.Fatalf("the sign-in page shows breadcrumbs:\n%s", body)
	}

	b.signIn()

	crumbs := regexp.MustCompile(`(?s)<nav class="shell-location"[^>]*>(.*?)</nav>`)
	text := func(path string) string {
		_, body := b.get(path)

		match := crumbs.FindStringSubmatch(body)
		if match == nil {
			t.Fatalf("%s has no breadcrumbs", path)
		}

		return strings.Join(strings.Fields(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(match[1], " ")), " ")
	}

	if got := text("/admin/"); got != "Dashboard" {
		t.Fatalf("dashboard breadcrumbs %q", got)
	}

	if got := text("/admin/tools"); got != "Dashboard Tools" {
		t.Fatalf("section breadcrumbs %q", got)
	}

	if _, body := b.get("/admin/"); strings.Contains(body, "Werkruimte") || strings.Contains(body, "Overzicht") {
		t.Fatal("the dashboard has another name than Dashboard")
	}
}

// toast is the markup of a toast with text, as the admin renders it: its
// level, role, the level go-partial's flash client hides it by, and text.
// The flash templates of a full page, which the client clones for the
// toasts it shows itself, are not toasts.
var toast = toastFinder{regexp.MustCompile(
	`(?s)<div class="toast toast-(\w+)" data-flash-level="(?:\w+)"\s+role="(\w+)"[^>]*>.*?<p class="toast-text">([^<]*)</p>`,
)}

// flashTemplates are the flash templates of a page ({{flashTemplate}}).
var flashTemplates = regexp.MustCompile(`(?s)<template data-flash-template="\w*">.*?</template>`)

// toastFinder finds toasts outside the flash templates.
type toastFinder struct {
	*regexp.Regexp
}

func (f toastFinder) FindStringSubmatch(body string) []string {
	return f.Regexp.FindStringSubmatch(flashTemplates.ReplaceAllString(body, ""))
}

func (f toastFinder) FindAllStringSubmatch(body string, n int) [][]string {
	return f.Regexp.FindAllStringSubmatch(flashTemplates.ReplaceAllString(body, ""), n)
}

func (f toastFinder) MatchString(body string) bool {
	return f.Regexp.MatchString(flashTemplates.ReplaceAllString(body, ""))
}

func TestRedirectNotices(t *testing.T) {
	t.Parallel()

	var badge atomic.Int32

	b := start(t, admin.Options{Language: "nl"}, toolsSection(&badge))
	b.signIn()

	cases := []struct {
		path   string
		notice string
	}{
		{"/admin/tools/send", "Mailing sent."},
		{"/admin/tools/publish", "Gepubliceerd."},
		{"/admin/tools/quiet", ""},
	}

	for _, test := range cases {
		status, body := b.post(test.path, url.Values{})
		if status != http.StatusOK {
			t.Fatalf("%s: %d", test.path, status)
		}

		if !strings.Contains(body, `<div id="flash-messages"`) {
			t.Fatalf("%s: no toast stack in the layout", test.path)
		}

		match := toast.FindStringSubmatch(body)
		if test.notice == "" {
			if match != nil {
				t.Fatalf("%s showed a toast: %q", test.path, match[0])
			}

			continue
		}

		if match == nil || match[3] != test.notice {
			t.Fatalf("%s: toast %q missing: %v", test.path, test.notice, match)
		}

		if match[1] != "success" || match[2] != "status" || !strings.Contains(match[0], `data-flash-level="success"`) {
			t.Fatalf("%s: a notice is a success toast, which hides itself: %q", test.path, match[0])
		}

		// The notice is shown once.
		if _, again := b.get("/admin/tools"); toast.MatchString(again) {
			t.Fatalf("%s: the toast showed twice", test.path)
		}
	}
}

// TestToastsOverHTMX follows a redirect the way HTMX does: the notice
// arrives with the next content swap, appended out of band to the toasts
// already in the layout.
func TestToastsOverHTMX(t *testing.T) {
	t.Parallel()

	var badge atomic.Int32

	b := start(t, admin.Options{Language: "en"}, toolsSection(&badge))
	b.signIn()

	request, err := http.NewRequest(http.MethodPost, b.server.URL+"/admin/tools/send", nil)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("HX-Request", "true")
	request.Header.Set("Sec-Fetch-Site", "same-origin")

	response, err := b.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	_ = response.Body.Close()

	if response.Header.Get("HX-Location") == "" {
		t.Fatalf("no HX-Location: %d", response.StatusCode)
	}

	body := b.hxGet("/admin/tools")
	if strings.Contains(body, "<body") {
		t.Fatal("an HTMX navigation got the whole page")
	}

	if !strings.Contains(body, `hx-swap-oob="beforeend" class="toast-stack"`) {
		t.Fatalf("the toasts are not appended out of band:\n%s", body)
	}

	// The stack itself stays: only toasts carry data-flash-level, which the
	// flash client dismisses, or the request error marker of admin.js.
	stack := body[strings.Index(body, `<div id="flash-messages"`):]
	stack = stack[:strings.Index(stack, ">")]

	if strings.Contains(stack, "data-flash-level") || strings.Contains(stack, "data-request-error") {
		t.Fatalf("the stack looks like a toast: %s", stack)
	}

	if match := toast.FindStringSubmatch(body); match == nil || match[3] != "Mailing sent." {
		t.Fatalf("toast missing from the swap: %v", match)
	}

	if body = b.hxGet("/admin/tools"); toast.MatchString(body) {
		t.Fatal("the next swap repeated the toast")
	}
}

// TestErrorToasts shows a message a handler added with flash.Add: an error
// is an alert and stays until it is dismissed.
func TestErrorToasts(t *testing.T) {
	t.Parallel()

	var badge atomic.Int32

	b := start(t, admin.Options{Language: "en"}, toolsSection(&badge))
	b.signIn()

	_, body := b.get("/admin/tools/failed")

	match := toast.FindStringSubmatch(body)
	if match == nil || match[3] != "The mailing failed." {
		t.Fatalf("error toast missing: %v", match)
	}

	if match[1] != "error" || match[2] != "alert" || !strings.Contains(match[0], `data-flash-level="error"`) {
		t.Fatalf("an error toast is an alert, which stays: %q", match[0])
	}

	if !strings.Contains(body, `data-flash-dismiss aria-label="Dismiss notification"`) {
		t.Fatal("the toast cannot be dismissed")
	}
}

func TestTranslationFallbacks(t *testing.T) {
	t.Parallel()

	var badge atomic.Int32

	// German has no catalogue: messages fall back to the key, with arguments.
	b := start(t, admin.Options{Language: "de"}, toolsSection(&badge))
	b.signIn()

	if _, body := b.get("/admin/tools/count"); body != "3 views today" {
		t.Fatalf("fallback with arguments: %q", body)
	}

	_, body := b.post("/admin/tools/check", url.Values{"Email": {"nobody"}})
	if strings.TrimSpace(body) != "Enter a valid email address." {
		t.Fatalf("go-form's English message: %q", body)
	}

	b = start(t, admin.Options{Language: "nl"}, toolsSection(&badge))
	b.signIn()

	_, body = b.post("/admin/tools/check", url.Values{"Email": {"nobody"}})
	if strings.TrimSpace(body) != "Vul een geldig e-mailadres in." {
		t.Fatalf("Dutch validation message: %q", body)
	}
}

func TestBadgesAndStylesheets(t *testing.T) {
	t.Parallel()

	var (
		badge  atomic.Int32
		secret atomic.Int32
	)

	hidden := admin.Section{
		ID:    "secret",
		Title: "Secret",
		Path:  "/secret",
		Badge: func(context.Context) int {
			secret.Add(1)

			return 1
		},
		Routes: func(*admin.Routes) {},
	}

	b := start(t, admin.Options{
		Language: "en",
		Authorize: func(_ context.Context, _ auth.User, permission admin.Permission) bool {
			return permission.Section != "secret"
		},
	}, toolsSection(&badge), hidden)
	b.signIn()

	_, body := b.get("/admin/tools")
	if strings.Contains(body, `class="nav-badge"`) {
		t.Fatal("a zero badge is shown")
	}

	badge.Store(7)

	_, body = b.get("/admin/tools")
	if !strings.Contains(body, `<span class="nav-badge">7</span>`) {
		t.Fatal("badge missing from the navigation")
	}

	if secret.Load() != 0 {
		t.Fatal("the badge of a section the user cannot see was counted")
	}

	link := regexp.MustCompile(`<link rel="stylesheet" href="(/admin/assets/sections/tools\.css\?v=[0-9a-f]+)" integrity="sha384-[^"]+"`).
		FindStringSubmatch(body)
	if link == nil {
		t.Fatal("section stylesheet not linked")
	}

	response, err := b.client.Get(b.server.URL + link[1])
	if err != nil {
		t.Fatal(err)
	}

	_ = response.Body.Close()

	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/css") {
		t.Fatalf("stylesheet: %d %s", response.StatusCode, response.Header.Get("Content-Type"))
	}

	if status, _ := b.get("/admin/assets/sections/unknown.css"); status != http.StatusNotFound {
		t.Fatalf("unknown stylesheet: %d", status)
	}
}

func TestMissingStylesheet(t *testing.T) {
	t.Parallel()

	accounts := newAccounts(t)

	section := admin.Section{
		ID:         "broken",
		Title:      "Broken",
		Path:       "/broken",
		Files:      fstest.MapFS{},
		Stylesheet: "assets/missing.css",
		Routes:     func(*admin.Routes) {},
	}

	if _, err := admin.New(accounts, admin.Options{CatalogueDir: t.TempDir()}, section); err == nil {
		t.Fatal("a missing stylesheet was accepted")
	}
}

func TestFieldGroups(t *testing.T) {
	t.Parallel()

	var badge atomic.Int32

	b := start(t, admin.Options{Language: "en"}, toolsSection(&badge))
	b.signIn()

	_, body := b.get("/admin/tools")

	for _, class := range []string{"field-group", "field-group-legend", "field-group-body"} {
		if !strings.Contains(body, `class="`+class+`"`) {
			t.Errorf("grouped fields lack %s", class)
		}
	}
}

// TestAuthorizeAdministrators checks that Authorize, when set, is also asked
// about administrators, including the Accounts and Groups screens.
func TestAuthorizeAdministrators(t *testing.T) {
	t.Parallel()

	var asked []admin.Permission

	b := start(t, admin.Options{
		Language: "en",
		Authorize: func(_ context.Context, user auth.User, permission admin.Permission) bool {
			if !user.IsAdmin() {
				t.Errorf("Authorize asked about %s, not the administrator", user.Email)
			}

			asked = append(asked, permission)

			return permission.Section != "accounts"
		},
	})
	b.signIn()

	if status, _ := b.get("/admin/accounts"); status != http.StatusForbidden {
		t.Fatalf("accounts refused by Authorize: %d", status)
	}

	if status, _ := b.get("/admin/groups"); status != http.StatusOK {
		t.Fatalf("groups allowed by Authorize: %d", status)
	}

	_, body := b.get("/admin/")
	if strings.Contains(body, `href="/admin/accounts"`) || !strings.Contains(body, `href="/admin/groups"`) {
		t.Fatal("the navigation does not follow Authorize")
	}

	sections := map[string]bool{}
	for _, permission := range asked {
		sections[permission.Section] = true
	}

	if !sections["accounts"] || !sections["groups"] {
		t.Fatalf("Authorize was asked about %v", asked)
	}
}

// TestErrorPages checks that a page that fails to render shows the admin's
// error page, and a partial request that fails adds an error toast out of
// band, both with status 500 and without the error's details.
func TestErrorPages(t *testing.T) {
	t.Parallel()

	var badge atomic.Int32

	b := start(t, admin.Options{Language: "en"}, toolsSection(&badge))
	b.signIn()

	status, body := b.get("/admin/tools/broken")
	if status != http.StatusInternalServerError || !strings.Contains(body, "This page could not be shown.") ||
		!strings.Contains(body, `<html lang="en">`) ||
		!strings.Contains(body, `<link rel="stylesheet" href="/admin/assets/admin.css`) {
		t.Fatalf("error page: %d\n%s", status, body)
	}

	if strings.Contains(body, "Missing") || strings.Contains(body, "broken.gohtml") {
		t.Fatalf("the error page shows the error:\n%s", body)
	}

	request, err := http.NewRequest(http.MethodGet, b.server.URL+"/admin/tools/broken", nil)
	if err != nil {
		t.Fatal(err)
	}

	connector.SetRequestTarget(connector.NewHTMX4(nil), request, "content")

	response, err := b.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	fragment, _ := io.ReadAll(response.Body)

	match := toast.FindStringSubmatch(string(fragment))
	if response.StatusCode != http.StatusInternalServerError || match == nil || match[1] != "error" ||
		match[3] != "The request failed. Please try again." ||
		!strings.Contains(string(fragment), `<div id="flash-messages" hx-swap-oob="beforeend">`) ||
		response.Header.Get("HX-Reswap") != "none" {
		t.Fatalf("error fragment: %d\n%s", response.StatusCode, fragment)
	}

	if strings.Contains(string(fragment), "Missing") || strings.Contains(string(fragment), "<main") {
		t.Fatalf("the error fragment shows the error or the page:\n%s", fragment)
	}
}

// TestCrossOriginRefusal checks that a post from another website is refused
// with status 403 and a sentence: a toast, out of band, for HTMX, which
// leaves the page as it is, and the text for any other request.
func TestCrossOriginRefusal(t *testing.T) {
	t.Parallel()

	var badge atomic.Int32

	b := start(t, admin.Options{Language: "en"}, toolsSection(&badge))
	b.signIn()

	const refused = "This request came from another website and was refused."

	status, body := b.postFrom("https://evil.example", "cross-site", "/admin/tools/send", url.Values{})
	if status != http.StatusForbidden || strings.TrimSpace(body) != refused {
		t.Fatalf("plain post: %d %q", status, body)
	}

	request, err := http.NewRequest(http.MethodPost, b.server.URL+"/admin/tools/send", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("Origin", "https://evil.example")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	connector.SetRequestTarget(connector.NewHTMX4(nil), request, "content")

	response, err := b.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	refusal, _ := io.ReadAll(response.Body)

	match := toast.FindStringSubmatch(string(refusal))
	if response.StatusCode != http.StatusForbidden || match == nil || match[1] != "error" || match[3] != refused ||
		!strings.Contains(string(refusal), `<div id="flash-messages" hx-swap-oob="beforeend">`) ||
		response.Header.Get("HX-Reswap") != "none" {
		t.Fatalf("HTMX post: %d %v\n%s", response.StatusCode, response.Header, refusal)
	}
}

// TestShellClientSettings checks what the layout gives the browser clients:
// the body's settings for every link and form, and the toast stack's flash
// templates, which the flash client clones for failed requests, on a full
// page only: an HTMX navigation appends no copies to the stack.
func TestShellClientSettings(t *testing.T) {
	t.Parallel()

	var badge atomic.Int32

	b := start(t, admin.Options{Language: "en"}, toolsSection(&badge))
	b.signIn()

	_, page := b.get("/admin/tools")

	for _, want := range []string{
		`hx-boost:inherited="target:#content swap:outerHTML"`,
		`hx-disable:inherited="find button[type=&#39;submit&#39;]"`,
		`hx-status:5xx:inherited="swap:none push:false"`,
		`<div id="flash-messages" data-flash-request-error="The request failed. Please try again."` +
			` data-flash-request-error-replace data-flash-request-error-body data-flash-request-error-skip-flash`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page lacks %s:\n%s", want, page)
		}
	}

	errorTemplate := regexp.MustCompile(
		`<template data-flash-template="error">\s*<div class="toast toast-error" data-flash-level="error"`,
	)
	if !errorTemplate.MatchString(page) {
		t.Fatalf("the page lacks the error toast's flash template:\n%s", page)
	}

	if body := b.hxGet("/admin/tools"); strings.Contains(body, "data-flash-template") {
		t.Fatalf("an HTMX navigation sent the flash templates again:\n%s", body)
	}
}

// TestShellTitle checks the browser tab's title: the page's heading and the
// admin's title, in the head of a full page and along with an HTMX
// navigation, from which HTMX sets the document's title, as the header's
// breadcrumb follows the page.
func TestShellTitle(t *testing.T) {
	t.Parallel()

	var badge atomic.Int32

	b := start(t, admin.Options{Language: "en"}, toolsSection(&badge))
	b.signIn()

	_, page := b.get("/admin/tools")
	if !strings.Contains(page, "<title>Tools · Test site</title>") {
		t.Fatalf("the page lacks its title:\n%s", page)
	}

	navigation := b.hxGet("/admin/")
	if !strings.Contains(navigation, "<title>Dashboard · Test site</title>") ||
		!strings.Contains(navigation, `<span class="shell-page" aria-current="page">Dashboard</span>`) {
		t.Fatalf("the navigation lacks the title:\n%s", navigation)
	}
}
