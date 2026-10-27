package content

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/donseba/go-webthings/db"
)

// Revisions can go through an optional review before they are published.
// Most sites never turn it on; while it is off nothing changes. While it is
// on for an instance (see WithReview), an editor submits a revision for
// review, a reviewer approves or rejects it with a short note, and only an
// approved revision can be published, by hand or by the scheduler. The
// state lives on the revision row, with who did what and when:
//
//	x_revision (..., review_state, submitted_at, submitted_by,
//	            reviewed_at, reviewed_by, review_note)
//
// The rest of a revision never changes. A new revision starts as a draft,
// so changing an approved text needs a new approval.

// ReviewState is where a revision is in the review workflow.
type ReviewState string

const (
	// ReviewDraft is a revision nobody asked to review; every revision
	// starts as one.
	ReviewDraft ReviewState = "draft"
	// ReviewPending is a revision submitted for review.
	ReviewPending ReviewState = "review"
	// ReviewApproved is a revision a reviewer approved; it can be published.
	ReviewApproved ReviewState = "approved"
	// ReviewRejected is a revision a reviewer sent back, with a note why.
	ReviewRejected ReviewState = "rejected"
)

// MaxReviewNote is the longest note a reviewer can leave, in characters.
const MaxReviewNote = 500

var (
	// ErrNotApproved refuses to publish or schedule a revision that is not
	// approved while the instance's review workflow is on.
	ErrNotApproved = errors.New("revision_not_approved")
	// ErrReviewState refuses a review step the revision is not ready for,
	// such as approving a revision nobody submitted, or a rejection
	// without a note.
	ErrReviewState = errors.New("invalid_review_state")
)

// Review is the review state of a revision. SubmittedBy and ReviewedBy are
// user IDs; empty, like the times, until that step happened.
type Review struct {
	State       ReviewState
	SubmittedBy string
	SubmittedAt time.Time
	ReviewedBy  string
	ReviewedAt  time.Time
	// Note is the reviewer's comment, required when rejecting.
	Note string
}

// ReviewPolicy reports whether publishing in an instance needs an approved
// revision. Sites usually decide it per admin section in their settings;
// the sections of revisioned content are named after their instance.
type ReviewPolicy func(ctx context.Context, instance string) bool

type reviewKey struct{}

// WithReview returns ctx in which publishing follows policy: the admin sets
// it for signed-in requests and the App's scheduler for its runs. Without a
// policy, as in seeds, imports and tests, no review is needed.
func WithReview(ctx context.Context, policy ReviewPolicy) context.Context {
	return context.WithValue(ctx, reviewKey{}, policy)
}

// ReviewRequired reports whether ctx's policy (see WithReview) requires an
// approved revision before publishing in instance.
func ReviewRequired(ctx context.Context, instance string) bool {
	policy, _ := ctx.Value(reviewKey{}).(ReviewPolicy)
	if policy == nil {
		return false
	}

	return policy(ctx, instance)
}

// approved refuses an item whose revision cannot be published under ctx's
// review policy.
func approved[F, T any](ctx context.Context, item Item[F, T]) error {
	if !ReviewRequired(ctx, item.Instance) || item.Revision.Review.State == ReviewApproved {
		return nil
	}

	return ErrNotApproved
}

// Reviewing is a store whose revisions can be reviewed; Revisions.In
// returns one.
type Reviewing[F, T any] interface {
	// ReviewRequired reports whether publishing needs an approved revision
	// under ctx's policy (see WithReview).
	ReviewRequired(ctx context.Context) bool
	// Submit asks for a review of a revision.
	Submit(ctx context.Context, id, revision string) error
	// Approve and Reject decide on a submitted revision.
	Approve(ctx context.Context, id, revision, note string) error
	Reject(ctx context.Context, id, revision, note string) error
	// InReview lists at most limit revisions waiting for review.
	InReview(ctx context.Context, limit int) ([]Item[F, T], error)
}

// Submit asks for a review of a draft or rejected revision; the user in ctx
// (see db.Actor) is the one who asked. An earlier revision of the item that
// still waits for review goes back to draft: the newest submission is the
// one to review. The published revision cannot be submitted.
func (r *Revisions[F, T]) Submit(ctx context.Context, instance, id, revisionID string) error {
	return r.db.Tx(ctx, func(ctx context.Context) error {
		item, err := r.Revision(ctx, instance, id, revisionID)
		if err != nil {
			return err
		}

		if item.IsPublishedRevision() {
			return ErrReviewState
		}

		_, err = r.db.Exec(ctx, `
			UPDATE `+r.spec.Table+`_revision
			SET review_state=?, submitted_at=NULL, submitted_by=NULL
			WHERE entity_id=? AND id<>? AND review_state=?`,
			ReviewDraft, id, revisionID, ReviewPending,
		)
		if err != nil {
			return err
		}

		at, by := db.Stamp(ctx)

		return r.review(ctx, revisionID, `
			review_state=?, submitted_at=?, submitted_by=?, reviewed_at=NULL, reviewed_by=NULL, review_note=''
			WHERE id=? AND review_state IN (?,?)`,
			ReviewPending, at, by, revisionID, ReviewDraft, ReviewRejected,
		)
	})
}

