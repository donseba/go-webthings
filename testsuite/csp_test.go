package testsuite

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/modules/polls"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/modules/stats"
	"github.com/donseba/go-webthings/site"
	"github.com/donseba/go-webthings/sitetest"

	"github.com/donseba/go-partial/ext/debug"
	partialerrors "github.com/donseba/go-partial/ext/errors"
)

// errorPageHash is the hash of go-partial's built-in error page's style,
// which every policy allows (see partialerrors.StyleHash).
var errorPageHash = partialerrors.StyleHash()

// importMapScript finds the text of an import map script.
var importMapScript = regexp.MustCompile(`(?s)<script type="importmap"[^>]*>(.*?)</script>`)

// importMapHash is the policy hash of the import map in body, computed
// here, apart from go-importmap.
func importMapHash(t *testing.T, body string) string {
	t.Helper()

	found := importMapScript.FindStringSubmatch(body)
	if found == nil {
		t.Fatalf("no import map in:\n%s", body)
	}

	sum := sha256.Sum256([]byte(found[1]))

	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// sitePolicy is the policy of a page of the site whose import map has hash,
// with frame-ancestors from the framing headers, or without when
// ancestors is empty, and the style-src hashes of go-partial.
func sitePolicy(hash, ancestors string, styles ...string) string {
	policy := "default-src 'self'; script-src 'self' " + hash + "; style-src 'self' " +
		strings.Join(append([]string{errorPageHash}, styles...), " ") +
		"; img-src 'self' data:; object-src 'none'; base-uri 'self'; form-action 'self'; report-uri /_/csp-report"

	if ancestors != "" {
		policy += "; frame-ancestors " + ancestors
	}

	return policy
}

// cspFiles is a theme with an import map in its layout.
func cspFiles() fstest.MapFS {
	return fstest.MapFS{
		"layout.gohtml": {Data: []byte(`<!doctype html><html><head>{{importmap}}</head><body>{{content}}</body></html>`)},
		"page.gohtml":   {Data: []byte(`<main id="content">{{.Title}}</main>`)},
		"404.gohtml":    {Data: []byte(`<main id="content">Not found</main>`)},
	}
}

// cspSite starts a site whose policy is mode by the configuration, or the
// site setting when mode is empty.
func cspSite(t *testing.T, mode string, configure func(config *webthings.Config)) *sitetest.Site {
	t.Helper()

	return sitetest.StartWith(t, sitetest.Options{
		Files: cspFiles(),
		Configure: func(config *webthings.Config) {
			config.ImportMap = siteImportMap(t)
			config.ContentSecurityPolicy = mode

			if configure != nil {
				configure(config)
			}
		},
	})
}

// TestContentSecurityPolicyModes checks the header of each mode of
// Config.ContentSecurityPolicy: off sends only the frame-ancestors of the
// framing headers, enforce the whole policy, with the hash of the page's
// import map, and report-only the policy as
// Content-Security-Policy-Report-Only, keeping frame-ancestors enforced,
// which browsers ignore in a reported policy. No policy allows inline code
// or eval.
func TestContentSecurityPolicyModes(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{site.CSPOff, site.CSPReportOnly, site.CSPEnforce} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			s := cspSite(t, mode, nil)

			for _, path := range []string{"/en/", "/en/missing"} {
				response, body := s.Visitor().Response(path)
				hash := importMapHash(t, body)
				enforced := response.Header.Get("Content-Security-Policy")
				reported := response.Header.Get("Content-Security-Policy-Report-Only")

				want := map[string][2]string{
					site.CSPOff:        {"frame-ancestors 'self'", ""},
					site.CSPReportOnly: {"frame-ancestors 'self'", sitePolicy(hash, "")},
					site.CSPEnforce:    {sitePolicy(hash, "'self'"), ""},
				}[mode]

				if enforced != want[0] || reported != want[1] {
					t.Fatalf(
						"%s: Content-Security-Policy %q, Report-Only %q, want %q and %q",
						path,
						enforced,
						reported,
						want[0],
						want[1],
					)
				}

				if strings.Contains(enforced+reported, "unsafe") {
					t.Fatalf("%s: the policy allows unsafe sources: %q %q", path, enforced, reported)
				}

				if response.Header.Get("X-Frame-Options") != "SAMEORIGIN" {
					t.Fatalf("%s: X-Frame-Options %q", path, response.Header.Get("X-Frame-Options"))
				}
			}
		})
	}
}

