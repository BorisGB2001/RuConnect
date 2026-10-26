package rendezvous

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/BorisGB2001/RuConnect/internal/rendezvous/pb"
)

// Online status across servers.
//
// The client asks its own ID server for the online state of every peer in
// its lists, using the IDs as stored, so peers on other servers arrive as
// "ID@host[:port]?key=...". For those the server asks the other server
// (which must be one an administrator added, see RemoteHost) and merges the
// answers, so one client sees who is online on every server.

const (
	federationTimeout = 1500 * time.Millisecond // the client gives up after 3 s
	federationTTL     = 15 * time.Second
	maxRemoteHosts    = 16
)

// splitPeerID splits "ID@host[:port]?key=..." into the ID and the host
// (lowercase, port kept); host is empty for plain IDs.
func splitPeerID(s string) (id, host string) {
	id, rest, ok := strings.Cut(s, "@")
	if !ok {
		return s, ""
	}
	host, _, _ = strings.Cut(rest, "?")
	return id, strings.ToLower(host)
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.Trim(h, "[]")
	}
	return strings.Trim(hostport, "[]")
}

// onlineAddr is where a server answers online queries: its ID port minus one.
func onlineAddr(hostport string) string {
	host, port := hostOnly(hostport), 21116
	if _, p, err := net.SplitHostPort(hostport); err == nil {
		if n, err := strconv.Atoi(p); err == nil && n > 1 {
			port = n
		}
	}
	return net.JoinHostPort(host, strconv.Itoa(port-1))
}

type onlineCache struct {
	mu sync.Mutex
	m  map[string]cachedState
}

type cachedState struct {
	online bool
	at     time.Time
}

func (c *onlineCache) get(key string) (bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	if !ok || time.Since(v.at) > federationTTL {
		return false, false
	}
	return v.online, true
}

func (c *onlineCache) put(key string, online bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil || len(c.m) > 100000 {
		c.m = map[string]cachedState{}
	}
	c.m[key] = cachedState{online, time.Now()}
}

// onlineStates answers an OnlineRequest: a bitmap, most significant bit first.
func (s *Server) onlineStates(ctx context.Context, peers []string) []byte {
	states := make([]byte, (len(peers)+7)/8)
	set := func(i int) { states[i/8] |= 1 << (7 - i%8) }
	type ask struct {
		idx []int
		ids []string
	}
	remote := map[string]*ask{}
	for i, p := range peers {
		id, host := splitPeerID(p)
		switch {
		case host == "" || s.LocalHost != nil && s.LocalHost(hostOnly(host)):
			if s.IsOnline(id) {
				set(i)
			}
		case s.RemoteHost != nil && s.RemoteHost(hostOnly(host)):
			if on, ok := s.fedCache.get(host + "|" + id); ok {
				if on {
					set(i)
				}
				continue
			}
			a := remote[host]
			if a == nil {
				if len(remote) >= maxRemoteHosts {
					continue
				}
				a = &ask{}
				remote[host] = a
			}
			a.idx = append(a.idx, i)
			a.ids = append(a.ids, id)
		}
	}
	if len(remote) == 0 {
		return states
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for host, a := range remote {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := queryOnline(ctx, onlineAddr(host), a.ids)
			if err != nil {
				s.Log.Debug("rendezvous: remote online query", "server", host, "err", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for j, on := range res {
				s.fedCache.put(host+"|"+a.ids[j], on)
				if on {
					set(a.idx[j])
				}
			}
		}()
	}
	wg.Wait()
	return states
}

// queryOnline asks another ID server for the online state of ids.
func queryOnline(ctx context.Context, addr string, ids []string) ([]bool, error) {
	ctx, cancel := context.WithTimeout(ctx, federationTimeout)
	defer cancel()
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	deadline, _ := ctx.Deadline()
	c.SetDeadline(deadline)
	if err := writeFrame(c, marshal(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_OnlineRequest{
		OnlineRequest: &pb.OnlineRequest{Peers: ids}}})); err != nil {
		return nil, err
	}
	r := bufio.NewReader(c)
	for i := 0; i < 2; i++ { // a server may open with a KeyExchange
		b, err := readFrame(r, MaxFrame)
		if err != nil {
			return nil, err
		}
		var m pb.RendezvousMessage
		if proto.Unmarshal(b, &m) != nil {
			continue
		}
		if or := m.GetOnlineResponse(); or != nil {
			out := make([]bool, len(ids))
			for j := range ids {
				if j/8 < len(or.States) {
					out[j] = or.States[j/8]&(1<<(7-j%8)) != 0
				}
			}
			return out, nil
		}
	}
	return nil, net.ErrClosed
}
