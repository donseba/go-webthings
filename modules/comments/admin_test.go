package comments

import (
	"testing"

	webthings "github.com/donseba/go-webthings"
)

// TestAdminPage checks that a group of messages is named after the newest
// page in the admin's language, also for a regional locale, and after the
// newest page when no message was written in that language.
func TestAdminPage(t *testing.T) {
	tests := []struct {
		admin string
		paths []string
		want  string
	}{
		{"nl", []string{"/en/journal/celadon", "/nl/journaal/celadon", "/nl/journaal/oud"}, "/nl/journaal/celadon"},
		{"nl_NL", []string{"/en/journal/celadon", "/nl/journaal/celadon"}, "/nl/journaal/celadon"},
		{"en", []string{"/nl-be/journaal/celadon", "/en/journal/celadon"}, "/en/journal/celadon"},
		{"de", []string{"/en/journal/celadon", "/nl/journaal/celadon"}, "/en/journal/celadon"},
		{"en", []string{"/"}, "/"},
	}

	for _, test := range tests {
		s := screens{env: webthings.Env{AdminLanguage: test.admin}}

		if got := s.adminPage(test.paths); got != test.want {
			t.Errorf("admin %s, %q: got %q, want %q", test.admin, test.paths, got, test.want)
		}
	}
}
