package webthings

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/donseba/go-webthings/site"

	"github.com/donseba/go-partial/ext/debug"
	partialerrors "github.com/donseba/go-partial/ext/errors"
	"github.com/donseba/go-router/middleware"
)

// The site's Content-Security-Policy (site.Settings.ContentSecurity, or
// Config.ContentSecurityPolicy) lets browsers run only the scripts and
// styles the site serves itself. It is go-router's middleware.StrictCSP
// with:
//
//   - frame-ancestors from the framing headers (see framing), or 'none' in
//     the admin;
//   - in script-src, the hash of the page's import map (go-importmap's
//     ScriptHash), so the page's {{importmap}} runs;
//   - in style-src, the hash of go-partial's built-in error page, which
//     the interactions client may write into a page (reloadOnError), and in
//     development that of the {{debug}} box;
//   - the origin of the media storage, when it serves the files itself
//     (media.Storage.URL), in img-src and media-src;
//   - the sources of Config.ContentSecuritySources, of the settings
//     (site.Settings.ContentSecuritySources) and of the theme the page
//     renders in (frontend.Theme.CSP);
//   - report-uri, the address browsers report what the policy blocks to
//     (see cspReports).
//
// Every source is a hash or an address, never a nonce, so the output cache
// keeps a page with its policy, and a page it serves again carries the
// hashes it was made with.

// cspHeader and cspReportOnlyHeader are the headers of an enforced and a
// reported policy.
const (
	cspHeader           = "Content-Security-Policy"
	cspReportOnlyHeader = "Content-Security-Policy-Report-Only"
)

// cspReportPath is where browsers report what the policy blocks, or would
// block (the route "csp.report").
const cspReportPath = "/_/csp-report"

// keptPolicy is the mode and the policy of one snapshot of the settings,
// made once for each snapshot.
type keptPolicy struct {
	snapshot *site.Snapshot
	mode     string
	policy   string
}

// checkContentSecurity checks Config.ContentSecurityPolicy and
// Config.ContentSecuritySources at Start.
func (a *App) checkContentSecurity() error {
	switch a.config.ContentSecurityPolicy {
	case "", site.CSPOff, site.CSPReportOnly, site.CSPEnforce:
	default:
		return fmt.Errorf("content security policy %q: use off, report-only or enforce", a.config.ContentSecurityPolicy)
	}

	for directive, sources := range a.config.ContentSecuritySources {
		if err := site.CheckCSPSources(directive, sources); err != nil {
			return fmt.Errorf("content security sources %s %v: %w", directive, sources, err)
		}
	}

	return nil
}

// policy returns the mode and the policy of the request's settings: the
// site setting, or Config.ContentSecurityPolicy, which wins; off when the
// settings cannot be read and the configuration says nothing.
func (a *App) policy(r *http.Request) keptPolicy {
	snapshot, err := a.Site.Snapshot(r.Context())
	if err != nil {
		var none site.Settings

		return keptPolicy{mode: a.contentSecurityMode(none), policy: a.basePolicy(none)}
	}

	kept := a.csp.Load()
	if kept != nil && kept.snapshot == snapshot {
		return *kept
	}

	settings := snapshot.Settings()
	kept = &keptPolicy{snapshot: snapshot, mode: a.contentSecurityMode(settings), policy: a.basePolicy(settings)}
	a.csp.Store(kept)

	return *kept
}

// contentSecurityMode is Config.ContentSecurityPolicy, or else the site
// setting.
func (a *App) contentSecurityMode(settings site.Settings) string {
	if a.config.ContentSecurityPolicy != "" {
		return a.config.ContentSecurityPolicy
	}

	return settings.ContentSecurityPolicy()
}

// basePolicy is the policy of every page and of the admin before the
// sources of a single page are added: see the top of this file.
// frame-ancestors is left to the framing headers.
func (a *App) basePolicy(settings site.Settings) string {
	policy := middleware.ParseCSP(middleware.StrictCSP)
	policy.Delete("frame-ancestors")
	policy.Add("style-src", partialerrors.StyleHash())

	if a.config.Development {
		policy.Add("style-src", debug.StyleHash())
	}

	if a.mediaOrigin != "" {
		policy.Add("img-src", a.mediaOrigin)
		policy.Add("media-src", a.mediaOrigin)
	}

	addSources(&policy, a.config.ContentSecuritySources)
	addSources(&policy, settings.ContentSecurityExtra())
	policy.Set("report-uri", cspReportPath)

	return policy.String()
}

