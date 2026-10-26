package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
)

type OIDCProvider struct {
	ID           int64  `json:"-"`
	GUID         string `json:"guid"`
	Name         string `json:"name"`
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	Scopes       string `json:"scopes"`
	Icon         string `json:"icon"`
	AutoCreate   bool   `json:"auto_create"`
	Enabled      bool   `json:"enabled"`
	CreatedAt    int64  `json:"created_at"`
}

type OIDCProviderInput struct {
	Name, Issuer, ClientID, ClientSecret, Scopes, Icon string
	AutoCreate, Enabled                                bool
}

// Provider names travel in "oidc/<name>" login options and URLs.
var providerNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,62}$`)

func (in *OIDCProviderInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Issuer = strings.TrimRight(strings.TrimSpace(in.Issuer), "/")
	in.ClientID = strings.TrimSpace(in.ClientID)
	in.Scopes = strings.Join(strings.Fields(in.Scopes), " ")
	if !providerNameRe.MatchString(in.Name) {
		return errors.New("provider name must be 1-63 letters, digits, spaces, '.', '_' or '-'")
	}
	if !strings.HasPrefix(in.Issuer, "https://") && !strings.HasPrefix(in.Issuer, "http://") {
		return errors.New("issuer must be an http(s) URL")
	}
	if in.ClientID == "" {
		return errors.New("client id is required")
	}
	if in.Scopes == "" {
		in.Scopes = "openid profile email"
	}
	if !strings.Contains(" "+in.Scopes+" ", " openid ") {
		in.Scopes = "openid " + in.Scopes
	}
	return nil
}

const oidcSelect = `SELECT id, guid, name, issuer, client_id, client_secret, scopes, icon, auto_create, enabled, created_at FROM oidc_providers`

func scanOIDC(row interface{ Scan(...any) error }) (*OIDCProvider, error) {
	var p OIDCProvider
	var auto, enabled int
	err := row.Scan(&p.ID, &p.GUID, &p.Name, &p.Issuer, &p.ClientID, &p.ClientSecret, &p.Scopes, &p.Icon, &auto, &enabled, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.AutoCreate, p.Enabled = auto != 0, enabled != 0
	return &p, nil
}

func (s *Store) CreateOIDCProvider(ctx context.Context, in OIDCProviderInput) (*OIDCProvider, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	guid := NewGUID()
	_, err := s.db.ExecContext(ctx, `INSERT INTO oidc_providers (guid, name, issuer, client_id, client_secret, scopes, icon, auto_create, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, guid, in.Name, in.Issuer, in.ClientID, in.ClientSecret, in.Scopes, in.Icon,
		boolInt(in.AutoCreate), boolInt(in.Enabled), now())
	if isUniqueErr(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return s.OIDCProviderByGUID(ctx, guid)
}

// UpdateOIDCProvider changes a provider; an empty ClientSecret keeps the stored one.
func (s *Store) UpdateOIDCProvider(ctx context.Context, guid string, in OIDCProviderInput) (*OIDCProvider, error) {
	cur, err := s.OIDCProviderByGUID(ctx, guid)
	if err != nil {
		return nil, err
	}
	if err := in.normalize(); err != nil {
		return nil, err
	}
	if in.ClientSecret == "" {
		in.ClientSecret = cur.ClientSecret
	}
	_, err = s.db.ExecContext(ctx, `UPDATE oidc_providers SET name = ?, issuer = ?, client_id = ?, client_secret = ?, scopes = ?,
		icon = ?, auto_create = ?, enabled = ? WHERE id = ?`, in.Name, in.Issuer, in.ClientID, in.ClientSecret, in.Scopes, in.Icon,
		boolInt(in.AutoCreate), boolInt(in.Enabled), cur.ID)
	if isUniqueErr(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return s.OIDCProviderByGUID(ctx, guid)
}

func (s *Store) DeleteOIDCProvider(ctx context.Context, guid string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM oidc_providers WHERE guid = ?`, guid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) OIDCProviderByGUID(ctx context.Context, guid string) (*OIDCProvider, error) {
	return scanOIDC(s.db.QueryRowContext(ctx, oidcSelect+` WHERE guid = ?`, guid))
}

func (s *Store) OIDCProviderByID(ctx context.Context, id int64) (*OIDCProvider, error) {
	return scanOIDC(s.db.QueryRowContext(ctx, oidcSelect+` WHERE id = ?`, id))
}

func (s *Store) OIDCProviderByName(ctx context.Context, name string) (*OIDCProvider, error) {
	return scanOIDC(s.db.QueryRowContext(ctx, oidcSelect+` WHERE name = ?`, strings.TrimSpace(name)))
}

func (s *Store) ListOIDCProviders(ctx context.Context, onlyEnabled bool) ([]*OIDCProvider, error) {
	q := oidcSelect
	if onlyEnabled {
		q += ` WHERE enabled = 1`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*OIDCProvider{}
	for rows.Next() {
		p, err := scanOIDC(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// OIDCUser finds the user linked to a provider subject.
func (s *Store) OIDCUser(ctx context.Context, providerID int64, subject string) (*User, error) {
	var uid int64
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM oidc_identities WHERE provider_id = ? AND subject = ?`,
		providerID, subject).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.UserByID(ctx, uid)
}

func (s *Store) LinkOIDCUser(ctx context.Context, providerID int64, subject string, userID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO oidc_identities (provider_id, subject, user_id, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(provider_id, subject) DO UPDATE SET user_id = excluded.user_id`, providerID, subject, userID, now())
	return err
}

// UserByEmail returns the single user with this email (case-insensitive).
func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users WHERE email = ? COLLATE NOCASE LIMIT 2`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		found = append(found, u)
	}
	if len(found) != 1 {
		return nil, ErrNotFound
	}
	return found[0], rows.Err()
}
