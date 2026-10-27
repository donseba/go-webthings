//go:build unix

package testsuite

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/sitetest"

	translator "github.com/donseba/go-translator/v2"
)

const dutchCatalogue = `msgid ""
msgstr ""
"Language: nl\n"
"Content-Type: text/plain; charset=UTF-8\n"

msgid "Read more"
msgstr "Lees meer"
`

type potLocale string

func (l potLocale) GetLocale() string {
	return string(l)
}

// watchPot makes <dir>/translations.pot a named pipe and returns a function
// that reports whether anything opened it for reading. A reader is never
// blocked: the watcher opens the pipe for writing and closes it at once, so
// the reader sees an empty file.
func watchPot(t *testing.T, dir string) func() bool {
	t.Helper()

	path := filepath.Join(dir, translator.DefaultPotFile)
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Fatal(err)
	}

	var (
		stopped atomic.Bool
		opened  atomic.Bool
		done    sync.WaitGroup
	)

	done.Add(1)

	go func() {
		defer done.Done()

		for {
			// Blocks until something opens the pipe for reading.
			pipe, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err != nil {
				return
			}

			_ = pipe.Close()

			if stopped.Load() {
				return
			}

			opened.Store(true)
		}
	}()

	t.Cleanup(func() {
		stopped.Store(true)

		// Opening the pipe for reading releases the watcher.
		pipe, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			_ = pipe.Close()
		}

		done.Wait()
	})

	return opened.Load
}

// useTranslators looks up texts without translation with the site's
// translations from siteDir, with the translators that serve sites and
// modules without translations, and in the Dutch admin of a site whose state
// is in stateDir.
func useTranslators(t *testing.T, siteDir, stateDir string) {
	t.Helper()

	tr, err := frontend.Translations(siteDir)
	if err != nil {
		t.Fatal(err)
	}

	loc := potLocale("nl")

	tr.Tl(loc, "Read more")
	tr.Tl(loc, "Not in the catalogue")
	tr.Ctl(loc, "menu", "Not in the catalogue")

	admin.T(context.Background(), "Not in the catalogue")
	admin.TN(context.Background(), "%d file", "%d files", 2, 2)

	s := sitetest.StartWith(t, sitetest.Options{
		Configure: func(config *webthings.Config) {
			config.StateDir = stateDir
			config.AdminLanguage = "nl"
		},
	})

	s.Get("/")
	s.Get("/admin")
	s.Get("/admin/accounts")
	s.Get("/admin/pages")
}

// TestTranslatorsLeavePot checks that the site's translations, the admin's
// and the translators without catalogues neither read nor create a
// translations.pot: they translate and record nothing, so the catalogue
// folder, the admin's catalogue folder and the working directory stay as
// they are. Not parallel: it changes the working directory.
func TestTranslatorsLeavePot(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		workDir := t.TempDir()
		t.Chdir(workDir)

		siteDir := t.TempDir()
		stateDir := t.TempDir()
		catalogueDir := filepath.Join(stateDir, "translations")

		if err := os.MkdirAll(catalogueDir, 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(siteDir, "nl.po"), []byte(dutchCatalogue), 0o644); err != nil {
			t.Fatal(err)
		}

		watched := map[string]func() bool{
			"working directory":  watchPot(t, workDir),
			"site translations":  watchPot(t, siteDir),
			"admin's catalogues": watchPot(t, catalogueDir),
		}

		useTranslators(t, siteDir, stateDir)

		for name, opened := range watched {
			if opened() {
				t.Errorf("translations.pot in the %s was read", name)
			}
		}
	})

	t.Run("create", func(t *testing.T) {
		workDir := t.TempDir()
		t.Chdir(workDir)

		siteDir := t.TempDir()
		stateDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(siteDir, "nl.po"), []byte(dutchCatalogue), 0o644); err != nil {
			t.Fatal(err)
		}

		useTranslators(t, siteDir, stateDir)

		for _, dir := range []string{workDir, siteDir, filepath.Join(stateDir, "translations")} {
			_, err := os.Stat(filepath.Join(dir, translator.DefaultPotFile))
			if !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("translations.pot in %s was created or cannot be checked: %v", dir, err)
			}
		}
	})
}
