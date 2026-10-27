// Package content stores multilingual content for modules. A module describes
// its content with two structs and a Spec; this package takes care of the
// tables, the languages, slugs, publication and which media the content
// uses.
//
//   - Revisions keeps immutable revisions: save drafts, preview, publish, restore.
//   - Collection keeps one row per item with a published flag and an order.
//
// Fields (F) and translated text (T) are structs whose `db` tags name columns.
// The tables each store expects are listed on Revisions and Collection;
// content/testdata holds a complete example schema for every dialect.
package content

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/locales"
)

var (
	// ErrNotFound is db.ErrNotFound, so errors from either layer match.
	ErrNotFound = db.ErrNotFound
	// ErrInvalid is content the store cannot take as it is, such as an
	// unknown language, a language twice or a slug with other characters
	// than lowercase letters, digits and dashes. The error names the value.
	ErrInvalid = errors.New("invalid_content")
	// ErrIncomplete refuses to publish content whose translation in the
	// required language is missing or incomplete (see Spec.Complete); the
	// error names the language.
	ErrIncomplete = errors.New("content_translation_incomplete")
	// ErrDuplicateSlug is a slug that another item of the same instance
	// already has in that language; the error names the language and slug.
	ErrDuplicateSlug = errors.New("duplicate_slug")
	// ErrMediaMissing is content that refers to a file no longer in the
	// media library.
	ErrMediaMissing = errors.New("referenced_media_not_found")
	// ErrTooLong is returned by Spec.Check functions for a text over its
	// limit; the admin's editor explains it without a module message.
	ErrTooLong = errors.New("content_too_long")
)

