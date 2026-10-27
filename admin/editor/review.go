package editor

import (
	"context"
	"net/http"
	"strings"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/content"
)

// The review workflow is optional and off unless the site turns it on for
// a section (see content.WithReview). While it is on, editors submit a
// revision for review, reviewers approve or reject it, and only approved
// revisions are published or scheduled; the store refuses the others.

// reviews returns the store's review workflow while it is on for the
// section; ok is false otherwise.
func (e Editor[F, T, I]) reviews(ctx context.Context) (content.Reviewing[F, T], bool) {
	reviews, ok := e.config.Store.(content.Reviewing[F, T])
	if !ok || !reviews.ReviewRequired(ctx) {
		return nil, false
	}

	return reviews, true
}

// reviewing reports whether the section's review workflow is on.
func (e Editor[F, T, I]) reviewing(ctx context.Context) bool {
	_, ok := e.reviews(ctx)

	return ok
}

// submit asks for a review of the revision in the URL.
func (e Editor[F, T, I]) submit(w http.ResponseWriter, r *http.Request) {
	reviews, ok := e.reviews(r.Context())
	if !ok {
		http.NotFound(w, r)

		return
	}

	e.reviewNotice(w, r, func(string) error {
		if err := reviews.Submit(r.Context(), r.PathValue("id"), r.PathValue("revision")); err != nil {
			return err
		}

		e.notifyReviewers(r)

		return nil
	}, "Submitted for review.")
}

// notifyReviewers mails the section's reviewers about the revision in the
// URL, when the admin can send mail (see admin.Admin.NotifyReviewers).
func (e Editor[F, T, I]) notifyReviewers(r *http.Request) {
	config, err := e.config.Locales.Configuration(r.Context())
	if err != nil {
		return
	}

	item, err := e.load(r)
	if err != nil {
		return
	}

	title := e.dashboardLabel(item, config.Default)
	if title == "" {
		title = e.admin.T(r, "Untitled")
	}

	path := e.revisionURL(r.Context(), item.ID, config.Default, item.Revision.ID)
	e.admin.NotifyReviewers(r, e.permission(admin.Review).Section, title, path)
}

// approve approves the revision in the URL and, when asked by a reviewer
// who may publish, publishes it.
func (e Editor[F, T, I]) approve(w http.ResponseWriter, r *http.Request) {
	reviews, ok := e.reviews(r.Context())
	if !ok {
		http.NotFound(w, r)

		return
	}

	var in admin.ReviewInput

	if errs, err := e.admin.MapForm(r, &in); err != nil || len(errs) > 0 {
		e.admin.BadRequest(w, r)

		return
	}

	publish := in.Publish && e.admin.Can(r, e.permission(admin.Publish))

	notice := "Approved."
	if publish {
		notice = "Approved and published."
	}

	e.reviewNotice(w, r, func(required string) error {
		id := r.PathValue("id")
		revision := r.PathValue("revision")

		if err := reviews.Approve(r.Context(), id, revision, in.Note); err != nil {
			return err
		}

		if !publish {
			return nil
		}

		return e.config.Store.Publish(r.Context(), id, revision, required)
	}, notice)
}

// reject sends the revision in the URL back to its editors with a note.
func (e Editor[F, T, I]) reject(w http.ResponseWriter, r *http.Request) {
	reviews, ok := e.reviews(r.Context())
	if !ok {
		http.NotFound(w, r)

		return
	}

	var in admin.ReviewInput

	if errs, err := e.admin.MapForm(r, &in); err != nil || len(errs) > 0 {
		e.admin.BadRequest(w, r)

		return
	}

	if strings.TrimSpace(in.Note) == "" {
		e.refuse(w, r, content.ErrReviewState, e.admin.T(r, "Write why the revision is rejected."))

		return
	}

	e.reviewNotice(w, r, func(string) error {
		return reviews.Reject(r.Context(), r.PathValue("id"), r.PathValue("revision"), in.Note)
	}, "Rejected.")
}

// reviewNotice runs a review step and returns to the revision it was on.
func (e Editor[F, T, I]) reviewNotice(w http.ResponseWriter, r *http.Request, change func(required string) error, notice string) {
	locale, config, err := e.Locale(r)
	if err != nil {
		e.admin.Fail(w, r, err)

		return
	}

	if err := change(config.Default); err != nil {
		e.refuse(w, r, err, e.Issue(r, err))

		return
	}

	target := e.revisionURL(r.Context(), r.PathValue("id"), locale, r.PathValue("revision"))
	e.admin.RedirectNotice(w, r, target, notice)
}

// inReview lists the revisions waiting for review, for the dashboard (see
// admin.Section.Review); nothing while the workflow is off.
func (e Editor[F, T, I]) inReview(ctx context.Context, limit int) ([]admin.ReviewItem, error) {
	reviews, ok := e.reviews(ctx)
	if !ok {
		return nil, nil
	}

	config, err := e.config.Locales.Configuration(ctx)
	if err != nil {
		return nil, err
	}

	items, err := reviews.InReview(ctx, limit)
	if err != nil {
		return nil, err
	}

	waiting := make([]admin.ReviewItem, 0, len(items))

	for _, item := range items {
		link := e.revisionURL(ctx, item.ID, config.Default, item.Revision.ID)

		waiting = append(waiting, admin.ReviewItem{
			Title:       e.dashboardLabel(item, config.Default),
			URL:         link,
			SubmittedBy: item.Revision.Review.SubmittedBy,
			SubmittedAt: item.Revision.Review.SubmittedAt,
		})
	}

	return waiting, nil
}
