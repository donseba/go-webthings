// Package markdown renders editor content. Content is stored as Markdown;
// raw HTML in the source is never rendered: goldmark drops each HTML block
// and inline tag and writes <!-- raw HTML omitted --> in its place (the text
// between inline tags stays). Media is referenced as ![alt](media:<uuid>)
// and resolved to a public URL at render time.
package markdown

import (
	"bytes"
	"cmp"
	"html/template"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// MediaPrefix marks a link or image destination that refers to a media asset.
const MediaPrefix = "media:"

var mediaPattern = regexp.MustCompile(`\(media:([0-9a-fA-F-]{36})\)`)

// Resolver turns a media ID into a public URL. It returns "" for unknown assets.
type Resolver func(id string) string

// Image is how an image asset is shown: the address of its src and,
// optionally, a srcset with sizes, the alt text used when the Markdown
// gives none, and the size of URL in pixels for the img's width and height
// attributes, so the browser keeps the image's place before it loads (0
// leaves them out).
type Image struct {
	URL    string
	Srcset string
	Sizes  string
	Alt    string
	Width  int
	Height int
}

// ImageResolver describes the image asset id. alt is the image's
// description in the Markdown; when it is empty, Image.Alt is used instead.
// ok is false when the resolver has nothing to add to Options.Media.
type ImageResolver func(id, alt string) (image Image, ok bool)

// Options are how Render resolves media.
type Options struct {
	// Media turns a media ID into a public URL, for links and images; nil
	// leaves media links empty.
	Media Resolver
	// Images describes media images; nil, or an image it has nothing for,
	// uses Media's URL alone.
	Images ImageResolver
	// Base makes the site's own addresses absolute, for readers away from
	// the site, such as a feed or an e-mail. It is the absolute address
	// the content's relative links lead from, such as
	// "https://example.com/en/": link and image destinations without a
	// scheme are resolved from it as a browser resolves them, both paths
	// on the site ("/en/about" becomes "https://example.com/en/about") and
	// relative addresses ("about" becomes "https://example.com/en/about"),
	// also those Media and Images give, every address of an image's
	// srcset among them. Addresses with a scheme
	// ("https://…", "mailto:…") and a fragment alone ("#top") stay as they
	// are. Empty, or not an absolute address, leaves every destination as
	// it is.
	Base string
}

// base is Options.Base parsed, or nil when it is empty or not absolute.
func (o Options) base() *url.URL {
	if o.Base == "" {
		return nil
	}

	parsed, err := url.Parse(o.Base)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return nil
	}

	return parsed
}

// resolve makes destination absolute from base (see Options.Base).
func resolve(base *url.URL, destination string) string {
	if base == nil || destination == "" || strings.HasPrefix(destination, "#") {
		return destination
	}

	reference, err := url.Parse(destination)
	if err != nil || reference.Scheme != "" {
		return destination
	}

	return base.ResolveReference(reference).String()
}

// AbsoluteSrcset makes every address of srcset, such as "/a.webp 480w,
// /b.webp 800w", absolute from base, as Options.Base does with the srcset
// of an image Images gives; the descriptors (480w) stay as they are. An
// empty base, or one that is not an absolute address, leaves srcset as it
// is.
func AbsoluteSrcset(base, srcset string) string {
	return absoluteSrcset(Options{Base: base}.base(), srcset)
}

// absoluteSrcset makes the addresses of srcset absolute from base (see
// AbsoluteSrcset).
func absoluteSrcset(base *url.URL, srcset string) string {
	if base == nil || srcset == "" {
		return srcset
	}

	candidates := strings.Split(srcset, ",")

	for i, candidate := range candidates {
		address, descriptor, _ := strings.Cut(strings.TrimSpace(candidate), " ")
		candidates[i] = strings.TrimSpace(resolve(base, address) + " " + strings.TrimSpace(descriptor))
	}

	return strings.Join(candidates, ", ")
}

// HTML renders Markdown to safe HTML. A nil resolver leaves media links empty.
func HTML(source string, resolve Resolver) template.HTML {
	return Render(source, Options{Media: resolve})
}

// engine renders every document. goldmark engines are safe for concurrent
// use, so it is built once: building one per call cost more than rendering
// a short text. Media are resolved per call by mediaTransformer after
// parsing, as the AST transformers of an engine are fixed.
var engine = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

