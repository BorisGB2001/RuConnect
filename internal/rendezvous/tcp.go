package rendezvous

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/BorisGB2001/RuConnect/internal/rendezvous/pb"
)

const (
	// idleTimeout closes request connections that go quiet.
	idleTimeout = 30 * time.Second
	// keepAlive is what registered TCP peers are told (seconds); they drop
	// the connection after 1.5x without traffic, so we ping more often.
	keepAlive         = 30
	heartbeatInterval = 15 * time.Second
)

// tcpSession is one client connection on the rendezvous port.
type tcpSession struct {
	s      *Server
	conn   net.Conn
	r      *bufio.Reader
	remote netip.AddrPort
	local  netip.AddrPort

	wmu    sync.Mutex
	cipher *cipherState // set after a key exchange
	closed bool

	regID string // set when the peer registers over this connection
}

func (t *tcpSession) send(m *pb.RendezvousMessage) error {
	b := marshal(m)
	t.wmu.Lock()
	defer t.wmu.Unlock()
	if t.closed {
		return net.ErrClosed
	}
	if t.cipher != nil {
		b = t.cipher.seal(b)
	}
	t.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return writeFrame(t.conn, b)
}

func (t *tcpSession) heartbeat() error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	t.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return writeFrame(t.conn, nil)
}

func (t *tcpSession) close() {
	t.wmu.Lock()
	t.closed = true
	t.wmu.Unlock()
	t.conn.Close()
}

// ServeTCP handles the rendezvous TCP port: key exchange, connection
// requests, peer replies and registrations from clients in TCP mode.
func (s *Server) ServeTCP(ctx context.Context, ln net.Listener) error {
	return serveListener(ctx, ln, func(c net.Conn) { s.handleTCP(ctx, c) })
}

// ServeAux handles the port below the rendezvous port: NAT type tests and
// online status queries.
func (s *Server) ServeAux(ctx context.Context, ln net.Listener) error {
	return serveListener(ctx, ln, func(c net.Conn) { s.handleAux(c) })
}

func serveListener(ctx context.Context, ln net.Listener, handle func(net.Conn)) error {
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		c, err := ln.Accept()
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
		if tc, ok := c.(*net.TCPConn); ok {
			tc.SetNoDelay(true)
		}
		go handle(c)
	}
}

func addrPort(a net.Addr) netip.AddrPort {
	if ta, ok := a.(*net.TCPAddr); ok {
		return normalize(ta.AddrPort())
	}
	ap, _ := netip.ParseAddrPort(a.String())
	return normalize(ap)
}

func (s *Server) handleTCP(ctx context.Context, c net.Conn) {
	t := &tcpSession{s: s, conn: c, r: bufio.NewReader(c), remote: addrPort(c.RemoteAddr()), local: addrPort(c.LocalAddr())}
	defer s.dropSession(t)

	hello, err := newServerHello(s.Key)
	if err != nil {
		return
	}
	// Clients that do not secure the connection skip one KeyExchange frame.
	if err := t.send(hello.msg); err != nil {
		return
	}
	first := true
	for {
		deadline := idleTimeout
		if t.regID != "" {
			deadline = 2 * keepAlive * time.Second
		}
		c.SetReadDeadline(time.Now().Add(deadline))
		frame, err := readFrame(t.r, MaxFrame)
		if err != nil {
			return
		}
		if len(frame) == 0 { // heartbeat (or its echo)
			s.touchTCPPeer(t)
			continue
		}
		if t.cipher != nil {
			if frame, err = t.cipher.open(frame); err != nil {
				return
			}
		}
		var msg pb.RendezvousMessage
		if proto.Unmarshal(frame, &msg) != nil {
			return
		}
		if first {
			first = false
			if ex := msg.GetKeyExchange(); ex != nil {
				cs, err := hello.finish(ex)
				if err != nil {
					s.Log.Debug("rendezvous: key exchange failed", "remote", t.remote, "err", err)
					return
				}
				t.wmu.Lock()
				t.cipher = cs
				t.wmu.Unlock()
				continue
			}
		}
		s.touchTCPPeer(t)
		if !s.handleTCPMessage(ctx, t, &msg) {
			return
		}
	}
}

