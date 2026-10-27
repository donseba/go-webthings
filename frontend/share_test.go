package frontend

import (
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/site"

	partialerrors "github.com/donseba/go-partial/ext/errors"
)

func TestShareLinks(t *testing.T) {
	links := ShareLinks("https://example.test/en/news?id=1&x=a b", `Tom & Jerry's "news"?`)

	want := map[string]string{
		"facebook": "https://www.facebook.com/sharer/sharer.php?u=https%3A%2F%2Fexample.test%2Fen%2Fnews%3Fid%3D1%26x%3Da%20b",
		"x": "https://x.com/intent/tweet?text=Tom%20%26%20Jerry%27s%20%22news%22%3F" +
			"&url=https%3A%2F%2Fexample.test%2Fen%2Fnews%3Fid%3D1%26x%3Da%20b",
		"email": "mailto:?subject=Tom%20%26%20Jerry%27s%20%22news%22%3F" +
			"&body=https%3A%2F%2Fexample.test%2Fen%2Fnews%3Fid%3D1%26x%3Da%20b",
		"bluesky": "https://bsky.app/intent/compose?text=Tom%20%26%20Jerry%27s%20%22news%22%3F%20" +
			"https%3A%2F%2Fexample.test%2Fen%2Fnews%3Fid%3D1%26x%3Da%20b",
	}

	networks := []string{}

	for _, link := range links {
		networks = append(networks, link.Network)

		if link.Label == "" {
			t.Errorf("%s has no label", link.Network)
		}

		if url, ok := want[link.Network]; ok && link.URL != url {
			t.Errorf("%s:\nwant %s\ngot  %s", link.Network, url, link.URL)
		}
	}

	if strings.Join(networks, ",") != "facebook,x,linkedin,whatsapp,telegram,email,bluesky" {
		t.Fatalf("networks: %v", networks)
	}

	// Without a title, the message is only the address.
	if links := ShareLinks("https://example.test/", ""); links[3].URL != "https://wa.me/?text=https%3A%2F%2Fexample.test%2F" {
		t.Fatalf("whatsapp without title: %s", links[3].URL)
	}
}

func TestAbsolute(t *testing.T) {
	request := httptest.NewRequest("GET", "https://www.example.test/en/news/hello?page=2", nil)
	request.Header.Set("X-Forwarded-Proto", "https")

	configured := "https://example.com/"
	current := "/en/news/hello"

	cases := []struct {
		page string
		base string
		r    bool
		want string
	}{
		{"", "", true, "https://www.example.test/en/news/hello"},
		{"/en/about?x=1#top", "", true, "https://www.example.test/en/about?x=1#top"},
		{"#top", "", true, "https://www.example.test/en/news/hello#top"},
		{"?page=3", "", true, "https://www.example.test/en/news/hello?page=3"},
		{"?page=3#comments", configured, true, "https://example.com/en/news/hello?page=3#comments"},
		{"../about#a b", "", true, "https://www.example.test/en/about#a%20b"},
		{"#top", configured, false, "#top"},
		{"//cdn.example.test/a.png", "", true, "//cdn.example.test/a.png"},
		{"mailto:hello@example.test", "", true, "mailto:hello@example.test"},
		{"other", "", true, "https://www.example.test/en/news/other"},
		{"https://elsewhere.test/a", "", true, "https://elsewhere.test/a"},
		{"/en/about", configured, true, "https://example.com/en/about"},
		{"/en/about", configured, false, "https://example.com/en/about"},
		{"/en/about", "", false, "/en/about"},
	}

	for _, c := range cases {
		r := request
		path := current

		if !c.r {
			r = nil
			path = ""
		}

		if got := absolute(c.page, c.base, r, path); got != c.want {
			t.Errorf("%q: want %q, got %q", c.page, c.want, got)
		}
	}
}

// TestCoreTexts checks that every text core translates in Go is offered for
// extraction: the 404 title, the labels of share links and profiles, and the
// message and the status texts of the error page.
func TestCoreTexts(t *testing.T) {
	texts := CoreTexts()

	want := []string{
		notFoundTitle,
		partialerrors.SafeMessage,
		"Not Found",
		"Too Many Requests",
		"Internal Server Error",
		"Service Unavailable",
	}

	for _, link := range ShareLinks("https://example.test/", "Title") {
		want = append(want, link.Label)
	}

	for _, network := range site.Networks {
		want = append(want, site.NetworkName(network))
	}

	for _, text := range want {
		if !slices.Contains(texts, text) {
			t.Errorf("CoreTexts misses %q", text)
		}
	}
}
