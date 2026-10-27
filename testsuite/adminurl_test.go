package testsuite

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/sitetest"
)

// adminLinks is a layout that links into the admin with adminURL: its
// dashboard, the page's editor and a route the admin does not have.
const adminLinks = `<!doctype html>
<html lang="{{.Locale}}">
<body>
<p id="admin">{{adminURL "admin.dashboard"}}|{{with .Page}}{{adminURL "admin.pages.edit" "id" .ID}}{{end}}|{{adminURL "admin.nothing.index"}}</p>
{{content}}
</body>
</html>
`

// TestAdminURLFunction checks the template function adminURL: signed-in
// editors get addresses in the admin wherever it is, visitors get nothing,
// so the admin's path stays off public pages, and a route the admin does
// not have gives nothing either.
func TestAdminURLFunction(t *testing.T) {
	t.Parallel()

	templates := t.TempDir()
	if err := os.WriteFile(filepath.Join(templates, "layout.gohtml"), []byte(adminLinks), 0o644); err != nil {
		t.Fatal(err)
	}

	s := sitetest.StartWith(t, sitetest.Options{
		Templates: templates,
		Configure: func(config *webthings.Config) {
			config.AdminPath = "/toolbox"
		},
	})

	status, body := s.Get("/en/")
	if status != http.StatusOK {
		t.Fatalf("home for an editor: %d", status)
	}

	links := between(body, `<p id="admin">`, `</p>`)

	dashboard, rest, _ := strings.Cut(links, "|")
	editor, unknown, _ := strings.Cut(rest, "|")

	if dashboard != "/toolbox/" || !strings.HasPrefix(editor, "/toolbox/pages/") || unknown != "" {
		t.Fatalf("links for an editor: %q", links)
	}

	response, body := s.Visitor().Response("/en/")
	if response.StatusCode != http.StatusOK || between(body, `<p id="admin">`, `</p>`) != "||" ||
		strings.Contains(body, "/toolbox") {
		t.Fatalf("links for a visitor: %d\n%s", response.StatusCode, body)
	}
}

// between is the text between start and end in s, or "".
func between(s, start, end string) string {
	_, after, found := strings.Cut(s, start)
	if !found {
		return ""
	}

	inside, _, _ := strings.Cut(after, end)

	return inside
}
