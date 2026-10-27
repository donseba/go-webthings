package markdown

import (
	"strings"
	"testing"
)

// benchSource is a page body of a few kilobytes with headings, emphasis,
// a list, a table, code and a media image.
var benchSource = strings.Repeat(`## A heading

Lorem ipsum dolor sit amet, **consectetur** adipiscing elit, sed do _eiusmod_
tempor incididunt ut labore et dolore magna aliqua. See [the docs](https://example.com).

- one
- two
- three

| a | b |
|---|---|
| 1 | 2 |

`+"`code`"+` and ![](media:0190a8f2-7c1e-7b3a-9d4e-1a2b3c4d5e6f)

`, 6)

func BenchmarkRender(b *testing.B) {
	options := Options{Media: func(id string) string {
		return "/media/" + id + ".png"
	}}

	for b.Loop() {
		Render(benchSource, options)
	}
}

func BenchmarkText(b *testing.B) {
	for b.Loop() {
		Text(benchSource)
	}
}
