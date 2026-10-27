package testsuite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/sitetest"
)

// TestMailTemplates checks module e-mail: the module's default text, the
// site's translations and formats in it, and themes replacing it.
func TestMailTemplates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	translations := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"snippets/when.gohtml": `<b>{{date . "long"}}</b>`,
	})
	writeFiles(t, translations, map[string]string{"nl.po": translationsNL})

	tl, err := frontend.Translations(translations)
	if err != nil {
		t.Fatal(err)
	}

	var env webthings.Env

	module := func(key string) webthings.Module {
		return webthings.Module{
			Type: "tickets",
			Key:  key,
			Mail: []frontend.Template{{Name: "confirmation", Data: ticket{}, Example: ticketMail}},
			Setup: func(e webthings.Env) (webthings.Parts, error) {
				if key == "box" {
					env = e
				}

				return webthings.Parts{}, nil
			},
		}
	}

	s := sitetest.StartWith(t, sitetest.Options{
		Templates: dir,
		Configure: func(config *webthings.Config) {
			config.Translations = tl
			// The theme gets its mail templates while the site runs: read
			// them again, as in development, rather than once.
			config.ReloadTemplates = true
		},
	}, module("box"), module("desk"))

	ctx := context.Background()

	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	settings.Timezone = "Europe/Amsterdam"
	if err := s.App.Site.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}

	data := ticket{Name: "Ada", Start: time.Date(2026, time.October, 25, 13, 0, 0, 0, time.UTC), Price: 4500}

	message, err := env.Frontend.Mail(ctx, "nl", "box", "confirmation", data)
	if err != nil {
		t.Fatal(err)
	}

	if message.Subject != "Uw kaartje: Ada" {
		t.Fatalf("subject: %q", message.Subject)
	}

	if message.Text != "Hallo Ada & co,\nzondag 25 oktober 2026 14:00 · € 45,00\n" || message.HTML != "" {
		t.Fatalf("text: %q html: %q", message.Text, message.HTML)
	}

	english, err := env.Frontend.Mail(ctx, "en", "box", "confirmation", data)
	if err != nil || english.Subject != "Your ticket: Ada" ||
		!strings.Contains(english.Text, "Sunday 25 October 2026 14:00 · €45.00") {
		t.Fatalf("english: %+v %v", english, err)
	}

	if _, err := env.Frontend.Mail(ctx, "nl", "box", "missing", data); !errors.Is(err, frontend.ErrNoTemplate) {
		t.Fatalf("missing template: %v", err)
	}

	// A theme replaces the mail of every instance of the type, or of one key.
	writeFiles(t, dir, map[string]string{
		"mail/tickets/confirmation.gohtml": `{{define "subject"}}Type{{end}}{{define "text"}}{{.Name}}{{end}}` +
			`{{define "html"}}<p>{{.Name}} {{"&"}} co</p>{{end}}`,
		"mail/desk/confirmation.gohtml": `{{define "subject"}}Desk{{end}}{{define "text"}}{{.Name}}{{end}}`,
	})

	byType, err := env.Frontend.Mail(ctx, "nl", "box", "confirmation", data)
	if err != nil || byType.Subject != "Type" || byType.HTML != "<p>Ada &amp; co</p>" {
		t.Fatalf("type template: %+v %v", byType, err)
	}

	byKey, err := env.Frontend.Mail(ctx, "nl", "desk", "confirmation", data)
	if err != nil || byKey.Subject != "Desk" {
		t.Fatalf("key template: %+v %v", byKey, err)
	}

	// Execute renders any template of the theme on its own.
	snippet, err := env.Frontend.Execute(ctx, "snippets/when", "nl", data.Start)
	if err != nil || snippet != "<b>25 oktober 2026</b>" {
		t.Fatalf("execute: %q %v", snippet, err)
	}

	// The default mail is a starting point for the site.
	out := t.TempDir()

	written, err := s.App.WriteTemplates(out, "box")
	if err != nil {
		t.Fatal(err)
	}

	source, err := os.ReadFile(filepath.Join(out, "mail", "box", "confirmation.gohtml"))
	if err != nil || string(source) != ticketMail {
		t.Fatalf("written %v: %v\n%s", written, err, source)
	}
}
