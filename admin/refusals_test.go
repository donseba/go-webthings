package admin_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"

	"github.com/donseba/go-partial/exp/flash"
)

// pageSection is a section with an empty page and the given extra routes.
func pageSection(id string, routes func(routes *admin.Routes)) admin.Section {
	return admin.Section{
		ID:    id,
		Title: id,
		Path:  "/" + id,
		Routes: func(r *admin.Routes) {
			a := r.Admin()

			r.Get("", admin.View, func(w http.ResponseWriter, req *http.Request) {
				a.Render(w, req, admin.Page{Title: id, Template: "templates/trash.gohtml", Data: admin.TrashView{}})
			})

			if routes != nil {
				routes(r)
			}
		},
	}
}

// TestRedirectFlash checks that a toast passed through a redirect keeps its
// level and its text: a warning stays until it is dismissed, and the text
// is not translated again, even when it is a catalogue key.
func TestRedirectFlash(t *testing.T) {
	t.Parallel()

	section := pageSection("flashy", func(routes *admin.Routes) {
		a := routes.Admin()

		routes.Post("/warn", admin.Edit, func(w http.ResponseWriter, r *http.Request) {
			a.RedirectFlash(w, r, "/admin/flashy", flash.Warn("Published."))
		})
		routes.Post("/long", admin.Edit, func(w http.ResponseWriter, r *http.Request) {
			a.RedirectFlash(w, r, "/admin/flashy", flash.Error(strings.Repeat("é", 3000)))
		})
	})

	b := start(t, admin.Options{Language: "nl"}, section)
	b.signIn()

	_, body := b.post("/admin/flashy/warn", url.Values{})

	match := toast.FindStringSubmatch(body)
	if match == nil || match[1] != "warn" || match[3] != "Published." {
		t.Fatalf("warning toast: %v", match)
	}

	// A text too long for the cookie is shortened at a character boundary,
	// not dropped.
	_, body = b.post("/admin/flashy/long", url.Values{})

	match = toast.FindStringSubmatch(body)
	if match == nil || match[1] != "error" || match[3] == "" || strings.Trim(match[3], "é") != "" ||
		len(match[3]) >= len(strings.Repeat("é", 3000)) {
		t.Fatalf("long toast: %v", match)
	}
}

// TestTranslatedRefusals checks that a post the admin cannot read and a
// refused right answer with a sentence in the admin language, which
// admin.js shows, rather than a machine code.
func TestTranslatedRefusals(t *testing.T) {
	t.Parallel()

	b := start(t, admin.Options{
		Language: "nl",
		Authorize: func(_ context.Context, _ auth.User, permission admin.Permission) bool {
			return permission.Action != admin.Create
		},
	})
	b.signIn()

	request, err := http.NewRequest(http.MethodPost, b.server.URL+"/admin/groups", strings.NewReader("garbage"))
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("Content-Type", "multipart/form-data; boundary=x")

	response, err := b.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	text, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()

	if response.StatusCode != http.StatusBadRequest || strings.TrimSpace(string(text)) != "Dit verzoek kon niet worden gelezen." {
		t.Fatalf("unreadable post: %d %q", response.StatusCode, text)
	}

	status, body := b.post("/admin/groups", url.Values{"Name": {"Writers"}})
	if status != http.StatusForbidden || strings.TrimSpace(body) != "Je hebt hier geen toestemming voor." {
		t.Fatalf("refused right: %d %q", status, body)
	}
}

