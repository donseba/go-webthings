package siteadmin

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/sitetest/catalogtest"
)

// TestCatalogues checks that the admin catalogues translate every text of
// the forms of the section in English and Dutch.
func TestCatalogues(t *testing.T) {
	catalogtest.Complete(t, files,
		performanceInput{},
		reviewInput{},
		securityInput{},
		settingsInput{},
		textInput{},
		profilesInput{},
		activateInput{},
		trashInput{},
	)
}

// TestTemplateCatalogues checks that the catalogues translate the texts of
// the section's templates and the setup guide's messages.
func TestTemplateCatalogues(t *testing.T) {
	quoted := `"((?:[^"\\]|\\.)+)"`
	patterns := map[string]*regexp.Regexp{
		"templates/*.gohtml": regexp.MustCompile(`tl localizer (?:\(or \.\w+ )?` + quoted),
		"guide*.go":          regexp.MustCompile(`(?:T\(\s*(?:r|ctx),\s*|SubmitText = |Action: +)` + quoted),
	}

	var texts []string

	for pattern, expression := range patterns {
		names, err := filepath.Glob(pattern)
		if err != nil || len(names) == 0 {
			t.Fatalf("%s: %v", pattern, err)
		}

		for _, name := range names {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}

			source, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}

			for _, match := range expression.FindAllStringSubmatch(string(source), -1) {
				text, err := strconv.Unquote(`"` + match[1] + `"`)
				if err != nil {
					t.Fatalf("%s: %q: %v", name, match[1], err)
				}

				texts = append(texts, text)
			}
		}
	}

	// The step titles, the guide's buttons, the plain pages and the Backup
	// button.
	for _, title := range stepTitles {
		texts = append(texts, title)
	}

	for _, page := range plainPages {
		texts = append(texts, page.title)
	}

	texts = append(
		texts,
		"Setup finished. Site settings opens the guide again.",
		"Skip for now",
		"Finish setup",
		"Open the setup guide",
		"Create page",
		"Create the ticked pages",
		"Download backup",
	)

	if len(texts) < 90 {
		t.Fatalf("only %d texts found", len(texts))
	}

	catalogtest.CompleteTexts(t, files, texts...)
}
