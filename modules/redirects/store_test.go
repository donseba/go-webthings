package redirects

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
)

// TestStore runs validation, loops, hits, missed paths and imports on every
// configured dialect.
func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "redirects", schema()); err != nil {
			t.Fatal(err)
		}

		s := &Service{key: "redirects", db: database, host: "example.com"}
		other := &Service{key: "other", db: database}

		old, err := s.Save(ctx, Redirect{From: "https://old.example/old/?page=1", To: "/en/new", Status: 301})
		if err != nil {
			t.Fatal(err)
		}

		if old.From != "/old" || old.To != "/en/new" || old.Hits != 0 || old.CreatedAt.IsZero() {
			t.Fatalf("saved: %+v", old)
		}

		// The same path in another instance is a different redirect.
		if _, err := other.Save(ctx, Redirect{From: "/old", To: "/elsewhere", Status: 302}); err != nil {
			t.Fatal(err)
		}

		refused := []struct {
			redirect Redirect
			want     error
		}{
			{Redirect{From: "/old/", To: "/x", Status: 301}, errDuplicate},
			{Redirect{From: "/self", To: "/self/?a=1", Status: 301}, errLoop},
			{Redirect{From: "/self", To: "https://example.com/self", Status: 301}, errLoop},
			{Redirect{From: "/en/new", To: "/old", Status: 301}, errLoop},
			{Redirect{From: "/admin/pages", To: "/x", Status: 301}, errReserved},
			{Redirect{From: "/", To: "/x", Status: 301}, errReserved},
			{Redirect{From: "/media/a.png", To: "/x", Status: 301}, errReserved},
			{Redirect{From: "/theme/site.css", To: "/x", Status: 301}, errReserved},
			{Redirect{From: "/_/polls/vote", To: "/x", Status: 301}, errReserved},
			{Redirect{From: "/robots.txt", To: "/x", Status: 301}, errReserved},
			{Redirect{From: "mailto:a@example.com", To: "/x", Status: 301}, errFrom},
			{Redirect{From: "/a", To: "javascript:alert(1)", Status: 301}, errTo},
			{Redirect{From: "/a", To: "//evil.example", Status: 301}, errTo},
			{Redirect{From: "/a", To: "/x", Status: 303}, errStatus},
		}

		for _, tc := range refused {
			if _, err := s.Save(ctx, tc.redirect); !errors.Is(err, tc.want) {
				t.Fatalf("%+v: want %v, got %v", tc.redirect, tc.want, err)
			}
		}

		// A chain through another redirect is allowed; a loop through it is not.
		chained, err := s.Save(ctx, Redirect{From: "older.html", To: "/old", Status: 308})
		if err != nil || chained.From != "/older.html" {
			t.Fatalf("chain: %+v %v", chained, err)
		}

		old.To = "/older.html"
		if _, err := s.Save(ctx, old); !errors.Is(err, errLoop) {
			t.Fatalf("loop through a chain: %v", err)
		}

		steps := s.Trace(ctx, "/older.html/")
		if len(steps) != 2 || steps[0].ID != chained.ID || steps[1].To != "/en/new" {
			t.Fatalf("trace: %+v", steps)
		}

		// Hits and misses are counted in memory and written by Flush.
		found, ok := s.Resolve(ctx, "/old/")
		if !ok || found.ID != old.ID {
			t.Fatalf("resolve: %+v %v", found, ok)
		}

		s.hit(old.ID)
		s.hit(old.ID)
		s.miss("/gone/")
		s.miss("/gone")
		s.miss("/missing")
		s.miss("/media/missing.png")

		if err := s.Flush(ctx); err != nil {
			t.Fatal(err)
		}

		s.miss("/gone")

		if err := s.Flush(ctx); err != nil {
			t.Fatal(err)
		}

		saved, err := s.Get(ctx, old.ID)
		if err != nil || saved.Hits != 2 || saved.LastHit == nil {
			t.Fatalf("hits: %+v %v", saved, err)
		}

		expectMissed(t, s, "/gone:3", "/missing:1")

		if err := s.pruneMisses(ctx, 1); err != nil {
			t.Fatal(err)
		}

		expectMissed(t, s, "/gone:3")

		// A redirect for a missed path removes it from the list.
		if _, err := s.Save(ctx, Redirect{From: "/gone", To: "https://example.org/", Status: 302}); err != nil {
			t.Fatal(err)
		}

		expectMissed(t, s)

		// An import with a wrong line saves nothing.
		_, err = s.Import(ctx, strings.NewReader("from;to;status\n/a;/en/a\n/b;/en/b;303\n/a;/en/c\n/c;/c\n"))

		var problems *ImportError
		if !errors.As(err, &problems) || len(problems.Lines) != 3 {
			t.Fatalf("import errors: %v", err)
		}

		if problems.Lines[0].Line != 3 || !errors.Is(problems.Lines[0].Err, errStatus) ||
			!errors.Is(problems.Lines[1].Err, errRepeated) || !errors.Is(problems.Lines[2].Err, errLoop) {
			t.Fatalf("import errors: %v", problems)
		}

		if _, ok := s.Resolve(ctx, "/a"); ok {
			t.Fatal("refused import saved a redirect")
		}

		_, err = s.Import(ctx, strings.NewReader("/a,/en/a\n/b,\"/en/b\n"))
		if !errors.As(err, &problems) || problems.Lines[0].Line != 2 {
			t.Fatalf("malformed CSV: %v", err)
		}

		// A valid import adds new redirects and replaces existing ones.
		count, err := s.Import(ctx, strings.NewReader("\ufeff/a,/en/a\n\n/old,/en/newer,302\n"))
		if err != nil || count != 2 {
			t.Fatalf("import: %d %v", count, err)
		}

		replaced, _ := s.Resolve(ctx, "/old")
		if replaced.ID != old.ID || replaced.To != "/en/newer" || replaced.Status != 302 || replaced.Hits != 2 {
			t.Fatalf("replaced: %+v", replaced)
		}

		if err := s.Delete(ctx, old.ID); err != nil {
			t.Fatal(err)
		}

		if _, ok := s.Resolve(ctx, "/old"); ok {
			t.Fatal("deleted redirect still resolves")
		}

		if _, ok := other.Resolve(ctx, "/old"); !ok {
			t.Fatal("other instance lost its redirect")
		}

		// Requests, admin changes and writes may happen at the same time.
		a, _ := s.Resolve(ctx, "/a")

		var group sync.WaitGroup

		for range 8 {
			group.Go(func() {
				for range 50 {
					if _, ok := s.Resolve(ctx, "/a/"); ok {
						s.hit(a.ID)
					}

					s.miss("/busy")
				}
			})
		}

		group.Go(func() {
			for range 5 {
				s.reload(ctx)

				if err := s.Flush(ctx); err != nil {
					t.Error(err)
				}
			}
		})

		group.Wait()

		if err := s.Flush(ctx); err != nil {
			t.Fatal(err)
		}

		if a, _ = s.Get(ctx, a.ID); a.Hits != 400 {
			t.Fatalf("concurrent hits: %d", a.Hits)
		}
	})
}

