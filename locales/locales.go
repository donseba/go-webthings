// Package locales checks the content languages a site offers, such as "en"
// and "nl-BE", and writes their codes the same way everywhere. Which
// languages are enabled comes from a Provider, usually the Site settings;
// how a language shows in addresses, and the admin's own language, are
// decided elsewhere.
package locales

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/text/language"
)

var (
	// ErrInvalid is returned for a language code that is not a valid BCP 47
	// tag, such as "en_US" or an empty code.
	ErrInvalid = errors.New("invalid_locale")
	// ErrDisabled is returned for a valid language code that the site does
	// not offer.
	ErrDisabled = errors.New("locale_disabled")
	// ErrConfiguration is returned for a list of languages that cannot work:
	// none, more than 100, an invalid or repeated code, or a default that is
	// not one of them.
	ErrConfiguration = errors.New("invalid_locale_configuration")
)

// Config is the list of content languages: Default is the language a
// visitor gets when the address names none, and Enabled are all languages
// in the order editors and visitors see them, the default among them.
type Config struct {
	Default string
	Enabled []string
}

// Provider gives the content languages, from the application's
// configuration or from the Site settings. Services ask for them in every
// operation, so a change in the settings takes effect at once.
type Provider interface {
	Configuration(context.Context) (Config, error)
}

// Service is a Provider with a fixed list of languages, checked once by
// New. Tests and tools use it where a site would pass its Site settings.
type Service struct{ config Config }

// Normalize returns the canonical form of a BCP 47 language code, such as
// "en-GB" for "en-gb", so the same language is always stored the same way.
// It returns ErrInvalid for a code that is not a language tag; codes with
// an underscore, such as "en_US", are refused rather than guessed.
func Normalize(code string) (string, error) {
	if code == "" || len(code) > 100 || strings.ContainsAny(code, "_ \t\r\n") {
		return "", ErrInvalid
	}

	tag, err := language.Parse(code)

	if err != nil || tag.String() == "und" {
		return "", ErrInvalid
	}

	return tag.String(), nil
}

// Validate checks a list of languages and returns it normalized, in the
// order given, as editors chose it. The input is copied, never changed. It
// returns ErrConfiguration when the list cannot work; see that error.
func Validate(config Config) (Config, error) {
	defaultCode, err := Normalize(config.Default)

	if err != nil || len(config.Enabled) == 0 || len(config.Enabled) > 100 {
		return Config{}, ErrConfiguration
	}

	out := Config{Default: defaultCode, Enabled: make([]string, 0, len(config.Enabled))}
	seen := map[string]bool{}

	for _, code := range config.Enabled {
		code, err = Normalize(code)

		if err != nil || seen[code] {
			return Config{}, ErrConfiguration
		}

		seen[code] = true
		out.Enabled = append(out.Enabled, code)
	}

	if !seen[defaultCode] {
		return Config{}, ErrConfiguration
	}

	return out, nil
}

// New returns a Service for config after checking it with Validate, so a
// mistake in the list fails at once with ErrConfiguration.
func New(config Config) (*Service, error) {
	config, err := Validate(config)
	if err != nil {
		return nil, err
	}

	return &Service{config: config}, nil
}

// Configuration implements Provider. It returns a copy, so callers may
// change the list without changing the Service.
func (s *Service) Configuration(ctx context.Context) (Config, error) {
	if err := ctx.Err(); err != nil {
		return Config{}, err
	}

	return Config{Default: s.config.Default, Enabled: append([]string(nil), s.config.Enabled...)}, nil
}

// Require returns the normalized form of code when it is one of the
// enabled languages. It returns ErrInvalid for a code that is no language
// and ErrDisabled for a language the site does not offer.
func (c Config) Require(code string) (string, error) {
	normalized, err := Normalize(code)
	if err != nil {
		return "", err
	}

	for _, enabled := range c.Enabled {
		if enabled == normalized {
			return normalized, nil
		}
	}

	return "", ErrDisabled
}
