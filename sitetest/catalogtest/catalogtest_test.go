package catalogtest_test

import (
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/donseba/go-webthings/sitetest/catalogtest"
)

// dutchOnly is a form whose label only the Dutch catalogue translates.
type dutchOnly struct {
	Name string `form:"input,text" label:"Name of the harbour master"`
}

// recorder is a testing.TB that keeps the errors instead of failing.
type recorder struct {
	testing.TB

	errors []string
}

func (r *recorder) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

// TestCompleteSkipsTheSourceLanguage checks that a text missing from the
// English catalogue is not reported, as it shows as written, and that one
// missing from another catalogue is.
func TestCompleteSkipsTheSourceLanguage(t *testing.T) {
	dutch := fstest.MapFS{
		"translations/nl_NL.po": {Data: []byte(`msgid ""
msgstr ""
"Language: nl_NL\n"
"Content-Type: text/plain; charset=UTF-8\n"

msgid "Name of the harbour master"
msgstr "Naam van de havenmeester"
`)},
	}

	catalogtest.Complete(t, dutch, dutchOnly{})

	missing := &recorder{TB: t}
	catalogtest.Complete(missing, fstest.MapFS{"translations/.keep": {}}, dutchOnly{})

	if len(missing.errors) != 1 || missing.errors[0] != `translations/nl_NL.po misses "Name of the harbour master"` {
		t.Fatalf("errors = %q, want the Dutch catalogue only", missing.errors)
	}
}
