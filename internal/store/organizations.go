package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Organization is a tenant: a company or branch with its own users,
// devices, groups, address books and strategies. Clients and panel users
// reach it through its domains (Host header). Records without an
// organization belong to the server itself and are visible only to global
// staff.
type Organization struct {
	ID        int64    `json:"-"`
	GUID      string   `json:"guid"`
	Name      string   `json:"name"`
	Note      string   `json:"note"`
	Domains   []string `json:"domains"`
	Users     int      `json:"users"`
	Devices   int      `json:"devices"`
	CreatedAt int64    `json:"created_at"`
}

// ErrNotEmpty is returned when deleting an organization that still has users or devices.
var ErrNotEmpty = errors.New("organization is not empty")

const orgSelect = `SELECT o.id, o.guid, o.name, o.note, o.created_at,
	(SELECT COUNT(*) FROM users u WHERE u.org_id = o.id),
	(SELECT COUNT(*) FROM devices d WHERE d.org_id = o.id)
	FROM organizations o`

func scanOrg(row interface{ Scan(...any) error }) (*Organization, error) {
	var o Organization
	err := row.Scan(&o.ID, &o.GUID, &o.Name, &o.Note, &o.CreatedAt, &o.Users, &o.Devices)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &o, err
}

func (s *Store) loadDomains(ctx context.Context, orgs ...*Organization) error {
	for _, o := range orgs {
		rows, err := s.db.QueryContext(ctx, `SELECT domain FROM org_domains WHERE org_id = ? ORDER BY domain`, o.ID)
		if err != nil {
			return err
		}
		o.Domains = []string{}
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				rows.Close()
				return err
			}
			o.Domains = append(o.Domains, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListOrganizations(ctx context.Context) ([]*Organization, error) {
	rows, err := s.db.QueryContext(ctx, orgSelect+` ORDER BY o.name`)
	if err != nil {
		return nil, err
	}
	out := []*Organization{}
	for rows.Next() {
		o, err := scanOrg(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.loadDomains(ctx, out...)
}

func (s *Store) organization(ctx context.Context, where string, arg any) (*Organization, error) {
	o, err := scanOrg(s.db.QueryRowContext(ctx, orgSelect+` WHERE `+where, arg))
	if err != nil {
		return nil, err
	}
	return o, s.loadDomains(ctx, o)
}

func (s *Store) OrganizationByGUID(ctx context.Context, guid string) (*Organization, error) {
	return s.organization(ctx, `o.guid = ?`, guid)
}

func (s *Store) OrganizationByID(ctx context.Context, id int64) (*Organization, error) {
	return s.organization(ctx, `o.id = ?`, id)
}

// OrganizationByDomain resolves a request host (without port) to its organization.
func (s *Store) OrganizationByDomain(ctx context.Context, domain string) (*Organization, error) {
	return s.organization(ctx, `o.id = (SELECT org_id FROM org_domains WHERE domain = ?)`, NormalizeDomain(domain))
}

// NormalizeDomain lowercases a host name and strips a trailing dot.
func NormalizeDomain(d string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
}

func validDomain(d string) bool {
	if d == "" || len(d) > 253 {
		return false
	}
	for _, label := range strings.Split(d, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

type OrganizationInput struct {
	Name, Note string
	Domains    []string
}

func (in *OrganizationInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("organization name is required")
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range in.Domains {
		d = NormalizeDomain(d)
		if d == "" || seen[d] {
			continue
		}
		if !validDomain(d) {
			return errors.New("invalid domain: " + d)
		}
		seen[d] = true
		out = append(out, d)
	}
	in.Domains = out
	return nil
}

// SaveOrganization creates (guid == "") or updates an organization and
// replaces its domains. A domain already used by another organization is
// ErrConflict.
func (s *Store) SaveOrganization(ctx context.Context, guid string, in OrganizationInput) (*Organization, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if guid == "" {
		guid = NewGUID()
		_, err = tx.ExecContext(ctx, `INSERT INTO organizations (guid, name, note, created_at) VALUES (?, ?, ?, ?)`,
			guid, in.Name, in.Note, now())
	} else {
		var res sql.Result
		res, err = tx.ExecContext(ctx, `UPDATE organizations SET name = ?, note = ? WHERE guid = ?`, in.Name, in.Note, guid)
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
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM organizations WHERE guid = ?`, guid).Scan(&id); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM org_domains WHERE org_id = ?`, id); err != nil {
		return nil, err
	}
	for _, d := range in.Domains {
		if _, err := tx.ExecContext(ctx, `INSERT INTO org_domains (domain, org_id) VALUES (?, ?)`, d, id); err != nil {
			if isUniqueErr(err) {
				return nil, ErrConflict
			}
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.OrganizationByGUID(ctx, guid)
}

// DeleteOrganization removes an empty organization together with its
// groups, strategies and shared address books.
func (s *Store) DeleteOrganization(ctx context.Context, guid string) error {
	o, err := s.OrganizationByGUID(ctx, guid)
	if err != nil {
		return err
	}
	if o.Users > 0 || o.Devices > 0 {
		return ErrNotEmpty
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM address_books WHERE org_id = ?`,
		`DELETE FROM device_groups WHERE org_id = ?`,
		`DELETE FROM user_groups WHERE org_id = ?`,
		`DELETE FROM strategies WHERE org_id = ?`,
		`DELETE FROM organizations WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, o.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// orgCond restricts a query to an organization: 0 means any, NoOrg means
// records without one.
func orgCond(col string, org int64) (string, []any) {
	switch {
	case org == 0:
		return "1=1", nil
	case org == NoOrg:
		return col + " IS NULL", nil
	default:
		return col + " = ?", []any{org}
	}
}

// NoOrg selects records that belong to no organization.
const NoOrg = -1