// handleTCPMessage returns false to close the connection.
func (s *Server) handleTCPMessage(ctx context.Context, t *tcpSession, msg *pb.RendezvousMessage) bool {
	switch u := msg.Union.(type) {
	case *pb.RendezvousMessage_PunchHoleRequest:
		s.punchHoleRequest(ctx, t, u.PunchHoleRequest)
	case *pb.RendezvousMessage_RequestRelay:
		s.requestRelay(ctx, t, u.RequestRelay)
	case *pb.RendezvousMessage_RelayResponse:
		s.relayResponse(ctx, t, u.RelayResponse)
	case *pb.RendezvousMessage_PunchHoleSent:
		s.punchHoleSent(ctx, u.PunchHoleSent, t.remote, false)
	case *pb.RendezvousMessage_LocalAddr:
		s.localAddr(ctx, u.LocalAddr, t.remote, false)
	case *pb.RendezvousMessage_TestNatRequest:
		t.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_TestNatResponse{
			TestNatResponse: &pb.TestNatResponse{Port: int32(t.remote.Port())}}})
	case *pb.RendezvousMessage_RegisterPk:
		res := s.registerPk(ctx, u.RegisterPk, t.remote)
		if res == nil {
			return false
		}
		if res.GetRegisterPkResponse().Result == pb.RegisterPkResponse_OK {
			s.attachTCPPeer(t, u.RegisterPk.Id)
			res.GetRegisterPkResponse().KeepAlive = keepAlive
		}
		t.send(res)
	case *pb.RendezvousMessage_Hc:
		// Only sent when we ask for feedback, which we do not.
	}
	return true
}

// Registered TCP peers.

func (s *Server) attachTCPPeer(t *tcpSession, id string) {
	if t.regID == "" {
		go func() {
			tick := time.NewTicker(heartbeatInterval)
			defer tick.Stop()
			for range tick.C {
				if t.heartbeat() != nil {
					return
				}
			}
		}()
	}
	t.regID = id
	s.peers.mu.Lock()
	if p := s.peers.peers[id]; p != nil {
		p.tcp = t
		p.lastReg = time.Now()
	}
	s.peers.mu.Unlock()
}

func (s *Server) touchTCPPeer(t *tcpSession) {
	if t.regID == "" {
		return
	}
	s.peers.mu.Lock()
	if p := s.peers.peers[t.regID]; p != nil && p.tcp == t {
		p.lastReg = time.Now()
	}
	s.peers.mu.Unlock()
}

func (s *Server) dropSession(t *tcpSession) {
	t.close()
	s.mu.Lock()
	if s.waiters[t.remote] == t {
		delete(s.waiters, t.remote)
	}
	s.mu.Unlock()
	if t.regID != "" {
		s.peers.mu.Lock()
		if p := s.peers.peers[t.regID]; p != nil && p.tcp == t {
			p.tcp = nil
			p.lastReg = time.Time{}
		}
		s.peers.mu.Unlock()
	}
}

// sendToPeer delivers a message to a registered peer over its TCP
// registration, or else to its UDP address.
func (s *Server) sendToPeer(id string, m *pb.RendezvousMessage) bool {
	s.peers.mu.Lock()
	p := s.peers.peers[id]
	var tcp *tcpSession
	var addr netip.AddrPort
	if p != nil {
		tcp, addr = p.tcp, p.addr
	}
	s.peers.mu.Unlock()
	if tcp != nil {
		return tcp.send(m) == nil
	}
	if !addr.IsValid() {
		return false
	}
	s.sendUDP(m, addr)
	return true
}

// Controlling side.

func (s *Server) wait(t *tcpSession) {
	s.mu.Lock()
	s.waiters[t.remote] = t
	s.mu.Unlock()
}

func (s *Server) waiter(addr netip.AddrPort) *tcpSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waiters[addr]
}

func punchFailure(f pb.PunchHoleResponse_Failure, other string) *pb.RendezvousMessage {
	return &pb.RendezvousMessage{Union: &pb.RendezvousMessage_PunchHoleResponse{
		PunchHoleResponse: &pb.PunchHoleResponse{Failure: f, OtherFailure: other}}}
}

