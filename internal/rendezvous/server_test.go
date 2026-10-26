package rendezvous

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/BorisGB2001/RuConnect/internal/rendezvous/pb"
)

type memStore struct {
	mu sync.Mutex
	m  map[string][2][]byte
}

func (m *memStore) LoadPeer(_ context.Context, id string) ([]byte, []byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.m[id]
	return v[0], v[1], ok, nil
}

func (m *memStore) SavePeer(_ context.Context, id string, uuid, pk []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.m[id] = [2][]byte{uuid, pk}
	return nil
}

type testEnv struct {
	srv           *Server
	pub           ed25519.PublicKey
	tcp, aux, udp string
}

func startServer(t *testing.T) *testEnv {
	t.Helper()
	pub, sk, _ := ed25519.GenerateKey(rand.Reader)
	s := NewServer(sk, &memStore{m: map[string][2][]byte{}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.RelayServers = []string{"relay.example:21117"}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	aux, _ := net.Listen("tcp", "127.0.0.1:0")
	uc, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	go s.ServeTCP(ctx, ln)
	go s.ServeAux(ctx, aux)
	go s.ServeUDP(ctx, uc)
	return &testEnv{srv: s, pub: pub, tcp: ln.Addr().String(), aux: aux.Addr().String(), udp: uc.LocalAddr().String()}
}

// tclient is a TCP rendezvous connection as the stock client drives it.
type tclient struct {
	t      *testing.T
	c      net.Conn
	r      *bufio.Reader
	cipher *cipherState
}

func dialRendezvous(t *testing.T, env *testEnv, secure bool) *tclient {
	t.Helper()
	c, err := net.Dial("tcp", env.tcp)
	if err != nil {
		t.Fatal(err)
	}
	tc := &tclient{t: t, c: c, r: bufio.NewReader(c)}
	hello := tc.recv().GetKeyExchange()
	if hello == nil {
		t.Fatal("server did not open with KeyExchange")
	}
	if secure {
		reply, cs := clientKX(t, env.pub, hello, 1)
		tc.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_KeyExchange{KeyExchange: reply}})
		tc.cipher = cs
	}
	return tc
}

func (tc *tclient) send(m *pb.RendezvousMessage) {
	b := marshal(m)
	if tc.cipher != nil && m.GetKeyExchange() == nil {
		b = tc.cipher.seal(b)
	}
	if err := writeFrame(tc.c, b); err != nil {
		tc.t.Fatal(err)
	}
}

func (tc *tclient) recv() *pb.RendezvousMessage {
	tc.t.Helper()
	for {
		tc.c.SetReadDeadline(time.Now().Add(3 * time.Second))
		b, err := readFrame(tc.r, MaxFrame)
		if err != nil {
			tc.t.Fatalf("recv: %v", err)
		}
		if len(b) == 0 {
			continue
		}
		if tc.cipher != nil {
			if b, err = tc.cipher.open(b); err != nil {
				tc.t.Fatal(err)
			}
		}
		var m pb.RendezvousMessage
		if err := proto.Unmarshal(b, &m); err != nil {
			tc.t.Fatal(err)
		}
		return &m
	}
}

// udpPeer is a controlled client registered over UDP.
type udpPeer struct {
	t  *testing.T
	c  *net.UDPConn
	id string
	pk ed25519.PublicKey
}

func registerUDP(t *testing.T, env *testEnv, id string) *udpPeer {
	t.Helper()
	ra, _ := net.ResolveUDPAddr("udp", env.udp)
	c, err := net.DialUDP("udp", nil, ra)
	if err != nil {
		t.Fatal(err)
	}
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	p := &udpPeer{t: t, c: c, id: id, pk: pk}
	p.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_RegisterPk{RegisterPk: &pb.RegisterPk{
		Id: id, Uuid: []byte("uuid-" + id), Pk: pk}}})
	if r := p.recv().GetRegisterPkResponse(); r == nil || r.Result != pb.RegisterPkResponse_OK {
		t.Fatalf("register: %v", r)
	}
	p.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_RegisterPeer{RegisterPeer: &pb.RegisterPeer{Id: id}}})
	if r := p.recv().GetRegisterPeerResponse(); r == nil || r.RequestPk {
		t.Fatalf("heartbeat: %v", r)
	}
	return p
}

