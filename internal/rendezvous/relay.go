package rendezvous

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/BorisGB2001/RuConnect/internal/rendezvous/pb"
)

// Relay pairs two clients that present the same uuid (handed out by the
// rendezvous server) and then copies bytes between them. The clients encrypt
// end to end, so the relay never parses anything after the first frame.
type Relay struct {
	// Key is the server public key (base64) clients must present; empty
	// accepts any.
	Key string
	// PairTimeout is how long the first side waits for its partner, and
	// IdleTimeout how long a pair may stay silent.
	PairTimeout, IdleTimeout time.Duration
	Log                      *slog.Logger

	mu      sync.Mutex
	waiting map[string]*relayConn
	active  int
}

// maxRelayWaiting bounds half-open relay requests held in memory.
const maxRelayWaiting = 10000

type relayConn struct {
	net.Conn
	r      *bufio.Reader // may hold bytes read past the first frame
	paired chan net.Conn
}

func NewRelay(key string, log *slog.Logger) *Relay {
	return &Relay{Key: key, PairTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, Log: log,
		waiting: map[string]*relayConn{}}
}

// Serve accepts relay connections until ctx ends.
func (rl *Relay) Serve(ctx context.Context, ln net.Listener) error {
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
		go rl.handle(c)
	}
}

// Active reports the number of relayed sessions.
func (rl *Relay) Active() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.active
}

func (rl *Relay) handle(c net.Conn) {
	r := bufio.NewReader(c)
	c.SetReadDeadline(time.Now().Add(rl.PairTimeout))
	frame, err := readFrame(r, MaxFrame)
	if err != nil {
		c.Close()
		return
	}
	var msg pb.RendezvousMessage
	if proto.Unmarshal(frame, &msg) != nil {
		c.Close()
		return
	}
	req := msg.GetRequestRelay()
	if req == nil || req.Uuid == "" || len(req.Uuid) > 128 {
		c.Close()
		return
	}
	if rl.Key != "" && req.LicenceKey != rl.Key {
		rl.Log.Warn("relay: key mismatch", "remote", c.RemoteAddr())
		c.Close()
		return
	}
	c.SetReadDeadline(time.Time{})
	rl.Log.Debug("relay: request", "remote", c.RemoteAddr(), "uuid", req.Uuid)
	me := &relayConn{Conn: c, r: r, paired: make(chan net.Conn, 1)}

	rl.mu.Lock()
	other, ok := rl.waiting[req.Uuid]
	if ok {
		delete(rl.waiting, req.Uuid)
		rl.active++
	} else if len(rl.waiting) < maxRelayWaiting {
		rl.waiting[req.Uuid] = me
	} else {
		rl.mu.Unlock()
		c.Close()
		return
	}
	rl.mu.Unlock()

	if !ok {
		select {
		case <-me.paired: // the partner's goroutine runs the pipe
		case <-time.After(rl.PairTimeout):
			rl.mu.Lock()
			if rl.waiting[req.Uuid] == me {
				delete(rl.waiting, req.Uuid)
				rl.mu.Unlock()
				c.Close()
				return
			}
			rl.mu.Unlock()
		}
		return
	}
	other.paired <- c
	rl.Log.Debug("relay: paired", "a", other.RemoteAddr(), "b", c.RemoteAddr())
	rl.pipe(other, me)
	rl.mu.Lock()
	rl.active--
	rl.mu.Unlock()
}

// pipe copies both ways until either side closes or the pair goes idle.
func (rl *Relay) pipe(a, b *relayConn) {
	defer a.Close()
	defer b.Close()
	var mu sync.Mutex
	last := time.Now()
	touch := func() { mu.Lock(); last = time.Now(); mu.Unlock() }
	done := make(chan struct{}, 2)
	cp := func(dst net.Conn, src *relayConn) {
		buf := make([]byte, 32<<10)
		for {
			n, err := src.r.Read(buf)
			if n > 0 {
				touch()
				if _, werr := dst.Write(buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			mu.Lock()
			idle := time.Since(last)
			mu.Unlock()
			if idle > rl.IdleTimeout {
				return
			}
		}
	}
}
