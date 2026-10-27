package admin

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// DefaultPageSize is how many rows a list screen shows per page when its
// ListPage sets no Size.
const DefaultPageSize = 50

// ListPage reads one page of a list screen, such as an inbox of messages or a
// list of subscribers, from the request's page and q parameters.
//
// Without a search the rows come from the database a page at a time. A
// search reads at most SearchLimit rows, keeps those whose Fields match
// (ignoring case and accents, like the site search) and pages the matches,
// so every list searches the same way and within the same bounds.
//
// Lists that hold all their rows in memory already set All instead of Load:
// they are filtered and paged in memory.
type ListPage[T any] struct {
	// Path is the address of the list, such as Routes.URL("index"). The pager's
	// links point there and keep the request's other parameters, such as a
	// filter tab and the search.
	Path string
	// Size is the number of rows per page; DefaultPageSize when zero.
	Size int
	// Load returns at most limit rows starting at offset, and whether more
	// rows follow. The arguments are in the order of TrashRecords.Load and
	// SQL's LIMIT and OFFSET.
	Load func(ctx context.Context, limit, offset int) (rows []T, more bool, err error)
	// All returns every row, for lists that are read whole; it is used when
	// Load is nil.
	All func(ctx context.Context) ([]T, error)
	// Fields returns the texts of a row that a search looks in, such as a
	// name, an e-mail address and a message. Without Fields a search
	// matches nothing.
	Fields func(T) []string
}

// List is one page of a list screen.
type List[T any] struct {
	// Rows are the rows of the page.
	Rows []T
	// Pager links to the pages before and after this one.
	Pager Pager
}

// Read returns the page of the list that r asks for.
func (p ListPage[T]) Read(r *http.Request) (List[T], error) {
	size := p.Size
	if size <= 0 {
		size = DefaultPageSize
	}

	ctx := r.Context()
	number := PageNumber(r)
	search := SearchOf(r)

	var (
		rows []T
		more bool
		err  error
	)

	switch {
	case p.Load == nil:
		rows, err = p.all(ctx)
		rows = p.filter(search, rows)
		rows, more = Paginate(rows, number, size)
	case search.Active():
		rows, _, err = p.Load(ctx, searchLimit, 0)
		rows = p.filter(search, rows)
		rows, more = Paginate(rows, number, size)
	default:
		rows, more, err = p.Load(ctx, size, (number-1)*size)
	}

	if err != nil {
		return List[T]{}, err
	}

	return List[T]{Rows: rows, Pager: NewPager(r, p.Path, number, more)}, nil
}

// all reads the whole list, or nothing when the list has no way to read.
func (p ListPage[T]) all(ctx context.Context) ([]T, error) {
	if p.All == nil {
		return nil, nil
	}

	return p.All(ctx)
}

// filter keeps the rows matching the search.
func (p ListPage[T]) filter(search Search, rows []T) []T {
	fields := p.Fields
	if fields == nil {
		fields = func(T) []string {
			return nil
		}
	}

	return Filter(search, rows, fields)
}

// Pager is the data of the shared "admin-pager" template: links to the
// previous and the next page of a list, empty at either end. Show it with
// {{template "admin-pager" .Pager}}; it shows nothing when the list fits on
// one page.
type Pager struct {
	// Page is the current page number, from 1.
	Page        int
	PreviousURL string
	NextURL     string
}

// NewPager returns the pager of page number of the list at path; more
// reports whether a next page exists. Its links keep the request's other
// parameters, such as a filter and the search, and change only the page.
// Lists that read their rows with ListPage get their pager from it; NewPager
// is for lists that page in another way.
func NewPager(r *http.Request, path string, number int, more bool) Pager {
	pager := Pager{Page: max(number, 1)}

	if pager.Page > 1 {
		pager.PreviousURL = PageURL(r, path, pager.Page-1)
	}

	if more {
		pager.NextURL = PageURL(r, path, pager.Page+1)
	}

	return pager
}

// PageNumber returns the page the request asks for with its page parameter;
// 1 when it asks for none or for an invalid one.
func PageNumber(r *http.Request) int {
	number, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil {
		return 1
	}

	return max(number, 1)
}

// PageURL returns the address of page number of the list at path, keeping
// the request's other parameters. The first page has no page parameter.
func PageURL(r *http.Request, path string, number int) string {
	values := r.URL.Query()

	values.Del("page")

	if number > 1 {
		values.Set("page", strconv.Itoa(number))
	}

	return withQuery(path, values)
}

// withQuery returns path with values as its query, or path alone without
// values.
func withQuery(path string, values url.Values) string {
	encoded := values.Encode()
	if encoded == "" {
		return path
	}

	return path + "?" + encoded
}
