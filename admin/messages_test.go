package admin_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"testing/fstest"

	"github.com/donseba/go-webthings/admin"
)

const countsHeader = `msgid ""
msgstr "Language: %s\nContent-Type: text/plain; charset=UTF-8\nPlural-Forms: nplurals=2; plural=(n != 1);\n"
`

// countsFiles has a plural message in English and Dutch, as a module's
// catalogues would.
var countsFiles = fstest.MapFS{
	"translations/en_US.po": {Data: []byte(fmt.Sprintf(countsHeader, "en_US") + `
msgid "%d vote"
msgid_plural "%d votes"
msgstr[0] "%d vote"
msgstr[1] "%d votes"

msgid "Closed"
msgstr "Closed"
`)},
	"translations/nl_NL.po": {Data: []byte(fmt.Sprintf(countsHeader, "nl_NL") + `
msgid "%d vote"
msgid_plural "%d votes"
msgstr[0] "%d stem"
msgstr[1] "%d stemmen"

msgid "Closed"
msgstr "Gesloten"
`)},
}

// countsSection answers /admin/counts?n= with the vote count and "Closed",
// translated from the request context only.
func countsSection() admin.Section {
	return admin.Section{
		ID:    "counts",
		Title: "Counts",
		Path:  "/counts",
		Files: countsFiles,
		Routes: func(routes *admin.Routes) {
			routes.Get("", admin.View, func(w http.ResponseWriter, r *http.Request) {
				n, _ := strconv.Atoi(r.URL.Query().Get("n"))
				ctx := r.Context()

				_, _ = fmt.Fprint(w, admin.TN(ctx, "%d vote", "%d votes", n, n), " · ", admin.T(ctx, "Closed"))
			})
		},
	}
}

func TestContextTranslations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		language string
		n        int
		want     string
	}{
		{"en", 1, "1 vote · Closed"},
		{"en", 3, "3 votes · Closed"},
		{"nl", 0, "0 stemmen · Gesloten"},
		{"nl", 1, "1 stem · Gesloten"},
		{"nl", 3, "3 stemmen · Gesloten"},
		// German has no catalogue: the English forms are formatted.
		{"de", 1, "1 vote · Closed"},
		{"de", 2, "2 votes · Closed"},
	}

	browsers := map[string]browser{}

	for _, test := range tests {
		b, ok := browsers[test.language]
		if !ok {
			b = start(t, admin.Options{Language: test.language}, countsSection())
			b.signIn()
			browsers[test.language] = b
		}

		if _, body := b.get("/admin/counts?n=" + strconv.Itoa(test.n)); body != test.want {
			t.Errorf("%s, %d: %q, want %q", test.language, test.n, body, test.want)
		}
	}
}

// TestCatalogueDir checks that a catalogue in CatalogueDir wins over the
// sections' own, that its empty entries keep theirs, that a regional
// language uses the catalogue of its language, and that the admin writes
// nothing to the folder.
func TestCatalogueDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	override := fmt.Sprintf(countsHeader, "nl_NL") + `
msgid "%d vote"
msgid_plural "%d votes"
msgstr[0] ""
msgstr[1] ""

msgid "Closed"
msgstr "Dicht"
`

	if err := os.WriteFile(filepath.Join(dir, "nl_NL.po"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		language string
		want     string
	}{
		{"nl", "3 stemmen · Dicht"},
		{"nl_BE", "3 stemmen · Dicht"},
		{"nl-NL", "3 stemmen · Dicht"},
	}

	for _, test := range tests {
		b := start(t, admin.Options{Language: test.language, CatalogueDir: dir}, countsSection())
		b.signIn()

		if _, body := b.get("/admin/counts?n=3"); body != test.want {
			t.Errorf("%s: %q, want %q", test.language, body, test.want)
		}
	}

	written, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(written) != 1 {
		t.Fatalf("the catalogue folder has %v", written)
	}
}

func TestContextTranslationsOutsideAdmin(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	if got := admin.T(ctx, "%d views today", 3); got != "3 views today" {
		t.Fatalf("T outside the admin: %q", got)
	}

	if got := admin.T(ctx, "100% done"); got != "100% done" {
		t.Fatalf("T without arguments: %q", got)
	}

	if got := admin.TN(ctx, "%d vote", "%d votes", 1, 1); got != "1 vote" {
		t.Fatalf("TN singular outside the admin: %q", got)
	}

	if got := admin.TN(ctx, "%d vote", "%d votes", 4, 4); got != "4 votes" {
		t.Fatalf("TN plural outside the admin: %q", got)
	}
}
