package store

import (
	"context"
	"database/sql"
	"errors"
)

// RendezvousPeer is the identity a RustDesk ID registered with on the
// built-in rendezvous server: the machine uuid and its public key.
type RendezvousPeer struct {
	ID   string
	UUID []byte
	PK   []byte
	IP   string
}

func (s *Store) RendezvousPeer(ctx context.Context, id string) (*RendezvousPeer, error) {
	p := RendezvousPeer{ID: id}
	err := s.db.QueryRowContext(ctx, `SELECT uuid, pk, ip FROM rendezvous_peers WHERE id = ?`, id).Scan(&p.UUID, &p.PK, &p.IP)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (s *Store) SaveRendezvousPeer(ctx context.Context, p *RendezvousPeer) error {
	t := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO rendezvous_peers (id, uuid, pk, ip, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET uuid = excluded.uuid, pk = excluded.pk, ip = excluded.ip, updated_at = excluded.updated_at`,
		p.ID, p.UUID, p.PK, p.IP, t, t)
	return err
}

func (s *Store) DeleteRendezvousPeer(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM rendezvous_peers WHERE id = ?`, id)
	return err
}
