package frontend

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestThemeAssetsReload checks that while templates reload, a changed or new
// theme asset gets a new version and is served as it is now, and that
// otherwise the fingerprints are taken once.
func TestThemeAssetsReload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, content string, modified time.Time) {
		t.Helper()

		path := filepath.Join(dir, name)

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
	}

	start := time.Now().Add(-time.Hour)
	write("layout.gohtml", `{{content}}`, start)
	write("assets/site.css", `body{color:black}`, start)

	themes, err := LoadThemes(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}

	theme, _ := themes.Get(DefaultTheme)

	fixed := &Frontend{themes: themes}
	reloading := &Frontend{themes: themes, reload: true}

	before := reloading.themeManifest(theme).URL("site.css")
	cached := fixed.themeManifest(theme).URL("site.css")

	if before != cached {
		t.Fatalf("versions differ: %s and %s", before, cached)
	}

	write("assets/site.css", `body{color:white}`, start.Add(time.Minute))
	write("assets/new.css", `p{}`, start)

	after := reloading.themeManifest(theme).URL("site.css")
	if after == before {
		t.Fatalf("a changed file keeps its version %s", after)
	}

	if !reloading.themeManifest(theme).Has("new.css") {
		t.Fatal("a new file is not found while reloading")
	}

	if fixed.themeManifest(theme).URL("site.css") != cached {
		t.Fatal("the fingerprints are taken again without reloading")
	}

	// Assets serves the route /theme/{file...}.
	request := httptest.NewRequest(http.MethodGet, "/theme/default/site.css", nil)
	request.SetPathValue("file", "default/site.css")

	response := httptest.NewRecorder()
	reloading.Assets().ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != `body{color:white}` {
		t.Fatalf("served: %d %s", response.Code, response.Body)
	}
}

// TestFocalPointsStylesheet checks that focal-points.css has a rule for
// every class media.FocalPoint.PositionClass gives, in steps of 5%, and
// nothing else.
func TestFocalPointsStylesheet(t *testing.T) {
	t.Parallel()

	css, err := fs.ReadFile(coreAssets, "assets/focal-points.css")
	if err != nil {
		t.Fatal(err)
	}

	rules := regexp.MustCompile(`(?m)^\.fp-(\d+)-(\d+) \{ object-position: (\d+)% (\d+)%; \}$`).
		FindAllStringSubmatch(string(css), -1)

	seen := map[string]bool{}

	for _, rule := range rules {
		if rule[1] != rule[3] || rule[2] != rule[4] {
			t.Errorf("rule %s sets another position", rule[0])
		}

		seen[rule[1]+"-"+rule[2]] = true
	}

	for x := 0; x <= 100; x += 5 {
		for y := 0; y <= 100; y += 5 {
			if !seen[strconv.Itoa(x)+"-"+strconv.Itoa(y)] {
				t.Errorf("no rule for .fp-%d-%d", x, y)
			}
		}
	}

	withoutComments := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(string(css), "")
	if len(rules) != 21*21 || strings.Count(withoutComments, "{") != len(rules) {
		t.Fatalf("%d rules, want %d and nothing else", len(rules), 21*21)
	}
}
