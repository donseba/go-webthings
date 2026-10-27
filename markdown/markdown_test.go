package markdown_test

import (
	"strings"
	"testing"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/markdown"
)

func TestHTMLEscapesRawHTMLAndResolvesMedia(t *testing.T) {
	id := db.NewID()
	source := "# Title\n\n<script>alert(1)</script>\n\n![Photo](media:" + id + ")"

	got := string(markdown.HTML(source, func(asset string) string {
		if asset == id {
			return "/media/photo.jpg"
		}

		return ""
	}))

	if strings.Contains(got, "<script>") {
		t.Fatalf("raw HTML rendered: %s", got)
	}

	if !strings.Contains(got, `<img src="/media/photo.jpg" alt="Photo">`) {
		t.Fatalf("media not resolved: %s", got)
	}

	if !strings.Contains(got, `<h1 id="title">Title</h1>`) {
		t.Fatalf("heading missing: %s", got)
	}
}

func TestMediaIDs(t *testing.T) {
	a, b := db.NewID(), db.NewID()
	source := "![x](media:" + a + ") [y](media:" + b + ") ![z](media:" + a + ")"

	ids := markdown.MediaIDs(source)
	if len(ids) != 2 || ids[0] != a || ids[1] != b {
		t.Fatalf("%v", ids)
	}
}

// TestLinks checks the link destinations found in Markdown: inline,
// reference and media links and bare web addresses, each once, without
// images, raw HTML and code.
func TestLinks(t *testing.T) {
	id := db.NewID()
	source := "See [about](/en/about) and [team](team \"Team\"), [again](/en/about).\n\n" +
		"[Read more][more] or visit https://example.com/en/news.\n\n" +
		"![A photo](media:" + id + ") [the brochure](media:" + id + ")\n\n" +
		"<a href=\"/en/raw\">raw</a> `[code](/en/code)`\n\n" +
		"[more]: ../en/contact#form\n"

	got := markdown.Links(source)
	want := []string{"/en/about", "team", "../en/contact#form", "https://example.com/en/news", "media:" + id}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("links:\n got %q\nwant %q", got, want)
	}
}

func TestText(t *testing.T) {
	source := "# Opening hours\n\nWe open at **nine**,\nsee [the map](https://example.com).\n\n" +
		"- coffee\n- tea\n\nSome <b>bold</b> words\n\n![A photo](media:" + db.NewID() + ")"

	got := markdown.Text(source)
	// Tags are dropped, the words between them kept.
	want := "Opening hours We open at nine, see the map. coffee tea Some bold words A photo"

	if got != want {
		t.Fatalf("text:\n got %q\nwant %q", got, want)
	}
}

// TestRenderImages checks images described by an image resolver: the
// resized address with a srcset, and the library's alt text where the
// Markdown has none.
func TestRenderImages(t *testing.T) {
	described, plain := db.NewID(), db.NewID()
	source := "![](media:" + described + ") ![Own words](media:" + described + ") ![](media:" + plain + ")"

	var asked []string

	got := string(markdown.Render(source, markdown.Options{
		Media: func(id string) string {
			return "/media/" + id + ".gif"
		},
		Images: func(id, alt string) (markdown.Image, bool) {
			asked = append(asked, alt)

			if id != described {
				return markdown.Image{}, false
			}

			return markdown.Image{
				URL:    "/media/v/sig/" + id + "_1200x0.webp",
				Srcset: "/a 480w, /b 800w",
				Sizes:  "100vw",
				Alt:    `Library "alt" <text>`,
				Width:  1200,
				Height: 800,
			}, true
		},
	}))

	size := ` width="1200" height="800"`
	want := []string{
		`<img src="/media/v/sig/` + described + `_1200x0.webp" alt="Library &quot;alt&quot; &lt;text&gt;" srcset="/a 480w, /b 800w" sizes="100vw"` + size + `>`,
		`<img src="/media/v/sig/` + described + `_1200x0.webp" alt="Own words" srcset="/a 480w, /b 800w" sizes="100vw"` + size + `>`,
		`<img src="/media/` + plain + `.gif" alt="">`,
	}

	for _, fragment := range want {
		if !strings.Contains(got, fragment) {
			t.Fatalf("want %s in\n%s", fragment, got)
		}
	}

	if len(asked) != 3 || asked[0] != "" || asked[1] != "Own words" {
		t.Fatalf("resolver asked with %q", asked)
	}
}

