package site

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// The values of Settings.ContentSecurity: whether the site sends a
// Content-Security-Policy that lets browsers run only the scripts and
// styles the site serves itself, which blocks most code injected into its
// pages (cross-site scripting).
const (
	// CSPOff sends no policy, but the frame-ancestors of Framing: the
	// default.
	CSPOff = "off"

	// CSPReportOnly sends the policy as Content-Security-Policy-Report-Only:
	// browsers block nothing and report what the policy would block, which
	// the site logs. The frame-ancestors of Framing stay enforced.
	CSPReportOnly = "report-only"

	// CSPEnforce sends the policy as Content-Security-Policy: browsers
	// block what it does not allow.
	CSPEnforce = "enforce"
)

// ErrCSPSource is a source a Content-Security-Policy may not get from the
// settings, a theme or the configuration (see CheckCSPSources).
var ErrCSPSource = errors.New("invalid_csp_source")

// ContentSecurityPolicy is whether the site sends a Content-Security-Policy:
// ContentSecurity, or CSPOff when it is empty or unknown.
func (s Settings) ContentSecurityPolicy() string {
	switch s.ContentSecurity {
	case CSPReportOnly, CSPEnforce:
		return s.ContentSecurity
	default:
		return CSPOff
	}
}

// ContentSecurityExtra are the sources ContentSecuritySources adds to the
// policy, by directive; lines that are not allowed are left out, which
// only happens to settings saved around Save.
func (s Settings) ContentSecurityExtra() map[string][]string {
	sources := map[string][]string{}

	for _, line := range strings.Split(s.ContentSecuritySources, "\n") {
		directive, list, err := parseCSPLine(line)
		if err != nil || directive == "" {
			continue
		}

		sources[directive] = append(sources[directive], list...)
	}

	return sources
}

// ParseCSPSources parses the text of Settings.ContentSecuritySources: one
// directive per line followed by its sources, such as
//
//	frame-src https://www.youtube-nocookie.com https://player.vimeo.com
//
// Blank lines are skipped. A line that CheckCSPSources refuses is an
// error that wraps ErrCSPSource and quotes the line.
func ParseCSPSources(text string) (map[string][]string, error) {
	sources := map[string][]string{}

	for _, line := range strings.Split(text, "\n") {
		directive, list, err := parseCSPLine(line)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", err, strings.TrimSpace(line))
		}

		if directive == "" {
			continue
		}

		sources[directive] = append(sources[directive], list...)
	}

	return sources, nil
}

// parseCSPLine parses one line of ParseCSPSources; a blank line gives no
// directive.
func parseCSPLine(line string) (string, []string, error) {
	fields := strings.Fields(strings.TrimSuffix(strings.TrimSpace(line), ";"))
	if len(fields) == 0 {
		return "", nil, nil
	}

	directive := strings.ToLower(fields[0])
	sources := fields[1:]

	if len(sources) == 0 {
		return "", nil, ErrCSPSource
	}

	if err := CheckCSPSources(directive, sources); err != nil {
		return "", nil, err
	}

	return directive, normalizeCSPSources(sources), nil
}

// cspDirectives are the directives that may get sources of their own:
// where a page loads things from. The rest, such as default-src,
// object-src, base-uri and frame-ancestors (see Framing), stays as the
// site sets it.
var cspDirectives = []string{
	"connect-src", "font-src", "form-action", "frame-src", "img-src", "manifest-src",
	"media-src", "script-src", "style-src", "worker-src",
}

var (
	// cspHost is a host source: an address with a scheme, a host that may
	// start with *., an optional port and an optional path.
	cspHost = regexp.MustCompile(`(?i)^(https?|wss?)://(\*\.)?[a-z0-9]([a-z0-9-]*[a-z0-9])?` +
		`(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*(:[0-9]{1,5})?(/[A-Za-z0-9._~%!$&()*+=:@/-]*)?$`)
	// cspHash is the hash of an inline script or style.
	cspHash = regexp.MustCompile(`^'sha(256|384|512)-[A-Za-z0-9+/]+={0,2}'$`)
)

// CheckCSPSources reports whether sources may be added to directive of the
// site's Content-Security-Policy, from the settings, a theme (theme.json's
// "csp") or the configuration. The directive must be one that says where
// pages load things from: connect-src, font-src, form-action, frame-src,
// img-src, manifest-src, media-src, script-src, style-src or worker-src.
// Each source must be one of:
//
//   - an address, such as https://www.youtube-nocookie.com or
//     https://*.example.com/embed/, with the scheme http, https, ws or wss;
//   - 'self';
//   - the schemes https:, wss:, data: or blob:, but not for script-src;
//   - the hash of one inline script or style, such as 'sha256-…', for
//     script-src and style-src only.
//
// Everything else is refused with ErrCSPSource, so no setting can open the
// policy up to inline code or eval ('unsafe-inline', 'unsafe-eval'), to
// every site (*) or to a nonce.
func CheckCSPSources(directive string, sources []string) error {
	if !slices.Contains(cspDirectives, directive) {
		return ErrCSPSource
	}

	for _, source := range sources {
		if !cspSourceAllowed(directive, source) {
			return ErrCSPSource
		}
	}

	return nil
}

// cspSourceAllowed reports whether source may be added to directive; see
// CheckCSPSources.
func cspSourceAllowed(directive, source string) bool {
	lower := strings.ToLower(source)

	switch {
	case lower == "'self'":
		return true
	case lower == "https:" || lower == "wss:" || lower == "data:" || lower == "blob:":
		return directive != "script-src"
	case cspHash.MatchString(source):
		return directive == "script-src" || directive == "style-src"
	default:
		return cspHost.MatchString(source)
	}
}

// normalizeCSPSources lowercases the keywords and schemes of sources,
// leaving the addresses, whose paths are case sensitive, and the hashes,
// whose base64 is.
func normalizeCSPSources(sources []string) []string {
	out := make([]string, 0, len(sources))

	for _, source := range sources {
		if strings.HasSuffix(source, ":") || strings.EqualFold(source, "'self'") {
			source = strings.ToLower(source)
		}

		out = append(out, source)
	}

	return out
}

// cleanContentSecurity checks ContentSecurity, empty or one of the CSP
// values, and ContentSecuritySources (see ParseCSPSources).
func cleanContentSecurity(settings *Settings) error {
	switch settings.ContentSecurity {
	case "", CSPOff, CSPReportOnly, CSPEnforce:
	default:
		return ErrInvalid
	}

	if _, err := ParseCSPSources(settings.ContentSecuritySources); err != nil {
		return err
	}

	return nil
}
