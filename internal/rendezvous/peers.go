package rendezvous

import (
	"context"
	"net/netip"
	"sync"
	"time"
)

// PeerStore persists the identity each ID registered with, so a restart does
// not let another machine take over an ID.
type PeerStore interface {
	LoadPeer(ctx context.Context, id string) (uuid, pk []byte, ok bool, err error)
	SavePeer(ctx context.Context, id string, uuid, pk []byte, ip string) error
}

// regTimeout is how long after its last heartbeat a peer counts as online;
// clients re-register every 12 seconds or so.
const regTimeout = 30 * time.Second

type peer struct {
	id       string
	addr     netip.AddrPort // UDP address of the last registration
	lastReg  time.Time
	uuid, pk []byte
	ip       string      // IP the pk was registered from
	loaded   bool        // uuid/pk are known (from the store or a registration)
	tcp      *tcpSession // registration connection of a peer in TCP mode

	pkTries  int // RegisterPk attempts in the current window
	pkWindow time.Time
}

type peerMap struct {
	mu    sync.Mutex
	peers map[string]*peer
	store PeerStore
}

func newPeerMap(store PeerStore) *peerMap {
	return &peerMap{peers: map[string]*peer{}, store: store}
}

// get returns the peer, loading its identity from the store on first use.
// It returns nil for an ID that never registered.
func (m *peerMap) get(ctx context.Context, id string) *peer {
	m.mu.Lock()
	p := m.peers[id]
	m.mu.Unlock()
	if p != nil {
		return p
	}
	if m.store == nil {
		return nil
	}
	uuid, pk, ok, err := m.store.LoadPeer(ctx, id)
	if err != nil || !ok {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if p = m.peers[id]; p == nil {
		p = &peer{id: id, uuid: uuid, pk: pk, loaded: true}
		m.peers[id] = p
	}
	return p
}

// getOrNew is get, creating an empty in-memory entry for unknown IDs.
func (m *peerMap) getOrNew(ctx context.Context, id string) *peer {
	if p := m.get(ctx, id); p != nil {
		return p
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if p := m.peers[id]; p != nil {
		return p
	}
	p := &peer{id: id}
	m.peers[id] = p
	return p
}

// online reports whether id registered recently, and its UDP address.
func (m *peerMap) online(id string) (netip.AddrPort, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.peers[id]
	if p == nil || !p.isOnline() {
		return netip.AddrPort{}, false
	}
	return p.addr, true
}

// OnlineIDs lists the IDs that registered recently.
func (m *peerMap) onlineIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for id, p := range m.peers {
		if p.isOnline() {
			out = append(out, id)
		}
	}
	return out
}

// isOnline needs the map lock held.
func (p *peer) isOnline() bool {
	return p.tcp != nil || !p.lastReg.IsZero() && time.Since(p.lastReg) < regTimeout
}
