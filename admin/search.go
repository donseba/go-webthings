package admin

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/donseba/go-webthings/frontend"
)

// Search is the search of a list screen: the q parameter of the request,
// matched the way the site search matches, ignoring case and accents.
type Search struct {
	// Text is the search as typed.
	Text  string
	query frontend.Query
}

// SearchOf returns the search of the request's q parameter.
func SearchOf(r *http.Request) Search {
	return NewSearch(r.URL.Query().Get("q"))
}

// NewSearch parses a search text.
func NewSearch(text string) Search {
	text = strings.TrimSpace(text)

	return Search{Text: text, query: frontend.ParseQuery(text)}
}

// Query returns the search text of the request's q parameter.
func Query(r *http.Request) string {
	return SearchOf(r).Text
}

// Matches reports whether every word of query occurs in one of fields; an
// empty query matches everything. A list checking many rows parses the
// query once with SearchOf instead.
func Matches(query string, fields ...string) bool {
	return NewSearch(query).Matches(fields...)
}

// Active reports whether there is something to search for.
func (s Search) Active() bool {
	return !s.query.Empty()
}

// Matches reports whether every word of the search occurs in one of fields,
// such as a title, a name and an e-mail address. Without search everything
// matches.
func (s Search) Matches(fields ...string) bool {
	if !s.Active() {
		return true
	}

	if len(fields) == 0 {
		return false
	}

	return s.query.Score(fields[0], fields[1:]...) > 0
}

// Filter keeps the items whose fields match the search.
func Filter[T any](s Search, items []T, fields func(T) []string) []T {
	if !s.Active() {
		return items
	}

	return slices.DeleteFunc(slices.Clone(items), func(item T) bool {
		return !s.Matches(fields(item)...)
	})
}

// Paginate returns page number (from 1) of items with size items per page,
// and whether more pages follow; for lists that are searched in memory.
func Paginate[T any](items []T, number, size int) ([]T, bool) {
	number = max(number, 1)
	start := min((number-1)*size, len(items))
	end := min(start+size, len(items))

	return items[start:end], end < len(items)
}

// SearchBox is the data of the shared "admin-search" template: a search
// field for the header of a list screen. It searches the current page,
// keeping its other parameters (such as the language or a filter tab) but
// starting again at the first page. Every admin page has it as a
// go-partial @model, computed only when a template calls it; a list
// template declares it and passes it on:
//
//	{{/* @model Search github.com/donseba/go-webthings/admin.SearchBox */}}
//	{{template "admin-search" Search}}
type SearchBox struct {
	Text   string
	Action string
	// Keep are the other parameters of the page.
	Keep []SearchParam
	// ClearURL is the page without the search.
	ClearURL string
	// NewLabel, translated, and NewURL are the section's "new item" action,
	// such as "New post", on the overview of a section that has one
	// (Section.NewLabel) when the user may create; the empty list offers
	// it. Both are empty otherwise.
	NewLabel string
	NewURL   string
}

// newAction is the quick action of the section whose overview is the page
// at current, such as "New post", translated, with its address, for the
// empty list of the overview (SearchBox); empty for other pages, for a
// section without one and without the right to create.
func (a *Admin) newAction(r *http.Request, current url.URL) (label, address string) {
	for _, section := range a.sections {
		if section.NewLabel == "" || current.Path != a.sectionURL(section) {
			continue
		}

		if !a.Can(r, Permission{Section: section.PermissionID(), Action: Create}) {
			return "", ""
		}

		address = a.newURL(section)
		if address == "" {
			return "", ""
		}

		return a.T(r, section.NewLabel), address
	}

	return "", ""
}

// SearchParam is one parameter kept by a search.
type SearchParam struct {
	Name  string
	Value string
}

// searchBox builds the search box of the page at current.
func searchBox(current url.URL) SearchBox {
	values := current.Query()
	box := SearchBox{Text: strings.TrimSpace(values.Get("q")), Action: current.Path}

	values.Del("q")
	values.Del("page")

	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		for _, value := range values[name] {
			box.Keep = append(box.Keep, SearchParam{Name: name, Value: value})
		}
	}

	box.ClearURL = current.Path
	if encoded := values.Encode(); encoded != "" {
		box.ClearURL += "?" + encoded
	}

	return box
}

// KeepSearch adds the search of r to target, a link within the same list
// such as a filter tab or the next page, so the search stays.
func KeepSearch(r *http.Request, target string) string {
	text := Query(r)
	if text == "" {
		return target
	}

	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}

	return target + separator + url.Values{"q": {text}}.Encode()
}

// searchLimit is the most rows a paged list reads to search in memory.
const searchLimit = 5000

// SearchLimit is how many rows a paged list reads when it searches: the
// list filters them with Search.Matches and pages the matches with Paginate.
func SearchLimit() int {
	return searchLimit
}
