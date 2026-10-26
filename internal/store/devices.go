package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type Device struct {
	ID           string `json:"id"`
	GUID         string `json:"guid"`
	UUID         string `json:"uuid"`
	Hostname     string `json:"hostname"`
	Username     string `json:"username"`
	OS           string `json:"os"`
	CPU          string `json:"cpu"`
	Memory       string `json:"memory"`
	Version      string `json:"version"`
	Note         string `json:"note"`
	IP           string `json:"ip"`
	UserID       *int64 `json:"-"`
	UserName     string `json:"user_name"`
	UserGUID     string `json:"user_guid"`
	GroupID      *int64 `json:"-"`
	GroupName    string `json:"group_name"`
	GroupGUID    string `json:"group_guid"`
	ServerID     *int64 `json:"-"`
	ServerName   string `json:"server_name"`
	ServerSlug   string `json:"server_slug"`
	StrategyName string `json:"strategy_name"`
	StrategyGUID string `json:"strategy_guid"`
	OrgID        int64  `json:"-"`
	OrgGUID      string `json:"org_guid"`
	OrgName      string `json:"org_name"`
	Conns        int    `json:"conns"`
	LastOnlineAt int64  `json:"last_online_at"`
	CreatedAt    int64  `json:"created_at"`
}

const deviceCols = `d.id, d.guid, d.uuid, d.hostname, d.username, d.os, d.cpu, d.memory, d.version, d.note, d.ip,
	d.user_id, COALESCE(u.name, ''), d.group_id, COALESCE(g.name, ''), d.server_id, COALESCE(sv.name, ''), COALESCE(sv.slug, ''), COALESCE(st.name, ''),
	COALESCE(u.guid, ''), COALESCE(g.guid, ''), COALESCE(st.guid, ''), IFNULL(d.org_id, 0), COALESCE(o.guid, ''), COALESCE(o.name, ''),
	d.conns, d.last_online_at, d.created_at`

const deviceFrom = ` FROM devices d LEFT JOIN users u ON u.id = d.user_id LEFT JOIN device_groups g ON g.id = d.group_id
	LEFT JOIN servers sv ON sv.id = d.server_id LEFT JOIN strategies st ON st.id = d.strategy_id
	LEFT JOIN organizations o ON o.id = d.org_id`

