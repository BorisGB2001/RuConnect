package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
)

// DefaultServerSlug names the site used by clients whose API URL has no /s/<slug> prefix.
const DefaultServerSlug = "default"

// Server is one hbbs/hbbr site. Devices registered with that site's hbbs are
// reachable from clients of other sites as "ID@id_server?key=key".
type Server struct {
	ID          int64  `json:"-"`
	GUID        string `json:"guid"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	IDServer    string `json:"id_server"`
	RelayServer string `json:"relay_server"`
	Key         string `json:"key"`
	Note        string `json:"note"`
	Devices     int    `json:"devices"`
	IsDefault   bool   `json:"is_default"`
	CreatedAt   int64  `json:"created_at"`
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

const serverSelect = `SELECT s.id, s.guid, s.name, s.slug, s.id_server, s.relay_server, s.key, s.note,
	(SELECT COUNT(*) FROM devices d WHERE d.server_id = s.id), s.created_at FROM servers s`

func scanServer(row interface{ Scan(...any) error }) (*Server, error) {
	var v Server
	err := row.Scan(&v.ID, &v.GUID, &v.Name, &v.Slug, &v.IDServer, &v.RelayServer, &v.Key, &v.Note, &v.Devices, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.IsDefault = v.Slug == DefaultServerSlug
	return &v, nil
}

type ServerInput struct {
	Name, Slug, IDServer, RelayServer, Key, Note string
}

func (in *ServerInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Slug = strings.ToLower(strings.TrimSpace(in.Slug))
	in.IDServer = strings.TrimSpace(in.IDServer)
	in.RelayServer = strings.TrimSpace(in.RelayServer)
	in.Key = strings.TrimSpace(in.Key)
	if in.Name == "" {
		return errors.New("server name is required")
	}
	if !slugRe.MatchString(in.Slug) {
		return errors.New("slug must be 1-63 characters: lowercase letters, digits, '-' or '_'")
	}
	if strings.ContainsAny(in.IDServer, "@?&/ ") {
		return errors.New("ID server must be a host name or host:port")
	}
	if strings.ContainsAny(in.Key, "&? ") {
		return errors.New("key contains invalid characters")
	}
	return nil
}

// EnsureDefaultServer creates the default site on first start. Its addresses
// are left empty so they follow RUCONNECT_ID_SERVER / RUCONNECT_RELAY_SERVER
// until an administrator overrides them in the panel.
func (s *Store) EnsureDefaultServer(ctx context.Context) (*Server, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO servers (guid, name, slug, created_at)
		VALUES (?, 'Default', ?, ?) ON CONFLICT(slug) DO NOTHING`, NewGUID(), DefaultServerSlug, now())
	if err != nil {
		return nil, err
	}
	return s.ServerBySlug(ctx, DefaultServerSlug)
}

func (s *Store) CreateServer(ctx context.Context, in ServerInput) (*Server, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	guid := NewGUID()
	_, err := s.db.ExecContext(ctx, `INSERT INTO servers (guid, name, slug, id_server, relay_server, key, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, guid, in.Name, in.Slug, in.IDServer, in.RelayServer, in.Key, in.Note, now())
	if isUniqueErr(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return s.ServerByGUID(ctx, guid)
}

func (s *Store) UpdateServer(ctx context.Context, guid string, in ServerInput) (*Server, error) {
	cur, err := s.ServerByGUID(ctx, guid)
	if err != nil {
		return nil, err
	}
	if cur.IsDefault {
		in.Slug = DefaultServerSlug // clients without a prefix depend on it
	}
	if err := in.normalize(); err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE servers SET name = ?, slug = ?, id_server = ?, relay_server = ?, key = ?, note = ?
		WHERE id = ?`, in.Name, in.Slug, in.IDServer, in.RelayServer, in.Key, in.Note, cur.ID)
	if isUniqueErr(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return s.ServerByGUID(ctx, guid)
}

func (s *Store) DeleteServer(ctx context.Context, guid string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM servers WHERE guid = ? AND slug != ?`, guid, DefaultServerSlug)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ServerByGUID(ctx context.Context, guid string) (*Server, error) {
	return scanServer(s.db.QueryRowContext(ctx, serverSelect+` WHERE s.guid = ?`, guid))
}

func (s *Store) ServerBySlug(ctx context.Context, slug string) (*Server, error) {
	return scanServer(s.db.QueryRowContext(ctx, serverSelect+` WHERE s.slug = ?`, strings.ToLower(slug)))
}

// ListServers returns all sites, the default one first.
func (s *Store) ListServers(ctx context.Context) ([]*Server, error) {
	rows, err := s.db.QueryContext(ctx, serverSelect+` ORDER BY s.slug != ?, s.name`, DefaultServerSlug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Server{}
	for rows.Next() {
		v, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetDeviceServer records which site a device last reported through.
func (s *Store) SetDeviceServer(ctx context.Context, deviceID string, serverID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET server_id = ? WHERE id = ? AND (server_id IS NULL OR server_id != ?)`,
		serverID, deviceID, serverID)
	return err
}

// DeviceServers maps device ids to their site ids for the given ids.
func (s *Store) DeviceServers(ctx context.Context, ids []string) (map[string]int64, error) {
	out := map[string]int64{}
	if len(ids) == 0 {
		return out, nil
	}
	q := `SELECT id, server_id FROM devices WHERE server_id IS NOT NULL AND id IN (?` + strings.Repeat(",?", len(ids)-1) + `)`
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var sid int64
		if err := rows.Scan(&id, &sid); err != nil {
			return nil, err
		}
		out[id] = sid
	}
	return out, rows.Err()
}
