package frontend

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestStructuredItems checks that module items lose their empty values,
// get absolute addresses in the properties that hold them, and that an
// item with nothing left is dropped.
func TestStructuredItems(t *testing.T) {
	abs := func(link string) string {
		return absolute(link, "https://example.test", nil, "/en/news/post")
	}

	item := resolveMap(Thing{
		"@type":     "Article",
		"headline":  "Hello",
		"url":       "/en/news/hello",
		"image":     "",
		"sameAs":    []string{"https://social.example/a", ""},
		"author":    Thing{"@id": OrganizationID},
		"about":     []Thing{{"@type": "Thing", "name": ""}},
		"dateZero":  time.Time{},
		"name":      "/not/an/address",
		"publisher": Thing{"@type": "Organization", "logo": "/media/logo.png"},
	}, abs)

	got, err := json.Marshal(compact(item))
	if err != nil {
		t.Fatal(err)
	}

	want := `{"@type":"Article","author":{"@id":"https://example.test/#organization"},"headline":"Hello",` +
		`"name":"/not/an/address","publisher":{"@type":"Organization","logo":"https://example.test/media/logo.png"},` +
		`"sameAs":["https://social.example/a"],"url":"https://example.test/en/news/hello"}`
	if string(got) != want {
		t.Fatalf("item:\n%s\nwant\n%s", got, want)
	}

	for _, empty := range []Thing{nil, {}, {"@type": "Organization", "name": " "}, {"name": "No type"}} {
		if out := compact(empty); out != nil {
			t.Fatalf("%v gives %v", empty, out)
		}
	}
}

// TestStructuredScript checks that html/template writes the graph as JSON
// that no text in it can end the script.
func TestStructuredScript(t *testing.T) {
	var b strings.Builder

	graph := []Thing{{"@type": "Article", "headline": `</script><script>alert("x")</script> & <!--`}}

	err := ldScript.Execute(&b, Thing{"@context": "https://schema.org", "@graph": graph})
	if err != nil {
		t.Fatal(err)
	}

	script := b.String()

	inner, ok := strings.CutPrefix(script, `<script type="application/ld+json">`)
	if !ok || strings.Count(script, "</script>") != 1 || strings.Contains(script, "<!--") {
		t.Fatalf("script: %s", script)
	}

	var data map[string]any

	if err := json.Unmarshal([]byte(strings.TrimSuffix(inner, "</script>")), &data); err != nil {
		t.Fatalf("%v: %s", err, script)
	}

	headline := data["@graph"].([]any)[0].(map[string]any)["headline"]
	if headline != `</script><script>alert("x")</script> & <!--` {
		t.Fatalf("headline: %v", headline)
	}
}

// TestItemList checks that an ItemList numbers the addresses it is given
// from 1, leaving out empty ones, that they are made absolute, and that a
// list without addresses adds nothing.
func TestItemList(t *testing.T) {
	if list := ItemList("News", "", ""); list != nil {
		t.Fatalf("empty list: %v", list)
	}

	abs := func(link string) string {
		return absolute(link, "https://example.test", nil, "/en/news")
	}

	list := resolveMap(ItemList("News", "/en/news/a", "", "/en/news/b"), abs)

	encoded, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}

	want := `{"@type":"ItemList","itemListElement":[` +
		`{"@type":"ListItem","position":1,"url":"https://example.test/en/news/a"},` +
		`{"@type":"ListItem","position":2,"url":"https://example.test/en/news/b"}],` +
		`"name":"News","numberOfItems":2}`
	if string(encoded) != want {
		t.Fatalf("list: %s", encoded)
	}
}
