package comments

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
)

// TestStore runs threading, moderation, paging and settings on every
// configured dialect.
func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "module:comments", schema()); err != nil {
			t.Fatal(err)
		}

		remarks := &Service{key: "remarks", db: database}
		guestbook := &Service{key: "guestbook", db: database}

		settings, err := remarks.Settings(ctx)
		if err != nil || !settings.Moderated || settings.Closed || settings.Notify != "" {
			t.Fatalf("default settings: %+v %v", settings, err)
		}

		ada := add(t, remarks, Entry{Subject: "post-1", Name: "Ada", Email: "ada@example.test", Body: "First\r\nline two"})
		bob := add(t, remarks, Entry{Subject: "post-1", Name: "Bob", Body: "Second", Approved: true})
		add(t, remarks, Entry{Subject: "post-2", Name: "Cy", Body: "Elsewhere", Approved: true})
		add(t, guestbook, Entry{Subject: "post-1", Name: "Dee", Body: "Other instance", Approved: true})

		// Ada waits for approval; Bob is shown.
		expectNames(t, remarks, "post-1", "Bob")
		expectCount(t, remarks, "post-1", 1)

		if pending, err := remarks.Pending(ctx); err != nil || pending != 1 {
			t.Fatalf("pending: %d %v", pending, err)
		}

		if err := remarks.Approve(ctx, ada.ID, true); err != nil {
			t.Fatal(err)
		}

		// A reply to a reply joins the top-level comment; a hidden reply is
		// not shown.
		reply := add(t, remarks, Entry{Subject: "post-1", ParentID: ada.ID, Name: "Eve", Body: "Agreed", Approved: true})
		add(t, remarks, Entry{Subject: "post-1", ParentID: reply.ID, Name: "Fay", Body: "Me too", Approved: true})
		add(t, remarks, Entry{Subject: "post-1", ParentID: ada.ID, Name: "Gil", Body: "Hidden"})

		staff, err := remarks.Reply(ctx, bob.ID, "The site", "Thanks Bob")
		if err != nil || !staff.Staff {
			t.Fatalf("staff reply: %+v %v", staff, err)
		}

		list, err := remarks.List(ctx, "post-1")
		if err != nil {
			t.Fatal(err)
		}

		if len(list) != 2 || list[0].Name != "Ada" || list[1].Name != "Bob" {
			t.Fatalf("threads: %+v", list)
		}

		if names(list[0].Replies) != "Eve|Fay" || names(list[1].Replies) != "The site" || !list[1].Replies[0].Staff {
			t.Fatalf("replies: %+v", list)
		}

		if list[0].Body != "First\nline two" || string(list[0].HTML) != "<p>First<br>line two</p>" {
			t.Fatalf("body: %q %q", list[0].Body, list[0].HTML)
		}

		expectCount(t, remarks, "post-1", 5)

		// Hiding a comment hides its replies too.
		if err := remarks.Approve(ctx, ada.ID, false); err != nil {
			t.Fatal(err)
		}

		expectNames(t, remarks, "post-1", "Bob")
		expectCount(t, remarks, "post-1", 2)

		// Invalid entries and parents are refused.
		invalid := []Entry{
			{Subject: "post-1", Name: "", Body: "No name"},
			{Subject: "post-1", Name: "Ann", Body: "  "},
			{Subject: "", Name: "Ann", Body: "No subject"},
			{Subject: "post-1", Name: "Ann\nBob", Body: "Two lines"},
			{Subject: "post-1", Name: "Ann", Email: "not an address", Body: "Bad mail"},
			{Subject: "post-1", Name: "Ann", Body: strings.Repeat("a", 5001)},
			{Subject: "post-2", ParentID: bob.ID, Name: "Ann", Body: "Wrong subject"},
			{Subject: "post-1", ParentID: db.NewID(), Name: "Ann", Body: "Unknown parent"},
			{Subject: "post-1", ParentID: "nonsense", Name: "Ann", Body: "Not an ID"},
		}

		for _, entry := range invalid {
			entry.Locale = "en"

			if _, err := remarks.Add(ctx, entry); !errors.Is(err, ErrInvalid) {
				t.Fatalf("%q: want ErrInvalid, got %v", entry.Body, err)
			}
		}

		// Another instance cannot touch these comments.
		if err := guestbook.Approve(ctx, bob.ID, false); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("approve from another instance: %v", err)
		}

		if err := guestbook.Delete(ctx, bob.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("delete from another instance: %v", err)
		}

		// Deleting a comment deletes its replies.
		if err := remarks.Delete(ctx, bob.ID); err != nil {
			t.Fatal(err)
		}

		pending, approved, err := remarks.counts(ctx)
		if err != nil || pending != 2 || approved != 3 {
			t.Fatalf("counts after delete: %d %d %v", pending, approved, err)
		}

		// Settings are per instance.
		if err := remarks.SaveSettings(ctx, Settings{Closed: true, Notify: "mod@example.test"}); err != nil {
			t.Fatal(err)
		}

		if err := remarks.SaveSettings(ctx, Settings{Notify: "a@b, c@d"}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid notify: %v", err)
		}

		settings, err = remarks.Settings(ctx)
		if err != nil || settings.Moderated || !settings.Closed || settings.Notify != "mod@example.test" {
			t.Fatalf("saved settings: %+v %v", settings, err)
		}

		if settings, _ := guestbook.Settings(ctx); !settings.Moderated {
			t.Fatalf("other instance settings: %+v", settings)
		}
	})
}

