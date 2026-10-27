package redirects

import (
	"context"
	"testing"

	"github.com/donseba/go-webthings/sitetest/catalogtest"
)

// TestCatalogues checks that the admin catalogues translate every text of
// the forms of the section in English and Dutch.
func TestCatalogues(t *testing.T) {
	catalogtest.Complete(t, files,
		testInput{},
		redirectInput{Status: statusChoices(context.Background(), 301)},
		importInput{},
	)
}
