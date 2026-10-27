package frontend_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/donseba/go-webthings/frontend"

	translator "github.com/donseba/go-translator/v2"
)

type locale string

func (l locale) GetLocale() string {
	return string(l)
}

const dutch = `msgid ""
msgstr ""
"Language: nl\n"
"Content-Type: text/plain; charset=UTF-8\n"

msgid "Read more"
msgstr "Lees meer"
`

// TestTranslationsWithoutPot checks that looking up texts writes no
// translations.pot and logs nothing.
func TestTranslationsWithoutPot(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "nl.po"), []byte(dutch), 0o644); err != nil {
		t.Fatal(err)
	}

	tr, err := frontend.Translations(dir)
	if err != nil {
		t.Fatal(err)
	}

	tl, ok := tr.FuncMap()["tl"].(func(translator.Localizer, string, ...any) string)
	if !ok {
		t.Fatalf("tl has type %T", tr.FuncMap()["tl"])
	}

	stdout := os.Stdout

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	os.Stdout = writer

	translated := tl(locale("nl"), "Read more")
	missing := tl(locale("nl"), "Not translated %d", 3)

	os.Stdout = stdout

	_ = writer.Close()

	printed, _ := io.ReadAll(reader)

	if translated != "Lees meer" || missing != "Not translated 3" {
		t.Fatalf("lookups: %q %q", translated, missing)
	}

	if len(printed) > 0 {
		t.Fatalf("printed: %s", printed)
	}

	if _, err := os.Stat(filepath.Join(dir, "translations.pot")); !os.IsNotExist(err) {
		t.Fatalf("translations.pot written: %v", err)
	}
}

// TestTranslationFallbacks checks that every translation function shows a
// text without translation as written: tn and ctn pick singular or plural.
func TestTranslationFallbacks(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "nl.po"), []byte(dutch), 0o644); err != nil {
		t.Fatal(err)
	}

	tr, err := frontend.Translations(dir)
	if err != nil {
		t.Fatal(err)
	}

	funcs := tr.FuncMap()

	tn := funcs["tn"].(func(translator.Localizer, string, string, int, ...any) string)
	ctl := funcs["ctl"].(func(translator.Localizer, string, string, ...any) string)
	ctn := funcs["ctn"].(func(translator.Localizer, string, string, string, int, ...any) string)

	for _, code := range []string{"nl", "en"} {
		got := []string{
			tn(locale(code), "%d comment", "%d comments", 1, 1),
			tn(locale(code), "%d comment", "%d comments", 3, 3),
			tn(locale(code), "one", "many", 0),
			ctl(locale(code), "menu", "Open"),
			ctl(locale(code), "", "Read more %s", "now"),
			ctn(locale(code), "menu", "%d item", "%d items", 2, 2),
		}

		want := []string{"1 comment", "3 comments", "many", "Open", "Read more now", "2 items"}

		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: got %q, want %q", code, got, want)
			}
		}
	}
}

// TestTranslationsLanguageFallback checks that a locale without a catalogue
// of its own uses the one of its language, and never another language's.
func TestTranslationsLanguageFallback(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "nl.po"), []byte(dutch), 0o644); err != nil {
		t.Fatal(err)
	}

	tr, err := frontend.Translations(dir)
	if err != nil {
		t.Fatal(err)
	}

	for code, want := range map[string]string{"nl_BE": "Lees meer", "nl-NL": "Lees meer", "en": "Read more", "de": "Read more"} {
		if got := tr.Tl(locale(code), "Read more"); got != want {
			t.Errorf("%s: got %q, want %q", code, got, want)
		}
	}
}
