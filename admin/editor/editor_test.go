package editor_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/locales"
)

type person struct {
	Name string
}

type text struct {
	Role string
}

type personInput struct {
	Role  string `form:"input,text" label:"Role"`
	Notes string `form:"textarea"   label:"Notes"`
}

type item = content.Item[person, text]

// store is an in-memory, revisioned content store with one revision per item
// and scheduling, enough to drive the editor.
type store struct {
	mu        sync.Mutex
	items     map[string]item
	trash     map[string]item
	deleteErr error
	// undeleteErr refuses bringing back the item with this ID.
	undeleteErr map[string]error
	published   []string
	// recentLimits are the limits Recent was asked for.
	recentLimits []int
}

func newStore(items ...item) *store {
	s := &store{items: map[string]item{}, trash: map[string]item{}, undeleteErr: map[string]error{}}

	for _, it := range items {
		s.items[it.ID] = it
	}

	return s
}

func (s *store) Spec() content.Spec[person, text] {
	return content.Spec[person, text]{
		Complete: func(text) bool {
			return true
		},
	}
}

func (s *store) List(context.Context) ([]item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var list []item

	for _, it := range s.items {
		list = append(list, it)
	}

	return list, nil
}

func (s *store) Load(_ context.Context, id, _ string) (item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	it, ok := s.items[id]
	if !ok {
		return item{}, content.ErrNotFound
	}

	return it, nil
}

// Save keeps the texts of a draft of an existing item; it creates none.
func (s *store) Save(_ context.Context, draft content.Draft[person, text]) (item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	it, ok := s.items[draft.ID]
	if !ok {
		return item{}, errors.New("not used")
	}

	it.Translations = draft.Translations
	s.items[draft.ID] = it

	return it, nil
}

func (s *store) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deleteErr != nil {
		return s.deleteErr
	}

	it, ok := s.items[id]
	if !ok {
		return content.ErrNotFound
	}

	deleted := time.Now()
	it.DeletedAt = &deleted
	it.DeletedBy = "user-1"
	s.trash[id] = it
	delete(s.items, id)

	return nil
}

func (s *store) Trashed(context.Context) ([]item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var list []item

	for _, it := range s.trash {
		list = append(list, it)
	}

	slices.SortFunc(list, func(x, y item) int {
		return y.DeletedAt.Compare(*x.DeletedAt)
	})

	return list, nil
}

func (s *store) Undelete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	it, ok := s.trash[id]
	if !ok {
		return content.ErrNotFound
	}

	if err := s.undeleteErr[id]; err != nil {
		return err
	}

	it.DeletedAt = nil
	it.DeletedBy = ""
	s.items[id] = it
	delete(s.trash, id)

	return nil
}

func (s *store) Purge(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.trash[id]; !ok {
		return content.ErrNotFound
	}

	delete(s.trash, id)

	return nil
}

// Recent returns the items last updated first, as the real stores do.
func (s *store) Recent(ctx context.Context, limit int) ([]item, error) {
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.recentLimits = append(s.recentLimits, limit)
	s.mu.Unlock()

	slices.SortFunc(list, func(x, y item) int {
		return y.UpdatedAt.Compare(x.UpdatedAt)
	})

	return list[:min(len(list), limit)], nil
}

// Publish publishes the item and, like the real stores, cancels its schedule.
func (s *store) Publish(_ context.Context, id, _, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	it := s.items[id]
	it.Published = true
	it.PublishedID = it.Revision.ID
	it.PublishedNumber = it.Revision.Number
	it.Schedule = content.Schedule{}
	s.items[id] = it
	s.published = append(s.published, id)

	return nil
}

func (s *store) Unpublish(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	it := s.items[id]
	it.Published = false
	it.PublishedID = ""
	it.Schedule = content.Schedule{}
	s.items[id] = it

	return nil
}

func (s *store) Revisioned() bool {
	return true
}

func (s *store) History(ctx context.Context, id string) ([]content.Revision, error) {
	it, err := s.Load(ctx, id, "")
	if err != nil {
		return nil, err
	}

	return []content.Revision{it.Revision}, nil
}

func (s *store) Restore(context.Context, string, string, string) (item, error) {
	return item{}, errors.New("not used")
}

func (s *store) RunDue(context.Context, time.Time, string) (int, error) {
	return 0, nil
}

