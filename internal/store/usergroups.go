package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// UserGroup gathers users; its members can reach the device groups it is granted.
type UserGroup struct {
	ID           int64    `json:"-"`
	GUID         string   `json:"guid"`
	Name         string   `json:"name"`
	Note         string   `json:"note"`
	Users        int      `json:"users"`
	DeviceGroups []string `json:"device_groups"` // guids of granted device groups
	CreatedAt    int64    `json:"created_at"`
}

const userGroupSelect = `SELECT g.id, g.guid, g.name, g.note, (SELECT COUNT(*) FROM users u WHERE u.group_id = g.id), g.created_at
	FROM user_groups g`

func scanUserGroup(row interface{ Scan(...any) error }) (*UserGroup, error) {
	var g UserGroup
	err := row.Scan(&g.ID, &g.GUID, &g.Name, &g.Note, &g.Users, &g.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &g, err
}

func (s *Store) loadAccess(ctx context.Context, groups []*UserGroup) error {
	for _, g := range groups {
		rows, err := s.db.QueryContext(ctx, `SELECT d.guid FROM user_group_access a JOIN device_groups d ON d.id = a.device_group_id
			WHERE a.user_group_id = ? ORDER BY d.name`, g.ID)
		if err != nil {
			return err
		}
		g.DeviceGroups = []string{}
		for rows.Next() {
			var guid string
			if err := rows.Scan(&guid); err != nil {
				rows.Close()
				return err
			}
			g.DeviceGroups = append(g.DeviceGroups, guid)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListUserGroups(ctx context.Context) ([]*UserGroup, error) {
	rows, err := s.db.QueryContext(ctx, userGroupSelect+` ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	out := []*UserGroup{}
	for rows.Next() {
		g, err := scanUserGroup(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.loadAccess(ctx, out)
}

func (s *Store) UserGroupByGUID(ctx context.Context, guid string) (*UserGroup, error) {
	g, err := scanUserGroup(s.db.QueryRowContext(ctx, userGroupSelect+` WHERE g.guid = ?`, guid))
	if err != nil {
		return nil, err
	}
	return g, s.loadAccess(ctx, []*UserGroup{g})
}

// SaveUserGroup creates (guid == "") or updates a user group and replaces its
// device group access list (device group guids).
func (s *Store) SaveUserGroup(ctx context.Context, guid, name, note string, deviceGroups []string) (*UserGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("group name is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if guid == "" {
		guid = NewGUID()
		_, err = tx.ExecContext(ctx, `INSERT INTO user_groups (guid, name, note, created_at) VALUES (?, ?, ?, ?)`, guid, name, note, now())
	} else {
		var res sql.Result
		res, err = tx.ExecContext(ctx, `UPDATE user_groups SET name = ?, note = ? WHERE guid = ?`, name, note, guid)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 0 {
				return nil, ErrNotFound
			}
		}
	}
	if isUniqueErr(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	var gid int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM user_groups WHERE guid = ?`, guid).Scan(&gid); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_group_access WHERE user_group_id = ?`, gid); err != nil {
		return nil, err
	}
	for _, dg := range deviceGroups {
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_group_access (user_group_id, device_group_id)
			SELECT ?, id FROM device_groups WHERE guid = ?`, gid, dg)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			var exists int
			tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_groups WHERE guid = ?`, dg).Scan(&exists)
			if exists == 0 {
				return nil, errors.New("device group " + dg + " not found")
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.UserGroupByGUID(ctx, guid)
}

func (s *Store) DeleteUserGroup(ctx context.Context, guid string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM user_groups WHERE guid = ?`, guid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetUserGroup puts a user into a group (0 removes them from any group).
func (s *Store) SetUserGroup(ctx context.Context, userID, groupID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET group_id = ?, updated_at = ? WHERE id = ?`, nullID(groupID), now(), userID)
	return err
}
