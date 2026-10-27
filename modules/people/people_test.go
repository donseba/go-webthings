package people_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/modules/people"
	"github.com/donseba/go-webthings/sitetest"
)

func TestPeople(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, people.Module("team", "Team"))
	ctx := context.Background()
	photo := s.Upload("jane.png", sitetest.PNG(t, 2, 2)).ID

	jane := s.Submit("/admin/team/new?locale=en", url.Values{
		"Name":     {"Jane Doe"},
		"Group":    {"Board"},
		"Role":     {"Chair"},
		"Bio":      {"Leads the *board*."},
		"Photo":    {photo},
		"Email":    {"jane@example.test"},
		"Phone":    {"+31 20 123 4567"},
		"Website":  {"https://jane.example.test"},
		"LinkedIn": {"https://www.linkedin.com/in/jane"},
	})

	// The Dutch tab keeps the shared fields and adds the translated ones.
	s.Submit(jane+"?locale=nl", url.Values{
		"Name":     {"Jane Doe"},
		"Slug":     {"jane-doe"},
		"Group":    {"Bestuur"},
		"Role":     {"Voorzitter"},
		"Photo":    {photo},
		"Email":    {"jane@example.test"},
		"Phone":    {"+31 20 123 4567"},
		"Website":  {"https://jane.example.test"},
		"LinkedIn": {"https://www.linkedin.com/in/jane"},
	})

	john := s.Submit("/admin/team/new?locale=en", url.Values{
		"Name":  {"John Smith"},
		"Group": {"Staff"},
		"Role":  {"Developer"},
	})

	// A second Jane Doe gets a free slug of her own.
	other := s.Submit("/admin/team/new?locale=en", url.Values{"Name": {"Jane Doe"}, "Group": {"Staff"}})

	// The admin list shows names with role and group, and invalid input comes
	// back with a message.
	s.Expect("/admin/team?locale=en", http.StatusOK, ">Jane Doe</a>", "Chair · Board", ">John Smith</a>", "Developer · Staff")
	s.Expect("/admin/team?locale=nl", http.StatusOK, ">Jane Doe</a>", "Voorzitter · Bestuur")

	code, body := s.PostStatus("/admin/team/new?locale=en", "/admin/team/new?locale=en", url.Values{
		"Name":  {"Bad Email"},
		"Email": {"not an email"},
	})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, `class="field-error"`) {
		t.Fatalf("invalid email: %d\n%s", code, body)
	}

	code, body = s.PostStatus("/admin/team/new?locale=en", "/admin/team/new?locale=en", url.Values{
		"Name": {"Copy"},
		"Slug": {"jane-doe"},
	})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "This web address is already used.") {
		t.Fatalf("taken slug: %d\n%s", code, body)
	}

	page := s.MountIn("team",
		sitetest.Translation{Locale: "en", Title: "Team", Slug: "team"},
		sitetest.Translation{Locale: "nl", Title: "Team", Slug: "team"},
	)
	s.ExpectMissing("/en/team", "Jane Doe")

	s.Publish(jane + "?locale=en")
	s.Publish(john + "?locale=en")

	// The list is grouped and links to each person's page.
	s.Expect("/en/team", http.StatusOK,
		"<h1>Team</h1>",
		"<h2>Board</h2>",
		`<a class="person" href="/en/team/jane-doe">Jane Doe</a> Chair <img src="/media/`,
		"<h2>Staff</h2>",
		`<a class="person" href="/en/team/john-smith">John Smith</a> Developer`,
	)
	s.ExpectMissing("/en/team", "jane-doe-2")

	s.Expect("/en/team/jane-doe", http.StatusOK,
		"<h1>Jane Doe</h1>",
		"Chair · Board",
		"<em>board</em>",
		`href="mailto:jane@example.test"`,
		`<a class="phone" href="tel:&#43;31201234567">&#43;31 20 123 4567</a>`,
		`href="https://www.linkedin.com/in/jane"`,
		`hreflang="nl" href="/nl/team/jane-doe"`,
	)
	s.Expect("/nl/team/jane-doe", http.StatusOK, "Voorzitter · Bestuur")

	// Signed-in editors get a link to the person in the admin; visitors do not.
	s.Expect("/en/team/jane-doe", http.StatusOK, `id="edit" href="`+jane+`?locale=en"`)
	s.Expect("/en/team", http.StatusOK, `id="edit" href="/admin/team?locale=en"`)
	s.Visitor().ExpectMissing("/en/team/jane-doe", `id="edit"`)

	// The site search finds published people by name, role and biography.
	s.Expect("/en/search?q=chair", http.StatusOK, `class="result people"><a href="/en/team/jane-doe">Jane Doe</a><p>Chair</p>`)
	s.Expect("/en/search?q=leads+board", http.StatusOK, `<a href="/en/team/jane-doe">Jane Doe</a><p>Leads the board.</p>`)
	s.Expect("/en/search?q=smith", http.StatusOK, `<a href="/en/team/john-smith">John Smith</a>`)
	s.Expect("/nl/search?q=voorzitter", http.StatusOK, `<a href="/nl/team/jane-doe">Jane Doe</a>`)
	s.Expect("/nl/search?q=smith", http.StatusOK, "nothing found")
	s.Expect("/nl/team", http.StatusOK, "<h2>Bestuur</h2>")
	s.ExpectMissing("/nl/team", "John Smith")
	s.Expect("/nl/team/john-smith", http.StatusNotFound)
	s.Expect("/en/team/jane-doe-2", http.StatusNotFound)
	s.Expect("/en/team/nobody", http.StatusNotFound)

	s.Publish(other + "?locale=en")
	s.Expect("/en/team/jane-doe-2", http.StatusOK, "<h1>Jane Doe</h1>")

	// Moving John up changes the order on the website.
	s.Post("/admin/team?locale=en", john+"/up?locale=en", url.Values{})

	_, body = s.Get("/en/team")
	if strings.Index(body, "John Smith") > strings.Index(body, "Board") {
		t.Fatalf("order on the website:\n%s", body)
	}

	s.Expect("/sitemap.xml", http.StatusOK, "/en/team/jane-doe</loc>", "/en/team/john-smith</loc>", "/nl/team/jane-doe</loc>")
	s.ExpectMissing("/sitemap.xml", "/nl/team/john-smith")

	// Template functions show people anywhere, here on the home page.
	s.Expect("/en/", http.StatusOK,
		`<ul class="everyone"><li>John Smith</li><li>Jane Doe</li><li>Jane Doe</li></ul>`,
		`<ul class="board"><li>Jane Doe</li></ul>`,
		`<a class="featured" href="/en/team/jane-doe">Jane Doe</a>`,
	)
	s.ExpectMissing("/en/", `class="nobody"`)

	service := people.Of(s.App, "team")

	board, err := service.InGroup(ctx, "nl", "bestuur")
	if err != nil || len(board) != 1 || board[0].Role != "Voorzitter" || board[0].URL != "/nl/team/jane-doe" {
		t.Fatalf("in group: %+v %v", board, err)
	}

	if _, err := service.BySlug(ctx, "en", "nobody"); err == nil {
		t.Fatal("found nobody")
	}

	// A draft of the page does not change the list's title on the website.
	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	s.Submit(page+"?locale=en", url.Values{"Title": {"Secret new name"}, "Slug": {"team"}, "Parent": {home}, "Module": {"team"}})
	s.Expect("/en/team", http.StatusOK, "<h1>Team</h1>")
	s.ExpectMissing("/en/team", "Secret new name")
}

// TestPeopleOnNoPage lists the people of an instance on no page without
// addresses, instead of links to a page that does not exist.
func TestPeopleOnNoPage(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, people.Module("unlisted", "Unlisted"))
	ctx := context.Background()

	ada := s.Submit("/admin/unlisted/new?locale=en", url.Values{"Name": {"Ada Lovelace"}, "Role": {"Analyst"}})
	s.Publish(ada + "?locale=en")

	service := people.Of(s.App, "unlisted")

	list, err := service.List(ctx, "en")
	if err != nil || len(list) != 1 || list[0].Name != "Ada Lovelace" || list[0].URL != "" {
		t.Fatalf("list: %+v %v", list, err)
	}

	one, err := service.BySlug(ctx, "en", "ada-lovelace")
	if err != nil || one.Role != "Analyst" || one.URL != "" {
		t.Fatalf("by slug: %+v %v", one, err)
	}
}
