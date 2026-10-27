package testsuite

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// The bundled modules are built like modules from any other repository, so
// a webshop or a site's own module can do everything they do. These rules keep
// it that way:
//
//   - core never imports a module (tests may, to test them end to end);
//   - modules use core's public API only, never internal/ packages;
//   - modules add no dependencies of their own: they may use the standard
//     library, core, other modules and the libraries core already uses.
//     Integrations with outside libraries belong in go-webthings-contrib;
//   - nothing here imports go-webthings-contrib;
//   - test helpers (sitetest, dbtest) are for tests only.
const (
	corePath    = "github.com/donseba/go-webthings"
	modulesPath = corePath + "/modules/"
	internal    = corePath + "/internal/"
	contribPath = "github.com/donseba/go-webthings-contrib"
)

var testHelpers = []string{corePath + "/sitetest", corePath + "/db/dbtest"}

type listedPackage struct {
	ImportPath   string
	Imports      []string
	TestImports  []string
	XTestImports []string
}

func TestArchitecture(t *testing.T) {
	t.Parallel()

	packages := listPackages(t)
	owners := moduleOwners(t)

	// The outside libraries core itself uses; modules may use these too.
	allowed := map[string]bool{}

	for _, pkg := range packages {
		if isModule(pkg.ImportPath) {
			continue
		}

		for _, imported := range pkg.Imports {
			if owner := owners[imported]; owner != "" && owner != corePath {
				allowed[owner] = true
			}
		}
	}

	var problems []string

	for _, pkg := range packages {
		module := isModule(pkg.ImportPath)
		helper := isTestHelper(pkg.ImportPath)

		for _, imported := range pkg.Imports {
			owner := owners[imported]

			switch {
			case strings.HasPrefix(imported, contribPath):
				problems = append(problems, pkg.ImportPath+" imports contrib package "+imported)
			case !module && isModule(imported):
				problems = append(problems, pkg.ImportPath+" (core) imports module "+imported)
			case module && strings.HasPrefix(imported, internal):
				problems = append(problems, pkg.ImportPath+" imports core internals "+imported)
			case module && owner != "" && owner != corePath && !allowed[owner]:
				problems = append(
					problems,
					pkg.ImportPath+" depends on "+owner+", which core does not use: move it to go-webthings-contrib",
				)
			case !helper && isTestHelper(imported):
				problems = append(problems, pkg.ImportPath+" imports test helper "+imported+" outside tests")
			}
		}

		for _, imported := range append(pkg.TestImports, pkg.XTestImports...) {
			if module && strings.HasPrefix(imported, internal) {
				problems = append(problems, pkg.ImportPath+" tests import core internals "+imported)
			}

			if strings.HasPrefix(imported, contribPath) {
				problems = append(problems, pkg.ImportPath+" tests import contrib package "+imported)
			}
		}
	}

	sort.Strings(problems)

	for _, problem := range problems {
		t.Error(problem)
	}

	if len(allowed) == 0 {
		t.Fatal("found no outside libraries used by core; is go list working?")
	}
}

func isModule(path string) bool {
	return strings.HasPrefix(path, modulesPath)
}

func isTestHelper(path string) bool {
	for _, helper := range testHelpers {
		if path == helper || strings.HasPrefix(path, helper+"/") {
			return true
		}
	}

	return false
}

// listPackages lists this repository's packages with their imports.
func listPackages(t *testing.T) []listedPackage {
	t.Helper()

	output := goList(t, "-json", "./...")
	decoder := json.NewDecoder(bytes.NewReader(output))

	var packages []listedPackage

	for {
		var pkg listedPackage

		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatal(err)
		}

		packages = append(packages, pkg)
	}

	return packages
}

// moduleOwners maps every package this repository depends on, including in
// tests, to the Go module that provides it; standard library packages map to "".
func moduleOwners(t *testing.T) map[string]string {
	t.Helper()

	output := goList(t, "-deps", "-test", "-f", "{{.ImportPath}}\t{{with .Module}}{{.Path}}{{end}}", "./...")
	owners := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(output))

	for scanner.Scan() {
		path, owner, _ := strings.Cut(scanner.Text(), "\t")
		owners[path] = owner
	}

	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}

	return owners
}

func goList(t *testing.T, args ...string) []byte {
	t.Helper()

	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go command is not available")
	}

	// The suite lives one folder below the repository root it lists.
	command := exec.Command(gobin, append([]string{"list"}, args...)...)
	command.Dir = ".."

	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("go list: %v\n%s", err, exit.Stderr)
		}

		t.Fatal(err)
	}

	return output
}
