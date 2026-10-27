package feed

import (
	"encoding/xml"
	"io"
	"strconv"
	"time"
)

// atomFeed is an Atom 1.0 feed document (RFC 4287).
type atomFeed struct {
	XMLName  xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Lang     string      `xml:"http://www.w3.org/XML/1998/namespace lang,attr,omitempty"`
	ID       string      `xml:"id"`
	Title    atomText    `xml:"title"`
	Subtitle *atomText   `xml:"subtitle"`
	Links    []atomLink  `xml:"link"`
	Updated  string      `xml:"updated"`
	Author   *atomPerson `xml:"author"`
	Entries  []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID         string         `xml:"id"`
	Title      atomText       `xml:"title"`
	Links      []atomLink     `xml:"link"`
	Published  string         `xml:"published,omitempty"`
	Updated    string         `xml:"updated"`
	Author     *atomPerson    `xml:"author"`
	Categories []atomCategory `xml:"category"`
	Summary    *atomText      `xml:"summary"`
	Content    *atomText      `xml:"content"`
}

// atomText is a text construct: plain text, or with Type "html" escaped
// HTML.
type atomText struct {
	Type string `xml:"type,attr,omitempty"`
	Body string `xml:",chardata"`
}

type atomLink struct {
	Rel    string `xml:"rel,attr,omitempty"`
	Type   string `xml:"type,attr,omitempty"`
	Href   string `xml:"href,attr"`
	Length string `xml:"length,attr,omitempty"`
}

type atomPerson struct {
	Name string `xml:"name"`
}

type atomCategory struct {
	Term string `xml:"term,attr"`
}

// writeAtom writes a valid feed as Atom.
func writeAtom(w io.Writer, feed Feed) error {
	document := atomFeed{
		Lang:    Language(feed.Language),
		ID:      feed.ID,
		Title:   atomText{Body: feed.Title},
		Links:   []atomLink{{Rel: "alternate", Type: "text/html", Href: feed.Link}},
		Updated: atomTime(feed.updated()),
	}

	if feed.Subtitle != "" {
		document.Subtitle = &atomText{Body: feed.Subtitle}
	}

	if feed.Self != "" {
		document.Links = append(document.Links, atomLink{Rel: "self", Type: Atom.MediaType(), Href: feed.Self})
	}

	if feed.Author != "" {
		document.Author = &atomPerson{Name: feed.Author}
	}

	for _, entry := range feed.Entries {
		document.Entries = append(document.Entries, atomEntryOf(entry))
	}

	return encode(w, document)
}

// atomEntryOf is an entry as Atom writes it.
func atomEntryOf(entry Entry) atomEntry {
	written := atomEntry{
		ID:      entry.ID,
		Title:   atomText{Body: entry.Title},
		Updated: atomTime(entry.updated()),
	}

	if entry.Link != "" {
		written.Links = append(written.Links, atomLink{Rel: "alternate", Type: "text/html", Href: entry.Link})
	}

	if entry.Image.URL != "" {
		written.Links = append(written.Links, atomLink{
			Rel:    "enclosure",
			Type:   entry.Image.Type,
			Href:   entry.Image.URL,
			Length: length(entry.Image.Length),
		})
	}

	if !entry.Published.IsZero() {
		written.Published = atomTime(entry.Published)
	}

	if entry.Author != "" {
		written.Author = &atomPerson{Name: entry.Author}
	}

	for _, category := range entry.Categories {
		written.Categories = append(written.Categories, atomCategory{Term: category})
	}

	if entry.Summary != "" {
		written.Summary = &atomText{Body: entry.Summary}
	}

	if entry.Content != "" {
		written.Content = &atomText{Type: "html", Body: entry.Content}
	}

	return written
}

// atomTime is a time in RFC 3339, as Atom's date constructs have it, in
// UTC to the second.
func atomTime(t time.Time) string {
	return t.UTC().Truncate(time.Second).Format(time.RFC3339)
}

// length is the size of an enclosure for its length attribute; "" leaves
// out a size that is not known.
func length(size int64) string {
	if size <= 0 {
		return ""
	}

	return strconv.FormatInt(size, 10)
}

// encode writes document as an indented XML document with its declaration.
func encode(w io.Writer, document any) error {
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}

	encoder := xml.NewEncoder(w)
	encoder.Indent("", "  ")

	if err := encoder.Encode(document); err != nil {
		return err
	}

	if err := encoder.Close(); err != nil {
		return err
	}

	_, err := io.WriteString(w, "\n")

	return err
}