// TestLatest pages through a guestbook, newest first, with replies.
func TestLatest(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "module:comments", schema()); err != nil {
			t.Fatal(err)
		}

		service := &Service{key: "guestbook", db: database}

		var first Comment

		for i := range 5 {
			entry := add(t, service, Entry{Subject: "guestbook", Name: string(rune('A' + i)), Body: "Hi", Approved: true})
			if i == 0 {
				first = entry
			}
		}

		add(t, service, Entry{Subject: "guestbook", Name: "Hidden", Body: "Spam"})

		if _, err := service.Reply(ctx, first.ID, "The site", "Welcome"); err != nil {
			t.Fatal(err)
		}

		page, more, err := service.Latest(ctx, 0, 3)
		if err != nil || !more || names(page) != "E|D|C" {
			t.Fatalf("page 1: %q %v %v", names(page), more, err)
		}

		page, more, err = service.Latest(ctx, 3, 3)
		if err != nil || more || names(page) != "B|A" || names(page[1].Replies) != "The site" {
			t.Fatalf("page 2: %+v %v %v", page, more, err)
		}

		// No limit returns everything after offset.
		page, more, err = service.Latest(ctx, 1, 0)
		if err != nil || more || names(page) != "D|C|B|A" || names(page[3].Replies) != "The site" {
			t.Fatalf("no limit: %q %v %v", names(page), more, err)
		}

		// An import keeps the original date of a message and its reply.
		written := time.Date(2019, 5, 4, 12, 30, 0, 0, time.UTC)
		old := add(t, service, Entry{Subject: "guestbook", Name: "Old", Body: "Hello", Approved: true, CreatedAt: written})
		reply := add(t, service, Entry{
			Subject:   "guestbook",
			ParentID:  old.ID,
			Name:      "The site",
			Body:      "Thanks",
			Approved:  true,
			Staff:     true,
			CreatedAt: written.Add(time.Hour),
		})

		if !old.CreatedAt.Equal(written) || !reply.CreatedAt.Equal(written.Add(time.Hour)) {
			t.Fatalf("import dates: %v %v", old.CreatedAt, reply.CreatedAt)
		}

		page, _, err = service.Latest(ctx, 0, 0)
		if err != nil || names(page) != "E|D|C|B|A|Old" || !page[5].CreatedAt.Equal(written) ||
			names(page[5].Replies) != "The site" {
			t.Fatalf("imported: %q %v", names(page), err)
		}
	})
}

func add(t *testing.T, service *Service, entry Entry) Comment {
	t.Helper()

	if entry.Locale == "" {
		entry.Locale = "en"
	}

	comment, err := service.Add(context.Background(), entry)
	if err != nil {
		t.Fatal(err)
	}

	return comment
}

