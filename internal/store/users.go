package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// User statuses, as understood by the RustDesk client.
const (
	UserDisabled   = 0
	UserNormal     = 1
	UserUnverified = -1
)

type User struct {
	ID           int64  `json:"-"`
	GUID         string `json:"guid"`
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	Email        string `json:"email"`
	Note         string `json:"note"`
	Status       int    `json:"status"`
	IsAdmin      bool   `json:"is_admin"`
	TOTPEnabled  bool   `json:"totp_enabled"`
	GroupID      *int64 `json:"-"`
	GroupName    string `json:"group_name"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
	PasswordHash string `json:"-"`
	TOTPSecret   string `json:"-"`
}

const userCols = `id, guid, name, display_name, email, note, status, is_admin, totp_secret, password_hash, created_at, updated_at,
	group_id, COALESCE((SELECT g.name FROM user_groups g WHERE g.id = users.group_id), '')`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var admin int
	var gid sql.NullInt64
	err := row.Scan(&u.ID, &u.GUID, &u.Name, &u.DisplayName, &u.Email, &u.Note, &u.Status, &admin,
		&u.TOTPSecret, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt, &gid, &u.GroupName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.IsAdmin = admin != 0
	if gid.Valid {
		u.GroupID = &gid.Int64
	}
	u.TOTPEnabled = u.TOTPSecret != ""
	return &u, nil
}

func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(h), err
}

// CheckPassword reports whether pw matches the user's password hash.
func (u *User) CheckPassword(pw string) bool {
	if u.PasswordHash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(pw)) == nil
}

type UserInput struct {
	Name        string
	DisplayName string
	Email       string
	Note        string
	Password    string
	Status      int
	IsAdmin     bool
}

func (s *Store) CreateUser(ctx context.Context, in UserInput) (*User, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errors.New("user name is required")
	}
	hash := ""
	if in.Password != "" {
		var err error
		if hash, err = HashPassword(in.Password); err != nil {
			return nil, err
		}
	}
	t := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO users (guid, name, display_name, email, note, password_hash, status, is_admin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		NewGUID(), name, in.DisplayName, in.Email, in.Note, hash, in.Status, boolInt(in.IsAdmin), t, t)
	if isUniqueErr(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return s.UserByName(ctx, name)
}

// UpdateUser changes profile fields. An empty Password keeps the current one.
func (s *Store) UpdateUser(ctx context.Context, guid string, in UserInput) (*User, error) {
	u, err := s.UserByGUID(ctx, guid)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = u.Name
	}
	hash := u.PasswordHash
	if in.Password != "" {
		if hash, err = HashPassword(in.Password); err != nil {
			return nil, err
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET name = ?, display_name = ?, email = ?, note = ?, password_hash = ?,
		status = ?, is_admin = ?, updated_at = ? WHERE id = ?`,
		name, in.DisplayName, in.Email, in.Note, hash, in.Status, boolInt(in.IsAdmin), now(), u.ID)
	if isUniqueErr(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	if in.Password != "" || in.Status != UserNormal {
		// Credentials changed or account disabled: sign the user out everywhere.
		if _, err := s.db.ExecContext(ctx, `DELETE FROM tokens WHERE user_id = ?`, u.ID); err != nil {
			return nil, err
		}
	}
	return s.UserByGUID(ctx, guid)
}

func (s *Store) SetPassword(ctx context.Context, userID int64, pw string) error {
	hash, err := HashPassword(pw)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, hash, now(), userID)
	return err
}

func (s *Store) SetTOTPSecret(ctx context.Context, userID int64, secret string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET totp_secret = ?, updated_at = ? WHERE id = ?`, secret, now(), userID)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, guid string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE guid = ?`, guid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UserByName(ctx context.Context, name string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE name = ?`, strings.TrimSpace(name)))
}

func (s *Store) UserByGUID(ctx context.Context, guid string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE guid = ?`, guid))
}

func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CountActiveAdmins counts enabled administrators other than excludeID.
func (s *Store) CountActiveAdmins(ctx context.Context, excludeID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_admin = 1 AND status = 1 AND id != ?`, excludeID).Scan(&n)
	return n, err
}

type UserFilter struct {
	Status *int
	Search string
	OnlyID int64 // when non-zero, only this user is returned
}

func (s *Store) ListUsers(ctx context.Context, f UserFilter, p Page) ([]*User, int, error) {
	where := []string{"1=1"}
	var args []any
	if f.Status != nil {
		where = append(where, "status = ?")
		args = append(args, *f.Status)
	}
	if f.Search != "" {
		where = append(where, "(name LIKE ? OR display_name LIKE ? OR email LIKE ?)")
		q := "%" + f.Search + "%"
		args = append(args, q, q, q)
	}
	if f.OnlyID != 0 {
		// The user and, when they belong to a group, the other members of it.
		where = append(where, "(id = ? OR group_id = (SELECT group_id FROM users WHERE id = ?))")
		args = append(args, f.OnlyID, f.OnlyID)
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := p.limitOffset()
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users WHERE `+cond+` ORDER BY name LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}

// Token is an access token issued at login. Only its SHA-256 hash is stored.
type Token struct {
	UserID     int64
	DeviceID   string
	DeviceUUID string
	DeviceInfo string
	IP         string
	CreatedAt  int64
	LastUsedAt int64
	ExpiresAt  int64
}

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// RandomToken returns n random bytes, hex encoded.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// CreateToken issues a new access token and returns its raw value.
func (s *Store) CreateToken(ctx context.Context, t Token, ttl time.Duration) (string, error) {
	raw := RandomToken(32)
	n := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO tokens (hash, user_id, device_id, device_uuid, device_info, ip, created_at, last_used_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		hashToken(raw), t.UserID, t.DeviceID, t.DeviceUUID, t.DeviceInfo, t.IP, n, n, n+int64(ttl.Seconds()))
	return raw, err
}

// UserByToken resolves a raw access token to its active user and refreshes its last use.
func (s *Store) UserByToken(ctx context.Context, raw string) (*User, *Token, error) {
	if raw == "" {
		return nil, nil, ErrNotFound
	}
	h := hashToken(raw)
	var t Token
	err := s.db.QueryRowContext(ctx, `SELECT user_id, device_id, device_uuid, device_info, ip, created_at, last_used_at, expires_at
		FROM tokens WHERE hash = ?`, h).
		Scan(&t.UserID, &t.DeviceID, &t.DeviceUUID, &t.DeviceInfo, &t.IP, &t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	n := now()
	if t.ExpiresAt < n {
		s.db.ExecContext(ctx, `DELETE FROM tokens WHERE hash = ?`, h)
		return nil, nil, ErrNotFound
	}
	u, err := s.UserByID(ctx, t.UserID)
	if err != nil {
		return nil, nil, err
	}
	if u.Status != UserNormal {
		return nil, nil, ErrNotFound
	}
	if n-t.LastUsedAt > 60 {
		s.db.ExecContext(ctx, `UPDATE tokens SET last_used_at = ? WHERE hash = ?`, n, h)
	}
	return u, &t, nil
}

func (s *Store) DeleteToken(ctx context.Context, raw string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tokens WHERE hash = ?`, hashToken(raw))
	return err
}

func (s *Store) DeleteExpiredTokens(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tokens WHERE expires_at < ?`, now())
	return err
}