// TestContentSecurityPolicyCached checks that a page the output cache
// serves again carries the policy it was made with, with the hash of its
// import map, and that templates that reload keep the hash and the
// rendered import map together.
func TestContentSecurityPolicyCached(t *testing.T) {
	t.Parallel()

	s := cspSite(t, site.CSPEnforce, func(config *webthings.Config) {
		config.Development = true
		config.OutputCacheTTL = time.Minute
	})

	var policies []string

	for _, want := range []string{"miss", "hit"} {
		response, body := s.Visitor().Response("/en/")
		if cache := response.Header.Get("X-Cache"); cache != want {
			t.Fatalf("X-Cache %q, want %q", cache, want)
		}

		policy := response.Header.Get("Content-Security-Policy")
		if policy != sitePolicy(importMapHash(t, body), "'self'", debug.StyleHash()) {
			t.Fatalf("%s: policy %q does not match the page:\n%s", want, policy, body)
		}

		policies = append(policies, policy)
	}

	if policies[0] != policies[1] {
		t.Fatalf("the hit has another policy: %q, %q", policies[0], policies[1])
	}

	reloading := sitetest.StartWith(t, sitetest.Options{
		Files: cspFiles(),
		Configure: func(config *webthings.Config) {
			config.Assets = fstest.MapFS{"site.js": {Data: []byte(`console.log("site");`)}}
			config.ReloadTemplates = true
			config.ContentSecurityPolicy = site.CSPEnforce
		},
	})

	response, body := reloading.Visitor().Response("/en/")
	if policy := response.Header.Get("Content-Security-Policy"); !strings.Contains(policy, importMapHash(t, body)) {
		t.Fatalf("reloading: policy %q lacks the hash of the import map:\n%s", policy, body)
	}
}

// TestContentSecurityPolicySetting checks the Security card: the policy
// follows the setting, frame-ancestors follows Framing, and only sources
// site.CheckCSPSources allows are saved.
func TestContentSecurityPolicySetting(t *testing.T) {
	t.Parallel()

	s := cspSite(t, "", nil)

	s.Expect("/admin/site", http.StatusOK, "Content-Security-Policy", "Report only, block nothing", "Outside sources")

	security := func(values url.Values) int {
		t.Helper()

		status, _ := s.PostStatus("/admin/site", "/admin/site/security?locale=en", values)

		return status
	}

	if status := security(url.Values{"Framing": {site.FramingSameOrigin}, "ContentSecurity": {"strict"}}); status !=
		http.StatusUnprocessableEntity {
		t.Fatalf("an unknown mode was saved: %d", status)
	}

	for _, sources := range []string{
		"script-src 'unsafe-inline'",
		"style-src 'unsafe-eval'",
		"img-src *",
		"frame-ancestors https://example.com",
		"script-src https:",
		"frame-src",
		"frame-src https://example.com; script-src 'self'",
		"script-src 'nonce-abc'",
	} {
		values := url.Values{
			"Framing":         {site.FramingSameOrigin},
			"ContentSecurity": {site.CSPEnforce},
			"Sources":         {sources},
		}

		if status := security(values); status != http.StatusUnprocessableEntity {
			t.Fatalf("sources %q were saved: %d", sources, status)
		}
	}

	if settings, _ := s.App.Site.Settings(t.Context()); settings.ContentSecurityPolicy() != site.CSPOff {
		t.Fatalf("a refused form changed the policy: %q", settings.ContentSecurity)
	}

	if status := security(url.Values{
		"Framing":         {site.FramingNone},
		"ContentSecurity": {site.CSPEnforce},
		"Sources":         {"frame-src https://www.youtube-nocookie.com\r\nimg-src https://images.example.com"},
	}); status != http.StatusOK {
		t.Fatalf("security: %d", status)
	}

	response, body := s.Visitor().Response("/en/")
	policy := response.Header.Get("Content-Security-Policy")
	want := strings.Replace(sitePolicy(importMapHash(t, body), "'none'"), "img-src 'self' data:",
		"img-src 'self' data: https://images.example.com", 1)
	want = strings.Replace(want, "; report-uri", "; frame-src 'self' https://www.youtube-nocookie.com; report-uri", 1)

	if policy != want || response.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("policy %q, want %q", policy, want)
	}

	// Any site may frame the pages: the policy has no frame-ancestors.
	security(url.Values{"Framing": {site.FramingAny}, "ContentSecurity": {site.CSPReportOnly}})

	response, body = s.Visitor().Response("/en/")
	if enforced, reported := response.Header.Get("Content-Security-Policy"),
		response.Header.Get("Content-Security-Policy-Report-Only"); enforced != "" ||
		reported != sitePolicy(importMapHash(t, body), "") {
		t.Fatalf("framed by any site: %q and report-only %q", enforced, reported)
	}

	dutchAdmin(t).Expect("/admin/site", http.StatusOK, "Alleen rapporteren, niets tegenhouden", "Externe bronnen")
}

