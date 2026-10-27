package content

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/media"
)

// Collection stores one row per item with a published flag and an order.
// Tables, for Table "x":
//
//	x             (id, instance, published, position, created_at, created_by,
//	               updated_at, updated_by, deleted_at, deleted_by, <fields>)
//	x_translation (item_id, locale, <text>)
//
// Delete moves an item to the trash (see Revisions). A unique index on a
// field, such as a slug, should leave deleted rows out: a partial index
// (WHERE deleted_at IS NULL) in SQLite and Postgres, a generated column in
// MySQL; content/testdata shows both.
type Collection[F, T any] struct {
	db     *db.DB
	spec   Spec[F, T]
	fields []column
	texts  []column
}

// NewCollection returns a Collection for spec on database. It checks that
// spec names a table, a Complete function and, with a SlugColumn, a text
// field of that name, so a mistake fails at start rather than in the first
// query.
func NewCollection[F, T any](database *db.DB, spec Spec[F, T]) (*Collection[F, T], error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}

	return &Collection[F, T]{
		db:     database,
		spec:   spec,
		fields: columnsOf(typeOf[F]()),
		texts:  columnsOf(typeOf[T]()),
	}, nil
}

// Spec returns the description the Collection was made with, for code that
// needs its rules, such as Available.
func (c *Collection[F, T]) Spec() Spec[F, T] {
	return c.spec
}

// Save creates or replaces an item. New items (empty or unknown ID) are added
// at the end, unpublished.
func (c *Collection[F, T]) Save(ctx context.Context, instance string, draft Draft[F, T]) (Item[F, T], error) {
	draft, err := c.spec.clean(draft)
	if err != nil {
		return Item[F, T]{}, err
	}

	var saved Item[F, T]

	err = c.db.Tx(ctx, func(ctx context.Context) error {
		now := time.Now().UTC()

		if draft.ID == "" {
			draft.ID = db.NewID()
		}

		err := c.update(ctx, instance, draft, now)
		if errors.Is(err, ErrNotFound) {
			err = c.insert(ctx, instance, draft, now)
		}

		if db.IsUnique(err) {
			// A unique field of the item, such as a slug, is taken.
			err = fmt.Errorf("%w: %w", ErrDuplicateSlug, err)
		}

		if err != nil {
			return err
		}

		_, err = c.db.Exec(ctx, `DELETE FROM `+c.spec.Table+`_translation WHERE item_id=?`, draft.ID)
		if err != nil {
			return err
		}

		if err := insertTranslations(
			ctx,
			c.db,
			c.spec.Table+"_translation",
			"item_id",
			draft.ID,
			draft.Translations,
			c.texts,
		); err != nil {
			return err
		}

		if err := c.checkSlugs(ctx, instance, draft); err != nil {
			return err
		}

		if err := track(ctx, c.db, c.spec, draft.ID, draft); err != nil {
			return err
		}

		saved, err = c.Get(ctx, instance, draft.ID)

		return err
	})

	return saved, c.spec.changed(ctx, err)
}

func (c *Collection[F, T]) insert(ctx context.Context, instance string, draft Draft[F, T], now time.Time) error {
	// The ID of a deleted item, or of another instance, is not found. It is
	// checked first: after a failed INSERT, Postgres refuses further
	// statements in the transaction.
	var taken int

	if err := c.db.Get(ctx, &taken, `SELECT COUNT(*) FROM `+c.spec.Table+` WHERE id=?`, draft.ID); err != nil {
		return err
	}

	if taken > 0 {
		return ErrNotFound
	}

	var position int

	err := c.db.Get(ctx, &position,
		`SELECT COALESCE(MAX(position),0)+1 FROM `+c.spec.Table+` WHERE instance=?`,
		instance,
	)
	if err != nil {
		return err
	}

	actor := db.Actor(ctx)
	columns := append(
		[]string{"id", "instance", "published", "position", "created_at", "created_by", "updated_at", "updated_by"},
		columnNames(c.fields, "")...)
	args := append([]any{draft.ID, instance, false, position, now, actor, now, actor}, values(draft.Fields, c.fields)...)

	_, err = c.db.Exec(ctx,
		`INSERT INTO `+c.spec.Table+`(`+strings.Join(columns, ",")+`) VALUES(`+placeholders(len(columns))+`)`,
		args...,
	)

	return err
}

func (c *Collection[F, T]) update(ctx context.Context, instance string, draft Draft[F, T], now time.Time) error {
	assignments := []string{"updated_at=?", "updated_by=?"}

	for _, name := range columnNames(c.fields, "") {
		assignments = append(assignments, name+"=?")
	}

	args := append([]any{now, db.Actor(ctx)}, values(draft.Fields, c.fields)...)
	args = append(args, draft.ID, instance)

	result, err := c.db.Exec(ctx,
		`UPDATE `+c.spec.Table+` SET `+strings.Join(assignments, ", ")+` WHERE id=? AND instance=? AND deleted_at IS NULL`,
		args...,
	)

	return db.Affected(result, err)
}

