package submissions

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/modules/publishing"
)

// TestStore runs proposals, decisions, media tracking and settings on every
// configured dialect.
func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "module:submissions", schema()); err != nil {
			t.Fatal(err)
		}

		storage, err := media.NewFilesystem(t.TempDir(), "/media")
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = storage.Close() })

		library := media.New(database, storage)

		posts := &drafts{}
		service := &Service{
			key:    "tips",
			target: "news",
			db:     database,
			media:  library,
			drafts: func() (drafter, error) {
				return posts, nil
			},
		}
		editor := db.NewID()
		staff := db.WithActor(ctx, editor)

		invalid := []Proposal{
			{Name: "Jane", Email: "jane@example.test", Body: "No title", Locale: "en"},
			{Name: "Jane", Email: "not an address", Title: "Title", Body: "Text", Locale: "en"},
			{Name: "Jane\nDoe", Email: "jane@example.test", Title: "Title", Body: "Text", Locale: "en"},
			// Control characters are refused as in comments: they end up in
			// mail subjects and admin lists.
			{Name: "Jane\tDoe", Email: "jane@example.test", Title: "Title", Body: "Text", Locale: "en"},
			{Name: "Jane", Email: "jane@example.test", Title: "Title\x00", Body: "Text", Locale: "en"},
			{Name: "Jane", Email: "jane@example.test", Title: "Title", Body: "  ", Locale: "en"},
			{Name: "Jane", Email: "jane@example.test", Title: strings.Repeat("x", 201), Body: "Text", Locale: "en"},
			{Name: "Jane", Email: "jane@example.test", Title: "Title", Body: "Text", Locale: "not a locale"},
		}

		for _, proposal := range invalid {
			if _, err := service.Create(ctx, proposal); !errors.Is(err, ErrInvalid) {
				t.Fatalf("%+v: want ErrInvalid, got %v", proposal, err)
			}
		}

		photo := upload(t, library)

		street, err := service.Create(ctx, Proposal{
			Name:    " Jane ",
			Email:   "jane@example.test",
			Title:   "Straatfeest op zaterdag",
			Body:    "Iedereen is *welkom*.\r\n",
			MediaID: photo,
			Locale:  "nl",
		})
		if err != nil {
			t.Fatal(err)
		}

		if street.Name != "Jane" || street.Body != "Iedereen is *welkom*." || street.Status != StatusNew ||
			street.MediaID != photo || street.Locale != "nl" || street.PostID != "" || !street.DecidedAt.IsZero() {
			t.Fatalf("created: %+v", street)
		}

		// A proposed image stays in the library until the proposal is gone.
		if err := library.Delete(ctx, photo); !errors.Is(err, media.ErrInUse) {
			t.Fatalf("delete proposed image: %v", err)
		}

		garden, err := service.Create(ctx, Proposal{
			Name:    "Joe",
			Email:   "joe@example.test",
			Title:   "Garden",
			Body:    "Plants",
			MediaID: upload(t, library),
			Locale:  "en",
		})
		if err != nil {
			t.Fatal(err)
		}

		list, more, err := service.List(ctx, StatusNew, 0, 1)
		if err != nil || len(list) != 1 || !more || list[0].ID != garden.ID {
			t.Fatalf("list: %+v %v %v", list, more, err)
		}

		// Visitors have no actor; the editor's decision is recorded.
		expectAudit(t, database, street.ID, "")

		accepted, err := service.Accept(staff, street.ID, editor)
		if err != nil {
			t.Fatal(err)
		}

		if accepted.Status != StatusAccepted || accepted.PostID == "" || accepted.DecidedBy != editor ||
			accepted.DecidedAt.IsZero() {
			t.Fatalf("accepted: %+v", accepted)
		}

		expectAudit(t, database, street.ID, editor)

		want := publishing.DraftInput{
			Locale:  "nl",
			Title:   "Straatfeest op zaterdag",
			Body:    "Iedereen is *welkom*.",
			ImageID: photo,
			Author:  editor,
			Note:    "Jane <jane@example.test>",
		}

		if len(posts.saved) != 1 || posts.saved[0] != want || accepted.PostID != posts.ids[0] {
			t.Fatalf("draft post: %+v %s", posts.saved, accepted.PostID)
		}

		if _, err := service.Accept(ctx, street.ID, editor); !errors.Is(err, ErrDecided) || len(posts.saved) != 1 {
			t.Fatalf("accept twice: %v", err)
		}

		// A failing draft leaves the submission new.
		posts.err = errors.New("draft refused")

		if _, err := service.Accept(ctx, garden.ID, editor); !errors.Is(err, posts.err) {
			t.Fatalf("failing draft: %v", err)
		}

		posts.err = nil

		if still, err := service.Get(ctx, garden.ID); err != nil || still.Status != StatusNew || still.PostID != "" {
			t.Fatalf("after failing draft: %+v %v", still, err)
		}

		if _, err := service.Reject(ctx, street.ID, editor); !errors.Is(err, ErrDecided) {
			t.Fatalf("reject accepted: %v", err)
		}

		rejected, err := service.Reject(staff, garden.ID, editor)
		if err != nil || rejected.Status != StatusRejected || rejected.PostID != "" {
			t.Fatalf("rejected: %+v %v", rejected, err)
		}

		if _, err := service.Reject(ctx, garden.ID, editor); !errors.Is(err, ErrDecided) {
			t.Fatalf("reject twice: %v", err)
		}

		for status, want := range map[string]int{StatusNew: 0, StatusAccepted: 1, StatusRejected: 1, "": 2} {
			if count, err := service.Count(ctx, status); err != nil || count != want {
				t.Fatalf("count %q: %d %v", status, count, err)
			}
		}

		testTrash(t, service, library, posts, garden, accepted)
		testSettings(t, service, database)
	})
}

