package newsletter

import "testing"

// TestFooterHTML checks where the unsubscribe link goes in the HTML of a
// mailing: in the footer's last paragraph, or after a footer that ends in
// something else.
func TestFooterHTML(t *testing.T) {
	link := `<a href="https://example.test/u">https://example.test/u</a>`

	cases := map[string]string{
		"<p>Unsubscribe:</p>\n":                 "<p>Unsubscribe: " + link + "</p>\n",
		"<p>Thanks.</p>\n<p>Unsubscribe:</p>\n": "<p>Thanks.</p>\n<p>Unsubscribe: " + link + "</p>\n",
		"<ul>\n<li>One</li>\n</ul>\n":           "<ul>\n<li>One</li>\n</ul>\n<p>" + link + "</p>\n",
		"":                                      "<p>" + link + "</p>\n",
	}

	for footer, want := range cases {
		if got := footerHTML(footer, link); got != want {
			t.Errorf("footerHTML(%q) = %q, want %q", footer, got, want)
		}
	}
}