// Render renders Markdown to safe HTML with media resolved by options.
func Render(source string, options Options) template.HTML {
	data := []byte(source)
	document := engine.Parser().Parse(text.NewReader(data))

	// The GFM transformers ran during parsing; the media transformer runs
	// last, as it did when it was registered with a lower priority.
	transformer := mediaTransformer{resolve: options.Media, images: options.Images, base: options.base()}
	transformer.Transform(document.(*ast.Document), text.NewReader(data), nil)

	var out bytes.Buffer

	if err := engine.Renderer().Render(&out, data, document); err != nil {
		return template.HTML(template.HTMLEscapeString(source))
	}

	//nolint:gosec // goldmark escapes raw HTML because WithUnsafe is not set.
	return template.HTML(out.String())
}

// MediaIDs returns the distinct media assets referenced by the source, in order.
func MediaIDs(source string) []string {
	var (
		ids  []string
		seen = map[string]bool{}
	)

	for _, match := range mediaPattern.FindAllStringSubmatch(source, -1) {
		id := strings.ToLower(match[1])
		if seen[id] {
			continue
		}

		seen[id] = true
		ids = append(ids, id)
	}

	return ids
}

// Links returns the distinct destinations of the links in source, in
// order, as written: those of [text](address) and reference links, and the
// web addresses GFM turns into links, such as https://example.com. Links to
// media assets are media:<uuid>; images are not links.
func Links(source string) []string {
	data := []byte(source)
	document := engine.Parser().Parse(text.NewReader(data))

	var (
		links []string
		seen  = map[string]bool{}
	)

	add := func(destination string) {
		if destination == "" || seen[destination] {
			return
		}

		seen[destination] = true
		links = append(links, destination)
	}

	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *ast.Link:
			add(string(n.Destination))
		case *ast.AutoLink:
			if n.AutoLinkType == ast.AutoLinkURL {
				add(string(n.URL(data)))
			}
		}

		return ast.WalkContinue, nil
	})

	return links
}

type mediaTransformer struct {
	resolve Resolver
	images  ImageResolver
	// base makes addresses absolute (see Options.Base).
	base *url.URL
}

func (m mediaTransformer) Transform(document *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()

	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *ast.Image:
			if !m.image(n, source) {
				n.Destination = m.url(n.Destination)
			}

			n.Destination = m.absolute(n.Destination)
		case *ast.Link:
			n.Destination = m.absolute(m.url(n.Destination))
		}

		return ast.WalkContinue, nil
	})
}

// image fills in a media image from the image resolver; false leaves it to
// the URL resolver.
func (m mediaTransformer) image(n *ast.Image, source []byte) bool {
	id, ok := strings.CutPrefix(string(n.Destination), MediaPrefix)
	if !ok || m.images == nil {
		return false
	}

	alt := strings.TrimSpace(altText(n, source))

	image, ok := m.images(id, alt)
	if !ok || image.URL == "" {
		return false
	}

	n.Destination = []byte(image.URL)

	if image.Srcset != "" {
		n.SetAttributeString("srcset", []byte(absoluteSrcset(m.base, image.Srcset)))
	}

	if image.Srcset != "" && image.Sizes != "" {
		n.SetAttributeString("sizes", []byte(image.Sizes))
	}

	if image.Width > 0 && image.Height > 0 {
		n.SetAttributeString("width", []byte(strconv.Itoa(image.Width)))
		n.SetAttributeString("height", []byte(strconv.Itoa(image.Height)))
	}

	// The alt text of the Markdown wins; without one the library's applies.
	if image.Alt != "" && alt == "" {
		for n.FirstChild() != nil {
			n.RemoveChild(n, n.FirstChild())
		}

		n.AppendChild(n, ast.NewString([]byte(image.Alt)))
	}

	return true
}

// altText is the text of an image's description.
func altText(n ast.Node, source []byte) string {
	var b strings.Builder

	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch c := child.(type) {
		case *ast.Text:
			b.Write(c.Segment.Value(source))
		case *ast.String:
			b.Write(c.Value)
		default:
			b.WriteString(altText(c, source))
		}
	}

	return b.String()
}

func (m mediaTransformer) url(destination []byte) []byte {
	raw, ok := strings.CutPrefix(string(destination), MediaPrefix)
	if !ok {
		return destination
	}

	if m.resolve == nil {
		return nil
	}

	return []byte(m.resolve(raw))
}

// absolute resolves a destination from the base (see Options.Base).
func (m mediaTransformer) absolute(destination []byte) []byte {
	if m.base == nil {
		return destination
	}

	return []byte(resolve(m.base, string(destination)))
}

// mediaReference is an image or a link to a media asset in Markdown:
// ![alt](media:<uuid>) or [text](media:<uuid>).
var mediaReference = regexp.MustCompile(`(!?)\[([^\]]*)\]\(media:([0-9a-fA-F-]{36})\)`)

// linkDestination is the destination of an inline link or image in
// Markdown, after "](", with or without angle brackets.
var linkDestination = regexp.MustCompile(`\]\([ \t]*(<[^<>\n]*>|[^\s)]+)`)

