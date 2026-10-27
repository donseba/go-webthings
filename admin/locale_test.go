package admin_test

import (
	"testing"

	"github.com/donseba/go-webthings/admin"
)

// TestLocaleLanguage checks that the admin's html lang attribute is a BCP 47
// tag that keeps the region.
func TestLocaleLanguage(t *testing.T) {
	for locale, want := range map[admin.Locale]string{
		"nl":         "nl",
		"en":         "en",
		"pt_BR":      "pt-BR",
		"en-us":      "en-US",
		"de_DE":      "de-DE",
		"zh_Hant_TW": "zh-Hant-TW",
		"Klingon_XX": "klingon",
	} {
		if got := locale.Language(); got != want {
			t.Fatalf("%s: %s, want %s", locale, got, want)
		}
	}
}
