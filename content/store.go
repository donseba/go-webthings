package content

import (
	"context"
	"time"
)

// Store is the content of one instance: everything an editor, an import or a
// seed needs. Get one with Revisions.In or Collection.In.
type Store[F, T any] interface {
	// Spec returns the description of the content.
	Spec() Spec[F, T]
	// List returns every item outside the trash, published or not, in the
	// store's order.
	List(ctx context.Context) ([]Item[F, T], error)
	// Load returns the latest state, or a specific revision when revision is set.
	Load(ctx context.Context, id, revision string) (Item[F, T], error)
	// Save stores draft: a new revision, or the new state of the item. It
	// does not publish.
	Save(ctx context.Context, draft Draft[F, T]) (Item[F, T], error)
	// Delete moves the item to the trash.
	Delete(ctx context.Context, id string) error
	// Trashed lists the items in the trash, most recently deleted first.
	Trashed(ctx context.Context) ([]Item[F, T], error)
	// Undelete brings an item back from the trash; ErrDuplicateSlug when
	// another item took its slug meanwhile.
	Undelete(ctx context.Context, id string) error
	// Purge removes an item in the trash for good.
	Purge(ctx context.Context, id string) error
	// Recent returns at most limit items, the most recently changed first.
	Recent(ctx context.Context, limit int) ([]Item[F, T], error)
	// Publish makes the item (or the given revision) public; locale must be complete.
	Publish(ctx context.Context, id, revision, locale string) error
	// Unpublish takes the item off the website; it stays in the admin.
	Unpublish(ctx context.Context, id string) error
	// Revisioned reports whether History and Restore are available.
	Revisioned() bool
	// History lists the item's revisions, newest first; nil when the store
	// keeps none.
	History(ctx context.Context, id string) ([]Revision, error)
	// Restore copies an old revision into a new latest revision by author.
	// It does not publish.
	Restore(ctx context.Context, id, revision, author string) (Item[F, T], error)
}

// Mover is implemented by stores whose items have an order (collections).
type Mover interface {
	Move(ctx context.Context, id string, position int) error
}

// Planner is implemented by stores whose publication can be planned: it
// lists the items with a planned change, the earliest first.
type Planner[F, T any] interface {
	Planned(ctx context.Context) ([]Item[F, T], error)
}

// In binds the store to an instance: saving creates drafts, publishing is
// explicit and revisions can be restored. The store implements Scheduling
// and Reviewing.
func (r *Revisions[F, T]) In(instance string) Store[F, T] {
	return revisioned[F, T]{store: r, instance: instance}
}

// Reviews is the review workflow of an instance (see Reviewing), for
// stores of their own that wrap Revisions, such as the page tree's.
func (r *Revisions[F, T]) Reviews(instance string) Reviewing[F, T] {
	return revisioned[F, T]{store: r, instance: instance}
}

// Scheduler carries out the planned publication changes of an instance, for
// the App's scheduler (see webthings.Parts.Scheduled).
func (r *Revisions[F, T]) Scheduler(instance string) Scheduler {
	return revisioned[F, T]{store: r, instance: instance}
}

type revisioned[F, T any] struct {
	store    *Revisions[F, T]
	instance string
}

func (s revisioned[F, T]) Spec() Spec[F, T] {
	return s.store.Spec()
}

func (s revisioned[F, T]) List(ctx context.Context) ([]Item[F, T], error) {
	return s.store.ListLatest(ctx, s.instance)
}

func (s revisioned[F, T]) Load(ctx context.Context, id, revision string) (Item[F, T], error) {
	if revision != "" {
		return s.store.Revision(ctx, s.instance, id, revision)
	}

	return s.store.Latest(ctx, s.instance, id)
}

func (s revisioned[F, T]) Save(ctx context.Context, draft Draft[F, T]) (Item[F, T], error) {
	return s.store.Save(ctx, s.instance, draft)
}

func (s revisioned[F, T]) Delete(ctx context.Context, id string) error {
	return s.store.Delete(ctx, s.instance, id)
}

func (s revisioned[F, T]) Trashed(ctx context.Context) ([]Item[F, T], error) {
	return s.store.Trashed(ctx, s.instance)
}

func (s revisioned[F, T]) Undelete(ctx context.Context, id string) error {
	return s.store.Undelete(ctx, s.instance, id)
}

func (s revisioned[F, T]) Purge(ctx context.Context, id string) error {
	return s.store.Purge(ctx, s.instance, id)
}

func (s revisioned[F, T]) Recent(ctx context.Context, limit int) ([]Item[F, T], error) {
	return s.store.Recent(ctx, s.instance, limit)
}

