package pageadmin_test

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/sitetest"
)

// remembered is the version of the page an edit form remembers, and
// saveAnyway the value of the notice's "Save anyway" button.
var (
	remembered = regexp.MustCompile(`<input type="hidden" name="_version" value="([^"]*)">`)
	saveAnyway = regexp.MustCompile(
		`<button class="button" type="submit" form="edit-form" name="_overwrite" value="([^"]*)">Save anyway</button>`,
	)
)

// savedByAda is the notice of a save refused because Ada saved the page.
var savedByAda = regexp.MustCompile(`Ada Lovelace saved this item at \d{2}:\d{2} while you were editing it\. ` +
	`Open their version in a new tab to compare, or save yours over it\.`)

// found returns the first group of pattern in page, failing the test when
// it is missing.
func found(t *testing.T, page string, pattern *regexp.Regexp) string {
	t.Helper()

	match := pattern.FindStringSubmatch(page)
	if match == nil {
		t.Fatalf("no %s in:\n%s", pattern, page)
	}

	return html.UnescapeString(match[1])
}

// TestSaveOverNewerSave checks that the page editor refuses a save over a
// save someone made after the form was opened, keeps what was typed, and
// saves it with "Save anyway"; a page saved twice in a row is not refused.
func TestSaveOverNewerSave(t *testing.T) {
	s := sitetest.Start(t)
	ctx := context.Background()

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.App.Auth.CreateUser(ctx, "ada@example.test", "Ada Lovelace", "ada long passphrase", auth.Admin)
	if err != nil {
		t.Fatal(err)
	}

	ada := s.As("ada@example.test", "ada long passphrase")
	editor := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About"}, "Parent": {home}}) + "?locale=en"

	_, opened := s.Get(editor)
	mine := url.Values{"Title": {"About us"}, "Parent": {home}, "_version": {found(t, opened, remembered)}}

	_, page := ada.Get(editor)
	ada.Post(editor, editor, url.Values{"Title": {"About Ada"}, "Parent": {home}, "_version": {found(t, page, remembered)}})

	status, refused := s.PostStatus(editor, editor, mine)
	if status != http.StatusUnprocessableEntity || !savedByAda.MatchString(refused) {
		t.Fatalf("a save over Ada's answers %d:\n%s", status, refused)
	}

	if !regexp.MustCompile(`name="Title"[^>]*value="About us"|value="About us"[^>]*name="Title"`).MatchString(refused) {
		t.Fatalf("the refused form lost the typed title:\n%s", refused)
	}

	mine.Set("_overwrite", found(t, refused, saveAnyway))

	status, saved := s.PostStatus(editor, editor, mine)
	if status != http.StatusOK ||
		!sitetest.HasToast(saved, "success", "Saved as a draft. Publish it to show it on the website.") {
		t.Fatalf("save anyway answers %d:\n%s", status, saved)
	}

	for _, title := range []string{"About our team", "About our work"} {
		again := url.Values{"Title": {title}, "Parent": {home}, "_version": {found(t, saved, remembered)}}

		status, saved = s.PostStatus(editor, editor, again)
		if status != http.StatusOK {
			t.Fatalf("saving %q after a save answers %d:\n%s", title, status, saved)
		}
	}

	s.Expect(editor, http.StatusOK, `value="About our work"`)
}