// addSources adds sources by directive to policy, the directives in order
// of their names, so the policy is the same every time.
func addSources(policy *middleware.CSPPolicy, sources map[string][]string) {
	directives := make([]string, 0, len(sources))

	for directive := range sources {
		directives = append(directives, directive)
	}

	slices.Sort(directives)

	for _, directive := range directives {
		policy.Add(directive, sources[directive]...)
	}
}

// storageOrigin is the origin, scheme and host, of the addresses a media
// storage serves its files at itself, such as an object store's CDN; empty
// when the site serves them (see media.Storage.URL).
func storageOrigin(storage interface{ URL(key string) string }) string {
	if storage == nil {
		return ""
	}

	address, err := url.Parse(storage.URL("csp"))
	if err != nil || address.Host == "" || (address.Scheme != "https" && address.Scheme != "http") {
		return ""
	}

	return address.Scheme + "://" + address.Host
}

// contentSecurity returns middleware that sends the policy of the settings
// (see policy), with the sources page adds for each request, or nothing
// when it is off. Framing, inside, merges frame-ancestors into it; the
// admin sets its own. The output cache, outside, keeps the policy with the
// page.
func (a *App) contentSecurity(page func(w http.ResponseWriter, r *http.Request) *http.Request) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		// The policy is set in the header before the middleware runs, so
		// it only lets the page add sources and what is inside merge its
		// directives (see go-router's CSP).
		merged := middleware.CSP("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if page != nil {
				r = page(w, r)
			}

			next.ServeHTTP(w, r)
		}))

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			kept := a.policy(r)

			switch kept.mode {
			case site.CSPEnforce:
				outer, rest := takePolicy(w.Header())
				outer.Override(middleware.ParseCSP(kept.policy))
				setPolicies(w.Header(), outer.String(), rest)
				merged.ServeHTTP(w, r)
			case site.CSPReportOnly:
				outer, rest := takePolicy(w.Header())

				reported := outer.Clone()
				reported.Override(middleware.ParseCSP(kept.policy))
				setPolicies(w.Header(), reported.String(), nil)

				reporting := &reportOnlyWriter{ResponseWriter: w, outer: outer, rest: rest}
				merged.ServeHTTP(reporting, r)
				reporting.finish()
			default:
				next.ServeHTTP(w, r)
			}
		})
	}
}

// sitePolicy adds the sources of a page of the site to its policy: the
// theme's (frontend.Theme.CSP) and the hash of the import map. While
// templates reload, the import map may change between the hash and the
// render, so the map is rendered here, with its hash, and {{importmap}}
// writes that rendering (see importMapHead).
func (a *App) sitePolicy(w http.ResponseWriter, r *http.Request) *http.Request {
	addCSPSources(w, a.Frontend.ThemeCSP(r.Context()))

	im := a.config.ImportMap
	if im == nil {
		return r
	}

	if !a.config.ReloadTemplates {
		if hash := im.ScriptHash(); hash != "" {
			middleware.AddCSPSource(w, "script-src", hash)
		}

		return r
	}

	head, hash, err := im.RenderWithHash()
	if err != nil {
		return r
	}

	if hash != "" {
		middleware.AddCSPSource(w, "script-src", hash)
	}

	return r.WithContext(context.WithValue(r.Context(), importMapKey{}, head))
}

// adminPolicy adds the hash of the admin's import map to the policy of an
// admin response.
func (a *App) adminPolicy(w http.ResponseWriter, r *http.Request) *http.Request {
	if a.backoffice != nil {
		if hash := a.backoffice.ImportMapHash(); hash != "" {
			middleware.AddCSPSource(w, "script-src", hash)
		}
	}

	return r
}

