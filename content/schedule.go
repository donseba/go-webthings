package content

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/donseba/go-webthings/db"
)

// Editors can plan the publication of a revision and the unpublication of
// an item; the App's scheduler carries the plans out (see Scheduler). The
// plan is kept on the item row:
//
//	x (..., scheduled_revision_id, publish_at, unpublish_at)
//
// Times are stored in UTC. Publishing or unpublishing by hand cancels the
// plan. Each due change is first claimed with a
// conditional UPDATE that clears it, in the transaction that makes the
// change, so when several processes run the scheduler only one of them
// publishes. A change that fails (for example because a translation became
// incomplete) is dropped rather than retried forever, and reported.

// ErrSchedule is returned for a schedule that cannot be carried out.
var ErrSchedule = errors.New("invalid_schedule")

// Schedule is the planned publication change of one item.
type Schedule struct {
	// RevisionID is published at PublishAt; both are empty when only
	// unpublishing is planned.
	RevisionID string
	PublishAt  time.Time
	// UnpublishAt takes the item offline; zero when not planned.
	UnpublishAt time.Time
}

// IsZero reports whether nothing is planned.
func (s Schedule) IsZero() bool {
	return s.PublishAt.IsZero() && s.UnpublishAt.IsZero()
}

// Scheduler carries out planned publication changes.
type Scheduler interface {
	// RunDue publishes and unpublishes what is due at now and returns how many
	// changes it made. Published revisions must be complete in required.
	RunDue(ctx context.Context, now time.Time, required string) (int, error)
}

// Scheduling is a store whose publication can be planned; Revisions.In
// returns one.
type Scheduling interface {
	Scheduler
	// Schedule replaces the plan of an item. required is the locale a
	// published revision must be complete in.
	Schedule(ctx context.Context, id string, schedule Schedule, required string) error
	// Unschedule cancels the plan of an item.
	Unschedule(ctx context.Context, id string) error
}

// Publisher makes the scheduled changes of RunDue. Owners whose publication
// has side effects, such as the page tree's routes, give their own; nil
// functions use the store's Publish and Unpublish.
//
// The functions must not clear the item's plan themselves: RunDue already
// cleared the step it carries out, and the rest (an unpublication after a
// publication) must stay. Calling the store's Publish and Unpublish with the
// ctx they receive is safe: it carries a marker that keeps them from
// cancelling the plan as they do when an editor publishes by hand.
type Publisher struct {
	// Publish publishes revisionID of item id, which must be complete in
	// the required language.
	Publish func(ctx context.Context, id, revisionID, required string) error
	// Unpublish takes item id off the website.
	Unpublish func(ctx context.Context, id string) error
}

// Schedule plans the publication of a revision and/or the unpublication of
// an item. The revision must belong to the item, be complete in required
// and, while the review workflow is on, approved (see WithReview);
// unpublishing must come after publishing. Times in the past are carried out
// on the scheduler's next run.
func (r *Revisions[F, T]) Schedule(ctx context.Context, instance, id string, schedule Schedule, required string) error {
	if schedule.IsZero() || schedule.PublishAt.IsZero() != (schedule.RevisionID == "") {
		return ErrSchedule
	}

	if !schedule.PublishAt.IsZero() && !schedule.UnpublishAt.IsZero() && !schedule.UnpublishAt.After(schedule.PublishAt) {
		return ErrSchedule
	}

	if schedule.RevisionID != "" {
		item, err := r.Revision(ctx, instance, id, schedule.RevisionID)
		if err != nil {
			return err
		}

		if required != "" && !r.spec.Available(item, required) {
			return fmt.Errorf("%w: %s", ErrIncomplete, required)
		}

		// The scheduler checks again when the time comes, in case the
		// review workflow was turned on meanwhile.
		if err := approved(ctx, item); err != nil {
			return err
		}
	}

	at, by := db.Stamp(ctx)

	result, err := r.db.Exec(ctx, `
		UPDATE `+r.spec.Table+`
		SET scheduled_revision_id=?, publish_at=?, unpublish_at=?, updated_at=?, updated_by=?
		WHERE id=? AND instance=? AND deleted_at IS NULL`,
		db.Ref(schedule.RevisionID), nullableTime(schedule.PublishAt), nullableTime(schedule.UnpublishAt),
		at, by, id, instance,
	)

	return db.Affected(result, err)
}

// Unschedule cancels the plan of an item.
func (r *Revisions[F, T]) Unschedule(ctx context.Context, instance, id string) error {
	at, by := db.Stamp(ctx)

	result, err := r.db.Exec(ctx, `
		UPDATE `+r.spec.Table+`
		SET scheduled_revision_id=NULL, publish_at=NULL, unpublish_at=NULL, updated_at=?, updated_by=?
		WHERE id=? AND instance=? AND deleted_at IS NULL`,
		at, by, id, instance,
	)

	return db.Affected(result, err)
}

// due is a planned change that has come.
type due struct {
	at      time.Time
	publish bool
}

