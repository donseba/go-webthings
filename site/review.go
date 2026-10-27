package site

import (
	"slices"
	"strings"
)

// Reviewed reports whether the review workflow is on for an admin section,
// by its permission ID, such as "pages" or "news": its revisions are then
// published only after a reviewer approved them (see content.WithReview).
func (s Settings) Reviewed(section string) bool {
	return section != "" && slices.Contains(s.Review, section)
}

// cleanReview trims, sorts and deduplicates the reviewed sections.
func cleanReview(sections []string) []string {
	var out []string

	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section != "" {
			out = append(out, section)
		}
	}

	slices.Sort(out)

	return slices.Compact(out)
}
