package frontend

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/donseba/go-webthings/pages"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// The site search looks through published pages and every module that offers
// a SearchFunc, in one language. Matching happens in Go over the published
// content, so it behaves the same on SQLite, Postgres and MySQL and ignores
// case and accents in every script ("ECOLE" finds "école"). That reads all
// published content per search, which suits the small and medium sites
// WebThings is made for; a site with many thousands of items would want a
// search index instead.
//
// The search screen is /<locale>/search?q=..., rendered with search.gohtml
// and a SearchView. Published pages come first: a page with the path
// /search replaces the screen (for example a page with its own design that
// uses {{search}}), and a site without search.gohtml answers 404 there.

// SearchPath is the path of the search screen below the language prefix.
const SearchPath = "/search"

// Search limits: results per search and the length of a query.
const (
	// DefaultSearchLimit is the number of results the search screen shows,
	// and what Frontend.Search returns for a limit of zero or less.
	DefaultSearchLimit = 20
	// MaxSearchLimit is the most results one search returns, whatever a
	// template asks for, so a search stays cheap.
	MaxSearchLimit = 100
	maxQueryLength = 200
	maxQueryTerms  = 10
)

// SearchResult is one hit of the site search.
type SearchResult struct {
	Title string
	// Summary is plain text around the first match.
	Summary string
	URL     string
	// Module is the module instance key, empty for pages.
	Module string
	// Type is the module type, for example "publishing", or "page".
	Type string
	// Score orders the results, highest first; see Query.Score.
	Score int
}

// SearchFunc searches a module instance's published content in locale for
// at most limit results. Modules parse query with ParseQuery and score and
// summarise with its methods, so all results rank alike.
type SearchFunc func(ctx context.Context, locale, query string, limit int) ([]SearchResult, error)

// SearchView is the data of search.gohtml.
type SearchView struct {
	Query   string
	Results []SearchResult
	// URL is the search screen in this language, for the form's action.
	URL string
	// Throttled is set when the visitor searched too often in a short time
	// (see Frontend.SetSearchThrottle): the search did not run, Results is
	// empty and the response has status 429, so the template shows a
	// message instead, also where a live search swaps the results.
	Throttled bool
}

// searchExample is the starting point of search.gohtml.
const searchExample = `{{/* @dot github.com/donseba/go-webthings/frontend.SearchView */}}
<main id="content">
    <form action="{{.URL}}" method="get" role="search">
        <input type="search" name="q" value="{{.Query}}" aria-label="Search">
        <button>Search</button>
    </form>

    {{if .Throttled}}
        <p>Too many searches in a short time. Wait a moment and try again.</p>
    {{else if .Query}}
        {{range .Results}}
            <article>
                <h2><a href="{{.URL}}">{{.Title}}</a></h2>
                {{with .Summary}}<p>{{.}}</p>{{end}}
            </article>
        {{else}}
            <p>Nothing found for “{{.Query}}”.</p>
        {{end}}
    {{end}}
</main>
`

// SearchTemplate declares search.gohtml, for checks and starting points.
func SearchTemplate() Template {
	return Template{Name: "search", Data: SearchView{}, Example: searchExample}
}

// Query is a parsed search: its words, folded for comparison.
type Query struct {
	terms  []string
	phrase string
}

// ParseQuery splits a search into words. Case and accents are folded away,
// so matching ignores them.
func ParseQuery(text string) Query {
	if utf8.RuneCountInString(text) > maxQueryLength {
		text = string([]rune(text)[:maxQueryLength])
	}

	folded, _ := fold(text)

	query := Query{phrase: strings.Join(strings.Fields(folded), " ")}

	for _, term := range strings.Fields(folded) {
		if !slices.Contains(query.terms, term) && len(query.terms) < maxQueryTerms {
			query.terms = append(query.terms, term)
		}
	}

	return query
}

// Empty reports whether the query has no words.
func (q Query) Empty() bool {
	return len(q.terms) == 0
}

// Score is 0 unless every word occurs in the title or in one of the texts.
// A word in the title counts 10, a word elsewhere 1, and the whole query in
// the title 5 more, so title matches rank above text matches.
func (q Query) Score(title string, texts ...string) int {
	if q.Empty() {
		return 0
	}

	return q.score(foldString(title), foldString(strings.Join(texts, " ")))
}

