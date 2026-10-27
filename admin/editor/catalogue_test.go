package editor

import (
	"testing"

	"github.com/donseba/go-webthings/sitetest/catalogtest"
)

// TestCatalogues checks that the admin catalogues translate every text of
// the editor's own forms in English and Dutch.
func TestCatalogues(t *testing.T) {
	catalogtest.Complete(t, nil,
		RedirectInput{},
		MovedInput{},
	)
}