// referenceDestination is the destination of a link reference definition,
// such as [about]: /en/about.
var referenceDestination = regexp.MustCompile(`(?m)^ {0,3}\[[^\]\n]+\]:[ \t]*(<[^<>\n]*>|\S+)`)

// PlainMedia is source for readers of the Markdown itself, such as the text
// part of an e-mail, with its media references resolved by options: an
// image becomes a link to the image, [alt](address), with the alt text of
// options.Images when the Markdown gives none and the address of
// options.Images or else options.Media; a link to media gets the address
// of options.Media. A reference that does not resolve keeps its text.
// With options.Base, the destinations of links and images outside code
// are resolved from it, as Render does.
func PlainMedia(source string, options Options) string {
	base := options.base()
	if base != nil {
		source = absoluteDestinations(source, base)
	}

	return mediaReference.ReplaceAllStringFunc(source, func(reference string) string {
		parts := mediaReference.FindStringSubmatch(reference)
		image := parts[1] == "!"
		text := strings.TrimSpace(parts[2])
		id := parts[3]

		address := ""
		if options.Media != nil {
			address = options.Media(id)
		}

		if image && options.Images != nil {
			if shown, ok := options.Images(id, text); ok {
				address = cmp.Or(shown.URL, address)

				if text == "" {
					text = shown.Alt
				}
			}
		}

		address = resolve(base, address)

		switch {
		case address == "":
			return text
		case text == "":
			return "<" + address + ">"
		default:
			return "[" + text + "](" + address + ")"
		}
	})
}

// absoluteDestinations resolves the destinations of source's links,
// images and link reference definitions from base, leaving those in code
// as they are.
func absoluteDestinations(source string, base *url.URL) string {
	code := codeRanges(source)

	var matches [][]int

	matches = append(matches, linkDestination.FindAllStringSubmatchIndex(source, -1)...)
	matches = append(matches, referenceDestination.FindAllStringSubmatchIndex(source, -1)...)

	slices.SortFunc(matches, func(a, b []int) int {
		return a[2] - b[2]
	})

	var (
		b    strings.Builder
		done int
	)

	for _, match := range matches {
		start, end := match[2], match[3]
		if start < done || inRanges(code, start) {
			continue
		}

		destination := source[start:end]

		resolved := resolve(base, strings.Trim(destination, "<>"))
		if strings.HasPrefix(destination, "<") {
			resolved = "<" + resolved + ">"
		}

		b.WriteString(source[done:start])
		b.WriteString(resolved)
		done = end
	}

	b.WriteString(source[done:])

	return b.String()
}

// codeRanges are the start and end offsets of the code in source: code
// spans and code blocks.
func codeRanges(source string) [][2]int {
	data := []byte(source)
	document := engine.Parser().Parse(text.NewReader(data))

	var ranges [][2]int

	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *ast.CodeSpan:
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				if text, ok := child.(*ast.Text); ok {
					ranges = append(ranges, [2]int{text.Segment.Start, text.Segment.Stop})
				}
			}

			return ast.WalkSkipChildren, nil
		case *ast.CodeBlock, *ast.FencedCodeBlock:
			lines := n.Lines()
			for i := range lines.Len() {
				line := lines.At(i)
				ranges = append(ranges, [2]int{line.Start, line.Stop})
			}
		}

		return ast.WalkContinue, nil
	})

	return ranges
}

// inRanges reports whether offset is within one of ranges.
func inRanges(ranges [][2]int, offset int) bool {
	for _, r := range ranges {
		if offset >= r[0] && offset < r[1] {
			return true
		}
	}

	return false
}

// Text renders Markdown as plain text on one line, for search and summaries:
// formatting, raw HTML and link targets are dropped, the words are kept
// (including image descriptions).
func Text(source string) string {
	data := []byte(source)
	document := engine.Parser().Parse(text.NewReader(data))

	var b strings.Builder

	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			// Blocks (paragraphs, headings, list items) are separate words.
			if node.Type() == ast.TypeBlock {
				b.WriteByte(' ')
			}

			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *ast.Text:
			b.Write(n.Segment.Value(data))

			if n.SoftLineBreak() || n.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(n.Value)
		case *ast.AutoLink:
			b.Write(n.Label(data))
		case *ast.CodeBlock, *ast.FencedCodeBlock:
			lines := n.Lines()
			for i := range lines.Len() {
				line := lines.At(i)
				b.Write(line.Value(data))
				b.WriteByte(' ')
			}
		}

		return ast.WalkContinue, nil
	})

	return strings.Join(strings.Fields(b.String()), " ")
}
