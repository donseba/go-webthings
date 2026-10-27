// Package translations is the "translations" command of a site: it writes
// the texts a site translates with tl, tn, ctl and ctn, found with
// go-translator's extract package, to a gettext template (translations.pot)
// and checks which texts each language's .po file still misses.
//
// The workflow for translators: extract the texts ("go run . translations"
// in a site), open a language's .po file in Poedit, choose Translation →
// Update from POT File, translate the new texts and save. "go run .
// translations check" lists what each language still misses.
//
// Extraction parses the templates instead of recording texts while pages are
// rendered, so it finds every text, also on pages nobody visited, and the
// running site never writes to its catalogues.
package translations

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/donseba/go-webthings/frontend"

	translator "github.com/donseba/go-translator/v2"
	"github.com/donseba/go-translator/v2/extract"
)

// ErrMissing is returned by the check when a language misses texts.
var ErrMissing = errors.New("translations missing")

// Usage describes the command's arguments.
const Usage = `usage: translations [check [--csv]]
  translations              write the templates' texts to translations/translations.pot
  translations check        list the texts each language's .po file misses
  translations check --csv  the same as CSV: file,context,text,plural,reference`

// potHeader is the header of the POT file Run writes. It has no creation
// date, so extracting unchanged templates gives an unchanged file.
var potHeader = extract.Header{
	Comment: "The texts of this website's templates, extracted by WebThings.\n" +
		"Do not edit: run the extraction again, then update each language's .po\n" +
		"file from this file (in Poedit: Translation → Update from POT File).",
	Generator: "WebThings",
}

// Run is the "translations" command of a site, for its main: without
// arguments it writes the texts of catalog (go-translator's extract package)
// to dir/translations.pot; with "check" it lists, per .po file in dir, the
// texts it misses (extract.Check), as CSV with "--csv", and returns
// ErrMissing when any language misses texts. Results go to stdout; the
// catalog's warnings, about texts that cannot be extracted, go to stderr.
func Run(catalog *extract.Catalog, dir string, args []string, stdout, stderr io.Writer) error {
	check, csvOutput, err := parseArgs(args)
	if err != nil {
		return err
	}

	for _, warning := range catalog.Warnings {
		if _, err := fmt.Fprintln(stderr, "warning:", warning.String()); err != nil {
			return err
		}
	}

	if !check {
		path := filepath.Join(dir, translator.DefaultPotFile)

		catalog.Header = potHeader

		if err := catalog.WritePOTFile(path); err != nil {
			return err
		}

		_, err = fmt.Fprintf(stdout, "wrote %s: %d texts from %d templates\n", path, len(catalog.Messages), catalog.Sources)

		return err
	}

	languages, err := extract.Check(catalog, os.DirFS(dir))
	if err != nil {
		return err
	}

	withoutBuiltIns(languages)

	if len(languages) == 0 {
		return fmt.Errorf("no .po files in %s", dir)
	}

	if csvOutput {
		err = writeCSV(stdout, languages)
	} else {
		err = writeReport(stdout, languages, len(catalog.Messages))
	}

	if err != nil {
		return err
	}

	var incomplete []string

	for _, language := range languages {
		if !language.Complete() {
			incomplete = append(incomplete, fmt.Sprintf("%s misses %d", language.File, len(language.Untranslated)))
		}
	}

	if len(incomplete) > 0 {
		return fmt.Errorf("%w: %s", ErrMissing, strings.Join(incomplete, ", "))
	}

	return nil
}

// withoutBuiltIns leaves the names of months and days and the date
// patterns (frontend.CalendarTexts, referenced as
// frontend.CalendarReference) out of the reports of the languages that have
// them built in (frontend.BuiltInCalendar), such as en and nl: their
// catalogues need not translate them, as the built-in ones show where they
// do not. An entry still replaces the built-in one. Other languages show
// English without them, so their catalogues still miss them.
func withoutBuiltIns(languages []extract.Report) {
	for i := range languages {
		report := &languages[i]
		if !frontend.BuiltInCalendar(report.Language) {
			continue
		}

		report.Missing = slices.DeleteFunc(report.Missing, calendarText)
		report.Empty = slices.DeleteFunc(report.Empty, calendarText)
		report.Untranslated = slices.DeleteFunc(report.Untranslated, calendarText)
	}
}

// calendarText reports whether message is one of frontend.CalendarTexts
// and used nowhere else.
func calendarText(message extract.Message) bool {
	return len(message.References) == 1 && message.References[0] == frontend.CalendarReference
}

func parseArgs(args []string) (check, csvOutput bool, err error) {
	if len(args) == 0 {
		return false, false, nil
	}

	if args[0] != "check" {
		return false, false, errors.New(Usage)
	}

	for _, arg := range args[1:] {
		if arg != "--csv" && arg != "-csv" {
			return false, false, errors.New(Usage)
		}

		csvOutput = true
	}

	return true, csvOutput, nil
}

func writeReport(w io.Writer, languages []extract.Report, total int) error {
	for _, language := range languages {
		if language.Complete() {
			if _, err := fmt.Fprintf(w, "%s: all %d texts translated\n", language.File, total); err != nil {
				return err
			}

			continue
		}

		if _, err := fmt.Fprintf(
			w,
			"%s: %d of %d texts missing\n",
			language.File,
			len(language.Untranslated),
			total,
		); err != nil {
			return err
		}

		for _, message := range language.Untranslated {
			if _, err := fmt.Fprintln(w, "  "+message.String()); err != nil {
				return err
			}
		}
	}

	return nil
}

func writeCSV(w io.Writer, languages []extract.Report) error {
	out := csv.NewWriter(w)

	if err := out.Write([]string{"file", "context", "text", "plural", "reference"}); err != nil {
		return err
	}

	for _, language := range languages {
		for _, message := range language.Untranslated {
			reference := ""
			if len(message.References) > 0 {
				reference = message.References[0]
			}

			row := []string{language.File, message.Context, message.ID, message.Plural, reference}

			if err := out.Write(row); err != nil {
				return err
			}
		}
	}

	out.Flush()

	return out.Error()
}
