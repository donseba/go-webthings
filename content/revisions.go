package content

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/media"
)

// Revisions stores content as immutable revisions. Tables, for Table "x":
//
//	x             (id, instance, published_revision_id, scheduled_revision_id,
//	               publish_at, unpublish_at, created_at, created_by,
//	               updated_at, updated_by, deleted_at, deleted_by)
//	x_revision    (id, entity_id, revision_no, author, created_at, note,
//	               review_state, submitted_at, submitted_by,
//	               reviewed_at, reviewed_by, review_note, <fields>)
//	x_translation (revision_id, locale, <text>)
//
// The item row keeps the audit columns (see db.Actor); a revision row is
// never changed except for its review state (see Review), and its author
// and created_at say who made it and when.
//
// Delete moves an item to the trash: it disappears from every list and
// lookup until Undelete brings it back or Purge removes it for good.
type Revisions[F, T any] struct {
	db     *db.DB
	spec   Spec[F, T]
	fields []column
	texts  []column
}

// NewRevisions returns a Revisions store for spec on database. It checks
// that spec names a table, a Complete function and, with a SlugColumn, a
// text field of that name, so a mistake fails at start rather than in the
// first query.
func NewRevisions[F, T any](database *db.DB, spec Spec[F, T]) (*Revisions[F, T], error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}

	return &Revisions[F, T]{
		db:     database,
		spec:   spec,
		fields: columnsOf(typeOf[F]()),
		texts:  columnsOf(typeOf[T]()),
	}, nil
}

// Spec returns the description the store was made with, for code that
// needs its rules, such as Available.
func (r *Revisions[F, T]) Spec() Spec[F, T] {
	return r.spec
}

// Save stores a new revision. An empty or unknown draft ID creates new content.
func (r *Revisions[F, T]) Save(ctx context.Context, instance string, draft Draft[F, T]) (Item[F, T], error) {
	draft, err := r.spec.clean(draft)
	if err != nil {
		return Item[F, T]{}, err
	}

	var saved Item[F, T]

	err = r.db.Tx(ctx, func(ctx context.Context) error {
		now := time.Now().UTC()

		if draft.ID == "" {
			draft.ID = db.NewID()
		}

		if err := r.ensure(ctx, instance, draft.ID, now); err != nil {
			return err
		}

		var number int

		err := r.db.Get(ctx, &number,
			`SELECT COALESCE(MAX(revision_no),0)+1 FROM `+r.spec.Table+`_revision WHERE entity_id=?`,
			draft.ID,
		)
		if err != nil {
			return err
		}

		revision := Revision{ID: db.NewID(), Number: number, Author: draft.Author, CreatedAt: now, Note: draft.Note}

		if err := r.insertRevision(ctx, draft, revision); err != nil {
			return err
		}

		saved, err = r.Revision(ctx, instance, draft.ID, revision.ID)

		return err
	})

	return saved, err
}

// ensure marks an item as updated, creating it when the ID is new. An item
// in the trash cannot get new revisions: it is not found.
func (r *Revisions[F, T]) ensure(ctx context.Context, instance, id string, now time.Time) error {
	actor := db.Actor(ctx)

	result, err := r.db.Exec(ctx,
		`UPDATE `+r.spec.Table+` SET updated_at=?, updated_by=? WHERE id=? AND instance=? AND deleted_at IS NULL`,
		now, actor, id, instance,
	)
	if err := db.Affected(result, err); !errors.Is(err, ErrNotFound) {
		return err
	}

	_, err = r.db.Exec(ctx, `
		INSERT INTO `+r.spec.Table+`(id,instance,created_at,created_by,updated_at,updated_by)
		VALUES(?,?,?,?,?,?)`,
		id, instance, now, actor, now, actor,
	)
	if db.IsUnique(err) {
		// The ID belongs to a deleted item, or to another instance.
		return ErrNotFound
	}

	return err
}

