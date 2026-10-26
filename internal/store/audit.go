package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type ConnAudit struct {
	ID           int64  `json:"-"`
	GUID         string `json:"guid"`
	DeviceID     string `json:"device_id"`
	ConnID       int64  `json:"conn_id"`
	SessionID    string `json:"session_id"`
	IP           string `json:"ip"`
	PeerID       string `json:"peer_id"`
	PeerName     string `json:"peer_name"`
	ConnType     int    `json:"conn_type"`
	PrimaryAuth  int    `json:"primary_auth"`
	TwoFactor    int    `json:"two_factor"`
	ConnAuditRef string `json:"conn_audit_ref"`
	Note         string `json:"note"`
	CreatedAt    int64  `json:"created_at"`
	AuthorizedAt int64  `json:"authorized_at"`
	ClosedAt     int64  `json:"closed_at"`
}

// ConnEvent is one /api/audit/conn record from a controlled device.
type ConnEvent struct {
	DeviceID, DeviceUUID string
	ConnID               int64
	SessionID            string
	Action               string // "new", "close", or "" for the "authorized" record
	IP                   string
	ConnAuditRef         string
	PeerID, PeerName     string
	ConnType             int
	PrimaryAuth          int
	TwoFactor            int
}

// openConn finds the not yet closed audit row of a device connection.
func (s *Store) openConn(ctx context.Context, e ConnEvent) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM audit_conns WHERE device_id = ? AND conn_id = ? AND closed_at = 0
		AND (session_id = ? OR session_id = '' OR ? = '') ORDER BY id DESC LIMIT 1`,
		e.DeviceID, e.ConnID, e.SessionID, e.SessionID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// RecordConnEvent folds a connection event into a single per-session audit row.
func (s *Store) RecordConnEvent(ctx context.Context, e ConnEvent) error {
	t := now()
	id, err := s.openConn(ctx, e)
	if err != nil {
		return err
	}
	switch e.Action {
	case "new":
		if id != 0 {
			// conn ids restart with the client process; close the stale row.
			if _, err := s.db.ExecContext(ctx, `UPDATE audit_conns SET closed_at = ? WHERE id = ?`, t, id); err != nil {
				return err
			}
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO audit_conns (guid, device_id, device_uuid, conn_id, session_id, ip, conn_audit_ref, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, NewGUID(), e.DeviceID, e.DeviceUUID, e.ConnID, e.SessionID, e.IP, e.ConnAuditRef, t)
		return err
	case "close":
		if id == 0 {
			return nil
		}
		_, err = s.db.ExecContext(ctx, `UPDATE audit_conns SET closed_at = ? WHERE id = ?`, t, id)
		return err
	default: // authorized
		if id == 0 {
			_, err = s.db.ExecContext(ctx, `INSERT INTO audit_conns (guid, device_id, device_uuid, conn_id, session_id, peer_id, peer_name,
					conn_type, primary_auth, two_factor, created_at, authorized_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, NewGUID(), e.DeviceID, e.DeviceUUID, e.ConnID, e.SessionID,
				e.PeerID, e.PeerName, e.ConnType, e.PrimaryAuth, e.TwoFactor, t, t)
			return err
		}
		_, err = s.db.ExecContext(ctx, `UPDATE audit_conns SET peer_id = ?, peer_name = ?, conn_type = ?, primary_auth = ?,
			two_factor = ?, authorized_at = ?, session_id = CASE WHEN session_id = '' THEN ? ELSE session_id END WHERE id = ?`,
			e.PeerID, e.PeerName, e.ConnType, e.PrimaryAuth, e.TwoFactor, t, e.SessionID, id)
		return err
	}
}

