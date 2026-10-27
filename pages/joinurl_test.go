package pages

import "testing"

// TestJoinURL checks how MountURL joins a page path to the language prefix.
func TestJoinURL(t *testing.T) {
	cases := []struct {
		base, relative, want string
	}{
		{"/en/news", "hello", "/en/news/hello"},
		{"/en/news", "/category/events", "/en/news/category/events"},
		{"/en/", "hello", "/en/hello"},
		{"/en/", "", "/en/"},
		{"/en/news", "", "/en/news"},
		{"", "hello", ""},
	}

	for _, tc := range cases {
		if got := joinURL(tc.base, tc.relative); got != tc.want {
			t.Errorf("JoinURL(%q, %q): got %q, want %q", tc.base, tc.relative, got, tc.want)
		}
	}
}
