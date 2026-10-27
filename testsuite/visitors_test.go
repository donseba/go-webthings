package testsuite

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/sitetest"
)

// helloMail is the mail template of the visitor module below.
const helloMail = `{{define "subject"}}Hello {{.Name}}{{end}}
{{define "text"}}{{tl localizer "Thank you."}}{{end}}`

// visitorModule receives a form at /_/<key>/hello with the shared visitor
// helpers: it remembers the visitor ID and language, mails the visitor and
// returns them to the page.
func visitorModule(key string, ids, languages *[]string) webthings.Module {
	return webthings.Module{
		Type: "visitors",
		Key:  key,
		Mail: []frontend.Template{{Name: "hello", Data: struct{ Name string }{}, Example: helloMail}},
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			actions := env.Actions()

			actions.Use(frontend.Limits(env.Key, frontend.ActionLimits{Body: 1 << 10, Posts: 2, Window: time.Hour}))
			actions.Post("/hello", func(w http.ResponseWriter, r *http.Request) {
				id, err := env.VisitorID(r.Context(), r, "2026-10-06")
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)

					return
				}

				*ids = append(*ids, id)
				*languages = append(*languages, env.Frontend.BackLocale(r))

				err = env.Frontend.SendMail(r.Context(), env.Mail, frontend.Letter{
					Locale:  env.AdminLanguage,
					Key:     env.Key,
					Name:    "hello",
					Data:    struct{ Name string }{Name: r.PostFormValue("name")},
					To:      []string{"ada@example.test"},
					ReplyTo: "staff@example.test",
				})
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)

					return
				}

				frontend.Back(w, r, env.Key, "sent")
			})

			return webthings.Parts{}, nil
		},
	}
}

// TestVisitorHelpers runs a module built on frontend.Actions, BackPath,
// BackLocale, SendMail and Env.VisitorID.
func TestVisitorHelpers(t *testing.T) {
	t.Parallel()

	var ids, languages []string

	s := sitetest.Start(t, visitorModule("hello", &ids, &languages), visitorModule("other", &ids, &languages))

	_, _, back := s.Send("/_/hello/hello", url.Values{"name": {"Ada"}, "back": {"/nl/contact?x=1#form"}})
	if back != "/nl/contact?hello=sent&x=1" {
		t.Fatalf("back: %q", back)
	}

	messages := s.Mails(1)
	if len(messages) != 1 || messages[0].Subject != "Hello Ada" || messages[0].To[0] != "ada@example.test" ||
		messages[0].ReplyTo != "staff@example.test" || !strings.Contains(messages[0].Text, "Thank you.") {
		t.Fatalf("mail: %+v", messages)
	}

	// The admin language defaults to English when the site sets none.
	s.Send("/_/hello/hello", url.Values{"name": {"Bob"}, "back": {"//elsewhere.example/en/"}})
	s.Send("/_/other/hello", url.Values{"name": {"Cy"}, "back": {"/en/"}})

	if len(languages) != 3 || languages[0] != "nl" || languages[1] != "en" || languages[2] != "en" {
		t.Fatalf("back locales: %v", languages)
	}

	// One visitor has one ID per instance, which is not their address.
	if len(ids[0]) != 64 || ids[0] != ids[1] || ids[0] == ids[2] || strings.Contains(ids[0], "127.0.0.1") {
		t.Fatalf("visitor ids: %v", ids)
	}

	// The third post to one instance within the hour is throttled.
	_, _, back = s.Send("/_/hello/hello", url.Values{"back": {"/en/"}})
	if back != "/en/?hello="+frontend.ResultThrottled || len(ids) != 3 {
		t.Fatalf("throttled: %q %v", back, ids)
	}
}