func scanDevice(row interface{ Scan(...any) error }) (*Device, error) {
	var d Device
	var uid, gid, sid sql.NullInt64
	err := row.Scan(&d.ID, &d.GUID, &d.UUID, &d.Hostname, &d.Username, &d.OS, &d.CPU, &d.Memory, &d.Version,
		&d.Note, &d.IP, &uid, &d.UserName, &gid, &d.GroupName, &sid, &d.ServerName, &d.ServerSlug, &d.StrategyName, &d.UserGUID, &d.GroupGUID, &d.StrategyGUID,
		&d.OrgID, &d.OrgGUID, &d.OrgName, &d.Conns, &d.LastOnlineAt, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if uid.Valid {
		d.UserID = &uid.Int64
	}
	if gid.Valid {
		d.GroupID = &gid.Int64
	}
	if sid.Valid {
		d.ServerID = &sid.Int64
	}
	return &d, nil
}

// SysInfo is the hardware/OS report a client uploads via /api/sysinfo.
type SysInfo struct {
	ID, UUID, Hostname, Username, OS, CPU, Memory, Version, IP string
}

// UpsertSysInfo records a device's system information, creating the device
// if needed (created reports that). Devices are bound to the uuid they first
// reported (trust on first use): a report for a known id with another uuid
// returns ErrConflict, so nobody can take over a device record. An
// administrator can delete the device to let a new machine claim the id.
func (s *Store) UpsertSysInfo(ctx context.Context, si SysInfo) (created bool, err error) {
	t := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO devices (id, guid, uuid, hostname, username, os, cpu, memory, version, ip, last_online_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO NOTHING`,
		si.ID, NewGUID(), si.UUID, si.Hostname, si.Username, si.OS, si.CPU, si.Memory, si.Version, si.IP, t, t, t)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return true, nil
	}
	res, err = s.db.ExecContext(ctx, `UPDATE devices SET uuid = ?, hostname = ?, username = ?, os = ?, cpu = ?, memory = ?,
		version = ?, ip = ?, last_online_at = ?, updated_at = ? WHERE id = ? AND (uuid = ? OR uuid = '')`,
		si.UUID, si.Hostname, si.Username, si.OS, si.CPU, si.Memory, si.Version, si.IP, t, t, si.ID, si.UUID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, ErrConflict
	}
	return false, nil
}

// DeviceMatches reports whether a device with this id and uuid is registered.
func (s *Store) DeviceMatches(ctx context.Context, id, uuid string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE id = ? AND uuid = ? AND uuid != ''`, id, uuid).Scan(&n)
	return n > 0, err
}

// Heartbeat marks a device online. It reports false when the device is unknown
// or its uuid changed, so the caller can ask the client for sysinfo.
func (s *Store) Heartbeat(ctx context.Context, id, uuid, ip string, conns int) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE devices SET last_online_at = ?, conns = ?, ip = ? WHERE id = ? AND uuid = ?`,
		now(), conns, ip, id, uuid)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) GetDevice(ctx context.Context, id string) (*Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx, `SELECT `+deviceCols+deviceFrom+` WHERE d.id = ?`, id))
}

type DeviceFilter struct {
	UserID int64 // only devices assigned to this user
	// AccessibleTo keeps devices the user owns or reaches through their user group.
	AccessibleTo int64
	GroupID      int64
	ServerID     int64
	Search       string
	// OnlineSince, when non-zero, keeps only devices seen at or after this unix time.
	OnlineSince int64
	Org         int64 // organization (0: any, NoOrg: none)
}

func (s *Store) ListDevices(ctx context.Context, f DeviceFilter, p Page) ([]*Device, int, error) {
	where := []string{"1=1"}
	var args []any
	if f.UserID != 0 {
		where = append(where, "d.user_id = ?")
		args = append(args, f.UserID)
	}
	if f.AccessibleTo != 0 {
		where = append(where, "(d.user_id = ? OR d.group_id IN "+accessibleGroups+")")
		args = append(args, f.AccessibleTo, f.AccessibleTo)
	}
	if f.GroupID != 0 {
		where = append(where, "d.group_id = ?")
		args = append(args, f.GroupID)
	}
	if f.ServerID != 0 {
		where = append(where, "d.server_id = ?")
		args = append(args, f.ServerID)
	}
	if f.Org != 0 {
		c, a := orgCond("d.org_id", f.Org)
		where = append(where, c)
		args = append(args, a...)
	}
	if f.OnlineSince != 0 {
		where = append(where, "d.last_online_at >= ?")
		args = append(args, f.OnlineSince)
	}
	if f.Search != "" {
		where = append(where, "(d.id LIKE ? OR d.hostname LIKE ? OR d.username LIKE ? OR d.note LIKE ? OR d.ip LIKE ?)")
		q := "%" + f.Search + "%"
		args = append(args, q, q, q, q, q)
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices d WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := p.limitOffset()
	rows, err := s.db.QueryContext(ctx, `SELECT `+deviceCols+deviceFrom+` WHERE `+cond+` ORDER BY d.id LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

// DeviceUpdate holds optional changes; nil fields are left untouched.
// A zero UserID / GroupID clears the assignment.
type DeviceUpdate struct {
	Note     *string
	UserID   *int64
	GroupID  *int64
	Hostname *string
	Username *string
}

func (s *Store) UpdateDevice(ctx context.Context, id string, up DeviceUpdate) error {
	var sets []string
	var args []any
	if up.Note != nil {
		sets = append(sets, "note = ?")
		args = append(args, *up.Note)
	}
	if up.Hostname != nil {
		sets = append(sets, "hostname = ?")
		args = append(args, *up.Hostname)
	}
	if up.Username != nil {
		sets = append(sets, "username = ?")
		args = append(args, *up.Username)
	}
	if up.UserID != nil {
		sets = append(sets, "user_id = ?")
		args = append(args, nullID(*up.UserID))
	}
	if up.GroupID != nil {
		sets = append(sets, "group_id = ?")
		args = append(args, nullID(*up.GroupID))
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, now(), id)
	res, err := s.db.ExecContext(ctx, `UPDATE devices SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AssignDeviceUserIfUnset binds a device to a user unless it already has an
// owner. uuid must match the device's registered uuid, so a client cannot
// claim somebody else's device by sending its id.
func (s *Store) AssignDeviceUserIfUnset(ctx context.Context, deviceID, uuid string, userID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET user_id = ?, updated_at = ? WHERE id = ? AND uuid = ? AND uuid != ''
		AND user_id IS NULL`, userID, now(), deviceID, uuid)
	return err
}

func (s *Store) DeleteDevice(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountDevices counts the devices of an organization (0: all).
func (s *Store) CountDevices(ctx context.Context, org, onlineSince int64) (total, online int, err error) {
	cond, args := orgCond("org_id", org)
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(last_online_at >= ?), 0) FROM devices WHERE `+cond,
		append([]any{onlineSince}, args...)...).Scan(&total, &online)
	return
}

// ClaimDeviceOrganization puts a device without an organization into org.
// Devices join the organization whose domain they first report to; only
// an administrator moves them afterwards.
func (s *Store) ClaimDeviceOrganization(ctx context.Context, id string, org int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET org_id = ? WHERE id = ? AND org_id IS NULL`, nullID(org), id)
	return err
}

