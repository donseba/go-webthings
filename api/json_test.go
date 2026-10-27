package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPaginate(t *testing.T) {
	t.Parallel()

	items := make([]int, 45)
	for i := range items {
		items[i] = i
	}

	for _, check := range []struct {
		query  string
		first  int
		count  int
		limit  int
		offset int
		next   string
	}{
		{"", 0, DefaultLimit, DefaultLimit, 0, "http://example.test/list?offset=20"},
		{"?limit=10&offset=40", 40, 5, 10, 40, ""},
		{"?limit=abc&offset=-5", 0, DefaultLimit, DefaultLimit, 0, "http://example.test/list?limit=abc&offset=20"},
		{"?limit=1000&locale=nl", 0, 45, MaxLimit, 0, ""},
		{"?limit=0&offset=44", 44, 1, 1, 44, ""},
		{"?offset=99", 0, 0, DefaultLimit, 99, ""},
	} {
		r := httptest.NewRequest(http.MethodGet, "http://example.test/list"+check.query, nil)
		page := Paginate(r, items)

		next := ""
		if page.Next != nil {
			next = *page.Next
		}

		if page.Total != 45 || page.Limit != check.limit || page.Offset != check.offset || len(page.Items) != check.count ||
			next != check.next {
			t.Errorf("%q: %+v, next %q", check.query, page, next)
		}

		if check.count > 0 && page.Items[0] != check.first {
			t.Errorf("%q: first item %d, want %d", check.query, page.Items[0], check.first)
		}
	}

	// An empty list is [] in JSON, not null.
	if page := Paginate(httptest.NewRequest(http.MethodGet, "/list", nil), []string(nil)); page.Items == nil {
		t.Error("an empty page has nil items")
	}
}
