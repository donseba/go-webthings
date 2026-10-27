package admin

import (
	"net/http"
	"net/url"

	"github.com/donseba/go-webthings/locales"
)

// Tabs is the data of the shared "admin-tabs" template: a row of links that
// filter a list screen, such as its statuses. Show it with
// {{template "admin-tabs" .Tabs}}.
type Tabs struct {
	// Label names the row for screen readers: a translation key such as
	// "Status".
	Label string
	Items []Tab
}

// Tab is one link of Tabs.
type Tab struct {
	// Label is the text of the link: a translation key such as "Pending",
	// or a name chosen by the site owner, which shows as written.
	Label string
	// Value is the value of the filter parameter the tab selects; empty for
	// the tab without the parameter, such as "All". FilterTabs uses it.
	Value string
	URL   string
	// Active marks the tab of the list being shown.
	Active bool
	// Count is shown as a badge after the label when Counted is set, such
	// as the number of messages with the tab's status.
	Count   int
	Counted bool
}

// FilterTabs returns the tabs of the list at path that set its filter
// parameter name to each tab's Value; the tab whose Value is current is
// active. The links keep the request's other parameters, such as the search,
// and start again at the first page. Tabs that have a URL already keep it.
func FilterTabs(r *http.Request, label, path, name, current string, tabs ...Tab) Tabs {
	out := Tabs{Label: label, Items: make([]Tab, 0, len(tabs))}

	for _, tab := range tabs {
		if tab.URL == "" {
			tab.URL = FilterURL(r, path, name, tab.Value)
		}

		tab.Active = tab.Value == current
		out.Items = append(out.Items, tab)
	}

	return out
}

// FilterURL returns the address of the list at path with its parameter name
// set to value, or without it when value is empty. It keeps the request's
// other parameters, such as the search, and starts again at the first page.
func FilterURL(r *http.Request, path, name, value string) string {
	values := r.URL.Query()

	values.Del("page")
	values.Del(name)

	if value != "" {
		values.Set(name, value)
	}

	return withQuery(path, values)
}

// ContentLocale returns the content language the request asks for with its
// locale parameter, when config enables it, and config's default language
// otherwise. Screens that edit content in several languages use it to pick
// the language they show.
func ContentLocale(r *http.Request, config locales.Config) string {
	locale, err := config.Require(r.URL.Query().Get("locale"))
	if err != nil {
		return config.Default
	}

	return locale
}

// LocaleURL returns target with its locale parameter set to locale, keeping
// target's other parameters.
func LocaleURL(target, locale string) string {
	address, err := url.Parse(target)
	if err != nil {
		return target
	}

	values := address.Query()
	values.Set("locale", locale)
	address.RawQuery = values.Encode()

	return address.String()
}

// TranslationStatus reports how far the text in one content language is:
// complete when it can be published, started when some text exists.
type TranslationStatus func(locale string) (complete, started bool)

// LocaleTabs returns the language tabs of the page at target, one for each
// of codes, named by the language's own name and linking to target in that
// language (see LocaleURL); the tab of current is active. Show them with {{template "locale-tabs.gohtml" .Tabs}}.
//
// status tells each tab how far its translation is, for a screen that edits
// one item. A nil status gives overview tabs, which only name the language:
// a list as a whole has no single translation state.
func LocaleTabs(codes []string, current, target string, status TranslationStatus) []LocaleTab {
	tabs := make([]LocaleTab, 0, len(codes))

	// Languages that share a name, such as pt and pt-BR, show their codes.
	names := map[string]int{}
	for _, code := range codes {
		names[LanguageName(code)]++
	}

	for _, code := range codes {
		name := LanguageName(code)
		if names[name] > 1 {
			name = LanguageLabel(code)
		}

		tab := LocaleTab{
			Code:     code,
			Name:     name,
			URL:      LocaleURL(target, code),
			Active:   code == current,
			Overview: status == nil,
		}

		if status != nil {
			tab.Complete, tab.Started = status(code)
		}

		tabs = append(tabs, tab)
	}

	return tabs
}