func (r *Revisions[F, T]) insertRevision(ctx context.Context, draft Draft[F, T], revision Revision) error {
	columns := append([]string{"id", "entity_id", "revision_no", "author", "created_at", "note"}, columnNames(r.fields, "")...)
	args := append([]any{revision.ID, draft.ID, revision.Number, db.Ref(revision.Author), revision.CreatedAt, revision.Note},
		values(draft.Fields, r.fields)...)

	_, err := r.db.Exec(ctx,
		`INSERT INTO `+r.spec.Table+`_revision(`+strings.Join(columns, ",")+`) VALUES(`+placeholders(len(columns))+`)`,
		args...,
	)
	if err != nil {
		return err
	}

	if err := insertTranslations(
		ctx,
		r.db,
		r.spec.Table+"_translation",
		"revision_id",
		revision.ID,
		draft.Translations,
		r.texts,
	); err != nil {
		return err
	}

	return track(ctx, r.db, r.spec, revision.ID, draft)
}

// Revision loads one specific revision.
func (r *Revisions[F, T]) Revision(ctx context.Context, instance, id, revisionID string) (Item[F, T], error) {
	return r.one(ctx, instance, `e.id=? AND v.id=?`, id, revisionID)
}

// Latest loads the newest revision, published or not.
func (r *Revisions[F, T]) Latest(ctx context.Context, instance, id string) (Item[F, T], error) {
	return r.one(ctx, instance, `e.id=? AND `+r.latest(), id)
}

// ListLatest loads the newest revision of every item, newest items first.
func (r *Revisions[F, T]) ListLatest(ctx context.Context, instance string) ([]Item[F, T], error) {
	return r.load(ctx, instance, r.latest())
}

// Outline is ListLatest with only the named texts of the translations, by
// their `db` names such as "slug" and "title"; the other texts stay empty.
// Code that only needs the shape of the content, such as a tree with titles
// and slugs, uses it to leave long texts such as bodies unread. An unknown
// name is an error.
func (r *Revisions[F, T]) Outline(ctx context.Context, instance string, texts ...string) ([]Item[F, T], error) {
	columns := make([]column, 0, len(texts))

	for _, name := range texts {
		index := slices.IndexFunc(r.texts, func(c column) bool {
			return c.name == name
		})
		if index < 0 {
			return nil, fmt.Errorf("content: %s has no text %q", r.spec.Table, name)
		}

		columns = append(columns, r.texts[index])
	}

	return r.list(ctx, instance, listing{where: r.latest(), texts: columns, outline: true})
}

// History lists revision metadata, newest first.
func (r *Revisions[F, T]) History(ctx context.Context, instance, id string) ([]Revision, error) {
	var rows []struct {
		ID        string       `db:"id"`
		Number    int          `db:"revision_no"`
		Author    db.Ref       `db:"author"`
		CreatedAt time.Time    `db:"created_at"`
		Note      string       `db:"note"`
		State     string       `db:"review_state"`
		Submitted sql.NullTime `db:"submitted_at"`
		Submitter db.Ref       `db:"submitted_by"`
		Reviewed  sql.NullTime `db:"reviewed_at"`
		Reviewer  db.Ref       `db:"reviewed_by"`
		Comment   string       `db:"review_note"`
	}

	err := r.db.Select(ctx, &rows, `
		SELECT v.id, v.revision_no, v.author, v.created_at, v.note, `+reviewColumns+`
		FROM `+r.spec.Table+`_revision v
		JOIN `+r.spec.Table+` e ON e.id=v.entity_id
		WHERE e.instance=? AND e.id=? AND e.deleted_at IS NULL
		ORDER BY v.revision_no DESC`,
		instance, id,
	)
	if err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return nil, ErrNotFound
	}

	history := make([]Revision, len(rows))

	for i, row := range rows {
		history[i] = Revision{
			ID:        row.ID,
			Number:    row.Number,
			Author:    row.Author.String(),
			CreatedAt: row.CreatedAt,
			Note:      row.Note,
			Review: reviewScan{
				state:       row.State,
				submittedAt: row.Submitted,
				submittedBy: row.Submitter,
				reviewedAt:  row.Reviewed,
				reviewedBy:  row.Reviewer,
				note:        row.Comment,
			}.review(),
		}
	}

	return history, nil
}