// relayAddress is the relay handed out for a session that came in on t.
func (s *Server) relayAddress(t *tcpSession) string {
	if r := s.relayFor(); r != "" {
		return r
	}
	if s.RelayPort > 0 && t.local.IsValid() && !t.local.Addr().IsUnspecified() {
		return net.JoinHostPort(t.local.Addr().String(), strconv.Itoa(s.RelayPort))
	}
	return ""
}

func (s *Server) punchHoleRequest(ctx context.Context, t *tcpSession, ph *pb.PunchHoleRequest) {
	if ph.LicenceKey != s.keyStr {
		s.Log.Warn("rendezvous: key mismatch", "remote", t.remote, "peer", ph.Id)
		t.send(punchFailure(pb.PunchHoleResponse_LICENSE_MISMATCH, ""))
		return
	}
	if s.Authorize != nil {
		if reason := s.Authorize(ctx, Request{Token: ph.Token, PeerID: ph.Id, From: t.remote.Addr()}); reason != "" {
			t.send(punchFailure(pb.PunchHoleResponse_ID_NOT_EXIST, reason))
			return
		}
	}
	if s.peers.get(ctx, ph.Id) == nil {
		t.send(punchFailure(pb.PunchHoleResponse_ID_NOT_EXIST, ""))
		return
	}
	peerAddr, online := s.peerAddr(ph.Id)
	if !online {
		t.send(punchFailure(pb.PunchHoleResponse_OFFLINE, ""))
		return
	}
	s.wait(t)
	relay := s.relayAddress(t)
	s.Log.Debug("rendezvous: connect request", "from", t.remote, "peer", ph.Id, "peer_addr", peerAddr,
		"nat", ph.NatType, "secure", t.cipher != nil, "relay", relay)
	natType := ph.NatType
	if s.AlwaysRelay {
		natType = pb.NatType_SYMMETRIC
	}
	from := encodeAddr(t.remote)
	var out *pb.RendezvousMessage
	if peerAddr.Addr() == t.remote.Addr() {
		// Same public address: hole punching between them cannot work, ask
		// the peer for its LAN address instead.
		out = &pb.RendezvousMessage{Union: &pb.RendezvousMessage_FetchLocalAddr{
			FetchLocalAddr: &pb.FetchLocalAddr{SocketAddr: from, RelayServer: relay}}}
	} else {
		out = &pb.RendezvousMessage{Union: &pb.RendezvousMessage_PunchHole{
			PunchHole: &pb.PunchHole{SocketAddr: from, NatType: natType, RelayServer: relay, ForceRelay: s.AlwaysRelay}}}
	}
	if !s.sendToPeer(ph.Id, out) {
		t.send(punchFailure(pb.PunchHoleResponse_OFFLINE, ""))
	}
}

// peerAddr returns the address the peer's registration comes from.
func (s *Server) peerAddr(id string) (netip.AddrPort, bool) {
	s.peers.mu.Lock()
	defer s.peers.mu.Unlock()
	p := s.peers.peers[id]
	if p == nil || !p.isOnline() {
		return netip.AddrPort{}, false
	}
	if p.tcp != nil {
		return p.tcp.remote, true
	}
	return p.addr, true
}

func (s *Server) requestRelay(ctx context.Context, t *tcpSession, rr *pb.RequestRelay) {
	if s.Authorize != nil {
		if reason := s.Authorize(ctx, Request{Token: rr.Token, PeerID: rr.Id, From: t.remote.Addr()}); reason != "" {
			t.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_RelayResponse{
				RelayResponse: &pb.RelayResponse{RefuseReason: reason}}})
			return
		}
	}
	if _, online := s.peerAddr(rr.Id); !online {
		t.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_RelayResponse{
			RelayResponse: &pb.RelayResponse{RefuseReason: "Remote desktop is offline"}}})
		return
	}
	s.wait(t)
	s.Log.Debug("rendezvous: relay request", "from", t.remote, "peer", rr.Id, "relay", rr.RelayServer)
	fwd := proto.Clone(rr).(*pb.RequestRelay)
	fwd.SocketAddr = encodeAddr(t.remote)
	fwd.Token = "" // the controlled side has no use for the controller's API token
	if fwd.RelayServer == "" {
		fwd.RelayServer = s.relayAddress(t)
	}
	s.sendToPeer(rr.Id, &pb.RendezvousMessage{Union: &pb.RendezvousMessage_RequestRelay{RequestRelay: fwd}})
}

