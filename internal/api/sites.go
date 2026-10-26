package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BorisGB2001/RuConnect/internal/store"
)

// Multi-server support.
//
// A RuConnect instance serves several hbbs/hbbr sites. Clients of a site use
// the API URL "<base>/s/<slug>" (plain "<base>" for the default site), so
// every request tells us which site its client is registered with.
//
// The stock client can reach a peer through another rendezvous server when
// the peer id is written as "ID@host[:port]?key=<public key>". Device lists
// handed to clients use that form for devices of other sites, so one client
// reaches devices on every site without changing its own settings.

type siteKey struct{}

// siteRouter strips a "/s/<slug>" prefix and records the site in the context.
func (s *Server) siteRouter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug := store.DefaultServerSlug
		if rest, ok := strings.CutPrefix(r.URL.Path, "/s/"); ok {
			var tail string
			slug, tail, _ = strings.Cut(rest, "/")
			r = r.Clone(r.Context())
			r.URL.Path = "/" + tail
			r.URL.RawPath = ""
		}
		site, err := s.store.ServerBySlug(r.Context(), slug)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Unknown server "+slug)
			return
		}
		if err != nil {
			s.writeStoreError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), siteKey{}, site)))
	})
}

// site returns the site the request came through.
func site(r *http.Request) *store.Server {
	if v, ok := r.Context().Value(siteKey{}).(*store.Server); ok {
		return v
	}
	return &store.Server{Slug: store.DefaultServerSlug, IsDefault: true}
}

// rendezvous returns the effective ID server and key of a site; the default
// site falls back to the configured address and the hbbs key file.
func (s *Server) rendezvous(v *store.Server) (host, key string) {
	host, key = v.IDServer, v.Key
	if v.IsDefault {
		if host == "" {
			host = s.cfg.IDServer
		}
		if key == "" {
			key = s.publicKey()
		}
	}
	return host, key
}

// relayOf returns the effective relay address of a site: the stored one, the
// configured one for the default site, else the ID server host (hbbr usually
// runs next to hbbs).
func (s *Server) relayOf(v *store.Server) string {
	relay := v.RelayServer
	if relay == "" && v.IsDefault {
		relay = s.cfg.RelayServer
	}
	if relay == "" {
		host, _ := s.rendezvous(v)
		if h, _, err := net.SplitHostPort(host); err == nil {
			relay = h
		} else {
			relay = host
		}
	}
	return relay
}

// peerResolver rewrites device ids for clients of one site.
type peerResolver struct {
	from    int64
	servers map[int64]*store.Server
	device  map[string]int64
	addr    map[int64]string // site id -> "@host?key=..." suffix
	hosts   map[string]bool  // known rendezvous hosts, for normalizing ids back
}

func (s *Server) newPeerResolver(r *http.Request, ids []string) (*peerResolver, error) {
	ctx := r.Context()
	list, err := s.store.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	pr := &peerResolver{from: site(r).ID, servers: map[int64]*store.Server{}, addr: map[int64]string{}, hosts: map[string]bool{}}
	for _, v := range list {
		pr.servers[v.ID] = v
		host, key := s.rendezvous(v)
		if host == "" {
			continue
		}
		pr.hosts[strings.ToLower(host)] = true
		suffix := "@" + host
		if key != "" {
			suffix += "?key=" + key
		}
		pr.addr[v.ID] = suffix
	}
	if pr.device, err = s.store.DeviceServers(ctx, ids); err != nil {
		return nil, err
	}
	return pr, nil
}

// address returns the id a client of the request's site should dial.
func (pr *peerResolver) address(id string) string {
	if strings.Contains(id, "@") {
		return id
	}
	sid, ok := pr.device[id]
	if !ok || sid == pr.from {
		return id
	}
	if suffix, ok := pr.addr[sid]; ok {
		return id + suffix
	}
	return id
}

