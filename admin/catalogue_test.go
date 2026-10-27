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
		codeInput{},
		confirmTwoStepInput{},
		turnOffTwoStepInput{},
		recoveryCodesInput{},
	)
}

// TestTwoStepCatalogues checks the texts of two-step sign-in outside its
// forms: the messages of its handlers and its templates.
func TestTwoStepCatalogues(t *testing.T) {
	catalogtest.CompleteTexts(
		t,
		nil,
		"Two-step sign-in",
		"Enter the code from your authenticator app.",
		wrongCode,
		tooMany,
		signInExpired,
		"Turn on two-step sign-in",
		"Turn off two-step sign-in",
		"Turn on",
		"Cancel",
		"On",
		"Off",
		"Make new recovery codes",
		"Two-step sign-in is on.",
		"Two-step sign-in is off.",
		"Two-step sign-in is off for this account.",
		"New recovery codes are ready. The old ones no longer work.",
		"Turn off two-step sign-in for this account? It then signs in with its password alone, until its owner turns it on again.",
		"Your recovery codes",
		"Keep these codes somewhere safe, such as a password manager. Each code signs you in once instead of a code from "+
			"the app, for example when you lose your phone. They are not shown again.",
		"Signing in asks for a code from your authenticator app after your password.",
		"%d recovery code left.",
		"New recovery codes",
		"New codes replace the old ones, which then no longer work.",
		"Turn off",
		"Without two-step sign-in, your password alone signs you in.",
		"Scan this QR code with an authenticator app on your phone, or enter the key by hand. Then enter the code the app shows.",
		"QR code for your authenticator app",
		"Key",
		"After your password, signing in then also asks for a code from an authenticator app on your phone, so your "+
			"password alone does not open your account.",
		"For someone who lost both their phone and their recovery codes. They can turn it on again on their own account page.",
	)
}