func expectNames(t *testing.T, service *Service, subject string, want ...string) {
	t.Helper()

	list, err := service.List(context.Background(), subject)
	if err != nil {
		t.Fatal(err)
	}

	if names(list) != strings.Join(want, "|") {
		t.Fatalf("%s: want %q, got %q", subject, want, names(list))
	}
}

func expectCount(t *testing.T, service *Service, subject string, want int) {
	t.Helper()

	count, err := service.Count(context.Background(), subject)
	if err != nil || count != want {
		t.Fatalf("%s: want %d comments, got %d %v", subject, want, count, err)
	}
}

func names(list []Comment) string {
	var out []string

	for _, comment := range list {
		out = append(out, comment.Name)
	}

	return strings.Join(out, "|")
}

// TestTrash records who wrote and changed messages, and moves them to the
// trash with their replies, back, and out for good, on every dialect.
func TestTrash(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "module:comments", schema()); err != nil {
			t.Fatal(err)
		}

		moderator := db.NewID()
		staff := db.WithActor(ctx, moderator)
		remarks := &Service{key: "remarks", db: database}
		bin := remarks.trash()

		// Visitors have no actor; staff replies and approvals record the
		// moderator.
		ada := add(t, remarks, Entry{Subject: "post-1", Name: "Ada", Email: "ada@example.test", Body: "Hello there"})
		expectAudit(t, database, ada.ID, "", "")

		if err := remarks.Approve(staff, ada.ID, true); err != nil {
			t.Fatal(err)
		}

		expectAudit(t, database, ada.ID, "", moderator)

		answer, err := remarks.Reply(staff, ada.ID, "The site", "Welcome")
		if err != nil {
			t.Fatal(err)
		}

		expectAudit(t, database, answer.ID, moderator, moderator)

		eve := add(t, remarks, Entry{Subject: "post-1", ParentID: ada.ID, Name: "Eve", Body: "Spam reply", Approved: true})
		bob := add(t, remarks, Entry{Subject: "post-1", Name: "Bob", Body: "Second", Approved: true})
		add(t, remarks, Entry{Subject: "post-1", Name: "Cy", Body: "Waiting"})

		if err := remarks.SaveSettings(staff, Settings{Moderated: true}); err != nil {
			t.Fatal(err)
		}

		var settingsBy db.Ref
		if err := database.Get(
			ctx,
			&settingsBy,
			`SELECT updated_by FROM wt_comment_setting WHERE instance=?`,
			"remarks",
		); err != nil ||
			settingsBy.String() != moderator {
			t.Fatalf("settings updated_by %q: %v", settingsBy, err)
		}

		// A reply deleted on its own stays in the trash when its message
		// leaves and comes back later.
		if err := remarks.Delete(staff, eve.ID); err != nil {
			t.Fatal(err)
		}

		if err := remarks.Delete(staff, ada.ID); err != nil {
			t.Fatal(err)
		}

		if err := remarks.Delete(staff, ada.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("delete twice: %v", err)
		}

		// Deleted messages leave every list and count.
		expectNames(t, remarks, "post-1", "Bob")
		expectCount(t, remarks, "post-1", 1)

		if latest, _, err := remarks.Latest(ctx, 0, 0); err != nil || names(latest) != "Bob" {
			t.Fatalf("latest: %q %v", names(latest), err)
		}

		if pending, approved, err := remarks.counts(ctx); err != nil || pending != 1 || approved != 1 {
			t.Fatalf("counts: %d %d %v", pending, approved, err)
		}

		if pending, err := remarks.Pending(ctx); err != nil || pending != 1 {
			t.Fatalf("pending: %d %v", pending, err)
		}

		if records, _, err := remarks.review(ctx, filterAll, 0, 10); err != nil || len(records) != 2 {
			t.Fatalf("review: %+v %v", records, err)
		}

		if err := remarks.Approve(staff, ada.ID, false); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("approve deleted: %v", err)
		}

		if _, err := remarks.Reply(staff, ada.ID, "The site", "Late"); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("reply to deleted: %v", err)
		}

		late := Entry{Subject: "post-1", ParentID: ada.ID, Name: "Fay", Body: "Late", Locale: "en"}
		if _, err := remarks.Add(ctx, late); !errors.Is(err, ErrInvalid) {
			t.Fatalf("visitor reply to deleted: %v", err)
		}

		// The trash lists the message and the reply deleted on its own, not
		// the reply that went along.
		items, total, err := bin.List(ctx, "", 10, 0)
		if err != nil || total != 2 || len(items) != 2 {
			t.Fatalf("trash: %+v %d %v", items, total, err)
		}

		if items[0].ID != ada.ID || items[0].Title != "Ada: Hello there" || items[0].Kind != "" ||
			items[0].DeletedBy != moderator || items[0].DeletedAt.IsZero() {
			t.Fatalf("trashed message: %+v", items[0])
		}

		if items[1].ID != eve.ID || items[1].Kind != "Reply to a message" {
			t.Fatalf("trashed reply: %+v", items[1])
		}

		if items, total, err := bin.List(ctx, "ada@example", 10, 0); err != nil || total != 1 || items[0].ID != ada.ID {
			t.Fatalf("search: %+v %d %v", items, total, err)
		}

		if items, total, err := bin.List(ctx, "", 1, 1); err != nil || total != 2 || len(items) != 1 || items[0].ID != eve.ID {
			t.Fatalf("page 2: %+v %d %v", items, total, err)
		}

		// A reply cannot come back while its message is in the trash.
		expectReason(t, bin.Restore(staff, []string{eve.ID}),
			"This reply cannot be restored while the message it answers is in the trash. Restore that message instead.")

		// Restoring the message brings back the reply deleted with it.
		restorer := db.NewID()
		if err := bin.Restore(db.WithActor(ctx, restorer), []string{ada.ID}); err != nil {
			t.Fatal(err)
		}

		expectNames(t, remarks, "post-1", "Ada", "Bob")
		expectCount(t, remarks, "post-1", 3)
		expectAudit(t, database, ada.ID, "", restorer)

		list, err := remarks.List(ctx, "post-1")
		if err != nil || names(list[0].Replies) != "The site" {
			t.Fatalf("restored replies: %+v %v", list, err)
		}

		expectReason(t, bin.Restore(staff, []string{ada.ID}), "This item is no longer in the trash.")

		// Now the reply can come back too.
		if err := bin.Restore(staff, []string{eve.ID}); err != nil {
			t.Fatal(err)
		}

		expectCount(t, remarks, "post-1", 4)

		// Purging removes the message and its replies for good; live
		// messages cannot be purged.
		expectReason(t, bin.Purge(staff, []string{bob.ID}), "This item is no longer in the trash.")

		if err := remarks.Delete(staff, ada.ID); err != nil {
			t.Fatal(err)
		}

		if err := bin.Purge(staff, []string{ada.ID}); err != nil {
			t.Fatal(err)
		}

		var left int
		if err := database.Get(
			ctx,
			&left,
			`SELECT COUNT(*) FROM wt_comment WHERE id IN (?, ?, ?)`,
			ada.ID,
			answer.ID,
			eve.ID,
		); err != nil ||
			left != 0 {
			t.Fatalf("after purge: %d %v", left, err)
		}

		if _, total, err := bin.List(ctx, "", 10, 0); err != nil || total != 0 {
			t.Fatalf("empty trash: %d %v", total, err)
		}

		expectNames(t, remarks, "post-1", "Bob")
	})
}

// expectAudit checks who created and last changed a message; empty is NULL.
func expectAudit(t *testing.T, database *db.DB, id, createdBy, updatedBy string) {
	t.Helper()

	var audit struct {
		CreatedBy db.Ref    `db:"created_by"`
		UpdatedBy db.Ref    `db:"updated_by"`
		CreatedAt time.Time `db:"created_at"`
		UpdatedAt time.Time `db:"updated_at"`
	}

	err := database.Get(context.Background(), &audit,
		`SELECT created_by, updated_by, created_at, updated_at FROM wt_comment WHERE id=?`, id)
	if err != nil {
		t.Fatal(err)
	}

	if audit.CreatedBy.String() != createdBy || audit.UpdatedBy.String() != updatedBy ||
		audit.CreatedAt.IsZero() || audit.UpdatedAt.IsZero() {
		t.Fatalf("audit of %s: %+v, want created by %q, updated by %q", id, audit, createdBy, updatedBy)
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
