package admin_test

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
)

// errorToast reports whether body shows text as an error toast.
func errorToast(body, text string) bool {
	for _, match := range toast.FindAllStringSubmatch(body, -1) {
		if match[1] == "error" && match[3] == html.EscapeString(text) {
			return true
		}
	}

	return false
}

// inlineNotice is a notice paragraph inside the page, which refusals no
// longer use.
var inlineNotice = regexp.MustCompile(`<p (role="status" )?class="notice`)

// TestRefusalsAreToasts checks that a refused action shows its reason as an
// error toast, while a refusal of one field of a form shows at that field.
func TestRefusalsAreToasts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	accounts := newAccounts(t)
	b := startWith(t, accounts, admin.Options{Language: "en"})
	b.signIn()

	users, err := accounts.Users(ctx)
	if err != nil {
		t.Fatal(err)
	}

	owner := "/admin/accounts/" + users[0].ID

	// The last administrator stays: a toast on the Accounts screen.
	status, body := b.post(owner+"/status", url.Values{})
	if status != http.StatusConflict || !errorToast(body, "At least one active administrator must remain.") {
		t.Fatalf("disable the last administrator: %d\n%s", status, body)
	}

	if inlineNotice.MatchString(body) {
		t.Fatalf("the refusal is shown inside the page:\n%s", body)
	}

	// A taken address is an error of the new account's e-mail field.
	status, body = b.post("/admin/accounts", url.Values{
		"Email":    {"owner@example.test"},
		"Password": {"another long passphrase"},
		"Role":     {string(auth.Editor)},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "This email address already has an account.") {
		t.Fatalf("taken address: %d\n%s", status, body)
	}

	if errorToast(body, "This email address already has an account.") {
		t.Fatal("a field error shows as a toast")
	}

	// A taken group name is an error of the new group's name field.
	if _, err := accounts.SaveGroup(ctx, auth.Group{Name: "Editors"}); err != nil {
		t.Fatal(err)
	}

	status, body = b.post("/admin/groups", url.Values{"Name": {"Editors"}})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "A group with this name already exists.") {
		t.Fatalf("taken group name: %d\n%s", status, body)
	}

	if errorToast(body, "A group with this name already exists.") {
		t.Fatal("a field error shows as a toast")
	}

	// The rights matrix shows the refusal at its name field too.
	writers, err := accounts.SaveGroup(ctx, auth.Group{Name: "Writers"})
	if err != nil {
		t.Fatal(err)
	}

	status, body = b.post("/admin/groups/"+writers.ID, url.Values{"name": {"Editors"}})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "A group with this name already exists.") ||
		errorToast(body, "A group with this name already exists.") {
		t.Fatalf("rename to a taken name: %d\n%s", status, body)
	}

	if inlineNotice.MatchString(body) {
		t.Fatalf("the refusal is shown inside the page:\n%s", body)
	}

	// The own password form shows its refusals at the fields.
	status, body = b.post("/admin/account", url.Values{
		"Current":  {"wrong password!"},
		"Password": {"a brand new passphrase"},
		"Repeat":   {"a brand new passphrase"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Your current password is not correct.") {
		t.Fatalf("wrong current password: %d\n%s", status, body)
	}

	if errorToast(body, "Your current password is not correct.") || inlineNotice.MatchString(body) {
		t.Fatalf("a field error shows outside its field:\n%s", body)
	}
}