// SetDeviceOrganization moves a device to another organization (0: none),
// dropping its owner, group and strategy, which belong to the old one.
func (s *Store) SetDeviceOrganization(ctx context.Context, id string, org int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE devices SET org_id = ?, user_id = NULL, group_id = NULL, strategy_id = NULL,
		updated_at = ? WHERE id = ?`, nullID(org), now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

type DeviceGroup struct {
	ID           int64  `json:"-"`
	GUID         string `json:"guid"`
	Name         string `json:"name"`
	Note         string `json:"note"`
	StrategyName string `json:"strategy_name"`
	StrategyGUID string `json:"strategy_guid"`
	OrgID        int64  `json:"-"`
	Devices      int    `json:"devices"`
	CreatedAt    int64  `json:"created_at"`
}

func scanGroup(row interface{ Scan(...any) error }) (*DeviceGroup, error) {
	var g DeviceGroup
	err := row.Scan(&g.ID, &g.GUID, &g.Name, &g.Note, &g.StrategyName, &g.StrategyGUID, &g.OrgID, &g.Devices, &g.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &g, err
}

const groupSelect = `SELECT g.id, g.guid, g.name, g.note,
	COALESCE((SELECT name FROM strategies st WHERE st.id = g.strategy_id), ''),
	COALESCE((SELECT guid FROM strategies st WHERE st.id = g.strategy_id), ''), IFNULL(g.org_id, 0), (SELECT COUNT(*) FROM devices d WHERE d.group_id = g.id), g.created_at FROM device_groups g`

// CreateDeviceGroup creates a group in an organization (0: none).
func (s *Store) CreateDeviceGroup(ctx context.Context, org int64, name, note string) (*DeviceGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("group name is required")
	}
	guid := NewGUID()
	_, err := s.db.ExecContext(ctx, `INSERT INTO device_groups (guid, name, note, org_id, created_at) VALUES (?, ?, ?, ?, ?)`,
		guid, name, note, nullID(org), now())
	if isUniqueErr(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return s.DeviceGroupByGUID(ctx, guid)
}

func (s *Store) UpdateDeviceGroup(ctx context.Context, guid, name, note string) (*DeviceGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("group name is required")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE device_groups SET name = ?, note = ? WHERE guid = ?`, name, note, guid)
	if isUniqueErr(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.DeviceGroupByGUID(ctx, guid)
}

func (s *Store) DeleteDeviceGroup(ctx context.Context, guid string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM device_groups WHERE guid = ?`, guid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeviceGroupByName finds a group by name within an organization (0: none).
func (s *Store) DeviceGroupByName(ctx context.Context, org int64, name string) (*DeviceGroup, error) {
	return scanGroup(s.db.QueryRowContext(ctx, groupSelect+` WHERE g.name = ? AND IFNULL(g.org_id, 0) = ?`, strings.TrimSpace(name), org))
}

func (s *Store) DeviceGroupByGUID(ctx context.Context, guid string) (*DeviceGroup, error) {
	return scanGroup(s.db.QueryRowContext(ctx, groupSelect+` WHERE g.guid = ?`, guid))
}

// accessibleGroups selects the device groups a user (bound twice) can reach
// through their user group.
const accessibleGroups = `(SELECT a.device_group_id FROM user_group_access a JOIN users u ON u.group_id = a.user_group_id WHERE u.id = ?)`

// ListDeviceGroups returns all groups, or, when userID is non-zero, the groups
// that user can reach: granted to their user group, or holding their devices.
// org restricts the list to an organization (0: any, NoOrg: none).
func (s *Store) ListDeviceGroups(ctx context.Context, org, userID int64, p Page) ([]*DeviceGroup, int, error) {
	cond, args := orgCond("g.org_id", org)
	if userID != 0 {
		cond += " AND (EXISTS (SELECT 1 FROM devices d WHERE d.group_id = g.id AND d.user_id = ?) OR g.id IN " + accessibleGroups + ")"
		args = append(args, userID, userID)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_groups g WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := p.limitOffset()
	rows, err := s.db.QueryContext(ctx, groupSelect+` WHERE `+cond+` ORDER BY g.name LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*DeviceGroup
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, g)
	}
	return out, total, rows.Err()
}
