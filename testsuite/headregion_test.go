package testsuite

import (
	"net/url"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/sitetest"
)

// headSwaps are the two out-of-band swaps of the head region of an HTMX
// navigation: the removal of the tags of the page left and the tags of the
// page arrived at, which go before the end marker.
const (
	headDelete = `<div hx-swap-oob="delete:[data-partial-head=&#34;head&#34;]~:not([data-partial-head-end=&#34;head&#34;])` +
		`:not([data-partial-head-end=&#34;head&#34;]~*)"></div>`
	headInsert = `<div hx-swap-oob="beforebegin:[data-partial-head-end=&#34;head&#34;]">`
)

// headTags are the tags body sends for the head region out of band, and
// its title; ok is false when it sends no head region.
func headTags(body string) (title, tags string, ok bool) {
	before, rest, ok := strings.Cut(body, headDelete+headInsert)
	if !ok {
		return "", "", false
	}

	tags, _, ok = strings.Cut(rest, "</div>")
	if !ok {
		return "", "", false
	}

	start := strings.LastIndex(before, "<title>")
	if start >= 0 {
		title = before[start:]
	}

	return title, tags, true
}

// TestHTMXNavigationHead checks that an HTMX navigation replaces the tags
// of the page in <head> with the starting point's head region: the
// title, the description, the canonical and alternate links and the feed
// links, from a page with feeds to one without and back, and that a whole
// page has them between the region's markers.
func TestHTMXNavigationHead(t *testing.T) {
	t.Parallel()

	limits := make(chan int, 100)

	s := sitetest.StartWith(t, sitetest.Options{
		StartingPoints: []string{"site", "ticker"},
	}, ticker("ticker", limits))

	s.MountIn("ticker",
		sitetest.Translation{Locale: "en", Title: "Ticker", Slug: "ticker"},
		sitetest.Translation{Locale: "nl", Title: "Tikker", Slug: "tikker"},
	)

	home, err := s.App.Pages.Home(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	about := s.Submit("/admin/pages/new?locale=en", url.Values{
		"Title":       {"About"},
		"Slug":        {"about"},
		"Parent":      {home},
		"Description": {"Who we are"},
	})
	s.Publish(about + "?locale=en")

	visitor := s.Visitor()
	response, page := visitor.Response("/en/ticker")
	origin := "http://" + response.Request.URL.Host

	tickerTags := []string{
		`<link rel="canonical" href="` + origin + `/en/ticker">`,
		`<link rel="alternate" hreflang="nl" href="` + origin + `/nl/tikker">`,
		`<link rel="alternate" type="application/atom+xml" title="Ticker from Test site" href="` + origin + `/en/ticker/feed.atom">`,
		`<link rel="alternate" type="application/rss+xml" title="Ticker from Test site" href="` + origin + `/en/ticker/feed.rss">`,
	}
	aboutTags := []string{
		`<meta name="description" content="Who we are">`,
		`<link rel="canonical" href="` + origin + `/en/about">`,
		`<link rel="alternate" hreflang="en" href="` + origin + `/en/about">`,
	}

	// A whole page has the title before the region's markers and its other
	// tags between them, in <head>, with no swap.
	head, _, _ := strings.Cut(page, "</head>")
	start := strings.Index(head, `<template data-partial-head="head"></template>`)
	end := strings.Index(head, `<template data-partial-head-end="head"></template>`)

	if start < 0 || end < start || strings.Index(head, "<title>Ticker · Test site</title>") > start ||
		strings.Contains(page, "hx-swap-oob=\"delete") {
		t.Fatalf("whole page: %s", page)
	}

	for _, tag := range tickerTags {
		if at := strings.Index(head, tag); at < start || at > end {
			t.Fatalf("whole page: %s not between the markers: %s", tag, head)
		}
	}

	cases := []struct {
		from, to, title string
		want, gone      []string
	}{
		{"/en/ticker", "/en/about", "<title>About · Test site</title>", aboutTags, tickerTags},
		{"/en/about", "/en/ticker", "<title>Ticker · Test site</title>", tickerTags, aboutTags},
		{"/en/ticker", "/en/ticker/topic/harbour", "<title>Topic harbour · Test site</title>", []string{
			`<link rel="canonical" href="` + origin + `/en/ticker/topic/harbour">`,
			`<link rel="alternate" type="application/rss+xml" title="Topic harbour from Test site" href="` +
				origin + `/en/ticker/topic/harbour/feed.rss">`,
		}, []string{`<meta name="description"`}},
	}

	for _, tc := range cases {
		_, body := navigate(t, visitor, origin+tc.from, tc.to)

		title, tags, ok := headTags(body)
		if !ok || title == "" || !strings.HasPrefix(title, tc.title) || strings.Contains(body, "<template") {
			t.Fatalf("%s to %s: head region: %s", tc.from, tc.to, body)
		}

		for _, tag := range tc.want {
			if !strings.Contains(tags, tag) {
				t.Fatalf("%s to %s: no %s in %s", tc.from, tc.to, tag, tags)
			}
		}

		for _, tag := range tc.gone {
			if strings.Contains(tags, tag) {
				t.Fatalf("%s to %s: %s stays in %s", tc.from, tc.to, tag, tags)
			}
		}

		// Every tag once.
		if strings.Count(tags, `rel="canonical"`) != 1 || strings.Count(body, "<title>") != 1 {
			t.Fatalf("%s to %s: tags %s", tc.from, tc.to, tags)
		}
	}
}

// TestHTMXNavigationWithoutHeadRegion checks that a theme without
// regions/head.gohtml keeps its tags in the layout: a whole page has them
// without markers, and a navigation sends no head region.
func TestHTMXNavigationWithoutHeadRegion(t *testing.T) {
	t.Parallel()

	limits := make(chan int, 100)

	s := sitetest.StartWith(t, sitetest.Options{StartingPoints: []string{"ticker"}}, ticker("ticker", limits))
	s.Mount("ticker", "Ticker", "ticker")

	visitor := s.Visitor()
	response, page := visitor.Response("/en/ticker")
	origin := "http://" + response.Request.URL.Host

	if !strings.Contains(page, `<head><title>Ticker · Test site</title><link rel="alternate" type="application/atom+xml"`) ||
		strings.Contains(page, "data-partial-head") {
		t.Fatalf("whole page: %s", page)
	}

	_, body := navigate(t, visitor, origin+"/en/", "/en/ticker")
	if strings.Contains(body, "data-partial-head") || strings.Contains(body, "<link") {
		t.Fatalf("navigation: %s", body)
	}
}
