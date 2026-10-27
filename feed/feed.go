// Package feed writes web feeds, Atom 1.0 (RFC 4287) and RSS 2.0, from one
// model: a Feed with its Entries. Atom is the preferred format; RSS is
// written for readers that know only RSS.
//
//	err := feed.Atom.Write(w, feed.Feed{
//		ID:      "https://example.com/en/news/feed.atom",
//		Title:   "News",
//		Link:    "https://example.com/en/news",
//		Self:    "https://example.com/en/news/feed.atom",
//		Author:  "Example",
//		Entries: entries,
//	})
//
// Both formats are written from the same fields, so a Feed is checked
// against the elements both specifications require (see Validate) before
// anything is written. Times are written in RFC 3339 for Atom and RFC 1123
// with a numeric zone for RSS; entry content is HTML, written escaped (Atom's
// type="html", RSS's content:encoded). The package uses the standard library
// only.
package feed

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

// Format is a feed format, also the extension of its address, such as
// feed.atom: Atom or RSS.
type Format string

// The formats a feed is written in.
const (
	Atom Format = "atom"
	RSS  Format = "rss"
)

// Formats are the formats a site offers a feed in, the preferred one, Atom,
// first.
var Formats = []Format{Atom, RSS}

// ContentType is the Content-Type of a feed in the format:
// application/atom+xml or application/rss+xml, in UTF-8.
func (f Format) ContentType() string {
	switch f {
	case Atom:
		return "application/atom+xml; charset=utf-8"
	case RSS:
		return "application/rss+xml; charset=utf-8"
	default:
		return ""
	}
}

// MediaType is the media type of the format without parameters, as a
// <link rel="alternate" type="…"> names it: application/atom+xml or
// application/rss+xml.
func (f Format) MediaType() string {
	media, _, _ := strings.Cut(f.ContentType(), ";")

	return media
}

// ErrInvalid is the error of a Feed that misses an element its formats
// require, or has one that is not valid (see Validate).
var ErrInvalid = errors.New("feed: invalid")

// Feed is a feed: what it is and its entries, newest first.
type Feed struct {
	// ID identifies the feed for good: an absolute IRI that does not change
	// when the feed moves, such as a tag: URI (see TagURI), or the address
	// of the feed when it stays where it is. Atom only.
	ID string
	// Title is the feed's name as readers list it.
	Title string
	// Subtitle says what the feed is about, Atom's subtitle; RSS's
	// description, which RSS requires, is the Title when it is empty.
	Subtitle string
	// Link is the absolute address of the web page the feed belongs to.
	Link string
	// Self is the absolute address of the feed in the format written, for
	// Atom's link rel="self" and RSS's atom:link; the writer of the
	// document sets it per format. Optional.
	Self string
	// Updated is when the feed last changed; zero is the newest Updated of
	// its entries.
	Updated time.Time
	// Language is the language of the feed, a BCP 47 tag such as "nl" or
	// "nl-BE"; a locale written with an underscore, such as "nl_BE", is
	// written with a hyphen. Atom's xml:lang, RSS's language. Optional.
	Language string
	// Author is the name of who publishes the feed, such as the site's
	// name. Atom requires an author for every entry: Author is the author
	// of entries without one of their own.
	Author string
	// Entries are the feed's items, newest first.
	Entries []Entry
}

// Entry is one item of a feed, such as a post.
type Entry struct {
	// ID identifies the entry for good, an absolute IRI such as a tag: URI
	// (see TagURI): it stays the same when the entry changes or moves.
	// RSS writes it as the guid.
	ID string
	// Title is the entry's title, as plain text.
	Title string
	// Link is the absolute address of the entry's page.
	Link string
	// Summary is a short text about the entry, as plain text, such as an
	// excerpt. Optional.
	Summary string
	// Content is the entry's whole content as HTML, with absolute
	// addresses, as a reader shows it away from the site. Optional when
	// the entry has a Link.
	Content string
	// Published is when the entry was first published; Updated is when
	// it last changed in a way that matters to readers, the Published
	// time when zero.
	Published time.Time
	Updated   time.Time
	// Categories are the names of the entry's categories.
	Categories []string
	// Author is the name of the entry's author; empty is the feed's.
	Author string
	// Image is an image of the entry, such as a post's featured image,
	// written as an enclosure. Optional.
	Image Enclosure
}

// Enclosure is a file of an entry, such as its image: its absolute address,
// its media type and its size in bytes, zero when it is not known.
type Enclosure struct {
	URL    string
	Type   string
	Length int64
}

// updated is the time the entry last changed: Updated, or Published when
// it is zero.
func (e Entry) updated() time.Time {
	if e.Updated.IsZero() {
		return e.Published
	}

	return e.Updated
}

// updated is the time the feed last changed: Updated, or the newest time
// an entry changed when it is zero.
func (f Feed) updated() time.Time {
	if !f.Updated.IsZero() {
		return f.Updated
	}

	var newest time.Time

	for _, entry := range f.Entries {
		if entry.updated().After(newest) {
			newest = entry.updated()
		}
	}

	return newest
}