// TestTrashKeepsSearch checks that restoring from a searched trash comes
// back to the same search, with the count shown as it was written.
func TestTrashKeepsSearch(t *testing.T) {
	t.Parallel()

	section := pageSection("basket", nil)
	section.Trash = &memoryTrash{items: []admin.TrashItem{
		{ID: "a", Title: "Apple pie", DeletedAt: time.Now()},
		{ID: "b", Title: "Banana bread", DeletedAt: time.Now()},
	}}

	b := start(t, admin.Options{Language: "nl"}, section)
	b.signIn()

	_, body := b.get("/admin/trash?q=apple")
	if !strings.Contains(body, `action="/admin/trash?q=apple"`) {
		t.Fatalf("the trash form drops the search:\n%s", body)
	}

	request, err := http.NewRequest(http.MethodPost, b.server.URL+"/admin/trash?q=apple",
		strings.NewReader(url.Values{"action": {"restore"}, "item": {"basket:a"}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := b.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	page, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()

	if response.Request.URL.RequestURI() != "/admin/trash?q=apple" {
		t.Fatalf("after restoring: %s", response.Request.URL.RequestURI())
	}

	match := toast.FindStringSubmatch(string(page))
	if match == nil || match[1] != "success" || match[3] != "1 item hersteld." {
		t.Fatalf("restore notice: %v", match)
	}
}

// TestGroupNameRefusals checks that an empty group name is explained as
// such, not as a taken name. A name of only spaces is already refused by
// the form as missing.
func TestGroupNameRefusals(t *testing.T) {
	t.Parallel()

	accounts := newAccounts(t)
	b := startWith(t, accounts, admin.Options{Language: "en"})
	b.signIn()

	status, body := b.post("/admin/groups", url.Values{"Name": {"   "}})
	if status != http.StatusUnprocessableEntity || strings.Contains(body, "already exists") {
		t.Fatalf("blank group name: %d\n%s", status, body)
	}

	group, err := accounts.SaveGroup(context.Background(), auth.Group{Name: "Writers"})
	if err != nil {
		t.Fatal(err)
	}

	// The matrix shows the refusal at the name, which keeps what was posted.
	status, body = b.post("/admin/groups/"+group.ID, url.Values{"name": {"   "}})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "This field is required.") ||
		!strings.Contains(body, `aria-invalid="true"`) {
		t.Fatalf("rename to nothing: %d\n%s", status, body)
	}

	status, body = b.post("/admin/groups/"+group.ID, url.Values{"name": {strings.Repeat("x", 101)}})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, `aria-invalid="true"`) {
		t.Fatalf("rename to a long name: %d\n%s", status, body)
	}
}

// TestProfileRefusalsKeepInput checks that a refused profile or role change
// shows the reason at its field and keeps what was posted.
func TestProfileRefusalsKeepInput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	accounts := newAccounts(t)
	b := startWith(t, accounts, admin.Options{Language: "en"})
	b.signIn()

	editor, err := accounts.CreateUser(ctx, "editor@example.test", "Eddie", "editor long passphrase", auth.Editor)
	if err != nil {
		t.Fatal(err)
	}

	profile := "/admin/accounts/" + editor.ID + "/profile"

	// A name that is too long is not an invalid e-mail address.
	status, body := b.post(profile, url.Values{"Email": {"eddie@example.test"}, "Name": {strings.Repeat("x", 201)}})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, `value="eddie@example.test"`) ||
		strings.Contains(body, "Enter a valid email address.") || toast.MatchString(body) {
		t.Fatalf("long name: %d\n%s", status, body)
	}

	status, body = b.post(profile, url.Values{"Email": {"owner@example.test"}, "Name": {"Eddie"}})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "This email address already has an account.") ||
		errorToast(body, "This email address already has an account.") || !strings.Contains(body, `value="owner@example.test"`) {
		t.Fatalf("taken address: %d\n%s", status, body)
	}

	// The last administrator keeps the role; the refusal is at the field.
	users, err := accounts.Users(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var owner string

	for _, user := range users {
		if user.IsAdmin() {
			owner = user.ID
		}
	}

	status, body = b.post("/admin/accounts/"+owner+"/role", url.Values{"Role": {string(auth.Editor)}})
	if status != http.StatusConflict || !strings.Contains(body, "At least one active administrator must remain.") ||
		errorToast(body, "At least one active administrator must remain.") {
		t.Fatalf("last administrator's role: %d\n%s", status, body)
	}
}