// checkSlugs keeps slugs unique per instance and locale among all items.
func (c *Collection[F, T]) checkSlugs(ctx context.Context, instance string, draft Draft[F, T]) error {
	if c.spec.SlugColumn == "" {
		return nil
	}

	for _, translation := range draft.Translations {
		slug := c.spec.slug(translation.Text)
		if slug == "" {
			continue
		}

		var count int

		err := c.db.Get(ctx, &count, `
			SELECT COUNT(*)
			FROM `+c.spec.Table+`_translation t
			JOIN `+c.spec.Table+` i ON i.id=t.item_id
			WHERE i.instance=? AND i.id<>? AND i.deleted_at IS NULL AND t.locale=? AND t.`+c.spec.SlugColumn+`=?`,
			instance, draft.ID, translation.Locale, slug,
		)
		if err != nil {
			return err
		}

		if count > 0 {
			return fmt.Errorf("%w: %s/%s", ErrDuplicateSlug, translation.Locale, slug)
		}
	}

	return nil
}

// Get returns one item of instance with every translation, published or
// not, for the admin. It returns ErrNotFound for an item that does not
// exist or is in the trash.
func (c *Collection[F, T]) Get(ctx context.Context, instance, id string) (Item[F, T], error) {
	items, err := c.live(ctx, `i.instance=? AND i.id=?`, instance, id)
	if err != nil {
		return Item[F, T]{}, err
	}

	if len(items) == 0 {
		return Item[F, T]{}, ErrNotFound
	}

	return items[0], nil
}

// List returns all items in their order.
func (c *Collection[F, T]) List(ctx context.Context, instance string) ([]Item[F, T], error) {
	return c.live(ctx, `i.instance=?`, instance)
}

// ListPublished returns published items that are complete in locale, in order.
func (c *Collection[F, T]) ListPublished(ctx context.Context, instance, locale string) ([]Item[F, T], error) {
	items, err := c.live(ctx, `i.instance=? AND i.published=?`, instance, true)
	if err != nil {
		return nil, err
	}

	out := items[:0]

	for _, item := range items {
		if c.spec.Available(item, locale) {
			out = append(out, item)
		}
	}

	return out, nil
}

// Published returns one published item if it is complete in locale.
func (c *Collection[F, T]) Published(ctx context.Context, instance, id, locale string) (Item[F, T], error) {
	item, err := c.Get(ctx, instance, id)
	if err != nil {
		return item, err
	}

	if !item.Published || !c.spec.Available(item, locale) {
		return Item[F, T]{}, ErrNotFound
	}

	return item, nil
}

// PublishedBySlug finds a published item by its translated slug.
func (c *Collection[F, T]) PublishedBySlug(ctx context.Context, instance, locale, slug string) (Item[F, T], error) {
	if c.spec.SlugColumn == "" {
		return Item[F, T]{}, ErrNotFound
	}

	items, err := c.live(ctx, `
		i.instance=? AND i.published=? AND EXISTS (
			SELECT 1 FROM `+c.spec.Table+`_translation t
			WHERE t.item_id=i.id AND t.locale=? AND t.`+c.spec.SlugColumn+`=?
		)`,
		instance, true, locale, slug,
	)
	if err != nil {
		return Item[F, T]{}, err
	}

	if len(items) == 0 || !c.spec.Available(items[0], locale) {
		return Item[F, T]{}, ErrNotFound
	}

	return items[0], nil
}

// SetPublished shows or hides an item. Publishing requires the required locale.
func (c *Collection[F, T]) SetPublished(ctx context.Context, instance, id string, published bool, required string) error {
	if published && required != "" {
		item, err := c.Get(ctx, instance, id)
		if err != nil {
			return err
		}

		if !c.spec.Available(item, required) {
			return fmt.Errorf("%w: %s", ErrIncomplete, required)
		}
	}

	at, by := db.Stamp(ctx)

	result, err := c.db.Exec(ctx, `
		UPDATE `+c.spec.Table+`
		SET published=?, updated_at=?, updated_by=?
		WHERE id=? AND instance=? AND deleted_at IS NULL`,
		published, at, by, id, instance,
	)

	return c.spec.changed(ctx, db.Affected(result, err))
}