// LastModified is when the feed's content last changed: Updated, or the
// newest time an entry changed. A server's Last-Modified header may be
// later, such as when an entry was removed, which leaves no newer time in
// the feed.
func (f Feed) LastModified() time.Time {
	return f.updated()
}

// Validate checks that the feed has what both formats require: an ID that
// is an absolute IRI, a Title, an absolute Link and a time it was updated,
// its own or one of an entry, and an Author unless every entry has one.
// Every entry needs an absolute ID, a Title, a time it was published or
// updated, and an absolute Link or Content. Addresses that are given must
// be absolute, an image needs its type, and the Language must be a
// language tag. The error wraps ErrInvalid and names each problem.
func (f Feed) Validate() error {
	var problems []error

	invalid := func(format string, args ...any) {
		problems = append(problems, fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, args...)...))
	}

	if !absolute(f.ID) {
		invalid("id %q is not an absolute IRI", f.ID)
	}

	if strings.TrimSpace(f.Title) == "" {
		invalid("no title")
	}

	if !absolute(f.Link) {
		invalid("link %q is not absolute", f.Link)
	}

	if f.Self != "" && !absolute(f.Self) {
		invalid("self link %q is not absolute", f.Self)
	}

	if f.updated().IsZero() {
		invalid("no updated time and no entries")
	}

	if f.Language != "" && !languageTag(Language(f.Language)) {
		invalid("language %q is not a language tag", f.Language)
	}

	authored := strings.TrimSpace(f.Author) != ""

	for i, entry := range f.Entries {
		if err := entry.validate(authored); err != nil {
			invalid("entry %d (%s): %v", i, entry.ID, err)
		}
	}

	return errors.Join(problems...)
}

// validate checks an entry; authored tells that the feed has an author.
func (e Entry) validate(authored bool) error {
	var problems []string

	if !absolute(e.ID) {
		problems = append(problems, "id is not an absolute IRI")
	}

	if strings.TrimSpace(e.Title) == "" {
		problems = append(problems, "no title")
	}

	if e.updated().IsZero() {
		problems = append(problems, "no published or updated time")
	}

	if e.Link == "" && strings.TrimSpace(e.Content) == "" {
		problems = append(problems, "no link and no content")
	}

	if e.Link != "" && !absolute(e.Link) {
		problems = append(problems, "link is not absolute")
	}

	if !authored && strings.TrimSpace(e.Author) == "" {
		problems = append(problems, "no author, and the feed has none")
	}

	if e.Image.URL != "" && (!absolute(e.Image.URL) || e.Image.Type == "") {
		problems = append(problems, "image needs an absolute address and a type")
	}

	if len(problems) == 0 {
		return nil
	}

	return errors.New(strings.Join(problems, ", "))
}

// absolute reports whether link is an absolute IRI: one with a scheme, such
// as https: or tag:.
func absolute(link string) bool {
	if strings.TrimSpace(link) != link || link == "" {
		return false
	}

	parsed, err := url.Parse(link)

	return err == nil && parsed.Scheme != ""
}

// Language is a locale as a BCP 47 language tag, as feeds write it: "nl_BE"
// becomes "nl-BE".
func Language(locale string) string {
	return strings.ReplaceAll(locale, "_", "-")
}

// languageTag reports whether tag looks like a BCP 47 language tag:
// subtags of one to eight letters and digits, separated by hyphens, the
// first of letters.
func languageTag(tag string) bool {
	for i, subtag := range strings.Split(tag, "-") {
		if subtag == "" || len(subtag) > 8 {
			return false
		}

		for _, r := range subtag {
			letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
			digit := r >= '0' && r <= '9'

			if !letter && (i == 0 || !digit) {
				return false
			}
		}
	}

	return true
}

// TagURI is a tag: URI (RFC 4151), an ID that stays the same when what it
// names moves, for Feed.ID and Entry.ID: authority is a domain name that
// was the publisher's on date, such as the site's host without its port,
// and specific tells the item apart from everything else the publisher
// named that day, such as "news/<post ID>".
//
//	feed.TagURI("example.com", created, "news/0199a3c1-…") // tag:example.com,2026-10-07:news/0199a3c1-…
func TagURI(authority string, date time.Time, specific string) string {
	return "tag:" + strings.ToLower(authority) + "," + date.UTC().Format(time.DateOnly) + ":" + specific
}

// Write writes feed in the format, after Validate; a feed that is not
// valid is not written.
func (f Format) Write(w io.Writer, feed Feed) error {
	if err := feed.Validate(); err != nil {
		return err
	}

	switch f {
	case Atom:
		return writeAtom(w, feed)
	case RSS:
		return writeRSS(w, feed)
	default:
		return fmt.Errorf("feed: unknown format %q", string(f))
	}
}