// addCSPSources adds sources by directive to the policy of the response, in
// the order of their names.
func addCSPSources(w http.ResponseWriter, sources map[string][]string) {
	directives := make([]string, 0, len(sources))

	for directive := range sources {
		directives = append(directives, directive)
	}

	slices.Sort(directives)

	for _, directive := range directives {
		middleware.AddCSPSource(w, directive, sources[directive]...)
	}
}

// importMapKey holds the import map a request's policy has the hash of
// (see sitePolicy).
type importMapKey struct{}

// importMapHead is {{importmap}}: the import map whose hash the request's
// policy has, rendered with it, or else the import map as it is now.
func (a *App) importMapHead(ctx context.Context) (template.HTML, error) {
	if head, ok := ctx.Value(importMapKey{}).(template.HTML); ok {
		return head, nil
	}

	return a.config.ImportMap.Render()
}

// reportOnlyWriter sends the policy as Content-Security-Policy-Report-Only
// once it is final, when the response is written: browsers then report
// what it would block and block nothing. They ignore frame-ancestors in a
// reported policy, so that one directive, from the framing headers, stays
// enforced.
type reportOnlyWriter struct {
	http.ResponseWriter
	// outer is the policy set before the app answered, such as by a
	// handler around it, and rest any further policies: they stay
	// enforced, the site's policy is only reported.
	outer middleware.CSPPolicy
	rest  []string
	done  bool
}

// takePolicy removes the policies from header and returns the first, parsed,
// and the others as they were: a policy set around the app is kept, the
// site's directives override only those it names.
func takePolicy(header http.Header) (middleware.CSPPolicy, []string) {
	policies := middleware.SplitCSP(header.Values(cspHeader))
	header.Del(cspHeader)

	if len(policies) == 0 {
		return middleware.CSPPolicy{}, nil
	}

	return middleware.ParseCSP(policies[0]), policies[1:]
}

// setPolicies sets the first policy and adds the others after it.
func setPolicies(header http.Header, first string, rest []string) {
	header.Set(cspHeader, first)

	for _, value := range rest {
		header.Add(cspHeader, value)
	}
}

// finish moves the policy to Content-Security-Policy-Report-Only, once.
func (w *reportOnlyWriter) finish() {
	if w.done {
		return
	}

	w.done = true
	header := w.Header()

	policies := middleware.SplitCSP(header.Values(cspHeader))
	if len(policies) == 0 {
		return
	}

	policy := middleware.ParseCSP(policies[0])
	enforced := policies[1:]

	// The outer policy stays enforced, with the frame-ancestors the site
	// decides (browsers ignore frame-ancestors in a reported policy).
	kept := w.outer.Clone()

	if ancestors, ok := policy.Sources("frame-ancestors"); ok {
		kept.Set("frame-ancestors", ancestors...)
		policy.Delete("frame-ancestors")
	}

	if !kept.IsZero() {
		enforced = append([]string{kept.String()}, enforced...)
	}

	enforced = append(enforced, w.rest...)

	header.Del(cspHeader)

	for _, value := range enforced {
		header.Add(cspHeader, value)
	}

	header.Set(cspReportOnlyHeader, policy.String())
}

func (w *reportOnlyWriter) WriteHeader(status int) {
	if status >= http.StatusOK || status == http.StatusSwitchingProtocols {
		w.finish()
	}

	w.ResponseWriter.WriteHeader(status)
}

func (w *reportOnlyWriter) Write(data []byte) (int, error) {
	w.finish()

	return w.ResponseWriter.Write(data)
}

func (w *reportOnlyWriter) Flush() {
	_ = w.FlushError()
}

