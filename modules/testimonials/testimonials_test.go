package testimonials_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/modules/testimonials"
	"github.com/donseba/go-webthings/sitetest"
)

func TestTestimonials(t *testing.T) {
	s := sitetest.StartWith(
		t,
		sitetest.Options{Templates: "testdata/templates"},
		testimonials.Module("testimonials", "Testimonials"),
	)
	ctx := context.Background()
	photo := s.Upload("alice.png", sitetest.PNG(t, 2, 2)).ID

	first := s.Submit("/admin/testimonials/new?locale=en", url.Values{
		"Quote":        {"They built our website in a week."},
		"Author":       {"Alice"},
		"Role":         {"Director"},
		"Organisation": {"Acme"},
		"Photo":        {photo},
	})

	s.Submit(first+"?locale=nl", url.Values{
		"Quote":        {"Ze bouwden onze website in een week."},
		"Author":       {"Alice"},
		"Role":         {"Directeur"},
		"Organisation": {"Acme"},
		"Photo":        {photo},
	})

	// The author's name, role and organisation are stored without the
	// spaces around them.
	second := s.Submit("/admin/testimonials/new?locale=en", url.Values{
		"Quote":        {"Friendly and fast."},
		"Author":       {"  Bob "},
		"Organisation": {" "},
	})

	status, body := s.PostStatus("/admin/testimonials/new?locale=en", "/admin/testimonials/new?locale=en", url.Values{
		"Quote":  {"Nobody said this."},
		"Author": {"   "},
	})
	// An author of only spaces is missing: the form refuses it as required.
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "This field is required.") {
		t.Fatalf("blank author: %d\n%s", status, body)
	}

	s.Publish(first + "?locale=en")
	page := s.MountIn("testimonials",
		sitetest.Translation{Locale: "en", Title: "Testimonials", Slug: "testimonials"},
		sitetest.Translation{Locale: "nl", Title: "Referenties", Slug: "testimonials"},
	)

	// Only the published testimonial is listed, each language with its own text.
	s.Expect("/en/testimonials", http.StatusOK,
		"<h1>Testimonials</h1>",
		"They built our website in a week. — Alice, Director, Acme",
		`<img src="/media/`,
	)
	s.ExpectMissing("/en/testimonials", "Friendly and fast.")
	s.ExpectMissing("/en/testimonials", "Ze bouwden")
	s.Expect("/nl/testimonials", http.StatusOK, "<h1>Referenties</h1>", "Ze bouwden onze website in een week. — Alice, Directeur")
	s.ExpectMissing("/nl/testimonials", "They built")

	// The second testimonial has no Dutch text, so it is shown in English only.
	s.Publish(second + "?locale=en")
	s.Expect("/en/testimonials", http.StatusOK, "Friendly and fast. — Bob, , </blockquote>")
	s.ExpectMissing("/nl/testimonials", "Friendly and fast.")

	service := testimonials.Of(s.App, "testimonials")
	expectQuotes(t, service, "en", "They built our website in a week.", "Friendly and fast.")
	expectQuotes(t, service, "nl", "Ze bouwden onze website in een week.")

	list, err := service.List(ctx, "en")
	if err != nil || list[0].PhotoURL == "" || list[1].PhotoURL != "" || list[0].Author != "Alice" {
		t.Fatalf("list: %+v %v", list, err)
	}

	// Moving Bob up changes the order on the website and in the service.
	s.Post("/admin/testimonials?locale=en", second+"/up?locale=en", url.Values{})
	expectQuotes(t, service, "en", "Friendly and fast.", "They built our website in a week.")

	_, body = s.Get("/en/testimonials")
	if strings.Index(body, "Friendly") > strings.Index(body, "They built") {
		t.Fatalf("order on the website:\n%s", body)
	}

	random, err := service.Random(ctx, "en", 1)
	if err != nil || len(random) != 1 {
		t.Fatalf("random: %+v %v", random, err)
	}

	// A limit of 0 or less means all testimonials, for Random and for the
	// template function, which counts from the top of the list.
	for _, limit := range []int{0, -1} {
		random, err := service.Random(ctx, "en", limit)
		if err != nil || len(random) != 2 {
			t.Fatalf("random %d: %+v %v", limit, random, err)
		}
	}

	s.Expect("/en/", http.StatusOK, `<p class="all">2</p>`, `<p class="negative">2</p>`, `<p class="first">Bob</p>`)

	// Signed-in editors get a link to the list in the admin.
	s.Expect("/en/testimonials", http.StatusOK, `id="edit" href="/admin/testimonials?locale=en"`)
	s.Visitor().ExpectMissing("/en/testimonials", `id="edit"`)

	// A draft of the page does not change the list's title on the website.
	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	s.Submit(
		page+"?locale=en",
		url.Values{"Title": {"Secret new name"}, "Slug": {"testimonials"}, "Parent": {home}, "Module": {"testimonials"}},
	)
	s.Expect("/en/testimonials", http.StatusOK, "<h1>Testimonials</h1>")
	s.ExpectMissing("/en/testimonials", "Secret new name")
}

func expectQuotes(t *testing.T, service *testimonials.Service, locale string, quotes ...string) {
	t.Helper()

	list, err := service.List(context.Background(), locale)
	if err != nil {
		t.Fatal(err)
	}

	var got []string

	for _, testimonial := range list {
		got = append(got, testimonial.Quote)
	}

	if strings.Join(got, "|") != strings.Join(quotes, "|") {
		t.Fatalf("%s: want %q, got %q", locale, quotes, got)
	}
}