// testTrash deletes the rejected garden and the accepted street proposal,
// restores and purges them.
func testTrash(t *testing.T, service *Service, library *media.Service, posts *drafts, garden, street Submission) {
	t.Helper()

	drafted := len(posts.saved)

	ctx := context.Background()
	bin := service.trash()
	editor := db.NewID()
	staff := db.WithActor(ctx, editor)

	// Deleting moves a proposal to the trash: it leaves the lists, counts
	// and decisions, while its image stays in the library.
	if err := service.Delete(staff, garden.ID); err != nil {
		t.Fatal(err)
	}

	if err := service.Delete(staff, garden.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}

	if _, err := service.Get(ctx, garden.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("get deleted: %v", err)
	}

	if _, err := service.Accept(staff, garden.ID, editor); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("accept deleted: %v", err)
	}

	for status, want := range map[string]int{StatusRejected: 0, StatusAccepted: 1, "": 1} {
		if count, err := service.Count(ctx, status); err != nil || count != want {
			t.Fatalf("count %q after delete: %d %v", status, count, err)
		}
	}

	if list, _, err := service.List(ctx, "", 0, 10); err != nil || len(list) != 1 || list[0].ID != street.ID {
		t.Fatalf("list after delete: %+v %v", list, err)
	}

	if err := library.Delete(ctx, garden.MediaID); !errors.Is(err, media.ErrInUse) {
		t.Fatalf("delete image of trashed proposal: %v", err)
	}

	if err := service.Delete(staff, street.ID); err != nil {
		t.Fatal(err)
	}

	// The trash lists both, most recently deleted first, with search and
	// pages.
	items, total, err := bin.List(ctx, "", 10, 0)
	if err != nil || total != 2 || items[0].ID != street.ID || items[1].ID != garden.ID ||
		items[1].Title != "Garden (Joe)" || items[1].DeletedBy != editor || items[1].DeletedAt.IsZero() {
		t.Fatalf("trash: %+v %d %v", items, total, err)
	}

	if items, total, err := bin.List(ctx, "joe@example", 10, 0); err != nil || total != 1 || items[0].ID != garden.ID {
		t.Fatalf("search: %+v %d %v", items, total, err)
	}

	if items, total, err := bin.List(ctx, "", 1, 1); err != nil || total != 2 || len(items) != 1 || items[0].ID != garden.ID {
		t.Fatalf("page 2: %+v %d %v", items, total, err)
	}

	// Restoring brings a proposal back as it was.
	if err := bin.Restore(staff, []string{garden.ID}); err != nil {
		t.Fatal(err)
	}

	if back, err := service.Get(ctx, garden.ID); err != nil || back.Status != StatusRejected || back.MediaID != garden.MediaID {
		t.Fatalf("restored: %+v %v", back, err)
	}

	expectAudit(t, service.db, garden.ID, editor)
	expectReason(t, bin.Restore(staff, []string{garden.ID}), "This item is no longer in the trash.")
	expectReason(t, bin.Purge(staff, []string{garden.ID}), "This item is no longer in the trash.")

	// Purging a rejected proposal moves its image to the library's trash;
	// an accepted one keeps it for the post, which is not touched.
	if err := service.Delete(staff, garden.ID); err != nil {
		t.Fatal(err)
	}

	if err := bin.Purge(staff, []string{garden.ID, street.ID}); err != nil {
		t.Fatal(err)
	}

	if _, err := library.Get(ctx, garden.MediaID); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("image of purged proposal: %v", err)
	}

	if _, err := library.Get(ctx, street.MediaID); err != nil {
		t.Fatalf("image of accepted proposal: %v", err)
	}

	if _, total, err := bin.List(ctx, "", 10, 0); err != nil || total != 0 {
		t.Fatalf("empty trash: %d %v", total, err)
	}

	var left int
	if err := service.db.Get(ctx, &left, `SELECT COUNT(*) FROM wt_submission`); err != nil || left != 0 {
		t.Fatalf("rows after purge: %d %v", left, err)
	}

	if len(posts.saved) != drafted {
		t.Fatal("deleting or purging made posts")
	}
}

