package newsletter

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/media"
)

// recorder is a Provider that remembers its calls as "subscribe alice@…".
type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) Subscribe(_ context.Context, email, _, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, "subscribe "+email)

	return nil
}

func (r *recorder) Unsubscribe(_ context.Context, email, _, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, "unsubscribe "+email)

	return nil
}

func (r *recorder) expect(t *testing.T, calls ...string) {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	if strings.Join(r.calls, "|") != strings.Join(calls, "|") {
		t.Fatalf("provider calls: want %q, got %q", calls, r.calls)
	}
}

// TestStore runs the subscriber, mailing and text storage on every configured dialect.
func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "module:newsletter", schema()); err != nil {
			t.Fatal(err)
		}

		texts, err := content.NewCollection(database, textSpec())
		if err != nil {
			t.Fatal(err)
		}

		languages, err := locales.New(locales.Config{Default: "en", Enabled: []string{"en", "nl"}})
		if err != nil {
			t.Fatal(err)
		}

		provider := &recorder{}
		service := &Service{key: "news", db: database, texts: texts, locales: languages, provider: provider}
		other := &Service{key: "other", db: database, texts: texts, locales: languages}

		testSubscribers(t, service, other, provider)
		testMailings(t, service)
		testTexts(t, service)

		imports := &recorder{}
		testImport(t, &Service{key: "imports", db: database, texts: texts, locales: languages, provider: imports}, imports)
		delivery := &Service{
			key:      "news",
			db:       database,
			texts:    texts,
			locales:  languages,
			provider: &recorder{},
			actions:  webthings.Env{Key: "news"}.Actions(),
		}
		delivery.registerActions()
		testDelivery(t, delivery)

		trashed := &recorder{}
		testTrash(t, &Service{key: "trash", db: database, texts: texts, locales: languages, provider: trashed}, trashed)
	})
}

func testSubscribers(t *testing.T, service, other *Service, provider *recorder) {
	t.Helper()

	ctx := context.Background()

	alice, send, err := service.register(ctx, "alice@example.test", "Alice", "en")
	if err != nil || !send || alice.Status != Pending || len(alice.Token) != 64 {
		t.Fatalf("register: %+v %v %v", alice, send, err)
	}

	// Signing up again while pending sends the same link again.
	again, send, err := service.register(ctx, "alice@example.test", "Alice A.", "en")
	if err != nil || !send || again.Token != alice.Token || again.ID != alice.ID {
		t.Fatalf("register again: %+v %v %v", again, send, err)
	}

	// Addresses are unique per instance only.
	if _, send, err := other.register(ctx, "alice@example.test", "", "en"); err != nil || !send {
		t.Fatalf("other instance: %v %v", send, err)
	}

	confirmed, err := service.Confirm(ctx, alice.Token)
	if err != nil || confirmed.Status != Confirmed || confirmed.ConfirmedAt == nil {
		t.Fatalf("confirm: %+v %v", confirmed, err)
	}

	if _, err := service.Confirm(ctx, alice.Token); err != nil {
		t.Fatalf("confirm twice: %v", err)
	}

	if _, send, _ := service.register(ctx, "alice@example.test", "", "en"); send {
		t.Fatal("confirmed subscriber was sent a confirmation again")
	}

	bob, _, err := service.register(ctx, "bob@example.test", "", "nl")
	if err != nil {
		t.Fatal(err)
	}

	counts, err := service.Counts(ctx)
	if err != nil || counts != (Counts{Pending: 1, Confirmed: 1}) || counts.Total() != 2 {
		t.Fatalf("counts: %+v %v", counts, err)
	}

	list, more, err := service.Subscribers(ctx, "", 0, 1)
	if err != nil || len(list) != 1 || !more || list[0].Email != "bob@example.test" {
		t.Fatalf("first page: %+v %v %v", list, more, err)
	}

	list, _, err = service.Subscribers(ctx, Confirmed, 0, 10)
	if err != nil || len(list) != 1 || list[0].Name != "Alice A." || list[0].ConfirmedAt == nil {
		t.Fatalf("confirmed: %+v %v", list, err)
	}

	expectRecipients(t, service, "", "alice@example.test")
	expectRecipients(t, service, "nl")

	left, err := service.Unsubscribe(ctx, alice.Token)
	if err != nil || left.Status != Unsubscribed {
		t.Fatalf("unsubscribe: %+v %v", left, err)
	}

	if _, err := service.Unsubscribe(ctx, alice.Token); err != nil {
		t.Fatalf("unsubscribe twice: %v", err)
	}

	// An old confirmation link does not bring back someone who left.
	if _, err := service.Confirm(ctx, alice.Token); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("confirm after unsubscribe: %v", err)
	}

	back, send, err := service.register(ctx, "alice@example.test", "", "nl")
	if err != nil || !send || back.Token == alice.Token || back.Status != Pending {
		t.Fatalf("register after unsubscribe: %+v %v %v", back, send, err)
	}

	for _, token := range []string{"", "short", strings.Repeat("0", 64)} {
		if _, err := service.Confirm(ctx, token); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("confirm %q: %v", token, err)
		}
	}

	if _, err := service.Confirm(ctx, bob.Token); err != nil {
		t.Fatal(err)
	}

	expectRecipients(t, service, "nl", "bob@example.test")

	if err := service.DeleteSubscriber(ctx, bob.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Subscriber(ctx, bob.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("deleted subscriber: %v", err)
	}

	if err := service.DeleteSubscriber(ctx, "not-an-id"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("delete unknown subscriber: %v", err)
	}

	provider.expect(t,
		"subscribe alice@example.test",
		"unsubscribe alice@example.test",
		"subscribe bob@example.test",
		"unsubscribe bob@example.test",
	)
}