func (p *udpPeer) send(m *pb.RendezvousMessage) { p.c.Write(marshal(m)) }

func (p *udpPeer) recv() *pb.RendezvousMessage {
	p.t.Helper()
	buf := make([]byte, 65536)
	p.c.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := p.c.Read(buf)
	if err != nil {
		p.t.Fatalf("udp recv: %v", err)
	}
	var m pb.RendezvousMessage
	proto.Unmarshal(buf[:n], &m)
	return &m
}

func TestPunchHoleFlow(t *testing.T) {
	env := startServer(t)
	peer := registerUDP(t, env, "123456789")

	ctl := dialRendezvous(t, env, true)
	ctl.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_PunchHoleRequest{PunchHoleRequest: &pb.PunchHoleRequest{
		Id: "123456789", LicenceKey: PublicKeyString(env.srv.Key), Version: "1.5.0", Token: "t"}}})

	// Both ends are on 127.0.0.1, so the server asks for the LAN address.
	fla := peer.recv().GetFetchLocalAddr()
	if fla == nil || fla.RelayServer != "relay.example:21117" {
		t.Fatalf("peer got %v", fla)
	}
	// The controlled side answers on a fresh plaintext connection.
	reply := dialRendezvous(t, env, false)
	reply.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_LocalAddr{LocalAddr: &pb.LocalAddr{
		Id: "123456789", SocketAddr: fla.SocketAddr, LocalAddr: encodeAddr(mustAP("192.168.1.5:40000")),
		RelayServer: fla.RelayServer, Version: "1.5.0"}}})

	res := ctl.recv().GetPunchHoleResponse()
	if res == nil || !res.GetIsLocal() || res.RelayServer != "relay.example:21117" {
		t.Fatalf("controller got %v", res)
	}
	if ap, _ := decodeAddr(res.SocketAddr); ap.String() != "192.168.1.5:40000" {
		t.Fatalf("local addr %s", ap)
	}
	// The peer key comes signed by the server.
	if len(res.Pk) < 64 || !ed25519.Verify(env.pub, res.Pk[64:], res.Pk[:64]) {
		t.Fatal("pk not signed")
	}
	var idpk pb.IdPk
	proto.Unmarshal(res.Pk[64:], &idpk)
	if idpk.Id != "123456789" || string(idpk.Pk) != string(peer.pk) {
		t.Fatalf("signed IdPk %v", &idpk)
	}
}

