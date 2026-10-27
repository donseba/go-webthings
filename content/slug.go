package content

import (
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// maxSlug keeps generated slugs short; editors can change them.
const maxSlug = 80

// ligatures are letters that do not decompose into a base letter.
var ligatures = strings.NewReplacer("æ", "ae", "œ", "oe", "ø", "o", "ß", "ss", "ł", "l", "đ", "d", "þ", "th", "ĳ", "ij")

// Slug derives a URL slug from a title, for example "Café opens again!"
// becomes "cafe-opens-again": lowercase ASCII letters and digits joined by
// dashes, at most 80 characters, cut between words. Text without such
// letters, for example in another script, gives "".
func Slug(title string) string {
	var b strings.Builder

	dash := false

	for _, r := range norm.NFD.String(ligatures.Replace(strings.ToLower(title))) {
		switch {
		case unicode.Is(unicode.Mn, r):
			// Accents are dropped with the decomposition, é becomes e.
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}

			b.WriteRune(r)

			dash = false
		default:
			dash = true
		}
	}

	slug := b.String()
	if len(slug) <= maxSlug {
		return slug
	}

	cut := slug[:maxSlug]

	// Drop a word the cut splits, unless that leaves too little.
	if slug[maxSlug] != '-' {
		if last := strings.LastIndexByte(cut, '-'); last > maxSlug/2 {
			cut = cut[:last]
		}
	}

	return strings.TrimRight(cut, "-")
}

// UniqueSlug returns slug when taken does not report it as used, and
// otherwise the first of slug-2, slug-3, … that is free, for example to give
// a new item a slug made with Slug that no other item has.
func UniqueSlug(slug string, taken func(slug string) bool) string {
	candidate := slug

	for n := 2; taken(candidate); n++ {
		candidate = slug + "-" + strconv.Itoa(n)
	}

	return candidate
}