// normalizePeerID turns "ID@host?key=..." back into "ID" when host is one of
// our sites, so address book entries keep their canonical id.
func (s *Server) normalizePeerID(r *http.Request, id string) string {
	raw, rest, ok := strings.Cut(id, "@")
	if !ok {
		return id
	}
	host, _, _ := strings.Cut(rest, "?")
	host = strings.ToLower(host)
	list, err := s.store.ListServers(r.Context())
	if err != nil {
		return id
	}
	for _, v := range list {
		if h, _ := s.rendezvous(v); h != "" && strings.EqualFold(h, host) {
			return raw
		}
	}
	return id
}

// Panel API for sites.

func (s *Server) adminListServers(w http.ResponseWriter, r *http.Request, u *store.User) {
	list, err := s.store.ListServers(r.Context())
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	type item struct {
		*store.Server
		// Effective values: for the default site, stored ones fall back to
		// RUCONNECT_ID_SERVER and the hbbs key file.
		EffectiveIDServer    string `json:"effective_id_server"`
		EffectiveRelayServer string `json:"effective_relay_server"`
		EffectiveKey         string `json:"effective_key"`
	}
	out := make([]item, 0, len(list))
	for _, v := range list {
		host, key := s.rendezvous(v)
		out = append(out, item{Server: v, EffectiveIDServer: host, EffectiveRelayServer: s.relayOf(v), EffectiveKey: key})
	}
	writeList(w, len(out), out)
}

func (s *Server) serverInput(w http.ResponseWriter, r *http.Request) (store.ServerInput, bool) {
	var req struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		IDServer    string `json:"id_server"`
		RelayServer string `json:"relay_server"`
		Key         string `json:"key"`
		Note        string `json:"note"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return store.ServerInput{}, false
	}
	return store.ServerInput{Name: req.Name, Slug: req.Slug, IDServer: req.IDServer, RelayServer: req.RelayServer,
		Key: req.Key, Note: req.Note}, true
}

func (s *Server) writeServerResult(w http.ResponseWriter, v *store.Server, err error) {
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "A server with this slug already exists")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Not found")
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusOK, v)
	}
}

func (s *Server) adminCreateServer(w http.ResponseWriter, r *http.Request, u *store.User) {
	in, ok := s.serverInput(w, r)
	if !ok {
		return
	}
	v, err := s.store.CreateServer(r.Context(), in)
	s.writeServerResult(w, v, err)
}

func (s *Server) adminUpdateServer(w http.ResponseWriter, r *http.Request, u *store.User) {
	in, ok := s.serverInput(w, r)
	if !ok {
		return
	}
	v, err := s.store.UpdateServer(r.Context(), r.PathValue("guid"), in)
	s.writeServerResult(w, v, err)
}

func (s *Server) adminDeleteServer(w http.ResponseWriter, r *http.Request, u *store.User) {
	err := s.store.DeleteServer(r.Context(), r.PathValue("guid"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "Server not found or it is the default server")
		return
	}
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type probeResult struct {
	Address   string `json:"address"`
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// probeTCP checks that host (default port def) accepts TCP connections.
func probeTCP(host string, def int) probeResult {
	if host == "" {
		return probeResult{Error: "not configured"}
	}
	addr := host
	if _, _, err := net.SplitHostPort(host); err != nil {
		addr = net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(def))
	}
	start := time.Now()
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	res := probeResult{Address: addr, LatencyMS: time.Since(start).Milliseconds()}
	if err != nil {
		res.Error = err.Error()
		return res
	}
	c.Close()
	res.OK = true
	return res
}

// adminCheckServer probes a site's hbbs (21116) and hbbr (21117) ports.
func (s *Server) adminCheckServer(w http.ResponseWriter, r *http.Request, u *store.User) {
	v, err := s.store.ServerByGUID(r.Context(), r.PathValue("guid"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	host, _ := s.rendezvous(v)
	relay := s.relayOf(v)
	var wg sync.WaitGroup
	var idRes, relayRes probeResult
	wg.Add(2)
	go func() { defer wg.Done(); idRes = probeTCP(host, 21116) }()
	go func() { defer wg.Done(); relayRes = probeTCP(relay, 21117) }()
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]any{"id_server": idRes, "relay_server": relayRes})
}
