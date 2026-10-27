package frontend_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
)

// contactInput is a visitor form with go-form's own messages.
type contactInput struct {
	Name  string `form:"input,text"  name:"name"  required:"true"`
	Email string `form:"input,email" name:"email"`
}

// TestFormMessagesInSiteTranslations checks that the site's translations of
// go-form's messages win, and that go-form shows the messages they do not
// translate in its own catalogue of the visitor's language, English when it
// has none, as the site's forms translate them (see Translations).
func TestFormMessagesInSiteTranslations(t *testing.T) {
	dir := t.TempDir()

	catalogues := map[string]string{
		"nl.po": dutch + `
msgid "form||Invalid email format"
msgstr "Dat is geen e-mailadres."
`,
		// go-form has no Greek messages: they stay English.
		"el.po": strings.Replace(dutch, "Language: nl", "Language: el", 1),
	}

	for name, data := range catalogues {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tr, err := frontend.Translations(dir)
	if err != nil {
		t.Fatal(err)
	}

	forms := form.NewTranslatedForm(func(loc form.Localizer, key string, args ...any) string {
		return tr.Tl(loc, key, args...)
	})

	for _, check := range []struct{ locale, name, email string }{
		{"nl", "Dit veld is verplicht.", "Dat is geen e-mailadres."},
		// A region uses the site's catalogue and go-form's of its language.
		{"nl_BE", "Dit veld is verplicht.", "Dat is geen e-mailadres."},
		{"el", "This field is required.", "Enter a valid email address."},
	} {
		errs := forms.ValidateFormLocalized(&contactInput{Email: "not an address"}, locale(check.locale))

		got := map[string]string{}

		for _, fieldError := range errs {
			field, message := fieldError.FieldError()
			got[field] = message
		}

		if got["name"] != check.name || got["email"] != check.email {
			t.Errorf("%s: got %q, want %q and %q", check.locale, got, check.name, check.email)
		}
	}
}

// commentInput is a visitor form as modules declare one.
type commentInput struct {
	Name   string `form:"input,text"   name:"name"   required:"true" maxLength:"10" line:"true"`
	Body   string `form:"textarea"     name:"body"   required:"true" maxLength:"5"`
	Places int    `form:"input,number" name:"places"                                            min:"1" max:"4"`
}

// TestBindForm checks the mapping and validation of visitors' posts: text
// trimmed, line breaks counted as one character, unreadable values and
// broken rules as field errors with their rule.
func TestBindForm(t *testing.T) {
	post := func(values url.Values) *http.Request {
		request := httptest.NewRequest(http.MethodPost, "/_/comments/post", strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		return request
	}

	site := frontend.New(nil, nil, nil, frontend.Missing())
	in := commentInput{Places: 1}

	errs, err := site.BindForm(post(url.Values{"name": {"  Jane "}, "body": {"a\r\nb\r\nc"}}), &in)
	if err != nil || len(errs) > 0 {
		t.Fatalf("valid post: %v %v", errs, err)
	}

	if in.Name != "Jane" || in.Body != "a\nb\nc" || in.Places != 1 {
		t.Fatalf("mapped: %+v", in)
	}

	in = commentInput{}

	errs, err = site.BindForm(post(url.Values{"name": {"Jane\tDoe"}, "body": {"too long"}, "places": {"abc"}}), &in)
	if err != nil {
		t.Fatal(err)
	}

	rules := map[string]string{}

	for _, fieldErr := range errs {
		validation, ok := fieldErr.(form.FieldValidationError)
		if !ok {
			t.Fatalf("error %T", fieldErr)
		}

		rules[validation.Field] = validation.Rule
	}

	// "abc" is not a number; validation's own complaint about the zero it
	// left (below the minimum) does not show as well.
	if rules["name"] != "line" || rules["body"] != "maxLength" || rules["places"] != "invalid" || len(errs) != 3 {
		t.Fatalf("errors: %v", errs)
	}
}
