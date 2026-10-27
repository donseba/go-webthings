package editor_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/content"
)

// marks is an in-memory admin.Permanence.
type marks struct {
	mu    sync.Mutex
	items map[string]bool
}

func (m *marks) Permanent(_ context.Context, section, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.items[section+"/"+id], nil
}

func (m *marks) SetPermanent(_ context.Context, section, id string, permanent bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.items == nil {
		m.items = map[string]bool{}
	}

	m.items[section+"/"+id] = permanent

	return nil
}

func peopleConfig(people *store) editor.Config[person, text, personInput] {
	return editor.Config[person, text, personInput]{
		Store: people,
		Label: func(value text) string {
			return value.Role
		},
	}
}

// signInEditor creates an editor with every right on people and signs in.
func signInEditor(t *testing.T, b browser, accounts *auth.Service) browser {
	t.Helper()
	ctx := context.Background()

	user, err := accounts.CreateUser(ctx, "editor@example.test", "", "editor long passphrase", auth.Editor)
	if err != nil {
		t.Fatal(err)
	}

	group, err := accounts.SaveGroup(ctx, auth.Group{
		Name:        "Editors",
		Permissions: auth.Permissions{"people": auth.Actions},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := accounts.SetUserGroups(ctx, user.ID, []string{group.ID}); err != nil {
		t.Fatal(err)
	}

	return signIn(t, b.server, "editor@example.test", "editor long passphrase")
}

const (
	markedNL = "Dit item is permanent: je kunt de publicatie niet intrekken, niet inplannen dat die wordt " +
		"ingetrokken en het item niet verwijderen, tot een beheerder de markering weghaalt."
	fixedNL = "Dit item blijft altijd op de website: je kunt de publicatie niet intrekken, niet inplannen " +
		"dat die wordt ingetrokken en het item niet verwijderen."
)

// TestPermanentMark checks that an administrator can mark an item
// permanent and remove the mark, with a confirmation; that the editor then
// offers and accepts no unpublish, planned unpublication or delete; and
// that editors cannot change the mark.
func TestPermanentMark(t *testing.T) {
	t.Parallel()

	published := ada()
	published.Published = true
	published.PublishedID = "r1"
	published.PublishedNumber = 1

	people := newStore(published)
	store := &marks{}
	b, accounts := serveWith(t, peopleConfig(people), admin.Options{Permanence: store})

	_, body := b.get("/admin/people/ada")
	if !strings.Contains(body, `action="/admin/people/ada/permanent?locale=en"`) ||
		!strings.Contains(body, `hx-confirm="Dit item permanent maken?`) {
		t.Fatalf("the owner is not offered the permanent mark with a confirmation:\n%s", body)
	}

	status, body := b.post("/admin/people/ada", "/admin/people/ada/permanent?locale=en", url.Values{})
	if status != http.StatusOK || !strings.Contains(body, "Dit item is nu permanent.") {
		t.Fatalf("mark: %d\n%s", status, body)
	}

	for _, offered := range []string{"/admin/people/ada/delete", "/admin/people/ada/unpublish", `name="UnpublishAt"`} {
		if strings.Contains(body, offered) {
			t.Fatalf("a permanent item offers %s", offered)
		}
	}

	if !strings.Contains(body, markedNL) || !strings.Contains(body, `action="/admin/people/ada/permanent/off?locale=en"`) {
		t.Fatal("the editor does not explain the mark or offer to remove it")
	}

	// Posted anyway, from a page opened before, the changes are refused.
	later := time.Now().Add(time.Hour).Format("2006-01-02T15:04")
	attempts := map[string]url.Values{
		"/admin/people/ada/delete":                {},
		"/admin/people/ada/unpublish":             {},
		"/admin/people/ada/revisions/r1/schedule": {"UnpublishAt": {later}},
	}

	for action, values := range attempts {
		status, refused := b.post("/admin/people/ada", action, values)
		if status != http.StatusUnprocessableEntity || !strings.Contains(refused, markedNL) {
			t.Fatalf("POST %s: %d", action, status)
		}
	}

	if it, err := people.Load(context.Background(), "ada", ""); err != nil || !it.Published {
		t.Fatalf("the permanent item changed: %+v %v", it, err)
	}

	// Planning a publication stays possible.
	status, _ = b.post("/admin/people/ada", "/admin/people/ada/revisions/r1/schedule", url.Values{"PublishAt": {later}})
	if status != http.StatusOK {
		t.Fatalf("plan a publication of a permanent item: %d", status)
	}

	// An editor with every right may not remove the mark.
	e := signInEditor(t, b, accounts)

	_, body = e.get("/admin/people/ada")
	if strings.Contains(body, "/permanent") || !strings.Contains(body, markedNL) {
		t.Fatal("an editor is offered the permanent mark")
	}

	status, body = e.post("/admin/people/ada", "/admin/people/ada/permanent/off", url.Values{})
	if status != http.StatusForbidden || !strings.Contains(body, "Je hebt hier geen toestemming voor.") {
		t.Fatalf("an editor removed the mark: %d", status)
	}

	status, body = b.post("/admin/people/ada", "/admin/people/ada/permanent/off?locale=en", url.Values{})
	if status != http.StatusOK || !strings.Contains(body, "Dit item is niet langer permanent.") ||
		!strings.Contains(body, "/admin/people/ada/delete") {
		t.Fatalf("remove the mark: %d", status)
	}

	status, _ = b.post("/admin/people/ada", "/admin/people/ada/delete", url.Values{})
	if _, err := people.Load(context.Background(), "ada", ""); status != http.StatusOK || err == nil {
		t.Fatalf("delete after the mark was removed: %d %v", status, err)
	}
}

// TestPermanentWithPlannedUnpublication checks that an item is not marked
// permanent while its unpublication is planned.
func TestPermanentWithPlannedUnpublication(t *testing.T) {
	t.Parallel()

	planned := ada()
	planned.Schedule = content.Schedule{UnpublishAt: time.Now().Add(time.Hour)}

	store := &marks{}
	b, _ := serveWith(t, peopleConfig(newStore(planned)), admin.Options{Permanence: store})

	status, body := b.post("/admin/people/ada", "/admin/people/ada/permanent", url.Values{})
	if status != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "Annuleer eerst de geplande intrekking: een permanent item blijft op de website.") {
		t.Fatalf("mark with a planned unpublication: %d", status)
	}

	if marked, _ := store.Permanent(context.Background(), "people", "ada"); marked {
		t.Fatal("the item was marked")
	}
}

// TestPermanentByNature checks that items Config.Permanent names are
// refused the changes too, also without a store of marks, and that their
// mark cannot be removed.
func TestPermanentByNature(t *testing.T) {
	t.Parallel()

	config := peopleConfig(newStore(ada()))
	config.Permanent = func(it item) bool {
		return it.ID == "ada"
	}

	b, _ := serveWith(t, config, admin.Options{Permanence: &marks{}})

	_, body := b.get("/admin/people/ada")
	if strings.Contains(body, "/permanent") || !strings.Contains(body, fixedNL) {
		t.Fatal("an item that is permanent by its nature offers the mark")
	}

	for _, action := range []string{"/admin/people/ada/delete", "/admin/people/ada/permanent/off"} {
		status, refused := b.post("/admin/people/ada", action, url.Values{})
		if status != http.StatusUnprocessableEntity || !strings.Contains(refused, fixedNL) {
			t.Fatalf("POST %s: %d", action, status)
		}
	}

	// Without a store nothing can be marked.
	plain, _ := serve(t, peopleConfig(newStore(ada())))

	_, body = plain.get("/admin/people/ada")
	if strings.Contains(body, "/permanent") {
		t.Fatal("the mark is offered without a store")
	}

	if status, _ := plain.post("/admin/people/ada", "/admin/people/ada/permanent", url.Values{}); status != http.StatusNotFound {
		t.Fatalf("mark without a store: %d", status)
	}
}