// Approve marks a submitted revision as approved by the user in ctx, with
// an optional note. Publishing is a separate step.
func (r *Revisions[F, T]) Approve(ctx context.Context, instance, id, revisionID, note string) error {
	return r.decide(ctx, instance, id, revisionID, ReviewApproved, note)
}

// Reject sends a submitted revision back; note says why and is required.
func (r *Revisions[F, T]) Reject(ctx context.Context, instance, id, revisionID, note string) error {
	if strings.TrimSpace(note) == "" {
		return ErrReviewState
	}

	return r.decide(ctx, instance, id, revisionID, ReviewRejected, note)
}

func (r *Revisions[F, T]) decide(ctx context.Context, instance, id, revisionID string, state ReviewState, note string) error {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > MaxReviewNote {
		return ErrInvalid
	}

	// Loading first refuses revisions of other items, instances and items
	// in the trash.
	if _, err := r.Revision(ctx, instance, id, revisionID); err != nil {
		return err
	}

	at, by := db.Stamp(ctx)

	return r.review(ctx, revisionID, `
		review_state=?, reviewed_at=?, reviewed_by=?, review_note=?
		WHERE id=? AND review_state=?`,
		state, at, by, note, revisionID, ReviewPending,
	)
}

// review runs an UPDATE of a revision's review columns; a revision that is
// not in the state the step needs is ErrReviewState.
func (r *Revisions[F, T]) review(ctx context.Context, revisionID, set string, args ...any) error {
	result, err := r.db.Exec(ctx, `UPDATE `+r.spec.Table+`_revision SET `+set, args...)

	err = db.Affected(result, err)
	if errors.Is(err, ErrNotFound) {
		// The revision exists (the caller loaded it): its state is wrong.
		return ErrReviewState
	}

	return err
}

// InReview loads at most limit revisions waiting for review, of live
// items, the longest waiting first.
func (r *Revisions[F, T]) InReview(ctx context.Context, instance string, limit int) ([]Item[F, T], error) {
	if limit <= 0 {
		return nil, nil
	}

	return r.list(ctx, instance, listing{
		where: `v.review_state=?`,
		args:  []any{ReviewPending},
		order: `v.submitted_at, v.id`,
		limit: limit,
	})
}

// Find loads a revision by its ID alone, for a preview that names only the
// revision; the item must be live and of instance.
func (r *Revisions[F, T]) Find(ctx context.Context, instance, revisionID string) (Item[F, T], error) {
	return r.one(ctx, instance, `v.id=?`, revisionID)
}

// PreviewBySlug is PublishedBySlug for an address an editor may preview
// (see frontend.PreviewParam): with a revision ID it returns that revision
// when it belongs to the content at slug, that is the published item with
// that slug or, when none is published there, an item whose revision has
// that slug in locale, such as one never published. For every other
// revision, and without one, it returns what PublishedBySlug returns, so a
// preview never shows other content at an address.
func (r *Revisions[F, T]) PreviewBySlug(ctx context.Context, instance, locale, slug, revisionID string) (Item[F, T], error) {
	published, err := r.PublishedBySlug(ctx, instance, locale, slug)
	if revisionID == "" || err != nil && !errors.Is(err, ErrNotFound) {
		return published, err
	}

	preview, findErr := r.Find(ctx, instance, revisionID)
	if findErr != nil {
		return published, err
	}

	text, ok := preview.Text(locale)

	switch {
	case !ok:
		return published, err
	case err == nil && published.ID == preview.ID:
		return preview, nil
	case err != nil && r.spec.slug(text) == slug:
		return preview, nil
	}

	return published, err
}

// reviewColumns are the review columns as list and History select them,
// after the revision's other columns.
const reviewColumns = `v.review_state, v.submitted_at, v.submitted_by, v.reviewed_at, v.reviewed_by, v.review_note`

// reviewScan scans reviewColumns.
type reviewScan struct {
	state       string
	submittedAt sql.NullTime
	submittedBy db.Ref
	reviewedAt  sql.NullTime
	reviewedBy  db.Ref
	note        string
}

func (s *reviewScan) pointers() []any {
	return []any{&s.state, &s.submittedAt, &s.submittedBy, &s.reviewedAt, &s.reviewedBy, &s.note}
}

func (s reviewScan) review() Review {
	return Review{
		State:       ReviewState(s.state),
		SubmittedBy: s.submittedBy.String(),
		SubmittedAt: s.submittedAt.Time,
		ReviewedBy:  s.reviewedBy.String(),
		ReviewedAt:  s.reviewedAt.Time,
		Note:        s.note,
	}
}