// RunDue carries out the plans of instance that are due at now, earliest
// first. It returns the number of changes made; failed changes are joined in
// the error and do not stop the others.
func (r *Revisions[F, T]) RunDue(
	ctx context.Context,
	instance string,
	now time.Time,
	required string,
	publisher Publisher,
) (int, error) {
	if publisher.Publish == nil {
		publisher.Publish = func(ctx context.Context, id, revisionID, required string) error {
			return r.Publish(ctx, instance, id, revisionID, required)
		}
	}

	if publisher.Unpublish == nil {
		publisher.Unpublish = func(ctx context.Context, id string) error {
			return r.Unpublish(ctx, instance, id)
		}
	}

	now = now.UTC()

	var rows []struct {
		ID          string       `db:"id"`
		RevisionID  db.Ref       `db:"scheduled_revision_id"`
		PublishAt   sql.NullTime `db:"publish_at"`
		UnpublishAt sql.NullTime `db:"unpublish_at"`
	}

	err := r.db.Select(ctx, &rows, `
		SELECT id, scheduled_revision_id, publish_at, unpublish_at
		FROM `+r.spec.Table+`
		WHERE instance=? AND deleted_at IS NULL AND (publish_at<=? OR unpublish_at<=?)`,
		instance, now, now,
	)
	if err != nil {
		return 0, err
	}

	var (
		changes int
		errs    []error
	)

	for _, row := range rows {
		var steps []due

		if row.PublishAt.Valid && !row.PublishAt.Time.After(now) && row.RevisionID != "" {
			steps = append(steps, due{at: row.PublishAt.Time, publish: true})
		}

		if row.UnpublishAt.Valid && !row.UnpublishAt.Time.After(now) {
			steps = append(steps, due{at: row.UnpublishAt.Time})
		}

		slices.SortStableFunc(steps, func(a, b due) int {
			return a.at.Compare(b.at)
		})

		for _, step := range steps {
			var (
				done bool
				err  error
			)

			if step.publish {
				done, err = r.runPublish(ctx, instance, row.ID, row.RevisionID.String(), now, required, publisher.Publish)
			} else {
				done, err = r.runUnpublish(ctx, instance, row.ID, now, publisher.Unpublish)
			}

			if err != nil {
				errs = append(errs, err)
			}

			if done {
				changes++
			}
		}
	}

	return changes, errors.Join(errs...)
}

// runPublish claims a due publication and publishes in one transaction. It
// reports false when another process or an editor changed the plan first.
func (r *Revisions[F, T]) runPublish(
	ctx context.Context,
	instance, id, revisionID string,
	now time.Time,
	required string,
	publish func(ctx context.Context, id, revisionID, required string) error,
) (bool, error) {
	claim := `UPDATE ` + r.spec.Table + ` SET scheduled_revision_id=NULL, publish_at=NULL
		WHERE id=? AND instance=? AND scheduled_revision_id=? AND publish_at<=?`

	return r.claim(ctx, claim, []any{id, instance, revisionID, now}, func(ctx context.Context) error {
		return publish(ctx, id, revisionID, required)
	})
}

// runUnpublish claims a due unpublication and unpublishes in one transaction.
func (r *Revisions[F, T]) runUnpublish(
	ctx context.Context,
	instance, id string,
	now time.Time,
	unpublish func(ctx context.Context, id string) error,
) (bool, error) {
	claim := `UPDATE ` + r.spec.Table + ` SET unpublish_at=NULL WHERE id=? AND instance=? AND unpublish_at<=?`

	return r.claim(ctx, claim, []any{id, instance, now}, func(ctx context.Context) error {
		return unpublish(ctx, id)
	})
}

// claim runs the conditional UPDATE and, when it changed the row, change in
// the same transaction. A failed change is rolled back and its plan dropped.
func (r *Revisions[F, T]) claim(ctx context.Context, claim string, args []any, change func(context.Context) error) (bool, error) {
	claimed := false

	err := r.db.Tx(ctx, func(ctx context.Context) error {
		result, err := r.db.Exec(ctx, claim, args...)

		err = db.Affected(result, err)
		if errors.Is(err, ErrNotFound) {
			return nil
		}

		if err != nil {
			return err
		}

		claimed = true

		return change(context.WithValue(ctx, schedulerKey{}, true))
	})

	switch {
	case err == nil:
		return claimed, nil
	case !claimed:
		return false, err
	}

	// The change failed: drop the plan, or it would fail on every run.
	result, dropErr := r.db.Exec(ctx, claim, args...)
	if dropErr = db.Affected(result, dropErr); errors.Is(dropErr, ErrNotFound) {
		dropErr = nil
	}

	return false, errors.Join(fmt.Errorf("scheduled change of %s %s: %w", r.spec.Table, args[0], err), dropErr)
}

type schedulerKey struct{}

// scheduled reports whether ctx is a change the scheduler makes, which
// already claimed its part of the plan.
func scheduled(ctx context.Context) bool {
	running, _ := ctx.Value(schedulerKey{}).(bool)

	return running
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}

	return value.UTC().Truncate(time.Second)
}