// TooLong reports whether one of the values is longer than limit
// characters. It counts characters, not bytes, as the admin's forms and the
// databases' VARCHAR columns do, so a text the form accepted in any script
// also passes a Spec.Check.
func TooLong(limit int, values ...string) bool {
	for _, value := range values {
		if utf8.RuneCountInString(value) > limit {
			return true
		}
	}

	return false
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Spec describes one content type.
type Spec[F, T any] struct {
	// Table is the base table name, for example "wt_publishing_post".
	Table string
	// Complete reports whether a translation can be shown publicly.
	Complete func(T) bool
	// SlugColumn names T's slug column. Empty means the content has no detail URLs.
	SlugColumn string
	// Check validates module rules after the kit normalized locales and slugs.
	Check func(fields F, translations []Translation[T]) error
	// Media lists referenced media asset IDs. They cannot be deleted while used.
	Media func(fields F, translations []Translation[T]) []string
	// Deletable refuses moving an item to the trash with an error, for
	// example while live items of another type still refer to it. Nil
	// allows every delete.
	Deletable func(ctx context.Context, id string) error
	// Changed, when set, is called after every committed write that may
	// change what visitors see: publishing, unpublishing, moving to the
	// trash and back and, for a Collection, saving and moving an item.
	// Modules set it to webthings.Env.Changed, so the output cache renders
	// its pages again also after a write made from Go, such as an import,
	// a seed or a background job.
	Changed func()
}

// changed calls Changed once the write that err reports on is committed,
// when it succeeded (see db.AfterWrite), and returns err.
func (s Spec[F, T]) changed(ctx context.Context, err error) error {
	return db.AfterWrite(ctx, err, s.Changed)
}

// Translation is the text of an item in one language, such as the title
// and body of a post in Dutch.
type Translation[T any] struct {
	// Locale is the language code, such as "nl"; stores normalize it (see
	// locales.Normalize).
	Locale string
	// Text is the translated text, the T of the Spec.
	Text T
}

// Revision is the metadata of one immutable revision.
type Revision struct {
	ID string
	// Number counts the item's revisions from 1, for people.
	Number int
	// Author is the ID of the user who saved it; empty for system changes.
	Author    string
	CreatedAt time.Time
	// Note is the change note the author gave, if any.
	Note string
	// Review is the revision's state in the optional review workflow (see
	// WithReview).
	Review Review
}

// Item is one piece of content. For Revisions it holds one specific revision.
type Item[F, T any] struct {
	ID           string
	Instance     string
	Fields       F
	Translations []Translation[T]
	CreatedAt    time.Time
	UpdatedAt    time.Time
	// CreatedBy and UpdatedBy are the IDs of the users who made the item and
	// changed it last; empty for visitors and the system (see db.Actor).
	CreatedBy string
	UpdatedBy string
	// DeletedAt is when the item went to the trash, by DeletedBy; nil for
	// live items. Only Trashed returns deleted items.
	DeletedAt *time.Time
	DeletedBy string

	// Published is true when the item is visible. For Revisions this means some
	// revision is published, not necessarily the one in this Item.
	Published bool

	// Collection only.
	Position int

	// Revisions only.
	Revision        Revision
	PublishedID     string
	PublishedNumber int
	// Schedule is the item's planned publication change, if any.
	Schedule Schedule
}

// Text returns the translation for locale, if one was written.
func (i Item[F, T]) Text(locale string) (T, bool) {
	for _, translation := range i.Translations {
		if translation.Locale == locale {
			return translation.Text, true
		}
	}

	var empty T

	return empty, false
}

// WithText returns the translations with locale set to text, keeping the others.
func (i Item[F, T]) WithText(locale string, text T) []Translation[T] {
	return SetText(i.Translations, locale, text)
}

// SetText returns translations with locale set to text, keeping the others.
func SetText[T any](translations []Translation[T], locale string, text T) []Translation[T] {
	out := make([]Translation[T], 0, len(translations)+1)

	for _, translation := range translations {
		if translation.Locale != locale {
			out = append(out, translation)
		}
	}

	return append(out, Translation[T]{Locale: locale, Text: text})
}

// IsPublishedRevision reports whether this Item holds the published revision.
func (i Item[F, T]) IsPublishedRevision() bool {
	return i.PublishedID != "" && i.PublishedID == i.Revision.ID
}

// Draft is what an editor submits: a new revision, or the new item state.
type Draft[F, T any] struct {
	// ID is empty for new content.
	ID string
	// Fields are the language-independent values.
	Fields F
	// Translations are the texts per language; languages left out have no
	// text.
	Translations []Translation[T]
	// Author is the user ID; empty for system changes. Revisions only.
	Author string
	// Note is an optional change note. Revisions only.
	Note string
}

// Available reports whether item has a complete translation for locale.
func (s Spec[F, T]) Available(item Item[F, T], locale string) bool {
	text, ok := item.Text(locale)

	return ok && s.Complete(text)
}

// clean normalizes locales, validates slugs, sorts translations and runs Check.
func (s Spec[F, T]) clean(draft Draft[F, T]) (Draft[F, T], error) {
	if len(draft.Translations) > 100 || len(draft.Note) > 2000 {
		return draft, ErrInvalid
	}

	seen := map[string]bool{}
	translations := make([]Translation[T], 0, len(draft.Translations))

	for _, translation := range draft.Translations {
		locale, err := locales.Normalize(translation.Locale)
		if err != nil || seen[locale] {
			return draft, fmt.Errorf("%w: locale %q", ErrInvalid, translation.Locale)
		}

		seen[locale] = true
		translation.Locale = locale

		if s.SlugColumn != "" {
			slug := strings.TrimSpace(s.slug(translation.Text))
			s.setSlug(&translation.Text, slug)

			if slug != "" && (len(slug) > 190 || !slugPattern.MatchString(slug)) {
				return draft, fmt.Errorf("%w: slug %q", ErrInvalid, slug)
			}
		}

		translations = append(translations, translation)
	}

	slices.SortFunc(translations, func(a, b Translation[T]) int {
		return strings.Compare(a.Locale, b.Locale)
	})

	draft.Translations = translations
	draft.Note = strings.TrimSpace(draft.Note)

	if s.Check != nil {
		if err := s.Check(draft.Fields, draft.Translations); err != nil {
			return draft, err
		}
	}

	return draft, nil
}

func (s Spec[F, T]) slug(text T) string {
	value := reflect.ValueOf(text)

	for _, column := range columnsOf(value.Type()) {
		if column.name == s.SlugColumn {
			return value.FieldByIndex(column.index).String()
		}
	}

	return ""
}

func (s Spec[F, T]) setSlug(text *T, slug string) {
	value := reflect.ValueOf(text).Elem()

	for _, column := range columnsOf(value.Type()) {
		if column.name == s.SlugColumn {
			value.FieldByIndex(column.index).SetString(slug)
		}
	}
}

func (s Spec[F, T]) validate() error {
	if s.Table == "" || s.Complete == nil {
		return errors.New("content spec needs Table and Complete")
	}

	if s.SlugColumn != "" {
		var text T

		found := false

		for _, column := range columnsOf(reflect.TypeOf(text)) {
			found = found || column.name == s.SlugColumn && column.kind == reflect.String
		}

		if !found {
			return fmt.Errorf("content spec %s: no string column %q in translation", s.Table, s.SlugColumn)
		}
	}

	return nil
}

func (s Spec[F, T]) media(fields F, translations []Translation[T]) []string {
	if s.Media == nil {
		return nil
	}

	ids := s.Media(fields, translations)
	slices.Sort(ids)

	return slices.Compact(slices.DeleteFunc(ids, func(id string) bool { return id == "" }))
}
