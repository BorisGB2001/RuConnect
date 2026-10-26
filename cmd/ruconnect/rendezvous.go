package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BorisGB2001/RuConnect/internal/config"
	"github.com/BorisGB2001/RuConnect/internal/rendezvous"
	"github.com/BorisGB2001/RuConnect/internal/store"
)

// startRendezvous starts the built-in ID server (TCP+UDP port, TCP port-1)
// and relay server; listener failures are reported on errc.
func startRendezvous(ctx context.Context, cfg config.Config, st *store.Store, log *slog.Logger, errc chan<- error) error {
	rc := cfg.Rendezvous
	sk, err := rendezvous.LoadOrCreateKey(rc.PrivateKeyFile, rc.ImportKeyFile)
	if err != nil {
		return fmt.Errorf("ID server key: %w", err)
	}
	key := rendezvous.PublicKeyString(sk)

	rs := rendezvous.NewServer(sk, peerStore{st}, log)
	rs.AlwaysRelay = rc.AlwaysRelay
	rs.Authorize = func(ctx context.Context, req rendezvous.Request) string {
		return authorizeConnection(ctx, st, req)
	}
	hosts := &serverHosts{cfg: cfg, st: st}
	rs.LocalHost, rs.RemoteHost = hosts.local, hosts.remote
	rs.RelayServers = relayServers(cfg)
	if rc.RelayEnabled {
		rs.RelayPort = rc.RelayPort
	}

	// Open every port first, so a busy one leaves nothing half started.
	var closers []io.Closer
	fail := func(err error) error {
		for _, c := range closers {
			c.Close()
		}
		return err
	}
	tcp, err := net.Listen("tcp", ":"+strconv.Itoa(rc.Port))
	if err != nil {
		return fail(err)
	}
	closers = append(closers, tcp)
	aux, err := net.Listen("tcp", ":"+strconv.Itoa(rc.Port-1))
	if err != nil {
		return fail(err)
	}
	closers = append(closers, aux)
	udp, err := net.ListenUDP("udp", &net.UDPAddr{Port: rc.Port})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, udp)
	var rl net.Listener
	if rc.RelayEnabled {
		if rl, err = net.Listen("tcp", ":"+strconv.Itoa(rc.RelayPort)); err != nil {
			return fail(err)
		}
	}
	run := func(name string, f func() error) {
		go func() {
			if err := f(); err != nil {
				errc <- fmt.Errorf("%s: %w", name, err)
			}
		}()
	}
	run("ID server tcp", func() error { return rs.ServeTCP(ctx, tcp) })
	run("ID server aux", func() error { return rs.ServeAux(ctx, aux) })
	run("ID server udp", func() error { return rs.ServeUDP(ctx, udp) })

	if rl != nil {
		relay := rendezvous.NewRelay(key, log)
		run("relay server", func() error { return relay.Serve(ctx, rl) })
	}
	log.Info("ID server started", "port", rc.Port, "relay", rc.RelayEnabled, "relay_port", rc.RelayPort,
		"relay_servers", strings.Join(rs.RelayServers, ","), "key", key)
	return nil
}

// relayServers are the relays handed to clients: RUCONNECT_RELAY_SERVERS,
// else RUCONNECT_RELAY_SERVER, else the ID server host with the relay port.
// Empty lets the ID server hand out the address each client reached it on.
func relayServers(cfg config.Config) []string {
	if len(cfg.Rendezvous.RelayServers) > 0 {
		return cfg.Rendezvous.RelayServers
	}
	if cfg.RelayServer != "" {
		return []string{cfg.RelayServer}
	}
	if cfg.IDServer != "" && cfg.Rendezvous.RelayEnabled {
		host := cfg.IDServer
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		return []string{net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(cfg.Rendezvous.RelayPort))}
	}
	return nil
}

type peerStore struct{ st *store.Store }

func (p peerStore) LoadPeer(ctx context.Context, id string) ([]byte, []byte, bool, error) {
	rp, err := p.st.RendezvousPeer(ctx, id)
	if err == store.ErrNotFound {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	return rp.UUID, rp.PK, true, nil
}

func (p peerStore) SavePeer(ctx context.Context, id string, uuid, pk []byte, ip string) error {
	return p.st.SaveRendezvousPeer(ctx, &store.RendezvousPeer{ID: id, UUID: uuid, PK: pk, IP: ip})
}

// authorizeConnection keeps organizations apart: a client logged in as a
// member of an organization may only reach that organization's devices.
// Clients that are not logged in, and server staff, are not restricted.
func authorizeConnection(ctx context.Context, st *store.Store, req rendezvous.Request) string {
	if req.Token == "" {
		return ""
	}
	u, _, err := st.UserByToken(ctx, req.Token)
	if err != nil || u.OrgID == 0 {
		return ""
	}
	d, err := st.GetDevice(ctx, req.PeerID)
	if err == nil && d.OrgID == u.OrgID {
		return ""
	}
	return "Access denied: this device belongs to another organization"
}

// serverHosts classifies host names in "ID@host" peer ids for online status:
// this server (its configured names, the default site and organization
// domains) or another server an administrator added as a site. The lists
// are re-read at most every 30 seconds.
type serverHosts struct {
	cfg config.Config
	st  *store.Store

	mu            sync.Mutex
	loaded        time.Time
	locals, peers map[string]bool
}

func hostOf(s string) string {
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		return strings.ToLower(u.Hostname())
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	return strings.ToLower(strings.Trim(strings.TrimSpace(s), "[]"))
}

func (h *serverHosts) refresh() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if time.Since(h.loaded) < 30*time.Second {
		return
	}
	ctx := context.Background()
	locals, peers := map[string]bool{}, map[string]bool{}
	for _, s := range []string{h.cfg.IDServer, h.cfg.APIServer, h.cfg.Domain} {
		if s != "" {
			locals[hostOf(s)] = true
		}
	}
	if orgs, err := h.st.ListOrganizations(ctx); err == nil {
		for _, o := range orgs {
			for _, d := range o.Domains {
				locals[d] = true
			}
		}
	}
	if sites, err := h.st.ListServers(ctx); err == nil {
		for _, s := range sites {
			if s.IDServer == "" {
				continue
			}
			if s.IsDefault {
				locals[hostOf(s.IDServer)] = true
			} else {
				peers[hostOf(s.IDServer)] = true
			}
		}
	}
	h.locals, h.peers, h.loaded = locals, peers, time.Now()
}

func (h *serverHosts) local(host string) bool {
	h.refresh()
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.locals[host]
}

func (h *serverHosts) remote(host string) bool {
	h.refresh()
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.peers[host] && !h.locals[host]
}
