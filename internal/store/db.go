// Package store is the persistence layer of RuConnect (SQLite).
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
)

var memSeq atomic.Int64

type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and applies migrations.
// Use ":memory:" for an in-memory database in tests.
func Open(path string) (*Store, error) {
	// Each in-memory database gets its own name so parallel tests stay isolated.
	dsn := fmt.Sprintf("file:mem%d?mode=memory&cache=shared", memSeq.Add(1))
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, err
		}
		dsn = "file:" + path
	}
	dsn += sep(dsn) + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows a single writer; one connection avoids SQLITE_BUSY and keeps
	// the in-memory test database shared.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func sep(dsn string) string {
	if strings.Contains(dsn, "?") {
		return "&"
	}
	return "?"
}

// migrations are applied in order; never edit an applied migration, append a new one.
var migrations = []string{
	`
CREATE TABLE users (
	id            INTEGER PRIMARY KEY,
	guid          TEXT NOT NULL UNIQUE,
	name          TEXT NOT NULL UNIQUE COLLATE NOCASE,
	display_name  TEXT NOT NULL DEFAULT '',
	email         TEXT NOT NULL DEFAULT '',
	note          TEXT NOT NULL DEFAULT '',
	password_hash TEXT NOT NULL DEFAULT '',
	status        INTEGER NOT NULL DEFAULT 1,
	is_admin      INTEGER NOT NULL DEFAULT 0,
	totp_secret   TEXT NOT NULL DEFAULT '',
	created_at    INTEGER NOT NULL,
	updated_at    INTEGER NOT NULL
);

CREATE TABLE tokens (
	hash         TEXT PRIMARY KEY,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	device_id    TEXT NOT NULL DEFAULT '',
	device_uuid  TEXT NOT NULL DEFAULT '',
	device_info  TEXT NOT NULL DEFAULT '',
	ip           TEXT NOT NULL DEFAULT '',
	created_at   INTEGER NOT NULL,
	last_used_at INTEGER NOT NULL,
	expires_at   INTEGER NOT NULL
);
CREATE INDEX tokens_user ON tokens(user_id);

CREATE TABLE device_groups (
	id         INTEGER PRIMARY KEY,
	guid       TEXT NOT NULL UNIQUE,
	name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
	note       TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);

CREATE TABLE devices (
	id             TEXT PRIMARY KEY,
	guid           TEXT NOT NULL UNIQUE,
	uuid           TEXT NOT NULL DEFAULT '',
	hostname       TEXT NOT NULL DEFAULT '',
	username       TEXT NOT NULL DEFAULT '',
	os             TEXT NOT NULL DEFAULT '',
	cpu            TEXT NOT NULL DEFAULT '',
	memory         TEXT NOT NULL DEFAULT '',
	version        TEXT NOT NULL DEFAULT '',
	note           TEXT NOT NULL DEFAULT '',
	ip             TEXT NOT NULL DEFAULT '',
	user_id        INTEGER REFERENCES users(id) ON DELETE SET NULL,
	group_id       INTEGER REFERENCES device_groups(id) ON DELETE SET NULL,
	conns          INTEGER NOT NULL DEFAULT 0,
	last_online_at INTEGER NOT NULL DEFAULT 0,
	created_at     INTEGER NOT NULL,
	updated_at     INTEGER NOT NULL
);
CREATE INDEX devices_user ON devices(user_id);
CREATE INDEX devices_group ON devices(group_id);

CREATE TABLE address_books (
	id         INTEGER PRIMARY KEY,
	guid       TEXT NOT NULL UNIQUE,
	name       TEXT NOT NULL,
	owner_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	personal   INTEGER NOT NULL DEFAULT 0,
	note       TEXT NOT NULL DEFAULT '',
	password   TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX address_books_personal ON address_books(owner_id) WHERE personal = 1;
CREATE UNIQUE INDEX address_books_shared_name ON address_books(name COLLATE NOCASE) WHERE personal = 0;

CREATE TABLE ab_rules (
	ab_id   INTEGER NOT NULL REFERENCES address_books(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	rule    INTEGER NOT NULL,
	PRIMARY KEY (ab_id, user_id)
);

CREATE TABLE ab_peers (
	ab_id              INTEGER NOT NULL REFERENCES address_books(id) ON DELETE CASCADE,
	peer_id            TEXT NOT NULL,
	alias              TEXT NOT NULL DEFAULT '',
	tags               TEXT NOT NULL DEFAULT '[]',
	hash               TEXT NOT NULL DEFAULT '',
	password           TEXT NOT NULL DEFAULT '',
	username           TEXT NOT NULL DEFAULT '',
	hostname           TEXT NOT NULL DEFAULT '',
	platform           TEXT NOT NULL DEFAULT '',
	note               TEXT NOT NULL DEFAULT '',
	force_always_relay TEXT NOT NULL DEFAULT 'false',
	rdp_port           TEXT NOT NULL DEFAULT '',
	rdp_username       TEXT NOT NULL DEFAULT '',
	login_name         TEXT NOT NULL DEFAULT '',
	created_at         INTEGER NOT NULL,
	updated_at         INTEGER NOT NULL,
	PRIMARY KEY (ab_id, peer_id)
);

CREATE TABLE ab_tags (
	ab_id INTEGER NOT NULL REFERENCES address_books(id) ON DELETE CASCADE,
	name  TEXT NOT NULL,
	color INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (ab_id, name)
);

CREATE TABLE legacy_ab (
	user_id    INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	data       TEXT NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE audit_conns (
	id             INTEGER PRIMARY KEY,
	guid           TEXT NOT NULL UNIQUE,
	device_id      TEXT NOT NULL,
	device_uuid    TEXT NOT NULL DEFAULT '',
	conn_id        INTEGER NOT NULL DEFAULT 0,
	session_id     TEXT NOT NULL DEFAULT '',
	ip             TEXT NOT NULL DEFAULT '',
	peer_id        TEXT NOT NULL DEFAULT '',
	peer_name      TEXT NOT NULL DEFAULT '',
	conn_type      INTEGER NOT NULL DEFAULT -1,
	primary_auth   INTEGER NOT NULL DEFAULT 0,
	two_factor     INTEGER NOT NULL DEFAULT 0,
	conn_audit_ref TEXT NOT NULL DEFAULT '',
	note           TEXT NOT NULL DEFAULT '',
	created_at     INTEGER NOT NULL,
	authorized_at  INTEGER NOT NULL DEFAULT 0,
	closed_at      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX audit_conns_conn ON audit_conns(device_id, conn_id);
CREATE INDEX audit_conns_session ON audit_conns(device_id, session_id);
CREATE INDEX audit_conns_created ON audit_conns(created_at);

CREATE TABLE audit_files (
	id         INTEGER PRIMARY KEY,
	device_id  TEXT NOT NULL,
	peer_id    TEXT NOT NULL DEFAULT '',
	conn_id    INTEGER NOT NULL DEFAULT 0,
	type       INTEGER NOT NULL DEFAULT 0,
	path       TEXT NOT NULL DEFAULT '',
	is_file    INTEGER NOT NULL DEFAULT 0,
	peer_ip    TEXT NOT NULL DEFAULT '',
	peer_name  TEXT NOT NULL DEFAULT '',
	num        INTEGER NOT NULL DEFAULT 0,
	files      TEXT NOT NULL DEFAULT '[]',
	created_at INTEGER NOT NULL
);
CREATE INDEX audit_files_created ON audit_files(created_at);

CREATE TABLE audit_alarms (
	id         INTEGER PRIMARY KEY,
	device_id  TEXT NOT NULL,
	conn_id    INTEGER NOT NULL DEFAULT 0,
	typ        INTEGER NOT NULL,
	ip         TEXT NOT NULL DEFAULT '',
	info       TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE INDEX audit_alarms_created ON audit_alarms(created_at);

CREATE TABLE settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`,
	// 2: multi-server support. Each device belongs to the hbbs site it registered with.
	`
CREATE TABLE servers (
	id           INTEGER PRIMARY KEY,
	guid         TEXT NOT NULL UNIQUE,
	name         TEXT NOT NULL,
	slug         TEXT NOT NULL UNIQUE COLLATE NOCASE,
	id_server    TEXT NOT NULL DEFAULT '',
	relay_server TEXT NOT NULL DEFAULT '',
	key          TEXT NOT NULL DEFAULT '',
	note         TEXT NOT NULL DEFAULT '',
	created_at   INTEGER NOT NULL
);
ALTER TABLE devices ADD COLUMN server_id INTEGER REFERENCES servers(id) ON DELETE SET NULL;
CREATE INDEX devices_server ON devices(server_id);
`,
	// 3: OpenID Connect login providers.
	`
CREATE TABLE oidc_providers (
	id            INTEGER PRIMARY KEY,
	guid          TEXT NOT NULL UNIQUE,
	name          TEXT NOT NULL UNIQUE COLLATE NOCASE,
	issuer        TEXT NOT NULL,
	client_id     TEXT NOT NULL,
	client_secret TEXT NOT NULL DEFAULT '',
	scopes        TEXT NOT NULL DEFAULT 'openid profile email',
	icon          TEXT NOT NULL DEFAULT '',
	auto_create   INTEGER NOT NULL DEFAULT 0,
	enabled       INTEGER NOT NULL DEFAULT 1,
	created_at    INTEGER NOT NULL
);
CREATE TABLE oidc_identities (
	provider_id INTEGER NOT NULL REFERENCES oidc_providers(id) ON DELETE CASCADE,
	subject     TEXT NOT NULL,
	user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at  INTEGER NOT NULL,
	PRIMARY KEY (provider_id, subject)
);
`,
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var version int
	err := s.db.QueryRowContext(ctx, `SELECT version FROM schema_version`).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES (0)`); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE schema_version SET version = ?`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func now() int64 { return time.Now().Unix() }

// NewGUID returns a random RFC 4122 version 4 UUID string.
func NewGUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Page is a 1-based pagination request.
type Page struct {
	Current  int
	PageSize int
}

func (p Page) limitOffset() (int, int) {
	size := p.PageSize
	if size <= 0 || size > 1000 {
		size = 100
	}
	cur := p.Current
	if cur <= 0 {
		cur = 1
	}
	return size, (cur - 1) * size
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func isUniqueErr(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "UNIQUE constraint failed") ||
		strings.Contains(err.Error(), "PRIMARY KEY"))
}

// GetSetting returns a stored setting or def when it is missing.
func (s *Store) GetSetting(ctx context.Context, key, def string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
