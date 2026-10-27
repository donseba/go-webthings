package feed

import (
	"encoding/xml"
	"io"
	"strconv"
	"time"
)

// rssDocument is an RSS 2.0 document, with the Atom namespace for its link
// to itself and the content module for entries' whole content.
type rssDocument struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Atom    string     `xml:"xmlns:atom,attr"`
	Content string     `xml:"xmlns:content,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string       `xml:"title"`
	Link          string       `xml:"link"`
	Description   string       `xml:"description"`
	Self          *rssAtomLink `xml:"atom:link"`
	Language      string       `xml:"language,omitempty"`
	LastBuildDate string       `xml:"lastBuildDate"`
	Items         []rssItem    `xml:"item"`
}

type rssAtomLink struct {
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
	Href string `xml:"href,attr"`
}

type rssItem struct {
	Title       string        `xml:"title"`
	Link        string        `xml:"link,omitempty"`
	Description string        `xml:"description,omitempty"`
	Content     string        `xml:"content:encoded,omitempty"`
	GUID        rssGUID       `xml:"guid"`
	PubDate     string        `xml:"pubDate"`
	Categories  []string      `xml:"category"`
	Enclosure   *rssEnclosure `xml:"enclosure"`
}

type rssGUID struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

type rssEnclosure struct {
	URL    string `xml:"url,attr"`
	Length string `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

// writeRSS writes a valid feed as RSS 2.0. The description, which RSS
// requires, is the Subtitle or else the Title; an entry's description is
// its Summary, and its Content goes in content:encoded. An entry's guid is
// its ID, a permalink only when it is the entry's Link.
func writeRSS(w io.Writer, feed Feed) error {
	channel := rssChannel{
		Title:         feed.Title,
		Link:          feed.Link,
		Description:   feed.Subtitle,
		Language:      Language(feed.Language),
		LastBuildDate: rssTime(feed.updated()),
	}

	if channel.Description == "" {
		channel.Description = feed.Title
	}

	if feed.Self != "" {
		channel.Self = &rssAtomLink{Rel: "self", Type: RSS.MediaType(), Href: feed.Self}
	}

	for _, entry := range feed.Entries {
		channel.Items = append(channel.Items, rssItemOf(entry))
	}

	return encode(w, rssDocument{
		Version: "2.0",
		Atom:    "http://www.w3.org/2005/Atom",
		Content: "http://purl.org/rss/1.0/modules/content/",
		Channel: channel,
	})
}

// rssItemOf is an entry as RSS writes it.
func rssItemOf(entry Entry) rssItem {
	published := entry.Published
	if published.IsZero() {
		published = entry.updated()
	}

	item := rssItem{
		Title:       entry.Title,
		Link:        entry.Link,
		Description: entry.Summary,
		Content:     entry.Content,
		GUID:        rssGUID{IsPermaLink: strconv.FormatBool(entry.ID == entry.Link), Value: entry.ID},
		PubDate:     rssTime(published),
		Categories:  entry.Categories,
	}

	// RSS requires the length of an enclosure: 0 when it is not known.
	if entry.Image.URL != "" {
		item.Enclosure = &rssEnclosure{
			URL:    entry.Image.URL,
			Length: strconv.FormatInt(max(entry.Image.Length, 0), 10),
			Type:   entry.Image.Type,
		}
	}

	return item
}

// rssTime is a time in RFC 1123 with a numeric zone (RFC 822 with a four
// digit year), as RSS's dates have it, in UTC.
func rssTime(t time.Time) string {
	return t.UTC().Format(time.RFC1123Z)
}