// expectAudit checks who last changed a proposal; empty is NULL. Proposals
// come from visitors, so created_by stays empty.
func expectAudit(t *testing.T, database *db.DB, id, updatedBy string) {
	t.Helper()

	var audit struct {
		CreatedBy db.Ref    `db:"created_by"`
		UpdatedBy db.Ref    `db:"updated_by"`
		CreatedAt time.Time `db:"created_at"`
		UpdatedAt time.Time `db:"updated_at"`
	}

	err := database.Get(context.Background(), &audit,
		`SELECT created_by, updated_by, created_at, updated_at FROM wt_submission WHERE id=?`, id)
	if err != nil {
		t.Fatal(err)
	}

	if audit.CreatedBy != "" || audit.UpdatedBy.String() != updatedBy || audit.CreatedAt.IsZero() || audit.UpdatedAt.IsZero() {
		t.Fatalf("audit of %s: %+v, want updated by %q", id, audit, updatedBy)
	}
}

// expectReason checks that a trash action failed with one TrashError.
func expectReason(t *testing.T, err error, reason string) {
	t.Helper()

	var failure *admin.TrashError
	if !errors.As(err, &failure) || failure.Reason != reason {
		t.Fatalf("want %q, got %v", reason, err)
	}
}

func testSettings(t *testing.T, service *Service, database *db.DB) {
	t.Helper()

	ctx := context.Background()
	editor := db.NewID()

	settings, err := service.Settings(ctx)
	if err != nil || settings != (Settings{}) {
		t.Fatalf("empty settings: %+v %v", settings, err)
	}

	if err := service.SaveSettings(ctx, Settings{Notify: "a@b, c@d", DecisionMail: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid address: %v", err)
	}

	if err := service.SaveSettings(ctx, Settings{Notify: " editors@example.test ", ConfirmMail: true}); err != nil {
		t.Fatal(err)
	}

	if err := service.SaveSettings(
		db.WithActor(ctx, editor),
		Settings{Notify: "editors@example.test", DecisionMail: true},
	); err != nil {
		t.Fatal(err)
	}

	var updatedBy db.Ref
	if err := database.Get(
		ctx,
		&updatedBy,
		`SELECT updated_by FROM wt_submission_setting WHERE instance=?`,
		"tips",
	); err != nil ||
		updatedBy.String() != editor {
		t.Fatalf("settings updated_by %q: %v", updatedBy, err)
	}

	settings, err = service.Settings(ctx)
	if err != nil || settings != (Settings{Notify: "editors@example.test", DecisionMail: true}) {
		t.Fatalf("settings: %+v %v", settings, err)
	}
}

// drafts records the posts an accepted submission becomes.
type drafts struct {
	saved []publishing.DraftInput
	ids   []string
	err   error
}

func (d *drafts) Draft(_ context.Context, input publishing.DraftInput) (string, error) {
	if d.err != nil {
		return "", d.err
	}

	d.saved = append(d.saved, input)
	d.ids = append(d.ids, db.NewID())

	return d.ids[len(d.ids)-1], nil
}

// upload adds a small PNG to the library and returns its ID.
func upload(t *testing.T, library *media.Service) string {
	t.Helper()

	var buffer bytes.Buffer

	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}

	asset, err := library.Upload(context.Background(), "photo.png", &buffer)
	if err != nil {
		t.Fatal(err)
	}

	return asset.ID
}
