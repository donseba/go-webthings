package admin_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"

	"github.com/donseba/go-webthings/admin"
)

// numbers are rows 1 to count, as a database would return them.
func numbers(count int) []int {
	rows := make([]int, count)
	for i := range rows {
		rows[i] = i + 1
	}

	return rows
}

// fields makes a number searchable by its digits.
func fields(n int) []string {
	return []string{strconv.Itoa(n)}
}

func TestListPageLoadsPagesInSQL(t *testing.T) {
	t.Parallel()

	all := numbers(25)

	var asked [][2]int

	list := admin.ListPage[int]{
		Path: "/admin/things",
		Size: 10,
		Load: func(_ context.Context, limit, offset int) ([]int, bool, error) {
			asked = append(asked, [2]int{offset, limit})
			end := min(offset+limit, len(all))

			return all[offset:end], end < len(all), nil
		},
		Fields: fields,
	}

	page, err := list.Read(httptest.NewRequest("GET", "/admin/things?filter=new&page=2", nil))
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(page.Rows, numbers(20)[10:]) || asked[0] != [2]int{10, 10} {
		t.Fatalf("page 2: %v, asked %v", page.Rows, asked)
	}

	if page.Pager.Page != 2 ||
		page.Pager.PreviousURL != "/admin/things?filter=new" ||
		page.Pager.NextURL != "/admin/things?filter=new&page=3" {
		t.Fatalf("pager: %+v", page.Pager)
	}

	last, _ := list.Read(httptest.NewRequest("GET", "/admin/things?page=3", nil))
	if len(last.Rows) != 5 || last.Pager.NextURL != "" || last.Pager.PreviousURL != "/admin/things?page=2" {
		t.Fatalf("last page: %v %+v", last.Rows, last.Pager)
	}
}

func TestListPageSearchesWithinTheLimit(t *testing.T) {
	t.Parallel()

	var limits []int

	list := admin.ListPage[int]{
		Path: "/admin/things",
		Size: 2,
		Load: func(_ context.Context, limit, offset int) ([]int, bool, error) {
			limits = append(limits, offset, limit)

			return numbers(30), false, nil
		},
		Fields: fields,
	}

	// 1, 10…19 and 21 contain a 1: twelve matches, paged by two.
	page, err := list.Read(httptest.NewRequest("GET", "/admin/things?q=1&page=2", nil))
	if err != nil {
		t.Fatal(err)
	}

	if limits[0] != 0 || limits[1] != admin.SearchLimit() {
		t.Fatalf("a search reads from 0 to the search limit, not %v", limits)
	}

	if !slices.Equal(page.Rows, []int{11, 12}) {
		t.Fatalf("matches on page 2: %v", page.Rows)
	}

	if page.Pager.PreviousURL != "/admin/things?q=1" || page.Pager.NextURL != "/admin/things?page=3&q=1" {
		t.Fatalf("pager keeps the search: %+v", page.Pager)
	}
}

func TestListPageReadsWholeLists(t *testing.T) {
	t.Parallel()

	list := admin.ListPage[int]{
		Path: "/admin/things",
		All: func(context.Context) ([]int, error) {
			return numbers(60), nil
		},
		Fields: fields,
	}

	page, err := list.Read(httptest.NewRequest("GET", "/admin/things", nil))
	if err != nil {
		t.Fatal(err)
	}

	if len(page.Rows) != admin.DefaultPageSize || page.Pager.NextURL != "/admin/things?page=2" {
		t.Fatalf("first page of a whole list: %d rows, %+v", len(page.Rows), page.Pager)
	}

	searched, _ := list.Read(httptest.NewRequest("GET", "/admin/things?q=5", nil))
	if !slices.Equal(searched.Rows, []int{5, 15, 25, 35, 45, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59}) {
		t.Fatalf("search of a whole list: %v", searched.Rows)
	}

	failing := admin.ListPage[int]{
		Load: func(context.Context, int, int) ([]int, bool, error) {
			return nil, false, errors.New("broken")
		},
	}

	if _, err := failing.Read(httptest.NewRequest("GET", "/", nil)); err == nil {
		t.Fatal("a failing load is reported")
	}
}

func TestPageNumber(t *testing.T) {
	t.Parallel()

	for query, want := range map[string]int{"": 1, "page=3": 3, "page=0": 1, "page=-2": 1, "page=x": 1} {
		if got := admin.PageNumber(httptest.NewRequest("GET", "/?"+query, nil)); got != want {
			t.Errorf("%q: page %d, want %d", query, got, want)
		}
	}
}

func TestNewPager(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest("GET", "/admin/trash?section=news&q=old&page=4", nil)

	pager := admin.NewPager(r, "/admin/trash", 1, false)
	if pager.PreviousURL != "" || pager.NextURL != "" {
		t.Fatalf("a single page has no links: %+v", pager)
	}

	pager = admin.NewPager(r, "/admin/trash", 2, true)
	if pager.PreviousURL != "/admin/trash?q=old&section=news" || pager.NextURL != "/admin/trash?page=3&q=old&section=news" {
		t.Fatalf("pager: %+v", pager)
	}
}
