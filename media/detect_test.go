package media

import (
	"slices"
	"strings"
	"testing"
)

// TestAcceptedTypes keeps the accepted uploads in line with the help text of
// the media library (mediaadmin's templates/library.gohtml and its refusal
// message): update those when this list changes.
func TestAcceptedTypes(t *testing.T) {
	var accepted []string

	for _, extension := range extensions {
		accepted = append(accepted, extension)
	}

	for _, refined := range containers {
		for extension := range refined {
			accepted = append(accepted, extension)
		}
	}

	slices.Sort(accepted)

	want := "csv docx gif jpg odp ods odt pdf png pptx txt xlsx zip"
	if got := strings.Join(accepted, " "); got != want {
		t.Fatalf("accepted = %s, want %s", got, want)
	}

	// WebP and SVG are refused, as the help text says, SVG also without an
	// XML declaration, under any name.
	refused := map[string]string{
		"photo.webp":   "RIFF\x24\x00\x00\x00WEBPVP8 ",
		"drawing.svg":  `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`,
		"bare.svg":     `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
		"spaced.txt":   "\xef\xbb\xbf\n  <SVG viewBox=\"0 0 1 1\"></SVG>",
		"doctype.csv":  `<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "x"><svg></svg>`,
		"commented.md": `<!-- drawing --><svg></svg>`,
	}

	for filename, data := range refused {
		if mimeType, _, ok := detect(filename, []byte(data)); ok {
			t.Errorf("%s accepted as %s", filename, mimeType)
		}
	}

	// Text that only mentions an <svg element is still text.
	for filename, data := range map[string]string{
		"notes.txt": "Use an <svg> element for icons.",
		"data.csv":  "tag,use\n<svg>,icons\n",
	} {
		if _, _, ok := detect(filename, []byte(data)); !ok {
			t.Errorf("%s refused", filename)
		}
	}
}