// Controlled side replies, routed back to the waiting controller.

func (s *Server) relayResponse(ctx context.Context, t *tcpSession, rr *pb.RelayResponse) {
	to, ok := decodeAddr(rr.SocketAddr)
	if !ok {
		return
	}
	w := s.waiter(to)
	s.Log.Debug("rendezvous: relay response", "from", t.remote, "to", to, "waiting", w != nil, "relay", rr.RelayServer)
	if w == nil {
		return
	}
	out := proto.Clone(rr).(*pb.RelayResponse)
	out.SocketAddr = nil
	if id := rr.GetId(); id != "" {
		out.Union = &pb.RelayResponse_Pk{Pk: s.signedPK(ctx, id)}
	}
	if out.RelayServer == "" {
		out.RelayServer = s.relayAddress(t)
	}
	w.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_RelayResponse{RelayResponse: out}})
}

func (s *Server) punchHoleSent(ctx context.Context, phs *pb.PunchHoleSent, from netip.AddrPort, udp bool) {
	to, ok := decodeAddr(phs.SocketAddr)
	if !ok {
		return
	}
	w := s.waiter(to)
	s.Log.Debug("rendezvous: punch hole sent", "from", from, "to", to, "waiting", w != nil)
	if w == nil {
		return
	}
	relay := phs.RelayServer
	if relay == "" {
		relay = s.relayAddress(w)
	}
	resp := &pb.PunchHoleResponse{
		SocketAddr:  encodeAddr(from),
		Pk:          s.signedPK(ctx, phs.Id),
		RelayServer: relay,
		Union:       &pb.PunchHoleResponse_NatType{NatType: phs.NatType},
		UpnpPort:    phs.UpnpPort,
		IsUdp:       udp,
	}
	w.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_PunchHoleResponse{PunchHoleResponse: resp}})
}

func (s *Server) localAddr(ctx context.Context, la *pb.LocalAddr, from netip.AddrPort, udp bool) {
	to, ok := decodeAddr(la.SocketAddr)
	if !ok {
		return
	}
	w := s.waiter(to)
	s.Log.Debug("rendezvous: local addr", "from", from, "to", to, "waiting", w != nil)
	if w == nil {
		return
	}
	relay := la.RelayServer
	if relay == "" {
		relay = s.relayAddress(w)
	}
	resp := &pb.PunchHoleResponse{
		SocketAddr:  la.LocalAddr,
		Pk:          s.signedPK(ctx, la.Id),
		RelayServer: relay,
		Union:       &pb.PunchHoleResponse_IsLocal{IsLocal: true},
	}
	w.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_PunchHoleResponse{PunchHoleResponse: resp}})
}

// Aux port: NAT tests and online queries, one request per connection.

func (s *Server) handleAux(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	c.SetReadDeadline(time.Now().Add(idleTimeout))
	frame, err := readFrame(r, MaxFrame)
	if err != nil {
		return
	}
	var msg pb.RendezvousMessage
	if proto.Unmarshal(frame, &msg) != nil {
		return
	}
	var out *pb.RendezvousMessage
	switch u := msg.Union.(type) {
	case *pb.RendezvousMessage_TestNatRequest:
		out = &pb.RendezvousMessage{Union: &pb.RendezvousMessage_TestNatResponse{
			TestNatResponse: &pb.TestNatResponse{Port: int32(addrPort(c.RemoteAddr()).Port())}}}
	case *pb.RendezvousMessage_OnlineRequest:
		peers := u.OnlineRequest.Peers
		if len(peers) > 10000 {
			peers = peers[:10000]
		}
		states := make([]byte, (len(peers)+7)/8)
		for i, id := range peers {
			if s.IsOnline(id) {
				states[i/8] |= 1 << (7 - i%8)
			}
		}
		out = &pb.RendezvousMessage{Union: &pb.RendezvousMessage_OnlineResponse{
			OnlineResponse: &pb.OnlineResponse{States: states}}}
	default:
		return
	}
	c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	writeFrame(c, marshal(out))
}
