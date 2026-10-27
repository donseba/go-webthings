package auth

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/donseba/go-webthings/db"
)

// Actions in the permission matrix, the columns a group's rights are set in
// for every section.
const (
	// Read opens a section and its items.
	Read = "read"
	// Create adds new items.
	Create = "create"
	// Edit changes items.
	Edit = "edit"
	// Publish makes changes visible on the public site, or hides them.
	Publish = "publish"
	// Delete moves items to the trash, and brings them back or deletes
	// them for good there.
	Delete = "delete"
	// Review approves or rejects revisions submitted for review, in sections
	// whose review workflow is on (see content.Review).
	Review = "review"
	// Export downloads the personal data a section holds, such as a
	// newsletter's subscribers or the registrations of an event, as CSV.
	Export = "export"
)

// Actions lists the matrix columns in order.
var Actions = []string{Read, Create, Edit, Publish, Delete, Review, Export}

// ErrGroupName is a group name that is empty or longer than 100
// characters.
var ErrGroupName = errors.New("invalid_group_name")

// Group gives its members rights: allowed actions per section (Pages, Media,
// a module instance, ...). Administrators do not need groups.
type Group struct {
	ID          string      `db:"id"`
	Name        string      `db:"name"`
	CreatedAt   time.Time   `db:"created_at"`
	CreatedBy   db.Ref      `db:"created_by"`
	UpdatedAt   time.Time   `db:"updated_at"`
	UpdatedBy   db.Ref      `db:"updated_by"`
	Members     []string    `db:"-"`
	Permissions Permissions `db:"-"`
}

// groupColumns are the columns of Group.
const groupColumns = `id, name, created_at, created_by, updated_at, updated_by`

// Permissions maps a section ID to its allowed actions.
type Permissions map[string][]string

// Allows reports whether action is allowed in section.
func (p Permissions) Allows(section, action string) bool {
	return slices.Contains(p[section], action)
}

// add allows action in section.
func (p Permissions) add(section, action string) {
	if !p.Allows(section, action) {
		p[section] = append(p[section], action)
	}
}

// Groups lists all groups by name, with members and permissions.
func (s *Service) Groups(ctx context.Context) ([]Group, error) {
	var groups []Group

	if err := s.db.Select(ctx, &groups, `SELECT `+groupColumns+` FROM wt_groups ORDER BY name`); err != nil {
		return nil, err
	}

	for i := range groups {
		if err := s.loadGroup(ctx, &groups[i]); err != nil {
			return nil, err
		}
	}

	return groups, nil
}

// Group returns one group with its members and permissions.
func (s *Service) Group(ctx context.Context, id string) (Group, error) {
	var group Group

	err := s.db.Get(ctx, &group, `SELECT `+groupColumns+` FROM wt_groups WHERE id=?`, id)
	if errors.Is(err, db.ErrNotFound) {
		return Group{}, ErrNotFound
	}

	if err != nil {
		return Group{}, err
	}

	return group, s.loadGroup(ctx, &group)
}

func (s *Service) loadGroup(ctx context.Context, group *Group) error {
	err := s.db.Select(ctx, &group.Members, `SELECT user_id FROM wt_group_members WHERE group_id=?`, group.ID)
	if err != nil {
		return err
	}

	var rows []struct {
		Section string `db:"section"`
		Action  string `db:"action"`
	}

	err = s.db.Select(ctx, &rows, `SELECT section, action FROM wt_group_permissions WHERE group_id=?`, group.ID)
	if err != nil {
		return err
	}

	group.Permissions = Permissions{}

	for _, row := range rows {
		group.Permissions.add(row.Section, row.Action)
	}

	return nil
}

