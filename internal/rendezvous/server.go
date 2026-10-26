package rendezvous

import (
	"context"
	"crypto/ed25519"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/BorisGB2001/RuConnect/internal/rendezvous/pb"
)

// Server is a RustDesk-compatible rendezvous (ID) server.
type Server struct {
	Key ed25519.PrivateKey
	// RelayServers are handed to clients for relayed sessions (host[:port]).
	RelayServers []string
	// RelayPort is the built-in relay's port, used to hand out this host as
	// the relay when RelayServers is empty (0: no built-in relay).
	RelayPort int
	// AlwaysRelay forces every session through the relay.
	AlwaysRelay bool
	// Authorize, when set, vets each connection request; a non-empty result
	// refuses it and is shown to the user.
	Authorize func(ctx context.Context, r Request) string
	// LocalHost reports whether a host name (from "ID@host" peer ids) is
	// this server; RemoteHost whether it is another server we may query
	// for online status. Both receive the host without port.
	LocalHost, RemoteHost func(host string) bool
	Log                   *slog.Logger

	keyStr string
	peers  *peerMap
	udp    *net.UDPConn

	mu      sync.Mutex
	waiters map[netip.AddrPort]*tcpSession // controllers waiting for a reply, by their TCP address
	relayRR uint64

	limiter  *ipLimiter
	fedCache onlineCache
}

// Request describes a connection request for Authorize.
type Request struct {
	Token  string // the controlling client's API access token, if logged in
	PeerID string
	From   netip.Addr
}

func NewServer(key ed25519.PrivateKey, store PeerStore, log *slog.Logger) *Server {
	return &Server{
		Key:     key,
		Log:     log,
		keyStr:  PublicKeyString(key),
		peers:   newPeerMap(store),
		waiters: map[netip.AddrPort]*tcpSession{},
		limiter: newIPLimiter(),
	}
}

// IsOnline reports whether id is registered and sent a heartbeat recently.
func (s *Server) IsOnline(id string) bool {
	_, ok := s.peers.online(id)
	return ok
}

// OnlineIDs lists the IDs that are currently online.
func (s *Server) OnlineIDs() []string { return s.peers.onlineIDs() }

func marshal(m *pb.RendezvousMessage) []byte {
	b, _ := proto.Marshal(m)
	return b
}

func (s *Server) sendUDP(m *pb.RendezvousMessage, to netip.AddrPort) {
	if s.udp == nil || !to.IsValid() {
		return
	}
	if _, err := s.udp.WriteToUDPAddrPort(marshal(m), to); err != nil {
		s.Log.Debug("rendezvous: udp send", "to", to, "err", err)
	}
}

// ServeUDP handles registrations and peer replies on the UDP port.
func (s *Server) ServeUDP(ctx context.Context, conn *net.UDPConn) error {
	s.udp = conn
	go func() { <-ctx.Done(); conn.Close() }()
	buf := make([]byte, 64<<10)
	for {
		n, from, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		var msg pb.RendezvousMessage
		if proto.Unmarshal(buf[:n], &msg) != nil {
			continue
		}
		s.handleUDP(ctx, &msg, normalize(from))
	}
}

func normalize(ap netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}

func (s *Server) handleUDP(ctx context.Context, msg *pb.RendezvousMessage, from netip.AddrPort) {
	switch u := msg.Union.(type) {
	case *pb.RendezvousMessage_RegisterPeer:
		s.registerPeer(ctx, u.RegisterPeer, from)
	case *pb.RendezvousMessage_RegisterPk:
		s.sendUDP(s.registerPk(ctx, u.RegisterPk, from), from)
	case *pb.RendezvousMessage_PunchHoleSent:
		s.punchHoleSent(ctx, u.PunchHoleSent, from, true)
	case *pb.RendezvousMessage_LocalAddr:
		s.localAddr(ctx, u.LocalAddr, from, true)
	}
}

// registerPeer is the controlled side's heartbeat.
func (s *Server) registerPeer(ctx context.Context, rp *pb.RegisterPeer, from netip.AddrPort) {
	if !validID(rp.Id) {
		return
	}
	p := s.peers.get(ctx, rp.Id)
	requestPK := true
	if p != nil {
		s.peers.mu.Lock()
		ipChanged := p.ip != "" && p.ip != from.Addr().String() && !from.Addr().IsLoopback()
		requestPK = len(p.pk) == 0 || ipChanged
		if !requestPK {
			p.addr = from
			p.lastReg = time.Now()
		}
		s.peers.mu.Unlock()
	}
	s.sendUDP(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_RegisterPeerResponse{
		RegisterPeerResponse: &pb.RegisterPeerResponse{RequestPk: requestPK}}}, from)
}

func pkResponse(r pb.RegisterPkResponse_Result) *pb.RendezvousMessage {
	return &pb.RendezvousMessage{Union: &pb.RendezvousMessage_RegisterPkResponse{
		RegisterPkResponse: &pb.RegisterPkResponse{Result: r}}}
}

