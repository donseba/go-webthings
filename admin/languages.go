package admin

import (
	"strings"

	"golang.org/x/text/language"
)

// endonyms are languages in their own language. A language is shown to every
// editor the same way, so its name never needs translating.
var endonyms = map[string]string{
	"af": "Afrikaans",
	"ar": "العربية",
	"bg": "Български",
	"ca": "Català",
	"cs": "Čeština",
	"cy": "Cymraeg",
	"da": "Dansk",
	"de": "Deutsch",
	"el": "Ελληνικά",
	"en": "English",
	"es": "Español",
	"et": "Eesti",
	"eu": "Euskara",
	"fa": "فارسی",
	"fi": "Suomi",
	"fr": "Français",
	"fy": "Frysk",
	"ga": "Gaeilge",
	"gl": "Galego",
	"he": "עברית",
	"hi": "हिन्दी",
	"hr": "Hrvatski",
	"hu": "Magyar",
	"id": "Bahasa Indonesia",
	"is": "Íslenska",
	"it": "Italiano",
	"ja": "日本語",
	"ko": "한국어",
	"lb": "Lëtzebuergesch",
	"lt": "Lietuvių",
	"lv": "Latviešu",
	"mt": "Malti",
	"nb": "Norsk bokmål",
	"nl": "Nederlands",
	"nn": "Norsk nynorsk",
	"no": "Norsk",
	"pl": "Polski",
	"pt": "Português",
	"ro": "Română",
	"ru": "Русский",
	"sk": "Slovenčina",
	"sl": "Slovenščina",
	"sq": "Shqip",
	"sr": "Српски",
	"sv": "Svenska",
	"th": "ไทย",
	"tr": "Türkçe",
	"uk": "Українська",
	"vi": "Tiếng Việt",
	"zh": "中文",
}

// LanguageName names a language code in its own language: "Nederlands" for
// "nl", "Deutsch" for "de". A regional variant such as "pt-BR" gets the name
// of its language ("Português"); a code without a known name is returned as
// it is.
func LanguageName(code string) string {
	if name, ok := endonyms[strings.ToLower(code)]; ok {
		return name
	}

	tag, err := language.Parse(code)
	if err != nil {
		return code
	}

	base, _ := tag.Base()
	if name, ok := endonyms[base.String()]; ok {
		return name
	}

	return code
}

// LanguageLabel is a language's own name with its code, such as
// "Nederlands (nl)" or "Português (pt-BR)"; an unknown code is shown alone.
func LanguageLabel(code string) string {
	name := LanguageName(code)
	if name == code {
		return code
	}

	return name + " (" + code + ")"
}
