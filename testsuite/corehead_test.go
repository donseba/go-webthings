package testsuite

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/sitetest"
)

// coreHeadTheme is a theme whose layout places {{slot "head"}} and links
// its stylesheets with {{styles}}, with files added or replaced by extra.
func coreHeadTheme(t *testing.T, extra map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	files := map[string]string{
		"layout.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.View */}}` +
			`<html><head><meta charset="utf-8">{{slot "head"}}{{styles "webthings.css" "site.css" "site.css"}}</head>` +
			`<body>{{slot "languages"}}{{content}}</body></html>`,
		"regions/languages.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.View */}}` +
			`<nav id="languages" {{oobAttr}}>{{range .Languages}}<a href="{{.URL}}">{{.Locale}}</a>{{end}}</nav>`,
		"page.gohtml":     `{{/* @dot github.com/donseba/go-webthings/frontend.Page */}}<main id="content"><h1>{{.Title}}</h1></main>`,
		"404.gohtml":      `{{/* @dot github.com/donseba/go-webthings/frontend.NotFound */}}<main id="content">Not found</main>`,
		"assets/site.css": `body { color: var(--accent); }`,
		"theme.json": `{"settings": [{"name": "accent", "label": "Accent", "type": "color", ` +
			`"default": "#336699", "css": "--accent"}]}`,
	}

	for name, source := range extra {
		files[name] = source
	}

	writeFiles(t, dir, files)

	return dir
}

// coreHeadSite starts a site in the theme of dir, in development, so a
// problem of a template, also of core's, stops its start, with an About
// page in English and Dutch below the home page.
func coreHeadSite(t *testing.T, dir string) *sitetest.Site {
	t.Helper()

	s := sitetest.StartWith(t, sitetest.Options{
		Templates: dir,
		Configure: func(config *webthings.Config) {
			config.Development = true
		},
	})

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

	s.Submit(about+"?locale=nl", url.Values{"Title": {"Over ons"}, "Slug": {"over-ons"}, "Parent": {home}})
	s.Publish(about + "?locale=nl")

	return s
}

// setSiteTexts saves the site's English SEO title, SEO description and
// tagline.
func setSiteTexts(t *testing.T, s *sitetest.Site, seoTitle, seoDescription, tagline string) {
	t.Helper()

	settings, err := s.App.Site.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	text := settings.Text("en")
	text.SEOTitle = seoTitle
	text.SEODescription = seoDescription
	text.Tagline = tagline
	settings.Translations = settings.WithText(text)

	if err := s.App.Site.Save(t.Context(), settings); err != nil {
		t.Fatal(err)
	}
}

// TestCoreHeadRegion checks core's head region in a theme without one of
// its own: the title, the description, the canonical and alternate links
// and the sharing tags, between the region's markers; the SEO title on
// the home page only; the description's fallbacks; no address of its own
// on a page that is not found; and the tags of the page arrived at with
// an HTMX navigation.
func TestCoreHeadRegion(t *testing.T) {
	t.Parallel()

	s := coreHeadSite(t, coreHeadTheme(t, nil))
	visitor := s.Visitor()

	response, page := visitor.Response("/en/about")
	origin := "http://" + response.Request.URL.Host

	head, _, _ := strings.Cut(page, "</head>")
	start := strings.Index(head, `<template data-partial-head="head"></template>`)
	end := strings.Index(head, `<template data-partial-head-end="head"></template>`)

	if start < 0 || end < start || !strings.Contains(head[:start], "<title>About · Test site</title>") {
		t.Fatalf("whole page: %s", head)
	}

	for _, tag := range []string{
		`<meta name="description" content="Who we are">`,
		`<link rel="canonical" href="` + origin + `/en/about">`,
		`<link rel="alternate" hreflang="en" href="` + origin + `/en/about">`,
		`<link rel="alternate" hreflang="nl" href="` + origin + `/nl/over-ons">`,
		`<link rel="alternate" hreflang="x-default" href="` + origin + `/en/about">`,
		`<meta property="og:title" content="About">`,
		`<meta property="og:description" content="Who we are">`,
		`<meta property="og:type" content="website">`,
		`<meta property="og:url" content="` + origin + `/en/about">`,
		`<meta property="og:site_name" content="Test site">`,
		`<meta property="og:locale" content="en">`,
		`<meta name="twitter:card" content="summary">`,
		`<script type="application/ld+json">`,
	} {
		if at := strings.Index(head, tag); at < start || at > end {
			t.Fatalf("no %s between the markers: %s", tag, head)
		}
	}

	// The SEO title is the home page's whole title; the SEO description
	// is the description of a page without one, else the tagline.
	setSiteTexts(t, s, "Harbour studio in the north", "All about the harbour", "Clay and fire")

	visitor.Expect("/en/", http.StatusOK,
		"<title>Harbour studio in the north</title>",
		`<meta property="og:title" content="Harbour studio in the north">`,
		`<meta name="description" content="All about the harbour">`,
		`<meta property="og:description" content="All about the harbour">`)
	visitor.Expect(
		"/en/about",
		http.StatusOK,
		"<title>About · Test site</title>",
		`<meta name="description" content="Who we are">`,
	)
	visitor.ExpectMissing("/en/about", "Harbour studio")

	setSiteTexts(t, s, "", "", "Clay and fire")
	visitor.Expect("/en/", http.StatusOK, "<title>Home · Test site</title>", `<meta name="description" content="Clay and fire">`)

	// A page that is not found has no address of its own to give.
	status, missing := visitor.Get("/en/nowhere")
	if status != http.StatusNotFound || !strings.Contains(missing, "<title>Not found · Test site</title>") {
		t.Fatalf("not found: %d %s", status, missing)
	}

	for _, tag := range []string{`rel="canonical"`, `hreflang=`, `og:url`, `application/ld+json`} {
		if strings.Contains(missing, tag) {
			t.Fatalf("not found has %s: %s", tag, missing)
		}
	}

	// An HTMX navigation replaces the tags with those of the page arrived
	// at, and the title.
	_, body := navigate(t, visitor, origin+"/en/about", "/en/")

	title, tags, ok := headTags(body)
	if !ok || !strings.HasPrefix(title, "<title>Home · Test site</title>") ||
		!strings.Contains(tags, `<link rel="canonical" href="`+origin+`/en/">`) || strings.Contains(tags, "/en/about") {
		t.Fatalf("navigation: %s", body)
	}
}