func (s revisioned[F, T]) Planned(ctx context.Context) ([]Item[F, T], error) {
	return s.store.Planned(ctx, s.instance)
}

func (s revisioned[F, T]) Publish(ctx context.Context, id, revision, locale string) error {
	return s.store.Publish(ctx, s.instance, id, revision, locale)
}

func (s revisioned[F, T]) Unpublish(ctx context.Context, id string) error {
	return s.store.Unpublish(ctx, s.instance, id)
}

func (s revisioned[F, T]) Revisioned() bool {
	return true
}

func (s revisioned[F, T]) History(ctx context.Context, id string) ([]Revision, error) {
	return s.store.History(ctx, s.instance, id)
}

func (s revisioned[F, T]) Restore(ctx context.Context, id, revision, author string) (Item[F, T], error) {
	return s.store.Restore(ctx, s.instance, id, revision, author)
}

func (s revisioned[F, T]) Schedule(ctx context.Context, id string, schedule Schedule, required string) error {
	return s.store.Schedule(ctx, s.instance, id, schedule, required)
}

func (s revisioned[F, T]) Unschedule(ctx context.Context, id string) error {
	return s.store.Unschedule(ctx, s.instance, id)
}

func (s revisioned[F, T]) RunDue(ctx context.Context, now time.Time, required string) (int, error) {
	return s.store.RunDue(ctx, s.instance, now, required, Publisher{})
}

func (s revisioned[F, T]) ReviewRequired(ctx context.Context) bool {
	return ReviewRequired(ctx, s.instance)
}

func (s revisioned[F, T]) Submit(ctx context.Context, id, revision string) error {
	return s.store.Submit(ctx, s.instance, id, revision)
}

func (s revisioned[F, T]) Approve(ctx context.Context, id, revision, note string) error {
	return s.store.Approve(ctx, s.instance, id, revision, note)
}

func (s revisioned[F, T]) Reject(ctx context.Context, id, revision, note string) error {
	return s.store.Reject(ctx, s.instance, id, revision, note)
}

func (s revisioned[F, T]) InReview(ctx context.Context, limit int) ([]Item[F, T], error) {
	return s.store.InReview(ctx, s.instance, limit)
}

// In binds the collection to an instance: saving changes the item directly,
// publishing sets a flag and items can be moved.
func (c *Collection[F, T]) In(instance string) Store[F, T] {
	return simple[F, T]{store: c, instance: instance}
}

type simple[F, T any] struct {
	store    *Collection[F, T]
	instance string
}

func (s simple[F, T]) Spec() Spec[F, T] {
	return s.store.Spec()
}

func (s simple[F, T]) List(ctx context.Context) ([]Item[F, T], error) {
	return s.store.List(ctx, s.instance)
}

func (s simple[F, T]) Load(ctx context.Context, id, _ string) (Item[F, T], error) {
	return s.store.Get(ctx, s.instance, id)
}

func (s simple[F, T]) Save(ctx context.Context, draft Draft[F, T]) (Item[F, T], error) {
	return s.store.Save(ctx, s.instance, draft)
}

func (s simple[F, T]) Delete(ctx context.Context, id string) error {
	return s.store.Delete(ctx, s.instance, id)
}

func (s simple[F, T]) Trashed(ctx context.Context) ([]Item[F, T], error) {
	return s.store.Trashed(ctx, s.instance)
}

func (s simple[F, T]) Undelete(ctx context.Context, id string) error {
	return s.store.Undelete(ctx, s.instance, id)
}

func (s simple[F, T]) Purge(ctx context.Context, id string) error {
	return s.store.Purge(ctx, s.instance, id)
}

func (s simple[F, T]) Recent(ctx context.Context, limit int) ([]Item[F, T], error) {
	return s.store.Recent(ctx, s.instance, limit)
}

func (s simple[F, T]) Publish(ctx context.Context, id, _, locale string) error {
	return s.store.SetPublished(ctx, s.instance, id, true, locale)
}

func (s simple[F, T]) Unpublish(ctx context.Context, id string) error {
	return s.store.SetPublished(ctx, s.instance, id, false, "")
}

func (s simple[F, T]) Move(ctx context.Context, id string, position int) error {
	return s.store.Move(ctx, s.instance, id, position)
}

func (s simple[F, T]) Revisioned() bool {
	return false
}

func (s simple[F, T]) History(context.Context, string) ([]Revision, error) {
	return nil, nil
}

func (s simple[F, T]) Restore(context.Context, string, string, string) (Item[F, T], error) {
	return Item[F, T]{}, ErrNotFound
}