// TestContentSecurityPolicyConfig checks that Start refuses sources and
// modes it does not allow, and adds those it does, with the theme's.
func TestContentSecurityPolicyConfig(t *testing.T) {
	t.Parallel()

	for _, config := range []webthings.Config{
		{ContentSecurityPolicy: "on"},
		{ContentSecuritySources: map[string][]string{"script-src": {"'unsafe-inline'"}}},
		{ContentSecuritySources: map[string][]string{"default-src": {"https://example.com"}}},
	} {
		config.StateDir = t.TempDir()
		config.Templates = cspFiles()

		app := webthings.New(config)
		if err := app.Start(t.Context()); err == nil {
			_ = app.Close()

			t.Fatalf("started with %q %v", config.ContentSecurityPolicy, config.ContentSecuritySources)
		}
	}

	files := cspFiles()
	files["layout.gohtml"] = &fstest.MapFile{Data: []byte(`<!doctype html><html><body>{{content}}</body></html>`)}
	files["theme.json"] = &fstest.MapFile{Data: []byte(`{"csp": {"font-src": ["https://fonts.example.com"]}}`)}

	s := sitetest.StartWith(t, sitetest.Options{
		Files: files,
		Configure: func(config *webthings.Config) {
			config.ContentSecurityPolicy = site.CSPEnforce
			config.ContentSecuritySources = map[string][]string{"connect-src": {"https://api.example.com"}}
		},
	})

	response, _ := s.Visitor().Response("/en/")
	policy := response.Header.Get("Content-Security-Policy")

	// go-partial's stylesheet of the error box, which has classes only, is
	// one of core's files.
	if css := request(t, s.App, http.MethodGet, "/_/partial-errors.css"); css.Code != http.StatusOK ||
		!strings.Contains(css.Body.String(), ".go-partial-error__title") {
		t.Fatalf("partial-errors.css: %d %s", css.Code, css.Body)
	}

	for _, want := range []string{"font-src 'self' https://fonts.example.com", "connect-src 'self' https://api.example.com"} {
		if !strings.Contains(policy, want) {
			t.Fatalf("policy %q lacks %q", policy, want)
		}
	}
}

// TestContentSecurityPolicyAdmin checks the admin's policy: the site's,
// with the hash of the admin's own import map, and frame-ancestors 'none'
// in every mode, enforced also when the rest is only reported.
func TestContentSecurityPolicyAdmin(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{site.CSPOff, site.CSPReportOnly, site.CSPEnforce} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			s := cspSite(t, mode, nil)

			for _, browser := range []*sitetest.Site{s.Visitor(), s} {
				response, body := browser.Response("/admin/login")
				if browser == s {
					response, body = browser.Response("/admin/")
				}

				hash := importMapHash(t, body)
				policy := sitePolicy(hash, "")
				want := map[string][2]string{
					site.CSPOff:        {"frame-ancestors 'none'", ""},
					site.CSPReportOnly: {"frame-ancestors 'none'", policy},
					site.CSPEnforce:    {policy + "; frame-ancestors 'none'", ""},
				}[mode]

				enforced := response.Header.Get("Content-Security-Policy")
				reported := response.Header.Get("Content-Security-Policy-Report-Only")

				if enforced != want[0] || reported != want[1] || response.Header.Get("X-Frame-Options") != "DENY" {
					t.Fatalf("%s: Content-Security-Policy %q, Report-Only %q, want %q and %q",
						response.Request.URL.Path, enforced, reported, want[0], want[1])
				}
			}

			if mode == site.CSPOff {
				// Without a policy the preview adds no sources: they would
				// make directives that allow only them, and block the theme.
				response, _ := s.Response("/admin/theme/preview/default")
				if policy := response.Header.Get("Content-Security-Policy"); policy != "frame-ancestors 'none'" {
					t.Fatalf("preview without a policy: Content-Security-Policy %q", policy)
				}
			}

			if mode != site.CSPEnforce {
				return
			}

			// A theme's preview is a page of the site in the admin: its
			// policy also allows the site's import map.
			_, admin := s.Response("/admin/")
			response, preview := s.Response("/admin/theme/preview/default")

			policy := response.Header.Get("Content-Security-Policy")
			if !strings.Contains(policy, importMapHash(t, preview)) || !strings.Contains(policy, importMapHash(t, admin)) {
				t.Fatalf("preview: policy %q lacks the hash of the site's or the admin's import map", policy)
			}
		})
	}
}

