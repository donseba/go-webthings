package polls

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"
)

// TestStore runs the schema, ordering, votes and their cleanup on every configured dialect.
func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "module:polls", schema()); err != nil {
			t.Fatal(err)
		}

		items, err := content.NewCollection(database, spec())
		if err != nil {
			t.Fatal(err)
		}

		service := &Service{key: "survey", db: database, items: items, actions: webthings.Env{Key: "survey"}.Actions()}
		other := &Service{key: "other", db: database, items: items, actions: webthings.Env{Key: "other"}.Actions()}

		service.registerActions()
		other.registerActions()

		store := service.Store()

		older := savePoll(t, store, "Older?", "older", nil)
		newer := savePoll(t, store, "Newer?", "newer", nil)

		for _, id := range []string{older.ID, newer.ID} {
			if err := store.Publish(ctx, id, "", "en"); err != nil {
				t.Fatal(err)
			}
		}

		// New polls come first.
		latest, err := service.Latest(ctx, nil, "en", 0)
		if err != nil || len(latest) != 2 || latest[0].Question != "Newer?" {
			t.Fatalf("latest: %+v %v", latest, err)
		}

		yes, no := newer.Fields.Options[0], newer.Fields.Options[1]

		for _, visitor := range []string{"a", "b", "c"} {
			if err := service.Vote(ctx, newer.ID, yes, visitor); err != nil {
				t.Fatal(err)
			}
		}

		if err := service.Vote(ctx, newer.ID, no, "d"); err != nil {
			t.Fatal(err)
		}

		failures := []struct {
			poll, option, visitor string
			want                  error
		}{
			{newer.ID, no, "a", ErrAlreadyVoted},
			{newer.ID, older.Fields.Options[0], "e", ErrInvalidOption},
			{"not-an-id", yes, "e", content.ErrNotFound},
			{db.NewID(), yes, "e", content.ErrNotFound},
		}

		for _, failure := range failures {
			err := service.Vote(ctx, failure.poll, failure.option, failure.visitor)
			if !errors.Is(err, failure.want) {
				t.Fatalf("vote %+v: %v", failure, err)
			}
		}

		// Each instance only sees its own polls.
		if err := other.Vote(ctx, newer.ID, yes, "e"); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("vote in other instance: %v", err)
		}

		poll, err := service.Find(ctx, nil, "en", "newer")
		if err != nil || poll.Total != 4 || poll.Options[0].Votes != 3 || poll.Options[0].Percent != 75 || poll.Voted {
			t.Fatalf("results: %+v %v", poll, err)
		}

		choices, err := service.choices(ctx, "d")
		if err != nil || choices[newer.ID] != no {
			t.Fatalf("choices: %v %v", choices, err)
		}

		if votes, err := service.votesFor(ctx, newer.ID, []string{no}); err != nil || votes != 1 {
			t.Fatalf("votes for no: %d %v", votes, err)
		}

		// A closed poll takes no votes.
		past := time.Now().Add(-time.Hour).UTC()
		closed := savePoll(t, store, "Closed?", "", &past)

		if err := store.Publish(ctx, closed.ID, "", "en"); err != nil {
			t.Fatal(err)
		}

		if err := service.Vote(ctx, closed.ID, closed.Fields.Options[0], "a"); !errors.Is(err, ErrClosed) {
			t.Fatalf("closed vote: %v", err)
		}

		if err := service.ResetVotes(ctx, newer.ID); err != nil {
			t.Fatal(err)
		}

		if poll, err := service.Find(ctx, nil, "en", newer.ID); err != nil || poll.Total != 0 {
			t.Fatalf("after reset: %+v %v", poll, err)
		}

		// Deleting a poll for good deletes its votes; in the trash it keeps
		// them, so it comes back as it was.
		if err := service.Vote(ctx, older.ID, older.Fields.Options[0], "a"); err != nil {
			t.Fatal(err)
		}

		if err := store.Delete(ctx, older.ID); err != nil {
			t.Fatal(err)
		}

		if err := store.Purge(ctx, older.ID); err != nil {
			t.Fatal(err)
		}

		var left int

		if err := database.Get(ctx, &left, `SELECT COUNT(*) FROM wt_poll_vote`); err != nil || left != 0 {
			t.Fatalf("votes left: %d %v", left, err)
		}

		// Translations cannot have more labels than the poll has options.
		_, err = store.Save(ctx, content.Draft[Fields, Text]{
			Fields:       Fields{Options: Lines{db.NewID()}},
			Translations: []content.Translation[Text]{{Locale: "en", Text: Text{Question: "?", Options: Lines{"a", "b"}}}},
		})
		if !errors.Is(err, errOptions) {
			t.Fatalf("mismatched labels: %v", err)
		}

		// Option IDs may be chosen by code, but must be set, short and unique.
		for _, ids := range []Lines{{"a", "a"}, {"a", ""}, {"a", strings.Repeat("b", 37)}} {
			_, err = store.Save(ctx, content.Draft[Fields, Text]{Fields: Fields{Options: ids}})
			if !errors.Is(err, errOptionID) {
				t.Fatalf("option IDs %q: %v", ids, err)
			}
		}

		own, err := store.Save(ctx, content.Draft[Fields, Text]{
			Fields: Fields{Options: Lines{"red", "green"}},
			Translations: []content.Translation[Text]{
				{Locale: "en", Text: Text{Question: "Colour?", Options: Lines{"Red", "Green"}}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := store.Publish(ctx, own.ID, "", "en"); err != nil {
			t.Fatal(err)
		}

		if err := service.Vote(ctx, own.ID, "green", "a"); err != nil {
			t.Fatalf("vote with an own option ID: %v", err)
		}
	})
}

// TestAddressID checks that visitors without a cookie are known by the
// first half of their visitor ID (webthings.Env.VisitorID, tested in core),
// as long as the cookie's id.
func TestAddressID(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/_/survey/vote", nil)
	service := &Service{key: "survey", visitorID: func(_ context.Context, r *http.Request, parts ...string) (string, error) {
		if r != request || len(parts) != 0 {
			t.Fatalf("visitor id asked for %v %v", r, parts)
		}

		return strings.Repeat("ab", 32), nil
	}}

	id, err := service.addressID(request)
	if err != nil || !visitorPattern.MatchString(id) || id != strings.Repeat("ab", 16) {
		t.Fatalf("address id: %q %v", id, err)
	}

	if _, err := (&Service{}).addressID(request); err == nil {
		t.Fatal("address id without a secret")
	}
}

func savePoll(t *testing.T, store content.Store[Fields, Text], question, slug string, closesAt *time.Time) Item {
	t.Helper()

	item, err := store.Save(context.Background(), content.Draft[Fields, Text]{
		Fields: Fields{Options: Lines{db.NewID(), db.NewID()}, ClosesAt: closesAt},
		Translations: []content.Translation[Text]{
			{Locale: "en", Text: Text{Question: question, Slug: slug, Options: Lines{"Yes", "No"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	return item
}

func TestOptionLines(t *testing.T) {
	labels, err := optionLines(" Yes \n\nNo\r\nMaybe")
	if err != nil || len(labels) != 3 || labels[0] != "Yes" || labels[1] != "No" {
		t.Fatalf("lines: %q %v", labels, err)
	}

	if _, err := optionLines("Only one"); !errors.Is(err, errOptions) {
		t.Fatalf("one option: %v", err)
	}
}