// Publish makes a revision public. required is the locale that must be complete,
// usually the site's default locale; published slugs must be unique per locale.
// It cancels the item's planned publication changes (see Schedule). While
// the instance's review workflow is on (see WithReview), only an approved
// revision is published; others are ErrNotApproved.
func (r *Revisions[F, T]) Publish(ctx context.Context, instance, id, revisionID, required string) error {
	err := r.db.Tx(ctx, func(ctx context.Context) error {
		item, err := r.Revision(ctx, instance, id, revisionID)
		if err != nil {
			return err
		}

		if err := approved(ctx, item); err != nil {
			return err
		}

		if required != "" && !r.spec.Available(item, required) {
			return fmt.Errorf("%w: %s", ErrIncomplete, required)
		}

		if err := r.checkSlugs(ctx, item); err != nil {
			return err
		}

		at, by := db.Stamp(ctx)

		result, err := r.db.Exec(ctx, `
			UPDATE `+r.spec.Table+`
			SET published_revision_id=?, `+cancelPlan(ctx)+`updated_at=?, updated_by=?
			WHERE id=? AND instance=? AND deleted_at IS NULL`,
			revisionID, at, by, id, instance,
		)

		return db.Affected(result, err)
	})

	return r.spec.changed(ctx, err)
}

// cancelPlan clears the planned publication changes of an item that is
// published or unpublished by hand: the editor decided now, so a plan made
// earlier would undo it. The scheduler's own changes keep the rest of the
// plan, for example the unpublication after a scheduled publication.
func cancelPlan(ctx context.Context) string {
	if scheduled(ctx) {
		return ""
	}

	return `scheduled_revision_id=NULL, publish_at=NULL, unpublish_at=NULL, `
}

func (r *Revisions[F, T]) checkSlugs(ctx context.Context, item Item[F, T]) error {
	if r.spec.SlugColumn == "" {
		return nil
	}

	for _, translation := range item.Translations {
		if !r.spec.Complete(translation.Text) {
			continue
		}

		var count int

		err := r.db.Get(ctx, &count, `
			SELECT COUNT(*)
			FROM `+r.spec.Table+`_translation t
			JOIN `+r.spec.Table+` e ON e.published_revision_id=t.revision_id
			WHERE e.instance=? AND e.id<>? AND e.deleted_at IS NULL AND t.locale=? AND t.`+r.spec.SlugColumn+`=?`,
			item.Instance, item.ID, translation.Locale, r.spec.slug(translation.Text),
		)
		if err != nil {
			return err
		}

		if count > 0 {
			return fmt.Errorf("%w: %s/%s", ErrDuplicateSlug, translation.Locale, r.spec.slug(translation.Text))
		}
	}

	return nil
}

// Unpublish takes an item offline and cancels its planned publication
// changes.
func (r *Revisions[F, T]) Unpublish(ctx context.Context, instance, id string) error {
	at, by := db.Stamp(ctx)

	result, err := r.db.Exec(ctx, `
		UPDATE `+r.spec.Table+`
		SET published_revision_id=NULL, `+cancelPlan(ctx)+`updated_at=?, updated_by=?
		WHERE id=? AND instance=? AND deleted_at IS NULL`,
		at, by, id, instance,
	)

	return r.spec.changed(ctx, db.Affected(result, err))
}

// Restore copies an old revision into a new latest revision. It does not publish.
func (r *Revisions[F, T]) Restore(ctx context.Context, instance, id, revisionID, author string) (Item[F, T], error) {
	old, err := r.Revision(ctx, instance, id, revisionID)
	if err != nil {
		return Item[F, T]{}, err
	}

	return r.Save(ctx, instance, Draft[F, T]{
		ID:           id,
		Fields:       old.Fields,
		Translations: old.Translations,
		Author:       author,
		Note:         fmt.Sprintf("Restored revision %d", old.Revision.Number),
	})
}

// rows is the store's table, for moving items of instance to the trash and
// back.
func (r *Revisions[F, T]) rows(instance string) db.SoftTable {
	return db.SoftTable{DB: r.db, Table: r.spec.Table, Scope: "instance=?", Args: []any{instance}}
}

// Delete moves the item to the trash. It keeps its revisions, its
// publication and the media it uses, so Undelete brings it back as it was;
// planned publication changes are cancelled.
func (r *Revisions[F, T]) Delete(ctx context.Context, instance, id string) error {
	if r.spec.Deletable != nil {
		if err := r.spec.Deletable(ctx, id); err != nil {
			return err
		}
	}

	err := r.rows(instance).DeleteWith(ctx, id, `scheduled_revision_id=NULL, publish_at=NULL, unpublish_at=NULL`)

	return r.spec.changed(ctx, err)
}

