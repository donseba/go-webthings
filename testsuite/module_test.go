package testsuite

import (
	"os/exec"
	"strings"
	"testing"
)

// TestModuleKeepsEveryFile guards the files a site gets when it requires
// go-webthings. A Go module leaves out every file in a folder below a folder
// named vendor (golang.org/x/mod/zip), so an embedded asset there is missing
// from the release while every test of the repository still passes. v0.1.0
// shipped without the admin's htmx that way.
func TestModuleKeepsEveryFile(t *testing.T) {
	t.Parallel()

	out, err := exec.Command("git", "-C", "..", "ls-files").Output()
	if err != nil {
		t.Skipf("git ls-files: %v", err)
	}

	for _, file := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if leftOutOfModule(file) {
			t.Errorf("%s is below a vendor folder, so the module leaves it out; use another folder name", file)
		}
	}
}

// leftOutOfModule reports whether the module zip leaves file out: it is in a
// folder below a folder named vendor. A file directly in a vendor folder,
// such as vendor/modules.txt, is kept.
func leftOutOfModule(file string) bool {
	parts := strings.Split(file, "/")

	for i, part := range parts {
		if part == "vendor" && len(parts)-i > 2 {
			return true
		}
	}

	return false
}

func TestLeftOutOfModule(t *testing.T) {
	t.Parallel()

	for file, want := range map[string]bool{
		"admin/assets/vendor/htmx/htmx.esm.min.js":   true,
		"vendor/github.com/x/y.go":                   true,
		"admin/assets/vendor/importmap.lock.json":    false,
		"vendor/modules.txt":                         false,
		"admin/assets/packages/htmx/htmx.esm.min.js": false,
		"docs/vendoring.md":                          false,
		"admin/assets/vendored/htmx/htmx.esm.min.js": false,
	} {
		if got := leftOutOfModule(file); got != want {
			t.Errorf("leftOutOfModule(%q) = %v, want %v", file, got, want)
		}
	}
}
