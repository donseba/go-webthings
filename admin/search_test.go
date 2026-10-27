package admin_test

import (
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/donseba/go-webthings/admin"
)

func TestSearchMatches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		query  string
		fields []string
		want   bool
	}{
		{"", []string{"anything"}, true},
		{"cafe", []string{"Café de Paris"}, true},
		{"CAFÉ paris", []string{"Café", "de Paris"}, true},
		{"cafe london", []string{"Café de Paris"}, false},
		{"ada", nil, false},
	}

	for _, test := range cases {
		if got := admin.Matches(test.query, test.fields...); got != test.want {
			t.Errorf("Matches(%q, %q) = %v", test.query, test.fields, got)
		}
	}

	request := httptest.NewRequest("GET", "/admin/people?q=+Ada+&page=2", nil)
	if admin.Query(request) != "Ada" || !admin.SearchOf(request).Active() {
		t.Fatalf("query: %q", admin.Query(request))
	}

	if got := admin.KeepSearch(request, "/admin/people?filter=all"); got != "/admin/people?filter=all&q=Ada" {
		t.Fatalf("KeepSearch: %s", got)
	}

	page, more := admin.Paginate([]int{1, 2, 3, 4, 5}, 2, 2)
	if !slices.Equal(page, []int{3, 4}) || !more {
		t.Fatalf("Paginate: %v %v", page, more)
	}

	names := admin.Filter(admin.NewSearch("Ö"), []string{"Öz", "Al", "oscar"}, func(name string) []string {
		return []string{name}
	})
	if !slices.Equal(names, []string{"Öz", "oscar"}) {
		t.Fatalf("Filter: %v", names)
	}
}
