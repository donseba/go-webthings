package translations_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/translations"

	translator "github.com/donseba/go-translator/v2"
	"github.com/donseba/go-translator/v2/extract"
)

const layout = `{{define "header"}}<h1>{{tl localizer "Welcome"}}</h1>{{end}}
<p>{{tl localizer "Hello, %s" .Name}}</p>
{{if .Items}}
	{{range .Items}}{{tn localizer "%d item" "%d items" .Count .Count}}{{end}}
{{else}}
	{{with .Empty}}{{ctl localizer "button" "Open"}}{{end}}
{{end}}
{{printf "%s!" (tl localizer "Nested")}}
{{template "header" .}}
{{/* {{tl localizer "Commented out"}} */}}
{{tl localizer .Dynamic}}
{{"Piped" | tl localizer}}
{{ctn localizer "inbox" "%d message" "%d messages" .N .N}}
{{tl localizer "Quote \" and\nnewline"}}
{{unknownHelper "not a translation"}}
`

const page = `{{tl localizer "Welcome"}}
{{ctl localizer "door" "Open"}}
{{tl localizer "%d item"}}
`

// catalog extracts the texts of the layout and the page.
func catalog(t *testing.T) *extract.Catalog {
	t.Helper()

	catalog, err := extract.Extract(
		extract.Source{Name: "default/layout.gohtml", Text: layout},
		extract.Source{Name: "default/page.gohtml", Text: page},
	)
	if err != nil {
		t.Fatal(err)
	}

	return catalog
}

const dutch = `msgid ""
msgstr ""
"Language: nl\n"
"Content-Type: text/plain; charset=UTF-8\n"
"Plural-Forms: nplurals=2; plural=(n != 1);\n"

msgid "Welcome"
msgstr "Welkom"

msgid "Hello, %s"
msgstr ""

msgid "%d item"
msgid_plural "%d items"
msgstr[0] "%d ding"
msgstr[1] ""

msgctxt "button"
msgid "Open"
msgstr "Openen"

msgid "Nested"
msgstr "Genest"

msgctxt "inbox"
msgid "%d message"
msgid_plural "%d messages"
msgstr[0] "%d bericht"
msgstr[1] "%d berichten"

msgid "Quote \" and\nnewline"
msgstr "Citaat \" en\nregel"

msgid "No longer used"
msgstr "Niet meer gebruikt"
`

func translationsDir(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func TestRunWritesPOT(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "translations")

	var stdout, stderr bytes.Buffer

	if err := translations.Run(catalog(t), dir, nil, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}

	pot, err := os.ReadFile(filepath.Join(dir, translator.DefaultPotFile))
	if err != nil {
		t.Fatal(err)
	}

	header := "# The texts of this website's templates, extracted by WebThings.\n" +
		"# Do not edit: run the extraction again, then update each language's .po\n" +
		"# file from this file (in Poedit: Translation → Update from POT File).\n" +
		"msgid \"\"\nmsgstr \"\"\n\"Project-Id-Version: \\n\"\n"

	if !strings.HasPrefix(string(pot), header) || !strings.Contains(string(pot), "\"X-Generator: WebThings\\n\"\n") {
		t.Errorf("POT file has another header:\n%s", pot)
	}

	if !strings.Contains(string(pot), "msgid \"Welcome\"") {
		t.Errorf("POT file misses Welcome:\n%s", pot)
	}

	if !strings.Contains(stdout.String(), "8 texts from 2 templates") {
		t.Errorf("stdout = %q", stdout.String())
	}

	if strings.Count(stderr.String(), "warning: ") != 2 {
		t.Errorf("stderr = %q, want two warnings", stderr.String())
	}

	// Extracting again gives the same file, so it only changes with the templates.
	if err := translations.Run(catalog(t), dir, nil, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}

	again, err := os.ReadFile(filepath.Join(dir, translator.DefaultPotFile))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(pot, again) {
		t.Error("a second extraction changed the POT file")
	}
}