// TestCoreHeadExtended checks that a theme's own head region that calls
// core's tags gets them and its own, each once, and that one that does
// not replaces them.
func TestCoreHeadExtended(t *testing.T) {
	t.Parallel()

	extended := coreHeadSite(t, coreHeadTheme(t, map[string]string{
		"regions/head.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.View */}}` +
			`{{template "webthings/head-tags.gohtml" .}}<meta name="theme-color" content="#123456">`,
	}))

	_, page := extended.Visitor().Get("/en/about")
	head, _, _ := strings.Cut(page, "</head>")

	if strings.Count(head, "<title>About · Test site</title>") != 1 || strings.Count(head, `rel="canonical"`) != 1 ||
		strings.Count(head, `<meta property="og:title" content="About">`) != 1 ||
		!strings.Contains(head, `<meta name="theme-color" content="#123456">`) {
		t.Fatalf("extended: %s", head)
	}

	replaced := coreHeadSite(t, coreHeadTheme(t, map[string]string{
		"regions/head.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.View */}}<title>{{.Title}}</title>`,
	}))

	_, page = replaced.Visitor().Get("/en/about")
	head, _, _ = strings.Cut(page, "</head>")

	if !strings.Contains(head, "<title>About</title>") || strings.Contains(head, "og:title") {
		t.Fatalf("replaced: %s", head)
	}
}

// TestCoreHeadWithoutSlot checks that a layout without {{slot "head"}},
// which writes its own tags, gets no head region from core: not on a
// whole page and not with an HTMX navigation.
func TestCoreHeadWithoutSlot(t *testing.T) {
	t.Parallel()

	s := coreHeadSite(t, coreHeadTheme(t, map[string]string{
		"layout.gohtml": `{{/* @dot github.com/donseba/go-webthings/frontend.View */}}` +
			`<html><head><title>{{.Title}}</title>{{/* slot "head" */}}</head><body>{{slot "languages"}}{{content}}</body></html>`,
	}))
	visitor := s.Visitor()

	response, page := visitor.Response("/en/about")
	origin := "http://" + response.Request.URL.Host

	if !strings.Contains(page, "<head><title>About</title></head>") || strings.Contains(page, "og:title") {
		t.Fatalf("whole page: %s", page)
	}

	_, body := navigate(t, visitor, origin+"/en/about", "/en/")
	if strings.Contains(body, "data-partial-head") || strings.Contains(body, "<title>") {
		t.Fatalf("navigation: %s", body)
	}
}

// stylesheetPattern finds the stylesheet links of a page.
var stylesheetPattern = regexp.MustCompile(
	`<link rel="stylesheet" href="([^"?]+)\?v=[0-9a-f]+" integrity="sha384-[^"]+" crossorigin="anonymous">`,
)

// TestStyles checks {{styles}}: partial-errors.css first, then the files
// in the order given, core's from core and the theme's from the theme,
// each once, then the values stylesheet, all versioned with their
// integrity.
func TestStyles(t *testing.T) {
	t.Parallel()

	s := coreHeadSite(t, coreHeadTheme(t, nil))

	_, page := s.Visitor().Get("/en/about")

	var links []string
	for _, match := range stylesheetPattern.FindAllStringSubmatch(page, -1) {
		links = append(links, match[1])
	}

	want := []string{"/_/partial-errors.css", "/_/webthings.css", "/theme/default/site.css", "/theme/default/values.css"}
	if strings.Join(links, " ") != strings.Join(want, " ") || strings.Count(page, `rel="stylesheet"`) != len(want) {
		t.Fatalf("stylesheets %v, want %v: %s", links, want, page)
	}

	if strings.Contains(page, "<style") || strings.Contains(page, ` style="`) {
		t.Fatalf("inline style: %s", page)
	}
}
