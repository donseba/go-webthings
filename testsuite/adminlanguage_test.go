package testsuite

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/modules/banners"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/newsletter"
	"github.com/donseba/go-webthings/modules/redirects"
	"github.com/donseba/go-webthings/sitetest"
)

// dutchAdmin starts a site with modules whose admin is in Dutch.
func dutchAdmin(t *testing.T, modules ...webthings.Module) *sitetest.Site {
	t.Helper()

	return sitetest.StartWith(t, sitetest.Options{
		Configure: func(config *webthings.Config) {
			config.AdminLanguage = "nl"
		},
	}, modules...)
}

// dutchDateTime is a date and time as a Dutch admin shows it.
var dutchDateTime = regexp.MustCompile(`\d{1,2} (jan|feb|mrt|apr|mei|jun|jul|aug|sep|okt|nov|dec) \d{4} \d{2}:\d{2}`)

// TestDutchAccounts checks that a Dutch admin names roles and statuses in
// Dutch, labels the role choices in Dutch and shows dates in Dutch.
func TestDutchAccounts(t *testing.T) {
	t.Parallel()

	s := dutchAdmin(t)

	_, body := s.Get("/admin/accounts")

	link := regexp.MustCompile(`href="(/admin/accounts/[^"]+)"`).FindStringSubmatch(body)
	if link == nil {
		t.Fatalf("no account in:\n%s", body)
	}

	s.Expect("/admin/accounts", http.StatusOK, "Beheerder", "Actief", "Redacteur (rechten via groepen)</option>")
	s.ExpectMissing("/admin/accounts", ">admin<")
	s.ExpectMissing("/admin/accounts", ">active<")
	s.ExpectMissing("/admin/accounts", "Editor (rights from groups)")

	_, body = s.Get(link[1])
	if !dutchDateTime.MatchString(body) {
		t.Fatalf("account dates are not Dutch:\n%s", body)
	}

	s.Expect("/admin/account", http.StatusOK, "Beheerder")
}

// TestSiteSettingsChoices checks the choices of the site settings: the
// timezone is one of the IANA zones, the default language one of the
// enabled languages and the languages are ticked from a list with their
// names in the admin language, or written as codes.
func TestSiteSettingsChoices(t *testing.T) {
	t.Parallel()

	s := dutchAdmin(t)
	ctx := context.Background()

	// html/template writes the "+" of the offsets as "&#43;".
	s.Expect("/admin/site", http.StatusOK,
		`<select`, `value="Europe/Amsterdam"`, "Europe/Amsterdam (UTC&#43;0",
		`type="checkbox"`, "Nederlands (nl)", "Deutsch (de)", "English (en)", "Andere talen")

	// A zone that does not exist is refused.
	status, _ := s.PostStatus("/admin/site", "/admin/site?locale=en", url.Values{
		"DefaultLocale": {"en"},
		"Locales":       {"en", "nl"},
		"Timezone":      {"Mars/Olympus_Mons"},
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown zone: %d", status)
	}

	settings, err := s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if settings.Timezone == "Mars/Olympus_Mons" {
		t.Fatal("unknown zone saved")
	}

	// Ticked languages keep the site's order, new ones follow, and written
	// codes are normalized.
	s.Post("/admin/site", "/admin/site?locale=en", url.Values{
		"DefaultLocale": {"nl"},
		"Locales":       {"de", "en", "nl"},
		"OtherLocales":  {"fy, PT-br"},
		"Timezone":      {"Europe/Amsterdam"},
	})

	settings, err = s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"en", "nl", "de", "fy", "pt-BR"}
	if settings.DefaultLocale != "nl" || !slices.Equal(settings.Locales, want) || settings.Timezone != "Europe/Amsterdam" {
		t.Fatalf("saved %q %q %q, want nl %q Europe/Amsterdam", settings.DefaultLocale, settings.Locales, settings.Timezone, want)
	}

	// Written languages are listed from then on, ticked.
	_, body := s.Get("/admin/site")

	for _, code := range want {
		checked := regexp.MustCompile(`value="` + code + `"\s+checked`)
		if !checked.MatchString(body) {
			t.Errorf("%s is not ticked", code)
		}
	}

	if !strings.Contains(body, "Português (pt-BR)") {
		t.Errorf("pt-BR is not named in Dutch:\n%s", body)
	}

	// The default language offers every listed language, so one ticked in
	// the same save can become the default.
	if !regexp.MustCompile(`<option value="fr"`).MatchString(body) {
		t.Fatalf("a language that is not enabled cannot be chosen as default:\n%s", body)
	}

	s.Post("/admin/site", "/admin/site?locale=en", url.Values{
		"DefaultLocale": {"fr"},
		"Locales":       {"en", "nl", "fr"},
		"Timezone":      {"Europe/Amsterdam"},
	})

	settings, err = s.App.Site.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if settings.DefaultLocale != "fr" || !slices.Contains(settings.Locales, "fr") {
		t.Fatalf("new default language: %q %q", settings.DefaultLocale, settings.Locales)
	}

	// The default language must be ticked; the error shows at the field.
	status, body = s.PostStatus("/admin/site", "/admin/site?locale=en", url.Values{
		"DefaultLocale": {"nl"},
		"Locales":       {"en"},
		"Timezone":      {"Europe/Amsterdam"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Vink de standaardtaal ook aan bij Talen.") {
		t.Fatalf("disabled default language: %d\n%s", status, body)
	}

	// A refusal of the whole form shows as a toast instead of inside the page.
	status, body = s.PostStatus("/admin/site", "/admin/site?locale=en", url.Values{
		"DefaultLocale": {"fr"},
		"Locales":       {"fr"},
		"OtherLocales":  {"not a language"},
		"Timezone":      {"Europe/Amsterdam"},
	})
	if status != http.StatusUnprocessableEntity ||
		!sitetest.HasToast(
			body,
			"error",
			"Controleer de talen en tijdzone: de standaardtaal moet zijn ingeschakeld en de tijdzone moet bestaan.",
		) {
		t.Fatalf("invalid language code: %d\n%s", status, body)
	}

	if strings.Contains(body, `class="notice mb-6"`) {
		t.Fatal("the refusal is still shown inside the page")
	}
}

// TestDutchModuleChoices checks that the choices modules offer in their
// forms are labelled in Dutch for a Dutch admin, with unchanged values.
func TestDutchModuleChoices(t *testing.T) {
	t.Parallel()

	s := dutchAdmin(t,
		banners.Module("banners", "Banners"),
		redirects.Module(),
		newsletter.Module("news", "Nieuwsbrief"),
		events.Module("agenda", "Agenda"),
	)

	s.Expect("/admin/banners/new?locale=en", http.StatusOK,
		`value="info"`, "Informatie</option>", "Succes</option>", "Waarschuwing</option>")
	s.Expect("/admin/redirects/new", http.StatusOK,
		`value="301"`, "301 Permanent verplaatst</option>", "302 Gevonden (tijdelijk)</option>")
	s.Expect("/admin/news/mailings/new", http.StatusOK,
		"Alle talen</option>", `value="nl"`, "Nederlands (nl)</option>", "English (en)</option>")
	s.Expect("/admin/agenda/new?locale=en", http.StatusOK,
		`<select`, `value="Europe/Amsterdam"`, `value="UTC"`)
}
