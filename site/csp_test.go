package site_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/donseba/go-webthings/site"
)

// TestCheckCSPSources checks which sources the settings, a theme and the
// configuration may add to the Content-Security-Policy.
func TestCheckCSPSources(t *testing.T) {
	t.Parallel()

	allowed := map[string][]string{
		"frame-src":   {"https://www.youtube-nocookie.com", "https://player.vimeo.com/video/"},
		"img-src":     {"https://*.example.com", "data:", "blob:", "'self'", "https://cdn.example.com:8443/Media/"},
		"script-src":  {"https://cdn.example.com/lib.js", "'sha256-ikhAzHpIhdk6wxu+v3TtAkGM69wDavllgwgrXyKsTBc='"},
		"style-src":   {"'sha384-abc+/def='"},
		"connect-src": {"wss://live.example.com"},
	}

	for directive, sources := range allowed {
		if err := site.CheckCSPSources(directive, sources); err != nil {
			t.Errorf("%s %v refused: %v", directive, sources, err)
		}
	}

	refused := [][2]string{
		{"default-src", "https://example.com"},
		{"frame-ancestors", "https://example.com"},
		{"report-uri", "https://example.com"},
		{"script-src", "'unsafe-inline'"},
		{"script-src", "'unsafe-eval'"},
		{"script-src", "'strict-dynamic'"},
		{"script-src", "'nonce-abc'"},
		{"script-src", "https:"},
		{"script-src", "data:"},
		{"img-src", "*"},
		{"img-src", "'sha256-abc='"},
		{"img-src", "ftp://example.com"},
		{"img-src", "https://example.com;script-src"},
		{"img-src", "https://exa mple.com"},
	}

	for _, source := range refused {
		if err := site.CheckCSPSources(source[0], []string{source[1]}); !errors.Is(err, site.ErrCSPSource) {
			t.Errorf("%s %s allowed: %v", source[0], source[1], err)
		}
	}
}

// TestParseCSPSources checks the text of the Security card's sources: one
// directive per line, blank lines skipped, keywords lowercased and paths
// kept as written.
func TestParseCSPSources(t *testing.T) {
	t.Parallel()

	sources, err := site.ParseCSPSources("FRAME-SRC https://www.youtube-nocookie.com/Embed/ 'SELF'\r\n\n" +
		"img-src https://images.example.com\nframe-src https://player.vimeo.com;")
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(
		sources["frame-src"],
		[]string{"https://www.youtube-nocookie.com/Embed/", "'self'", "https://player.vimeo.com"},
	) ||
		!slices.Equal(sources["img-src"], []string{"https://images.example.com"}) ||
		len(sources) != 2 {
		t.Fatalf("sources: %v", sources)
	}

	for _, text := range []string{"frame-src", "frame-src https://a.example.com\nscript-src 'unsafe-inline'"} {
		if _, err := site.ParseCSPSources(text); !errors.Is(err, site.ErrCSPSource) {
			t.Errorf("%q: %v", text, err)
		}
	}

	settings := site.Settings{ContentSecuritySources: "img-src https://ok.example.com\nscript-src *"}
	if extra := settings.ContentSecurityExtra(); len(extra) != 1 || extra["img-src"][0] != "https://ok.example.com" {
		t.Fatalf("extra: %v", extra)
	}

	for value, want := range map[string]string{"": site.CSPOff, "on": site.CSPOff, "enforce": site.CSPEnforce, "report-only": site.CSPReportOnly} {
		if got := (site.Settings{ContentSecurity: value}).ContentSecurityPolicy(); got != want {
			t.Errorf("%q: %q, want %q", value, got, want)
		}
	}
}
