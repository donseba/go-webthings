package editor_test

import (
	"context"
	"embed"
	"html"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/media"
)

//go:embed testdata/schema
var testdata embed.FS

// noteText is the text of a note or a card in one language.
type noteText struct {
	Body string `db:"body"`
}

type noteInput struct {
	Body string `form:"textarea" label:"Text"`
}

// noteStore is the store of notes or cards, which have no fields.
type noteStore = content.Store[struct{}, noteText]

// shared is an item two editors change: the section and the store that
// keep it, and the address of its editor.
type shared struct {
	name    string
	section string
	store   noteStore
	id      string
	editor  string
}

// text is the item's English text as the store has it.
func (s shared) text(t *testing.T) string {
	t.Helper()

	item, err := s.store.Load(context.Background(), s.id, "")
	if err != nil {
		t.Fatal(err)
	}

	value, _ := item.Text("en")

	return value.Body
}

// conflictSite is a Dutch admin with an editor of notes, which keep
// revisions, and one of cards, a collection, with an item each; owner and
// ada are the browsers of two signed-in users.
type conflictSite struct {
	owner browser
	ada   browser
	items []shared
}

// startConflicts serves the admin of conflictSite on database.
func startConflicts(t *testing.T, database *db.DB) conflictSite {
	t.Helper()
	ctx := context.Background()

	schema, err := fs.Sub(testdata, "testdata/schema")
	if err != nil {
		t.Fatal(err)
	}

	if err := database.ApplySchema(ctx, "auth", auth.Schema()); err != nil {
		t.Fatal(err)
	}

	if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
		t.Fatal(err)
	}

	if err := database.ApplySchema(ctx, "editor_test", schema); err != nil {
		t.Fatal(err)
	}

	accounts, err := auth.New(database)
	if err != nil {
		t.Fatal(err)
	}

	if err := accounts.Bootstrap(ctx, "owner@example.test", "test-only long passphrase"); err != nil {
		t.Fatal(err)
	}

	_, err = accounts.CreateUser(ctx, "ada@example.test", "Ada Lovelace", "ada long passphrase", auth.Admin)
	if err != nil {
		t.Fatal(err)
	}

	languages, err := locales.New(locales.Config{Default: "en", Enabled: []string{"en"}})
	if err != nil {
		t.Fatal(err)
	}

	notes, err := content.NewRevisions(database, noteSpec("t_note"))
	if err != nil {
		t.Fatal(err)
	}

	cards, err := content.NewCollection(database, noteSpec("t_card"))
	if err != nil {
		t.Fatal(err)
	}

	items := []shared{
		{name: "revisions", section: "notes", store: notes.In("notes")},
		{name: "collection", section: "cards", store: cards.In("cards")},
	}

	var sections []admin.Section

	for i, item := range items {
		sections = append(sections, editor.New(noteConfig(item.section, item.store, languages)))

		saved, err := item.store.Save(ctx, content.Draft[struct{}, noteText]{
			Translations: []content.Translation[noteText]{{Locale: "en", Text: noteText{Body: "First text"}}},
		})
		if err != nil {
			t.Fatal(err)
		}

		items[i].id = saved.ID
		items[i].editor = "/admin/" + item.section + "/" + saved.ID
	}

	backoffice, err := admin.New(accounts, admin.Options{Language: "nl", CatalogueDir: t.TempDir()}, sections...)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/admin/", backoffice)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return conflictSite{
		owner: signIn(t, server, "owner@example.test", "test-only long passphrase"),
		ada:   signIn(t, server, "ada@example.test", "ada long passphrase"),
		items: items,
	}
}

func noteSpec(table string) content.Spec[struct{}, noteText] {
	return content.Spec[struct{}, noteText]{
		Table: table,
		Complete: func(text noteText) bool {
			return text.Body != ""
		},
	}
}