func TestRunCheck(t *testing.T) {
	dir := translationsDir(t, map[string]string{"nl.po": dutch})

	var stdout, stderr bytes.Buffer

	err := translations.Run(catalog(t), dir, []string{"check"}, &stdout, &stderr)
	if !errors.Is(err, translations.ErrMissing) || !strings.Contains(err.Error(), "nl.po misses 3") {
		t.Fatalf("error = %v, want ErrMissing for nl.po", err)
	}

	report := stdout.String()

	for _, want := range []string{
		"nl.po: 3 of 8 texts missing\n",
		"  \"Hello, %s\" (default/layout.gohtml:2)\n",
		"  \"%d item\" / \"%d items\" (default/layout.gohtml:4)\n",
		"  [door] \"Open\" (default/page.gohtml:2)\n",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report misses %q:\n%s", want, report)
		}
	}

	stdout.Reset()

	err = translations.Run(catalog(t), dir, []string{"check", "--csv"}, &stdout, &stderr)
	if !errors.Is(err, translations.ErrMissing) {
		t.Fatalf("error = %v, want ErrMissing", err)
	}

	csv := stdout.String()

	for _, want := range []string{
		"file,context,text,plural,reference\n",
		"nl.po,,\"Hello, %s\",,default/layout.gohtml:2\n",
		"nl.po,,%d item,%d items,default/layout.gohtml:4\n",
		"nl.po,door,Open,,default/page.gohtml:2\n",
	} {
		if !strings.Contains(csv, want) {
			t.Errorf("CSV misses %q:\n%s", want, csv)
		}
	}
}

func TestRunCheckComplete(t *testing.T) {
	dir := translationsDir(t, map[string]string{"nl.po": dutch})
	complete := &extract.Catalog{}
	complete.Add(extract.Texts("a.gohtml:1", "Welcome")...)

	var stdout, stderr bytes.Buffer

	if err := translations.Run(complete, dir, []string{"check"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}

	if stdout.String() != "nl.po: all 1 texts translated\n" {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestRunArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer

	for _, args := range [][]string{{"chekc"}, {"check", "--xml"}} {
		err := translations.Run(&extract.Catalog{}, t.TempDir(), args, &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("Run(%q) error = %v, want usage", args, err)
		}
	}

	err := translations.Run(&extract.Catalog{}, t.TempDir(), []string{"check"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "no .po files") {
		t.Errorf("check without .po files: error = %v", err)
	}
}

// TestRunCheckCalendar checks that the names of months and days and the
// date patterns are not missing from the catalogue of a language that has
// them built in, en or nl, and are from that of another language, which
// shows English without them.
func TestRunCheckCalendar(t *testing.T) {
	header := func(language string) string {
		return "msgid \"\"\nmsgstr \"\"\n\"Language: " + language + "\\n\"\n\"Content-Type: text/plain; charset=UTF-8\\n\"\n\n" +
			"msgid \"Welcome\"\nmsgstr \"W\"\n"
	}

	dir := translationsDir(t, map[string]string{
		"en.po": header("en"),
		"nl.po": header("nl_NL"),
		"de.po": header("de"),
	})

	catalog := &extract.Catalog{}
	catalog.Add(extract.Texts("a.gohtml:1", "Welcome")...)

	calendar := frontend.CalendarTexts()
	for i := range calendar {
		calendar[i].References = []string{frontend.CalendarReference}
	}

	catalog.Add(calendar...)

	var stdout, stderr bytes.Buffer

	err := translations.Run(catalog, dir, []string{"check"}, &stdout, &stderr)
	if !errors.Is(err, translations.ErrMissing) || err.Error() != "translations missing: de.po misses 45" {
		t.Fatalf("error = %v, want de.po only", err)
	}

	for _, want := range []string{"en.po: all 46 texts translated\n", "nl.po: all 46 texts translated\n", "de.po: 45 of 46 texts missing\n"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("report misses %q:\n%s", want, stdout.String())
		}
	}
}
