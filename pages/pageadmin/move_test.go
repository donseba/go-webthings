package pageadmin_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

// TestDragPages checks the endpoint behind dragging pages in the overview:
// reordering changes the menu at once, a new parent is saved as a draft
// with a notice, refused places explain why, and only editors with the
// right to edit get the handles and may post.
func TestDragPages(t *testing.T) {
	s := sitetest.Start(t, publishing.Module("news", "News"))

	home, err := s.App.Pages.Home(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	about := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About"}, "Parent": {home}})
	team := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"Team"}, "Parent": {id(about)}})
	contact := s.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"Contact"}, "Parent": {home}})
	s.Mount("news", "News", "news")
	s.Publish(about + "?locale=en")
	s.Publish(team + "?locale=en")
	s.Publish(contact + "?locale=en")

	overview := "/admin/pages?locale=en"
	move := func(page string) string {
		return page + "/move?locale=en"
	}

	// Every page but Home has a handle and a place to post to; the module
	// page takes no children. The script and its stylesheet are linked.
	_, body := s.Get(overview)
	for _, part := range []string{
		`data-move="` + move(contact) + `"`,
		`data-tree-handle`,
		`data-leaf`,
		`src="/admin/assets/tree.js?v=`,
		`/admin/assets/sections/pages.css`,
	} {
		if !strings.Contains(body, part) {
			t.Fatalf("overview misses %s:\n%s", part, body)
		}
	}

	if strings.Contains(body, `data-move="`+move("/admin/pages/"+home)+`"`) {
		t.Fatalf("home can be dragged:\n%s", body)
	}

	s.Expect("/admin/assets/tree.js", http.StatusOK, "data-tree-handle")

	// A reorder: Contact first below Home, in the menu at once.
	s.Post(overview, move(contact), url.Values{"parent": {home}, "index": {"0"}})

	_, body = s.Get(overview)
	inOrder(t, body, ">Home<", "Contact", "About", "Team", "News")

	visitor := s.Visitor()
	_, page := visitor.Get("/en/")
	inOrder(t, page, `href="/en/contact"`, `href="/en/about"`, `href="/en/news"`)

	// A new parent: Team below Contact is a draft, with a notice; the
	// website keeps the old address until it is published.
	_, body = s.PostStatus(overview, move(team), url.Values{"parent": {id(contact)}, "index": {"0"}})
	if !strings.Contains(body, "The new place is saved as a draft.") {
		t.Fatalf("no draft notice:\n%s", body)
	}

	// Without HTMX the notice travels through the redirect, still a warning.
	draftWarning := "The new place is saved as a draft. Publish the page to move it on the website; its address changes then."
	if !sitetest.HasToast(body, "warn", draftWarning) {
		t.Fatalf("the draft notice is no warning:\n%s", body)
	}

	inOrder(t, body, ">Home<", "Contact", "Team", "/contact/team", "Changes not published", "About", "News")
	visitor.Expect("/en/about/team", http.StatusOK)

	s.Publish(team + "?locale=en")
	visitor.Expect("/en/contact/team", http.StatusOK)

	// Refused places keep the tree and say why.
	nodes, err := s.App.Pages.Tree(context.Background(), "en")
	if err != nil {
		t.Fatal(err)
	}

	var news string

	for _, node := range nodes {
		if node.Page.Fields.Module == "news" {
			news = node.Page.ID
		}
	}

	refused := []struct {
		page   string
		values url.Values
		status int
		reason string
	}{
		{
			contact,
			url.Values{"parent": {id(team)}, "index": {"0"}},
			http.StatusUnprocessableEntity,
			"cannot be placed below itself",
		},
		{about, url.Values{"parent": {news}, "index": {"0"}}, http.StatusUnprocessableEntity, "cannot have child pages"},
		{
			"/admin/pages/" + home,
			url.Values{"parent": {id(about)}, "index": {"0"}},
			http.StatusUnprocessableEntity,
			"cannot be placed below itself",
		},
		{about, url.Values{"parent": {""}, "index": {"0"}}, http.StatusUnprocessableEntity, "Choose a parent page."},
		{about, url.Values{"parent": {home}, "index": {"first"}}, http.StatusBadRequest, ""},
		{about, url.Values{"parent": {home}, "index": {"-1"}}, http.StatusBadRequest, ""},
		{
			"/admin/pages/0190a000-0000-7000-8000-00000000ffff",
			url.Values{"parent": {home}, "index": {"0"}},
			http.StatusNotFound,
			"",
		},
	}

	for _, attempt := range refused {
		status, body := s.PostStatus(overview, move(attempt.page), attempt.values)
		if status != attempt.status || !strings.Contains(body, attempt.reason) {
			t.Fatalf("POST %s %v: %d\n%s", move(attempt.page), attempt.values, status, body)
		}
	}

	_, body = s.Get(overview)
	inOrder(t, body, ">Home<", "Contact", "Team", "About", "News")
}

// TestDragPagesNeedsEdit checks that editors who may only read pages get no
// handles and cannot post a new place.
func TestDragPagesNeedsEdit(t *testing.T) {
	admin := sitetest.Start(t)

	home, err := admin.App.Pages.Home(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	about := admin.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About"}, "Parent": {home}})

	group := admin.Submit("/admin/groups", url.Values{"Name": {"Readers"}})
	admin.Post(group, group, url.Values{"name": {"Readers"}, "allow": {"pages:read"}})

	account := admin.Submit("/admin/accounts", url.Values{
		"Email":    {"reader@example.test"},
		"Password": {"reader long passphrase"},
		"Role":     {"editor"},
	})
	admin.Post(account, account+"/groups", url.Values{"group": {group[strings.LastIndex(group, "/")+1:]}})

	reader := admin.As("reader@example.test", "reader long passphrase")
	overview := "/admin/pages?locale=en"

	reader.ExpectMissing(overview, "data-tree-handle")
	reader.ExpectMissing(overview, "data-move=")

	status, _ := reader.PostStatus(overview, about+"/move?locale=en", url.Values{"parent": {home}, "index": {"0"}})
	if status != http.StatusForbidden {
		t.Fatalf("reader moved a page: %d", status)
	}
}
