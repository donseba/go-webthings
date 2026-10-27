package frontend

import (
	"fmt"
	"net/http"
	"os"
	"slices"
	"sync"

	"github.com/donseba/go-webthings/site"

	partialerrors "github.com/donseba/go-partial/ext/errors"
	translator "github.com/donseba/go-translator/v2"
)

// notFoundTitle is the title of the 404 page, translated with the site's
// translations.
const notFoundTitle = "Not found"

// errorStatuses are the statuses whose text (http.StatusText) is the
// heading of go-partial's built-in error page, and the .Title of a theme's
// 500.gohtml, translated with the site's translations.
var errorStatuses = []int{
	http.StatusNotFound,
	http.StatusTooManyRequests,
	http.StatusInternalServerError,
	http.StatusServiceUnavailable,
}

// CoreTexts are the texts core translates in Go with the site's
// translations rather than in a template, so extraction finds them only
// through this list: the title of the 404 page, the names of social
// networks, the labels of share links and profiles (site.NetworkName), the
// title of feeds with the site's name ("%[1]s from %[2]s", see Feed), the
// message of a theme's 500.gohtml (go-partial's ext/errors SafeMessage,
// its .Message), which arrives translated in the page's language, and the
// status texts of the error page's heading (its .Title; see errorStatuses).
//
// The names of months and days and the date patterns of Format, which
// Format looks up with a context, are CalendarTexts.
func CoreTexts() []string {
	texts := []string{notFoundTitle, feedTitle, partialerrors.SafeMessage}

	for _, status := range errorStatuses {
		texts = append(texts, http.StatusText(status))
	}

	for _, network := range append(slices.Clone(site.Networks), "email") {
		name := site.NetworkName(network)
		if !slices.Contains(texts, name) {
			texts = append(texts, name)
		}
	}

	return texts
}

// Translations loads the site's own UI strings from <dir>/<locale>.po (for
// example translations/nl.po), for webthings.Config.Translations: templates
// use them as {{tl localizer "Read more"}}, with plurals {{tn localizer "%d
// comment" "%d comments" .Count .Count}} and contexts ctl and ctn
// (go-translator's functions), and core translates its own texts (see
// CoreTexts), go-form's labels and messages and the names of months and days
// with them. Text without a translation is shown as written (go-translator's
// ShowOriginal: for tn and ctn the singular when n is 1, the plural
// otherwise), so a newly enabled language works before its catalogue exists.
// A locale without a catalogue of its own uses the one of its language
// (go-translator's language fallback): nl_BE finds nl.po, and nl finds
// nl_NL.po. go-form's own messages, such as its validation errors, that a
// catalogue does not translate show in go-form's catalogue of the page's
// language.
func Translations(dir string) (*translator.Translator, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}

	// Texts are extracted from the templates with App.Translations (package
	// translations), so lookups while serving record nothing: a deploy never
	// writes to its catalogue folder, which may be read-only.
	tr := translator.New(
		translator.WithTranslationsDir(dir),
		translator.WithUntranslated(translator.ShowOriginal),
		translator.WithLanguageFallback(true),
	)

	if err := tr.LoadLanguages(os.DirFS(dir)); err != nil {
		return nil, fmt.Errorf("translations %s: %w", dir, err)
	}

	return tr, nil
}

// SetTranslations sets the site's translations (see Translations): the
// themes' templates, components and mail get its tl, tn, ctl and ctn, and
// core translates with it its own texts (CoreTexts), go-form's texts of
// visitor forms and the names of months and days (see Format). Without, every text shows as written. Call it
// before serving.
func (f *Frontend) SetTranslations(tr *translator.Translator) {
	f.translations = tr
	f.calendars.Forget()
	f.loadParts()

	if f.themes != nil {
		f.loadRenderers()
	}
}

// tr is the site's translator (SetTranslations), or without one go-translator
// without catalogues, which shows every text as written.
func (f *Frontend) tr() *translator.Translator {
	if f.translations != nil {
		return f.translations
	}

	return untranslated()
}

// untranslated is go-translator without catalogues, for templates and forms
// of a site without translations (Translations): modules' default templates
// use tl, so they work there too, every text shown as written.
var untranslated = sync.OnceValue(func() *translator.Translator {
	return translator.Untranslated()
})