// TestExportRight checks that the Export right guards exports, and that
// the matrix offers it only for sections with an export.
func TestExportRight(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	accounts := newAccounts(t)

	letters := pageSection("letters", func(routes *admin.Routes) {
		routes.Get("/export", admin.Export, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "email\n")
		})
	})
	notes := pageSection("notes", nil)

	b := startWith(t, accounts, admin.Options{Language: "nl"}, letters, notes)
	b.signIn()

	editor, err := accounts.CreateUser(ctx, "editor@example.test", "", "editor long passphrase", auth.Editor)
	if err != nil {
		t.Fatal(err)
	}

	group, err := accounts.SaveGroup(ctx, auth.Group{
		Name:        "Writers",
		Permissions: auth.Permissions{"letters": {auth.Read, auth.Edit}, "notes": {auth.Read}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := accounts.SetUserGroups(ctx, editor.ID, []string{group.ID}); err != nil {
		t.Fatal(err)
	}

	_, body := b.get("/admin/groups/" + group.ID)
	if !strings.Contains(body, "Exporteren") || !strings.Contains(body, `value="letters:export"`) ||
		strings.Contains(body, `value="notes:export"`) {
		t.Fatalf("matrix export column:\n%s", body)
	}

	writer := browser{t: t, client: newClient(b.server), server: b.server}
	writer.signInAs("editor@example.test", "editor long passphrase")

	if status, _ := writer.get("/admin/letters/export"); status != http.StatusForbidden {
		t.Fatalf("export without the right: %d", status)
	}

	group.Permissions["letters"] = append(group.Permissions["letters"], auth.Export)
	if _, err := accounts.SaveGroup(ctx, group); err != nil {
		t.Fatal(err)
	}

	if status, csv := writer.get("/admin/letters/export"); status != http.StatusOK || csv != "email\n" {
		t.Fatalf("export with the right: %d %q", status, csv)
	}

	// Without any export the matrix has no Export column.
	others := newAccounts(t)
	plain := startWith(t, others, admin.Options{Language: "nl"}, pageSection("notes", nil))
	plain.signIn()

	readers, err := others.SaveGroup(ctx, auth.Group{Name: "Readers"})
	if err != nil {
		t.Fatal(err)
	}

	if _, body := plain.get(
		"/admin/groups/" + readers.ID,
	); strings.Contains(body, "Exporteren") ||
		strings.Contains(body, ":export") {
		t.Fatal("the matrix offers exports without any")
	}
}

// TestPluralAndContextFallbacks checks that tn, ctl and ctn in admin
// templates fall back like tl, instead of showing go-translator's markers.
func TestPluralAndContextFallbacks(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{"templates/words.gohtml": {Data: []byte(`<main id="content">` +
		`<p id="tn">{{tn localizer "%d apple" "%d apples" 2 2}}</p>` +
		`<p id="ctl">{{ctl localizer "month" "May %d" 5}}</p>` +
		`<p id="ctn">{{ctn localizer "fruit" "%d pear" "%d pears" 1 1}}</p>` +
		`</main>`)}}

	section := admin.Section{
		ID:    "words",
		Title: "Words",
		Path:  "/words",
		Files: files,
		Routes: func(routes *admin.Routes) {
			a := routes.Admin()

			routes.Get("", admin.View, func(w http.ResponseWriter, r *http.Request) {
				a.Render(w, r, admin.Page{Title: "Words", Template: "templates/words.gohtml", Files: files})
			})
		},
	}

	b := start(t, admin.Options{Language: "nl"}, section)
	b.signIn()

	_, body := b.get("/admin/words")

	for _, want := range []string{`<p id="tn">2 apples</p>`, `<p id="ctl">May 5</p>`, `<p id="ctn">1 pear</p>`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s:\n%s", want, body)
		}
	}
}
