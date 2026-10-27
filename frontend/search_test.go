package frontend_test

import (
	"strings"
	"testing"

	"github.com/donseba/go-webthings/frontend"
)

func TestQueryScore(t *testing.T) {
	query := frontend.ParseQuery("  Ecole  ÉTÉ ")

	// Case and accents are ignored, in any script.
	if score := query.Score("L'école d'été"); score != 20 {
		t.Fatalf("title match: %d", score)
	}

	// The whole query in the title counts more.
	if score := query.Score("Ecole été 2026"); score != 25 {
		t.Fatalf("phrase match: %d", score)
	}

	if score := query.Score("Nieuws", "De ECOLE is in de zomer (été) dicht"); score != 2 {
		t.Fatalf("text match: %d", score)
	}

	// Every word must occur.
	if score := query.Score("École", "winter"); score != 0 {
		t.Fatalf("partial match: %d", score)
	}

	// One word in the title outranks all words in the text.
	title := frontend.ParseQuery("straße").Score("Hauptstrasse")
	text := frontend.ParseQuery("straße").Score("Plein", "in der Strasse")

	if title <= text || text == 0 {
		t.Fatalf("title %d, text %d", title, text)
	}

	if !frontend.ParseQuery(" \t ").Empty() {
		t.Fatal("blank query is not empty")
	}

	if frontend.ParseQuery("ПРИВЕТ").Score("привет мир") == 0 {
		t.Fatal("cyrillic case not folded")
	}
}

func TestSnippet(t *testing.T) {
	text := strings.Repeat("filler words here ", 20) + "the Café opens at nine " + strings.Repeat("more text after ", 20)
	snippet := frontend.ParseQuery("cafe").Snippet(text, 60)

	if !strings.Contains(snippet, "Café opens") || !strings.HasPrefix(snippet, "…") || !strings.HasSuffix(snippet, "…") {
		t.Fatalf("snippet: %q", snippet)
	}

	if len([]rune(snippet)) > 62 {
		t.Fatalf("snippet too long: %d", len([]rune(snippet)))
	}

	if short := frontend.ParseQuery("x").Snippet("Short text", 60); short != "Short text" {
		t.Fatalf("short text: %q", short)
	}

	summary := frontend.Summary(frontend.ParseQuery("opens"), 200, "An excerpt", "the shop opens")
	if summary != "the shop opens" {
		t.Fatalf("summary of the matching text: %q", summary)
	}
}

func TestRank(t *testing.T) {
	results := frontend.Rank([]frontend.SearchResult{
		{Title: "B", Score: 1},
		{Title: "C", Score: 10},
		{Title: "A", Score: 1},
	}, 2)

	if len(results) != 2 || results[0].Title != "C" || results[1].Title != "A" {
		t.Fatalf("rank: %+v", results)
	}
}