func (s *store) Schedule(context.Context, string, content.Schedule, string) error {
	return nil
}

func (s *store) Unschedule(context.Context, string) error {
	return nil
}

type browser struct {
	t      *testing.T
	client *http.Client
	server *httptest.Server
}

func (b browser) get(path string) (int, string) {
	b.t.Helper()

	response, err := b.client.Get(b.server.URL + path)
	if err != nil {
		b.t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)

	return response.StatusCode, string(body)
}

// post submits a form of the page at from, as a browser does: it opens the
// page, then posts from the same origin.
func (b browser) post(from, path string, values url.Values) (int, string) {
	b.t.Helper()

	if status, page := b.get(from); status != http.StatusOK {
		b.t.Fatalf("GET %s: %d\n%s", from, status, page)
	}

	request, err := http.NewRequest(http.MethodPost, b.server.URL+path, strings.NewReader(values.Encode()))
	if err != nil {
		b.t.Fatal(err)
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", b.server.URL)
	request.Header.Set("Sec-Fetch-Site", "same-origin")

	response, err := b.client.Do(request)
	if err != nil {
		b.t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)

	return response.StatusCode, string(body)
}

// run serves an admin in Dutch with the editor and signs in.
func run(t *testing.T, config editor.Config[person, text, personInput]) browser {
	t.Helper()

	b, _ := serve(t, config)

	return b
}

// serve serves an admin in Dutch with the editor, signs in as the owner
// and returns the accounts too, for tests with other users.
func serve(t *testing.T, config editor.Config[person, text, personInput]) (browser, *auth.Service) {
	t.Helper()

	return serveWith(t, config, admin.Options{})
}

// serveWith is serve with admin options, such as a store of permanent
// marks; the language and catalogue are set here.
func serveWith(t *testing.T, config editor.Config[person, text, personInput], options admin.Options) (browser, *auth.Service) {
	t.Helper()
	ctx := context.Background()
	database := dbtest.SQLite(t)

	if err := database.ApplySchema(ctx, "auth", auth.Schema()); err != nil {
		t.Fatal(err)
	}

	accounts, err := auth.New(database)
	if err != nil {
		t.Fatal(err)
	}

	if err := accounts.Bootstrap(ctx, "owner@example.test", "test-only long passphrase"); err != nil {
		t.Fatal(err)
	}

	languages, err := locales.New(locales.Config{Default: "en", Enabled: []string{"en"}})
	if err != nil {
		t.Fatal(err)
	}

	config.ID = "people"
	config.Title = "People"

	if config.NewLabel == "" {
		config.NewLabel = "New person"
	}

	config.Locales = languages
	config.Form = func(_ context.Context, it item, locale string) (personInput, error) {
		value, _ := it.Text(locale)

		return personInput{Role: value.Role}, nil
	}
	config.Apply = func(context.Context, personInput, *content.Draft[person, text], string) error {
		return nil
	}

	options.Language = "nl"

	if options.CatalogueDir == "" {
		options.CatalogueDir = t.TempDir()
	}

	backoffice, err := admin.New(accounts, options, editor.New(config))
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/admin/", backoffice)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return signIn(t, server, "owner@example.test", "test-only long passphrase"), accounts
}

// signIn returns a browser signed in to the admin at server. The browser
// uses the server's own transport: closing a test server also closes the
// idle connections of http.DefaultTransport, which breaks a request of a
// parallel test.
func signIn(t *testing.T, server *httptest.Server, email, password string) browser {
	t.Helper()

	jar, _ := cookiejar.New(nil)
	b := browser{t: t, client: &http.Client{Jar: jar, Transport: server.Client().Transport}, server: server}

	status, _ := b.post("/admin/login", "/admin/login", url.Values{
		"Email":    {email},
		"Password": {password},
	})
	if status != http.StatusOK {
		t.Fatalf("sign in: %d", status)
	}

	return b
}

func ada() item {
	return item{
		ID:           "ada",
		Fields:       person{Name: "Ada Lovelace"},
		Translations: []content.Translation[text]{{Locale: "en", Text: text{Role: "Analyst"}}},
		Revision:     content.Revision{ID: "r1", Number: 1, CreatedAt: time.Now()},
	}
}

func TestItemLabelAndNotes(t *testing.T) {
	t.Parallel()

	grace := ada()
	grace.ID = "grace"
	grace.Fields.Name = "Grace Hopper"

	var (
		mu    sync.Mutex
		calls []int
	)

	b := run(t, editor.Config[person, text, personInput]{
		Store: newStore(ada(), grace),
		Label: func(value text) string {
			return value.Role
		},
		ItemLabel: func(it item, _ string) string {
			return it.Fields.Name
		},
		// The admin runs in Dutch, so the context translates. The note of
		// Grace is a catalogue key too, but notes are shown as written.
		Notes: func(ctx context.Context, items []item, locale string) map[string]string {
			mu.Lock()
			calls = append(calls, len(items))
			mu.Unlock()

			return map[string]string{
				"ada":   admin.T(ctx, "Draft") + " " + locale,
				"grace": "Draft",
			}
		},
	})

	_, body := b.get("/admin/people")
	if !strings.Contains(body, ">Ada Lovelace</a>") || strings.Contains(body, ">Analyst</a>") {
		t.Fatal("the overview does not use ItemLabel")
	}

	if !strings.Contains(body, `<span class="muted"> · Concept en</span>`) {
		t.Fatal("the note translated with the request context is missing")
	}

	if !strings.Contains(body, `<span class="muted"> · Draft</span>`) {
		t.Fatal("a note is translated again by the editor")
	}

	mu.Lock()
	seen := slices.Clone(calls)
	mu.Unlock()

	if len(seen) != 1 || seen[0] != 2 {
		t.Fatalf("Notes calls = %v, want one call with both items", seen)
	}

	_, body = b.get("/admin/people/ada")
	if !strings.Contains(body, "<h1 class=\"admin-page-title\">Ada Lovelace</h1>") {
		t.Fatal("the editor title does not use ItemLabel")
	}
}

func TestLabelFromText(t *testing.T) {
	t.Parallel()

	b := run(t, editor.Config[person, text, personInput]{
		Store: newStore(ada()),
		Label: func(value text) string {
			return value.Role
		},
	})

	if _, body := b.get("/admin/people"); !strings.Contains(body, ">Analyst</a>") {
		t.Fatal("Label is not used without ItemLabel")
	}
}

var errLinked = errors.New("linked")

func TestDeleteRefused(t *testing.T) {
	t.Parallel()

	people := newStore(ada())
	b := run(t, editor.Config[person, text, personInput]{
		Store: people,
		Label: func(value text) string {
			return value.Role
		},
		Issue: func(err error) string {
			if errors.Is(err, errLinked) {
				return "Remove the links first."
			}

			return ""
		},
	})

	cases := []struct {
		err     error
		status  int
		message string
	}{
		{
			dbtest.ForeignKeyError(t),
			http.StatusUnprocessableEntity,
			"Dit item is nog in gebruik.",
		},
		{errLinked, http.StatusUnprocessableEntity, "Remove the links first."},
		{errors.New("disk full"), http.StatusInternalServerError, ""},
	}

	for _, test := range cases {
		people.deleteErr = test.err

		status, body := b.post("/admin/people/ada", "/admin/people/ada/delete", url.Values{})
		if status != test.status {
			t.Fatalf("%v: status %d", test.err, status)
		}

		// The reason shows as an error toast, not inside the page.
		if test.message != "" &&
			!regexp.MustCompile(`(?s)toast-error".*?<p class="toast-text">`+regexp.QuoteMeta(test.message)+`</p>`).
				MatchString(body) {
			t.Fatalf("%v: toast %q missing", test.err, test.message)
		}

		if strings.Contains(body, `class="notice mb-6"`) {
			t.Fatalf("%v: the reason is still shown inside the page", test.err)
		}
	}

	people.deleteErr = nil

	status, body := b.post("/admin/people/ada", "/admin/people/ada/delete", url.Values{})
	if status != http.StatusOK || !strings.Contains(body, "Naar de prullenbak verplaatst.") {
		t.Fatalf("delete: %d", status)
	}
}

func TestPublishCancelsSchedule(t *testing.T) {
	t.Parallel()

	planned := ada()
	planned.Schedule = content.Schedule{RevisionID: "r1", PublishAt: time.Now().Add(24 * time.Hour)}

	people := newStore(planned)
	b := run(t, editor.Config[person, text, personInput]{
		Store: people,
		Label: func(value text) string {
			return value.Role
		},
	})

	_, body := b.get("/admin/people/ada")
	if !strings.Contains(body, `hx-confirm="Nu publiceren annuleert de planning. Doorgaan?"`) {
		t.Fatal("publishing a scheduled item does not ask to cancel the schedule")
	}

	if !strings.Contains(body, "Met de hand publiceren of de publicatie intrekken annuleert de planning.") {
		t.Fatal("the schedule does not say that publishing by hand cancels it")
	}

	status, body := b.post("/admin/people/ada", "/admin/people/ada/revisions/r1/publish", url.Values{})
	if status != http.StatusOK || !strings.Contains(body, "Gepubliceerd. De planning is geannuleerd.") {
		t.Fatalf("publish notice: %d", status)
	}

	if strings.Contains(body, "annuleert de planning. Doorgaan?") {
		t.Fatal("the cancelled schedule still asks for confirmation")
	}

	// Without a schedule the usual notice is shown.
	_, body = b.post("/admin/people/ada", "/admin/people/ada/unpublish", url.Values{})
	if !strings.Contains(body, `<p class="toast-text">Publicatie ingetrokken.</p>`) {
		t.Fatal("unpublish notice")
	}
}

// unrevisioned is the store as content without revisions.
type unrevisioned struct {
	*store
}

func (unrevisioned) Revisioned() bool {
	return false
}

// TestSaveNotices checks what saving says: content with revisions saves a
// draft, with a "Save draft" button and a toast that says the website shows
// it once it is published; content without revisions keeps "Save" and
// "Changes saved.". Unpublishing asks first in both.
func TestSaveNotices(t *testing.T) {
	t.Parallel()

	published := ada()
	published.Published = true
	published.PublishedID = "r1"
	published.PublishedNumber = 1

	cases := []struct {
		name   string
		store  content.Store[person, text]
		button string
		toast  string
	}{
		{
			"revisions",
			newStore(published),
			">Concept opslaan</button>",
			"Opgeslagen als concept. Publiceer het om het op de website te tonen.",
		},
		{"no revisions", unrevisioned{newStore(published)}, ">Opslaan</button>", "Wijzigingen opgeslagen."},
	}

	for _, test := range cases {
		b := run(t, editor.Config[person, text, personInput]{Store: test.store})

		_, body := b.get("/admin/people/ada")
		if !strings.Contains(body, test.button) {
			t.Fatalf("%s: the form has no %s", test.name, test.button)
		}

		unpublish := regexp.MustCompile(
			`(?s)<form action="/admin/people/ada/unpublish[^"]*"[^>]*hx-confirm="Dit van de website halen\?"`,
		)
		if !unpublish.MatchString(body) {
			t.Fatalf("%s: unpublishing does not ask first:\n%s", test.name, body)
		}

		if !strings.Contains(body, "Terug naar People") {
			t.Fatalf("%s: the back link does not name the overview", test.name)
		}

		status, body := b.post("/admin/people/ada", "/admin/people/ada", url.Values{"Role": {"Mathematician"}})

		match := toastText.FindStringSubmatch(body)
		if status != http.StatusOK || match == nil || match[1] != test.toast {
			t.Fatalf("%s: save toast %v, want %q", test.name, match, test.toast)
		}
	}
}

// toastText is the text of a success toast.
var toastText = regexp.MustCompile(
	`<div class="toast toast-success"[^>]*>\s*<svg[^>]*>.*?</svg>\s*<p class="toast-text">([^<]*)</p>`,
)

// TestEmptyOverview checks that an empty overview offers the section's
// "new item" action, which a search without results does not.
func TestEmptyOverview(t *testing.T) {
	t.Parallel()

	b := run(t, editor.Config[person, text, personInput]{Store: newStore()})

	_, body := b.get("/admin/people")
	if !strings.Contains(body, `Hier staat nog niets. <a href="/admin/people/new">New person</a>`) {
		t.Fatalf("the empty overview does not offer the new item:\n%s", body)
	}

	if _, body = b.get("/admin/people?q=nobody"); strings.Contains(body, `<a href="/admin/people/new">New person</a></td>`) {
		t.Fatal("a search without results offers the new item")
	}
}
