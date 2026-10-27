package admin_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/donseba/go-webthings/admin"
)

// TestValidationMessages checks that go-form's messages are translated by
// its own catalogues, merged into the admin's, and show in go-form's
// English in a language without a catalogue.
func TestValidationMessages(t *testing.T) {
	t.Parallel()

	accounts := newAccounts(t)

	cases := map[string][]string{
		"nl": {"Dit veld is verplicht.", "Vul een geldig e-mailadres in."},
		"en": {"This field is required.", "Enter a valid email address."},
		"de": {"This field is required.", "Enter a valid email address."},
	}

	for language, want := range cases {
		backoffice, err := admin.New(accounts, admin.Options{Language: language, CatalogueDir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}

		r := httptest.NewRequest("POST", "/admin/", nil)
		r = r.WithContext(backoffice.Context(context.Background()))

		input := struct {
			Name  string `form:"input,text"  label:"Name"  required:"true"`
			Email string `form:"input,email" label:"Email"`
		}{Email: "not an address"}

		errs := backoffice.Validate(r, &input)
		if len(errs) != 2 {
			t.Fatalf("%s: errors %v", language, errs)
		}

		for i, message := range want {
			if _, got := errs[i].FieldError(); got != message {
				t.Errorf("%s: message %d is %q, want %q", language, i, got, message)
			}
		}
	}
}
