// Package bundled knows the template functions of the modules bundled with
// WebThings, also of those a site does not use, so the template check at
// Start can say where an unknown function comes from.
package bundled

import (
	"maps"
	"slices"
)

// funcs are the template functions of the bundled modules, by the type of
// the module that offers them. testsuite's TestBundledFuncs keeps them in
// step with the modules' Funcs.
var funcs = map[string][]string{
	"banners": {"banner", "banners"},
	"comments": {
		"commentCount", "commentForm", "commentInput", "commentResult", "commentThread", "commentURL",
		"comments", "commentsClosed",
	},
	"directory":     {"directoryCategories", "directoryEntries", "directoryEntry"},
	"documents":     {"documents", "documentsIn"},
	"events":        {"events"},
	"gallery":       {"album", "albums"},
	"lists":         {"entries"},
	"newsletter":    {"newsletter", "newsletterForm", "newsletterResult", "newsletterURL"},
	"people":        {"people", "peopleIn", "person"},
	"polls":         {"poll", "pollBox", "pollURL", "polls"},
	"publishing":    {"posts"},
	"registrations": {"registerURL", "registration", "registrationCode", "registrationForm", "registrationResult"},
	"stats":         {"pageViews", "popular"},
	"submissions":   {"submitInput", "submitResult", "submitURL"},
	"testimonials":  {"testimonials"},
}

// ModuleOf is the type of the bundled module that offers the template
// function name, such as "newsletter" for newsletterForm, or "".
func ModuleOf(name string) string {
	for module, names := range funcs {
		if slices.Contains(names, name) {
			return module
		}
	}

	return ""
}

// Funcs are the template functions of the bundled modules, by the type of
// the module that offers them; a copy.
func Funcs() map[string][]string {
	copied := maps.Clone(funcs)
	for module, names := range copied {
		copied[module] = slices.Clone(names)
	}

	return copied
}