// TestImportSize checks that an import over 1 MiB is refused whole rather
// than cut, which would import the lines before the cut only.
func TestImportSize(t *testing.T) {
	var source strings.Builder

	for i := range 4000 {
		source.WriteString("/page-" + strconv.Itoa(i) + ",https://example.org/" + strings.Repeat("a", 280) + "\n")
	}

	if source.Len() <= maxImportSize {
		t.Fatalf("the test import has %d bytes only", source.Len())
	}

	if _, err := readCSV(strings.NewReader(source.String())); !errors.Is(err, errTooLarge) {
		t.Fatalf("an import over 1 MiB: %v", err)
	}

	records, err := readCSV(strings.NewReader(source.String()[:maxImportSize]))
	if err != nil || len(records) == 0 {
		t.Fatalf("an import of 1 MiB: %d lines, %v", len(records), err)
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"/old":                       "/old",
		"/old/":                      "/old",
		"old.html":                   "/old.html",
		"https://old.example/a/b/?q": "/a/b",
		"/a//b/./c/../d#top":         "/a/b/d",
		"/caf%C3%A9":                 "/café",
		"/Case":                      "/Case",
	}

	for in, want := range cases {
		got, err := Normalize(in)
		if err != nil || got != want {
			t.Fatalf("%q: want %q, got %q %v", in, want, got, err)
		}
	}

	for _, in := range []string{"", "ftp://example.com/a", "/" + strings.Repeat("a", 500)} {
		if _, err := Normalize(in); err == nil {
			t.Fatalf("%q accepted", in)
		}
	}

	if got := target("/en/new", "utm_source=x"); got != "/en/new?utm_source=x" {
		t.Fatalf("query kept: %q", got)
	}

	if got := target("/en/new?a=1", "b=2"); got != "/en/new?a=1" {
		t.Fatalf("own query: %q", got)
	}
}