func noteConfig(id string, store noteStore, languages locales.Provider) editor.Config[struct{}, noteText, noteInput] {
	return editor.Config[struct{}, noteText, noteInput]{
		ID:      id,
		Title:   id,
		Store:   store,
		Locales: languages,
		Label: func(text noteText) string {
			return text.Body
		},
		Form: func(_ context.Context, item content.Item[struct{}, noteText], locale string) (noteInput, error) {
			text, _ := item.Text(locale)

			// The form has the fields of the text.
			return noteInput(text), nil
		},
		Apply: func(_ context.Context, in noteInput, draft *content.Draft[struct{}, noteText], locale string) error {
			draft.Translations = content.SetText(draft.Translations, locale, noteText(in))

			return nil
		},
	}
}

// remembered is the version of the item an edit form remembers.
var remembered = regexp.MustCompile(`<input type="hidden" name="_version" value="([^"]*)">`)

// saveAnyway is the value of the "Save anyway" button of the notice, which
// submits the edit form.
var saveAnyway = regexp.MustCompile(
	`<button class="button" type="submit" form="edit-form" name="_overwrite" value="([^"]*)">Toch opslaan</button>`,
)

// editForm is the opening tag of the edit form the button submits.
var editForm = regexp.MustCompile(`<form action="/admin/[a-z]+/[^"]*"[^>]*\sid="edit-form"`)

// match returns the first group of pattern in page, failing the test when
// it is missing.
func match(t *testing.T, page string, pattern *regexp.Regexp) string {
	t.Helper()

	found := pattern.FindStringSubmatch(page)
	if found == nil {
		t.Fatalf("no %s in:\n%s", pattern, page)
	}

	return html.UnescapeString(found[1])
}

// savedBy matches the notice of a refused save, in Dutch: who saved the
// item, when name is set, and at what time.
func savedBy(name string) *regexp.Regexp {
	if name == "" {
		return regexp.MustCompile(`<p>Dit item is om \d{2}:\d{2} opgeslagen terwijl je het aan het bewerken was\. ` +
			`Open de opgeslagen versie in een nieuw tabblad om te vergelijken, of sla de jouwe toch op\.</p>`)
	}

	return regexp.MustCompile(`<p>` + regexp.QuoteMeta(name) + ` heeft dit item om \d{2}:\d{2} opgeslagen terwijl je ` +
		`het aan het bewerken was\. Open die versie in een nieuw tabblad om te vergelijken, of sla de jouwe toch op\.</p>`)
}

// save opens the editor at path and saves body with the form, as an editor
// does in a tab of their own, and returns the page it ends on.
func (b browser) save(path, body string) string {
	b.t.Helper()

	_, page := b.get(path)

	status, saved := b.post(path, path, url.Values{"Body": {body}, "_version": {match(b.t, page, remembered)}})
	if status != http.StatusOK {
		b.t.Fatalf("save %s: %d\n%s", path, status, saved)
	}

	return saved
}