// registerPk binds an ID to the machine (uuid) and key that first claimed it.
func (s *Server) registerPk(ctx context.Context, rk *pb.RegisterPk, from netip.AddrPort) *pb.RendezvousMessage {
	if len(rk.Uuid) == 0 || len(rk.Pk) == 0 || len(rk.Uuid) > 256 || len(rk.Pk) > 256 {
		return nil
	}
	if !validID(rk.Id) {
		return pkResponse(pb.RegisterPkResponse_INVALID_ID_FORMAT)
	}
	ip := from.Addr().String()
	if !s.limiter.allow(ip, rk.Id) {
		return pkResponse(pb.RegisterPkResponse_TOO_FREQUENT)
	}
	p := s.peers.getOrNew(ctx, rk.Id)
	s.peers.mu.Lock()
	if len(p.uuid) > 0 && string(p.uuid) != string(rk.Uuid) {
		s.peers.mu.Unlock()
		s.Log.Warn("rendezvous: id claimed by another machine", "id", rk.Id, "ip", ip)
		return pkResponse(pb.RegisterPkResponse_UUID_MISMATCH)
	}
	if len(p.pk) > 0 && string(p.pk) != string(rk.Pk) && p.ip != "" && p.ip != ip {
		// Same machine id but a new key from a new address: likely a copied
		// uuid, not the registered machine.
		s.peers.mu.Unlock()
		s.Log.Warn("rendezvous: key change from a new address refused", "id", rk.Id, "ip", ip)
		return pkResponse(pb.RegisterPkResponse_UUID_MISMATCH)
	}
	if time.Since(p.pkWindow) > 6*time.Second {
		p.pkTries = 0
	} else if p.pkTries > 2 {
		s.peers.mu.Unlock()
		return pkResponse(pb.RegisterPkResponse_TOO_FREQUENT)
	}
	p.pkTries++
	p.pkWindow = time.Now()
	changed := len(p.uuid) == 0 || string(p.pk) != string(rk.Pk) || p.ip != ip
	p.uuid, p.pk, p.ip, p.loaded = rk.Uuid, rk.Pk, ip, true
	p.addr = from
	p.lastReg = time.Now()
	uuid, pk := p.uuid, p.pk
	s.peers.mu.Unlock()
	if changed && s.peers.store != nil {
		if err := s.peers.store.SavePeer(ctx, rk.Id, uuid, pk, ip); err != nil {
			s.Log.Error("rendezvous: save peer", "id", rk.Id, "err", err)
			return pkResponse(pb.RegisterPkResponse_SERVER_ERROR)
		}
	}
	return pkResponse(pb.RegisterPkResponse_OK)
}

// validID accepts RustDesk IDs: 6-100 characters, letters, digits, - and _.
func validID(id string) bool {
	if len(id) < 6 || len(id) > 100 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// signedPK returns the peer's public key signed by the server, which lets the
// controlling client verify it talks to the machine registered under id.
func (s *Server) signedPK(ctx context.Context, id string) []byte {
	p := s.peers.get(ctx, id)
	if p == nil {
		return nil
	}
	s.peers.mu.Lock()
	pk := p.pk
	s.peers.mu.Unlock()
	if len(pk) == 0 {
		return nil
	}
	b, _ := proto.Marshal(&pb.IdPk{Id: id, Pk: pk})
	return signCombined(s.Key, b)
}

// relayFor picks the relay server for a session (round robin).
func (s *Server) relayFor() string {
	if len(s.RelayServers) == 0 {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.relayRR++
	return s.RelayServers[s.relayRR%uint64(len(s.RelayServers))]
}

// ipLimiter throttles RegisterPk per source IP: bursts of attempts and the
// number of distinct IDs one IP may claim per day.
type ipLimiter struct {
	mu sync.Mutex
	m  map[string]*ipState
}

type ipState struct {
	n      int
	window time.Time
	ids    map[string]bool
	idsDay time.Time
}

func newIPLimiter() *ipLimiter { return &ipLimiter{m: map[string]*ipState{}} }

func (l *ipLimiter) allow(ip, id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	st := l.m[ip]
	if st == nil {
		if len(l.m) > 100000 {
			for k, v := range l.m {
				if now.Sub(v.window) > time.Minute && now.Sub(v.idsDay) > 24*time.Hour {
					delete(l.m, k)
				}
			}
		}
		st = &ipState{window: now, ids: map[string]bool{}, idsDay: now}
		l.m[ip] = st
	}
	if now.Sub(st.window) > time.Minute {
		st.n, st.window = 0, now
	}
	if st.n++; st.n > 30 {
		return false
	}
	if now.Sub(st.idsDay) > 24*time.Hour {
		st.ids, st.idsDay = map[string]bool{}, now
	}
	if !st.ids[id] {
		if len(st.ids) >= 300 {
			return false
		}
		st.ids[id] = true
	}
	return true
}