// inlineScript finds a <script> element without src that a browser runs:
// any but the import map, whose hash the policy has, and data blocks such
// as application/json and application/ld+json.
var inlineScript = regexp.MustCompile(`(?i)<script(?:\s+[^>]*)?>`)

// runsInline reports whether the script tag runs inline code.
func runsInline(tag string) bool {
	lower := strings.ToLower(tag)

	switch {
	case strings.Contains(lower, " src="):
		return false
	case strings.Contains(lower, `type="importmap"`), strings.Contains(lower, `type="application/json"`),
		strings.Contains(lower, `type="application/ld+json"`):
		return false
	default:
		return true
	}
}

// inlineHandler finds event handler attributes, such as onclick, and
// htmx's hx-on, which need 'unsafe-inline' or eval.
var inlineHandler = regexp.MustCompile(`(?i)\s(on[a-z]+|hx-on[:a-z-]*)=`)

// TestAdminWithoutInlineCode signs in, visits the admin's pages, with
// content, an image, statistics and a second editor on the page, and fails
// for a style attribute, a <style> element, an inline script or an event
// handler attribute on any of them: a strict Content-Security-Policy
// blocks them all.
func TestAdminWithoutInlineCode(t *testing.T) {
	t.Parallel()

	s := sitetest.StartWith(t, sitetest.Options{
		StartingPoints: []string{"news", "survey"},
		Configure: func(config *webthings.Config) {
			config.ContentSecurityPolicy = site.CSPEnforce
		},
	},
		publishing.Module("news", "News"),
		polls.Module("survey", "Polls"),
		stats.Module(),
	)

	s.Mount("news", "News", "news")
	photo := s.Upload("harbour.png", sitetest.PNG(t, 1600, 900))
	post := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title": {"Hello"}, "Slug": {"hello"}, "Date": {"2026-10-01"}, "Body": {"Boats"}, "Image": {photo.ID},
	})
	s.Publish(post + "?locale=en")
	s.Visitor().Get("/en/news/hello")

	links := regexp.MustCompile(`href="(/admin/[^"#]*)"`)
	checked := map[string]bool{}
	queue := []string{"/admin/", "/admin/media/" + photo.ID + "?locale=en", "/admin/stats", post + "?locale=en"}

	for len(queue) > 0 && len(checked) < 150 {
		address := queue[0]
		queue = queue[1:]

		if checked[address] || strings.Contains(address, "/assets/") || strings.Contains(address, "/logout") {
			continue
		}

		checked[address] = true

		response, body := s.Response(address)
		if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/html") {
			continue
		}

		if found := inlineStyle.FindAllString(body, -1); found != nil {
			t.Errorf("%s (%d) has inline styles %q", address, response.StatusCode, found)
		}

		for _, tag := range inlineScript.FindAllString(body, -1) {
			if runsInline(tag) {
				t.Errorf("%s (%d) has an inline script %s", address, response.StatusCode, tag)
			}
		}

		if found := inlineHandler.FindAllString(body, -1); found != nil {
			t.Errorf("%s (%d) has event handler attributes %q", address, response.StatusCode, found)
		}

		if policy := response.Header.Get("Content-Security-Policy"); strings.Contains(body, `type="importmap"`) &&
			!strings.Contains(policy, importMapHash(t, body)) {
			t.Errorf("%s: policy %q lacks the hash of the import map", address, policy)
		}

		for _, link := range links.FindAllStringSubmatch(body, -1) {
			queue = append(queue, strings.ReplaceAll(link[1], "&amp;", "&"))
		}
	}

	for _, want := range []string{"/admin/", "/admin/stats", "/admin/media/" + photo.ID + "?locale=en", "/admin/site", "/admin/theme"} {
		if !checked[want] {
			t.Errorf("%s was not checked; checked %v", want, checked)
		}
	}
}

