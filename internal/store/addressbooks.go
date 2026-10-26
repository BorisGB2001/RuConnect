package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// Address book share rules, as understood by the RustDesk client.
const (
	RuleNone  = 0
	RuleRead  = 1
	RuleWrite = 2
	RuleFull  = 3
)

type AddressBook struct {
	ID        int64  `json:"-"`
	GUID      string `json:"guid"`
	Name      string `json:"name"`
	OwnerID   int64  `json:"-"`
	Owner     string `json:"owner"`
	OwnerGUID string `json:"owner_guid"`
	Personal  bool   `json:"personal"`
	Note      string `json:"note"`
	Password  string `json:"password"`
	Peers     int    `json:"peers"`
	CreatedAt int64  `json:"created_at"`
	OrgID     int64  `json:"-"`
	OrgGUID   string `json:"org_guid"`
	OrgName   string `json:"org_name"`
	// Rule is the effective rule of the user the book was loaded for.
	Rule int `json:"rule"`
}

const abSelect = `SELECT a.id, a.guid, a.name, a.owner_id, COALESCE(u.name, ''), COALESCE(u.guid, ''), a.personal, a.note, a.password,
	(SELECT COUNT(*) FROM ab_peers p WHERE p.ab_id = a.id), a.created_at, IFNULL(a.org_id, 0), COALESCE((SELECT o.guid FROM organizations o WHERE o.id = a.org_id), ''), COALESCE((SELECT o.name FROM organizations o WHERE o.id = a.org_id), '')
	FROM address_books a LEFT JOIN users u ON u.id = a.owner_id`

