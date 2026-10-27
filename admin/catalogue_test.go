package admin

import (
	"testing"

	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/sitetest/catalogtest"
)

// TestCatalogues checks that the admin catalogues translate every text of
// the admin's own forms in English and Dutch.
func TestCatalogues(t *testing.T) {
	catalogtest.Complete(t, nil,
		loginInput{},
		membershipInput{},
		createInput{Role: new(Admin).roles(nil, auth.Editor)},
		profileInput{},
		roleInput{Role: new(Admin).roles(nil, auth.Admin)},
		groupInput{},
		matrixInput{},
		setupInput{},
		forgotInput{},
		resetInput{},
		passwordInput{},
		ReviewInput{},
		approveInput{},
		rejectInput{},
		ScheduleInput{},
		publishInput{},
	)
}
