package testsuite

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/modules/comments"
	"github.com/donseba/go-webthings/sitetest"

	form "github.com/donseba/go-form/v2"
	formtemplates "github.com/donseba/go-form/v2/templates"
)

// visitorFormPage lays out the comment form with go-form's functions, as a
// theme does: form_open with a class and go-partial's HTMX attributes, and
// a form that writes its tag itself with form_hidden.
const visitorFormPage = `<main id="content"><h1>{{.Title}}</h1>
{{with commentInput "remarks" "page-1"}}
{{form_open . "class" "comment-form" (submitAttrs runtime (commentURL "remarks") "target" "#remarks")}}
{{form_field . "name" nil}}
{{form_field . "body" nil}}
{{form_close}}
<form class="own" method="post" action="{{commentURL "remarks"}}">{{form_hidden .}}</form>
{{end}}
</main>`

// TestVisitorFormFunctions renders a visitor form with go-form's template
// functions: the limits come from the module's input, labels and go-form's
// own texts from the site's translations in the page's language, and the
// classes from webthings.css or the site's form theme.
func TestVisitorFormFunctions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"templates/layout.gohtml": `<body>{{content}}</body>`,
		"templates/page.gohtml":   visitorFormPage,
		"translations/nl.po": `msgid ""
msgstr ""
"Language: nl\n"
"Content-Type: text/plain; charset=UTF-8\n"

msgid "Name"
msgstr "Naam"
`,
	})

	translations, err := frontend.Translations(filepath.Join(dir, "translations"))
	if err != nil {
		t.Fatal(err)
	}

	s := sitetest.StartWith(t, sitetest.Options{
		Templates: filepath.Join(dir, "templates"),
		Configure: func(config *webthings.Config) {
			config.Translations = translations
		},
	}, comments.Module("remarks", "Remarks"))

	s.Expect("/en/", http.StatusOK,
		`action="/_/remarks/post"`,
		`class="wt-form__field"`,
		`id="remarks-name"`,
		`name="name"`,
		`maxlength="100"`,
		`maxlength="5000"`,
		`class="wt-form__input"`,
		`autocomplete="name"`,
		`name="website" value="" tabindex="-1" autocomplete="off" aria-hidden="true" class="form-honeypot">`,
		`(required)`,
		`class="wt-form comment-form"`,
		`hx-post="/_/remarks/post"`,
		`hx-target="#remarks"`,
		`<form class="own" method="post" action="/_/remarks/post"><input type="text" name="website"`,
		`name="back" value="/en/#remarks"`,
	)

	// Empty theme classes and styles write no attribute, and the spam trap
	// is hidden by webthings.css, not by a style attribute.
	s.ExpectMissing("/en/", `style=""`)
	s.ExpectMissing("/en/", `class=""`)
	s.ExpectMissing("/en/", `style="position:absolute`)

	// The label from the site's catalogue, go-form's own text from go-form's.
	s.Expect("/nl/", http.StatusOK, "Naam", "(verplicht)")
}

// TestFormTheme checks that a site renders visitor forms with a go-form
// theme of its own (Config.FormTheme).
func TestFormTheme(t *testing.T) {
	t.Parallel()

	_, err := formtemplates.RegisterClassTheme("visitor-form-test", formtemplates.ThemeClasses{
		Wrapper: formtemplates.StyleOption{Class: "field"},
		Input:   formtemplates.StyleOption{Class: "control"},
	})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"templates/layout.gohtml": `<body>{{content}}</body>`,
		"templates/page.gohtml":   visitorFormPage,
	})

	s := sitetest.StartWith(t, sitetest.Options{
		Templates: filepath.Join(dir, "templates"),
		Configure: func(config *webthings.Config) {
			config.FormTheme = "visitor-form-test"
		},
	}, comments.Module("remarks", "Remarks"))

	s.Expect("/en/", http.StatusOK, `class="field"`, `class="control"`)
	s.ExpectMissing("/en/", "wt-form__field")
}

// meeting is a visitor form with a date-time.
type meeting struct {
	At time.Time `form:"input,datetime-local" name:"at" label:"When" required:"true"`
}

// TestVisitorFormTimezone checks that a visitor form shows a date-time in
// the site's timezone and that BindForm reads the visitor's wall-clock time
// in it, as the admin does.
func TestVisitorFormTimezone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"layout.gohtml": `<body>{{content}}</body>`,
		"page.gohtml": `<main id="content"><h1>{{.Title}}</h1>` +
			`{{with meetingForm}}{{form_open .}}{{form_field . "at" nil}}{{form_close}}{{end}}</main>`,
	})

	planned := make(chan time.Time, 1)
	module := webthings.Module{
		Type: "meetings",
		Key:  "meeting",
		Funcs: template.FuncMap{
			"meetingForm": func(ctx context.Context) form.RenderModel {
				return frontend.VisitorForm(
					ctx,
					meeting{At: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)},
					"/_/meeting/plan",
					"meeting",
				)
			},
		},
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			env.Actions().Post("/plan", func(w http.ResponseWriter, r *http.Request) {
				var in meeting

				errs, err := env.Frontend.BindForm(r, &in)
				if err != nil || len(errs) > 0 {
					http.Error(w, "invalid", http.StatusBadRequest)

					return
				}

				planned <- in.At
				w.WriteHeader(http.StatusNoContent)
			})

			return webthings.Parts{}, nil
		},
	}

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir}, module)
	ctx := context.Background()

	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	settings.Timezone = "Asia/Kolkata"

	if err := s.App.Site.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}

	// 12:00 UTC is 17:30 in Kolkata (UTC+5:30).
	s.Expect("/en/", http.StatusOK, `type="datetime-local"`, `value="2026-10-06T17:30"`)

	if status, _ := s.PostStatus(
		"/en/",
		"/_/meeting/plan",
		url.Values{"at": {"2026-10-06T17:30"}},
	); status != http.StatusNoContent {
		t.Fatalf("plan: %d", status)
	}

	if at := <-planned; !at.Equal(time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("planned at %s", at.UTC())
	}
}
