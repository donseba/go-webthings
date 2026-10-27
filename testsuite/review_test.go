package testsuite

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

// TestReviewWorkflow follows a post through the optional review: off by
// default, then on for News only.
func TestReviewWorkflow(t *testing.T) {
	t.Parallel()

	options := sitetest.Options{
		StartingPoints: []string{"news"},
		// The test runs the scheduler itself.
		Configure: func(config *webthings.Config) {
			config.ScheduleInterval = -1
		},
	}
	admin := sitetest.StartWith(t, options, publishing.Module("news", "News"))
	ctx := context.Background()
	posts := publishing.Of(admin.App, "news").Store()

	admin.Mount("news", "News", "news")

	// The matrix offers the Review right in revisioned sections only.
	group := admin.Submit("/admin/groups", url.Values{"Name": {"Empty"}})
	admin.Expect(group, http.StatusOK, `value="news:review"`, `value="pages:review"`)
	admin.ExpectMissing(group, `value="media:review"`)

	// Off by default: editors publish their drafts as before.
	first := admin.Submit("/admin/news/new?locale=en", url.Values{"Title": {"First"}, "Slug": {"first"}, "Date": {"2026-10-01"}})
	admin.ExpectMissing(first+"?locale=en", "Submit for review")
	admin.Publish(first + "?locale=en")

	// A publication planned before the workflow is turned on.
	planned := admin.Submit(
		"/admin/news/new?locale=en",
		url.Values{"Title": {"Planned"}, "Slug": {"planned"}, "Date": {"2026-10-01"}},
	)

	plannedDraft, err := posts.Load(ctx, lastID(planned), "")
	if err != nil {
		t.Fatal(err)
	}

	plan := content.Schedule{RevisionID: plannedDraft.Revision.ID, PublishAt: time.Now().Add(-time.Minute)}
	if err := posts.(content.Scheduling).Schedule(ctx, plannedDraft.ID, plan, "en"); err != nil {
		t.Fatal(err)
	}

	// Turn it on for News.
	admin.Expect("/admin/site", http.StatusOK, "Sections that need a review", `value="news"`, `value="pages"`)
	admin.Post("/admin/site", "/admin/site/review?locale=en", url.Values{"Sections": {"news"}})

	settings, err := admin.App.Site.Settings(ctx)
	if err != nil || !settings.Reviewed("news") || settings.Reviewed("pages") {
		t.Fatalf("review setting: %v %v", settings.Review, err)
	}

	// The scheduler respects it: the plan is dropped, the post stays a draft.
	if changes, err := admin.App.RunSchedules(ctx); changes != 0 || !errors.Is(err, content.ErrNotApproved) {
		t.Fatalf("scheduled publication of a draft: %d %v", changes, err)
	}

	admin.Visitor().Expect("/en/news/planned", http.StatusNotFound)

	writer := signIn(t, admin, "writer@example.test", "Writers", "news:read", "news:create", "news:edit", "news:publish")
	reviewer := signIn(t, admin, "reviewer@example.test", "Reviewers", "news:read", "news:review")

	post := writer.Submit(
		"/admin/news/new?locale=en",
		url.Values{"Title": {"Second"}, "Slug": {"second"}, "Date": {"2026-10-02"}},
	)
	editor := post + "?locale=en"

	draft, err := posts.Load(ctx, lastID(post), "")
	if err != nil {
		t.Fatal(err)
	}

	revision := post + "/revisions/" + draft.Revision.ID

	// A draft cannot be published or scheduled, even by an editor who may
	// publish.
	writer.Expect(editor, http.StatusOK, "Submit for review", "once a reviewer approves it")
	writer.ExpectMissing(editor, `/revisions/`+draft.Revision.ID+`/publish`)

	status, body := writer.PostStatus(editor, revision+"/publish?locale=en", url.Values{})
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "approval") {
		t.Fatalf("publishing a draft: %d", status)
	}

	status, _ = writer.PostStatus(editor, revision+"/schedule?locale=en", url.Values{
		"PublishAt": {time.Now().Add(time.Hour).Format("2006-01-02T15:04")},
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("scheduling a draft: %d", status)
	}

	// Reviewers may not submit or approve what they cannot see in review.
	if status, _ := reviewer.PostStatus(
		editor,
		revision+"/approve?locale=en",
		url.Values{},
	); status != http.StatusUnprocessableEntity {
		t.Fatalf("approving a draft: %d", status)
	}

	// The writer submits; the reviewers, the administrator among them, get
	// an e-mail.
	writer.Post(editor, revision+"/submit?locale=en", url.Values{})
	writer.Expect(editor, http.StatusOK, "In review")

	mails := admin.Mails(2)
	recipients := map[string]bool{}

	for _, message := range mails {
		if strings.Contains(message.Subject, "Waiting for your review: Second") {
			recipients[message.To[0]] = true
		}
	}

	if !recipients["reviewer@example.test"] || !recipients["admin@example.test"] || recipients["writer@example.test"] {
		t.Fatalf("review requests went to %v", recipients)
	}

	// The dashboard lists the submitted revision, linked to it, with who
	// submitted it.
	dashboard := admin.Dashboard()

	link := `<a href="` + post + `?locale=en&amp;revision=` + draft.Revision.ID + `">Second</a>`
	if !strings.Contains(dashboard, link) || !strings.Contains(dashboard, "News · Waiting for review · by writer@example.test") {
		t.Fatalf("dashboard lacks the revision in review:\n%s", dashboard)
	}

	// A rejection needs a note, and the writer sees it.
	reviewer.Expect(editor, http.StatusOK, "Review this revision", "Approve", "Reject")

	if status, _ := reviewer.PostStatus(
		editor,
		revision+"/reject?locale=en",
		url.Values{"Note": {" "}},
	); status != http.StatusUnprocessableEntity {
		t.Fatalf("rejecting without a note: %d", status)
	}

	reviewer.Post(editor, revision+"/reject?locale=en", url.Values{"Note": {"Add a photo."}})
	writer.Expect(editor, http.StatusOK, "Rejected", "Add a photo.", "Submit for review")

	// Submitted again and approved by a reviewer who may not publish: the
	// approval does not publish, also when asked to.
	writer.Post(editor, revision+"/submit?locale=en", url.Values{})
	reviewer.Post(editor, revision+"/approve?locale=en", url.Values{"Note": {"Fine."}, "Publish": {"true"}})
	admin.Visitor().Expect("/en/news/second", http.StatusNotFound)

	// Reviewed revisions leave the dashboard's list.
	if strings.Contains(admin.Dashboard(), "Waiting for review") {
		t.Fatal("the dashboard still lists an approved revision")
	}

	// Now the writer can publish it.
	writer.Expect(editor, http.StatusOK, "Approved")
	writer.Publish(editor)
	admin.Visitor().Expect("/en/news/second", http.StatusOK, "Second")

	// An administrator reviews and publishes in one step.
	third := writer.Submit("/admin/news/new?locale=en", url.Values{"Title": {"Third"}, "Slug": {"third"}, "Date": {"2026-10-03"}})

	thirdDraft, err := posts.Load(ctx, lastID(third), "")
	if err != nil {
		t.Fatal(err)
	}

	thirdRevision := third + "/revisions/" + thirdDraft.Revision.ID
	writer.Post(third+"?locale=en", thirdRevision+"/submit?locale=en", url.Values{})
	admin.Expect(third+"?locale=en", http.StatusOK, "Publish this revision now")
	admin.Post(third+"?locale=en", thirdRevision+"/approve?locale=en", url.Values{"Publish": {"true"}})
	admin.Visitor().Expect("/en/news/third", http.StatusOK, "Third")

	// Pages are not reviewed: they publish as before.
	home, err := admin.App.Pages.Home(ctx)
	if err != nil {
		t.Fatal(err)
	}

	about := admin.Submit("/admin/pages/new?locale=en", url.Values{"Title": {"About"}, "Slug": {"about"}, "Parent": {home}})
	admin.ExpectMissing(about+"?locale=en", "Submit for review")
	admin.Publish(about + "?locale=en")
	admin.Visitor().Expect("/en/about", http.StatusOK, "About")
}