// Move puts an item at position; other items keep their relative order.
func (c *Collection[F, T]) Move(ctx context.Context, instance, id string, position int) error {
	err := c.db.Tx(ctx, func(ctx context.Context) error {
		items, err := c.List(ctx, instance)
		if err != nil {
			return err
		}

		var order []string

		for _, item := range items {
			if item.ID != id {
				order = append(order, item.ID)
			}
		}

		if len(order) == len(items) {
			return ErrNotFound
		}

		position = min(max(position, 0), len(order))
		order = append(order[:position], append([]string{id}, order[position:]...)...)

		for i, itemID := range order {
			_, err := c.db.Exec(ctx, `UPDATE `+c.spec.Table+` SET position=? WHERE id=?`, i+1, itemID)
			if err != nil {
				return err
			}
		}

		return nil
	})

	return c.spec.changed(ctx, err)
}

// rows is the store's table, for moving items of instance to the trash and
// back.
func (c *Collection[F, T]) rows(instance string) db.SoftTable {
	return db.SoftTable{DB: c.db, Table: c.spec.Table, Scope: "instance=?", Args: []any{instance}}
}

// Delete moves the item to the trash. It keeps its place, its publication
// and the media it uses, so Undelete brings it back as it was.
func (c *Collection[F, T]) Delete(ctx context.Context, instance, id string) error {
	if c.spec.Deletable != nil {
		if err := c.spec.Deletable(ctx, id); err != nil {
			return err
		}
	}

	return c.spec.changed(ctx, c.rows(instance).Delete(ctx, id))
}

// Undelete brings an item back from the trash. An item whose slug another
// item took meanwhile stays in the trash with ErrDuplicateSlug.
func (c *Collection[F, T]) Undelete(ctx context.Context, instance, id string) error {
	err := c.rows(instance).Undelete(ctx, id, func(ctx context.Context) error {
		items, err := c.load(ctx, listing{where: `i.instance=? AND i.id=?`, args: []any{instance, id}, deleted: true})
		if err != nil {
			return err
		}

		if len(items) == 0 {
			return ErrNotFound
		}

		return c.checkSlugs(ctx, instance, Draft[F, T]{ID: id, Translations: items[0].Translations})
	})
	if db.IsUnique(err) {
		// A unique field of the item, such as a slug, is taken.
		return fmt.Errorf("%w: %w", ErrDuplicateSlug, err)
	}

	return c.spec.changed(ctx, err)
}

// Purge removes an item in the trash for good.
func (c *Collection[F, T]) Purge(ctx context.Context, instance, id string) error {
	return c.rows(instance).Purge(ctx, id, func(ctx context.Context) error {
		return media.Untrack(ctx, c.db, c.spec.Table, id)
	})
}

// Trashed returns the items in the trash, most recently deleted first.
func (c *Collection[F, T]) Trashed(ctx context.Context, instance string) ([]Item[F, T], error) {
	return c.load(ctx, listing{
		where:   `i.instance=?`,
		args:    []any{instance},
		deleted: true,
		order:   `i.deleted_at DESC, i.id`,
	})
}

// Recent returns at most limit items, the most recently changed first.
func (c *Collection[F, T]) Recent(ctx context.Context, instance string, limit int) ([]Item[F, T], error) {
	if limit <= 0 {
		return nil, nil
	}

	return c.load(ctx, listing{
		where: `i.instance=?`,
		args:  []any{instance},
		order: `i.updated_at DESC, i.id`,
		limit: limit,
	})
}

// live lists the live items matching where, in their order.
func (c *Collection[F, T]) live(ctx context.Context, where string, args ...any) ([]Item[F, T], error) {
	return c.load(ctx, listing{where: where, args: args, order: `i.position, i.created_at`})
}

func (c *Collection[F, T]) load(ctx context.Context, l listing) ([]Item[F, T], error) {
	rows, err := c.db.Query(ctx, `
		SELECT i.id, i.instance, i.published, i.position, i.created_at, i.updated_at,
			i.created_by, i.updated_by, i.deleted_at, i.deleted_by`+prefixed(columnNames(c.fields, "i."))+`
		FROM `+c.spec.Table+` i
		WHERE `+l.where+l.sql("i"),
		l.args...,
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
			item      Item[F, T]
			published sql.NullBool
			audit     auditColumns
		)

		destinations := append([]any{
			&item.ID, &item.Instance, &published, &item.Position, &item.CreatedAt, &item.UpdatedAt,
		}, audit.pointers()...)
		destinations = append(destinations, pointers(&item.Fields, c.fields)...)

		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}

		fillAudit(&item, audit)
		item.Published = published.Bool
		items = append(items, item)
		ids = append(ids, item.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	translations, err := loadTranslations[T](ctx, c.db, c.spec.Table+"_translation", "item_id", ids, c.texts)
	if err != nil {
		return nil, err
	}

	for i := range items {
		items[i].Translations = translations[items[i].ID]
	}

	return items, nil
}