// Undelete brings an item back from the trash. A published item whose slug
// another published item took meanwhile stays in the trash with
// ErrDuplicateSlug.
func (r *Revisions[F, T]) Undelete(ctx context.Context, instance, id string) error {
	err := r.rows(instance).Undelete(ctx, id, func(ctx context.Context) error {
		published, err := r.list(ctx, instance, listing{
			where:   `e.id=? AND v.id=e.published_revision_id`,
			args:    []any{id},
			deleted: true,
		})
		if err != nil || len(published) == 0 {
			return err
		}

		return r.checkSlugs(ctx, published[0])
	})

	return r.spec.changed(ctx, err)
}

// Purge removes an item in the trash for good, with all its revisions.
func (r *Revisions[F, T]) Purge(ctx context.Context, instance, id string) error {
	return r.rows(instance).Purge(ctx, id, func(ctx context.Context) error {
		var revisions []string

		err := r.db.Select(ctx, &revisions,
			`SELECT id FROM `+r.spec.Table+`_revision WHERE entity_id=?`,
			id,
		)
		if err != nil {
			return err
		}

		for _, revision := range revisions {
			if err := media.Untrack(ctx, r.db, r.spec.Table, revision); err != nil {
				return err
			}
		}

		return nil
	})
}

// Trashed loads the latest revision of every item in the trash, most
// recently deleted first.
func (r *Revisions[F, T]) Trashed(ctx context.Context, instance string) ([]Item[F, T], error) {
	return r.list(ctx, instance, listing{
		where:   r.latest(),
		deleted: true,
		order:   `e.deleted_at DESC, e.id`,
	})
}

// Recent loads the latest revision of at most limit items, the most
// recently changed first, for overviews such as the admin dashboard.
func (r *Revisions[F, T]) Recent(ctx context.Context, instance string, limit int) ([]Item[F, T], error) {
	if limit <= 0 {
		return nil, nil
	}

	return r.list(ctx, instance, listing{
		where: r.latest(),
		order: `e.updated_at DESC, e.id`,
		limit: limit,
	})
}

// Planned loads the latest revision of the items with a planned
// publication change, the earliest change first.
func (r *Revisions[F, T]) Planned(ctx context.Context, instance string) ([]Item[F, T], error) {
	return r.list(ctx, instance, listing{
		where: r.latest() + ` AND (e.publish_at IS NOT NULL OR e.unpublish_at IS NOT NULL)`,
		order: `COALESCE(e.publish_at, e.unpublish_at), e.id`,
	})
}

// Published loads the published revision if it is complete in locale.
func (r *Revisions[F, T]) Published(ctx context.Context, instance, id, locale string) (Item[F, T], error) {
	item, err := r.one(ctx, instance, `e.id=? AND v.id=e.published_revision_id`, id)
	if err != nil {
		return item, err
	}

	if !r.spec.Available(item, locale) {
		return Item[F, T]{}, ErrNotFound
	}

	return item, nil
}

// PublishedBySlug finds published content by its translated slug.
func (r *Revisions[F, T]) PublishedBySlug(ctx context.Context, instance, locale, slug string) (Item[F, T], error) {
	if r.spec.SlugColumn == "" {
		return Item[F, T]{}, ErrNotFound
	}

	item, err := r.one(ctx, instance, `
		v.id=e.published_revision_id AND EXISTS (
			SELECT 1 FROM `+r.spec.Table+`_translation t
			WHERE t.revision_id=v.id AND t.locale=? AND t.`+r.spec.SlugColumn+`=?
		)`,
		locale, slug,
	)
	if err != nil {
		return item, err
	}

	if !r.spec.Available(item, locale) {
		return Item[F, T]{}, ErrNotFound
	}

	return item, nil
}

// AllPublished loads the published revision of every item, in any language.
func (r *Revisions[F, T]) AllPublished(ctx context.Context, instance string) ([]Item[F, T], error) {
	return r.load(ctx, instance, `v.id=e.published_revision_id`)
}

// ListPublished loads published content that is complete in locale, newest first.
func (r *Revisions[F, T]) ListPublished(ctx context.Context, instance, locale string) ([]Item[F, T], error) {
	items, err := r.load(ctx, instance, `v.id=e.published_revision_id`)
	if err != nil {
		return nil, err
	}

	out := items[:0]

	for _, item := range items {
		if r.spec.Available(item, locale) {
			out = append(out, item)
		}
	}

	return out, nil
}