// TestCSPReports checks the address browsers report violations to: it
// answers 204 to reports, of both formats, and to anything else posted,
// and 429 to a visitor who sends too many.
func TestCSPReports(t *testing.T) {
	t.Parallel()

	s := cspSite(t, site.CSPReportOnly, nil)

	reports := []struct {
		contentType string
		body        string
	}{
		{"application/csp-report", `{"csp-report": {"document-uri": "http://example.test/en/?q=secret", ` +
			`"effective-directive": "style-src-attr", "blocked-uri": "inline", "disposition": "report"}}`},
		{"application/reports+json", `[{"type": "csp-violation", "body": {"documentURL": "http://example.test/en/", ` +
			`"effectiveDirective": "script-src-elem", "blockedURL": "inline", "disposition": "enforce"}}]`},
		{"application/csp-report", `{"csp-report": {"blocked-uri": "chrome-extension://abc/x.js"}}`},
		{"text/plain", `not json`},
	}

	for _, report := range reports {
		r := httptest.NewRequest(http.MethodPost, "/_/csp-report", strings.NewReader(report.body))
		r.Header.Set("Content-Type", report.contentType)

		response := httptest.NewRecorder()
		s.App.ServeHTTP(response, r)

		if response.Code != http.StatusNoContent {
			t.Fatalf("%s: %d", report.body, response.Code)
		}
	}

	// Only posts are reports; a GET finds the site's 404.
	if response := request(t, s.App, http.MethodGet, "/_/csp-report"); response.Code != http.StatusNotFound {
		t.Fatalf("GET: %d", response.Code)
	}

	limited := false

	for range 40 {
		response := request(t, s.App, http.MethodPost, "/_/csp-report")
		if response.Code == http.StatusTooManyRequests {
			limited = true

			break
		}
	}

	if !limited {
		t.Fatal("reports are not limited")
	}
}

// TestContentSecurityPolicyOuter checks that a policy a handler around the
// app set before it answered is kept: enforce overrides only the directives
// the site's policy names, report-only keeps the outer policy enforced, with
// the site's frame-ancestors, and reports the merged one.
func TestContentSecurityPolicyOuter(t *testing.T) {
	t.Parallel()

	const outer = "upgrade-insecure-requests; frame-ancestors https://partner.example; script-src 'self' 'unsafe-inline'"

	for _, mode := range []string{site.CSPReportOnly, site.CSPEnforce} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			s := cspSite(t, mode, nil)

			around := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Security-Policy", outer)
				s.App.ServeHTTP(w, r)
			})

			recorder := httptest.NewRecorder()
			around.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/en/", nil))

			enforced := recorder.Header().Get("Content-Security-Policy")
			reported := recorder.Header().Get("Content-Security-Policy-Report-Only")

			if mode == site.CSPEnforce {
				if !strings.Contains(enforced, "upgrade-insecure-requests") ||
					strings.Contains(enforced, "unsafe-inline") ||
					!strings.Contains(enforced, "frame-ancestors 'self'") {
					t.Fatalf("enforce: %q keeps not the outer directive, or lets it weaken the site's", enforced)
				}

				return
			}

			if enforced != "upgrade-insecure-requests; frame-ancestors 'self'; script-src 'self' 'unsafe-inline'" {
				t.Fatalf("report-only: enforced %q, want the outer policy with the site's frame-ancestors", enforced)
			}

			if !strings.Contains(reported, "upgrade-insecure-requests") || strings.Contains(reported, "unsafe-inline") ||
				strings.Contains(reported, "frame-ancestors") {
				t.Fatalf("report-only: reported %q", reported)
			}
		})
	}
}