// TestRenderBase checks that Base makes the site's own addresses absolute:
// links, images and media, also those the resolvers give, every address of
// an image's srcset among them, paths on the site and relative addresses,
// which are resolved from Base, and leaves fragments and addresses with a
// scheme as they are.
func TestRenderBase(t *testing.T) {
	photo := db.NewID()
	file := db.NewID()
	source := "[About](/en/about) [Top](#top) [Other](https://other.example/a) [CDN](//cdn.example/a.css)\n\n" +
		"[Team](team) [Up](../nl/over) [Mail](mailto:hi@example.com) [Next](?page=2)\n\n" +
		"![Photo](media:" + photo + ") [File](media:" + file + ") ![Logo](/theme/logo.png) ![Map](map.png)"

	got := string(markdown.Render(source, markdown.Options{
		Base: "https://example.com/en/",
		Media: func(id string) string {
			return "/media/" + id + ".pdf"
		},
		Images: func(id, _ string) (markdown.Image, bool) {
			return markdown.Image{
				URL:    "/media/v/sig/" + id + ".jpg",
				Srcset: "/media/v/sig/" + id + "_480.jpg 480w, https://cdn.example/" + id + ".jpg 800w,small.jpg 1x",
			}, true
		},
	}))

	for _, want := range []string{
		`href="https://example.com/en/about"`,
		`href="#top"`,
		`href="https://other.example/a"`,
		`href="https://cdn.example/a.css"`,
		`href="https://example.com/en/team"`,
		`href="https://example.com/nl/over"`,
		`href="mailto:hi@example.com"`,
		`href="https://example.com/en/?page=2"`,
		`src="https://example.com/media/v/sig/` + photo + `.jpg"`,
		`href="https://example.com/media/` + file + `.pdf"`,
		`src="https://example.com/theme/logo.png"`,
		`src="https://example.com/en/map.png"`,
		`srcset="https://example.com/media/v/sig/` + photo + `_480.jpg 480w, https://cdn.example/` + photo + `.jpg 800w, https://example.com/en/small.jpg 1x"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in\n%s", want, got)
		}
	}

	// A Base that is not an absolute address leaves every address as it is.
	for _, base := range []string{"", "/en/", "example.com"} {
		got := string(markdown.Render("[About](/en/about) [Team](team)", markdown.Options{Base: base}))
		if !strings.Contains(got, `href="/en/about"`) || !strings.Contains(got, `href="team"`) {
			t.Fatalf("base %q: %s", base, got)
		}

		if got := markdown.AbsoluteSrcset(base, "/a.webp 480w, b.webp 800w"); got != "/a.webp 480w, b.webp 800w" {
			t.Fatalf("srcset with base %q: %s", base, got)
		}
	}

	if got := markdown.AbsoluteSrcset("https://example.com/en/", " /a.webp 480w,b.webp  800w, //cdn.example/c.webp"); got !=
		"https://example.com/a.webp 480w, https://example.com/en/b.webp 800w, https://cdn.example/c.webp" {
		t.Fatalf("AbsoluteSrcset: %s", got)
	}
}

// TestPlainMediaBase checks that PlainMedia with Base resolves the
// destinations of links, images, link reference definitions and media
// from it, as Render does, and leaves code, fragments and addresses with a
// scheme as they are.
func TestPlainMediaBase(t *testing.T) {
	file := db.NewID()
	source := "Read [about us](/en/about), [the team](team \"Team\") and [more](<more info>).\n\n" +
		"![Map](map.png) [Top](#top) [Shop](https://shop.example/) [The file](media:" + file + ")\n\n" +
		"See `[code](/en/code)` and [ref].\n\n" +
		"    [indented](/en/indented)\n\n" +
		"[ref]: /en/ref\n"

	got := markdown.PlainMedia(source, markdown.Options{
		Base: "https://example.com/en/",
		Media: func(id string) string {
			return "/media/" + id + ".pdf"
		},
	})

	want := "Read [about us](https://example.com/en/about), [the team](https://example.com/en/team \"Team\")" +
		" and [more](<https://example.com/en/more%20info>).\n\n" +
		"![Map](https://example.com/en/map.png) [Top](#top) [Shop](https://shop.example/)" +
		" [The file](https://example.com/media/" + file + ".pdf)\n\n" +
		"See `[code](/en/code)` and [ref].\n\n" +
		"    [indented](/en/indented)\n\n" +
		"[ref]: https://example.com/en/ref\n"

	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}

	// Without Base the links stay as they are.
	if got := markdown.PlainMedia(
		"[about us](/en/about) [team](team)",
		markdown.Options{},
	); got != "[about us](/en/about) [team](team)" {
		t.Fatalf("without base: %s", got)
	}
}

// TestPlainMedia checks that media references become links with addresses
// for readers of the Markdown: an image a link to it with the library's
// alt text where the Markdown has none, a link the file's address, and an
// unknown asset its text alone.
func TestPlainMedia(t *testing.T) {
	photo, file, missing := db.NewID(), db.NewID(), db.NewID()
	source := "Look: ![](media:" + photo + ")\n\n" +
		"![Mine](media:" + photo + ") [The file](media:" + file + ") [Gone](media:" + missing + ")" +
		" ![](media:" + missing + ") [elsewhere](https://example.com/)"

	options := markdown.Options{
		Media: func(id string) string {
			if id == missing {
				return ""
			}

			return "https://example.com/media/" + id
		},
		Images: func(id, _ string) (markdown.Image, bool) {
			if id != photo {
				return markdown.Image{}, false
			}

			return markdown.Image{URL: "https://example.com/mail/" + id + ".jpg", Alt: "A shop"}, true
		},
	}

	want := "Look: [A shop](https://example.com/mail/" + photo + ".jpg)\n\n" +
		"[Mine](https://example.com/mail/" + photo + ".jpg) [The file](https://example.com/media/" + file + ") Gone" +
		"  [elsewhere](https://example.com/)"

	if got := markdown.PlainMedia(source, options); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