// TestNoindex checks which "not found" answers are left out of the missed
// paths.
func TestNoindex(t *testing.T) {
	for value, want := range map[string]bool{
		"":                   false,
		"nofollow":           false,
		"noindex":            true,
		"NoIndex, nofollow":  true,
		"none":               true,
		"googlebot: noindex": true,
		"noimageindex":       false,
	} {
		header := http.Header{}
		if value != "" {
			header.Set("X-Robots-Tag", value)
		}

		if got := noindex(header); got != want {
			t.Errorf("X-Robots-Tag %q: want %v, got %v", value, want, got)
		}
	}
}

func expectMissed(t *testing.T, s *Service, want ...string) {
	t.Helper()

	list, err := s.Missed(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}

	var got []string

	for _, missed := range list {
		got = append(got, missed.Path+":"+strconv.FormatInt(missed.Hits, 10))
	}

	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("missed: want %q, got %q", want, got)
	}
}

// TestTrash moves redirects to the trash and back on every configured
// dialect: deleted redirects leave their path free, stop redirecting and
// come back only while nothing else uses the path.
func TestTrash(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		if err := database.ApplySchema(context.Background(), "redirects", schema()); err != nil {
			t.Fatal(err)
		}

		editor := db.NewID()
		ctx := db.WithActor(context.Background(), editor)
		s := &Service{key: "redirects", db: database, host: "example.com"}
		bin := s.trash()

		old, err := s.Save(ctx, Redirect{From: "/old", To: "/en/new", Status: 301})
		if err != nil {
			t.Fatal(err)
		}

		if old.CreatedBy.String() != editor || old.UpdatedBy.String() != editor || old.CreatedAt.IsZero() ||
			old.UpdatedAt.IsZero() || old.DeletedAt != nil {
			t.Fatalf("audit columns: %+v", old)
		}

		// Changes by another editor are recorded; hits by visitors are not.
		other := db.NewID()

		old.Status = 302
		if _, err := s.Save(db.WithActor(ctx, other), old); err != nil {
			t.Fatal(err)
		}

		s.hit(old.ID)

		if err := s.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}

		changed, err := s.Get(ctx, old.ID)
		if err != nil || changed.UpdatedBy.String() != other || changed.CreatedBy.String() != editor || changed.Hits != 1 {
			t.Fatalf("changed: %+v %v", changed, err)
		}

		s.miss("/old-missed")

		if err := s.Flush(ctx); err != nil {
			t.Fatal(err)
		}

		// A deleted redirect stops redirecting and leaves every list.
		if err := s.Delete(ctx, old.ID); err != nil {
			t.Fatal(err)
		}

		if err := s.Delete(ctx, old.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("delete twice: %v", err)
		}

		if _, ok := s.Resolve(ctx, "/old"); ok {
			t.Fatal("deleted redirect resolves")
		}

		if _, err := s.Get(ctx, old.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("get deleted: %v", err)
		}

		if list, err := s.List(ctx); err != nil || len(list) != 0 {
			t.Fatalf("list: %+v %v", list, err)
		}

		// Editing a deleted redirect finds nothing to change.
		if _, err := s.Save(ctx, changed); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("save deleted: %v", err)
		}

		trashed, _, err := s.Trashed(ctx, 0, 0)
		if err != nil || len(trashed) != 1 || trashed[0].DeletedAt == nil || trashed[0].DeletedBy.String() != editor {
			t.Fatalf("trashed: %+v %v", trashed, err)
		}

		// Its path is free for a new redirect, also from an import, and a
		// missed path of a deleted redirect is listed again.
		s.miss("/old")

		if err := s.Flush(ctx); err != nil {
			t.Fatal(err)
		}

		expectMissed(t, s, "/old:1", "/old-missed:1")

		if _, err := s.Import(ctx, strings.NewReader("/old,/en/newer\n")); err != nil {
			t.Fatal(err)
		}

		taken, ok := s.Resolve(ctx, "/old")
		if !ok || taken.ID == old.ID || taken.To != "/en/newer" {
			t.Fatalf("new redirect for the path: %+v %v", taken, ok)
		}

		// Restoring it now is a conflict that keeps it in the trash.
		err = bin.Restore(ctx, []string{old.ID})

		var refused *admin.TrashError
		if !errors.As(err, &refused) || refused.ID != old.ID ||
			refused.Reason != "This redirect cannot be restored: another redirect uses its old path now." {
			t.Fatalf("restore over a taken path: %v", err)
		}

		if err := s.Delete(ctx, taken.ID); err != nil {
			t.Fatal(err)
		}

		// A redirect made meanwhile can turn it into a loop.
		back, err := s.Save(ctx, Redirect{From: "/en/new", To: "/old", Status: 301})
		if err != nil {
			t.Fatal(err)
		}

		err = bin.Restore(ctx, []string{old.ID})
		if !errors.As(err, &refused) || !errors.Is(err, errLoop) {
			t.Fatalf("restore into a loop: %v", err)
		}

		if err := s.Delete(ctx, back.ID); err != nil {
			t.Fatal(err)
		}

		// The section's trash lists the deleted redirects, newest first,
		// searches and pages.
		items, total, err := bin.List(ctx, "", 10, 0)
		if err != nil || total != 3 || len(items) != 3 || items[0].ID != back.ID || items[2].ID != old.ID ||
			items[2].Title != "/old → /en/new" || items[2].DeletedBy != editor || items[2].DeletedAt.IsZero() {
			t.Fatalf("trash: %d %+v %v", total, items, err)
		}

		if items, total, err = bin.List(ctx, "newer", 10, 0); err != nil || total != 1 || items[0].ID != taken.ID {
			t.Fatalf("search: %d %+v %v", total, items, err)
		}

		if items, total, err = bin.List(ctx, "", 1, 1); err != nil || total != 3 || len(items) != 1 || items[0].ID != taken.ID {
			t.Fatalf("page: %d %+v %v", total, items, err)
		}

		// Restored, it redirects again, and its path is no longer missed.
		restorer := db.NewID()

		if err := bin.Restore(db.WithActor(ctx, restorer), []string{old.ID}); err != nil {
			t.Fatal(err)
		}

		restored, ok := s.Resolve(ctx, "/old")
		if !ok || restored.ID != old.ID || restored.Hits != 1 || restored.UpdatedBy.String() != restorer ||
			restored.DeletedAt != nil || restored.DeletedBy != "" {
			t.Fatalf("restored: %+v %v", restored, ok)
		}

		expectMissed(t, s, "/old-missed:1")

		err = bin.Restore(ctx, []string{old.ID})
		if !errors.As(err, &refused) || refused.Reason != "This item is no longer in the trash." {
			t.Fatalf("restore twice: %v", err)
		}

		// Purging removes only redirects in the trash.
		if err := bin.Purge(ctx, []string{old.ID}); !errors.As(err, &refused) {
			t.Fatalf("purge a live redirect: %v", err)
		}

		if err := bin.Purge(ctx, []string{taken.ID, back.ID}); err != nil {
			t.Fatal(err)
		}

		if _, total, err := bin.List(ctx, "", 10, 0); err != nil || total != 0 {
			t.Fatalf("purged: %d %v", total, err)
		}

		if _, ok := s.Resolve(ctx, "/old"); !ok {
			t.Fatal("purge removed a live redirect")
		}
	})
}

