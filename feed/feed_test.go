package feed_test

import (
	"bytes"
	"encoding/xml"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/feed"
)

// update writes the golden files anew: go test ./feed -update.
var update = flag.Bool("update", false, "write the golden files")

// example is a feed with every field: a post with content, categories and
// an image, and one with a summary only and an author of its own.
func example() feed.Feed {
	published := time.Date(2026, 10, 1, 9, 30, 0, 0, time.FixedZone("CEST", 2*60*60))
	created := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)

	return feed.Feed{
		ID:       "https://example.com/nl_BE/nieuws/feed.atom",
		Title:    "Nieuws & meer",
		Subtitle: "Het laatste <nieuws> van \"Atelier\"",
		Link:     "https://example.com/nl_BE/nieuws",
		Self:     "https://example.com/nl_BE/nieuws/feed.atom",
		Language: "nl_BE",
		Author:   "Atelier Noord",
		Entries: []feed.Entry{
			{
				ID:         feed.TagURI("Example.com", created, "news/0199a3c1-0000-7000-8000-000000000001"),
				Title:      "Open <atelier> & koffie",
				Link:       "https://example.com/nl_BE/nieuws/open-atelier",
				Summary:    "Kom langs op zaterdag.",
				Content:    `<p>Kom <em>langs</em> &amp; kijk.</p><p><img src="https://example.com/media/v/x/1.jpg" alt="Klei"></p>`,
				Published:  published,
				Updated:    published.Add(time.Hour),
				Categories: []string{"Keramiek", "Open dag"},
				Image:      feed.Enclosure{URL: "https://example.com/media/v/x/1.jpg", Type: "image/jpeg"},
			},
			{
				ID:        "https://example.com/nl_BE/nieuws/oud",
				Title:     "Oud bericht",
				Link:      "https://example.com/nl_BE/nieuws/oud",
				Summary:   "Alleen een samenvatting.",
				Published: created,
				Author:    "Ada",
				Image:     feed.Enclosure{URL: "https://example.com/media/2.png", Type: "image/png", Length: 1234},
			},
		},
	}
}

func TestGolden(t *testing.T) {
	t.Parallel()

	for _, format := range feed.Formats {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()

			var written bytes.Buffer

			// The writer of a document gives it its own address.
			write := example()
			write.Self = "https://example.com/nl_BE/nieuws/feed." + string(format)

			if err := format.Write(&written, write); err != nil {
				t.Fatal(err)
			}

			golden(t, filepath.Join("testdata", "example."+string(format)+".golden"), written.Bytes())

			// The document is well-formed XML.
			decoder := xml.NewDecoder(bytes.NewReader(written.Bytes()))

			for {
				_, err := decoder.Token()
				if err != nil {
					if !errors.Is(err, io.EOF) {
						t.Fatalf("not well-formed: %v", err)
					}

					break
				}
			}
		})
	}
}

// golden compares got with the golden file, or writes it with -update.
func golden(t *testing.T, path string, got []byte) {
	t.Helper()

	if *update {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs (go test ./feed -update writes it):\n%s", path, got)
	}
}

// TestAtomDecodes reads the Atom document back: the escaped HTML content
// comes back as it was given, and times are RFC 3339 in UTC.
func TestAtomDecodes(t *testing.T) {
	t.Parallel()

	var written bytes.Buffer

	if err := feed.Atom.Write(&written, example()); err != nil {
		t.Fatal(err)
	}

	var document struct {
		Lang    string `xml:"http://www.w3.org/XML/1998/namespace lang,attr"`
		Updated string `xml:"updated"`
		Entries []struct {
			Content struct {
				Type string `xml:"type,attr"`
				Body string `xml:",chardata"`
			} `xml:"content"`
			Updated string `xml:"updated"`
		} `xml:"entry"`
	}

	if err := xml.Unmarshal(written.Bytes(), &document); err != nil {
		t.Fatal(err)
	}

	if document.Lang != "nl-BE" {
		t.Fatalf("xml:lang %q", document.Lang)
	}

	// The feed's time is the newest entry's.
	if document.Updated != "2026-10-01T08:30:00Z" || document.Entries[0].Updated != "2026-10-01T08:30:00Z" {
		t.Fatalf("updated %q, entry %q", document.Updated, document.Entries[0].Updated)
	}

	content := document.Entries[0].Content
	if content.Type != "html" || content.Body != example().Entries[0].Content {
		t.Fatalf("content %+v", content)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	if err := example().Validate(); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		change func(*feed.Feed)
		want   string
	}{
		"relative id": {
			change: func(f *feed.Feed) { f.ID = "/en/news/feed.atom" },
			want:   "not an absolute IRI",
		},
		"no title": {
			change: func(f *feed.Feed) { f.Title = " " },
			want:   "no title",
		},
		"relative link": {
			change: func(f *feed.Feed) { f.Link = "/en/news" },
			want:   "link \"/en/news\" is not absolute",
		},
		"no time": {
			change: func(f *feed.Feed) { f.Entries = nil },
			want:   "no updated time",
		},
		"language": {
			change: func(f *feed.Feed) { f.Language = "nl BE" },
			want:   "not a language tag",
		},
		"no author": {
			change: func(f *feed.Feed) { f.Author = "" },
			want:   "entry 0",
		},
		"entry without time": {
			change: func(f *feed.Feed) { f.Entries[1].Published = time.Time{} },
			want:   "no published or updated time",
		},
		"entry without link or content": {
			change: func(f *feed.Feed) { f.Entries[1].Link = "" },
			want:   "no link and no content",
		},
		"entry with relative id": {
			change: func(f *feed.Feed) { f.Entries[0].ID = "news/1" },
			want:   "id is not an absolute IRI",
		},
		"image without type": {
			change: func(f *feed.Feed) { f.Entries[0].Image.Type = "" },
			want:   "image needs",
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			invalid := example()
			test.change(&invalid)

			err := invalid.Validate()
			if !errors.Is(err, feed.ErrInvalid) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate: %v, want %q", err, test.want)
			}

			// An invalid feed is not written.
			var written bytes.Buffer

			if err := feed.Atom.Write(&written, invalid); err == nil || written.Len() > 0 {
				t.Fatalf("written: %v\n%s", err, written.String())
			}
		})
	}

	// A feed without entries has a time of its own.
	empty := example()
	empty.Entries = nil
	empty.Updated = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	if err := empty.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTagURI(t *testing.T) {
	t.Parallel()

	date := time.Date(2026, 10, 7, 23, 30, 0, 0, time.FixedZone("", -2*60*60))

	got := feed.TagURI("Example.COM", date, "news/1")
	if got != "tag:example.com,2026-10-08:news/1" {
		t.Fatal(got)
	}
}

func TestContentType(t *testing.T) {
	t.Parallel()

	if feed.Atom.ContentType() != "application/atom+xml; charset=utf-8" ||
		feed.RSS.ContentType() != "application/rss+xml; charset=utf-8" {
		t.Fatal(feed.Atom.ContentType(), feed.RSS.ContentType())
	}

	if feed.Atom.MediaType() != "application/atom+xml" || feed.RSS.MediaType() != "application/rss+xml" {
		t.Fatal(feed.Atom.MediaType(), feed.RSS.MediaType())
	}
}