// score is Score of a title and text folded already (see foldString).
func (q Query) score(foldedTitle, foldedText string) int {
	if q.Empty() {
		return 0
	}

	score := 0

	for _, term := range q.terms {
		switch {
		case strings.Contains(foldedTitle, term):
			score += 10
		case strings.Contains(foldedText, term):
			score++
		default:
			return 0
		}
	}

	if len(q.terms) > 1 && strings.Contains(foldedTitle, q.phrase) {
		score += 5
	}

	return score
}

// Snippet is about length characters of text around the first matching
// word, cut at word boundaries, with "…" where text was left out. Without a
// match it is the start of text.
func (q Query) Snippet(text string, length int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)

	if len(runes) <= length {
		return text
	}

	match := 0
	folded, offsets := fold(text)

	for _, term := range q.terms {
		if at := strings.Index(folded, term); at >= 0 {
			match = utf8.RuneCountInString(text[:offsets[at]])

			break
		}
	}

	// Start a third of the length before the match, at the start of a word.
	from := max(0, min(match-length/3, len(runes)-length))
	for from > 0 && from < match && runes[from-1] != ' ' {
		from++
	}

	to := min(len(runes), from+length)
	for to < len(runes) && to > match && runes[to] != ' ' {
		to--
	}

	if to <= from {
		to = min(len(runes), from+length)
	}

	snippet := strings.TrimSpace(string(runes[from:to]))

	if from > 0 {
		snippet = "…" + snippet
	}

	if to < len(runes) {
		snippet += "…"
	}

	return snippet
}

// folded remembers how non-ASCII runes fold.
var folded sync.Map

// fold lowers case and drops accents, rune by rune, and returns for every
// byte of the result the offset of the rune it came from in text.
func fold(text string) (string, []int) {
	var b strings.Builder

	offsets := make([]int, 0, len(text))

	for i, r := range text {
		var part string

		switch {
		case r < utf8.RuneSelf:
			part = string(unicode.ToLower(r))
		default:
			part = foldRune(r)
		}

		for range len(part) {
			offsets = append(offsets, i)
		}

		b.WriteString(part)
	}

	return b.String(), offsets
}

// foldRune decomposes r, drops its accents (nonspacing marks) and folds case,
// so "É" becomes "e" and "ß" becomes "ss".
func foldRune(r rune) string {
	if known, ok := folded.Load(r); ok {
		return known.(string)
	}

	var b strings.Builder

	for _, part := range norm.NFD.String(string(r)) {
		if !unicode.Is(unicode.Mn, part) {
			b.WriteRune(part)
		}
	}

	result := cases.Fold().String(b.String())
	folded.Store(r, result)

	return result
}

// SetSearch gives the frontend the modules' search functions by instance key.
func (f *Frontend) SetSearch(searchers map[string]SearchFunc) {
	f.searchers = searchers
}

// Search finds published pages and module content in locale that contain
// every word of query, best matches first. A module that fails is logged and
// left out, so one module cannot break the search.
func (f *Frontend) Search(ctx context.Context, locale, query string, limit int) ([]SearchResult, error) {
	parsed := ParseQuery(query)
	if parsed.Empty() {
		return nil, nil
	}

	if limit <= 0 {
		limit = DefaultSearchLimit
	}

	limit = min(limit, MaxSearchLimit)

	results, summaries, err := f.searchPages(ctx, locale, parsed)
	if err != nil {
		return nil, err
	}

	for _, key := range slices.Sorted(maps.Keys(f.searchers)) {
		found, err := f.searchers[key](ctx, locale, query, limit)
		if err != nil {
			slog.ErrorContext(ctx, "search module", "module", key, "error", err)

			continue
		}

		for _, result := range found {
			if result.Module == "" {
				result.Module = key
			}

			if result.Type == "" {
				result.Type = f.types[key]
			}

			if result.URL != "" {
				results = append(results, result)
			}
		}
	}

	results = Rank(results, limit)

	// Only the pages that made it get a summary, the costly part of a hit.
	for i, result := range results {
		if summary, ok := summaries[result.URL]; ok && result.Type == "page" && result.Module == "" {
			results[i].Summary = summary()
		}
	}

	return results, nil
}

