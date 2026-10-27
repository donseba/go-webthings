package admin_test

import (
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/locales"
)

func TestFilterTabs(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest("GET", "/admin/inbox?status=archived&q=offer&page=3", nil)

	tabs := admin.FilterTabs(r, "Status", "/admin/inbox", "status", "archived",
		admin.Tab{Label: "New", Value: "new", Count: 4, Counted: true},
		admin.Tab{Label: "Archived", Value: "archived"},
		admin.Tab{Label: "All"},
		admin.Tab{Label: "Elsewhere", URL: "/admin/other"},
	)

	if tabs.Label != "Status" || len(tabs.Items) != 4 {
		t.Fatalf("tabs: %+v", tabs)
	}

	want := []struct {
		url    string
		active bool
	}{
		{"/admin/inbox?q=offer&status=new", false},
		{"/admin/inbox?q=offer&status=archived", true},
		{"/admin/inbox?q=offer", false},
		{"/admin/other", false},
	}

	for i, tab := range tabs.Items {
		if tab.URL != want[i].url || tab.Active != want[i].active {
			t.Errorf("tab %d: %+v, want %+v", i, tab, want[i])
		}
	}

	if !tabs.Items[0].Counted || tabs.Items[0].Count != 4 {
		t.Fatalf("count: %+v", tabs.Items[0])
	}
}

func TestContentLocale(t *testing.T) {
	t.Parallel()

	config := locales.Config{Default: "en", Enabled: []string{"en", "nl"}}

	for query, want := range map[string]string{"": "en", "locale=nl": "nl", "locale=de": "en", "locale=%2F": "en"} {
		if got := admin.ContentLocale(httptest.NewRequest("GET", "/admin/pages?"+query, nil), config); got != want {
			t.Errorf("%q: %q, want %q", query, got, want)
		}
	}
}

func TestLocaleURL(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"/admin/pages":                       "/admin/pages?locale=nl",
		"/admin/news/1?locale=en":            "/admin/news/1?locale=nl",
		"/admin/news/1?locale=en&revision=7": "/admin/news/1?locale=nl&revision=7",
	}

	for target, want := range cases {
		if got := admin.LocaleURL(target, "nl"); got != want {
			t.Errorf("%q: %q, want %q", target, got, want)
		}
	}
}

func TestLocaleTabs(t *testing.T) {
	t.Parallel()

	overview := admin.LocaleTabs([]string{"en", "nl"}, "nl", "/admin/pages", nil)
	if len(overview) != 2 || !overview[0].Overview || overview[0].Active || !overview[1].Active ||
		overview[1].URL != "/admin/pages?locale=nl" {
		t.Fatalf("overview tabs: %+v", overview)
	}

	status := func(code string) (bool, bool) {
		return code == "en", code != "fr"
	}

	tabs := admin.LocaleTabs([]string{"en", "nl", "fr"}, "en", "/admin/news/1?locale=en", status)
	if tabs[0].Overview || !tabs[0].Complete || tabs[1].Complete || !tabs[1].Started || tabs[2].Started {
		t.Fatalf("item tabs: %+v", tabs)
	}

	// The tabs name the languages in their own language, with the code
	// only where two languages share a name.
	named := admin.LocaleTabs([]string{"nl", "pt", "pt-BR", "xx"}, "nl", "/admin/pages", nil)

	var names []string
	for _, tab := range named {
		names = append(names, tab.Name)
	}

	if want := []string{"Nederlands", "Português (pt)", "Português (pt-BR)", "xx"}; !slices.Equal(names, want) {
		t.Fatalf("tab names %q, want %q", names, want)
	}
}
