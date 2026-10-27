package registrations

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/site"
)

// TestAvailabilities checks that the admin overview's availabilities of all
// subjects at once tell what Availability tells of each: open, closed by
// hand or by a deadline, full, and closed by the start of its event or by
// an event that is gone.
func TestAvailabilities(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		service := open(t, database)

		agenda := &agenda{}
		service.events = func() (eventSource, error) {
			return agenda, nil
		}

		event := func(start time.Time) string {
			id := db.NewID()
			agenda.list = append(agenda.list, events.Info{ID: id, Title: "Event", Start: start, Published: true})

			return id
		}

		past := time.Now().Add(-time.Hour)
		future := time.Now().Add(48 * time.Hour)

		subjects := []Settings{
			{SubjectID: event(future), Open: true, Capacity: 10},
			{SubjectID: event(past), Open: true},
			{SubjectID: event(future), Open: false},
			{SubjectID: event(future), Open: true, Capacity: 1},
			{SubjectID: event(future), Open: true, ClosesAt: &past},
			{SubjectID: db.NewID(), Open: true},
		}

		for _, settings := range subjects {
			if err := service.SaveSettings(ctx, settings); err != nil {
				t.Fatal(err)
			}
		}

		for _, subject := range []string{subjects[0].SubjectID, subjects[3].SubjectID} {
			signup := Signup{SubjectID: subject, Name: "Ada", Email: "ada@example.com", Adults: 1, Locale: "en"}

			if _, err := service.Register(ctx, signup); err != nil {
				t.Fatal(err)
			}
		}

		all, err := service.AllSettings(ctx)
		if err != nil {
			t.Fatal(err)
		}

		summaries, err := service.Summaries(ctx)
		if err != nil {
			t.Fatal(err)
		}

		list, err := service.availabilities(ctx, all, summaries)
		if err != nil || len(list) != len(subjects) {
			t.Fatalf("availabilities: %d, %v", len(list), err)
		}

		for _, settings := range subjects {
			one, err := service.Availability(ctx, settings.SubjectID, "")
			if err != nil {
				t.Fatal(err)
			}

			// The overview shows neither the notes nor the closing time.
			one.Notes = ""
			one.ClosesAt = time.Time{}

			if got, want := fmt.Sprintf("%+v", list[settings.SubjectID]), fmt.Sprintf("%+v", one); got != want {
				t.Fatalf("subject %+v:\n got %s\nwant %s", settings, got, want)
			}
		}

		if !list[subjects[0].SubjectID].Open || list[subjects[3].SubjectID].Open || !list[subjects[3].SubjectID].Full {
			t.Fatalf("availabilities: %+v", list)
		}
	})
}

// BenchmarkOverview compares the overview's availabilities of 50 events,
// read per subject with Availability and at once.
func BenchmarkOverview(b *testing.B) {
	ctx := context.Background()

	database, err := db.Open(ctx, "sqlite", "file:"+filepath.Join(b.TempDir(), "bench.sqlite"))
	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() { _ = database.Close() })

	for name, schema := range map[string]fs.FS{"media": media.Schema(), "site": site.Schema(), "module:registrations": schema()} {
		if err := database.ApplySchema(ctx, name, schema); err != nil {
			b.Fatal(err)
		}
	}

	settings := site.New(database)

	if err := settings.Init(ctx, "Bench", locales.Config{Default: "en", Enabled: []string{"en"}}); err != nil {
		b.Fatal(err)
	}

	service := &Service{
		key:      "signup",
		db:       database,
		site:     settings,
		currency: "EUR",
		actions:  webthings.Env{Key: "signup"}.Actions(),
	}
	agenda := &agenda{}
	service.events = func() (eventSource, error) {
		return agenda, nil
	}

	for i := range 50 {
		id := db.NewID()
		agenda.list = append(agenda.list, events.Info{ID: id, Title: fmt.Sprint("Event ", i), Start: time.Now().Add(time.Hour)})

		if err := service.SaveSettings(ctx, Settings{SubjectID: id, Open: true, Capacity: 100}); err != nil {
			b.Fatal(err)
		}

		signup := Signup{SubjectID: id, Name: "Ada", Email: "ada@example.com", Adults: 2, Locale: "en"}

		if _, err := service.Register(ctx, signup); err != nil {
			b.Fatal(err)
		}
	}

	all, err := service.AllSettings(ctx)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("per subject", func(b *testing.B) {
		for b.Loop() {
			for _, subject := range all {
				if _, err := service.Availability(ctx, subject.SubjectID, ""); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("at once", func(b *testing.B) {
		for b.Loop() {
			summaries, err := service.Summaries(ctx)
			if err != nil {
				b.Fatal(err)
			}

			if _, err := service.availabilities(ctx, all, summaries); err != nil {
				b.Fatal(err)
			}
		}
	})
}
