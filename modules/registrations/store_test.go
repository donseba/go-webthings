package registrations

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/site"
)

var codePattern = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$`)

// open prepares the tables and returns a service for instance "signup".
func open(t *testing.T, database *db.DB) *Service {
	t.Helper()

	ctx := context.Background()

	if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
		t.Fatal(err)
	}

	if err := database.ApplySchema(ctx, "site", site.Schema()); err != nil {
		t.Fatal(err)
	}

	if err := database.ApplySchema(ctx, "module:registrations", schema()); err != nil {
		t.Fatal(err)
	}

	settings := site.New(database)

	if err := settings.Init(ctx, "Test", locales.Config{Default: "en", Enabled: []string{"en", "nl"}}); err != nil {
		t.Fatal(err)
	}

	return &Service{key: "signup", db: database, site: settings, currency: "EUR", actions: webthings.Env{Key: "signup"}.Actions()}
}

func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := open(t, database)

		other := *service
		other.key = "other"

		signup := Signup{SubjectID: "bbq", Name: "Ada  Lovelace", Email: "ada@example.com", Adults: 2, Locale: "en"}

		if _, err := service.Register(ctx, signup); !errors.Is(err, ErrClosed) {
			t.Fatalf("without settings: %v", err)
		}

		if unset, err := service.Availability(ctx, "bbq", "en"); err != nil || unset.Configured || !unset.Closed {
			t.Fatalf("without settings: %+v %v", unset, err)
		}

		err := service.SaveSettings(ctx, Settings{
			SubjectID:  "bbq",
			Title:      "Barbecue",
			Capacity:   5,
			AdultPrice: 1250,
			ChildPrice: 500,
			Texts: []SubjectText{
				{Locale: "en", Notes: "Bring a plate", PaymentLink: "https://pay.example/bbq"},
				{Locale: "nl", Notes: "Neem een bord mee"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := service.Register(ctx, signup); !errors.Is(err, ErrClosed) {
			t.Fatalf("not open: %v", err)
		}

		if closed, err := service.Availability(ctx, "bbq", "nl"); err != nil || !closed.Configured || !closed.Closed ||
			closed.Notes != "Neem een bord mee" || closed.Currency != "EUR" {
			t.Fatalf("closed: %+v %v", closed, err)
		}

		invalid := Settings{SubjectID: "bbq", Texts: []SubjectText{{Locale: "not a locale"}}}
		if err := service.SaveSettings(ctx, invalid); !errors.Is(err, ErrInvalid) {
			t.Fatalf("text in an invalid locale: %v", err)
		}

		deadline := time.Now().Add(time.Hour)

		err = service.SaveSettings(ctx, Settings{
			SubjectID:     "bbq",
			Title:         "Barbecue",
			Open:          true,
			Capacity:      5,
			ClosesAt:      &deadline,
			AdultPrice:    1250,
			ChildPrice:    500,
			AllowChildren: true,
			Texts:         []SubjectText{{Locale: "nl", Notes: "Neem een bord en beker mee"}},
		})
		if err != nil {
			t.Fatal(err)
		}

		// Texts of languages that are not saved again are kept.
		settings, err := service.Settings(ctx, "bbq")
		if err != nil || !settings.Open || settings.Capacity != 5 || settings.AdultPrice != 1250 ||
			settings.ClosesAt == nil || settings.ClosesAt.Sub(deadline).Abs() > time.Millisecond ||
			len(settings.Texts) != 2 || settings.Texts[0].PaymentLink != "https://pay.example/bbq" ||
			settings.Texts[1].Notes != "Neem een bord en beker mee" {
			t.Fatalf("settings: %+v %v", settings, err)
		}

		// Languages without texts read the site's default language.
		if text := service.text(ctx, settings, "de"); text.Notes != "Bring a plate" {
			t.Fatalf("fallback text: %+v", text)
		}

		if _, err := other.Settings(ctx, "bbq"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("settings of another instance: %v", err)
		}

		for _, invalid := range []Signup{
			{SubjectID: "bbq", Email: "ada@example.com", Adults: 1},
			{SubjectID: "bbq", Name: "Ada", Email: "not an address", Adults: 1},
			{SubjectID: "bbq", Name: "Ada", Email: "ada@example.com"},
			{SubjectID: "bbq", Name: "Ada", Email: "ada@example.com", Adults: -1, Children: 2},
			{SubjectID: "bbq", Name: "Ada", Email: "ada@example.com", Adults: 60},
			{SubjectID: "b b q", Name: "Ada", Email: "ada@example.com", Adults: 1},
		} {
			if _, err := service.Register(ctx, invalid); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid %+v: %v", invalid, err)
			}
		}

		signup.Children = 1

		first, err := service.Register(ctx, signup)
		if err != nil {
			t.Fatal(err)
		}

		if !codePattern.MatchString(first.Code) || first.Name != "Ada Lovelace" || first.Total != 3000 ||
			first.Payment != PaymentPending || first.Places() != 3 {
			t.Fatalf("registration: %+v", first)
		}

		if _, err := service.Register(ctx, signup); !errors.Is(err, ErrFull) {
			t.Fatalf("over capacity: %v", err)
		}

		availability, err := service.Availability(ctx, "bbq", "en")
		if err != nil || !availability.Open || availability.Taken != 3 || availability.Left != 2 ||
			availability.Prices.Adult != 1250 || availability.ClosesAt.IsZero() {
			t.Fatalf("availability: %+v %v", availability, err)
		}

		// Codes are found however visitors type them, and only in their instance.
		loose := first.Code[:4] + " " + first.Code[5:]

		found, err := service.ByCode(ctx, loose)
		if err != nil || found.ID != first.ID || found.Email != "ada@example.com" {
			t.Fatalf("by code: %+v %v", found, err)
		}

		if _, err := other.ByCode(ctx, first.Code); !errors.Is(err, ErrNotFound) {
			t.Fatalf("code of another instance: %v", err)
		}

		if err := service.Update(ctx, first.ID, PaymentPaid, "Paid cash"); err != nil {
			t.Fatal(err)
		}

		if err := service.Update(ctx, first.ID, "refunded", ""); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unknown payment status: %v", err)
		}

		second, err := service.Register(ctx, Signup{SubjectID: "bbq", Name: "Bob", Email: "bob@example.com", Adults: 2})
		if err != nil {
			t.Fatal(err)
		}

		summaries, err := service.Summaries(ctx)
		if err != nil || summaries["bbq"].Registrations != 2 || summaries["bbq"].Places != 5 {
			t.Fatalf("summaries: %+v %v", summaries, err)
		}

		full, _ := service.Availability(ctx, "bbq", "en")
		if full.Open || !full.Full || full.Left != 0 {
			t.Fatalf("full: %+v", full)
		}

		// Cancelling frees the places; unpaid registrations stop waiting for payment.
		if err := service.Cancel(ctx, second.ID); err != nil {
			t.Fatal(err)
		}

		if err := service.Cancel(ctx, second.ID); err != nil {
			t.Fatalf("cancel twice: %v", err)
		}

		cancelled, err := service.Registration(ctx, second.ID)
		if err != nil || !cancelled.Cancelled() || cancelled.Payment != PaymentCancelled {
			t.Fatalf("cancelled: %+v %v", cancelled, err)
		}

		if err := service.Cancel(ctx, first.ID); err != nil {
			t.Fatal(err)
		}

		paid, _ := service.Registration(ctx, first.ID)
		if paid.Payment != PaymentPaid || paid.AdminNotes != "Paid cash" {
			t.Fatalf("paid and cancelled: %+v", paid)
		}

		list, err := service.Registrations(ctx, "bbq")
		if err != nil || len(list) != 2 || list[0].ID != first.ID {
			t.Fatalf("registrations: %+v %v", list, err)
		}

		if err := service.Delete(ctx, second.ID); err != nil {
			t.Fatal(err)
		}

		if err := service.Delete(ctx, second.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("delete twice: %v", err)
		}

		if err := service.Cancel(ctx, second.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cancel deleted: %v", err)
		}

		// Children only where allowed, and the deadline closes registration.
		past := time.Now().Add(-time.Minute)

		err = service.SaveSettings(ctx, Settings{SubjectID: "talk", Title: "Talk", Open: true})
		if err != nil {
			t.Fatal(err)
		}

		free, err := service.Register(ctx, Signup{SubjectID: "talk", Name: "Cy", Email: "cy@example.com", Adults: 1})
		if err != nil || free.Total != 0 || free.Payment != PaymentNone {
			t.Fatalf("free: %+v %v", free, err)
		}

		_, err = service.Register(ctx, Signup{SubjectID: "talk", Name: "Cy", Email: "cy@example.com", Adults: 1, Children: 1})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("children not allowed: %v", err)
		}

		err = service.SaveSettings(ctx, Settings{SubjectID: "talk", Title: "Talk", Open: true, ClosesAt: &past})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := service.Register(ctx, Signup{SubjectID: "talk", Name: "Di", Email: "di@example.com"}); err == nil {
			t.Fatal("registered after the deadline")
		}

		_, err = service.Register(ctx, Signup{SubjectID: "talk", Name: "Di", Email: "di@example.com", Adults: 1})
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("after the deadline: %v", err)
		}

		all, err := service.AllSettings(ctx)
		if err != nil || len(all) != 2 || all[0].SubjectID != "bbq" {
			t.Fatalf("all settings: %+v %v", all, err)
		}
	})
}

// TestTrash moves registrations to the trash and back on every configured
// dialect: deleted registrations free their places and leave every list,
// and come back only while their places are free.
func TestTrash(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		service := open(t, database)
		bin := service.trash()

		// Editors change settings; visitors register without an actor.
		editor := db.NewID()
		ctx := db.WithActor(context.Background(), editor)
		visitor := context.Background()

		err := service.SaveSettings(ctx, Settings{SubjectID: "bbq", Title: "Barbecue", Open: true, Capacity: 3})
		if err != nil {
			t.Fatal(err)
		}

		var audit struct {
			CreatedBy db.Ref    `db:"created_by"`
			CreatedAt time.Time `db:"created_at"`
			UpdatedBy db.Ref    `db:"updated_by"`
		}

		err = database.Get(ctx, &audit, `
			SELECT created_by, created_at, updated_by FROM wt_registration_subject
			WHERE instance=? AND subject_id=?`,
			"signup", "bbq",
		)
		if err != nil || audit.CreatedBy.String() != editor || audit.UpdatedBy.String() != editor || audit.CreatedAt.IsZero() {
			t.Fatalf("subject audit columns: %+v %v", audit, err)
		}

		other := db.NewID()

		err = service.SaveSettings(
			db.WithActor(ctx, other),
			Settings{SubjectID: "bbq", Title: "Barbecue", Open: true, Capacity: 3},
		)
		if err != nil {
			t.Fatal(err)
		}

		settings, err := service.Settings(ctx, "bbq")
		if err != nil || settings.UpdatedBy.String() != other {
			t.Fatalf("settings changed by: %+v %v", settings, err)
		}

		register := func(name string, adults int) Registration {
			t.Helper()

			registration, err := service.Register(visitor, Signup{
				SubjectID: "bbq", Name: name, Email: strings.ToLower(name) + "@example.com", Adults: adults,
			})
			if err != nil {
				t.Fatal(err)
			}

			return registration
		}

		ada := register("Ada", 2)
		bob := register("Bob", 1)

		stored, err := service.Registration(ctx, bob.ID)
		if err != nil || stored.CreatedBy != "" || stored.UpdatedBy != "" || stored.UpdatedAt.IsZero() ||
			stored.DeletedAt != nil {
			t.Fatalf("registration audit columns: %+v %v", stored, err)
		}

		if err := service.Update(ctx, bob.ID, PaymentNone, "Friend of Ada"); err != nil {
			t.Fatal(err)
		}

		if stored, _ = service.Registration(ctx, bob.ID); stored.UpdatedBy.String() != editor {
			t.Fatalf("updated by: %+v", stored)
		}

		// A deleted registration frees its places and leaves every list.
		if err := service.Delete(ctx, bob.ID); err != nil {
			t.Fatal(err)
		}

		availability, err := service.Availability(ctx, "bbq", "en")
		if err != nil || availability.Taken != 2 || availability.Left != 1 || !availability.Open {
			t.Fatalf("availability: %+v %v", availability, err)
		}

		summaries, err := service.Summaries(ctx)
		if err != nil || summaries["bbq"].Registrations != 1 || summaries["bbq"].Places != 2 {
			t.Fatalf("summaries: %+v %v", summaries, err)
		}

		if list, err := service.Registrations(ctx, "bbq"); err != nil || len(list) != 1 || list[0].ID != ada.ID {
			t.Fatalf("registrations: %+v %v", list, err)
		}

		if _, err := service.ByCode(visitor, bob.Code); !errors.Is(err, ErrNotFound) {
			t.Fatalf("code of a deleted registration: %v", err)
		}

		if _, err := service.Registration(ctx, bob.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted registration: %v", err)
		}

		if err := service.Update(ctx, bob.ID, PaymentPaid, ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("update deleted: %v", err)
		}

		if err := service.Cancel(visitor, bob.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cancel deleted: %v", err)
		}

		trashed, _, err := service.Trashed(ctx, 0, 0)
		if err != nil || len(trashed) != 1 || trashed[0].ID != bob.ID || trashed[0].DeletedBy.String() != editor ||
			trashed[0].DeletedAt == nil {
			t.Fatalf("trashed: %+v %v", trashed, err)
		}

		// Its place is taken meanwhile: restoring it would exceed the capacity.
		cy := register("Cy", 1)

		err = bin.Restore(ctx, []string{bob.ID})

		var refused *admin.TrashError
		if !errors.As(err, &refused) || refused.ID != bob.ID ||
			refused.Reason != "This registration cannot be restored: not enough places are left." {
			t.Fatalf("restore over capacity: %v", err)
		}

		// A cancelled registration takes no places, so it comes back even
		// when the subject is full.
		if err := service.Cancel(visitor, cy.ID); err != nil {
			t.Fatal(err)
		}

		if stored, _ = service.Registration(ctx, cy.ID); stored.UpdatedBy != "" || stored.UpdatedAt.Before(stored.CreatedAt) {
			t.Fatalf("cancelled by a visitor: %+v", stored)
		}

		if err := service.Delete(ctx, cy.ID); err != nil {
			t.Fatal(err)
		}

		// The section's trash names who registered for what, newest first,
		// and searches and pages.
		items, total, err := bin.List(ctx, "", 10, 0)
		if err != nil || total != 2 || items[0].ID != cy.ID || items[1].Title != "Bob · Barbecue" ||
			items[1].DeletedBy != editor || items[1].DeletedAt.IsZero() {
			t.Fatalf("trash: %d %+v %v", total, items, err)
		}

		if items, total, err = bin.List(ctx, "bob@example.com", 10, 0); err != nil || total != 1 || items[0].ID != bob.ID {
			t.Fatalf("search: %d %+v %v", total, items, err)
		}

		if items, total, err = bin.List(
			ctx,
			"barbecue",
			1,
			1,
		); err != nil || total != 2 || len(items) != 1 ||
			items[0].ID != bob.ID {
			t.Fatalf("page: %d %+v %v", total, items, err)
		}

		// With Ada's places freed, Bob comes back with his code.
		if err := service.Cancel(ctx, ada.ID); err != nil {
			t.Fatal(err)
		}

		restorer := db.NewID()

		if err := bin.Restore(db.WithActor(ctx, restorer), []string{bob.ID, cy.ID}); err != nil {
			t.Fatal(err)
		}

		restored, err := service.ByCode(visitor, bob.Code)
		if err != nil || restored.ID != bob.ID || restored.UpdatedBy.String() != restorer || restored.DeletedAt != nil ||
			restored.DeletedBy != "" || restored.AdminNotes != "Friend of Ada" {
			t.Fatalf("restored: %+v %v", restored, err)
		}

		if taken, err := service.taken(ctx, "bbq"); err != nil || taken != 1 {
			t.Fatalf("taken after restore: %d %v", taken, err)
		}

		err = bin.Restore(ctx, []string{bob.ID})
		if !errors.As(err, &refused) || refused.Reason != "This item is no longer in the trash." {
			t.Fatalf("restore twice: %v", err)
		}

		// Purging removes only registrations in the trash.
		if err := bin.Purge(ctx, []string{bob.ID}); !errors.As(err, &refused) {
			t.Fatalf("purge a live registration: %v", err)
		}

		if err := service.Delete(ctx, bob.ID); err != nil {
			t.Fatal(err)
		}

		if err := bin.Purge(ctx, []string{bob.ID}); err != nil {
			t.Fatal(err)
		}

		if _, total, err := bin.List(ctx, "", 10, 0); err != nil || total != 0 {
			t.Fatalf("purged: %d %v", total, err)
		}

		var left int

		if err := database.Get(ctx, &left, `SELECT COUNT(*) FROM wt_registration WHERE id=?`, bob.ID); err != nil || left != 0 {
			t.Fatalf("purged row: %d %v", left, err)
		}
	})
}

// TestCapacityRace signs up many visitors at once: the capacity holds.
func TestCapacityRace(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := open(t, database)

		if err := service.SaveSettings(ctx, Settings{SubjectID: "gig", Title: "Gig", Open: true, Capacity: 5}); err != nil {
			t.Fatal(err)
		}

		var (
			wg       sync.WaitGroup
			mu       sync.Mutex
			admitted int
			full     int
		)

		for range 12 {
			wg.Go(func() {
				_, err := service.Register(ctx, Signup{SubjectID: "gig", Name: "Fan", Email: "fan@example.com", Adults: 1})

				mu.Lock()
				defer mu.Unlock()

				switch {
				case err == nil:
					admitted++
				case errors.Is(err, ErrFull):
					full++
				default:
					t.Error(err)
				}
			})
		}

		wg.Wait()

		taken, err := service.taken(ctx, "gig")
		if err != nil || admitted != 5 || full != 7 || taken != 5 {
			t.Fatalf("admitted %d, full %d, taken %d, %v", admitted, full, taken, err)
		}
	})
}

// TestEventDeadline closes registration for an event when it starts.
func TestEventDeadline(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := open(t, database)

		agenda := &agenda{}
		service.events = func() (eventSource, error) {
			return agenda, nil
		}

		amsterdam, err := time.LoadLocation("Europe/Amsterdam")
		if err != nil {
			t.Fatal(err)
		}

		event := func(title, slug string, start time.Time) string {
			id := db.NewID()
			agenda.list = append(agenda.list, events.Info{
				ID:        id,
				Title:     title,
				Start:     start.In(amsterdam),
				Timezone:  "Europe/Amsterdam",
				Published: true,
				URL:       "/en/agenda/" + slug,
			})

			return id
		}

		upcoming := event("Fair", "fair", time.Now().Add(48*time.Hour).Truncate(time.Minute))
		started := event("Market", "market", time.Now().Add(-time.Hour).Truncate(time.Minute))

		for _, id := range []string{upcoming, started} {
			if err := service.SaveSettings(ctx, Settings{SubjectID: id, Open: true}); err != nil {
				t.Fatal(err)
			}
		}

		signup := Signup{SubjectID: upcoming, Name: "Ada", Email: "ada@example.com", Adults: 1, Locale: "nl"}

		if _, err := service.Register(ctx, signup); err != nil {
			t.Fatal(err)
		}

		signup.SubjectID = started

		if _, err := service.Register(ctx, signup); !errors.Is(err, ErrClosed) {
			t.Fatalf("started event: %v", err)
		}

		subject, err := service.Subject(ctx, upcoming, "nl")
		if err != nil || subject.Title != "Fair" || subject.Start.Location().String() != "Europe/Amsterdam" ||
			subject.URL != "/en/agenda/fair" {
			t.Fatalf("subject: %+v %v", subject, err)
		}

		list, err := service.eventSubjects(ctx, "en")
		if err != nil || len(list) != 2 || list[0].ID != upcoming || list[0].Title != "Fair" {
			t.Fatalf("events: %+v %v", list, err)
		}

		availability, err := service.Availability(ctx, upcoming, "en")
		if err != nil || !availability.Open || availability.ClosesAt.IsZero() {
			t.Fatalf("availability: %+v %v", availability, err)
		}

		// An event that is gone closes registration.
		agenda.list = agenda.list[1:]

		if gone, err := service.Availability(ctx, upcoming, "en"); err != nil || gone.Open {
			t.Fatalf("removed event: %+v %v", gone, err)
		}

		// A key that names no events instance fails clearly.
		service.events = linkedEvents(func(string) any { return nil }, "agenda")

		if _, err := service.Availability(ctx, started, "en"); !errors.Is(err, ErrNoEvents) {
			t.Fatalf("missing events instance: %v", err)
		}
	})
}

// agenda is an events instance with fixed events, latest start first.
type agenda struct {
	list []events.Info
}

func (a *agenda) Event(_ context.Context, id, _ string) (events.Info, error) {
	for _, info := range a.list {
		if info.ID == id {
			return info, nil
		}
	}

	return events.Info{}, ErrNotFound
}

func (a *agenda) Events(context.Context, string) ([]events.Info, error) {
	return a.list, nil
}

func TestCents(t *testing.T) {
	for input, want := range map[string]Cents{"": 0, "12": 1200, "12.5": 1250, "12,50": 1250, ".99": 99, "0.05": 5} {
		got, err := ParseCents(input)
		if err != nil || got != want {
			t.Fatalf("ParseCents(%q) = %d, %v", input, got, err)
		}
	}

	for _, input := range []string{"abc", "1.234", "-1", "1.2.3", "99999999999"} {
		if _, err := ParseCents(input); err == nil {
			t.Fatalf("ParseCents(%q) accepted", input)
		}
	}

	if Cents(1250).String() != "12.50" || Cents(5).String() != "0.05" {
		t.Fatal("Cents.String")
	}

	if normalizeCode("k7q4 9m2x") != "K7Q4-9M2X" || normalizeCode("K7Q4-9M2") != "" {
		t.Fatal("normalizeCode")
	}
}
