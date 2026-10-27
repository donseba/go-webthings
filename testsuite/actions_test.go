package testsuite

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/sitetest"

	form "github.com/donseba/go-form/v2"
)

// signatures is a tiny module with a visitor form shown on any page.
type signatures struct {
	mu    sync.Mutex
	names []string
}

// signature is the petition's form.
type signature struct {
	Name string `form:"input,text" name:"name" label:"Name" required:"true" maxLength:"100"`
}

func (s *signatures) module() webthings.Module {
	return webthings.Module{
		Type: "signatures",
		Key:  "petition",
		Funcs: template.FuncMap{
			"signatures": func(ctx context.Context) []string {
				s.mu.Lock()
				defer s.mu.Unlock()

				return append([]string(nil), s.names...)
			},
			"signatureForm": func(ctx context.Context) form.RenderModel {
				return frontend.VisitorForm(ctx, signature{}, "/_/petition/sign", "petition")
			},
		},
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			env.Actions().Post("/sign", func(w http.ResponseWriter, r *http.Request) {
				if !frontend.IsSpam(r) {
					s.mu.Lock()
					s.names = append(s.names, r.PostFormValue("name"))
					s.mu.Unlock()
				}

				frontend.Back(w, r, "petition", "signed")
			})

			return webthings.Parts{}, nil
		},
	}
}

func TestVisitorActions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	page := `<main id="content"><h1>{{.Title}}</h1>
{{range signatures}}<p class="signature">{{.}}</p>{{end}}
<section id="petition">{{with signatureForm}}{{form_open .}}{{form_field . "name" nil}}{{form_close}}{{end}}</section></main>`

	if err := os.WriteFile(filepath.Join(dir, "page.gohtml"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}

	petition := &signatures{}
	s := sitetest.StartWith(t, sitetest.Options{Templates: dir}, petition.module())

	// A module's own form, made with VisitorForm, has the spam trap, the
	// way back to its element and ids with the module's prefix.
	s.Expect("/en/", http.StatusOK,
		`action="/_/petition/sign"`,
		`name="website"`,
		`name="back" value="/en/#petition"`,
		`id="petition-name"`,
	)

	status, _, landed := s.Send("/_/petition/sign", url.Values{"name": {"Ada"}, "back": {"/en/"}})
	if status != http.StatusOK || landed != "/en/?petition=signed" {
		t.Fatalf("sign: %d %s", status, landed)
	}

	s.Expect("/en/", http.StatusOK, `<p class="signature">Ada</p>`)

	// Bots that fill the honeypot are thanked but not counted.
	s.Send("/_/petition/sign", url.Values{"name": {"Bot"}, "website": {"spam"}, "back": {"/en/"}})
	s.ExpectMissing("/en/", "Bot")

	if status := s.CrossSite("/_/petition/sign", url.Values{"name": {"Mallory"}}); status != http.StatusForbidden {
		t.Fatalf("cross-site post: %d", status)
	}

	// Only paths on this site are followed back.
	_, _, landed = s.Send("/_/petition/sign", url.Values{"name": {"Eve"}, "back": {"//evil.example/"}})
	if !strings.HasPrefix(landed, "/?petition=signed") && !strings.HasPrefix(landed, "/en/") {
		t.Fatalf("followed an outside address: %s", landed)
	}
}

func TestModuleClose(t *testing.T) {
	t.Parallel()

	var order []string

	module := func(key string) webthings.Module {
		return webthings.Module{
			Type: "closing",
			Key:  key,
			Setup: func(webthings.Env) (webthings.Parts, error) {
				return webthings.Parts{
					Close: func(context.Context) error {
						order = append(order, key)

						return nil
					},
				}, nil
			},
		}
	}

	s := sitetest.Start(t, module("first"), module("second"))

	if err := s.App.Close(); err != nil {
		t.Fatal(err)
	}

	if strings.Join(order, ",") != "second,first" {
		t.Fatalf("close order: %v", order)
	}
}
