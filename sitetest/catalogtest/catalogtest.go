// Package catalogtest checks in tests that the admin's catalogues translate
// every text of a section's forms in each language the admin comes with, so
// a label or description added to a form cannot go untranslated:
//
//	func TestCatalogues(t *testing.T) {
//		catalogtest.Complete(t, files, settingsInput{}, entryInput{})
//	}
//
// files is the section's Files, with its catalogues in translations/. Like
// sitetest, it is for tests only.
package catalogtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	form "github.com/donseba/go-form/v2"
	translator "github.com/donseba/go-translator/v2"
)

// Languages are the languages of the bundled admin catalogues.
var Languages = []string{"en_US", "nl_NL"}

// Source is the language the texts are written in. Complete does not
// check its catalogue: a text it misses shows as written, which is the
// text in that language already.
const Source = "en_US"

// Complete fails t for every text of models, go-form's Texts of the forms
// a section shows, that the admin's catalogues of one of Languages but
// Source miss:
// translations/<language>.po of the admin itself and of section, a
// section's Files (nil for the admin's own forms), layered as the admin
// loads them (go-translator's LoadLanguages). An empty translation counts
// as missing.
func Complete(t testing.TB, section fs.FS, models ...any) {
	t.Helper()

	texts := form.Texts(models...)
	if len(texts) == 0 {
		t.Fatal("the forms have no texts")
	}

	CompleteTexts(t, section, texts...)
}

// CompleteTexts is Complete for texts a section shows other than its forms'
// labels, such as the messages of its templates ({{tl localizer "…"}}) and
// of its handlers (Admin.T).
func CompleteTexts(t testing.TB, section fs.FS, texts ...string) {
	t.Helper()

	layers := []fs.FS{adminCatalogues(t)}

	if section != nil {
		folder, err := fs.Sub(section, "translations")
		if err != nil {
			t.Fatal(err)
		}

		layers = append(layers, folder)
	}

	tr := translator.New()

	if err := tr.LoadLanguages(layers...); err != nil {
		t.Fatal(err)
	}

	for _, language := range Languages {
		if language == Source {
			continue
		}

		loc := locale(language)

		for _, text := range texts {
			if _, ok := tr.Lookup(loc, text); !ok {
				t.Errorf("translations/%s.po misses %q", language, text)
			}
		}
	}
}

// adminCatalogues is the folder of the admin's own catalogues,
// admin/translations in go-webthings' source, next to this package's.
func adminCatalogues(t testing.TB) fs.FS {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("the source of package catalogtest is unknown")
	}

	return os.DirFS(filepath.Join(filepath.Dir(file), "..", "..", "admin", "translations"))
}

// locale is a language as a go-translator Localizer.
type locale string

// GetLocale returns the language.
func (l locale) GetLocale() string {
	return string(l)
}