// ConnGUIDBySession returns the GUID of the newest audit row of a session on a device.
func (s *Store) ConnGUIDBySession(ctx context.Context, deviceID, sessionID string) (string, error) {
	var guid string
	err := s.db.QueryRowContext(ctx, `SELECT guid FROM audit_conns WHERE device_id = ? AND session_id = ?
		ORDER BY id DESC LIMIT 1`, deviceID, sessionID).Scan(&guid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return guid, err
}

func (s *Store) SetConnNoteBySession(ctx context.Context, deviceID, sessionID, note string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE audit_conns SET note = ? WHERE id = (SELECT id FROM audit_conns
		WHERE device_id = ? AND session_id = ? ORDER BY id DESC LIMIT 1)`, note, deviceID, sessionID)
	return err
}

func (s *Store) SetConnNote(ctx context.Context, guid, note string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE audit_conns SET note = ? WHERE guid = ?`, note, guid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AuditFilter narrows audit listings. Zero values mean "any".
type AuditFilter struct {
	DeviceID string
	PeerID   string
	From, To int64
}

func (f AuditFilter) where(peerCol string) (string, []any) {
	where := []string{"1=1"}
	var args []any
	if f.DeviceID != "" {
		where = append(where, "device_id = ?")
		args = append(args, f.DeviceID)
	}
	if f.PeerID != "" && peerCol != "" {
		where = append(where, peerCol+" = ?")
		args = append(args, f.PeerID)
	}
	if f.From != 0 {
		where = append(where, "created_at >= ?")
		args = append(args, f.From)
	}
	if f.To != 0 {
		where = append(where, "created_at <= ?")
		args = append(args, f.To)
	}
	return strings.Join(where, " AND "), args
}

func (s *Store) ListConnAudits(ctx context.Context, f AuditFilter, p Page) ([]*ConnAudit, int, error) {
	cond, args := f.where("peer_id")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_conns WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := p.limitOffset()
	rows, err := s.db.QueryContext(ctx, `SELECT id, guid, device_id, conn_id, session_id, ip, peer_id, peer_name, conn_type,
		primary_auth, two_factor, conn_audit_ref, note, created_at, authorized_at, closed_at FROM audit_conns WHERE `+cond+`
		ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*ConnAudit{}
	for rows.Next() {
		var a ConnAudit
		if err := rows.Scan(&a.ID, &a.GUID, &a.DeviceID, &a.ConnID, &a.SessionID, &a.IP, &a.PeerID, &a.PeerName, &a.ConnType,
			&a.PrimaryAuth, &a.TwoFactor, &a.ConnAuditRef, &a.Note, &a.CreatedAt, &a.AuthorizedAt, &a.ClosedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, &a)
	}
	return out, total, rows.Err()
}

type FileAudit struct {
	ID        int64  `json:"id"`
	DeviceID  string `json:"device_id"`
	PeerID    string `json:"peer_id"`
	ConnID    int64  `json:"conn_id"`
	Type      int    `json:"type"`
	Path      string `json:"path"`
	IsFile    bool   `json:"is_file"`
	PeerIP    string `json:"peer_ip"`
	PeerName  string `json:"peer_name"`
	Num       int64  `json:"num"`
	Files     string `json:"files"` // JSON array of [name, size]
	CreatedAt int64  `json:"created_at"`
}

func (s *Store) AddFileAudit(ctx context.Context, a FileAudit) error {
	if a.Files == "" {
		a.Files = "[]"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_files (device_id, peer_id, conn_id, type, path, is_file, peer_ip, peer_name, num, files, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, a.DeviceID, a.PeerID, a.ConnID, a.Type, a.Path, boolInt(a.IsFile),
		a.PeerIP, a.PeerName, a.Num, a.Files, now())
	return err
}

func (s *Store) ListFileAudits(ctx context.Context, f AuditFilter, p Page) ([]*FileAudit, int, error) {
	cond, args := f.where("peer_id")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_files WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := p.limitOffset()
	rows, err := s.db.QueryContext(ctx, `SELECT id, device_id, peer_id, conn_id, type, path, is_file, peer_ip, peer_name, num, files, created_at
		FROM audit_files WHERE `+cond+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*FileAudit{}
	for rows.Next() {
		var a FileAudit
		var isFile int
		if err := rows.Scan(&a.ID, &a.DeviceID, &a.PeerID, &a.ConnID, &a.Type, &a.Path, &isFile, &a.PeerIP, &a.PeerName,
			&a.Num, &a.Files, &a.CreatedAt); err != nil {
			return nil, 0, err
		}
		a.IsFile = isFile != 0
		out = append(out, &a)
	}
	return out, total, rows.Err()
}

type AlarmAudit struct {
	ID        int64  `json:"id"`
	DeviceID  string `json:"device_id"`
	ConnID    int64  `json:"conn_id"`
	Typ       int    `json:"typ"`
	IP        string `json:"ip"`
	Info      string `json:"info"` // raw JSON object from the client
	CreatedAt int64  `json:"created_at"`
}

func (s *Store) AddAlarmAudit(ctx context.Context, a AlarmAudit) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_alarms (device_id, conn_id, typ, ip, info, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		a.DeviceID, a.ConnID, a.Typ, a.IP, a.Info, now())
	return err
}

func (s *Store) ListAlarmAudits(ctx context.Context, f AuditFilter, p Page) ([]*AlarmAudit, int, error) {
	cond, args := f.where("")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_alarms WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := p.limitOffset()
	rows, err := s.db.QueryContext(ctx, `SELECT id, device_id, conn_id, typ, ip, info, created_at FROM audit_alarms WHERE `+cond+`
		ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*AlarmAudit{}
	for rows.Next() {
		var a AlarmAudit
		if err := rows.Scan(&a.ID, &a.DeviceID, &a.ConnID, &a.Typ, &a.IP, &a.Info, &a.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, &a)
	}
	return out, total, rows.Err()
}

// PruneAudit deletes audit records older than the given unix time.
func (s *Store) PruneAudit(ctx context.Context, before int64) error {
	for _, t := range []string{"audit_conns", "audit_files", "audit_alarms"} {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM `+t+` WHERE created_at < ?`, before); err != nil {
			return err
		}
	}
	return nil
}
