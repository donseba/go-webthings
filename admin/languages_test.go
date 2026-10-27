package admin

import "testing"

func TestLanguageLabels(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"nl":    "Nederlands (nl)",
		"DE":    "Deutsch (DE)",
		"pt-BR": "Português (pt-BR)",
		"sr":    "Српски (sr)",
		"x-abc": "x-abc",
		"tlh":   "tlh",
	}

	for code, want := range cases {
		if got := LanguageLabel(code); got != want {
			t.Errorf("LanguageLabel(%q) = %q, want %q", code, got, want)
		}
	}
}