func (r *Revisions[F, T]) latest() string {
	return `v.revision_no=(SELECT MAX(m.revision_no) FROM ` + r.spec.Table + `_revision m WHERE m.entity_id=v.entity_id)`
}

func (r *Revisions[F, T]) one(ctx context.Context, instance, where string, args ...any) (Item[F, T], error) {
	items, err := r.load(ctx, instance, where, args...)
	if err != nil {
		return Item[F, T]{}, err
	}

	if len(items) == 0 {
		return Item[F, T]{}, ErrNotFound
	}

	return items[0], nil
}

// load loads live items, newest first.
func (r *Revisions[F, T]) load(ctx context.Context, instance, where string, args ...any) ([]Item[F, T], error) {
	return r.list(ctx, instance, listing{where: where, args: args})
}

// listing selects the revisions list loads.
type listing struct {
	where string
	args  []any
	// deleted lists the items in the trash instead of the live ones.
	deleted bool
	// order is the ORDER BY; empty lists the newest items first.
	order string
	// limit is the most rows; zero means all.
	limit int
	// texts are the translation columns read when outline is set; all of
	// them otherwise.
	texts   []column
	outline bool
}

func (l listing) sql(alias string) string {
	query := ` AND ` + alias + `.deleted_at IS NULL`
	if l.deleted {
		query = ` AND ` + alias + `.deleted_at IS NOT NULL`
	}

	order := l.order
	if order == "" {
		order = alias + `.created_at DESC, ` + alias + `.id`
	}

	query += ` ORDER BY ` + order

	if l.limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, l.limit)
	}

	return query
}

func (r *Revisions[F, T]) list(ctx context.Context, instance string, l listing) ([]Item[F, T], error) {
	rows, err := r.db.Query(ctx, `
		SELECT v.id, v.entity_id, v.revision_no, v.author, v.created_at, v.note, `+reviewColumns+`,
			e.instance, e.published_revision_id, e.created_at, e.updated_at,
			e.created_by, e.updated_by, e.deleted_at, e.deleted_by,
			e.scheduled_revision_id, e.publish_at, e.unpublish_at,
			COALESCE(p.revision_no, 0)`+prefixed(columnNames(r.fields, "v."))+`
		FROM `+r.spec.Table+`_revision v
		JOIN `+r.spec.Table+` e ON e.id=v.entity_id
		LEFT JOIN `+r.spec.Table+`_revision p ON p.id=e.published_revision_id
		WHERE e.instance=? AND `+l.where+l.sql("e"),
		append([]any{instance}, l.args...)...,
	)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	var (
		items []Item[F, T]
		ids   []string
	)

	for rows.Next() {
		var (
			item        Item[F, T]
			author      db.Ref
			published   db.Ref
			scheduled   db.Ref
			publishAt   sql.NullTime
			unpublishAt sql.NullTime
			audit       auditColumns
			review      reviewScan
		)

		destinations := append([]any{
			&item.Revision.ID, &item.ID, &item.Revision.Number, &author, &item.Revision.CreatedAt, &item.Revision.Note,
		}, review.pointers()...)
		destinations = append(destinations, &item.Instance, &published, &item.CreatedAt, &item.UpdatedAt)
		destinations = append(destinations, audit.pointers()...)
		destinations = append(destinations, &scheduled, &publishAt, &unpublishAt, &item.PublishedNumber)
		destinations = append(destinations, pointers(&item.Fields, r.fields)...)

		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}

		fillAudit(&item, audit)
		item.Revision.Author = author.String()
		item.Revision.Review = review.review()
		item.PublishedID = published.String()
		item.Published = published != ""
		item.Schedule = Schedule{
			RevisionID:  scheduled.String(),
			PublishAt:   publishAt.Time,
			UnpublishAt: unpublishAt.Time,
		}
		items = append(items, item)
		ids = append(ids, item.Revision.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	texts := r.texts
	if l.outline {
		texts = l.texts
	}

	translations, err := loadTranslations[T](ctx, r.db, r.spec.Table+"_translation", "revision_id", ids, texts)
	if err != nil {
		return nil, err
	}

	for i := range items {
		items[i].Translations = translations[items[i].Revision.ID]
	}

	return items, nil
}

func prefixed(names []string) string {
	if len(names) == 0 {
		return ""
	}

	return ", " + strings.Join(names, ", ")
}