func testMailings(t *testing.T, service *Service) {
	t.Helper()

	ctx := context.Background()

	draft, err := service.SaveMailing(ctx, Mailing{Subject: "Hello", Body: "First *draft*"})
	if err != nil {
		t.Fatal(err)
	}

	draft.Locale = "nl"
	draft.Body = "Second draft"

	draft, err = service.SaveMailing(ctx, draft)
	if err != nil || draft.Body != "Second draft" || draft.Locale != "nl" || draft.SentAt != nil {
		t.Fatalf("save draft: %+v %v", draft, err)
	}

	sent, err := service.startSending(ctx, draft.ID, "https://example.test/")
	if err != nil || sent.SentAt == nil || !sent.Sending() {
		t.Fatalf("start sending: %+v %v", sent, err)
	}

	if _, err := service.startSending(ctx, draft.ID, ""); !errors.Is(err, errAlreadySent) {
		t.Fatalf("send twice: %v", err)
	}

	if _, err := service.startSending(ctx, db.NewID(), ""); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("send unknown mailing: %v", err)
	}

	if _, err := service.SaveMailing(ctx, sent); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("change sent mailing: %v", err)
	}

	missing := Mailing{Subject: "Photo", Body: "![Gone](media:" + db.NewID() + ")"}
	if _, err := service.SaveMailing(ctx, missing); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("mailing with missing media: %v", err)
	}

	list, err := service.Mailings(ctx)
	if err != nil || len(list) != 1 || list[0].Subject != "Hello" || !list[0].Sending() {
		t.Fatalf("mailings: %+v %v", list, err)
	}

	if err := service.DeleteMailing(ctx, draft.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Mailing(ctx, draft.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("deleted mailing: %v", err)
	}

	// The sender skips a mailing deleted while it was sent, also once it is
	// restored: deleting stopped it.
	if err := service.deliverAll(ctx); err != nil {
		t.Fatal(err)
	}

	if err := service.UndeleteMailing(ctx, draft.ID); err != nil {
		t.Fatal(err)
	}

	stopped, err := service.Mailing(ctx, draft.ID)
	if err != nil || stopped.Sending() || stopped.FinishedAt == nil || stopped.SentAt == nil {
		t.Fatalf("restored mailing that was being sent: %+v %v", stopped, err)
	}
}

