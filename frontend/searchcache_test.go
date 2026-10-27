package frontend

import (
	"testing"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/pages"
)

// TestFoldString checks that foldString folds as fold does.
func TestFoldString(t *testing.T) {
	for _, text := range []string{"", "Café ÉCOLE", "Straße", "日本語 Ωmega", "bad \xff byte", "MiXeD 123"} {
		want, _ := fold(text)

		if got := foldString(text); got != want {
			t.Fatalf("foldString(%q) = %q, want %q", text, got, want)
		}
	}
}

// TestPageTexts checks that the search texts of a page revision are made
// once per revision and language, and score as Query.Score does.
func TestPageTexts(t *testing.T) {
	var cache pageTexts

	page := pages.Page{
		Translations: []content.Translation[pages.Text]{
			{Locale: "en", Text: pages.Text{Title: "Café", Description: "Opening", Body: "We open at **nine**."}},
			{Locale: "nl", Text: pages.Text{Title: "Koffie", Body: "Open om **negen**."}},
		},
	}
	page.Revision.ID = "r1"

	text, searched := cache.get(page, "en")
	if text.Title != "Café" || searched.body != "We open at nine." {
		t.Fatalf("texts: %+v %+v", text, searched)
	}

	query := ParseQuery("cafe nine")
	got := query.score(searched.title, searched.text)
	want := query.Score(text.Title, text.Description, searched.body)

	if got != want || got == 0 {
		t.Fatalf("score %d, want %d", got, want)
	}

	if _, dutch := cache.get(page, "nl"); dutch.body != "Open om negen." {
		t.Fatalf("Dutch: %+v", dutch)
	}

	// The revision's texts are kept: a changed page is another revision.
	page.Translations[0].Text.Body = "changed"

	if _, again := cache.get(page, "en"); again.body != "We open at nine." {
		t.Fatalf("kept texts: %+v", again)
	}

	page.Revision.ID = "r2"

	if _, next := cache.get(page, "en"); next.body != "changed" {
		t.Fatalf("next revision: %+v", next)
	}
}