// SaveGroup creates a group (empty ID) or renames it and replaces its permissions.
func (s *Service) SaveGroup(ctx context.Context, group Group) (Group, error) {
	group.Name = strings.TrimSpace(group.Name)
	if group.Name == "" || len(group.Name) > 100 {
		return Group{}, ErrGroupName
	}

	err := s.db.Tx(ctx, func(ctx context.Context) error {
		now := s.now().UTC()

		if group.ID == "" {
			group.ID = db.NewID()

			_, err := s.db.Exec(ctx,
				`INSERT INTO wt_groups(id,name,created_at,created_by,updated_at,updated_by) VALUES(?,?,?,?,?,?)`,
				group.ID, group.Name, now, db.Actor(ctx), now, db.Actor(ctx),
			)
			if err != nil {
				return err
			}
		} else {
			result, err := s.db.Exec(ctx,
				`UPDATE wt_groups SET name=?, updated_at=?, updated_by=? WHERE id=?`,
				group.Name, now, db.Actor(ctx), group.ID,
			)
			if err := changed(result, err); err != nil {
				return err
			}
		}

		if _, err := s.db.Exec(ctx, `DELETE FROM wt_group_permissions WHERE group_id=?`, group.ID); err != nil {
			return err
		}

		for section, actions := range group.Permissions {
			for _, action := range actions {
				if !slices.Contains(Actions, action) {
					continue
				}

				_, err := s.db.Exec(ctx,
					`INSERT INTO wt_group_permissions(group_id,section,action) VALUES(?,?,?)`,
					group.ID, section, action,
				)
				if err != nil {
					return err
				}
			}
		}

		return nil
	})
	if db.IsUnique(err) {
		return Group{}, ErrDuplicate
	}

	if err != nil {
		return Group{}, err
	}

	return s.Group(ctx, group.ID)
}

// DeleteGroup removes a group; its members keep their other groups.
func (s *Service) DeleteGroup(ctx context.Context, id string) error {
	result, err := s.db.Exec(ctx, `DELETE FROM wt_groups WHERE id=?`, id)

	return changed(result, err)
}

// UserGroups lists the IDs of the groups a user belongs to.
func (s *Service) UserGroups(ctx context.Context, userID string) ([]string, error) {
	var ids []string

	err := s.db.Select(ctx, &ids, `SELECT group_id FROM wt_group_members WHERE user_id=?`, userID)

	return ids, err
}

// SetUserGroups replaces the groups a user belongs to.
func (s *Service) SetUserGroups(ctx context.Context, userID string, groupIDs []string) error {
	return s.db.Tx(ctx, func(ctx context.Context) error {
		if _, err := s.db.Exec(ctx, `DELETE FROM wt_group_members WHERE user_id=?`, userID); err != nil {
			return err
		}

		for _, id := range groupIDs {
			_, err := s.db.Exec(ctx, `INSERT INTO wt_group_members(group_id,user_id) VALUES(?,?)`, id, userID)
			if db.IsForeignKey(err) || errors.Is(err, db.ErrNotFound) {
				return ErrNotFound
			}

			if err != nil {
				return err
			}
		}

		return nil
	})
}

// Permissions returns what a user may do: the combined rights of their groups.
// Administrators may do everything and get nil; check IsAdmin first.
func (s *Service) Permissions(ctx context.Context, user User) (Permissions, error) {
	var rows []struct {
		Section string `db:"section"`
		Action  string `db:"action"`
	}

	err := s.db.Select(ctx, &rows, `
		SELECT DISTINCT p.section, p.action
		FROM wt_group_permissions p
		JOIN wt_group_members m ON m.group_id = p.group_id
		WHERE m.user_id=?`,
		user.ID,
	)
	if err != nil {
		return nil, err
	}

	permissions := Permissions{}

	for _, row := range rows {
		permissions.add(row.Section, row.Action)
	}

	return permissions, nil
}

// IsAdmin reports whether the user may do everything, including managing
// accounts and groups.
func (u User) IsAdmin() bool {
	return u.Role == Admin
}

// SetRole makes a user an administrator or an editor. The last active
// administrator cannot become an editor.
func (s *Service) SetRole(ctx context.Context, userID string, role Role) error {
	if role != Admin && role != Editor {
		return ErrNotFound
	}

	return s.db.Tx(ctx, func(ctx context.Context) error {
		if role == Editor {
			var others int

			err := s.db.Get(ctx, &others,
				`SELECT COUNT(*) FROM wt_users WHERE id<>? AND role=? AND status=?`,
				userID, Admin, Active,
			)
			if err != nil {
				return err
			}

			if others == 0 {
				return ErrLastAdmin
			}
		}

		result, err := s.db.Exec(ctx,
			`UPDATE wt_users SET role=?, updated_at=?, updated_by=? WHERE id=?`,
			role, s.now().UTC(), db.Actor(ctx), userID,
		)

		return changed(result, err)
	})
}

// changed returns ErrNotFound when a statement matched no row.
func changed(result sql.Result, err error) error {
	err = db.Affected(result, err)
	if errors.Is(err, db.ErrNotFound) {
		return ErrNotFound
	}

	return err
}
