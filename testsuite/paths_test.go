package testsuite

import (
	"slices"
	"testing"

	webthings "github.com/donseba/go-webthings"
)

func TestIsCorePath(t *testing.T) {
	t.Parallel()

	app := webthings.New(webthings.Config{})

	for path, want := range map[string]bool{
		"/admin":            true,
		"/admin/pages":      true,
		"/_/polls/vote":     true,
		"/media/photo.jpg":  true,
		"/assets/site.css":  true,
		"/theme":            true,
		"/theme/site.css":   true,
		"/sitemap.xml":      true,
		"/robots.txt":       true,
		"/healthz":          true,
		"/readyz":           true,
		"/metrics":          true,
		"/":                 false,
		"/administrator":    false,
		"/themes/old":       false,
		"/en/media":         false,
		"/sitemap.xml.bak":  false,
		"/_underscore-page": false,
	} {
		if got := app.IsCorePath(path); got != want {
			t.Errorf("IsCorePath(%q): want %v, got %v", path, want, got)
		}
	}

	want := []string{
		"/admin", "/healthz", "/readyz", "/metrics",
		"/_", "/media", "/assets", "/theme", "/sitemap.xml", "/robots.txt",
	}
	if paths := app.CorePaths(); !slices.Equal(paths, want) {
		t.Errorf("CorePaths: %v", paths)
	}

	if !(webthings.Env{}).IsCorePath("/admin/pages") || (webthings.Env{}).IsCorePath("/en/") {
		t.Error("an Env without app does not know the default core paths")
	}
}

// TestIsCorePathAdminPath checks that the admin's core path follows
// Config.AdminPath, and that /admin is free then.
func TestIsCorePathAdminPath(t *testing.T) {
	t.Parallel()

	app := webthings.New(webthings.Config{AdminPath: "/toolbox"})

	for path, want := range map[string]bool{
		"/toolbox":       true,
		"/toolbox/pages": true,
		"/admin":         false,
		"/admin/pages":   false,
		"/toolboxes":     false,
	} {
		if got := app.IsCorePath(path); got != want {
			t.Errorf("IsCorePath(%q): want %v, got %v", path, want, got)
		}
	}

	if paths := app.CorePaths(); paths[0] != "/toolbox" {
		t.Errorf("CorePaths: %v", paths)
	}
}
