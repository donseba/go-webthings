package testsuite

import (
	"bytes"
	"context"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/sitetest"
)

// TestEnvServiceAndSecret checks that modules reach each other's API once the
// app runs, and get secrets of their own.
func TestEnvServiceAndSecret(t *testing.T) {
	t.Parallel()

	type api struct{ name string }

	var (
		first, second webthings.Env
		duringSetup   any
	)

	module := func(key string, env *webthings.Env) webthings.Module {
		return webthings.Module{
			Type: "peer",
			Key:  key,
			Setup: func(e webthings.Env) (webthings.Parts, error) {
				*env = e
				duringSetup = e.Service("second")

				return webthings.Parts{Service: &api{name: key}}, nil
			},
		}
	}

	s := sitetest.StartWith(t, sitetest.Options{
		Configure: func(config *webthings.Config) {
			config.AdminLanguage = "nl"
		},
	}, module("first", &first), module("second", &second))

	if duringSetup != nil {
		t.Fatalf("service resolved before start: %v", duringSetup)
	}

	if peer, ok := first.Service("second").(*api); !ok || peer.name != "second" {
		t.Fatalf("service: %#v", first.Service("second"))
	}

	if first.Service("missing") != nil {
		t.Fatal("unknown key resolved")
	}

	if first.AdminLanguage != "nl" {
		t.Fatalf("admin language: %q", first.AdminLanguage)
	}

	ctx := context.Background()

	key, err := first.Secret(ctx, "visitors")
	if err != nil || len(key) != 32 {
		t.Fatalf("secret: %x %v", key, err)
	}

	again, _ := first.Secret(ctx, "visitors")
	other, _ := second.Secret(ctx, "visitors")
	media, _ := s.App.Site.Secret(ctx, "media_variants")

	if !bytes.Equal(key, again) || bytes.Equal(key, other) || bytes.Equal(key, media) {
		t.Fatal("secrets are not stable and separate per instance")
	}

	if _, err := first.Secret(ctx, "../media_variants"); err == nil {
		t.Fatal("invalid secret name accepted")
	}
}

type ticket struct {
	Name  string
	Start time.Time
	Price int64
}

const ticketMail = `{{define "subject"}}{{tl localizer "Your ticket"}}:
  {{.Name}}{{end}}
{{define "text"}}
{{tl localizer "Hello"}} {{.Name}} & co,
{{date .Start "full"}} {{time .Start}} · {{money .Price "EUR"}}
{{end}}`

const translationsNL = `msgid ""
msgstr ""
"Language: nl\n"
"Content-Type: text/plain; charset=UTF-8\n"

msgid "Your ticket"
msgstr "Uw kaartje"

msgid "Hello"
msgstr "Hallo"
`
