package admin

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// TestTamperedVendorFile checks that the admin refuses to start with a
// vendored or built file that differs from what the lock records.
func TestTamperedVendorFile(t *testing.T) {
	t.Parallel()

	shared, err := fs.Sub(files, "assets")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := loadAssets(shared); err != nil {
		t.Fatalf("the embedded assets: %v", err)
	}

	copied := fstest.MapFS{}

	err = fs.WalkDir(shared, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		data, err := fs.ReadFile(shared, name)
		if err != nil {
			return err
		}

		copied[name] = &fstest.MapFile{Data: data, Mode: 0o644}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A vendored package and the editor bundle the admin builds itself
	// (a local package of the lock) are both checked.
	for _, file := range []string{"packages/htmx/htmx.esm.min.js", "editor/prosemirror.js"} {
		original := copied[file]
		copied[file] = &fstest.MapFile{Data: append([]byte("/* changed */"), original.Data...), Mode: 0o644}

		_, err = loadAssets(copied)
		if err == nil || !strings.Contains(err.Error(), "integrity") {
			t.Fatalf("a changed %s is accepted: %v", file, err)
		}

		copied[file] = original
	}
}
