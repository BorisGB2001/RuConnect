package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Strategy is a named set of client options ("config_options") that devices
// apply when they receive it in a heartbeat response. A device uses its own
// strategy, else its device group's, else the default strategy.
type Strategy struct {
	ID         int64             `json:"-"`
	GUID       string            `json:"guid"`
	Name       string            `json:"name"`
	Note       string            `json:"note"`
	Options    map[string]string `json:"options"`
	IsDefault  bool              `json:"is_default"`
	ModifiedAt int64             `json:"modified_at"`
	CreatedAt  int64             `json:"created_at"`
	Devices    int               `json:"devices"`
	Groups     int               `json:"groups"`
	OrgID      int64             `json:"-"`
	OrgGUID    string            `json:"org_guid"`
	OrgName    string            `json:"org_name"`
}

type StrategyInput struct {
	Name      string
	Note      string
	Options   map[string]string
	IsDefault bool
	OrgID     int64 // organization on create (0: none); ignored on update
}

const strategySelect = `SELECT s.id, s.guid, s.name, s.note, s.options, s.is_default, s.modified_at, s.created_at,
	(SELECT COUNT(*) FROM devices d WHERE d.strategy_id = s.id),
	(SELECT COUNT(*) FROM device_groups g WHERE g.strategy_id = s.id), IFNULL(s.org_id, 0), COALESCE((SELECT o.guid FROM organizations o WHERE o.id = s.org_id), ''), COALESCE((SELECT o.name FROM organizations o WHERE o.id = s.org_id), '') FROM strategies s`

func scanStrategy(row interface{ Scan(...any) error }) (*Strategy, error) {
	var v Strategy
	var opts string
	var def int
	err := row.Scan(&v.ID, &v.GUID, &v.Name, &v.Note, &opts, &def, &v.ModifiedAt, &v.CreatedAt, &v.Devices, &v.Groups, &v.OrgID, &v.OrgGUID, &v.OrgName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.IsDefault = def != 0
	if json.Unmarshal([]byte(opts), &v.Options) != nil || v.Options == nil {
		v.Options = map[string]string{}
	}
	return &v, nil
}

// nextStamp returns a modification time in unix seconds greater than every
// stamp in use. Stamps are unique across strategies because the client only
// compares the number: a device moved to another strategy must see a change.
func nextStamp(ctx context.Context, tx *sql.Tx) (int64, error) {
	var max int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(modified_at), 0) FROM strategies`).Scan(&max); err != nil {
		return 0, err
	}
	t := time.Now().Unix()
	if t <= max {
		t = max + 1
	}
	return t, nil
}

func (in *StrategyInput) normalize() (string, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return "", errors.New("strategy name is required")
	}
	clean := map[string]string{}
	for k, v := range in.Options {
		k = strings.TrimSpace(k)
		if k == "" || strings.ContainsAny(k, " \t\n") {
			return "", errors.New("invalid option name " + k)
		}
		clean[k] = v
	}
	b, err := json.Marshal(clean)
	return string(b), err
}

func (s *Store) CreateStrategy(ctx context.Context, in StrategyInput) (*Strategy, error) {
	opts, err := in.normalize()
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if in.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE strategies SET is_default = 0 WHERE is_default = 1 AND IFNULL(org_id, 0) = ?`, in.OrgID); err != nil {
			return nil, err
		}
	}
	st, err := nextStamp(ctx, tx)
	if err != nil {
		return nil, err
	}
	guid := NewGUID()
	_, err = tx.ExecContext(ctx, `INSERT INTO strategies (guid, name, note, options, is_default, org_id, modified_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, guid, in.Name, in.Note, opts, boolInt(in.IsDefault), nullID(in.OrgID), st, now())
	if isUniqueErr(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.StrategyByGUID(ctx, guid)
}

func (s *Store) UpdateStrategy(ctx context.Context, guid string, in StrategyInput) (*Strategy, error) {
	cur, err := s.StrategyByGUID(ctx, guid)
	if err != nil {
		return nil, err
	}
	opts, err := in.normalize()
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if in.IsDefault && !cur.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE strategies SET is_default = 0 WHERE is_default = 1 AND IFNULL(org_id, 0) = ?`, cur.OrgID); err != nil {
			return nil, err
		}
	}
	st, err := nextStamp(ctx, tx)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE strategies SET name = ?, note = ?, options = ?, is_default = ?, modified_at = ? WHERE id = ?`,
		in.Name, in.Note, opts, boolInt(in.IsDefault), st, cur.ID)
	if isUniqueErr(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.StrategyByGUID(ctx, guid)
}

func (s *Store) DeleteStrategy(ctx context.Context, guid string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM strategies WHERE guid = ?`, guid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) StrategyByGUID(ctx context.Context, guid string) (*Strategy, error) {
	return scanStrategy(s.db.QueryRowContext(ctx, strategySelect+` WHERE s.guid = ?`, guid))
}

// StrategyByName finds a strategy by name within an organization (0: none).
func (s *Store) StrategyByName(ctx context.Context, org int64, name string) (*Strategy, error) {
	return scanStrategy(s.db.QueryRowContext(ctx, strategySelect+` WHERE s.name = ? AND IFNULL(s.org_id, 0) = ?`, strings.TrimSpace(name), org))
}

// ListStrategies lists the strategies of an organization (0: any, NoOrg: none).
func (s *Store) ListStrategies(ctx context.Context, org int64) ([]*Strategy, error) {
	cond, args := orgCond("s.org_id", org)
	rows, err := s.db.QueryContext(ctx, strategySelect+` WHERE `+cond+` ORDER BY s.is_default DESC, s.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Strategy{}
	for rows.Next() {
		v, err := scanStrategy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// EffectiveStrategy returns the strategy a device should apply, or nil: its
// own, its group's, else the default strategy of its organization.
func (s *Store) EffectiveStrategy(ctx context.Context, deviceID string) (*Strategy, error) {
	v, err := scanStrategy(s.db.QueryRowContext(ctx, strategySelect+` WHERE s.id = COALESCE(
		(SELECT d.strategy_id FROM devices d WHERE d.id = ?),
		(SELECT g.strategy_id FROM devices d JOIN device_groups g ON g.id = d.group_id WHERE d.id = ?),
		(SELECT st.id FROM strategies st WHERE st.is_default = 1
			AND IFNULL(st.org_id, 0) = (SELECT IFNULL(d.org_id, 0) FROM devices d WHERE d.id = ?) LIMIT 1))`,
		deviceID, deviceID, deviceID))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return v, err
}

// SetDeviceStrategy assigns a strategy to a device (0 clears it).
func (s *Store) SetDeviceStrategy(ctx context.Context, deviceID string, strategyID int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE devices SET strategy_id = ?, updated_at = ? WHERE id = ?`, nullID(strategyID), now(), deviceID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetGroupStrategy assigns a strategy to a device group (0 clears it).
func (s *Store) SetGroupStrategy(ctx context.Context, groupGUID string, strategyID int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE device_groups SET strategy_id = ? WHERE guid = ?`, nullID(strategyID), groupGUID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// StrategyNames returns the strategy names assigned directly to a device and to its group.
func (s *Store) StrategyNames(ctx context.Context, deviceID string) (device, group string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT name FROM strategies WHERE id = d.strategy_id), ''),
		COALESCE((SELECT st.name FROM device_groups g JOIN strategies st ON st.id = g.strategy_id WHERE g.id = d.group_id), '')
		FROM devices d WHERE d.id = ?`, deviceID).Scan(&device, &group)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}
