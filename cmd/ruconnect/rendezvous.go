package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"

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
	rs.RelayServers = relayServers(cfg)
	if rc.RelayEnabled {
		rs.RelayPort = rc.RelayPort
	}

	tcp, err := net.Listen("tcp", ":"+strconv.Itoa(rc.Port))
	if err != nil {
		return err
	}
	aux, err := net.Listen("tcp", ":"+strconv.Itoa(rc.Port-1))
	if err != nil {
		return err
	}
	udp, err := net.ListenUDP("udp", &net.UDPAddr{Port: rc.Port})
	if err != nil {
		return err
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

	if rc.RelayEnabled {
		rl, err := net.Listen("tcp", ":"+strconv.Itoa(rc.RelayPort))
		if err != nil {
			return err
		}
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
