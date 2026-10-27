package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/media"

	"github.com/donseba/go-partial/exp/localization"
)

// The codes of errors (Error.Code), which stay the same while their
// messages are translated.
const (
	// CodeNotFound: no published content at the address, in the language
	// asked for, or no endpoint.
	CodeNotFound = "not_found"
	// CodeUnknownLocale: the site does not offer the language asked for.
	CodeUnknownLocale = "unknown_locale"
	// CodeRateLimited: the client made more requests than Config.RateLimit.
	CodeRateLimited = "rate_limited"
	// CodeInternal: the server failed; the log has the details.
	CodeInternal = "internal_error"
)

// ErrorResponse is the body of every error: {"error": {...}}.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody is an error: the response's HTTP status, a stable code (see
// CodeNotFound and the others) and the status text in the request's
// language, for people.
type ErrorBody struct {
	Status  int    `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Image is an image of the media library as pages show it (media.Image): a
// copy at most media.ImageWidth wide, a srcset of smaller copies, its size,
// the editors' alt text in the request's language and the uploaded
// original, every address absolute. A field of type *Image is null without
// an image.
type Image struct {
	URL         string `json:"url"`
	Srcset      string `json:"srcset"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Alt         string `json:"alt"`
	OriginalURL string `json:"original_url"`
}

// List is a page of a list: Items from Offset, at most Limit of them, of
// Total in all, and Next, the absolute address of the next page, null on
// the last.
type List[T any] struct {
	Items  []T     `json:"items"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
	Next   *string `json:"next"`
}

// Write answers a request with v as JSON, status 200. The app gives the
// response its ETag and Cache-Control and keeps it in the output cache.
func Write(w http.ResponseWriter, r *http.Request, v any) {
	writeJSON(w, r, http.StatusOK, v)
}

// writeJSON answers with v as JSON and status.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		Fail(w, r, err)

		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// Error answers a request with an error: status, code and the status text
// in the request's language (see ErrorBody).
func Error(w http.ResponseWriter, r *http.Request, status int, code string) {
	message := http.StatusText(status)

	if current, ok := scopeOf(r); ok && current.api.options.Translations != nil {
		message = current.api.options.Translations.Tl(localization.New(current.locale), message)
	}

	// Errors change with the content: a 404 may be a page tomorrow.
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, r, status, ErrorResponse{Error: ErrorBody{Status: status, Code: code, Message: message}})
}

// NotFound answers 404 not_found: no published content here, or no
// endpoint.
func NotFound(w http.ResponseWriter, r *http.Request) {
	Error(w, r, http.StatusNotFound, CodeNotFound)
}

// Fail logs err and answers 500 internal_error.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "content api", "path", r.URL.Path, "error", err)
	Error(w, r, http.StatusInternalServerError, CodeInternal)
}

// ImageOf is the image asset id as an Image (see Image), nil for an empty
// id, an unknown asset or a request outside the API.
func ImageOf(r *http.Request, id string) *Image {
	current, ok := scopeOf(r)
	if id == "" || !ok {
		return nil
	}

	library := current.api.options.Media
	shown := library.Image(r.Context(), id, media.ImageWidth)

	if shown.URL == "" {
		return nil
	}

	return &Image{
		URL:         URL(r, shown.URL),
		Srcset:      markdown.AbsoluteSrcset(URL(r, "/"), shown.Srcset),
		Width:       shown.Width,
		Height:      shown.Height,
		Alt:         library.AltText(r.Context(), id, current.locale),
		OriginalURL: URL(r, shown.OriginalURL),
	}
}

// HTML renders editor content, which is Markdown, as HTML for readers away
// from the site: as the site shows it, in the request's language, with
// every address of the site absolute, images with their srcset, size and
// alt text included.
func HTML(r *http.Request, source string) string {
	current, ok := scopeOf(r)
	if source == "" || !ok {
		return ""
	}

	// Paths of the site, such as links to pages and the media of Media and
	// Images, get the site's address in front; relative links lead from the
	// home page of the language, as for content without an address of its
	// own (see package links).
	base := URL(r, "/"+current.locale+"/")
	options := current.api.options.Frontend.MarkdownOptions(r.Context(), current.locale, base, false)

	return string(markdown.Render(source, options))
}

// Paginate returns the page of items a request asks for with its limit
// and offset parameters (see List): DefaultLimit items from the start when
// it names none, at most MaxLimit. A limit or offset that is not a whole
// number, or is out of range, is the default or the nearest one allowed.
func Paginate[T any](r *http.Request, items []T) List[T] {
	query := r.URL.Query()
	limit := parameter(query.Get("limit"), DefaultLimit)
	limit = max(1, min(limit, MaxLimit))
	offset := max(0, parameter(query.Get("offset"), 0))

	page := List[T]{Items: []T{}, Total: len(items), Limit: limit, Offset: offset}

	if offset < len(items) {
		page.Items = items[offset:min(len(items), offset+limit)]
	}

	if offset+limit < len(items) {
		query.Set("offset", strconv.Itoa(offset+limit))
		next := URL(r, r.URL.Path+"?"+query.Encode())
		page.Next = &next
	}

	return page
}

// parameter is the whole number of a query parameter, or fallback.
func parameter(value string, fallback int) int {
	number, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return number
}

// Optional is value as a field that is null when empty, such as a URL
// of content on no page.
func Optional(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}