// Rank sorts results best first (by score, then title) and keeps at most
// limit of them.
func Rank(results []SearchResult, limit int) []SearchResult {
	slices.SortStableFunc(results, func(a, b SearchResult) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score)
		}

		return strings.Compare(a.Title, b.Title)
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results
}

// searchPages searches the published pages of locale. The results have no
// summary yet; summaries makes it by result URL.
func (f *Frontend) searchPages(
	ctx context.Context,
	locale string,
	query Query,
) (results []SearchResult, summaries map[string]func() string, err error) {
	routes, err := f.pages.Routes(ctx, locale)
	if err != nil {
		return nil, nil, err
	}

	summaries = map[string]func() string{}

	for _, route := range routes {
		page, err := f.pages.Published(ctx, route)
		if errors.Is(err, pages.ErrNotFound) {
			continue
		}

		if err != nil {
			return nil, nil, err
		}

		text, searched := f.pageTexts.get(page, locale)

		score := query.score(searched.title, searched.text)
		if score == 0 {
			continue
		}

		address := link(locale, route.Path, "")
		results = append(results, SearchResult{
			Title: text.Title,
			URL:   address,
			Type:  "page",
			Score: score,
		})

		summaries[address] = func() string {
			return Summary(query, 200, text.Description, searched.body)
		}
	}

	return results, summaries, nil
}

// Summary is a snippet (see Query.Snippet) of the first text that matches the
// query, or of the first text that is not empty.
func Summary(query Query, length int, texts ...string) string {
	first := ""

	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			continue
		}

		if first == "" {
			first = text
		}

		if query.Score("", text) > 0 {
			return query.Snippet(text, length)
		}
	}

	return query.Snippet(first, length)
}

// searchScreen serves /<locale>/search, when no page has that path.
func (f *Frontend) searchScreen(w http.ResponseWriter, r *http.Request, locale string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method_not_allowed", http.StatusMethodNotAllowed)

		return
	}

	r = r.WithContext(withMount(r.Context(), Mount{Locale: locale, Path: SearchPath, URL: r.URL.Path}))

	view, err := f.view(r, locale, "", nil)
	if err != nil {
		f.Fail(w, r, err)

		return
	}

	if !f.renderer(view.Theme.Name).Has("search") {
		f.NotFound(w, r)

		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) > maxQueryLength {
		query = string([]rune(query)[:maxQueryLength])
	}

	// A throttled search is still the search page, with a message instead
	// of results: a live search swaps it in like any other answer.
	throttled := query != "" && !f.searchAllowed(r)
	status := http.StatusOK

	var results []SearchResult

	if throttled {
		status = http.StatusTooManyRequests
	} else {
		results, err = f.Search(r.Context(), locale, query, DefaultSearchLimit)
		if err != nil {
			f.Fail(w, r, err)

			return
		}
	}

	// The language switch keeps the query.
	suffix := ""
	if query != "" {
		suffix = "?" + url.Values{"q": {query}}.Encode()
	}

	view = view.filled()
	view.Languages = slices.Clone(view.Languages)

	for i := range view.Languages {
		view.Languages[i].URL = link(view.Languages[i].Locale, SearchPath, "") + suffix
	}

	view.Title = query

	// A live search asks for the results at the address of the new query,
	// which the address bar shows: the regions follow, with the title.
	view.fragmentRegions = true

	view.Content = SearchView{
		Query:     query,
		Results:   results,
		URL:       link(locale, SearchPath, ""),
		Throttled: throttled,
	}

	// Result pages are endless variations of the site's content.
	w.Header().Set("X-Robots-Tag", "noindex")
	f.write(w, r, "search", view, status)
}

// search is the template function {{range search "query" 5}}: results in the
// language of the page being rendered.
func search(ctx context.Context, query string, limit int) []SearchResult {
	current, ok := renderingFrom(ctx)
	if !ok {
		return nil
	}

	results, err := current.frontend.Search(ctx, current.locale, query, limit)
	if err != nil {
		slog.ErrorContext(ctx, "template search", "error", err)
	}

	return results
}

// searchURL is the search screen in the language of the page being
// rendered, for a search form in the layout.
func searchURL(ctx context.Context) string {
	return link(Locale(ctx), SearchPath, "")
}
