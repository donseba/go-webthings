package pages

import (
	"context"
	"slices"
)

// MoveTo places a page at index among the children of parentID in the
// latest tree, 0 being the first place; an index past the last place puts
// it last. author is the user who moves the page.
//
// Below the page's current parent this is a reorder like Move: positions are
// not revisioned, so the published menu follows at once. Below another
// parent the page's address changes, so MoveTo saves a new draft revision
// with that parent, as choosing another parent in the editor does: the
// published menu keeps the page at its old place until the draft is
// published. MoveTo reports whether it saved a draft.
//
// The same rules as for Save apply: a page cannot go below itself or the
// pages below it (ErrCycle), nor below a module page (ErrModuleParent);
// every page except Home needs a parent (ErrParent), and Home stays the root
// (ErrCycle, as every page is below it).
func (s *Service) MoveTo(ctx context.Context, id, parentID string, index int, author string) (bool, error) {
	if index < 0 {
		return false, ErrInvalid
	}

	defer s.table.Invalidate()

	drafted := false

	err := s.db.Tx(ctx, func(ctx context.Context) error {
		latest, err := s.outline(ctx)
		if err != nil {
			return err
		}

		at := slices.IndexFunc(latest, func(page Page) bool {
			return page.ID == id
		})
		if at < 0 {
			return ErrNotFound
		}

		page := latest[at]

		switch {
		case page.Fields.ParentID == "" && parentID == "":
			return nil
		case page.Fields.ParentID == "":
			return ErrCycle
		case parentID == "":
			return ErrParent
		}

		if parentID != page.Fields.ParentID {
			// The outline lacks the bodies the new revision keeps.
			full, err := s.store.Latest(ctx, instance, id)
			if err != nil {
				return err
			}

			// Save puts the page last below its new parent and keeps its
			// published place in the menu; the renumbering below then moves
			// it to index in the admin's tree.
			_, err = s.Save(ctx, Draft{
				ID: page.ID,
				Fields: Fields{
					ParentID: parentID,
					Module:   page.Fields.Module,
					Position: page.Fields.Position,
				},
				Translations: full.Translations,
				Author:       author,
			})
			if err != nil {
				return err
			}

			drafted = true

			latest, err = s.outline(ctx)
			if err != nil {
				return err
			}
		}

		return s.renumber(ctx, children(latest)[parentID], id, index)
	})

	return drafted, err
}

// renumber moves the page id among siblings, in sibling order, to index and
// numbers them 1, 2, 3 and so on, which also settles equal positions. It
// rebuilds the published routes, as the menu follows positions at once.
func (s *Service) renumber(ctx context.Context, siblings []Page, id string, index int) error {
	from := slices.IndexFunc(siblings, func(page Page) bool {
		return page.ID == id
	})
	if from < 0 {
		return ErrNotFound
	}

	moved := slices.Delete(slices.Clone(siblings), from, from+1)
	moved = slices.Insert(moved, min(index, len(moved)), siblings[from])

	for i, page := range moved {
		if page.Fields.Position == i+1 {
			continue
		}

		if _, err := s.db.Exec(ctx, `UPDATE wt_page SET position=? WHERE id=?`, i+1, page.ID); err != nil {
			return err
		}
	}

	return s.rebuild(ctx)
}
