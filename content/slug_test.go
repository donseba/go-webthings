package content_test

import (
	"strings"
	"testing"

	"github.com/donseba/go-webthings/content"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Café opens again!":            "cafe-opens-again",
		"  Ærø & Straße — 2026  ":      "aero-strasse-2026",
		"Zoë's naïve résumé":           "zoe-s-naive-resume",
		"Ĳsselmeer, Łódź":              "ijsselmeer-lodz",
		"Новости":                      "",
		strings.Repeat("word ", 30):    strings.TrimSuffix(strings.Repeat("word-", 16), "-"),
		strings.Repeat("a", 90) + " b": strings.Repeat("a", 80),
	}

	for title, want := range cases {
		if got := content.Slug(title); got != want {
			t.Errorf("%q: got %q, want %q", title, got, want)
		}
	}
}

func TestTooLong(t *testing.T) {
	// Five characters in Cyrillic are ten bytes; the limit counts characters.
	if content.TooLong(5, "", "Новос") {
		t.Fatal("five characters are not longer than five")
	}

	if !content.TooLong(5, "ok", "Новости") {
		t.Fatal("seven characters are longer than five")
	}

	if content.TooLong(5) {
		t.Fatal("no values are not too long")
	}
}

func TestUniqueSlug(t *testing.T) {
	taken := map[string]bool{"news": true, "news-2": true}
	used := func(slug string) bool {
		return taken[slug]
	}

	if got := content.UniqueSlug("about", used); got != "about" {
		t.Fatalf("free slug: %q", got)
	}

	if got := content.UniqueSlug("news", used); got != "news-3" {
		t.Fatalf("taken slug: %q", got)
	}
}