func TestRelayFlowAndFailures(t *testing.T) {
	env := startServer(t)
	key := PublicKeyString(env.srv.Key)
	peer := registerUDP(t, env, "222333444")

	// Unknown id, wrong key.
	c := dialRendezvous(t, env, false)
	c.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_PunchHoleRequest{PunchHoleRequest: &pb.PunchHoleRequest{Id: "999999999", LicenceKey: key}}})
	if r := c.recv().GetPunchHoleResponse(); r == nil || r.Failure != pb.PunchHoleResponse_ID_NOT_EXIST {
		t.Fatalf("unknown id: %v", r)
	}
	c.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_PunchHoleRequest{PunchHoleRequest: &pb.PunchHoleRequest{Id: "222333444", LicenceKey: "nope"}}})
	if r := c.recv().GetPunchHoleResponse(); r == nil || r.Failure != pb.PunchHoleResponse_LICENSE_MISMATCH {
		t.Fatalf("bad key: %v", r)
	}

	// RequestRelay from the controller is forwarded; the RelayResponse goes back.
	ctl := dialRendezvous(t, env, true)
	ctl.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_RequestRelay{RequestRelay: &pb.RequestRelay{
		Id: "222333444", Uuid: "u-1", RelayServer: "relay.example:21117", Secure: true, Token: "secret"}}})
	rr := peer.recv().GetRequestRelay()
	if rr == nil || rr.Uuid != "u-1" || rr.Token != "" {
		t.Fatalf("peer got %v", rr)
	}
	back := dialRendezvous(t, env, false)
	back.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_RelayResponse{RelayResponse: &pb.RelayResponse{
		SocketAddr: rr.SocketAddr, Version: "1.5.0"}}})
	if r := ctl.recv().GetRelayResponse(); r == nil || r.RefuseReason != "" || r.RelayServer == "" {
		t.Fatalf("controller got %v", r)
	}

	// Online query on the aux port.
	ac, _ := net.Dial("tcp", env.aux)
	writeFrame(ac, marshal(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_OnlineRequest{OnlineRequest: &pb.OnlineRequest{
		Id: "x", Peers: []string{"999999999", "222333444"}}}}))
	ac.SetReadDeadline(time.Now().Add(3 * time.Second))
	b, err := readFrame(bufio.NewReader(ac), MaxFrame)
	if err != nil {
		t.Fatal(err)
	}
	var m pb.RendezvousMessage
	proto.Unmarshal(b, &m)
	if st := m.GetOnlineResponse().GetStates(); len(st) != 1 || st[0] != 0b0100_0000 {
		t.Fatalf("states %08b", st)
	}

	// Another machine cannot take the registered id.
	ra, _ := net.ResolveUDPAddr("udp", env.udp)
	u, _ := net.DialUDP("udp", nil, ra)
	thief := &udpPeer{t: t, c: u}
	thief.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_RegisterPk{RegisterPk: &pb.RegisterPk{
		Id: "222333444", Uuid: []byte("other"), Pk: make([]byte, 32)}}})
	if r := thief.recv().GetRegisterPkResponse(); r.GetResult() != pb.RegisterPkResponse_UUID_MISMATCH {
		t.Fatalf("takeover: %v", r)
	}
}

func TestTCPRegistration(t *testing.T) {
	env := startServer(t)
	peer := dialRendezvous(t, env, true)
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	peer.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_RegisterPk{RegisterPk: &pb.RegisterPk{
		Id: "555666777", Uuid: []byte("u"), Pk: pk}}})
	r := peer.recv().GetRegisterPkResponse()
	if r.GetResult() != pb.RegisterPkResponse_OK || r.KeepAlive != keepAlive {
		t.Fatalf("tcp register: %v", r)
	}
	if !env.srv.IsOnline("555666777") {
		t.Fatal("tcp peer not online")
	}
	// Requests reach the peer over its (encrypted) registration connection.
	ctl := dialRendezvous(t, env, false)
	ctl.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_PunchHoleRequest{PunchHoleRequest: &pb.PunchHoleRequest{
		Id: "555666777", LicenceKey: PublicKeyString(env.srv.Key)}}})
	if m := peer.recv(); m.GetFetchLocalAddr() == nil && m.GetPunchHole() == nil {
		t.Fatalf("peer got %v", m)
	}
	peer.c.Close()
	deadline := time.Now().Add(2 * time.Second)
	for env.srv.IsOnline("555666777") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if env.srv.IsOnline("555666777") {
		t.Fatal("peer still online after disconnect")
	}
}

func TestAuthorizeRefuses(t *testing.T) {
	env := startServer(t)
	env.srv.Authorize = func(_ context.Context, r Request) string {
		if r.Token == "outsider" {
			return "Access denied"
		}
		return ""
	}
	registerUDP(t, env, "444555666")
	c := dialRendezvous(t, env, true)
	c.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_PunchHoleRequest{PunchHoleRequest: &pb.PunchHoleRequest{
		Id: "444555666", LicenceKey: PublicKeyString(env.srv.Key), Token: "outsider"}}})
	if r := c.recv().GetPunchHoleResponse(); r.GetOtherFailure() != "Access denied" {
		t.Fatalf("punch: %v", r)
	}
	c.send(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_RequestRelay{RequestRelay: &pb.RequestRelay{
		Id: "444555666", Uuid: "u", Token: "outsider"}}})
	if r := c.recv().GetRelayResponse(); r.GetRefuseReason() != "Access denied" {
		t.Fatalf("relay: %v", r)
	}
}