func scanAB(row interface{ Scan(...any) error }) (*AddressBook, error) {
	var a AddressBook
	var personal int
	err := row.Scan(&a.ID, &a.GUID, &a.Name, &a.OwnerID, &a.Owner, &a.OwnerGUID, &personal, &a.Note, &a.Password, &a.Peers, &a.CreatedAt, &a.OrgID, &a.OrgGUID, &a.OrgName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.Personal = personal != 0
	return &a, nil
}

// PersonalAB returns the user's personal address book, creating it on first use.
func (s *Store) PersonalAB(ctx context.Context, userID int64) (*AddressBook, error) {
	a, err := scanAB(s.db.QueryRowContext(ctx, abSelect+` WHERE a.owner_id = ? AND a.personal = 1`, userID))
	if errors.Is(err, ErrNotFound) {
		t := now()
		_, err = s.db.ExecContext(ctx, `INSERT INTO address_books (guid, name, owner_id, personal, created_at, updated_at)
			VALUES (?, 'My address book', ?, 1, ?, ?) ON CONFLICT DO NOTHING`, NewGUID(), userID, t, t)
		if err != nil {
			return nil, err
		}
		a, err = scanAB(s.db.QueryRowContext(ctx, abSelect+` WHERE a.owner_id = ? AND a.personal = 1`, userID))
	}
	if err != nil {
		return nil, err
	}
	a.Rule = RuleFull
	return a, nil
}

func (s *Store) ABByGUID(ctx context.Context, guid string) (*AddressBook, error) {
	return scanAB(s.db.QueryRowContext(ctx, abSelect+` WHERE a.guid = ?`, guid))
}

// SharedABByName finds a shared book by name within an organization (0: none).
func (s *Store) SharedABByName(ctx context.Context, org int64, name string) (*AddressBook, error) {
	return scanAB(s.db.QueryRowContext(ctx, abSelect+` WHERE a.personal = 0 AND a.name = ? COLLATE NOCASE AND IFNULL(a.org_id, 0) = ?`,
		strings.TrimSpace(name), org))
}

// adminOf reports whether u administers book a: server administrators
// administer every book, organization administrators those of their organization.
func adminOf(u *User, a *AddressBook) bool {
	return u.IsAdmin && (u.OrgID == 0 || u.OrgID == a.OrgID)
}

// ABForUser loads a book and the user's effective rule on it.
// Owners and administrators get full control of shared books; personal
// books are only visible to their owner.
func (s *Store) ABForUser(ctx context.Context, guid string, u *User) (*AddressBook, error) {
	a, err := s.ABByGUID(ctx, guid)
	if err != nil {
		return nil, err
	}
	switch {
	case a.Personal:
		if a.OwnerID != u.ID {
			return nil, ErrNotFound
		}
		a.Rule = RuleFull
	case a.OwnerID == u.ID || adminOf(u, a):
		a.Rule = RuleFull
	default:
		err := s.db.QueryRowContext(ctx, `SELECT rule FROM ab_rules WHERE ab_id = ? AND user_id = ?`, a.ID, u.ID).Scan(&a.Rule)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
	}
	return a, nil
}

// SharedABsForUser lists the shared books visible to the user with their
// rules. With u == nil all shared books of org (0: any, NoOrg: none) are
// listed (panel).
func (s *Store) SharedABsForUser(ctx context.Context, u *User, org int64, p Page) ([]*AddressBook, int, error) {
	oc, args := orgCond("a.org_id", org)
	cond := "a.personal = 0 AND " + oc
	if u != nil {
		if u.IsAdmin && u.OrgID != 0 {
			cond += " AND (a.org_id = ? OR a.owner_id = ? OR EXISTS (SELECT 1 FROM ab_rules r WHERE r.ab_id = a.id AND r.user_id = ?))"
			args = append(args, u.OrgID, u.ID, u.ID)
		} else if !u.IsAdmin {
			cond += " AND (a.owner_id = ? OR EXISTS (SELECT 1 FROM ab_rules r WHERE r.ab_id = a.id AND r.user_id = ?))"
			args = append(args, u.ID, u.ID)
		}
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM address_books a WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := p.limitOffset()
	rows, err := s.db.QueryContext(ctx, abSelect+` WHERE `+cond+` ORDER BY a.name LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	var out []*AddressBook
	for rows.Next() {
		a, err := scanAB(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for _, a := range out {
		a.Rule = RuleFull
		if u != nil && !adminOf(u, a) && a.OwnerID != u.ID {
			if err := s.db.QueryRowContext(ctx, `SELECT rule FROM ab_rules WHERE ab_id = ? AND user_id = ?`, a.ID, u.ID).
				Scan(&a.Rule); err != nil {
				return nil, 0, err
			}
		}
	}
	return out, total, nil
}

// CreateSharedAB creates a shared book in an organization (0: none).
func (s *Store) CreateSharedAB(ctx context.Context, org int64, name string, ownerID int64, note, password string) (*AddressBook, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("address book name is required")
	}
	if strings.EqualFold(name, "My address book") || strings.EqualFold(name, "Legacy address book") {
		return nil, errors.New("this address book name is reserved by the client")
	}
	t := now()
	guid := NewGUID()
	_, err := s.db.ExecContext(ctx, `INSERT INTO address_books (guid, name, owner_id, personal, note, password, org_id, created_at, updated_at)
		VALUES (?, ?, ?, 0, ?, ?, ?, ?, ?)`, guid, name, ownerID, note, password, nullID(org), t, t)
	if isUniqueErr(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return s.ABByGUID(ctx, guid)
}

func (s *Store) UpdateSharedAB(ctx context.Context, guid, name string, ownerID int64, note, password string) (*AddressBook, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("address book name is required")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE address_books SET name = ?, owner_id = ?, note = ?, password = ?, updated_at = ?
		WHERE guid = ? AND personal = 0`, name, ownerID, note, password, now(), guid)
	if isUniqueErr(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.ABByGUID(ctx, guid)
}

func (s *Store) DeleteAB(ctx context.Context, guid string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM address_books WHERE guid = ? AND personal = 0`, guid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type ABRule struct {
	UserGUID string `json:"user_guid"`
	UserName string `json:"user_name"`
	Rule     int    `json:"rule"`
}

func (s *Store) ListABRules(ctx context.Context, abID int64) ([]ABRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.guid, u.name, r.rule FROM ab_rules r JOIN users u ON u.id = r.user_id
		WHERE r.ab_id = ? ORDER BY u.name`, abID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ABRule{}
	for rows.Next() {
		var r ABRule
		if err := rows.Scan(&r.UserGUID, &r.UserName, &r.Rule); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetABRule grants a user a rule on a shared book; RuleNone revokes it.
func (s *Store) SetABRule(ctx context.Context, abID, userID int64, rule int) error {
	if rule == RuleNone {
		_, err := s.db.ExecContext(ctx, `DELETE FROM ab_rules WHERE ab_id = ? AND user_id = ?`, abID, userID)
		return err
	}
	if rule < RuleRead || rule > RuleFull {
		return errors.New("invalid rule")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO ab_rules (ab_id, user_id, rule) VALUES (?, ?, ?)
		ON CONFLICT(ab_id, user_id) DO UPDATE SET rule = excluded.rule`, abID, userID, rule)
	return err
}

// ABPeer is an address book entry in the client's JSON shape.
type ABPeer struct {
	ID               string   `json:"id"`
	Hash             string   `json:"hash,omitempty"`
	Password         string   `json:"password,omitempty"`
	Username         string   `json:"username"`
	Hostname         string   `json:"hostname"`
	Platform         string   `json:"platform"`
	Alias            string   `json:"alias"`
	Tags             []string `json:"tags"`
	ForceAlwaysRelay string   `json:"forceAlwaysRelay"`
	RdpPort          string   `json:"rdpPort"`
	RdpUsername      string   `json:"rdpUsername"`
	LoginName        string   `json:"loginName"`
	Note             string   `json:"note"`
}

// ABPeerColumns maps client JSON keys to ab_peers columns for partial updates.
var ABPeerColumns = map[string]string{
	"alias":            "alias",
	"tags":             "tags",
	"hash":             "hash",
	"password":         "password",
	"username":         "username",
	"hostname":         "hostname",
	"platform":         "platform",
	"note":             "note",
	"forceAlwaysRelay": "force_always_relay",
	"rdpPort":          "rdp_port",
	"rdpUsername":      "rdp_username",
	"loginName":        "login_name",
}

func (s *Store) ListABPeers(ctx context.Context, abID int64, p Page) ([]*ABPeer, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ab_peers WHERE ab_id = ?`, abID).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := p.limitOffset()
	rows, err := s.db.QueryContext(ctx, `SELECT peer_id, hash, password, username, hostname, platform, alias, tags,
		force_always_relay, rdp_port, rdp_username, login_name, note FROM ab_peers WHERE ab_id = ?
		ORDER BY peer_id LIMIT ? OFFSET ?`, abID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*ABPeer{}
	for rows.Next() {
		var pr ABPeer
		var tags string
		if err := rows.Scan(&pr.ID, &pr.Hash, &pr.Password, &pr.Username, &pr.Hostname, &pr.Platform, &pr.Alias, &tags,
			&pr.ForceAlwaysRelay, &pr.RdpPort, &pr.RdpUsername, &pr.LoginName, &pr.Note); err != nil {
			return nil, 0, err
		}
		if json.Unmarshal([]byte(tags), &pr.Tags) != nil || pr.Tags == nil {
			pr.Tags = []string{}
		}
		out = append(out, &pr)
	}
	return out, total, rows.Err()
}

func (s *Store) HasABPeer(ctx context.Context, abID int64, peerID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ab_peers WHERE ab_id = ? AND peer_id = ?`, abID, peerID).Scan(&n)
	return n > 0, err
}

func (s *Store) CountABPeers(ctx context.Context, abID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ab_peers WHERE ab_id = ?`, abID).Scan(&n)
	return n, err
}

// MaxABPeers caps the size of one address book.
const MaxABPeers = 10000

// AddABPeer inserts a peer, or replaces the fields of an existing one.
func (s *Store) AddABPeer(ctx context.Context, abID int64, p *ABPeer) error {
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("peer id is required")
	}
	if n, err := s.CountABPeers(ctx, abID); err != nil {
		return err
	} else if n >= MaxABPeers {
		if exists, _ := s.HasABPeer(ctx, abID, strings.TrimSpace(p.ID)); !exists {
			return ErrFull
		}
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if p.ForceAlwaysRelay == "" {
		p.ForceAlwaysRelay = "false"
	}
	tags, _ := json.Marshal(p.Tags)
	t := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO ab_peers (ab_id, peer_id, alias, tags, hash, password, username, hostname, platform,
			note, force_always_relay, rdp_port, rdp_username, login_name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(ab_id, peer_id) DO UPDATE SET alias = excluded.alias, tags = excluded.tags, hash = excluded.hash,
			password = excluded.password, username = excluded.username, hostname = excluded.hostname,
			platform = excluded.platform, note = excluded.note, force_always_relay = excluded.force_always_relay,
			rdp_port = excluded.rdp_port, rdp_username = excluded.rdp_username, login_name = excluded.login_name,
			updated_at = excluded.updated_at`,
		abID, strings.TrimSpace(p.ID), p.Alias, string(tags), p.Hash, p.Password, p.Username, p.Hostname, p.Platform,
		p.Note, p.ForceAlwaysRelay, p.RdpPort, p.RdpUsername, p.LoginName, t, t)
	return err
}

// UpdateABPeer applies a partial update; keys are ab_peers column names and
// values must already be strings (tags as a JSON array string).
func (s *Store) UpdateABPeer(ctx context.Context, abID int64, peerID string, cols map[string]string) error {
	if len(cols) == 0 {
		return nil
	}
	var sets []string
	var args []any
	for col, v := range cols {
		sets = append(sets, col+" = ?")
		args = append(args, v)
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, now(), abID, peerID)
	res, err := s.db.ExecContext(ctx, `UPDATE ab_peers SET `+strings.Join(sets, ", ")+` WHERE ab_id = ? AND peer_id = ?`, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteABPeers(ctx context.Context, abID int64, ids []string) error {
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM ab_peers WHERE ab_id = ? AND peer_id = ?`, abID, id); err != nil {
			return err
		}
	}
	return nil
}

type ABTag struct {
	Name  string `json:"name"`
	Color int64  `json:"color"`
}

func (s *Store) ListABTags(ctx context.Context, abID int64) ([]ABTag, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, color FROM ab_tags WHERE ab_id = ? ORDER BY name`, abID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ABTag{}
	for rows.Next() {
		var t ABTag
		if err := rows.Scan(&t.Name, &t.Color); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) AddABTag(ctx context.Context, abID int64, t ABTag) error {
	if strings.TrimSpace(t.Name) == "" {
		return errors.New("tag name is required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO ab_tags (ab_id, name, color) VALUES (?, ?, ?)
		ON CONFLICT(ab_id, name) DO UPDATE SET color = excluded.color`, abID, t.Name, t.Color)
	return err
}

// EnsureABTag creates a tag unless it exists, keeping an existing color.
func (s *Store) EnsureABTag(ctx context.Context, abID int64, t ABTag) error {
	if strings.TrimSpace(t.Name) == "" {
		return errors.New("tag name is required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO ab_tags (ab_id, name, color) VALUES (?, ?, ?)
		ON CONFLICT(ab_id, name) DO NOTHING`, abID, t.Name, t.Color)
	return err
}

func (s *Store) UpdateABTagColor(ctx context.Context, abID int64, t ABTag) error {
	res, err := s.db.ExecContext(ctx, `UPDATE ab_tags SET color = ? WHERE ab_id = ? AND name = ?`, t.Color, abID, t.Name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RenameABTag renames a tag and the references to it in the book's peers.
func (s *Store) RenameABTag(ctx context.Context, abID int64, oldName, newName string) error {
	if strings.TrimSpace(newName) == "" {
		return errors.New("tag name is required")
	}
	return s.rewritePeerTags(ctx, abID, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE ab_tags SET name = ? WHERE ab_id = ? AND name = ?`, newName, abID, oldName)
		if isUniqueErr(err) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	}, func(tags []string) []string {
		out := make([]string, 0, len(tags))
		for _, t := range tags {
			if t == oldName {
				t = newName
			}
			if !containsStr(out, t) {
				out = append(out, t)
			}
		}
		return out
	})
}

// DeleteABTags removes tags and the references to them in the book's peers.
func (s *Store) DeleteABTags(ctx context.Context, abID int64, names []string) error {
	return s.rewritePeerTags(ctx, abID, func(tx *sql.Tx) error {
		for _, n := range names {
			if _, err := tx.ExecContext(ctx, `DELETE FROM ab_tags WHERE ab_id = ? AND name = ?`, abID, n); err != nil {
				return err
			}
		}
		return nil
	}, func(tags []string) []string {
		out := make([]string, 0, len(tags))
		for _, t := range tags {
			if !containsStr(names, t) {
				out = append(out, t)
			}
		}
		return out
	})
}

func (s *Store) rewritePeerTags(ctx context.Context, abID int64, tagOp func(*sql.Tx) error, rewrite func([]string) []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tagOp(tx); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT peer_id, tags FROM ab_peers WHERE ab_id = ?`, abID)
	if err != nil {
		return err
	}
	updates := map[string]string{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var tags []string
		if json.Unmarshal([]byte(raw), &tags) != nil {
			continue
		}
		b, _ := json.Marshal(rewrite(tags))
		if string(b) != raw {
			updates[id] = string(b)
		}
	}
	rows.Close()
	for id, tags := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE ab_peers SET tags = ? WHERE ab_id = ? AND peer_id = ?`, tags, abID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// LegacyAB returns the user's legacy (single blob) address book, or "" when empty.
func (s *Store) LegacyAB(ctx context.Context, userID int64) (string, int64, error) {
	var data string
	var updated int64
	err := s.db.QueryRowContext(ctx, `SELECT data, updated_at FROM legacy_ab WHERE user_id = ?`, userID).Scan(&data, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	}
	return data, updated, err
}

func (s *Store) SetLegacyAB(ctx context.Context, userID int64, data string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO legacy_ab (user_id, data, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`, userID, data, now())
	return err
}
