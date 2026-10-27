package admin

import (
	"strings"

	"golang.org/x/text/language"
)

// Locale is the admin UI language as a locale, for example "nl" or "pt_BR".
// Catalogues are named by locale, such as nl_NL.po; a lookup in a locale
// without a catalogue of its own uses one of the same language (see
// loadTranslations).
type Locale string

// newLocale is the locale of a language code such as "nl", "en" or "pt_BR":
// English without one.
func newLocale(language string) Locale {
	language = strings.TrimSpace(language)
	if language == "" {
		return "en"
	}

	return Locale(language)
}

// GetLocale returns the locale as text, as go-translator asks its
// localizers.
func (l Locale) GetLocale() string {
	return string(l)
}

// Language is the locale as a BCP 47 language tag, such as "nl" or "pt-BR"
// for pt_BR, for the html lang attribute; a locale that is not a valid tag
// gives its language part.
func (l Locale) Language() string {
	tag, err := language.Parse(string(l))
	if err == nil {
		return tag.String()
	}

	code, _, _ := strings.Cut(strings.ReplaceAll(string(l), "-", "_"), "_")

	return strings.ToLower(code)
}
