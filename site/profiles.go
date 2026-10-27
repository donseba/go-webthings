package site

import (
	"errors"
	"net/url"
	"slices"
	"strings"

	form "github.com/donseba/go-form/v2"
)

// Profile is one of the site's social media profiles.
type Profile struct {
	// Network is one of Networks, for example "instagram".
	Network string
	// URL is the profile's web address.
	URL string
}

// Networks are the networks the site can have a profile on, in the order the
// admin and templates show them.
var Networks = []string{
	"facebook",
	"instagram",
	"linkedin",
	"x",
	"mastodon",
	"bluesky",
	"threads",
	"youtube",
	"tiktok",
	"pinterest",
	"github",
	"whatsapp",
	"telegram",
}

// networkNames are the networks' own names, including the networks pages can
// be shared on (see frontend share links).
var networkNames = map[string]string{
	"facebook":  "Facebook",
	"instagram": "Instagram",
	"linkedin":  "LinkedIn",
	"x":         "X",
	"mastodon":  "Mastodon",
	"bluesky":   "Bluesky",
	"threads":   "Threads",
	"youtube":   "YouTube",
	"tiktok":    "TikTok",
	"pinterest": "Pinterest",
	"github":    "GitHub",
	"whatsapp":  "WhatsApp",
	"telegram":  "Telegram",
	"email":     "E-mail",
}

// NetworkName is the English name of a social network, for example
// "LinkedIn" for "linkedin", or the network itself when it is unknown.
// Templates translate it with the site's translations.
func NetworkName(network string) string {
	if name, ok := networkNames[network]; ok {
		return name
	}

	return network
}

// ErrProfile is returned for a profile on an unknown network, or with an
// address that is not a web address.
var ErrProfile = errors.New("invalid_site_profile")

// profilePrefix starts the stored name of a profile, for example
// "profile_instagram"; the value is its address.
const profilePrefix = "profile_"

// CleanProfileURL checks a profile address and completes it: an address
// without a scheme gets https://, so editors can paste "instagram.com/me".
func CleanProfileURL(value string) (string, error) {
	value = strings.TrimSpace(value)

	if !strings.Contains(value, "://") {
		value = "https://" + value
	}

	if len(value) > 1000 || !form.ValidURL("http,https", value) {
		return "", ErrProfile
	}

	// An address with a user name, such as https://me@instagram.com, may
	// hide where it leads.
	if parsed, err := url.Parse(value); err != nil || parsed.User != nil {
		return "", ErrProfile
	}

	return value, nil
}

// cleanProfiles validates the profiles, completes their addresses, drops
// empty ones and puts them in the order of Networks.
func cleanProfiles(profiles []Profile) ([]Profile, error) {
	var out []Profile

	for _, profile := range profiles {
		if strings.TrimSpace(profile.URL) == "" {
			continue
		}

		if !slices.Contains(Networks, profile.Network) {
			return nil, ErrProfile
		}

		address, err := CleanProfileURL(profile.URL)
		if err != nil {
			return nil, err
		}

		if slices.ContainsFunc(out, func(existing Profile) bool {
			return existing.Network == profile.Network
		}) {
			return nil, ErrProfile
		}

		out = append(out, Profile{Network: profile.Network, URL: address})
	}

	sortProfiles(out)

	return out, nil
}

// sortProfiles orders profiles as Networks lists them.
func sortProfiles(profiles []Profile) {
	slices.SortStableFunc(profiles, func(a, b Profile) int {
		return slices.Index(Networks, a.Network) - slices.Index(Networks, b.Network)
	})
}

// Profile returns the address of the site's profile on network, or "".
func (s Settings) Profile(network string) string {
	for _, profile := range s.Profiles {
		if profile.Network == network {
			return profile.URL
		}
	}

	return ""
}
