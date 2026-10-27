package locales_test

import (
	"context"
	"errors"
	"testing"

	"github.com/donseba/go-webthings/locales"
)

func TestConfiguration(t *testing.T) {
	input := locales.Config{Default: "NL-nl", Enabled: []string{"NL-nl", "en", "de"}}

	service, err := locales.New(input)
	if err != nil {
		t.Fatal(err)
	}

	// The service keeps its own copy.
	input.Enabled[0] = "fr"

	config, err := service.Configuration(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if config.Default != "nl-NL" || config.Enabled[0] != "nl-NL" {
		t.Fatalf("not canonical or out of order: %+v", config)
	}

	if _, err := config.Require("en-GB"); !errors.Is(err, locales.ErrDisabled) {
		t.Fatalf("a disabled language was accepted: %v", err)
	}

	code, err := config.Require("nl-nl")
	if err != nil || code != "nl-NL" {
		t.Fatalf("canonical code: %s %v", code, err)
	}
}

func TestInvalidConfiguration(t *testing.T) {
	invalid := []locales.Config{
		{Default: "nl", Enabled: []string{"en"}},
		{Default: "nl", Enabled: []string{"nl", "NL"}},
		{Default: "en_US", Enabled: []string{"en_US"}},
		{Default: "no-such-language", Enabled: []string{"no-such-language"}},
	}

	for _, config := range invalid {
		if _, err := locales.New(config); !errors.Is(err, locales.ErrConfiguration) {
			t.Fatalf("accepted %+v: %v", config, err)
		}
	}
}