// FlushError sends the headers, with the final policy, and lets
// http.ResponseController flush through this writer.
func (w *reportOnlyWriter) FlushError() error {
	w.finish()

	return http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap lets http.ResponseController and go-router's AddCSPSource reach
// the writers below.
func (w *reportOnlyWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// cspReportLimit is the most bytes of one report that are read.
const cspReportLimit = 64 << 10

// cspReports answers the reports browsers send to report-uri: each
// violation is logged as a warning, with the page's path, the directive,
// what was blocked and where, and stored nowhere. Every visitor may send
// some reports a minute (the rest is answered 429), and at most some a
// minute are logged in all, so no one can flood the log. Reports of
// browser extensions, which the site cannot change, are left out.
func cspReports() http.Handler {
	logged := middleware.NewLimiter(60, time.Minute)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, cspReportLimit))
		if err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)

			return
		}

		for _, violation := range parseCSPReports(body) {
			if violation.fromExtension() || !logged.Allow("", time.Now()) {
				continue
			}

			slog.WarnContext(r.Context(), "content security policy violation",
				"disposition", violation.Disposition,
				"directive", violation.Directive,
				"blocked", withoutQuery(violation.Blocked),
				"page", withoutQuery(violation.Page),
				"source", withoutQuery(violation.Source),
				"line", violation.Line,
				"column", violation.Column,
				"sample", truncate(violation.Sample, 80),
			)
		}

		w.WriteHeader(http.StatusNoContent)
	})

	limited := middleware.RateLimit(middleware.RateLimitOptions{Limit: 30, Window: time.Minute})

	return limited(handler)
}

// cspViolation is what a report says about one violation.
type cspViolation struct {
	Disposition string
	Directive   string
	Blocked     string
	Page        string
	Source      string
	Line        int
	Column      int
	Sample      string
}

// fromExtension reports whether a browser extension caused the violation.
func (v cspViolation) fromExtension() bool {
	for _, address := range []string{v.Blocked, v.Source} {
		scheme, _, _ := strings.Cut(address, ":")

		if strings.HasSuffix(scheme, "-extension") {
			return true
		}
	}

	return false
}

// parseCSPReports reads the violations of a report body: report-uri's
// {"csp-report": {…}}, or the Reporting API's list of reports, of which
// those of type csp-violation; nothing for anything else.
func parseCSPReports(body []byte) []cspViolation {
	var legacy struct {
		Report *struct {
			Page               string `json:"document-uri"`
			ViolatedDirective  string `json:"violated-directive"`
			EffectiveDirective string `json:"effective-directive"`
			Disposition        string `json:"disposition"`
			Blocked            string `json:"blocked-uri"`
			Source             string `json:"source-file"`
			Line               int    `json:"line-number"`
			Column             int    `json:"column-number"`
			Sample             string `json:"script-sample"`
		} `json:"csp-report"`
	}

	if err := json.Unmarshal(body, &legacy); err == nil && legacy.Report != nil {
		report := legacy.Report

		return []cspViolation{{
			Disposition: report.Disposition,
			Directive:   cmp.Or(report.EffectiveDirective, report.ViolatedDirective),
			Blocked:     report.Blocked,
			Page:        report.Page,
			Source:      report.Source,
			Line:        report.Line,
			Column:      report.Column,
			Sample:      report.Sample,
		}}
	}

	var reports []struct {
		Type string `json:"type"`
		Body struct {
			Page        string `json:"documentURL"`
			Directive   string `json:"effectiveDirective"`
			Disposition string `json:"disposition"`
			Blocked     string `json:"blockedURL"`
			Source      string `json:"sourceFile"`
			Line        int    `json:"lineNumber"`
			Column      int    `json:"columnNumber"`
			Sample      string `json:"sample"`
		} `json:"body"`
	}

	if err := json.Unmarshal(body, &reports); err != nil {
		return nil
	}

	var violations []cspViolation

	for _, report := range reports {
		if report.Type != "csp-violation" {
			continue
		}

		violations = append(violations, cspViolation{
			Disposition: report.Body.Disposition,
			Directive:   report.Body.Directive,
			Blocked:     report.Body.Blocked,
			Page:        report.Body.Page,
			Source:      report.Body.Source,
			Line:        report.Body.Line,
			Column:      report.Body.Column,
			Sample:      report.Body.Sample,
		})
	}

	return violations
}

// withoutQuery is an address without its query and fragment, which may
// hold what a visitor typed; keywords such as "inline" stay as they are.
func withoutQuery(address string) string {
	address, _, _ = strings.Cut(address, "#")
	address, _, _ = strings.Cut(address, "?")

	return address
}

// truncate shortens text to at most n runes.
func truncate(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}

	return string(runes[:n])
}