// failing is a mail.Sender that refuses one address.
type failing struct {
	mail.Memory

	refuse string
}

func (f *failing) Send(ctx context.Context, message mail.Message) error {
	if message.To[0] == f.refuse {
		return errors.New("refused")
	}

	return f.Memory.Send(ctx, message)
}

// testDelivery sends a mailing in the background: interrupted, resumed and
// with a message the mail server refuses.
func testDelivery(t *testing.T, service *Service) {
	t.Helper()

	ctx := context.Background()
	outbox := &failing{refuse: "carol@example.test"}
	service.mail = outbox

	var ids []string

	for _, email := range []string{"alice@example.test", "bob@example.test", "carol@example.test", "dave@example.test"} {
		subscriber, err := service.Add(ctx, Contact{Email: email})
		if err != nil {
			t.Fatal(err)
		}

		ids = append(ids, subscriber.ID)
	}

	slices.Sort(ids)

	draft, err := service.SaveMailing(ctx, Mailing{Subject: "News", Body: "Hello *all*"})
	if err != nil {
		t.Fatal(err)
	}

	started, err := service.startSending(ctx, draft.ID, "https://example.test/")
	if err != nil || started.Total != 4 {
		t.Fatalf("start: %+v %v", started, err)
	}

	// A restart after the first subscriber had their turn continues with the
	// second; the first does not get the mailing twice.
	_, err = service.db.Exec(ctx, `UPDATE wt_newsletter_mailing SET progress=?, recipients=1 WHERE id=?`, ids[0], draft.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.deliverAll(ctx); err != nil {
		t.Fatal(err)
	}

	done, err := service.Mailing(ctx, draft.ID)
	if err != nil || done.Sending() || done.FinishedAt == nil || done.Recipients != 3 || done.Failed != 1 || done.Total != 4 {
		t.Fatalf("finished: %+v %v", done, err)
	}

	messages := outbox.Sent()
	if len(messages) != 2 {
		t.Fatalf("messages: %+v", messages)
	}

	for _, message := range messages {
		if message.To[0] == "carol@example.test" || message.Subject != "News" ||
			!strings.Contains(message.Text, "https://example.test/_/news/unsubscribe?token=") ||
			!strings.Contains(message.HTML, "<em>all</em>") {
			t.Fatalf("message: %+v", message)
		}
	}

	// The background sender sends what it is woken for, and stops on close.
	service.start()

	second, err := service.SaveMailing(ctx, Mailing{Subject: "More news", Body: "Again"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.startSending(ctx, second.ID, "https://example.test"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)

	for {
		mailing, err := service.Mailing(ctx, second.ID)
		if err != nil {
			t.Fatal(err)
		}

		if !mailing.Sending() {
			if mailing.Recipients != 3 || mailing.Failed != 1 {
				t.Fatalf("background mailing: %+v", mailing)
			}

			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("background mailing did not finish: %+v", mailing)
		}

		time.Sleep(10 * time.Millisecond)
	}

	if err := service.close(ctx); err != nil {
		t.Fatal(err)
	}
}

// testImport adds existing subscribers without a confirmation.
func testImport(t *testing.T, service *Service, provider *recorder) {
	t.Helper()

	ctx := context.Background()

	pending, _, err := service.register(ctx, "pending@example.test", "Pat", "nl")
	if err != nil {
		t.Fatal(err)
	}

	left, _, err := service.register(ctx, "left@example.test", "", "en")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Unsubscribe(ctx, left.Token); err != nil {
		t.Fatal(err)
	}

	since := time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)

	added, err := service.Add(
		ctx,
		Contact{Email: " New@Example.test ", Name: "  New   Person ", Locale: "nl", ConfirmedAt: since},
	)
	if err != nil || added.Status != Confirmed || added.Email != "new@example.test" || added.Name != "New Person" ||
		added.Locale != "nl" || !added.ConfirmedAt.Equal(since) {
		t.Fatalf("add: %+v %v", added, err)
	}

	if _, err := service.Add(ctx, Contact{Email: "left@example.test"}); !errors.Is(err, ErrUnsubscribed) {
		t.Fatalf("add someone who left: %v", err)
	}

	// The unsubscribe holds while the record is in the trash.
	if err := service.DeleteSubscriber(ctx, left.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Add(ctx, Contact{Email: "left@example.test"}); !errors.Is(err, ErrUnsubscribed) {
		t.Fatalf("add someone who left, in the trash: %v", err)
	}

	if err := service.UndeleteSubscriber(ctx, left.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Add(ctx, Contact{Email: "not an address"}); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("add an invalid address: %v", err)
	}

	result, err := service.Import(ctx, []Contact{
		{Email: "new@example.test"},
		{Email: "pending@example.test"},
		{Email: "left@example.test"},
		{Email: "fresh@example.test", Locale: "fr"},
		{Email: "@nowhere"},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := ImportResult{Added: 1, Confirmed: 1, Existing: 1, Unsubscribed: 1, Invalid: []string{"@nowhere"}}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("import: %+v", result)
	}

	confirmed, err := service.Subscriber(ctx, pending.ID)
	if err != nil || confirmed.Status != Confirmed || confirmed.Name != "Pat" || confirmed.Locale != "nl" {
		t.Fatalf("pending subscriber after import: %+v %v", confirmed, err)
	}

	// An unknown language becomes the site's default.
	expectRecipients(t, service, "en", "fresh@example.test")

	provider.expect(t,
		"subscribe new@example.test",
		"subscribe pending@example.test",
		"subscribe fresh@example.test",
	)
}

func TestReadContacts(t *testing.T) {
	source := "\ufeffEmail;Name;Status;Locale\n" +
		"a@example.test;Ann;confirmed;\n" +
		"b@example.test;Bob;unsubscribed;nl\n" +
		";;;\n" +
		"'c@example.test;Cas;;nl\n"

	contacts, skipped, err := readContacts(strings.NewReader(source), "en")
	if err != nil {
		t.Fatal(err)
	}

	want := []Contact{
		{Email: "a@example.test", Name: "Ann", Locale: "en"},
		{Email: "c@example.test", Name: "Cas", Locale: "nl"},
	}

	if !reflect.DeepEqual(contacts, want) || skipped != 1 {
		t.Fatalf("contacts: %+v, skipped %d", contacts, skipped)
	}

	if _, _, err := readContacts(strings.NewReader("name\nAnn\n"), "en"); !errors.Is(err, errHeader) {
		t.Fatalf("no email column: %v", err)
	}

	if _, _, err := readContacts(strings.NewReader(""), "en"); !errors.Is(err, errHeader) {
		t.Fatalf("empty file: %v", err)
	}
}

func testTexts(t *testing.T, service *Service) {
	t.Helper()

	ctx := context.Background()

	if text := service.Text(ctx, "nl"); text != defaultText {
		t.Fatalf("default text: %+v", text)
	}

	english := Text{Title: "News", Intro: "Monthly.", Footer: "Bye:"}

	item, err := service.Texts().Save(ctx, content.Draft[struct{}, Text]{
		Translations: []content.Translation[Text]{{Locale: "en", Text: english}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Unpublished texts are not used yet.
	if text := service.Text(ctx, "en"); text != defaultText {
		t.Fatalf("unpublished text: %+v", text)
	}

	if err := service.Texts().Publish(ctx, item.ID, "", "en"); err != nil {
		t.Fatal(err)
	}

	if text := service.Text(ctx, "nl"); text != english {
		t.Fatalf("fallback to the default language: %+v", text)
	}

	_, err = service.Texts().Save(ctx, content.Draft[struct{}, Text]{
		ID:           item.ID,
		Translations: []content.Translation[Text]{{Locale: "en", Text: Text{Title: strings.Repeat("a", 201)}}},
	})
	if !errors.Is(err, content.ErrTooLong) {
		t.Fatalf("long title: %v", err)
	}
}

func expectRecipients(t *testing.T, service *Service, locale string, emails ...string) {
	t.Helper()

	list, err := service.Recipients(context.Background(), locale)
	if err != nil {
		t.Fatal(err)
	}

	var got []string

	for _, subscriber := range list {
		got = append(got, subscriber.Email)
	}

	if strings.Join(got, "|") != strings.Join(emails, "|") {
		t.Fatalf("recipients %q: want %q, got %q", locale, emails, got)
	}
}

// testTrash deletes subscribers and mailings to the trash and restores and
// purges them through the section's trash.
func testTrash(t *testing.T, service *Service, provider *recorder) {
	t.Helper()

	visitor := context.Background()
	editor := db.NewID()
	ctx := db.WithActor(visitor, editor)

	// Visitors sign up without an actor; an editor's import records the editor.
	alice, _, err := service.register(visitor, "alice@example.test", "Alice", "en")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Confirm(visitor, alice.Token); err != nil {
		t.Fatal(err)
	}

	bob, err := service.Add(ctx, Contact{Email: "bob@example.test", Name: "Bob"})
	if err != nil {
		t.Fatal(err)
	}

	expectAudit(t, service, "wt_newsletter_subscriber", alice.ID, "", "")
	expectAudit(t, service, "wt_newsletter_subscriber", bob.ID, editor, editor)

	draft, err := service.SaveMailing(ctx, Mailing{Subject: "Spring news", Body: "Hello"})
	if err != nil {
		t.Fatal(err)
	}

	expectAudit(t, service, "wt_newsletter_mailing", draft.ID, editor, editor)

	if err := service.DeleteSubscriber(ctx, alice.ID); err != nil {
		t.Fatal(err)
	}

	if err := service.DeleteSubscriber(ctx, alice.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}

	if err := service.DeleteMailing(ctx, draft.ID); err != nil {
		t.Fatal(err)
	}

	// A deleted subscriber is off every list, and the links stop working.
	counts, err := service.Counts(ctx)
	if err != nil || counts != (Counts{Confirmed: 1}) {
		t.Fatalf("counts after delete: %+v %v", counts, err)
	}

	list, _, err := service.Subscribers(ctx, "", 0, 0)
	if err != nil || len(list) != 1 || list[0].ID != bob.ID {
		t.Fatalf("subscribers after delete: %+v %v", list, err)
	}

	expectRecipients(t, service, "", "bob@example.test")

	if audience, err := service.audience(ctx, ""); err != nil || audience != 1 {
		t.Fatalf("audience after delete: %d %v", audience, err)
	}

	if _, err := service.Confirm(visitor, alice.Token); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("confirm deleted: %v", err)
	}

	if _, err := service.Unsubscribe(visitor, alice.Token); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("unsubscribe deleted: %v", err)
	}

	if mailings, err := service.Mailings(ctx); err != nil || len(mailings) != 0 {
		t.Fatalf("mailings after delete: %+v %v", mailings, err)
	}

	// The trash lists both kinds, most recently deleted first.
	bin := service.trash()

	items, total, err := bin.List(ctx, "", 10, 0)
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("trash: %+v %d %v", items, total, err)
	}

	if items[0].Title != "Spring news" || items[0].Kind != "Mailing" || items[0].DeletedBy != editor ||
		items[1].Title != "Alice <alice@example.test>" || items[1].Kind != "Subscriber" || items[1].DeletedAt.IsZero() {
		t.Fatalf("trash items: %+v", items)
	}

	found, total, err := bin.List(ctx, "alice", 10, 0)
	if err != nil || total != 1 || len(found) != 1 || found[0].ID != items[1].ID {
		t.Fatalf("trash search: %+v %d %v", found, total, err)
	}

	paged, total, err := bin.List(ctx, "", 1, 1)
	if err != nil || total != 2 || len(paged) != 1 || paged[0].ID != items[1].ID {
		t.Fatalf("trash page: %+v %d %v", paged, total, err)
	}

	// The address signs up again as a new subscriber, so the deleted one
	// cannot come back.
	again, send, err := service.register(visitor, "alice@example.test", "", "en")
	if err != nil || !send || again.ID == alice.ID || again.Status != Pending {
		t.Fatalf("register after delete: %+v %v %v", again, send, err)
	}

	var failure *admin.TrashError

	err = bin.Restore(ctx, []string{items[1].ID})
	if !errors.As(err, &failure) || failure.ID != items[1].ID ||
		failure.Reason != "This subscriber cannot be restored: another subscriber uses the e-mail address now." {
		t.Fatalf("restore taken address: %v", err)
	}

	// Once the new signup is gone too, the old subscriber comes back confirmed.
	if err := service.DeleteSubscriber(ctx, again.ID); err != nil {
		t.Fatal(err)
	}

	if err := bin.Restore(ctx, []string{items[0].ID, items[1].ID}); err != nil {
		t.Fatal(err)
	}

	restored, err := service.Subscriber(ctx, alice.ID)
	if err != nil || restored.Status != Confirmed {
		t.Fatalf("restored subscriber: %+v %v", restored, err)
	}

	expectAudit(t, service, "wt_newsletter_subscriber", alice.ID, "", editor)
	expectRecipients(t, service, "", "alice@example.test", "bob@example.test")

	if mailing, err := service.Mailing(ctx, draft.ID); err != nil || mailing.SentAt != nil {
		t.Fatalf("restored mailing: %+v %v", mailing, err)
	}

	err = bin.Restore(ctx, []string{items[0].ID})
	if !errors.As(err, &failure) || failure.Reason != "This item is no longer in the trash." {
		t.Fatalf("restore twice: %v", err)
	}

	// Purging removes only what is in the trash.
	if err := service.DeleteMailing(ctx, draft.ID); err != nil {
		t.Fatal(err)
	}

	items, _, err = bin.List(ctx, "", 10, 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("trash before purge: %+v %v", items, err)
	}

	if err := bin.Purge(ctx, []string{items[0].ID, items[1].ID}); err != nil {
		t.Fatal(err)
	}

	var rows int

	err = service.db.Get(ctx, &rows, `SELECT COUNT(*) FROM wt_newsletter_subscriber WHERE instance=?`, service.key)
	if err != nil || rows != 2 {
		t.Fatalf("subscribers after purge: %d %v", rows, err)
	}

	err = service.db.Get(ctx, &rows, `SELECT COUNT(*) FROM wt_newsletter_mailing WHERE instance=?`, service.key)
	if err != nil || rows != 0 {
		t.Fatalf("mailings after purge: %d %v", rows, err)
	}

	if items, total, err := bin.List(ctx, "", 10, 0); err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("trash after purge: %+v %d %v", items, total, err)
	}

	provider.expect(t,
		"subscribe alice@example.test",
		"subscribe bob@example.test",
		"unsubscribe alice@example.test",
		"subscribe alice@example.test",
	)
}

// expectAudit checks who created and last changed a row.
func expectAudit(t *testing.T, service *Service, table, id, createdBy, updatedBy string) {
	t.Helper()

	var row struct {
		CreatedBy db.Ref `db:"created_by"`
		UpdatedBy db.Ref `db:"updated_by"`
	}

	err := service.db.Get(context.Background(), &row, `SELECT created_by, updated_by FROM `+table+` WHERE id=?`, id)
	if err != nil {
		t.Fatal(err)
	}

	if row.CreatedBy.String() != createdBy || row.UpdatedBy.String() != updatedBy {
		t.Fatalf("%s audit: created by %q, updated by %q", table, row.CreatedBy, row.UpdatedBy)
	}
}