// TestRedirect checks the redirects the page editor makes (links.Redirector):
// a new one, one that replaces the redirect of its path, one whose target
// had a redirect, which would hide the page there now, and a loop and a
// reserved path, which are refused.
func TestRedirect(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "redirects", schema()); err != nil {
			t.Fatal(err)
		}

		s := &Service{key: "redirects", db: database, host: "example.com"}

		if err := s.Redirect(ctx, "/en/about", "/en/about-us"); err != nil {
			t.Fatal(err)
		}

		if to, ok := s.Redirected(ctx, "/en/about/"); !ok || to != "/en/about-us" {
			t.Fatalf("redirected to %q, %v", to, ok)
		}

		// The page moves on: the redirect of its old path follows.
		if err := s.Redirect(ctx, "/en/about", "/en/company"); err != nil {
			t.Fatal(err)
		}

		// Back to its first address, which redirected away: that redirect
		// goes to the trash.
		if err := s.Redirect(ctx, "/en/company", "/en/about"); err != nil {
			t.Fatal(err)
		}

		if _, ok := s.Redirected(ctx, "/en/about"); ok {
			t.Fatal("the redirect of the page's address stays")
		}

		list, err := s.List(ctx)
		if err != nil || len(list) != 1 || list[0].From != "/en/company" || list[0].To != "/en/about" || list[0].Status != 301 {
			t.Fatalf("redirects: %+v %v", list, err)
		}

		if _, total, err := s.Trashed(ctx, 0, 0); err != nil || total != 1 {
			t.Fatalf("trash: %d %v", total, err)
		}

		if err := s.Redirect(ctx, "/en/x", "/en/x/"); !errors.Is(err, errLoop) {
			t.Fatalf("loop: %v", err)
		}

		if err := s.Redirect(ctx, "/", "/en/about"); !errors.Is(err, errReserved) {
			t.Fatalf("home: %v", err)
		}
	})
}