// TestSaveOverNewerSave checks that a save is refused when someone saved
// the item after its form was opened: the form shows again as the editor
// typed it, with a notice of who saved and when, a link to that version and
// "Save anyway", which saves over that save but not over a later one. It
// checks content with revisions and a collection, on every database.
func TestSaveOverNewerSave(t *testing.T) {
	t.Parallel()

	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		site := startConflicts(t, database)

		for _, item := range site.items {
			// The owner opens the editor; Ada saves the item in between.
			_, opened := site.owner.get(item.editor)
			version := match(t, opened, remembered)

			if !editForm.MatchString(opened) {
				t.Fatalf("%s: the edit form has no id:\n%s", item.name, opened)
			}

			site.ada.save(item.editor, "Text of Ada")

			mine := url.Values{"Body": {"Text of the owner"}, "_version": {version}}

			status, refused := site.owner.post(item.editor, item.editor, mine)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("%s: the save over Ada's answers %d", item.name, status)
			}

			if !savedBy("Ada Lovelace").MatchString(refused) {
				t.Fatalf("%s: the refusal does not name Ada:\n%s", item.name, refused)
			}

			if !strings.Contains(refused, ">Text of the owner</textarea>") {
				t.Fatalf("%s: the refused form lost what the owner typed", item.name)
			}

			if item.text(t) != "Text of Ada" {
				t.Fatalf("%s: the refused save changed the item to %q", item.name, item.text(t))
			}

			// The form still builds on the version it was opened on, so
			// saving it again is refused again.
			if match(t, refused, remembered) != version {
				t.Fatalf("%s: the refused form forgot the version it was opened on", item.name)
			}

			if status, _ := site.owner.post(item.editor, item.editor, mine); status != http.StatusUnprocessableEntity {
				t.Fatalf("%s: saving the refused form again answers %d", item.name, status)
			}

			if !strings.Contains(refused, `target="_blank" rel="noopener">Opgeslagen versie openen</a>`) {
				t.Fatalf("%s: the notice does not open Ada's version:\n%s", item.name, refused)
			}

			// "Save anyway" saves over Ada's save, not over a later one.
			mine.Set("_overwrite", match(t, refused, saveAnyway))
			site.ada.save(item.editor, "Second text of Ada")

			status, refused = site.owner.post(item.editor, item.editor, mine)
			if status != http.StatusUnprocessableEntity || !savedBy("Ada Lovelace").MatchString(refused) {
				t.Fatalf("%s: saving anyway over a later save answers %d", item.name, status)
			}

			if item.text(t) != "Second text of Ada" {
				t.Fatalf("%s: saving anyway over a later save changed the item", item.name)
			}

			mine.Set("_overwrite", match(t, refused, saveAnyway))

			status, saved := site.owner.post(item.editor, item.editor, mine)
			if status != http.StatusOK || item.text(t) != "Text of the owner" {
				t.Fatalf("%s: save anyway: %d, text %q\n%s", item.name, status, item.text(t), saved)
			}

			// The saved form builds on the new version: the owner saves
			// twice more without a question.
			for _, body := range []string{"Second text of the owner", "Third text of the owner"} {
				again := url.Values{"Body": {body}, "_version": {match(t, saved, remembered)}}

				status, saved = site.owner.post(item.editor, item.editor, again)
				if status != http.StatusOK || item.text(t) != body {
					t.Fatalf("%s: saving again: %d, text %q\n%s", item.name, status, item.text(t), saved)
				}
			}
		}

		// Content with revisions keeps every save: the first text, Ada's
		// two and the owner's three.
		history, err := site.items[0].store.History(context.Background(), site.items[0].id)
		if err != nil || len(history) != 6 {
			t.Fatalf("history: %d revisions, %v", len(history), err)
		}

		// The editor of an older revision builds on the latest, as its
		// save does.
		_, older := site.owner.get(site.items[0].editor + "?revision=" + history[5].ID)
		if match(t, older, remembered) != history[0].ID {
			t.Fatal("the editor of an older revision does not remember the latest")
		}
	})
}

// TestSaveOverOwnSave checks that a save is refused also when the newer
// save is the editor's own in another tab, and that a save without an
// author, such as an import, is told without a name.
func TestSaveOverOwnSave(t *testing.T) {
	t.Parallel()

	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		site := startConflicts(t, database)

		for _, item := range site.items {
			_, opened := site.owner.get(item.editor)
			mine := url.Values{"Body": {"Text of the first tab"}, "_version": {match(t, opened, remembered)}}

			site.owner.save(item.editor, "Text of the second tab")

			status, refused := site.owner.post(item.editor, item.editor, mine)
			if status != http.StatusUnprocessableEntity || !savedBy("owner@example.test").MatchString(refused) {
				t.Fatalf("%s: a save over the second tab answers %d:\n%s", item.name, status, refused)
			}

			// An import saves the item after the owner opens it again.
			_, opened = site.owner.get(item.editor)
			mine.Set("_version", match(t, opened, remembered))

			_, err := item.store.Save(context.Background(), content.Draft[struct{}, noteText]{
				ID:           item.id,
				Translations: []content.Translation[noteText]{{Locale: "en", Text: noteText{Body: "Imported text"}}},
			})
			if err != nil {
				t.Fatal(err)
			}

			status, refused = site.owner.post(item.editor, item.editor, mine)
			if status != http.StatusUnprocessableEntity || !savedBy("").MatchString(refused) {
				t.Fatalf("%s: a save over an import answers %d:\n%s", item.name, status, refused)
			}
		}
	})
}
