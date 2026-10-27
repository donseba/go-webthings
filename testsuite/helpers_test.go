package testsuite

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
)

// Helpers shared by the tests of this package.

// writeFiles writes files below dir, creating folders.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for name, source := range files {
		path := filepath.Join(dir, name)

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// screens is a module with a screen, a broken screen and an action, mounted on
// a page, to look at what module screens get.
func screens(asset *string) webthings.Module {
	return webthings.Module{
		Type:  "probe",
		Key:   "probe",
		Title: "Probe",
		Templates: []frontend.Template{
			{Name: "screen", Data: ""},
			{Name: "broken", Data: ""},
		},
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			routes := env.Frontend.Router()

			routes.Get("/{$}", func(w http.ResponseWriter, r *http.Request) {
				env.Frontend.Render(w, r, frontend.Content{Template: "screen", Title: "Screen", Data: *asset})
			})

			routes.Get("/broken", func(w http.ResponseWriter, r *http.Request) {
				env.Frontend.Render(w, r, frontend.Content{Template: "broken", Title: "Broken"})
			})

			routes.Get("/gone", func(w http.ResponseWriter, r *http.Request) {
				env.Frontend.Fail(w, r, content.ErrNotFound)
			})

			routes.Get("/failing", func(w http.ResponseWriter, r *http.Request) {
				env.Frontend.Fail(w, r, errors.New("probe storage is down"))
			})

			env.Actions().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.ReadAll(r.Body); err != nil {
					http.Error(w, "too_large", http.StatusRequestEntityTooLarge)

					return
				}

				w.WriteHeader(http.StatusNoContent)
			})

			return webthings.Parts{Public: routes}, nil
		},
	}
}
